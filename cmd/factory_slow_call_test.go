package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// REQ-CROSS-176: a factory call that is taking long says so.
//
// `factory sync` makes up to ~8 sequential server calls, each allowed 120
// seconds. One stalled connection — a VPN flap, a mid-request network change, a
// slow beta deploy — turns "a few seconds" into two silent minutes before the
// timeout error appears. Nothing between step messages distinguishes a slow
// server from a hung CLI, and the user restarts a sync that was working.
func TestSlowFactoryCallPrintsANotice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(60 * time.Millisecond)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	prevAfter, prevTo := slowCallNoticeAfter, slowCallNoticeTo
	var buf bytes.Buffer
	slowCallNoticeAfter, slowCallNoticeTo = 5*time.Millisecond, &buf
	defer func() { slowCallNoticeAfter, slowCallNoticeTo = prevAfter, prevTo }()

	env := &factoryEnv{APIURL: srv.URL, token: "t"}
	if _, _, err := env.call("POST", "/api/v1/sync/batch", map[string]any{}); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "still waiting") || !strings.Contains(out, "/api/v1/sync/batch") {
		t.Fatalf("no notice for a slow call — a slow server and a hung CLI look identical (got %q)", out)
	}
}

func TestFastFactoryCallPrintsNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	prevAfter, prevTo := slowCallNoticeAfter, slowCallNoticeTo
	var buf bytes.Buffer
	slowCallNoticeAfter, slowCallNoticeTo = 200*time.Millisecond, &buf
	defer func() { slowCallNoticeAfter, slowCallNoticeTo = prevAfter, prevTo }()

	env := &factoryEnv{APIURL: srv.URL, token: "t"}
	if _, _, err := env.call("GET", "/api/v1/sync/gates", nil); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("fast call produced noise: %q — the notice must mark the exception, not the rule", buf.String())
	}
}
