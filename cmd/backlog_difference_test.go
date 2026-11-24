package cmd

// Epic 337 (D3), test case TC-D3-6 (9ca2ef3a-fc94-4824-b89a-855b9fc2a301).
//
// Criterion 77008421 ("The backlog gap kind difference, with its fields" at 4):
// modernpath backlog create sends a difference with legacy_requirement,
// what_changed, evidence and, when given, requirement, accepting_accepts and
// risk. Before it sends anything, it refuses one without legacy_requirement,
// what_changed or evidence, and one that sets classification or
// changes_stored_values, saying that a person sets them with Accept on the
// Initiative's Proof tab. Its help names the fields and the four
// classifications and says who sets them. The server takes a difference's
// fields on create under metadata.difference, as it stores them, so the file
// gives them there and the command sends them there; a top-level difference
// key stays an unsupported create field.
//
// Criterion f93bff7b ("A difference is resolved only by a fix or by a person's
// acceptance by name" at 6): modernpath backlog update reads the record first
// only when the disposition it would send starts with ACCEPTED with, and for a
// difference refuses to send it, saying that a person accepts a difference by
// name on the Initiative's Proof tab and the agent never does. It sends CLOSED
// by and the other dispositions without reading the record, and never a
// difference object. Its help states the difference rules and the two accepted
// forms of CLOSED by a pull request, PR #<number> and the pull request's
// address.
//
// On slice 1 backlog create refuses every difference before it sends anything,
// with the old message that a gap requires gap_kind capability or
// specification, so the server receives no POST and the refusals do not say
// why; backlog update sends ACCEPTED with as the PATCH alone, without reading
// the record. That CLOSED by and the other dispositions go out as the PATCH
// alone holds on the slice and guards backlog_test.go's TestBacklogUpdate. The
// test uses no new Go symbol, so it builds on the slice.
//
// Review round 2 (audit group A's third doubt): backlog get serves the difference
// key the server gives, as docs/cli.md says: --full --json carries it and --fields
// difference is accepted. Before the fix --full left it out and --fields refused it.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const (
	d3DiffRecord = "11111111-1111-4111-8111-111111111111"
	d3DiffLegacy = "a1a1a1a1-1111-4111-8111-111111111111"
	d3DiffNew    = "b2b2b2b2-2222-4222-8222-222222222222"
)

// The sentences the command says, as the criteria give them.
const (
	// Epic 461 (H4): the Proof tab's Accept form became the panel Decide opens.
	d3PersonSetsClassification = "a person sets classification and changes_stored_values with Decide on the Initiative's Proof tab"
	d3PersonAcceptsByName      = "a person accepts a difference by name on the Initiative's Proof tab, and the agent never does"
)

type d3BacklogRequest struct {
	method, path string
	query        url.Values
	body         map[string]any
}

// d3BacklogServer records every request and answers a POST with a new OPEN
// difference, a GET with record and a PATCH with record updated by the body.
func d3BacklogServer(t *testing.T, record map[string]any) (*httptest.Server, *[]d3BacklogRequest) {
	t.Helper()
	seen := []d3BacklogRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		seen = append(seen, d3BacklogRequest{method: r.Method, path: r.URL.Path, query: r.URL.Query(), body: body})
		w.Header().Set("Content-Type", "application/json")
		answer := map[string]any{}
		for key, value := range record {
			answer[key] = value
		}
		switch r.Method {
		case http.MethodPost:
			metadata, _ := body["metadata"].(map[string]any)
			answer = map[string]any{"id": d3DiffRecord, "system_id": 4, "display_ref": "Gap 7", "kind": "gap", "gap_kind": "difference",
				"title": body["title"], "disposition": "OPEN", "difference": metadata["difference"]}
			w.WriteHeader(http.StatusCreated)
		case http.MethodPatch:
			for key, value := range body {
				answer[key] = value
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"backlog": answer}})
	}))
	t.Cleanup(server.Close)
	return server, &seen
}

// d3RunBacklog runs the backlog command with input on stdin against server.
func d3RunBacklog(t *testing.T, server *httptest.Server, input string, args ...string) (string, error) {
	t.Helper()
	cmd := newBacklogCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(input))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	return out.String(), err
}

func d3JSON(t *testing.T, value any) string {
	t.Helper()
	blob, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(blob)
}

// d3Folded folds whitespace and code marks, so a help phrase may wrap.
func d3Folded(text string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(text, "`", "")), " ")
}

func TestD3_9ca2ef3a_BacklogCreateSendsADifferenceWithItsFields(t *testing.T) {
	for _, c := range []struct {
		name       string
		difference string
	}{
		{"with every field",
			`{"legacy_requirement":{"kind":"user_requirement","id":"` + d3DiffLegacy + `"},
			  "what_changed":"Retries stop after three attempts instead of five",
			  "evidence":"The replay of the capture fails on the fourth retry",
			  "requirement":{"kind":"user_requirement","id":"` + d3DiffNew + `"},
			  "accepting_accepts":"Partners see a failed delivery after three attempts",
			  "risk":"A partner that relied on five attempts loses messages"}`},
		{"with the three it needs",
			`{"legacy_requirement":{"kind":"system_requirement","id":"` + d3DiffLegacy + `"},
			  "what_changed":"Totals are rounded to cents","evidence":"Run 12, test Totals"}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			server, seen := d3BacklogServer(t, nil)
			input := `{"kind":"gap","gap_kind":"difference","title":"Retries stop after three","metadata":{"difference":` + c.difference + `}}`
			out, err := d3RunBacklog(t, server, input, "create", "--file", "-", "--json")
			if err != nil {
				t.Fatalf("backlog create of a difference: %v\n%s", err, out)
			}
			if len(*seen) != 1 || (*seen)[0].method != http.MethodPost || (*seen)[0].path != "/api/v1/backlog" {
				t.Fatalf("the server received %#v; want one POST /api/v1/backlog", *seen)
			}
			body := (*seen)[0].body
			var want map[string]any
			if err := json.Unmarshal([]byte(c.difference), &want); err != nil {
				t.Fatal(err)
			}
			metadata, _ := body["metadata"].(map[string]any)
			if got := d3JSON(t, metadata["difference"]); got != d3JSON(t, want) {
				t.Errorf("the POST sent metadata.difference %s; want the file's fields as given %s", got, d3JSON(t, want))
			}
			if _, top := body["difference"]; top {
				t.Errorf("the POST sent a top-level difference key: %#v", body)
			}
			if body["kind"] != "gap" || body["gap_kind"] != "difference" || body["title"] != "Retries stop after three" || body["system_id"] != float64(4) {
				t.Errorf("the POST sent %#v; want kind gap, gap_kind difference, the title and system_id 4", body)
			}
			if !strings.Contains(out, d3DiffRecord) {
				t.Errorf("backlog create printed no record:\n%s", out)
			}
		})
	}
}

func TestD3_9ca2ef3a_BacklogCreateRefusesADifferenceBeforeSendingAnything(t *testing.T) {
	legacy := `"legacy_requirement":{"kind":"user_requirement","id":"` + d3DiffLegacy + `"}`
	what := `"what_changed":"Retries stop after three attempts"`
	evidence := `"evidence":"The replay fails on the fourth retry"`
	for _, c := range []struct {
		name, fields string
		want         []string
	}{
		{"without legacy_requirement", what + `,` + evidence, []string{"legacy_requirement"}},
		{"without what_changed", legacy + `,` + evidence, []string{"what_changed"}},
		{"without evidence", legacy + `,` + what, []string{"evidence"}},
		{"with classification", legacy + `,` + what + `,` + evidence + `,"classification":"not_defect"`,
			[]string{"classification", d3PersonSetsClassification}},
		{"with changes_stored_values", legacy + `,` + what + `,` + evidence + `,"changes_stored_values":false`,
			[]string{"changes_stored_values", d3PersonSetsClassification}},
	} {
		t.Run(c.name, func(t *testing.T) {
			server, seen := d3BacklogServer(t, nil)
			input := `{"kind":"gap","gap_kind":"difference","title":"Retries stop after three","metadata":{"difference":{` + c.fields + `}}}`
			out, err := d3RunBacklog(t, server, input, "create", "--file", "-")
			if err == nil {
				t.Fatalf("backlog create sent a difference %s:\n%s", c.name, out)
			}
			if len(*seen) != 0 {
				t.Errorf("backlog create reached the server %d times before refusing a difference %s", len(*seen), c.name)
			}
			for _, want := range c.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal of a difference %s does not say %q: %v", c.name, want, err)
				}
			}
		})
	}
}

// A top-level difference key stays an unsupported create field (guard: it
// holds on the slice, where every field outside the list is refused).
func TestD3_9ca2ef3a_BacklogCreateRefusesATopLevelDifferenceKey(t *testing.T) {
	server, seen := d3BacklogServer(t, nil)
	input := `{"kind":"gap","gap_kind":"difference","title":"Retries stop after three","difference":{"legacy_requirement":{"kind":"user_requirement","id":"` + d3DiffLegacy + `"},"what_changed":"Retries stop after three attempts","evidence":"The replay fails"}}`
	out, err := d3RunBacklog(t, server, input, "create", "--file", "-")
	if err == nil || !strings.Contains(err.Error(), `unsupported Backlog field "difference"`) {
		t.Errorf("backlog create with a top-level difference key: error = %v; want the unsupported field refused\n%s", err, out)
	}
	if len(*seen) != 0 {
		t.Errorf("backlog create reached the server %d times with a top-level difference key", len(*seen))
	}
}

func TestD3_9ca2ef3a_BacklogCreateHelpNamesTheDifferenceFields(t *testing.T) {
	cmd := newBacklogCommand(func() (*cliEnv, error) { t.Fatal("help loaded the workspace"); return nil, nil })
	cmd.SetArgs([]string{"create", "--help"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	help := d3Folded(out.String())
	for _, want := range []string{
		"gap_kind capability, specification or difference",
		"metadata.difference",
		"legacy_requirement", "what_changed", "evidence", "requirement", "accepting_accepts", "risk",
		"introduced, legacy, not_defect or unclassified",
		d3PersonSetsClassification,
	} {
		if !strings.Contains(help, want) {
			t.Errorf("the help of backlog create does not say %q:\n%s", want, help)
		}
	}
}

// d3Difference is a difference as the backlog read serves it.
var d3Difference = map[string]any{"id": d3DiffRecord, "system_id": 4, "display_ref": "Gap 7", "kind": "gap", "gap_kind": "difference",
	"title": "Retries stop after three", "disposition": "OPEN",
	"difference": map[string]any{"legacy_requirement": map[string]any{"kind": "user_requirement", "id": d3DiffLegacy, "display_ref": "UR-13-0", "title": "Retry the partner feed"},
		"what_changed": "Retries stop after three attempts", "evidence": "The replay fails on the fourth retry", "classification": "unclassified",
		"changes_stored_values": nil, "state": "open", "open": true}}

func TestD3_9ca2ef3a_BacklogGetServesTheDifference(t *testing.T) {
	decoded := func(t *testing.T, args ...string) map[string]any {
		t.Helper()
		server, seen := d3BacklogServer(t, d3Difference)
		out, err := d3RunBacklog(t, server, "", append([]string{"get", d3DiffRecord, "--json"}, args...)...)
		if err != nil {
			t.Fatalf("backlog get %v: %v\n%s", args, err, out)
		}
		if len(*seen) != 1 || (*seen)[0].method != http.MethodGet || (*seen)[0].path != "/api/v1/backlog/"+d3DiffRecord {
			t.Fatalf("backlog get %v asked %#v; want one GET of the record", args, *seen)
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(out), &record); err != nil {
			t.Fatalf("backlog get %v did not write JSON: %v\n%s", args, err, out)
		}
		return record
	}
	servedDifference := func(t *testing.T, record map[string]any) {
		t.Helper()
		difference, ok := record["difference"].(map[string]any)
		if !ok {
			t.Fatalf("the record has no difference: %#v", record)
		}
		if difference["what_changed"] != "Retries stop after three attempts" || difference["state"] != "open" {
			t.Errorf("the record's difference is not the one served: %#v", difference)
		}
	}

	servedDifference(t, decoded(t, "--full"))
	fields := decoded(t, "--fields", "difference")
	servedDifference(t, fields)
	if len(fields) != 1 {
		t.Errorf("--fields difference gives %#v; want the difference alone", fields)
	}
	if _, given := decoded(t)["difference"]; given {
		t.Errorf("the default read gives the difference; it gives only the record's identity and disposition")
	}
}

func TestD3_9ca2ef3a_BacklogUpdateRefusesToSendTheAcceptanceOfADifference(t *testing.T) {
	server, seen := d3BacklogServer(t, d3Difference)
	input := `{"disposition":"ACCEPTED with USER:the owner accepts three retries","notes_md":"Accepted"}`
	out, err := d3RunBacklog(t, server, input, "update", d3DiffRecord, "--file", "-")
	if err == nil {
		t.Errorf("backlog update sent the acceptance of a difference:\n%s", out)
	} else if !strings.Contains(err.Error(), d3PersonAcceptsByName) {
		t.Errorf("the refusal does not say %q: %v", d3PersonAcceptsByName, err)
	}
	reads, patches := 0, 0
	for _, r := range *seen {
		switch r.method {
		case http.MethodGet:
			reads++
			if r.path != "/api/v1/backlog/"+d3DiffRecord || r.query.Get("system_id") != "4" {
				t.Errorf("the read was %s %s?%s; want GET /api/v1/backlog/%s?system_id=4", r.method, r.path, r.query.Encode(), d3DiffRecord)
			}
		case http.MethodPatch:
			patches++
		}
	}
	if reads != 1 {
		t.Errorf("backlog update read the record %d times before ACCEPTED with; want once: %#v", reads, *seen)
	}
	if patches != 0 {
		t.Errorf("backlog update sent ACCEPTED with for a difference %d times; want none: %#v", patches, *seen)
	}
}

func TestD3_9ca2ef3a_BacklogUpdateReadsFirstOnlyForAcceptedWith(t *testing.T) {
	assumption := map[string]any{"id": d3DiffRecord, "system_id": 4, "display_ref": "Assumed default 3", "kind": "assumption",
		"title": "Show the points balance on the receipt", "disposition": "ROUTED to 288"}
	t.Run("ACCEPTED with on a record that is not a difference is read, then sent", func(t *testing.T) {
		server, seen := d3BacklogServer(t, assumption)
		input := `{"disposition":"ACCEPTED with USER:yes, show the balance"}`
		out, err := d3RunBacklog(t, server, input, "update", d3DiffRecord, "--file", "-", "--json")
		if err != nil {
			t.Fatalf("backlog update of an assumed default: %v\n%s", err, out)
		}
		if len(*seen) != 2 || (*seen)[0].method != http.MethodGet || (*seen)[1].method != http.MethodPatch {
			t.Fatalf("the server received %#v; want the read, then the PATCH", *seen)
		}
		if got := (*seen)[1].body["disposition"]; got != "ACCEPTED with USER:yes, show the balance" {
			t.Errorf("the PATCH sent disposition %v", got)
		}
	})
	for _, disposition := range []string{
		"CLOSED by 288",
		"CLOSED by PR #976",
		"CLOSED by https://github.com/modernpath/new-billing/pull/976",
		"CLOSED by PR:972",
		"OPEN",
		"DEFERRED",
		"ROUTED to 288",
		"REJECTED with USER:not ours",
	} {
		t.Run(disposition+" is sent as the PATCH alone (guard)", func(t *testing.T) {
			server, seen := d3BacklogServer(t, d3Difference)
			input := d3JSON(t, map[string]any{"disposition": disposition, "notes_md": "Fixed by the merged change"})
			out, err := d3RunBacklog(t, server, input, "update", d3DiffRecord, "--file", "-", "--json")
			if err != nil {
				t.Fatalf("backlog update with %s: %v\n%s", disposition, err, out)
			}
			if len(*seen) != 1 || (*seen)[0].method != http.MethodPatch || (*seen)[0].path != "/api/v1/backlog/"+d3DiffRecord {
				t.Fatalf("the server received %#v; want one PATCH and no read", *seen)
			}
			body := (*seen)[0].body
			if body["disposition"] != disposition {
				t.Errorf("the PATCH sent disposition %v; want %q", body["disposition"], disposition)
			}
			if _, sent := body["difference"]; sent {
				t.Errorf("the PATCH sent a difference object: %#v", body)
			}
		})
	}
}

func TestD3_9ca2ef3a_BacklogUpdateNeverSendsADifferenceObject(t *testing.T) {
	server, seen := d3BacklogServer(t, d3Difference)
	input := `{"difference":{"classification":"not_defect","changes_stored_values":false}}`
	out, err := d3RunBacklog(t, server, input, "update", d3DiffRecord, "--file", "-")
	if err == nil {
		t.Fatalf("backlog update sent a difference object:\n%s", out)
	}
	if len(*seen) != 0 {
		t.Errorf("backlog update reached the server %d times with a difference object", len(*seen))
	}
	for _, want := range []string{"never sends a difference object", "only the Proof tab's decision panel, a person's act, sets a difference's classification and changes_stored_values"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal of a difference object does not say %q: %v", want, err)
		}
	}
}

func TestD3_9ca2ef3a_BacklogUpdateHelpStatesTheDifferenceRules(t *testing.T) {
	cmd := newBacklogCommand(func() (*cliEnv, error) { t.Fatal("help loaded the workspace"); return nil, nil })
	cmd.SetArgs([]string{"update", "--help"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	help := d3Folded(out.String())
	for _, want := range []string{
		"A difference is resolved only by a fix or by a person's acceptance by name",
		d3PersonAcceptsByName,
		"reads the record first only when the disposition starts with ACCEPTED with",
		"never sends a difference object",
		"CLOSED by PR #<number>",
		"CLOSED by <the pull request's address>",
		"CLOSED by PR:972",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("the help of backlog update does not say %q:\n%s", want, help)
		}
	}
}

// Epic 461 (H4), TC-H4-S20 (SR-4 criterion 4): backlog create sends both
// Systems' answers, legacy_answer and new_answer, under metadata.difference,
// and refuses before sending, naming the field, any answer that is not an
// object of exactly commit (7 to 64 hexadecimal characters) and text (1 to
// 2,000 code points). Its help names both as optional: an agent adds them
// when a replay or test shows both Systems' outputs, and evidence alone stays
// the normal case.
//
// f4a173d3-e304-4b32-9f6f-0403d9caa311
func TestH4_BacklogCreateSendsBothSystemsAnswersAndShapeChecksThem(t *testing.T) {
	legacy := `"legacy_requirement":{"kind":"user_requirement","id":"` + d3DiffLegacy + `"},"what_changed":"The usage line replaces unknown command","evidence":"The replay of the quote capture fails"`
	answers := `"legacy_answer":{"commit":"a180f96","text":"unknown command"},"new_answer":{"commit":"601b46f0a1b2c3d4e5f60718293a4b5c6d7e8f90","text":"usage: python3 -m neworders quote <sku> <qty>"}`

	server, seen := d3BacklogServer(t, nil)
	input := `{"kind":"gap","gap_kind":"difference","title":"Usage line","metadata":{"difference":{` + legacy + `,` + answers + `}}}`
	out, err := d3RunBacklog(t, server, input, "create", "--file", "-", "--json")
	if err != nil {
		t.Fatalf("backlog create with both answers: %v\n%s", err, out)
	}
	if len(*seen) != 1 || (*seen)[0].method != http.MethodPost {
		t.Fatalf("the server received %#v; want one POST", *seen)
	}
	metadata, _ := (*seen)[0].body["metadata"].(map[string]any)
	difference, _ := metadata["difference"].(map[string]any)
	wantLegacy := map[string]any{"commit": "a180f96", "text": "unknown command"}
	wantNew := map[string]any{"commit": "601b46f0a1b2c3d4e5f60718293a4b5c6d7e8f90", "text": "usage: python3 -m neworders quote <sku> <qty>"}
	if got := d3JSON(t, difference["legacy_answer"]); got != d3JSON(t, wantLegacy) {
		t.Errorf("the POST sent legacy_answer %s; want %s", got, d3JSON(t, wantLegacy))
	}
	if got := d3JSON(t, difference["new_answer"]); got != d3JSON(t, wantNew) {
		t.Errorf("the POST sent new_answer %s; want %s", got, d3JSON(t, wantNew))
	}

	long := strings.Repeat("\U0001F600", 1000) + strings.Repeat("w", 1001)
	exact := strings.Repeat("\U0001F600", 1000) + strings.Repeat("w", 1000)
	for _, c := range []struct {
		name, field, answer string
		refused             bool
	}{
		{"a commit of 7 characters and a text of 2,000", "legacy_answer", `{"commit":"ABCDEF0","text":` + d3JSON(t, exact) + `}`, false},
		{"a commit of 64 characters", "new_answer", `{"commit":"` + strings.Repeat("a", 64) + `","text":"x"}`, false},
		{"a commit of 6 characters", "legacy_answer", `{"commit":"a180f9","text":"x"}`, true},
		{"a commit of 65 characters", "new_answer", `{"commit":"` + strings.Repeat("a", 65) + `","text":"x"}`, true},
		{"a commit that is not hexadecimal", "legacy_answer", `{"commit":"a180f9z","text":"x"}`, true},
		{"an empty text", "new_answer", `{"commit":"a180f96","text":""}`, true},
		{"a blank text", "new_answer", `{"commit":"a180f96","text":"   "}`, true},
		{"a text of 2,001 code points", "new_answer", `{"commit":"a180f96","text":` + d3JSON(t, long) + `}`, true},
		{"a missing text", "legacy_answer", `{"commit":"a180f96"}`, true},
		{"a missing commit", "legacy_answer", `{"text":"x"}`, true},
		{"an extra key", "new_answer", `{"commit":"a180f96","text":"x","exit":2}`, true},
		{"a text that is not a string", "new_answer", `{"commit":"a180f96","text":2}`, true},
		{"a string", "legacy_answer", `"unknown command"`, true},
		{"null", "legacy_answer", `null`, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			server, seen := d3BacklogServer(t, nil)
			input := `{"kind":"gap","gap_kind":"difference","title":"Usage line","metadata":{"difference":{` + legacy + `,"` + c.field + `":` + c.answer + `}}}`
			out, err := d3RunBacklog(t, server, input, "create", "--file", "-")
			if !c.refused {
				if err != nil || len(*seen) != 1 {
					t.Fatalf("backlog create refused %s: %v\n%s", c.name, err, out)
				}
				return
			}
			want := c.field + " takes exactly commit (7 to 64 hexadecimal characters) and text (1 to 2,000 characters)"
			if err == nil || err.Error() != want {
				t.Errorf("backlog create with %s: error = %v; want %q", c.name, err, want)
			}
			if len(*seen) != 0 {
				t.Errorf("backlog create reached the server %d times with %s", len(*seen), c.name)
			}
		})
	}

	cmd := newBacklogCommand(func() (*cliEnv, error) { t.Fatal("help loaded the workspace"); return nil, nil })
	cmd.SetArgs([]string{"create", "--help"})
	var help bytes.Buffer
	cmd.SetOut(&help)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	folded := d3Folded(help.String())
	for _, want := range []string{
		`Optional, legacy_answer and new_answer, each {"commit":"<7 to 64 hexadecimal characters>","text":"<1 to 2,000 characters>"}, give both Systems' answers`,
		"an agent adds them when a replay or test shows both Systems' outputs",
		"evidence alone stays the normal case",
	} {
		if !strings.Contains(folded, want) {
			t.Errorf("the help of backlog create does not say %q:\n%s", want, folded)
		}
	}
}
