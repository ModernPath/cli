package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequirementSetContextUsesBoundScopedEndpointAndDefaultsName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/systems/4/requirements/system/"+obsoleteRequirementID+"/context" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["context"] != "snake-game" || payload["context_name"] != "snake-game" {
			t.Errorf("payload = %#v", payload)
		}
		_, _ = w.Write([]byte(`{"data":{"id":"` + obsoleteRequirementID + `","work_status":"DONE","context":"snake-game","context_name":"snake-game","result":"updated"}}`))
	}))
	defer server.Close()

	command := newRequirementSetContextCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	command.SetArgs([]string{obsoleteRequirementID, "--kind", "system", "--context", "snake-game", "--json"})
	var out bytes.Buffer
	command.SetOut(&out)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"context_name": "snake-game"`) {
		t.Fatalf("receipt = %s", out.String())
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
