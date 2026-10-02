package cmd

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// UR-RDD-ONBOARD-003 journeys: part of a Git repository is
// inventoried by path, captured, and proven delivered over the same paths.
// The server here applies the real server's bundle rule: the uploaded files
// are exactly the authorized ones and their digest is the authorized digest.

func TestURRDDONBOARD003PathOfOversizedRepositoryWithSymlinkIsInventoriedAndCaptured(t *testing.T) {
	big := strings.Repeat("x", 17_000_000)
	root := scopeRepo(t, map[string]string{"pkg/a.txt": "alpha", "other/big1.bin": big, "other/big2.bin": big})
	if err := os.Symlink("a.txt", filepath.Join(root, "pkg", "link.txt")); err != nil {
		t.Fatal(err)
	}
	scopeCommit(t, root, "tracked link inside the named path")
	if _, _, err := scopeInventory(t, "--repository", "repo="+root); err == nil {
		t.Fatal("control: the whole repository must be refused")
	}

	inventory, out, err := scopeInventory(t, "--repository", "repo="+root, "--path", "repo=pkg")
	if err != nil {
		t.Fatalf("the named path could not be inventoried: %v: %s", err, out)
	}
	repo := inventory.Repositories[0]
	if !reflect.DeepEqual(scopePaths(repo.Files), []string{"pkg/a.txt"}) || repo.Dirty || repo.Revision != gitRun(t, root, "rev-parse", "HEAD") {
		t.Fatalf("scoped inventory is not the named path at the repository's clean commit: %+v", repo)
	}
	if !scopeExcluded(repo.Exclusions, "other", "subtree", "outside the authorized scope") || !scopeExcluded(repo.Exclusions, "pkg/link.txt", "file", "symbolic link") {
		t.Fatalf("left-out paths and the link are not disclosed: %+v", repo.Exclusions)
	}

	var captured []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/sync/contract":
			w.WriteHeader(404)
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/reverse-engineering/runs/scoped-run"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"id": "scoped-run", "authorization": map[string]any{"repositories": []reverseRepository{repo}},
			}})
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/reverse-engineering/runs/scoped-run/sources"):
			var payload struct {
				Files []struct {
					Path    string `json:"path"`
					Content string `json:"content_base64"`
				} `json:"files"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			uploaded := []reverseFile{}
			for _, file := range payload.Files {
				content, err := base64.StdEncoding.DecodeString(file.Content)
				if err != nil {
					t.Errorf("upload is not base64: %s", file.Path)
				}
				uploaded = append(uploaded, reverseFile{file.Path, reverseDigest(content), int64(len(content))})
				captured = append(captured, file.Path)
			}
			if len(uploaded) != len(repo.Files) || reverseSnapshot(uploaded) != repo.SnapshotDigest {
				w.WriteHeader(422)
				_, _ = w.Write([]byte(`{"error":"source_inventory_mismatch"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"id":"capture-1","state":"queued"}}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	out, err = reCommand(t, server, "", "capture-source", "--run", "scoped-run", "--repository", "repo", "--root", root)
	if err != nil || !strings.Contains(out, "capture-1") {
		t.Fatalf("the named path could not be captured: %v: %s", err, out)
	}
	if !reflect.DeepEqual(captured, []string{"pkg/a.txt"}) {
		t.Fatalf("capture uploaded something other than the named path: %v", captured)
	}
}

// scopeJourneyRun inventories one path of a pushed checkout and returns
// the run's repository entry with the delivery input that names its snapshot.
func scopeJourneyRun(t *testing.T) (string, asBuiltDeliveryInput, reverseRepository) {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, t.TempDir(), "init", "--bare", "--initial-branch=main", remote)
	root := filepath.Join(t.TempDir(), "checkout")
	gitRun(t, t.TempDir(), "clone", remote, root)
	scopeWrite(t, root, map[string]string{"pkg/a.txt": "alpha", "pkg/a_test.txt": "asserts alpha", "other/c.txt": "gamma"})
	scopeCommit(t, root, "code and its tests")
	gitRun(t, root, "push", "origin", "main")
	inventory, out, err := scopeInventory(t, "--repository", "catalog="+root, "--path", "catalog=pkg")
	if err != nil {
		t.Fatalf("the named path could not be inventoried: %v: %s", err, out)
	}
	repo := inventory.Repositories[0]
	return root, asBuiltDeliveryInput{Key: "observation", RepositoryKey: "catalog", Root: root, TestedRevision: repo.Revision, SnapshotDigest: repo.SnapshotDigest}, repo
}

func TestURRDDONBOARD003PathScopedRunDeliveryProofMatchesCapture(t *testing.T) {
	_, input, repo := scopeJourneyRun(t)
	state := &scopeRunServer{repositories: []reverseRepository{repo}}
	out, err := scopeDeliveryProof(t, state.start(t), input)
	if err != nil {
		t.Fatalf("delivery proof of the path-scoped run refused: %v: %s", err, out)
	}
	if state.posts != 1 || state.report["snapshot_digest"] != repo.SnapshotDigest || state.report["integrated_revision"] != repo.Revision {
		t.Fatalf("the observation does not match the captured scope at the default-branch tip: %v", state.report)
	}
	if state.report["authorization_run_id"] != "scoped-run" || state.report["measured_files"] != float64(2) || state.report["dirty"] != false {
		t.Fatalf("the observation does not cover the run's authorized paths: %v", state.report)
	}
}

func TestURRDDONBOARD003PathScopedRunDeliveryRefusals(t *testing.T) {
	root, input, repo := scopeJourneyRun(t)
	state := &scopeRunServer{repositories: []reverseRepository{repo}}
	server := state.start(t)
	refuses := func(step, reason string, proof asBuiltDeliveryInput) {
		t.Helper()
		if _, err := scopeDeliveryProof(t, server, proof); err == nil || !strings.Contains(err.Error(), reason) {
			t.Fatalf("%s: refusal does not say %q: %v", step, reason, err)
		}
		if state.posts != 0 {
			t.Fatalf("%s: a refused proof was recorded", step)
		}
	}
	scopeWrite(t, root, map[string]string{"other/scratch.txt": "uncommitted"})
	refuses("repository is not clean", "dirty", input)
	if err := os.Remove(filepath.Join(root, "other", "scratch.txt")); err != nil {
		t.Fatal(err)
	}

	// A commit outside the named path that is not pushed: the named path still matches the
	// capture, but the tested revision is not the default-branch tip.
	scopeWrite(t, root, map[string]string{"other/c.txt": "gamma changed"})
	scopeCommit(t, root, "change outside the named path")
	unpushed := input
	unpushed.TestedRevision = gitRun(t, root, "rev-parse", "HEAD")
	refuses("tested revision is not the default-branch tip", "integration proof is missing", unpushed)
	gitRun(t, root, "push", "origin", "main")

	// A file under the authorized paths differs from the capture, delivered.
	scopeWrite(t, root, map[string]string{"pkg/a.txt": "alpha changed"})
	scopeCommit(t, root, "change inside the named path")
	gitRun(t, root, "push", "origin", "main")
	changed := input
	changed.TestedRevision = gitRun(t, root, "rev-parse", "HEAD")
	refuses("an authorized file differs from the capture", "snapshot digest does not match the current repository content", changed)

	// Naming the new digest does not help: it is not what the run captured.
	renamed := changed
	renamed.SnapshotDigest = scopeCurrentDigest(t, root, repo)
	refuses("the input digest is not the run's capture", "captured", renamed)
}

// scopeCurrentDigest is the digest of the run's authorized paths as they are now.
func scopeCurrentDigest(t *testing.T, root string, repo reverseRepository) string {
	t.Helper()
	current := make([]reverseFile, len(repo.Files))
	for i, file := range repo.Files {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file.Path)))
		if err != nil {
			t.Fatal(err)
		}
		current[i] = reverseFile{file.Path, reverseDigest(content), int64(len(content))}
	}
	return reverseSnapshot(current)
}
