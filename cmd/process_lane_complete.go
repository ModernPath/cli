package cmd

import (
	"fmt"
	"net/url"

	"slices"
	"sort"

	"strings"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------- complete

var (
	laneCompleteApply  bool
	laneCompleteLog    string
	laneCompleteKind   string
	laneCompleteGateID string
	laneCompleteDryRun bool
)

var processLaneCompleteCmd = &cobra.Command{
	Use:   "complete --log <run> | --apply [--gate <LANE-BATCH>]",
	Short: "Open one lane-batch gate over the eligible small changes, or apply an answered one",
	Long: `Complete small changes together with one approval. Without --apply it takes
every small change that is IN_REVIEW and passed process lane check at its
delivered commit, and is not already waiting in another batch. For each it
records the run you name with --log as the run you report (nothing checks
the run itself, and the approver is told so), then asks for one approval
that lists each change and its files. The approver can reject single
changes. The changes left out are listed with the reason. --dry-run prints
the batch and writes nothing.

With --apply, after the approval, each approved change becomes DONE; a
rejected change stays IN_REVIEW, to be fixed and put in a new batch.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		if laneCompleteApply {
			return processLaneApply(env, laneCompleteGateID)
		}
		if laneCompleteLog == "" {
			return fmt.Errorf("--log is required: the delivered run's reference (a CI url or the command) is what each member's evidence and completion trace cite")
		}
		return processLaneComplete(env, laneCompleteLog, firstNonEmpty(laneCompleteKind, "ci"), laneCompleteDryRun)
	},
}

type laneMember struct {
	id, class, revision, eligibility string
	files                            []string
	tests                            string
}

// eligibilityFiles reads the non-test files and the test count back from the
// server's eligibility trace body.
func eligibilityFiles(body string) (files []string, tests string) {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "Test files (not counted): "); ok {
			tests = v
			break
		}
		if strings.HasPrefix(line, "- `") && strings.HasSuffix(line, "`") {
			files = append(files, strings.TrimSuffix(strings.TrimPrefix(line, "- `"), "`"))
		}
	}
	return files, tests
}

func processLaneComplete(env *factoryEnv, log, kind string, dryRun bool) error {
	srs, _, err := fetchRequirementLists(env, fmt.Sprintf("/api/v1/sync/requirements?system_id=%d", env.SystemID))
	if err != nil {
		return err
	}
	gates, err := listGates(env, "all")
	if err != nil {
		return err
	}
	// The latest server eligibility trace per (SR, revision), and the members
	// an open or answered batch already names.
	type elig struct{ state, id, body string }
	eligibility := map[string]elig{}
	inBatch := map[string]string{}
	for _, g := range gates {
		switch str(g, "purpose") {
		case "lane-eligibility":
			for _, id := range stringSlice(g["exact_scope"]) {
				eligibility[id+"@"+str(g, "fingerprint")] = elig{strings.ToLower(str(g, "state")), str(g, "external_id"), str(g, "body_md")}
			}
		case "lane-batch":
			if s := str(g, "state"); s == "open" || s == "answered" {
				for _, id := range stringSlice(g["exact_scope"]) {
					inBatch[id] = str(g, "external_id") + " (" + s + ")"
				}
			}
		}
	}

	var members []laneMember
	var excluded []string
	for _, raw := range srs {
		r, _ := raw.(map[string]any)
		id, class, status, rev := str(r, "external_id"), str(r, "lane_class"), str(r, "work_status"), str(r, "delivered_revision")
		if class == "" || !slices.Contains([]string{"TODO", "IN_PROGRESS", "IN_REVIEW"}, status) {
			continue
		}
		e := eligibility[id+"@"+rev]
		switch {
		case inBatch[id] != "":
			excluded = append(excluded, fmt.Sprintf("%s — already named by %s", id, inBatch[id]))
		case status != "IN_REVIEW":
			excluded = append(excluded, fmt.Sprintf("%s — %s, not IN_REVIEW (process advance %s)", id, status, id))
		case rev == "" || e.state == "":
			excluded = append(excluded, fmt.Sprintf("%s — no eligibility check at its delivered revision (process lane check %s --commit <sha>)", id, id))
		case e.state != "pass":
			excluded = append(excluded, fmt.Sprintf("%s — eligibility %s at %s (%s): it leaves the lane", id, strings.ToUpper(e.state), laneShort(rev), e.id))
		default:
			files, tests := eligibilityFiles(e.body)
			members = append(members, laneMember{id: id, class: class, revision: rev, eligibility: e.id, files: files, tests: tests})
		}
	}
	sort.Slice(members, func(i, j int) bool { return members[i].id < members[j].id })
	sort.Strings(excluded)
	printExcluded := func() {
		if len(excluded) > 0 {
			fmt.Println("not in this batch:")
			for _, x := range excluded {
				fmt.Printf("  %s\n", x)
			}
		}
	}
	if len(members) == 0 {
		fmt.Println("nothing to complete: no small change is IN_REVIEW with a passing eligibility at its delivered revision")
		printExcluded()
		return nil
	}
	for _, m := range members {
		if gitOut(env.Root, "rev-parse", "--verify", "--quiet", m.revision+"^{commit}") == "" {
			return fmt.Errorf("%s's delivered revision %s is not a commit in this repository — fetch the default branch first; nothing was written", m.id, laneShort(m.revision))
		}
	}
	gateID, err := laneFreeGateID(env, "LANE-BATCH-"+time.Now().UTC().Format("20060102"))
	if err != nil {
		return err
	}
	ids := make([]string, len(members))
	for i, m := range members {
		ids[i] = m.id
	}
	fmt.Printf("lane batch %s over %d small change(s): %s\n", gateID, len(members), strings.Join(ids, ", "))
	for _, m := range members {
		fmt.Printf("  %s (%s) delivered at %s — %s; files: %s\n", m.id, m.class, laneShort(m.revision), m.eligibility, strings.Join(m.files, ", "))
	}
	printExcluded()
	if dryRun {
		fmt.Println("dry run — nothing posted")
		return nil
	}

	var traces []string
	for _, m := range members {
		if err := holdLanePiece(env, m.id, "completion"); err != nil {
			return err
		}
		agg, _, err := singleSRAggregate(env, m.id)
		if err != nil {
			return err
		}
		if err := factoryEvidenceRun(env, evidenceOpts{kind: kind, log: log, pass: m.id, revision: m.revision}); err != nil {
			return err
		}
		traceID, existing, _, err := freeTraceID(env, "LANE-COMPLETE-TRACE-"+m.id, agg)
		if err != nil {
			return err
		}
		if existing != "" {
			printInfo("%s already passes for %s at %s — reused", existing, m.id, agg)
			traces = append(traces, existing)
			continue
		}
		fields := map[string]any{
			"title":                fmt.Sprintf("%s completion trace — recorded by process lane complete at %s", m.id, laneShort(m.revision)),
			"purpose":              "completion",
			"transition":           "IN_REVIEW->DONE",
			"exact_scope":          []string{m.id},
			"fingerprint":          agg,
			"verdict":              "PASS",
			"sources":              traceSources([]string{"RUN:" + log}),
			"application_revision": m.revision,
			"body_md": fmt.Sprintf("Lane completion audit of %s at its delivered revision %s. Evidence: the %s run the agent reported (%s), naming %s; not verified. Eligibility: %s PASS (server verdict over the file list the CLI reported).",
				m.id, m.revision, kind, log, m.id, m.eligibility),
		}
		if err := authorTrace(env, traceID, fields); err != nil {
			return err
		}
		traces = append(traces, traceID)
	}

	var changes strings.Builder
	changes.WriteString("Approved small changes become DONE; a rejected one stays IN_REVIEW.\n")
	options := []map[string]any{{"key": "approve", "label": "Approve every small change not rejected"}}
	for _, m := range members {
		fmt.Fprintf(&changes, "- %s (%s, delivered at %s): %s", m.id, m.class, laneShort(m.revision), strings.Join(m.files, ", "))
		if m.tests != "" {
			fmt.Fprintf(&changes, "; %s test file(s)", m.tests)
		}
		changes.WriteString("\n")
		options = append(options, map[string]any{"key": "reject:" + m.id, "label": "Reject " + m.id})
	}
	brief := map[string]any{
		"what":                fmt.Sprintf("Complete %d small change(s): %s", len(members), strings.Join(ids, ", ")),
		"why_now":             fmt.Sprintf("Each is IN_REVIEW after a narrow independent review. The run is the run the agent reported (%s, %s); nothing verified it. Eligibility is the server's verdict over the file list the CLI reported at each delivered revision.", kind, log),
		"changes_if_approved": strings.TrimRight(changes.String(), "\n"),
		"risk_if_wrong":       "A wrong small change is marked DONE; reject it with reject:<SR> to keep it IN_REVIEW.",
		"recommendation":      "Approve, rejecting any change whose files or class you do not accept.",
	}
	fields := map[string]any{
		"title":                          fmt.Sprintf("Complete %d small change(s)", len(members)),
		"gate_class":                     "human",
		"gate_kind":                      "approval_request",
		"purpose":                        "lane-batch",
		"transition":                     "IN_REVIEW->DONE",
		"exact_scope":                    ids,
		"options":                        options,
		"recommended_option_key":         "approve",
		"brief":                          brief,
		"prerequisite_gate_external_ids": traces,
	}
	if _, err := authorCreate(env, "gate", gateID, fields); err != nil {
		return fmt.Errorf("%w\n  the runs and completion traces are recorded; fix the fact the refusal names and rerun — the traces are reused", err)
	}
	fmt.Printf("answer it in the web app (Mission Control) or with `factory answer %s --options approve[,reject:<SR>…]`: approve, rejecting any change you do not accept; then `process lane complete --apply`\n", gateID)
	return nil
}

func processLaneApply(env *factoryEnv, gateID string) error {
	var batches []map[string]any
	if gateID != "" {
		status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/gates/%s?system_id=%d", url.PathEscape(gateID), env.SystemID), nil)
		if err != nil {
			return err
		}
		if status != 200 {
			return gateShowError(status, body, gateID)
		}
		g, _ := dataOf(body)["gate"].(map[string]any)
		if str(g, "purpose") != "lane-batch" || str(g, "state") != "answered" {
			return fmt.Errorf("%s is a %s gate in state %s — --apply takes an answered lane-batch gate; nothing was written", gateID, presentPin(str(g, "purpose")), presentPin(str(g, "state")))
		}
		batches = append(batches, g)
	} else {
		gates, err := listGates(env, "answered")
		if err != nil {
			return err
		}
		for _, g := range gates {
			if str(g, "purpose") == "lane-batch" {
				batches = append(batches, g)
			}
		}
	}
	if len(batches) == 0 {
		return fmt.Errorf("there is no answered lane-batch gate to apply — open one with `process lane complete --log <run>` and answer it in the web app or with `factory answer <gate>`; nothing was written")
	}
	failed := 0
	for _, g := range batches {
		id, fp := str(g, "external_id"), str(g, "fingerprint")
		chosen := stringSlice(g["chosen_option_keys"])
		members := stringSlice(g["exact_scope"])
		fmt.Printf("── %s (answer: %s)\n", id, firstNonEmpty(str(g, "answer"), strings.Join(chosen, ", ")))
		items, err := fetchDirectItems(env, members, false, false)
		if err != nil {
			return err
		}
		approved := slices.Contains(chosen, "approve")
		for _, m := range members {
			status := str(items[m].payload, "work_status")
			switch {
			case slices.Contains(chosen, "reject:"+m):
				fmt.Printf("  ✗ %s rejected — it stays IN_REVIEW; fix it and include it in a new lane batch\n", m)
			case !approved:
				fmt.Printf("  %s not approved — the batch approved nothing\n", m)
			case status == "DONE":
				fmt.Printf("  %s already DONE\n", m)
			case status != "IN_REVIEW":
				fmt.Printf("  ✗ %s is %s, not IN_REVIEW — not advanced\n", m, presentPin(status))
				failed++
			default:
				if err := authorAdvance(env, "requirement", m, "DONE", "IN_REVIEW", id, fp, "approve", ""); err != nil {
					fmt.Printf("  ✗ %s not advanced: %v\n", m, err)
					failed++
				}
			}
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d approved small change(s) were not advanced — each reason is printed above", failed)
	}
	return nil
}
