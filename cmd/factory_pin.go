package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// REQ-CROSS-361 (EPIC-CLI-014): set the caller's compliance PIN from the CLI.
// The compliance PIN (Core.Compliance.PinAuth, REQ-CMP-004) guards release
// signoff and release activation, but the only PIN-setup UI is unreachable in
// the product frontend — so a CLI-first user had no way to establish one. This
// wraps the same authenticated set-PIN endpoint the frontend already calls.

var pinSetStdin bool

var pinFormat = regexp.MustCompile(`^\d{4,6}$`)

var factoryPinCmd = &cobra.Command{
	Use:   "pin",
	Short: "Manage your compliance PIN (the guard release signoff and activation require)",
}

var factoryPinSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set your compliance PIN (4-6 digits)",
	Long: "Set your compliance PIN — the same PIN release signoff verifies and release\n" +
		"activation requires. By default it prompts twice with no echo and requires the\n" +
		"two entries to match. For automation, --pin-stdin reads the PIN from stdin;\n" +
		"feed it from a file or secret store rather than an inline `echo` (which would\n" +
		"itself land in shell history / `ps`). This command never takes the PIN as a\n" +
		"command-line argument.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		pin, err := readPin(pinSetStdin)
		if err != nil {
			return err
		}
		return setCompliancePin(env, pin)
	},
}

// readPin obtains a PIN without ever exposing it in argv. With --pin-stdin it
// reads one line from stdin (automation). Otherwise it prompts twice with no
// echo and requires the entries to match — the same typo guard the setup UI has,
// so a mistyped PIN cannot silently become the durable per-user compliance PIN.
func readPin(fromStdin bool) (string, error) {
	if fromStdin {
		return readPinStdin(os.Stdin)
	}
	if !stdinIsTerminal() {
		return "", fmt.Errorf("no terminal for a PIN prompt — pipe the PIN and pass --pin-stdin for non-interactive use")
	}
	first, err := promptHiddenPin("New compliance PIN (4-6 digits): ")
	if err != nil {
		return "", err
	}
	second, err := promptHiddenPin("Confirm PIN: ")
	if err != nil {
		return "", err
	}
	if first != second {
		return "", fmt.Errorf("the two PINs do not match")
	}
	return first, nil
}

// readPinStdin reads exactly one line from r and trims it — the --pin-stdin path,
// so the PIN never appears in argv, `ps`, or shell history.
func readPinStdin(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" && err != nil {
		return "", fmt.Errorf("read PIN from stdin: %w", err)
	}
	return line, nil
}

// promptHiddenPin writes a prompt to stderr and reads a line with no terminal
// echo (mirrors authenticateWithManualToken's manual-token read in auth.go).
func promptHiddenPin(label string) (string, error) {
	fmt.Fprint(os.Stderr, label)
	raw, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

// setCompliancePin POSTs the PIN to the compliance set-PIN endpoint — the same
// Core.Compliance.PinAuth.set_pin the frontend calls (REQ-CMP-004). The server
// validates 4-6 digits; we validate here too so a malformed PIN fails before the
// network call and never leaves the machine.
func setCompliancePin(env *factoryEnv, pin string) error {
	if !pinFormat.MatchString(pin) {
		return fmt.Errorf("a compliance PIN must be 4-6 digits")
	}
	status, resp, err := env.call("POST", "/api/compliance/pin", map[string]any{"pin": pin})
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		if msg, _ := resp["error"].(string); msg != "" {
			return fmt.Errorf("set PIN failed (%d): %s", status, msg)
		}
		return fmt.Errorf("set PIN failed (HTTP %d)", status)
	}
	printSuccess("compliance PIN set — release signoff and activation will accept it")
	return nil
}
