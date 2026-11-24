package cmd

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// Epic 335 (D1): modernpath roadmap recording — link each replay test case of
// the bound System to its capture test case and to the capture's passing
// result on the legacy system, and its break test to the replay, from a file,
// on a person's word. The server checks every rule of the links; the command
// checks only the file's shape and the agent name, and a file it refuses
// sends nothing.

// roadmapRecordingAgentRule is A1's slug rule for an agent name, matched
// against the whole value. The server also refuses the name chat.
const roadmapRecordingAgentRule = "^[a-z0-9][a-z0-9._-]{0,62}$"

var roadmapRecordingAgent = regexp.MustCompile(roadmapRecordingAgentRule)

// roadmapRecordingKeys are the four keys of a recording, in the order the
// refusal lists them.
var roadmapRecordingKeys = []string{"replay_test_case_id", "break_test_case_id", "capture_test_case_id", "capture_result_id"}

func newRoadmapRecordingCommand(load func() (*cliEnv, error)) *cobra.Command {
	recording := &cobra.Command{Use: "recording", Short: "Link each replay test to its capture and its break test from a file (on a person's word)", Args: cobra.NoArgs,
		Long: `Link each replay test of the bound System to its capture test case and to the
capture's passing result on the legacy system, and each break test to its
replay, from --file (or --file - for stdin), on a person's word. Run it from a
folder bound to the new system, before the replay or its break test has run.

The file is a JSON object {"recordings": [...]}. Each recording is
{"replay_test_case_id": "<uuid>", "break_test_case_id": "<uuid>",
"capture_test_case_id": "<uuid>", "capture_result_id": "<uuid>"}: the replay
test and its break test in the bound System, and the capture test case and
its passing result on the legacy system. A key outside these four, and an
empty or missing recordings list, are refused before anything is sent.

The links are checked against an open Modernization Release of the bound
System: --release <id> names it; without it, the server takes the one whose
legacy system holds the capture test case. A capture links only when its
result passed at the Release's reference revision or, onto a new system, at a
later revision of the legacy system, and only while neither the replay nor its
break test has a recorded result. The whole file is refused at the first
recording that breaks a rule, and nothing is written. The refusal reads
<code> — item <index> (replay <uuid>) — <reason>, the index counted from 0.
The same file again writes nothing; a newer passing result of the same
capture test case moves the replay's link to it.

The command prints each recording's replay and its three links, each written,
kept or moved. --dry-run checks every recording and prints its result (would
link, would move, unchanged, or refused with the reason); nothing is written.
When any recording would be refused, --dry-run prints "<n> of <m> recordings
would be refused; nothing was written." last and exits with status 1, with
--json too, so a script can tell a refusal from success.
--agent names the coding agent (lowercase letters, digits, '.', '_' and '-');
each link's audit event names it beside the signed-in person. --json prints
the server's answer.
Example: {"recordings":[{"replay_test_case_id":"<uuid>","break_test_case_id":"<uuid>","capture_test_case_id":"<uuid>","capture_result_id":"<uuid>"}]}`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString("file")
			if path == "" {
				return fmt.Errorf("--file is required")
			}
			file, err := readObjectInput(cmd, path)
			if err != nil {
				return err
			}
			recordings, err := roadmapRecordingFile(file)
			if err != nil {
				return err
			}
			payload := map[string]any{"recordings": recordings}
			if cmd.Flags().Changed("release") {
				release, _ := cmd.Flags().GetInt64("release")
				if release <= 0 {
					return fmt.Errorf("--release must be a Release id")
				}
				payload["release_id"] = release
			}
			if cmd.Flags().Changed("agent") {
				agent, _ := cmd.Flags().GetString("agent")
				if !roadmapRecordingAgent.MatchString(agent) {
					return fmt.Errorf("--agent %q is not an agent name: it must match %s (lowercase letters, digits, '.', '_' and '-', at most 63 characters)",
						agent, roadmapRecordingAgentRule)
				}
				payload["agent"] = agent
			}
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			if dryRun {
				payload["dry_run"] = true
			}

			env, err := roadmapEnv(load)
			if err != nil {
				return err
			}
			status, body, err := env.write(http.MethodPost, "/api/v1/systems/"+strconv.Itoa(env.SystemID)+"/roadmap/recording", payload)
			if err != nil {
				return err
			}
			if status < 200 || status >= 300 {
				return roadmapRecordingRefusal(status, body)
			}
			data, ok := body["data"].(map[string]any)
			if !ok {
				return fmt.Errorf("roadmap recording response is missing its data")
			}
			// Epic 461 (H4), SR-8: a dry run that would refuse a recording
			// exits with status 1 after printing, also with --json.
			refused := roadmapRecordingRefusedCount(data)
			if machine, _ := cmd.Flags().GetBool("json"); machine {
				if err := writeCommandJSON(cmd, data); err != nil {
					return err
				}
				if dryRun && refused.refused > 0 {
					return reportedError{refused.err()}
				}
				return nil
			}
			if dryRun {
				writeRoadmapRecordingDryRun(cmd.OutOrStdout(), data)
				if refused.refused > 0 {
					fmt.Fprintln(cmd.OutOrStdout(), refused.err().Error())
					return reportedError{refused.err()}
				}
			} else {
				writeRoadmapRecordingLinks(cmd.OutOrStdout(), data)
			}
			return nil
		}}
	recording.Flags().String("file", "", "Recordings JSON file, or - for stdin (required)")
	recording.Flags().Int64("release", 0, "The open Modernization Release to check the recordings against")
	recording.Flags().String("agent", "", "The coding agent linking the replays, recorded beside the signed-in person")
	recording.Flags().Bool("dry-run", false, "Check every recording and print its result; write nothing")
	recording.Flags().Bool("json", false, "Emit JSON")
	return recording
}

// roadmapRecordingFile checks the file's shape and returns its recordings,
// unchanged: only recordings at the top, a non-empty list, and no key outside
// the four in a recording. Every other rule is the server's.
func roadmapRecordingFile(file map[string]any) ([]any, error) {
	if key := firstKeyOutside(file, []string{"recordings"}); key != "" {
		return nil, fmt.Errorf("unsupported recording file field %q (allowed: recordings)", key)
	}
	recordings, ok := file["recordings"].([]any)
	if !ok || len(recordings) == 0 {
		return nil, fmt.Errorf("the recording file needs a non-empty \"recordings\" array")
	}
	for i, entry := range recordings {
		recording, ok := entry.(map[string]any)
		if !ok {
			continue // the server refuses it as invalid_recordings
		}
		if key := firstKeyOutside(recording, roadmapRecordingKeys); key != "" {
			return nil, fmt.Errorf("recording %d: unsupported field %q (allowed: %s)", i, key, strings.Join(roadmapRecordingKeys, ", "))
		}
	}
	return recordings, nil
}

// firstKeyOutside is the first key of m, in sorted order, that allowed does
// not hold, or "".
func firstKeyOutside(m map[string]any, allowed []string) string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		known := false
		for _, a := range allowed {
			known = known || key == a
		}
		if !known {
			return key
		}
	}
	return ""
}

// roadmapRecordingRefusal reads a refused recording as
// "<code> — item <i> (replay <uuid>) — <message>". A refusal of the whole
// request (invalid_agent, invalid_recordings) carries no index; it keeps its
// code before its sentence.
func roadmapRecordingRefusal(status int, body map[string]any) error {
	errVal, _ := body["error"].(map[string]any)
	code, message := str(errVal, "code"), str(errVal, "message")
	if _, indexed := body["index"]; !indexed && code != "" && message != "" {
		return serverRefusalError{status: status, msg: fmt.Sprintf("roadmap recording refused (server %d): %s — %s", status, code, message)}
	}
	return serverRefusal("roadmap recording refused", status, body)
}

// writeRoadmapRecordingLinks prints each recording's replay and, under it, its
// three links with their states.
func writeRoadmapRecordingLinks(out io.Writer, data map[string]any) {
	recordings, _ := data["recordings"].([]any)
	for _, r := range recordings {
		recording, ok := r.(map[string]any)
		if !ok {
			continue
		}
		links, _ := recording["links"].(map[string]any)
		result, _ := recording["capture_result"].(map[string]any)
		fmt.Fprintf(out, "recording %s: replay %s\n", roadmapRecordingIndex(recording), roadmapRecordingTitled(recording["replay"]))
		fmt.Fprintf(out, "  capture test case %s: %s\n", roadmapRecordingTitled(recording["capture_test_case"]), roadmapRecordingLinkState(links["capture_test_case"]))
		fmt.Fprintf(out, "  capture result %s at %s: %s\n", str(result, "id"), str(result, "commit"), roadmapRecordingLinkState(links["capture_result"]))
		fmt.Fprintf(out, "  break test %s: %s\n", roadmapRecordingTitled(recording["break_test"]), roadmapRecordingLinkState(links["break_test"]))
	}
}

// roadmapRecordingRefusals counts the recordings a dry run would refuse.
type roadmapRecordingRefusals struct{ refused, total int }

func (r roadmapRecordingRefusals) err() error {
	return fmt.Errorf("%d of %d recordings would be refused; nothing was written.", r.refused, r.total)
}

func roadmapRecordingRefusedCount(data map[string]any) roadmapRecordingRefusals {
	recordings, _ := data["recordings"].([]any)
	count := roadmapRecordingRefusals{total: len(recordings)}
	for _, r := range recordings {
		if recording, ok := r.(map[string]any); ok && str(recording, "result") == "refused" {
			count.refused++
		}
	}
	return count
}

// writeRoadmapRecordingDryRun prints what each recording would do.
func writeRoadmapRecordingDryRun(out io.Writer, data map[string]any) {
	fmt.Fprintln(out, "Dry run: nothing was written.")
	recordings, _ := data["recordings"].([]any)
	for _, r := range recordings {
		recording, ok := r.(map[string]any)
		if !ok {
			continue
		}
		answer := str(recording, "result")
		switch answer {
		case "link", "move":
			answer = "would " + answer
		case "refused":
			if refusal, ok := recording["error"].(map[string]any); ok {
				answer = "refused: " + str(refusal, "code") + " — " + str(refusal, "message")
			}
		}
		fmt.Fprintf(out, "recording %s: replay %s: %s\n", roadmapRecordingIndex(recording), roadmapRecordingTitled(recording["replay"]), answer)
	}
}

// roadmapRecordingIndex is a recording's index as served, counted from 0.
func roadmapRecordingIndex(recording map[string]any) string {
	if index := recording["index"]; index != nil {
		return fmt.Sprint(index)
	}
	return "?"
}

// roadmapRecordingTitled is "<title> (<id>)".
func roadmapRecordingTitled(value any) string {
	m, _ := value.(map[string]any)
	return str(m, "title") + " (" + str(m, "id") + ")"
}

// roadmapRecordingLinkState is a link's state; a moved capture result link
// names the result it moved from.
func roadmapRecordingLinkState(value any) string {
	link, _ := value.(map[string]any)
	state := str(link, "state")
	if replaced := str(link, "replaced_result_id"); state == "moved" && replaced != "" {
		return "moved from " + replaced
	}
	return state
}
