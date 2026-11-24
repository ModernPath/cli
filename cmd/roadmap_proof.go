package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// Epic 336 (D2): modernpath roadmap proof — print the Proof tab of one
// Initiative that replaces part of the legacy system: for each legacy
// requirement it keeps or changes, its agreed level of proof and the level it
// reached, read from captured runs of the modernized system and never typed,
// so a coding agent and a person see the same facts.

// roadmapProofLevelWords are the Proof tab's words for the five levels, strongest first.
var roadmapProofLevelWords = map[string]string{
	"compared":         "Compared",
	"screens_compared": "Screens compared",
	"tests_only":       "Tests only",
	"reviewed_only":    "Reviewed only",
	"not_yet":          "Not yet",
}

func newRoadmapProofCommand(load func() (*cliEnv, error)) *cobra.Command {
	proof := &cobra.Command{Use: "proof", Short: "Print the Proof tab of an Initiative: the agreed level of proof of each kept or changed legacy requirement and the level it reached", Args: cobra.NoArgs,
		Long: `Print the Proof tab of one Initiative of the bound System that replaces part of
the legacy system. --initiative <id> names it. For each legacy requirement the
Initiative keeps or changes, the tab gives its agreed level of proof, with
where that level came from, and the level it reached, read from captured runs
of the modernized system and from the replays of the legacy recordings, never
typed. --json writes the route's answer.

The five levels, strongest first: Compared (the replay of a current capture of
an interface or stored data and its break test both pass in the run that
decides), Screens compared (the capture holds screens alone), Tests only (every
test of the criterion passes and no such replay), Reviewed only (no run decides,
or the run lacks a passing result, and an implementation review of the current
planning cycle names the criterion) and Not yet, which a row reads as Not
proved yet. A requirement takes the weakest level over its new requirements and
their criteria. It is proved as agreed when it reached at least its agreed
level, and proved below the agreed level when it reached Screens compared,
Tests only or Reviewed only under it.

Only a run of the modernized system at the commit that merged the change, or at
a commit that contains it, counts, and only when it holds a passing result for
every test of the requirement's criteria: once the done Epic's merge commit is
recorded with 'epics record-merge', a run captured with 'modernization record'
from a full-depth checkout counts when its commit is that merge commit or its
chain of commits reaches it. Until then the row says what it waits for. The
proof is read once the Release's reference revision is set; before that the
route refuses with reference_revision_required, and the command prints the
sentence.

The command prints the Initiative and its Release with the legacy system, the
counts in the Proof tab's words (kept or changed, proved as agreed, proved below
the agreed level with how many were accepted by name, not proved yet, revised
since selected, capture not reproducible) and one line per row: its short
reference and title, its outcome, its agreed level with its basis (estimated
from its files, recorded by reverse engineering, set by {person} with the
reason, raised by {person} with the reason, or changed requirement) and the
level reached with its reason, then its marks, and the differences that name
it by reference and state. A row below its agreed level says proved below the
agreed level and, while a person's acceptance of that lower level applies,
accepted by <name> with their words; a row not proved yet reads Not proved
yet. A refusal prints the sentence in the body's error message.

Then come the Initiative's Done when items with where each stands, as its
Overview tab lists them (All linked work is complete, Acceptance recorded, No
open difference and, under an open Release or none, Proved as agreed: n of m
with the rows proved below the agreed level, accepted by name and not proved
yet, nothing kept or changed to prove, read once the Release's reference
revision is set, or proof cannot be read with the server's sentence), and what
it waits for of its acceptance, its open differences and its proof, in the
words of roadmap show; the acceptance for the Release it is
shown under, with its step, the report's state, its counts and each
requirement's state (passed, excluded, failed or not tested yet) with its
revision, the notes of what failed or could not be tried and the legacy
requirements it modernizes; and its differences, each with its reference,
title and state, who accepted it, and the legacy requirement it names. A mask
added to a replay after its first failing run cites an accepted difference by
the reference printed here. The Initiative waits for a difference only while No
open difference is not met; the list of differences shows them as they stand
today, so a difference raised after the Initiative's Release closed is listed
and the Initiative does not wait for it. A read from a server before the
acceptance and the differences prints none of these lines.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			initiative, _ := cmd.Flags().GetString("initiative")
			machine, _ := cmd.Flags().GetBool("json")
			initiative = strings.TrimSpace(initiative)
			if initiative == "" {
				return fmt.Errorf("roadmap proof needs --initiative <id>")
			}
			env, err := roadmapEnv(load)
			if err != nil {
				return err
			}
			query := url.Values{}
			query.Set("line_id", initiative)
			status, body, err := env.get("/api/v1/systems/" + strconv.Itoa(env.SystemID) + "/roadmap/proof?" + query.Encode())
			data, err := roadmapData(status, body, err)
			if err != nil {
				return err
			}
			if machine {
				var buf bytes.Buffer
				encoder := json.NewEncoder(&buf)
				encoder.SetEscapeHTML(false)
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(data); err != nil {
					return err
				}
				_, err := cmd.OutOrStdout().Write(buf.Bytes())
				return err
			}
			writeRoadmapProof(cmd, data)
			return nil
		}}
	proof.Flags().String("initiative", "", "The id of an Initiative that replaces part of the legacy system")
	proof.Flags().Bool("json", false, "Print the route's answer as JSON")
	return proof
}

// writeRoadmapProof prints the Proof tab in its words.
func writeRoadmapProof(cmd *cobra.Command, data map[string]any) {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, roadmapProofHead(data))
	if counts, ok := data["counts"].(map[string]any); ok {
		fmt.Fprintln(out, roadmapProofCounts(counts))
	}
	rows, _ := data["rows"].([]any)
	for _, r := range rows {
		if row, ok := r.(map[string]any); ok {
			fmt.Fprintln(out, roadmapProofRowLine(row))
		}
	}
	writeRoadmapProofDone(out, data)
	writeRoadmapProofAcceptance(out, data)
	writeRoadmapProofDifferences(out, data)
}

// roadmapProofHead is the Initiative, its Release and the legacy system it
// replaces or modernizes in place.
func roadmapProofHead(data map[string]any) string {
	initiative, _ := data["initiative"].(map[string]any)
	release, _ := data["release"].(map[string]any)
	legacy, _ := data["legacy_system"].(map[string]any)
	head := fmt.Sprintf("%s · %s", orDash(str(initiative, "title")), orDash(str(release, "name")))
	if name := str(legacy, "name"); name != "" {
		if inPlace, _ := data["in_place"].(bool); inPlace {
			head += " · modernizes " + name + " in place"
		} else {
			head += " · replaces " + name
		}
	}
	return head
}

// roadmapProofBelow is the reached_reason the proof read gives a row below its
// agreed level with no other reason (Epic 459, H2).
const roadmapProofBelow = "proved below the agreed level"

// roadmapProofCounts is the counts line in the Proof tab's summary words
// (Epic 459, H2); "(k accepted by name)" only when k is above zero.
func roadmapProofCounts(counts map[string]any) string {
	below := count(counts, "proved_below") + " " + roadmapProofBelow
	if accepted := roadmapCount(counts["proved_below_accepted"]); accepted > 0 {
		below += fmt.Sprintf(" (%d accepted by name)", accepted)
	}
	return fmt.Sprintf("%s kept or changed · %s proved as agreed · %s · %s not proved yet · %s revised since selected · %s capture not reproducible",
		count(counts, "kept_or_changed"), count(counts, "proved"), below, count(counts, "not_yet"),
		count(counts, "revised_since_selected"), count(counts, "capture_not_reproducible"))
}

// roadmapProofRowLine is one row of the Proof tab: its short reference and
// title, its outcome, its agreed level with the basis, the level reached in
// the row's proof words, its marks and its differences.
func roadmapProofRowLine(row map[string]any) string {
	name := strings.TrimSpace(str(row, "display_ref") + " " + str(row, "title"))
	agreed := "agreed level " + roadmapProofLevel(str(row, "expected_level")) + ", " + roadmapProofBasis(row)
	if flag, _ := row["capture_not_reproducible"].(bool); flag {
		agreed += " · capture not reproducible"
	}
	var marks []string
	if flag, _ := row["revised_since_selected"].(bool); flag {
		marks = append(marks, "revised since selected")
	}
	if flag, _ := row["legacy_changed_since_recorded"].(bool); flag {
		marks = append(marks, "legacy changed since recorded")
	}
	line := fmt.Sprintf("  %s: %s · %s · %s", orDash(name), orDash(str(row, "outcome")), agreed, roadmapProofReached(row))
	if len(marks) > 0 {
		line += " · " + strings.Join(marks, " · ")
	}
	if differences := roadmapProofRowDifferences(row); differences != "" {
		line += " · differences: " + differences
	}
	return line
}

// roadmapProofReached is the level a row reached in its proof words (Epic
// 459, H2): a row below its agreed level says proved below the agreed level
// and, while an acceptance of that level applies, accepted by the person's
// name with their words; a row not proved yet reads Not proved yet, never Not
// yet alone; each with the read's reason.
func roadmapProofReached(row map[string]any) string {
	level, reason, state := str(row, "reached_level"), str(row, "reached_reason"), str(row, "proof_state")
	withReason := func(words string) string {
		if reason != "" && reason != roadmapProofBelow {
			return words + ", " + reason
		}
		return words
	}
	switch {
	case state == "below" || state == "below_accepted":
		words := withReason("reached "+roadmapProofLevel(level)) + ", " + roadmapProofBelow
		if acceptance, ok := row["lower_level_acceptance"].(map[string]any); ok && state == "below_accepted" {
			// The web app's words for a person no longer in the workspace.
			name := "a former member"
			if by, _ := acceptance["accepted_by"].(map[string]any); str(by, "name") != "" {
				name = str(by, "name")
			}
			words += ", accepted by " + name
			if said := oneLine(str(acceptance, "words")); said != "" {
				words += ": " + said
			}
		}
		return words
	case level == "not_yet" || (state == "not_yet" && level == ""):
		return withReason("Not proved yet")
	case state == "not_yet":
		return withReason("reached " + roadmapProofLevel(level) + ", not proved yet")
	default:
		words := "reached " + roadmapProofLevel(level)
		if reason != "" {
			words += ", " + reason
		}
		return words
	}
}

// roadmapProofRowDifferences names a row's differences by reference and state
// (Epic 337, D3), or "" for a row without one.
func roadmapProofRowDifferences(row map[string]any) string {
	differences, _ := row["differences"].([]any)
	parts := make([]string, 0, len(differences))
	for _, d := range differences {
		if difference, ok := d.(map[string]any); ok {
			parts = append(parts, fmt.Sprintf("%s (%s)", orDash(str(difference, "display_ref")), orDash(str(difference, "state"))))
		}
	}
	return strings.Join(parts, ", ")
}

// writeRoadmapProofDone prints the Initiative's Done when items with where
// each stands, as its Overview tab lists them, then what it waits for of its
// acceptance and its open differences, in roadmap show's words (Epic 337,
// D3). A difference waits only while done_when holds differences_resolved
// unmet: a difference raised after the close of the Initiative's Release, or
// one under a Release closed before D3's deploy, whose done_when holds
// work_complete alone, is listed under Differences and waits for nothing. An
// entry without done_when prints nothing.
func writeRoadmapProofDone(out io.Writer, data map[string]any) {
	items, _ := data["done_when"].([]any)
	if len(items) == 0 {
		return
	}
	state := "Not done yet"
	if done, _ := data["done"].(bool); done {
		state = "Done"
	}
	fmt.Fprintln(out, "Done when: "+state)
	var waits, proofWaits []map[string]any
	differencesWait := false
	for _, i := range items {
		item, ok := i.(map[string]any)
		if !ok {
			continue
		}
		met, _ := item["met"].(bool)
		stands := "not met"
		if met {
			stands = "met"
		}
		switch str(item, "key") {
		case "work_complete":
			work := "No work yet"
			if parts := roadmapWorkParts(item); len(parts) > 0 {
				work = strings.Join(parts, ", ")
			}
			fmt.Fprintf(out, "  All linked work is complete: %s, %s\n", stands, work)
		case "acceptance":
			line := "  Acceptance recorded: " + stands
			if !met {
				step := str(item, "step")
				line += ", " + roadmapStepWords(step)
				// The acceptance waits for a person, or for the team while
				// requirements still have to modernize its legacy ones.
				turn := "person"
				if step == "acceptance_waits_for_requirements" {
					turn = "team"
				}
				release, _ := item["release"].(map[string]any)
				waits = append(waits, map[string]any{"kind": "acceptance", "title": str(release, "name"), "step": step, "turn": turn})
			}
			fmt.Fprintln(out, line)
		case "differences_resolved":
			line := "  No open difference: " + stands
			if !met {
				differencesWait = true
				line += fmt.Sprintf(", %d open", roadmapCount(item["open"]))
			}
			fmt.Fprintln(out, line)
		case "proof_as_agreed":
			fmt.Fprintln(out, "  Proved as agreed: "+stands+", "+roadmapProofStands(item))
			if !met {
				proofWaits = roadmapProofWaits(item)
			}
		}
	}
	if differencesWait {
		differences, _ := data["differences"].([]any)
		for _, d := range differences {
			if difference, ok := d.(map[string]any); ok {
				if open, _ := difference["open"].(bool); open {
					waits = append(waits, map[string]any{"kind": "difference", "display_ref": str(difference, "display_ref"),
						"title": str(difference, "title"), "step": "difference_open", "turn": "team"})
				}
			}
		}
	}
	waits = append(waits, proofWaits...)
	for _, item := range waits {
		fmt.Fprintln(out, "  Waits for "+roadmapWaitingWords(item))
	}
}

// roadmapProofStands is where the Done when item Proved as agreed stands, in
// the Overview's words (Epic 459, H2): n of m with the rows proved below the
// agreed level, those accepted by name and those not proved yet, each only
// when above zero; that nothing is kept or changed; that it is read once the
// reference revision is set; or that the proof cannot be read, with the
// refusal's sentence.
func roadmapProofStands(item map[string]any) string {
	if reason, ok := item["reason"].(map[string]any); ok {
		return "proof cannot be read: " + orDash(str(reason, "message"))
	}
	counts, ok := item["counts"].(map[string]any)
	if !ok {
		return "proof cannot be read"
	}
	kept := roadmapCount(counts["kept_or_changed"])
	if kept == 0 {
		return "nothing kept or changed to prove"
	}
	if waits, _ := item["waits_for_reference_revision"].(bool); waits {
		return "read once the Release's reference revision is set"
	}
	words := fmt.Sprintf("%d of %d", roadmapCount(counts["as_agreed"]), kept)
	accepted := roadmapCount(counts["below_accepted"])
	if below := roadmapCount(counts["below"]) + accepted; below > 0 {
		words += fmt.Sprintf(" · %d %s", below, roadmapProofBelow)
		if accepted > 0 {
			words += fmt.Sprintf(" (%d accepted by name)", accepted)
		}
	}
	if notYet := roadmapCount(counts["not_yet"]); notYet > 0 {
		words += fmt.Sprintf(" · %d not proved yet", notYet)
	}
	return words
}

// roadmapProofWaits are the proof items an unmet Proved as agreed waits for,
// as the Roadmap read gives them in waiting_for (contract §3): the proof that
// cannot be read, alone; else the reference revision, alone; else the rows
// proved below the agreed level, a person's turn, and those not proved yet,
// the team's, each while its count is above zero.
func roadmapProofWaits(item map[string]any) []map[string]any {
	if reason, ok := item["reason"].(map[string]any); ok {
		return []map[string]any{{"kind": "proof", "step": "proof_unreadable", "turn": "person", "reason": reason}}
	}
	if waits, _ := item["waits_for_reference_revision"].(bool); waits {
		return []map[string]any{{"kind": "proof", "step": "proof_waits_for_reference_revision", "turn": "person"}}
	}
	counts, _ := item["counts"].(map[string]any)
	var items []map[string]any
	if below := roadmapCount(counts["below"]); below > 0 {
		items = append(items, map[string]any{"kind": "proof", "step": "proof_below_agreed_level", "turn": "person", "count": float64(below)})
	}
	if notYet := roadmapCount(counts["not_yet"]); notYet > 0 {
		items = append(items, map[string]any{"kind": "proof", "step": "proof_not_yet", "turn": "team", "count": float64(notYet)})
	}
	return items
}

func roadmapStepWords(step string) string {
	if words := roadmapSteps[step]; words != "" {
		return words
	}
	return orDash(strings.ReplaceAll(step, "_", " "))
}

// roadmapAcceptanceStates are the words of the acceptance's state: the step
// words of roadmap show, and Acceptance recorded once it is met.
var roadmapAcceptanceStates = map[string]string{
	"not_started":            "acceptance_not_started",
	"in_progress":            "acceptance_in_progress",
	"retest":                 "acceptance_retest",
	"waits_for_requirements": "acceptance_waits_for_requirements",
}

var roadmapReportStatuses = map[string]string{"draft": "draft", "signed_off": "signed off", "rejected": "rejected", "superseded": "superseded"}

// roadmapRequirementStates are the words of a requirement's state in the
// acceptance: in progress while an approved Epic holds it, spec pending while
// it has no spec at its current revision, and then its result.
var roadmapRequirementStates = map[string]string{
	"passed":       "passed",
	"failed":       "failed",
	"excluded":     "excluded",
	"not_tested":   "not tested yet",
	"in_progress":  "in progress (an approved Epic is still building it)",
	"spec_pending": "spec pending (no spec at its current revision yet)",
}

// writeRoadmapProofAcceptance prints the acceptance for the Release the
// Initiative is shown under: its step, the report's state and the counts,
// then each requirement's state with its revision, its notes and the legacy
// requirements it modernizes (Epic 337, D3).
func writeRoadmapProofAcceptance(out io.Writer, data map[string]any) {
	acceptance, ok := data["acceptance"].(map[string]any)
	if !ok {
		return
	}
	release, _ := acceptance["release"].(map[string]any)
	state := str(acceptance, "state")
	parts := []string{"Acceptance recorded"}
	if state != "met" {
		parts[0] = roadmapStepWords(roadmapAcceptanceStates[state])
	}
	if report, ok := acceptance["report"].(map[string]any); ok {
		status := roadmapReportStatuses[str(report, "status")]
		if status == "" {
			status = orDash(str(report, "status"))
		}
		parts = append(parts, "report "+status)
	} else {
		parts = append(parts, "no report yet")
	}
	if counts, ok := acceptance["counts"].(map[string]any); ok {
		parts = append(parts, fmt.Sprintf("%s passed, %s excluded, %s failed, %s not tested yet of %s",
			count(counts, "passed"), count(counts, "excluded"), count(counts, "failed"), count(counts, "not_tested"), count(counts, "total")))
	}
	fmt.Fprintln(out, roadmapAcceptanceTitle(str(release, "name"))+": "+strings.Join(parts, " · "))
	requirements, _ := acceptance["requirements"].([]any)
	for _, r := range requirements {
		requirement, ok := r.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(str(requirement, "display_ref") + " " + oneLine(str(requirement, "title")))
		words := roadmapRequirementStates[str(requirement, "state")]
		if words == "" {
			words = orDash(str(requirement, "state"))
		}
		line := fmt.Sprintf("  %s, revision %s: %s", orDash(name), acceptanceText(requirement["revision"]), words)
		if notes := oneLine(str(requirement, "notes")); notes != "" {
			line += " · " + notes
		}
		if legacy := roadmapProofRefs(requirement["legacy"]); legacy != "" {
			line += " · modernizes " + legacy
		}
		fmt.Fprintln(out, line)
	}
	if waiting := roadmapProofRefs(acceptance["waits_for_requirements"]); waiting != "" {
		fmt.Fprintln(out, "  Waits for the requirements that modernize "+waiting)
	}
}

// roadmapProofRefs names requirements by their short references: each a
// string, or an object with display_ref, else its title or id.
func roadmapProofRefs(value any) string {
	items, _ := value.([]any)
	refs := make([]string, 0, len(items))
	for _, item := range items {
		switch ref := item.(type) {
		case string:
			refs = append(refs, ref)
		case map[string]any:
			name := str(ref, "display_ref")
			if name == "" {
				name = str(ref, "title")
			}
			if name == "" {
				name = str(ref, "id")
			}
			refs = append(refs, orDash(name))
		}
	}
	return strings.Join(refs, ", ")
}

// writeRoadmapProofDifferences prints the Initiative's differences: how many
// are open, then each with its reference and title, its state, who accepted
// it and the legacy requirement it names (Epic 337, D3).
func writeRoadmapProofDifferences(out io.Writer, data map[string]any) {
	differences, ok := data["differences"].([]any)
	if !ok {
		return
	}
	if len(differences) == 0 {
		fmt.Fprintln(out, "Differences: none")
		return
	}
	open := 0
	for _, d := range differences {
		if difference, ok := d.(map[string]any); ok {
			if isOpen, _ := difference["open"].(bool); isOpen {
				open++
			}
		}
	}
	fmt.Fprintf(out, "Differences: %d open of %d\n", open, len(differences))
	for _, d := range differences {
		difference, ok := d.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(str(difference, "display_ref") + " " + oneLine(str(difference, "title")))
		state := orDash(str(difference, "state"))
		if by, ok := difference["accepted_by"].(map[string]any); ok && state == "accepted" && str(by, "name") != "" {
			state += " by " + str(by, "name")
		}
		line := fmt.Sprintf("  %s: %s", orDash(name), state)
		if legacy, ok := difference["legacy_requirement"].(map[string]any); ok {
			line += " · names " + orDash(strings.TrimSpace(str(legacy, "display_ref")+" "+oneLine(str(legacy, "title"))))
		}
		fmt.Fprintln(out, line)
	}
}

func roadmapProofLevel(code string) string {
	if words, ok := roadmapProofLevelWords[code]; ok {
		return words
	}
	if code == "" {
		return "no level yet"
	}
	return strings.ReplaceAll(code, "_", " ")
}

// roadmapProofBasis gives the basis of a row's agreed level in the
// Selection tab's words, with the person and the reason where the basis has them.
func roadmapProofBasis(row map[string]any) string {
	decision, _ := row["decision"].(map[string]any)
	person := "a person"
	if setBy, ok := decision["set_by"].(map[string]any); ok {
		if name := str(setBy, "name"); name != "" {
			person = name
		}
	}
	reason := str(decision, "expected_level_reason")
	withReason := func(words string) string {
		if reason != "" {
			return words + ": " + reason
		}
		return words
	}
	switch str(row, "expected_level_basis") {
	case "raised":
		return withReason("raised by " + person)
	case "person":
		return withReason("set by " + person)
	case "changed":
		return "changed requirement"
	case "recorded":
		return "recorded by reverse engineering"
	case "estimated":
		return "estimated from its files"
	case "":
		return "no basis"
	default:
		return strings.ReplaceAll(str(row, "expected_level_basis"), "_", " ")
	}
}

func count(counts map[string]any, key string) string {
	switch value := counts[key].(type) {
	case float64:
		return strconv.FormatInt(int64(value), 10)
	case nil:
		return "0"
	default:
		return fmt.Sprint(value)
	}
}
