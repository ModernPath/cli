package cmd

import (
	"fmt"
	"net/url"

	"path/filepath"
	"slices"

	"strings"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------- enter

var processLaneEnterCmd = &cobra.Command{
	Use:   "enter <SR>",
	Short: "Enter a small change by the current lane authorization (PROPOSED->TODO) in one call",
	Long: `Enter one small change (PROPOSED->TODO) under the System's current lane
authorization, in one call. The SR needs a lane class, a passing narrow
review of its current content, and an approved lane authorization. The
server checks that the authorization is current and covers the class, that
you may apply it, the daily cap, and that the review was independent; if it
refuses, its reason is printed.

If the SR already entered through the lane and changed afterwards (TODO to
IN_REVIEW), record a new narrow review of it first; enter then applies the
authorization again.

Nothing is written when the lane class, the passing review or the approved
authorization is missing. Afterwards you work on the SR in the build phase.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processLaneEnter(env, args[0])
	},
}

// narrowReviewCandidates are the trace ids that may hold the SR's narrow
// review: the one the last review pull's context names, and the facts' own.
func narrowReviewCandidates(env *factoryEnv, sr string, facts *deliveryFacts) []string {
	var ids []string
	if mode, ctx := readContextStamp(filepath.Join(env.Root, workingSetDir, sr)); mode == "review" && ctx != "" {
		ids = append(ids, "LANE-REVIEW-"+sr+"-"+ctx)
	}
	if facts != nil && facts.ColdReview.TraceExternalID != "" && !slices.Contains(ids, facts.ColdReview.TraceExternalID) {
		ids = append(ids, facts.ColdReview.TraceExternalID)
	}
	return ids
}

// narrowReviewTrace returns the passing narrow review trace among the
// candidates, pinned at `agg` when agg is given; nil when there is none.
func narrowReviewTrace(env *factoryEnv, sr string, candidates []string, agg string) (map[string]any, error) {
	for _, id := range candidates {
		status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/gates/%s?system_id=%d", url.PathEscape(id), env.SystemID), nil)
		if err != nil {
			return nil, err
		}
		if status != 200 {
			continue
		}
		g, _ := dataOf(body)["gate"].(map[string]any)
		if str(g, "purpose") != "cold-review" || !strings.EqualFold(str(g, "state"), "pass") || !slices.Contains(stringSlice(g["exact_scope"]), sr) {
			continue
		}
		pin := firstNonEmpty(str(g, "evaluated_scope_fingerprint"), str(g, "fingerprint"))
		if agg != "" && pin != agg {
			continue
		}
		for _, raw := range anyList(g["sources"]) {
			if src, ok := raw.(map[string]any); ok && strings.EqualFold(str(src, "ref"), "LANE:narrow") {
				return g, nil
			}
		}
	}
	return nil, nil
}

func processLaneEnter(env *factoryEnv, sr string) error {
	rec, err := readLaneSR(env, sr)
	if err != nil {
		return err
	}
	if err := refuseWithoutLaneClass(sr, rec); err != nil {
		return err
	}
	status := str(rec, "work_status")
	reapply := false
	switch status {
	case "PROPOSED":
	case "TODO", "IN_PROGRESS", "IN_REVIEW":
		reapply = true
	default:
		return fmt.Errorf("%s is %s — the lane enters a PROPOSED small change (or re-applies to an entered one that changed); nothing was written", sr, presentPin(status))
	}

	var facts *deliveryFacts
	held := holdsPiece(env, sr)
	if held {
		if dc, err := readDeliveryContextFor(env, sr); err == nil {
			facts = dc.Data.Facts
		}
	}
	candidates := narrowReviewCandidates(env, sr, facts)
	review, err := narrowReviewTrace(env, sr, candidates, "")
	if err != nil {
		return err
	}
	if review == nil {
		return fmt.Errorf("%s has no passing narrow review — pull it with `working-set pull %s --for-review`, have it reviewed and record it with `process lane review %s --file review.json`; nothing was written", sr, sr, sr)
	}
	auth, err := currentLaneAuthorization(env)
	if err != nil {
		return err
	}
	if auth == nil {
		return fmt.Errorf("there is no answered lane authorization on this System — prepare one with `process lane authorize` and have a workspace admin answer it in the web app or with `process lane approve <gate>`; nothing was written")
	}
	authID := str(auth, "external_id")

	if !held {
		if err := holdLanePiece(env, sr, "entry"); err != nil {
			return err
		}
	}
	agg, facts, err := singleSRAggregate(env, sr)
	if err != nil {
		return err
	}
	if review, err = narrowReviewTrace(env, sr, narrowReviewCandidates(env, sr, facts), agg); err != nil {
		return err
	}
	if review == nil {
		return fmt.Errorf("%s has no passing narrow review at its current aggregate %s — it changed after the review; re-pull it with `working-set pull %s --for-review`, review it again and record it with `process lane review %s`; nothing was written", sr, agg, sr, sr)
	}

	if reapply {
		fmt.Printf("re-apply %s to %s (%s, changed after its lane entry) at %s, reviewed by %s\n", authID, sr, status, agg, str(review, "external_id"))
		if _, err := authorPost(env, map[string]any{"action": "lane_reapply", "record": map[string]any{"external_id": sr}, "lane_ref": authID}); err != nil {
			return err
		}
		printSuccess("re-applied the lane authorization %s to %s at its current aggregate", authID, sr)
		return nil
	}

	fmt.Printf("enter %s PROPOSED->TODO by the lane authorization %s (class %s, narrow review %s at %s)\n", sr, authID, str(rec, "lane_class"), str(review, "external_id"), agg)
	data, err := authorPost(env, map[string]any{
		"action":   "advance",
		"record":   map[string]any{"kind": "requirement", "external_id": sr},
		"to":       "TODO",
		"expected": "PROPOSED",
		"lane_ref": authID,
	})
	if err != nil {
		return err
	}
	row, _ := data["requirement"].(map[string]any)
	printSuccess("entered %s → %s (basis %s, lane authorization %s)", sr, firstNonEmpty(str(row, "work_status"), "TODO"),
		firstNonEmpty(str(data, "transition_basis"), "lane_authorization"), authID)
	// The selection follows the SR into the build.
	if err := workingSetSelect(env, wsSelectOpts{scope: sr, kind: "single_sr", phase: "build"}, time.Now()); err != nil {
		printWarning("%s entered, but its selection was not moved to the build phase: %v", sr, err)
	}
	printInfo("next: the red-first build; record the runs with `factory evidence`, then `process advance %s --log <run>`", sr)
	return nil
}
