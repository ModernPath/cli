package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Epic 436 (SR-4917, SR-4918): the `constraints` group against an httptest
// store. Every test builds the group through newConstraintsCommand with a load
// function, as the backlog tests do, so a file or flag refusal that must happen
// before the workspace is read can prove it never loaded.

const constraintsBasePath = "/api/v1/systems/4/engineering-constraints"

type constraintCall struct {
	method, path string
	body         map[string]any
	hasBody      bool
}

// constraintServer answers every request with the given status and body and
// records the calls it received.
func constraintServer(t *testing.T, status int, body string, calls *[]constraintCall) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := constraintCall{method: r.Method, path: r.URL.RequestURI()}
		raw := new(bytes.Buffer)
		_, _ = raw.ReadFrom(r.Body)
		if raw.Len() > 0 {
			call.hasBody = true
			if err := json.Unmarshal(raw.Bytes(), &call.body); err != nil {
				t.Errorf("request body is not a JSON object: %v", err)
			}
		}
		*calls = append(*calls, call)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func constraintRecordJSON(ref, state string, number int, revision any) string {
	record := map[string]any{
		"id": "11111111-1111-4111-8111-111111111111", "ref": ref, "number": number, "state": state, "system_id": 4,
		"title": "Phoenix controllers call Core, not Repo", "statement": "A controller never calls Aiengine.Repo.",
		"rationale": "The Core boundary keeps authorization in one place.", "verification_method": "Read the diff for Repo calls in a controller.",
		"current_revision": revision, "pending_change": false,
		"created_by":  map[string]any{"id": 7, "name": "Ivan"},
		"inserted_at": "2026-11-17T10:00:00Z", "updated_at": "2026-11-17T10:00:00Z",
	}
	blob, _ := json.Marshal(map[string]any{"data": map[string]any{"constraint": record}})
	return string(blob)
}

func runConstraints(t *testing.T, load func() (*cliEnv, error), stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := newConstraintsCommand(load)
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(stdin))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	return out.String(), err
}

func neverLoad(t *testing.T) func() (*cliEnv, error) {
	return func() (*cliEnv, error) { t.Fatal("the workspace was loaded before the local check"); return nil, nil }
}

const validConstraintFile = `{"title":"Phoenix controllers call Core, not Repo","statement":"A controller never calls Aiengine.Repo.","rationale":"The Core boundary keeps authorization in one place.","verification_method":"Read the diff for Repo calls in a controller."}`

// TestCase 6a5de14e-391b-46c0-9ec2-e690d1bc4893
func TestConstraintsCreateAndUpdateRefuseAnInvalidFileBeforeLoading(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`{"statement":"s","verification_method":"v"}`, `"title"`},
		{`{"title":"t","verification_method":"v"}`, `"statement"`},
		{`{"title":"t","statement":"s"}`, `"verification_method"`},
		{`{"title":"  ","statement":"s","verification_method":"v"}`, `"title"`},
		{`{"title":"t","statement":"s","verification_method":"v","state":"active"}`, `unsupported constraint field "state"`},
		{`{"title":"t","statement":"s","verification_method":"v","system_id":4}`, `unsupported constraint field "system_id"`},
		{`{"title":"t","statement":"s","verification_method":"v","rationale":7}`, `"rationale"`},
		{`{"title":5,"statement":"s","verification_method":"v"}`, `"title"`},
		{`[]`, "JSON object"},
	} {
		for _, args := range [][]string{{"create", "--file", "-"}, {"update", "EC-3", "--file", "-"}} {
			_, err := runConstraints(t, neverLoad(t), tc.input, args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%v with %s: error = %v; want %q", args, tc.input, err, tc.want)
			}
		}
	}
	// A ref that is neither EC-<n> nor a UUID is refused before the file is read.
	for _, ref := range []string{"EC-0", "EC-", "12", "ec-x", "EC-1 2"} {
		_, err := runConstraints(t, neverLoad(t), validConstraintFile, "update", ref, "--file", "-")
		if err == nil || !strings.Contains(err.Error(), "EC-<number>") {
			t.Errorf("update %q: error = %v", ref, err)
		}
	}
}

// TestCase 6a5de14e-391b-46c0-9ec2-e690d1bc4893
func TestConstraintsCreatePostsTheFileAndPrintsTheReceipt(t *testing.T) {
	var calls []constraintCall
	server := constraintServer(t, 201, constraintRecordJSON("EC-12", "proposed", 12, nil), &calls)
	defer server.Close()
	load := func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil }

	out, err := runConstraints(t, load, validConstraintFile, "create", "--file", "-")
	if err != nil {
		t.Fatal(err)
	}
	if out != "EC-12  proposed\n" {
		t.Fatalf("text receipt = %q", out)
	}
	if len(calls) != 1 || calls[0].method != "POST" || calls[0].path != constraintsBasePath {
		t.Fatalf("calls = %#v", calls)
	}
	body := calls[0].body
	if body["system_id"] != float64(4) || body["title"] != "Phoenix controllers call Core, not Repo" || body["statement"] != "A controller never calls Aiengine.Repo." ||
		body["rationale"] != "The Core boundary keeps authorization in one place." || body["verification_method"] != "Read the diff for Repo calls in a controller." {
		t.Fatalf("create body = %#v", body)
	}
	if _, has := body["actor"]; has {
		t.Fatalf("create without --agent sent an actor: %#v", body)
	}

	// --json prints the record as served.
	calls = nil
	out, err = runConstraints(t, load, validConstraintFile, "create", "--file", "-", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(out), &record); err != nil || record["ref"] != "EC-12" || record["state"] != "proposed" || record["title"] == nil {
		t.Fatalf("--json receipt = %s (%v)", out, err)
	}

	// The file may omit rationale; the body then carries no rationale key.
	calls = nil
	if _, err = runConstraints(t, load, `{"title":"t","statement":"s","verification_method":"v"}`, "create", "--file", "-"); err != nil {
		t.Fatal(err)
	}
	if _, has := calls[0].body["rationale"]; has {
		t.Fatalf("rationale was sent although the file omitted it: %#v", calls[0].body)
	}
}

// TestCase 6a5de14e-391b-46c0-9ec2-e690d1bc4893
// UR-904 criterion c47b6674: an agent in a session creates the constraint on
// the person's request; --agent rides the body as the actor object.
func TestConstraintsCreateByAnAgentSendsTheActor(t *testing.T) {
	var calls []constraintCall
	server := constraintServer(t, 201, constraintRecordJSON("EC-13", "proposed", 13, nil), &calls)
	defer server.Close()
	load := func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil }
	out, err := runConstraints(t, load, validConstraintFile, "create", "--file", "-", "--agent", "rdd-constraint")
	if err != nil {
		t.Fatal(err)
	}
	if out != "EC-13  proposed\n" {
		t.Fatalf("receipt = %q", out)
	}
	actor, _ := calls[0].body["actor"].(map[string]any)
	if actor["kind"] != "agent" || actor["agent_slug"] != "rdd-constraint" {
		t.Fatalf("actor = %#v", calls[0].body["actor"])
	}
}

// TestCase 6a5de14e-391b-46c0-9ec2-e690d1bc4893
func TestConstraintsUpdatePatchesByRef(t *testing.T) {
	revision := map[string]any{"number": 2, "title": "old", "statement": "old", "rationale": nil, "verification_method": "old",
		"accepted_by": map[string]any{"id": 7, "name": "Ivan"}, "accepted_at": "2026-11-10T09:00:00Z", "accept_reason": "USER:2026-11-10: accepted"}
	for _, ref := range []string{"EC-12", "ec-12", "11111111-1111-4111-8111-111111111111"} {
		var calls []constraintCall
		server := constraintServer(t, 200, constraintRecordJSON("EC-12", "active", 12, revision), &calls)
		load := func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil }
		out, err := runConstraints(t, load, validConstraintFile, "update", ref, "--file", "-", "--agent", "builder")
		server.Close()
		if err != nil {
			t.Fatal(err)
		}
		if out != "EC-12  active\n" {
			t.Fatalf("update receipt = %q", out)
		}
		if len(calls) != 1 || calls[0].method != "PATCH" || calls[0].path != constraintsBasePath+"/"+ref {
			t.Fatalf("calls = %#v", calls)
		}
		body := calls[0].body
		actor, _ := body["actor"].(map[string]any)
		if body["title"] != "Phoenix controllers call Core, not Repo" || body["system_id"] != float64(4) || actor["agent_slug"] != "builder" || actor["kind"] != "agent" {
			t.Fatalf("update body = %#v", body)
		}
	}
}

// TestCase 6a5de14e-391b-46c0-9ec2-e690d1bc4893
func TestConstraintsCreateRefusesAReceiptOfAnotherSystemOrAnotherState(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"data":{"constraint":{"id":"11111111-1111-4111-8111-111111111111","ref":"EC-1","state":"proposed","system_id":99}}}`, "different System"},
		{`{"data":{"constraint":{"id":"EC-1","ref":"EC-1","state":"proposed","system_id":4}}}`, "identity"},
		{`{"data":{"constraint":{"id":"11111111-1111-4111-8111-111111111111","ref":"EC-1","state":"active","system_id":4}}}`, "proposed"},
		{`{"data":{}}`, "missing its record"},
	} {
		var calls []constraintCall
		server := constraintServer(t, 201, tc.body, &calls)
		load := func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil }
		_, err := runConstraints(t, load, validConstraintFile, "create", "--file", "-")
		server.Close()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("receipt %s: error = %v; want %q", tc.body, err, tc.want)
		}
	}
}

// TestCase cc9a034e-f538-4477-aa7b-541e6922db98
func TestConstraintsAcceptAndDeactivateRefuseANonUserReasonLocally(t *testing.T) {
	for _, verb := range []string{"accept", "deactivate"} {
		for _, args := range [][]string{
			{verb, "EC-12"},
			{verb, "EC-12", "--reason", ""},
			{verb, "EC-12", "--reason", "   "},
			{verb, "EC-12", "--reason", "USER:"},
			{verb, "EC-12", "--reason", "USER:   "},
			{verb, "EC-12", "--reason", "accepted by Ivan"},
			{verb, "EC-12", "--reason", "user: lower case"},
		} {
			_, err := runConstraints(t, neverLoad(t), "", args...)
			if err == nil || err.Error() != "--reason must be the human's decision beginning USER:" {
				t.Errorf("%v: error = %v", args, err)
			}
		}
		// The ref is checked before the workspace as well.
		if _, err := runConstraints(t, neverLoad(t), "", verb, "nope", "--reason", "USER:2026-11-17: yes"); err == nil || !strings.Contains(err.Error(), "EC-<number>") {
			t.Errorf("%s with a bad ref: error = %v", verb, err)
		}
	}
}

// TestCase cc9a034e-f538-4477-aa7b-541e6922db98
func TestConstraintsAcceptAndDeactivatePostTheReason(t *testing.T) {
	revision := map[string]any{"number": 1, "title": "t", "statement": "s", "rationale": nil, "verification_method": "v",
		"accepted_by": map[string]any{"id": 7, "name": "Ivan"}, "accepted_at": "2026-11-17T10:00:00Z", "accept_reason": "USER:2026-11-17: hold every Epic to it"}
	for _, tc := range []struct{ verb, state string }{{"accept", "active"}, {"deactivate", "deactivated"}} {
		var calls []constraintCall
		server := constraintServer(t, 200, constraintRecordJSON("EC-12", tc.state, 12, revision), &calls)
		load := func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil }
		reason := "USER:2026-11-17: hold every Epic to it"
		out, err := runConstraints(t, load, "", tc.verb, "EC-12", "--reason", reason)
		if err != nil {
			t.Fatal(err)
		}
		if out != "EC-12  "+tc.state+"\n" {
			t.Fatalf("%s receipt = %q", tc.verb, out)
		}
		if len(calls) != 1 || calls[0].method != "POST" || calls[0].path != constraintsBasePath+"/EC-12/"+tc.verb {
			t.Fatalf("calls = %#v", calls)
		}
		if calls[0].body["reason"] != reason {
			t.Fatalf("%s body = %#v", tc.verb, calls[0].body)
		}
		if _, has := calls[0].body["actor"]; has {
			t.Fatalf("%s without --agent sent an actor: %#v", tc.verb, calls[0].body)
		}

		// --agent and --json on the same write.
		calls = nil
		out, err = runConstraints(t, load, "", tc.verb, "EC-12", "--reason", reason, "--agent", "session", "--json")
		server.Close()
		if err != nil {
			t.Fatal(err)
		}
		actor, _ := calls[0].body["actor"].(map[string]any)
		if actor["kind"] != "agent" || actor["agent_slug"] != "session" {
			t.Fatalf("%s actor = %#v", tc.verb, calls[0].body["actor"])
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(out), &record); err != nil || record["state"] != tc.state {
			t.Fatalf("%s --json = %s (%v)", tc.verb, out, err)
		}
	}
}

// TestCase cc9a034e-f538-4477-aa7b-541e6922db98
func TestConstraintsPrintAServerRefusalAsOneLine(t *testing.T) {
	for _, tc := range []struct {
		args         []string
		status       int
		body, want   string
		expectMethod string
	}{
		{[]string{"accept", "EC-12", "--reason", "USER:2026-11-17: yes"}, 422, `{"error":{"code":"nothing_to_accept","message":"EC-12 is active and has no pending change"}}`,
			"constraints accept (server 422): EC-12 is active and has no pending change", "POST"},
		{[]string{"deactivate", "EC-12", "--reason", "USER:2026-11-17: yes"}, 422, `{"error":{"code":"not_active","message":"only an active constraint can be deactivated"}}`,
			"constraints deactivate (server 422): only an active constraint can be deactivated", "POST"},
		{[]string{"delete", "EC-12"}, 422, `{"error":{"code":"active_must_be_deactivated","message":"deactivate EC-12 before deleting it"}}`,
			"constraints delete (server 422): deactivate EC-12 before deleting it", "DELETE"},
		{[]string{"get", "EC-99"}, 404, `{"error":{"code":"not_found","message":"constraint not found"}}`,
			"constraints get (server 404): constraint not found", "GET"},
		{[]string{"accept", "EC-12", "--reason", "USER:2026-11-17: yes"}, 422, `{"error":{"code":"human_decision_required"}}`,
			"constraints accept (server 422): human_decision_required", "POST"},
	} {
		var calls []constraintCall
		server := constraintServer(t, tc.status, tc.body, &calls)
		load := func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil }
		_, err := runConstraints(t, load, "", tc.args...)
		server.Close()
		if err == nil {
			t.Fatalf("%v: no error", tc.args)
		}
		first := strings.SplitN(err.Error(), "\n", 2)[0]
		if first != tc.want {
			t.Errorf("%v: first error line = %q; want %q", tc.args, first, tc.want)
		}
		if len(calls) != 1 || calls[0].method != tc.expectMethod {
			t.Errorf("%v: calls = %#v", tc.args, calls)
		}
	}
}

// TestCase 83ef66ec-9c1b-41c8-b460-8d9f7ed32526
func TestConstraintsListPrintsRowsAndTakesAStateFilter(t *testing.T) {
	list := `{"data":{"constraints":[
	  {"id":"11111111-1111-4111-8111-111111111111","ref":"EC-3","number":3,"state":"proposed","title":"No raw tasks","current_revision":null,"pending_change":false},
	  {"id":"22222222-2222-4222-8222-222222222222","ref":"EC-12","number":12,"state":"active","title":"Phoenix controllers call Core, not Repo","current_revision":{"number":2},"pending_change":true},
	  {"id":"33333333-3333-4333-8333-333333333333","ref":"EC-15","number":15,"state":"deactivated","title":"Log JSON to stdout","current_revision":{"number":1},"pending_change":false}
	]}}`
	var calls []constraintCall
	server := constraintServer(t, 200, list, &calls)
	defer server.Close()
	load := func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil }

	out, err := runConstraints(t, load, "", "list")
	if err != nil {
		t.Fatal(err)
	}
	want := "EC-3  proposed  rev -  No raw tasks\nEC-12  active  rev 2  Phoenix controllers call Core, not Repo\nEC-15  deactivated  rev 1  Log JSON to stdout\n"
	if out != want {
		t.Fatalf("list text = %q\nwant %q", out, want)
	}
	if len(calls) != 1 || calls[0].method != "GET" || calls[0].path != constraintsBasePath {
		t.Fatalf("calls = %#v", calls)
	}

	calls = nil
	out, err = runConstraints(t, load, "", "list", "--state", "active", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if calls[0].path != constraintsBasePath+"?state=active" {
		t.Fatalf("filtered path = %s", calls[0].path)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 3 || rows[1]["ref"] != "EC-12" {
		t.Fatalf("--json list = %s (%v)", out, err)
	}

	// An unknown state is refused before the workspace is read.
	_, err = runConstraints(t, neverLoad(t), "", "list", "--state", "open")
	if err == nil || !strings.Contains(err.Error(), "proposed") || !strings.Contains(err.Error(), "active") || !strings.Contains(err.Error(), "deactivated") {
		t.Fatalf("unknown state: error = %v", err)
	}
}

// TestCase 83ef66ec-9c1b-41c8-b460-8d9f7ed32526
func TestConstraintsGetPrintsTheRowTheRevisionsAndTheEvents(t *testing.T) {
	record := `{"data":{"constraint":{
	  "id":"11111111-1111-4111-8111-111111111111","ref":"EC-12","number":12,"state":"active","system_id":4,
	  "title":"Phoenix controllers call Core, not Repo","statement":"A controller never calls Aiengine.Repo directly.",
	  "rationale":"The Core boundary keeps authorization in one place.","verification_method":"Read the diff for Repo calls in a controller.",
	  "current_revision":{"number":2,"title":"Phoenix controllers call Core, not Repo","statement":"A controller never calls Aiengine.Repo.","rationale":null,"verification_method":"Read the diff for Repo calls in a controller.","accepted_by":{"id":7,"name":"Ivan"},"accepted_at":"2026-11-15T09:00:00Z","accept_reason":"USER:2026-11-15: second wording"},
	  "pending_change":true,
	  "created_by":{"id":7,"name":"Ivan"},"deactivated_by":null,"deactivated_at":null,"deactivate_reason":null,
	  "inserted_at":"2026-11-10T09:00:00Z","updated_at":"2026-11-17T10:00:00Z",
	  "revisions":[
	    {"number":1,"title":"Controllers call Core","statement":"First wording.","rationale":null,"verification_method":"Read the diff.","accepted_by":{"id":7,"name":"Ivan"},"accepted_at":"2026-11-10T09:00:00Z","accept_reason":"USER:2026-11-10: first wording"},
	    {"number":2,"title":"Phoenix controllers call Core, not Repo","statement":"A controller never calls Aiengine.Repo.","rationale":null,"verification_method":"Read the diff for Repo calls in a controller.","accepted_by":{"id":7,"name":"Ivan"},"accepted_at":"2026-11-15T09:00:00Z","accept_reason":"USER:2026-11-15: second wording"}
	  ],
	  "audit_events":[
	    {"action":"created","actor":{"id":7,"name":"Ivan"},"agent_slug":"rdd-constraint","at":"2026-11-10T08:00:00Z","reason":null},
	    {"action":"accepted","actor":{"id":7,"name":"Ivan"},"agent_slug":null,"at":"2026-11-10T09:00:00Z","reason":"USER:2026-11-10: first wording"},
	    {"action":"updated","actor":{"id":8,"name":"Mara"},"agent_slug":"builder","at":"2026-11-17T10:00:00Z","reason":null}
	  ]
	}}}`
	var calls []constraintCall
	server := constraintServer(t, 200, record, &calls)
	defer server.Close()
	load := func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil }

	out, err := runConstraints(t, load, "", "get", "EC-12")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].method != "GET" || calls[0].path != constraintsBasePath+"/EC-12" {
		t.Fatalf("calls = %#v", calls)
	}
	// The ref and state lead, then the row text, the current revision with
	// its number, then the revisions and the events oldest first.
	ordered := []string{
		"EC-12  active",
		"title: Phoenix controllers call Core, not Repo",
		"statement: A controller never calls Aiengine.Repo directly.",
		"rationale: The Core boundary keeps authorization in one place.",
		"verification_method: Read the diff for Repo calls in a controller.",
		"pending change: yes",
		"current revision: 2",
		"statement: A controller never calls Aiengine.Repo.",
		"revisions:",
		"1  2026-11-10T09:00:00Z  Ivan  USER:2026-11-10: first wording",
		"2  2026-11-15T09:00:00Z  Ivan  USER:2026-11-15: second wording",
		"events:",
		"2026-11-10T08:00:00Z  created  Ivan  agent rdd-constraint",
		"2026-11-10T09:00:00Z  accepted  Ivan  USER:2026-11-10: first wording",
		"2026-11-17T10:00:00Z  updated  Mara  agent builder",
	}
	at := 0
	for _, want := range ordered {
		i := strings.Index(out[at:], want)
		if i < 0 {
			t.Fatalf("get text lacks %q after offset %d:\n%s", want, at, out)
		}
		at += i + len(want)
	}
	if strings.Contains(out, "%!") || strings.Contains(out, "<nil>") || strings.Contains(out, "map[") {
		t.Fatalf("get text leaks a Go value:\n%s", out)
	}

	out, err = runConstraints(t, load, "", "get", "11111111-1111-4111-8111-111111111111", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["ref"] != "EC-12" || len(got["revisions"].([]any)) != 2 || len(got["audit_events"].([]any)) != 3 {
		t.Fatalf("--json get = %s (%v)", out, err)
	}
	if calls[1].path != constraintsBasePath+"/11111111-1111-4111-8111-111111111111" {
		t.Fatalf("get by UUID path = %s", calls[1].path)
	}

	// A Proposed constraint has no revision and no history yet.
	proposed := constraintServer(t, 200, `{"data":{"constraint":{"id":"11111111-1111-4111-8111-111111111111","ref":"EC-3","number":3,"state":"proposed","system_id":4,"title":"No raw tasks","statement":"Use Oban.","rationale":null,"verification_method":"grep Task.","current_revision":null,"pending_change":false,"created_by":{"id":7,"name":"Ivan"},"revisions":[],"audit_events":[]}}}`, &calls)
	defer proposed.Close()
	out, err = runConstraints(t, func() (*cliEnv, error) { return nativeCLIEnv(t, proposed, 4), nil }, "", "get", "EC-3")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"EC-3  proposed", "rationale: —", "pending change: no", "current revision: —"} {
		if !strings.Contains(out, want) {
			t.Errorf("proposed get text lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "<nil>") || strings.Contains(out, "%!") {
		t.Fatalf("proposed get text leaks a Go value:\n%s", out)
	}
}

// TestCase 83ef66ec-9c1b-41c8-b460-8d9f7ed32526
func TestConstraintsDiscardAndDeleteSendTheRefOnly(t *testing.T) {
	for _, tc := range []struct{ verb, method, suffix string }{{"discard", "POST", "/discard"}, {"delete", "DELETE", ""}} {
		var calls []constraintCall
		server := constraintServer(t, 200, constraintRecordJSON("EC-12", "active", 12, map[string]any{"number": 1}), &calls)
		load := func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil }
		out, err := runConstraints(t, load, "", tc.verb, "EC-12")
		if err != nil {
			t.Fatal(err)
		}
		if out != "EC-12  active\n" {
			t.Fatalf("%s receipt = %q", tc.verb, out)
		}
		if len(calls) != 1 || calls[0].method != tc.method || calls[0].path != constraintsBasePath+"/EC-12"+tc.suffix {
			t.Fatalf("%s calls = %#v", tc.verb, calls)
		}
		for key := range calls[0].body {
			t.Errorf("%s sent a body field %q without --agent", tc.verb, key)
		}

		// --agent adds the actor and nothing else; --json prints the record.
		calls = nil
		out, err = runConstraints(t, load, "", tc.verb, "EC-12", "--agent", "builder", "--json")
		server.Close()
		if err != nil {
			t.Fatal(err)
		}
		if len(calls[0].body) != 1 {
			t.Fatalf("%s --agent body = %#v", tc.verb, calls[0].body)
		}
		actor, _ := calls[0].body["actor"].(map[string]any)
		if actor["kind"] != "agent" || actor["agent_slug"] != "builder" {
			t.Fatalf("%s actor = %#v", tc.verb, calls[0].body["actor"])
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(out), &record); err != nil || record["ref"] != "EC-12" {
			t.Fatalf("%s --json = %s (%v)", tc.verb, out, err)
		}

		// The ref is checked locally.
		if _, err := runConstraints(t, neverLoad(t), "", tc.verb, "EC-"); err == nil || !strings.Contains(err.Error(), "EC-<number>") {
			t.Errorf("%s with a bad ref: error = %v", tc.verb, err)
		}
	}
}
