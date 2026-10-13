package cmd

// The cold review of the #779 fixes (CODE-OUTAGE-4-1..4-3, 2026-10-13): a 5xx
// or a request without an answer on the replay's own reads stops or retries,
// never sets the entry aside; the replay-time capability check is covered; an
// answered probe is never an outage.

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func coreFault() *replayReply {
	return &replayReply{status: 503, body: map[string]any{"error": map[string]any{"reason": "unavailable", "message": "database busy"}}}
}

// REQ-CROSS-539 AC6/AC7 (CODE-OUTAGE-4-1) — the read-back after a 409 that
// meets a 5xx stops the replay; one that gets no answer is retried. Neither
// sets the entry aside as conflicted.
func TestReadBackFailureDoesNotConflict(t *testing.T) {
	for _, tc := range []struct {
		name    string
		slow    bool
		attempt float64
	}{{name: "a 5xx", slow: false, attempt: 0}, {name: "no answer", slow: true, attempt: 1}} {
		t.Run(tc.name, func(t *testing.T) {
			f := &replayFake{}
			f.on = func(_ *replayFake, r journeyRequest, _ url.Values) *replayReply {
				switch {
				case r.path == "/api/v1/sync/author":
					return conflict409("stale_fingerprint")
				case r.method == "GET" && r.path == "/api/v1/sync/items":
					if tc.slow {
						time.Sleep(600 * time.Millisecond)
						return nil
					}
					return coreFault()
				}
				return nil
			}
			root := replayWorkspace(t, f)
			keep(t, "author", "update", "REQ-R-2", "--title", "New title", "--expected-fingerprint", "fp-1")
			if tc.slow {
				scaleOutageTimeouts(t, 500*time.Millisecond, 200*time.Millisecond)
			}
			serverBack(t, root)

			runCLI(t, "outage", "replay")

			if n := len(asideEntries(t, root, "conflicted")); n != 0 {
				t.Errorf("a read-back that failed proves nothing; %d conflicted", n)
			}
			kept := keptEntries(t, root)
			if len(kept) != 1 {
				t.Fatalf("the entry stays kept; %d kept", len(kept))
			}
			if got, _ := kept[0].entry["attempts"].(float64); got != tc.attempt {
				t.Errorf("attempts %v, want %v", got, tc.attempt)
			}
		})
	}
}

// REQ-CROSS-539 AC6 (CODE-OUTAGE-4-1) — a kept push whose selection read meets
// a 5xx stops the replay and stays kept.
func TestPushSelectionFaultStopsReplay(t *testing.T) {
	root, _, selection := outagePushWorkspace(t)
	*selection = func(string) *replayReply { return coreFault() }
	serverBack(t, root)

	runCLI(t, "outage", "replay")

	for _, sub := range []string{"conflicted", "refused"} {
		if n := len(asideEntries(t, root, sub)); n != 0 {
			t.Errorf("a server fault sets nothing aside as %s; %d there", sub, n)
		}
	}
	if n := len(keptEntries(t, root)); n != 1 {
		t.Errorf("the kept push stays kept; %d kept", n)
	}
}

// REQ-CROSS-538 AC2 / REQ-CROSS-539 (CODE-OUTAGE-4-2) — a kept RESOLVED
// disposition is checked against the server's capability when it is replayed:
// refused without it, sent with it, and kept when the contract read meets a
// 5xx.
func TestReplayChecksFindingCapability(t *testing.T) {
	advertised := &replayReply{body: data(map[string]any{"version": 1,
		"capabilities": map[string]any{"author.finding": []any{"finding_resolution"}}})}
	for _, tc := range []struct {
		name     string
		contract *replayReply
		sent     bool
		refused  int
		kept     int
	}{
		{name: "not advertised", contract: nil, refused: 1},
		{name: "advertised", contract: advertised, sent: true},
		{name: "contract read meets a 5xx", contract: coreFault(), kept: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &replayFake{}
			root := replayWorkspace(t, f)
			keep(t, "process", "findings", "disposition", "--id", "F-K-1", "--disposition", "RESOLVED",
				"--resolution", "packet-edit", "--expected-fingerprint", "fp-f-1")
			f.contract = tc.contract
			serverBack(t, root)

			_, out := runCLI(t, "outage", "replay")

			sent := false
			for _, p := range f.posts() {
				if p.path == "/api/v1/sync/author" && str(obj(p.body, "record"), "external_id") == "F-K-1" {
					sent = true
				}
			}
			if sent != tc.sent {
				t.Errorf("sent %v, want %v:\n%s", sent, tc.sent, out)
			}
			refused := asideEntries(t, root, "refused")
			if len(refused) != tc.refused {
				t.Errorf("refused %d, want %d:\n%s", len(refused), tc.refused, out)
			}
			if tc.refused > 0 && !strings.Contains(str(refused[0].entry, "reason"), "finding_resolution") {
				t.Errorf("the refusal names the capability, got %v", refused[0].entry)
			}
			if n := len(keptEntries(t, root)); n != tc.kept {
				t.Errorf("kept %d, want %d:\n%s", n, tc.kept, out)
			}
		})
	}
}

// REQ-CROSS-535 (CODE-OUTAGE-4-3) — a probe that gets an answer, even one with
// an empty body, is not an outage.
func TestAnsweredProbeIsNotAnOutage(t *testing.T) {
	srv, _ := storeFake(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
		gatewayText(http.StatusServiceUnavailable))
	root := outageWorkspace(t, srv.URL)

	code, out := runCLI(t, "factory", "gates")

	if code == exitTempFail || strings.Contains(out, "unavailable since") {
		t.Errorf("an answered probe confirms no outage; exit %d:\n%s", code, out)
	}
	if state := readAvailability(t, root); state != nil {
		t.Errorf("no availability state, got %v", state)
	}
}

// REQ-CROSS-535 AC1/AC5 (CODE-OUTAGE-4-3) — every transport failure counts,
// whatever its cause's type; a URL that does not parse or names no usable
// scheme does not.
func TestNetworkFailureClasses(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{&url.Error{Op: "Get", URL: "http://h/x", Err: errors.New("http: server closed idle connection")}, true},
		{&url.Error{Op: "Get", URL: "http://h/x", Err: errors.New("http2: client connection lost")}, true},
		{&url.Error{Op: "parse", URL: "127.0.0.1:9/x", Err: errors.New("first path segment in URL cannot contain colon")}, false},
		{&url.Error{Op: "Get", URL: "ftp://h/x", Err: errors.New(`unsupported protocol scheme "ftp"`)}, false},
		{errors.New("decode systems: unexpected EOF"), false},
	} {
		if got := isNetworkFailure(tc.err); got != tc.want {
			t.Errorf("isNetworkFailure(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

// REQ-CROSS-539 AC6/AC7 (CODE-OUTAGE-4-1, round 2) — a kept push whose own
// write meets an outage stops the replay; one whose write gets no answer is
// kept for another try. Neither is refused.
func TestKeptPushWriteFailureStaysKept(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outage  bool
		attempt float64
	}{{name: "an outage", outage: true, attempt: 0}, {name: "no answer", outage: false, attempt: 1}} {
		t.Run(tc.name, func(t *testing.T) {
			root, _, _ := outagePushWorkspace(t)
			var writing atomic.Bool
			outagePushFront = func(w http.ResponseWriter, r *http.Request) bool {
				if r.Method == "POST" && r.URL.Path == "/api/v1/sync/author" {
					writing.Store(true)
					if tc.outage {
						gatewayText(http.StatusServiceUnavailable)(w, r)
					} else {
						time.Sleep(600 * time.Millisecond)
						w.WriteHeader(http.StatusOK)
					}
					return true
				}
				if tc.outage && r.URL.Path == "/api/systems" && writing.Load() {
					gatewayText(http.StatusServiceUnavailable)(w, r)
					return true
				}
				return false
			}
			t.Cleanup(func() { outagePushFront = nil })
			if !tc.outage {
				scaleOutageTimeouts(t, 500*time.Millisecond, 200*time.Millisecond)
			}
			serverBack(t, root)

			_, out := runCLI(t, "outage", "replay")

			for _, sub := range []string{"conflicted", "refused"} {
				if n := len(asideEntries(t, root, sub)); n != 0 {
					t.Errorf("a push whose write failed without an answer is not %s; %d there:\n%s", sub, n, out)
				}
			}
			kept := keptEntries(t, root)
			if len(kept) != 1 {
				t.Fatalf("the kept push stays kept; %d kept:\n%s", len(kept), out)
			}
			if got, _ := kept[0].entry["attempts"].(float64); got != tc.attempt {
				t.Errorf("attempts %v, want %v", got, tc.attempt)
			}
		})
	}
}
