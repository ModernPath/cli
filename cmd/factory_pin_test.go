package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/fatih/color"
)

// REQ-CROSS-361 (EPIC-CLI-014): `modernpath factory pin set` sets the caller's
// compliance PIN by POSTing to the same endpoint the frontend uses, so a
// CLI-first user can set the PIN that release signoff and activation require
// without the (unreachable) Compliance UI.
//
// RED first: setCompliancePin / readPinStdin do not exist yet, so this file
// does not compile — the pin-set behavior is absent.

func pinTestEnv(t *testing.T, srv *httptest.Server) *factoryEnv {
	t.Helper()
	return &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}
}

func TestSetCompliancePin_PostsPinToEndpoint(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"has_pin": true}})
	}))
	defer srv.Close()

	if err := setCompliancePin(pinTestEnv(t, srv), "2468"); err != nil {
		t.Fatalf("setCompliancePin: %v", err)
	}
	if gotMethod != "POST" || gotPath != "/api/compliance/pin" {
		t.Errorf("hit %s %s, want POST /api/compliance/pin", gotMethod, gotPath)
	}
	if gotBody["pin"] != "2468" {
		t.Errorf("posted pin = %v, want 2468", gotBody["pin"])
	}
}

func TestSetCompliancePin_ServerRejectionIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_pin_format"})
	}))
	defer srv.Close()

	err := setCompliancePin(pinTestEnv(t, srv), "123456")
	if err == nil {
		t.Fatal("expected an error on a 422, got nil (a silent success would hide a rejected PIN)")
	}
	if !strings.Contains(err.Error(), "invalid_pin_format") {
		t.Errorf("error %q should carry the server's reason", err.Error())
	}
}

func TestSetCompliancePin_ClientValidatesFormat(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()

	if err := setCompliancePin(pinTestEnv(t, srv), "12ab"); err == nil {
		t.Error("expected a client-side format error for a non-digit PIN")
	}
	if called {
		t.Error("a malformed PIN must not reach the server")
	}
}

// The PIN must never be echoed to stdout/stderr (a leak into terminal scrollback).
func TestSetCompliancePin_DoesNotEchoPin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"has_pin": true}})
	}))
	defer srv.Close()

	out := captureOutput(t, func() {
		if err := setCompliancePin(pinTestEnv(t, srv), "97531"); err != nil {
			t.Fatalf("setCompliancePin: %v", err)
		}
	})
	if strings.Contains(out, "97531") {
		t.Errorf("output leaked the PIN: %q", out)
	}
}

// --pin-stdin reads exactly one line (trimmed) from the provided reader, so the
// PIN never appears in argv/ps/shell history.
func TestReadPinStdin_ReadsOneTrimmedLine(t *testing.T) {
	got, err := readPinStdin(strings.NewReader("2468\n8642\n"))
	if err != nil {
		t.Fatalf("readPinStdin: %v", err)
	}
	if got != "2468" {
		t.Errorf("readPinStdin = %q, want 2468 (one line, trimmed)", got)
	}
}

// captureOutput redirects os.Stdout, os.Stderr, and fatih/color's writer — which
// printSuccess uses via color.Green — for the duration of fn, so an accidental
// print of the PIN is actually caught rather than slipping through color.Output.
func captureOutput(t *testing.T, fn func()) string {
	t.Helper()
	oldOut, oldErr, oldColor := os.Stdout, os.Stderr, color.Output
	r, w, _ := os.Pipe()
	os.Stdout, os.Stderr, color.Output = w, w, w
	defer func() { os.Stdout, os.Stderr, color.Output = oldOut, oldErr, oldColor }()
	fn()
	_ = w.Close()
	data, _ := io.ReadAll(r)
	return string(data)
}
