package cmd

// Epic 336 (D2), test case TC-D2-29 ("D2 · epics record-merge sends the merge
// commit, and the agent with --agent, and prints the recorded commit from the
// route's answer or the refusal's sentence, and its help and its section of
// the CLI guide describe it"), criterion 94aab61d (SR-D2-8 at 2):
//   - `modernpath epics record-merge <epic-id> --commit <sha> [--agent <slug>]
//     [--json]` POSTs {"commit": <sha>} to /api/work/epics/<id>/merge-commit,
//     with "actor": {"kind": "agent", "agent_slug": <slug>} only with --agent;
//   - it prints the recorded commit from the body's merge_commit_sha, and the
//     commit it replaced when the body names one; --json prints the data;
//   - it refuses without --commit, sending nothing, and prints a refusal's
//     sentence, the body's error message;
//   - phrase tests under the same criterion: the command's help and the kit
//     copy of the CLI guide name --commit, say that the merge commit of the
//     last pull request that carried the change is recorded once that pull
//     request is merged, before the run that proves the requirements is
//     captured, and that recording it again replaces it.
//
// Epic 424 (SR-4663 at revision 2) adds phrase checks under the same criterion:
// the help, and the record-merge section of the guide's kit copy, say "Any
// authorized workspace member records it, and only on a DONE Epic." and name
// neither the implementation approver nor the assignee. Its RED: both still
// say that only the Epic's implementation approver or its assignee records it.
// The sample refusal is invalid_lifecycle_state, because the server no longer
// sends not_change_owner.
//
// On D2's skeleton `record-merge` is no command of the epics group, so the test
// server receives no request and every assertion fails; the test uses no new
// production symbol, so it builds on the skeleton.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const d2MergePath = "/api/work/epics/42/merge-commit"
const d2MergeSha = "a3f2c9d8e1b7406f5c2d9a8b7e6f5d4c3b2a1908"
const d2MergeReplaced = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c"

type d2MergeRequest struct {
	method, path string
	body         map[string]any
}

// d2MergeServer answers the reachability probe and records every other
// request, replying with status and reply.
func d2MergeServer(t *testing.T, status int, reply string) (*httptest.Server, *[]d2MergeRequest) {
	t.Helper()
	seen := []d2MergeRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		var decoded map[string]any
		_ = json.NewDecoder(r.Body).Decode(&decoded)
		seen = append(seen, d2MergeRequest{method: r.Method, path: r.URL.Path, body: decoded})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(server.Close)
	return server, &seen
}

func d2MergeFolded(text string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(text, "`", "")), " ")
}

func TestD2_94aab61d_RecordMergeSendsTheCommitAndPrintsTheRecordedCommit(t *testing.T) {
	reply := `{"data":{"id":42,"merge_commit_sha":"` + d2MergeSha + `","merge_commit_recorded_at":"2026-11-03T10:00:00Z","replaced_merge_commit_sha":null}}`
	server, seen := d2MergeServer(t, http.StatusOK, reply)
	cobraWorkspace(t, server)

	out, err := runRoot(t, "epics", "record-merge", "42", "--commit", strings.ToUpper(d2MergeSha))
	if err != nil {
		t.Fatalf("record-merge: %v\n%s", err, out)
	}
	if len(*seen) != 1 || (*seen)[0].method != http.MethodPost || (*seen)[0].path != d2MergePath {
		t.Fatalf("requests = %#v; want one POST %s", *seen, d2MergePath)
	}
	body := (*seen)[0].body
	if body["commit"] != strings.ToUpper(d2MergeSha) {
		t.Errorf("commit sent = %v; want the flag's value as given, %s", body["commit"], strings.ToUpper(d2MergeSha))
	}
	if _, has := body["actor"]; has {
		t.Errorf("an actor was sent without --agent: %v", body["actor"])
	}
	if !strings.Contains(out, d2MergeSha) || !strings.Contains(out, "42") {
		t.Errorf("the output does not print the recorded commit from the body for Epic 42:\n%s", out)
	}
}

func TestD2_94aab61d_RecordMergeSendsTheAgentAndPrintsTheReplacedCommit(t *testing.T) {
	reply := `{"data":{"id":42,"merge_commit_sha":"` + d2MergeSha + `","merge_commit_recorded_at":"2026-11-03T10:00:00Z","replaced_merge_commit_sha":"` + d2MergeReplaced + `"}}`
	server, seen := d2MergeServer(t, http.StatusOK, reply)
	cobraWorkspace(t, server)

	out, err := runRoot(t, "epics", "record-merge", "42", "--commit", d2MergeSha, "--agent", "claude-code")
	if err != nil {
		t.Fatalf("record-merge --agent: %v\n%s", err, out)
	}
	if len(*seen) != 1 {
		t.Fatalf("requests = %#v; want one POST %s", *seen, d2MergePath)
	}
	actor, _ := (*seen)[0].body["actor"].(map[string]any)
	if actor["kind"] != "agent" || actor["agent_slug"] != "claude-code" {
		t.Errorf("actor sent = %v; want {kind: agent, agent_slug: claude-code}", (*seen)[0].body["actor"])
	}
	if !strings.Contains(out, d2MergeReplaced) {
		t.Errorf("the output does not name the commit the recording replaced:\n%s", out)
	}

	jsonOut, err := runRoot(t, "epics", "record-merge", "42", "--commit", d2MergeSha, "--json")
	if err != nil {
		t.Fatalf("record-merge --json: %v\n%s", err, jsonOut)
	}
	var printed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(jsonOut)), &printed); err != nil {
		t.Fatalf("--json did not print the data object: %v\n%s", err, jsonOut)
	}
	if printed["merge_commit_sha"] != d2MergeSha || printed["replaced_merge_commit_sha"] != d2MergeReplaced {
		t.Errorf("--json printed %v; want the route's data", printed)
	}
}

func TestD2_94aab61d_RecordMergeRefusesWithoutCommitAndPrintsTheRefusalSentence(t *testing.T) {
	sentence := "Record merge commit needs a DONE Epic; this Epic is IN_PROGRESS. Any authorized workspace member records the merge commit once the Epic is DONE."
	server, seen := d2MergeServer(t, http.StatusConflict, `{"error":{"code":"invalid_lifecycle_state","message":"`+sentence+`"}}`)
	cobraWorkspace(t, server)

	out, err := runRoot(t, "epics", "record-merge", "42")
	if err == nil {
		t.Fatalf("record-merge without --commit was not refused:\n%s", out)
	}
	if len(*seen) != 0 {
		t.Fatalf("a request was sent without --commit: %#v", *seen)
	}
	if !strings.Contains(err.Error()+out, "commit") {
		t.Errorf("the refusal does not name --commit: %v\n%s", err, out)
	}

	out, err = runRoot(t, "epics", "record-merge", "42", "--commit", d2MergeSha)
	if err == nil {
		t.Fatalf("a 409 refusal was not reported:\n%s", out)
	}
	if !strings.Contains(err.Error()+out, sentence) {
		t.Errorf("the refusal does not print the body's sentence %q: %v\n%s", sentence, err, out)
	}
}

func TestD2_94aab61d_RecordMergeHelpDescribesTheCommand(t *testing.T) {
	help, err := runRoot(t, "epics", "record-merge", "--help")
	if err != nil {
		t.Fatal(err)
	}
	text := d2MergeFolded(help)
	for _, want := range []string{
		"--commit",
		"the merge commit of the last pull request that carried the change",
		"once that pull request is merged",
		"before the run that proves the kept or changed requirements is captured",
		"Recording it again replaces it.",
		"Any authorized workspace member records it, and only on a DONE Epic.",
		"--agent",
		"--json",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("epics record-merge --help lacks %q", want)
		}
	}
	for _, gone := range []string{"approver", "assignee"} {
		if strings.Contains(strings.ToLower(text), gone) {
			t.Errorf("epics record-merge --help still names the %s", gone)
		}
	}
}

// The kit copy of the CLI guide, opened by its path as reference_test.go does;
// cli_guide_test.go keeps it equal to modernpath-core/docs/cli.md.
func TestD2_94aab61d_CLIGuideDescribesRecordMerge(t *testing.T) {
	raw, err := os.ReadFile("../internal/kit/assets/cli-guide/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	guide := d2MergeFolded(string(raw))
	for _, want := range []string{
		"modernpath epics record-merge <epic-id> --commit <sha>",
		"the merge commit of the last pull request that carried the change is recorded once that pull request is merged",
		"before the run that proves the kept or changed requirements is captured",
		"Recording it again replaces it.",
	} {
		if !strings.Contains(guide, want) {
			t.Errorf("the CLI guide lacks %q", want)
		}
	}

	// The record-merge section alone: the guide names assignee_id elsewhere.
	section := d2MergeFolded(d2MergeGuideSection(t, string(raw), "### Record a done Epic's merge commit"))
	if !strings.Contains(section, "Any authorized workspace member records it, and only on a DONE Epic.") {
		t.Errorf("the guide's record-merge section does not say who records the commit:\n%s", section)
	}
	for _, gone := range []string{"approver", "assignee"} {
		if strings.Contains(strings.ToLower(section), gone) {
			t.Errorf("the guide's record-merge section still names the %s", gone)
		}
	}
}

// d2MergeGuideSection returns the guide's text from heading to the next heading
// of the same or a higher level.
func d2MergeGuideSection(t *testing.T, guide, heading string) string {
	t.Helper()
	start := strings.Index(guide, "\n"+heading+"\n")
	if start < 0 {
		t.Fatalf("the CLI guide has no section %q", heading)
	}
	rest := guide[start+1+len(heading):]
	if end := strings.Index(rest, "\n### "); end >= 0 {
		rest = rest[:end]
	}
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}
