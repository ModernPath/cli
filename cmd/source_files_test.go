package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceFileRelocatePostsBoundSystemAndKeepsIdentityReceipt(t *testing.T) {
	const sourceID = "11111111-1111-4111-8111-111111111111"
	const destinationID = "22222222-2222-4222-8222-222222222222"
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/compliance/source-files/"+sourceID+"/relocate" {
			t.Errorf("relocation request = %s %s", r.Method, r.URL.String())
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"` + sourceID + `","repository_key":"api","path":"lib/new.ex","result":"relocated"}}`))
	}))
	defer server.Close()
	file := filepath.Join(t.TempDir(), "relocation.json")
	input := `{"expected_path":"lib/old.ex","path":"lib/new.ex","destination_id":"` + destinationID + `","reason":"Code moved"}`
	if err := os.WriteFile(file, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	command := newSourceFilesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	command.SetArgs([]string{"relocate", sourceID, "--file", file, "--json"})
	var output bytes.Buffer
	command.SetOut(&output)
	err := command.Execute()
	if err != nil {
		t.Fatal(err)
	}
	if received["system_id"] != float64(4) || received["expected_path"] != "lib/old.ex" || received["path"] != "lib/new.ex" || received["destination_id"] != destinationID || received["reason"] != "Code moved" || len(received) != 5 {
		t.Fatalf("relocation payload = %#v", received)
	}
	for _, want := range []string{sourceID, `"repository_key"`, `"path"`, `"result"`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("receipt missing %q: %s", want, output.String())
		}
	}
	for _, want := range []string{destinationID, "Both SourceFile identities", "oldest"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("collision receipt missing %q: %s", want, output.String())
		}
	}
}

func TestSourceFileRelocateAcceptsUppercaseUUIDWithCanonicalReceipt(t *testing.T) {
	const sourceID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	const destinationID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/compliance/source-files/"+sourceID+"/relocate" {
			t.Errorf("request = %s %s", r.Method, r.URL.String())
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["destination_id"] != destinationID || body["system_id"] != float64(4) {
			t.Errorf("payload = %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"` + sourceID + `","repository_key":"api","path":"lib/new.ex","result":"relocated"}}`))
	}))
	defer server.Close()
	file := filepath.Join(t.TempDir(), "relocation.json")
	input := `{"expected_path":"lib/old.ex","path":"lib/new.ex","reason":"Code moved","destination_id":"BBBBBBBB-BBBB-4BBB-8BBB-BBBBBBBBBBBB"}`
	if err := os.WriteFile(file, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	command := newSourceFilesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	command.SetArgs([]string{"relocate", "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA", "--file", file, "--json"})
	var output bytes.Buffer
	command.SetOut(&output)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), sourceID) || !strings.Contains(output.String(), destinationID) || !strings.Contains(output.String(), `"result": "relocated"`) {
		t.Fatalf("receipt = %s", output.String())
	}
}

func TestSourceFileRelocateRejectsUnscopedOrMalformedInputBeforeWrite(t *testing.T) {
	const sourceID = "11111111-1111-4111-8111-111111111111"
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++ }))
	defer server.Close()
	for _, tc := range []struct{ name, id, input, want string }{
		{"bad id", "not-an-id", `{"expected_path":"old","path":"new","reason":"move"}`, "UUID"},
		{"empty reason", sourceID, `{"expected_path":"old","path":"new","reason":" "}`, "reason"},
		{"empty path", sourceID, `{"expected_path":"old","path":" ","reason":"move"}`, "path"},
		{"empty expected path", sourceID, `{"expected_path":" ","path":"new","reason":"move"}`, "expected_path"},
		{"bad destination", sourceID, `{"expected_path":"old","path":"new","reason":"move","destination_id":"maybe"}`, "destination_id"},
		{"system override", sourceID, `{"expected_path":"old","path":"new","reason":"move","system_id":9}`, "system_id"},
		{"tenant override", sourceID, `{"expected_path":"old","path":"new","reason":"move","tenant_id":9}`, "tenant_id"},
		{"repository override", sourceID, `{"expected_path":"old","path":"new","reason":"move","repository_key":"other"}`, "repository_key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "relocation.json")
			if err := os.WriteFile(file, []byte(tc.input), 0o600); err != nil {
				t.Fatal(err)
			}
			command := newSourceFilesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
			command.SetArgs([]string{"relocate", tc.id, "--file", file, "--json"})
			err := command.Execute()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
	if requests != 0 {
		t.Fatalf("malformed input made %d requests", requests)
	}
}

func TestSourceFileRelocateSurfacesExactCollisionRefusal(t *testing.T) {
	const sourceID = "11111111-1111-4111-8111-111111111111"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":"destination_exists","message":"Pass destination_id for the existing SourceFile"}}`))
	}))
	defer server.Close()
	file := filepath.Join(t.TempDir(), "relocation.json")
	if err := os.WriteFile(file, []byte(`{"expected_path":"old","path":"new","reason":"move"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	command := newSourceFilesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	command.SetArgs([]string{"relocate", sourceID, "--file", file, "--json"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "server 409") || !strings.Contains(err.Error(), "Pass destination_id") {
		t.Fatalf("collision refusal = %v", err)
	}
}
