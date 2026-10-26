package cmd

// Epic 345: modernpath initiative — Initiative rounds (review, approve,
// submit and complete an Initiative's Epics in one step) and list, get,
// create, link and unlink over the existing /api/roadmap/lines routes. The
// tests drive rootCmd, so before the command group exists each fails on
// cobra's "unknown command" error rather than on a build error.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const (
	iniLine  = "44444444-4444-4444-8444-444444444444"
	iniOther = "55555555-5555-4555-8555-555555555555"
	iniReq   = "66666666-6666-4666-8666-666666666666"
)

type iniRequest struct {
	method, path string
	query        map[string]string
	body         map[string]any
}

// iniServer answers the reachability probe and records every other request;
// reply chooses the status and the whole JSON body for each request.
func iniServer(t *testing.T, reply func(r *http.Request) (int, any)) (*httptest.Server, *[]iniRequest) {
	t.Helper()
	seen := []iniRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &body)
		}
		query := map[string]string{}
		for key := range r.URL.Query() {
			query[key] = r.URL.Query().Get(key)
		}
		seen = append(seen, iniRequest{method: r.Method, path: r.URL.Path, query: query, body: body})
		status, out := reply(r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(server.Close)
	return server, &seen
}

func iniOK(data any) func(*http.Request) (int, any) {
	return func(*http.Request) (int, any) { return 200, map[string]any{"data": data} }
}

func iniText(out string, err error) string {
	if err != nil {
		return out + "\n" + err.Error()
	}
	return out
}

func iniJSON(t *testing.T, value any) string {
	t.Helper()
	blob, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(blob)
}

// TestCase ce6768fc: initiative round commands send their request bodies and
// query strings, check exits nonzero when not ready, a refusal prints the
// Epic's index and id, and invalid ids or --epics fail before any request.
func TestInitiativeRoundCommandsSendTheirRequestBodiesAndQueryStringsCheckExitsNonzeroWhenNotReadyARefusalPrintsTheEpicsIndexAndIdAndInvalidIdsOrEpicsFailBeforeAnyRequest(t *testing.T) {
	round := "/api/work/initiatives/" + iniLine
	for _, action := range []string{"approve-implementation", "submit-review", "approve-completion"} {
		t.Run(action, func(t *testing.T) {
			server, seen := iniServer(t, iniOK(map[string]any{"items": []any{map[string]any{"id": 7}, map[string]any{"id": 3}}}))
			cobraWorkspace(t, server, 1)
			out, err := runRoot(t, "initiative", action, iniLine, "--epics", "7,3", "--reason", "USER:2026-10-25 round")
			if err != nil {
				t.Fatalf("initiative %s: %v\n%s", action, err, out)
			}
			if len(*seen) != 1 {
				t.Fatalf("requests = %#v", *seen)
			}
			got := (*seen)[0]
			if got.method != "POST" || got.path != round+"/"+action {
				t.Fatalf("request = %s %s", got.method, got.path)
			}
			want := map[string]any{"epic_ids": []any{float64(7), float64(3)}, "reason": "USER:2026-10-25 round"}
			if iniJSON(t, got.body) != iniJSON(t, want) {
				t.Fatalf("body = %s; want %s", iniJSON(t, got.body), iniJSON(t, want))
			}
		})
	}

	for _, tc := range []struct {
		name  string
		args  []string
		path  string
		query map[string]string
	}{
		{"review-bundle", []string{"initiative", "review-bundle", iniLine, "--epics", "3,7", "--phase", "plan-ready", "--json"}, round + "/review-bundle", map[string]string{"epic_ids": "3,7", "phase": "plan-ready"}},
		{"presentation", []string{"initiative", "presentation", iniLine, "--epics", "3,7", "--json"}, round + "/completion-presentation", map[string]string{"epic_ids": "3,7"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, seen := iniServer(t, iniOK(map[string]any{"members": []any{}}))
			cobraWorkspace(t, server, 1)
			out, err := runRoot(t, tc.args...)
			if err != nil {
				t.Fatalf("%v: %v\n%s", tc.args, err, out)
			}
			if len(*seen) != 1 {
				t.Fatalf("requests = %#v", *seen)
			}
			got := (*seen)[0]
			if got.method != "GET" || got.path != tc.path {
				t.Fatalf("request = %s %s; want GET %s", got.method, got.path, tc.path)
			}
			for key, value := range tc.query {
				if got.query[key] != value {
					t.Fatalf("query %s = %q; want %q (query %#v)", key, got.query[key], value, got.query)
				}
			}
		})
	}

	t.Run("check", func(t *testing.T) {
		sha := strings.Repeat("b", 40)
		server, seen := iniServer(t, iniOK(map[string]any{
			"phase": "implementation", "ready": false,
			"members": []any{
				map[string]any{"epic_id": 3, "ready": true, "blockers": []any{}},
				map[string]any{"epic_id": 7, "ready": false, "blockers": []any{map[string]any{"code": "plan_review_required", "message": "Record a plan review"}}},
			},
		}))
		cobraWorkspace(t, server, 1)
		out, err := runRoot(t, "initiative", "check", iniLine, "--epics", "3,7", "--phase", "implementation", "--git-sha", sha)
		if err == nil {
			t.Fatalf("check must exit nonzero when the round is not ready:\n%s", out)
		}
		if strings.Contains(err.Error(), "unknown command") {
			t.Fatalf("initiative check: %v", err)
		}
		if len(*seen) != 1 {
			t.Fatalf("requests = %#v", *seen)
		}
		got := (*seen)[0]
		if got.method != "POST" || got.path != round+"/gates/check" {
			t.Fatalf("request = %s %s", got.method, got.path)
		}
		if got.body["phase"] != "implementation" || !reflect.DeepEqual(got.body["epic_ids"], []any{float64(3), float64(7)}) ||
			got.body["git_sha"] != sha || got.body["git_sha_source"] != "explicit" {
			t.Fatalf("check body = %#v", got.body)
		}
		if _, ok := got.body["git_dirty"]; !ok {
			t.Fatalf("check body lacks git_dirty: %#v", got.body)
		}
	})

	t.Run("review from stdin", func(t *testing.T) {
		input := map[string]any{
			"phase":             "plan",
			"reviewer_context":  "independent-round-reviewer",
			"reason":            "Reviewed the round",
			"findings":          []any{},
			"scope_implemented": false,
			"members": []any{
				map[string]any{"epic_id": float64(7), "criteria": []any{"c7"}},
				map[string]any{"epic_id": float64(3), "criteria": []any{"c3"}},
			},
			"criteria":    []any{"c1"},
			"obligations": []any{"c1", "k1"},
		}
		server, seen := iniServer(t, iniOK(map[string]any{"reviews": []any{}}))
		cobraWorkspace(t, server, 1)
		rootCmd.SetIn(strings.NewReader(iniJSON(t, input)))
		defer rootCmd.SetIn(nil)
		out, err := runRoot(t, "initiative", "review", iniLine, "--file", "-")
		if err != nil {
			t.Fatalf("initiative review: %v\n%s", err, out)
		}
		if len(*seen) != 1 {
			t.Fatalf("requests = %#v", *seen)
		}
		got := (*seen)[0]
		if got.method != "POST" || got.path != round+"/phase-reviews" {
			t.Fatalf("request = %s %s", got.method, got.path)
		}
		if !reflect.DeepEqual(got.body, input) {
			t.Fatalf("review body changed:\n got %s\nwant %s", iniJSON(t, got.body), iniJSON(t, input))
		}
	})

	// Epic 345 implementation review (M2): --agent sends actor as the single
	// approval commands do.
	for _, action := range []string{"approve-implementation", "submit-review", "approve-completion"} {
		t.Run(action+" --agent", func(t *testing.T) {
			server, seen := iniServer(t, iniOK(map[string]any{"line_id": iniLine, "items": []any{}}))
			cobraWorkspace(t, server, 1)
			out, err := runRoot(t, "initiative", action, iniLine, "--epics", "7,3", "--reason", "USER:2026-10-25 round", "--agent", "claude-code")
			if err != nil {
				t.Fatalf("initiative %s --agent: %v\n%s", action, err, out)
			}
			if len(*seen) != 1 {
				t.Fatalf("requests = %#v", *seen)
			}
			want := map[string]any{
				"epic_ids": []any{float64(7), float64(3)}, "reason": "USER:2026-10-25 round",
				"actor": map[string]any{"kind": "agent", "agent_slug": "claude-code"},
			}
			if iniJSON(t, (*seen)[0].body) != iniJSON(t, want) {
				t.Fatalf("body = %s; want %s", iniJSON(t, (*seen)[0].body), iniJSON(t, want))
			}
		})
	}

	t.Run("blocked gate prints the Epic's report first", func(t *testing.T) {
		server, _ := iniServer(t, func(*http.Request) (int, any) {
			return 409, map[string]any{
				"error": map[string]any{
					"code": "epic_gate_blocked", "message": "Epic transition conditions are not satisfied",
					"gate": map[string]any{"epic_id": 7, "phase": "implementation", "ready": false,
						"blockers": []any{map[string]any{"code": "plan_review_required", "message": "Record a plan review"}}},
				},
				"index": 1,
				"item":  map[string]any{"epic_id": 7},
			}
		})
		cobraWorkspace(t, server, 1)
		out, err := runRoot(t, "initiative", "approve-implementation", iniLine, "--epics", "3,7", "--reason", "USER:2026-10-25 round")
		if err == nil {
			t.Fatalf("a blocked round succeeded:\n%s", out)
		}
		text := iniText(out, err)
		report, refusal := strings.Index(text, "plan_review_required"), strings.Index(text, "item 1 (Epic 7)")
		if report < 0 || refusal < 0 || report > refusal {
			t.Fatalf("the Epic's gate report must print before the refusal:\n%s", text)
		}
	})

	t.Run("refusal names the Epic", func(t *testing.T) {
		server, _ := iniServer(t, func(*http.Request) (int, any) {
			return 409, map[string]any{
				"error": map[string]any{"code": "epic_gate_blocked", "message": "Epic transition conditions are not satisfied"},
				"index": 1,
				"item":  map[string]any{"epic_id": 7},
			}
		})
		cobraWorkspace(t, server, 1)
		out, err := runRoot(t, "initiative", "approve-implementation", iniLine, "--epics", "3,7", "--reason", "USER:2026-10-25 round")
		if err == nil {
			t.Fatalf("a refused round succeeded:\n%s", out)
		}
		text := iniText(out, err)
		if !strings.Contains(text, "epic_gate_blocked") || !strings.Contains(text, "item 1 (Epic 7)") {
			t.Fatalf("refusal must name the code and item 1 (Epic 7):\n%s", text)
		}
	})

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"non-UUID Initiative id", []string{"initiative", "approve-implementation", "line-1", "--epics", "3,7", "--reason", "USER:2026-10-25"}},
		{"empty --epics", []string{"initiative", "approve-implementation", iniLine, "--epics", "", "--reason", "USER:2026-10-25"}},
		{"repeated id", []string{"initiative", "submit-review", iniLine, "--epics", "3,3", "--reason", "Verified"}},
		{"11 ids", []string{"initiative", "approve-completion", iniLine, "--epics", "1,2,3,4,5,6,7,8,9,10,11", "--reason", "USER:2026-10-25"}},
		{"non-UUID check id", []string{"initiative", "check", "line-1", "--epics", "3,7", "--phase", "implementation"}},
		{"non-UUID review-bundle id", []string{"initiative", "review-bundle", "line-1", "--epics", "3,7", "--phase", "plan-ready"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, seen := iniServer(t, iniOK(map[string]any{}))
			cobraWorkspace(t, server, 1)
			out, err := runRoot(t, tc.args...)
			if err == nil {
				t.Fatalf("%v accepted:\n%s", tc.args, out)
			}
			if strings.Contains(err.Error(), "unknown command") {
				t.Fatalf("%v: %v", tc.args, err)
			}
			if len(*seen) != 0 {
				t.Fatalf("%v sent %d requests before refusing", tc.args, len(*seen))
			}
		})
	}
}

func iniLines() []any {
	return []any{
		map[string]any{
			"id": iniLine, "text": "Pay in one step", "release_id": 7, "horizon": "now", "progress": "in_progress",
			"work": []any{map[string]any{"kind": "epic", "id": 345, "title": "Card payment Epic", "status": "IN_PROGRESS", "own": true}},
		},
		map[string]any{
			"id": iniOther, "text": "Refund by card", "release_id": nil, "horizon": "later", "progress": "shaped", "work": []any{},
		},
	}
}

// TestCase fa11707c: initiative list reads the bound System's Initiatives in
// one GET and prints id, text, release, horizon and progress; get prints one
// from the list or fails not found; a non-UUID id fails before any request.
func TestInitiativeListReadsTheBoundSystemsInitiativesInOneGETAndPrintsIdTextReleaseHorizonAndProgressGetPrintsOneFromTheListOrFailsNotFoundANonUUIDIdFailsBeforeAnyRequest(t *testing.T) {
	server, seen := iniServer(t, iniOK(iniLines()))
	cobraWorkspace(t, server, 1)

	out, err := runRoot(t, "initiative", "list")
	if err != nil {
		t.Fatalf("initiative list: %v\n%s", err, out)
	}
	if len(*seen) != 1 || (*seen)[0].method != "GET" || (*seen)[0].path != "/api/roadmap/lines" || (*seen)[0].query["system_id"] != "1" {
		t.Fatalf("list requests = %#v", *seen)
	}
	for _, want := range []string{iniLine, "Pay in one step", "7", "now", "in_progress", iniOther, "Refund by card", "later", "shaped"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list output lacks %q:\n%s", want, out)
		}
	}

	out, err = runRoot(t, "initiative", "list", "--json")
	if err != nil {
		t.Fatalf("initiative list --json: %v\n%s", err, out)
	}
	var printed []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &printed); err != nil || len(printed) != 2 || printed[0]["id"] != iniLine {
		t.Fatalf("list --json must print the served array: err=%v\n%s", err, out)
	}

	before := len(*seen)
	out, err = runRoot(t, "initiative", "get", iniLine)
	if err != nil {
		t.Fatalf("initiative get: %v\n%s", err, out)
	}
	if len(*seen) != before+1 || (*seen)[before].path != "/api/roadmap/lines" || (*seen)[before].query["system_id"] != "1" {
		t.Fatalf("get must read the list once: %#v", (*seen)[before:])
	}
	if !strings.Contains(out, "Pay in one step") || !strings.Contains(out, "Card payment Epic") || strings.Contains(out, "Refund by card") {
		t.Fatalf("get must print that Initiative with its work only:\n%s", out)
	}

	missing := "77777777-7777-4777-8777-777777777777"
	out, err = runRoot(t, "initiative", "get", missing)
	if err == nil || !strings.Contains(iniText(out, err), "Initiative "+missing+" not found in System 1") {
		t.Fatalf("get of an absent id: err=%v\n%s", err, out)
	}

	before = len(*seen)
	out, err = runRoot(t, "initiative", "get", "line-1")
	if err == nil || len(*seen) != before {
		t.Fatalf("a non-UUID id must fail before any request: err=%v requests=%d\n%s", err, len(*seen)-before, out)
	}
}

// TestCase 3a86fbc6: initiative create sends the file's allowed fields with
// system_id in one POST and refuses any other field before loading.
func TestInitiativeCreateSendsTheFilesAllowedFieldsWithSystemIdInOnePOSTAndRefusesAnyOtherFieldBeforeLoading(t *testing.T) {
	created := map[string]any{"id": iniLine, "text": "Pay in one step", "horizon": "now", "release_id": 7, "progress": "shaped", "work": []any{}}
	server, seen := iniServer(t, func(*http.Request) (int, any) { return 201, map[string]any{"data": created} })
	cobraWorkspace(t, server, 1)

	file := writeAccFile(t, "initiative.json", `{"text":"Pay in one step","horizon":"now","release_id":7,"goal_label":"Payments","line_type":"feature","source":"USER:2026-10-25","notes":"One step"}`)
	out, err := runRoot(t, "initiative", "create", "--file", file)
	if err != nil {
		t.Fatalf("initiative create: %v\n%s", err, out)
	}
	if len(*seen) != 1 || (*seen)[0].method != "POST" || (*seen)[0].path != "/api/roadmap/lines" {
		t.Fatalf("create requests = %#v", *seen)
	}
	want := map[string]any{
		"system_id": float64(1), "text": "Pay in one step", "horizon": "now", "release_id": float64(7),
		"goal_label": "Payments", "line_type": "feature", "source": "USER:2026-10-25", "notes": "One step",
	}
	if !reflect.DeepEqual((*seen)[0].body, want) {
		t.Fatalf("create body = %s; want %s", iniJSON(t, (*seen)[0].body), iniJSON(t, want))
	}
	if !strings.Contains(out, iniLine) {
		t.Fatalf("create must print the created Initiative:\n%s", out)
	}

	for _, extra := range []string{`"epic_id":345`, `"system_id":2`, `"id":"` + iniOther + `"`, `"progress":"done"`} {
		before := len(*seen)
		bad := writeAccFile(t, "bad.json", `{"text":"Pay","horizon":"now",`+extra+`}`)
		out, err := runRoot(t, "initiative", "create", "--file", bad)
		if err == nil || strings.Contains(err.Error(), "unknown command") {
			t.Fatalf("create with %s: err=%v\n%s", extra, err, out)
		}
		if len(*seen) != before {
			t.Fatalf("create with %s sent a request before refusing", extra)
		}
	}
}

// TestCase 6e55cfc5: initiative link and unlink send the epic or requirement
// body with system_id, refuse both or neither flag, and print
// already_on_roadmap with its ids, not_found and field errors.
func TestInitiativeLinkAndUnlinkSendTheEpicOrRequirementBodyWithSystemIdRefuseBothOrNeitherFlagAndPrintAlreadyOnRoadmapWithItsIdsNotFoundAndFieldErrors(t *testing.T) {
	line := map[string]any{"id": iniLine, "text": "Pay in one step", "work": []any{}}
	for _, tc := range []struct {
		name         string
		args         []string
		method, path string
		body         map[string]any
	}{
		{"link epic", []string{"initiative", "link", iniLine, "--epic", "345"}, "POST", "/api/roadmap/lines/" + iniLine + "/link",
			map[string]any{"system_id": float64(1), "epic_id": float64(345)}},
		{"link requirement", []string{"initiative", "link", iniLine, "--requirement", iniReq}, "POST", "/api/roadmap/lines/" + iniLine + "/link",
			map[string]any{"system_id": float64(1), "requirement_id": iniReq, "requirement_kind": "user_requirement"}},
		{"link system requirement", []string{"initiative", "link", iniLine, "--requirement", iniReq, "--requirement-kind", "system_requirement"}, "POST", "/api/roadmap/lines/" + iniLine + "/link",
			map[string]any{"system_id": float64(1), "requirement_id": iniReq, "requirement_kind": "system_requirement"}},
		{"unlink epic", []string{"initiative", "unlink", iniLine, "--epic", "345"}, "DELETE", "/api/roadmap/lines/" + iniLine + "/links",
			map[string]any{"system_id": float64(1), "kind": "epic", "id": "345"}},
		{"unlink user requirement", []string{"initiative", "unlink", iniLine, "--requirement", iniReq}, "DELETE", "/api/roadmap/lines/" + iniLine + "/links",
			map[string]any{"system_id": float64(1), "kind": "user_requirement", "id": iniReq}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, seen := iniServer(t, iniOK(line))
			cobraWorkspace(t, server, 1)
			out, err := runRoot(t, tc.args...)
			if err != nil {
				t.Fatalf("%v: %v\n%s", tc.args, err, out)
			}
			if len(*seen) != 1 || (*seen)[0].method != tc.method || (*seen)[0].path != tc.path {
				t.Fatalf("requests = %#v; want %s %s", *seen, tc.method, tc.path)
			}
			if !reflect.DeepEqual((*seen)[0].body, tc.body) {
				t.Fatalf("body = %s; want %s", iniJSON(t, (*seen)[0].body), iniJSON(t, tc.body))
			}
		})
	}

	for _, args := range [][]string{
		{"initiative", "link", iniLine, "--epic", "345", "--requirement", iniReq},
		{"initiative", "link", iniLine},
		{"initiative", "unlink", iniLine, "--epic", "345", "--requirement", iniReq},
		{"initiative", "unlink", iniLine},
		// The server unlinks only an Epic or a user requirement, so unlink takes no --requirement-kind.
		{"initiative", "unlink", iniLine, "--requirement", iniReq, "--requirement-kind", "system_requirement"},
		{"initiative", "link", "line-1", "--epic", "345"},
	} {
		t.Run("refused "+strings.Join(args[1:], " "), func(t *testing.T) {
			server, seen := iniServer(t, iniOK(line))
			cobraWorkspace(t, server, 1)
			out, err := runRoot(t, args...)
			if err == nil || strings.Contains(err.Error(), "unknown command") {
				t.Fatalf("%v: err=%v\n%s", args, err, out)
			}
			if len(*seen) != 0 {
				t.Fatalf("%v sent %d requests before refusing", args, len(*seen))
			}
		})
	}

	for _, tc := range []struct {
		name   string
		status int
		body   map[string]any
		check  func(text string) bool
	}{
		{"already_on_roadmap", 409, map[string]any{"error": "already_on_roadmap", "ids": []any{345}}, func(text string) bool {
			for _, l := range strings.Split(text, "\n") {
				if strings.Contains(l, "already_on_roadmap") && strings.Contains(l, "345") {
					return true
				}
			}
			return false
		}},
		{"not_found", 404, map[string]any{"error": "not_found"}, func(text string) bool { return strings.Contains(text, "not_found") }},
		{"field errors", 422, map[string]any{"errors": map[string]any{"text": []any{"can't be blank"}}}, func(text string) bool {
			return strings.Contains(text, "text: can't be blank")
		}},
	} {
		t.Run("prints "+tc.name, func(t *testing.T) {
			server, _ := iniServer(t, func(*http.Request) (int, any) { return tc.status, tc.body })
			cobraWorkspace(t, server, 1)
			out, err := runRoot(t, "initiative", "link", iniLine, "--epic", "345")
			if err == nil {
				t.Fatalf("a %d refusal succeeded:\n%s", tc.status, out)
			}
			if text := iniText(out, err); strings.Contains(text, "unknown command") || !tc.check(text) {
				t.Fatalf("%s refusal printed:\n%s", tc.name, text)
			}
		})
	}
}
