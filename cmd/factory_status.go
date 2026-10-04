package cmd

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/manifest"
	"github.com/spf13/cobra"
)

var factoryStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the local sync stamp, server active release, and pending op count",
	Long: `Show the workspace binding, the local sync-release stamp, the server active
release, the pending op count, the
pieces you hold, and one server line: the store revision the bound server is
serving and the sync contract version it advertises (or why it could not be
read). The server line is a single read bounded at 2 s and writes nothing —
after a merge it tells you whether production serves it.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// REQ-CROSS-391: print the binding through the renderer `status` uses
		// before refusing, so an unbound workspace reads the same in both.
		if cfgDir, err := config.FindConfigDir(); err == nil && cfgDir != "" {
			if cfg, err := config.ReadConfig(); err == nil {
				auth, _ := config.ReadAuth()
				for _, line := range bindingLines(cfg, auth) {
					fmt.Println(line)
				}
			}
		}
		env, err := factoryBindingLoad()
		if err != nil {
			return err
		}
		release := env.CurrentRelease
		if release == "" {
			release = "(none — sync runs unscoped; set one with 'factory release use <slug>')"
		}
		fmt.Printf("workspace: %s\nsync stamp: %s\n", env.Root, release)
		fmt.Printf("server:    %s\n", serverLine(env))

		if ops, warnings, err := env.workspaceOps(); err == nil {
			printGapWarnings(warnings)
			fmt.Printf("pending:   %d ops on next sync (server hash-diffs; unchanged ops are no-ops)\n", len(ops))
		}
		if _, fromFile, err := manifest.Load(env.Root); err == nil {
			if fromFile {
				fmt.Printf("manifest:  %s\n", manifest.Path(env.Root))
			} else {
				fmt.Println("manifest:  defaults (modernpath-v1 layout; write one with 'factory manifest init')")
			}
		}
		printHeldPieces(env)
		return nil
	},
}

// serverLineTimeout bounds the server line's one read: an offline status must
// answer "not reachable" in seconds, not wait out the 120 s call default. A
// variable only so a test can shorten the bound it measures.
var serverLineTimeout = 2 * time.Second

// serverLine — REQ-CROSS-417 (EPIC-CLI-021): what the bound server is serving,
// for `factory status` and `status`: the store revision from the
// x-modernpath-store-revision header (REQ-CROSS-348, the deploy revision of
// the serving core) and the contract version from GET /api/v1/sync/contract
// (REQ-CROSS-390). One read, no write, so after a merge a session can compare
// the served revision with the merge commit (BACKLOG-TOOL-1). Best-effort like
// printHeldPieces: unsigned, unreachable and unserved each say so.
func serverLine(env *factoryEnv) string {
	if strings.TrimSpace(env.token) == "" {
		auth, err := config.ReadAuth()
		if err != nil || strings.TrimSpace(auth.Token) == "" {
			return "not signed in — `modernpath auth login`, then status names the server revision"
		}
		env.token = auth.Token
	}
	saved := env.callTimeout
	env.callTimeout = serverLineTimeout
	defer func() { env.callTimeout = saved }()

	status, body, err := env.call("GET", "/api/v1/sync/contract", nil)
	if err != nil {
		return fmt.Sprintf("not reachable (%v)", err)
	}
	revision := "store revision not served"
	if env.storeRevision != "" {
		revision = "store " + env.storeRevision
	}
	served := "contract not advertised"
	if v, ok := dataOf(body)["version"].(float64); ok && status == http.StatusOK {
		served = fmt.Sprintf("contract %d", int(v))
	} else if env.contractVersion != "" {
		served = "contract " + env.contractVersion
	}
	return revision + " · " + served
}

// printHeldPieces — REQ-CROSS-379 (EPIC-CLI-018): status lists every piece the
// caller holds. Best-effort by design: status is a binding command that must
// run without a readable token (TestFactoryStatusDoesNotLoadAuthentication),
// so a missing or malformed token says so instead of failing the command.
func printHeldPieces(env *factoryEnv) {
	auth, err := config.ReadAuth()
	if err != nil || strings.TrimSpace(auth.Token) == "" {
		fmt.Println("held:      not signed in — `modernpath auth login`, then status lists the pieces you hold")
		return
	}
	env.token = auth.Token
	status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/work-selection?system_id=%d", env.SystemID), nil)
	switch {
	case err != nil:
		fmt.Printf("held:      unavailable (%v)\n", err)
	case status != 200:
		if pieces := stringSlice(body["pieces"]); len(pieces) > 0 {
			fmt.Printf("held:      %d current pieces — %s (name one with --piece)\n", len(pieces), strings.Join(pieces, ", "))
			// An ambiguous selection still carries the independent active-release
			// context. Keep provenance visible without choosing a piece.
			printActiveReleaseSource(env, body)
		} else {
			fmt.Printf("held:      unavailable (server %d)\n", status)
		}
	default:
		current, _ := dataOf(body)["current"].(map[string]any)
		if current == nil {
			fmt.Println("held:      no current piece")
		} else {
			fmt.Printf("held:      %s (phase %s)\n", str(current, "scope_external_id"), str(current, "phase"))
		}
		printActiveReleaseSource(env, dataOf(body))
	}
}

// printActiveReleaseSource — REQ-CROSS-407: status names the store's active
// release with the USER: source of its selection gate on this system, or the
// active-without-gate state and its remedy. Best-effort like the rest of
// status: a failed gates read says so and never fails the command.
func printActiveReleaseSource(env *factoryEnv, selection map[string]any) {
	releases, _ := selection["active_release"].([]any)
	if len(releases) != 1 {
		return
	}
	rm, _ := releases[0].(map[string]any)
	slug := str(rm, "slug")
	gates, err := fetchList(env, fmt.Sprintf("/api/v1/sync/gates?system_id=%d&state=all", env.SystemID), "gates")
	if err != nil {
		fmt.Printf("active:    active release %s — selection gate unavailable (%v)\n", slug, err)
		return
	}
	if tag, gate, ok := releaseSelectionGateSource(gates, slug); ok {
		fmt.Printf("active:    active release %s — %s (gate %s)\n", slug, tag, gate)
		return
	}
	fmt.Printf("active:    active release %s — %s\n", slug, missingReleaseGateLine(slug))
}
