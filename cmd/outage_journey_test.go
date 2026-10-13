package cmd

// UR-CROSS-534 (EPIC-CLI-OUTAGE) upper RED — development continues while the
// ModernPath server is down, and the record catches up when it is back. One
// workspace, one fake store at one address, switched down (nothing listens)
// and up again; every step runs through run(args) as the installed binary does.

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type journeyRequest struct {
	method, path string
	body         map[string]any
}

// journeyStore is the fake store once it is back: it serves the gate the
// developer answered offline, accepts writes, and records every store request
// in arrival order (the probe and the contract read are not recorded).
type journeyStore struct {
	mu       sync.Mutex
	requests []journeyRequest
	cfp, sfp string
}

func (s *journeyStore) recorded() []journeyRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]journeyRequest(nil), s.requests...)
}

func (s *journeyStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/systems":
		healthyProbe(w, r)
		return
	case "/api/v1/sync/contract":
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("x-modernpath-store-revision", "rev-up")
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	s.requests = append(s.requests, journeyRequest{r.Method, r.URL.Path, body})
	s.mu.Unlock()

	reply := func(data map[string]any) { _ = json.NewEncoder(w).Encode(map[string]any{"data": data}) }
	switch {
	case r.Method == "GET" && r.URL.Path == "/api/v1/sync/gates/ENTRY-EPIC-J":
		reply(map[string]any{"gate": map[string]any{
			"external_id": "ENTRY-EPIC-J", "state": "open", "purpose": "entry",
			"answer_readiness":            map[string]any{"requires_review": true},
			"content_fingerprint":         s.cfp,
			"evaluated_scope_fingerprint": s.sfp,
		}})
	case r.Method == "POST" && r.URL.Path == "/api/v1/sync/gates/ENTRY-EPIC-J/answer":
		reply(map[string]any{"gate": map[string]any{
			"external_id": "ENTRY-EPIC-J", "answer": body["answer"], "source_tag": body["source_tag"],
		}})
	case r.Method == "POST" && r.URL.Path == "/api/v1/sync/evidence":
		reply(map[string]any{"result": "created", "run": map[string]any{"external_id": body["external_id"]}})
	case r.Method == "POST" && r.URL.Path == "/api/v1/sync/author":
		reply(map[string]any{"backlog": map[string]any{"external_id": "BACKLOG-J-1", "fingerprint": strings.Repeat("b", 64)}})
	case r.URL.Path == "/api/v1/sync/gates":
		reply(map[string]any{"gates": []any{}})
	case r.URL.Path == "/api/v1/feed":
		reply(map[string]any{"items": []any{}})
	default:
		reply(map[string]any{})
	}
}

// journeyWorkspace is a store-backed git workspace whose last pull left a
// snapshot of the open entry gate (both review pins) and of the decision queue.
func journeyWorkspace(t *testing.T, apiURL, cfp, sfp string, snapshotAt time.Time) string {
	t.Helper()
	root := outageWorkspace(t, apiURL)
	gitRun(t, root, "init", "-q", "-b", "main")
	gitRun(t, root, "-c", "user.email=dev@example.com", "-c", "user.name=Dev", "commit", "-q", "--allow-empty", "-m", "init")
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("process/store-backed.md", "# Store-backed\n")
	at := snapshotAt.UTC().Format(time.RFC3339)
	write(".modernpath/working-set/ENTRY-EPIC-J.md", "# ENTRY-EPIC-J — working-set snapshot\n\n"+
		"- **Snapshot at:** "+at+"\n"+
		"- **Source store/revision:** "+apiURL+" · system 1 · store rev-down\n\n"+
		"## GATE ENTRY-EPIC-J — Enter EPIC-J\n\n"+
		"- **Kind:** human / approval_request\n"+
		"- **State:** OPEN\n"+
		"- **Actor / evaluator:** opener human / answerer —\n"+
		"- **Prerequisites:** none\n"+
		"- **Fingerprint:** —\n"+
		"- **Content fingerprint:** "+cfp+"\n"+
		"- **Scope fingerprint:** "+sfp+"\n"+
		"- **Application:** —\n"+
		"- **Predecessor / successor:** — / —\n")
	write(yourMoveDir+"/GATES.md", "# Your move — open human decisions\n\n"+
		"- **Snapshot at:** "+at+"\n"+
		"- **Source store/revision:** "+apiURL+" · system 1 · store rev-down\n\n"+
		"1 open gate(s). Only `OPEN` human gates appear here; this file is a\nregenerated projection, never an authority.\n\n"+
		"## ENTRY-EPIC-J — Enter EPIC-J\n\n- **Kind:** approval_request\n")
	return root
}

func TestOutageJourney(t *testing.T) {
	scaleOutageTimeouts(t, 200*time.Millisecond, 2*time.Second)
	cfp, sfp := strings.Repeat("c", 64), strings.Repeat("5", 64)

	// One address for the whole journey: down while nothing listens on it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	apiURL := "http://" + addr
	root := journeyWorkspace(t, apiURL, cfp, sfp, time.Now().Add(-2*time.Hour))

	// --- down ---------------------------------------------------------------
	// C1/C2: each command says so quickly, keeps the write, and the session
	// continues (exit 0).
	timed := func(args ...string) (int, string, time.Duration) {
		start := time.Now()
		code, out := runCLI(t, args...)
		return code, out, time.Since(start)
	}

	code, out, _ := timed("factory", "evidence", "--pass", "REQ-J-1", "--log", "go test ./...")
	if code != 0 || !strings.Contains(strings.ToLower(out), "kept") {
		t.Fatalf("down: factory evidence must keep the run on this machine and exit 0; exit %d:\n%s", code, out)
	}

	code, out, took := timed("factory", "answer", "ENTRY-EPIC-J", "--text", "approve", "--options", "approve", "--source", "USER:2026-10-04:journey")
	if code != 0 || !strings.Contains(strings.ToLower(out), "kept") {
		t.Fatalf("down: factory answer must keep the answer with the snapshot's pins and exit 0; exit %d:\n%s", code, out)
	}
	if took > time.Second {
		t.Errorf("down: a known outage must answer within 1 s, factory answer took %s", took)
	}

	code, out, took = timed("your-move")
	if code != 0 {
		t.Fatalf("down: your-move must answer from the snapshot and exit 0; exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "From the snapshot of") || !strings.Contains(out, "ENTRY-EPIC-J") {
		t.Errorf("down: your-move must print the GATES.md snapshot under its age line:\n%s", out)
	}
	if took > time.Second {
		t.Errorf("down: a known outage must answer within 1 s, your-move took %s", took)
	}
	downEnded := time.Now()

	// --- up -----------------------------------------------------------------
	store := &journeyStore{cfp: cfp, sfp: sfp}
	up, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("reopen %s: %v", addr, err)
	}
	srv := httptest.NewUnstartedServer(store)
	_ = srv.Listener.Close()
	srv.Listener = up
	srv.Start()
	t.Cleanup(srv.Close)
	// Time passes: the outage record ages past the window in which nothing
	// calls the server (REQ-CROSS-536), so the next command asks it again.
	serverBack(t, root)

	// A read never replays (C3, D8).
	code, out = runCLI(t, "your-move")
	if code != 0 {
		t.Fatalf("up: your-move exit %d:\n%s", code, out)
	}
	for _, r := range store.recorded() {
		if r.method == "POST" {
			t.Fatalf("up: your-move must send no kept write, it sent %s %s", r.method, r.path)
		}
	}

	// The next write replays both kept entries, in order, before its own request.
	before := len(store.recorded())
	code, out = runCLI(t, "author", "backlog", "BACKLOG-J-1", "--kind", "backlog", "--title", "A discovery",
		"--observed", "seen during the outage", "--why-unrouted", "owner unclear")
	if code != 0 {
		t.Fatalf("up: author backlog exit %d:\n%s", code, out)
	}
	var posts []journeyRequest
	for _, r := range store.recorded()[before:] {
		if r.method == "POST" {
			posts = append(posts, r)
		}
	}
	paths := make([]string, len(posts))
	for i, p := range posts {
		paths[i] = p.path
	}
	wantPaths := []string{"/api/v1/sync/evidence", "/api/v1/sync/gates/ENTRY-EPIC-J/answer", "/api/v1/sync/author"}
	if strings.Join(paths, " ") != strings.Join(wantPaths, " ") {
		t.Fatalf("up: want the kept evidence, then the kept answer, then the write's own request; posted %v\n%s", paths, out)
	}

	evidence := posts[0].body
	if id, _ := evidence["external_id"].(string); !strings.HasPrefix(id, "RUN-") {
		t.Errorf("the kept run must replay with its run id, got %v", evidence["external_id"])
	}
	ranAt, err := time.Parse(time.RFC3339, str(evidence, "ran_at"))
	if err != nil || ranAt.After(downEnded) {
		t.Errorf("the kept run must replay with the ran_at fixed while down (before %s), got %q", downEnded.Format(time.RFC3339), str(evidence, "ran_at"))
	}
	if str(evidence, "branch") != "main" {
		t.Errorf("the kept run must carry the branch it ran on, got %q", str(evidence, "branch"))
	}

	answer := posts[1].body
	if answer["answer"] != "approve" || answer["source_tag"] != "USER:2026-10-04:journey" {
		t.Errorf("the kept answer must replay its text and USER: source, got %v", answer)
	}
	review, _ := answer["review"].(map[string]any)
	if str(review, "content_fingerprint") != cfp || str(review, "evaluated_scope_fingerprint") != sfp {
		t.Errorf("the kept answer must replay the snapshot's review pins, got %v", answer["review"])
	}

	if record, _ := posts[2].body["record"].(map[string]any); str(record, "external_id") != "BACKLOG-J-1" {
		t.Errorf("the write's own request must follow the replay, got %v", posts[2].body)
	}
}
