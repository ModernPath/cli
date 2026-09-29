package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-CROSS-442 (PR #694 review, #7): `author apply` read each record's
// fingerprint at run time, so an edit another session made after the plan was
// written was overwritten with exit 0. A plan record carries the fingerprint
// it was written against (expected_fingerprint), or apply takes it from the
// record's working-set pull snapshot; a record with neither is refused.

// editedElsewhere is the store after another session edited REQ-T-1 once the
// plan was written against fp-seed-REQ-T-1.
func editedElsewhere(s *fakeAuthorStore) {
	seedApplyStore(s)
	s.records["REQ-T-1"]["description"] = "another session's edit"
	s.records["REQ-T-1"]["fingerprint"] = "fp-other-edit"
}

const stalePlanYAML = `requirements:
  - id: REQ-T-1
    kind: sr
    context: CROSS
    title: An existing SR
    description: the new statement
    expected_fingerprint: fp-seed-REQ-T-1
`

func TestREQCROSS442AuthorApplyRefusesARecordChangedSinceThePlan(t *testing.T) {
	store := newFakeAuthorStore()
	editedElsewhere(store)
	cobraWorkspace(t, store.serve(t))
	plan := writePlanFile(t, "plan.yaml", stalePlanYAML)

	out, err := runRoot(t, "author", "apply", "--file", plan)
	if err == nil {
		t.Fatalf("a record changed since the plan must refuse, exit 0 today\n%s", out)
	}
	if len(store.posts) != 0 {
		t.Errorf("the refusal comes before any write, posted %v", store.posts)
	}
	if store.records["REQ-T-1"]["description"] != "another session's edit" {
		t.Errorf("the other session's edit was overwritten: %v", store.records["REQ-T-1"])
	}
	msg := err.Error() + out
	for _, want := range []string{"REQ-T-1", "fp-seed-REQ-T-1", "fp-other-edit"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal names the record and both fingerprints; %q is missing: %s", want, msg)
		}
	}
}

func TestREQCROSS442AuthorApplyFromPullGuardsOnTheSnapshot(t *testing.T) {
	store := newFakeAuthorStore()
	editedElsewhere(store)
	cobraWorkspace(t, store.serve(t))
	// The pull the plan was written from: REQ-T-1 at fp-seed-REQ-T-1.
	snap := "# REQ-T-1 — working-set snapshot\n\n- **Snapshot at:** 2026-09-28T10:00:00Z\n\n" +
		"## REQ-T-1 — An existing SR\n\n- **Fingerprint:** fp-seed-REQ-T-1\n- **Kind / status:** requirement / PROPOSED\n"
	if err := os.MkdirAll(filepath.Join(".modernpath", "working-set"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".modernpath", "working-set", "REQ-T-1.md"), []byte(snap), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := writePlanFile(t, "plan.yaml", strings.ReplaceAll(stalePlanYAML, "    expected_fingerprint: fp-seed-REQ-T-1\n", ""))

	out, err := runRoot(t, "author", "apply", "--file", plan, "--from-pull")
	if err == nil {
		t.Fatalf("--from-pull must refuse a record that moved since its pull\n%s", out)
	}
	if len(store.posts) != 0 {
		t.Errorf("the refusal comes before any write, posted %v", store.posts)
	}
	msg := err.Error() + out
	for _, want := range []string{"REQ-T-1", "fp-seed-REQ-T-1", "fp-other-edit", "working-set pull"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal names the record, both fingerprints and the pull to refresh; %q is missing: %s", want, msg)
		}
	}
}

// writePullSnapshot writes the by-id working-set snapshot of REQ-T-1 at fp.
func writePullSnapshot(t *testing.T, fp string) {
	t.Helper()
	snap := "# REQ-T-1 — working-set snapshot\n\n- **Snapshot at:** 2026-09-28T10:00:00Z\n\n" +
		"## REQ-T-1 — An existing SR\n\n- **Fingerprint:** " + fp + "\n- **Kind / status:** requirement / PROPOSED\n"
	if err := os.MkdirAll(filepath.Join(".modernpath", "working-set"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".modernpath", "working-set", "REQ-T-1.md"), []byte(snap), 0o644); err != nil {
		t.Fatal(err)
	}
}

// REQ-CROSS-442 (revised): the expected fingerprint of an existing record is
// the one its working-set pull recorded, with no flag, so a plan written from
// a pull and applied after another session's edit is refused with no write.
// Before, it overwrote the edit and exited 0.
func TestREQCROSS442ApplyRefusesAPlanEditedAfterItsPull(t *testing.T) {
	store := newFakeAuthorStore()
	editedElsewhere(store)
	cobraWorkspace(t, store.serve(t))
	writePullSnapshot(t, "fp-seed-REQ-T-1")
	plan := writePlanFile(t, "plan.yaml", strings.ReplaceAll(stalePlanYAML, "    expected_fingerprint: fp-seed-REQ-T-1\n", ""))

	out, err := runRoot(t, "author", "apply", "--file", plan)
	if err == nil {
		t.Fatalf("a record that moved since its pull must refuse the plan, exit 0 today\n%s", out)
	}
	if len(store.posts) != 0 {
		t.Errorf("the refusal comes before any write, posted %v", store.posts)
	}
	if store.records["REQ-T-1"]["description"] != "another session's edit" {
		t.Errorf("the other session's edit was overwritten: %v", store.records["REQ-T-1"])
	}
	msg := err.Error() + out
	for _, want := range []string{"REQ-T-1", "fp-seed-REQ-T-1", "fp-other-edit", "working-set pull"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal names the record, both fingerprints and the pull to refresh; %q is missing: %s", want, msg)
		}
	}
}

// An existing record the plan would change, with neither a pull nor a pinned
// fingerprint, has no version to guard on: apply refuses it before any write
// and never reads one at run time.
func TestREQCROSS442ApplyRefusesAnExistingRecordWithNoPullOrPin(t *testing.T) {
	store := newFakeAuthorStore()
	seedApplyStore(store)
	cobraWorkspace(t, store.serve(t))
	plan := writePlanFile(t, "plan.yaml", strings.ReplaceAll(stalePlanYAML, "    expected_fingerprint: fp-seed-REQ-T-1\n", ""))

	out, err := runRoot(t, "author", "apply", "--file", plan)
	if err == nil {
		t.Fatalf("an unguarded change to an existing record must refuse, exit 0 today\n%s", out)
	}
	if len(store.posts) != 0 {
		t.Errorf("the refusal comes before any write, posted %v", store.posts)
	}
	msg := err.Error() + out
	for _, want := range []string{"REQ-T-1", "expected_fingerprint", "working-set pull"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal names the record and both ways to guard it; %q is missing: %s", want, msg)
		}
	}
}
