package cmd

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// REQ-PLN-134 §134.3 (EPIC-NEXT-005): `modernpath focus` is the CLI door onto
// per-person focus. It declares (`focus <id>`), ends (`--clear`) or lists (no
// argument) through /api/v1/focus with the workspace bearer. Visibility only —
// it assigns nothing and locks nothing. The `--infer` conclusion form is
// REQ-PLN-135.

var (
	focusClearFlag   bool
	focusDropRef     string
	focusInferRef    string
	focusInferSource string
)

var focusCmd = &cobra.Command{
	Use:   "focus [REQ-*|EPIC-*]",
	Short: "Declare what you're working on right now (visibility only — no assignment, no lock)",
	Long: `Declare what you are on so teammates see it where they pick work.

  modernpath focus REQ-PLN-133   declare focus on a requirement or epic
  modernpath focus --drop REQ-X  drop your focus on one ref (you can hold several)
  modernpath focus --clear       end all your focus lanes
  modernpath focus               list who is on what, on this system`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}

		switch {
		case focusInferRef != "":
			return runFocusInfer(env)
		case focusDropRef != "":
			return runFocusDrop(env, focusDropRef)
		case focusClearFlag:
			return runFocusClear(env)
		case len(args) == 1:
			return runFocusDeclare(env, args[0])
		default:
			return runFocusList(env)
		}
	},
}

func init() {
	focusCmd.Flags().BoolVar(&focusClearFlag, "clear", false, "End all your focus lanes")
	focusCmd.Flags().StringVar(&focusDropRef, "drop", "", "Drop your focus on one ref (REQ-*|EPIC-*)")
	// --infer is internal: the context hook and factory sync spawn it to post a
	// CLI-confirmed inferred conclusion (REQ-PLN-135). Not for interactive use.
	focusCmd.Flags().StringVar(&focusInferRef, "infer", "", "Post an inferred conclusion for a ref (internal)")
	focusCmd.Flags().StringVar(&focusInferSource, "source", "", "The inference source for --infer (branch|commit|prompt|cli)")
	_ = focusCmd.Flags().MarkHidden("infer")
	_ = focusCmd.Flags().MarkHidden("source")
	rootCmd.AddCommand(focusCmd)
}

// focusOutcome separates the HTTP result from the printing so the doors are
// testable without a terminal (the statusReport idiom).
type focusOutcome struct {
	status  int
	focus   map[string]any
	ended   []map[string]any
	list    []map[string]any
	message string
}

func focusDeclare(env *factoryEnv, id string) (focusOutcome, error) {
	ref := strings.ToUpper(strings.TrimSpace(id))

	status, body, err := env.call("POST", "/api/v1/focus", map[string]any{
		"system_id":       env.SystemID,
		"ref_external_id": ref,
		"source":          "cli",
	})
	if err != nil {
		return focusOutcome{}, err
	}
	if status != 200 {
		return focusOutcome{status: status, message: focusServerMessage(body)}, nil
	}
	return focusOutcome{status: 200, focus: focusEntry(body, "focus")}, nil
}

func focusClear(env *factoryEnv) (focusOutcome, error) {
	status, body, err := env.call("DELETE", fmt.Sprintf("/api/v1/focus?system_id=%d", env.SystemID), nil)
	if err != nil {
		return focusOutcome{}, err
	}
	if status != 200 {
		return focusOutcome{status: status, message: focusServerMessage(body)}, nil
	}
	return focusOutcome{status: 200, ended: focusEntryList(body, "ended")}, nil
}

// focusDrop ends the caller's lane on one ref — DELETE ?ref= (REQ-PLN-142
// §142.5). Other lanes are untouched.
func focusDrop(env *factoryEnv, ref string) (focusOutcome, error) {
	ref = strings.ToUpper(strings.TrimSpace(ref))
	path := fmt.Sprintf("/api/v1/focus?system_id=%d&ref=%s", env.SystemID, url.QueryEscape(ref))
	status, body, err := env.call("DELETE", path, nil)
	if err != nil {
		return focusOutcome{}, err
	}
	if status != 200 {
		return focusOutcome{status: status, message: focusServerMessage(body)}, nil
	}
	return focusOutcome{status: 200, ended: focusEntryList(body, "ended")}, nil
}

func focusList(env *factoryEnv) (focusOutcome, error) {
	status, body, err := env.call("GET", fmt.Sprintf("/api/v1/focus?system_id=%d", env.SystemID), nil)
	if err != nil {
		return focusOutcome{}, err
	}
	if status != 200 {
		return focusOutcome{status: status, message: focusServerMessage(body)}, nil
	}

	var list []map[string]any
	if raw, ok := dataOf(body)["focus_states"].([]any); ok {
		for _, item := range raw {
			if m, ok := item.(map[string]any); ok {
				list = append(list, m)
			}
		}
	}
	return focusOutcome{status: 200, list: list}, nil
}

// focusInfer posts one CLI-confirmed inferred conclusion with the heartbeat
// identity (REQ-PLN-135 §135.4). Declared-wins is enforced server-side; the
// response carries the resulting current focus.
func focusInfer(env *factoryEnv, ref, source string) (focusOutcome, error) {
	status, body, err := env.call("POST", "/api/v1/focus", map[string]any{
		"system_id":           env.SystemID,
		"ref_external_id":     strings.ToUpper(strings.TrimSpace(ref)),
		"set_by":              "inferred",
		"source":              source,
		"workspace_ref":       filepath.Base(env.Root),
		"machine_fingerprint": machineFingerprint(env.Root),
	})
	if err != nil {
		return focusOutcome{}, err
	}
	if status != 200 {
		return focusOutcome{status: status, message: focusServerMessage(body)}, nil
	}
	return focusOutcome{status: 200, focus: focusEntry(body, "focus")}, nil
}

func focusEntry(body map[string]any, key string) map[string]any {
	if m, ok := dataOf(body)[key].(map[string]any); ok {
		return m
	}
	return nil
}

// focusEntryList reads `data.<key>` as an array of entries — the DELETE door
// returns the ended lanes as a list (REQ-PLN-142 §142.5: all-clear ends N).
func focusEntryList(body map[string]any, key string) []map[string]any {
	var out []map[string]any
	if raw, ok := dataOf(body)[key].([]any); ok {
		for _, item := range raw {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

func focusServerMessage(body map[string]any) string {
	if e, ok := body["error"].(map[string]any); ok {
		if msg, ok := e["message"].(string); ok && msg != "" {
			return msg
		}
	}
	return "the server refused the request"
}

func formatFocusLine(entry map[string]any) string {
	ref := str(entry, "ref_external_id")
	setBy := str(entry, "set_by")
	title := str(entry, "title")
	resolved, _ := entry["resolved"].(bool)

	if resolved && title != "" {
		return fmt.Sprintf("%s — %s (%s)", ref, title, setBy)
	}
	return fmt.Sprintf("%s (not in this system's ledger) (%s)", ref, setBy)
}

func formatFocusListLine(entry map[string]any) string {
	name := "user"
	if u, ok := entry["user"].(map[string]any); ok {
		if n := str(u, "name"); n != "" {
			name = n
		}
	}

	ref := str(entry, "ref_external_id")
	title := str(entry, "title")
	line := fmt.Sprintf("%s · %s", name, ref)
	if title != "" {
		line += " — " + title
	}
	line += fmt.Sprintf(" · %s/%s", str(entry, "set_by"), str(entry, "source"))
	if age := focusAge(str(entry, "started_at")); age != "" {
		line += " · " + age
	}
	return line
}

func focusAge(startedAt string) string {
	if startedAt == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, startedAt)
	if err != nil {
		return ""
	}

	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func runFocusDeclare(env *factoryEnv, id string) error {
	out, err := focusDeclare(env, id)
	if err != nil {
		return err
	}
	if out.status != 200 {
		printWarning("%s", out.message)
		return nil
	}
	printSuccess("focus: %s", formatFocusLine(out.focus))
	return nil
}

func runFocusClear(env *factoryEnv) error {
	out, err := focusClear(env)
	if err != nil {
		return err
	}
	if out.status != 200 {
		printWarning("%s", out.message)
		return nil
	}
	if len(out.ended) == 0 {
		printInfo("no focus to clear")
		return nil
	}
	for _, e := range out.ended {
		printSuccess("cleared focus: %s", str(e, "ref_external_id"))
	}
	return nil
}

func runFocusDrop(env *factoryEnv, ref string) error {
	out, err := focusDrop(env, ref)
	if err != nil {
		return err
	}
	if out.status != 200 {
		printWarning("%s", out.message)
		return nil
	}
	if len(out.ended) == 0 {
		printInfo("no focus on %s to drop", strings.ToUpper(strings.TrimSpace(ref)))
		return nil
	}
	for _, e := range out.ended {
		printSuccess("dropped focus: %s", str(e, "ref_external_id"))
	}
	return nil
}

// runFocusInfer posts the conclusion and, on success, learns the server's
// resulting current focus (declared may have won) into the buffer. On failure
// it logs and leaves the current line as it was, so the next confirmation
// retries (§135.4) — never fatal, so a hook or sync never fails on it.
func runFocusInfer(env *factoryEnv) error {
	out, err := focusInfer(env, focusInferRef, focusInferSource)
	if err != nil {
		printWarning("inferred focus not recorded: %v", err)
		return nil
	}
	if out.status != 200 {
		printWarning("%s", out.message)
		return nil
	}
	if out.focus != nil {
		// REQ-PLN-143 §143.1: merge the posted lane into the caller's set (declared
		// may have won its ref) rather than replacing the other lanes.
		_ = mergeFocusLane(env.Root, str(out.focus, "ref_external_id"), str(out.focus, "set_by"), focusInferSource, time.Now())
	}
	return nil
}

func runFocusList(env *factoryEnv) error {
	out, err := focusList(env)
	if err != nil {
		return err
	}
	if out.status != 200 {
		printWarning("%s", out.message)
		return nil
	}
	if len(out.list) == 0 {
		printInfo("nobody has declared focus on this system")
		return nil
	}
	for _, entry := range out.list {
		fmt.Println(formatFocusListLine(entry))
	}
	return nil
}
