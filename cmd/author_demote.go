package cmd

// REQ-CROSS-421 (EPIC-CLI-022): `author demote` — the sanctioned way to reopen
// a delivered item. It opens the demotion gate PROCESS.md §Attributable
// demotions names (purpose demotion, the destination by basis, the human's
// USER: reason, no prerequisite trace) and, after the human's answer, applies
// it: the server runs the invalidation, retires the item's own entry on a
// demotion to PROPOSED (a defect keeps it, REQ-CROSS-435), and moves the user
// requirement and the epic that follow on the same gate
// (REQ-CROSS-418/419). The dedicated flag set never rebinds `author advance`'s
// --to (author_advance_flags_test.go documents the hazard).
//
// REQ-CROSS-462 (EPIC-CLI-DELTA): --ids/--file open one gate over several
// items of one starting state, at the next free id of the DEMOTE-<first id>
// series, and --apply advances every item of the answered gate.

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

var demotionDestinations = map[string]string{
	"reversed-decision": "PROPOSED",
	"defect":            "IN_PROGRESS",
	"superseded":        "OBSOLETE",
}

// demoteTitleLimit is the gate title column's width.
const demoteTitleLimit = 255

type demoteOpts struct {
	id, kind, to, basis, reason, supersededBy, gateID string
	// ids is a batch (REQ-CROSS-462); empty means the single id.
	ids []string
}

var (
	demoteTo           string
	demoteBasis        string
	demoteReason       string
	demoteSupersededBy string
	demoteGateID       string
	demoteKind         string
	demoteApply        bool
	demoteIDs          string
	demoteFile         string
)

var authorDemoteCmd = &cobra.Command{
	Use:   "demote [<external-id>] [--ids A,B,C | --file <ids.txt>]",
	Short: "Reopen delivered items on one attributable demotion gate: --to PROPOSED|IN_PROGRESS|OBSOLETE --basis reversed-decision|defect|superseded --reason USER:…; --apply after the answer",
	Long: `Send delivered work back with one attributable decision (PROCESS.md
§Attributable demotions). Each item must be IN_REVIEW or DONE; its owning
epic and the user requirement that requires it follow on the same gate, and
the siblings the decision did not touch keep their evidence.

Open (no --apply): name one item, or several with --ids A,B,C or --file
(one id per line). It refuses locally — before any request — without a
--reason that is a USER: source (USER:<date>:<why>), when the basis does not
match the destination (reversed-decision -> PROPOSED, defect -> IN_PROGRESS,
superseded -> OBSOLETE, which also needs --superseded-by <id>); then reads
each item's current status from the store as the FROM and refuses, before
any write, an item that is not IN_REVIEW or DONE, and a batch whose items are
not all in one state (it names both groups: run the group holding user
requirements first). Then it posts one gate (purpose demotion,
approval_request, transition <from>-><to>, every item as its exact scope,
approve/decline options, a brief in plain words listing every item and the
user requirements and epics that follow, the reason and the basis as
sources). The gate id is DEMOTE-<first id>, or the next free id in its
DEMOTE-<first id>-R<n> series when the earlier gates are closed or withdrawn;
an open or answered gate in the series is refused and named. --gate-id names
the id yourself. A demotion gate names no prerequisite trace and needs none:
it is an attributable decision, not a fingerprint check. --kind epic demotes
epics.

Apply (--apply): after the human answers approve (Mission Control, or
factory answer <gate> --options approve --text "USER:<date>: …"), reads the
answered gate and advances every item it names — user requirements first,
then the rest — re-reading the gate before each item; the gate id, its
current fingerprint and the answer are read from the store, never typed.
--gate-id <gate> --apply takes no id; <id> --apply without --gate-id applies
the newest gate in the DEMOTE-<id> series. An item with its own applied entry
on the gate is skipped. An item a follow on another gate already moved out
of the starting state is reported with that gate and passed over; the gate
cannot then close, and the output names author gate-withdraw. Any other
refusal stops the run, lists what was applied and what remains, and exits
non-zero; a re-run resumes. It prints the server's transition basis, the
dependents that followed and the next step: for PROPOSED, re-plan and
cold-review, then process enter opens the successor entry gate; for
IN_PROGRESS, rebuild red-first (rdd-build) and process complete re-completes
the epic; for OBSOLETE, the epic's completion excludes the item.

A delegated agent is denied this verb by the subagent guard; the permission
placement is ask, beside process supersede and process reenter.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if demoteApply {
			if demoteIDs != "" || demoteFile != "" {
				return fmt.Errorf("--apply reads the items from the gate — pass --gate-id <gate> (or the id the gate was opened for), not --ids or --file")
			}
			if len(args) == 0 && demoteGateID == "" {
				return fmt.Errorf("--apply needs the gate: pass --gate-id <gate>, or the id the gate was opened for")
			}
		}
		var ids []string
		if !demoteApply {
			var err error
			if ids, err = demoteBatchIDs(args, demoteIDs, demoteFile); err != nil {
				return err
			}
		}
		env, err := authorEnv()
		if err != nil {
			return err
		}
		if demoteApply {
			id := ""
			if len(args) == 1 {
				id = args[0]
			}
			return authorDemoteApply(env, id, demoteKind, demoteGateID)
		}
		return authorDemoteOpen(env, demoteOpts{
			id: ids[0], ids: ids, kind: demoteKind, to: demoteTo, basis: demoteBasis, reason: demoteReason,
			supersededBy: demoteSupersededBy, gateID: demoteGateID,
		})
	},
}

func init() {
	authorDemoteCmd.Flags().StringVar(&demoteTo, "to", "", "the destination: PROPOSED (reversed-decision), IN_PROGRESS (defect) or OBSOLETE (superseded)")
	authorDemoteCmd.Flags().StringVar(&demoteBasis, "basis", "", "the basis: reversed-decision | defect | superseded")
	authorDemoteCmd.Flags().StringVar(&demoteReason, "reason", "", "the human's reason as a USER: source (USER:<date>:<why>)")
	authorDemoteCmd.Flags().StringVar(&demoteSupersededBy, "superseded-by", "", "the record that supersedes the item (required with --basis superseded)")
	authorDemoteCmd.Flags().StringVar(&demoteGateID, "gate-id", "", "the gate id to open or apply (default: the next free id in the DEMOTE-<first id> series; with --apply, the newest)")
	authorDemoteCmd.Flags().StringVar(&demoteKind, "kind", "requirement", "requirement | epic")
	authorDemoteCmd.Flags().BoolVar(&demoteApply, "apply", false, "apply the answered demotion gate: advance every item on it and report what followed")
	authorDemoteCmd.Flags().StringVar(&demoteIDs, "ids", "", "several items for one gate, comma-separated (all IN_REVIEW or all DONE)")
	authorDemoteCmd.Flags().StringVar(&demoteFile, "file", "", "a file naming the items for one gate, one id per line")
}

// demoteBatchIDs is the items to open one gate over: the positional id, or
// --ids, or --file — exactly one of them. Duplicates are dropped.
func demoteBatchIDs(args []string, idsFlag, file string) ([]string, error) {
	sources := 0
	var raw []string
	if len(args) == 1 {
		sources++
		raw = append(raw, args[0])
	}
	if idsFlag != "" {
		sources++
		raw = append(raw, strings.Split(idsFlag, ",")...)
	}
	if file != "" {
		sources++
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
				raw = append(raw, line)
			}
		}
	}
	if sources != 1 {
		return nil, fmt.Errorf("name the items one way: an id, --ids A,B,C or --file <ids.txt>; nothing was opened")
	}
	seen := map[string]bool{}
	ids := []string{}
	for _, id := range raw {
		if id = strings.TrimSpace(id); id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no item ids were given; nothing was opened")
	}
	return ids, nil
}

// demoteLocalChecks are the refusals made before any request.
func demoteLocalChecks(o demoteOpts) error {
	if !strings.HasPrefix(o.reason, "USER:") {
		return fmt.Errorf("a demotion carries the human's reason as a USER: source — pass --reason USER:<date>:<why>; nothing was opened")
	}
	destination, known := demotionDestinations[o.basis]
	if !known {
		return fmt.Errorf("--basis %q is not a demotion basis — one of reversed-decision (to PROPOSED), defect (to IN_PROGRESS) or superseded (to OBSOLETE); nothing was opened", o.basis)
	}
	if o.to != destination {
		return fmt.Errorf("basis %s sends an item to %s, not %s — reversed-decision -> PROPOSED, defect -> IN_PROGRESS, superseded -> OBSOLETE; nothing was opened", o.basis, destination, presentPin(o.to))
	}
	if o.basis == "superseded" && o.supersededBy == "" {
		return fmt.Errorf("a superseded demotion names the record that supersedes the item — pass --superseded-by <id>; nothing was opened")
	}
	return nil
}

// authorDemoteOpen posts the demotion gate after the local checks.
func authorDemoteOpen(env *factoryEnv, o demoteOpts) error {
	if err := demoteLocalChecks(o); err != nil {
		return err
	}
	kind := o.kind
	if kind == "" {
		kind = "requirement"
	}
	ids := o.ids
	if len(ids) == 0 {
		ids = []string{o.id}
	}
	o.id = ids[0]
	records, err := readDemoteRecords(env, kind, ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, ok := records[id]; !ok {
			return fmt.Errorf("%s is not a%s the store serves", id, map[bool]string{true: "n epic", false: " requirement"}[kind == "epic"])
		}
	}
	from, err := batchStartingState(ids, records)
	if err != nil {
		return err
	}
	base := "DEMOTE-" + o.id
	gateID := o.gateID
	if gateID != "" {
		if err := gateIDFree(env, gateID); err != nil {
			return err
		}
	} else if gateID, _, err = freeGateID(env, base); err != nil {
		return err
	}

	brief := demotionBrief(o, from)
	if len(ids) > 1 {
		urs, epics := demotionFollowers(env, kind, ids, records)
		brief = batchDemotionBrief(o, ids, from, urs, epics)
	}
	fields := map[string]any{
		"title":       demoteTitle(ids, o.to, o.basis),
		"gate_kind":   "approval_request",
		"purpose":     "demotion",
		"transition":  from + "->" + o.to,
		"exact_scope": ids,
		"basis":       o.basis,
		"options": []map[string]any{
			{"key": "approve", "label": "Approve the demotion"},
			{"key": "decline", "label": "Keep it as delivered"},
		},
		"recommended_option_key": "approve",
		"brief":                  brief,
		"sources":                []map[string]any{{"kind": "user", "ref": o.reason}},
	}
	if o.supersededBy != "" {
		fields["superseded_by"] = o.supersededBy
	}
	label := "item"
	if len(ids) > 1 {
		label = "items"
	}
	fmt.Printf("demotion gate %s\n  %s: %s (%s -> %s, basis %s)\n  reason: %s\n", gateID, label, strings.Join(ids, ", "), from, o.to, o.basis, o.reason)
	if _, err := authorCreate(env, "gate", gateID, fields); err != nil {
		return err
	}
	apply := fmt.Sprintf("author demote %s --apply", o.id)
	if len(ids) > 1 || gateID != base {
		apply = fmt.Sprintf("author demote --gate-id %s --apply", gateID)
	}
	printInfo("answer it in Mission Control or with `factory answer %s --options approve --text \"USER:<date>: …\"`, then run `%s`", gateID, apply)
	return nil
}

// batchStartingState is the one state every item is in; it refuses an item
// that is not delivered and a batch that mixes states, naming the groups with
// the one holding user requirements first (D4: a batch is never split).
func batchStartingState(ids []string, records map[string]demoteRecord) (string, error) {
	var undelivered []string
	groups := map[string][]string{}
	var states []string
	for _, id := range ids {
		st := records[id].status
		if st != "IN_REVIEW" && st != "DONE" {
			undelivered = append(undelivered, fmt.Sprintf("%s is %s", id, presentPin(st)))
			continue
		}
		if _, seen := groups[st]; !seen {
			states = append(states, st)
		}
		groups[st] = append(groups[st], id)
	}
	if len(undelivered) > 0 {
		return "", fmt.Errorf("only delivered work is demoted: %s, not IN_REVIEW or DONE; nothing was opened", strings.Join(undelivered, ", "))
	}
	if len(states) == 1 {
		return states[0], nil
	}
	holdsUR := func(st string) bool {
		for _, id := range groups[st] {
			if records[id].ur {
				return true
			}
		}
		return false
	}
	sort.SliceStable(states, func(i, j int) bool {
		if holdsUR(states[i]) != holdsUR(states[j]) {
			return holdsUR(states[i])
		}
		return states[i] == "IN_REVIEW"
	})
	parts, runs := []string{}, []string{}
	for _, st := range states {
		parts = append(parts, fmt.Sprintf("%s: %s", st, strings.Join(groups[st], ", ")))
		runs = append(runs, fmt.Sprintf("`author demote --ids %s …`", strings.Join(groups[st], ",")))
	}
	return "", fmt.Errorf("a batch reopen holds one starting state, and these items are in %d — %s; run them as separate batches, the group holding user requirements first: %s; nothing was opened",
		len(states), strings.Join(parts, "; "), strings.Join(runs, ", then "))
}

// demoteTitle names the items within the title column's limit: every id when
// they fit, else the first ids and "and N more".
func demoteTitle(ids []string, to, basis string) string {
	suffix := fmt.Sprintf(" to %s — %s", to, basis)
	n := len(ids)
	for k := n; k >= 1; k-- {
		var names string
		switch {
		case k == n && n == 1:
			names = ids[0]
		case k == n:
			names = strings.Join(ids[:n-1], ", ") + " and " + ids[n-1]
		default:
			names = fmt.Sprintf("%s and %d more", strings.Join(ids[:k], ", "), n-k)
		}
		if title := "Demote " + names + suffix; utf8.RuneCountInString(title) <= demoteTitleLimit || k == 1 {
			return title
		}
	}
	return "Demote" + suffix
}

// listIDs renders ids as "A", "A and B", "A, B and C".
func listIDs(ids []string) string {
	switch len(ids) {
	case 0:
		return ""
	case 1:
		return ids[0]
	}
	return strings.Join(ids[:len(ids)-1], ", ") + " and " + ids[len(ids)-1]
}

func demotionBrief(o demoteOpts, from string) map[string]any {
	what := map[string]string{
		"PROPOSED":    fmt.Sprintf("Send %s back to planning: the decision it rests on is reversed.", o.id),
		"IN_PROGRESS": fmt.Sprintf("Send %s back to build: its delivered behaviour is defective.", o.id),
		"OBSOLETE":    fmt.Sprintf("Retire %s: it is superseded by %s.", o.id, o.supersededBy),
	}[o.to]
	return map[string]any{
		"what":                what,
		"why_now":             fmt.Sprintf("%s is %s and %s. %s", o.id, from, demotionCause[o.to], o.reason),
		"changes_if_approved": demotionChanges[o.to],
		"risk_if_wrong":       "Reversible: a demoted item re-enters or re-completes through the ordinary ceremony; declining keeps it as delivered.",
		"recommendation":      "approve — the reason is recorded on the gate and the loop reconciles the consequences.",
	}
}

var demotionCause = map[string]string{
	"PROPOSED":    "the decision has changed",
	"IN_PROGRESS": "a defect was found in what shipped",
	"OBSOLETE":    "a newer record replaces it",
}

var demotionChanges = map[string]string{
	"PROPOSED":    "The item returns to PROPOSED; its evidence, its traces (the plan's cold review included) and its own entry approval are invalidated; the user requirement that requires it and its epic follow — the epic back to planning. Siblings keep their evidence and approval.",
	"IN_PROGRESS": "The item returns to IN_PROGRESS; its own evidence, lower trace and the completion traces are invalidated while the epic's cold review and the user requirement's upper validation stand; the user requirement and the epic follow to IN_PROGRESS. Siblings keep their evidence.",
	"OBSOLETE":    "The item becomes OBSOLETE with the replacement linked; the evidence that stood on it is invalidated; the user requirement and the epic that were done return to IN_PROGRESS; the epic's completion excludes the item.",
}

// batchDemotionBrief lists every item and the combined effect: the user
// requirements and epics that follow on the one answer.
func batchDemotionBrief(o demoteOpts, ids []string, from string, urs, epics []string) map[string]any {
	items := listIDs(ids)
	what := map[string]string{
		"PROPOSED":    fmt.Sprintf("Send %d items back to planning — %s: the decision they rest on is reversed.", len(ids), items),
		"IN_PROGRESS": fmt.Sprintf("Send %d items back to build — %s: their delivered behaviour is defective.", len(ids), items),
		"OBSOLETE":    fmt.Sprintf("Retire %d items — %s: they are superseded by %s.", len(ids), items, o.supersededBy),
	}[o.to]
	var follow []string
	if len(urs) > 0 {
		follow = append(follow, "user requirements "+listIDs(urs))
	}
	if len(epics) > 0 {
		follow = append(follow, "epics "+listIDs(epics))
	}
	if len(follow) > 0 {
		what += " Following on the same answer: " + strings.Join(follow, "; ") + "."
	}
	return map[string]any{
		"what":                what,
		"why_now":             fmt.Sprintf("%s are %s and %s. %s", items, from, demotionCause[o.to], o.reason),
		"changes_if_approved": "For each item: " + demotionChanges[o.to],
		"risk_if_wrong":       "Reversible: a demoted item re-enters or re-completes through the ordinary ceremony; declining keeps every item as delivered.",
		"recommendation":      "approve — the reason is recorded on the gate, every item is applied from the one answer, and the loop reconciles the consequences.",
	}
}

// demoteRecord is what the open and apply paths read about one item.
type demoteRecord struct {
	status  string
	ur      bool
	parents []string
}

// readDemoteRecords reads the items' current status (and, for requirements,
// whether each is a user requirement and its parents) from one list read —
// the same reads working-set pull renders — since no single-record status
// read exists. An id the store does not serve is absent from the map.
func readDemoteRecords(env *factoryEnv, kind string, ids []string) (map[string]demoteRecord, error) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	out := map[string]demoteRecord{}
	if kind == "epic" {
		epics, err := fetchList(env, fmt.Sprintf("/api/v1/sync/epics?system_id=%d", env.SystemID), "epics")
		if err != nil {
			return nil, err
		}
		for _, e := range epics {
			m, _ := e.(map[string]any)
			for _, key := range []string{str(m, "code"), str(m, "external_id")} {
				if key != "" && want[key] {
					out[key] = demoteRecord{status: str(m, "process_status")}
				}
			}
		}
		return out, nil
	}
	srs, urs, err := fetchRequirementLists(env, fmt.Sprintf("/api/v1/sync/requirements?system_id=%d", env.SystemID))
	if err != nil {
		return nil, err
	}
	for i, list := range [][]any{srs, urs} {
		for _, r := range list {
			m, _ := r.(map[string]any)
			if id := str(m, "external_id"); want[id] {
				out[id] = demoteRecord{status: str(m, "work_status"), ur: i == 1, parents: stringSlice(m["parent_external_ids"])}
			}
		}
	}
	return out, nil
}

// demotionFollowers is the user requirements and epics a batch moves besides
// its items: the parents of its system requirements and the epics that hold
// any item. A failed epic read leaves the epics out; the brief is advisory.
func demotionFollowers(env *factoryEnv, kind string, ids []string, records map[string]demoteRecord) (urs, epics []string) {
	if kind == "epic" {
		return nil, nil
	}
	inBatch := map[string]bool{}
	for _, id := range ids {
		inBatch[id] = true
	}
	seen := map[string]bool{}
	for _, id := range ids {
		for _, p := range records[id].parents {
			if !inBatch[p] && !seen[p] {
				seen[p] = true
				urs = append(urs, p)
			}
		}
	}
	list, err := fetchList(env, fmt.Sprintf("/api/v1/sync/epics?system_id=%d", env.SystemID), "epics")
	if err != nil {
		return urs, nil
	}
	for _, e := range list {
		m, _ := e.(map[string]any)
		for _, member := range servedMembers(m, nil) {
			if inBatch[member] {
				epics = append(epics, firstNonEmpty(str(m, "code"), str(m, "external_id")))
				break
			}
		}
	}
	return urs, epics
}

// newestGateInSeries is the last id of the <base>, <base>-R2… series a gate
// holds; empty when none does.
func newestGateInSeries(env *factoryEnv, base string) (string, error) {
	newest := ""
	for n := 1; n <= 50; n++ {
		id := base
		if n > 1 {
			id = fmt.Sprintf("%s-R%d", base, n)
		}
		status, body, err := env.call("GET",
			fmt.Sprintf("/api/v1/sync/gates/%s?system_id=%d", url.PathEscape(id), env.SystemID), nil)
		if err != nil {
			return "", err
		}
		if status == 200 {
			newest = id
			continue
		}
		if em, ok := body["error"].(map[string]any); ok && status == 404 && strings.HasPrefix(str(em, "message"), "no such gate") {
			return newest, nil
		}
		return "", gateShowError(status, body, id)
	}
	return newest, nil
}

// readDemotionGate reads one gate by id.
func readDemotionGate(env *factoryEnv, gateID, id string) (map[string]any, error) {
	status, body, err := env.call("GET",
		fmt.Sprintf("/api/v1/sync/gates/%s?system_id=%d", url.PathEscape(gateID), env.SystemID), nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		if err := gateShowError(status, body, gateID); err != nil && strings.Contains(err.Error(), "does not exist") {
			return nil, fmt.Errorf("no demotion gate %s — open it first with `author demote %s --to … --basis … --reason USER:…`", gateID, orMarker(id, "<id>"))
		}
		return nil, gateShowError(status, body, gateID)
	}
	gate, _ := dataOf(body)["gate"].(map[string]any)
	return gate, nil
}

// ownAppliedEntry reports whether the gate records the item's own application
// (a follow entry, basis demotion_follow, is not the item's own).
func ownAppliedEntry(gate map[string]any, id string) bool {
	entries, _ := gate["applied_transitions"].([]any)
	for _, e := range entries {
		m, _ := e.(map[string]any)
		if str(m, "external_id") == id && str(m, "basis") != "demotion_follow" {
			return true
		}
	}
	return false
}

// followingGate is the gate whose follow moved the item, read from the gate
// list; empty when no gate records one.
func followingGate(env *factoryEnv, gates *[]any, id string) (string, error) {
	if *gates == nil {
		list, err := fetchList(env, fmt.Sprintf("/api/v1/sync/gates?system_id=%d&state=all", env.SystemID), "gates")
		if err != nil {
			return "", err
		}
		*gates = list
	}
	for _, g := range *gates {
		gm, _ := g.(map[string]any)
		entries, _ := gm["applied_transitions"].([]any)
		for _, e := range entries {
			m, _ := e.(map[string]any)
			if str(m, "external_id") == id && str(m, "basis") == "demotion_follow" {
				return str(gm, "external_id"), nil
			}
		}
	}
	return "", nil
}

// authorDemoteApply advances every item of the answered demotion gate: the
// named gate, or the newest in the DEMOTE-<id> series.
func authorDemoteApply(env *factoryEnv, id, kind, gateID string) error {
	if kind == "" {
		kind = "requirement"
	}
	if gateID == "" {
		newest, err := newestGateInSeries(env, "DEMOTE-"+id)
		if err != nil {
			return err
		}
		if newest == "" {
			return fmt.Errorf("no demotion gate DEMOTE-%s — open it first with `author demote %s --to … --basis … --reason USER:…`", id, id)
		}
		gateID = newest
	}
	gate, err := readDemotionGate(env, gateID, id)
	if err != nil {
		return err
	}
	switch str(gate, "state") {
	case "open":
		return fmt.Errorf("%s is open — answer it first (Mission Control, or `factory answer %s --options approve --text \"USER:<date>: …\"`); nothing was applied", gateID, gateID)
	case "answered":
	case "closed":
		return fmt.Errorf("%s is closed — its answer was already applied; nothing to do", gateID)
	default:
		return fmt.Errorf("%s is %s — a demotion is applied from an answered gate; nothing was applied", gateID, presentPin(str(gate, "state")))
	}
	if !contains(stringSlice(gate["chosen_option_keys"]), "approve") {
		return fmt.Errorf("%s was answered %s, not approve — the item stays as delivered; nothing was applied", gateID, presentPin(strings.Join(stringSlice(gate["chosen_option_keys"]), ",")))
	}
	from, to, ok := strings.Cut(str(gate, "transition"), "->")
	if !ok {
		return fmt.Errorf("%s carries no FROM->TO transition — not a demotion gate this verb can apply", gateID)
	}
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	scope := stringSlice(gate["exact_scope"])
	if len(scope) == 0 && id != "" {
		scope = []string{id}
	}
	records, err := readDemoteRecords(env, kind, scope)
	if err != nil {
		return err
	}
	// User requirements first: a system requirement applied first would move
	// its user requirement as a follow and skip the UR's own invalidation.
	order := append([]string{}, scope...)
	sort.SliceStable(order, func(i, j int) bool { return records[order[i]].ur && !records[order[j]].ur })

	var applied, passed []string
	var gates []any
	for i, item := range order {
		if i > 0 {
			if gate, err = readDemotionGate(env, gateID, id); err != nil {
				return demoteStopped(gateID, item, err, applied, order[i:])
			}
			if str(gate, "state") == "closed" {
				break
			}
		}
		if ownAppliedEntry(gate, item) {
			printInfo("skipped: %s is already applied on %s", item, gateID)
			continue
		}
		if rec, known := records[item]; known && rec.status != from {
			other, err := followingGate(env, &gates, item)
			if err != nil {
				return demoteStopped(gateID, item, err, applied, order[i:])
			}
			if other != "" {
				printWarning("passed over: %s is already %s — a follow on %s moved it", item, rec.status, other)
				passed = append(passed, item)
				continue
			}
		}
		// The gate read serves `fingerprint` — the shadow value `advance
		// --gate-fingerprint` guards on; the row's content_fingerprint is the
		// fallback for a server that predates it (F-CLI022-PR2-03).
		fp := firstNonEmpty(str(gate, "fingerprint"), str(gate, "content_fingerprint"))
		if err := authorAdvance(env, kind, item, to, from, gateID, fp, "approve", ""); err != nil {
			if len(order) == 1 {
				return err
			}
			return demoteStopped(gateID, item, err, applied, order[i:])
		}
		applied = append(applied, item)
	}
	items := listIDs(scope)
	switch to {
	case "PROPOSED":
		printInfo("next: re-plan %s and record a fresh cold review; `process enter` then opens its successor entry gate (the siblings' approval under the shared gate stands)", items)
	case "IN_PROGRESS":
		printInfo("next: rebuild %s red-first (rdd-build); `process complete` re-completes the epic on the siblings' current evidence", items)
	case "OBSOLETE":
		printInfo("next: nothing for %s — the epic's completion excludes it; its user requirement and epic rebuild on what replaces it", items)
	}
	if len(passed) > 0 {
		if gate, err := readDemotionGate(env, gateID, id); err == nil && str(gate, "state") != "closed" {
			printWarning("%s cannot close: %s was moved by another gate and has no entry of its own here — withdraw it with `author gate-withdraw %s --reason \"USER:<date>: …\"`", gateID, strings.Join(passed, ", "), gateID)
		}
	}
	return nil
}

// demoteStopped is the refusal that stops a batch apply: what was applied,
// what remains, and how to resume.
func demoteStopped(gateID, item string, cause error, applied, remaining []string) error {
	return fmt.Errorf("%s was refused: %v — stopped; applied: %s; remaining: %s; re-run `author demote --gate-id %s --apply` to resume",
		item, cause, orMarker(strings.Join(applied, ", "), "none"), strings.Join(remaining, ", "), gateID)
}
