package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const obsoleteRequirementID = "11111111-1111-4111-8111-111111111111"

func TestRequirementObsoleteUsesScopedReasonedEndpointAndValidatesReceipt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/systems/4/requirements/system/"+obsoleteRequirementID+"/obsolete" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload["reason"] != "No longer required" {
			t.Errorf("payload = %#v, error = %v", payload, err)
		}
		_, _ = w.Write([]byte(`{"data":{"id":"` + obsoleteRequirementID + `","work_status":"OBSOLETE","result":"obsoleted"}}`))
	}))
	defer server.Close()

	command := newRequirementObsoleteCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	command.SetArgs([]string{obsoleteRequirementID, "--kind", "system", "--reason", " No longer required ", "--json"})
	var out bytes.Buffer
	command.SetOut(&out)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"work_status": "OBSOLETE"`) {
		t.Fatalf("receipt = %s", out.String())
	}
}

func TestRequirementObsoleteRejectsInvalidInputBeforeLoadingWorkspace(t *testing.T) {
	for _, args := range [][]string{
		{"not-a-uuid", "--kind", "system", "--reason", "because"},
		{obsoleteRequirementID, "--kind", "epic", "--reason", "because"},
		{obsoleteRequirementID, "--kind", "user", "--reason", "  "},
	} {
		command := newRequirementObsoleteCommand(func() (*cliEnv, error) { t.Fatal("invalid input loaded workspace"); return nil, nil })
		command.SetArgs(args)
		if err := command.Execute(); err == nil {
			t.Fatalf("accepted invalid args %v", args)
		}
	}
}

func TestRequirementObsoleteAcceptsCanonicalReceiptForUppercaseUUID(t *testing.T) {
	const id = "abcdefab-1234-4234-8234-abcdefabcdef"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.EqualFold(r.URL.Path, "/api/v1/systems/4/requirements/system/"+id+"/obsolete") {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":{"id":"` + id + `","work_status":"OBSOLETE","result":"obsoleted"}}`))
	}))
	defer server.Close()

	command := newRequirementObsoleteCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	command.SetArgs([]string{strings.ToUpper(id), "--kind", "system", "--reason", "No longer required", "--json"})
	var out bytes.Buffer
	command.SetOut(&out)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"id": "`+id+`"`) {
		t.Fatalf("receipt = %s", out.String())
	}
}

func TestRequirementObsoleteReportsServerRefusalAndRejectsMismatchedReceipt(t *testing.T) {
	for _, scenario := range []struct {
		name, response, want string
		status               int
	}{
		{"server refusal", `{"error":"requirement_owned"}`, "requirement obsoletion refused", http.StatusConflict},
		{"wrong identity", `{"data":{"id":"22222222-2222-4222-8222-222222222222","work_status":"OBSOLETE"}}`, "does not confirm", http.StatusOK},
		{"wrong status", `{"data":{"id":"` + obsoleteRequirementID + `","work_status":"PROPOSED"}}`, "does not confirm", http.StatusOK},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(scenario.status)
				_, _ = w.Write([]byte(scenario.response))
			}))
			defer server.Close()
			command := newRequirementObsoleteCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
			command.SetArgs([]string{obsoleteRequirementID, "--kind", "user", "--reason", "because"})
			if err := command.Execute(); err == nil || !strings.Contains(err.Error(), scenario.want) {
				t.Fatalf("error = %v; want %q", err, scenario.want)
			}
		})
	}
}
