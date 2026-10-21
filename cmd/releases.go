package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"io"
	"os"
	"strconv"
	"strings"
)

func init() {
	rootCmd.AddCommand(newReleasesCommand(cliEnvLoad))
	// Keep only the two supported legacy entry points, not the retired factory machinery.
	factory := &cobra.Command{Use: "factory", Short: "Compatibility commands; use releases instead"}
	release := &cobra.Command{Use: "release", Short: "Compatibility release commands"}
	release.AddCommand(newReleaseActivate(cliEnvLoad))
	factory.AddCommand(release, newReleasePin(cliEnvLoad))
	rootCmd.AddCommand(factory)
}

func newReleasesCommand(load func() (*cliEnv, error)) *cobra.Command {
	root := &cobra.Command{Use: "releases", Short: "Read and activate delivery releases in the bound System"}
	root.AddCommand(newReleaseList(load), newReleaseActivate(load), newReleasePin(load))
	return root
}

func releaseList(env *cliEnv) ([]any, error) {
	status, body, err := env.get(fmt.Sprintf("/api/compliance/releases?system_id=%d", env.SystemID))
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, releaseFailure(status, body)
	}
	data, _ := body["data"].(map[string]any)
	items, ok := data["items"].([]any)
	if !ok {
		return nil, fmt.Errorf("release list is missing its items")
	}
	for _, raw := range items {
		row, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid release row")
		}
		sid, valid := numberAsInt(row["system_id"])
		id, idOK := numberAsInt(row["id"])
		if !valid || sid != int64(env.SystemID) || !idOK || id <= 0 {
			return nil, fmt.Errorf("release row has no identity or belongs to a different System")
		}
	}
	return items, nil
}

func newReleaseList(load func() (*cliEnv, error)) *cobra.Command {
	c := &cobra.Command{Use: "list", Short: "List releases in the bound System", Args: cobra.NoArgs}
	c.Flags().Bool("json", false, "Emit JSON")
	c.RunE = func(c *cobra.Command, _ []string) error {
		env, err := load()
		if err != nil {
			return err
		}
		if env.SystemID <= 0 {
			return fmt.Errorf("releases require a bound System")
		}
		items, err := releaseList(env)
		if err != nil {
			return err
		}
		return writeRecord(c, map[string]any{"items": items})
	}
	return c
}

func newReleaseActivate(load func() (*cliEnv, error)) *cobra.Command {
	c := &cobra.Command{Use: "activate <id|slug>", Short: "Make a delivery release active with your compliance PIN", Args: cobra.ExactArgs(1),
		Long: "Activate or reactivate a release in the bound System. Use --pin-stdin for secret-store input; otherwise the terminal prompts without echo. --close-current consents to closing the active incumbent read from this System. Unfinished work must be handled through the release close flow first. Creation stays planned. No retired process-store gate is written."}
	c.Flags().String("by", "auto", "Select by auto, id, or exact slug; disambiguate numeric slugs explicitly")
	c.Flags().Bool("pin-stdin", false, "Read PIN from stdin instead of a hidden terminal prompt")
	c.Flags().Bool("close-current", false, "Explicitly consent to closing the current active delivery release")
	c.Flags().String("reason", "", "Optional activation reason")
	c.Flags().Bool("json", false, "Emit JSON")
	c.RunE = func(c *cobra.Command, args []string) error {
		env, err := load()
		if err != nil {
			return err
		}
		if env.SystemID <= 0 {
			return fmt.Errorf("activation requires a bound System")
		}
		items, err := releaseList(env)
		if err != nil {
			return err
		}
		requested := args[0]
		id, numericErr := strconv.ParseInt(requested, 10, 64)
		by, _ := c.Flags().GetString("by")
		if by != "auto" && by != "id" && by != "slug" {
			return fmt.Errorf("--by must be auto, id, or slug")
		}
		if by == "id" && (numericErr != nil || id <= 0) {
			return fmt.Errorf("--by id requires a positive release ID")
		}
		var target map[string]any
		var incumbent map[string]any
		for _, raw := range items {
			row := raw.(map[string]any)
			rid, _ := numberAsInt(row["id"])
			idMatch := by != "slug" && numericErr == nil && id > 0 && rid == id
			slugMatch := by != "id" && row["slug"] == requested
			if idMatch || slugMatch {
				if target != nil {
					return fmt.Errorf("ambiguous release; use --by id or --by slug to select explicitly")
				}
				target = row
			}
			if row["status"] == "active" && row["is_base"] != true {
				if incumbent != nil {
					return fmt.Errorf("multiple active delivery releases; resolve this before activation")
				}
				incumbent = row
			}
		}
		if target == nil {
			return fmt.Errorf("release not found in the bound System")
		}
		if target["is_base"] == true {
			return fmt.Errorf("Base cannot be activated")
		}
		id, _ = numberAsInt(target["id"])
		consent, _ := c.Flags().GetBool("close-current")
		payload := map[string]any{"system_id": env.SystemID, "close_current": false}
		if incumbent != nil && incumbent["id"] != target["id"] {
			if !consent {
				return fmt.Errorf("another release is active; use --close-current only if you consent to closing it")
			}
			payload["close_current"] = true
			payload["expected_current_release_id"] = incumbent["id"]
		}
		pin, err := readReleasePIN(c, false)
		if err != nil {
			return err
		}
		reason, _ := c.Flags().GetString("reason")
		payload["pin"] = pin
		payload["reason"] = strings.TrimSpace(reason)
		status, body, err := env.write("POST", fmt.Sprintf("/api/compliance/releases/%d/activate", id), payload)
		if err != nil {
			return fmt.Errorf("activation request failed: %s", strings.ReplaceAll(err.Error(), pin, "[redacted]"))
		}
		if status != 200 {
			return releaseFailure(status, body)
		}
		data, _ := body["data"].(map[string]any)
		row, _ := data["release"].(map[string]any)
		rid, ok := numberAsInt(row["id"])
		sid, scoped := numberAsInt(row["system_id"])
		if !ok || rid != id || !scoped || sid != int64(env.SystemID) || row["status"] != "active" {
			return fmt.Errorf("activation receipt names a different release/System or is not active")
		}
		return writeRecord(c, map[string]any{"release": selectKeys(row, []string{"id", "name", "slug", "status", "system_id"})})
	}
	return c
}

func newReleasePin(load func() (*cliEnv, error)) *cobra.Command {
	root := &cobra.Command{Use: "pin", Short: "Manage your compliance PIN"}
	for _, action := range []string{"status", "set"} {
		action := action
		c := &cobra.Command{Use: action, Args: cobra.NoArgs, Short: action + " your compliance PIN"}
		c.Flags().Bool("json", false, "Emit JSON")
		if action == "set" {
			c.Flags().Bool("pin-stdin", false, "Read a 4–6 digit PIN from stdin instead of prompting twice")
		}
		c.RunE = func(c *cobra.Command, _ []string) error {
			env, err := load()
			if err != nil {
				return err
			}
			method := "GET"
			var input any
			pin := ""
			if action == "set" {
				pin, err = readReleasePIN(c, true)
				if err != nil {
					return err
				}
				input = map[string]any{"pin": pin}
				method = "POST"
			}
			status, body, err := env.request(method, "/api/compliance/pin", input)
			if err != nil {
				if pin != "" {
					return fmt.Errorf("PIN request failed: %s", strings.ReplaceAll(err.Error(), pin, "[redacted]"))
				}
				return err
			}
			if status < 200 || status >= 300 {
				return releaseFailure(status, body)
			}
			data, _ := body["data"].(map[string]any)
			has, ok := data["has_pin"].(bool)
			if !ok || (action == "set" && !has) {
				return fmt.Errorf("invalid PIN status receipt")
			}
			return writeRecord(c, map[string]any{"has_pin": has})
		}
		root.AddCommand(c)
	}
	return root
}

func readReleasePIN(c *cobra.Command, confirm bool) (string, error) {
	fromStdin, _ := c.Flags().GetBool("pin-stdin")
	var pin string
	if fromStdin {
		b, err := io.ReadAll(io.LimitReader(c.InOrStdin(), 64))
		if err != nil {
			return "", fmt.Errorf("cannot read PIN")
		}
		pin = strings.TrimSpace(string(b))
	} else {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return "", fmt.Errorf("PIN requires a terminal or --pin-stdin")
		}
		fmt.Fprint(c.ErrOrStderr(), "Compliance PIN: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(c.ErrOrStderr())
		if err != nil {
			return "", fmt.Errorf("cannot read PIN")
		}
		pin = string(b)
		if confirm {
			fmt.Fprint(c.ErrOrStderr(), "Confirm PIN: ")
			b, err = term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(c.ErrOrStderr())
			if err != nil || string(b) != pin {
				return "", fmt.Errorf("PINs do not match")
			}
		}
	}
	if len(pin) < 4 || len(pin) > 6 || strings.IndexFunc(pin, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return "", fmt.Errorf("PIN must be 4–6 digits")
	}
	return pin, nil
}

// Never render arbitrary response/error bodies from an endpoint that receives a secret.
func releaseFailure(status int, body map[string]any) error {
	code, _ := body["error"].(string)
	switch code {
	case "pin_required", "pin_not_set", "invalid_pin", "pin_locked", "locked", "release_open", "incumbent_changed", "incumbent_has_unfinished_work", "not_found", "forbidden", "base_release", "invalid_close_current", "invalid_incumbent", "invalid_reason":
		return fmt.Errorf("release operation refused (HTTP %d): %s", status, code)
	}
	if status == 404 {
		return fmt.Errorf("release endpoint unavailable (HTTP 404); deploy a server supporting native release activation")
	}
	return fmt.Errorf("release operation refused (HTTP %d)", status)
}
