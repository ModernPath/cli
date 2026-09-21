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
