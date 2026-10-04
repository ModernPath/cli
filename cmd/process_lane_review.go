package cmd

import (
	"fmt"

	"os"
	"path/filepath"

	"strings"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------- review pull

// workingSetPullForReview is `working-set pull <SR> --for-review`: a read that
// renders one SR read-only under a fresh review context for the lane's narrow
// review. The stamp records the SR's content fingerprint and its packet
// sections; with those unchanged, the single-SR aggregate is the one the
// reviewer read, which `process lane review` checks before it records.
func workingSetPullForReview(env *factoryEnv, ids []string, now time.Time) error {
	for _, id := range ids {
		if unsafeSnapshotName(id) {
			return fmt.Errorf("%q is not a plain external id — refusing to pull", id)
		}
	}
	items, err := fetchDirectItems(env, ids, false, false)
	if err != nil {
		return err
	}
	for _, id := range ids {
		item, ok := items[id]
		if !ok {
			return fmt.Errorf("%s is not served by %s (unknown external id)", id, env.APIURL)
		}
		if item.kind != "system" {
			return fmt.Errorf("%s is a %s record — a by-id review pull takes a system requirement (the small-change lane's narrow review); review an epic with `working-set pull --scope --for-review`", id, item.kind)
		}
		sections, sectionPairs, err := laneSectionFingerprints(env, id)
		if err != nil {
			return err
		}
		agg := ""
		if holdsPiece(env, id) {
			if dc, err := readDeliveryContextFor(env, id); err == nil {
				agg = dc.Data.PacketFingerprint
			}
		}
		ctxID := newContextID("review")
		dir := filepath.Join(env.Root, workingSetDir, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := atomicWrite(filepath.Join(dir, reviewBundleFile), []byte(renderLaneReviewBundle(id, item.payload, ctxID, sections, now))); err != nil {
			return err
		}
		stamp := fmt.Sprintf("# working-set context\n\nmode: review\ncontext_id: %s\nscope: single_sr:%s\npulled_at: %s\ncontent: %s\nsections: %s\n",
			ctxID, id, now.Format(time.RFC3339), str(item.payload, "fingerprint"), sectionPairs)
		if agg != "" {
			stamp += "aggregate: " + agg + "\n"
		}
		if err := atomicWrite(filepath.Join(dir, contextFile), []byte(stamp)); err != nil {
			return err
		}
		printSuccess("review pull %s → %s (review context %s)", id, filepath.Join(workingSetDir, id, reviewBundleFile), ctxID)
	}
	return nil
}

func renderLaneReviewBundle(id string, rec map[string]any, ctxID string, sections []map[string]any, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# REVIEW — %s (small-change lane, narrow review)\n\n", id)
	fmt.Fprintf(&b, "- **Scope:** single_sr:%s\n", id)
	fmt.Fprintf(&b, "- **Lane class:** %s\n", orMarker(str(rec, "lane_class"), "«not set — `process lane review` refuses it»"))
	fmt.Fprintf(&b, "- **Content fingerprint:** %s\n", orMarker(str(rec, "fingerprint"), "—"))
	fmt.Fprintf(&b, "- **Review context:** %s\n", ctxID)
	fmt.Fprintf(&b, "- **Pulled at:** %s\n\n", now.Format(time.RFC3339))
	b.WriteString("One narrow pass, no rounds: check that the statement, boundary, RED plan and lane class describe one small change the lane may take (PROCESS.md, Small-change lane). A blocking finding is a FAIL, and the change leaves the lane for single-SR scope.\n\n")
	b.WriteString("## System requirement\n\n")
	writeBundleRequirement(&b, id, scopeRecord{kind: "system", payload: rec})
	b.WriteString("## Packet sections\n\n")
	if len(sections) == 0 {
		b.WriteString("None — the SR record is the small change's packet.\n")
	}
	for _, sm := range sectionOrder(sections) {
		fmt.Fprintf(&b, "### %s · %s\n\n%s\n\n", str(sm, "section_key"), orMarker(str(sm, "content_fingerprint"), "—"), strings.TrimRight(str(sm, "content"), "\n"))
	}
	return b.String()
}

// ---------------------------------------------------------------- review

var laneReviewFile string

var processLaneReviewCmd = &cobra.Command{
	Use:   "review <SR> --file <review.json>",
	Short: "Record the small change's narrow independent review in one call",
	Long: `Record the lane's narrow review of one small change from the reviewer's
report file (the same file process review record reads). The SR must have
been pulled with working-set pull <SR> --for-review: that pull records what
the reviewer read.

It refuses before any write when the SR was not pulled for review, has no
lane class (set it before the review), changed since the review pull (pull
it again and review again), or when a PASS would leave a blocking finding
open. Otherwise it records the findings, their dispositions and the review
verdict for the SR as it was reviewed.

There is one pass and no second round: after a FAIL the change leaves the
lane and is planned as a single SR with its full packet and review.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if laneReviewFile == "" {
			return fmt.Errorf("--file <review.json> is required")
		}
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processLaneReview(env, args[0], laneReviewFile)
	},
}

func processLaneReview(env *factoryEnv, sr, file string) error {
	var review reviewFile
	if err := readJSONFile(file, &review); err != nil {
		return err
	}
	review.Verdict = strings.ToUpper(strings.TrimSpace(review.Verdict))
	if review.Verdict != "PASS" && review.Verdict != "FAIL" {
		return fmt.Errorf("the review's verdict must be PASS or FAIL (got %q) — nothing was written", review.Verdict)
	}
	if unsafeSnapshotName(sr) {
		return fmt.Errorf("%q is not a plain external id", sr)
	}
	dir := filepath.Join(env.Root, workingSetDir, sr)
	mode, ctxID := readContextStamp(dir)
	stampedContent, stampedSections, stampedAgg := readStampField(dir, "content"), readStampField(dir, "sections"), readContextAggregate(dir)
	if mode != "review" || ctxID == "" || readStampField(dir, "scope") != "single_sr:"+sr || (stampedContent == "" && stampedAgg == "") {
		return fmt.Errorf("%s carries no review-mode stamp — pull it with `working-set pull %s --for-review` before the review, and record from that pull; nothing was written", sr, sr)
	}
	rec, err := readLaneSR(env, sr)
	if err != nil {
		return err
	}
	if err := refuseWithoutLaneClass(sr, rec); err != nil {
		return err
	}
	if stampedContent != "" {
		_, sections, err := laneSectionFingerprints(env, sr)
		if err != nil {
			return err
		}
		if now := str(rec, "fingerprint"); now != stampedContent || sections != stampedSections {
			return fmt.Errorf("%s changed since the review pull (content %s, now %s; sections %s, now %s) — re-pull it with `working-set pull %s --for-review` and review again; nothing was written",
				sr, stampedContent, presentPin(now), stampedSections, sections, sr)
		}
	}
	scope := "single_sr:" + sr
	for i := range review.Findings {
		if review.Findings[i].Scope == "" {
			review.Findings[i].Scope = scope
		}
		if review.Findings[i].Scope != scope {
			return fmt.Errorf("finding %s names scope %s, but this review records %s — nothing was written", review.Findings[i].ID, review.Findings[i].Scope, scope)
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
	if err := checkReviewEntries(env, review.Findings, review.Dispositions); err != nil {
		return err
	}
	var existing map[string]map[string]any
	if review.Verdict == "PASS" {
		if existing, err = readScopeFindings(env, scope); err != nil {
			return err
		}
	}

	if err := holdLanePiece(env, sr, "cold_review"); err != nil {
		return err
	}
	agg, facts, err := singleSRAggregate(env, sr)
	if err != nil {
		return err
	}
	if stampedAgg != "" && stampedAgg != agg {
		return fmt.Errorf("the single-SR aggregate of %s moved since the review pull (stamped %s, now %s) — re-pull it with `working-set pull %s --for-review` and review again; nothing was written", sr, stampedAgg, agg, sr)
	}
	if review.Verdict == "PASS" {
		var served []string
		if facts != nil {
			served = facts.ColdReview.OpenFindingIDs
		}
		if open := openAfterReview(served, existing, review); len(open) > 0 {
			return fmt.Errorf("the verdict is PASS, but material finding(s) would stay OPEN or DEFERRED on %s: %s — resolve or reject them in the file's dispositions, or record the verdict as FAIL; nothing was written", scope, strings.Join(open, ", "))
		}
	}

	failed := 0
	if len(review.Findings) > 0 {
		fmt.Println("findings:")
		failed += addFindingEntries(env, review.Findings, findingPin{aggregate: agg, contextID: ctxID})
	}
	if len(review.Dispositions) > 0 {
		fmt.Println("dispositions:")
		failed += applyDispositionEntries(env, review.Dispositions)
	}
	if failed > 0 {
		return fmt.Errorf("%d finding(s) or disposition(s) were not recorded — the narrow review was not recorded; fix them and re-run (what was recorded is skipped)", failed)
	}

	sources := []map[string]any{laneNarrowSource}
	sources = append(sources, traceSources(nonEmpty(review.Source))...)
	fields := map[string]any{
		"title":                "Narrow review of " + sr + " (small-change lane)",
		"purpose":              "cold-review",
		"transition":           "PROPOSED->TODO",
		"exact_scope":          []string{sr},
		"fingerprint":          agg,
		"verdict":              review.Verdict,
		"sources":              sources,
		"review_context_id":    ctxID,
		"application_revision": gitHead(env.Root),
	}
	if review.Body != "" {
		fields["body_md"] = review.Body
	}
	fmt.Println("trace:")
	if err := authorTrace(env, "LANE-REVIEW-"+sr+"-"+ctxID, fields); err != nil {
		return err
	}
	if review.Verdict == "FAIL" {
		fmt.Printf("%s failed its narrow review — it leaves the lane: plan it as single-SR scope with its full packet and review (there is no second narrow round)\n", sr)
		return nil
	}
	printInfo("next: `process lane enter %s`", sr)
	return nil
}
