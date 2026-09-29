package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-CROSS-442 (revised): a fingerprint conflict is reported for that record
// and the rest continue; any other refusal stops the run and names what was
// written, including a record created but not yet patched and the pull that
// recovers it. The run exits non-zero when any record did not apply.

// applyLine is the output line apply printed for id.
func applyLine(out, id string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, id+" ") {
			return line
		}
	}
	return ""
}

// patchedOK reports whether the store accepted a patch of id.
func patchedOK(s *fakeAuthorStore, id string) bool {
	for _, p := range s.postsFor(id, "patch") {
		if p.status == 200 {
			return true
		}
	}
	return false
}

// refuseOnce answers the first post of action on id with status.
func refuseOnce(action, id string, status int) func(map[string]any) (bool, int, map[string]any, string) {
	done := false
	return func(body map[string]any) (bool, int, map[string]any, string) {
		rec, _ := body["record"].(map[string]any)
		if done || str(body, "action") != action || str(rec, "external_id") != id {
			return false, 0, nil, ""
		}
		done = true
		return true, status, refusalBody("the store refused " + id), ""
	}
}

func TestREQCROSS442ApplyAConflictOnOneRecordLetsTheRestContinue(t *testing.T) {
	store := newFakeAuthorStore()
	seedApplyStore(store)
	store.authorHook = refuseOnce("patch", "REQ-T-1", 409)
	cobraWorkspace(t, store.serve(t))

	out, err := runRoot(t, "author", "apply", "--file", writePlanFile(t, "plan.yaml", pinned(applyPlanYAML)))
	if err == nil {
		t.Fatalf("a record that did not apply exits non-zero\n%s", out)
	}
	for _, id := range []string{"EPIC-T", "UR-T-1", "REQ-T-2"} {
		if !patchedOK(store, id) {
			t.Errorf("%s must still be written after REQ-T-1's conflict: %v", id, store.posts)
		}
	}
	if line := applyLine(out, "REQ-T-1"); !strings.Contains(line, "conflict") {
		t.Errorf("REQ-T-1's line reports its conflict, got %q\n%s", line, out)
	}
	if !strings.Contains(err.Error(), "REQ-T-1") {
		t.Errorf("the error names the record that conflicted: %v", err)
	}
}

func TestREQCROSS442ApplyARefusalStopsTheRunAndNamesWhatWasWritten(t *testing.T) {
	store := newFakeAuthorStore()
	seedApplyStore(store)
	// Writes run creates (UR-T-1, REQ-T-2), then patches in plan order:
	// EPIC-T, UR-T-1, REQ-T-1 (refused), REQ-T-2 (never reached).
	store.fail["REQ-T-1"] = true
	cobraWorkspace(t, store.serve(t))

	out, err := runRoot(t, "author", "apply", "--file", writePlanFile(t, "plan.yaml", pinned(applyPlanYAML)))
	if err == nil {
		t.Fatalf("a refusal exits non-zero\n%s", out)
	}
	if n := len(store.postsFor("REQ-T-2", "patch")); n != 0 {
		t.Fatalf("a non-409 refusal stops the run; REQ-T-2 was still patched: %v", store.posts)
	}
	msg := err.Error()
	for _, want := range []string{"REQ-T-1", "EPIC-T", "UR-T-1", "REQ-T-2", "working-set pull REQ-T-2"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the error names the refusal, what was written and the pull that recovers the created record; %q is missing: %s", want, msg)
		}
	}
	if strings.Contains(msg, "run the same file again") {
		t.Errorf("rerunning the file cannot recover a created, unpatched record without its pull: %s", msg)
	}
	if line := applyLine(out, "REQ-T-2"); !strings.Contains(line, "created, not patched") {
		t.Errorf("a created record whose patch never ran says so, got %q\n%s", line, out)
	}
	if line := applyLine(out, "REQ-T-1"); !strings.Contains(line, "not written") {
		t.Errorf("the refused record is shown as not written, got %q\n%s", line, out)
	}
}

func TestREQCROSS442ApplyACreatedRecordWhosePatchIsRefusedIsCreatedNotPatched(t *testing.T) {
	store := newFakeAuthorStore()
	seedApplyStore(store)
	store.authorHook = refuseOnce("patch", "REQ-T-2", 422)
	cobraWorkspace(t, store.serve(t))

	out, err := runRoot(t, "author", "apply", "--file", writePlanFile(t, "plan.yaml", pinned(applyPlanYAML)))
	if err == nil {
		t.Fatalf("a refused patch exits non-zero\n%s", out)
	}
	line := applyLine(out, "REQ-T-2")
	if !strings.Contains(line, "created, not patched") {
		t.Errorf("REQ-T-2 was created but its patch refused: want `created, not patched`, got %q\n%s", line, out)
	}
	if created := store.postsFor("REQ-T-2", "create"); len(created) == 1 && strings.Contains(line, created[0].returned) && !strings.Contains(line, "not patched") {
		t.Errorf("the create's fingerprint alone reads as a finished record: %q", line)
	}
	if !strings.Contains(err.Error(), "working-set pull REQ-T-2") {
		t.Errorf("the recovery names the pull of the created record: %v", err)
	}
}

func TestREQCROSS442ApplyACreateThatAlreadyExistsSaysSo(t *testing.T) {
	store := newFakeAuthorStore()
	seedApplyStore(store)
	store.authorHook = refuseOnce("create", "UR-T-1", 409)
	cobraWorkspace(t, store.serve(t))

	out, err := runRoot(t, "author", "apply", "--file", writePlanFile(t, "plan.yaml", pinned(applyPlanYAML)))
	if err == nil {
		t.Fatalf("a record that did not apply exits non-zero\n%s", out)
	}
	line := applyLine(out, "UR-T-1")
	if !strings.Contains(line, "already exists") {
		t.Errorf("a create refused with 409 names the record as already existing, got %q\n%s", line, out)
	}
	if strings.HasSuffix(strings.TrimRight(line, " "), "since") || strings.Contains(line, "since  ") {
		t.Errorf("the line prints an empty fingerprint: %q", line)
	}
	if !patchedOK(store, "REQ-T-2") {
		t.Errorf("a 409 on one create lets the rest continue: %v", store.posts)
	}
}

// writeServedSnapshot writes the by-id working-set snapshot of REQ-T-1 at fp
// in the shape `working-set pull` writes: metadata with the written-body hash.
func writeServedSnapshot(t *testing.T, fp string) string {
	t.Helper()
	body := "## REQ-T-1 — An existing SR\n\n- **Fingerprint:** " + fp + "\n- **Kind / status:** requirement / PROPOSED\n"
	content := "# REQ-T-1 — working-set snapshot\n\n- **Snapshot at:** 2026-09-28T10:00:00Z\n- **Source store/revision:** test\n" +
		sourceIdentityKey + sha256Hex([]byte("served")) + "\n" + writtenBodyKey + sha256Hex([]byte(body)) + "\n\n" + body
	path := filepath.Join(".modernpath", "working-set", "REQ-T-1.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const unpinnedStatement = `requirements:
  - id: REQ-T-1
    kind: sr
    context: CROSS
    title: An existing SR
    description: %s
`

// After apply writes a record, its working-set pull carries the fingerprint
// the write returned, so the same session's next apply is not refused as
// "changed since its working-set pull".
func TestREQCROSS442TwoAppliesInARowFromOnePullBothSucceed(t *testing.T) {
	store := newFakeAuthorStore()
	seedApplyStore(store)
	cobraWorkspace(t, store.serve(t))
	snap := writeServedSnapshot(t, "fp-seed-REQ-T-1")

	first := writePlanFile(t, "first.yaml", strings.Replace(unpinnedStatement, "%s", "the first statement", 1))
	if out, err := runRoot(t, "author", "apply", "--file", first); err != nil {
		t.Fatalf("first apply: %v\n%s", err, out)
	}
	written := str(store.records["REQ-T-1"], "fingerprint")
	if fp, _ := snapshotFingerprint(".", "REQ-T-1"); fp != written {
		t.Fatalf("the pull snapshot still guards on %q; the write returned %q", fp, written)
	}
	raw, err := os.ReadFile(snap)
	if err != nil {
		t.Fatal(err)
	}
	parsed, perr := parseWorkingSetSnapshot("REQ-T-1.md", string(raw))
	if perr != nil || !snapshotBodyMatches(parsed) {
		t.Errorf("the refreshed snapshot must not read as a local edit to the next pull (%v)", perr)
	}
	if !strings.Contains(string(raw), sourceIdentityKey+sha256Hex([]byte("served"))) {
		t.Errorf("the source identity stays at the pull, so `working-set check` still reports the snapshot stale:\n%s", raw)
	}
	if strings.Contains(string(raw), "old statement") || !strings.Contains(string(raw), "An existing SR") {
		t.Errorf("only the fingerprint line changes:\n%s", raw)
	}

	second := writePlanFile(t, "second.yaml", strings.Replace(unpinnedStatement, "%s", "the second statement", 1))
	if out, err := runRoot(t, "author", "apply", "--file", second); err != nil {
		t.Fatalf("the second apply from the same pull must succeed: %v\n%s", err, out)
	}
	if store.records["REQ-T-1"]["description"] != "the second statement" {
		t.Errorf("the second apply wrote nothing: %v", store.records["REQ-T-1"])
	}
}

// apply moves the by-id snapshot's fingerprint but not its content, so the
// success line says the content is from before the write: a copy taken from
// the snapshot would carry the old text.
func TestREQCROSS442ApplySaysTheRefreshedSnapshotContentPredatesTheWrite(t *testing.T) {
	store := newFakeAuthorStore()
	seedApplyStore(store)
	cobraWorkspace(t, store.serve(t))
	writeServedSnapshot(t, "fp-seed-REQ-T-1")

	out, err := runRoot(t, "author", "apply", "--file", writePlanFile(t, "plan.yaml", strings.Replace(unpinnedStatement, "%s", "the new statement", 1)))
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	line := applyLine(out, "REQ-T-1")
	for _, want := range []string{"updated", "before", "`working-set pull REQ-T-1`"} {
		if !strings.Contains(line, want) {
			t.Errorf("the success line says the snapshot content predates the write and to re-pull; %q is missing: %q\n%s", want, line, out)
		}
	}
}

// A scope pull's member file is what push diffs against: moving only its
// fingerprint would let the next push revert the write. apply leaves it and
// says to re-pull it.
func TestREQCROSS442ApplyAfterAScopePullSaysToRePullIt(t *testing.T) {
	store := newFakeAuthorStore()
	seedApplyStore(store)
	cobraWorkspace(t, store.serve(t))
	member := filepath.Join(".modernpath", "working-set", "EPIC-T", "members", "REQ-T-1.md")
	if err := os.MkdirAll(filepath.Dir(member), 0o755); err != nil {
		t.Fatal(err)
	}
	file := "# REQ-T-1 — working-set (authoring)\n\n- **Kind:** system\n- **Served fingerprint:** fp-seed-REQ-T-1\n- **Context:** authoring:authoring-x\n\n" +
		"## description\n```authoring:editable\nold statement\n```\n"
	if err := os.WriteFile(member, []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runRoot(t, "author", "apply", "--file", writePlanFile(t, "plan.yaml", strings.Replace(unpinnedStatement, "%s", "the new statement", 1)))
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	if line := applyLine(out, "REQ-T-1"); !strings.Contains(line, "re-pull") || !strings.Contains(line, "updated") {
		t.Errorf("the success line says to re-pull the scope file, got %q\n%s", line, out)
	}
	raw, _ := os.ReadFile(member)
	if string(raw) != file {
		t.Errorf("a push-able scope file is left as pulled:\n%s", raw)
	}
}
