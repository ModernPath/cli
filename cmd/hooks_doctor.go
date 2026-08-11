package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// EPIC-CTX-001 (`USER:2026-08-11`): the hooks invoke `modernpath` by bare name,
// so PATH decides which build runs. On the machine this was written, two copies
// existed and the earlier one shadowed the newer — the deadline and the outcome
// log were committed, tested and simply never executed. Nothing reported that,
// because a stale binary exits cleanly (`RUN:2026-08-11`).
var hooksDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Report which modernpath the hooks will actually run",
	Long: `Reports every modernpath on PATH, which one wins, and whether the hooks
are wired to it.

The hooks call the CLI by name. A second, older copy earlier in PATH silently
downgrades every hook run while still exiting 0 — the failure this command
exists to make visible.`,
	RunE: runHooksDoctor,
}

func init() {
	hooksCmd.AddCommand(hooksDoctorCmd)
}

// pathCopies returns every executable named `name` on the given PATH, in the
// order the shell would consider them. The first is the one that runs; any
// others are shadowed.
func pathCopies(name, pathEnv string, exists func(string) bool) []string {
	var found []string
	seen := map[string]bool{}

	for _, dir := range strings.Split(pathEnv, string(os.PathListSeparator)) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		if seen[candidate] || !exists(candidate) {
			continue
		}
		seen[candidate] = true
		found = append(found, candidate)
	}

	return found
}

func executableExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

func runHooksDoctor(cmd *cobra.Command, args []string) error {
	copies := pathCopies("modernpath", os.Getenv("PATH"), executableExists)

	switch len(copies) {
	case 0:
		printWarning("No modernpath found on PATH — the hooks are silent no-ops.\n")
		printInfo("Install with scripts/install-local.sh, then re-run 'modernpath hooks install'.\n")
		return nil
	case 1:
		printSuccess("modernpath: %s (version %s)\n", copies[0], Version)
	default:
		printSuccess("modernpath: %s (version %s) — this is the one the hooks run\n", copies[0], Version)
		printWarning("%d other copies are shadowed by it:\n", len(copies)-1)
		for _, other := range copies[1:] {
			fmt.Printf("    %s\n", other)
		}
		printInfo("Shadowed copies go stale silently. scripts/install-local.sh writes all of them.\n")
	}

	agents := detectInstalledAgents()
	if len(agents) == 0 {
		printWarning("No agent config directories found (.claude/.cursor/.codex).\n")
		return nil
	}

	for _, key := range agents {
		agent := hookAgents[key]
		raw, err := os.ReadFile(agent.configPath)
		if err != nil {
			printWarning("%s: no hooks configured (%s)\n", agent.name, agent.configPath)
			continue
		}

		text := string(raw)
		reportHookFamily(agent.name, "context", text, contextHookMarker)
		if agent.name == "Claude Code" {
			reportHookFamily(agent.name, "sync", text, syncHookMarker)
		}

		if strings.Contains(text, agent.scriptName) {
			printWarning("%s: a hook still points at %s — a file this version does not write. Re-run 'modernpath hooks install'.\n",
				agent.name, agent.scriptName)
		}
	}

	reportRecentOutcomes()
	return nil
}

func reportHookFamily(agentName, family, config, marker string) {
	if strings.Contains(config, marker) {
		printSuccess("%s: %s hook wired\n", agentName, family)
	} else {
		printWarning("%s: %s hook NOT wired\n", agentName, family)
	}
}

// The log is the only place a degraded hook shows up, so the doctor reads it
// rather than asking the user to.
func reportRecentOutcomes() {
	raw, err := os.ReadFile(filepath.Join(".modernpath", "context-hook.log"))
	if err != nil {
		printInfo("No context-hook log yet — run a prompt, then check again.\n")
		return
	}

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) > 10 {
		lines = lines[len(lines)-10:]
	}

	failures := 0
	for _, line := range lines {
		if strings.Contains(line, "· failed ·") {
			failures++
		}
	}

	if failures > 0 {
		printWarning("Context hook: %d of the last %d runs failed:\n", failures, len(lines))
		for _, line := range lines {
			if strings.Contains(line, "· failed ·") {
				fmt.Printf("    %s\n", line)
			}
		}
	} else {
		printSuccess("Context hook: last %d runs healthy\n", len(lines))
	}
}
