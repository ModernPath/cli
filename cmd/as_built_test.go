package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSRRDDASBUILTCLI001PreviewOpenApplyReadback(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sync/contract" {
			w.WriteHeader(404)
			return
		}
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer t" {
			t.Error("missing authenticated actor")
		}
		_, _ = w.Write([]byte(`{"data":{"eligible":true,"proof_digest":"sha256:exact","receipt":{"destination":"DONE"}}}`))
	}))
	defer server.Close()
	proof := `{"version":1,"requirements":[{"kind":"SR","external_id":"SR-X","content_fingerprint":"exact","clauses":[]}],"delivery":[]}`
	for _, step := range []struct {
		input string
		args  []string
		want  string
	}{
		{proof, []string{"proof-preview", "--file", "-"}, "POST /api/v1/systems/4/reverse-engineering/proof-preview"},
		{`{"key":"open","proof":` + proof + `,"proof_digest":"sha256:exact","brief":{"what":"Accept","why_now":"Verified","changes_if_approved":"DONE","risk_if_wrong":"Wrong acceptance","recommendation":"Accept"}}`, []string{"acceptance-open", "--file", "-"}, "POST /api/v1/systems/4/reverse-engineering/acceptances"},
		{`{"key":"apply","proof_digest":"sha256:exact","gate_fingerprint":"exact"}`, []string{"acceptance-apply", "--gate", "ASBUILT-X", "--file", "-"}, "POST /api/v1/systems/4/reverse-engineering/acceptances/ASBUILT-X/apply"},
		{"", []string{"acceptance-status", "--gate", "ASBUILT-X"}, "GET /api/v1/systems/4/reverse-engineering/acceptances/ASBUILT-X"},
	} {
		out, err := reCommand(t, server, step.input, step.args...)
		if err != nil {
			t.Fatalf("%v: %v", step.args, err)
		}
		if calls[len(calls)-1] != step.want || !strings.Contains(out, "sha256:exact") {
			t.Fatalf("wrong call/readback: %v %s", calls, out)
		}
	}
}

func TestSRRDDASBUILTCLI001DeliveryObservationFetchesActualDefaultBranch(t *testing.T) {
	remote := filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, t.TempDir(), "init", "--bare", "--initial-branch=main", remote)
	root := filepath.Join(t.TempDir(), "checkout")
	gitRun(t, t.TempDir(), "clone", remote, root)
	if err := os.WriteFile(filepath.Join(root, "catalog.txt"), []byte("configured product"), 0600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", "catalog.txt")
	gitRun(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "catalog")
	gitRun(t, root, "push", "origin", "main")
	inventory, err := buildReverseInventory([]string{"catalog=" + root})
	if err != nil {
		t.Fatal(err)
	}
	repo := inventory.Repositories[0]
	input := asBuiltDeliveryInput{Key: "observation", RepositoryKey: "catalog", Root: root, TestedRevision: repo.Revision, SnapshotDigest: repo.SnapshotDigest}
	report, err := collectAsBuiltDelivery(input)
	if err != nil || report["integrated_revision"] != repo.Revision || report["default_branch"] != "main" {
		t.Fatalf("delivery: %v %v", report, err)
	}
	// The first upload commits but its response is lost. A retry observes a new
	// time, yet must return the digest retained by the server on the first call.
	var retained string
	calls := 0
	omitDigest := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sync/contract" {
			w.WriteHeader(404)
			return
		}
		calls++
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if calls == 1 {
			retained = payload["raw_evidence"].(string)
			w.WriteHeader(503)
			return
		}
		if payload["raw_evidence"] == retained {
			t.Error("retry did not perform a fresh observation")
		}
		if omitDigest {
			_, _ = w.Write([]byte(`{"data":{"run":{"id":"retained-run"}}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"result": "replayed", "run": map[string]any{"id": "retained-run"},
			"report_digest": reverseDigest([]byte(retained)),
		}})
	}))
	defer server.Close()
	raw, _ := json.Marshal(input)
	if _, err := reCommand(t, server, string(raw), "delivery-proof", "--file", "-"); err == nil {
		t.Fatal("lost upload response must be reported")
	}
	out, err := reCommand(t, server, string(raw), "delivery-proof", "--file", "-")
	if err != nil || !strings.Contains(out, reverseDigest([]byte(retained))) || calls != 2 {
		t.Fatalf("retry lost the retained proof: %s %v calls=%d", out, err, calls)
	}
	omitDigest = true
	if _, err := reCommand(t, server, string(raw), "delivery-proof", "--file", "-"); err == nil || !strings.Contains(err.Error(), "retained report digest") {
		t.Fatalf("server without a retained digest accepted: %v", err)
	}
	gitRun(t, root, "checkout", "-b", "unmerged")
	gitRun(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "unmerged change")
	inventory, err = buildReverseInventory([]string{"catalog=" + root})
	if err != nil {
		t.Fatal(err)
	}
	input.TestedRevision = inventory.Repositories[0].Revision
	input.SnapshotDigest = inventory.Repositories[0].SnapshotDigest
	if _, err := collectAsBuiltDelivery(input); err == nil || !strings.Contains(err.Error(), "integration proof is missing") {
		t.Fatalf("unmerged branch accepted: %v", err)
	}
	raw, _ = json.Marshal(input)
	if _, err := reCommand(t, server, string(raw), "delivery-proof", "--file", "-"); err == nil || calls != 3 {
		t.Fatalf("retry skipped current integration check: %v calls=%d", err, calls)
	}
}

func TestSRRDDASBUILTCLI001ExecutionProofRetainsExactReportAndDurableIDs(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sync/contract" {
			w.WriteHeader(404)
			return
		}
		if r.Method != "POST" || r.URL.Path != "/api/v1/sync/evidence" {
			t.Errorf("wrong evidence channel: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&received)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"run": map[string]any{"id": "run-uuid"}, "results": []map[string]string{{"id": "result-uuid"}}, "report_digest": "sha256:server-retained"}})
	}))
	defer server.Close()
	report := `{"command":"existing tests","environment":"local","executed_tests":[{"test_case_ref":"catalog test","result":"pass"}]}`
	input := map[string]any{"key": "execution", "kind": "local_test", "sha": "tested-sha", "ran_at": "2026-09-27T09:00:00Z", "raw_evidence": report, "results": []map[string]any{{"target_external_id": "SR-X", "target_type": "requirement", "target_clause": "AC-X", "test_case_ref": "catalog test", "role": "LOWER", "result": "pass", "content_fingerprint": "exact", "revision": "tested-sha", "detail": map[string]string{"assertion": "product is visible", "production_subject": "catalog.products"}}}}
	raw, _ := json.Marshal(input)
	out, err := reCommand(t, server, string(raw), "execution-proof", "--file", "-")
	if err != nil || !strings.Contains(out, "result-uuid") || !strings.Contains(out, "sha256:server-retained") || strings.Contains(out, reverseDigest([]byte(report))) {
		t.Fatalf("readback: %s %v", out, err)
	}
	if received["system_id"] != float64(4) || received["raw_evidence"] != report {
		t.Fatalf("scope/report changed: %v", received)
	}
}

func TestSRRDDASBUILTCLI001ExecutionProofRequiresServerRetainedDigest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sync/contract" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"run":{"id":"run-uuid"},"results":[{"id":"result-uuid"}]}}`))
	}))
	defer server.Close()
	report := `{"command":"existing tests","environment":"local","executed_tests":[{"test_case_ref":"catalog test","result":"pass"}]}`
	input := map[string]any{"key": "execution", "kind": "local_test", "sha": "tested-sha", "ran_at": "2026-09-27T09:00:00Z", "raw_evidence": report, "results": []map[string]any{{"target_external_id": "SR-X", "target_type": "requirement", "target_clause": "AC-X", "test_case_ref": "catalog test", "role": "LOWER", "result": "pass", "content_fingerprint": "exact", "revision": "tested-sha", "detail": map[string]string{"assertion": "product is visible", "production_subject": "catalog.products"}}}}
	raw, _ := json.Marshal(input)
	if _, err := reCommand(t, server, string(raw), "execution-proof", "--file", "-"); err == nil || !strings.Contains(err.Error(), "server did not return the retained report digest") {
		t.Fatalf("missing server digest was accepted: %v", err)
	}
}

func TestSRRDDASBUILTCLI001UnknownFieldsRefuseBeforeCall(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}})
	}))
	defer server.Close()
	for _, input := range []string{`{"version":1,"requirements":[],"delivery":[],"skip_red":true}`, `{"version":1,"requirements":[{"kind":"SR","external_id":"SR-X","content_fingerprint":"x","clauses":[],"actor_id":2}],"delivery":[]}`} {
		_, err := reCommand(t, server, input, "proof-preview", "--file", "-")
		if err == nil || calls != 0 {
			t.Fatalf("invalid input reached server: %v calls=%d", err, calls)
		}
	}
}

func TestSRRDDASBUILTCLI001InventoryRejectsRepositoryKeysContainingEquals(t *testing.T) {
	_, input := asBuiltDeliveryFixture(t)
	input.RepositoryKey = "bad=key"
	if _, err := collectAsBuiltDelivery(input); err == nil || !strings.Contains(err.Error(), "repository key") {
		t.Fatalf("repository key containing '=' was not explicitly rejected: %v", err)
	}
}

func TestSRRDDASBUILTCLI001DeliveryOriginAllowsSSHUsernameOnly(t *testing.T) {
	root, input := asBuiltDeliveryFixture(t)
	gitRun(t, root, "remote", "set-url", "origin", "ssh://git@code.example/catalog.git")
	if _, err := collectAsBuiltDelivery(input); err == nil || strings.Contains(err.Error(), "credentials") {
		t.Fatalf("SSH username-only origin was rejected as credentials: %v", err)
	}
}

func TestSRRDDASBUILTCLI001DeliveryOriginRejectsHTTPCredentialsQueryAndFragment(t *testing.T) {
	for _, origin := range []string{
		"https://user:password@code.example/catalog.git",
		"https://user@code.example/catalog.git",
		"https://code.example/catalog.git?token=secret",
		"https://code.example/catalog.git#branch",
	} {
		t.Run(origin, func(t *testing.T) {
			root, input := asBuiltDeliveryFixture(t)
			gitRun(t, root, "remote", "set-url", "origin", origin)
			if _, err := collectAsBuiltDelivery(input); err == nil || !strings.Contains(err.Error(), "origin must not contain") {
				t.Fatalf("unsafe origin accepted or wrong refusal: %v", err)
			}
		})
	}
}

func TestSRRDDASBUILTCLI001DeliveryReportsMeasuredCommandArguments(t *testing.T) {
	_, input := asBuiltDeliveryFixture(t)
	report, err := collectAsBuiltDelivery(input)
	if err != nil {
		t.Fatal(err)
	}
	commands, ok := report["commands"].([][]string)
	if !ok || len(commands) != 3 {
		t.Fatalf("delivery report does not contain the argv actually executed: %#v", report["commands"])
	}
	want := [][]string{
		{"git", "ls-remote", "--symref", "origin", "HEAD"},
		{"git", "fetch", "--quiet", "origin", "+refs/heads/main:refs/remotes/origin/main"},
		{"git", "rev-parse", "refs/remotes/origin/main"},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("recorded argv = %#v, want %#v", commands, want)
	}
}

func TestSRRDDASBUILTCLI001DeliveryRefusalsDistinguishDirtyRevisionAndDigest(t *testing.T) {
	root, input := asBuiltDeliveryFixture(t)
	if err := os.WriteFile(filepath.Join(root, "changed.txt"), []byte("dirty"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := collectAsBuiltDelivery(input); err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("dirty repository refusal was not specific: %v", err)
	}

	gitRun(t, root, "add", "changed.txt")
	gitRun(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "change")
	if _, err := collectAsBuiltDelivery(input); err == nil || !strings.Contains(err.Error(), "revision") {
		t.Fatalf("revision mismatch refusal was not specific: %v", err)
	}

	current, err := buildReverseInventory([]string{"catalog=" + root})
	if err != nil {
		t.Fatal(err)
	}
	input.TestedRevision = current.Repositories[0].Revision
	input.SnapshotDigest = "sha256:wrong"
	if _, err := collectAsBuiltDelivery(input); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("snapshot digest mismatch refusal was not specific: %v", err)
	}
}

func asBuiltDeliveryFixture(t *testing.T) (string, asBuiltDeliveryInput) {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, t.TempDir(), "init", "--bare", "--initial-branch=main", remote)
	root := filepath.Join(t.TempDir(), "checkout")
	gitRun(t, t.TempDir(), "clone", remote, root)
	if err := os.WriteFile(filepath.Join(root, "catalog.txt"), []byte("configured product"), 0600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", "catalog.txt")
	gitRun(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "catalog")
	gitRun(t, root, "push", "origin", "main")
	inventory, err := buildReverseInventory([]string{"catalog=" + root})
	if err != nil {
		t.Fatal(err)
	}
	repo := inventory.Repositories[0]
	return root, asBuiltDeliveryInput{Key: "observation", RepositoryKey: "catalog", Root: root, TestedRevision: repo.Revision, SnapshotDigest: repo.SnapshotDigest}
}
