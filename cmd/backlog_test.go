package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBacklogUpdate(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.Method != "PATCH" || r.URL.Path != "/api/v1/backlog/"+id || body["system_id"] != float64(4) || body["disposition"] != "CLOSED by PR:972" {
			t.Errorf("update = %s %s %#v", r.Method, r.URL, body)
		}
		body["id"] = id
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"backlog": body}})
	}))
	defer server.Close()
	cmd := newBacklogCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	cmd.SetArgs([]string{"update", id, "--file", "-", "--json"})
	cmd.SetIn(strings.NewReader(`{"disposition":"CLOSED by PR:972","disposition_ref":"PR:972","notes_md":"Verified fix"}`))
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "CLOSED by PR:972") || !strings.Contains(out.String(), "Verified fix") {
		t.Fatal(out.String())
	}
}

func TestBacklogUpdateRejectsInvalidInputBeforeLoading(t *testing.T) {
	for _, input := range []string{`{}`, `{"title":"new"}`, `{"system_id":4}`, `{"raised_by":"fake"}`, `{"metadata":{}}`, `{"notes_md":null}`, `{"disposition":"DONE"}`, `{"disposition":"CLOSED by "}`} {
		cmd := newBacklogCommand(func() (*cliEnv, error) { t.Fatal("invalid input loaded workspace"); return nil, nil })
		cmd.SetArgs([]string{"update", "11111111-1111-4111-8111-111111111111", "--file", "-"})
		cmd.SetIn(strings.NewReader(input))
		if err := cmd.Execute(); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestBacklogUpdateRefusalsAndReceiptIdentity(t *testing.T) {
	for _, tc := range []struct {
		status     int
		body, want string
	}{
		{404, `{"error":{"message":"backlog record not found"}}`, "not found"},
		{422, `{"error":{"message":"unsupported Backlog update field"}}`, "unsupported"},
		{200, `{"data":{"backlog":{"id":"11111111-1111-4111-8111-111111111111","system_id":99}}}`, "different System"},
		{200, `{"data":{"backlog":{"id":"22222222-2222-4222-8222-222222222222","system_id":4}}}`, "different record identity"},
		{200, `{"data":{"backlog":[]}}`, "missing its record"},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		cmd := newBacklogCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
		cmd.SetArgs([]string{"update", "11111111-1111-4111-8111-111111111111", "--file", "-"})
		cmd.SetIn(strings.NewReader(`{"notes_md":"evidence"}`))
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("error = %v; want %s", err, tc.want)
		}
		server.Close()
	}
	cmd := newBacklogCommand(func() (*cliEnv, error) { t.Fatal("bad UUID loaded workspace"); return nil, nil })
	cmd.SetArgs([]string{"update", "bad-id", "--file", "-"})
	cmd.SetIn(strings.NewReader(`{"notes_md":"evidence"}`))
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "UUID") {
		t.Fatal(err)
	}
}

func TestBacklogCreateAndReadContracts(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	for _, operation := range []string{"create", "list", "get"} {
		t.Run(operation, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path := "/api/v1/backlog"
				if operation == "get" {
					path += "/" + id
				}
				if r.URL.Path != path {
					t.Errorf("path = %s", r.URL.Path)
				}
				row := map[string]any{"id": id, "system_id": 4, "title": "Mock discovery", "kind": "backlog", "disposition": "OPEN", "observation": "Full content"}
				if operation == "create" {
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					if r.Method != "POST" || body["system_id"] != float64(4) || body["title"] != "Mock discovery" {
						t.Errorf("request = %s %#v", r.Method, body)
					}
					w.WriteHeader(201)
				} else if r.Method != "GET" || r.URL.Query().Get("system_id") != "4" {
					t.Errorf("read = %s %s", r.Method, r.URL)
				}
				var result any = row
				if operation == "list" {
					result = []any{row}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"backlog": result}})
			}))
			defer server.Close()
			cmd := newBacklogCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
			args := []string{operation, "--json"}
			if operation == "create" {
				args = append(args, "--file", "-")
			}
			if operation == "get" {
				args = append(args, id, "--full")
			}
			cmd.SetArgs(args)
			cmd.SetIn(strings.NewReader(`{"title":"Mock discovery"}`))
			var out bytes.Buffer
			cmd.SetOut(&out)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), id) {
				t.Fatal(out.String())
			}
			if operation == "get" && !strings.Contains(out.String(), "Full content") {
				t.Fatal(out.String())
			}
		})
	}
}

func TestBacklogRejectsInvalidCreateBeforeLoading(t *testing.T) {
	for _, input := range []string{`{}`, `{"title":" "}`, `{"title":"x","tenant_id":2}`, `{"title":"x","raised_by":"fake"}`, `{"title":"x","disposition":"OPEN"}`, `{"title":"x","metadata":{"raised_by_user_id":2}}`, `{"title":"x","kind":"gap"}`, `{"title":"x","kind":"wrong"}`, `{"title":"x","observation":42}`} {
		cmd := newBacklogCommand(func() (*cliEnv, error) { t.Fatal("invalid content loaded workspace"); return nil, nil })
		cmd.SetArgs([]string{"create", "--file", "-"})
		cmd.SetIn(strings.NewReader(input))
		if err := cmd.Execute(); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestBacklogReportsRefusalsAndRejectsUnscopedReceipts(t *testing.T) {
	for _, scenario := range []struct {
		status     int
		body, want string
	}{
		{422, `{"error":{"message":"denied"}}`, "Backlog request refused"},
		{201, `{"data":{"backlog":{"id":"11111111-1111-4111-8111-111111111111","system_id":99,"disposition":"OPEN"}}}`, "different System"},
		{201, `{"data":{"backlog":{"system_id":4}}}`, "invalid native identity"},
		{201, `{}`, "missing its data"},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(scenario.status)
			_, _ = w.Write([]byte(scenario.body))
		}))
		cmd := newBacklogCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
		cmd.SetArgs([]string{"create", "--file", "-"})
		cmd.SetIn(strings.NewReader(`{"title":"x"}`))
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), scenario.want) {
			t.Errorf("error = %v; want %s", err, scenario.want)
		}
		server.Close()
	}
}

// Epic 288 (B1): `backlog list` lists what it can and names what it skipped.
// One row the server sent with a foreign System, a non-UUID id or no object at
// all aborted the whole listing, so one bad record hid every good one.
func TestBacklogListSkipsAnInvalidRowAndNamesIt(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	const foreign = "22222222-2222-4222-8222-222222222222"
	rows := []any{
		map[string]any{"id": id, "system_id": 4, "title": "Mock discovery", "kind": "backlog", "disposition": "OPEN"},
		map[string]any{"id": foreign, "system_id": 99, "title": "other System"},
		map[string]any{"id": "BL-1", "system_id": 4, "title": "legacy identity"},
		"text",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"backlog": rows}})
	}))
	defer server.Close()

	run := func(args ...string) (string, string, error) {
		cmd := newBacklogCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
		cmd.SetArgs(args)
		var out bytes.Buffer
		cmd.SetOut(&out)
		var stderr []byte
		var err error
		captureStderr(t, func() { err = cmd.Execute() }, &stderr)
		return out.String(), string(stderr), err
	}

	out, stderr, err := run("list", "--json")
	if err != nil {
		t.Fatalf("list must keep going past an invalid row, got: %v", err)
	}
	var page struct {
		Backlog []map[string]any `json:"backlog"`
		Total   int              `json:"total"`
		Skipped []struct {
			ID     string `json:"id"`
			Reason string `json:"reason"`
		} `json:"skipped"`
	}
	if err := json.Unmarshal([]byte(out), &page); err != nil {
		t.Fatalf("list --json must be JSON: %v\n%s", err, out)
	}
	if len(page.Backlog) != 1 || page.Backlog[0]["id"] != id || page.Total != 1 {
		t.Fatalf("the valid row alone is listed and counted, got total %d:\n%s", page.Total, out)
	}
	if len(page.Skipped) != 3 {
		t.Fatalf("every skipped row is named under skipped, got %d:\n%s", len(page.Skipped), out)
	}
	for i, want := range []struct{ id, reason string }{
		{foreign, "different System"},
		{"BL-1", "invalid native identity"},
		{"?", "not an object"},
	} {
		if page.Skipped[i].ID != want.id || !strings.Contains(page.Skipped[i].Reason, want.reason) {
			t.Errorf("skipped[%d] = %+v, want id %q with reason %q", i, page.Skipped[i], want.id, want.reason)
		}
	}
	for _, want := range []string{foreign, "BL-1", "different System", "invalid native identity", "not an object"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("a skipped row is named on stderr; %q missing in:\n%s", want, stderr)
		}
	}

	out, _, err = run("list")
	if err != nil {
		t.Fatalf("text list must keep going past an invalid row, got: %v", err)
	}
	if !strings.Contains(out, id) || strings.Contains(out, foreign) {
		t.Fatalf("text list prints the valid row and not the skipped one:\n%s", out)
	}

	// Every row invalid: an empty page at exit 0, each row named on stderr.
	rows = rows[1:]
	out, stderr, err = run("list", "--json")
	if err != nil {
		t.Fatalf("a list of only invalid rows is an empty page, not a failure: %v", err)
	}
	page.Backlog, page.Skipped = nil, nil
	if err := json.Unmarshal([]byte(out), &page); err != nil {
		t.Fatalf("list --json must be JSON: %v\n%s", err, out)
	}
	if len(page.Backlog) != 0 || page.Total != 0 || len(page.Skipped) != 3 {
		t.Errorf("want an empty page with three skipped rows, got:\n%s", out)
	}
	if !strings.Contains(stderr, foreign) || !strings.Contains(stderr, "BL-1") || !strings.Contains(stderr, "not an object") {
		t.Errorf("every skipped row is named on stderr:\n%s", stderr)
	}
	if out, _, err := run("list"); err != nil || strings.Contains(out, foreign) {
		t.Errorf("the text list of only invalid rows is empty at exit 0: %v\n%s", err, out)
	}
}

// Epic 288 (B2): a text row with a null disposition printed `%!s(<nil>)`, and
// a multi-line title broke the one-row-per-record shape.
func TestBacklogListTextRendersAnEmptyDisposition(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		row := map[string]any{"id": id, "system_id": 4, "disposition": nil, "title": "a\n  b"}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"backlog": []any{row}}})
	}))
	defer server.Close()
	cmd := newBacklogCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	cmd.SetArgs([]string{"list"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "%!") {
		t.Fatalf("a null column must render as a dash, not a format error:\n%s", out.String())
	}
	if want := id + "  —  —  a b\n"; !strings.Contains(out.String(), want) {
		t.Fatalf("want the row %q in:\n%s", want, out.String())
	}
}

// Epic 298 (SR-3813, TC-1398): list rows, get text and the update echo name a
// routing or closure target by the title the server serves on the link, as
// `ROUTED to 288 — <title>`; a row without a titled link and JSON output stay
// as served.
func TestBacklogListPrintsTheTargetTitle(t *testing.T) {
	routedID := "11111111-1111-4111-8111-111111111111"
	closedID := "22222222-2222-4222-8222-222222222222"
	routed := map[string]any{"id": routedID, "system_id": 4, "kind": "backlog", "title": "Routed discovery", "disposition": "ROUTED to 288",
		"links": []any{map[string]any{"role": "routed_to", "type": "epic", "id": 288, "title": "Keep backlog routing truthful", "display_ref": nil, "available": true}}}
	closed := map[string]any{"id": closedID, "system_id": 4, "kind": "tooling", "title": "Closed by text", "disposition": "CLOSED by PR:972", "links": []any{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var result any
		switch {
		case r.Method == "PATCH" && r.URL.Path == "/api/v1/backlog/"+routedID:
			result = routed
		case r.Method == "GET" && r.URL.Path == "/api/v1/backlog/"+routedID:
			result = routed
		case r.Method == "GET" && r.URL.Path == "/api/v1/backlog":
			result = []any{routed, closed}
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"backlog": result}})
	}))
	defer server.Close()
	run := func(args ...string) string {
		t.Helper()
		cmd := newBacklogCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
		cmd.SetArgs(args)
		cmd.SetIn(strings.NewReader(`{"disposition":"ROUTED to 288","disposition_ref":"USER:2026-10-22:plan-save"}`))
		var out bytes.Buffer
		cmd.SetOut(&out)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	titled := "ROUTED to 288 — Keep backlog routing truthful"
	list := run("list")
	if !strings.Contains(list, routedID+"  backlog  "+titled+"  Routed discovery\n") {
		t.Fatalf("list row lacks the target title: %q", list)
	}
	if !strings.Contains(list, closedID+"  tooling  CLOSED by PR:972  Closed by text\n") {
		t.Fatalf("a row without a titled link changed: %q", list)
	}
	if got := run("get", routedID); !strings.Contains(got, "disposition: "+titled+"\n") {
		t.Fatalf("get text lacks the target title: %q", got)
	}
	if echo := run("update", routedID, "--file", "-"); !strings.Contains(echo, "disposition: "+titled+"\n") {
		t.Fatalf("update echo lacks the target title: %q", echo)
	}
	for _, args := range [][]string{{"get", routedID, "--full", "--json"}, {"list", "--json"}, {"update", routedID, "--file", "-", "--json"}} {
		out := run(args...)
		if strings.Contains(out, "—") || !strings.Contains(out, "ROUTED to 288") || !strings.Contains(out, "Keep backlog routing truthful") {
			t.Fatalf("JSON output must serve the disposition and the link as stored: %q", out)
		}
	}
}
