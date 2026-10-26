package cmd

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
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
	focusClearFlag       bool
	focusDropRef         string
	focusInferRef        string
	focusInferSource     string
	focusRequirementKind string
	focusRefKind         string
)

var requirementUUIDFocusRef = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

var focusCmd = &cobra.Command{
	Use:   "focus [<uuid>|<epic-id>] [--requirement-kind system_requirement|user_requirement] [--ref-kind backlog_record]",
	Short: "Declare what you're working on right now (visibility only — no assignment, no lock)",
	Long: `Declare what you are on so teammates see it where they pick work.

	  modernpath focus 8f3d6ca1-4dc2-4bc3-9f4a-1bc50e830001 --requirement-kind system_requirement
	  modernpath focus 42  declare focus on an epic
	  modernpath focus --drop <uuid-or-epic-id>  drop one focus lane
  modernpath focus <uuid> --ref-kind backlog_record  declare focus on a Backlog record
  modernpath focus --clear       end all your focus lanes
  modernpath focus               list who is on what, on this system`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if focusRefKind != "" && (focusRefKind != "backlog_record" || focusRequirementKind != "") {
			return fmt.Errorf("--ref-kind accepts backlog_record and cannot be combined with --requirement-kind")
		}
		if focusRequirementKind != "" && focusRequirementKind != "system_requirement" && focusRequirementKind != "user_requirement" {
			return fmt.Errorf("--requirement-kind accepts system_requirement or user_requirement")
		}
		if focusRefKind != "" && (focusClearFlag || (len(args) == 0 && focusDropRef == "" && focusInferRef == "")) {
			return fmt.Errorf("--ref-kind requires a Backlog UUID or --drop selection")
		}
		env, err := cliEnvLoad()
		if err != nil {
			return err
		}
		if machine, _ := cmd.Flags().GetBool("json"); machine {
			return runFocusJSON(cmd, env, args)
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
	focusCmd.Flags().Bool("json", false, "Emit focus rows or the change receipt as JSON")
	focusCmd.Flags().BoolVar(&focusClearFlag, "clear", false, "End all your focus lanes")
	focusCmd.Flags().StringVar(&focusDropRef, "drop", "", "Drop your focus on one typed UUID or Epic integer ID")
	focusCmd.Flags().StringVar(&focusRefKind, "ref-kind", "", "Use backlog_record with a Backlog UUID")
	focusCmd.Flags().StringVar(&focusRequirementKind, "requirement-kind", "", "Requirement kind required with a requirement UUID")
	// --infer posts a typed inferred focus conclusion.
	focusCmd.Flags().StringVar(&focusInferRef, "infer", "", "Post an inferred conclusion for a ref (internal)")
	focusCmd.Flags().StringVar(&focusInferSource, "source", "", "The inference source for --infer (branch|commit|prompt|cli)")
	_ = focusCmd.Flags().MarkHidden("infer")
	_ = focusCmd.Flags().MarkHidden("source")
	rootCmd.AddCommand(focusCmd)
}

func runFocusJSON(cmd *cobra.Command, env *cliEnv, args []string) error {
	var out focusOutcome
	var err error
	operation := "listed"
	switch {
	case focusInferRef != "":
		operation = "inferred"
		out, err = focusInfer(env, focusInferRef, focusInferSource, selectedFocusKind())
	case focusDropRef != "":
		operation = "dropped"
		out, err = focusDrop(env, focusDropRef, selectedFocusKind())
	case focusClearFlag:
		operation = "cleared"
		out, err = focusClear(env)
	case len(args) == 1:
		operation = "declared"
		out, err = focusDeclare(env, args[0], selectedFocusKind())
	default:
		out, err = focusList(env)
	}
	if err != nil {
		return err
	}
	if out.status != 200 {
		return fmt.Errorf("focus %s: %s", operation, out.message)
	}
	if operation == "listed" {
		items := make([]map[string]any, 0, len(out.list))
		for _, entry := range out.list {
			row := map[string]any{}
			for _, key := range []string{"ref_id", "ref_kind", "requirement_kind", "backlog_kind", "display_number", "title", "user", "set_by", "source", "started_at"} {
				if value, exists := entry[key]; exists {
					row[key] = value
				}
			}
			row["display_ref"] = focusDisplayRef(entry)
			items = append(items, row)
		}
		return writeCommandJSON(cmd, map[string]any{"items": items})
	}
	receipt := map[string]any{"operation": operation}
	if out.focus != nil {
		receipt["ref_id"] = out.focus["ref_id"]
		receipt["display_ref"] = focusDisplayRef(out.focus)
	} else {
		receipt["count"] = len(out.ended)
	}
	return writeRecord(cmd, receipt)
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

func focusDeclare(env *cliEnv, id, requirementKind string) (focusOutcome, error) {
	ref, kind, err := focusReference(id, requirementKind)
	if err != nil {
		return focusOutcome{}, err
	}

	request := map[string]any{
		"system_id": env.SystemID,
		"ref_id":    ref,
		"source":    "cli",
	}
	if kind == "backlog_record" {
		request["ref_kind"] = kind
	} else if kind != "" {
		request["requirement_kind"] = kind
	}
	status, body, err := env.write("POST", "/api/v1/focus", request)
	if err != nil {
		return focusOutcome{}, err
	}
	if status != 200 {
		return focusOutcome{status: status, message: focusServerMessage(body)}, nil
	}
	return focusOutcome{status: 200, focus: focusEntry(body, "focus")}, nil
}

func focusClear(env *cliEnv) (focusOutcome, error) {
	status, body, err := env.write("DELETE", fmt.Sprintf("/api/v1/focus?system_id=%d", env.SystemID), nil)
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
func focusDrop(env *cliEnv, ref, requirementKind string) (focusOutcome, error) {
	nativeRef, kind, err := focusReference(ref, requirementKind)
	if err != nil {
		return focusOutcome{}, err
	}
	path := fmt.Sprintf("/api/v1/focus?system_id=%d&ref_id=%s", env.SystemID, url.QueryEscape(fmt.Sprint(nativeRef)))
	if kind == "backlog_record" {
		path += "&ref_kind=" + kind
	} else if kind != "" {
		path += "&requirement_kind=" + url.QueryEscape(kind)
	}
	status, body, err := env.write("DELETE", path, nil)
	if err != nil {
		return focusOutcome{}, err
	}
	if status != 200 {
		return focusOutcome{status: status, message: focusServerMessage(body)}, nil
	}
	return focusOutcome{status: 200, ended: focusEntryList(body, "ended")}, nil
}

func focusList(env *cliEnv) (focusOutcome, error) {
	status, body, err := env.get(fmt.Sprintf("/api/v1/focus?system_id=%d", env.SystemID))
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

// focusInfer posts one inferred conclusion. Declared-wins is enforced server-side; the
// response carries the resulting current focus.
func focusInfer(env *cliEnv, ref, source, requirementKind string) (focusOutcome, error) {
	if requirementKind == "backlog_record" {
		return focusOutcome{}, fmt.Errorf("Backlog focus must be declared explicitly")
	}
	nativeRef, kind, err := focusReference(ref, requirementKind)
	if err != nil {
		return focusOutcome{}, err
	}
	request := map[string]any{
		"system_id": env.SystemID,
		"ref_id":    nativeRef,
		"set_by":    "inferred",
		"source":    source,
	}
	if kind != "" {
		request["requirement_kind"] = kind
	}
	status, body, err := env.write("POST", "/api/v1/focus", request)
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
	ref := focusDisplayRef(entry)
	setBy := str(entry, "set_by")
	title := str(entry, "title")
	resolved, _ := entry["resolved"].(bool)

	if resolved && title != "" {
		return fmt.Sprintf("%s — %s (%s)", ref, title, setBy)
	}
	return fmt.Sprintf("%s (not resolved in this system) (%s)", ref, setBy)
}

func formatFocusListLine(entry map[string]any) string {
	name := "user"
	if u, ok := entry["user"].(map[string]any); ok {
		if n := str(u, "name"); n != "" {
			name = n
		}
	}

	ref := focusDisplayRef(entry)
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

func focusDisplayRef(entry map[string]any) string {
	if ref := str(entry, "display_ref"); ref != "" {
		return ref
	}
	if str(entry, "ref_kind") == "epic" || str(entry, "ref_kind") == "backlog_record" {
		return fmt.Sprint(entry["ref_id"])
	}
	return "Requirement"
}

func selectedFocusKind() string {
	if focusRefKind != "" {
		return focusRefKind
	}
	return focusRequirementKind
}

func focusReference(value, requirementKind string) (any, string, error) {
	ref := strings.TrimSpace(value)
	if requirementUUIDFocusRef.MatchString(ref) {
		if requirementKind != "system_requirement" && requirementKind != "user_requirement" && requirementKind != "backlog_record" {
			return nil, "", fmt.Errorf("a UUID requires --requirement-kind system_requirement or user_requirement, or --ref-kind backlog_record")
		}
		return strings.ToLower(ref), requirementKind, nil
	}
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil && id > 0 {
		if requirementKind != "" {
			return nil, "", fmt.Errorf("--requirement-kind applies only to requirement UUIDs; Epic focus uses its integer ID")
		}
		return id, "", nil
	}
	return nil, "", fmt.Errorf("focus requires a typed requirement UUID, an explicit Backlog UUID or Epic integer ID")
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

func runFocusDeclare(env *cliEnv, id string) error {
	out, err := focusDeclare(env, id, selectedFocusKind())
	if err != nil {
		return err
	}
	if out.status != 200 {
		return fmt.Errorf("focus request refused: %s", out.message)
	}
	printSuccess("focus: %s", formatFocusLine(out.focus))
	return nil
}

func runFocusClear(env *cliEnv) error {
	out, err := focusClear(env)
	if err != nil {
		return err
	}
	if out.status != 200 {
		return fmt.Errorf("focus request refused: %s", out.message)
	}
	if len(out.ended) == 0 {
		printInfo("no focus to clear")
		return nil
	}
	for _, e := range out.ended {
		printSuccess("cleared focus: %s", focusDisplayRef(e))
	}
	return nil
}

func runFocusDrop(env *cliEnv, ref string) error {
	out, err := focusDrop(env, ref, selectedFocusKind())
	if err != nil {
		return err
	}
	if out.status != 200 {
		return fmt.Errorf("focus request refused: %s", out.message)
	}
	if len(out.ended) == 0 {
		if requirementUUIDFocusRef.MatchString(strings.TrimSpace(ref)) {
			printInfo("no focus on the selected requirement to drop")
		} else {
			printInfo("no focus on %s to drop", strings.ToUpper(strings.TrimSpace(ref)))
		}
		return nil
	}
	for _, e := range out.ended {
		printSuccess("dropped focus: %s", focusDisplayRef(e))
	}
	return nil
}

// runFocusInfer posts a typed inferred focus conclusion.
func runFocusInfer(env *cliEnv) error {
	out, err := focusInfer(env, focusInferRef, focusInferSource, selectedFocusKind())
	if err != nil {
		printWarning("inferred focus not recorded: %v", err)
		return nil
	}
	if out.status != 200 {
		printWarning("%s", out.message)
		return nil
	}
	return nil
}

func runFocusList(env *cliEnv) error {
	out, err := focusList(env)
	if err != nil {
		return err
	}
	if out.status != 200 {
		return fmt.Errorf("focus list refused: %s", out.message)
	}
	if len(out.list) == 0 {
		printInfo("nobody has declared focus on this system")
		return nil
	}
	for _, entry := range out.list {
		fmt.Println(strings.Join(strings.Fields(formatFocusListLine(entry)), " "))
	}
	return nil
}
