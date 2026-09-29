package cmd

// REQ-CROSS-373 (EPIC-CLI-017): `process enter <scope>` opens the entry gate
// from store facts — the epic plus every member still in the FROM state (or
// the single SR), the passing cold-review trace at the current aggregate as
// prerequisite, the brief from the packet's entry_brief section — and refuses
// with the specific unmet fact before any write.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/api"
)

const entryBrief = `**Brief:**
- What: Approve entering EPIC-A.
- Why now: The packet is reviewed and
  the members are waiting.
- Changes if approved: Three verbs land.
- Risk if wrong: Low; a gate is withdrawable.
- Recommendation: approve — the change is bounded.
`

func enterFacts(members []map[string]any, scopeStatus, verdict string, independent bool, traceID string, missing []string) map[string]any {
	return map[string]any{
		"aggregate":   pinAggregate,
		"scope":       map[string]any{"external_id": "EPIC-A", "kind": "epic", "status": scopeStatus},
		"members":     members,
		"cold_review": map[string]any{"verdict": verdict, "independent": independent, "trace_external_id": traceID},
		"sections":    map[string]any{"complete": len(missing) == 0, "missing": missing},
	}
}

func member(id, status string) map[string]any {
	return map[string]any{"external_id": id, "kind": "sr", "status": status, "content_fingerprint": strings.Repeat("1", 64),
		"evidence_state": "claimed", "red_recorded": false, "lower_trace_pass": false}
}

func enterServer(t *testing.T, facts map[string]any, serveFacts bool) *ceremonyServer {
	t.Helper()
	cs := newCeremonyServer(t, facts, serveFacts)
	cs.packetSections = []map[string]any{{"section_key": "entry_brief", "content": entryBrief}}
	return cs
}

// Review nit 4: an indented sub-bullet inside a continuation is part of its
// bullet, and a bold label (`- **What:**`) is the same bullet.
func TestParseBriefSectionKeepsSubBulletsAndBoldLabels(t *testing.T) {
	brief, err := parseBriefSection("- **What:** x\n- Why now: because\n  - first reason\n  - second reason\n- Changes if approved: c\n- Risk if wrong: r\n- Recommendation: approve\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if brief["what"] != "x" {
		t.Fatalf("a bold label is the same bullet, got %v", brief)
	}
	if !strings.Contains(fmt.Sprint(brief["why_now"]), "first reason") || !strings.Contains(fmt.Sprint(brief["why_now"]), "second reason") {
		t.Fatalf("indented sub-bullets join their bullet, got %q", brief["why_now"])
	}
}

func TestParseBriefSection(t *testing.T) {
	brief, err := parseBriefSection(entryBrief)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if brief["what"] != "Approve entering EPIC-A." || brief["recommendation"] != "approve — the change is bounded." {
		t.Fatalf("bullets must map to the brief keys, got %v", brief)
	}
	if brief["why_now"] != "The packet is reviewed and the members are waiting." {
		t.Fatalf("a continuation line joins its bullet, got %q", brief["why_now"])
	}
	if _, err := parseBriefSection("- What: x\n- Why now: y\n"); err == nil || !strings.Contains(err.Error(), "Risk if wrong") {
		t.Fatalf("a missing bullet is refused by name, got %v", err)
	}
}

func TestProcessEnterOpensTheGateFromStoreFacts(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{
		member("REQ-A-1", "PROPOSED"), member("REQ-A-2", "PROPOSED"), member("REQ-A-3", "TODO"),
	}, "PROPOSED", "pass", true, "CR-A", nil), true)
	env := enterEnv(t, cs)

	var err error
	out := captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{}) })
	if err != nil {
		t.Fatalf("enter: %v\n%s", err, out)
	}
	if len(cs.authored) != 1 || cs.authored[0]["action"] != "create" {
		t.Fatalf("exactly one gate is created, got %v", cs.authored)
	}
	record, _ := cs.authored[0]["record"].(map[string]any)
	if record["kind"] != "gate" || record["external_id"] != "ENTRY-EPIC-A" || record["purpose"] != "entry" ||
		record["gate_kind"] != "approval_request" || record["transition"] != "PROPOSED->TODO" {
		t.Fatalf("the entry gate shape is wrong: %v", record)
	}
	scope := stringSlice(record["exact_scope"])
	if strings.Join(scope, ",") != "EPIC-A,REQ-A-1,REQ-A-2" {
		t.Fatalf("the scope is the epic plus every member still in FROM (the TODO member is skipped), got %v", scope)
	}
	if prereq := stringSlice(record["prerequisite_gate_external_ids"]); len(prereq) != 1 || prereq[0] != "CR-A" {
		t.Fatalf("the cold-review trace is the prerequisite, got %v", record["prerequisite_gate_external_ids"])
	}
	brief, _ := record["brief"].(map[string]any)
	if brief["what"] != "Approve entering EPIC-A." {
		t.Fatalf("the brief is read from the entry_brief section, got %v", record["brief"])
	}
	if opts, _ := record["options"].([]any); len(opts) == 0 || opts[0].(map[string]any)["key"] != "approve" {
		t.Fatalf("an approving option is what the answer rides, got %v", record["options"])
	}
	if len(cs.selections) != 1 || cs.selections[0]["phase"] != "entry" || cs.selections[0]["scope_external_id"] != "EPIC-A" {
		t.Fatalf("the selection moves to phase entry, got %v", cs.selections)
	}
	if !strings.Contains(out, "REQ-A-3") || !strings.Contains(out, "ENTRY-EPIC-A") {
		t.Fatalf("the skipped member and the gate id are reported: %q", out)
	}
}

func TestProcessEnterSingleSR(t *testing.T) {
	facts := map[string]any{
		"aggregate":   pinAggregate,
		"scope":       map[string]any{"external_id": "REQ-S-1", "kind": "requirement", "status": "PROPOSED"},
		"members":     []map[string]any{member("REQ-S-1", "PROPOSED")},
		"cold_review": map[string]any{"verdict": "pass", "independent": true, "trace_external_id": "CR-S"},
		"sections":    map[string]any{"complete": true, "missing": []string{}},
	}
	cs := enterServer(t, facts, true)
	env := enterEnv(t, cs)
	if err := processEnter(env, "REQ-S-1", enterOpts{}); err != nil {
		t.Fatalf("enter: %v", err)
	}
	record, _ := cs.authored[0]["record"].(map[string]any)
	if scope := stringSlice(record["exact_scope"]); len(scope) != 1 || scope[0] != "REQ-S-1" {
		t.Fatalf("a single SR scope names the SR alone, got %v", scope)
	}
	if cs.selections[0]["scope_kind"] != "single_sr" {
		t.Fatalf("the phase move keeps the single_sr kind, got %v", cs.selections[0])
	}
}

func assertNoWrites(t *testing.T, cs *ceremonyServer) {
	t.Helper()
	if len(cs.authored)+len(cs.selections)+len(cs.reconciles)+len(cs.evidence) != 0 {
		t.Fatalf("a refusal writes nothing, got authored=%v selections=%v", cs.authored, cs.selections)
	}
}

func TestProcessEnterRefusesWithoutAColdReviewTrace(t *testing.T) {
	for _, tc := range []struct {
		name    string
		verdict string
		indep   bool
		trace   string
	}{{"absent", "absent", false, ""}, {"fail", "fail", true, "CR-A"}, {"not independent", "pass", false, "CR-A"}} {
		cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "PROPOSED", tc.verdict, tc.indep, tc.trace, nil), true)
		env := enterEnv(t, cs)
		err := processEnter(env, "EPIC-A", enterOpts{})
		if err == nil || !strings.Contains(err.Error(), "cold-review") {
			t.Fatalf("%s: the missing independent passing cold-review trace must be named, got %v", tc.name, err)
		}
		assertNoWrites(t, cs)
	}
}

func TestProcessEnterRefusesWithMissingSections(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "PROPOSED", "pass", true, "CR-A", []string{"enrichment:REQ-A-1"}), true)
	env := enterEnv(t, cs)
	err := processEnter(env, "EPIC-A", enterOpts{})
	if err == nil || !strings.Contains(err.Error(), "enrichment:REQ-A-1") || !strings.Contains(err.Error(), "working-set push") {
		t.Fatalf("missing sections are named with the remedy, got %v", err)
	}
	assertNoWrites(t, cs)
}

func TestProcessEnterRefusesWithoutFacts(t *testing.T) {
	cs := enterServer(t, nil, false)
	env := enterEnv(t, cs)
	err := processEnter(env, "EPIC-A", enterOpts{})
	if err == nil || !strings.Contains(err.Error(), "deploy") {
		t.Fatalf("a server without facts is refused before any write, got %v", err)
	}
	assertNoWrites(t, cs)
}

func TestProcessEnterRefusesAnUnparseableBrief(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "PROPOSED", "pass", true, "CR-A", nil), true)
	cs.packetSections = []map[string]any{{"section_key": "entry_brief", "content": "- What: only this\n"}}
	env := enterEnv(t, cs)
	err := processEnter(env, "EPIC-A", enterOpts{})
	if err == nil || !strings.Contains(err.Error(), "Why now") {
		t.Fatalf("an incomplete brief is refused naming the missing bullet, got %v", err)
	}
	assertNoWrites(t, cs)

	cs.packetSections = nil
	err = processEnter(env, "EPIC-A", enterOpts{})
	if err == nil || !strings.Contains(err.Error(), "entry_brief") || !strings.Contains(err.Error(), "--brief-file") {
		t.Fatalf("no brief section and no --brief-file is refused naming both, got %v", err)
	}
	assertNoWrites(t, cs)
}

// REQ-CROSS-422 (EPIC-CLI-022, SCN-DEMOTE-006): a taken primary id that is
// neither open nor answered — closed after an earlier entry, or withdrawn —
// is not an error: the verb derives ENTRY-<scope>-R2 (the next free -R<n>)
// and names the predecessor, so a scope demoted to PROPOSED and re-planned
// re-enters with no by-hand id juggling.
func TestProcessEnterDerivesTheSuccessorIdWhenTheEntryGateIsClosed(t *testing.T) {
	for _, state := range []string{"closed", "dismissed"} {
		cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "PROPOSED", "pass", true, "CR-A", nil), true)
		cs.existingGates["ENTRY-EPIC-A"] = map[string]any{"external_id": "ENTRY-EPIC-A", "state": state, "applied_state": "applied"}
		env := enterEnv(t, cs)
		var err error
		out := captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{}) })
		if err != nil {
			t.Fatalf("%s: a closed primary id derives its successor, got %v\n%s", state, err, out)
		}
		record, _ := cs.authored[0]["record"].(map[string]any)
		if record["external_id"] != "ENTRY-EPIC-A-R2" {
			t.Fatalf("%s: the successor id is ENTRY-EPIC-A-R2, got %v", state, record["external_id"])
		}
		if !strings.Contains(out, "ENTRY-EPIC-A") || !strings.Contains(out, "predecessor") {
			t.Fatalf("%s: the plan names the closed predecessor: %q", state, out)
		}
		if !strings.Contains(str(record, "body_md"), "ENTRY-EPIC-A") {
			t.Fatalf("%s: the gate body names its predecessor, got %v", state, record["body_md"])
		}
	}
}

// A successor already in the series is skipped too: -R2 closed derives -R3.
func TestProcessEnterWalksTheSuccessorSeries(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "PROPOSED", "pass", true, "CR-A", nil), true)
	cs.existingGates["ENTRY-EPIC-A"] = map[string]any{"external_id": "ENTRY-EPIC-A", "state": "closed"}
	cs.existingGates["ENTRY-EPIC-A-R2"] = map[string]any{"external_id": "ENTRY-EPIC-A-R2", "state": "closed"}
	env := enterEnv(t, cs)
	if err := processEnter(env, "EPIC-A", enterOpts{}); err != nil {
		t.Fatalf("enter: %v", err)
	}
	if record, _ := cs.authored[0]["record"].(map[string]any); record["external_id"] != "ENTRY-EPIC-A-R3" {
		t.Fatalf("the next free id in the series is ENTRY-EPIC-A-R3, got %v", record["external_id"])
	}
}

func TestProcessEnterDryRunPostsNothing(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "PROPOSED", "pass", true, "CR-A", nil), true)
	env := enterEnv(t, cs)
	var err error
	out := captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{dryRun: true}) })
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !strings.Contains(out, "ENTRY-EPIC-A") || !strings.Contains(out, "CR-A") {
		t.Fatalf("the dry run prints the plan: %q", out)
	}
	assertNoWrites(t, cs)
}

// Review round 2, finding 4: an id that is already open or answered is not a
// taken id to rotate past — the operator should answer or apply it.
func TestProcessEnterSaysAnswerItWhenTheGateIsOpen(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "PROPOSED", "pass", true, "CR-A", nil), true)
	cs.existingGates["ENTRY-EPIC-A"] = map[string]any{"external_id": "ENTRY-EPIC-A", "state": "open"}
	env := enterEnv(t, cs)
	err := processEnter(env, "EPIC-A", enterOpts{})
	if err == nil || !strings.Contains(err.Error(), "already open") || strings.Contains(err.Error(), "-R2") {
		t.Fatalf("an open gate is named as open, never rotated, got %v", err)
	}
	assertNoWrites(t, cs)
}

// Review round 2, nit 13: members entering from PENDING_VERIFICATION while
// the epic is still PROPOSED yield a members-only gate; the plan says why the
// epic is not named.
func TestProcessEnterSaysWhyTheEpicIsNotNamed(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PENDING_VERIFICATION")}, "PROPOSED", "pass", true, "CR-A", nil), true)
	env := enterEnv(t, cs)
	var err error
	out := captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{dryRun: true}) })
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !strings.Contains(out, "EPIC-A is PROPOSED") || !strings.Contains(out, "not named") {
		t.Fatalf("the plan says the epic is not named and why: %q", out)
	}
}

// BACKLOG-TOOL-58 (a defect against REQ-CROSS-422's D14 pin rule, over
// REQ-CROSS-414's admission set): a NEW PROPOSED member added to an epic that
// is ALREADY entered yields a members-only gate — the epic is not named, so
// the store resolves the lone member as the gate's subject. Its prerequisite
// is the epic-scoped cold-review trace, pinned at the epic's aggregate, so
// the gate must carry that aggregate; with no pin the store compares the
// trace against the member's own aggregate and refuses the open.
func TestProcessEnterPinsAMembersOnlyGateAtTheEpicsAggregate(t *testing.T) {
	for _, status := range []string{"TODO", "READY", "IN_PROGRESS", "IN_REVIEW", "BLOCKED"} {
		cs := enterServer(t, enterFacts([]map[string]any{
			member("REQ-A-1", "PROPOSED"), member("REQ-A-2", "TODO"),
		}, status, "pass", true, "CR-A", nil), true)
		env := enterEnv(t, cs)
		var err error
		out := captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{}) })
		if err != nil {
			t.Fatalf("%s: enter: %v\n%s", status, err, out)
		}
		record, _ := cs.authored[0]["record"].(map[string]any)
		if scope := stringSlice(record["exact_scope"]); len(scope) != 1 || scope[0] != "REQ-A-1" {
			t.Fatalf("%s: a members-only gate names the PROPOSED member alone, got %v", status, scope)
		}
		if record["evaluated_scope_fingerprint"] != pinAggregate {
			t.Fatalf("%s: the gate carries the epic's aggregate so its epic-scoped cold review is its prerequisite, got %v",
				status, record["evaluated_scope_fingerprint"])
		}
		if !strings.Contains(out, "pin:") || !strings.Contains(out, pinAggregate) {
			t.Fatalf("%s: the plan names the pin it will send: %q", status, out)
		}
		// PR #626 cold review round 2, finding B-1: this is the success path
		// this verb now creates, so its note must read as one. The old wording
		// was refusal-shaped and told the operator to "enter the epic with its
		// <status> members first", which names nothing that exists.
		if strings.Contains(out, "admitted only once the epic itself has been entered") ||
			strings.Contains(out, "members first if it has any") {
			t.Fatalf("%s: the entered-epic note must not read as a refusal: %q", status, out)
		}
		// The exact note, not just the words: "already entered" also appears in
		// the unrelated "already entered, not named:" line for entered members.
		want := fmt.Sprintf("  EPIC-A is %s, already entered — not named by this gate; it enters the epic's new PROPOSED members on the epic's own cold review\n", presentPin(status))
		if !strings.Contains(out, want) {
			t.Fatalf("%s: the note must read %q, got %q", status, want, out)
		}
	}
}

// An epic outside the admission set keeps the original note. DEFERRED is the
// case: the epic is not named, no pin is sent, and "enter the epic first" is
// genuinely what the operator has to do. (A still-PROPOSED epic never reaches
// this arm — it equals `from`, so its own gate names it.)
func TestProcessEnterKeepsTheRefusalShapedNoteForAnUnenteredEpic(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "DEFERRED", "pass", true, "CR-A", nil), true)
	env := enterEnv(t, cs)
	var err error
	out := captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{dryRun: true}) })
	if err != nil {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "admitted only once the epic itself has been entered") {
		t.Fatalf("a terminal epic still gets the enter-the-epic-first note: %q", out)
	}
}

// A PROPOSED epic is named by its own gate, so there is nothing to pin: the
// store resolves the epic as the subject and its own aggregate as the pin.
func TestProcessEnterSendsNoPinWhenTheGateNamesTheEpic(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "PROPOSED", "pass", true, "CR-A", nil), true)
	env := enterEnv(t, cs)
	if err := processEnter(env, "EPIC-A", enterOpts{}); err != nil {
		t.Fatalf("enter: %v", err)
	}
	record, _ := cs.authored[0]["record"].(map[string]any)
	if _, carried := record["evaluated_scope_fingerprint"]; carried {
		t.Fatalf("a gate that names the epic carries no pin, got %v", record["evaluated_scope_fingerprint"])
	}
}

// BACKLOG-TOOL-58 (PR #626 cold review, finding 2): the dry run must report
// the pin the real open WILL send, which is a fact the verb holds without
// asking the server. Comparing the named cold-review trace's own aggregate
// with that pin is not a check: the server picks cr.TraceExternalID only from
// traces already at facts.Aggregate, so the two agree by construction.
//
// The pin is what a reader cannot otherwise see. When none will be sent for a
// members-only gate, the store resolves the named member's own aggregate
// instead — where the epic's cold-review trace does not pass — and the plan
// says so rather than leaving the line blank.
func TestProcessEnterDryRunReportsThePinItWillSend(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "IN_PROGRESS", "pass", true, "CR-A", nil), true)
	env := enterEnv(t, cs)

	var err error
	out := captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{dryRun: true}) })
	if err != nil {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "pin: the epic's packet aggregate "+pinAggregate) {
		t.Fatalf("the dry run names the pin it will send: %q", out)
	}
	assertNoWrites(t, cs)

	// A terminal epic is outside the admission set, so no pin is sent and the
	// store falls back to the member's own aggregate. The store refuses this
	// shape on its own legality rule; the plan must not read as if the epic's
	// aggregate were what the gate would carry.
	for _, status := range []string{"DONE", "OBSOLETE", "DEFERRED"} {
		cs = enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, status, "pass", true, "CR-A", nil), true)
		env = enterEnv(t, cs)
		out = captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{dryRun: true}) })
		if err != nil {
			t.Fatalf("%s: dry run: %v\n%s", status, err, out)
		}
		if strings.Contains(out, "pin: the epic's packet aggregate") {
			t.Fatalf("%s: no pin is sent for a terminal epic: %q", status, out)
		}
		if !strings.Contains(out, "pin: none") || !strings.Contains(out, "own packet aggregate") {
			t.Fatalf("%s: the plan says the store resolves the member's own aggregate: %q", status, out)
		}
		assertNoWrites(t, cs)
	}
}

// ---------------------------------------------------------------- SR-CLI-028-002 (EPIC-CLI-028)
//
// `process enter` reads the selection's reconnaissance revision, fetches the
// remote default branch, and refuses when the tip moved past that revision
// and a path the packet cites changed — unless the drift is accepted with a
// USER: source, which the gate body records. Every enter test therefore runs
// in a temporary repository with a LOCAL origin (a bare repository on disk),
// so the fetch is real and needs no network, and the fixture selection serves
// the tip as its recon_revision.

// enterRepo is a repository whose main is pushed to a local bare origin; the
// tip is the one commit, and origin/HEAD points at main. a.txt, VERSION and
// c.md exist so a packet can cite them.
func enterRepo(t *testing.T) (root, tip string) {
	t.Helper()
	root = t.TempDir()
	gitRun(t, root, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(root, "a.txt"), "one\n")
	writeFile(t, filepath.Join(root, "VERSION"), "1\n")
	writeFile(t, filepath.Join(root, "c.md"), "c\n")
	tip = gitCommitAll(t, root, "one")
	bare := filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, root, "init", "-q", "--bare", bare)
	gitRun(t, root, "remote", "add", "origin", bare)
	gitRun(t, root, "push", "-q", "origin", "main")
	gitRun(t, root, "remote", "set-head", "origin", "main")
	return root, tip
}

// pushMain changes one file on main, commits it and pushes, returning the new
// tip of origin/main.
func pushMain(t *testing.T, root, file, body, msg string) string {
	t.Helper()
	writeFile(t, filepath.Join(root, file), body)
	tip := gitCommitAll(t, root, msg)
	gitRun(t, root, "push", "-q", "origin", "main")
	return tip
}

// enterEnv is the env every enter test runs under: a repository whose tip is
// the selection's recon_revision, so the reconnaissance reads current unless
// a test moves the tip.
func enterEnv(t *testing.T, cs *ceremonyServer) *factoryEnv {
	t.Helper()
	root, tip := enterRepo(t)
	cs.reconRevision = tip
	return &factoryEnv{Root: root, APIURL: cs.srv.URL, SystemID: 4, token: "t"}
}

// citeSection serves a reconnaissance section citing the given paths, in the
// shapes packet text uses: `CODE:<path>:<line>`, a backticked citation with
// trailing punctuation, an extension-less path, and a repo@rev: qualifier.
func citeSection(cs *ceremonyServer) {
	cs.packetSections = append(cs.packetSections, map[string]any{
		"section_key": "reconnaissance",
		"content": "Surface: CODE:a.txt:3 and `TEST:cmd/x_test.go:10`; the version file `CODE:VERSION`. " +
			"Docs: CODE:modernpath-core@abc123:c.md.",
	})
}

// enterCommand runs `modernpath process enter` through the command line from
// the repository root bound to the ceremony server — the path the new flags
// are parsed on.
func enterCommand(t *testing.T, cs *ceremonyServer, root string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	restore := stubListSystemsFn(func(apiURL, token string) ([]api.System, error) { return []api.System{{ID: 4}}, nil })
	t.Cleanup(restore)
	if err := os.MkdirAll(filepath.Join(root, ".modernpath"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`{"api_url":%q,"system_id":4,"system_name":"T","system_slug":"t"}`, cs.srv.URL)
	writeFile(t, filepath.Join(root, ".modernpath", "config.json"), cfg)
	writeFile(t, filepath.Join(root, ".modernpath", "auth.json"), `{"token":"t"}`)
	t.Chdir(root)
	resetTreeFlags(rootCmd)
	rootCmd.SetArgs(append([]string{"process", "enter"}, args...))
	var err error
	out := captureOut(t, func() { err = rootCmd.Execute() })
	rootCmd.SetArgs(nil)
	resetTreeFlags(rootCmd)
	return out, err
}

func authoredGateBody(cs *ceremonyServer, i int) string {
	rec, _ := cs.authored[i]["record"].(map[string]any)
	return str(rec, "body_md")
}

func driftFacts() map[string]any {
	return enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "PROPOSED", "pass", true, "CR-A", nil)
}

// C1: no reconnaissance revision on the selection refuses before the facts
// read and before any write, naming the verb that records one.
func TestSRCLI028002EnterRefusesWithoutAReconnaissanceRevisionBeforeTheFactsRead(t *testing.T) {
	cs := enterServer(t, driftFacts(), true)
	env := enterEnv(t, cs)
	cs.reconRevision = ""
	err := processEnter(env, "EPIC-A", enterOpts{})
	if err == nil || !strings.Contains(err.Error(), "records no reconnaissance revision") || !strings.Contains(err.Error(), "working-set select EPIC-A --recon-revision") {
		t.Fatalf("a selection with no recon_revision must be refused naming the verb, got %v", err)
	}
	for _, r := range cs.reads {
		if r == "delivery-context" {
			t.Fatalf("the refusal must come before the facts read, reads were %v", cs.reads)
		}
	}
	assertNoWrites(t, cs)
}

// C2: the tip equal to the reconnaissance revision is current.
func TestSRCLI028002EnterProceedsWhenTheTipEqualsTheReconnaissanceRevision(t *testing.T) {
	cs := enterServer(t, driftFacts(), true)
	env := enterEnv(t, cs)
	var err error
	out := captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{}) })
	if err != nil {
		t.Fatalf("enter: %v", err)
	}
	if !strings.Contains(out, "reconnaissance "+cs.reconRevision+" current") {
		t.Fatalf("a current reconnaissance must be said so, got:\n%s", out)
	}
	if len(cs.authored) != 1 {
		t.Fatalf("the gate must open, authored %v", cs.authored)
	}
}

// C2: a packet reconnoitred on a branch AHEAD of the tip is current — the
// branch's own commits are not drift (F-CLI028-R1-01).
func TestSRCLI028002EnterProceedsWhenTheReconnaissanceIsAheadOfTheTipOnABranch(t *testing.T) {
	cs := enterServer(t, driftFacts(), true)
	citeSection(cs)
	env := enterEnv(t, cs)
	gitRun(t, env.Root, "checkout", "-q", "-b", "feature")
	writeFile(t, filepath.Join(env.Root, "a.txt"), "branch edit of a cited file\n")
	cs.reconRevision = gitCommitAll(t, env.Root, "branch work")
	var err error
	out := captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{}) })
	if err != nil {
		t.Fatalf("a reconnaissance ahead of the tip is current, got %v", err)
	}
	if !strings.Contains(out, "reconnaissance "+cs.reconRevision+" current") {
		t.Fatalf("expected the current line, got:\n%s", out)
	}
}

// C3: the tip moved but no cited path changed — proceed, saying so.
func TestSRCLI028002EnterProceedsWhenTheTipMovedButNoCitedPathChanged(t *testing.T) {
	cs := enterServer(t, driftFacts(), true)
	citeSection(cs)
	env := enterEnv(t, cs)
	tip := pushMain(t, env.Root, "d.txt", "uncited\n", "move main")
	var err error
	out := captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{}) })
	if err != nil {
		t.Fatalf("a moved tip with no cited path changed must proceed, got %v", err)
	}
	if !strings.Contains(out, "origin/main moved to "+tip) || !strings.Contains(out, "no cited path changed") {
		t.Fatalf("the moved-but-unaffected line must name the tip, got:\n%s", out)
	}
	if len(cs.authored) != 1 {
		t.Fatalf("the gate must open, authored %v", cs.authored)
	}
}

// C2: a cited path changed between the reconnaissance and the tip refuses,
// naming the tip and each changed cited path (the diff is merge-base to tip;
// an uncited change is not named; a path cited with a line, in backticks,
// without an extension or behind a repo@rev: qualifier is matched by path).
func TestSRCLI028002EnterRefusesWhenACitedPathChangedSinceTheReconnaissance(t *testing.T) {
	cs := enterServer(t, driftFacts(), true)
	citeSection(cs)
	env := enterEnv(t, cs)
	recon := cs.reconRevision
	pushMain(t, env.Root, "a.txt", "two\n", "a moved")
	pushMain(t, env.Root, "d.txt", "uncited\n", "d moved")
	tip := pushMain(t, env.Root, "VERSION", "2\n", "version bumped")
	err := processEnter(env, "EPIC-A", enterOpts{})
	if err == nil {
		t.Fatal("a changed cited path must refuse entry")
	}
	msg := err.Error()
	for _, want := range []string{"reconnaissance at " + recon + " is stale", "origin/main " + tip, "2 cited path(s): VERSION, a.txt", "--allow-drift USER:", "--recon-revision"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the refusal must carry %q, got %q", want, msg)
		}
	}
	if strings.Contains(msg, "d.txt") || strings.Contains(msg, "c.md") {
		t.Fatalf("an unchanged or uncited path must not be named, got %q", msg)
	}
	assertNoWrites(t, cs)
}

// C2: the path token after the prefix is read when whitespace separates it
// from CODE: or TEST: — packet text writes `CODE: <path>` as well as
// `CODE:<path>` — so a changed path cited that way refuses too. Read as no
// citation at all, such a packet would enter past real drift.
func TestSRCLI028002EnterRefusesWhenAPathCitedAfterASpaceChanged(t *testing.T) {
	cs := enterServer(t, driftFacts(), true)
	cs.packetSections = append(cs.packetSections, map[string]any{
		"section_key": "reconnaissance",
		"content":     "Surface: CODE: a.txt:3 — the reader; the version file CODE:  VERSION.",
	})
	env := enterEnv(t, cs)
	recon := cs.reconRevision
	pushMain(t, env.Root, "a.txt", "two\n", "a moved")
	tip := pushMain(t, env.Root, "VERSION", "2\n", "version bumped")
	err := processEnter(env, "EPIC-A", enterOpts{})
	if err == nil {
		t.Fatal("a changed path cited after a space must refuse entry")
	}
	msg := err.Error()
	for _, want := range []string{"reconnaissance at " + recon + " is stale", "origin/main " + tip, "2 cited path(s): VERSION, a.txt"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the refusal must carry %q, got %q", want, msg)
		}
	}
	assertNoWrites(t, cs)
}

// C2: a reconnaissance revision the repository does not hold is a refusal
// naming it (merge-base exit 128, not "not an ancestor").
func TestSRCLI028002EnterRefusesAnUnknownReconnaissanceRevision(t *testing.T) {
	cs := enterServer(t, driftFacts(), true)
	env := enterEnv(t, cs)
	cs.reconRevision = strings.Repeat("d", 40)
	err := processEnter(env, "EPIC-A", enterOpts{})
	if err == nil || !strings.Contains(err.Error(), cs.reconRevision) || !strings.Contains(err.Error(), "not present in the repository") {
		t.Fatalf("an unknown reconnaissance revision must be refused by name, got %v", err)
	}
	assertNoWrites(t, cs)
}

// C3: --allow-drift USER:… proceeds and the gate body records the source,
// the tip and the changed cited paths.
func TestSRCLI028002EnterAcceptsDriftWithAUserSourceAndRecordsItOnTheGate(t *testing.T) {
	cs := enterServer(t, driftFacts(), true)
	citeSection(cs)
	env := enterEnv(t, cs)
	tip := pushMain(t, env.Root, "a.txt", "two\n", "a moved")
	_, err := enterCommand(t, cs, env.Root, "EPIC-A", "--allow-drift", "USER:2026-09-28:a.txt changed by the merged sibling, packet re-read")
	if err != nil {
		t.Fatalf("an accepted drift must proceed, got %v", err)
	}
	if len(cs.authored) != 1 {
		t.Fatalf("the gate must open once, authored %v", cs.authored)
	}
	body := authoredGateBody(cs, 0)
	for _, want := range []string{"Reconnaissance drift accepted (USER:2026-09-28:a.txt changed by the merged sibling, packet re-read)", "origin/main " + tip, "changed cited paths: a.txt"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the gate body must carry %q, got %q", want, body)
		}
	}
}

// C3: a drift source that is not a USER: line is refused before any request.
func TestSRCLI028002EnterRefusesANonUserDriftSourceBeforeAnyRequest(t *testing.T) {
	cs := enterServer(t, driftFacts(), true)
	env := enterEnv(t, cs)
	_, err := enterCommand(t, cs, env.Root, "EPIC-A", "--allow-drift", "yes")
	if err == nil || !strings.Contains(err.Error(), "USER:") {
		t.Fatalf("a non-USER drift source must be refused naming the form, got %v", err)
	}
	if len(cs.reads) != 0 {
		t.Fatalf("the refusal must come before any request, reads were %v", cs.reads)
	}
	assertNoWrites(t, cs)
}

// C2: the fetch precedes every comparison; --no-fetch skips it (offline
// fixtures only) and reads the local origin ref.
func TestSRCLI028002EnterFetchesTheDefaultBranchUnlessNoFetch(t *testing.T) {
	cs := enterServer(t, driftFacts(), true)
	env := enterEnv(t, cs)
	gitRun(t, env.Root, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
	_, err := enterCommand(t, cs, env.Root, "EPIC-A")
	if err == nil || !strings.Contains(err.Error(), "could not fetch origin/main") {
		t.Fatalf("an unreachable origin must refuse before any comparison, got %v", err)
	}
	assertNoWrites(t, cs)
	out, err := enterCommand(t, cs, env.Root, "EPIC-A", "--no-fetch")
	if err != nil {
		t.Fatalf("--no-fetch must read the local origin ref, got %v", err)
	}
	if !strings.Contains(out, "reconnaissance "+cs.reconRevision+" current") {
		t.Fatalf("expected the current line, got:\n%s", out)
	}
}

// C3 (REQ-CROSS-422): both calls of a two-call entry run the check against
// the same reconnaissance revision — the drift refuses both without
// --allow-drift, and with it each gate's body carries the line.
func TestSRCLI028002TheSecondCallOfATwoCallEntryRepeatsTheCheck(t *testing.T) {
	facts := enterFacts([]map[string]any{member("REQ-A-1", "PENDING_VERIFICATION"), member("REQ-A-2", "PROPOSED")}, "PROPOSED", "pass", true, "CR-A", nil)
	cs := enterServer(t, facts, true)
	citeSection(cs)
	env := enterEnv(t, cs)
	pushMain(t, env.Root, "a.txt", "two\n", "a moved")
	for i := 0; i < 2; i++ {
		if err := processEnter(env, "EPIC-A", enterOpts{}); err == nil || !strings.Contains(err.Error(), "is stale") {
			t.Fatalf("call %d must refuse on the standing drift, got %v", i+1, err)
		}
	}
	assertNoWrites(t, cs)
	source := "USER:2026-09-28:drift accepted"
	if _, err := enterCommand(t, cs, env.Root, "EPIC-A", "--allow-drift", source); err != nil {
		t.Fatalf("first call: %v", err)
	}
	facts["members"] = []map[string]any{member("REQ-A-1", "TODO"), member("REQ-A-2", "PROPOSED")}
	if _, err := enterCommand(t, cs, env.Root, "EPIC-A", "--allow-drift", source); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(cs.authored) != 2 {
		t.Fatalf("two gates must open, authored %d", len(cs.authored))
	}
	for i, id := range []string{"ENTRY-EPIC-A-VERIFY", "ENTRY-EPIC-A"} {
		rec, _ := cs.authored[i]["record"].(map[string]any)
		if str(rec, "external_id") != id {
			t.Fatalf("gate %d must be %s, got %v", i+1, id, rec["external_id"])
		}
		if !strings.Contains(authoredGateBody(cs, i), "Reconnaissance drift accepted ("+source+")") {
			t.Fatalf("gate %s must carry the drift line, got %q", id, authoredGateBody(cs, i))
		}
	}
}

// F-PR697-01 (cold review of PR #697): when the diff itself fails — the
// reconnaissance revision shares no merge base with the tip (an orphan
// commit, a shallow clone cut below the merge base) — entry refuses naming
// both revisions; it never reads a failed diff as "no cited path changed".
func TestSRCLI028002EnterRefusesWhenTheDiffAgainstTheReconnaissanceFails(t *testing.T) {
	cs := enterServer(t, driftFacts(), true)
	citeSection(cs)
	env := enterEnv(t, cs)
	gitRun(t, env.Root, "checkout", "-q", "--orphan", "island")
	writeFile(t, filepath.Join(env.Root, "a.txt"), "unrelated history\n")
	cs.reconRevision = gitCommitAll(t, env.Root, "orphan reconnaissance")
	err := processEnter(env, "EPIC-A", enterOpts{})
	if err == nil || !strings.Contains(err.Error(), "could not diff") || !strings.Contains(err.Error(), cs.reconRevision) {
		t.Fatalf("a failed diff must refuse naming the reconnaissance revision, got %v", err)
	}
	assertNoWrites(t, cs)
}

// F-PR697-02: C1 says the dry run refuses too — the drift check precedes the
// dry-run return, so a stale reconnaissance is named, not planned around.
func TestSRCLI028002EnterDryRunRefusesOnDriftToo(t *testing.T) {
	cs := enterServer(t, driftFacts(), true)
	citeSection(cs)
	env := enterEnv(t, cs)
	pushMain(t, env.Root, "a.txt", "two\n", "a moved")
	var err error
	out := captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{dryRun: true}) })
	if err == nil || !strings.Contains(err.Error(), "is stale") {
		t.Fatalf("--dry-run must refuse on drift, got %v", err)
	}
	if strings.Contains(out, "dry run — nothing posted") {
		t.Fatalf("the dry run must not reach the plan on a stale reconnaissance, got:\n%s", out)
	}
	assertNoWrites(t, cs)
}

// F-PR697-04/05: a reconnaissance revision beginning with '-' is refused
// before it reaches git as an option; a `:L12` or `:12–14` (en dash) line
// suffix is stripped from a citation like `:12` is.
func TestSRCLI028002EnterRefusesAnOptionShapedRevisionAndStripsLineSuffixes(t *testing.T) {
	cs := enterServer(t, driftFacts(), true)
	env := enterEnv(t, cs)
	cs.reconRevision = "--output=/tmp/x"
	err := processEnter(env, "EPIC-A", enterOpts{})
	if err == nil || !strings.Contains(err.Error(), "not a revision") {
		t.Fatalf("an option-shaped revision must be refused by name, got %v", err)
	}
	assertNoWrites(t, cs)
	got := citedPaths([]any{map[string]any{"content": "CODE:a.txt:L12 and TEST:b_test.go:12–14 and CODE:c.md:7-9."}})
	for _, want := range []string{"a.txt", "b_test.go", "c.md"} {
		if !got[want] {
			t.Fatalf("the extractor must strip the line suffix and keep %q, got %v", want, got)
		}
	}
}
