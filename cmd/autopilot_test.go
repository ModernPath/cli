package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const grantTestID = "11111111-1111-4111-8111-111111111111"
const grantTestInput = `{"epic_ids":[7],"expires_at":"2099-11-17T13:00:00Z","exclusions":["billing"],"source":"USER: fixture only"}`

func TestAutopilotCreatesExplicitGrant(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		if r.Method != "POST" || r.URL.Path != "/api/work/autopilot-grants" || r.Header.Get("Authorization") == "" || b["system_id"] != float64(4) || b["source"] != "USER: fixture only" {
			t.Errorf("create %s %s %#v", r.Method, r.URL, b)
		}
		b["id"] = grantTestID
		b["granted_by_id"] = 5
		b["inserted_at"] = "2026-11-17T12:00:00Z"
		_ = json.NewEncoder(w).Encode(map[string]any{"data": b})
	}))
	defer s.Close()
	cmd := newAutopilotCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, s, 4), nil })
	cmd.SetArgs([]string{"create", "--file", "-", "--json"})
	cmd.SetIn(strings.NewReader(grantTestInput))
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["id"] != grantTestID || result["granted_by_id"] != float64(5) || !strings.Contains(out.String(), "billing") {
		t.Fatal(out.String())
	}
}

func TestAutopilotOnlyExposesCreation(t *testing.T) {
	cmd := newAutopilotCommand(func() (*cliEnv, error) { t.Fatal("unexpected credential load"); return nil, nil })
	children := cmd.Commands()
	if len(children) != 1 || children[0].Name() != "create" {
		t.Fatalf("expected only create, got %v", children)
	}
}

func TestAutopilotRejectsInvalidTermsBeforeCredentials(t *testing.T) {
	for _, input := range []string{`{}`, `{"granted_by_id":5}`, `{"epic_ids":[],"expires_at":"2099-11-17T13:00:00Z","exclusions":[],"source":"USER: fixture"}`, `{"epic_ids":[7],"expires_at":"tomorrow","exclusions":[],"source":"USER: fixture"}`} {
		cmd := newAutopilotCommand(func() (*cliEnv, error) { t.Fatal("invalid input loaded credentials"); return nil, nil })
		cmd.SetArgs([]string{"create", "--file", "-"})
		cmd.SetIn(strings.NewReader(input))
		if err := cmd.Execute(); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestAutopilotRefusalsAreNonzero(t *testing.T) {
	for _, code := range []string{"invalid_grant", "not_found", "forbidden"} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(403)
			fmt.Fprintf(w, `{"error":{"code":%q,"message":"stop"}}`, code)
		}))
		cmd := newAutopilotCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, s, 4), nil })
		cmd.SetArgs([]string{"create", "--file", "-", "--json"})
		cmd.SetIn(strings.NewReader(grantTestInput))
		err := cmd.Execute()
		s.Close()
		if err == nil || !strings.Contains(err.Error(), code) {
			t.Fatalf("%s: %v", code, err)
		}
	}
}
