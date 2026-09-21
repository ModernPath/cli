package cmd

// REQ-CROSS-378 (EPIC-CLI-017): `factory evidence --revision <rev>` pins the
// run to the named commit instead of HEAD, so a RED can be recorded at the RED
// commit without a checkout; every result carries that revision in full. The
// server's record-time warnings ride the response and are printed, never
// swallowed.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitCommitAll stages everything under dir and commits it, returning the full
// sha. The identity is set per call so the fixture never depends on the
// developer's global git config.
func gitCommitAll(t *testing.T, dir, message string) string {
	t.Helper()
	for _, args := range [][]string{
		{"add", "-A"},
		{"-c", "user.email=t@test.local", "-c", "user.name=t", "commit", "-q", "-m", message},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return gitOut(dir, "rev-parse", "HEAD")
}

func evidenceCaptureServer(t *testing.T, warnings []string) (*httptest.Server, *map[string]any) {
	t.Helper()
	captured := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/sync/evidence" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"result":   "created",
				"run":      map[string]any{"external_id": "RUN-1", "sha": captured["sha"]},
				"warnings": warnings,
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &captured
}

func TestEvidenceRevisionPinsTheNamedCommit(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	writeFile(t, filepath.Join(root, "a.txt"), "red\n")
	first := gitCommitAll(t, root, "red")
	writeFile(t, filepath.Join(root, "a.txt"), "green\n")
	second := gitCommitAll(t, root, "green")
	if first == second || first == "" {
		t.Fatalf("fixture needs two distinct commits, got %q and %q", first, second)
	}

	srv, captured := evidenceCaptureServer(t, nil)
	env := &factoryEnv{Root: root, APIURL: srv.URL, SystemID: 4, token: "t"}

	err := factoryEvidenceRun(env, evidenceOpts{kind: "local_test", log: "go test", fail: "REQ-X-1", role: "RED", revision: first})
	if err != nil {
		t.Fatalf("evidence with --revision: %v", err)
	}

	if got := (*captured)["sha"]; got != first[:7] {
		t.Fatalf("the run must pin to the named revision's short sha %q, got %v (HEAD is %q)", first[:7], got, second[:7])
	}
	results, _ := (*captured)["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("want one result, got %v", results)
	}
	if rev := results[0].(map[string]any)["revision"]; rev != first {
		t.Fatalf("every result must carry the full named revision %q, got %v", first, rev)
	}
}

func TestEvidenceRefusesAnUnknownRevision(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	writeFile(t, filepath.Join(root, "a.txt"), "x\n")
	gitCommitAll(t, root, "one")

	srv, captured := evidenceCaptureServer(t, nil)
	env := &factoryEnv{Root: root, APIURL: srv.URL, SystemID: 4, token: "t"}

	err := factoryEvidenceRun(env, evidenceOpts{kind: "local_test", pass: "REQ-X-1", revision: "no-such-rev"})
	if err == nil || !strings.Contains(err.Error(), "no-such-rev") {
		t.Fatalf("an unknown --revision must be refused naming it, got %v", err)
	}
	if len(*captured) != 0 {
		t.Fatalf("nothing may be posted for an unknown revision, got %v", *captured)
	}
}

func TestEvidencePrintsServedWarnings(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	writeFile(t, filepath.Join(root, "a.txt"), "x\n")
	gitCommitAll(t, root, "one")

	warning := "no RED is recorded before this passing result for REQ-X-1 — reconcile will refuse TODO->IN_PROGRESS until one is"
	srv, _ := evidenceCaptureServer(t, []string{warning})
	env := &factoryEnv{Root: root, APIURL: srv.URL, SystemID: 4, token: "t"}

	// Warnings go to the colored stderr writer (the pipeable-stdout rule);
	// stdout is captured too so the success line does not leak into the run.
	var err error
	var errOut string
	out := captureOut(t, func() {
		errOut = captureWarnings(t, func() {
			err = factoryEvidenceRun(env, evidenceOpts{kind: "local_test", pass: "REQ-X-1"})
		})
	})

	if err != nil {
		t.Fatalf("a warning is not a refusal: %v", err)
	}
	if !strings.Contains(errOut, warning) {
		t.Fatalf("the served warning must be printed on stderr; stdout=%q stderr=%q", out, errOut)
	}
}
