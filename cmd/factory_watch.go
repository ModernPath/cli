package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------- watch

var (
	watchInterval int
	watchCycles   int
)

var factoryWatchCmd = &cobra.Command{
	Use:   "watch",
	Short: "The daemon: sync + heartbeat + pull --apply on a cadence; SIGINT closes the session",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Store-backed (REQ-CROSS-329): watch's per-cycle sync is the same
		// file-derived push the flip retires — decline up front rather than
		// looping doomed syncs. Resolved by walking up, so a subdir invocation
		// is caught too; no separate pull-only watch entrypoint exists.
		if _, active := storeBackedFromCwd(); active {
			printInfo("this workspace is store-backed (process/store-backed.md) — 'factory watch' syncs file ledgers that are retired; there is nothing to watch")
			printInfo("read store state with 'modernpath working-set pull' / 'modernpath factory status'; write it with 'modernpath author'")
			return nil
		}

		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}

		closeSession := func() {
			_, _, _ = env.call("POST", "/api/v1/sync/session/close", map[string]any{
				"system_id":           env.SystemID,
				"workspace_ref":       filepath.Base(env.Root),
				"machine_fingerprint": machineFingerprint(env.Root),
			})
		}

		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-sigs
			fmt.Println("\nclosing session…")
			closeSession()
			exit(0)
		}()

		for cycle := 1; ; cycle++ {
			printInfo("=== watch cycle %d (%s) ===", cycle, time.Now().UTC().Format(time.RFC3339))
			if err := factorySyncRun(env, false); err != nil {
				printWarning("sync failed: %v — retrying next cycle", err)
			}
			if err := factoryPullRun(env, true); err != nil {
				printWarning("pull failed: %v — retrying next cycle", err)
			}
			if watchCycles > 0 && cycle >= watchCycles {
				break
			}
			time.Sleep(time.Duration(watchInterval) * time.Second)
		}
		closeSession()
		printSuccess("watch ended — session closed")
		return nil
	},
}
