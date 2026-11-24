package cmd

// Epic 335 (D1), test case 0c182a43 ("D1 · modernpath roadmap recording sends
// the file with its flags, checks it locally, prints the links and a refusal,
// and traces list's help, the command reference and the CLI guide describe
// it"), criterion fe0ecccd (contract §5, §6 and §10):
//   - `modernpath roadmap recording --file <file> [--release <id>] [--agent
//     <slug>] [--dry-run] [--json]` POSTs {"recordings": <the file's,
//     unchanged>, "release_id", "agent", "dry_run": true} to the bound
//     System's route, each of the last three only with its flag;
//   - it refuses, exiting non-zero and sending nothing, a top-level key other
//     than recordings, a recording key outside the four, an empty or missing
//     recordings, and an --agent outside A1's slug rule;
//   - it prints each recording's links, a dry run's result for each
//     recording, and a 422 or 409 refusal as
//     `<code> — item <i> (replay <id>) — <message>`; --json prints the
//     route's data;
//   - phrase tests under the same criterion: `traces list --help`, the
//     rendered CLI reference (the rdd-modernize row of the pass-to-command
//     table and four of D1's refusal codes in the refusals table) and the kit
//     copy of the CLI guide, each read with the whitespace folded and the
//     backticks stripped.
//
// On the first slice the command sends the file unchanged and prints nothing,
// and the help, the reference and the guide say nothing of it, so every test
// fails on an assertion. Guards, asserted first where they hold on the slice:
// one POST to the bound System's route, the recordings sent unchanged, and no
// release_id, agent or dry_run without their flags. The tests drive the root
// command and use no new production symbol, so they build on the slice.

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const d1RecPath = "/api/v1/systems/1/roadmap/recording"

// A1's slug rule for an agent name (contract §5).
const d1RecAgentRule = "^[a-z0-9][a-z0-9._-]{0,62}$"

const (
	d1RecReplay1   = "a1a1a1a1-1111-4111-8111-000000000001"
	d1RecBreak1    = "b1b1b1b1-1111-4111-8111-000000000001"
	d1RecCapture1  = "c1c1c1c1-1111-4111-8111-000000000001"
	d1RecResult1   = "e1e1e1e1-1111-4111-8111-000000000001"
	d1RecReplay2   = "a2a2a2a2-2222-4222-8222-000000000002"
	d1RecBreak2    = "b2b2b2b2-2222-4222-8222-000000000002"
	d1RecCapture2  = "c2c2c2c2-2222-4222-8222-000000000002"
	d1RecResult2   = "e2e2e2e2-2222-4222-8222-000000000002"
	d1RecReplaced2 = "e0e0e0e0-2222-4222-8222-000000000002"
	d1RecReplay3   = "a3a3a3a3-3333-4333-8333-000000000003"
	d1RecReplay4   = "a4a4a4a4-4444-4444-8444-000000000004"
)

// The recordings file: two recordings, the second with its keys in another
// order, as a person may write it.
const d1RecFile = `{"recordings": [
  {"replay_test_case_id": "` + d1RecReplay1 + `", "break_test_case_id": "` + d1RecBreak1 + `",
   "capture_test_case_id": "` + d1RecCapture1 + `", "capture_result_id": "` + d1RecResult1 + `"},
  {"capture_result_id": "` + d1RecResult2 + `", "capture_test_case_id": "` + d1RecCapture2 + `",
   "break_test_case_id": "` + d1RecBreak2 + `", "replay_test_case_id": "` + d1RecReplay2 + `"}
]}`

// The route's answer for written links (contract §4, §5): the first recording
// writes its three links; the second keeps two and moves its capture result
// link from an earlier result.
const d1RecLinkedData = `{"release":{"id":21,"name":"Cutover"},"dry_run":false,"recordings":[
 {"index":0,"replay":{"id":"` + d1RecReplay1 + `","title":"Invoice total replay"},
  "break_test":{"id":"` + d1RecBreak1 + `","title":"Invoice total break test"},
  "capture_test_case":{"id":"` + d1RecCapture1 + `","title":"Invoice total capture"},
  "capture_result":{"id":"` + d1RecResult1 + `","commit":"a1b2c3d4e5"},
  "links":{"capture_test_case":{"trace_id":"71000000-0000-4000-8000-000000000001","state":"written"},
           "capture_result":{"trace_id":"71000000-0000-4000-8000-000000000002","state":"written"},
           "break_test":{"trace_id":"71000000-0000-4000-8000-000000000003","state":"written"}}},
 {"index":1,"replay":{"id":"` + d1RecReplay2 + `","title":"Tax rounding replay"},
  "break_test":{"id":"` + d1RecBreak2 + `","title":"Tax rounding break test"},
  "capture_test_case":{"id":"` + d1RecCapture2 + `","title":"Tax rounding capture"},
  "capture_result":{"id":"` + d1RecResult2 + `","commit":"0f0e0d0c0b"},
  "links":{"capture_test_case":{"trace_id":"72000000-0000-4000-8000-000000000001","state":"kept"},
           "capture_result":{"trace_id":"72000000-0000-4000-8000-000000000002","state":"moved","replaced_result_id":"` + d1RecReplaced2 + `"},
           "break_test":{"trace_id":"72000000-0000-4000-8000-000000000003","state":"kept"}}}]}`

const d1RecLinkedReply = `{"data":` + d1RecLinkedData + `}`

// The route's answer for a dry run: one recording of each result.
const d1RecDryRunReply = `{"data":{"release":{"id":21,"name":"Cutover"},"dry_run":true,"recordings":[
 {"index":0,"replay":{"id":"` + d1RecReplay1 + `","title":"Invoice total replay"},"result":"link","error":null},
 {"index":1,"replay":{"id":"` + d1RecReplay2 + `","title":"Tax rounding replay"},"result":"move","error":null},
 {"index":2,"replay":{"id":"` + d1RecReplay3 + `","title":"Discount replay"},"result":"unchanged","error":null},
 {"index":3,"replay":{"id":"` + d1RecReplay4 + `","title":"Credit note replay"},"result":"refused",
  "error":{"code":"capture_result_not_passing","message":"The capture result e9 did not pass, so no replay can be linked to it."}}]}}`

type d1RecRequest struct {
	method, path string
	body         map[string]any
}

// d1RecServer answers the reachability probe and records every other request,
// replying with status and reply.
func d1RecServer(t *testing.T, status int, reply string) (*httptest.Server, *[]d1RecRequest) {
	t.Helper()
	seen := []d1RecRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		var decoded map[string]any
		_ = json.NewDecoder(r.Body).Decode(&decoded)
		seen = append(seen, d1RecRequest{method: r.Method, path: r.URL.Path, body: decoded})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(server.Close)
	return server, &seen
}

func d1RecWriteFile(t *testing.T, name, content string) string {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return name
}

// d1RecOneRequest is a guard on the slice: the command sends one POST to the
// bound System's route.
func d1RecOneRequest(t *testing.T, seen *[]d1RecRequest) d1RecRequest {
	t.Helper()
	if len(*seen) != 1 {
		t.Fatalf("requests = %#v; want one POST %s", *seen, d1RecPath)
	}
	got := (*seen)[0]
	if got.method != http.MethodPost || got.path != d1RecPath {
		t.Fatalf("request = %s %s; want POST %s", got.method, got.path, d1RecPath)
	}
	return got
}

// d1RecSameRecordings is a guard on the slice: the request's recordings are
// the file's, unchanged.
func d1RecSameRecordings(t *testing.T, body map[string]any, file string) {
	t.Helper()
	var want map[string]any
	if err := json.Unmarshal([]byte(file), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(body["recordings"], want["recordings"]) {
		got, _ := json.Marshal(body["recordings"])
		wantJSON, _ := json.Marshal(want["recordings"])
		t.Fatalf("recordings = %s; want the file's, unchanged: %s", got, wantJSON)
	}
}

func d1RecBodyKeys(body map[string]any) []string {
	keys := make([]string, 0, len(body))
	for key := range body {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// d1RecLines is the output's non-empty lines with their surrounding whitespace
// trimmed, so the indentation of a link line is not asserted.
func d1RecLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// d1RecHasBlock says whether block appears in lines as consecutive lines.
func d1RecHasBlock(lines, block []string) bool {
	for i := 0; i+len(block) <= len(lines); i++ {
		if reflect.DeepEqual(lines[i:i+len(block)], block) {
			return true
		}
	}
	return false
}

// d1RecFolded strips Markdown code marks and folds whitespace, so a phrase may
// wrap or mark its words.
func d1RecFolded(text string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(text, "`", "")), " ")
}

func TestD1_0c182a43_SendsTheRecordingsUnchangedWithTheReleaseAndTheAgent(t *testing.T) {
	server, seen := d1RecServer(t, http.StatusOK, d1RecLinkedReply)
	cobraWorkspace(t, server, 1)
	file := d1RecWriteFile(t, "recordings.json", d1RecFile)

	out, err := runRoot(t, "roadmap", "recording", "--file", file, "--release", "21", "--agent", "claude-code")
	if err != nil {
		t.Fatalf("roadmap recording: %v\n%s", err, out)
	}
	got := d1RecOneRequest(t, seen)
	d1RecSameRecordings(t, got.body, d1RecFile)

	if got.body["release_id"] != float64(21) {
		t.Errorf("release_id = %#v; want 21 from --release", got.body["release_id"])
	}
	if got.body["agent"] != "claude-code" {
		t.Errorf("agent = %#v; want \"claude-code\" from --agent", got.body["agent"])
	}
	if dry, present := got.body["dry_run"]; present && dry != false {
		t.Errorf("dry_run = %#v without --dry-run", dry)
	}
	for _, key := range d1RecBodyKeys(got.body) {
		switch key {
		case "recordings", "release_id", "agent", "dry_run":
		default:
			t.Errorf("the request carries %q; want only recordings, release_id, agent and dry_run", key)
		}
	}
}

func TestD1_0c182a43_SendsNoFlagKeysWithoutTheirFlagsAndPrintsEachRecordingsLinks(t *testing.T) {
	server, seen := d1RecServer(t, http.StatusOK, d1RecLinkedReply)
	cobraWorkspace(t, server, 1)
	file := d1RecWriteFile(t, "recordings.json", d1RecFile)

	out, err := runRoot(t, "roadmap", "recording", "--file", file)
	if err != nil {
		t.Fatalf("roadmap recording: %v\n%s", err, out)
	}
	got := d1RecOneRequest(t, seen)
	d1RecSameRecordings(t, got.body, d1RecFile)
	if keys := d1RecBodyKeys(got.body); !reflect.DeepEqual(keys, []string{"recordings"}) {
		t.Fatalf("the request carries %q without --release, --agent and --dry-run; want only recordings", keys)
	}

	lines := d1RecLines(out)
	for _, block := range [][]string{
		{
			"recording 0: replay Invoice total replay (" + d1RecReplay1 + ")",
			"capture test case Invoice total capture (" + d1RecCapture1 + "): written",
			"capture result " + d1RecResult1 + " at a1b2c3d4e5: written",
			"break test Invoice total break test (" + d1RecBreak1 + "): written",
		},
		{
			"recording 1: replay Tax rounding replay (" + d1RecReplay2 + ")",
			"capture test case Tax rounding capture (" + d1RecCapture2 + "): kept",
			"capture result " + d1RecResult2 + " at 0f0e0d0c0b: moved from " + d1RecReplaced2,
			"break test Tax rounding break test (" + d1RecBreak2 + "): kept",
		},
	} {
		if !d1RecHasBlock(lines, block) {
			t.Errorf("the output lacks the links of %s as the lines\n  %s\noutput:\n%s", block[0], strings.Join(block, "\n  "), out)
		}
	}
}

func TestD1_0c182a43_DryRunSendsDryRunAndPrintsEachRecordingsResult(t *testing.T) {
	server, seen := d1RecServer(t, http.StatusOK, d1RecDryRunReply)
	cobraWorkspace(t, server, 1)
	file := d1RecWriteFile(t, "recordings.json", d1RecFile)

	out, err := runRoot(t, "roadmap", "recording", "--file", file, "--release", "21", "--dry-run")
	// Epic 461 (H4), SR-8 (TC-H4-S19): a dry run with a refused recording exits with status 1 after
	// printing every result, with the count of the refused ones last.
	var refused reportedError
	if !errors.As(err, &refused) || refused.Error() != "1 of 4 recordings would be refused; nothing was written." {
		t.Fatalf("roadmap recording --dry-run with a refused recording: err = %v; want the reported refusal count\n%s", err, out)
	}
	if lines := d1RecLines(out); len(lines) == 0 || lines[len(lines)-1] != "1 of 4 recordings would be refused; nothing was written." {
		t.Errorf("the last line is not the refusal count:\n%s", out)
	}
	got := d1RecOneRequest(t, seen)
	d1RecSameRecordings(t, got.body, d1RecFile)

	if got.body["dry_run"] != true {
		t.Errorf("dry_run = %#v; want true from --dry-run", got.body["dry_run"])
	}
	if got.body["release_id"] != float64(21) {
		t.Errorf("release_id = %#v; want 21 from --release", got.body["release_id"])
	}
	if _, present := got.body["agent"]; present {
		t.Errorf("agent = %#v without --agent", got.body["agent"])
	}

	lines := d1RecLines(out)
	if len(lines) == 0 || lines[0] != "Dry run: nothing was written." {
		t.Errorf("the first line is not %q:\n%s", "Dry run: nothing was written.", out)
	}
	for _, want := range []string{
		"recording 0: replay Invoice total replay (" + d1RecReplay1 + "): would link",
		"recording 1: replay Tax rounding replay (" + d1RecReplay2 + "): would move",
		"recording 2: replay Discount replay (" + d1RecReplay3 + "): unchanged",
		"recording 3: replay Credit note replay (" + d1RecReplay4 + "): refused: capture_result_not_passing — The capture result e9 did not pass, so no replay can be linked to it.",
	} {
		if !d1RecHasBlock(lines, []string{want}) {
			t.Errorf("the dry run's output lacks the line %q:\n%s", want, out)
		}
	}
}

func TestD1_0c182a43_JSONPrintsTheRoutesData(t *testing.T) {
	server, seen := d1RecServer(t, http.StatusOK, d1RecLinkedReply)
	cobraWorkspace(t, server, 1)
	file := d1RecWriteFile(t, "recordings.json", d1RecFile)

	out, err := runRoot(t, "roadmap", "recording", "--file", file, "--json")
	if err != nil {
		t.Fatalf("roadmap recording --json: %v\n%s", err, out)
	}
	d1RecOneRequest(t, seen)

	var want, printed any
	if err := json.Unmarshal([]byte(d1RecLinkedData), &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(out), &printed); err != nil {
		t.Fatalf("--json printed no JSON document (%v):\n%s", err, out)
	}
	if !reflect.DeepEqual(printed, want) {
		t.Errorf("--json printed\n%s\nwant the route's data\n%s", out, d1RecLinkedData)
	}
}

func TestD1_0c182a43_PrintsARefusalsCodeItemAndSentence(t *testing.T) {
	for _, refusal := range []struct {
		name, code, message, replay string
		status, index               int
	}{
		{"422 capture_result_not_passing", "capture_result_not_passing",
			"The capture result " + d1RecResult1 + " did not pass, so no replay can be linked to it.", d1RecReplay1, http.StatusUnprocessableEntity, 0},
		{"409 replay_already_run", "replay_already_run",
			"The replay Tax rounding replay already has a recorded result, so it cannot be linked to a capture now.", d1RecReplay2, http.StatusConflict, 1},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			reply, _ := json.Marshal(map[string]any{
				"error": map[string]any{"code": refusal.code, "message": refusal.message},
				"index": refusal.index,
				"item":  map[string]any{"kind": "replay", "id": refusal.replay},
			})
			server, seen := d1RecServer(t, refusal.status, string(reply))
			cobraWorkspace(t, server, 1)
			file := d1RecWriteFile(t, "recordings.json", d1RecFile)

			out, err := runRoot(t, "roadmap", "recording", "--file", file)
			d1RecOneRequest(t, seen)
			if err == nil {
				t.Fatalf("a %d refusal exited zero; want an error naming its code, item and sentence:\n%s", refusal.status, out)
			}
			want := refusal.code + " — item " + strconv.Itoa(refusal.index) + " (replay " + refusal.replay + ") — " + refusal.message
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q; want it to contain %q", err.Error(), want)
			}
		})
	}
}

func TestD1_0c182a43_RefusesLocallyAndSendsNothing(t *testing.T) {
	recording := func(replay, extra string) string {
		return `{"replay_test_case_id":"` + replay + `","break_test_case_id":"` + d1RecBreak1 +
			`","capture_test_case_id":"` + d1RecCapture1 + `","capture_result_id":"` + d1RecResult1 + `"` + extra + `}`
	}
	for _, refusal := range []struct {
		name, file string
		flags      []string
		want       []string
	}{
		{"an unknown top-level key",
			`{"recordings":[` + recording(d1RecReplay1, "") + `],"release_id":21}`, nil,
			[]string{`unsupported recording file field "release_id" (allowed: recordings)`}},
		{"an unknown recording key",
			`{"recordings":[` + recording(d1RecReplay1, "") + `,` + recording(d1RecReplay2, `,"commit":"a1b2c3d4e5"`) + `]}`, nil,
			[]string{`recording 1: unsupported field "commit" (allowed: replay_test_case_id, break_test_case_id, capture_test_case_id, capture_result_id)`}},
		{"an empty recordings", `{"recordings":[]}`, nil, []string{"recordings"}},
		{"a missing recordings", `{}`, nil, []string{"recordings"}},
		{"an agent name outside the slug rule",
			`{"recordings":[` + recording(d1RecReplay1, "") + `]}`, []string{"--agent", "Claude Code"},
			[]string{"--agent", d1RecAgentRule}},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			server, seen := d1RecServer(t, http.StatusOK, d1RecLinkedReply)
			cobraWorkspace(t, server, 1)
			file := d1RecWriteFile(t, "recordings.json", refusal.file)

			args := append([]string{"roadmap", "recording", "--file", file}, refusal.flags...)
			out, err := runRoot(t, args...)
			if err == nil {
				t.Fatalf("%s was accepted (requests %#v):\n%s", refusal.name, *seen, out)
			}
			if strings.Contains(err.Error(), "unknown command") || strings.Contains(err.Error(), "unknown flag") {
				t.Fatalf("roadmap recording or its flag is unknown: %v", err)
			}
			for _, want := range refusal.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q; want it to contain %q", err.Error(), want)
				}
			}
			if len(*seen) != 0 {
				t.Errorf("the refused file reached the server: %#v", *seen)
			}
		})
	}
}

func TestD1_0c182a43_TracesListHelpNamesRoadmapRecordingAsTheWriterOfAReplaysLinks(t *testing.T) {
	out, err := runRoot(t, "traces", "list", "--help")
	if err != nil {
		t.Fatalf("traces list --help: %v", err)
	}
	want := "A replay's links to its capture test case and to the capture's passing result, and its break test's link to the replay, are written only by roadmap recording."
	if !strings.Contains(d1RecFolded(out), want) {
		t.Errorf("traces list --help lacks %q:\n%s", want, out)
	}
}

// d1RecSection returns the rendered loop's section under heading, up to the
// next heading of the same or a higher level.
func d1RecSection(t *testing.T, loop, heading string) string {
	t.Helper()
	start := strings.Index(loop, heading+"\n")
	if start < 0 {
		t.Fatalf("the rendered reference has no section %q", heading)
	}
	rest := loop[start+len(heading)+1:]
	end := len(rest)
	for _, next := range []string{"\n### ", "\n## "} {
		if i := strings.Index(rest, next); i >= 0 && i < end {
			end = i
		}
	}
	return rest[:end]
}

func d1RecTableRows(section string) []string {
	var rows []string
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "| ") {
			rows = append(rows, line)
		}
	}
	return rows
}

func TestD1_0c182a43_CLIReferenceHasTheModernizeRowAndTheRecordingRefusals(t *testing.T) {
	ref := renderCLIReference(rootCmd)
	loop := ref
	if i := strings.Index(ref, "## Global flags"); i >= 0 {
		loop = ref[:i]
	}

	var modernize []string
	for _, row := range d1RecTableRows(d1RecSection(t, loop, "### Pass to command")) {
		cells := strings.Split(strings.Trim(row, "| "), " | ")
		if len(cells) >= 3 && d1RecFolded(cells[1]) == "rdd-modernize" {
			modernize = append(modernize, row)
		}
	}
	switch {
	case len(modernize) != 1:
		t.Errorf("the pass-to-command table has %d rows whose Skill is rdd-modernize; want one: %q", len(modernize), modernize)
	default:
		cells := strings.Split(strings.Trim(modernize[0], "| "), " | ")
		if commands := d1RecFolded(strings.Join(cells[2:], " | ")); !strings.Contains(commands, "roadmap recording") {
			t.Errorf("the rdd-modernize row's commands do not hold roadmap recording: %s", modernize[0])
		}
	}

	rows := d1RecTableRows(d1RecSection(t, loop, "### Refusals and what clears them"))
	for _, code := range []string{"capture_result_not_passing", "capture_revision_not_allowed", "capture_result_older", "replay_already_run"} {
		named := false
		for _, row := range rows {
			if strings.Contains(d1RecFolded(row), code) {
				named = true
				break
			}
		}
		if !named {
			t.Errorf("the refusals table has no row naming %s", code)
		}
	}
}

// The kit copy of the CLI guide, opened by its path as reference_test.go does;
// cli_guide_test.go keeps it equal to modernpath-core/docs/cli.md.
func TestD1_0c182a43_CLIGuideDescribesRoadmapRecording(t *testing.T) {
	raw, err := os.ReadFile("../internal/kit/assets/cli-guide/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	guide := d1RecFolded(string(raw))
	for _, want := range []string{
		"Run roadmap recording from a folder bound to the new system.",
		"It links each replay test to its capture test case and to the capture's passing result, and the break test to the replay, before either has run.",
		"--dry-run answers each recording's result and writes nothing.",
		"The whole file is refused at the first recording that breaks a rule, and nothing is written.",
		"recordings", "replay_test_case_id", "break_test_case_id", "capture_test_case_id", "capture_result_id",
	} {
		if !strings.Contains(guide, want) {
			t.Errorf("the CLI guide lacks %q", want)
		}
	}
}

// h4RecRun runs the root command as Execute does and answers its output and
// its exit status.
func h4RecRun(t *testing.T, args ...string) (string, int) {
	t.Helper()
	resetTreeFlags(rootCmd)
	rootCmd.SetArgs(args)
	defer func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	}()
	code := -1
	out := captureOut(t, func() {
		rootCmd.SetOut(os.Stdout)
		rootCmd.SetErr(os.Stderr)
		beginCommandRun(args)
		code = finishCommandRun(rootCmd.Execute())
	})
	return out, code
}

// Epic 461 (H4), TC-H4-S19 (SR "roadmap recording --dry-run exits with an
// error when a recording would be refused"): a dry run prints each
// recording's result as before and, when any would be refused, the last line
// "<n> of <m> recordings would be refused; nothing was written." and exits
// with status 1, with --json too after printing the answer; a dry run with no
// refusal and a refusal of the whole request exit as today; the help says so.
//
// b2062e08-6e32-4b1c-8c50-9b661ac8d5ce
func TestH4_RoadmapRecordingDryRunExitsOneWhenARecordingWouldBeRefused(t *testing.T) {
	server, _ := d1RecServer(t, http.StatusOK, d1RecDryRunReply)
	cobraWorkspace(t, server, 1)
	file := d1RecWriteFile(t, "recordings.json", d1RecFile)
	out, code := h4RecRun(t, "roadmap", "recording", "--file", file, "--dry-run")
	lines := d1RecLines(out)
	if code != 1 {
		t.Errorf("a dry run with a refused recording exits %d; want 1\n%s", code, out)
	}
	if len(lines) < 6 || lines[len(lines)-1] != "1 of 4 recordings would be refused; nothing was written." {
		t.Errorf("the last line is not %q:\n%s", "1 of 4 recordings would be refused; nothing was written.", out)
	}
	if !d1RecHasBlock(lines, []string{"recording 3: replay Credit note replay (" + d1RecReplay4 + "): refused: capture_result_not_passing — The capture result e9 did not pass, so no replay can be linked to it."}) {
		t.Errorf("the dry run no longer prints each recording's result:\n%s", out)
	}

	server, _ = d1RecServer(t, http.StatusOK, d1RecDryRunReply)
	cobraWorkspace(t, server, 1)
	file = d1RecWriteFile(t, "recordings.json", d1RecFile)
	out, code = h4RecRun(t, "roadmap", "recording", "--file", file, "--dry-run", "--json")
	if code != 1 {
		t.Errorf("a dry run with --json and a refused recording exits %d; want 1\n%s", code, out)
	}
	var answer map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &answer); err != nil || answer["dry_run"] != true {
		t.Errorf("--json prints the answer alone: %v\n%s", err, out)
	}

	clean := strings.ReplaceAll(d1RecDryRunReply, `"result":"refused",
  "error":{"code":"capture_result_not_passing","message":"The capture result e9 did not pass, so no replay can be linked to it."}`, `"result":"link","error":null`)
	server, _ = d1RecServer(t, http.StatusOK, clean)
	cobraWorkspace(t, server, 1)
	file = d1RecWriteFile(t, "recordings.json", d1RecFile)
	out, code = h4RecRun(t, "roadmap", "recording", "--file", file, "--dry-run")
	if code != 0 || strings.Contains(out, "would be refused") {
		t.Errorf("a dry run with no refusal exits %d and says:\n%s", code, out)
	}

	server, _ = d1RecServer(t, http.StatusUnprocessableEntity, `{"error":{"code":"invalid_recordings","message":"The file holds no recording."}}`)
	cobraWorkspace(t, server, 1)
	file = d1RecWriteFile(t, "recordings.json", d1RecFile)
	out, code = h4RecRun(t, "roadmap", "recording", "--file", file, "--dry-run")
	if code != 1 || !strings.Contains(out, "roadmap recording refused (server 422): invalid_recordings — The file holds no recording.") || strings.Contains(out, "would be refused") {
		t.Errorf("a refusal of the whole request exits %d and says:\n%s", code, out)
	}

	cmd := newRoadmapRecordingCommand(func() (*cliEnv, error) { t.Fatal("help loaded the workspace"); return nil, nil })
	cmd.SetArgs([]string{"--help"})
	var help bytes.Buffer
	cmd.SetOut(&help)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if folded := d3Folded(help.String()); !strings.Contains(folded, `When any recording would be refused, --dry-run prints "<n> of <m> recordings would be refused; nothing was written." last and exits with status 1, with --json too, so a script can tell a refusal from success.`) {
		t.Errorf("the help does not say how a refused dry run exits:\n%s", folded)
	}
}
