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
	"strings"
	"testing"
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
