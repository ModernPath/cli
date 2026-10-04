package cmd

import (
	"fmt"

	"path/filepath"

	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------- authorize

var (
	laneAuthClasses  []string
	laneAuthAppliers []string
	laneAuthExpires  string
	laneAuthCap      int
	laneAuthExclude  []string
	laneAuthFile     string
	laneAuthID       string
	laneAuthTitle    string
)

type laneAuthTerms struct {
	Classes       []string `json:"classes"`
	ExcludedGlobs []string `json:"excluded_globs"`
	Appliers      []int    `json:"appliers"`
	ExpiresAt     string   `json:"expires_at"`
	DailyCap      int      `json:"daily_cap"`
}

var processLaneAuthorizeCmd = &cobra.Command{
	Use:   "authorize --classes <c,…> --appliers <user id,…> --expires <date> --cap <n> [--exclude <glob>]… | --file <lane.json>",
	Short: "Prepare a lane authorization for this System; a workspace admin answers it in the web app or with process lane approve",
	Long: `Ask for a lane authorization for this System. It names the classes of
small change it covers (defect_with_failing_test, wording, presentation,
dependency_patch), the user ids who may apply it, when it expires (a date,
a date and time, or <n>d from now; at most 30 days), a daily cap (1 to 10)
and any extra paths the lane must not touch. --file reads the same terms
from a JSON file, for example:

  {"classes": ["wording"], "appliers": [7], "expires_at": "20d",
   "daily_cap": 5, "excluded_globs": ["docs/legal/**"]}

A workspace admin answers it once: in the web app (Mission Control), or with
process lane approve <gate>. factory answer, an API token or an integration
cannot answer it. The server checks every term and prints why it refuses
one.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		terms, err := laneAuthorizeTerms(time.Now().UTC())
		if err != nil {
			return err
		}
		return processLaneAuthorize(env, terms)
	},
}

// parseLaneExpiry reads RFC 3339, a date (its UTC midnight) or <n>d from now.
func parseLaneExpiry(v string, now time.Time) (string, error) {
	v = strings.TrimSpace(v)
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	if n, ok := strings.CutSuffix(v, "d"); ok {
		if days, err := strconv.Atoi(n); err == nil && days > 0 {
			return now.Add(time.Duration(days) * 24 * time.Hour).Truncate(time.Second).Format(time.RFC3339), nil
		}
	}
	return "", fmt.Errorf("--expires %q: give an RFC 3339 time, a date (YYYY-MM-DD, read as its UTC midnight) or <n>d from now; nothing was written", v)
}

func laneAuthorizeTerms(now time.Time) (laneAuthTerms, error) {
	var t laneAuthTerms
	if laneAuthFile != "" {
		if err := readJSONFile(filepath.Clean(laneAuthFile), &t); err != nil {
			return t, fmt.Errorf("--file: %w", err)
		}
	} else {
		for _, c := range laneAuthClasses {
			if c = strings.TrimSpace(c); c != "" {
				t.Classes = append(t.Classes, c)
			}
		}
		for _, a := range laneAuthAppliers {
			n, err := strconv.Atoi(strings.TrimSpace(a))
			if err != nil || n <= 0 {
				return t, fmt.Errorf("--appliers %q: appliers are user ids (positive integers); nothing was written", a)
			}
			t.Appliers = append(t.Appliers, n)
		}
		t.ExpiresAt, t.DailyCap, t.ExcludedGlobs = laneAuthExpires, laneAuthCap, laneAuthExclude
	}
	switch {
	case len(t.Classes) == 0:
		return t, fmt.Errorf("--classes is required: the classes the authorization covers (defect_with_failing_test, wording, presentation, dependency_patch); nothing was written")
	case len(t.Appliers) == 0:
		return t, fmt.Errorf("--appliers is required: the user ids who may apply the authorization; nothing was written")
	case t.ExpiresAt == "":
		return t, fmt.Errorf("--expires is required: at most 30 days from now; nothing was written")
	case t.DailyCap == 0:
		return t, fmt.Errorf("--cap is required: the daily cap of lane applications (1 to 10); nothing was written")
	}
	expires, err := parseLaneExpiry(t.ExpiresAt, now)
	if err != nil {
		return t, err
	}
	t.ExpiresAt = expires
	if t.ExcludedGlobs == nil {
		t.ExcludedGlobs = []string{}
	}
	return t, nil
}

func processLaneAuthorize(env *factoryEnv, t laneAuthTerms) error {
	id := laneAuthID
	if id != "" {
		if err := gateIDFree(env, id); err != nil {
			return err
		}
	} else {
		free, err := laneFreeGateID(env, "LANE-AUTH")
		if err != nil {
			return err
		}
		id = free
	}
	title := laneAuthTitle
	if title == "" {
		title = "Small-change lane for this System: " + strings.Join(t.Classes, ", ")
	}
	appliers := make([]string, len(t.Appliers))
	for i, a := range t.Appliers {
		appliers[i] = strconv.Itoa(a)
	}
	excluded := "the default excluded areas only"
	if len(t.ExcludedGlobs) > 0 {
		excluded = "the default excluded areas and " + strings.Join(t.ExcludedGlobs, ", ")
	}
	body := fmt.Sprintf("Authorizes the small-change lane on this System (PROCESS.md, Small-change lane).\n\n- Classes: %s\n- Excluded: %s\n- Appliers (user ids): %s\n- Expires: %s\n- Daily cap: %d\n\nAnswered once by a workspace admin, in the web app or with the developer CLI's process lane approve. Every application appears in the authorizer's feed; it can be withdrawn at any time.",
		strings.Join(t.Classes, ", "), excluded, strings.Join(appliers, ", "), t.ExpiresAt, t.DailyCap)
	fields := map[string]any{
		"title":       title,
		"gate_class":  "human",
		"gate_kind":   "approval_request",
		"purpose":     "lane_authorization",
		"exact_scope": []string{fmt.Sprintf("system:%d", env.SystemID)},
		"options":     []map[string]any{{"key": "approve", "label": "Authorize the small-change lane"}},
		"body_md":     body,
		"raw_payload": map[string]any{
			"classes": t.Classes, "excluded_globs": t.ExcludedGlobs, "appliers": t.Appliers,
			"expires_at": t.ExpiresAt, "daily_cap": t.DailyCap,
		},
	}
	if _, err := authorCreate(env, "gate", id, fields); err != nil {
		return err
	}
	fmt.Printf("lane authorization %s\n  classes: %s\n  excluded: %s\n  appliers: %s\n  expires: %s\n  daily cap: %d\n",
		id, strings.Join(t.Classes, ", "), excluded, strings.Join(appliers, ", "), t.ExpiresAt, t.DailyCap)
	fmt.Printf("%s is open: a workspace admin answers it once, in the web app (Mission Control) or with the developer CLI\n", id)
	printInfo("next: `modernpath process lane approve %s` as a workspace admin, or answer it in the web app", id)
	return nil
}
