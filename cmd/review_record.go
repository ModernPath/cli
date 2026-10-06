package cmd

// REQ-CROSS-450 (EPIC-CLI-TURNS): `process review record --file review.json`
// records a delegated cold review from the reviewer's structured output in one
// call. It refuses before any write without a review-mode pull stamp, when the
// packet aggregate moved since that pull, or when a PASS would leave a
// material finding OPEN or DEFERRED; then it adds the findings, applies the
// guarded dispositions and records the cold-review trace pinned to the
// STAMPED aggregate, stopping before the trace on any failure.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

var (
	reviewRecordContext string
	reviewRecordFile    string
	reviewRecordScope   string
	reviewRecordID      string
	reviewRecordTitle   string
)

// reviewFile is the reviewer's report JSON, the schema the installed
// rdd-cold-reviewer agent ends its report with.
type reviewFile struct {
	Verdict      string             `json:"verdict"`
	Body         string             `json:"body"`
	Source       string             `json:"source"`
	Findings     []findingEntry     `json:"findings"`
	Dispositions []dispositionEntry `json:"dispositions"`
}

var processReviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Record a delegated cold review",
}

var processReviewRecordCmd = &cobra.Command{
	Use:   "record --file <review.json>",
	Short: "Record a whole cold review — findings, dispositions and the trace — in one call",
	Long: `Record a delegated cold review from the reviewer's report JSON in one call:
{verdict: PASS|FAIL, body, source, findings[], dispositions[]}. A finding is
{id, category, severity, owner, source, body, introduced_by} and a
disposition is {id, from, disposition, ref, resolution, widens}, as for
process findings add --file and process findings disposition --file; their
scope defaults to the reviewed scope, and naming another scope is refused.
A RESOLVED disposition names its resolution (packet-edit, scope or
decision), as --resolution does.

The scope is --scope (bare or <kind>:<id>), or the piece you hold (--piece
when you hold several). It must have been pulled with working-set pull
--scope --for-review. Pass its printed context ID with --review-context;
the immutable snapshot binds the packet aggregate and rendered file hashes.

It refuses before any write when:
  - the explicit review snapshot is missing or fails integrity validation;
  - the packet aggregate moved since the review pull (re-pull --for-review
    and review again);
  - the verdict is PASS while a material finding in scope would stay OPEN
    or DEFERRED: the server's open material findings plus the file's new
    ones, after the file's dispositions.
  - a finding or disposition breaks a rule of the single verbs: the
    category and severity vocabulary, a RESOLVED without its resolution,
    scope without ref, decision without a USER: ref, widens outside a
    packet edit, or a resolution or introduced_by a server without
    finding_resolution would drop.

Then it records the new findings (ids already recorded are skipped) against
the stamped aggregate and review context, applies each disposition only
while the finding is still in its "from" disposition, and records the
cold-review trace (plan->entry) naming the scope and each member, pinned to
the stamped aggregate. Each result is printed; any failure stops before the
trace and exits non-zero, and a re-run records only what is missing. The
trace id is --id, or CR-TRACE-<scope>-<review context>.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if reviewRecordFile == "" {
			return fmt.Errorf("--file <review.json> is required")
		}
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processReviewRecord(env)
	},
}

func processReviewRecord(env *factoryEnv) error {
	var review reviewFile
	if err := readJSONFile(reviewRecordFile, &review); err != nil {
		return err
	}
	review.Verdict = strings.ToUpper(strings.TrimSpace(review.Verdict))
	if review.Verdict != "PASS" && review.Verdict != "FAIL" {
		return fmt.Errorf("the review's verdict must be PASS or FAIL (got %q) — nothing was written", review.Verdict)
	}

	// The reviewed scope and the stamp its review pull left.
	ext := normalizeScopeToken(reviewRecordScope)
	if ext == "" {
		_, held, err := heldPieceScope(env, processPiece)
		if err != nil {
			return err
		}
		ext = held
	}
	if unsafeSnapshotName(ext) {
		return fmt.Errorf("scope %q is not a safe path component", ext)
	}
	scopeTokens := []string{ext}
	if reviewRecordScope != "" {
		scopeTokens = append(scopeTokens, reviewRecordScope)
	}
	selected, err := resolveReviewSnapshot(env, reviewRecordContext, scopeTokens, "")
	if err != nil {
		return err
	}
	dir := selected.Directory
	ctxID := selected.Manifest.ContextID
	stamped := selected.Manifest.AggregateFingerprint
	kind := selected.Manifest.ScopeKind
	scope := kind + ":" + ext

	dc, err := readDeliveryContextFor(env, ext)
	if err != nil {
		return err
	}
	if current := dc.Data.PacketFingerprint; current != stamped {
		return fmt.Errorf("the packet aggregate of %s moved since the review pull (stamped %s, now %s) — re-pull it with `working-set pull --scope --for-review` and review again; nothing was written",
			ext, stamped, presentPin(current))
	}
	facts := dc.Data.Facts
	if facts == nil {
		return fmt.Errorf("the server serves no delivery facts for %s — the review cannot be checked; nothing was written", ext)
	}

	for i := range review.Findings {
		if review.Findings[i].Scope == "" {
			review.Findings[i].Scope = scope
		}
		if review.Findings[i].Scope != scope {
			return fmt.Errorf("finding %s names scope %s, but this review records %s — nothing was written", review.Findings[i].ID, review.Findings[i].Scope, scope)
		}
		if aggregate := review.Findings[i].Aggregate; aggregate != "" && aggregate != stamped {
			return fmt.Errorf("finding %s aggregate pin does not match the selected review snapshot — nothing was written", review.Findings[i].ID)
		}
	}
	for i := range review.Dispositions {
		if review.Dispositions[i].Scope == "" {
			review.Dispositions[i].Scope = scope
		}
		if review.Dispositions[i].Scope != scope {
			return fmt.Errorf("disposition %s names scope %s, but this review records %s — nothing was written", review.Dispositions[i].ID, review.Dispositions[i].Scope, scope)
		}
	}

	if review.Verdict == "PASS" {
		existing, err := readScopeFindings(env, scope)
		if err != nil {
			return err
		}
		if open := openAfterReview(facts.ColdReview.OpenFindingIDs, existing, review); len(open) > 0 {
			return fmt.Errorf("the verdict is PASS, but material finding(s) would stay OPEN or DEFERRED on %s: %s — resolve or reject them in the file's dispositions, or record the verdict as FAIL; nothing was written",
				scope, strings.Join(open, ", "))
		}
	}

	if err := checkReviewEntries(env, review.Findings, review.Dispositions); err != nil {
		return err
	}
	failed := 0
	if len(review.Findings) > 0 {
		fmt.Println("findings:")
		failed += addFindingEntries(env, review.Findings, findingPin{aggregate: stamped, contextID: ctxID, snapshot: selected})
	}
	if len(review.Dispositions) > 0 {
		if err := revalidateReviewSnapshot(env, selected); err != nil {
			return err
		}
		fmt.Println("dispositions:")
		failed += applyDispositionEntries(env, review.Dispositions, selected)
	}
	if failed > 0 {
		return fmt.Errorf("%d finding(s) or disposition(s) were not recorded — the cold-review trace was not recorded; fix them and re-run (what was recorded is skipped)", failed)
	}

	id := reviewRecordID
	if id == "" {
		id = "CR-TRACE-" + ext + "-" + ctxID
	}
	exactScope := []string{ext}
	for _, m := range facts.Members {
		if m.ExternalID != "" && m.ExternalID != ext {
			exactScope = append(exactScope, m.ExternalID)
		}
	}
	title := reviewRecordTitle
	if title == "" {
		title = "Cold review of " + ext
	}
	fields := map[string]any{
		"title":                          title,
		"purpose":                        "cold-review",
		"transition":                     inferredTransitions["trace"]["cold-review"],
		"exact_scope":                    exactScope,
		"fingerprint":                    stamped, // the aggregate the reviewer read, never a fresh one
		"verdict":                        review.Verdict,
		"sources":                        traceSources(nonEmpty(review.Source)),
		"prerequisite_gate_external_ids": []string(nil),
		"application_revision":           gitHead(env.Root),
		"review_context_id":              ctxID,
	}
	if review.Body != "" {
		fields["body_md"] = review.Body
	}
	// REQ-CROSS-464: what the review read, item by item, so a later round can
	// pull only what changed (`working-set pull --since`). A stamp from an
	// older CLI carries none and records no map.
	if reviewed := readContextReviewed(dir); len(reviewed) > 0 {
		fields["reviewed_fingerprints"] = reviewed
	}
	// Retain the original digest after finding/disposition writes, even if a
	// replacement snapshot carries an internally valid manifest.
	if err := revalidateReviewSnapshot(env, selected); err != nil {
		return err
	}
	fmt.Println("trace:")
	return authorTrace(env, id, fields)
}

// openAfterReview is the material findings that would stay OPEN or DEFERRED
// once the review is recorded: the server's open material ids plus the file's
// new material findings, after the file's dispositions.
func openAfterReview(served []string, existing map[string]map[string]any, review reviewFile) []string {
	open := map[string]bool{}
	for _, id := range served {
		open[id] = true
	}
	for _, f := range review.Findings {
		if _, recorded := existing[f.ID]; !recorded && materialFinding(f.Category, f.Severity) {
			open[f.ID] = true
		}
	}
	for _, d := range review.Dispositions {
		switch d.Disposition {
		case "RESOLVED", "REJECTED":
			delete(open, d.ID)
		case "OPEN", "DEFERRED":
			category, severity := str(existing[d.ID], "category"), str(existing[d.ID], "severity")
			for _, f := range review.Findings {
				if f.ID == d.ID {
					category, severity = f.Category, f.Severity
				}
			}
			if materialFinding(category, severity) {
				open[d.ID] = true
			}
		}
	}
	out := make([]string, 0, len(open))
	for id := range open {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// materialFinding mirrors the store's rule: a blocking category that is not
// a note.
func materialFinding(category, severity string) bool {
	return contains(findingBlockingCategories, category) && severity != "note"
}

// stampedScopeKind reads the scope kind the review pull stamped
// (`scope: <kind>:<id>`); "epic" when the stamp names none.
func stampedScopeKind(dir string) string {
	for _, line := range strings.Split(readContextFile(dir), "\n") {
		if v, ok := strings.CutPrefix(line, "scope: "); ok {
			if kind, _ := splitScope(strings.TrimSpace(v)); kind != "" {
				return kind
			}
		}
	}
	return "epic"
}

func nonEmpty(values ...string) []string {
	out := []string{}
	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func init() {
	processReviewRecordCmd.Flags().StringVar(&reviewRecordContext, "review-context", "", "the immutable snapshot context printed by the review pull")
	processReviewRecordCmd.Flags().StringVar(&reviewRecordFile, "file", "", "the reviewer's report JSON {verdict, body, source, findings[], dispositions[]}")
	processReviewRecordCmd.Flags().StringVar(&reviewRecordScope, "scope", "", "the reviewed scope (bare or <kind>:<id>); the held piece when omitted")
	processReviewRecordCmd.Flags().StringVar(&reviewRecordID, "id", "", "the trace id (default CR-TRACE-<scope>-<review context>)")
	processReviewRecordCmd.Flags().StringVar(&reviewRecordTitle, "title", "", "the trace title (default \"Cold review of <scope>\")")
	processReviewCmd.AddCommand(processReviewRecordCmd)
	processCmd.AddCommand(processReviewCmd)
}
