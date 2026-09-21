package cmd

// SR-CROSS-328 — RED first. The store-backed release read reports the active
// release WITH its USER: source, taken from an answered release_selection gate
// (external id GATE-RELEASE-<slug>, purpose release_selection) read via
// GET /api/v1/sync/gates — never from local config or a request body. It
// composes that gate with the base-excluded one-active release set the
// work-selection read already serves (list_active_process_for_tenant/1,
// REQ-CROSS-340 — active-only, never planned).
//
// Each case drives the read through workingSetPull("selection"), which writes
// WORK-SELECTION.md — the snapshot rdd-start preflight step 2 reads. The read is
// behind the store-backed marker; a file-backed workspace renders as before.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// markStoreBacked writes the flip's tracked marker into the workspace root so
// storeBackedWorkspace(root) reads true — the guard the store-backed release
// read is behind.
func markStoreBacked(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "process"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "process", "store-backed.md"),
		[]byte("# Store-backed declaration\nretired: process/releases.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// releaseSelectionGate is an answered release_selection gate bound to slug by the
// GATE-RELEASE-<slug> external-id convention, carrying a USER: source_tag —
// exactly what the localhost backfill authors and answers.
func releaseSelectionGate(slug, source string) map[string]any {
	return map[string]any{
		"external_id":        "GATE-RELEASE-" + slug,
		"kind":               "approval_request",
		"purpose":            "release_selection",
		"state":              "answered",
		"answer":             "approved",
		"answered_at":        "2026-08-26T00:00:00Z",
		"source_tag":         source,
		"chosen_option_keys": []any{"approve"},
		"exact_scope":        []any{"system:4", "release:" + slug},
	}
}

func oneActiveSelection(slug string) map[string]any {
	// REQ-CROSS-340: the server's active_release now carries only genuinely
	// active releases (the canonical active+slug+not-base predicate), never a
	// planned one. This fixture models that contract; the earlier "(planned)"
	// shape was REQ-CROSS-328 evidence and is superseded here.
	return map[string]any{
		"active_release": []any{map[string]any{"slug": slug, "status": "active"}},
		"current":        map[string]any{"scope_external_id": "EPIC-X", "scope_kind": "epic"},
		"suspended":      []any{},
		"history":        []any{},
	}
}

func readWorkSelection(t *testing.T, env *factoryEnv) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(env.Root, workingSetDir, "WORK-SELECTION.md"))
	if err != nil {
		t.Fatalf("WORK-SELECTION.md not written: %v", err)
	}
	return string(raw)
}

// (a) exactly one active release + an answered release_selection gate for that
// slug carrying a USER: source → active WITH its source.
func TestStoreBackedReleaseReadReportsActiveWithSource(t *testing.T) {
	fx := &wsFixture{
		workSelection: oneActiveSelection("modernpath-v1-09"),
		answeredGates: []any{releaseSelectionGate("modernpath-v1-09", "USER:2026-08-04")},
	}
	env := wsEnv(t, wsServe(t, fx))
	markStoreBacked(t, env.Root)

	if err := workingSetPull(env, []string{"selection"}, wsNow); err != nil {
		t.Fatalf("pull selection failed: %v", err)
	}
	got := readWorkSelection(t, env)
	for _, want := range []string{
		"Active release:** modernpath-v1-09 (active)",
		"Active release source:** USER:2026-08-04",
		"GATE-RELEASE-modernpath-v1-09",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in WORK-SELECTION.md:\n%s", want, got)
		}
	}
}

// (b) no answered release_selection gate resolves → source-missing; the
// invariant is not presented as satisfied (the preflight stops, exactly as it
// would for a fileless registry).
func TestStoreBackedReleaseReadReportsSourceMissing(t *testing.T) {
	fx := &wsFixture{
		workSelection: oneActiveSelection("modernpath-v1-09"),
		answeredGates: []any{}, // nothing carries the source
	}
	env := wsEnv(t, wsServe(t, fx))
	markStoreBacked(t, env.Root)

	if err := workingSetPull(env, []string{"selection"}, wsNow); err != nil {
		t.Fatalf("pull selection failed: %v", err)
	}
	got := readWorkSelection(t, env)
	if !strings.Contains(got, "Active release source:** MISSING") {
		t.Errorf("a store-backed read with no answered release_selection gate must report source-missing:\n%s", got)
	}
}

// (b′) an answered release_selection gate scoped to a DIFFERENT or closed release
// slug never satisfies the active slug's source — source-missing stands, never a
// wrong-slug false-green (cold-review CR-2). Both bindings are exercised: a gate
// whose external id names another slug, and a gate whose id matches but whose
// served exact_scope names another release.
func TestStoreBackedReleaseReadRejectsWrongSlugGate(t *testing.T) {
	contradictingScope := releaseSelectionGate("modernpath-v1-09", "USER:2026-01-01")
	contradictingScope["exact_scope"] = []any{"system:4", "release:modernpath-v1-08"}

	cases := map[string][]any{
		"different external id and slug":   {releaseSelectionGate("modernpath-v1-08", "USER:2026-01-01")},
		"matching id, contradicting scope": {contradictingScope},
	}
	for name, gates := range cases {
		t.Run(name, func(t *testing.T) {
			fx := &wsFixture{workSelection: oneActiveSelection("modernpath-v1-09"), answeredGates: gates}
			env := wsEnv(t, wsServe(t, fx))
			markStoreBacked(t, env.Root)

			if err := workingSetPull(env, []string{"selection"}, wsNow); err != nil {
				t.Fatalf("pull selection failed: %v", err)
			}
			got := readWorkSelection(t, env)
			if !strings.Contains(got, "Active release source:** MISSING") {
				t.Errorf("a gate for another slug must not satisfy the active slug's source:\n%s", got)
			}
			if strings.Contains(got, "USER:2026-01-01") {
				t.Errorf("the wrong-slug gate's source must not be reported:\n%s", got)
			}
		})
	}
}

// (c) zero or more than one open non-base release → NONE-ACTIVE / VIOLATION,
// never a false single, and no source line under the store-backed read.
func TestStoreBackedReleaseReadReportsNoneAndViolation(t *testing.T) {
	t.Run("none active", func(t *testing.T) {
		fx := &wsFixture{
			workSelection: map[string]any{
				"active_release": []any{}, "current": nil, "suspended": []any{}, "history": []any{},
			},
			// A source gate exists but there is no single active release to bind it to.
			answeredGates: []any{releaseSelectionGate("modernpath-v1-09", "USER:2026-08-04")},
		}
		env := wsEnv(t, wsServe(t, fx))
		markStoreBacked(t, env.Root)

		if err := workingSetPull(env, []string{"selection"}, wsNow); err != nil {
			t.Fatalf("pull selection failed: %v", err)
		}
		got := readWorkSelection(t, env)
		if !strings.Contains(got, "NONE ACTIVE") {
			t.Errorf("zero active releases must read NONE ACTIVE:\n%s", got)
		}
		if strings.Contains(got, "Active release source:**") {
			t.Errorf("no single active release — no source line may appear:\n%s", got)
		}
	})
	t.Run("violation", func(t *testing.T) {
		fx := &wsFixture{
			workSelection: map[string]any{
				"active_release": []any{
					map[string]any{"slug": "modernpath-v1-09", "status": "active"},
					map[string]any{"slug": "modernpath-v1-10", "status": "active"},
				},
				"current": nil, "suspended": []any{}, "history": []any{},
			},
		}
		env := wsEnv(t, wsServe(t, fx))
		markStoreBacked(t, env.Root)

		if err := workingSetPull(env, []string{"selection"}, wsNow); err != nil {
			t.Fatalf("pull selection failed: %v", err)
		}
		got := readWorkSelection(t, env)
		if !strings.Contains(got, "VIOLATION") {
			t.Errorf(">1 active release must read VIOLATION:\n%s", got)
		}
		if strings.Contains(got, "Active release source:**") {
			t.Errorf("more than one active release — no source line may appear:\n%s", got)
		}
	})
}

// A file-backed workspace (no marker) never reads gates for a source and renders
// exactly as before — the active-release line with no source line.
func TestFileBackedReleaseReadRendersNoSourceLine(t *testing.T) {
	fx := &wsFixture{
		workSelection: oneActiveSelection("modernpath-v1-09"),
		answeredGates: []any{releaseSelectionGate("modernpath-v1-09", "USER:2026-08-04")},
	}
	env := wsEnv(t, wsServe(t, fx))
	// no markStoreBacked — this is a file-backed workspace

	if err := workingSetPull(env, []string{"selection"}, wsNow); err != nil {
		t.Fatalf("pull selection failed: %v", err)
	}
	got := readWorkSelection(t, env)
	if !strings.Contains(got, "Active release:** modernpath-v1-09 (active)") {
		t.Errorf("the file-backed active-release line must be unchanged:\n%s", got)
	}
	if strings.Contains(got, "Active release source:**") {
		t.Errorf("file-backed: no source line may appear:\n%s", got)
	}
}

// --- PR-review P2 findings: authoritative gate + snapshot currency ---

// Finding 1 (a): a SUPERSEDED release_selection gate keeps its answer and
// answered_at but is never authoritative (PROCESS.md §Gates). state=all serves
// it, so the read must reject it — source-missing, not a false-green.
func TestStoreBackedReleaseReadRejectsSupersededGate(t *testing.T) {
	g := releaseSelectionGate("modernpath-v1-09", "USER:2026-08-04")
	g["state"] = "superseded"
	g["successor_external_id"] = "GATE-RELEASE-modernpath-v1-09-v2"
	fx := &wsFixture{
		workSelection:   oneActiveSelection("modernpath-v1-09"),
		supersededGates: []any{g},
	}
	env := wsEnv(t, wsServe(t, fx))
	markStoreBacked(t, env.Root)
	if err := workingSetPull(env, []string{"selection"}, wsNow); err != nil {
		t.Fatalf("pull selection failed: %v", err)
	}
	got := readWorkSelection(t, env)
	if !strings.Contains(got, "Active release source:** MISSING") {
		t.Errorf("a superseded release_selection gate must not be authoritative:\n%s", got)
	}
	if strings.Contains(got, "USER:2026-08-04") {
		t.Errorf("the superseded gate's source must not be reported:\n%s", got)
	}
}

// Finding 1 (b): an answered gate whose chosen option is non-approving
// (request_changes/defer) is not an approval; source-missing stands.
func TestStoreBackedReleaseReadRejectsNonApprovingGate(t *testing.T) {
	for _, opt := range []string{"request_changes", "defer"} {
		t.Run(opt, func(t *testing.T) {
			g := releaseSelectionGate("modernpath-v1-09", "USER:2026-08-04")
			g["chosen_option_keys"] = []any{opt}
			g["answer"] = "Not approved."
			fx := &wsFixture{
				workSelection: oneActiveSelection("modernpath-v1-09"),
				answeredGates: []any{g},
			}
			env := wsEnv(t, wsServe(t, fx))
			markStoreBacked(t, env.Root)
			if err := workingSetPull(env, []string{"selection"}, wsNow); err != nil {
				t.Fatalf("pull selection failed: %v", err)
			}
			got := readWorkSelection(t, env)
			if !strings.Contains(got, "Active release source:** MISSING") {
				t.Errorf("a %s (non-approving) gate must not be an authoritative source:\n%s", opt, got)
			}
		})
	}
}

// Finding 1 (c): the source_tag must be a USER: human answer; an agent/system
// tag does not satisfy the preflight's "USER: source" requirement.
func TestStoreBackedReleaseReadRejectsNonUserSource(t *testing.T) {
	fx := &wsFixture{
		workSelection: oneActiveSelection("modernpath-v1-09"),
		answeredGates: []any{releaseSelectionGate("modernpath-v1-09", "AGENT:backfill")},
	}
	env := wsEnv(t, wsServe(t, fx))
	markStoreBacked(t, env.Root)
	if err := workingSetPull(env, []string{"selection"}, wsNow); err != nil {
		t.Fatalf("pull selection failed: %v", err)
	}
	got := readWorkSelection(t, env)
	if !strings.Contains(got, "Active release source:** MISSING") {
		t.Errorf("a non-USER: source must not satisfy the preflight:\n%s", got)
	}
}

// Finding 2: the snapshot's source identity must depend on the release-source
// gate, so a gate-only approval (work-selection payload unchanged) makes the
// pulled snapshot stale and `check --refresh` folds the now-present source in.
func TestStoreBackedReleaseSourceGateDrivesSnapshotCurrency(t *testing.T) {
	fx := &wsFixture{
		workSelection: oneActiveSelection("modernpath-v1-09"),
		answeredGates: []any{}, // pulled before the release is approved
	}
	env := wsEnv(t, wsServe(t, fx))
	markStoreBacked(t, env.Root)
	if err := workingSetPull(env, []string{"selection"}, wsNow); err != nil {
		t.Fatalf("pull selection failed: %v", err)
	}
	if !strings.Contains(readWorkSelection(t, env), "Active release source:** MISSING") {
		t.Fatalf("precondition: a snapshot pulled before approval must be MISSING")
	}
	// Gate-only change: only the gate appears; the work-selection payload is identical.
	fx.answeredGates = []any{releaseSelectionGate("modernpath-v1-09", "USER:2026-08-26")}
	if err := workingSetCheck(env, false, wsNow); err == nil || !strings.Contains(err.Error(), selectionFile) {
		t.Fatalf("a gate-only approval must make %s stale, got %v", selectionFile, err)
	}
	if err := workingSetCheck(env, true, wsNow); err != nil {
		t.Fatalf("check --refresh failed: %v", err)
	}
	got := readWorkSelection(t, env)
	if !strings.Contains(got, "Active release source:** USER:2026-08-26") || strings.Contains(got, "MISSING") {
		t.Errorf("check --refresh must fold the approved gate into the snapshot:\n%s", got)
	}
}
