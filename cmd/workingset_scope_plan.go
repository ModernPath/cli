package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// requiredPacketKeys is the canonical section keys the phase table requires for
// a scope: the fixed four (reconnaissance, state_inventory, red_strategy,
// decisions — SR-CLI-028-001), plus enrichment:<SR> for each selected system-
// requirement member the store index knows (for a single_sr scope, its own SR).
// A user-requirement member and a member the index does not know get none —
// mirroring the server, which never requires them (REQ-CROSS-332 PD-5).
// requiredSectionKeys returns the section keys the plan check requires for the
// scope as the server serves them (facts.sections.required, REQ-CROSS-383);
// when the server serves none — it predates the key, or the piece cannot be
// read — the frozen-list derivation is the fallback and the caller is told.
func requiredSectionKeys(env *factoryEnv, scopeExt, scopeKind string, members []string, idx map[string]scopeRecord) []string {
	if resp, err := readDeliveryContextFor(env, scopeExt); err == nil && resp.Data.Facts != nil && resp.Data.Facts.Sections.Required != nil {
		return resp.Data.Facts.Sections.Required
	}
	printWarning("the server serves no required section keys for %s — scaffolding from the frozen member list; `process check --phase plan` is the authority", scopeExt)
	return requiredPacketKeys(scopeKind, scopeExt, members, idx)
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

// pullPacketSections writes the served sections and returns them, so the
// review bundle renders the same read the files hold.
func pullPacketSections(env *factoryEnv, dir, scopeKind, scopeExt string, scaffold bool, requiredKeys []string) ([]map[string]any, error) {
	scope := scopeKind + ":" + scopeExt
	status, body, err := env.call("GET",
		fmt.Sprintf("/api/v1/sync/packet-sections?system_id=%d&scope=%s", env.SystemID, scope), nil)
	if err != nil {
		// A transport failure is a FAILED pull, not honest absence — swallowing it
		// left a reused scope dir's prior packet files in place, stamped into the
		// new context.
		return nil, err
	}
	if status == 404 {
		// A genuinely absent endpoint (an older server): honest absence. Remove any
		// packet files a prior pull into this reused scope dir left, so stale
		// content is never carried into the new context.
		return nil, os.RemoveAll(filepath.Join(dir, "packet"))
	}
	if status != 200 {
		return nil, serverRefusal("packet-sections read", status, body)
	}
	sections, err := listFromData(body, "packet_sections")
	if err != nil {
		return nil, err
	}
	served := map[string]string{}
	rows := []map[string]any{}
	for _, s := range sections {
		sm, _ := s.(map[string]any)
		key := str(sm, "section_key")
		if key == "" {
			continue
		}
		rows = append(rows, sm)
		fname := packetFileName(key)
		if unsafeSnapshotName(fname) {
			return nil, fmt.Errorf("packet section key %q maps to an unsafe file name %q — refusing to pull", key, fname)
		}
		if reservedPacketCollision(key) {
			return nil, fmt.Errorf("extra packet section key %q maps to the reserved canonical file name %q — rename the extra section", key, fname)
		}
		if err := atomicWrite(filepath.Join(dir, "packet", fname), []byte(str(sm, "content"))); err != nil {
			return nil, err
		}
		served[key] = str(sm, "content_fingerprint")
	}
	// Record the fingerprint each section was pulled at (#6) so push can carry it
	// as the whole-blob CAS expectation — a concurrent change then 409s instead of
	// being silently overwritten, the same guarantee item files get from their
	// served-fingerprint header. A dotfile, skipped by push's `.md` scan.
	blob, _ := json.Marshal(served)
	if err := atomicWrite(packetFingerprintManifest(dir), blob); err != nil {
		return nil, err
	}

	// REQ-CROSS-332: on an authoring pull, lay down a stub for every canonical
	// section the phase table requires that the store does not serve, so the
	// author sees what the loop expects of the scope just pulled. A stub never
	// overwrites a local file (PD-3) and is not recorded in the served-fingerprint
	// sidecar (PD-4). Not on --for-review (PD-2), not on a 404 (returned above).
	if scaffold {
		for _, key := range requiredKeys {
			if _, isServed := served[key]; isServed {
				continue
			}
			fname := packetFileName(key)
			if unsafeSnapshotName(fname) {
				continue
			}
			path := filepath.Join(dir, "packet", fname)
			if _, err := os.Stat(path); err == nil {
				continue // never overwrite a local file
			}
			if err := atomicWrite(path, []byte(packetStubMarker(key, scopeKind, scopeExt)+"\n")); err != nil {
				return nil, err
			}
		}
	}
	return rows, nil
}
