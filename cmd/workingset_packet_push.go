package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// staleUnchangedSections — REQ-CROSS-384 (EPIC-CLI-018): the filled, served,
// content-unchanged sections whose scope-context stamp is stale (the server
// lists them under facts.sections.missing at the current context), as update
// puts of their current content so the server re-stamps them. With all, every
// such section regardless of staleness (--restamp). A section pushed in this
// same call is excluded — it already carries the new stamp — and so is an
// unfilled stub or a section the store never held (that is authoring, not a
// re-stamp). A server that serves no facts has nothing to judge: nothing.
func staleUnchangedSections(env *factoryEnv, dir, scopeKind, scopeExt string, exclude map[string]bool, all bool) ([]plannedSection, error) {
	entries, err := localPacketFiles(dir)
	if err != nil || len(entries) == 0 {
		return nil, err
	}
	stale := map[string]bool{}
	if !all {
		resp, err := readDeliveryContextFor(env, scopeExt)
		if err != nil || resp.Data.Facts == nil {
			return nil, nil
		}
		for _, k := range resp.Data.Facts.Sections.Missing {
			stale[k] = true
		}
		if len(stale) == 0 {
			return nil, nil
		}
	}
	sections, err := fetchList(env,
		fmt.Sprintf("/api/v1/sync/packet-sections?system_id=%d&scope=%s:%s", env.SystemID, scopeKind, scopeExt),
		"packet_sections")
	if err != nil {
		return nil, err
	}
	served := map[string]map[string]any{}
	for _, s := range sections {
		sm, _ := s.(map[string]any)
		served[str(sm, "section_key")] = sm
	}
	pulled := readPacketFingerprints(dir)
	var out []plannedSection
	for _, e := range entries {
		key := sectionKeyFromFile(e.Name())
		srv := served[key]
		if exclude[key] || srv == nil || (!all && !stale[key]) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, "packet", e.Name()))
		if err != nil {
			return nil, err
		}
		content := string(raw)
		if unfilledPacketSection(content, key, scopeKind, scopeExt) {
			continue
		}
		body := stripPacketStub(content, key, scopeKind, scopeExt)
		if str(srv, "content") != body {
			continue // a changed file was planned as an ordinary put already
		}
		expected := pulled[key]
		if expected == "" {
			expected = str(srv, "content_fingerprint")
		}
		out = append(out, plannedSection{key: key, filename: e.Name(), content: body, action: "update", expected: expected, raw: append([]byte(nil), raw...)})
	}
	return out, nil
}

// plannedSection is one packet-section whole-blob write the plan pass computed
// but has not yet issued (the validate-all-before-any-write contract).
type plannedSection struct {
	key         string
	filename    string // local packet filename captured during staging
	content     string
	action      string // "create" | "update"
	expected    string // pull-time CAS fingerprint for an update
	raw         []byte // exact local file bytes staged during validation
	fingerprint string // canonical server fingerprint for a no-op reconciliation
}

// planPacketSections reads every local packet file and computes which changed vs
// the served snapshot, WITHOUT posting — the plan half of the push's validate-
// all-before-any-write pass. A read error aborts before any write.
func planPacketSections(env *factoryEnv, dir, scopeKind, scopeExt string, plan, skipped *[]string) ([]plannedSection, []plannedSection, error) {
	entries, err := localPacketFiles(dir)
	if err != nil {
		return nil, nil, err
	}
	served := map[string]map[string]any{}
	status, response, err := env.call("GET",
		fmt.Sprintf("/api/v1/sync/packet-sections?system_id=%d&scope=%s:%s", env.SystemID, scopeKind, scopeExt),
		nil)
	// Pull permits an absent packet endpoint. It is also optional for an
	// item-only push, but local packet files still require a canonical read.
	if err == nil && status == 404 && len(entries) == 0 {
		return nil, nil, nil
	}
	if err == nil && status != 200 {
		err = serverRefusal("", status, response)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("could not read packet sections before push: %w", err)
	}
	sections, err := listFromData(response, "packet_sections")
	if err != nil {
		return nil, nil, fmt.Errorf("could not read packet sections before push: %w", err)
	}
	for _, s := range sections {
		sm, ok := s.(map[string]any)
		if !ok || str(sm, "section_key") == "" {
			return nil, nil, fmt.Errorf("packet-section read contains an invalid entry — refusing push")
		}
		served[str(sm, "section_key")] = sm
	}
	pulled := readPacketFingerprints(dir)
	var planned []plannedSection
	var reconcile []plannedSection
	for _, e := range entries {
		key := sectionKeyFromFile(e.Name())
		raw, err := os.ReadFile(filepath.Join(dir, "packet", e.Name()))
		if err != nil {
			return nil, nil, err
		}
		content := string(raw)
		// REQ-CROSS-331: a blank file or an unfilled stub is not authored — skip
		// it and name it, never send an empty whole-blob put and never empty a
		// served section.
		if unfilledPacketSection(content, key, scopeKind, scopeExt) {
			*skipped = append(*skipped, fmt.Sprintf("packet section %s — unfilled stub, not pushed (%s)", key, e.Name()))
			continue
		}
		// A filled stub is sent with the marker line removed; the unchanged-file
		// comparison and the CAS plan run on that stripped body (D3).
		body := stripPacketStub(content, key, scopeKind, scopeExt)
		srv := served[key]
		if srv != nil && str(srv, "content") == body {
			fingerprint := str(srv, "content_fingerprint")
			baselineName := filepath.Join("packet", e.Name())
			entry, known, berr := readScopedDraftBaselineEntry(dir, baselineName)
			if berr != nil || !known {
				return nil, nil, manualScopedRecovery(baselineName, "has no usable local draft baseline for retry reconciliation")
			}
			baselineDrift := sha256Hex(raw) != entry.SHA256
			casDrift := pulled[key] != fingerprint
			if baselineDrift || casDrift {
				// A never-served scaffold has no pull-time CAS. Equal canonical
				// content plus the staged-byte check can recover its accepted create.
				if fingerprint == "" || (pulled[key] == "" && entry.Origin != "stub") {
					return nil, nil, manualScopedRecovery(filepath.Join("packet", e.Name()), "has no usable packet CAS fingerprint for retry reconciliation")
				}
				reconcile = append(reconcile, plannedSection{key: key, filename: e.Name(), content: body, raw: append([]byte(nil), raw...), fingerprint: fingerprint})
			}
			continue
		}
		*plan = append(*plan, fmt.Sprintf("packet section %s (whole-blob put)", key))
		ps := plannedSection{key: key, filename: e.Name(), content: body, action: "create", raw: append([]byte(nil), raw...)}
		if srv != nil {
			ps.action = "update"
			// A fresh server fingerprint cannot authorize overwriting a version
			// this local file never observed, including a concurrently created section.
			ps.expected = pulled[key]
			if ps.expected == "" {
				return nil, nil, manualScopedRecovery(filepath.Join("packet", e.Name()), "has no pull-time packet CAS fingerprint; nothing was written")
			}
		}
		planned = append(planned, ps)
	}
	return planned, reconcile, nil
}

// applyPacketSections posts the planned packet-section writes — the apply half,
// run only after every item and section has validated.
func applyPacketSections(env *factoryEnv, dir, ctxID, scopeKind, scopeExt string, planned []plannedSection) ([]string, error) {
	pulled := readPacketFingerprints(dir)
	conflicts := []string{}
	for _, ps := range planned {
		record := map[string]any{"kind": "packet_section", "scope_kind": scopeKind,
			"scope_external_id": scopeExt, "section_key": ps.key, "content": ps.content,
			"authoring_context_id": ctxID}
		if ps.action == "update" {
			record["expected_fingerprint"] = ps.expected
		}
		name := filepath.Join("packet", ps.filename)
		adoptCreatedPacket := false
		if ps.action == "create" {
			_, known, baselineErr := readScopedDraftBaselineEntry(dir, name)
			adoptCreatedPacket = baselineErr == nil && !known
		}
		status, resp, err := postAuthor(env, map[string]any{"action": ps.action, "record": record})
		if err != nil {
			return conflicts, fmt.Errorf("packet section %s: %v", ps.key, err)
		}
		switch {
		case status == 409:
			conflicts = append(conflicts, "packet:"+ps.key)
		case status != 200:
			return conflicts, serverRefusal("packet section "+ps.key, status, resp)
		default:
			path := filepath.Join(dir, name)
			current, rerr := os.ReadFile(path)
			if rerr != nil || !bytes.Equal(current, ps.raw) {
				return conflicts, fmt.Errorf("packet section %s was accepted, but %s changed while it was being pushed; its bytes and local baseline were left untouched", ps.key, name)
			}
			if stripPacketStub(string(current), ps.key, scopeKind, scopeExt) != ps.content {
				return conflicts, fmt.Errorf("packet section %s was accepted, but %s no longer matches the staged content; preserve the local scope and inspect the accepted write", ps.key, name)
			}
			fingerprint := ""
			if data, ok := resp["data"].(map[string]any); ok {
				if obj, ok := data["packet_section"].(map[string]any); ok {
					fingerprint = str(obj, "fingerprint")
				}
			}
			if fingerprint == "" || adoptCreatedPacket {
				var cerr error
				fingerprint, cerr = canonicalPacketFingerprint(env, scopeKind, scopeExt, ps.key, ps.content)
				if cerr != nil {
					return conflicts, fmt.Errorf("packet section %s was accepted, but its canonical read failed; local bytes and baselines were preserved: %w", ps.key, cerr)
				}
			}
			pulled[ps.key] = fingerprint
			if err := writePacketFingerprints(dir, pulled); err != nil {
				return conflicts, fmt.Errorf("packet section %s was accepted, but its CAS metadata could not be refreshed: %w", ps.key, err)
			}
			if err := refreshScopedDraftBaseline(dir, name, ps.raw, adoptCreatedPacket); err != nil {
				return conflicts, fmt.Errorf("packet section %s was accepted and its CAS metadata refreshed, but its local draft baseline could not be refreshed: %w", ps.key, err)
			}
		}
	}
	return conflicts, nil
}

func canonicalPacketFingerprint(env *factoryEnv, scopeKind, scopeExt, key, expectedContent string) (string, error) {
	sections, err := fetchList(env,
		fmt.Sprintf("/api/v1/sync/packet-sections?system_id=%d&scope=%s:%s", env.SystemID, scopeKind, scopeExt),
		"packet_sections")
	if err != nil {
		return "", err
	}
	var matched map[string]any
	for _, section := range sections {
		item, ok := section.(map[string]any)
		if !ok {
			return "", fmt.Errorf("canonical packet read contains an invalid entry")
		}
		if str(item, "section_key") != key {
			continue
		}
		if matched != nil {
			return "", fmt.Errorf("canonical packet read repeats section %s", key)
		}
		matched = item
	}
	if matched == nil || str(matched, "content") != expectedContent {
		return "", fmt.Errorf("canonical packet read did not confirm authored content for %s", key)
	}
	fingerprint := str(matched, "content_fingerprint")
	if fingerprint == "" {
		fingerprint = str(matched, "fingerprint")
	}
	if fingerprint == "" {
		return "", fmt.Errorf("canonical packet read has no fingerprint for %s", key)
	}
	return fingerprint, nil
}

func reconcilePacketSections(dir, scopeKind, scopeExt string, staged []plannedSection) error {
	pulled := readPacketFingerprints(dir)
	for _, ps := range staged {
		name := filepath.Join("packet", ps.filename)
		current, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !bytes.Equal(current, ps.raw) {
			return fmt.Errorf("%s changed during packet retry reconciliation; authored bytes were left untouched", name)
		}
		if stripPacketStub(string(current), ps.key, scopeKind, scopeExt) != ps.content {
			return manualScopedRecovery(name, "no longer matches the accepted packet content")
		}
		if _, known, err := readScopedDraftBaselineEntry(dir, name); err != nil || !known {
			return manualScopedRecovery(name, "has no usable local draft baseline for retry reconciliation")
		}
	}
	for _, ps := range staged {
		name := filepath.Join("packet", ps.filename)
		if pulled[ps.key] != ps.fingerprint {
			pulled[ps.key] = ps.fingerprint
			if err := writePacketFingerprints(dir, pulled); err != nil {
				return fmt.Errorf("packet section %s matches the accepted write, but its CAS metadata could not be refreshed: %w", ps.key, err)
			}
		}
		if err := refreshScopedDraftBaseline(dir, name, ps.raw, false); err != nil {
			return fmt.Errorf("packet section %s matches the accepted write, but its local draft baseline could not be refreshed: %w", ps.key, err)
		}
	}
	return nil
}

func localPacketFiles(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(filepath.Join(dir, "packet"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []os.DirEntry
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			files = append(files, entry)
		}
	}
	return files, nil
}
