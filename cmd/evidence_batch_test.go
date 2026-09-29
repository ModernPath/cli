package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// REQ-CROSS-445 (EPIC-CLI-TURNS): `factory evidence --file` records several
// runs in one call, one POST per entry, each with a run id derived from the
// whole entry — so a re-run of the same entry updates the same run, and two
// different results never share an id.

type evidenceCapture struct {
	posts []map[string]any
}

func evidenceBatchServer(t *testing.T) (*httptest.Server, *evidenceCapture) {
	t.Helper()
	c := &evidenceCapture{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/evidence", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		c.posts = append(c.posts, body)
		if str(body, "log_ref") == "boom" {
			w.WriteHeader(422)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "refused run boom"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"result": "recorded-by-server", "run": map[string]any{"external_id": body["external_id"]}, "warnings": []string{},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, c
}

// evidenceWorkspace binds the workspace and gives it one commit, so HEAD
// resolves to a revision.
func evidenceWorkspace(t *testing.T, srv *httptest.Server) {
	t.Helper()
	cobraWorkspace(t, srv)
	dir, _ := os.Getwd()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func runIDs(posts []map[string]any) []string {
	var ids []string
	for _, p := range posts {
		ids = append(ids, str(p, "external_id"))
	}
	return ids
}

func TestREQCROSS445EvidenceFilePostsOneRunPerEntry(t *testing.T) {
	srv, capture := evidenceBatchServer(t)
	evidenceWorkspace(t, srv)
	runs := writePlanFile(t, "runs.json", `[
 {"fail": ["REQ-A-1"], "role": "red", "revision": "HEAD", "kind": "local_test", "log": "go test ./cmd -run X", "totals": {"passed": 0, "failed": 1}},
 {"pass": ["REQ-A-1", "REQ-A-2"], "kind": "local_test", "log": "go test ./...", "totals": {"passed": 2, "failed": 0}},
 {"pass": ["REQ-A-3"], "log": "boom"},
 {"pass": ["EPIC-A"], "skip": ["REQ-A-4"], "log": "go test ./... (epic)"}
]`)

	out, err := runRoot(t, "factory", "evidence", "--file", runs)
	if err == nil {
		t.Errorf("a refused run must exit non-zero\n%s", out)
	}
	if len(capture.posts) != 4 {
		t.Fatalf("one POST per entry, the failing one included, got %d", len(capture.posts))
	}
	first := capture.posts[0]
	results, _ := first["results"].([]any)
	r0, _ := results[0].(map[string]any)
	if r0["role"] != "RED" || r0["result"] != "fail" || r0["target_external_id"] != "REQ-A-1" || str(r0, "revision") == "" {
		t.Errorf("the entry's role, outcome and revision ride its results, posted %v", r0)
	}
	if first["log_ref"] != "go test ./cmd -run X" || first["kind"] != "local_test" {
		t.Errorf("the entry's log and kind ride the run, posted %v", first)
	}
	if totals, _ := first["totals"].(map[string]any); totals["failed"] != float64(1) {
		t.Errorf("the entry's totals ride the run, posted %v", first["totals"])
	}
	if len(capture.posts[3]["results"].([]any)) != 2 {
		t.Errorf("the run after the refused one still posts with its targets, posted %v", capture.posts[3])
	}
	for _, id := range runIDs(capture.posts) {
		if !strings.Contains(out, id) {
			t.Errorf("each run id is printed; %s is missing:\n%s", id, out)
		}
	}
	if !strings.Contains(out, "recorded-by-server") || !strings.Contains(out, "refused run boom") {
		t.Errorf("the server's result and the refusal are printed:\n%s", out)
	}

	// The same file again: the same ids, so the server updates the same runs.
	firstIDs := runIDs(capture.posts)
	capture.posts = nil
	_, _ = runRoot(t, "factory", "evidence", "--file", runs)
	if strings.Join(runIDs(capture.posts), ",") != strings.Join(firstIDs, ",") {
		t.Errorf("a re-run of the same entries keeps their ids: %v then %v", firstIDs, runIDs(capture.posts))
	}
}

func TestREQCROSS445EvidenceRunIDCoversTheWholeEntry(t *testing.T) {
	srv, capture := evidenceBatchServer(t)
	evidenceWorkspace(t, srv)
	runs := writePlanFile(t, "runs.json", `[
 {"pass": ["REQ-A-1"], "log": "go test ./..."},
 {"pass": ["REQ-A-1"], "log": "go test ./..."},
 {"fail": ["REQ-A-1"], "log": "go test ./..."},
 {"pass": ["REQ-A-1"], "log": "go test ./...", "kind": "ci"},
 {"pass": ["REQ-A-1"], "log": "go test ./cmd"},
 {"pass": ["REQ-A-1"], "log": "go test ./...", "role": "RED"}
]`)
	if out, err := runRoot(t, "factory", "evidence", "--file", runs); err != nil {
		t.Fatalf("evidence --file: %v\n%s", err, out)
	}
	ids := runIDs(capture.posts)
	if len(ids) != 6 {
		t.Fatalf("one run per entry, got %v", ids)
	}
	if ids[0] != ids[1] {
		t.Errorf("the same entry gives the same id: %s vs %s", ids[0], ids[1])
	}
	seen := map[string]int{}
	for i, id := range ids[1:] {
		if j, dup := seen[id]; dup {
			t.Errorf("entries %d and %d differ in outcome, kind, log or role but share id %s", j+1, i+1, id)
		}
		seen[id] = i + 1
	}
}
