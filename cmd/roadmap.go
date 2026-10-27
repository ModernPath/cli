package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// Epic 324 (A1): modernpath roadmap — read the Roadmap of the bound System and
// apply a Roadmap plan from a file, on the routes under the System that need
// no Chat session. A coding agent applies a plan on a person's word; the
// receipt names the signed-in person and, with --agent, the agent.

// roadmapMaxRequestID is the longest request id the server accepts: it stores
// the id with the 4-byte prefix cli: in a 255-byte receipt key.
const roadmapMaxRequestID = 251

func init() { rootCmd.AddCommand(newRoadmapCommand(cliEnvLoad)) }

func newRoadmapCommand(load func() (*cliEnv, error)) *cobra.Command {
	parent := &cobra.Command{Use: "roadmap", Short: "Read the Roadmap of the bound System and apply a Roadmap plan from a file",
		Long: `The Roadmap of the bound System: its Releases, the Initiatives shown under each,
and the Initiatives with no Release by horizon. An Initiative has one bounded
context, a kind of work, and the Epics that deliver it. 'roadmap show' reads it;
'roadmap selection' writes the legacy requirements of a context, or an
Initiative's keep, change and drop decisions, to a file; 'roadmap apply'
applies a Roadmap plan from a file, on a person's word, with the checks Chat's
plan card runs.`}
	parent.AddCommand(newRoadmapShowCommand(load), newRoadmapSelectionCommand(load), newRoadmapApplyCommand(load))
	return parent
}

func newRoadmapShowCommand(load func() (*cliEnv, error)) *cobra.Command {
	show := &cobra.Command{Use: "show", Short: "Print the Roadmap: Releases with their Initiatives, then the rest by horizon", Args: cobra.NoArgs,
		Long: `Print the Roadmap of the bound System. Each Release comes first with the
Initiatives shown under it — an Initiative sits under the earliest open Release
of its Epics, else its own — then the Initiatives with no Release, by horizon
(Now, Next, Later). Each Initiative shows its horizon, its context (marked
"new context" while no requirement of the System has that code), its kind of
work, and the Epics it covers. A Release's and an Initiative's planning
context (decisions, assumptions, open questions and references, as stored)
prints under its name; an empty one prints nothing. The first line says whether
the Now plan is agreed. Under each Initiative a line says whether it is done:
Done; No work yet when none of its linked work counts; or Not done with how
many of its Epics, user requirements and system requirements are complete
(only the kinds it has). Under an Initiative that is not done, one line per
unfinished item of its linked work gives its step and whose turn it is, or
says it waits for the team to shape it into work.

Under a Modernization Release come the legacy system it replaces (or "in
place"), its reference revision, one line per context of its scope with how
many of its legacy requirements are undecided and how many decided
("Billing (BILL): 1 of 4 undecided, 3 decided"), and, when the Release has
legacy requirements that the business owner's latest agreement did not
record and no Initiative has decided yet, "New since the agreement: <n>
legacy requirements". Under an Initiative that replaces part of the legacy
system a line gives its keep, change and drop decisions
("Decisions: 2 keep, 1 change, 1 drop").

The context counts and the decisions both end with how many kept or changed
legacy requirements are drafted and built (", 2 drafted, 1 built"). Onto a new system, a legacy requirement is
drafted while an Epic's saved plan or a written trace links a new requirement
to it with a modernizes trace, and built once a written one has a DONE source.
In place, it is drafted while an Epic's saved plan takes it or links a test to
it, and built once that Epic is DONE. Drafted includes built.

--json prints the served read: releases, no_release, contexts and plan_state,
with planning_context on each Release and Initiative; done, done_when,
waiting_for and selection (keep, change, drop, agreed, drafted and built, or
null for new work) on each Initiative; and modernization (each context's
in_scope, undecided, decided, drafted and built) and new_since_agreement_count
on each Release.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := roadmapEnv(load)
			if err != nil {
				return err
			}
			status, body, err := env.get("/api/v1/systems/" + strconv.Itoa(env.SystemID) + "/roadmap")
			data, err := roadmapData(status, body, err)
			if err != nil {
				return err
			}
			if machine, _ := cmd.Flags().GetBool("json"); machine {
				return writeCommandJSON(cmd, data)
			}
			return writeRoadmap(cmd, data)
		}}
	show.Flags().Bool("json", false, "Emit JSON")
	return show
}

func newRoadmapApplyCommand(load func() (*cliEnv, error)) *cobra.Command {
	apply := &cobra.Command{Use: "apply", Short: "Apply a Roadmap plan from a file (on a person's word)", Args: cobra.NoArgs,
		Long: `Apply a Roadmap plan from --file (or --file - for stdin) to the bound System, in
one call and all or nothing, with the checks Chat's plan card runs. The file is
a JSON object {"request_id": "...", "operations": [...]}: create_release,
create_line, update_line, reorder, move_epic, archive_line and set_selection,
with the fields Chat's plan takes, at most 50 operations besides the
set_selection ones. create_line and update_line
take "context", an Initiative's bounded context: a requirement context code or
name of the System, or a new one (an empty string on update_line clears it).
They also take "planning_context", the Initiative's whole planning context:
{"decisions":[{"statement","rationale"?}], "assumptions":[{"statement"}],
"open_questions":[{"question","status":"open"|"resolved","resolution"?}],
"references":[{"kind":"document"|"file","target_ref"}]}. A list left out is
empty, update_line replaces the whole planning context and {} clears it; each
reference names a document (document:doc:<id>, document:sd:<uuid>) or a file
(file:v2:...) of the workspace. A field outside an operation's kind is
refused, naming it.

set_selection sets the selection of an Initiative that replaces part of the
legacy system: its keep, change and drop decisions on the legacy requirements
of its Release, one row per legacy requirement.
{"op": "set_selection", "line_id": "<initiative id>", "rows": [...], "clear": [...]}
names the Initiative by "line_id", or by "line_ref", the "ref" a create_line
earlier in the plan gives the Initiative it creates ({"op": "create_line",
"ref": "provider", ...}). Each row is {"requirement_kind": "user_requirement" |
"system_requirement", "requirement_id": "<uuid>", "outcome": "keep" | "change"
| "drop", "reason": "...", "expected_level": "compared" | "screens_compared" |
"tests_only", "expected_level_reason": "...", "move": true}: a drop needs a
reason; an expected level, with its reason, goes only on a keep row; a row
without outcome changes only the expected level of the Initiative's keep row;
"move": true takes a legacy requirement another Initiative holds. Each "clear"
entry {"requirement_kind", "requirement_id"} deletes the Initiative's row, so
the legacy requirement is undecided again. One set_selection takes at most
1,000 rows, and a plan at most 5,000 rows in all. A refused set_selection
lists each refused row by its position (rows first, then clear), its title, or
its requirement id when it names no legacy requirement of the Release, and the
reason; nothing is written. A held row cannot change, be cleared or move to
another Initiative while an approved Epic relies on its decision or a written
modernizes trace's new requirement modernizes it; the command prints a
sentence that says what holds it, who approved that and who can act.
'roadmap selection' writes the legacy requirements to decide.

The request id is the file's request_id, else --request-id, else derived from
the operations, so the same file yields the same id; a --request-id that
differs from the file's is refused. Applying a file again returns the saved
result and writes nothing; a new request id applies it again. A changed file
under the same request_id is refused as a conflict.

--dry-run checks the plan and prints each operation's summary and each
refused row; nothing is written. --agent names the coding agent (lowercase
letters, digits, '.', '_' and '-'); the receipt names it beside the signed-in
person.
Example: {"request_id":"q3-plan","operations":[{"op":"create_line","horizon":"next","text":"Gift cards","context":"Payments"}]}
Example: {"request_id":"intg-selection","operations":[{"op":"set_selection","line_id":"<initiative id>","rows":[{"requirement_kind":"user_requirement","requirement_id":"<uuid>","outcome":"drop","reason":"The bank takes the API now."}]}]}`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString("file")
			if path == "" {
				return fmt.Errorf("--file is required")
			}
			plan, err := readObjectInput(cmd, path)
			if err != nil {
				return err
			}
			operations, ok := plan["operations"].([]any)
			if !ok || len(operations) == 0 {
				return fmt.Errorf("the plan file needs a non-empty \"operations\" array")
			}
			for key := range plan {
				if key != "operations" && key != "request_id" {
					return fmt.Errorf("unsupported plan file field %q (allowed: request_id, operations)", key)
				}
			}
			flagID, _ := cmd.Flags().GetString("request-id")
			requestID, err := roadmapRequestID(plan, flagID, operations)
			if err != nil {
				return err
			}
			agent, _ := cmd.Flags().GetString("agent")
			dryRun, _ := cmd.Flags().GetBool("dry-run")

			env, err := roadmapEnv(load)
			if err != nil {
				return err
			}
			payload := map[string]any{"operations": operations, "request_id": requestID}
			if agent != "" {
				payload["agent"] = agent
			}
			if dryRun {
				payload["dry_run"] = true
			}
			status, body, err := env.write(http.MethodPost, "/api/v1/systems/"+strconv.Itoa(env.SystemID)+"/roadmap/plan", payload)
			if err == nil && status == http.StatusConflict {
				return fmt.Errorf("request id %s was already used for a different plan: nothing was written. "+
					"Change request_id in the file (or --request-id) to apply this plan", requestID)
			}
			data, err := roadmapData(status, body, err)
			if err != nil {
				return err
			}
			if machine, _ := cmd.Flags().GetBool("json"); machine {
				return writeCommandJSON(cmd, data)
			}
			if dryRun {
				return writeRoadmapDryRun(cmd, data)
			}
			return writeRoadmapReceipt(cmd, data, requestID)
		}}
	apply.Flags().String("file", "", "Roadmap plan JSON file, or - for stdin (required)")
	apply.Flags().String("request-id", "", "Request id when the file has none (1 to 251 bytes)")
	apply.Flags().String("agent", "", "The coding agent applying the plan, recorded beside the signed-in person")
	apply.Flags().Bool("dry-run", false, "Check the plan and print each operation's summary; write nothing")
	apply.Flags().Bool("json", false, "Emit JSON")
	return apply
}

// roadmapEnv refuses an unbound workspace before it loads credentials.
func roadmapEnv(load func() (*cliEnv, error)) (*cliEnv, error) {
	if cfg, err := commandConfig(); err == nil && cfg.SystemID <= 0 {
		return nil, fmt.Errorf("roadmap requires a bound System; run 'modernpath init' to bind one")
	}
	env, err := load()
	if err != nil {
		return nil, err
	}
	if env.SystemID <= 0 {
		return nil, fmt.Errorf("roadmap requires a bound System; run 'modernpath init' to bind one")
	}
	return env, nil
}

// roadmapRequestID takes the file's request_id, else --request-id, else an id
// derived from the operations: the same operations, in any key order and
// spacing, derive the same id.
func roadmapRequestID(plan map[string]any, flagID string, operations []any) (string, error) {
	fileID, present := plan["request_id"]
	if present {
		id, ok := fileID.(string)
		if !ok || strings.TrimSpace(id) == "" {
			return "", fmt.Errorf("request_id in the plan file must be a non-empty string")
		}
		if flagID != "" && flagID != id {
			return "", fmt.Errorf("--request-id %s differs from the plan file's request_id %s; use the file's, or remove it from the file", flagID, id)
		}
		return checkRoadmapRequestID(id)
	}
	if flagID != "" {
		return checkRoadmapRequestID(flagID)
	}
	canonical, err := json.Marshal(operations)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return "plan-" + hex.EncodeToString(sum[:]), nil
}

func checkRoadmapRequestID(id string) (string, error) {
	if len(id) == 0 || len(id) > roadmapMaxRequestID {
		return "", fmt.Errorf("request id must be 1 to %d bytes", roadmapMaxRequestID)
	}
	if strings.HasPrefix(id, "chat:") {
		return "", fmt.Errorf("request id must not start with chat: (Chat's plan cards use it)")
	}
	return id, nil
}

// roadmapData unwraps {data: ...}; a refusal names the server code and what
// it points at (the operation, the field, the codes, the rule).
func roadmapData(status int, body map[string]any, err error) (map[string]any, error) {
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("Roadmap request refused (server %d): %s", status, roadmapRefusal(body))
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("roadmap response is missing its data")
	}
	return data, nil
}

func roadmapRefusal(body map[string]any) string {
	code := str(body, "error")
	if code == "" {
		return refusalText(body)
	}
	var parts []string
	if index, ok := body["index"]; ok && index != nil {
		parts = append(parts, fmt.Sprintf("operation %v", index))
	}
	if op := str(body, "op"); op != "" {
		parts = append(parts, op)
	}
	if row, ok := body["row"]; ok && row != nil {
		parts = append(parts, fmt.Sprintf("row %v", row))
	}
	if field := str(body, "field"); field != "" {
		parts = append(parts, "field "+field)
	}
	if rule := str(body, "rule"); rule != "" {
		parts = append(parts, "the agent name must match "+rule)
	}
	if codes, ok := body["codes"].([]any); ok && len(codes) > 0 {
		parts = append(parts, "codes "+joinAny(codes))
	}
	if releases, ok := body["releases"].([]any); ok && len(releases) > 0 {
		parts = append(parts, "releases "+roadmapReleaseNames(releases))
	}
	// A refused set_selection lists its refused rows on the lines under the
	// refusal (Epic 332, B3).
	var rows strings.Builder
	if errs, ok := body["errors"].([]any); ok {
		for _, e := range errs {
			if m, ok := e.(map[string]any); ok {
				if index, ok := m["index"]; ok && index != nil {
					parts = append(parts, fmt.Sprintf("operation %v: %s", index, str(m, "reason")))
				} else {
					parts = append(parts, "the plan: "+str(m, "reason"))
				}
				if refused, _ := m["rows"].([]any); len(refused) > 0 {
					rows.WriteString("\n" + roadmapPlanError(m) + "\n")
					writeRoadmapRefusedRows(&rows, refused, "  ")
				}
			}
		}
	}
	text := code
	if len(parts) > 0 {
		text += " (" + strings.Join(parts, "; ") + ")"
	}
	return text + strings.TrimRight(rows.String(), "\n")
}

// roadmapReleaseNames names each Release of a refusal by its name and id.
func roadmapReleaseNames(releases []any) string {
	names := make([]string, 0, len(releases))
	for _, r := range releases {
		if m, ok := r.(map[string]any); ok {
			names = append(names, fmt.Sprintf("%s (id %v)", str(m, "name"), m["id"]))
		}
	}
	return strings.Join(names, ", ")
}

// roadmapPlanReasons are the sentences of the reasons a set_selection, or a
// move of an Initiative with decisions, is refused for: the words Chat's plan
// card shows (Epic 332, B3).
var roadmapPlanReasons = map[string]string{
	"selection_rows_refused":         "Some rows of this initiative's selection are refused.",
	"initiative_not_replacing":       "Only an initiative that replaces part of the legacy system has a selection.",
	"initiative_context_required":    "Give the initiative a context before setting its selection.",
	"release_not_modernization":      "The initiative's release does not replace a legacy system.",
	"unknown_line_ref":               "No initiative earlier in the plan has this ref.",
	"too_many_rows":                  "An initiative's selection takes at most 1,000 rows, and a plan at most 5,000.",
	"requirement_not_found":          "This is not a legacy requirement of the initiative's release.",
	"requirement_not_in_scope":       "This legacy requirement is outside the scope of the initiative's release.",
	"reason_required":                "A drop in an initiative's selection needs a reason.",
	"invalid_outcome":                "A row of an initiative's selection is keep, change or drop.",
	"invalid_expected_level":         "An expected level, with its reason, goes only on a keep row of an initiative.",
	"selected_by_another_initiative": "Another initiative already holds this legacy requirement.",
	"duplicate_requirement":          "The initiative's selection names this legacy requirement twice.",
	"selection_release_mismatch":     "The initiative has keep, change and drop decisions, so it stays in a release that replaces its legacy system.",
	"selection_row_held":             "This legacy requirement's decision is held by an approved Epic or a written modernizes trace.",
}

// roadmapPlanError is one error of a plan: the operation, or the whole plan
// when the error names none, and its reason code, with the reason's sentence
// when it has one.
func roadmapPlanError(m map[string]any) string {
	where := "the plan"
	if index, ok := m["index"]; ok && index != nil {
		where = fmt.Sprintf("operation %v (%s)", index, orDash(str(m, "op")))
	}
	reason := str(m, "reason")
	if sentence := roadmapPlanReasons[reason]; sentence != "" {
		reason += " — " + sentence
	}
	return where + ": " + reason
}

// writeRoadmapRefusedRows writes one line per refused row of a set_selection:
// its position, its title (its requirement id when it names no legacy
// requirement of the Release), the reason in words and, for a legacy
// requirement another Initiative holds, that Initiative. A held row (Epic 334,
// C1) gets the sentence of roadmapHeldRow instead, which starts with its title.
func writeRoadmapRefusedRows(out io.Writer, rows []any, indent string) {
	for _, r := range rows {
		row, ok := r.(map[string]any)
		if !ok {
			continue
		}
		name := str(row, "title")
		if name == "" {
			name = str(row, "requirement_id")
		}
		if sentence, held := roadmapHeldRow(row, orDash(name)); held {
			fmt.Fprintf(out, "%srow %v: %s\n", indent, row["position"], sentence)
			continue
		}
		reason := str(row, "reason")
		if sentence := roadmapPlanReasons[reason]; sentence != "" {
			reason = sentence
		}
		line := fmt.Sprintf("%srow %v: %s: %s", indent, row["position"], orDash(name), reason)
		if held, ok := row["held_by"].(map[string]any); ok {
			line += " (held by " + str(held, "title") + ")"
		}
		fmt.Fprintln(out, line)
	}
}

// roadmapHeldRow is the sentence for a refused row whose decision a live trace
// holds (Epic 334, C1): what holds it (the approved Epic, or the new
// requirement of a written modernizes trace), who approved that and who can
// act; for a move, also the Initiative that holds the decision (held_by). held
// is false for a row without a usable holder, which keeps B3's line.
func roadmapHeldRow(row map[string]any, name string) (sentence string, held bool) {
	holder, ok := row["holder"].(map[string]any)
	if !ok || str(row, "reason") != "selection_row_held" {
		return "", false
	}
	approver, _ := holder["approver"].(map[string]any)
	title, by := str(holder, "title"), str(approver, "name")
	if title == "" || by == "" {
		return "", false
	}
	initiative, moved := row["held_by"].(map[string]any)
	var clause, act string
	switch str(holder, "kind") {
	case "epic":
		object := "its decision"
		if moved {
			object = "it"
		}
		clause = fmt.Sprintf("the Epic %s, approved by %s, relies on %s", title, by, object)
		act = by + " can return that Epic to planning."
	case "requirement":
		clause = fmt.Sprintf("%s, approved by %s, modernizes it", title, by)
		act = fmt.Sprintf("A later Epic that holds %s can retire that trace.", title)
	default:
		return "", false
	}
	if moved {
		return fmt.Sprintf("%s cannot move to this initiative: the initiative %s holds its decision, and %s. %s",
			name, orDash(str(initiative, "title")), clause, act), true
	}
	return fmt.Sprintf("%s cannot change: %s. %s", name, clause, act), true
}

func joinAny(values []any) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, fmt.Sprint(v))
	}
	return strings.Join(parts, ", ")
}

var roadmapPlanStates = map[string]string{
	"never_agreed":            "Now plan: never agreed",
	"agreed":                  "Now plan: agreed",
	"changed_since_agreement": "Now plan: changed since agreement",
}

var roadmapWorkKinds = map[string]string{"new_work": "new work", "replaces_legacy": "replaces legacy"}

func writeRoadmap(cmd *cobra.Command, data map[string]any) error {
	out := cmd.OutOrStdout()
	names := map[string]string{}
	if contexts, ok := data["contexts"].([]any); ok {
		for _, c := range contexts {
			if m, ok := c.(map[string]any); ok {
				names[str(m, "code")] = str(m, "name")
			}
		}
	}
	state := str(data, "plan_state")
	if words, ok := roadmapPlanStates[state]; ok {
		state = words
	} else {
		state = "Now plan: " + orDash(state)
	}
	fmt.Fprintln(out, state)

	releases, _ := data["releases"].([]any)
	for _, r := range releases {
		release, ok := r.(map[string]any)
		if !ok {
			continue
		}
		fmt.Fprintf(out, "\nRelease %s (%s%s)\n", str(release, "name"), orDash(str(release, "status")), roadmapTarget(release))
		writeRoadmapModernization(cmd, release, "  ")
		writePlanningContext(cmd, release["planning_context"], "  ")
		initiatives, _ := release["initiatives"].([]any)
		if len(initiatives) == 0 {
			fmt.Fprintln(out, "  (no initiatives)")
		}
		for _, i := range initiatives {
			if m, ok := i.(map[string]any); ok {
				fmt.Fprintln(out, "  "+roadmapInitiative(m, names))
				writeRoadmapDecisions(cmd, m, "    ")
				writeRoadmapDone(cmd, m, "    ")
				writePlanningContext(cmd, m["planning_context"], "    ")
			}
		}
	}

	fmt.Fprintln(out, "\nNo release")
	noRelease, _ := data["no_release"].(map[string]any)
	for _, horizon := range []string{"now", "next", "later"} {
		initiatives, _ := noRelease[horizon].([]any)
		fmt.Fprintf(out, "  %s\n", strings.ToUpper(horizon[:1])+horizon[1:])
		if len(initiatives) == 0 {
			fmt.Fprintln(out, "    (none)")
		}
		for _, i := range initiatives {
			if m, ok := i.(map[string]any); ok {
				fmt.Fprintln(out, "    "+roadmapInitiative(m, names))
				writeRoadmapDecisions(cmd, m, "      ")
				writeRoadmapDone(cmd, m, "      ")
				writePlanningContext(cmd, m["planning_context"], "      ")
			}
		}
	}
	return nil
}

// writeRoadmapDecisions prints under an Initiative that replaces part of the
// legacy system its keep, change and drop decisions (Epic 332, B3), then how
// many are drafted and built (Epic 334, C1). An Initiative whose selection is
// null, new work, prints nothing.
func writeRoadmapDecisions(cmd *cobra.Command, m map[string]any, indent string) {
	selection, ok := m["selection"].(map[string]any)
	if !ok {
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%sDecisions: %d keep, %d change, %d drop%s\n", indent,
		roadmapCount(selection["keep"]), roadmapCount(selection["change"]), roadmapCount(selection["drop"]),
		roadmapDraftedBuilt(selection))
}

// roadmapDraftedBuilt is ", <n> drafted, <n> built" for counts that carry
// drafted (Epic 334, C1), and nothing for a read from before them.
func roadmapDraftedBuilt(counts map[string]any) string {
	drafted, present := counts["drafted"]
	if !present {
		return ""
	}
	return fmt.Sprintf(", %d drafted, %d built", roadmapCount(drafted), roadmapCount(counts["built"]))
}

// writeRoadmapModernization prints under a Modernization Release (Epic 330,
// B1) the legacy system it replaces, or that it modernizes in place; its
// reference revision, or that none is set; and one line per context in its
// scope with how many of its requirements are undecided and, from Epic 332
// (B3), decided, and from Epic 334 (C1) drafted and built; then how many
// legacy requirements are new since the agreement, when any are. A Delivery
// Release, or a read without release_type, prints nothing.
func writeRoadmapModernization(cmd *cobra.Command, release map[string]any, indent string) {
	if str(release, "release_type") != "modernization" {
		return
	}
	out := cmd.OutOrStdout()
	kind := "Modernization"
	if inPlace, _ := release["in_place"].(bool); inPlace {
		kind += " · in place"
	} else if legacy := str(release, "legacy_system_name"); legacy != "" {
		kind += " · replacing " + legacy
	}
	fmt.Fprintln(out, indent+kind)
	revision := str(release, "legacy_reference_revision")
	if revision == "" {
		revision = "not set"
	}
	fmt.Fprintln(out, indent+"Reference revision: "+revision)
	modernization, _ := release["modernization"].(map[string]any)
	contexts, _ := modernization["contexts"].([]any)
	for _, c := range contexts {
		context, ok := c.(map[string]any)
		if !ok {
			continue
		}
		label := str(context, "code")
		if name := str(context, "name"); name != "" {
			label = name + " (" + label + ")"
		}
		line := fmt.Sprintf("%s%s: %d of %d undecided", indent, label,
			roadmapCount(context["undecided"]), roadmapCount(context["in_scope"]))
		if decided, present := context["decided"]; present {
			line += fmt.Sprintf(", %d decided", roadmapCount(decided))
		}
		line += roadmapDraftedBuilt(context)
		fmt.Fprintln(out, line)
	}
	if count := roadmapCount(release["new_since_agreement_count"]); count > 0 {
		fmt.Fprintf(out, "%sNew since the agreement: %s\n", indent, roadmapLegacyRequirements(count))
	}
}

// roadmapLegacyRequirements is a count of legacy requirements in words.
func roadmapLegacyRequirements(count int) string {
	if count == 1 {
		return "1 legacy requirement"
	}
	return fmt.Sprintf("%d legacy requirements", count)
}

// roadmapSteps and roadmapTurns are the Initiative page's words for the step
// and turn codes of waiting_for (Epic 326, A3).
var roadmapSteps = map[string]string{
	"blocked":             "Blocked",
	"not_shaped":          "Not shaped into work yet",
	"building":            "Being built",
	"plan_approval":       "Plan waiting for approval",
	"completion_approval": "Completion waiting for approval",
	"deferred":            "Deferred",
}

var roadmapTurns = map[string]string{"team": "The team", "person": "A person on the team"}

// roadmapCountKinds names the kinds of linked work in done_when's counts.
var roadmapCountKinds = []struct{ key, words string }{
	{"epic", "Epics"},
	{"user_requirement", "user requirements"},
	{"system_requirement", "system requirements"},
}

// writeRoadmapDone prints under an Initiative whether it is done (Epic 326,
// A3): Done; No work yet when no linked work counts; or Not done with n of m
// for each kind that has an item that counts. Under an Initiative that is not
// done, one line per item of waiting_for with its title, its step and whose
// turn it is. A read without done prints nothing.
func writeRoadmapDone(cmd *cobra.Command, m map[string]any, indent string) {
	done, ok := m["done"].(bool)
	if !ok {
		return
	}
	out := cmd.OutOrStdout()
	if done {
		fmt.Fprintln(out, indent+"Done")
		return
	}
	var parts []string
	if items, _ := m["done_when"].([]any); len(items) > 0 {
		for _, item := range items {
			entry, _ := item.(map[string]any)
			if str(entry, "key") != "work_complete" {
				continue
			}
			counts, _ := entry["counts"].(map[string]any)
			for _, kind := range roadmapCountKinds {
				count, _ := counts[kind.key].(map[string]any)
				counting := roadmapCount(count["counting"])
				if counting == 0 {
					continue
				}
				parts = append(parts, fmt.Sprintf("%d of %d %s", roadmapCount(count["done"]), counting, kind.words))
			}
		}
	}
	if len(parts) == 0 {
		fmt.Fprintln(out, indent+"No work yet")
	} else {
		fmt.Fprintln(out, indent+"Not done: "+strings.Join(parts, ", ")+" complete")
	}
	waiting, _ := m["waiting_for"].([]any)
	for _, w := range waiting {
		item, ok := w.(map[string]any)
		if !ok {
			continue
		}
		if str(item, "kind") == "initiative" {
			fmt.Fprintln(out, indent+"  Waits for the team to shape it into work")
			continue
		}
		title := str(item, "title")
		if ref := str(item, "display_ref"); ref != "" {
			title = ref + " " + title
		}
		step := roadmapSteps[str(item, "step")]
		if step == "" {
			step = orDash(str(item, "step"))
		}
		if epic, ok := item["member_epic"].(map[string]any); ok {
			step += " (Epic " + str(epic, "title") + ")"
		}
		turn := roadmapTurns[str(item, "turn")]
		if turn == "" {
			turn = orDash(str(item, "turn"))
		}
		fmt.Fprintf(out, "%s  Waits for %s: %s, %s\n", indent, title, step, turn)
	}
}

// roadmapCount reads a served count, a json.Number as the client decodes it
// or a float64.
func roadmapCount(value any) int {
	switch v := value.(type) {
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	case float64:
		return int(v)
	default:
		return 0
	}
}

// writePlanningContext prints a Release's or an Initiative's planning context
// (Epic 325, A2) under it, one entry per line, references as stored. An empty
// planning context, {} or four empty lists, prints nothing.
func writePlanningContext(cmd *cobra.Command, value any, indent string) {
	context, _ := value.(map[string]any)
	var lines []string
	for _, d := range planningContextEntries(context, "decisions") {
		line := "Decision: " + str(d, "statement")
		if rationale := str(d, "rationale"); rationale != "" {
			line += " (rationale: " + rationale + ")"
		}
		lines = append(lines, line)
	}
	for _, a := range planningContextEntries(context, "assumptions") {
		lines = append(lines, "Assumption: "+str(a, "statement"))
	}
	for _, q := range planningContextEntries(context, "open_questions") {
		line := "Open question: " + str(q, "question") + " [" + orDash(str(q, "status")) + "]"
		if resolution := str(q, "resolution"); resolution != "" {
			line += " resolution: " + resolution
		}
		lines = append(lines, line)
	}
	for _, r := range planningContextEntries(context, "references") {
		lines = append(lines, "Reference: "+orDash(str(r, "kind"))+" "+str(r, "target_ref"))
	}
	if len(lines) == 0 {
		return
	}
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, indent+"Planning context:")
	for _, line := range lines {
		fmt.Fprintln(out, indent+"  "+line)
	}
}

func planningContextEntries(context map[string]any, list string) []map[string]any {
	items, _ := context[list].([]any)
	entries := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			entries = append(entries, m)
		}
	}
	return entries
}

func roadmapTarget(release map[string]any) string {
	if deadline := str(release, "deadline"); deadline != "" {
		return ", target " + deadline
	}
	if period := str(release, "target_period"); period != "" {
		return ", " + period
	}
	return ""
}

// roadmapInitiative is one Initiative on one line: its text, then its horizon,
// context, kind of work and Epics, then its id.
func roadmapInitiative(m map[string]any, names map[string]string) string {
	details := []string{str(m, "horizon")}
	if context := str(m, "context"); context != "" {
		label := "context " + context
		if name := names[context]; name != "" && name != context {
			label = "context " + name + " (" + context + ")"
		}
		if isNew, _ := m["new_context"].(bool); isNew {
			label += " (new context)"
		}
		details = append(details, label)
	} else {
		details = append(details, "no context")
	}
	kind := str(m, "work_kind")
	if words, ok := roadmapWorkKinds[kind]; ok {
		kind = words
	}
	details = append(details, orDash(kind))
	if epics, ok := m["epic_ids"].([]any); ok && len(epics) > 0 {
		ids := make([]string, 0, len(epics))
		for _, e := range epics {
			ids = append(ids, fmt.Sprint(e))
		}
		sort.Strings(ids)
		details = append(details, "Epics "+strings.Join(ids, ", "))
	}
	return fmt.Sprintf("- %s  [%s]  %s", str(m, "text"), strings.Join(details, ", "), str(m, "id"))
}

func writeRoadmapDryRun(cmd *cobra.Command, data map[string]any) error {
	out := cmd.OutOrStdout()
	operations, _ := data["operations"].([]any)
	for _, o := range operations {
		if m, ok := o.(map[string]any); ok {
			fmt.Fprintf(out, "%v. %s\n", m["index"], str(m, "summary"))
		}
	}
	errs, _ := data["errors"].([]any)
	for _, e := range errs {
		if m, ok := e.(map[string]any); ok {
			fmt.Fprintln(out, "error: "+roadmapPlanError(m))
			if rows, _ := m["rows"].([]any); len(rows) > 0 {
				writeRoadmapRefusedRows(out, rows, "  ")
			}
		}
	}
	fmt.Fprintln(out, "Dry run: nothing was written.")
	if len(errs) > 0 {
		return fmt.Errorf("the plan has %d error(s); nothing was written", len(errs))
	}
	return nil
}

func writeRoadmapReceipt(cmd *cobra.Command, data map[string]any, requestID string) error {
	out := cmd.OutOrStdout()
	results, _ := data["results"].([]any)
	replayed, _ := data["replayed"].(bool)
	if replayed {
		fmt.Fprintf(out, "Request id %s was already applied: nothing was written. The saved result follows; a new request id applies the file again.\n", requestID)
	} else {
		fmt.Fprintf(out, "Applied %d operation(s) under request id %s.\n", len(results), requestID)
	}
	for _, r := range results {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		line := fmt.Sprintf("%v. %s", m["index"], str(m, "op"))
		for _, key := range []string{"line_id", "release_id", "epic_id", "text", "name", "context", "written", "unchanged", "cleared", "moved"} {
			if value, present := m[key]; present && value != nil {
				line += fmt.Sprintf(" %s=%v", key, value)
			}
		}
		fmt.Fprintln(out, line)
	}
	if contexts, ok := data["new_contexts"].([]any); ok && len(contexts) > 0 {
		for _, c := range contexts {
			fmt.Fprintf(out, "%v (new context: no requirement of the System has this code yet)\n", c)
		}
	}
	applied := "Applied by"
	if replayed {
		applied = "Originally applied by"
	}
	if actor, ok := data["actor"].(map[string]any); ok {
		person := str(actor, "name")
		if person == "" {
			person = "user " + fmt.Sprint(actor["id"])
		} else {
			person += fmt.Sprintf(" (user %v)", actor["id"])
		}
		if agent := str(data, "agent"); agent != "" {
			fmt.Fprintf(out, "%s %s, agent %s.\n", applied, person, agent)
		} else {
			fmt.Fprintf(out, "%s %s.\n", applied, person)
		}
	}
	return nil
}
