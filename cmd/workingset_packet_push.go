package cmd

import (
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
	entries, _ := os.ReadDir(filepath.Join(dir, "packet"))
	var out []plannedSection
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
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
		out = append(out, plannedSection{key: key, content: body, action: "update", expected: expected})
	}
	return out, nil
}

// plannedSection is one packet-section whole-blob write the plan pass computed
// but has not yet issued (the validate-all-before-any-write contract).
type plannedSection struct {
	key      string
	content  string
	action   string // "create" | "update"
	expected string // pull-time CAS fingerprint for an update
}

// planPacketSections reads every local packet file and computes which changed vs
// the served snapshot, WITHOUT posting — the plan half of the push's validate-
// all-before-any-write pass. A read error aborts before any write.
func planPacketSections(env *factoryEnv, dir, scopeKind, scopeExt string, plan, skipped *[]string) ([]plannedSection, error) {
	served := map[string]map[string]any{}
	if sections, err := fetchList(env,
		fmt.Sprintf("/api/v1/sync/packet-sections?system_id=%d&scope=%s:%s", env.SystemID, scopeKind, scopeExt),
		"packet_sections"); err == nil {
		for _, s := range sections {
			sm, _ := s.(map[string]any)
			served[str(sm, "section_key")] = sm
		}
	}
	pulled := readPacketFingerprints(dir)
	entries, _ := os.ReadDir(filepath.Join(dir, "packet"))
	var planned []plannedSection
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		key := sectionKeyFromFile(e.Name())
		raw, err := os.ReadFile(filepath.Join(dir, "packet", e.Name()))
		if err != nil {
			return nil, err
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
			continue
		}
		*plan = append(*plan, fmt.Sprintf("packet section %s (whole-blob put)", key))
		ps := plannedSection{key: key, content: body, action: "create"}
		if srv != nil {
			ps.action = "update"
			// The CAS expectation is the fingerprint recorded at PULL, not the one
			// re-read just now — otherwise a concurrent writer's change would be
			// adopted as the expectation and silently overwritten. Fall back to the
			// current fingerprint only when no pull-time record exists (a hand-made
			// file), which at least preserves prior behaviour.
			ps.expected = pulled[key]
			if ps.expected == "" {
				ps.expected = str(srv, "content_fingerprint")
			}
		}
		planned = append(planned, ps)
	}
	return planned, nil
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
			// Refresh the pull-time CAS sidecar with the fingerprint the server just
			// returned, so a second local edit without a re-pull carries the CURRENT
			// fingerprint instead of the stale pull-time one and deterministically
			// 409s.
			if data, ok := resp["data"].(map[string]any); ok {
				if obj, ok := data["packet_section"].(map[string]any); ok {
					if nf := str(obj, "fingerprint"); nf != "" {
						pulled[ps.key] = nf
						writePacketFingerprints(dir, pulled)
					}
				}
			}
		}
	}
	return conflicts, nil
}
