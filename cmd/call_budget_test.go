package cmd

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// REQ-CROSS-448 (EPIC-CLI-TURNS): the saving is held by a test, not claimed.
// A four-SR epic is taken from planning to IN_REVIEW twice against one
// stateful fake store, through cobra, the way an agent runs the verbs:
//
//   - the baseline is the single-record path the mp-process-cli skill keeps
//     as the fallback (today's path before the batch verbs);
//   - the batch path is the one the skill's phase blocks now give.
//
// Both end in the same state — every SR IN_REVIEW, a cold-review PASS trace,
// the entry gate applied — and the batch path may take at most a quarter of
// the baseline's calls. Every verb either path runs must be one the skill
// names, so the count follows the skill rather than a private script.

const budgetEpic = "EPIC-CB"

var (
	budgetAgg = strings.Repeat("c", 64)
	budgetUR  = "UR-CB-1"
	budgetSRs = []string{"REQ-CB-1", "REQ-CB-2", "REQ-CB-3", "REQ-CB-4"}
)

// budgetStore is the stateful store both paths run against: records, traces
// and human gates, packet sections, findings, evidence, the work selection
// and reconcile, each derived from what the verbs wrote.
type budgetStore struct {
	*fakeAuthorStore
	t         *testing.T
	sections  map[string]map[string]any // section_key -> row
	red, pass map[string]bool
	selection map[string]any
	unknown   []string
	recon     string // the revision the packet is reconnoitred at (origin/main's tip)
}

func newBudgetStore(t *testing.T) *budgetStore {
	b := &budgetStore{fakeAuthorStore: newFakeAuthorStore(), t: t, sections: map[string]map[string]any{},
		red: map[string]bool{}, pass: map[string]bool{}}
	b.contextFn = b.context
	b.authorHook = b.authorWrite
	b.routes = b.extraRoutes
	return b
}

func (b *budgetStore) status(id string) string {
	rec := b.records[id]
	if rec == nil {
		return ""
	}
	if s := str(rec, "process_status"); s != "" {
		return s
	}
	return str(rec, "work_status")
}

// members is the epic's stored membership, the UR first.
func (b *budgetStore) members() []string {
	epic := b.records[budgetEpic]
	if epic == nil {
		return nil
	}
	return append(stringSlice(epic["user_requirement_external_ids"]), stringSlice(epic["requirement_external_ids"])...)
}

func (b *budgetStore) requiredSections() []string {
	keys := []string{"reconnaissance", "red_strategy", "decisions"}
	for _, m := range b.members() {
		if b.records[m]["_kind"] == "system" {
			keys = append(keys, "enrichment:"+m)
		}
	}
	return keys
}

// coldReviewPass is the passing, independent cold-review trace at the
// aggregate, or "".
func (b *budgetStore) coldReviewPass() string {
	for id, g := range b.gates {
		if g["purpose"] == "cold-review" && g["verdict"] == "PASS" && g["fingerprint"] == budgetAgg && str(g, "review_context_id") != "" {
			return id
		}
	}
	return ""
}

func (b *budgetStore) openMaterial() []string {
	var out []string
	for id, rec := range b.records {
		if rec["_kind"] != "finding" || rec["scope_external_id"] != budgetEpic {
			continue
		}
		d := str(rec, "disposition")
		if (d == "OPEN" || d == "DEFERRED") && materialFinding(str(rec, "category"), str(rec, "severity")) {
			out = append(out, id)
		}
	}
	return out
}

func (b *budgetStore) entryApplied() bool {
	g := b.gates["ENTRY-"+budgetEpic]
	return g != nil && g["applied"] == true
}

func (b *budgetStore) facts() map[string]any {
	var members []any
	for _, id := range b.members() {
		kind := "sr"
		if b.records[id]["_kind"] == "user" {
			kind = "ur"
		}
		evidence := "claimed"
		if b.pass[id] {
			evidence = "passing"
		}
		_, lower := b.gates["TRACE-LOWER-"+id]
		members = append(members, map[string]any{"external_id": id, "kind": kind, "status": b.status(id),
			"content_fingerprint": str(b.records[id], "fingerprint"), "evidence_state": evidence,
			"red_recorded": b.red[id], "lower_trace_pass": lower})
	}
	var missing []string
	for _, k := range b.requiredSections() {
		if b.sections[k] == nil {
			missing = append(missing, k)
		}
	}
	cr := map[string]any{"verdict": "", "independent": false, "trace_external_id": "", "open_finding_ids": b.openMaterial()}
	if id := b.coldReviewPass(); id != "" {
		cr["verdict"], cr["independent"], cr["trace_external_id"] = "pass", true, id
	}
	return map[string]any{
		"aggregate":   budgetAgg,
		"scope":       map[string]any{"external_id": budgetEpic, "kind": "epic", "status": b.status(budgetEpic)},
		"members":     members,
		"cold_review": cr,
		"sections":    map[string]any{"complete": len(missing) == 0, "missing": missing, "required": b.requiredSections()},
		"entry_gate":  map[string]any{"applied": b.entryApplied(), "pinned_aggregate": budgetAgg},
	}
}

func check(name string, ok bool) map[string]any {
	state := "FAIL"
	if ok {
		state = "PASS"
	}
	return map[string]any{"name": name, "state": state}
}

func (b *budgetStore) context(piece string) map[string]any {
	if b.selection == nil {
		return map[string]any{"derived_reason": "no_current_selection"}
	}
	return map[string]any{
		"packet_fingerprint": budgetAgg, "process_revision": strings.Repeat("d", 40),
		"derived_phase": str(b.selection, "phase"), "declared_phase": str(b.selection, "phase"),
		"checks": map[string]any{"cold_review": []any{
			check("independent_verdict", b.coldReviewPass() != ""), check("findings", len(b.openMaterial()) == 0)}},
		"facts": b.facts(), "facts_state": "served",
	}
}

func (b *budgetStore) authorWrite(body map[string]any) (bool, int, map[string]any, string) {
	record, _ := body["record"].(map[string]any)
	id, kind, action := str(record, "external_id"), str(record, "kind"), str(body, "action")
	switch {
	case kind == "packet_section":
		key := str(record, "section_key")
		if prev := b.sections[key]; prev != nil && action == "update" && str(record, "expected_fingerprint") != str(prev, "content_fingerprint") {
			return true, 409, refusalBody("stale section " + key), ""
		}
		fp := b.nextFP("section-" + key)
		b.sections[key] = map[string]any{"scope_kind": "epic", "scope_external_id": budgetEpic, "section_key": key,
			"content": record["content"], "content_fingerprint": fp, "authoring_context_id": record["authoring_context_id"]}
		return true, 200, map[string]any{"data": map[string]any{"packet_section": map[string]any{"section_key": key, "fingerprint": fp}}}, fp
	case kind == "gate" && action == "create":
		if _, taken := b.gates[id]; taken {
			return true, 409, refusalBody("a gate is opened once"), ""
		}
		gate := map[string]any{}
		for k, v := range record {
			gate[k] = v
		}
		gate["state"], gate["fingerprint"], gate["content_fingerprint"] = "open", b.nextFP(id), "row-"+id
		gate["evaluated_scope_fingerprint"] = budgetAgg
		b.gates[id] = gate
		return true, 200, map[string]any{"data": map[string]any{"gate": gate}}, str(gate, "fingerprint")
	case action == "advance" && str(body, "gate_ref") != "":
		gate := b.gates[str(body, "gate_ref")]
		if gate == nil || gate["state"] != "answered" {
			return true, 422, refusalBody("the gate is not answered"), ""
		}
		if str(body, "gate_fingerprint") != str(gate, "fingerprint") || str(body, "gate_answer") != "approve" {
			return true, 409, refusalBody("stale gate fingerprint or answer"), ""
		}
		if !slices.Contains(stringSlice(gate["exact_scope"]), id) {
			return true, 422, refusalBody("the gate's scope does not name " + id), ""
		}
		status, resp, fp := b.author(body)
		if status == 200 {
			done := true
			for _, s := range stringSlice(gate["exact_scope"]) {
				if b.status(s) == "PROPOSED" {
					done = false
				}
			}
			if done {
				gate["applied"], gate["state"] = true, "closed"
			}
		}
		return true, status, resp, fp
	case action == "create" && (kind == "epic" || kind == "finding"):
		status, resp, fp := b.author(body)
		if status == 200 && kind == "epic" {
			b.records[id]["process_status"] = "PROPOSED"
		}
		if status == 200 && kind == "finding" && str(b.records[id], "disposition") == "" {
			b.records[id]["disposition"] = "OPEN"
		}
		return true, status, resp, fp
	}
	return false, 0, nil, ""
}

// reconcile applies one automatic step per item per pass.
func (b *budgetStore) reconcile() []any {
	var out []any
	move := func(id, to string) {
		from := b.status(id)
		if b.records[id]["_kind"] == "epic" {
			b.records[id]["process_status"] = to
		} else {
			b.records[id]["work_status"] = to
		}
		out = append(out, transition(id, from, to))
	}
	for _, id := range b.members() {
		if b.records[id]["_kind"] != "system" {
			continue
		}
		_, lower := b.gates["TRACE-LOWER-"+id]
		switch b.status(id) {
		case "TODO":
			if b.entryApplied() && b.red[id] {
				move(id, "IN_PROGRESS")
			}
		case "IN_PROGRESS":
			if lower && b.pass[id] {
				move(id, "IN_REVIEW")
			}
		}
	}
	if b.status(budgetEpic) == "TODO" && len(out) > 0 {
		move(budgetEpic, "IN_PROGRESS")
	}
	return out
}

func (b *budgetStore) extraRoutes(mux *http.ServeMux) {
	encode := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/api/v1/sync/work-selection", func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.calls++
		if r.Method == http.MethodPost {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			next := map[string]any{"scope_kind": "epic", "scope_external_id": budgetEpic, "members": b.members(), "phase": "plan"}
			if b.selection != nil {
				for k, v := range b.selection {
					next[k] = v
				}
			}
			for _, k := range []string{"scope_kind", "scope_external_id", "phase", "recon_revision"} {
				if v := str(body, k); v != "" {
					next[k] = v
				}
			}
			if m := stringSlice(body["members"]); len(m) > 0 {
				next["members"] = m
			}
			b.selection = next
			encode(w, map[string]any{"data": map[string]any{"selection": next}})
			return
		}
		current := any(nil)
		if b.selection != nil {
			current = b.selection
		}
		encode(w, map[string]any{"data": map[string]any{"current": current, "suspended": []any{}, "history": []any{},
			"active_release": []any{map[string]any{"slug": "r", "status": "active"}}}})
	})
	mux.HandleFunc("/api/v1/sync/packet-sections", func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.calls++
		rows := []any{}
		for _, k := range append(b.requiredSections(), "entry_brief") {
			if row := b.sections[k]; row != nil {
				rows = append(rows, row)
			}
		}
		encode(w, map[string]any{"data": map[string]any{"packet_sections": rows}})
	})
	mux.HandleFunc("/api/v1/sync/gates/ENTRY-"+budgetEpic+"/answer", func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.calls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gate := b.gates["ENTRY-"+budgetEpic]
		review, _ := body["review"].(map[string]any)
		if gate == nil || gate["state"] != "open" || str(review, "content_fingerprint") != str(gate, "content_fingerprint") {
			w.WriteHeader(422)
			encode(w, refusalBody("not answerable"))
			return
		}
		gate["state"], gate["answer"], gate["chosen_option_keys"] = "answered", body["answer"], body["chosen_option_keys"]
		gate["fingerprint"] = b.nextFP("ENTRY")
		encode(w, map[string]any{"data": map[string]any{"gate": gate}})
	})
	mux.HandleFunc("/api/v1/sync/evidence", func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.calls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		results, _ := body["results"].([]any)
		for _, raw := range results {
			res, _ := raw.(map[string]any)
			id := str(res, "target_external_id")
			switch {
			case str(res, "result") == "fail" && str(res, "role") == "RED":
				b.red[id] = true
			case str(res, "result") == "pass":
				b.pass[id] = true
			}
		}
		encode(w, map[string]any{"data": map[string]any{"result": "created", "run": map[string]any{"external_id": body["external_id"]}, "warnings": []any{}}})
	})
	mux.HandleFunc("/api/v1/sync/reconcile", func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.calls++
		encode(w, map[string]any{"data": map[string]any{"applied": true, "transitions": b.reconcile(), "fails": []any{}}})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		b.unknown = append(b.unknown, r.Method+" "+r.URL.Path)
		b.mu.Unlock()
		http.NotFound(w, r)
	})
}

// budgetRun counts the CLI invocations of one path and checks each verb it
// runs against the skill.
type budgetRun struct {
	t     *testing.T
	store *budgetStore
	skill string
	calls int
}

func (r *budgetRun) run(args ...string) string {
	r.t.Helper()
	r.calls++
	verb := strings.Join(args[:min(len(args), 2)], " ")
	if args[0] == "process" && len(args) > 2 && !strings.HasPrefix(args[2], "-") && (args[1] == "review" || args[1] == "findings") {
		verb = strings.Join(args[:3], " ")
	}
	if !strings.Contains(r.skill, verb) {
		r.t.Errorf("the path runs `%s`, which the mp-process-cli skill never names", verb)
	}
	for _, a := range args {
		if a == "--file" || a == "--all" || a == "--gate" || a == "--for-review" {
			given := false
			for _, line := range strings.Split(r.skill, "\n") {
				if strings.Contains(line, verb) && strings.Contains(line, a) {
					given = true
				}
			}
			if !given {
				r.t.Errorf("the path runs `%s %s`, which the skill's phase blocks never give", verb, a)
			}
		}
	}
	out, err := runRoot(r.t, args...)
	if err != nil {
		r.t.Fatalf("`modernpath %s` failed: %v\n%s\nunserved: %v", strings.Join(args, " "), err, out, r.store.unknown)
	}
	return out
}

func (b *budgetStore) fp(id string) string { return str(b.records[id], "fingerprint") }

// budgetWorkspace binds a workspace with a git history: a RED commit then a
// GREEN one, so evidence resolves both revisions.
func budgetWorkspace(t *testing.T, b *budgetStore) (red string) {
	t.Helper()
	cobraWorkspace(t, b.serve(t))
	dir, _ := os.Getwd()
	gitRun := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	gitRun("init", "-q", "-b", "main")
	gitRun("commit", "-q", "--allow-empty", "-m", "red")
	red = gitRun("rev-parse", "HEAD")
	gitRun("commit", "-q", "--allow-empty", "-m", "green")
	// SR-CLI-028-002: `process enter` compares origin's default branch with
	// the selection's reconnaissance revision, so the workspace has an origin
	// whose tip is the revision the packet is reconnoitred at.
	origin := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "--bare", "-b", "main", origin).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	gitRun("remote", "add", "origin", origin)
	gitRun("push", "-q", "origin", "main")
	gitRun("fetch", "-q", "origin")
	b.recon = gitRun("rev-parse", "HEAD")
	return red
}

// fillPacket writes every scaffolded packet stub and the entry brief, as the
// planner does between the pull and the push.
func fillPacket(t *testing.T) {
	t.Helper()
	dir := filepath.Join(workingSetDir, budgetEpic, "packet")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		body := "The " + sectionKeyFromFile(e.Name()) + " section, authored.\n"
		if err := os.WriteFile(filepath.Join(dir, e.Name()), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	brief := "- What: enter the four-SR epic\n- Why now: it is planned\n- Changes if approved: the build starts\n- Risk if wrong: rework\n- Recommendation: approve\n"
	if err := os.WriteFile(filepath.Join(dir, packetFileName("entry_brief")), []byte(brief), 0o644); err != nil {
		t.Fatal(err)
	}
}

const budgetReviewer = `{"verdict":"PASS","body":"the packet holds","source":"RUN:2026-09-28:cold-review",
 "findings":[
  {"id":"F-CB-1","category":"traceability","severity":"note","owner":"core","source":"r","body":"a wording note"},
  {"id":"F-CB-2","category":"scope","severity":"minor","owner":"core","source":"r","body":"a scope remark"}],
 "dispositions":[]}`

func budgetPlan() string {
	var b strings.Builder
	b.WriteString("epic: {id: " + budgetEpic + ", title: Call budget, description: four SRs to IN_REVIEW}\nrequirements:\n")
	b.WriteString("  - {id: " + budgetUR + ", kind: ur, context: CROSS, title: The user outcome, description: the user can do it,\n")
	b.WriteString("     criteria: [{external_id: AS-1, kind: scenario, given: a plan, when: it runs, then: it lands}]}\n")
	for _, sr := range budgetSRs {
		b.WriteString("  - {id: " + sr + ", kind: sr, context: CROSS, title: " + sr + " title, description: the statement,\n")
		b.WriteString("     rationale: why, boundary: the CLI, verification_method: Go tests, parents: [" + budgetUR + "]}\n")
	}
	b.WriteString("members: [" + budgetUR + ", " + strings.Join(budgetSRs, ", ") + "]\n")
	return b.String()
}

func (b *budgetStore) assertEndState(t *testing.T, path string) {
	t.Helper()
	for _, sr := range budgetSRs {
		if got := b.status(sr); got != "IN_REVIEW" {
			t.Errorf("%s: %s ends %s, not IN_REVIEW", path, sr, got)
		}
	}
	if b.coldReviewPass() == "" {
		t.Errorf("%s: no independent cold-review PASS trace at the aggregate", path)
	}
	if !b.entryApplied() {
		t.Errorf("%s: the entry gate is not applied", path)
	}
	if got := b.status(budgetUR); got != "TODO" {
		t.Errorf("%s: %s ends %s, not TODO (entered)", path, budgetUR, got)
	}
	for _, f := range []string{"F-CB-1", "F-CB-2"} {
		if b.records[f] == nil {
			t.Errorf("%s: finding %s was not recorded", path, f)
		}
	}
}

func readSkill(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "internal", "kit", "assets", "skills", "mp-process-cli", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// The baseline: the single-record fallback the skill keeps.
func runSingleRecordPath(t *testing.T, skill string) (*budgetStore, int) {
	b := newBudgetStore(t)
	red := budgetWorkspace(t, b)
	r := &budgetRun{t: t, store: b, skill: skill}

	// Author the plan record by record.
	r.run("author", "epic", budgetEpic, "--title", "Call budget", "--description", "four SRs to IN_REVIEW")
	r.run("author", "requirement", budgetUR, "--kind", "ur", "--context", "CROSS", "--title", "The user outcome")
	r.run("author", "update", budgetUR, "--expected-fingerprint", b.fp(budgetUR), "--description", "the user can do it",
		"--criteria", `[{"external_id":"AS-1","kind":"scenario","given":"a plan","when":"it runs","then":"it lands"}]`)
	for _, sr := range budgetSRs {
		r.run("author", "requirement", sr, "--context", "CROSS", "--title", sr+" title")
		r.run("author", "update", sr, "--expected-fingerprint", b.fp(sr), "--description", "the statement",
			"--rationale", "why", "--boundary", "the CLI", "--verification-method", "Go tests")
		r.run("author", "relate", sr, "--expected-fingerprint", b.fp(sr), "--parent", budgetUR)
	}
	member := []string{"author", "member", budgetEpic, "--expected-fingerprint", b.fp(budgetEpic)}
	for _, m := range append([]string{budgetUR}, budgetSRs...) {
		member = append(member, "--member", m)
	}
	r.run(member...)

	// Take the scope, author the packet.
	r.run("working-set", "select", budgetEpic, "--kind", "epic", "--phase", "plan", "--members", strings.Join(append([]string{budgetUR}, budgetSRs...), ","), "--recon-revision", b.recon)
	r.run("working-set", "pull", "--scope")
	fillPacket(t)
	r.run("working-set", "push")

	// Cold review: the review pull, the aggregate, each finding, the verdict, the check.
	r.run("working-set", "pull", "--scope", "--for-review")
	r.run("process", "next", "-v")
	r.run("process", "findings", "add", "--scope", "epic:"+budgetEpic, "--id", "F-CB-1", "--category", "traceability",
		"--severity", "note", "--owner", "core", "--source", "r", "--body", "a wording note", "--aggregate", budgetAgg)
	r.run("process", "findings", "add", "--scope", "epic:"+budgetEpic, "--id", "F-CB-2", "--category", "scope",
		"--severity", "minor", "--owner", "core", "--source", "r", "--body", "a scope remark", "--aggregate", budgetAgg)
	trace := []string{"author", "trace", "CR-TRACE-" + budgetEpic + "-R1", "--purpose", "cold-review", "--verdict", "PASS",
		"--scope", budgetEpic, "--source", "RUN:2026-09-28:cold-review", "--title", "Cold review of " + budgetEpic}
	for _, m := range append([]string{budgetUR}, budgetSRs...) {
		trace = append(trace, "--scope", m)
	}
	r.run(trace...)
	r.run("process", "check", "--phase", "cold_review")

	// Entry: the gate, the answer, the gate's fingerprint, each advance.
	r.run("process", "enter", budgetEpic)
	r.run("factory", "answer", "ENTRY-"+budgetEpic, "--options", "approve", "--text", "USER:2026-09-28: approve")
	gate := "ENTRY-" + budgetEpic
	r.run("factory", "gates", gate)
	gfp := str(b.gates[gate], "fingerprint")
	for _, m := range append([]string{budgetUR}, budgetSRs...) {
		r.run("author", "advance", m, "--kind", "requirement", "--to", "TODO", "--expected", "PROPOSED",
			"--gate", gate, "--gate-answer", "approve", "--gate-fingerprint", gfp)
	}
	r.run("author", "advance", budgetEpic, "--kind", "epic", "--to", "TODO", "--expected", "PROPOSED",
		"--gate", gate, "--gate-answer", "approve", "--gate-fingerprint", gfp)

	// Build: RED and the passing run per SR, then the advance per SR.
	for _, sr := range budgetSRs {
		r.run("factory", "evidence", "--fail", sr, "--role", "RED", "--revision", red, "--log", "go test ./cmd -run "+sr)
		r.run("factory", "evidence", "--pass", sr, "--log", "go test ./cmd -run "+sr)
		r.run("process", "advance", sr, "--log", "go test ./cmd -run "+sr)
	}
	return b, r.calls
}

// The batch path: the skill's phase blocks.
func runBatchPath(t *testing.T, skill string) (*budgetStore, int) {
	b := newBudgetStore(t)
	red := budgetWorkspace(t, b)
	r := &budgetRun{t: t, store: b, skill: skill}

	r.run("author", "apply", "--file", writePlanFile(t, "plan.yaml", budgetPlan()))

	r.run("working-set", "select", budgetEpic, "--kind", "epic", "--phase", "plan", "--members", strings.Join(append([]string{budgetUR}, budgetSRs...), ","), "--recon-revision", b.recon)
	r.run("working-set", "pull", "--scope")
	fillPacket(t)
	r.run("working-set", "push")

	r.run("working-set", "pull", "--scope", "--for-review")
	r.run("process", "review", "record", "--file", writePlanFile(t, "review.json", budgetReviewer))

	r.run("process", "enter", budgetEpic)
	r.run("factory", "answer", "ENTRY-"+budgetEpic, "--options", "approve", "--text", "USER:2026-09-28: approve")
	r.run("author", "advance", "--gate", "ENTRY-"+budgetEpic)

	var runs []map[string]any
	for _, sr := range budgetSRs {
		runs = append(runs, map[string]any{"fail": []string{sr}, "role": "RED", "revision": red, "log": "go test ./cmd -run " + sr})
	}
	for _, sr := range budgetSRs {
		runs = append(runs, map[string]any{"pass": []string{sr}, "log": "go test ./cmd -run " + sr})
	}
	raw, _ := json.Marshal(runs)
	r.run("factory", "evidence", "--file", writePlanFile(t, "runs.json", string(raw)))
	r.run("process", "advance", "--all", "--piece", budgetEpic, "--log", "go test ./cmd")
	return b, r.calls
}

func TestCallBudgetBatchPathTakesAQuarterOfTheSingleRecordPath(t *testing.T) {
	skill := readSkill(t)

	single, baseline := runSingleRecordPath(t, skill)
	single.assertEndState(t, "single-record path")

	batched, batch := runBatchPath(t, skill)
	batched.assertEndState(t, "batch path")

	t.Logf("call budget: single-record path %d calls (%d HTTP requests), batch path %d calls (%d HTTP requests)",
		baseline, single.calls, batch, batched.calls)
	if batch*4 > baseline {
		t.Errorf("the batch path takes %d calls, more than a quarter of the single-record path's %d", batch, baseline)
	}
}
