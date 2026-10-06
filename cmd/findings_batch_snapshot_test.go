package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SR-CLI-REVIEW-SNAPSHOT-001 AC4 applies to the batch command as well as
// individual findings: every entry must describe the selected snapshot.
func batchSnapshotWorkspace(t *testing.T) (*fakeAuthorStore, string, []findingEntry) {
	t.Helper()
	store := newFakeAuthorStore()
	store.contexts["EPIC-F"] = map[string]any{"packet_fingerprint": strings.Repeat("f", 64)}
	store.contexts["EPIC-OTHER"] = map[string]any{"packet_fingerprint": strings.Repeat("e", 64)}
	srv := store.serve(t)
	cobraWorkspace(t, srv)
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := writeReviewConsumerSnapshot(t, root, srv.URL, 1, "review-old", "epic", "EPIC-F", strings.Repeat("a", 64))
	writeReviewConsumerSnapshot(t, root, srv.URL, 1, "review-new", "epic", "EPIC-F", strings.Repeat("b", 64))
	entries := []findingEntry{
		{ID: "F-ONE", Scope: "epic:EPIC-F", Category: "correctness", Severity: "major", Source: "review", Body: "first finding"},
		{ID: "F-TWO", Scope: "epic:EPIC-F", Category: "contract", Severity: "minor", Source: "review", Body: "second finding"},
	}
	return store, dir, entries
}

func runBatchSnapshotFindings(t *testing.T, entries []findingEntry, context string, extra ...string) (string, error) {
	t.Helper()
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	file := writePlanFile(t, "findings.json", string(raw))
	args := []string{"process", "findings", "add", "--file", file, "--review-context", context}
	return runRoot(t, append(args, extra...)...)
}

func assertBatchSnapshotRefusal(t *testing.T, store *fakeAuthorStore, entries []findingEntry, context, reason string, extra ...string) {
	t.Helper()
	out, err := runBatchSnapshotFindings(t, entries, context, extra...)
	if err == nil || !strings.Contains(err.Error(), reason) {
		t.Fatalf("expected %s refusal, got %v\n%s", reason, err, out)
	}
	if len(store.posts) != 0 {
		t.Fatalf("snapshot preflight must refuse the entire batch before POST: %v", store.posts)
	}
}

func TestBatchFindingsBindEveryEntryToSelectedSnapshot(t *testing.T) {
	store, dir, entries := batchSnapshotWorkspace(t)
	out, err := runBatchSnapshotFindings(t, entries, "review-old")
	if err != nil {
		t.Fatalf("batch findings: %v\n%s", err, out)
	}
	creates := findingCreates(store)
	if len(creates) != len(entries) {
		t.Fatalf("expected both findings, got %v", creates)
	}
	for _, e := range entries {
		r := creates[e.ID]
		if r["aggregate_fingerprint"] != strings.Repeat("a", 64) || r["review_context_id"] != "review-old" {
			t.Errorf("%s must use the selected snapshot, got %v", e.ID, r)
		}
		body := str(r, "body")
		if !strings.HasPrefix(body, e.Body) || !strings.Contains(body, reviewManifestDigestFromDisk(t, dir)) {
			t.Errorf("%s must retain the authored body and selected snapshot digest: %q", e.ID, body)
		}
	}
}

func TestBatchFindingsRejectMissingSnapshotBeforeAnyPost(t *testing.T) {
	store, _, entries := batchSnapshotWorkspace(t)
	assertBatchSnapshotRefusal(t, store, entries, "review-missing", "was not found")
}

func TestBatchFindingsRejectEditedSnapshotBeforeAnyPost(t *testing.T) {
	store, dir, entries := batchSnapshotWorkspace(t)
	path := filepath.Join(dir, "item.md")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	assertBatchSnapshotRefusal(t, store, entries, "review-old", "integrity")
}

func TestBatchFindingsRejectCommandPinMismatchBeforeAnyPost(t *testing.T) {
	store, _, entries := batchSnapshotWorkspace(t)
	assertBatchSnapshotRefusal(t, store, entries, "review-old", "aggregate pin", "--aggregate", strings.Repeat("c", 64))
}

func TestBatchFindingsRejectLaterEntryPinMismatchBeforeAnyPost(t *testing.T) {
	store, _, entries := batchSnapshotWorkspace(t)
	entries[1].Aggregate = strings.Repeat("c", 64)
	assertBatchSnapshotRefusal(t, store, entries, "review-old", "aggregate pin")
}

func TestBatchFindingsRejectLaterEntryScopeMismatchBeforeAnyPost(t *testing.T) {
	store, _, entries := batchSnapshotWorkspace(t)
	entries[1].Scope = "epic:EPIC-OTHER"
	assertBatchSnapshotRefusal(t, store, entries, "review-old", "scope")
}

func TestBatchFindingsRejectLaterMissingIDBeforeAnyPost(t *testing.T) {
	store, _, entries := batchSnapshotWorkspace(t)
	entries[1].ID = ""
	assertBatchSnapshotRefusal(t, store, entries, "review-old", "id")
}

// SR-CLI-REVIEW-SNAPSHOT-001 AC4 and UR-CLI-DRAFT-PROTECTION-001 AC4:
// every finding write must still describe the snapshot selected at batch entry.
func TestBatchFindingsStopWhenSnapshotChangesBetweenPosts(t *testing.T) {
	for _, rehash := range []bool{false, true} {
		name := "edited_file"
		if rehash {
			name = "replaced_snapshot"
		}
		t.Run(name, func(t *testing.T) {
			store, dir, entries := batchSnapshotWorkspace(t)
			entries = append(entries, findingEntry{ID: "F-THREE", Scope: entries[0].Scope,
				Category: "correctness", Severity: "minor", Source: "review", Body: "third finding"})
			path := filepath.Join(dir, "item.md")
			original := readScopeFile(t, path)
			manifestPath := filepath.Join(dir, "MANIFEST.json")
			manifest := readScopeFile(t, manifestPath)
			var mutationErr error
			store.authorHook = func(body map[string]any) (bool, int, map[string]any, string) {
				record, _ := body["record"].(map[string]any)
				if body["action"] == "create" && record["external_id"] == "F-ONE" {
					mutationErr = os.Chmod(path, 0o644)
					if mutationErr == nil {
						mutationErr = os.WriteFile(path, []byte("edited during finding submission\n"), 0o644)
					}
					if mutationErr == nil && rehash {
						rehashReviewTestFile(t, dir, "item.md")
					}
				}
				return false, 0, nil, ""
			}
			out, err := runBatchSnapshotFindings(t, entries, "review-old")
			if mutationErr != nil {
				t.Fatal(mutationErr)
			}
			if err == nil || !strings.Contains(err.Error(), "2 of 3") || !strings.Contains(out, "snapshot") {
				t.Fatalf("expected remaining findings refused after snapshot change: %v\n%s", err, out)
			}
			if creates := findingCreates(store); len(creates) != 1 || creates["F-ONE"] == nil {
				t.Fatalf("only the first finding may be posted, got %v", creates)
			}
			store.authorHook = nil
			if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(manifestPath, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
				t.Fatal(err)
			}
			if out, err := runBatchSnapshotFindings(t, entries, "review-old"); err != nil {
				t.Fatalf("retry after restoring snapshot: %v\n%s", err, out)
			}
			if got := len(store.posts); got != 3 {
				t.Fatalf("retry must post only the two remaining findings, got %d posts", got)
			}
		})
	}
}
