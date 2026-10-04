package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------- drift

var driftReport bool

var factoryDriftCmd = &cobra.Command{
	Use:   "drift",
	Short: "Compare each target's evidence sha to the working tree (Done decays)",
	Long: `Compare each target's evidence sha to the working tree (Done decays).

A target whose recorded run no longer matches the head, or whose run's
validity lapsed, is a drift item: your-move lists it as [Drift] with its
basis — the run's revision and the head it no longer matches, or the run's
time with its validity lapsed — and the re-record path, a fresh passing run
recorded at the current head with 'factory evidence --pass <id>'.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}

		status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/evidence/latest?system_id=%d", env.SystemID), nil)
		if err != nil {
			return err
		}
		if status != 200 {
			return serverRefusal("", status, body)
		}

		tracePaths, err := env.workspaceTracePaths()
		if err != nil {
			return err
		}

		head := gitOut(env.Root, "rev-parse", "--short", "HEAD")
		drifted := 0
		reportFailures := 0

		targets, _ := dataOf(body)["targets"].([]any)
		for _, t := range targets {
			m, _ := t.(map[string]any)
			if str(m, "result") != "pass" || str(m, "sha") == "" || str(m, "sha") == "unknown" {
				continue
			}
			baseID := strings.SplitN(str(m, "target_external_id"), "#AC", 2)[0]
			paths := tracePaths[baseID]
			if len(paths) == 0 {
				continue
			}

			// evidence sha -> working tree, plus untracked: uncommitted AND
			// brand-new files are drift too
			changedSet := map[string]bool{}
			for _, line := range strings.Split(gitOut(env.Root, "diff", "--name-only", str(m, "sha")), "\n") {
				if line != "" {
					changedSet[line] = true
				}
			}
			for _, line := range strings.Split(gitOut(env.Root, "ls-files", "--others", "--exclude-standard"), "\n") {
				if line != "" {
					changedSet[line] = true
				}
			}

			matched := []string{}
			for changed := range changedSet {
				for _, pStr := range paths {
					if pStr != "" && (changed == pStr || strings.HasSuffix(changed, "/"+pStr) || strings.Contains(changed, pStr)) {
						matched = append(matched, changed)
						break
					}
				}
			}
			if len(matched) == 0 {
				continue
			}
			drifted++
			sort.Strings(matched)
			fmt.Printf("\n%s  evidence at %s — %d traced file(s) changed since:\n", str(m, "target_external_id"), str(m, "sha"), len(matched))
			for i, f := range matched {
				if i >= 5 {
					break
				}
				fmt.Printf("  ~ %s\n", f)
			}

			if driftReport {
				if len(matched) > 20 {
					matched = matched[:20]
				}
				if reportErr := reportDriftTarget(env, str(m, "target_external_id"), matched, head, str(m, "sha")); reportErr != nil {
					printError("  -> drift report failed: %v\n", reportErr)
					reportFailures++
				} else {
					printInfo("  -> drift_detected reported (state stale until re-verified)")
				}
			}
		}

		if reportFailures > 0 {
			return fmt.Errorf("%d drift report(s) failed — server state unchanged for those targets", reportFailures)
		}

		if drifted == 0 {
			printSuccess("no drift — evidence still covers what is on disk")
		} else if !driftReport {
			fmt.Printf("\n%d drifted — record with: modernpath factory drift --report\n", drifted)
		}
		return nil
	},
}

// reportDriftTarget posts one target's drift facts. "Reported" must mean the
// server recorded it: a discarded POST result printed success over network
// failures and 4xx/5xx (external review, RUN:2026-08-19); the second round
// asked for the branches to be pinned by tests (factory_drift_report_test.go).
func reportDriftTarget(env *factoryEnv, targetID string, changed []string, head, since string) error {
	status, _, err := env.call("POST", "/api/v1/sync/evidence/drift", map[string]any{
		"system_id": env.SystemID, "target_external_id": targetID,
		"changed": changed, "head": head, "since": since,
	})
	if err != nil {
		return err
	}
	if status >= 300 {
		return fmt.Errorf("rejected: HTTP %d", status)
	}
	return nil
}
