package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequirementCreatePostsBoundNativeContentAndReturnsPlanIdentity(t *testing.T) {
	for _, kind := range []string{"user", "system"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/api/compliance/"+kind+"-requirements" {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if payload["system_id"] != float64(4) || payload["title"] != "New behavior" || payload["description"] != "Requested outcome" || len(payload) != 3 {
					t.Errorf("payload = %#v", payload)
				}
				w.WriteHeader(http.StatusCreated)
				fmt.Fprintf(w, `{"data":{"id":"11111111-1111-4111-8111-111111111111","requirement_kind":%q,"display_ref":"UR-1-0","system_id":4,"title":"New behavior","work_status":"PROPOSED","revision":0,"description":"Large content omitted from receipt"}}`, kind+"_requirement")
			}))
			defer server.Close()
			input := `{"title":"New behavior","description":"Requested outcome"}`
			file := "-"
			if kind == "user" {
				file = filepath.Join(t.TempDir(), "requirement.json")
				if err := os.WriteFile(file, []byte(input), 0600); err != nil {
					t.Fatal(err)
				}
			}
			command := newRequirementCreateCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
			command.SetArgs([]string{"--kind", kind, "--file", file, "--json"})
			command.SetIn(strings.NewReader(input))
			var out bytes.Buffer
			command.SetOut(&out)
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			var receipt map[string]json.RawMessage
			if err := json.Unmarshal(out.Bytes(), &receipt); err != nil {
				t.Fatal(err)
			}
			assertJSONKeys(t, receipt, []string{"display_ref", "id", "requirement_kind", "revision", "system_id", "title", "work_status"})
			if !strings.Contains(out.String(), "11111111-1111-4111-8111-111111111111") || !strings.Contains(out.String(), kind+"_requirement") {
				t.Fatalf("missing plan identity: %s", out.String())
			}
		})
	}
}

func TestRequirementCreateRejectsBadContentBeforeLoadingWorkspace(t *testing.T) {
	for _, input := range []string{
		`{}`, `{"title":" "}`, `{"title":4}`, `{"title":"New","id":"11111111-1111-4111-8111-111111111111"}`,
		`{"title":"New","system_id":8}`, `{"title":"New","tenant_id":9}`, `{"title":"New","work_status":"DONE"}`,
		`{"title":"New","revision":4}`, `{"title":"New","release_id":3}`, `{"title":"New","criteria":[]}`,
		`{"title":"New","actor":"Wrong kind"}`, `{"title":"New","priority":"high"}`, `{"title":"New","description":[]}`, `null`, `[]`, `{"title":"New"} {}`,
	} {
		t.Run(input, func(t *testing.T) {
			command := newRequirementCreateCommand(func() (*cliEnv, error) { t.Fatal("invalid content loaded workspace"); return nil, nil })
			command.SetArgs([]string{"--kind", "system", "--file", "-"})
			command.SetIn(strings.NewReader(input))
			if err := command.Execute(); err == nil {
				t.Fatal("invalid content accepted")
			}
		})
	}
	command := newRequirementCreateCommand(func() (*cliEnv, error) { t.Fatal("invalid kind loaded workspace"); return nil, nil })
	command.SetArgs([]string{"--kind", "epic", "--file", "-"})
	if err := command.Execute(); err == nil {
		t.Fatal("invalid kind accepted")
	}
}

func TestRequirementCreateReportsRefusalsAndRejectsUnscopedReceipts(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		status   int
		response string
		want     string
	}{
		{"refusal", 422, `{"errors":{"title":["is invalid"]}}`, "requirement creation refused"},
		{"foreign", 201, `{"data":{"system_id":99}}`, "different System"},
		{"missing identity", 201, `{"data":{"system_id":4}}`, "native identity"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(scenario.status)
				_, _ = w.Write([]byte(scenario.response))
			}))
			defer server.Close()
			command := newRequirementCreateCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
			command.SetArgs([]string{"--kind", "user", "--file", "-"})
			command.SetIn(strings.NewReader(`{"title":"New behavior"}`))
			if err := command.Execute(); err == nil || !strings.Contains(err.Error(), scenario.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestRequirementCreateEmptyObjectExplainsMissingTitle(t *testing.T) {
	err := validateRequirementCreate(map[string]any{}, "system")
	if err == nil || err.Error() != "requirement title is required" {
		t.Fatalf("error = %v", err)
	}
}
