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

// Epic 322: requirements obsolete --file posts one all-or-nothing batch.
const (
	batchObsoleteSystemID = "22222222-2222-4222-8222-222222222222"
	batchObsoleteUserID   = "33333333-3333-4333-8333-333333333333"
)

func writeBatchObsoleteFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "batch.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRequirementObsoleteBatchFile(t *testing.T) {
	t.Run("requirements obsolete --file posts the batch once and prints each item's result in input order", func(t *testing.T) {
		file := writeBatchObsoleteFile(t, `{"reason":"Replaced by the reverse-engineered set","items":[`+
			`{"kind":"system","id":"`+obsoleteRequirementID+`"},`+
			`{"kind":"user","id":"`+batchObsoleteUserID+`","reason":"Merged into another requirement"},`+
			`{"kind":"system","id":"`+batchObsoleteSystemID+`"}]}`)
		response := `{"data":{"items":[` +
			`{"index":0,"kind":"system","id":"` + obsoleteRequirementID + `","work_status":"OBSOLETE","result":"obsoleted"},` +
			`{"index":1,"kind":"user","id":"` + batchObsoleteUserID + `","work_status":"OBSOLETE","result":"already_obsolete"},` +
			`{"index":2,"kind":"system","id":"` + batchObsoleteSystemID + `","work_status":"OBSOLETE","result":"obsoleted"}]}}`
		requests := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			if r.Method != http.MethodPost || r.URL.Path != "/api/v1/systems/4/requirements/obsolete" {
				t.Errorf("request = %s %s", r.Method, r.URL.Path)
			}
			var payload struct {
				Reason string           `json:"reason"`
				Items  []map[string]any `json:"items"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("payload error = %v", err)
			}
			if payload.Reason != "Replaced by the reverse-engineered set" || len(payload.Items) != 3 ||
				payload.Items[0]["id"] != obsoleteRequirementID || payload.Items[1]["kind"] != "user" ||
				payload.Items[1]["reason"] != "Merged into another requirement" || payload.Items[2]["id"] != batchObsoleteSystemID {
				t.Errorf("payload = %#v", payload)
			}
			_, _ = w.Write([]byte(response))
		}))
		defer server.Close()

		// Human output: one line per item, in input order, with index, kind, id and result.
		command := newRequirementObsoleteCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
		command.SetArgs([]string{"--file", file})
		var out bytes.Buffer
		command.SetOut(&out)
		if err := command.Execute(); err != nil {
			t.Fatal(err)
		}
		if requests != 1 {
			t.Fatalf("requests = %d; want one batch request", requests)
		}
		var lines []string
		for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
			if strings.TrimSpace(line) != "" {
				lines = append(lines, line)
			}
		}
		want := [][]string{
			{"0", "system", obsoleteRequirementID, "obsoleted"},
			{"1", "user", batchObsoleteUserID, "already_obsolete"},
			{"2", "system", batchObsoleteSystemID, "obsoleted"},
		}
		if len(lines) != len(want) {
			t.Fatalf("output lines = %q; want %d lines", lines, len(want))
		}
		for i, fields := range want {
			for _, field := range fields {
				if !strings.Contains(lines[i], field) {
					t.Fatalf("line %d = %q; want it to contain %q", i, lines[i], field)
				}
			}
		}

		// --json: the same items as a JSON array.
		command = newRequirementObsoleteCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
		command.SetArgs([]string{"--file", file, "--json"})
		out.Reset()
		command.SetOut(&out)
		if err := command.Execute(); err != nil {
			t.Fatal(err)
		}
		var items []map[string]any
		if err := json.Unmarshal(out.Bytes(), &items); err != nil {
			t.Fatalf("--json output is not a JSON array: %v\n%s", err, out.String())
		}
		if len(items) != len(want) {
			t.Fatalf("items = %#v", items)
		}
		for i, fields := range want {
			got := items[i]
			if fmt.Sprint(got["index"]) != fields[0] || got["kind"] != fields[1] || got["id"] != fields[2] || got["result"] != fields[3] {
				t.Fatalf("item %d = %#v; want %v", i, got, fields)
			}
		}
	})
}

func TestRequirementObsoletePositionalAndFileExclusion(t *testing.T) {
	t.Run("positional obsolete keeps its route and body, and --file excludes a positional id, --kind and --reason", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/api/v1/systems/4/requirements/system/"+obsoleteRequirementID+"/obsolete" {
				t.Errorf("request = %s %s", r.Method, r.URL.Path)
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || len(payload) != 1 || payload["reason"] != "No longer required" {
				t.Errorf("payload = %#v, error = %v", payload, err)
			}
			_, _ = w.Write([]byte(`{"data":{"id":"` + obsoleteRequirementID + `","work_status":"OBSOLETE","result":"obsoleted"}}`))
		}))
		defer server.Close()

		command := newRequirementObsoleteCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
		command.SetArgs([]string{obsoleteRequirementID, "--kind", "system", "--reason", "No longer required", "--json"})
		var out bytes.Buffer
		command.SetOut(&out)
		if err := command.Execute(); err != nil {
			t.Fatal(err)
		}
		var receipt map[string]any
		if err := json.Unmarshal(out.Bytes(), &receipt); err != nil {
			t.Fatalf("receipt = %s (%v)", out.String(), err)
		}
		if receipt["id"] != obsoleteRequirementID || receipt["work_status"] != "OBSOLETE" || receipt["result"] != "obsoleted" {
			t.Fatalf("receipt = %#v", receipt)
		}

		file := writeBatchObsoleteFile(t, `{"reason":"Replaced","items":[{"kind":"system","id":"`+obsoleteRequirementID+`"}]}`)
		const exclusion = "--file cannot be combined with a positional id, --kind or --reason"
		for _, args := range [][]string{
			{obsoleteRequirementID, "--file", file},
			{"--file", file, "--kind", "system"},
			{"--file", file, "--reason", "Replaced"},
		} {
			loaded := false
			command := newRequirementObsoleteCommand(func() (*cliEnv, error) { loaded = true; return nil, fmt.Errorf("workspace loaded") })
			command.SetArgs(args)
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			err := command.Execute()
			if err == nil || !strings.Contains(err.Error(), exclusion) {
				t.Fatalf("args %v: error = %v; want %q", args, err, exclusion)
			}
			if loaded {
				t.Fatalf("args %v: the workspace was loaded before the exclusion was refused", args)
			}
		}
	})
}

// Epic 380, SR-4653-0 criterion d3c8893a: requirements obsolete's help, as
// modernpath install renders it into the reference, says a published
// requirement is retired through an Epic plan's retire and confirmed by its
// domain owner. The command's behaviour is unchanged.
func TestRequirementObsoleteHelpPointsPublishedRequirementsToAnEpicsRetire(t *testing.T) {
	ref := renderCLIReference(rootCmd)
	for name, text := range map[string]string{
		"requirements obsolete --help":               newRequirementObsoleteCommand(cliEnvLoad).Long,
		"the rendered requirements obsolete section": referenceSection(t, ref, "modernpath requirements obsolete"),
	} {
		normalized := strings.Join(strings.Fields(text), " ")
		for _, want := range []string{
			"A published requirement is retired through an Epic plan's `retire`",
			"confirmed by its domain owner",
		} {
			if !strings.Contains(normalized, want) {
				t.Errorf("%s does not say %q", name, want)
			}
		}
	}
}
