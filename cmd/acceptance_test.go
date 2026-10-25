package cmd

// Epic 318: modernpath acceptance — release acceptance specs and reports
// through the bound System. Release acceptance is not RDD evidence. The tests
// drive the root command, so a missing command is an "unknown command"
// assertion rather than a build error.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const (
	accLine   = "11111111-1111-4111-8111-111111111111"
	accSpec   = "22222222-2222-4222-8222-222222222222"
	accReport = "33333333-3333-4333-8333-333333333333"
)

type accRequest struct {
	method, path, systemQuery string
	body                      map[string]any
}

// accServer answers the reachability probe and records every other request,
// replying with status and a {data: reply} body.
func accServer(t *testing.T, status int, reply any) (*httptest.Server, *[]accRequest) {
	t.Helper()
	seen := []accRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		seen = append(seen, accRequest{method: r.Method, path: r.URL.Path, systemQuery: r.URL.Query().Get("system_id"), body: body})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status >= 300 {
			_ = json.NewEncoder(w).Encode(reply)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": reply})
	}))
	t.Cleanup(server.Close)
	return server, &seen
}

func writeAccFile(t *testing.T, name, content string) string {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestAcceptanceCommandsSendBoundSystemAndPrintJSON(t *testing.T) {
	spec := map[string]any{"id": accSpec, "line_id": accLine, "title": "Pay by card", "revision": 1}
	report := map[string]any{"id": accReport, "line_id": accLine, "status": "draft", "tester_name": "Tess Tester"}
	for _, tc := range []struct {
		name         string
		args         []string
		file         string
		reply        any
		method, path string
		read         bool
		body         map[string]any
		want         string
	}{
		{name: "specs list", args: []string{"acceptance", "specs", "list", "--line", accLine, "--json"}, reply: []any{spec},
			method: "GET", path: "/api/roadmap/lines/" + accLine + "/acceptance-specs", read: true, want: "Pay by card"},
		{name: "specs create", args: []string{"acceptance", "specs", "create", "--line", accLine, "--json"}, file: `{"title":"Pay by card","steps_md":"1. Pay","expected_md":"Paid"}`, reply: spec,
			method: "POST", path: "/api/roadmap/lines/" + accLine + "/acceptance-specs", body: map[string]any{"title": "Pay by card", "steps_md": "1. Pay", "expected_md": "Paid"}, want: accSpec},
		{name: "specs update", args: []string{"acceptance", "specs", "update", accSpec, "--json"}, file: `{"steps_md":"1. Pay twice"}`, reply: spec,
			method: "PATCH", path: "/api/roadmap/acceptance-specs/" + accSpec, body: map[string]any{"steps_md": "1. Pay twice"}, want: accSpec},
		{name: "specs retire", args: []string{"acceptance", "specs", "retire", accSpec, "--json"}, reply: spec,
			method: "POST", path: "/api/roadmap/acceptance-specs/" + accSpec + "/retire", body: map[string]any{}, want: accSpec},
		{name: "report open", args: []string{"acceptance", "report", "open", "--line", accLine, "--json"}, file: `{"release_id":7,"environment":"staging","build_ref":"abc123"}`, reply: report,
			method: "POST", path: "/api/roadmap/lines/" + accLine + "/acceptance-reports", body: map[string]any{"release_id": float64(7), "environment": "staging", "build_ref": "abc123"}, want: "Tess Tester"},
		{name: "report get", args: []string{"acceptance", "report", "get", accReport, "--json"}, reply: report,
			method: "GET", path: "/api/roadmap/acceptance-reports/" + accReport, read: true, want: accReport},
		{name: "report record", args: []string{"acceptance", "report", "record", accReport, "--spec", accSpec, "--json"}, file: `{"outcome":"fail","notes_md":"Declined"}`, reply: map[string]any{"id": "r1", "spec_id": accSpec, "outcome": "fail"},
			method: "PUT", path: "/api/roadmap/acceptance-reports/" + accReport + "/results/" + accSpec, body: map[string]any{"outcome": "fail", "notes_md": "Declined"}, want: `"outcome": "fail"`},
		{name: "report sign-off", args: []string{"acceptance", "report", "sign-off", accReport, "--note", "One known issue", "--source", "USER:2026-10-25", "--json"}, reply: map[string]any{"id": accReport, "status": "signed_off"},
			method: "POST", path: "/api/roadmap/acceptance-reports/" + accReport + "/sign-off", body: map[string]any{"sign_off_note": "One known issue", "source": "USER:2026-10-25"}, want: "signed_off"},
		{name: "report reject", args: []string{"acceptance", "report", "reject", accReport, "--note", "Wrong build", "--source", "USER:2026-10-25:reject", "--json"}, reply: map[string]any{"id": accReport, "status": "rejected"},
			method: "POST", path: "/api/roadmap/acceptance-reports/" + accReport + "/reject", body: map[string]any{"sign_off_note": "Wrong build", "source": "USER:2026-10-25:reject"}, want: "rejected"},
		{name: "release", args: []string{"acceptance", "release", "7", "--json"}, reply: []any{map[string]any{"line_id": accLine, "text": "Pay in one step", "report_id": nil, "status": "none"}},
			method: "GET", path: "/api/compliance/releases/7/acceptance", read: true, want: "Pay in one step"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, seen := accServer(t, 200, tc.reply)
			cobraWorkspace(t, server, 1)
			args := tc.args
			if tc.file != "" {
				args = append(append([]string{}, args...), "--file", writeAccFile(t, "input.json", tc.file))
			}
			out, err := runRoot(t, args...)
			if err != nil {
				t.Fatalf("%v: %v\n%s", args, err, out)
			}
			if len(*seen) != 1 {
				t.Fatalf("requests = %#v", *seen)
			}
			got := (*seen)[0]
			if got.method != tc.method || got.path != tc.path {
				t.Fatalf("request = %s %s; want %s %s", got.method, got.path, tc.method, tc.path)
			}
			if tc.read {
				if got.systemQuery != "1" {
					t.Fatalf("read system_id = %q", got.systemQuery)
				}
			} else {
				want := map[string]any{"system_id": float64(1)}
				for key, value := range tc.body {
					want[key] = value
				}
				gotBlob, _ := json.Marshal(got.body)
				wantBlob, _ := json.Marshal(want)
				if string(gotBlob) != string(wantBlob) {
					t.Fatalf("body = %s; want %s", gotBlob, wantBlob)
				}
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("output lacks %q:\n%s", tc.want, out)
			}
		})
	}
}

func TestAcceptanceRefusesBadInputBeforeLoading(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		file string
	}{
		{"unknown spec field", []string{"acceptance", "specs", "create", "--line", accLine}, `{"title":"T","colour":"red"}`},
		{"server-set spec field", []string{"acceptance", "specs", "create", "--line", accLine}, `{"title":"T","revision":3}`},
		{"spec without title", []string{"acceptance", "specs", "create", "--line", accLine}, `{"steps_md":"1."}`},
		{"server-set update field", []string{"acceptance", "specs", "update", accSpec}, `{"title":"T","line_id":"x"}`},
		{"empty update", []string{"acceptance", "specs", "update", accSpec}, `{}`},
		{"server-set report field", []string{"acceptance", "report", "open", "--line", accLine}, `{"tester_id":4}`},
		{"server-set status", []string{"acceptance", "report", "open", "--line", accLine}, `{"status":"signed_off"}`},
		{"server-set result field", []string{"acceptance", "report", "record", accReport, "--spec", accSpec}, `{"outcome":"pass","spec_revision":2}`},
		{"bad outcome", []string{"acceptance", "report", "record", accReport, "--spec", accSpec}, `{"outcome":"great"}`},
		{"bad line id", []string{"acceptance", "specs", "list", "--line", "line-1"}, ""},
		{"bad report id", []string{"acceptance", "report", "get", "report-1"}, ""},
		{"bad release id", []string{"acceptance", "release", "seven"}, ""},
		{"sign-off without source", []string{"acceptance", "report", "sign-off", accReport}, ""},
		{"sign-off with an agent source", []string{"acceptance", "report", "sign-off", accReport, "--source", "AGENT:claude"}, ""},
		{"sign-off with an empty USER source", []string{"acceptance", "report", "sign-off", accReport, "--source", "USER:"}, ""},
		{"reject without source", []string{"acceptance", "report", "reject", accReport, "--note", "Wrong build"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				healthyProbe(w, r)
			}))
			defer server.Close()
			cobraWorkspace(t, server, 1)
			args := tc.args
			if tc.file != "" {
				args = append(append([]string{}, args...), "--file", writeAccFile(t, "input.json", tc.file))
			}
			out, err := runRoot(t, args...)
			if err == nil {
				t.Fatalf("%v accepted:\n%s", args, out)
			}
			if strings.Contains(err.Error(), "unknown command") {
				t.Fatalf("%v: %v", args, err)
			}
			if requests != 0 {
				t.Fatalf("%v reached the server %d times before refusing: %v", args, requests, err)
			}
		})
	}
}

func TestAcceptanceNamesServerCodes(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
		args   []string
	}{
		{404, "not_found", []string{"acceptance", "report", "get", accReport}},
		{404, "not_found", []string{"acceptance", "release", "7"}},
		{409, "report_closed", []string{"acceptance", "report", "sign-off", accReport, "--source", "USER:2026-10-25"}},
		{409, "report_closed", []string{"acceptance", "report", "reject", accReport, "--note", "Late", "--source", "USER:2026-10-25"}},
		{422, "results_missing", []string{"acceptance", "report", "sign-off", accReport, "--source", "USER:2026-10-25"}},
	} {
		t.Run(tc.code+" "+tc.args[1], func(t *testing.T) {
			server, seen := accServer(t, tc.status, map[string]any{"error": tc.code})
			cobraWorkspace(t, server, 1)
			_, err := runRoot(t, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.code) {
				t.Fatalf("%v: error = %v; want it to name %s", tc.args, err, tc.code)
			}
			if len(*seen) != 1 {
				t.Fatalf("requests = %#v", *seen)
			}
		})
	}
}
