package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------- release

// factory release use|show|clear — the workspace-level release selector
// (REQ-CROSS-017). Writes only local config; the server materializes the
// release (find-or-create by slug) on the first scoped sync. This is the
// local sync stamp; the system-wide active release lives in the store and is
// set with `factory release activate` (REQ-CROSS-339) — the two never merge.
var factoryReleaseCmd = &cobra.Command{
	Use:   "release",
	Short: "Select the current release that factory sync stamps and scopes to",
}

var factoryReleaseUseCmd = &cobra.Command{
	Use:   "use <slug>",
	Short: "Set current_release in .modernpath/config.json (e.g. modernpath-v1-09)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		slug := strings.TrimSpace(args[0])
		if slug == "" {
			return fmt.Errorf("usage: modernpath factory release use <slug>")
		}
		cfg, err := config.ReadConfig()
		if err != nil {
			return err
		}
		cfg.CurrentRelease = slug
		if err := config.WriteConfig(cfg); err != nil {
			return err
		}
		printSuccess("current release: %s (local sync stamp in .modernpath/config.json; the system-wide active release lives in the store — activate one with 'factory release activate <slug>')", slug)
		return nil
	},
}

var factoryReleaseShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the current release",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.ReadConfig()
		if err != nil {
			return err
		}
		if cfg.CurrentRelease == "" {
			fmt.Println("(none — sync runs unscoped; set one with 'factory release use <slug>')")
		} else {
			fmt.Println(cfg.CurrentRelease)
		}
		return nil
	},
}

var factoryReleaseClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Unset the current release (sync runs unscoped; existing stamps stay)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.ReadConfig()
		if err != nil {
			return err
		}
		cfg.CurrentRelease = ""
		if err := config.WriteConfig(cfg); err != nil {
			return err
		}
		printSuccess("current release cleared — sync runs unscoped (server stamps are left untouched)")
		return nil
	},
}

// factory release activate <slug> — REQ-CROSS-339 (EPIC-CLI-010): the
// system-wide activation verb. Unlike `use`, which only stamps local config
// the bulk sync consumes, `activate` calls the guarded server operation that
// sets the release active. The server composes an attributable source when the
// caller does not supply one. The two never merge.
var (
	releaseActivateSource       string
	releaseActivatePin          string
	releaseActivateReason       string
	releaseActivateCloseCurrent bool
	releaseActivatePinStdin     bool
	activationPinFromStdin      = func() (string, error) { return readPinStdin(os.Stdin) }
	activationPinSetup          = func() (string, error) { return readPin(false) }
)

var factoryReleaseActivateCmd = &cobra.Command{
	Use:   "activate <slug>",
	Short: "Activate a delivery release on the bound system (distinct from the local `use` stamp)",
	Long: `Activate or reactivate a delivery release on the bound system.

The activation needs the release PIN of the signed-in person (set in Mission
Control; pass it with --pin or --pin-stdin). It records the release-selection gate
GATE-RELEASE-<slug> on the system it is run from. The server composes an
attributable source unless --source is supplied. Re-running
the activation on a system whose release is active but carries no such gate
records one without changing the release; a gate of that purpose and scope
that is open or answered otherwise is superseded by the next
GATE-RELEASE-<slug>-<n>, and the reads take the newest approved one.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		slug := strings.TrimSpace(args[0])
		if slug == "" {
			return fmt.Errorf("usage: modernpath factory release activate <slug>")
		}
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		pin, err := activationPin(env, releaseActivatePin, releaseActivatePinStdin)
		if err != nil {
			return err
		}
		return activateRelease(env, slug, releaseActivateSource, pin, releaseActivateCloseCurrent, releaseActivateReason)
	},
}

// activateRelease posts the server-owned release activation operation. An
// optional source is preserved; without it the server composes the source.
func activateRelease(env *factoryEnv, slug, source, pin string, closeCurrent bool, reason string) error {
	body := map[string]any{
		"action":        "release_activate",
		"slug":          slug,
		"close_current": closeCurrent,
	}
	if source != "" {
		body["source"] = source
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		body["reason"] = reason
	}
	if pin != "" {
		body["pin"] = pin
	}
	data, err := authorPost(env, body)
	if err != nil {
		return err
	}
	if row, ok := data["release_activation"].(map[string]any); ok {
		printSuccess("release %s is now %s on this system (source: %s; closed: %s)", str(row, "slug"), str(row, "status"), str(row, "source"), closedReleaseNames(row))
	} else {
		printSuccess("release %s activated on this system", slug)
	}
	return nil
}

func activationPin(env *factoryEnv, supplied string, fromStdin bool) (string, error) {
	status, response, err := env.call("GET", "/api/compliance/pin", nil)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("PIN status failed (HTTP %d): %s", status, str(response, "error"))
	}
	data, ok := response["data"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("PIN status response is missing data.has_pin")
	}
	hasPin, ok := data["has_pin"].(bool)
	if !ok {
		return "", fmt.Errorf("PIN status response is missing data.has_pin")
	}
	if supplied != "" {
		if !hasPin {
			return "", fmt.Errorf("no compliance PIN is set — run 'modernpath factory pin set --pin-stdin' before non-interactive activation")
		}
		return supplied, nil
	}
	if fromStdin {
		if !hasPin {
			return "", fmt.Errorf("no compliance PIN is set — run 'modernpath factory pin set --pin-stdin' before non-interactive activation")
		}
		return activationPinFromStdin()
	}
	if hasPin {
		if !stdinIsTerminal() {
			return "", fmt.Errorf("no terminal for a PIN prompt — pipe the PIN and pass --pin-stdin")
		}
		return promptHiddenPin("Compliance PIN: ")
	}
	if !stdinIsTerminal() {
		return "", fmt.Errorf("no compliance PIN is set — run 'modernpath factory pin set --pin-stdin' before non-interactive activation")
	}
	pin, err := activationPinSetup()
	if err != nil {
		return "", err
	}
	if err := setCompliancePin(env, pin); err != nil {
		return "", err
	}
	return pin, nil
}

func closedReleaseNames(row map[string]any) string {
	closed, _ := row["closed_releases"].([]any)
	names := make([]string, 0, len(closed))
	for _, item := range closed {
		if release, ok := item.(map[string]any); ok {
			names = append(names, str(release, "name"))
		}
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}
