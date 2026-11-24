package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// Epic 459 (H2): modernpath roadmap accept-lower-level — record, on a
// person's explicit word, that a kept or changed legacy requirement of an
// Initiative stays proved below its agreed level, as the Proof tab's Accept
// the lower level does, on the same route. The command checks only the file's
// shape and the agent name; every other rule is the server's.

// roadmapAcceptKeys are the seven keys of the file, in the order the refusal
// lists them.
var roadmapAcceptKeys = []string{"line_id", "requirement_kind", "requirement_id", "reached_level", "expected_level", "words", "source"}

func newRoadmapAcceptLowerLevelCommand(load func() (*cliEnv, error)) *cobra.Command {
	accept := &cobra.Command{Use: "accept-lower-level", Short: "Accept a requirement's lower level of proof by name, from a file (on the person's explicit word)", Args: cobra.NoArgs,
		Long: `Record that a kept or changed legacy requirement of an Initiative stays proved
below its agreed level, from --file (or --file - for stdin), as a person does
with Accept the lower level on the Initiative's Proof tab. Run it from a folder
bound to the new system.

An agent records an acceptance only on the person's explicit word, with the
person's own words as the USER: source and as the words, never from a
template. Otherwise it asks the person to accept the lower level on the
Initiative's Proof tab. The acceptance names the signed-in person.

The file is a JSON object with the row the person saw and their words:
{"line_id": "<initiative uuid>", "requirement_kind": "user_requirement" or
"system_requirement", "requirement_id": "<uuid>", "reached_level": "<the
level reached>", "expected_level": "<the agreed level>", "words": "<the
person's words>", "source": "USER:<date>:<the person's words>"}. The levels
are the codes roadmap proof --json gives each row: compared, screens_compared,
tests_only or reviewed_only. A key outside these seven is refused before
anything is sent.

--agent names the coding agent (lowercase letters, digits, '.', '_' and '-', at
most 63 characters) and is required: the acceptance's audit event names it
beside the signed-in person, so the audit trail tells an agent's recording from
the Proof tab's. Without --agent, or with a name outside that rule, the command
sends nothing.

The server accepts the lower level only on a row below its agreed level (it
reached Screens compared, Tests only or Reviewed only) of an Initiative shown
under an open Release or under none, while the row is at the levels the person
saw. An acceptance counts while the row's Initiative, outcome and agreed level
stay the same and the row is at exactly the level accepted. The same person's
same acceptance again writes nothing. A refusal prints the server's code and
sentence and the command exits non-zero: invalid_agent, words_required,
words_too_long (over 2,000 characters), human_source_required (no USER:
followed by text), release_closed, release_not_modernization, proof_changed
(the row's levels differ from the file's) or proof_not_below (the row is
proved as agreed or not proved yet); not_found when the bound System, the
Initiative or its keep or change row of the requirement is not found; and
subscription_read_only in a read-only workspace.

The command prints the Initiative and its Release, the counts and the row's new
state in the words of roadmap proof, such as "reached Tests only, proved below
the agreed level, accepted by <name>: <words>". --json writes the server's
answer, the Initiative's proof entry after the write.
Example: {"line_id":"<uuid>","requirement_kind":"system_requirement","requirement_id":"<uuid>","reached_level":"tests_only","expected_level":"compared","words":"Tests are enough: the export is checked by hand","source":"USER:2026-11-22:Tests are enough: the export is checked by hand"}`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString("file")
			if path == "" {
				return fmt.Errorf("--file is required")
			}
			agent, _ := cmd.Flags().GetString("agent")
			if agent == "" {
				return fmt.Errorf("roadmap accept-lower-level needs --agent <name>: the coding agent that records the person's acceptance")
			}
			if !roadmapRecordingAgent.MatchString(agent) {
				return fmt.Errorf("--agent %q is not an agent name: it must match %s (lowercase letters, digits, '.', '_' and '-', at most 63 characters)",
					agent, roadmapRecordingAgentRule)
			}
			file, err := readObjectInput(cmd, path)
			if err != nil {
				return err
			}
			if key := firstKeyOutside(file, roadmapAcceptKeys); key != "" {
				return fmt.Errorf("unsupported accept-lower-level file field %q (allowed: %s); the agent comes from --agent",
					key, strings.Join(roadmapAcceptKeys, ", "))
			}
			payload := make(map[string]any, len(file)+1)
			for key, value := range file {
				payload[key] = value
			}
			payload["agent"] = agent

			env, err := roadmapEnv(load)
			if err != nil {
				return err
			}
			status, body, err := env.write(http.MethodPost, "/api/v1/systems/"+strconv.Itoa(env.SystemID)+"/roadmap/proof/lower-level", payload)
			if err != nil {
				return err
			}
			if status < 200 || status >= 300 {
				return roadmapAcceptRefusal(status, body)
			}
			data, ok := body["data"].(map[string]any)
			if !ok {
				return fmt.Errorf("roadmap accept-lower-level response is missing its data")
			}
			if machine, _ := cmd.Flags().GetBool("json"); machine {
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
			writeRoadmapAcceptance(cmd.OutOrStdout(), data, str(file, "requirement_kind"), str(file, "requirement_id"))
			return nil
		}}
	accept.Flags().String("file", "", "Acceptance JSON file, or - for stdin (required)")
	accept.Flags().String("agent", "", "The coding agent recording the person's acceptance, beside the signed-in person (required)")
	accept.Flags().Bool("json", false, "Print the server's answer as JSON")
	return accept
}

// roadmapAcceptRefusal reads the route's refusals: a 422 {"error": {"code",
// "message"}}, the 403 {"error": "subscription_read_only",
// "error_description"} of a read-only workspace and the 404 {"error":
// "not_found"}, each as its code and sentence.
func roadmapAcceptRefusal(status int, body map[string]any) error {
	const what = "roadmap accept-lower-level refused"
	switch refusal := body["error"].(type) {
	case map[string]any:
		if code := str(refusal, "code"); code != "" {
			text := code
			if message := str(refusal, "message"); message != "" {
				text += " — " + message
			}
			return serverRefusalError{status: status, msg: fmt.Sprintf("%s (server %d): %s", what, status, text)}
		}
	case string:
		if description := str(body, "error_description"); description != "" {
			return serverRefusalError{status: status, msg: fmt.Sprintf("%s (server %d): %s — %s", what, status, refusal, description)}
		}
		if refusal == "not_found" {
			return serverRefusalError{status: status, msg: withBindingHint(status, fmt.Sprintf(
				"%s (server %d): not_found — the bound System, the Initiative or its keep or change row of the requirement was not found", what, status))}
		}
	}
	return serverRefusal(what, status, body)
}

// writeRoadmapAcceptance prints the Initiative and its Release, the counts and
// the accepted row's new state, as roadmap proof prints them.
func writeRoadmapAcceptance(out io.Writer, data map[string]any, kind, id string) {
	fmt.Fprintln(out, roadmapProofHead(data))
	if counts, ok := data["counts"].(map[string]any); ok {
		fmt.Fprintln(out, roadmapProofCounts(counts))
	}
	rows, _ := data["rows"].([]any)
	for _, r := range rows {
		if row, ok := r.(map[string]any); ok && str(row, "requirement_kind") == kind && strings.EqualFold(str(row, "requirement_id"), id) {
			fmt.Fprintln(out, roadmapProofRowLine(row))
			return
		}
	}
	fmt.Fprintf(out, "  The answer holds no row of %s %s; read it with roadmap proof.\n", orDash(kind), orDash(id))
}
