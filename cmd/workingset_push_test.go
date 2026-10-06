package cmd

// REQ-CROSS-314 (SR-CLI-0085), EPIC-CLI-008: `working-set push [--dry-run]`
// parses each scope-directory item file in the CLI's own grammar (SR-CLI-0084),
// diffs it against the pulled snapshot, assembles ONE `author patch` per changed
// record under the fingerprint recorded at pull, prints the op plan, and refuses
// — naming the file — anything it cannot type (a projection edit, a relation
// removed by deleting its line, an empty clear, an unknown id). A 409 on one
// record leaves that file unpushed and reports every other file independently.
//
// RED first: `workingSetPush` and the `internal/authoring/diff` engine do not
// exist.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// A requirement payload carrying explicit relations, for the base of a diff.
func reqWith(id, boundary string, rels []any) map[string]any {
	m := map[string]any{
		"external_id": id, "title": "t", "context": "CROSS",
		"work_status": "IN_REVIEW", "boundary": boundary, "fingerprint": id + "-fp",
	}
	if rels != nil {
		m["relations"] = rels
	}
	return m
}

func canonicalScopeRequirement(fx *wsFixture, boundary, fingerprint string) map[string]any {
	original, ok := fx.requirements[0].(map[string]any)
	if !ok {
		panic("scope fixture requirement is not an object")
	}
	canonical := make(map[string]any, len(original))
	for key, value := range original {
		canonical[key] = value
	}
	canonical["boundary"] = boundary
	canonical["fingerprint"] = fingerprint
	return canonical
}

func rel(direction, target, authority string) any {
	return map[string]any{"direction": direction, "target": target, "authority": authority}
}

// pull the scope, then hand the caller each member file's path to edit.
func pulledScope(t *testing.T, fx *wsFixture) (*factoryEnv, string) {
	t.Helper()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull --scope: %v", err)
	}
	return env, scopeDir(env)
}

func memberPath(dir, id string) string { return filepath.Join(dir, "members", id+".md") }

func edit(t *testing.T, path string, fn func(string) string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(fn(string(b))), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func scopedBaselineEntryForTest(t *testing.T, dir, rel string) scopedDraftBaselineEntry {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, scopedDraftBaselineFile))
	if err != nil {
		t.Fatalf("read local draft baseline: %v", err)
	}
	var baseline scopedDraftBaseline
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatalf("decode local draft baseline: %v", err)
	}
	entry, ok := baseline.Files[filepath.ToSlash(rel)]
	if !ok {
		t.Fatalf("baseline has no managed entry for %s", rel)
	}
	return entry
}

func assertScopedBaselineMatchesFile(t *testing.T, dir, rel string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	entry := scopedBaselineEntryForTest(t, dir, rel)
	if entry.SHA256 != sha256Hex(content) {
		t.Fatalf("baseline hash for %s does not match the unchanged local bytes", rel)
	}
}

func patchPostFor(fx *wsFixture, ext string) map[string]any {
	for _, p := range fx.authorPosts {
		if p["action"] == "patch" {
			if rec, ok := p["record"].(map[string]any); ok && str(rec, "external_id") == ext {
				return rec
			}
		}
	}
	return nil
}

func TestREQCROSS314PushEmitsOnePatchWithEveryOpUnderThePulledFingerprint(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)

	// edit member REQ-CROSS-310: change a scalar and add a relation (acceptance
	// content is read-only — a scenario edit is refused, covered separately)
	edit(t, memberPath(dir, "REQ-CROSS-310"), func(s string) string {
		s = strings.Replace(s, "the reads", "the reads AND writes", 1)
		return s +
			"\n## relations\n```authoring:relations\n- declares UR-CLI-008 [candidate]\n```\n"
	})

	fx.authorPosts = nil
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("push: %v", err)
	}

	// exactly one patch POST, for REQ-CROSS-310, under the pulled fingerprint
	var patches int
	for _, p := range fx.authorPosts {
		if p["action"] == "patch" {
			patches++
		}
	}
	if patches != 1 {
		t.Fatalf("want exactly 1 patch POST, got %d (%v)", patches, fx.authorPosts)
	}
	rec := patchPostFor(fx, "REQ-CROSS-310")
	if rec == nil {
		t.Fatal("no patch for REQ-CROSS-310")
	}
	if str(rec, "expected_fingerprint") != "sr310-fp" {
		t.Fatalf("patch expected_fingerprint = %q, want the pulled sr310-fp", str(rec, "expected_fingerprint"))
	}
	if str(rec, "boundary") != "the reads AND writes" {
		t.Fatalf("patch did not carry the edited boundary: %v", rec["boundary"])
	}
	rels, _ := rec["relations"].([]any)
	if len(rels) != 1 {
		t.Fatalf("want one relation op, got %v", rec["relations"])
	}
	rop, _ := rels[0].(map[string]any)
	if str(rop, "mode") != "declare" {
		t.Fatalf("relation op mode = %q, want declare", str(rop, "mode"))
	}
	// carries the authoring-context id from the pulled directory
	if !strings.HasPrefix(str(rec, "authoring_context_id"), "authoring-") {
		t.Fatalf("patch missing the authoring context id: %v", rec["authoring_context_id"])
	}
}

func TestPushRefreshConsumesCanonicalMutationResponseWithoutAnotherItemRead(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	edit(t, memberPath(dir, "REQ-CROSS-310"), func(s string) string {
		return strings.Replace(s, "the reads", "the reads and writes", 1)
	})

	canonical := reqWith("REQ-CROSS-310", "the reads and writes", nil)
	canonical["fingerprint"] = "canonical-after-patch"
	fx.authorSyncItems = map[string]any{
		"REQ-CROSS-310": map[string]any{
			"kind": "system", "item": canonical, "gates": []any{},
		},
	}
	readsBeforePush := fx.itemsHits

	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("push: %v", err)
	}
	if fx.itemsHits != readsBeforePush+1 {
		t.Fatalf("single-record push should make only its initial exact read and consume the canonical mutation response; direct item reads=%d before push, %d after", readsBeforePush, fx.itemsHits)
	}
	refreshed := readScopeFile(t, memberPath(dir, "REQ-CROSS-310"))
	if !strings.Contains(refreshed, "canonical-after-patch") || !strings.Contains(refreshed, "the reads and writes") {
		t.Fatalf("push did not refresh from the canonical mutation response:\n%s", refreshed)
	}
}

func TestREQCROSS314DryRunPrintsThePlanAndPostsNothing(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	edit(t, memberPath(dir, "REQ-CROSS-310"), func(s string) string {
		return strings.Replace(s, "the reads", "the reads AND writes", 1)
	})

	fx.authorPosts = nil
	if err := workingSetPush(env, true); err != nil {
		t.Fatalf("push --dry-run: %v", err)
	}
	if len(fx.authorPosts) != 0 {
		t.Fatalf("--dry-run posted %d requests, want 0", len(fx.authorPosts))
	}
}

func TestREQCROSS314AProjectionEditIsRefusedNamingTheFile(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	// the status block is a read-only projection; editing it is refused
	edit(t, memberPath(dir, "REQ-CROSS-310"), func(s string) string {
		return strings.Replace(s, "IN_REVIEW", "DONE", 1)
	})

	fx.authorPosts = nil
	err := workingSetPush(env, false)
	if err == nil {
		t.Fatal("push accepted an edit to a read-only projection block")
	}
	if !strings.Contains(err.Error(), "REQ-CROSS-310") {
		t.Fatalf("refusal does not name the file: %v", err)
	}
	if len(fx.authorPosts) != 0 {
		t.Fatalf("a refused file still posted: %v", fx.authorPosts)
	}
}

func TestREQCROSS314ADeletedRelationLineIsRefused(t *testing.T) {
	fx := scopeFixture()
	fx.requirements = []any{reqWith("REQ-CROSS-310", "the reads", []any{rel("declares", "UR-CLI-008", "confirmed")})}
	fx.workSelection["current"].(map[string]any)["members"] = []any{"REQ-CROSS-310"}
	env, dir := pulledScope(t, fx)

	// delete the relations block entirely (drop the line without a withdraw marker)
	edit(t, memberPath(dir, "REQ-CROSS-310"), func(s string) string {
		i := strings.Index(s, "## relations")
		if i < 0 {
			t.Fatalf("pulled file has no relations block:\n%s", s)
		}
		return s[:i]
	})

	fx.authorPosts = nil
	err := workingSetPush(env, false)
	if err == nil {
		t.Fatal("push accepted a relation removed by line-deletion")
	}
	if !strings.Contains(err.Error(), "UR-CLI-008") || !strings.Contains(strings.ToLower(err.Error()), "withdraw") {
		t.Fatalf("refusal should name the dropped edge and point at withdraw: %v", err)
	}
	if len(fx.authorPosts) != 0 {
		t.Fatalf("a refused file still posted: %v", fx.authorPosts)
	}
}

func TestREQCROSS314AWithdrawMarkerProducesAWithdrawOp(t *testing.T) {
	fx := scopeFixture()
	fx.requirements = []any{reqWith("REQ-CROSS-310", "the reads", []any{rel("declares", "UR-CLI-008", "confirmed")})}
	fx.workSelection["current"].(map[string]any)["members"] = []any{"REQ-CROSS-310"}
	env, dir := pulledScope(t, fx)

	// replace the edge line with an explicit withdraw marker
	edit(t, memberPath(dir, "REQ-CROSS-310"), func(s string) string {
		return strings.Replace(s, "- declares UR-CLI-008 [confirmed]", "- withdraw UR-CLI-008", 1)
	})

	fx.authorPosts = nil
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("push: %v", err)
	}
	rec := patchPostFor(fx, "REQ-CROSS-310")
	if rec == nil {
		t.Fatal("no patch for REQ-CROSS-310")
	}
	rels, _ := rec["relations"].([]any)
	if len(rels) != 1 {
		t.Fatalf("want one relation op, got %v", rec["relations"])
	}
	rop, _ := rels[0].(map[string]any)
	if str(rop, "mode") != "withdraw" {
		t.Fatalf("relation op mode = %q, want withdraw", str(rop, "mode"))
	}
}

func TestREQCROSS314AnUnknownMemberFileIsRefusedWithTheCreateHint(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)

	// a member file the store does not know
	unknown := "# REQ-CROSS-999 — working-set (authoring)\n\n- **Served fingerprint:** none\n- **Context:** authoring:authoring-x\n\n## title\n```authoring:editable\nghost\n```\n"
	if err := os.WriteFile(memberPath(dir, "REQ-CROSS-999"), []byte(unknown), 0o644); err != nil {
		t.Fatal(err)
	}

	fx.authorPosts = nil
	err := workingSetPush(env, false)
	if err == nil {
		t.Fatal("push accepted a member file whose id the store does not know")
	}
	if !strings.Contains(err.Error(), "REQ-CROSS-999") || !strings.Contains(err.Error(), "author apply") {
		t.Fatalf("refusal should name the file and point at author apply: %v", err)
	}
	if len(fx.authorPosts) != 0 {
		t.Fatalf("the refusal comes before any write, posted %v", fx.authorPosts)
	}
}

// unknownToTheStore is a member file for an id the store does not know. It
// states its kind, so nothing but the rule itself keeps push from creating it.
const unknownToTheStore = "# REQ-CROSS-999 — working-set (authoring)\n\n- **Kind:** system\n- **Served fingerprint:** none\n- **Context:** authoring:authoring-x\n\n" +
	"## title\n```authoring:editable\nnot in the store\n```\n\n" +
	"## context\n```authoring:editable\nCROSS\n```\n\n" +
	"## boundary\n```authoring:editable\nthe CLI\n```\n"

// declareMember adds id to the scope file's members block.
func declareMember(t *testing.T, dir, id string) {
	t.Helper()
	edit(t, filepath.Join(dir, "EPIC-CLI-008.md"), func(s string) string {
		out := strings.Replace(s, "- REQ-CROSS-311\n```", "- REQ-CROSS-311\n- "+id+"\n```", 1)
		if out == s {
			t.Fatalf("the scope file has no members block to declare %s in:\n%s", id, s)
		}
		return out
	})
}

// REQ-CROSS-442 (owner decision USER:2026-09-28, option A): push never
// creates a record; `author apply` is the only create path. A file for an id
// the store does not know is refused before any write, whether the frozen
// selection holds it, the scope file declares it in the same push, or
// neither — and the refusal names `author apply`.
func TestREQCROSS442PushRefusesAnUnknownIDBeforeAnyWrite(t *testing.T) {
	cases := map[string]func(fx *wsFixture, dir string){
		"outside the selection": func(*wsFixture, string) {},
		"declared in the scope file in the same push": func(_ *wsFixture, dir string) {
			declareMember(t, dir, "REQ-CROSS-999")
		},
		"a member of the frozen selection": func(fx *wsFixture, _ string) {
			fx.workSelection["current"].(map[string]any)["members"] = []any{"REQ-CROSS-310", "REQ-CROSS-311", "REQ-CROSS-999"}
			// The scope's served membership is what the pulled scope file shows.
			fx.epics[0].(map[string]any)["requirement_external_ids"] = []any{"REQ-CROSS-310", "REQ-CROSS-311"}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			fx := scopeFixture()
			env, dir := pulledScope(t, fx)
			edit(t, memberPath(dir, "REQ-CROSS-310"), func(s string) string {
				return strings.Replace(s, "the reads", "changed 310", 1)
			})
			if err := os.WriteFile(memberPath(dir, "REQ-CROSS-999"), []byte(unknownToTheStore), 0o644); err != nil {
				t.Fatal(err)
			}
			setup(fx, dir)

			fx.authorPosts = nil
			err := workingSetPush(env, false)
			if err == nil {
				t.Fatal("push accepted a file for an id the store does not know")
			}
			if len(fx.authorPosts) != 0 {
				t.Fatalf("the refusal comes before any write, posted %v", fx.authorPosts)
			}
			for _, want := range []string{"REQ-CROSS-999", "author apply"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal names the id and the create path; %q is missing: %v", want, err)
				}
			}
		})
	}
}

// A members/ file whose record the store knows, but the frozen selection
// does not hold, is reported as skipped. The scope's served membership holds
// it, so the advice must not say it is not a member: it is not in the frozen
// selection, and re-selecting is what lets push send it.
func TestREQCROSS442PushSkipsAServedMemberOutsideTheFrozenSelection(t *testing.T) {
	fx := scopeFixture()
	fx.epics[0].(map[string]any)["requirement_external_ids"] = []any{"REQ-CROSS-310", "REQ-CROSS-311", "REQ-CROSS-999"}
	fx.requirements = append(fx.requirements, map[string]any{"external_id": "REQ-CROSS-999", "title": "a served member",
		"context": "CROSS", "boundary": "the CLI", "work_status": "PROPOSED", "fingerprint": "sr999-fp"})
	env, dir := pulledScope(t, fx)
	file := "# REQ-CROSS-999 — working-set (authoring)\n\n- **Served fingerprint:** sr999-fp\n- **Context:** authoring:authoring-x\n\n" +
		"## title\n```authoring:editable\na served member\n```\n\n" +
		"## boundary\n```authoring:editable\nthe CLI, edited\n```\n"
	if err := os.WriteFile(memberPath(dir, "REQ-CROSS-999"), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}

	fx.authorPosts = nil
	var pushErr error
	out := captureOut(t, func() { pushErr = workingSetPush(env, false) })
	if pushErr != nil {
		t.Fatalf("push: %v\n%s", pushErr, out)
	}
	if len(fx.authorPosts) != 0 {
		t.Errorf("a file outside the frozen selection is not pushed, posted %v", fx.authorPosts)
	}
	for _, want := range []string{"REQ-CROSS-999", "skipped", "frozen selection", "re-select"} {
		if !strings.Contains(out, want) {
			t.Errorf("the skip line misses %q, got:\n%s", want, out)
		}
	}
	for _, wrong := range []string{"make it a member", "not a member"} {
		if strings.Contains(out, wrong) {
			t.Errorf("the scope's served membership holds REQ-CROSS-999; the advice must not say %q:\n%s", wrong, out)
		}
	}
}

// REQ-CROSS-442 (revised): lane_class is one of the SR's mutable fields, so a
// pulled SR shows it and push can change it.
func TestREQCROSS442PulledSRShowsAndPushesLaneClass(t *testing.T) {
	fx := scopeFixture()
	fx.requirements[0].(map[string]any)["lane_class"] = "wording"
	env, dir := pulledScope(t, fx)

	file := readScopeFile(t, memberPath(dir, "REQ-CROSS-310"))
	if !strings.Contains(file, "## lane_class\n```authoring:editable\nwording\n```") {
		t.Fatalf("the pulled SR does not show its lane class as an editable field:\n%s", file)
	}
	edit(t, memberPath(dir, "REQ-CROSS-310"), func(s string) string {
		return strings.Replace(s, "```authoring:editable\nwording\n```", "```authoring:editable\npresentation\n```", 1)
	})
	fx.authorPosts = nil
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("push: %v", err)
	}
	if rec := patchPostFor(fx, "REQ-CROSS-310"); str(rec, "lane_class") != "presentation" {
		t.Fatalf("the patch must carry the edited lane class, posted %v", rec)
	}
}

func TestREQCROSS314AConflictOnOneFileLeavesTheOtherSucceeding(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	fx.authorConflict = map[string]bool{"REQ-CROSS-310": true}

	edit(t, memberPath(dir, "REQ-CROSS-310"), func(s string) string {
		return strings.Replace(s, "the reads", "changed 310", 1)
	})
	edit(t, memberPath(dir, "REQ-CROSS-311"), func(s string) string {
		return strings.Replace(s, "```authoring:editable\npatch\n```", "```authoring:editable\npatch edited\n```", 1)
	})

	fx.authorPosts = nil
	err := workingSetPush(env, false)
	if err == nil {
		t.Fatal("push should report the per-file 409")
	}
	if !strings.Contains(err.Error(), "REQ-CROSS-310") {
		t.Fatalf("the conflict should name REQ-CROSS-310: %v", err)
	}
	// both files were attempted independently: 311 still posted its patch
	if patchPostFor(fx, "REQ-CROSS-311") == nil {
		t.Fatalf("REQ-CROSS-311 was not pushed despite 310 conflicting: %v", fx.authorPosts)
	}
}

// Acceptance criteria are served structured ({given,when,then,statement}); the
// pulled file must render the real GWT (they rendered blank — invisible to author
// and cold reviewer), and editing one must be REFUSED rather than pushing a
// statement-only, synthetic-id replace-set that supersedes the real criteria (#1).
func TestREQCROSS313AcceptanceScenariosRenderTheServedGWT(t *testing.T) {
	fx := scopeFixture()
	fx.requirements[0].(map[string]any)["criteria"] = []any{
		map[string]any{"external_id": "REQ-CROSS-310#AC1", "kind": "acceptance",
			"given": "a pulled scope", "when": "a member has criteria", "then": "they render", "statement": ""},
	}
	_, dir := pulledScope(t, fx)

	s := readScopeFile(t, memberPath(dir, "REQ-CROSS-310"))
	if !strings.Contains(s, "a pulled scope") || !strings.Contains(s, "they render") {
		t.Fatalf("pulled file does not render the served acceptance GWT (rendered blank):\n%s", s)
	}
}

func TestREQCROSS314EditingAnAcceptanceScenarioIsRefused(t *testing.T) {
	fx := scopeFixture()
	fx.requirements[0].(map[string]any)["criteria"] = []any{
		map[string]any{"external_id": "REQ-CROSS-310#AC1", "kind": "acceptance",
			"given": "a scope", "when": "edited", "then": "it refuses", "statement": ""},
	}
	env, dir := pulledScope(t, fx)

	// add a scenario bullet — a change to acceptance content
	edit(t, memberPath(dir, "REQ-CROSS-310"), func(s string) string {
		return strings.Replace(s, "```authoring:scenarios\n", "```authoring:scenarios\n- GIVEN an added WHEN pushed THEN refused\n", 1)
	})

	fx.authorPosts = nil
	err := workingSetPush(env, false)
	if err == nil {
		t.Fatal("push accepted an acceptance-scenario edit (a wiping statement-only replace-set)")
	}
	if !strings.Contains(err.Error(), "REQ-CROSS-310") || !strings.Contains(strings.ToLower(err.Error()), "acceptance") {
		t.Fatalf("refusal should name the file and acceptance content: %v", err)
	}
	if len(fx.authorPosts) != 0 {
		t.Fatalf("a refused acceptance edit still posted: %v", fx.authorPosts)
	}
}

func TestREQCROSS314PushAcceptsReorderedServerCriteria(t *testing.T) {
	fx := scopeFixture()
	req := fx.requirements[0].(map[string]any)
	first := map[string]any{"external_id": "C-1", "kind": "criterion", "statement": "first criterion"}
	second := map[string]any{"external_id": "C-2", "kind": "criterion", "statement": "second criterion"}
	req["criteria"] = []any{first, second}
	env, dir := pulledScope(t, fx)

	req["criteria"] = []any{second, first}
	edit(t, memberPath(dir, "REQ-CROSS-310"), func(s string) string {
		return strings.Replace(s, "the reads", "the reads AND writes", 1)
	})
	fx.authorPosts = nil
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("server criterion order must not prevent an unrelated edit: %v", err)
	}
	patch := patchPostFor(fx, "REQ-CROSS-310")
	if patch == nil || str(patch, "boundary") != "the reads AND writes" {
		t.Fatalf("expected the intended boundary edit: %v", fx.authorPosts)
	}
	if _, exists := patch["criteria"]; exists {
		t.Fatalf("push must not replace acceptance content: %v", patch)
	}
}

// source_citations are served as {kind,ref} maps; the pulled file must render
// them (they were dropped, invisible to author and cold reviewer), and on push a
// citation must ride back as the map shape the {:array,:map} column accepts — not
// a bare string that 422s (#5).
func TestREQCROSS313SourceCitationsRenderTheServedRefs(t *testing.T) {
	fx := scopeFixture()
	fx.requirements[0].(map[string]any)["source_citations"] = []any{
		map[string]any{"kind": "user", "ref": "USER:2026-09-02"},
		map[string]any{"kind": "code", "ref": "core/author.ex:1"},
	}
	_, dir := pulledScope(t, fx)

	s := readScopeFile(t, memberPath(dir, "REQ-CROSS-310"))
	if !strings.Contains(s, "USER:2026-09-02") || !strings.Contains(s, "core/author.ex:1") {
		t.Fatalf("pulled file does not render the served source citations:\n%s", s)
	}
}

func TestREQCROSS314AddingACitationPushesTheMapShape(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)

	// the author adds a source citation to a member that had none
	edit(t, memberPath(dir, "REQ-CROSS-310"), func(s string) string {
		return s + "\n## source_citations\n```authoring:citations\n- USER:2026-09-02:approved\n```\n"
	})

	fx.authorPosts = nil
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("push: %v", err)
	}
	rec := patchPostFor(fx, "REQ-CROSS-310")
	if rec == nil {
		t.Fatal("no patch for REQ-CROSS-310")
	}
	cites, _ := rec["source_citations"].([]any)
	if len(cites) != 1 {
		t.Fatalf("want one source citation op, got %v", rec["source_citations"])
	}
	m, ok := cites[0].(map[string]any)
	if !ok {
		t.Fatalf("a citation must be sent as a {kind,ref} map, got %T: %v", cites[0], cites[0])
	}
	if str(m, "ref") != "USER:2026-09-02:approved" || str(m, "kind") != "user" {
		t.Fatalf("citation map shape wrong: %v", m)
	}
}

// A packet section's whole-blob CAS must use the fingerprint recorded at PULL,
// not a fingerprint re-read at push time — otherwise a concurrent writer's change
// is silently overwritten instead of surfacing a 409 (#6). Item files already
// honour this via their served-fingerprint header.
func TestREQCROSS314APacketSectionPushCarriesThePullTimeFingerprint(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)

	// a concurrent writer changes the section on the server AFTER this pull
	fx.packetSections = []any{map[string]any{
		"scope_kind": "epic", "scope_external_id": "EPIC-CLI-008",
		"section_key": "reconnaissance", "content": "# recon\nchanged by someone else",
		"content_fingerprint": "ps-fp-2", "authoring_context_id": "ctx-2",
	}}

	// the author edits their local copy of the section
	edit(t, filepath.Join(dir, "packet", "10-recon.md"), func(s string) string { return s + "\nmy local edit" })

	fx.authorPosts = nil
	_ = workingSetPush(env, false)

	var rec map[string]any
	for _, p := range fx.authorPosts {
		r, _ := p["record"].(map[string]any)
		if str(r, "kind") == "packet_section" && str(r, "section_key") == "reconnaissance" {
			rec = r
		}
	}
	if rec == nil {
		t.Fatalf("no packet_section push captured: %v", fx.authorPosts)
	}
	if got := str(rec, "expected_fingerprint"); got != "ps-fp-1" {
		t.Fatalf("packet CAS used %q, want the pull-time ps-fp-1 (a concurrent change must 409, not be overwritten)", got)
	}
}

// The push must validate and plan EVERY item and packet section before issuing
// any POST: a valid earlier file must not land its mutation only for a malformed
// later file to abort the command afterwards, leaving a partial write and a plan
// printed after the fact.
func TestREQCROSS314AMalformedLaterFileAbortsBeforeAnyPost(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)

	// a valid edit to the earlier member (would produce a real patch)...
	edit(t, memberPath(dir, "REQ-CROSS-310"), func(s string) string {
		return strings.Replace(s, "the reads", "the reads AND writes", 1)
	})
	// ...and a later member file with free text the closed grammar cannot parse.
	edit(t, memberPath(dir, "REQ-CROSS-311"), func(s string) string {
		return s + "\nstray free text outside any block\n"
	})

	fx.authorPosts = nil
	err := workingSetPush(env, false)
	if err == nil {
		t.Fatal("a malformed later file must abort the push")
	}
	if len(fx.authorPosts) != 0 {
		t.Fatalf("a malformed later file must abort BEFORE any POST, but %d posted: %v", len(fx.authorPosts), fx.authorPosts)
	}
}

func TestREQCROSS314AnUnchangedFilePostsNothing(t *testing.T) {
	fx := scopeFixture()
	env, _ := pulledScope(t, fx)

	fx.authorPosts = nil
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("push of an unedited scope: %v", err)
	}
	if len(fx.authorPosts) != 0 {
		t.Fatalf("an unchanged scope posted %d requests, want 0", len(fx.authorPosts))
	}
}

// REQ-CROSS-331 — `working-set push` treats a blank packet file, or one that is
// still the unfilled stub `working-set pull` scaffolds, as not authored: it is
// skipped and named in the plan, never sent as a whole-blob put, and never used
// to empty a served section. A filled stub is sent with the marker line removed
// and the rest byte-identical; the unchanged comparison runs on that stripped
// body. RED: today a blank/stub file posts an empty whole-blob put.

func writePacketFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "packet", name), []byte(content), 0o644); err != nil {
		t.Fatalf("write packet %s: %v", name, err)
	}
}

func packetPost(fx *wsFixture, key string) (string, map[string]any) {
	for _, p := range fx.authorPosts {
		r, _ := p["record"].(map[string]any)
		if str(r, "kind") == "packet_section" && str(r, "section_key") == key {
			return str(p, "action"), r
		}
	}
	return "", nil
}

func packetPostFor(fx *wsFixture, key string) map[string]any {
	_, rec := packetPost(fx, key)
	return rec
}

func TestREQCROSS331PushSkipsABlankPacketFileNamingIt(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	writePacketFile(t, dir, "30-red-strategy.md", "")

	fx.authorPosts = nil
	var pushErr error
	out := captureOut(t, func() { pushErr = workingSetPush(env, false) })
	if pushErr != nil {
		t.Fatalf("push: %v", pushErr)
	}
	if packetPostFor(fx, "red_strategy") != nil {
		t.Fatalf("a blank packet file must not be pushed: %v", fx.authorPosts)
	}
	if !strings.Contains(out, "packet section red_strategy — unfilled stub, not pushed (30-red-strategy.md)") {
		t.Fatalf("the plan must name the skipped unfilled file, got:\n%s", out)
	}
	// With no fillable file the summary says nothing to push, N skipped — not
	// "every file is unchanged". (The count also covers the stubs `pull`
	// scaffolds for the other required sections, REQ-CROSS-332.)
	if !strings.Contains(out, "nothing to push") || !strings.Contains(out, "unfilled packet stub(s) skipped") {
		t.Fatalf("with only unfilled files the summary must say nothing to push, N skipped, got:\n%s", out)
	}
}

func TestREQCROSS331PushSkipsAnUnfilledStubUnderBOMCRLFAndMissingNewline(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	marker := packetStubMarker("decisions", "epic", "EPIC-CLI-008")
	// A leading byte-order mark, a CRLF line ending, and no trailing newline —
	// each an editor default — must not defeat the unfilled test.
	writePacketFile(t, dir, "40-decisions.md", "\uFEFF"+marker+"\r")

	fx.authorPosts = nil
	var pushErr error
	out := captureOut(t, func() { pushErr = workingSetPush(env, false) })
	if pushErr != nil {
		t.Fatalf("push: %v", pushErr)
	}
	if packetPostFor(fx, "decisions") != nil {
		t.Fatalf("an unfilled stub (BOM/CRLF/no newline) must not be pushed: %v", fx.authorPosts)
	}
	if !strings.Contains(out, "packet section decisions — unfilled stub, not pushed (40-decisions.md)") {
		t.Fatalf("the plan must name the skipped stub, got:\n%s", out)
	}
}

func TestREQCROSS331PushSendsAFilledStubWithoutTheMarker(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	marker := packetStubMarker("red_strategy", "epic", "EPIC-CLI-008")
	body := "the real red strategy\nwith two lines\n"
	writePacketFile(t, dir, "30-red-strategy.md", marker+"\n"+body)

	fx.authorPosts = nil
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("push: %v", err)
	}
	action, rec := packetPost(fx, "red_strategy")
	if rec == nil {
		t.Fatalf("a filled stub must be pushed: %v", fx.authorPosts)
	}
	if got := str(rec, "content"); got != body {
		t.Fatalf("the pushed body must be the file minus the marker line, got %q want %q", got, body)
	}
	if action != "create" {
		t.Fatalf("an unserved key must be a create, got %q", action)
	}
}

func TestREQCROSS331PushDoesNotEmptyAServedSection(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	// empty the served recon file
	writePacketFile(t, dir, "10-recon.md", "")

	fx.authorPosts = nil
	var pushErr error
	out := captureOut(t, func() { pushErr = workingSetPush(env, false) })
	if pushErr != nil {
		t.Fatalf("push: %v", pushErr)
	}
	if packetPostFor(fx, "reconnaissance") != nil {
		t.Fatalf("emptying a served section must not push an update: %v", fx.authorPosts)
	}
	if !strings.Contains(out, "packet section reconnaissance — unfilled stub, not pushed (10-recon.md)") {
		t.Fatalf("the plan must name the emptied served file as unfilled, got:\n%s", out)
	}
}

func TestREQCROSS331APushedStubIsNotRePutOnTheNextPush(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	marker := packetStubMarker("red_strategy", "epic", "EPIC-CLI-008")
	body := "the red strategy body\n"
	writePacketFile(t, dir, "30-red-strategy.md", marker+"\n"+body)

	fx.authorPosts = nil
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("first push: %v", err)
	}
	if packetPostFor(fx, "red_strategy") == nil {
		t.Fatalf("first push must create the filled stub: %v", fx.authorPosts)
	}

	// The fixture's author handler does not update the served list, so re-serve
	// the STRIPPED body as the store now holds it (as the REQ-CROSS-314 CAS test
	// does). The next push must compare on that stripped body and post nothing.
	fx.packetSections = append(fx.packetSections, map[string]any{
		"scope_kind": "epic", "scope_external_id": "EPIC-CLI-008",
		"section_key": "red_strategy", "content": body,
		"content_fingerprint": "served-after-epic:EPIC-CLI-008:red_strategy",
	})

	fx.authorPosts = nil
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("second push: %v", err)
	}
	if packetPostFor(fx, "red_strategy") != nil {
		t.Fatalf("a pushed stub must not be re-put on the next push: %v", fx.authorPosts)
	}
}

func TestREQCROSS331DryRunReportsTheUnfilledFileAndPostsNothing(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	writePacketFile(t, dir, "30-red-strategy.md", "")

	fx.authorPosts = nil
	var pushErr error
	out := captureOut(t, func() { pushErr = workingSetPush(env, true) })
	if pushErr != nil {
		t.Fatalf("dry-run push: %v", pushErr)
	}
	if len(fx.authorPosts) != 0 {
		t.Fatalf("--dry-run must post nothing, got %v", fx.authorPosts)
	}
	if !strings.Contains(out, "unfilled stub, not pushed (30-red-strategy.md)") {
		t.Fatalf("--dry-run must report the unfilled file, got:\n%s", out)
	}
}

// Review finding 1 (RUN:2026-09-06): a trailing space on the marker line — an
// editor re-saving the stub without filling it — must not defeat the unfilled
// test. Otherwise the marker text itself is pushed as the section's content, and
// the server's blank-content guard (REQ-CROSS-330) does not catch it because the
// content is non-blank. RED: today the trailing-space line is not stripped.
func TestREQCROSS331PushSkipsAStubWithTrailingWhitespaceOnTheMarker(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	marker := packetStubMarker("red_strategy", "epic", "EPIC-CLI-008")
	writePacketFile(t, dir, "30-red-strategy.md", marker+" \n")

	fx.authorPosts = nil
	var pushErr error
	out := captureOut(t, func() { pushErr = workingSetPush(env, false) })
	if pushErr != nil {
		t.Fatalf("push: %v", pushErr)
	}
	if packetPostFor(fx, "red_strategy") != nil {
		t.Fatalf("a stub with trailing whitespace on the marker must not be pushed: %v", fx.authorPosts)
	}
	if !strings.Contains(out, "packet section red_strategy — unfilled stub, not pushed (30-red-strategy.md)") {
		t.Fatalf("the trailing-whitespace stub must be named unfilled, got:\n%s", out)
	}
}

// SR-CLI-028-001 C2 (EPIC-CLI-028): the scaffolded inventory stub follows the
// fixed-key path — `15-state-inventory.md` maps to `state_inventory`, and an
// untouched stub is not pushed (REQ-CROSS-331 AC1 preserved for the new key).
// RED: the file keys to `15-state-inventory` and is pushed as an extra section.
func TestSRCLI028001PushSkipsAnUntouchedStateInventoryStub(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	writePacketFile(t, dir, "15-state-inventory.md", packetStubMarker("state_inventory", "epic", "EPIC-CLI-008")+"\n")

	fx.authorPosts = nil
	var pushErr error
	out := captureOut(t, func() { pushErr = workingSetPush(env, false) })
	if pushErr != nil {
		t.Fatalf("push: %v", pushErr)
	}
	for _, key := range []string{"state_inventory", "15-state-inventory"} {
		if packetPostFor(fx, key) != nil {
			t.Fatalf("an untouched inventory stub must not be pushed (as %s): %v", key, fx.authorPosts)
		}
	}
	if !strings.Contains(out, "packet section state_inventory — unfilled stub, not pushed (15-state-inventory.md)") {
		t.Fatalf("the plan must name the skipped stub by its canonical key, got:\n%s", out)
	}
}

// REQ-CROSS-489 guard: a scope pull and push carry no trace links, even from a
// server that serves the key on every requirement entry.
func TestREQCROSS489ScopePullAndPushCarryNoTraceLinks(t *testing.T) {
	fx := scopeFixture()
	var queries []string
	env := wsEnv(t, tlServe(t, fx, &queries, tlServedTraces()))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull --scope: %v", err)
	}
	dir := scopeDir(env)
	path := memberPath(dir, "REQ-CROSS-310")
	if body := readScopeFile(t, path); strings.Contains(body, "Trace links") {
		t.Fatalf("the member file must not carry trace links:\n%s", body)
	}
	edit(t, path, func(s string) string { return strings.Replace(s, "the reads", "the reads AND writes", 1) })

	fx.authorPosts = nil
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("push: %v", err)
	}
	if len(fx.authorPosts) == 0 {
		t.Fatal("push posted nothing; the guard checks nothing")
	}
	for _, post := range fx.authorPosts {
		raw, _ := json.Marshal(post)
		if strings.Contains(string(raw), "traces") || strings.Contains(string(raw), "trace_status") {
			t.Errorf("a push must not send trace links: %s", raw)
		}
	}
}

// TestSuccessfulItemPushAdvancesTheLocalBaselineBeforeTheNextPull covers
// REQ-CROSS-332#AC10 and guards the ordinary accepted-push lifecycle.
func TestSuccessfulItemPushAdvancesTheLocalBaselineBeforeTheNextPull(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	path := memberPath(dir, "REQ-CROSS-310")
	edit(t, path, func(s string) string {
		return strings.Replace(s, "the reads", "the reads and writes", 1)
	})
	canonical := canonicalScopeRequirement(fx, "the reads and writes", "canonical-after-item-push")
	fx.authorSyncItems = map[string]any{"REQ-CROSS-310": map[string]any{
		"kind": "system", "item": canonical, "gates": []any{},
	}}
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("push: %v", err)
	}
	fx.requirements[0] = canonical
	if err := workingSetPullScope(env, false, wsNow.Add(time.Minute)); err != nil {
		t.Fatalf("clean pull after accepted push must not look locally dirty: %v", err)
	}
	assertScopedBaselineMatchesFile(t, dir, filepath.Join("members", "REQ-CROSS-310.md"))
}

// TestAcceptedItemPatchCanonicalReadFailureThenRetryRecovers covers
// UR-CLI-DRAFT-PROTECTION-001#AC5 and REQ-CROSS-332#AC11: the accepted patch is visible on retry as semantic
// equality, so only CLI metadata and its baseline can advance.
func TestAcceptedItemPatchCanonicalReadFailureThenRetryRecovers(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	path := memberPath(dir, "REQ-CROSS-310")
	edit(t, path, func(s string) string {
		return strings.Replace(s, "the reads", "the reads and writes", 1)
	})
	authored := readScopeFile(t, path)
	authoredBody := itemBody(authored)
	baselineBefore := scopedBaselineEntryForTest(t, dir, filepath.Join("members", "REQ-CROSS-310.md"))

	fx.authorPosts = nil
	fx.itemsStatusAfterAuthorPost = 500
	if err := workingSetPush(env, false); err == nil {
		t.Fatal("expected canonical item read to fail after accepted patch")
	}
	if len(fx.authorPosts) != 1 {
		t.Fatalf("expected one accepted patch before canonical read failure, got %d posts", len(fx.authorPosts))
	}
	if entry := scopedBaselineEntryForTest(t, dir, filepath.Join("members", "REQ-CROSS-310.md")); entry.SHA256 != baselineBefore.SHA256 {
		t.Fatal("failed canonical read advanced the local draft baseline")
	}

	canonical := canonicalScopeRequirement(fx, "the reads and writes", "accepted-item-fingerprint")
	fx.requirements[0] = canonical
	fx.itemsStatusAfterAuthorPost = 0
	postsBeforeRetry := len(fx.authorPosts)
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("equality retry should reconcile the accepted patch: %v", err)
	}
	if len(fx.authorPosts) != postsBeforeRetry {
		t.Fatalf("equality retry posted a duplicate write: %v", fx.authorPosts[postsBeforeRetry:])
	}
	after := readScopeFile(t, path)
	afterBody := itemBody(after)
	if afterBody != authoredBody {
		t.Fatalf("equality retry changed the authored body\nbefore: %q\nafter:  %q", authoredBody, afterBody)
	}
	if !strings.Contains(after, "accepted-item-fingerprint") {
		t.Fatalf("equality retry did not refresh the canonical CAS metadata:\n%s", after)
	}
	assertScopedBaselineMatchesFile(t, dir, filepath.Join("members", "REQ-CROSS-310.md"))
}

// TestAcceptedItemRetryPreservesConcurrentEditAtMetadataRestamp covers
// REQ-CROSS-332#AC11: the final canonical read cannot baseline bytes changed
// after the retry's initial file staging.
func TestAcceptedItemRetryPreservesConcurrentEditAtMetadataRestamp(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	path := memberPath(dir, "REQ-CROSS-310")
	edit(t, path, func(s string) string {
		return strings.Replace(s, "the reads", "the reads and writes", 1)
	})
	fx.itemsStatusAfterAuthorPost = 500
	if err := workingSetPush(env, false); err == nil {
		t.Fatal("expected accepted item patch followed by canonical read failure")
	}
	canonical := canonicalScopeRequirement(fx, "the reads and writes", "retry-race-canonical-fingerprint")
	fx.requirements[0] = canonical
	fx.itemsStatusAfterAuthorPost = 0
	baselineBefore := scopedBaselineEntryForTest(t, dir, filepath.Join("members", "REQ-CROSS-310.md"))
	concurrent := []byte(readScopeFile(t, path) + "\nconcurrent edit during canonical reread\n")
	reads := 0
	var callbackErr error
	fx.afterExactItemsRead = func(_ []string) {
		reads++
		if reads == 2 {
			callbackErr = os.WriteFile(path, concurrent, 0o644)
		}
	}
	postsBeforeRetry := len(fx.authorPosts)
	err := workingSetPush(env, false)
	if callbackErr != nil {
		t.Fatalf("fixture concurrent retry edit: %v", callbackErr)
	}
	if reads != 2 {
		t.Fatalf("expected initial and final canonical reads, got %d", reads)
	}
	if err == nil {
		t.Fatal("retry should refuse metadata restamp after the local bytes changed")
	}
	if len(fx.authorPosts) != postsBeforeRetry {
		t.Fatalf("metadata race issued another write: %v", fx.authorPosts[postsBeforeRetry:])
	}
	if got := readScopeFile(t, path); got != string(concurrent) {
		t.Fatalf("retry metadata restamp overwrote concurrent bytes\n got: %q\nwant: %q", got, concurrent)
	}
	if got := scopedBaselineEntryForTest(t, dir, filepath.Join("members", "REQ-CROSS-310.md")); got.SHA256 != baselineBefore.SHA256 {
		t.Fatal("retry metadata race falsely advanced the local baseline")
	}
}

// TestAcceptedPacketPutReadFailureThenRetryRefreshesCAS covers
// UR-CLI-DRAFT-PROTECTION-001#AC5 and REQ-CROSS-332#AC11 for a no-op section.
func TestAcceptedPacketPutReadFailureThenRetryRefreshesCAS(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	const authoredBody = "authored reconnaissance\n"
	writePacketFile(t, dir, "10-recon.md", authoredBody)

	fx.authorPosts = nil
	fx.authorOmitPacketFingerprint = true
	fx.packetSectionsStatusAfterAuthorPost = 500
	baselineBefore := scopedBaselineEntryForTest(t, dir, filepath.Join("packet", "10-recon.md"))
	pushErr := workingSetPush(env, false)
	if packetPostFor(fx, "reconnaissance") == nil {
		t.Fatalf("expected an accepted packet put before the failed read: %v", fx.authorPosts)
	}
	if pushErr == nil {
		t.Fatal("push reported success after the accepted put's canonical packet read failed")
	}
	if entry := scopedBaselineEntryForTest(t, dir, filepath.Join("packet", "10-recon.md")); entry.SHA256 != baselineBefore.SHA256 {
		t.Fatal("failed canonical packet read advanced the local draft baseline")
	}
	postsBeforeRetry := len(fx.authorPosts)
	fx.packetSectionsStatusAfterAuthorPost = 0
	_, contextID := readContextStamp(dir)
	fx.packetSections = []any{map[string]any{
		"scope_kind": "epic", "scope_external_id": "EPIC-CLI-008",
		"section_key": "reconnaissance", "content": authoredBody,
		"content_fingerprint":  "accepted-packet-fingerprint",
		"authoring_context_id": contextID,
	}}
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("equality retry should reconcile the accepted packet put: %v", err)
	}
	if len(fx.authorPosts) != postsBeforeRetry {
		t.Fatalf("equality retry posted a duplicate packet write: %v", fx.authorPosts[postsBeforeRetry:])
	}
	if got := readScopeFile(t, filepath.Join(dir, "packet", "10-recon.md")); got != authoredBody {
		t.Fatalf("equality retry changed packet bytes: got %q want %q", got, authoredBody)
	}
	if got := readPacketFingerprints(dir)["reconnaissance"]; got != "accepted-packet-fingerprint" {
		t.Fatalf("equality retry packet CAS fingerprint = %q, want accepted-packet-fingerprint", got)
	}
	assertScopedBaselineMatchesFile(t, dir, filepath.Join("packet", "10-recon.md"))
}

// TestAcceptedNewPacketPathGetsBaselineAfterCanonicalConfirmation covers
// UR-CLI-DRAFT-PROTECTION-001#AC5 and REQ-CROSS-332#AC11.
func TestAcceptedNewPacketPathGetsBaselineAfterCanonicalConfirmation(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	const body = "new packet notes\n"
	writePacketFile(t, dir, "notes.md", body)
	_, contextID := readContextStamp(dir)
	fx.afterAuthorPost = func(rec map[string]any) {
		if str(rec, "kind") != "packet_section" || str(rec, "section_key") != "notes" {
			return
		}
		fx.packetSections = append(fx.packetSections, map[string]any{
			"scope_kind": "epic", "scope_external_id": "EPIC-CLI-008",
			"section_key": "notes", "content": body,
			"content_fingerprint": "accepted-notes-fingerprint", "authoring_context_id": contextID,
		})
	}
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("push newly created packet file: %v", err)
	}
	if packetPostFor(fx, "notes") == nil {
		t.Fatalf("expected accepted notes packet write, got %v", fx.authorPosts)
	}
	assertScopedBaselineMatchesFile(t, dir, filepath.Join("packet", "notes.md"))
	postsAfterCreate := len(fx.authorPosts)
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("unchanged push after accepted new packet path: %v", err)
	}
	if len(fx.authorPosts) != postsAfterCreate {
		t.Fatalf("unchanged push repeated the packet write: %v", fx.authorPosts[postsAfterCreate:])
	}
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull after accepted new packet path: %v", err)
	}
	if got := readScopeFile(t, filepath.Join(dir, "packet", "notes.md")); got != body {
		t.Fatalf("pull changed the packet body: got %q want %q", got, body)
	}
}

// TestAcceptedPacketRetryRestampsWithReconciledCAS covers REQ-CROSS-332#AC11.
func TestAcceptedPacketRetryRestampsWithReconciledCAS(t *testing.T) {
	fx := scopeFixture()
	fx.enforcePacketCAS = true
	env, dir := pulledScope(t, fx)
	const body = "accepted packet body\n"
	writePacketFile(t, dir, "10-recon.md", body)
	_, contextID := readContextStamp(dir)
	fx.authorOmitPacketFingerprint = true
	fx.packetSectionsStatusAfterAuthorPost = 500
	fx.afterAuthorPost = func(rec map[string]any) {
		if str(rec, "kind") != "packet_section" {
			return
		}
		fx.packetSections = []any{map[string]any{
			"scope_kind": "epic", "scope_external_id": "EPIC-CLI-008",
			"section_key": "reconnaissance", "content": body,
			"content_fingerprint": "accepted-packet-fingerprint", "authoring_context_id": contextID,
		}}
	}
	if err := workingSetPush(env, false); err == nil {
		t.Fatal("expected canonical packet read failure after the accepted write")
	}
	accepted := packetPostFor(fx, "reconnaissance")
	if accepted == nil {
		t.Fatalf("expected accepted packet write before read failure: %v", fx.authorPosts)
	}
	fx.packetSectionsStatusAfterAuthorPost = 0
	previousRestamp := wsPushRestamp
	wsPushRestamp = true
	defer func() { wsPushRestamp = previousRestamp }()
	postsBeforeRetry := len(fx.authorPosts)
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("--restamp retry should reconcile accepted CAS before restamping: %v", err)
	}
	if len(fx.authorPosts) != postsBeforeRetry+1 {
		t.Fatalf("expected one successful restamp retry, posts=%v", fx.authorPosts[postsBeforeRetry:])
	}
	retry := fx.authorPosts[len(fx.authorPosts)-1]["record"].(map[string]any)
	if got := str(retry, "expected_fingerprint"); got != "accepted-packet-fingerprint" {
		t.Fatalf("restamp used stale CAS fingerprint %q, want accepted-packet-fingerprint", got)
	}
}

// TestAcceptedPacketRetryAutomaticRestampUsesReconciledCAS covers REQ-CROSS-332#AC11.
func TestAcceptedPacketRetryAutomaticRestampUsesReconciledCAS(t *testing.T) {
	fx := scopeFixture()
	fx.enforcePacketCAS = true
	env, dir := pulledScope(t, fx)
	const body = "accepted packet body\n"
	writePacketFile(t, dir, "10-recon.md", body)
	_, contextID := readContextStamp(dir)
	fx.authorOmitPacketFingerprint = true
	fx.packetSectionsStatusAfterAuthorPost = 500
	fx.afterAuthorPost = func(rec map[string]any) {
		if str(rec, "kind") != "packet_section" {
			return
		}
		fx.packetSections = []any{map[string]any{
			"scope_kind": "epic", "scope_external_id": "EPIC-CLI-008",
			"section_key": "reconnaissance", "content": body,
			"content_fingerprint": "accepted-packet-fingerprint", "authoring_context_id": contextID,
		}}
	}
	if err := workingSetPush(env, false); err == nil {
		t.Fatal("expected canonical packet read failure after the accepted write")
	}
	if packetPostFor(fx, "reconnaissance") == nil {
		t.Fatalf("expected accepted packet write before read failure: %v", fx.authorPosts)
	}
	fx.packetSectionsStatusAfterAuthorPost = 0
	fx.deliveryContext = staleFacts("reconnaissance")
	postsBeforeRetry := len(fx.authorPosts)
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("automatic stale-section retry should reconcile accepted CAS first: %v", err)
	}
	if len(fx.authorPosts) != postsBeforeRetry+1 {
		t.Fatalf("expected one successful automatic restamp, posts=%v", fx.authorPosts[postsBeforeRetry:])
	}
	retry := fx.authorPosts[len(fx.authorPosts)-1]["record"].(map[string]any)
	if got := str(retry, "expected_fingerprint"); got != "accepted-packet-fingerprint" {
		t.Fatalf("automatic restamp used stale CAS fingerprint %q, want accepted-packet-fingerprint", got)
	}
}

// TestAcceptedItemThenPacketReadFailureThenRetryReconciles covers REQ-CROSS-332
// #AC11 when the item succeeds before the packet's canonical read fails.
func TestAcceptedItemThenPacketReadFailureThenRetryReconciles(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	itemPath := memberPath(dir, "REQ-CROSS-310")
	edit(t, itemPath, func(s string) string {
		return strings.Replace(s, "the reads", "the reads and writes", 1)
	})
	const packetBody = "accepted packet after item\n"
	writePacketFile(t, dir, "10-recon.md", packetBody)
	fx.afterAuthorPost = func(rec map[string]any) {
		if str(rec, "external_id") == "REQ-CROSS-310" {
			fx.requirements[0] = canonicalScopeRequirement(fx, str(rec, "boundary"), "accepted-before-packet-failure")
		}
	}
	fx.authorOmitPacketFingerprint = true
	fx.packetSectionsStatusAfterAuthorPost = 500
	if err := workingSetPush(env, false); err == nil {
		t.Fatal("expected packet canonical read failure after accepted item and packet writes")
	}
	if len(fx.authorPosts) != 2 {
		t.Fatalf("expected one accepted item patch and one accepted packet put, got %v", fx.authorPosts)
	}
	itemAuthoredBody := itemBody(readScopeFile(t, itemPath))
	itemBaseline := scopedBaselineEntryForTest(t, dir, filepath.Join("members", "REQ-CROSS-310.md"))
	packetBaseline := scopedBaselineEntryForTest(t, dir, filepath.Join("packet", "10-recon.md"))
	fx.packetSectionsStatusAfterAuthorPost = 0
	fx.packetSections = []any{map[string]any{
		"scope_kind": "epic", "scope_external_id": "EPIC-CLI-008",
		"section_key": "reconnaissance", "content": packetBody,
		"content_fingerprint":  "accepted-packet-after-item-failure",
		"authoring_context_id": "accepted-context",
	}}
	fx.afterAuthorPost = nil
	postsBeforeRetry := len(fx.authorPosts)
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("no-op retry should reconcile both accepted writes: %v", err)
	}
	if len(fx.authorPosts) != postsBeforeRetry {
		t.Fatalf("retry duplicated an accepted write: %v", fx.authorPosts[postsBeforeRetry:])
	}
	if itemBody(readScopeFile(t, itemPath)) != itemAuthoredBody {
		t.Fatalf("item authored body changed during reconciliation:\n%s", itemBody(readScopeFile(t, itemPath)))
	}
	if got := scopedBaselineEntryForTest(t, dir, filepath.Join("members", "REQ-CROSS-310.md")); got.SHA256 == itemBaseline.SHA256 {
		t.Fatal("item baseline did not advance after equality retry")
	}
	if got := scopedBaselineEntryForTest(t, dir, filepath.Join("packet", "10-recon.md")); got.SHA256 == packetBaseline.SHA256 {
		t.Fatal("packet baseline did not advance after equality retry")
	}
	assertScopedBaselineMatchesFile(t, dir, filepath.Join("members", "REQ-CROSS-310.md"))
	assertScopedBaselineMatchesFile(t, dir, filepath.Join("packet", "10-recon.md"))
}

// TestAcceptedPacketMetadataFailureKeepsPriorBaseline covers REQ-CROSS-332
// #AC11: a failed CAS sidecar write cannot bless an accepted packet draft.
func TestAcceptedPacketMetadataFailureKeepsPriorBaseline(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	writePacketFile(t, dir, "10-recon.md", "packet body accepted\n")
	baselineBefore := scopedBaselineEntryForTest(t, dir, filepath.Join("packet", "10-recon.md"))
	manifest := packetFingerprintManifest(dir)
	var callbackErr error
	fx.afterAuthorPost = func(rec map[string]any) {
		if str(rec, "kind") != "packet_section" {
			return
		}
		if err := os.Remove(manifest); err != nil {
			callbackErr = err
			return
		}
		if err := os.Mkdir(manifest, 0o755); err != nil {
			callbackErr = err
			return
		}
		callbackErr = os.WriteFile(filepath.Join(manifest, "block"), []byte("keep"), 0o644)
	}
	err := workingSetPush(env, false)
	if callbackErr != nil {
		t.Fatalf("fixture block after accepted packet PUT: %v", callbackErr)
	}
	if err == nil {
		t.Fatal("accepted packet write with failed CAS metadata refresh must report partial success")
	}
	if packetPostFor(fx, "reconnaissance") == nil {
		t.Fatalf("expected accepted packet put before metadata failure: %v", fx.authorPosts)
	}
	if got := scopedBaselineEntryForTest(t, dir, filepath.Join("packet", "10-recon.md")); got.SHA256 != baselineBefore.SHA256 {
		t.Fatal("failed CAS metadata refresh advanced the packet draft baseline")
	}
}

// TestAcceptedItemBaselineFailureThenRetryRepairsCurrentCAS covers REQ-CROSS-332
// #AC11: retry must repair a stale local hash even when server CAS is current.
func TestAcceptedItemBaselineFailureThenRetryRepairsCurrentCAS(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	path := memberPath(dir, "REQ-CROSS-310")
	edit(t, path, func(s string) string {
		return strings.Replace(s, "the reads", "the reads and writes", 1)
	})
	authoredBody := itemBody(readScopeFile(t, path))
	baselineBefore := scopedBaselineEntryForTest(t, dir, filepath.Join("members", "REQ-CROSS-310.md"))
	canonical := canonicalScopeRequirement(fx, "the reads and writes", "accepted-item-current-cas")
	fx.authorSyncItems = map[string]any{"REQ-CROSS-310": map[string]any{
		"kind": "system", "item": canonical, "gates": []any{},
	}}
	baselinePath := filepath.Join(dir, scopedDraftBaselineFile)
	savedBaselinePath := baselinePath + ".saved"
	var callbackErr error
	fx.afterAuthorPost = func(rec map[string]any) {
		if str(rec, "external_id") != "REQ-CROSS-310" {
			return
		}
		// Directory obstruction also fails when CI runs as root, unlike chmod.
		if err := os.Rename(baselinePath, savedBaselinePath); err != nil {
			callbackErr = err
			return
		}
		callbackErr = os.Mkdir(baselinePath, 0o755)
	}
	err := workingSetPush(env, false)
	if callbackErr != nil {
		t.Fatalf("block baseline refresh after accepted item patch: %v", callbackErr)
	}
	if err == nil || !strings.Contains(err.Error(), "could not refresh the local draft baseline") {
		t.Fatalf("expected accepted item patch followed by baseline refresh failure, got %v", err)
	}
	if err := os.Remove(baselinePath); err != nil {
		t.Fatalf("remove baseline obstruction: %v", err)
	}
	if err := os.Rename(savedBaselinePath, baselinePath); err != nil {
		t.Fatalf("restore prior baseline: %v", err)
	}
	fx.afterAuthorPost = nil
	if len(fx.authorPosts) != 1 {
		t.Fatalf("expected one accepted item patch, got %v", fx.authorPosts)
	}
	if itemBody(readScopeFile(t, path)) != authoredBody || !strings.Contains(readScopeFile(t, path), "accepted-item-current-cas") {
		t.Fatalf("accepted canonical refresh did not retain authored body and current CAS:\n%s", readScopeFile(t, path))
	}
	if got := scopedBaselineEntryForTest(t, dir, filepath.Join("members", "REQ-CROSS-310.md")); got.SHA256 != baselineBefore.SHA256 {
		t.Fatal("failed baseline refresh changed the previous baseline entry")
	}
	fx.requirements[0] = canonical
	postsBeforeRetry := len(fx.authorPosts)
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("retry should repair local hash with current CAS and no duplicate patch: %v", err)
	}
	if len(fx.authorPosts) != postsBeforeRetry {
		t.Fatalf("baseline repair duplicated the accepted patch: %v", fx.authorPosts[postsBeforeRetry:])
	}
	if itemBody(readScopeFile(t, path)) != authoredBody {
		t.Fatalf("baseline repair rewrote authored body:\n%s", itemBody(readScopeFile(t, path)))
	}
	assertScopedBaselineMatchesFile(t, dir, filepath.Join("members", "REQ-CROSS-310.md"))
}

// TestAcceptedItemCanonicalRefreshPreservesConcurrentLocalEdit covers
// REQ-CROSS-332#AC11: an edit racing an accepted write is never overwritten by
// the canonical metadata refresh.
func TestAcceptedItemCanonicalRefreshPreservesConcurrentLocalEdit(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	path := memberPath(dir, "REQ-CROSS-310")
	edit(t, path, func(s string) string {
		return strings.Replace(s, "the reads", "the reads and writes", 1)
	})
	canonical := canonicalScopeRequirement(fx, "the reads and writes", "accepted-item-race-fingerprint")
	fx.authorSyncItems = map[string]any{"REQ-CROSS-310": map[string]any{
		"kind": "system", "item": canonical, "gates": []any{},
	}}
	baselineBefore := scopedBaselineEntryForTest(t, dir, filepath.Join("members", "REQ-CROSS-310.md"))
	concurrent := []byte(readScopeFile(t, path) + "\nconcurrent author edit\n")
	var callbackErr error
	fx.afterAuthorPost = func(rec map[string]any) {
		if str(rec, "external_id") == "REQ-CROSS-310" {
			callbackErr = os.WriteFile(path, concurrent, 0o644)
		}
	}
	err := workingSetPush(env, false)
	if callbackErr != nil {
		t.Fatalf("fixture concurrent edit: %v", callbackErr)
	}
	if err == nil {
		t.Fatal("accepted patch with a concurrent local edit must report a partial refresh failure")
	}
	if got := readScopeFile(t, path); got != string(concurrent) {
		t.Fatalf("canonical refresh overwrote concurrent local bytes\n got: %q\nwant: %q", got, concurrent)
	}
	if got := scopedBaselineEntryForTest(t, dir, filepath.Join("members", "REQ-CROSS-310.md")); got.SHA256 != baselineBefore.SHA256 {
		t.Fatal("concurrent local bytes were falsely baselined")
	}
}

// TestPushRetryMissingCASPinRequiresManualArchiveRecovery covers
// UR-CLI-DRAFT-PROTECTION-001#AC6 and REQ-CROSS-332#AC12.
func TestPushRetryMissingCASPinRequiresManualArchiveRecovery(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	path := memberPath(dir, "REQ-CROSS-310")
	edit(t, path, func(s string) string {
		s = strings.Replace(s, "- **Served fingerprint:** sr310-fp\n", "", 1)
		return strings.Replace(s, "the reads", "the reads and writes", 1)
	})
	fx.authorPosts = nil
	fx.itemsStatusAfterAuthorPost = 500
	if err := workingSetPush(env, false); err == nil {
		t.Fatal("expected canonical read to fail after accepted patch with absent CAS pin")
	}
	if len(fx.authorPosts) != 1 {
		t.Fatalf("expected one accepted patch before the failed read, got %v", fx.authorPosts)
	}

	canonical := canonicalScopeRequirement(fx, "the reads and writes", "accepted-item-with-missing-pin")
	fx.requirements[0] = canonical
	fx.itemsStatusAfterAuthorPost = 0
	before := scopeTreeBytes(t, dir)
	postsBeforeRetry := len(fx.authorPosts)
	err := workingSetPush(env, false)
	assertManualScopeRecoveryInstructions(t, err)
	if len(fx.authorPosts) != postsBeforeRetry {
		t.Fatalf("retry without the original CAS pin issued another write: %v", fx.authorPosts[postsBeforeRetry:])
	}
	if after := scopeTreeBytes(t, dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("manual-recovery refusal changed the scope tree\nbefore: %#v\nafter:  %#v", before, after)
	}
}

// TestPushRetryOperationMarkerRequiresManualArchiveRecovery covers the
// UR-CLI-DRAFT-PROTECTION-001#AC6 and REQ-CROSS-332#AC12 normalization fallback: retry must not rewrite authored
// operation-marker bytes merely to make the file canonical.
func TestPushRetryOperationMarkerRequiresManualArchiveRecovery(t *testing.T) {
	fx := scopeFixture()
	fx.requirements = []any{reqWith("REQ-CROSS-310", "the reads", []any{rel("declares", "UR-CLI-008", "confirmed")})}
	fx.workSelection["current"].(map[string]any)["members"] = []any{"REQ-CROSS-310"}
	env, dir := pulledScope(t, fx)
	path := memberPath(dir, "REQ-CROSS-310")
	edit(t, path, func(s string) string {
		return strings.Replace(s, "- declares UR-CLI-008 [confirmed]", "- withdraw UR-CLI-008", 1)
	})
	markerDraft := readScopeFile(t, path)
	fx.authorPosts = nil
	fx.itemsStatusAfterAuthorPost = 500
	if err := workingSetPush(env, false); err == nil {
		t.Fatal("expected canonical read to fail after accepted relation withdrawal")
	}
	if patchPostFor(fx, "REQ-CROSS-310") == nil {
		t.Fatalf("expected accepted relation withdrawal before the failed read: %v", fx.authorPosts)
	}

	canonical := reqWith("REQ-CROSS-310", "the reads", nil)
	canonical["fingerprint"] = "accepted-withdrawal-fingerprint"
	fx.requirements = []any{canonical}
	fx.itemsStatusAfterAuthorPost = 0
	before := scopeTreeBytes(t, dir)
	postsBeforeRetry := len(fx.authorPosts)
	err := workingSetPush(env, false)
	assertManualScopeRecoveryInstructions(t, err)
	if !strings.Contains(err.Error(), "UR-CLI-008") || !strings.Contains(err.Error(), "changed since pull") {
		t.Fatalf("ambiguous withdrawal retry must name the target and changed store state: %v", err)
	}
	if len(fx.authorPosts) != postsBeforeRetry {
		t.Fatalf("operation-marker retry issued another write: %v", fx.authorPosts[postsBeforeRetry:])
	}
	if got := readScopeFile(t, path); got != markerDraft {
		t.Fatalf("operation-marker fallback rewrote authored bytes\nbefore: %s\nafter:  %s", markerDraft, got)
	}
	if after := scopeTreeBytes(t, dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("operation-marker fallback changed the scope tree\nbefore: %#v\nafter:  %#v", before, after)
	}
}

// TestLegacyScopeArchiveFreshPullAndManualReapply covers REQ-CROSS-332#AC12:
// old unbaselined scope bytes are preserved while an operator uses a fresh CAS.
func TestLegacyScopeArchiveFreshPullAndManualReapply(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	if err := os.Remove(filepath.Join(dir, scopedDraftBaselineFile)); err != nil {
		t.Fatalf("remove baseline to model a legacy scope: %v", err)
	}
	path := memberPath(dir, "REQ-CROSS-310")
	edit(t, path, func(s string) string {
		return strings.Replace(s, "the reads", "the reads and writes", 1)
	})
	if err := workingSetPullScope(env, false, wsNow.Add(time.Minute)); err == nil {
		t.Fatal("legacy scope refresh should refuse replacement without a baseline")
	} else {
		assertManualScopeRecoveryInstructions(t, err)
	}
	archived := scopeTreeBytes(t, dir)
	archiveDir := dir + ".manual-archive"
	if err := os.Rename(dir, archiveDir); err != nil {
		t.Fatalf("archive complete legacy scope: %v", err)
	}
	if err := workingSetPullScope(env, false, wsNow.Add(2*time.Minute)); err != nil {
		t.Fatalf("pull fresh scope: %v", err)
	}
	path = memberPath(dir, "REQ-CROSS-310")
	edit(t, path, func(s string) string {
		return strings.Replace(s, "the reads", "the reads and writes", 1)
	})
	fx.authorPosts = nil
	fx.afterAuthorPost = func(rec map[string]any) {
		if str(rec, "external_id") == "REQ-CROSS-310" {
			fx.requirements[0] = canonicalScopeRequirement(fx, str(rec, "boundary"), "legacy-recovered-fingerprint")
		}
	}
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("push manually reapplied draft with fresh CAS: %v", err)
	}
	if len(fx.authorPosts) != 1 {
		t.Fatalf("manual reapply should issue exactly one accepted patch, got %v", fx.authorPosts)
	}
	record, _ := fx.authorPosts[0]["record"].(map[string]any)
	if str(record, "expected_fingerprint") != "sr310-fp" {
		t.Fatalf("manual reapply did not use the fresh pull CAS: %v", record)
	}
	if after := scopeTreeBytes(t, archiveDir); !reflect.DeepEqual(after, archived) {
		t.Fatalf("legacy archive bytes changed during recovery\nbefore: %#v\nafter:  %#v", archived, after)
	}
}

// TestOperationMarkerArchiveFreshPullAndManualReapply covers
// REQ-CROSS-332#AC12: a withdrawal already accepted by the store is archived,
// omitted from the fresh canonical file, and any remaining author edit uses its
// new CAS without changing the archive.
func TestOperationMarkerArchiveFreshPullAndManualReapply(t *testing.T) {
	fx := scopeFixture()
	fx.requirements = []any{reqWith("REQ-CROSS-310", "the reads", []any{rel("declares", "UR-CLI-008", "confirmed")})}
	fx.workSelection["current"].(map[string]any)["members"] = []any{"REQ-CROSS-310"}
	env, dir := pulledScope(t, fx)
	path := memberPath(dir, "REQ-CROSS-310")
	edit(t, path, func(s string) string {
		return strings.Replace(s, "- declares UR-CLI-008 [confirmed]", "- withdraw UR-CLI-008", 1)
	})
	fx.afterAuthorPost = func(rec map[string]any) {
		if str(rec, "external_id") == "REQ-CROSS-310" {
			canonical := reqWith("REQ-CROSS-310", "the reads", nil)
			canonical["fingerprint"] = "accepted-withdrawal-fingerprint"
			fx.requirements[0] = canonical
		}
	}
	fx.itemsStatusAfterAuthorPost = 500
	if err := workingSetPush(env, false); err == nil {
		t.Fatal("expected accepted withdrawal followed by failed canonical read")
	}
	if len(fx.authorPosts) != 1 {
		t.Fatalf("expected accepted withdrawal before the failed read, got %v", fx.authorPosts)
	}
	fx.itemsStatusAfterAuthorPost = 0
	fx.afterAuthorPost = nil
	if err := workingSetPush(env, false); err == nil {
		t.Fatal("accepted operation marker should require manual archive recovery")
	} else {
		assertManualScopeRecoveryInstructions(t, err)
	}
	archived := scopeTreeBytes(t, dir)
	archiveDir := dir + ".manual-archive"
	if err := os.Rename(dir, archiveDir); err != nil {
		t.Fatalf("archive complete operation-marker scope: %v", err)
	}
	if err := workingSetPullScope(env, false, wsNow.Add(2*time.Minute)); err != nil {
		t.Fatalf("pull fresh scope after accepted withdrawal: %v", err)
	}
	path = memberPath(dir, "REQ-CROSS-310")
	if strings.Contains(readScopeFile(t, path), "withdraw UR-CLI-008") {
		t.Fatal("fresh canonical pull retained the accepted withdrawal marker")
	}
	edit(t, path, func(s string) string {
		return strings.Replace(s, "the reads", "the reads and writes", 1)
	})
	fx.authorPosts = nil
	fx.afterAuthorPost = func(rec map[string]any) {
		if str(rec, "external_id") == "REQ-CROSS-310" {
			canonical := reqWith("REQ-CROSS-310", str(rec, "boundary"), nil)
			canonical["fingerprint"] = "manual-reapply-fingerprint"
			fx.requirements[0] = canonical
		}
	}
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("push manually reapplied body with fresh CAS: %v", err)
	}
	if len(fx.authorPosts) != 1 {
		t.Fatalf("manual reapply should issue exactly one patch, got %v", fx.authorPosts)
	}
	record, _ := fx.authorPosts[0]["record"].(map[string]any)
	if str(record, "expected_fingerprint") != "accepted-withdrawal-fingerprint" {
		t.Fatalf("manual reapply did not use current canonical CAS: %v", record)
	}
	if after := scopeTreeBytes(t, archiveDir); !reflect.DeepEqual(after, archived) {
		t.Fatalf("operation-marker archive changed during recovery\nbefore: %#v\nafter:  %#v", archived, after)
	}
}
