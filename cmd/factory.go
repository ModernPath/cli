package cmd

// Factory command and flag wiring. Implementations live in focused factory_*.go files.

import (
	"time"

	"github.com/spf13/cobra"
)

var factoryCmd = &cobra.Command{
	Use:     "factory",
	Aliases: []string{"mc"},
	Short:   "Mission Control: sync the workspace, answer gates, track the loop",
	Long: `The workspace<->server datasync and decision surface (Mission Control).

Bind once with 'modernpath factory connect --system <id>', then:
  release   select the current release; sync stamps + scopes to it
  sync      push requirements/epics/gates/events (idempotent, hash-diffed)
  gates     the open decision queue
  answer    record a USER: decision (first-wins on the server)
  pull      server-born answers -> workspace records (--apply acks the echo)
  evidence  post a test/CI run as evidence
  drift     compare evidence shas to the working tree (Done decays)
  watch     the daemon: sync + heartbeat + pull --apply on a cadence`,
}

// ---------------------------------------------------------------- wiring

func init() {
	factoryConnectCmd.Flags().IntVar(&factoryConnectSystem, "system", 0, "System id to bind this workspace to")

	factorySyncCmd.Flags().BoolVar(&factorySyncDryRun, "dry-run", false, "print the ops without sending")
	factorySyncCmd.Flags().BoolVar(&factorySyncJSON, "json", false, "with --dry-run: emit the full op batch as JSON")
	factorySyncCmd.Flags().BoolVar(&factorySyncNoDocs, "no-docs", false, "skip workspace-document ops (upsert_document); sync process state only")

	factoryGatesCmd.Flags().StringVar(&gatesKind, "kind", "", "show only this kind (approval_request, decision, question, roadblock, …)")
	factoryGatesCmd.Flags().StringVar(&gatesState, "state", "", "list gate history in this state: open, answered, dismissed, superseded, all (default: the open queue)")
	factoryGatesCmd.Flags().BoolVar(&gatesAudit, "audit", false, "audit one gate: answer and application blockers, review provenance and next actions")
	factoryGatesCmd.Flags().BoolVar(&gatesJSON, "json", false, "emit the server's gate envelope as JSON on stdout and nothing else")
	addPageFlags(factoryGatesCmd, &gatesPage)

	factoryAnswerCmd.Flags().StringVar(&answerText, "text", "", "the answer, recorded verbatim as the USER: decision")
	factoryAnswerCmd.Flags().StringVar(&answerOptions, "options", "", "chosen option keys, comma-separated")
	factoryAnswerCmd.Flags().StringVar(&answerSource, "source", "", "override the USER:<date> source tag")

	factoryPullCmd.Flags().BoolVar(&pullApply, "apply", false, "record the answers in the workspace and ack the echo")

	factoryEvidenceCmd.Flags().StringVar(&evidenceKind, "kind", "local_test", "run kind: ci|local_test|browser_verification|manual")
	factoryEvidenceCmd.Flags().StringVar(&evidenceLog, "log", "", "log reference (command line, CI url)")
	factoryEvidenceCmd.Flags().StringVar(&evidenceTotals, "totals", "", "totals, e.g. passed=478,failed=0")
	factoryEvidenceCmd.Flags().StringVar(&evidencePass, "pass", "", "passing target ids, comma-separated")
	factoryEvidenceCmd.Flags().StringVar(&evidenceFail, "fail", "", "failing target ids, comma-separated")
	factoryEvidenceCmd.Flags().StringVar(&evidenceSkip, "skip", "", "skipped target ids, comma-separated")
	factoryEvidenceCmd.Flags().StringVar(&evidenceRole, "role", "", "evidence role stamped on every result this run — RED for a red-first result (upper-cased here; the server matches RED exactly); empty = unset")
	factoryEvidenceCmd.Flags().StringVar(&evidenceFile, "file", "", "record several runs from a JSON array of {pass, fail, skip, role, revision, kind, log, totals}, one run per entry with an id derived from the entry")
	factoryEvidenceCmd.Flags().StringVar(&evidenceRevision, "revision", "", "pin the run to this commit (sha, tag or branch) instead of HEAD — record a RED at the RED commit without a checkout")

	// Q-ARCH-016 (USER:2026-08-18): --report was advertised in drift's own
	// output but never registered; the drift-report POST was unreachable.
	factoryDriftCmd.Flags().BoolVar(&driftReport, "report", false,
		"POST the drift facts to the server (marks affected evidence stale until re-verified)")

	factoryWatchCmd.Flags().IntVar(&watchInterval, "interval", 120, "seconds between cycles")
	factoryWatchCmd.Flags().IntVar(&watchCycles, "cycles", 0, "stop after N cycles (0 = forever)")

	factoryReleaseActivateCmd.Flags().StringVar(&releaseActivateSource, "source", "", "optional attributable source (the server composes one when omitted)")
	factoryReleaseActivateCmd.Flags().StringVar(&releaseActivatePin, "pin", "", "existing release PIN confirmation (otherwise prompt or --pin-stdin)")
	factoryReleaseActivateCmd.Flags().StringVar(&releaseActivateReason, "reason", "", "optional activation reason recorded by the server")
	factoryReleaseActivateCmd.Flags().BoolVar(&releaseActivateCloseCurrent, "close-current", false, "explicitly close the currently active release in this system (planned releases are never closed)")
	factoryReleaseActivateCmd.Flags().BoolVar(&releaseActivatePinStdin, "pin-stdin", false, "read the existing compliance PIN from stdin")
	factoryReleaseCmd.AddCommand(factoryReleaseUseCmd, factoryReleaseShowCmd, factoryReleaseClearCmd, factoryReleaseActivateCmd)

	factoryPinCmd.AddCommand(factoryPinSetCmd)
	factoryPinSetCmd.Flags().BoolVar(&pinSetStdin, "pin-stdin", false,
		"read the PIN from stdin (one line) instead of prompting — for non-interactive use")

	factorySyncCmd.Flags().BoolVar(&factorySyncIfQuiescent, "if-quiescent", false,
		"hook mode: sync only when the workspace is coherent; log outcomes, never error")
	factorySyncCmd.Flags().StringVar(&factorySyncTrigger, "trigger", "manual", "trigger label for the hook log")
	factorySyncCmd.Flags().DurationVar(&factorySyncMinInterval, "min-interval", 60*time.Second,
		"debounce: skip when the last successful sync is younger than this")

	factoryCmd.AddCommand(factoryConnectCmd, factoryStatusCmd, factorySyncCmd, factoryGatesCmd,
		factoryAnswerCmd, factoryPullCmd, factoryEvidenceCmd, factoryDriftCmd, factoryWatchCmd,
		factoryManifestCmd, factoryReleaseCmd, factoryPinCmd, factoryImageCmd)
	factoryImageCmd.Flags().StringVar(&imagePurpose, "purpose", "", "context tag stored with the image (e.g. decision-brief)")
	rootCmd.AddCommand(factoryCmd)
}
