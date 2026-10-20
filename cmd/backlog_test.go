package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
