package cmd

// Epic 459 (H2), SR "One set of status and proof words in the command line",
// criterion 3 (e38c2dc3), and UR "Accept a lower level of proof by name",
// scenario 3 (81e2c6c9): modernpath roadmap accept-lower-level --file <file>
// --agent <name> sends the Initiative, the requirement, the level reached and
// the agreed level the person saw, the person's words, a USER: source and the
// agent to POST /api/v1/systems/:system_id/roadmap/proof/lower-level; it
// refuses before sending anything without --agent or with an agent name
// outside the rule; it prints the row's new state, accepted by the person with
// their words, or the server's code and sentence and exits non-zero (the 422
// {"error": {"code", "message"}}, the 403 {"error": "subscription_read_only",
// "error_description"} and the 404 {"error": "not_found"} shapes, contract §2);
// --json writes the answer. Its help and the CLI reference say that an agent
// records an acceptance only on the person's explicit word, with the person's
// own words as the source.
//
// At the Epic's base the roadmap group has no accept-lower-level command, so
// each run ends with an unknown flag error, no request reaches the server and
// the assertions fail. The tests use no new Go symbol.

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
)

const h2AcceptPath = "/api/v1/systems/1/roadmap/proof/lower-level"

const h2AcceptWords = "Screens are enough here: the feed is checked by hand each month"

// h2AcceptFile is what the agent writes from the person's explicit word: the
// row the person saw and their own words, as the words and the USER: source.
const h2AcceptFile = `{"line_id":"` + d2ProofInitiative + `","requirement_kind":"system_requirement",
 "requirement_id":"a3a3a3a3-3333-4333-8333-333333333333","reached_level":"screens_compared","expected_level":"compared",
 "words":"` + h2AcceptWords + `","source":"USER:2026-11-22:` + h2AcceptWords + `"}`

// h2AcceptFlags are the person-facing sentences the help, the reference and the
// CLI guide give.
var h2AcceptRule = []string{
	"An agent records an acceptance only on the person's explicit word",
	"with the person's own words as the USER: source and as the words",
	"never from a template",
	"accept the lower level on the Initiative's Proof tab",
}

func h2AcceptFileOnDisk(t *testing.T, content string) string {
	t.Helper()
	return writeRoadmapPlan(t, "accept.json", content)
}

// 67e0b33c-1077-4836-9cc0-4e216a85b5fe
func TestRoadmapAcceptLowerLevelSendsTheFileAndTheAgentAndPrintsTheRowsNewState(t *testing.T) {
	server, seen := roadmapServer(t, http.StatusOK, h2ProofEntry)
	cobraWorkspace(t, server, 1)
	file := h2AcceptFileOnDisk(t, h2AcceptFile)

	out, err := runRoot(t, "roadmap", "accept-lower-level", "--file", file, "--agent", "claude-code")
	if err != nil {
		t.Fatalf("roadmap accept-lower-level: %v\n%s", err, out)
	}
	got := requireOneRoadmapRequest(t, seen, http.MethodPost, h2AcceptPath)
	var want map[string]any
	if err := json.Unmarshal([]byte(h2AcceptFile), &want); err != nil {
		t.Fatal(err)
	}
	want["agent"] = "claude-code"
	if !reflect.DeepEqual(got.body, want) {
		t.Errorf("the request body = %#v\nwant %#v", got.body, want)
	}
	for _, line := range []struct{ what, pattern string }{
		{"the Initiative and its Release", `^Provider connections · Billing modernization · replaces Legacy billing$`},
		{"the counts after the write", `^5 kept or changed · 2 proved as agreed · 2 proved below the agreed level \(1 accepted by name\) · 1 not proved yet · 0 revised since selected · 0 capture not reproducible$`},
		{"the row's new state, accepted by the person with their words",
			`^  SR-14-0 Show the partner feed: keep · agreed level Compared, estimated from its files · reached Screens compared, proved below the agreed level, accepted by Pia Planner: ` + h2AcceptWords + `$`},
	} {
		if d3LineMatching(out, line.pattern) == "" {
			t.Errorf("roadmap accept-lower-level does not print %s: no line matches %s\n%s", line.what, line.pattern, out)
		}
	}
	// Only the accepted row prints, not every row of the Initiative.
	if strings.Contains(out, "SR-13-0") || strings.Contains(out, "SR-15-0") {
		t.Errorf("roadmap accept-lower-level prints rows other than the accepted one:\n%s", out)
	}
}

// 67e0b33c-1077-4836-9cc0-4e216a85b5fe
func TestRoadmapAcceptLowerLevelRefusesBeforeSendingAnything(t *testing.T) {
	for _, c := range []struct {
		name    string
		file    string
		args    []string
		answers []string
	}{
		{"without --agent", h2AcceptFile, nil, []string{"roadmap accept-lower-level needs --agent <name>"}},
		{"with an agent name outside the rule", h2AcceptFile, []string{"--agent", "Claude Code"},
			[]string{`--agent "Claude Code" is not an agent name`, roadmapRecordingAgentRule}},
		{"with a key outside the file's seven", strings.Replace(h2AcceptFile, `"line_id"`, `"agent":"claude-code","line_id"`, 1), []string{"--agent", "claude-code"},
			[]string{`unsupported accept-lower-level file field "agent"`, "line_id, requirement_kind, requirement_id, reached_level, expected_level, words, source"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			server, seen := roadmapServer(t, http.StatusOK, h2ProofEntry)
			cobraWorkspace(t, server, 1)
			file := h2AcceptFileOnDisk(t, c.file)
			out, err := runRoot(t, append([]string{"roadmap", "accept-lower-level", "--file", file}, c.args...)...)
			if err == nil {
				t.Fatalf("roadmap accept-lower-level %s ended without an error:\n%s", c.name, out)
			}
			for _, answer := range c.answers {
				if !strings.Contains(err.Error(), answer) {
					t.Errorf("the refusal %q does not say %q", err.Error(), answer)
				}
			}
			if len(*seen) != 0 {
				t.Errorf("a refused command sent %d requests; want none: %#v", len(*seen), *seen)
			}
		})
	}
}

// 67e0b33c-1077-4836-9cc0-4e216a85b5fe
func TestRoadmapAcceptLowerLevelPrintsTheServersRefusalAndExitsNonZero(t *testing.T) {
	for _, c := range []struct {
		name    string
		status  int
		body    string
		answers []string
	}{
		{"a 422 refusal with its code and sentence", http.StatusUnprocessableEntity,
			`{"error":{"code":"proof_not_below","message":"Only a requirement proved below its agreed level can be accepted at a lower level."}}`,
			[]string{"server 422", "proof_not_below", "Only a requirement proved below its agreed level can be accepted at a lower level."}},
		{"a read-only workspace", http.StatusForbidden,
			`{"error":"subscription_read_only","error_description":"This workspace is read-only. Renew the subscription to make changes."}`,
			[]string{"server 403", "subscription_read_only", "This workspace is read-only. Renew the subscription to make changes."}},
		{"no keep or change row of the requirement", http.StatusNotFound, `{"error":"not_found"}`,
			[]string{"server 404", "not_found"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			server, seen := roadmapServer(t, c.status, c.body)
			cobraWorkspace(t, server, 1)
			file := h2AcceptFileOnDisk(t, h2AcceptFile)
			out, err := runRoot(t, "roadmap", "accept-lower-level", "--file", file, "--agent", "claude-code")
			if err == nil {
				t.Fatalf("a refused acceptance ended without an error, so the command exits zero:\n%s", out)
			}
			for _, answer := range c.answers {
				if !strings.Contains(err.Error(), answer) {
					t.Errorf("the refusal %q does not say %q", err.Error(), answer)
				}
			}
			if len(*seen) != 1 {
				t.Errorf("requests = %d; want the one POST", len(*seen))
			}
			if strings.Contains(out, "accepted by") {
				t.Errorf("a refused acceptance prints a row accepted by a person:\n%s", out)
			}
		})
	}
}

// 67e0b33c-1077-4836-9cc0-4e216a85b5fe
func TestRoadmapAcceptLowerLevelJSONWritesTheServersAnswer(t *testing.T) {
	server, _ := roadmapServer(t, http.StatusOK, h2ProofEntry)
	cobraWorkspace(t, server, 1)
	file := h2AcceptFileOnDisk(t, h2AcceptFile)

	stdout, err := runRootStdout(t, "roadmap", "accept-lower-level", "--file", file, "--agent", "claude-code", "--json")
	if err != nil {
		t.Fatalf("roadmap accept-lower-level --json: %v\n%s", err, stdout)
	}
	var got, served map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("--json did not print one JSON document: %v\n%s", err, stdout)
	}
	if err := json.Unmarshal([]byte(h2ProofEntry), &served); err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(served["data"])
	have, _ := json.Marshal(got)
	if string(want) != string(have) {
		t.Errorf("--json does not write the server's answer:\n got %s\nwant %s", have, want)
	}
}

// 67e0b33c-1077-4836-9cc0-4e216a85b5fe
func TestRoadmapAcceptLowerLevelHelpReferenceAndGuideSayThePersonsOwnWords(t *testing.T) {
	requireRule := func(t *testing.T, what, text string) {
		t.Helper()
		plain := g3Plain(text)
		for _, sentence := range h2AcceptRule {
			if !strings.Contains(plain, sentence) {
				t.Errorf("%s does not say %q", what, sentence)
			}
		}
		for _, flag := range []string{"--file", "--agent", "--json"} {
			if !strings.Contains(plain, flag) {
				t.Errorf("%s does not name %s", what, flag)
			}
		}
	}
	out, err := runRoot(t, "roadmap", "accept-lower-level", "--help")
	if err != nil {
		t.Fatalf("roadmap accept-lower-level --help: %v\n%s", err, out)
	}
	requireRule(t, "the help of roadmap accept-lower-level", out)

	ref := renderCLIReference(rootCmd)
	if !strings.Contains(ref, "## `modernpath roadmap accept-lower-level`\n") {
		t.Fatalf("the CLI reference has no section for roadmap accept-lower-level")
	}
	requireRule(t, "the CLI reference's section", referenceSection(t, ref, "modernpath roadmap accept-lower-level"))

	guide, err := os.ReadFile("../internal/kit/assets/cli-guide/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	section := d2GuideSection(string(guide), "roadmap accept-lower-level")
	if section == "" {
		t.Fatalf("the kit's CLI guide has no section that runs modernpath roadmap accept-lower-level")
	}
	requireRule(t, "the CLI guide's section", section)
}
