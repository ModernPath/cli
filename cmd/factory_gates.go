package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------- gates / answer

var factoryGatesCmd = &cobra.Command{
	Use:   "gates [external_id]",
	Short: "The open decision queue; a gate by id, or gate history with --state",
	Long: `Without arguments, the open decision queue (questions, decisions, approvals).

With an external_id, show that one gate — its state and, when it carries an
answer, the answer, chosen options, USER: source, answerer and applied state —
so "did my approval land?" is answerable without reading the event stream. An
applied answer is stored closed; read it by id or under --state all.
The stored decision brief is shown without --json or --verbose: What, Why now,
Changes if approved, Risk if wrong and Recommendation.

With --audit and an external_id, read answer and application blockers, review
provenance and recovery guidance without changing the work selection.

With --state, list gate history: open, answered, dismissed, superseded, or all.
--json prints the server's gate envelope on stdout and nothing else.

A listing prints at most --limit gates (default 50) from --offset in text,
and says how to see more; under --json every gate is printed unless --limit
is given.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// factoryEnvLoad stays first: a local refusal (e.g. a token without the
		// project audience) must send nothing, including the reachability check
		// (TestFactoryGatesRefusesTokenWithoutProjectAudienceBeforeSending).
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		id := ""
		if len(args) == 1 {
			id = args[0]
		}
		return factoryGatesRun(env, gatesState, gatesKind, gatesJSON, id, os.Stdout, os.Stderr, gatesAudit)
	},
}

// validGateState reports whether s is one of the server filter's five values.
// `closed` is deliberately excluded (REQ-CROSS-109 D2): an applied answer is
// stored closed and is reached through `all` or the by-id read, not this filter.
func validGateState(s string) bool {
	switch s {
	case "open", "answered", "dismissed", "superseded", "all":
		return true
	}
	return false
}

// factoryGatesRun is the testable seam behind `factory gates`. out carries the
// rendering (or, under jsonOut, only the JSON envelope); errOut carries warnings,
// so JSON on stdout stays parseable — the requirements-corpus precedent
// (REQ-CROSS-324/121). --state is validated here, before any request, so a typo
// never becomes a call; the server stays the authority and its 422 is surfaced.
func factoryGatesRun(env *factoryEnv, state, kind string, jsonOut bool, id string, out, errOut io.Writer, audit bool) error {
	if audit && id == "" {
		return fmt.Errorf("--audit requires an external_id")
	}
	if id != "" && (state != "" || kind != "") {
		return fmt.Errorf("--state and --kind apply to the listing, not to a gate by id (--json combines with either form)")
	}
	if err := gatesPage.validate(); err != nil {
		return err
	}
	if state != "" && !validGateState(state) {
		return fmt.Errorf("unknown --state %q — use one of open, answered, dismissed, superseded, all", state)
	}
	if id != "" {
		return factoryGateShow(env, id, jsonOut, out, audit)
	}
	return factoryGateList(env, state, kind, jsonOut, out, errOut)
}

// factoryGateShow reads one gate by id: GET /sync/gates/<id>?system_id=<bound>.
// The id is path-escaped. Absence — the server's own "no such gate" message —
// is a non-zero exit naming the id; any other 404 (a server without the route
// answers Phoenix's {"errors":{"detail":"Not Found"}}) is a server error, never
// read as absence.
func factoryGateShow(env *factoryEnv, id string, jsonOut bool, out io.Writer, audit bool) error {
	path := fmt.Sprintf("/api/v1/sync/gates/%s?system_id=%d", url.PathEscape(id), env.SystemID)
	if audit {
		path += "&audit=true"
	}
	status, body, err := env.call("GET", path, nil)
	if err != nil {
		return err
	}
	if status != 200 {
		return gateShowError(status, body, id)
	}
	gate, _ := dataOf(body)["gate"].(map[string]any)
	if gate == nil {
		// Symmetry with the list path's nil→[] (a malformed/renamed 200 envelope):
		// emit {"gate": {}} rather than {"gate": null} so a consumer's parse holds.
		gate = map[string]any{}
	}
	diagnostics, _ := gate["audit"].(map[string]any)
	if audit && diagnostics == nil {
		return fmt.Errorf("server did not return the requested gate audit; update the server before using --audit")
	}
	if jsonOut {
		return emitJSONEnvelope(out, "gate", gate)
	}
	renderGate(out, gate)
	if audit {
		renderGateAudit(out, diagnostics)
	}
	return nil
}

func renderGateAudit(out io.Writer, audit map[string]any) {
	answer, _ := audit["answer"].(map[string]any)
	application, _ := audit["application"].(map[string]any)
	fmt.Fprintf(out, "\nAnswer ready: %v\nApplication: %s\n", answer["ready"], str(application, "status"))
	if remaining, ok := application["remaining_scope"].([]any); ok && len(remaining) > 0 {
		fmt.Fprintf(out, "Remaining scope: %v\n", remaining)
	}
	for _, section := range []struct {
		name  string
		value map[string]any
	}{
		{"Answer", answer}, {"Application", application},
	} {
		blockers, _ := section.value["blockers"].([]any)
		for _, item := range blockers {
			blocker, _ := item.(map[string]any)
			fmt.Fprintf(out, "%s [%s]: %s\n  Next: %s\n", section.name, str(blocker, "code"), str(blocker, "message"), str(blocker, "next_action"))
		}
	}
	reviews, _ := audit["reviews"].([]any)
	for _, item := range reviews {
		review, _ := item.(map[string]any)
		fmt.Fprintf(out, "Review %s: %s; context=%v; independent=%v; reason=%v\n", str(review, "external_id"), str(review, "state"), review["review_context_id"], review["independent"], review["independence_reason"])
	}
}

// gateShowError concludes absence ONLY from the server's nested "no such gate"
// message; every other non-200 is reported verbatim as `server <status>: …`.
func gateShowError(status int, body map[string]any, id string) error {
	if status == 404 {
		if em, ok := body["error"].(map[string]any); ok && strings.HasPrefix(str(em, "message"), "no such gate") {
			return fmt.Errorf("gate %s does not exist", id)
		}
	}
	return fmt.Errorf("server %d: %s", status, gateErrText(body))
}

// gateErrText pulls a human message out of the error shapes the server and the
// gateway use: a nested {"error":{"message":…}}, a flat {"error":"…"}, or
// Phoenix's {"errors":{"detail":…}} for a route it does not have.
func gateErrText(body map[string]any) string {
	if em, ok := body["error"].(map[string]any); ok {
		if m := str(em, "message"); m != "" {
			return m
		}
	}
	if s, ok := body["error"].(string); ok && s != "" {
		return s
	}
	if em, ok := body["errors"].(map[string]any); ok {
		if d := str(em, "detail"); d != "" {
			return d
		}
	}
	return refusalText(body)
}

func factoryGateList(env *factoryEnv, state, kind string, jsonOut bool, out, errOut io.Writer) error {
	apiPath := fmt.Sprintf("/api/v1/sync/gates?system_id=%d", env.SystemID)
	if state != "" {
		apiPath += "&state=" + url.QueryEscape(state)
	}
	status, body, err := env.call("GET", apiPath, nil)
	if err != nil {
		return err
	}
	if status != 200 {
		return fmt.Errorf("server %d: %s", status, gateErrText(body))
	}
	gates, _ := dataOf(body)["gates"].([]any)
	shown := filterGatesByKind(gates, kind)
	start, end := gatesPage.window(len(shown), jsonOut)
	if jsonOut {
		return emitPagedJSON(out, "gates", shown[start:end], end, len(shown))
	}
	// History (answered|dismissed|superseded|all) renders each gate's stored
	// state; the open queue (default, or explicit --state open) is unchanged.
	history := state != "" && state != "open"
	if len(shown) == 0 {
		switch {
		case history && state == "all":
			fmt.Fprint(out, "no gates\n")
		case history:
			fmt.Fprintf(out, "no %s gates\n", state)
		case kind != "" && len(gates) > 0:
			// A queue that holds gates of other kinds is not clear.
			fmt.Fprint(out, gateQueueFooter(0, gateKindBreakdown(gates), kind))
		default:
			fmt.Fprint(out, "✓ no open gates — the queue is clear\n")
		}
		return nil
	}
	if history {
		for _, g := range shown[start:end] {
			m, _ := g.(map[string]any)
			renderGateHistory(out, m)
		}
		if state == "all" {
			fmt.Fprintf(out, "\n%d gate(s)\n", len(shown))
		} else {
			fmt.Fprintf(out, "\n%d %s gate(s)\n", len(shown), state)
		}
		fmt.Fprint(out, pageFooter(start, end, len(shown)))
		return nil
	}
	for _, g := range shown[start:end] {
		m, _ := g.(map[string]any)
		fmt.Fprintf(out, "\n%s  [%s]  %s\n", str(m, "external_id"), str(m, "kind"), str(m, "title"))
		if rec := str(m, "recommendation"); rec != "" {
			fmt.Fprintf(out, "  recommends: %.120s\n", rec)
		}
		if options, ok := m["options"].([]any); ok {
			for _, o := range options {
				om, _ := o.(map[string]any)
				fmt.Fprintf(out, "  - %s: %.100s\n", str(om, "key"), str(om, "label"))
			}
		}
	}
	fmt.Fprint(out, gateQueueFooter(len(shown), gateKindBreakdown(gates), kind))
	fmt.Fprint(out, pageFooter(start, end, len(shown)))
	return nil
}

func gateBriefLines(g map[string]any) []string {
	brief, _ := g["brief"].(map[string]any)
	var lines []string
	for _, field := range []struct{ key, label string }{
		{"what", "What"},
		{"why_now", "Why now"},
		{"changes_if_approved", "Changes if approved"},
		{"risk_if_wrong", "Risk if wrong"},
		{"recommendation", "Recommendation"},
	} {
		if value := str(brief, field.key); strings.TrimSpace(value) != "" {
			lines = append(lines, field.label+": "+value)
		}
	}
	return lines
}

// renderGate shows the decision brief and stored gate metadata; -v adds body_md.
func renderGate(out io.Writer, g map[string]any) {
	fmt.Fprintf(out, "\n%s  [%s]  %s\n", str(g, "external_id"), str(g, "kind"), str(g, "title"))
	fmt.Fprintf(out, "  state: %s", str(g, "state"))
	if a := str(g, "applied_state"); a != "" {
		fmt.Fprintf(out, "   applied: %s", a)
	}
	fmt.Fprintln(out)
	if lines := gateBriefLines(g); len(lines) > 0 {
		fmt.Fprintln(out, "  Brief:")
		for _, line := range lines {
			fmt.Fprintf(out, "  %s\n", strings.ReplaceAll(line, "\n", "\n    "))
		}
	}
	if ans := str(g, "answer"); ans != "" {
		fmt.Fprintf(out, "  answer: %s\n", ans)
	}
	if keys := gateOptionKeys(g); keys != "" {
		fmt.Fprintf(out, "  chosen: %s\n", keys)
	}
	if src := str(g, "source_tag"); src != "" {
		fmt.Fprintf(out, "  source: %s\n", src)
	}
	if who := gateAnswerer(g); who != "" {
		if at := str(g, "answered_at"); at != "" {
			fmt.Fprintf(out, "  answered by %s at %s\n", who, at)
		} else {
			fmt.Fprintf(out, "  answered by %s\n", who)
		}
	}
	if isTraceGate(g) {
		fmt.Fprintf(out, "  verdict: %s\n", fieldOr(g, "verdict", "—"))
		if who := gateEvaluator(g); who != "" {
			if at := str(g, "evaluated_at"); at != "" {
				fmt.Fprintf(out, "  evaluated by %s at %s\n", who, at)
			} else {
				fmt.Fprintf(out, "  evaluated by %s\n", who)
			}
		}
		// application_revision is the recording HEAD, not a test run.
		fmt.Fprintf(out, "  recorded at: %s\n", fieldOr(g, "application_revision", "—"))
		fmt.Fprintf(out, "  pinned to: %s\n", gatePin(g))
		fmt.Fprintf(out, "  scope: %s\n", idListOr(g, "exact_scope", "—"))
		fmt.Fprintf(out, "  purpose: %s\n", fieldOr(g, "purpose", "—"))
		fmt.Fprintf(out, "  transition: %s\n", fieldOr(g, "transition", "—"))
		fmt.Fprintf(out, "  prerequisites: %s\n", idListOr(g, "prerequisite_gate_external_ids", "none"))
		if _, has := g["predecessor_external_id"]; has {
			fmt.Fprintf(out, "  predecessor: %s\n", servedOr(g, "predecessor_external_id", "—"))
		}
		if _, has := g["successor_external_id"]; has {
			fmt.Fprintf(out, "  successor: %s\n", servedOr(g, "successor_external_id", "—"))
		}
	}
	if verbose {
		if body := str(g, "body_md"); body != "" {
			fmt.Fprintf(out, "\n%s\n", strings.TrimRight(body, "\n"))
		}
	}
}

// isTraceGate: the store's gate_class, or a verdict on a gate that predates it.
func isTraceGate(g map[string]any) bool {
	return str(g, "gate_class") == "trace" || str(g, "verdict") != ""
}

// gateEvaluator mirrors gateAnswerer over the evaluator quartet.
func gateEvaluator(g map[string]any) string {
	if n := str(g, "evaluator_name"); n != "" {
		return n
	}
	kind := str(g, "evaluator_kind")
	if slug := str(g, "evaluator_agent_slug"); slug != "" {
		if kind != "" {
			return kind + " " + slug
		}
		return slug
	}
	if id := numericID(g["evaluator_user_id"]); id != "" {
		return "user #" + id
	}
	return kind
}

// gatePin names the fingerprint a trace is pinned to and what that
// fingerprint is: a packet aggregate for the cold-review, entry and
// completion purposes, the item's content hash for lower and upper.
func gatePin(g map[string]any) string {
	fp := str(g, "evaluated_scope_fingerprint")
	if fp == "" {
		return "—"
	}
	return fp + " (" + gatePinClass(str(g, "purpose")) + ")"
}

func gatePinClass(purpose string) string {
	switch purpose {
	case "cold_review", "entry", "completion":
		return "packet aggregate"
	case "lower", "upper":
		return "content hash"
	}
	return "fingerprint"
}

// renderGateHistory is one line-group in a --state listing: the gate, its stored
// state, and — when it carries an answer — its source and answerer.
func renderGateHistory(out io.Writer, g map[string]any) {
	fmt.Fprintf(out, "\n%s  [%s]  %s\n", str(g, "external_id"), str(g, "kind"), str(g, "title"))
	fmt.Fprintf(out, "  state: %s\n", str(g, "state"))
	if src := str(g, "source_tag"); src != "" {
		fmt.Fprintf(out, "  source: %s\n", src)
	}
	if who := gateAnswerer(g); who != "" {
		fmt.Fprintf(out, "  answered by %s\n", who)
	}
}

// gateAnswerer prefers the resolved name; otherwise the answerer kind with the
// agent slug, or the numeric user id the server serves when a user resolves to
// no membership name — never discarding that id down to a bare kind.
func gateAnswerer(g map[string]any) string {
	if n := str(g, "answerer_name"); n != "" {
		return n
	}
	kind := str(g, "answerer_kind")
	if slug := str(g, "answerer_agent_slug"); slug != "" {
		if kind != "" {
			return kind + " " + slug
		}
		return slug
	}
	if id := answererUserID(g); id != "" {
		return "user #" + id
	}
	return kind
}

// answererUserID formats the numeric answerer_user_id the server serves when a
// user (e.g. a superuser) resolves to no membership name. A JSON number decodes
// as float64 without UseNumber, which the string-only str() silently drops, so
// format it as a base-10 integer — no trailing ".0", no scientific notation for
// a large id — keeping the identity visible instead of collapsing to "human".
func answererUserID(g map[string]any) string {
	return numericID(g["answerer_user_id"])
}

// numericID formats a served numeric user id (REQ-CROSS-425 reuses it for the
// evaluator); an absent or non-numeric value is "".
func numericID(raw any) string {
	switch v := raw.(type) {
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case json.Number:
		return v.String()
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	}
	return ""
}

func gateOptionKeys(g map[string]any) string {
	keys, _ := g["chosen_option_keys"].([]any)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if s, ok := k.(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// emitGatesJSON writes {"gates":[…]} to out, an empty list as [] and never null,
// so a consumer's JSON.parse on stdout cannot throw.
func emitGatesJSON(out io.Writer, gates []any) error {
	if gates == nil {
		gates = []any{}
	}
	return emitJSONEnvelope(out, "gates", gates)
}

func emitJSONEnvelope(out io.Writer, key string, val any) error {
	blob, err := json.MarshalIndent(map[string]any{key: val}, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(out, string(blob))
	return nil
}

// kindCount is one kind of gate and how many of it are open.
type kindCount struct {
	kind string
	n    int
}

// gateKindBreakdown counts the queue by kind, largest first, ties by name so the
// order is stable between runs.
func gateKindBreakdown(gates []any) []kindCount {
	n := map[string]int{}
	for _, g := range gates {
		m, _ := g.(map[string]any)
		n[str(m, "kind")]++
	}
	out := make([]kindCount, 0, len(n))
	for k, c := range n {
		out = append(out, kindCount{kind: k, n: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].n != out[j].n {
			return out[i].n > out[j].n
		}
		return out[i].kind < out[j].kind
	})
	return out
}

func filterGatesByKind(gates []any, kind string) []any {
	if kind == "" {
		return gates
	}
	out := []any{}
	for _, g := range gates {
		m, _ := g.(map[string]any)
		if str(m, "kind") == kind {
			out = append(out, g)
		}
	}
	return out
}

// gateQueueFooter closes the listing. A bare total misleads: a queue of 148 is
// read as an approval backlog when 0 of it is approvals and the rest are
// questions and product decisions — different work at a different cadence. The
// kinds are named so the number means something, and a filtered view still
// reports the unfiltered total so narrowing cannot hide the queue.
func gateQueueFooter(shown int, breakdown []kindCount, filter string) string {
	total := 0
	parts := make([]string, 0, len(breakdown))
	for _, kc := range breakdown {
		total += kc.n
		parts = append(parts, fmt.Sprintf("%d %s", kc.n, kc.kind))
	}
	answer := "answer with: modernpath factory answer <id> --text \"…\" [--options k1,k2]"

	if filter == "" {
		return fmt.Sprintf("\n%d open (%s) — %s\n", total, strings.Join(parts, " · "), answer)
	}
	// Not "the queue is clear": there is a queue, it just holds nothing of the
	// kind that was asked for. Reporting it as clear would be this row's own
	// defect one level down.
	if shown == 0 {
		return fmt.Sprintf("\nno open %s — %d open of other kinds (%s)\n", filter, total, strings.Join(parts, " · "))
	}
	return fmt.Sprintf("\n%d %s of %d open (%s) — %s\n", shown, filter, total, strings.Join(parts, " · "), answer)
}
