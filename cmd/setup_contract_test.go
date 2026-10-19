package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/fatih/color"
)

func runRootStdout(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	diagnostic, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr, oldColor, oldDiagnostic, oldState := os.Stdout, os.Stderr, color.Output, color.Error, commandErrors
	defer func() {
		os.Stdout, os.Stderr, color.Output, color.Error, commandErrors = oldOut, oldErr, oldColor, oldDiagnostic, oldState
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
		out.Close()
		diagnostic.Close()
	}()
	os.Stdout, os.Stderr, color.Output, color.Error = out, diagnostic, out, diagnostic
	rootCmd.SetOut(out)
	rootCmd.SetErr(diagnostic)
	rootCmd.SetArgs(args)
	resetTreeFlags(rootCmd)
	beginCommandRun(args)
	err = rootCmd.Execute()
	data, readErr := os.ReadFile(out.Name())
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(data), err
}

func TestNewJSONReturnsOnlyProjectReceipt(t *testing.T) {
	server, _ := newTestServer(t, http.StatusCreated, `{"data":{"id":7}}`)
	cobraWorkspace(t, server)
	out, err := runRootStdout(t, "new", "Ledger", "--desc", "A double-entry ledger service for small finance teams.", "--profile", "Elixir", "--type", "api", "--yes", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var receipt map[string]any
	if err := json.Unmarshal([]byte(out), &receipt); err != nil || receipt["system_id"] != float64(4242) || receipt["epic_id"] != float64(7) || receipt["operation"] != "created" {
		t.Fatalf("project receipt = %q; %v", out, err)
	}
}

func TestInitJSONAlreadyBoundReturnsReceiptWithoutNetwork(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++ }))
	defer server.Close()
	cobraWorkspace(t, server)
	for _, args := range [][]string{{"init", "--json"}, {"init", "workspace", "--json"}} {
		out, err := runRootStdout(t, args...)
		var receipt map[string]any
		if err != nil || json.Unmarshal([]byte(out), &receipt) != nil || receipt["operation"] != "unchanged" || receipt["system_id"] != float64(1) {
			t.Fatalf("binding receipt for %v = %q, %v", args, out, err)
		}
	}
	if requests != 0 {
		t.Fatalf("already-bound init made %d network requests", requests)
	}
}

func TestFocusJSONListReceiptAndRefusal(t *testing.T) {
	refused := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		if refused {
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"error":"focus unavailable"}`))
			return
		}
		if r.Method == http.MethodDelete {
			_, _ = w.Write([]byte(`{"data":{"ended":[]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"focus_states":[{"ref_id":42,"ref_kind":"epic","title":"Cart","user":{"name":"Test"}}]}}`))
	}))
	defer server.Close()
	cobraWorkspace(t, server)
	out, err := runRootStdout(t, "focus", "--json")
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if err != nil || json.Unmarshal([]byte(out), &page) != nil || len(page.Items) != 1 || page.Items[0]["display_ref"] != "42" {
		t.Fatalf("focus list = %q, %v", out, err)
	}
	out, err = runRootStdout(t, "focus", "--clear", "--json")
	var receipt map[string]any
	if err != nil || json.Unmarshal([]byte(out), &receipt) != nil || receipt["operation"] != "cleared" || receipt["count"] != float64(0) {
		t.Fatalf("focus receipt = %q, %v", out, err)
	}
	refused = true
	if out, err = runRootStdout(t, "focus", "--json"); err == nil || out != "" {
		t.Fatalf("focus refusal reported success: %q, %v", out, err)
	}
}
