package cmd

// Proposed consumer RED cases for SR-CLI-REVIEW-SNAPSHOT-001. Keep each
// failure a top-level test: scripts/verify-cli-suite.mjs rejects failing child
// t.Run events.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type reviewConsumerCapture struct {
	posted map[string]any
}

func reviewConsumerServer(t *testing.T, liveAggregate string, capture *reviewConsumerCapture) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capture.posted)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"finding": map[string]any{"external_id": "F-REVIEW", "fingerprint": strings.Repeat("f", 64)},
			"gate":    map[string]any{"external_id": "TRACE-REVIEW", "state": "pass", "fingerprint": strings.Repeat("f", 64)},
		}})
	})
	mux.HandleFunc("/api/v1/sync/delivery-context", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"packet_fingerprint": liveAggregate,
			"facts": map[string]any{"aggregate": liveAggregate, "scope": map[string]any{"external_id": "EPIC-F", "kind": "epic"},
				"members": []any{}, "sections": map[string]any{"required": []any{"reconnaissance"}}},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func reviewConsumerWorkspace(t *testing.T, liveAggregate string) (*httptest.Server, *reviewConsumerCapture, string, string, string) {
	t.Helper()
	capture := &reviewConsumerCapture{}
	srv := reviewConsumerServer(t, liveAggregate, capture)
	cobraWorkspace(t, srv)
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	oldAgg, newAgg := strings.Repeat("a", 64), strings.Repeat("b", 64)
	oldDir := writeReviewConsumerSnapshot(t, root, srv.URL, 1, "review-old", "epic", "EPIC-F", oldAgg)
	_ = writeReviewConsumerSnapshot(t, root, srv.URL, 1, "review-new", "epic", "EPIC-F", newAgg)
	return srv, capture, root, oldDir, oldAgg
}

func writeReviewConsumerSnapshot(t *testing.T, root, storeURL string, systemID int, ctxID, scopeKind, scopeID, aggregate string) string {
	t.Helper()
	scopeRoot := filepath.Join(root, reviewSnapshotDir, scopeID)
	content := []byte("# Reviewed item\n\nSnapshot data.\n")
	stamp := []byte("# review context\ncontext_id: " + ctxID + "\n")
	files := map[string]string{"item.md": sha256Hex(content), contextFile: sha256Hex(stamp)}
	manifest := reviewSnapshotManifest{
		StoreURL: storeURL, SystemID: systemID, Version: 1, ScopeKind: scopeKind, ScopeExternalID: scopeID,
		ContextID: ctxID, AggregateFingerprint: aggregate, SelectionFingerprint: strings.Repeat("1", 64),
		ReconRevision: "recon-1", RequiredSections: []string{"reconnaissance"}, Files: files,
	}
	manifest.SnapshotDigest, _ = reviewSnapshotDigest(manifest)
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	plan := scopedPullPlan{files: map[string]scopedPullFile{
		"item.md":       {content: content, origin: "item"},
		contextFile:     {content: stamp, origin: "context"},
		"MANIFEST.json": {content: append(manifestBytes, '\n'), origin: "manifest"},
	}}
	if err := writeReviewSnapshot(scopeRoot, ctxID, plan); err != nil {
		t.Fatalf("write review fixture: %v", err)
	}
	return filepath.Join(scopeRoot, ctxID)
}

func mutateReviewManifest(t *testing.T, dir string, update func(*reviewSnapshotManifest), recalc bool) {
	t.Helper()
	path := filepath.Join(dir, "MANIFEST.json")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest reviewSnapshotManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	update(&manifest)
	if recalc {
		manifest.SnapshotDigest, err = reviewSnapshotDigest(manifest)
		if err != nil {
			t.Fatal(err)
		}
	}
	raw, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertReviewConsumerRefusal(t *testing.T, err error, reason string, capture *reviewConsumerCapture) {
	t.Helper()
	if err == nil {
		t.Fatal("expected review snapshot refusal before author POST")
	}
	if strings.Contains(strings.ToLower(err.Error()), "unknown flag") || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(reason)) {
		t.Fatalf("expected %q refusal, got %v", reason, err)
	}
	if capture.posted != nil {
		t.Fatalf("refused review operation must not POST, got %v", capture.posted)
	}
}

func runReviewFinding(t *testing.T, args ...string) error {
	t.Helper()
	cmdArgs := []string{"process", "findings", "add", "--scope", "epic:EPIC-F", "--id", "F-REVIEW",
		"--severity", "major", "--category", "correctness", "--body", "finding body", "--source", "review"}
	cmdArgs = append(cmdArgs, args...)
	_, err := runRoot(t, cmdArgs...)
	return err
}

func runReviewTrace(t *testing.T, args ...string) error {
	t.Helper()
	cmdArgs := []string{"author", "trace", "TRACE-REVIEW", "--purpose", "cold-review", "--scope", "EPIC-F", "--verdict", "PASS"}
	cmdArgs = append(cmdArgs, args...)
	_, err := runRoot(t, cmdArgs...)
	return err
}

func reviewRecord(capture *reviewConsumerCapture) map[string]any {
	if capture.posted == nil {
		return nil
	}
	record, _ := capture.posted["record"].(map[string]any)
	return record
}

func reviewManifestDigestFromDisk(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "MANIFEST.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest reviewSnapshotManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest.SnapshotDigest
}

func TestFindingsAddReviewContextFlagSelectsOlderSnapshotAndEmbedsPin(t *testing.T) {
	live := strings.Repeat("f", 64)
	_, capture, _, oldDir, oldAgg := reviewConsumerWorkspace(t, live)
	if err := runReviewFinding(t, "--review-context", "review-old"); err != nil {
		t.Fatalf("findings add: %v", err)
	}
	record := reviewRecord(capture)
	if record == nil || record["review_context_id"] != "review-old" || record["aggregate_fingerprint"] != oldAgg {
		t.Fatalf("finding should use selected snapshot id and aggregate, got %v", record)
	}
	digest := reviewManifestDigestFromDisk(t, oldDir)
	body, _ := record["body"].(string)
	if !strings.Contains(body, "review-old") || !strings.Contains(body, oldAgg) || !strings.Contains(body, digest) {
		t.Fatalf("finding body must bind selected snapshot id, aggregate and digest: %q", body)
	}
}

func TestAuthorTraceReviewContextFlagSelectsOlderSnapshotAndEmbedsPin(t *testing.T) {
	live := strings.Repeat("f", 64)
	_, capture, _, oldDir, oldAgg := reviewConsumerWorkspace(t, live)
	if err := runReviewTrace(t, "--review-context", "review-old"); err != nil {
		t.Fatalf("author trace: %v", err)
	}
	record := reviewRecord(capture)
	if record == nil || record["review_context_id"] != "review-old" || record["fingerprint"] != oldAgg {
		t.Fatalf("trace should use selected snapshot id and aggregate, got %v", record)
	}
	digest := reviewManifestDigestFromDisk(t, oldDir)
	body, _ := record["body_md"].(string)
	if !strings.Contains(body, "review-old") || !strings.Contains(body, oldAgg) || !strings.Contains(body, digest) {
		t.Fatalf("trace body_md must bind selected snapshot id, aggregate and digest: %q", body)
	}
}

func TestFindingsAddWithoutSelectorDoesNotInheritAuthoringReviewStamp(t *testing.T) {
	live := strings.Repeat("f", 64)
	capture := &reviewConsumerCapture{}
	srv := reviewConsumerServer(t, live, capture)
	cobraWorkspace(t, srv)
	root, _ := os.Getwd()
	dir := filepath.Join(root, workingSetDir, "EPIC-F")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, contextFile), []byte("mode: review\ncontext_id: legacy-review\nscope: epic:EPIC-F\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runReviewFinding(t); err != nil {
		t.Fatalf("regular finding should still post: %v", err)
	}
	record := reviewRecord(capture)
	if record == nil || record["aggregate_fingerprint"] != live {
		t.Fatalf("regular finding should use live aggregate, got %v", record)
	}
	if record["review_context_id"] != nil && record["review_context_id"] != "" {
		t.Fatalf("regular finding inherited a review context: %v", record["review_context_id"])
	}
}

func TestAuthorTraceColdReviewWithoutSelectorRefusesEvenWithLegacyStamp(t *testing.T) {
	var posted bool
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) { posted = true })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)
	dir := filepath.Join(env.Root, workingSetDir, "EPIC-F")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, contextFile), []byte("mode: review\ncontext_id: legacy-review\nscope: epic:EPIC-F\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := authorTrace(env, "TRACE-REVIEW", map[string]any{"purpose": "cold-review", "exact_scope": []string{"EPIC-F"}, "verdict": "PASS", "fingerprint": strings.Repeat("a", 64)})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "review context") || posted {
		t.Fatalf("expected review-context refusal before POST, err=%v posted=%v", err, posted)
	}
}

func reviewFailureTest(t *testing.T, ctx, reason string, mutate func(string)) {
	t.Helper()
	_, capture, _, dir, _ := reviewConsumerWorkspace(t, strings.Repeat("f", 64))
	if mutate != nil {
		mutate(dir)
	}
	err := runReviewFinding(t, "--review-context", ctx)
	assertReviewConsumerRefusal(t, err, reason, capture)
}

func reviewFindingFailureTest(t *testing.T, ctx, reason string, mutate func(string), extra ...string) {
	t.Helper()
	_, capture, _, dir, _ := reviewConsumerWorkspace(t, strings.Repeat("f", 64))
	if mutate != nil {
		mutate(dir)
	}
	args := []string{"--review-context", ctx}
	args = append(args, extra...)
	err := runReviewFinding(t, args...)
	assertReviewConsumerRefusal(t, err, reason, capture)
}

func reviewTraceFailureTest(t *testing.T, ctx, reason string, mutate func(string), extra ...string) {
	t.Helper()
	_, capture, _, dir, _ := reviewConsumerWorkspace(t, strings.Repeat("f", 64))
	if mutate != nil {
		mutate(dir)
	}
	args := []string{"--review-context", ctx}
	args = append(args, extra...)
	err := runReviewTrace(t, args...)
	assertReviewConsumerRefusal(t, err, reason, capture)
}

func TestFindingsAddRefusesEditedReviewSnapshotBeforePost(t *testing.T) {
	reviewFailureTest(t, "review-old", "integrity", func(dir string) {
		if err := os.Chmod(filepath.Join(dir, "item.md"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "item.md"), []byte("edited"), 0o644); err != nil {
			t.Fatal(err)
		}
	})
}
func TestAuthorTraceRefusesEditedReviewSnapshotBeforePost(t *testing.T) {
	reviewTraceFailureTest(t, "review-old", "integrity", func(dir string) {
		if err := os.Chmod(filepath.Join(dir, "item.md"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "item.md"), []byte("edited"), 0o644); err != nil {
			t.Fatal(err)
		}
	})
}
func TestFindingsAddRefusesAggregateOnlyManifestEditBeforePost(t *testing.T) {
	reviewFailureTest(t, "review-old", "digest", func(dir string) {
		mutateReviewManifest(t, dir, func(m *reviewSnapshotManifest) { m.AggregateFingerprint = strings.Repeat("c", 64) }, false)
	})
}
func TestAuthorTraceRefusesAggregateOnlyManifestEditBeforePost(t *testing.T) {
	reviewTraceFailureTest(t, "review-old", "digest", func(dir string) {
		mutateReviewManifest(t, dir, func(m *reviewSnapshotManifest) { m.AggregateFingerprint = strings.Repeat("c", 64) }, false)
	})
}
func TestFindingsAddRefusesReviewStoreMismatchBeforePost(t *testing.T) {
	reviewFailureTest(t, "review-old", "store", func(dir string) {
		mutateReviewManifest(t, dir, func(m *reviewSnapshotManifest) { m.StoreURL = "https://other.invalid" }, true)
	})
}
func TestFindingsAddRefusesReviewSystemMismatchBeforePost(t *testing.T) {
	reviewFailureTest(t, "review-old", "system", func(dir string) { mutateReviewManifest(t, dir, func(m *reviewSnapshotManifest) { m.SystemID++ }, true) })
}
func TestFindingsAddRefusesReviewScopeMismatchBeforePost(t *testing.T) {
	reviewFailureTest(t, "review-old", "scope", func(dir string) {
		mutateReviewManifest(t, dir, func(m *reviewSnapshotManifest) { m.ScopeExternalID = "EPIC-OTHER" }, true)
	})
}
func TestFindingsAddRefusesReviewContextMismatchBeforePost(t *testing.T) {
	reviewFailureTest(t, "review-old", "context", func(dir string) {
		mutateReviewManifest(t, dir, func(m *reviewSnapshotManifest) { m.ContextID = "review-other" }, true)
	})
}
func TestFindingsAddRefusesConflictingExplicitAggregatePinBeforePost(t *testing.T) {
	reviewFindingFailureTest(t, "review-old", "aggregate", nil, "--aggregate", strings.Repeat("c", 64))
}
func TestAuthorTraceRefusesConflictingExplicitFingerprintBeforePost(t *testing.T) {
	reviewTraceFailureTest(t, "review-old", "aggregate", nil, "--fingerprint", strings.Repeat("c", 64))
}
func TestFindingsAddRefusesUnsafeManifestPathBeforePost(t *testing.T) {
	reviewFailureTest(t, "review-old", "path", func(dir string) {
		mutateReviewManifest(t, dir, func(m *reviewSnapshotManifest) { m.Files["../outside.md"] = strings.Repeat("0", 64) }, true)
	})
}
func TestFindingsAddRefusesUnlistedSnapshotFileBeforePost(t *testing.T) {
	reviewFailureTest(t, "review-old", "unlisted", func(dir string) {
		if err := os.WriteFile(filepath.Join(dir, "extra.md"), []byte("extra"), 0o644); err != nil {
			t.Fatal(err)
		}
	})
}
