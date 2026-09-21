package cmd

// REQ-CROSS-313 (SR-CLI-0084), EPIC-CLI-008: `working-set pull --scope
// [--for-review]` resolves the current work selection and materializes the
// scope as a directory in the CLI-owned authoring render.
//
// RED first: `workingSetPullScope` does not exist.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func scopeSelection() map[string]any {
	return map[string]any{
		"current": map[string]any{
			"scope_kind":        "epic",
			"scope_external_id": "EPIC-CLI-008",
			"members":           []any{"REQ-CROSS-310", "REQ-CROSS-311"},
			"fingerprint":       "agg-fp",
			"phase":             "build",
			"recon_revision":    "54611fd52",
		},
		"active_release": []any{map[string]any{"slug": "modernpath-v1-09", "status": "active"}},
	}
}

func scopeFixture() *wsFixture {
	return &wsFixture{
		workSelection: scopeSelection(),
		epics: []any{map[string]any{
			"external_id": "EPIC-CLI-008", "title": "Process navigation brain",
			"description": "the loop's navigation half", "owner": "core",
			"outcome_source": "USER:2026-09-01", "process_status": "IN_PROGRESS",
			"fingerprint": "epic-served-fp",
		}},
		requirements: []any{
			map[string]any{"external_id": "REQ-CROSS-310", "title": "parity", "context": "CROSS",
				"work_status": "IN_REVIEW", "boundary": "the reads", "rationale": "round-trip",
				"verification_method": "unit", "fingerprint": "sr310-fp"},
			map[string]any{"external_id": "REQ-CROSS-311", "title": "patch", "context": "CROSS",
				"work_status": "IN_REVIEW", "fingerprint": "sr311-fp"},
		},
		packetSections: []any{map[string]any{
			"scope_kind": "epic", "scope_external_id": "EPIC-CLI-008",
			"section_key": "reconnaissance", "content": "# recon\nthe surface",
			"content_fingerprint": "ps-fp-1", "authoring_context_id": "ctx-1",
		}},
	}
}

func scopeDir(env *factoryEnv) string {
	return filepath.Join(env.Root, ".modernpath", "working-set", "EPIC-CLI-008")
}

func readScopeFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestREQCROSS313PullScopeMaterializesTheScopeDirectory(t *testing.T) {
	fx := scopeFixture()
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull --scope: %v", err)
	}
	dir := scopeDir(env)

	// the scope item, carrying its SERVED fingerprint in the header
	scope := readScopeFile(t, filepath.Join(dir, "EPIC-CLI-008.md"))
	if !strings.Contains(scope, "epic-served-fp") {
		t.Fatalf("scope file does not carry the served fingerprint:\n%s", scope)
	}

	// members materialized WITHOUT the caller naming them (resolved from selection)
	for _, m := range []string{"REQ-CROSS-310", "REQ-CROSS-311"} {
		if _, err := os.Stat(filepath.Join(dir, "members", m+".md")); err != nil {
			t.Fatalf("member %s not materialized: %v", m, err)
		}
	}

	// the selection projection
	if _, err := os.Stat(filepath.Join(dir, "SELECTION.md")); err != nil {
		t.Fatalf("SELECTION.md not written: %v", err)
	}

	// the packet section, mapped to its canonical file with content
	recon := readScopeFile(t, filepath.Join(dir, "packet", "10-recon.md"))
	if !strings.Contains(recon, "the surface") {
		t.Fatalf("recon packet file missing content:\n%s", recon)
	}

	// findings projection renders "unavailable" (the /sync/findings read is 404
	// until SR-315), and the pull does not fail
	findings := readScopeFile(t, filepath.Join(dir, "findings", "COLD-REVIEW.md"))
	if !strings.Contains(strings.ToLower(findings), "unavailable") {
		t.Fatalf("findings projection should be unavailable:\n%s", findings)
	}
}

func TestREQCROSS313ForReviewIsReadOnlyAndCarriesAReviewContextId(t *testing.T) {
	fx := scopeFixture()
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPullScope(env, true, wsNow); err != nil {
		t.Fatalf("pull --scope --for-review: %v", err)
	}
	dir := scopeDir(env)

	member := readScopeFile(t, filepath.Join(dir, "members", "REQ-CROSS-310.md"))
	// review render carries no editable field block — every block is read-only
	if strings.Contains(member, editableFieldMarker) {
		t.Fatalf("--for-review member carries an editable block:\n%s", member)
	}

	// the directory is stamped with a review-context id the push engine refuses
	ctx := readScopeFile(t, filepath.Join(dir, contextFile))
	if !strings.Contains(ctx, "review") {
		t.Fatalf("context stamp is not review mode:\n%s", ctx)
	}
	if !strings.Contains(ctx, "context_id:") {
		t.Fatalf("context stamp carries no context id:\n%s", ctx)
	}
}

func TestREQCROSS313OrdinaryPullStampsAnAuthoringContextId(t *testing.T) {
	fx := scopeFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatal(err)
	}
	ctx := readScopeFile(t, filepath.Join(scopeDir(env), contextFile))
	if !strings.Contains(ctx, "authoring") {
		t.Fatalf("ordinary pull should stamp authoring mode:\n%s", ctx)
	}
}

// #1 (external review): a served identifier is a filesystem path component only
// after it is proven a single safe component. A scope external id, a member id,
// or a section key carrying `../` or a separator must be refused before any
// join, never allowed to escape the working-set directory.
func TestPullScopeRefusesTraversingScopeId(t *testing.T) {
	fx := scopeFixture()
	sel := fx.workSelection["current"].(map[string]any)
	sel["scope_external_id"] = "../../pwned"
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPullScope(env, false, wsNow); err == nil {
		t.Fatal("pull --scope accepted a traversing scope id; expected a refusal")
	}
	// nothing was written outside the working-set directory
	escaped := filepath.Join(env.Root, ".modernpath", "pwned.md")
	if _, err := os.Stat(escaped); err == nil {
		t.Fatalf("a file escaped to %s", escaped)
	}
}

func TestPullScopeRefusesTraversingMemberId(t *testing.T) {
	fx := scopeFixture()
	sel := fx.workSelection["current"].(map[string]any)
	sel["members"] = []any{"../../../etc-pwned"}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPullScope(env, false, wsNow); err == nil {
		t.Fatal("pull --scope accepted a traversing member id; expected a refusal")
	}
}

// A real error from the packet-sections endpoint (auth, network, 5xx) must fail
// the pull, not be swallowed as "honest absence" — otherwise a reused scope dir
// keeps its previously-pulled packet files and stamps them into the new context.
func TestPullScopePropagatesPacketSectionServerError(t *testing.T) {
	fx := scopeFixture()
	fx.packetSectionsStatus = 500
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPullScope(env, false, wsNow); err == nil {
		t.Fatal("a 500 from packet-sections must fail the pull, not be swallowed as absence")
	}
}

// A 404 is a genuinely absent endpoint (an older server): honest absence, so the
// pull succeeds — but any packet files a prior pull left in the reused scope dir
// must be cleaned, never carried into the new context.
func TestPullScopeCleansStalePacketFilesWhenTheEndpointIsAbsent(t *testing.T) {
	fx := scopeFixture()
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("first pull: %v", err)
	}
	dir := scopeDir(env)
	if entries, _ := os.ReadDir(filepath.Join(dir, "packet")); len(entries) == 0 {
		t.Fatal("precondition: the first pull should have written packet files")
	}

	fx.packetSectionsStatus = 404
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("an absent (404) packet-sections endpoint is honest absence and must not fail the pull: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "packet")); len(entries) != 0 {
		t.Fatalf("stale packet files must be cleaned on an absent endpoint, found %d", len(entries))
	}
}

// REQ-CROSS-332 — an authoring pull scaffolds the canonical packet stubs the
// phase table requires and the store does not serve: the fixed three plus one
// enrichment per selected SR member the index knows as a system requirement. A
// stub never overwrites a local file; a UR member and an unknown member get
// none; --for-review and a 404 scaffold nothing. RED: pull writes only served
// sections today.

// scaffoldFixture is an epic scope with one SR member and one UR member, and the
// store serves no packet sections.
func scaffoldFixture() *wsFixture {
	sel := scopeSelection()
	sel["current"].(map[string]any)["members"] = []any{"REQ-CROSS-310", "UR-CLI-008"}
	return &wsFixture{
		workSelection: sel,
		epics: []any{map[string]any{
			"external_id": "EPIC-CLI-008", "title": "Process navigation brain",
			"fingerprint": "epic-served-fp",
		}},
		requirements: []any{map[string]any{
			"external_id": "REQ-CROSS-310", "title": "sr", "context": "CROSS",
			"work_status": "IN_REVIEW", "fingerprint": "sr310-fp",
		}},
		userRequirements: []any{wsUserReq("UR-CLI-008", "the user outcome")},
		packetSections:   nil,
	}
}

func packetMdCount(t *testing.T, dir string) int {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Join(dir, "packet"))
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			n++
		}
	}
	return n
}

func TestREQCROSS332PullScaffoldsTheRequiredStubsWhenTheStoreServesNone(t *testing.T) {
	fx := scaffoldFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull: %v", err)
	}
	dir := scopeDir(env)
	want := []string{"10-recon.md", "30-red-strategy.md", "40-decisions.md", "20-enrichment-REQ-CROSS-310.md"}
	for _, f := range want {
		content := readScopeFile(t, filepath.Join(dir, "packet", f))
		if !unfilledPacketSection(content, sectionKeyFromFile(f), "epic", "EPIC-CLI-008") {
			t.Fatalf("%s must be scaffolded as an unfilled stub, got:\n%s", f, content)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "packet", "20-enrichment-UR-CLI-008.md")); err == nil {
		t.Fatal("a user-requirement member must get no enrichment stub")
	}
	if got := packetMdCount(t, dir); got != len(want) {
		t.Fatalf("packet/ must hold exactly %d stubs, found %d", len(want), got)
	}
}

func TestREQCROSS332PullScaffoldsOnlyTheMissingKeysBesideServedSections(t *testing.T) {
	fx := scopeFixture() // serves reconnaissance; members 310 and 311 are both SRs
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull: %v", err)
	}
	dir := scopeDir(env)
	recon := readScopeFile(t, filepath.Join(dir, "packet", "10-recon.md"))
	if !strings.Contains(recon, "the surface") {
		t.Fatalf("a served section must be written whole, got:\n%s", recon)
	}
	if unfilledPacketSection(recon, "reconnaissance", "epic", "EPIC-CLI-008") {
		t.Fatal("a served section must not be scaffolded over with a stub")
	}
	for _, f := range []string{"30-red-strategy.md", "40-decisions.md", "20-enrichment-REQ-CROSS-310.md", "20-enrichment-REQ-CROSS-311.md"} {
		content := readScopeFile(t, filepath.Join(dir, "packet", f))
		if !unfilledPacketSection(content, sectionKeyFromFile(f), "epic", "EPIC-CLI-008") {
			t.Fatalf("%s must be a stub, got:\n%s", f, content)
		}
	}
}

func TestREQCROSS332PullScaffoldsTheScopeSRForASingleSRScope(t *testing.T) {
	sel := scopeSelection()
	cur := sel["current"].(map[string]any)
	cur["scope_kind"] = "single_sr"
	cur["scope_external_id"] = "REQ-CROSS-310"
	cur["members"] = []any{}
	fx := &wsFixture{
		workSelection: sel,
		requirements: []any{map[string]any{
			"external_id": "REQ-CROSS-310", "title": "sr", "context": "CROSS",
			"work_status": "IN_REVIEW", "fingerprint": "sr310-fp",
		}},
		packetSections: nil,
	}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull: %v", err)
	}
	dir := filepath.Join(env.Root, ".modernpath", "working-set", "REQ-CROSS-310")
	for _, f := range []string{"10-recon.md", "30-red-strategy.md", "40-decisions.md", "20-enrichment-REQ-CROSS-310.md"} {
		if _, err := os.Stat(filepath.Join(dir, "packet", f)); err != nil {
			t.Fatalf("a single_sr scope must scaffold %s: %v", f, err)
		}
	}
}

func TestREQCROSS332PullGivesAnUnknownMemberNoStub(t *testing.T) {
	sel := scopeSelection()
	sel["current"].(map[string]any)["members"] = []any{"REQ-CROSS-310", "REQ-CROSS-999"}
	fx := &wsFixture{
		workSelection: sel,
		epics:         []any{map[string]any{"external_id": "EPIC-CLI-008", "title": "e", "fingerprint": "epic-served-fp"}},
		requirements: []any{map[string]any{
			"external_id": "REQ-CROSS-310", "title": "sr", "context": "CROSS",
			"work_status": "IN_REVIEW", "fingerprint": "sr310-fp",
		}},
		packetSections: nil,
	}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull: %v", err)
	}
	dir := scopeDir(env)
	if _, err := os.Stat(filepath.Join(dir, "packet", "20-enrichment-REQ-CROSS-310.md")); err != nil {
		t.Fatalf("the known SR member must get a stub: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "packet", "20-enrichment-REQ-CROSS-999.md")); err == nil {
		t.Fatal("an unknown member must get no enrichment stub (the store never requires it)")
	}
}

func TestREQCROSS332PullNeverOverwritesALocalFileForAnUnservedKey(t *testing.T) {
	fx := scopeFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("first pull: %v", err)
	}
	dir := scopeDir(env)
	filled := "the real red strategy the author wrote\n"
	if err := os.WriteFile(filepath.Join(dir, "packet", "30-red-strategy.md"), []byte(filled), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("second pull: %v", err)
	}
	if got := readScopeFile(t, filepath.Join(dir, "packet", "30-red-strategy.md")); got != filled {
		t.Fatalf("a local file for an unserved key must be left byte-identical, got:\n%s", got)
	}
}

func TestREQCROSS332ForReviewPullScaffoldsNothing(t *testing.T) {
	fx := scaffoldFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, true, wsNow); err != nil {
		t.Fatalf("for-review pull: %v", err)
	}
	if got := packetMdCount(t, scopeDir(env)); got != 0 {
		t.Fatalf("--for-review must scaffold no stubs, found %d", got)
	}
}

func TestREQCROSS332AnAbsentEndpointScaffoldsNothing(t *testing.T) {
	fx := scaffoldFixture()
	fx.packetSectionsStatus = 404
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if got := packetMdCount(t, scopeDir(env)); got != 0 {
		t.Fatalf("a 404 endpoint must scaffold nothing, found %d stub(s)", got)
	}
}

// Guard (green once REQ-CROSS-331 is in): a pull immediately followed by a push
// posts nothing — the scaffolded stubs are all unfilled, and push skips them.
func TestREQCROSS332PullThenPushPostsNothing(t *testing.T) {
	fx := scaffoldFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull: %v", err)
	}
	fx.authorPosts = nil
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("push: %v", err)
	}
	if len(fx.authorPosts) != 0 {
		t.Fatalf("a pull then a push with no edits must post nothing, got %v", fx.authorPosts)
	}
}
