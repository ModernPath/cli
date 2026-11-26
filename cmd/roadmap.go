package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

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
plan card runs; 'roadmap recording' links each replay test to its capture, and
its break test to the replay, from a file; 'roadmap proof' prints the Proof tab
of an Initiative: the agreed level of proof of each kept or changed legacy
requirement and the level it reached; 'roadmap accept-lower-level' records, on
a person's explicit word, that a requirement stays proved below its agreed
level.`}
	parent.AddCommand(newRoadmapShowCommand(load), newRoadmapSelectionCommand(load), newRoadmapApplyCommand(load), newRoadmapStatusDiagnosticsCommand(load), newRoadmapRecordingCommand(load), newRoadmapProofCommand(load), newRoadmapAcceptLowerLevelCommand(load), newRoadmapVerifyCommand(load))
	return parent
}

// SR-4950-0: read diagnostics without requesting or changing a draft.
func newRoadmapStatusDiagnosticsCommand(load func() (*cliEnv, error)) *cobra.Command {
	command := &cobra.Command{
		Use:   "status-diagnostics [draft-id]",
		Short: "Read safe diagnostics for a Roadmap status draft",
		Long:  "Read a status draft in the bound System, or its latest draft when no UUID is given. Shows state, timestamps, a safe failure summary and job attempts when available.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && !requirementUUIDPattern.MatchString(args[0]) {
				return fmt.Errorf("draft ID must be a UUID")
			}

			env, err := roadmapEnv(load)
			if err != nil {
				return err
			}
			path := "/api/v1/systems/" + strconv.Itoa(env.SystemID) + "/roadmap/status/diagnostics"
			if len(args) == 1 {
				path += "?update_id=" + args[0]
			}
			status, body, err := env.get(path)
			data, err := roadmapData(status, body, err)
			if err != nil {
				return err
			}
			if machine, _ := cmd.Flags().GetBool("json"); machine {
				return writeCommandJSON(cmd, data)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Roadmap status draft %s\nStatus: %s (recorded: %s)\nCreated: %s\nUpdated: %s\n",
				str(data, "id"), str(data, "status"), str(data, "recorded_status"),
				str(data, "inserted_at"), str(data, "updated_at"))
			if failure, ok := data["failure"].(map[string]any); ok {
				fmt.Fprintf(out, "Failure: %s (%s)\n", str(failure, "summary"), str(failure, "category"))
			}
			if job, ok := data["job"].(map[string]any); ok {
				fmt.Fprintf(out, "Job: %s, attempt %v/%v\n", str(job, "state"), job["attempt"], job["max_attempts"])
			} else {
				fmt.Fprintln(out, "Job details unavailable.")
			}
			return nil
		},
	}
	command.Flags().Bool("json", false, "Emit JSON")
	return command
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

Before those lines, an Initiative that replaces part of the legacy system and
is not done has a line for the agreement of Now: with a proposed Epic, under
any Release, while the approval gate would refuse that Epic for the agreement;
with no Epic yet, in Now under a planned or active Modernization Release,
while it is not in the latest agreement or, since it, its context, kind of
work or decisions changed or its own or its Release's planning context changed
or was opened. It also has a line for its undecided legacy requirements, with
a context under a planned or active Modernization Release, while it has no
Epic yet or the agreement line shows; that line comes first. They print as
"Waits for 6 legacy requirements undecided: Decide them on the Selection tab,
The team" and "Waits for the agreement of Now: Not agreed yet, <name>
(business owner)", with "Changed since the agreement" once the plan changed
after the agreement, "The business owner" when the owner has no name and "A
workspace admin" while none is named; --json carries them as planning_waits.

An Initiative that replaces part of the legacy system is done only when its
work is complete, its acceptance is recorded (the domain expert signed off a
report for the Release it is shown under that covers it) and no difference on
its kept or changed legacy requirements is open. Under one that is not done
the lines also give its acceptance, as "Acceptance for <Release>", and each
open difference, by its reference and title, each with its step and whose
turn it is in the Initiative page's words; its acceptance line appears only
once a linked item's own status is in progress, in review or done. The
acceptance's step is
Acceptance test not started, Acceptance test in progress or New acceptance
test needed: a spec changed after the sign-off, the turn of A person on the
team, or Waits for the requirements that modernize its kept and changed
requirements, the turn of The team; an open difference's step is Open
difference: fix it or accept it by name, the turn of The team. One shown
under a Release whose last close came before the platform recorded the
acceptance at a close is read as before: done when its work is complete.

Under an open Release or under none, such an Initiative is also done only when
its proof is as agreed: every kept or changed requirement on its Proof tab is
proved as agreed, or proved below the agreed level with the lower level
accepted by name. An Initiative that is built, accepted or done prints its
stage in the web app's words instead of the done line: Built · waits for
acceptance; Accepted, then what remains (n proved below the agreed level, n
not proved yet, proof waits for the reference revision or proof cannot be
read, n open differences), such as "Accepted · 2 proved below the agreed level
· 1 open difference"; Done; or Done · accepted for new work whose Release has
a signed-off acceptance report. Under one that is not done the lines also give
its proof: "<n> proved below the agreed level: Prove it at the agreed level or
accept the lower level by name", the turn of A person on the team; "<n> not
proved yet: Waits for a run that proves it", the turn of The team;
"Reference revision: Set the Release's reference revision", the turn of A
person on the team; or "Proof cannot be read" with the server's sentence, the
turn of A person on the team.

Under a Modernization Release come the legacy system it replaces (or "in
place"), its reference revision, one line per context of its scope with how
many of its legacy requirements are undecided and how many decided
("Billing (BILL): 1 of 4 undecided, 3 decided"), and, when the Release has
legacy requirements that the business owner's latest agreement did not
record and no Initiative has decided yet, "New since the agreement: <n>
legacy requirements". Under an Initiative that replaces part of the legacy
system a line gives its keep, change and drop decisions
("Decisions: 2 keep, 1 change, 1 drop").

When a Release's close continued an Initiative in a later Release, the part it
continues prints "continues: <text> (INIT-<n>)" and the continued one
"continued in <text> (INIT-<n>)".

The context counts and the decisions both end with how many kept or changed
legacy requirements are drafted and built (", 2 drafted, 1 built"). Onto a new system, a legacy requirement is
drafted while an Epic's saved plan or a written trace links a new requirement
to it with a modernizes trace, and built once a written one has a DONE source.
In place, it is drafted while an Epic's saved plan takes it or links a test to
it, and built once that Epic is DONE. Drafted includes built.

--readiness asks the server for each Modernization Release's readiness
(readiness=true) and prints it after the Release's context lines, counted
the way the Heartbeat counts them:
- Proved as agreed: how many of the kept or changed requirements runs
  captured after the change prove at least at their agreed level, with the
  percent. Before the reference revision is set the line says "Proof is read
  once the reference revision is set.", and while nothing is kept or changed a
  sentence stands in place of the percent.
- Proved below the agreed level: how many are proved, but below their agreed
  level, with how many of them a person accepted by name.
- Drafted, not proved yet (drafted with no proof at all, from
  drafted_not_proved_yet); Not drafted yet; and Undecided, with the dropped
  count.
- One line with how many are built, their Epic complete; how many kept or
  changed user requirements a domain expert accepted; and what the ledger
  misses: how many legacy files with behaviour (a screen, an interface or
  stored data) no requirement traces, and in which contexts, or, while the
  files cannot be read, the read's message, which says why. Every such file
  follows by path and role ("no role" when its analysis gave none) under its
  context, then those not placed in a context.
- The latest replays: how many of the replays with a captured result match
  in their newest capture and how many changed since the capture before,
  then each changed replay with both outcomes, commits and dates, each replay
  captured once and each not captured yet; or "No replay has been captured
  in this Release yet."
For a Release whose legacy system was deleted the block is one sentence:
"The legacy system was deleted, so its readiness can no longer be read."
The block's Built counts the kept or changed requirements of the Initiatives
shown under the Release, as Proved does, while the context lines' built
counts over every decided row of the context. So the context lines can add
up to more than the block's Built.

--json prints the served read: releases, no_release, contexts and plan_state,
with planning_context on each Release and Initiative; progress, accepted,
done, done_when (its work_complete item and, for an Initiative that replaces
part of the legacy system, its acceptance and differences_resolved items, and
under an open Release or none its proof_as_agreed item), waiting_for,
planning_waits (each item's kind, id, step, turn, and count, or person and
all_decided) and selection (keep, change, drop, agreed, drafted and built,
or null for new work), continues and continued_by (the other part's id,
display_number, text, archived_at, release_id and release_name, or null)
on each Initiative; and modernization (each context's
in_scope, undecided, decided, drafted and built) and new_since_agreement_count
on each Release. With --readiness, each Modernization Release's modernization
also carries proof_waits_for_reference_revision, standing, readiness, built,
accepted, ledger_misses and replays.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := roadmapEnv(load)
			if err != nil {
				return err
			}
			path := "/api/v1/systems/" + strconv.Itoa(env.SystemID) + "/roadmap"
			readiness, _ := cmd.Flags().GetBool("readiness")
			if readiness {
				path += "?readiness=true"
			}
			status, body, err := env.get(path)
			data, err := roadmapData(status, body, err)
			if err != nil {
				return err
			}
			if machine, _ := cmd.Flags().GetBool("json"); machine {
				return writeCommandJSON(cmd, data)
			}
			return writeRoadmap(cmd, data, readiness)
		}}
	show.Flags().Bool("json", false, "Emit JSON")
	show.Flags().Bool("readiness", false, "Print each Modernization Release's readiness, what the ledger misses and the latest replays (sends readiness=true)")
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
reason; an expected level, with its reason, goes only on a keep row, and on a
row already decided change only as "compared", a raise the server refuses with
raise_needs_accepted_differences and a sentence while no accepted difference
names the requirement or one naming it is open; a row that names "change" with
"compared" keeps a stored raise, except on a row marked revised since selected,
where it is refused the same way; a row without outcome changes only the
expected level of the Initiative's keep row, or raises or clears the raise of
its change row;
"move": true takes a legacy requirement another Initiative holds. Each "clear"
entry {"requirement_kind", "requirement_id"} deletes the Initiative's row, so
the legacy requirement is undecided again. One set_selection takes at most
1,000 rows, and a plan at most 5,000 rows in all. A refused set_selection
lists each refused row by its position (rows first, then clear), its title, or
its requirement id when it names no legacy requirement of the Release, and the
reason; nothing is written. A held row cannot change, be cleared or move to
another Initiative while an approved Epic relies on its decision or a written
modernizes trace's new requirement modernizes it; the command prints a
sentence that says what holds it, who approved that and who can act. A row
with move: true cannot take a decision of an Initiative under a closed
Modernization Release, also when the row is held; that decision stays with
the closed Release.
'roadmap selection' writes the legacy requirements to decide.

An archive_line of an Initiative that a later part continues is refused with
initiative_continued, with and without --dry-run; the command prints the
server's sentence, which names the continuation.

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
					parts = append(parts, fmt.Sprintf("operation %v: %s", index, roadmapReasonWithMessage(m)))
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
// move of an Initiative with decisions, is refused for. The list began as the
// words Chat's plan card shows (Epic 332, B3); the web app now words
// invalid_expected_level with the agreed level (Epic 462, H5a), and these
// words stay as they are.
var roadmapPlanReasons = map[string]string{
	"selection_rows_refused":      "Some rows of this initiative's selection are refused.",
	"initiative_not_replacing":    "Only an initiative that replaces part of the legacy system has a selection.",
	"initiative_context_required": "Give the initiative a context before setting its selection.",
	"release_not_modernization":   "The initiative's release does not replace a legacy system.",
	// Epic 462 (H5a), SR ff34255d criterion a2bacea5: a released or archived Modernization Release.
	"release_closed":           "The initiative's release is closed, so this initiative can no longer change its decisions.",
	"unknown_line_ref":         "No initiative earlier in the plan has this ref.",
	"too_many_rows":            "An initiative's selection takes at most 1,000 rows, and a plan at most 5,000.",
	"requirement_not_found":    "This is not a legacy requirement of the initiative's release.",
	"requirement_not_in_scope": "This legacy requirement is outside the scope of the initiative's release.",
	"reason_required":          "A drop in an initiative's selection needs a reason.",
	"invalid_outcome":          "A row of an initiative's selection is keep, change or drop.",
	"invalid_expected_level":   "An expected level, with its reason, goes only on a keep row of an initiative.",
	// Epic 336 (D2): a raise of a changed row to compared; the server's row carries its sentence in message.
	"raise_needs_accepted_differences": "A changed requirement can expect compared only once each intended change is an accepted difference.",
	"selected_by_another_initiative":   "Another initiative already holds this legacy requirement.",
	"duplicate_requirement":            "The initiative's selection names this legacy requirement twice.",
	"selection_release_mismatch":       "The initiative has keep, change and drop decisions, so it stays in a release that replaces its legacy system.",
	"selection_row_held":               "This legacy requirement's decision is held by an approved Epic or a written modernizes trace.",
}

// roadmapPlanError is one error of a plan: the operation, or the whole plan
// when the error names none, and its reason code, with the reason's sentence
// when it has one.
func roadmapPlanError(m map[string]any) string {
	where := "the plan"
	if index, ok := m["index"]; ok && index != nil {
		where = fmt.Sprintf("operation %v (%s)", index, orDash(str(m, "op")))
	}
	reason := roadmapReasonWithMessage(m)
	if sentence := roadmapPlanReasons[str(m, "reason")]; sentence != "" {
		reason += " — " + sentence
	}
	return where + ": " + reason
}

// roadmapMessageReasons are the reasons whose refusal carries the server's
// own sentence in message, which names what the person needs: Epic 458 (H1a)'s
// initiative_continued names the continuation.
var roadmapMessageReasons = map[string]bool{"initiative_continued": true}

// roadmapReasonWithMessage is an error's reason, followed by the server's
// message for a reason that carries one.
func roadmapReasonWithMessage(m map[string]any) string {
	reason := str(m, "reason")
	if message := str(m, "message"); message != "" && roadmapMessageReasons[reason] {
		return reason + " — " + message
	}
	return reason
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
		// Epic 336 (D2): a refused raise carries the sentence that says why,
		// naming the requirement, in message.
		if message := str(row, "message"); message != "" {
			fmt.Fprintf(out, "%srow %v: %s\n", indent, row["position"], message)
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

func writeRoadmap(cmd *cobra.Command, data map[string]any, readiness bool) error {
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
		if readiness {
			writeRoadmapReadiness(cmd, release, "  ")
		}
		writePlanningContext(cmd, release["planning_context"], "  ")
		initiatives, _ := release["initiatives"].([]any)
		if len(initiatives) == 0 {
			fmt.Fprintln(out, "  (no initiatives)")
		}
		for _, i := range initiatives {
			if m, ok := i.(map[string]any); ok {
				fmt.Fprintln(out, "  "+roadmapInitiative(m, names))
				writeRoadmapContinuation(cmd, m, "    ")
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
				writeRoadmapContinuation(cmd, m, "      ")
				writeRoadmapDecisions(cmd, m, "      ")
				writeRoadmapDone(cmd, m, "      ")
				writePlanningContext(cmd, m["planning_context"], "      ")
			}
		}
	}
	return nil
}

// writeRoadmapContinuation prints under an Initiative the continuation link
// (Epic 458, H1a): "continues: <text> (INIT-<n>)" for the Initiative it
// continues and "continued in <text> (INIT-<n>)" for the one that continues
// it. A read without the link prints nothing.
func writeRoadmapContinuation(cmd *cobra.Command, m map[string]any, indent string) {
	out := cmd.OutOrStdout()
	if other, ok := m["continues"].(map[string]any); ok {
		fmt.Fprintf(out, "%scontinues: %s\n", indent, roadmapInitiativeRef(other))
	}
	if other, ok := m["continued_by"].(map[string]any); ok {
		fmt.Fprintf(out, "%scontinued in %s\n", indent, roadmapInitiativeRef(other))
	}
}

// roadmapInitiativeRef is "<text> (INIT-<n>)", or the text alone with no number.
func roadmapInitiativeRef(m map[string]any) string {
	if number, ok := m["display_number"]; ok && number != nil {
		return fmt.Sprintf("%s (INIT-%v)", str(m, "text"), number)
	}
	return str(m, "text")
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

// writeRoadmapReadiness prints, with --readiness, a Modernization Release's
// readiness (Epic 338, D4) after the lines writeRoadmapModernization prints,
// counted the way the Heartbeat counts them: Proved as agreed with its
// percent, or the sentence while proof waits for the reference revision;
// Proved below the agreed level (Epic 459); Drafted, not proved yet, Not
// drafted yet and Undecided with the dropped count; one line with built,
// accepted and what the ledger misses, or the read's message while it cannot
// list the files; every file the ledger misses under its context, then those
// not placed in a context; the latest replays; and the Next step while the
// reference revision is not chosen (Epic 460). A Release whose legacy system
// was deleted, which the server serves with a null legacy_system_id and none
// of the readiness keys, prints one sentence. A Delivery Release, or a read
// without the readiness, prints nothing.
func writeRoadmapReadiness(cmd *cobra.Command, release map[string]any, indent string) {
	if str(release, "release_type") != "modernization" {
		return
	}
	out := cmd.OutOrStdout()
	modernization, _ := release["modernization"].(map[string]any)
	if id, present := release["legacy_system_id"]; present && id == nil {
		fmt.Fprintln(out, indent+"The legacy system was deleted, so its readiness can no longer be read.")
		return
	}
	standing, ok := modernization["standing"].(map[string]any)
	if !ok {
		return
	}
	fmt.Fprintln(out, indent+"Readiness:")
	inner := indent + "  "
	if line := roadmapProvedLine(modernization, standing); line != "" {
		fmt.Fprintln(out, inner+line)
	}
	// Epic 459 (H2): the rows proved below the agreed level apart from Proved as
	// agreed, with how many were accepted by name; a read with no reference
	// revision gives no proved_below and prints no such line.
	if below, present := standing["proved_below"]; present && below != nil {
		fmt.Fprintf(out, "%sProved below the agreed level: %d (%d accepted by name)\n", inner,
			roadmapCount(below), roadmapCount(standing["proved_below_accepted"]))
	}
	// Drafted, not proved yet counts the drafted rows with no proof at all; a read
	// from before Epic 459 gives drafted_not_proved alone.
	draftedNotProved, present := standing["drafted_not_proved_yet"]
	if !present {
		draftedNotProved = standing["drafted_not_proved"]
	}
	fmt.Fprintf(out, "%sDrafted, not proved yet: %d in a saved Epic plan\n", inner, roadmapCount(draftedNotProved))
	fmt.Fprintf(out, "%sNot drafted yet: %d kept or changed, in no Epic yet\n", inner, roadmapCount(standing["not_drafted"]))
	fmt.Fprintf(out, "%sUndecided: %d in no Initiative yet, with %d dropped\n", inner,
		roadmapCount(standing["undecided"]), roadmapCount(standing["dropped"]))

	accepted, _ := modernization["accepted"].(map[string]any)
	ledger, _ := modernization["ledger_misses"].(map[string]any)
	parts := []string{
		fmt.Sprintf("Built, their Epic complete: %d", roadmapCount(modernization["built"])),
		fmt.Sprintf("accepted by a domain expert: %d of %s", roadmapCount(accepted["count"]),
			roadmapPlural(roadmapCount(accepted["of"]), "kept or changed user requirement", "kept or changed user requirements")),
	}
	if words := roadmapLedgerWords(ledger); words != "" {
		parts = append(parts, words)
	}
	fmt.Fprintln(out, inner+strings.Join(parts, " · "))
	writeRoadmapLedgerFiles(cmd, ledger, inner)
	writeRoadmapReplays(cmd, modernization["replays"], inner)
	if next := roadmapReferenceRevisionNext(modernization); next != "" {
		fmt.Fprintln(out, inner+next)
	}
}

// roadmapReferenceRevisionNext is the step a Release with no reference
// revision waits for (Epic 460, H3), from the read's
// progress.reference_revision.state: record the legacy System's test results,
// or have a person choose the revision. A chosen revision, a read with no
// progress (an older server) and a Release whose legacy System was deleted
// give none.
func roadmapReferenceRevisionNext(modernization map[string]any) string {
	progress, _ := modernization["progress"].(map[string]any)
	revision, _ := progress["reference_revision"].(map[string]any)
	switch str(revision, "state") {
	case "results_not_recorded":
		return "Next: record the legacy System's test results under this Release with modernpath modernization record (onto a new system with --legacy-checkout), then a person chooses the reference revision on the Release page's Modernization tab."
	case "waits_for_a_person":
		return "Next: a person chooses the reference revision on the Release page's Modernization tab."
	}
	return ""
}

// roadmapProvedLine is the Proved as agreed figure with its percent, or the
// Heartbeat's sentence while proof waits for the reference revision; a
// sentence stands in place of the percent while nothing is kept or changed.
func roadmapProvedLine(modernization, standing map[string]any) string {
	if waits, _ := modernization["proof_waits_for_reference_revision"].(bool); waits {
		return "Proof is read once the reference revision is set."
	}
	proved, present := standing["proved"]
	if !present || proved == nil {
		return ""
	}
	kept := roadmapCount(standing["kept_or_changed"])
	line := fmt.Sprintf("Proved as agreed: %d of %d kept or changed, proved by runs captured after the change",
		roadmapCount(proved), kept)
	readiness, _ := modernization["readiness"].(map[string]any)
	if percent, ok := roadmapNumber(readiness["percent"]); ok {
		return line + fmt.Sprintf(" · %d%%", int(math.Round(percent)))
	}
	if kept == 0 {
		return line + ". No legacy requirement is kept or changed yet, so there is no percent."
	}
	return line
}

// roadmapLedgerWords is what the ledger misses on the line with built and
// accepted: how many legacy files with behaviour no requirement traces, in
// which contexts, or, while it cannot be read, the read's message, which says
// why (Epic 460, H3: no repository, an analysis running or failed, no file).
func roadmapLedgerWords(ledger map[string]any) string {
	if ledger == nil {
		return ""
	}
	if analysed, _ := ledger["analysed"].(bool); !analysed {
		if message := str(ledger, "message"); message != "" {
			return message
		}
		return "what the ledger misses is read from the legacy system's analysis, which has not run yet"
	}
	words := "what the ledger misses: " + roadmapLegacyFiles(roadmapCount(ledger["total"]))
	var names []string
	for _, context := range roadmapLedgerContexts(ledger) {
		name := str(context, "name")
		if name == "" {
			name = str(context, "code")
		}
		names = append(names, name)
	}
	if len(names) > 0 {
		words += ", in " + roadmapJoinWords(names)
	}
	notPlaced, _ := ledger["not_placed"].(map[string]any)
	if count := roadmapCount(notPlaced["count"]); count > 0 {
		words += fmt.Sprintf("; %d of them not placed in a context", count)
	}
	return words
}

// roadmapLedgerContexts are the ledger's contexts that list a file.
func roadmapLedgerContexts(ledger map[string]any) []map[string]any {
	contexts, _ := ledger["contexts"].([]any)
	var listed []map[string]any
	for _, c := range contexts {
		if context, ok := c.(map[string]any); ok && roadmapCount(context["count"]) > 0 {
			listed = append(listed, context)
		}
	}
	return listed
}

// writeRoadmapLedgerFiles prints every file the ledger misses by path and
// role, under its context with its count of files, then those not placed in a
// context, as the Release page's Show lists them.
func writeRoadmapLedgerFiles(cmd *cobra.Command, ledger map[string]any, indent string) {
	if analysed, _ := ledger["analysed"].(bool); !analysed {
		return
	}
	out := cmd.OutOrStdout()
	files := func(entry map[string]any) {
		list, _ := entry["files"].([]any)
		for _, f := range list {
			file, ok := f.(map[string]any)
			if !ok {
				continue
			}
			fmt.Fprintf(out, "%s  %s (%s)\n", indent, str(file, "path"), roadmapFileRole(file))
		}
	}
	for _, context := range roadmapLedgerContexts(ledger) {
		label := str(context, "code")
		if name := str(context, "name"); name != "" {
			label = name + " (" + label + ")"
		}
		fmt.Fprintf(out, "%s%s: %s\n", indent, label, roadmapPlural(roadmapCount(context["count"]), "file", "files"))
		files(context)
	}
	if notPlaced, ok := ledger["not_placed"].(map[string]any); ok && roadmapCount(notPlaced["count"]) > 0 {
		fmt.Fprintf(out, "%sNot placed in a context: %s\n", indent, roadmapPlural(roadmapCount(notPlaced["count"]), "file", "files"))
		files(notPlaced)
	}
}

// roadmapFileRole is a missed file's role, or "no role" when its analysis gave
// none, as the Release page and the close page show it.
func roadmapFileRole(file map[string]any) string {
	if role := str(file, "role"); role != "" {
		return role
	}
	return "no role"
}

// roadmapLegacyFiles is a count of legacy files the ledger misses in words.
func roadmapLegacyFiles(count int) string {
	return roadmapPlural(count, "legacy file", "legacy files") + " with behaviour and no requirement"
}

// roadmapNumber is a served number, decoded as json.Number or float64; false
// for null or a missing key.
func roadmapNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case float64:
		return v, true
	default:
		return 0, false
	}
}

// writeRoadmapReplays prints the latest replays: how many of the replays
// with a captured result match in their newest capture and how many changed
// since the capture before, then each changed replay, each replay captured
// once and each not captured yet; or the read's sentence when no replay of
// the Release has a captured result.
func writeRoadmapReplays(cmd *cobra.Command, value any, indent string) {
	replays, ok := value.(map[string]any)
	if !ok {
		return
	}
	out := cmd.OutOrStdout()
	changed, _ := replays["changed"].([]any)
	newest := str(replays, "newest_capture_at")
	if captured, known := replays["captured"].(bool); (known && !captured) || newest == "" {
		message := str(replays, "message")
		if message == "" {
			message = "No replay has been captured in this Release yet."
		}
		fmt.Fprintln(out, indent+message)
	} else {
		fmt.Fprintf(out, "%sLatest replays, newest capture %s: %d of %d match · %d changed since the capture before\n", indent,
			roadmapDay(newest), roadmapCount(replays["match"]), roadmapCount(replays["with_result"]), len(changed))
	}
	for _, r := range changed {
		if replay, ok := r.(map[string]any); ok {
			previous, _ := replay["previous"].(map[string]any)
			newest, _ := replay["newest"].(map[string]any)
			fmt.Fprintf(out, "%s  %s: %s, then %s\n", indent, str(replay, "title"), roadmapCaptured(previous), roadmapCaptured(newest))
		}
	}
	once, _ := replays["captured_once"].([]any)
	for _, r := range once {
		if replay, ok := r.(map[string]any); ok {
			newest, _ := replay["newest"].(map[string]any)
			fmt.Fprintf(out, "%s  %s: captured once, %s\n", indent, str(replay, "title"), roadmapCaptured(newest))
		}
	}
	notCaptured, _ := replays["not_captured"].([]any)
	for _, r := range notCaptured {
		if replay, ok := r.(map[string]any); ok {
			fmt.Fprintf(out, "%s  %s: not captured yet\n", indent, str(replay, "title"))
		}
	}
}

// roadmapResultWords are the outcomes of a captured result in words.
var roadmapResultWords = map[string]string{
	"pass": "passed", "fail": "failed", "skip": "skipped", "error": "errored", "inconclusive": "inconclusive",
}

// roadmapCaptured is one captured result: its outcome, its commit and the day
// its run was captured.
func roadmapCaptured(result map[string]any) string {
	outcome := str(result, "outcome")
	if words, ok := roadmapResultWords[outcome]; ok {
		outcome = words
	}
	return fmt.Sprintf("%s at %s on %s", orDash(outcome), orDash(str(result, "commit")), orDash(roadmapDay(str(result, "captured_at"))))
}

// roadmapDay is the day of a served timestamp, or the value as served.
func roadmapDay(value string) string {
	if at, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return at.UTC().Format("2006-01-02")
	}
	return value
}

// roadmapPlural picks the singular words for a count of one.
func roadmapPlural(count int, one, many string) string {
	if count == 1 {
		return "1 " + one
	}
	return strconv.Itoa(count) + " " + many
}

// roadmapJoinWords joins names as "A", "A and B" or "A, B and C".
func roadmapJoinWords(names []string) string {
	if len(names) <= 1 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
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
	// Epic 337 (D3): the acceptance and the open differences of an Initiative
	// that replaces part of the legacy system.
	"acceptance_not_started":            "Acceptance test not started",
	"acceptance_in_progress":            "Acceptance test in progress",
	"acceptance_retest":                 "New acceptance test needed: a spec changed after the sign-off",
	"acceptance_waits_for_requirements": "Waits for the requirements that modernize its kept and changed requirements",
	"difference_open":                   "Open difference: fix it or accept it by name",
	// Epic 459 (H2): the proof items of an Initiative that replaces part of the
	// legacy system, under an open Release or none.
	"proof_below_agreed_level":           "Prove it at the agreed level or accept the lower level by name",
	"proof_not_yet":                      "Waits for a run that proves it",
	"proof_waits_for_reference_revision": "Set the Release's reference revision",
}

var roadmapTurns = map[string]string{"team": "The team", "person": "A person on the team"}

// roadmapCountKinds names the kinds of linked work in done_when's counts.
var roadmapCountKinds = []struct{ key, words string }{
	{"epic", "Epics"},
	{"user_requirement", "user requirements"},
	{"system_requirement", "system requirements"},
}

// roadmapAcceptanceTitle names the acceptance an Initiative waits for: for the
// Release it is shown under, or, shown under none, the acceptance alone.
func roadmapAcceptanceTitle(release string) string {
	if release == "" {
		return "Acceptance"
	}
	return "Acceptance for " + release
}

// roadmapWorkParts is "n of m <kind>" for each kind of linked work with an
// item that counts, from a work_complete item's counts.
func roadmapWorkParts(entry map[string]any) []string {
	var parts []string
	counts, _ := entry["counts"].(map[string]any)
	for _, kind := range roadmapCountKinds {
		count, _ := counts[kind.key].(map[string]any)
		counting := roadmapCount(count["counting"])
		if counting == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d of %d %s", roadmapCount(count["done"]), counting, kind.words))
	}
	return parts
}

// roadmapWaitingWords is an item of waiting_for in words: its title, its
// step and whose turn it is.
func roadmapWaitingWords(item map[string]any) string {
	title := str(item, "title")
	if ref := str(item, "display_ref"); ref != "" {
		title = ref + " " + title
	}
	if str(item, "kind") == "acceptance" {
		title = roadmapAcceptanceTitle(str(item, "title"))
	}
	step := roadmapSteps[str(item, "step")]
	if str(item, "kind") == "proof" {
		title, step = roadmapProofWait(item, step)
	}
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
	return fmt.Sprintf("%s: %s, %s", title, step, turn)
}

// roadmapPlanningWords is an item of planning_waits in words (Epic 471, H5b,
// SR-3 criterion 0): the undecided legacy requirements of the Initiative's
// context, for the team, or the agreement of Now with who agrees: the business
// owner by name, "The business owner" when the person has no name, or "A
// workspace admin" while none is named.
func roadmapPlanningWords(item map[string]any) string {
	switch str(item, "kind") {
	case "selection":
		count := roadmapCount(item["count"])
		if count == 1 {
			return "1 legacy requirement undecided: Decide it on the Selection tab, The team"
		}
		return fmt.Sprintf("%d legacy requirements undecided: Decide them on the Selection tab, The team", count)
	case "agreement":
		state := "Not agreed yet"
		if str(item, "step") == "agreement_changed" {
			state = "Changed since the agreement"
		}
		who := "A workspace admin"
		if person, ok := item["person"].(map[string]any); ok {
			who = "The business owner"
			if name := str(person, "name"); name != "" {
				who = name + " (business owner)"
			}
		}
		return "the agreement of Now: " + state + ", " + who
	}
	return orDash(str(item, "kind")) + ": " + orDash(str(item, "step"))
}

// roadmapProofWait is a proof item's title and step in the Overview's words
// (Epic 459, H2): the count of rows proved below the agreed level or not
// proved yet, the reference revision, or that the proof cannot be read with
// the refusal's sentence as its step.
func roadmapProofWait(item map[string]any, step string) (string, string) {
	switch str(item, "step") {
	case "proof_below_agreed_level":
		return fmt.Sprintf("%d proved below the agreed level", roadmapCount(item["count"])), step
	case "proof_not_yet":
		return fmt.Sprintf("%d not proved yet", roadmapCount(item["count"])), step
	case "proof_waits_for_reference_revision":
		return "Reference revision", step
	case "proof_unreadable":
		reason, _ := item["reason"].(map[string]any)
		return "Proof cannot be read", strings.TrimSuffix(strings.TrimSpace(str(reason, "message")), ".")
	}
	return "Proof", step
}

// roadmapDoneWhenItem is the item of an Initiative's done_when with key, or nil.
func roadmapDoneWhenItem(m map[string]any, key string) map[string]any {
	items, _ := m["done_when"].([]any)
	for _, i := range items {
		if item, ok := i.(map[string]any); ok && str(item, "key") == key {
			return item
		}
	}
	return nil
}

// roadmapStagePhrase is the web app's stage phrase of an Initiative that is
// built, accepted or done (Epic 459, H2): Built · waits for acceptance;
// Accepted, then what remains; Done, or Done · accepted when done_when holds
// no acceptance item and the line is accepted. Any other progress gives "".
func roadmapStagePhrase(m map[string]any) string {
	switch str(m, "progress") {
	case "built":
		return "Built · waits for acceptance"
	case "accepted":
		return strings.Join(append([]string{"Accepted"}, roadmapAcceptedRemains(m)...), " · ")
	case "done":
		if accepted, _ := m["accepted"].(bool); accepted && roadmapDoneWhenItem(m, "acceptance") == nil {
			return "Done · accepted"
		}
		return "Done"
	}
	return ""
}

// roadmapAcceptedRemains is what an accepted Initiative still waits for, in
// this order, each only when present: the rows proved below the agreed level
// and those not proved yet, or instead of both that its proof waits for the
// reference revision or cannot be read; then its open differences while No
// open difference is not met.
func roadmapAcceptedRemains(m map[string]any) []string {
	var parts []string
	if proof := roadmapDoneWhenItem(m, "proof_as_agreed"); proof != nil {
		_, unreadable := proof["reason"].(map[string]any)
		waits, _ := proof["waits_for_reference_revision"].(bool)
		counts, counted := proof["counts"].(map[string]any)
		switch {
		case unreadable:
			parts = append(parts, "proof cannot be read")
		case waits:
			parts = append(parts, "proof waits for the reference revision")
		case counted:
			if below := roadmapCount(counts["below"]); below > 0 {
				parts = append(parts, fmt.Sprintf("%d proved below the agreed level", below))
			}
			if notYet := roadmapCount(counts["not_yet"]); notYet > 0 {
				parts = append(parts, fmt.Sprintf("%d not proved yet", notYet))
			}
		}
	}
	if differences := roadmapDoneWhenItem(m, "differences_resolved"); differences != nil {
		if met, _ := differences["met"].(bool); !met {
			if open := roadmapCount(differences["open"]); open > 0 {
				parts = append(parts, roadmapPlural(open, "open difference", "open differences"))
			}
		}
	}
	return parts
}

// writeRoadmapDone prints under an Initiative whether it is done (Epic 326,
// A3): Done; No work yet when no linked work counts; or Not done with n of m
// for each kind that has an item that counts. Under an Initiative that is not
// done, one line per item of waiting_for with its title, its step and whose
// turn it is: from Epic 337 (D3) also the acceptance, as Acceptance for
// <Release>, and each open difference by its reference and title. From Epic
// 459 (H2) an Initiative that is built, accepted or done prints its stage
// phrase instead, and the proof items print in the Overview's words. A read
// without done prints nothing.
func writeRoadmapDone(cmd *cobra.Command, m map[string]any, indent string) {
	done, ok := m["done"].(bool)
	if !ok {
		return
	}
	out := cmd.OutOrStdout()
	if phrase := roadmapStagePhrase(m); phrase != "" {
		fmt.Fprintln(out, indent+phrase)
		if done {
			return
		}
		writeRoadmapWaits(out, m, indent)
		return
	}
	if done {
		fmt.Fprintln(out, indent+"Done")
		return
	}
	var parts []string
	if items, _ := m["done_when"].([]any); len(items) > 0 {
		for _, item := range items {
			entry, _ := item.(map[string]any)
			if str(entry, "key") == "work_complete" {
				parts = append(parts, roadmapWorkParts(entry)...)
			}
		}
	}
	if len(parts) == 0 {
		fmt.Fprintln(out, indent+"No work yet")
	} else {
		fmt.Fprintln(out, indent+"Not done: "+strings.Join(parts, ", ")+" complete")
	}
	writeRoadmapWaits(out, m, indent)
}

// writeRoadmapWaits prints one line per item of an Initiative's waiting_for,
// after one line per item of its planning_waits (Epic 471, H5b).
func writeRoadmapWaits(out io.Writer, m map[string]any, indent string) {
	planning, _ := m["planning_waits"].([]any)
	for _, p := range planning {
		if item, ok := p.(map[string]any); ok {
			fmt.Fprintf(out, "%s  Waits for %s\n", indent, roadmapPlanningWords(item))
		}
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
		fmt.Fprintf(out, "%s  Waits for %s\n", indent, roadmapWaitingWords(item))
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
		details = append(details, roadmapEpicList(epics))
	}
	return fmt.Sprintf("- %s  [%s]  %s", str(m, "text"), strings.Join(details, ", "), str(m, "id"))
}

// roadmapEpicList names an Initiative's Epics in number order with their count
// (Epic 460, H3): "Epic 2 (1 Epic)" or "Epics 2, 10 (2 Epics)".
func roadmapEpicList(epics []any) string {
	ids := make([]string, 0, len(epics))
	for _, e := range epics {
		ids = append(ids, fmt.Sprint(e))
	}
	sort.SliceStable(ids, func(i, j int) bool {
		a, errA := strconv.ParseInt(ids[i], 10, 64)
		b, errB := strconv.ParseInt(ids[j], 10, 64)
		switch {
		case errA == nil && errB == nil:
			return a < b
		case errA == nil:
			return true
		case errB == nil:
			return false
		}
		return ids[i] < ids[j]
	})
	if len(ids) == 1 {
		return "Epic " + ids[0] + " (1 Epic)"
	}
	return fmt.Sprintf("Epics %s (%d Epics)", strings.Join(ids, ", "), len(ids))
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
