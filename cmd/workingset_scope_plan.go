package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func requiredSectionKeys(env *factoryEnv, scopeExt, scopeKind string, members []string, idx map[string]scopeRecord) ([]string, error) {
	resp, err := readDeliveryContextFor(env, scopeExt)
	if err == nil && resp.Data.Facts != nil && resp.Data.Facts.Sections.Required != nil {
		return resp.Data.Facts.Sections.Required, nil
	}
	var responseErr *deliveryContextHTTPError
	if err != nil && (!errors.As(err, &responseErr) || responseErr.StatusCode != 404) {
		return nil, fmt.Errorf("could not read required packet sections before scoped pull: %w", err)
	}
	printWarning("the server serves no required section keys for %s — scaffolding from the frozen member list; `process check --phase plan` is the authority", scopeExt)
	return requiredPacketKeys(scopeKind, scopeExt, members, idx), nil
}

// requiredPacketKeys derives the canonical keys from the frozen selection
// members — the fallback when the server serves no required list.
func requiredPacketKeys(scopeKind, scopeExt string, members []string, idx map[string]scopeRecord) []string {
	keys := []string{"reconnaissance", "state_inventory", "red_strategy", "decisions"}
	isSR := func(id string) bool {
		r, ok := idx[id]
		return ok && r.kind == "system"
	}
	if scopeKind == "single_sr" {
		if isSR(scopeExt) {
			keys = append(keys, "enrichment:"+scopeExt)
		}
		return keys
	}
	for _, m := range members {
		if isSR(m) {
			keys = append(keys, "enrichment:"+m)
		}
	}
	return keys
}

// stagePacketSections fetches and validates the complete packet response before
// the caller starts writing any part of a scoped pull.
func stagePacketSections(env *factoryEnv, dir, scopeKind, scopeExt string, scaffold bool, requiredKeys []string) (stagedPacketSections, error) {
	staged := stagedPacketSections{files: map[string]scopedPullFile{}}
	if err := validateRequiredPacketKeys(requiredKeys); err != nil {
		return stagedPacketSections{}, err
	}
	scope := scopeKind + ":" + scopeExt
	status, body, err := env.call("GET",
		fmt.Sprintf("/api/v1/sync/packet-sections?system_id=%d&scope=%s", env.SystemID, scope), nil)
	if err != nil {
		return stagedPacketSections{}, err
	}
	if status == 404 {
		staged.absent = true
		return staged, nil
	}
	if status != 200 {
		return stagedPacketSections{}, serverRefusal("packet-sections read", status, body)
	}
	sections, err := listFromData(body, "packet_sections")
	if err != nil {
		return stagedPacketSections{}, err
	}
	served := map[string]string{}
	filenames := map[string]string{}
	for _, s := range sections {
		sm, ok := s.(map[string]any)
		if !ok {
			return stagedPacketSections{}, fmt.Errorf("packet section entry is not an object — refusing to pull")
		}
		key := str(sm, "section_key")
		if key == "" {
			return stagedPacketSections{}, fmt.Errorf("packet section has no section_key — refusing to pull")
		}
		staged.sections = append(staged.sections, sm)
		fname := packetFileName(key)
		if unsafeSnapshotName(fname) {
			return stagedPacketSections{}, fmt.Errorf("packet section key %q maps to an unsafe file name %q — refusing to pull", key, fname)
		}
		if reservedPacketCollision(key) {
			return stagedPacketSections{}, fmt.Errorf("extra packet section key %q maps to the reserved canonical file name %q — rename the extra section", key, fname)
		}
		if _, exists := served[key]; exists {
			return stagedPacketSections{}, fmt.Errorf("packet response repeats section key %q — refusing to pull", key)
		}
		if prior, exists := filenames[fname]; exists {
			return stagedPacketSections{}, fmt.Errorf("packet section keys %q and %q map to the same file %q — refusing to pull", prior, key, fname)
		}
		content, ok := sm["content"].(string)
		if !ok {
			return stagedPacketSections{}, fmt.Errorf("packet section %q has no string content — refusing to pull", key)
		}
		staged.files[fname] = scopedPullFile{content: []byte(content), origin: "served-packet", managed: true, packetKey: key}
		filenames[fname] = key
		served[key] = str(sm, "content_fingerprint")
	}
	// Record the fingerprint each section was pulled at (#6) so push can carry it
	// as the whole-blob CAS expectation — a concurrent change then 409s instead of
	// being silently overwritten, the same guarantee item files get from their
	// served-fingerprint header. A dotfile, skipped by push's `.md` scan.
	staged.fingerprints = served

	// REQ-CROSS-332: on an authoring pull, lay down a stub for every canonical
	// section the phase table requires that the store does not serve, so the
	// author sees what the loop expects of the scope just pulled. A stub never
	// overwrites a local file (PD-3) and is not recorded in the served-fingerprint
	// sidecar (PD-4). Not on --for-review (PD-2), not on a 404 (returned above).
	if scaffold {
		for _, key := range requiredKeys {
			fname := packetFileName(key)
			if prior, exists := filenames[fname]; exists && prior != key {
				return stagedPacketSections{}, fmt.Errorf("packet section keys %q and %q map to the same file %q — refusing to pull", prior, key, fname)
			}
			if _, isServed := served[key]; isServed {
				continue
			}
			path := filepath.Join(dir, "packet", fname)
			if _, err := os.Stat(path); err == nil {
				continue // never overwrite a local file
			} else if !os.IsNotExist(err) {
				return stagedPacketSections{}, err
			}
			staged.files[fname] = scopedPullFile{content: []byte(packetStubMarker(key, scopeKind, scopeExt) + "\n"), origin: "stub", managed: true}
			filenames[fname] = key
		}
	}
	return staged, nil
}

func (p *scopedPullPlan) add(name string, content []byte, origin string) {
	managed := origin == "item" || origin == "served-packet" || origin == "stub"
	p.files[filepath.ToSlash(name)] = scopedPullFile{content: content, origin: origin, managed: managed}
}

type scopedPullFile struct {
	content   []byte
	origin    string
	managed   bool
	packetKey string
}

type scopedPullPlan struct {
	files              map[string]scopedPullFile
	packetAbsent       bool
	packetFingerprints map[string]string
}

type stagedPacketSections struct {
	sections     []map[string]any
	files        map[string]scopedPullFile
	fingerprints map[string]string
	absent       bool
}

func validateRequiredPacketKeys(requiredKeys []string) error {
	filenames := map[string]string{}
	keys := map[string]bool{}
	for _, key := range requiredKeys {
		if key == "" {
			return fmt.Errorf("required packet section has an empty key — refusing to pull")
		}
		if keys[key] {
			return fmt.Errorf("required packet section key %q is repeated — refusing to pull", key)
		}
		keys[key] = true
		fname := packetFileName(key)
		if unsafeSnapshotName(fname) {
			return fmt.Errorf("required packet section key %q maps to an unsafe file name %q — refusing to pull", key, fname)
		}
		if reservedPacketCollision(key) {
			return fmt.Errorf("required packet section key %q aliases a reserved canonical file %q — refusing to pull", key, fname)
		}
		if prior, ok := filenames[fname]; ok {
			return fmt.Errorf("required packet section keys %q and %q map to the same file %q — refusing to pull", prior, key, fname)
		}
		filenames[fname] = key
	}
	return nil
}

// Packet CAS is part of each section's refresh, rather than a separately
// sorted file write. The persistence step checkpoints only completed sections.
func (p *scopedPullPlan) addPacket(packet stagedPacketSections) {
	p.packetAbsent = packet.absent
	p.packetFingerprints = packet.fingerprints
	for name, file := range packet.files {
		p.files[filepath.ToSlash(filepath.Join("packet", name))] = file
	}
}
