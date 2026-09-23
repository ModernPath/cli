package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-CROSS-393 (EPIC-CLI-019) — `author backlog` writes one backlog, gap or
// tooling record through the author path, `author update --kind backlog`
// edits it against its fingerprint, and `working-set pull` renders it in the
// canonical BACKLOG.md shape.

func authorCapture(t *testing.T) (*httptest.Server, *map[string]any) {
	t.Helper()
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"backlog": map[string]any{"external_id": "BACKLOG-CROSS-0001", "fingerprint": strings.Repeat("a", 64)},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &got
}

func runRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	resetTreeFlags(rootCmd)
	// Reset after the run too: a flag left Changed leaks into a later test
	// that builds a record from the command directly (authorUpdateRecord).
	t.Cleanup(func() { resetTreeFlags(rootCmd) })
	rootCmd.SetArgs(args)
	defer rootCmd.SetArgs(nil)
	var err error
	out := captureOutput(t, func() { err = rootCmd.Execute() })
	return out, err
}

// (a) The flags post one create with kind backlog and every field.
func TestAuthorBacklogPostsTheRecordThroughCobra(t *testing.T) {
	srv, got := authorCapture(t)
	cobraWorkspace(t, srv)

	out, err := runRoot(t, "author", "backlog", "BACKLOG-CROSS-0001", "--kind", "backlog", "--title", "A discovery",
		"--observed", "the pull refuses the release gate", "--why-unrouted", "owner unclear",
		"--candidate-route", "PROPOSED SR", "--affected", "REQ-CROSS-388,REQ-CROSS-389")
	if err != nil {
		t.Fatalf("author backlog: %v", err)
	}
	record, _ := (*got)["record"].(map[string]any)
	if (*got)["action"] != "create" || record["kind"] != "backlog" || record["external_id"] != "BACKLOG-CROSS-0001" {
		t.Fatalf("want a create of kind backlog, posted %v", *got)
	}
	if record["backlog_kind"] != "backlog" || record["title"] != "A discovery" || record["observation"] != "the pull refuses the release gate" ||
		record["why_unrouted"] != "owner unclear" || record["candidate_route"] != "PROPOSED SR" {
		t.Errorf("record fields incomplete: %v", record)
	}
	if affected, _ := record["affected_external_ids"].([]any); len(affected) != 2 || affected[0] != "REQ-CROSS-388" {
		t.Errorf("affected ids must post as a list, got %v", record["affected_external_ids"])
	}
	if !strings.Contains(out, strings.Repeat("a", 64)) {
		t.Errorf("the fingerprint must be printed:\n%s", out)
	}
}

// A gap needs its kind, affected traces and consequence; the CLI refuses
// before posting, naming the three flags.
func TestAuthorBacklogGapRequiresItsFields(t *testing.T) {
	srv, got := authorCapture(t)
	cobraWorkspace(t, srv)

	_, err := runRoot(t, "author", "backlog", "GAP-CLI-004", "--kind", "gap", "--title", "No carrier", "--observed", "x", "--why-unrouted", "y")
	if err == nil {
		t.Fatal("a gap without its fields must be refused")
	}
	for _, flag := range []string{"--gap-kind", "--affected-trace", "--consequence"} {
		if !strings.Contains(err.Error(), flag) {
			t.Errorf("the refusal must name %s, got %v", flag, err)
		}
	}
	if *got != nil {
		t.Errorf("nothing may be posted for a refused gap, posted %v", *got)
	}
}

// REQ-CROSS-432: a backlog or tooling record has no gap fields. The server
// refuses them; the create refuses them here first, before any request, naming
// each flag given, as it does for a gap missing them.
func TestAuthorBacklogRefusesGapFieldsOnANonGap(t *testing.T) {
	for _, kind := range []string{"backlog", "tooling"} {
		t.Run(kind, func(t *testing.T) {
			srv, got := authorCapture(t)
			cobraWorkspace(t, srv)

			// REQ-CROSS-432 (F-CLI024-R1-03): an unknown gap kind is still a gap
			// flag on a record that is not a gap, and is refused as one.
			_, err := runRoot(t, "author", "backlog", "BACKLOG-CLI-9", "--kind", kind, "--title", "x",
				"--gap-kind", "bogus", "--affected-trace", "REQ-X", "--consequence", "cannot prove X")
			if err == nil {
				t.Fatalf("a %s record must refuse gap fields", kind)
			}
			for _, flag := range []string{"--gap-kind", "--affected-trace", "--consequence"} {
				if !strings.Contains(err.Error(), flag) {
					t.Errorf("the refusal must name %s, got %v", flag, err)
				}
			}
			if *got != nil {
				t.Errorf("nothing may be posted, posted %v", *got)
			}
		})
	}
}

// (c) A disposition change rides `author update --kind backlog` with its
// source and the expected fingerprint.
func TestAuthorUpdateBacklogPostsDispositionAndSource(t *testing.T) {
	srv, got := authorCapture(t)
	cobraWorkspace(t, srv)

	_, err := runRoot(t, "author", "update", "BACKLOG-CROSS-0004", "--kind", "backlog",
		"--disposition", "ROUTED to REQ-CROSS-388", "--source", "USER:2026-09-13: routed", "--expected-fingerprint", strings.Repeat("b", 64))
	if err != nil {
		t.Fatalf("author update --kind backlog: %v", err)
	}
	record, _ := (*got)["record"].(map[string]any)
	if (*got)["action"] != "update" || record["kind"] != "backlog" || record["external_id"] != "BACKLOG-CROSS-0004" {
		t.Fatalf("want an update of kind backlog, posted %v", *got)
	}
	if record["disposition"] != "ROUTED to REQ-CROSS-388" || record["source"] != "USER:2026-09-13: routed" || record["expected_fingerprint"] != strings.Repeat("b", 64) {
		t.Errorf("disposition, source and fingerprint must ride the post, got %v", record)
	}
}

// REQ-BKLG-005: DEFERRED is a first-class backlog disposition — `author update
// --kind backlog --disposition DEFERRED` posts it verbatim. The behaviour has
// existed server-side since REQ-CROSS-393; this closes its CLI coverage gap.
func TestAuthorUpdateBacklogPostsDeferredDisposition(t *testing.T) {
	srv, got := authorCapture(t)
	cobraWorkspace(t, srv)

	_, err := runRoot(t, "author", "update", "BACKLOG-CROSS-0004", "--kind", "backlog",
		"--disposition", "DEFERRED", "--source", "USER:2026-09-17: deferred", "--expected-fingerprint", strings.Repeat("b", 64))
	if err != nil {
		t.Fatalf("author update --kind backlog --disposition DEFERRED: %v", err)
	}
	record, _ := (*got)["record"].(map[string]any)
	if record["kind"] != "backlog" || record["disposition"] != "DEFERRED" || record["source"] != "USER:2026-09-17: deferred" {
		t.Errorf("DEFERRED disposition and source must ride the post, got %v", record)
	}
}

// --- REQ-CROSS-393: a backlog record's body is editable (BACKLOG-TOOL-79) ---
//
// The server takes notes_md, observation, why_unrouted, candidate_route,
// gap_kind, consequence and affected_external_ids on the same
// fingerprint-guarded update it takes a disposition on (Core.Author
// @backlog_content_fields). The flags were registered on `author backlog`
// only, so `author update --kind backlog --notes "…"` never reached the
// server — cobra rejected the flag, and the only way to correct a filed
// record's body was to file a second one.

func TestAuthorUpdateBacklogPostsTheContentFields(t *testing.T) {
	srv, got := authorCapture(t)
	cobraWorkspace(t, srv)

	_, err := runRoot(t, "author", "update", "BACKLOG-CROSS-0004", "--kind", "backlog",
		"--notes", "the surface landed in the CLI",
		"--observed", "author update rejects --notes",
		"--why-unrouted", "owner unclear",
		"--candidate-route", "PROPOSED SR",
		"--affected", "REQ-CROSS-393,REQ-CROSS-405",
		"--expected-fingerprint", strings.Repeat("b", 64))
	if err != nil {
		t.Fatalf("author update --kind backlog with body fields: %v", err)
	}
	record, _ := (*got)["record"].(map[string]any)
	if record["kind"] != "backlog" || record["expected_fingerprint"] != strings.Repeat("b", 64) {
		t.Fatalf("want a fingerprint-guarded backlog update, posted %v", *got)
	}
	for field, want := range map[string]string{
		"notes_md":        "the surface landed in the CLI",
		"observation":     "author update rejects --notes",
		"why_unrouted":    "owner unclear",
		"candidate_route": "PROPOSED SR",
	} {
		if record[field] != want {
			t.Errorf("%s must ride the edit, got %v", field, record[field])
		}
	}
	affected, _ := record["affected_external_ids"].([]any)
	if len(affected) != 2 || affected[0] != "REQ-CROSS-393" || affected[1] != "REQ-CROSS-405" {
		t.Errorf("--affected must post as a list, got %v", record["affected_external_ids"])
	}
	// An unnamed field is never sent, so the stored value survives the edit.
	if _, present := record["title"]; present {
		t.Errorf("an unset flag must send no key, posted %v", record)
	}
}

func TestAuthorUpdateGapPostsItsOwnFields(t *testing.T) {
	srv, got := authorCapture(t)
	cobraWorkspace(t, srv)

	_, err := runRoot(t, "author", "update", "GAP-CLI-004", "--kind", "backlog",
		"--gap-kind", "capability", "--consequence", "the trace cannot prove the read",
		"--affected-trace", "REQ-CROSS-393,REQ-CROSS-405",
		"--expected-fingerprint", strings.Repeat("b", 64))
	if err != nil {
		t.Fatalf("author update --kind backlog on a gap: %v", err)
	}
	record, _ := (*got)["record"].(map[string]any)
	if record["gap_kind"] != "capability" || record["consequence"] != "the trace cannot prove the read" {
		t.Errorf("the gap fields must ride the edit, got %v", record)
	}
	// REQ-CROSS-393: the traces a gap blocks change as the gap is understood, so
	// they are editable like every other body field the guarded update takes.
	traces, _ := record["affected_trace_external_ids"].([]any)
	if len(traces) != 2 || traces[0] != "REQ-CROSS-393" || traces[1] != "REQ-CROSS-405" {
		t.Errorf("--affected-trace must post as a list, got %v", record["affected_trace_external_ids"])
	}

	// The vocabulary is checked here, as the create checks it, naming the set.
	*got = nil
	_, err = runRoot(t, "author", "update", "GAP-CLI-004", "--kind", "backlog",
		"--gap-kind", "nonsense", "--expected-fingerprint", strings.Repeat("b", 64))
	if err == nil || !strings.Contains(err.Error(), "--gap-kind") {
		t.Fatalf("an unknown gap kind must be refused naming the flag, got %v", err)
	}
	if *got != nil {
		t.Errorf("a refused edit must post nothing, posted %v", *got)
	}
}

// --- REQ-CROSS-393 / REQ-BKLG-006: --detail on a backlog record is a silent
// loss (BACKLOG-TOOL-49) ---
//
// The backlog schema has no detail_md. `--detail` maps to that field, the
// server's update drops it (it is not in Core.Author @backlog_content_fields),
// and the CLI prints "✓ updated" on the 200 — so prose typed into --detail
// disappeared with a success line over it.

func TestAuthorUpdateBacklogRefusesDetail(t *testing.T) {
	srv, got := authorCapture(t)
	cobraWorkspace(t, srv)

	_, err := runRoot(t, "author", "update", "BACKLOG-CROSS-0004", "--kind", "backlog",
		"--detail", "a paragraph the store would never hold",
		"--expected-fingerprint", strings.Repeat("b", 64))
	if err == nil {
		t.Fatal("--detail on a backlog record must be refused — the schema has no detail_md")
	}
	for _, want := range []string{"--detail", "--notes"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %s, got %v", want, err)
		}
	}
	if *got != nil {
		t.Errorf("the refusal must come before any request, posted %v", *got)
	}
}

// REQ-CROSS-393 (BACKLOG-TOOL-49): every requirement or epic field is lost the
// same way on a backlog record. The server reads only its backlog content
// fields, answers 200, and the CLI printed "✓ updated" over the lost value.
// Each is refused before any request, naming the flag.
func TestAuthorUpdateBacklogRefusesRequirementFields(t *testing.T) {
	for _, c := range []struct{ flag, value string }{
		{"--description", "a description"},
		{"--stage", "build"},
		{"--priority", "high"},
		{"--owner", "someone"},
		{"--boundary", "the change boundary"},
		{"--rationale", "why"},
		{"--verification-method", "unit test"},
		{"--context", "CLI"},
		{"--criteria", `[{"external_id":"AC-1","kind":"criterion","statement":"x"}]`},
	} {
		t.Run(c.flag, func(t *testing.T) {
			srv, got := authorCapture(t)
			cobraWorkspace(t, srv)

			_, err := runRoot(t, "author", "update", "BACKLOG-CROSS-0004", "--kind", "backlog",
				c.flag, c.value, "--expected-fingerprint", strings.Repeat("b", 64))
			if err == nil {
				t.Fatalf("%s on a backlog record must be refused — the server keeps nothing", c.flag)
			}
			if !strings.Contains(err.Error(), c.flag) {
				t.Errorf("the refusal must name %s, got %v", c.flag, err)
			}
			if *got != nil {
				t.Errorf("the refusal must come before any request, posted %v", *got)
			}
		})
	}
}

// A requirement's own detail body is untouched by that refusal.
func TestAuthorUpdateRequirementStillTakesDetail(t *testing.T) {
	srv, got := authorCapture(t)
	cobraWorkspace(t, srv)

	_, err := runRoot(t, "author", "update", "REQ-CROSS-393",
		"--detail", "the unbounded prose field", "--expected-fingerprint", strings.Repeat("b", 64))
	if err != nil {
		t.Fatalf("author update --detail on a requirement: %v", err)
	}
	record, _ := (*got)["record"].(map[string]any)
	if record["detail_md"] != "the unbounded prose field" {
		t.Errorf("--detail must still ride a requirement edit, got %v", record["detail_md"])
	}
}

// The body fields belong to a backlog record. On a requirement or an epic they
// are refused before any request, naming each flag given.
func TestAuthorUpdateRefusesBacklogFieldsOnARequirement(t *testing.T) {
	srv, got := authorCapture(t)
	cobraWorkspace(t, srv)

	_, err := runRoot(t, "author", "update", "REQ-CROSS-393",
		"--notes", "prose", "--observed", "something", "--affected-trace", "REQ-X",
		"--expected-fingerprint", strings.Repeat("b", 64))
	if err == nil {
		t.Fatal("backlog body fields on a requirement must be refused")
	}
	for _, flag := range []string{"--notes", "--observed", "--affected-trace", "--kind backlog"} {
		if !strings.Contains(err.Error(), flag) {
			t.Errorf("the refusal must name %s, got %v", flag, err)
		}
	}
	if *got != nil {
		t.Errorf("a refused edit must post nothing, posted %v", *got)
	}
}

// (d) A pulled backlog record renders the canonical BACKLOG.md shape; a gap
// renders its trace fields.
func TestPullRendersBacklogAndGapRecords(t *testing.T) {
	fx := &wsFixture{backlog: []any{
		map[string]any{
			"external_id": "BACKLOG-CROSS-0001", "kind": "backlog", "title": "The pull refuses the release gate",
			"raised_by": "jane@example.com", "raised_at": "2026-09-13T12:00:00Z",
			"observation": "unknown external id", "why_unrouted": "owner unclear", "candidate_route": "PROPOSED SR",
			"affected_external_ids": []any{"REQ-CROSS-388"}, "disposition": "OPEN",
			"content_fingerprint": strings.Repeat("c", 64), "metadata": map[string]any{"cli_version": "0.5.0+abc"},
		},
		map[string]any{
			"external_id": "GAP-CLI-004", "kind": "gap", "title": "No carrier for gaps", "gap_kind": "capability",
			"affected_trace_external_ids": []any{"REQ-CROSS-393"}, "consequence": "a gap has no record",
			"disclosed_in_gate_external_ids": []any{"ENTRY-EPIC-CLI-019"}, "disposition": "OPEN",
			"content_fingerprint": strings.Repeat("d", 64),
		},
	}}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPull(env, []string{"BACKLOG-CROSS-0001", "GAP-CLI-004"}, wsNow); err != nil {
		t.Fatalf("pull failed: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(env.Root, workingSetDir, "BACKLOG-CROSS-0001.md"))
	if err != nil {
		t.Fatalf("BACKLOG-CROSS-0001.md not written: %v", err)
	}
	got := string(raw)
	for _, want := range []string{"## BACKLOG-CROSS-0001 — The pull refuses the release gate", "Raised by / at:** jane@example.com / 2026-09-13T12:00:00Z",
		"Observed:** unknown external id", "Why unrouted:** owner unclear", "Candidate route:** PROPOSED SR",
		"Affected items:** REQ-CROSS-388", "Disposition:** OPEN", "Fingerprint:** " + strings.Repeat("c", 64), "cli_version"} {
		if !strings.Contains(got, want) {
			t.Errorf("backlog render lacks %q:\n%s", want, got)
		}
	}

	raw, err = os.ReadFile(filepath.Join(env.Root, workingSetDir, "GAP-CLI-004.md"))
	if err != nil {
		t.Fatalf("GAP-CLI-004.md not written: %v", err)
	}
	got = string(raw)
	for _, want := range []string{"## GAP-CLI-004 — No carrier for gaps", "Kind:** capability", "Affected traces:** REQ-CROSS-393",
		"Consequence:** a gap has no record", "Disclosed in:** ENTRY-EPIC-CLI-019", "Disposition:** OPEN"} {
		if !strings.Contains(got, want) {
			t.Errorf("gap render lacks %q:\n%s", want, got)
		}
	}
}
