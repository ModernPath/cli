// REQ-CROSS-210/Q-ARCH-016 — `factory drift --report`'s failure branches,
// pinned after the second external review round (RUN:2026-08-19): the POST's
// result was once discarded entirely, and the first fix shipped untested.
package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func driftEnv(url string) *factoryEnv {
	return &factoryEnv{Root: ".", APIURL: url, SystemID: 1, token: "t"}
}

func TestDriftReportSuccessIsQuiet(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Method + " " + r.URL.Path
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	t.Cleanup(server.Close)

	if err := reportDriftTarget(driftEnv(server.URL), "REQ-X-001", []string{"a.go"}, "abc", "def"); err != nil {
		t.Fatalf("a 200 answer must not be an error: %v", err)
	}
	if got != "POST /api/v1/sync/evidence/drift" {
		t.Fatalf("wrong request: %q", got)
	}
}

func TestDriftReportHTTPErrorIsNamed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	t.Cleanup(server.Close)

	err := reportDriftTarget(driftEnv(server.URL), "REQ-X-001", nil, "abc", "def")
	if err == nil {
		t.Fatal("HTTP 500 must be an error — the server recorded nothing")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("the status must be named: %v", err)
	}
}

func TestDriftReportTransportErrorIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close() // connection refused from here on

	if err := reportDriftTarget(driftEnv(server.URL), "REQ-X-001", nil, "abc", "def"); err == nil {
		t.Fatal("a transport failure must be an error — the server never heard the report")
	}
}
