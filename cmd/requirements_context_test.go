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

// Epic 340 (G1), test case 9022296b: set-context without --context-name sends
// only the context, and the server fills in the name from the System's list of
// contexts. This replaces the test that asserted the old default, the code
// sent as the name (TestRequirementSetContextUsesBoundScopedEndpointAndDefaultsName).
func TestRequirementSetContextUsesBoundScopedEndpointAndSendsNoName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/systems/4/requirements/system/"+obsoleteRequirementID+"/context" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["context"] != "snake-game" {
			t.Errorf("payload = %#v", payload)
		}
		if name, sent := payload["context_name"]; sent {
			t.Errorf("set-context without --context-name sent the name %#v", name)
		}
		_, _ = w.Write([]byte(`{"data":{"id":"` + obsoleteRequirementID + `","work_status":"DONE","context":"snake-game","context_name":"Snake game","result":"updated"}}`))
	}))
	defer server.Close()

	command := newRequirementSetContextCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	command.SetArgs([]string{obsoleteRequirementID, "--kind", "system", "--context", "snake-game", "--json"})
	var out bytes.Buffer
	command.SetOut(&out)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"context_name": "Snake game"`) {
		t.Fatalf("receipt = %s", out.String())
	}
}

// Epic 340 (G1), test case 9022296b: without --context-name, set-context
// accepts the non-blank name the server returns and prints the name and the
// result: for a requirement of another context, for one already in the
// context with a minority name (updated), and for one that already carries the
// list's name (unchanged), also the only requirement of its context.
func TestRequirementSetContextAcceptsTheNameTheServerReturns(t *testing.T) {
	for _, tc := range []struct {
		name, context, contextName, result string
	}{
		{"from another context", "BILLING", "Billing", "updated"},
		{"already in the context with a minority name", "BILLING", "Billing", "updated"},
		{"the only requirement of its context", "SHIPPING", "Shipping", "unchanged"},
		{"its own name decides the list's name", "BILLING", "Billing", "unchanged"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload map[string]any
				_ = json.NewDecoder(r.Body).Decode(&payload)
				if len(payload) != 1 || payload["context"] != tc.context {
					t.Errorf("payload = %#v; want only the context %s", payload, tc.context)
				}
				fmt.Fprintf(w, `{"data":{"id":%q,"work_status":"PROPOSED","context":%q,"context_name":%q,"result":%q}}`,
					obsoleteRequirementID, tc.context, tc.contextName, tc.result)
			}))
			defer server.Close()

			command := newRequirementSetContextCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
			command.SetArgs([]string{obsoleteRequirementID, "--kind", "system", "--context", tc.context})
			var out bytes.Buffer
			command.SetOut(&out)
			if err := command.Execute(); err != nil {
				t.Fatalf("set-context refused the server's receipt: %v", err)
			}
			for _, want := range []string{"context_name: " + tc.contextName, "result: " + tc.result} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
		})
	}
}

// Epic 340 (G1), test case 9022296b, guard: a receipt with another context or
// a blank name is still refused, and an explicit --context-name is still sent
// and must come back.
func TestRequirementSetContextStillRefusesAnotherContextOrABlankName(t *testing.T) {
	for _, tc := range []struct {
		name, receipt string
		args          []string
	}{
		{"another context", `{"data":{"id":"` + obsoleteRequirementID + `","context":"OTHER","context_name":"Billing","result":"updated"}}`, nil},
		{"an empty name", `{"data":{"id":"` + obsoleteRequirementID + `","context":"BILLING","context_name":"","result":"updated"}}`, nil},
		{"a blank name", `{"data":{"id":"` + obsoleteRequirementID + `","context":"BILLING","context_name":"  ","result":"updated"}}`, nil},
		{"another name than the one sent", `{"data":{"id":"` + obsoleteRequirementID + `","context":"BILLING","context_name":"Invoices","result":"updated"}}`, []string{"--context-name", "Billing"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload map[string]any
				_ = json.NewDecoder(r.Body).Decode(&payload)
				if tc.args != nil && payload["context_name"] != "Billing" {
					t.Errorf("an explicit --context-name was not sent: %#v", payload)
				}
				_, _ = w.Write([]byte(tc.receipt))
			}))
			defer server.Close()
			command := newRequirementSetContextCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
			command.SetArgs(append([]string{obsoleteRequirementID, "--kind", "system", "--context", "BILLING"}, tc.args...))
			if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "does not confirm") {
				t.Fatalf("error = %v; want a refusal that the response does not confirm the values", err)
			}
		})
	}
}

func TestRequirementSetContextRejectsInvalidInputBeforeLoadingWorkspace(t *testing.T) {
	for _, args := range [][]string{
		{"not-a-uuid", "--kind", "system", "--context", "snake-game"},
		{obsoleteRequirementID, "--kind", "epic", "--context", "snake-game"},
		{obsoleteRequirementID, "--kind", "system", "--context", "  "},
	} {
		command := newRequirementSetContextCommand(func() (*cliEnv, error) { t.Fatal("invalid input loaded workspace"); return nil, nil })
		command.SetArgs(args)
		if err := command.Execute(); err == nil {
			t.Fatalf("accepted invalid args %v", args)
		}
	}
}

func TestRequirementSetContextRejectsExplicitBlankName(t *testing.T) {
	for _, name := range []string{"", "  "} {
		t.Run(fmt.Sprintf("name=%q", name), func(t *testing.T) {
			loaded := false
			command := newRequirementSetContextCommand(func() (*cliEnv, error) {
				loaded = true
				return nil, fmt.Errorf("workspace must not load")
			})
			command.SetArgs([]string{obsoleteRequirementID, "--kind", "system", "--context", "Billing", "--context-name", name})
			err := command.Execute()
			if loaded || err == nil || err.Error() != "context_name must contain 1 to 255 bytes of nonblank text" {
				t.Fatalf("loaded=%v error=%v", loaded, err)
			}
		})
	}
}

func TestRequirementSetContextAcceptsCanonicalReceiptForUppercaseUUID(t *testing.T) {
	const id = "abcdefab-1234-4234-8234-abcdefabcdef"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || !strings.EqualFold(r.URL.Path, "/api/v1/systems/4/requirements/system/"+id+"/context") {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":{"id":"` + id + `","work_status":"DONE","context":"shifts","context_name":"shifts","result":"updated"}}`))
	}))
	defer server.Close()

	command := newRequirementSetContextCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	command.SetArgs([]string{strings.ToUpper(id), "--kind", "system", "--context", "shifts", "--json"})
	var out bytes.Buffer
	command.SetOut(&out)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"id": "`+id+`"`) {
		t.Fatalf("receipt = %s", out.String())
	}
}

func TestRequirementSetContextReportsRefusalAndRejectsMismatchedReceipt(t *testing.T) {
	for _, scenario := range []struct {
		name, response, want string
		status               int
	}{
		{"server refusal", `{"error":"requirement_owned"}`, "requirement context update refused", http.StatusConflict},
		{"wrong identity", `{"data":{"id":"22222222-2222-4222-8222-222222222222","context":"shifts","context_name":"shifts"}}`, "does not confirm", http.StatusOK},
		{"wrong context", `{"data":{"id":"` + obsoleteRequirementID + `","context":"other","context_name":"other"}}`, "does not confirm", http.StatusOK},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(scenario.status)
				_, _ = w.Write([]byte(scenario.response))
			}))
			defer server.Close()
			command := newRequirementSetContextCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
			command.SetArgs([]string{obsoleteRequirementID, "--kind", "user", "--context", "shifts"})
			if err := command.Execute(); err == nil || !strings.Contains(err.Error(), scenario.want) {
				t.Fatalf("error = %v; want %q", err, scenario.want)
			}
		})
	}
}
