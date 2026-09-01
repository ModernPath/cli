package cmd

// REQ-CROSS-183 — `modernpath context <REQ-id>` assembles the task's context
// pack from platform data. RED first: these tests describe the seam
// (buildContextPack + renderContextPack + the REQ-id dispatch) before it
// exists, and the offline degradation contract: local sections render from
// the ledger parse, the server section says unavailable — never invented.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// packFixture writes a minimal workspace: one ledger with a row whose
// citations exercise all three resolution states (at-path, basename→resolved,
// nowhere), a user-requirements file carrying the row's UR and its SCN, and
// the cited source files themselves.
func packFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("tasks/DEMO-REQUIREMENTS.md", `# REQUIREMENTS — DEMO (Demo Context)

| ID | Title | Stage | Status | Source | Tests | Code |
|---|---|---|---|---|---|---|
| REQ-DEMO-001 | App boots and greets | MVP | READY | DOC:docs/demo.md | `+"`src/app_test.go`"+` | `+"`src/app.go`"+` |
| REQ-DEMO-002 | Untested thing | MVP | PROPOSED | DOC:docs/demo.md | — | `+"`src/app.go`"+` |

Totals: 2 requirements — 1 READY, 1 PROPOSED

### REQ-DEMO-001 — App boots and greets
- **Status:** READY · **Stage:** MVP
- **UR:** UR-DEMO-001
- **Statement:** The app boots and prints a greeting to the operator.
- **Acceptance criteria:**
  - GIVEN the built binary WHEN it starts THEN a greeting prints.
- **Tests:** `+"`src/app_test.go`"+`
- **Code:** `+"`src/app.go`"+`, `+"`CODE:util.go`"+` (helper), `+"`ghost/missing.go`"+`
- **Log:** —

### REQ-DEMO-002 — Untested thing
- **Status:** PROPOSED · **Stage:** MVP
- **Statement:** A thing without tests.
- **Tests:** —
- **Code:** `+"`src/app.go`"+`
`)

	write("requirements/DEMO-USER-REQUIREMENTS.md", `# USER REQUIREMENTS — DEMO

## UR-DEMO-001 — The operator sees the app come up
- Actor: operator
- Statement: An operator starting the app can tell it is alive.
- Source: USER:2026-08-16:demo
- Validation: SCN-DEMO-001
- Status: READY

### SCN-DEMO-001 — The greeting appears

`+"```gherkin"+`
Scenario: The greeting appears
  Given the built binary
  When the operator starts it
  Then a greeting prints
`+"```"+`
`)

	write("src/app.go", "package main\n\nfunc main() { println(\"hi\") }\n")
	write("src/app_test.go", "package main\n\nimport \"testing\"\n\nfunc TestMain2(t *testing.T) {}\n")
	// cited as bare `util.go` — resolves by basename, not at path
	write("lib/util.go", "package lib\n\nfunc Util() {}\n")
	return root
}

func findCitation(t *testing.T, cites []packCitation, ref string) packCitation {
	t.Helper()
	for _, c := range cites {
		if c.Ref == ref {
			return c
		}
	}
	t.Fatalf("no citation with ref %q in %+v", ref, cites)
	return packCitation{}
}

func TestContextPackAssemblesLocalSections(t *testing.T) {
	root := packFixture(t)

	pack, err := buildContextPack(root, "REQ-DEMO-001", serverUnavailable("not connected"))
	if err != nil {
		t.Fatalf("buildContextPack: %v", err)
	}

	// 1. the row itself, with its ledger + detail-block location
	r := pack.Requirement
	if r.ID != "REQ-DEMO-001" || r.Title != "App boots and greets" {
		t.Fatalf("row identity wrong: %+v", r)
	}
	if r.Status != "READY" || r.Stage != "MVP" || r.Context != "DEMO" {
		t.Fatalf("row status/stage/context wrong: %+v", r)
	}
	if r.Statement != "The app boots and prints a greeting to the operator." {
		t.Fatalf("statement wrong: %q", r.Statement)
	}
	if len(r.Criteria) != 1 || !strings.Contains(r.Criteria[0], "GIVEN the built binary") {
		t.Fatalf("criteria wrong: %+v", r.Criteria)
	}
	if r.Ledger != filepath.Join("tasks", "DEMO-REQUIREMENTS.md") {
		t.Fatalf("ledger path wrong: %q", r.Ledger)
	}
	if r.DetailLine == 0 {
		t.Fatalf("detail-block line not located: %+v", r)
	}
	if r.UR != "UR-DEMO-001" {
		t.Fatalf("UR wrong: %q", r.UR)
	}

	// 2. upward trace: the UR and its SCNs from requirements/*-USER-REQUIREMENTS.md
	if pack.Trace.UR == nil {
		t.Fatalf("upward trace missing: %+v", pack.Trace)
	}
	if pack.Trace.UR.ID != "UR-DEMO-001" || pack.Trace.UR.Status != "READY" {
		t.Fatalf("UR wrong: %+v", pack.Trace.UR)
	}
	if len(pack.Trace.UR.Scenarios) != 1 || pack.Trace.UR.Scenarios[0].ID != "SCN-DEMO-001" {
		t.Fatalf("SCNs wrong: %+v", pack.Trace.UR.Scenarios)
	}

	// 3. cited files with resolution state + size + language
	atPath := findCitation(t, pack.Citations, "src/app.go")
	if atPath.Resolution != "at-path" || atPath.SizeBytes == 0 || atPath.Language != "Go" {
		t.Fatalf("at-path citation wrong: %+v", atPath)
	}
	base := findCitation(t, pack.Citations, "util.go")
	if base.Resolution != "basename" || base.ResolvedPath != filepath.Join("lib", "util.go") {
		t.Fatalf("basename citation wrong: %+v", base)
	}
	nowhere := findCitation(t, pack.Citations, "ghost/missing.go")
	if nowhere.Resolution != "nowhere" || nowhere.SizeBytes != 0 {
		t.Fatalf("nowhere citation wrong: %+v", nowhere)
	}
	tests := findCitation(t, pack.Citations, "src/app_test.go")
	if tests.Field != "tests" {
		t.Fatalf("tests citation not attributed to the tests field: %+v", tests)
	}

	// 5. coverage state: triage summary + test-axis position
	cov := pack.Coverage
	if cov.Citations != len(pack.Citations) {
		t.Fatalf("coverage total %d != %d citations", cov.Citations, len(pack.Citations))
	}
	if cov.AtPath < 2 || cov.Basename != 1 || cov.Nowhere != 1 {
		t.Fatalf("coverage triage wrong: %+v", cov)
	}
	if cov.TestAxis != "tests-cited" {
		t.Fatalf("test axis wrong: %q", cov.TestAxis)
	}
}

func TestContextPackTestAxisDash(t *testing.T) {
	root := packFixture(t)
	pack, err := buildContextPack(root, "REQ-DEMO-002", serverUnavailable("not connected"))
	if err != nil {
		t.Fatalf("buildContextPack: %v", err)
	}
	if pack.Coverage.TestAxis != "dash" {
		t.Fatalf("a — tests cell must report the dash axis, got %q", pack.Coverage.TestAxis)
	}
	if pack.Trace.UR != nil {
		t.Fatalf("a row without a UR must not invent one: %+v", pack.Trace.UR)
	}
}

func TestContextPackOfflineServerSaysUnavailable(t *testing.T) {
	root := packFixture(t)
	pack, err := buildContextPack(root, "REQ-DEMO-001",
		serverUnavailable("not connected — run 'modernpath factory connect'"))
	if err != nil {
		t.Fatalf("buildContextPack: %v", err)
	}
	if pack.Server.Available {
		t.Fatalf("offline pack claims the server: %+v", pack.Server)
	}

	var out bytes.Buffer
	renderContextPack(&out, pack)
	text := out.String()

	for _, want := range []string{
		"REQ-DEMO-001",
		"tasks/DEMO-REQUIREMENTS.md",
		"UR-DEMO-001",
		"SCN-DEMO-001",
		"server: unavailable (not connected — run 'modernpath factory connect')",
		"ghost/missing.go",
		"nowhere",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("rendered pack lacks %q:\n%s", want, text)
		}
	}
	// never invented: no server-ish claims in the offline render
	if strings.Contains(text, "work_status") {
		t.Fatalf("offline render invents server data:\n%s", text)
	}
}

func TestContextPackJSONShape(t *testing.T) {
	root := packFixture(t)
	pack, err := buildContextPack(root, "REQ-DEMO-001", serverUnavailable("offline"))
	if err != nil {
		t.Fatalf("buildContextPack: %v", err)
	}
	raw, err := json.Marshal(pack)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"requirement", "upward_trace", "citations", "server", "coverage"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("json pack lacks %q: %s", key, raw)
		}
	}
	req, _ := decoded["requirement"].(map[string]any)
	if req["id"] != "REQ-DEMO-001" {
		t.Fatalf("json requirement.id wrong: %v", req)
	}
	srv, _ := decoded["server"].(map[string]any)
	if srv["available"] != false || srv["reason"] != "offline" {
		t.Fatalf("json server section wrong: %v", srv)
	}
}

func TestContextPackUnknownIDFails(t *testing.T) {
	root := packFixture(t)
	_, err := buildContextPack(root, "REQ-DEMO-999", serverUnavailable("offline"))
	if err == nil {
		t.Fatal("unknown REQ id must fail")
	}
	if !strings.Contains(err.Error(), "REQ-DEMO-999") {
		t.Fatalf("the error must name the id: %v", err)
	}
}

func TestContextPackDispatchRecognisesReqIDs(t *testing.T) {
	for arg, want := range map[string]bool{
		"REQ-CROSS-183":            true,
		"REQ-DEAL-028":             true,
		"How does auth work?":      false,
		"REQ-CROSS-183 and beyond": false,
		"UR-DEMO-001":              false,
	} {
		if got := isContextPackID(arg); got != want {
			t.Fatalf("isContextPackID(%q) = %v, want %v", arg, got, want)
		}
	}
}
