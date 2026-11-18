package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func executeChat(t *testing.T, env *cliEnv, args ...string) (string, error) {
	t.Helper()
	c := newChatsCommand(func() (*cliEnv, error) { return env, nil })
	c.SetArgs(args)
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	err := c.Execute()
	return out.String(), err
}
func chatFixture() map[string]any {
	return map[string]any{"id": 7, "title": "Planning context", "context": map[string]any{"items": []any{map[string]any{"kind": "epic", "target_ref": "364"}}}, "ai_turn_status": "failed", "ai_turn_started_at": "2026-11-17T10:00:00Z", "ai_turn_failure_reason": "Provider unavailable", "ai_turn_activity": map[string]any{"turn_id": "turn-fixture", "completed_tool_calls": 2}, "turns": []any{map[string]any{"id": 1, "role": "user", "content": "Finish planning\nPreserve the owner's decision", "author": map[string]any{"id": 5, "name": "Planner"}, "attachments": []any{map[string]any{"id": "image-1", "filename": "scope.png"}}}, map[string]any{"id": 2, "role": "assistant", "content": strings.Repeat("full transcript λ\n", 9000), "model_used": "server-model", "tool_name": nil}}}
}

// TestCase 9340bd13-1541-4dd2-9ecc-23487a05ad93
func TestChatsReadPreservesFullServerDataAndOnlyUsesGET(t *testing.T) {
	chat := chatFixture()
	for _, op := range []string{"list", "get"} {
		t.Run(op, func(t *testing.T) {
			requests := 0
			var env *cliEnv
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				path := "/api/chats/7"
				if op == "list" {
					path = "/api/chats"
				}
				if r.Method != "GET" || r.URL.Path != path || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer "+env.token {
					t.Errorf("unexpected Chat read %s %s", r.Method, r.URL)
				}
				data := any(chat)
				if op == "list" {
					data = []any{map[string]any{"id": 7, "title": "Planning context", "ai_turn_status": "failed"}}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
			}))
			defer server.Close()
			env = nativeCLIEnv(t, server, 105)
			args := []string{op, "--json"}
			if op != "list" {
				args = []string{op, "7", "--json"}
			}
			out, err := executeChat(t, env, args...)
			if err != nil {
				t.Fatal(err)
			}
			if requests != 1 {
				t.Fatalf("expected one authenticated GET; got %d", requests)
			}
			var got any
			if err = json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			if op == "get" {
				raw, _ := json.Marshal(chat)
				var want any
				_ = json.Unmarshal(raw, &want)
				if !reflect.DeepEqual(got, want) {
					t.Fatal("transcript/context/metadata changed or truncated")
				}
			}
		})
	}
}

// TestCase 9340bd13-1541-4dd2-9ecc-23487a05ad93
func TestChatsHumanReadIncludesPlanningTextAndMetadata(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": chatFixture()})
	}))
	defer s.Close()
	out, err := executeChat(t, nativeCLIEnv(t, s, 105), "get", "7")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Planning context", "Finish planning\nPreserve the owner's decision", "364", "scope.png", "server-model"} {
		if !strings.Contains(out, want) {
			t.Fatalf("human transcript missing %q", want)
		}
	}
}

// TestCase 055a9d7e-e88c-425e-97d3-7652ff109e3a
func TestChatsOnlyExposeListAndGet(t *testing.T) {
	command := newChatsCommand(func() (*cliEnv, error) { t.Fatal("unexpected credential load"); return nil, nil })
	commands := command.Commands()
	if len(commands) != 2 {
		t.Fatalf("expected list and get only; got %d commands", len(commands))
	}
	for _, command := range commands {
		if command.Name() != "list" && command.Name() != "get" {
			t.Fatalf("unexpected Chat command %q", command.Name())
		}
	}
}

// TestCase 055a9d7e-e88c-425e-97d3-7652ff109e3a
func TestChatsRejectInvalidIDsAndExposeNoWriteCommand(t *testing.T) {
	for _, args := range [][]string{{"get", "0"}, {"get", "-1"}, {"get", "../7"}, {"diagnostics", "7"}, {"continue", "7"}, {"send", "7"}, {"create"}} {
		c := newChatsCommand(func() (*cliEnv, error) {
			t.Error("invalid invocation loaded credentials")
			return nil, fmt.Errorf("must not load")
		})
		c.SetArgs(args)
		c.SetOut(&bytes.Buffer{})
		c.SetErr(&bytes.Buffer{})
		if err := c.Execute(); err == nil {
			t.Fatalf("unsupported invocation succeeded: %v", args)
		}
	}
}

// TestCase 055a9d7e-e88c-425e-97d3-7652ff109e3a
func TestChatsRefuseUnauthorizedForeignAndMalformedResponses(t *testing.T) {
	for _, status := range []int{401, 403, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = fmt.Fprint(w, "{\"error\":\"Chat unavailable\"}")
			}))
			defer s.Close()
			if _, err := executeChat(t, nativeCLIEnv(t, s, 105), "get", "7", "--json"); err == nil {
				t.Fatalf("HTTP %d reported success", status)
			}
		})
	}
	for name, body := range map[string]string{"invalid-json": "not-json", "missing-data": "{}", "wrong-chat": "{\"data\":{\"id\":8,\"turns\":[]}}", "missing-transcript": "{\"data\":{\"id\":7}}", "bad-message": "{\"data\":{\"id\":7,\"turns\":[{}]}}"} {
		t.Run(name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, body) }))
			defer s.Close()
			if _, err := executeChat(t, nativeCLIEnv(t, s, 105), "get", "7", "--json"); err == nil {
				t.Fatal("malformed Chat reported success")
			}
		})
	}
}

// TestCase 055a9d7e-e88c-425e-97d3-7652ff109e3a
func TestChatsRejectIncompleteHTTPBodyEvenWhenJSONPrefixIsValid(t *testing.T) {
	body := "{\"data\":{\"id\":7,\"turns\":[]}}"
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(body)+10))
		_, _ = fmt.Fprint(w, body)
	}))
	defer s.Close()
	if _, err := executeChat(t, nativeCLIEnv(t, s, 105), "get", "7", "--json"); err == nil {
		t.Fatal("incomplete HTTP response reported complete transcript")
	}
}

// TestCase 055a9d7e-e88c-425e-97d3-7652ff109e3a
func TestChatsBrokenUnauthorizedBodyStillReportsCredentialRefusal(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "50")
		w.WriteHeader(401)
		_, _ = fmt.Fprint(w, "{}")
	}))
	defer s.Close()
	_, err := executeChat(t, nativeCLIEnv(t, s, 105), "get", "7", "--json")
	var rejected credentialError
	if !errors.As(err, &rejected) {
		t.Fatalf("401 lost its normal credential repair: %v", err)
	}
}
