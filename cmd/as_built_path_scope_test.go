package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// scopeRunServer is the server side of a scoped delivery proof: it serves one
// run's authorization and retains the evidence the command posts.
type scopeRunServer struct {
	runStatus    int
	repositories []reverseRepository
	order        []string
	runGets      int
	posts        int
	report       map[string]any
}

func (s *scopeRunServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/sync/contract":
			w.WriteHeader(404)
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/reverse-engineering/runs/scoped-run"):
			s.runGets++
			s.order = append(s.order, "run")
			if s.runStatus != 0 {
				w.WriteHeader(s.runStatus)
				_, _ = w.Write([]byte(`{"error":"not_found"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"id": "scoped-run", "authorization": map[string]any{"repositories": s.repositories},
			}})
		case r.Method == "POST" && r.URL.Path == "/api/v1/sync/evidence":
			s.posts++
			s.order = append(s.order, "evidence")
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			raw, _ := payload["raw_evidence"].(string)
			_ = json.Unmarshal([]byte(raw), &s.report)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"result": "recorded", "run": map[string]any{"id": "evidence-run"},
				"report_digest": reverseDigest([]byte(raw)),
			}})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// scopeDeliveryFixture is a pushed checkout with a named path and a second
// directory, and the authorization of a run scoped to that path. The scope is
// built by hand so these cases do not depend on the inventory's path option.
func scopeDeliveryFixture(t *testing.T) (string, asBuiltDeliveryInput, reverseRepository) {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, t.TempDir(), "init", "--bare", "--initial-branch=main", remote)
	root := filepath.Join(t.TempDir(), "checkout")
	gitRun(t, t.TempDir(), "clone", remote, root)
	scopeWrite(t, root, map[string]string{"pkg/a.txt": "alpha", "other/c.txt": "gamma"})
	scopeCommit(t, root, "catalog")
	gitRun(t, root, "push", "origin", "main")
	inventory, err := buildReverseInventory([]string{"catalog=" + root})
	if err != nil {
		t.Fatal(err)
	}
	whole := inventory.Repositories[0]
	scoped := reverseRepository{Key: "catalog", Revision: whole.Revision, Dirty: false}
	for _, file := range whole.Files {
		if strings.HasPrefix(file.Path, "pkg/") {
			scoped.Files = append(scoped.Files, file)
		}
	}
	scoped.SnapshotDigest = reverseSnapshot(scoped.Files)
	if len(scoped.Files) != 1 || scoped.SnapshotDigest == whole.SnapshotDigest {
		t.Fatalf("fixture does not separate the named path from the repository: %+v", scoped)
	}
	input := asBuiltDeliveryInput{Key: "observation", RepositoryKey: "catalog", Root: root, TestedRevision: whole.Revision, SnapshotDigest: scoped.SnapshotDigest}
	return root, input, scoped
}

func scopeDeliveryProof(t *testing.T, server *httptest.Server, input asBuiltDeliveryInput) (string, error) {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return reCommand(t, server, string(raw), "delivery-proof", "--run", "scoped-run", "--file", "-")
}

func TestSRRDDASBUILTCLI002ScopedRunDeliveryMatchesCapturedSnapshot(t *testing.T) {
	_, input, scoped := scopeDeliveryFixture(t)
	state := &scopeRunServer{repositories: []reverseRepository{scoped}}
	out, err := scopeDeliveryProof(t, state.start(t), input)
	if err != nil {
		t.Fatalf("scoped delivery proof refused: %v: %s", err, out)
	}
	if state.posts != 1 || state.report["snapshot_digest"] != scoped.SnapshotDigest {
		t.Fatalf("report does not carry the captured scope snapshot: posts=%d report=%v", state.posts, state.report)
	}
}

func TestSRRDDASBUILTCLI002ReportNamesRunAndMeasuredFiles(t *testing.T) {
	_, input, scoped := scopeDeliveryFixture(t)
	state := &scopeRunServer{repositories: []reverseRepository{scoped}}
	out, err := scopeDeliveryProof(t, state.start(t), input)
	if err != nil {
		t.Fatalf("scoped delivery proof refused: %v: %s", err, out)
	}
	if state.runGets != 1 || len(state.order) == 0 || state.order[0] != "run" {
		t.Fatalf("the run was not read once, before anything else: gets=%d order=%v", state.runGets, state.order)
	}
	if state.report["authorization_run_id"] != "scoped-run" || state.report["measured_files"] != float64(len(scoped.Files)) {
		t.Fatalf("report does not name the run and the files measured: %v", state.report)
	}
}

func TestSRRDDASBUILTCLI002UnreadableOrForeignRunRefusesBeforeObservation(t *testing.T) {
	foreign := reverseRepository{Key: "elsewhere"}
	for _, tc := range []struct {
		name   string
		state  *scopeRunServer
		reason []string
	}{
		{"unreadable run", &scopeRunServer{runStatus: 404}, []string{"could not read run", "scoped-run"}},
		{"run without the repository", &scopeRunServer{repositories: []reverseRepository{foreign}}, []string{"does not authorize repository", "catalog"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, input, _ := scopeDeliveryFixture(t)
			// An unreachable origin: reaching the remote would fail with a different error.
			gitRun(t, root, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
			_, err := scopeDeliveryProof(t, tc.state.start(t), input)
			if err == nil {
				t.Fatal("delivery proof succeeded without a readable authorizing run")
			}
			for _, reason := range tc.reason {
				if !strings.Contains(err.Error(), reason) {
					t.Fatalf("refusal does not say %q: %v", reason, err)
				}
			}
			if strings.Contains(err.Error(), "remote") || tc.state.runGets != 1 || tc.state.posts != 0 {
				t.Fatalf("refusal came after observation or without reading the run: %v gets=%d posts=%d", err, tc.state.runGets, tc.state.posts)
			}
		})
	}
}

func TestSRRDDASBUILTCLI002ScopedRunKeepsTheFourRefusals(t *testing.T) {
	root, input, scoped := scopeDeliveryFixture(t)
	state := &scopeRunServer{repositories: []reverseRepository{scoped}}
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
	scopeWrite(t, root, map[string]string{"other/changed.txt": "dirty"})
	refuses("dirty repository", "dirty", input)

	scopeCommit(t, root, "change outside the named path")
	refuses("revision other than the tested one", "tested revision does not match", input)

	unpushed := input
	unpushed.TestedRevision = gitRun(t, root, "rev-parse", "HEAD")
	refuses("tested revision is not the default-branch tip", "integration proof is missing", unpushed)

	gitRun(t, root, "push", "origin", "main")
	wrong := unpushed
	wrong.SnapshotDigest = "sha256:wrong"
	refuses("input digest is not the run's capture", "captured", wrong)

	scopeWrite(t, root, map[string]string{"pkg/a.txt": "alpha changed"})
	scopeCommit(t, root, "change inside the named path")
	gitRun(t, root, "push", "origin", "main")
	changed := input
	changed.TestedRevision = gitRun(t, root, "rev-parse", "HEAD")
	refuses("authorized file differs from the capture", "snapshot digest does not match the current repository content", changed)
}

// The proof is compared with what the run captured, not only with the digest
// typed into the input: a delivered change to an authorized file refuses even
// when the input names the new digest.
func TestSRRDDASBUILTCLI002InputDigestMustBeTheRunsCapture(t *testing.T) {
	root, input, scoped := scopeDeliveryFixture(t)
	scopeWrite(t, root, map[string]string{"pkg/a.txt": "alpha changed"})
	scopeCommit(t, root, "change inside the named path")
	gitRun(t, root, "push", "origin", "main")
	input.TestedRevision = gitRun(t, root, "rev-parse", "HEAD")
	input.SnapshotDigest = scopeCurrentDigest(t, root, scoped)
	state := &scopeRunServer{repositories: []reverseRepository{scoped}}
	_, err := scopeDeliveryProof(t, state.start(t), input)
	if err == nil || !strings.Contains(err.Error(), "captured") || !strings.Contains(err.Error(), "scoped-run") {
		t.Fatalf("a digest other than the run's capture was accepted or the refusal does not name the run: %v", err)
	}
	if state.runGets != 1 || state.posts != 0 {
		t.Fatalf("a proof that does not match the capture was recorded: gets=%d posts=%d", state.runGets, state.posts)
	}
}

// Regression guard: without a named run the report's key set is exactly the
// delivered one. The server replays a retained report by comparing every key
// but observed_at, so an added key would turn a retry into a conflict.
func TestSRRDDASBUILTCLI002WholeRepositoryReportKeysAreUnchanged(t *testing.T) {
	_, input := asBuiltDeliveryFixture(t)
	report, err := collectAsBuiltDelivery(input)
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(report))
	for key := range report {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	want := []string{"command", "commands", "default_branch", "dirty", "integrated_revision", "observed_at", "origin", "repository_key", "snapshot_digest", "tested_revision"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("whole-repository report keys changed:\n got %v\nwant %v", keys, want)
	}
}
