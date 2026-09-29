package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-CROSS-446 (EPIC-CLI-TURNS): process enter checks the independent
// passing cold review first, then re-stamps the packet sections whose content
// is unchanged but whose scope context moved — re-putting the SERVED content
// under the served content fingerprint, never under --dry-run — re-reads, and
// refuses only the sections still missing, by name. --brief-file takes the
// markdown brief bullets as well as JSON and refuses with the parse error.

func restampServer(t *testing.T, verdict string, missing, missingAfter []string) *ceremonyServer {
	t.Helper()
	cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "PROPOSED", verdict, true, "CR-A", missing), true)
	cs.factsAfterWrite = enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "PROPOSED", verdict, true, "CR-A", missingAfter)
	cs.packetSections = []map[string]any{
		{"section_key": "entry_brief", "content": entryBrief, "content_fingerprint": "ps-brief-fp"},
		{"section_key": "reconnaissance", "content": "the served recon", "content_fingerprint": "ps-recon-fp", "authoring_context_id": "authoring-1"},
	}
	return cs
}

func sectionPuts(cs *ceremonyServer) []map[string]any {
	var out []map[string]any
	for _, a := range cs.authored {
		if rec, _ := a["record"].(map[string]any); str(rec, "kind") == "packet_section" {
			out = append(out, a)
		}
	}
	return out
}

func gateCreates(cs *ceremonyServer) int {
	n := 0
	for _, a := range cs.authored {
		if rec, _ := a["record"].(map[string]any); str(rec, "kind") == "gate" {
			n++
		}
	}
	return n
}

func TestREQCROSS446EnterRestampsStaleUnchangedSectionsAndProceeds(t *testing.T) {
	cs := restampServer(t, "pass", []string{"reconnaissance"}, nil)
	env := enterEnv(t, cs)
	// A local copy that differs must not be what is re-put: the served content is.
	dir := filepath.Join(env.Root, workingSetDir, "EPIC-A", "packet")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "10-recon.md"), []byte("a local draft"), 0o644)

	var err error
	out := captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{}) })
	if err != nil {
		t.Fatalf("stale but unchanged sections are re-stamped and entry proceeds: %v\n%s", err, out)
	}
	puts := sectionPuts(cs)
	if len(puts) != 1 {
		t.Fatalf("one re-put for the stale section, got %v", cs.authored)
	}
	rec, _ := puts[0]["record"].(map[string]any)
	if puts[0]["action"] != "update" || rec["section_key"] != "reconnaissance" || rec["content"] != "the served recon" ||
		rec["expected_fingerprint"] != "ps-recon-fp" {
		t.Errorf("the re-put carries the served content under the served fingerprint, posted %v", puts[0])
	}
	if cs.order[0] != "author:update" || gateCreates(cs) != 1 {
		t.Errorf("the re-stamp comes first, then the gate opens, order %v", cs.order)
	}
	if !strings.Contains(out, "reconnaissance") {
		t.Errorf("the re-stamp is reported:\n%s", out)
	}
}

func TestREQCROSS446EnterWithoutAPassingReviewRefusesBeforeTheRestamp(t *testing.T) {
	cs := restampServer(t, "fail", []string{"reconnaissance"}, nil)
	env := enterEnv(t, cs)
	err := processEnter(env, "EPIC-A", enterOpts{})
	if err == nil || !strings.Contains(err.Error(), "cold-review") {
		t.Fatalf("the cold-review check runs before any re-stamp, got %v", err)
	}
	assertNoWrites(t, cs)
}

func TestREQCROSS446EnterRefusesASectionStillMissingByName(t *testing.T) {
	cs := restampServer(t, "pass", []string{"enrichment:REQ-A-1", "reconnaissance"}, []string{"enrichment:REQ-A-1"})
	env := enterEnv(t, cs)
	var err error
	captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{}) })
	if err == nil || !strings.Contains(err.Error(), "enrichment:REQ-A-1") || strings.Contains(err.Error(), "reconnaissance") {
		t.Fatalf("only the genuinely missing section is refused, by name, got %v", err)
	}
	if gateCreates(cs) != 0 {
		t.Errorf("no gate opens while a section is missing")
	}
}

func TestREQCROSS446EnterDryRunWritesNothing(t *testing.T) {
	cs := restampServer(t, "pass", []string{"reconnaissance"}, nil)
	env := enterEnv(t, cs)
	var err error
	out := captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{dryRun: true}) })
	if err != nil {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	assertNoWrites(t, cs)
	if !strings.Contains(out, "reconnaissance") || !strings.Contains(out, "ENTRY-EPIC-A") {
		t.Errorf("the dry run names the section it would re-stamp and the gate:\n%s", out)
	}
}

func TestREQCROSS446EnterReadsAMarkdownBriefFile(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "PROPOSED", "pass", true, "CR-A", nil), true)
	env := enterEnv(t, cs)
	file := filepath.Join(t.TempDir(), "brief.md")
	if err := os.WriteFile(file, []byte(entryBrief), 0o644); err != nil {
		t.Fatal(err)
	}
	var err error
	captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{briefFile: file}) })
	if err != nil {
		t.Fatalf("a markdown brief file is parsed: %v", err)
	}
	var brief map[string]any
	for _, a := range cs.authored {
		if rec, _ := a["record"].(map[string]any); str(rec, "kind") == "gate" {
			brief, _ = rec["brief"].(map[string]any)
		}
	}
	if str(brief, "what") != "Approve entering EPIC-A." || str(brief, "recommendation") == "" {
		t.Errorf("the gate carries the parsed markdown brief, got %v", brief)
	}
}

func TestREQCROSS446EnterRefusesAnUnreadableBriefFileWithItsError(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "PROPOSED", "pass", true, "CR-A", nil), true)
	env := enterEnv(t, cs)
	file := filepath.Join(t.TempDir(), "brief.md")
	if err := os.WriteFile(file, []byte("- What: only this\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := processEnter(env, "EPIC-A", enterOpts{briefFile: file})
	if err == nil || !strings.Contains(err.Error(), "Why now") || !strings.Contains(err.Error(), file) {
		t.Fatalf("an unparseable brief file is refused with the parse error and the file, got %v", err)
	}
	err = processEnter(env, "EPIC-A", enterOpts{briefFile: filepath.Join(t.TempDir(), "absent.md")})
	if err == nil || !strings.Contains(err.Error(), "absent.md") {
		t.Fatalf("an unreadable brief file is refused with its error, got %v", err)
	}
	assertNoWrites(t, cs)
}
