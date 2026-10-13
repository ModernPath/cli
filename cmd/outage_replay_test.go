package cmd

// REQ-CROSS-539 (EPIC-CLI-OUTAGE) — when the server is back, the kept writes
// are recorded in order and conflicts are reported, never forced. Every case
// keeps writes through run(args) while the server is known to be unavailable,
// lets the outage record age past the fresh window, and drives the next write
// command or `outage replay` against a recording fake store. It asserts only
// the exit code, the printed text, the journal files and the requests the
// fake saw.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// replayReply is one fake answer: a JSON body, or a plain-text one when text
// is set (an edge's answer, with no store header).
type replayReply struct {
	status int
	body   any
	text   string
	header map[string]string
}

// replayFake is a store that records every request except the probe and the
// contract read. on answers first; nil falls through to the defaults. While
// down, every request, the probe included, gets the gateway's plain 503.
type replayFake struct {
	mu       sync.Mutex
	requests []journeyRequest
	down     bool
	gates    map[string]map[string]any
	items    map[string]map[string]any // external id → {"kind": …, "item": …}
	on       func(f *replayFake, r journeyRequest, q url.Values) *replayReply
	// contract, when set, is the sync contract read's answer; nil is a 404.
	contract *replayReply
}

func (f *replayFake) setDown(down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.down = down
}

func (f *replayFake) recorded() []journeyRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]journeyRequest(nil), f.requests...)
}

// posts are the recorded writes, in arrival order.
func (f *replayFake) posts() []journeyRequest {
	var out []journeyRequest
	for _, r := range f.recorded() {
		if r.method != "GET" {
			out = append(out, r)
		}
	}
	return out
}

func (f *replayFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	down := f.down
	f.mu.Unlock()
	if down {
		gatewayText(http.StatusServiceUnavailable)(w, r)
		return
	}
	switch r.URL.Path {
	case "/api/systems":
		healthyProbe(w, r)
		return
	case "/api/v1/sync/contract":
		if f.contract == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(firstStatus(f.contract.status))
		_ = json.NewEncoder(w).Encode(f.contract.body)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	req := journeyRequest{r.Method, r.URL.Path, body}
	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.mu.Unlock()

	reply := (*replayReply)(nil)
	if f.on != nil {
		reply = f.on(f, req, r.URL.Query())
	}
	if reply == nil {
		reply = f.defaultReply(req, r.URL.Query())
	}
	for k, v := range reply.header {
		w.Header().Set(k, v)
	}
	if reply.text != "" {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(reply.status)
		_, _ = w.Write([]byte(reply.text))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("x-modernpath-store-revision", "rev-back")
	w.WriteHeader(firstStatus(reply.status))
	_ = json.NewEncoder(w).Encode(reply.body)
}

func firstStatus(s int) int {
	if s == 0 {
		return http.StatusOK
	}
	return s
}

func data(m map[string]any) map[string]any { return map[string]any{"data": m} }

func (f *replayFake) defaultReply(r journeyRequest, q url.Values) *replayReply {
	switch {
	case r.method == "GET" && strings.HasPrefix(r.path, "/api/v1/sync/gates/"):
		id := strings.TrimPrefix(r.path, "/api/v1/sync/gates/")
		if g, ok := f.gates[id]; ok {
			return &replayReply{body: data(map[string]any{"gate": g})}
		}
		return &replayReply{status: 404, body: map[string]any{"error": map[string]any{"message": "gate not found"}}}
	case r.method == "POST" && strings.HasSuffix(r.path, "/answer"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.path, "/api/v1/sync/gates/"), "/answer")
		return &replayReply{body: data(map[string]any{"gate": map[string]any{
			"external_id": id, "state": "answered", "answer": r.body["answer"],
			"chosen_option_keys": r.body["chosen_option_keys"], "source_tag": r.body["source_tag"],
		}})}
	case r.method == "POST" && r.path == "/api/v1/sync/evidence":
		return &replayReply{body: data(map[string]any{"result": "created", "run": map[string]any{"external_id": r.body["external_id"]}})}
	case r.method == "POST" && r.path == "/api/v1/sync/author":
		rec := obj(r.body, "record")
		return &replayReply{body: data(map[string]any{str(rec, "kind"): map[string]any{
			"external_id": str(rec, "external_id"), "fingerprint": "fp-after-" + str(rec, "external_id"),
		}})}
	case r.method == "GET" && r.path == "/api/v1/sync/items":
		items := []any{}
		for _, id := range q["ids[]"] {
			if it, ok := f.items[id]; ok {
				items = append(items, map[string]any{"kind": it["kind"], "item": it["item"], "gates": []any{}})
			}
		}
		return &replayReply{body: data(map[string]any{"items": items})}
	case r.path == "/api/v1/sync/gates":
		return &replayReply{body: data(map[string]any{"gates": []any{}})}
	case r.path == "/api/v1/feed":
		return &replayReply{body: data(map[string]any{"items": []any{}})}
	}
	return &replayReply{body: data(map[string]any{})}
}

// conflict409 is core's answer to a write whose basis moved.
func conflict409(reason string) *replayReply {
	return &replayReply{status: 409, body: map[string]any{"error": map[string]any{"reason": reason}}}
}

// replayWorkspace is a git workspace bound to the fake, signed in as the
// person who keeps the writes, with an outage checked just now: writes are
// kept and nothing reaches the fake until serverBack.
func replayWorkspace(t *testing.T, f *replayFake) string {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return replayWorkspaceAt(t, srv.URL)
}

func replayWorkspaceAt(t *testing.T, apiURL string) string {
	t.Helper()
	scaleOutageTimeouts(t, 500*time.Millisecond, 3*time.Second)
	root := outageWorkspace(t, apiURL)
	gitRun(t, root, "init", "-q", "-b", "main")
	gitRun(t, root, "-c", "user.email=dev@example.com", "-c", "user.name=Dev", "commit", "-q", "--allow-empty", "-m", "init")
	signedInAs(t, root, map[string]any{"sub": "user-539", "email": "dev@example.com"})
	freshUnavailable(t, root)
	return root
}

// keep runs a keeping verb during the outage and asserts it was kept.
func keep(t *testing.T, args ...string) {
	t.Helper()
	code, out := runCLI(t, args...)
	if code != 0 || !strings.Contains(out, "Kept on this machine") {
		t.Fatalf("%v must be kept on this machine; exit %d:\n%s", args, code, out)
	}
}

// serverBack ages the outage record past the window in which nothing calls
// the server (REQ-CROSS-536), as time passing does: the next command asks
// the server again.
func serverBack(t *testing.T, root string) {
	t.Helper()
	st := readAvailability(t, root)
	if st == nil {
		return
	}
	st["last_checked"] = time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)
	st["next_check"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(availabilityPath(root), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// asideEntries reads the entries a replay moved to conflicted/ or refused/.
func asideEntries(t *testing.T, root, sub string) []keptFile {
	t.Helper()
	dir := filepath.Join(journalPath(root), sub)
	dirents, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []keptFile
	for _, d := range dirents {
		if !strings.HasSuffix(d.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, d.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var e map[string]any
		if err := json.Unmarshal(raw, &e); err != nil {
			t.Fatalf("%s/%s is not JSON: %v", sub, d.Name(), err)
		}
		out = append(out, keptFile{d.Name(), e})
	}
	return out
}

func postPaths(posts []journeyRequest) []string {
	out := make([]string, len(posts))
	for i, p := range posts {
		out[i] = p.path
		if rec := obj(p.body, "record"); rec != nil {
			out[i] += " " + str(rec, "external_id")
		}
	}
	return out
}

const ownWrite = "BACKLOG-OWN-1"

// ownBacklog is the next write command: its own request is the author post
// of BACKLOG-OWN-1.
func ownBacklog(t *testing.T) (int, string) {
	t.Helper()
	return runCLI(t, "author", "backlog", ownWrite, "--kind", "backlog", "--title", "Own write",
		"--observed", "seen after the outage", "--why-unrouted", "owner unclear")
}

func governedGate(id, cfp, sfp string) map[string]any {
	return map[string]any{"external_id": id, "state": "open", "purpose": "entry",
		"answer_readiness":    map[string]any{"requires_review": true},
		"content_fingerprint": cfp, "evaluated_scope_fingerprint": sfp}
}

func questionGate(id, cfp string) map[string]any {
	return map[string]any{"external_id": id, "state": "open", "purpose": "question",
		"answer_readiness":    map[string]any{"requires_review": false},
		"content_fingerprint": cfp}
}

var (
	pinC = strings.Repeat("c", 64)
	pinS = strings.Repeat("5", 64)
	pinD = strings.Repeat("d", 64)
)

// AC1 — the next write command, or `outage replay`, sends the kept entries in
// sequence order before its own request, with their fixed values and kept
// pins, and removes each from the journal when recorded.
func TestReplayInOrderBeforeOwnRequest(t *testing.T) {
	prepare := func(t *testing.T) (string, *replayFake) {
		f := &replayFake{gates: map[string]map[string]any{
			"ENTRY-EPIC-R": governedGate("ENTRY-EPIC-R", pinC, pinS),
			"Q-R-1":        questionGate("Q-R-1", pinD),
		}}
		root := replayWorkspace(t, f)
		gateSnapshot(t, root, ".modernpath/working-set/ENTRY-EPIC-R.md", "ENTRY-EPIC-R", "approval_request", pinC, pinS, time.Now().Add(-time.Hour))
		gateSnapshot(t, root, ".modernpath/working-set/Q-R-1.md", "Q-R-1", "question", pinD, "", time.Now().Add(-time.Hour))
		keep(t, "factory", "evidence", "--pass", "REQ-R-1", "--log", "go test ./...")
		keep(t, "factory", "answer", "ENTRY-EPIC-R", "--text", "approve", "--options", "approve", "--source", "USER:2026-10-04:r")
		keep(t, "author", "update", "REQ-R-2", "--title", "New title", "--expected-fingerprint", "fp-1")
		keep(t, "factory", "answer", "Q-R-1", "--text", "yes", "--source", "USER:2026-10-04:q")
		if n := len(f.recorded()); n != 0 {
			t.Fatalf("%d request(s) reached the fake while it was known to be unavailable", n)
		}
		serverBack(t, root)
		return root, f
	}
	checkReplayed := func(t *testing.T, root string, f *replayFake, posts []journeyRequest, keptRun string) {
		t.Helper()
		want := []string{"/api/v1/sync/evidence", "/api/v1/sync/gates/ENTRY-EPIC-R/answer", "/api/v1/sync/author REQ-R-2", "/api/v1/sync/gates/Q-R-1/answer"}
		got := postPaths(posts)
		if len(got) < len(want) || strings.Join(got[:len(want)], " | ") != strings.Join(want, " | ") {
			t.Fatalf("the kept writes must be sent in sequence order; posted %v", got)
		}
		if str(posts[0].body, "external_id") != keptRun {
			t.Errorf("the kept run must replay with its fixed run id %s, got %v", keptRun, posts[0].body["external_id"])
		}
		answer := posts[1].body
		if answer["answer"] != "approve" || answer["source_tag"] != "USER:2026-10-04:r" {
			t.Errorf("the kept answer must replay its text and USER: source, got %v", answer)
		}
		review := obj(answer, "review")
		if str(review, "content_fingerprint") != pinC || str(review, "evaluated_scope_fingerprint") != pinS {
			t.Errorf("a governed answer must carry its kept review pins, got %v", answer["review"])
		}
		update := obj(posts[2].body, "record")
		if str(update, "expected_fingerprint") != "fp-1" || str(update, "title") != "New title" {
			t.Errorf("the kept edit must replay on its kept fingerprint with its values, got %v", update)
		}
		if _, has := posts[3].body["review"]; has {
			t.Errorf("an answer to a gate that is not governed carries no review, got %v", posts[3].body)
		}
		if n := len(keptEntries(t, root)); n != 0 {
			t.Errorf("each recorded entry is removed from the journal; %d remain", n)
		}
		for _, sub := range []string{"conflicted", "refused"} {
			if n := len(asideEntries(t, root, sub)); n != 0 {
				t.Errorf("nothing conflicted or was refused; %d entr(ies) in %s/", n, sub)
			}
		}
	}

	t.Run("the next write command", func(t *testing.T) {
		root, f := prepare(t)
		keptRun := str(list(obj(keptEntries(t, root)[0].entry, "fixed"), "runs")[0].(map[string]any), "run_id")

		code, out := ownBacklog(t)

		if code != 0 {
			t.Fatalf("the write command exits 0 after the replay; exit %d:\n%s", code, out)
		}
		posts := f.posts()
		checkReplayed(t, root, f, posts, keptRun)
		if len(posts) != 5 || str(obj(posts[4].body, "record"), "external_id") != ownWrite {
			t.Errorf("the command's own request follows the replay; posted %v", postPaths(posts))
		}
		if !strings.Contains(out, "recorded 4") {
			t.Errorf("the summary says what was recorded:\n%s", out)
		}
	})

	t.Run("outage replay", func(t *testing.T) {
		root, f := prepare(t)
		keptRun := str(list(obj(keptEntries(t, root)[0].entry, "fixed"), "runs")[0].(map[string]any), "run_id")

		code, out := runCLI(t, "outage", "replay")

		if code != 0 {
			t.Fatalf("outage replay exits 0; exit %d:\n%s", code, out)
		}
		posts := f.posts()
		checkReplayed(t, root, f, posts, keptRun)
		if len(posts) != 4 {
			t.Errorf("outage replay sends only the kept writes; posted %v", postPaths(posts))
		}
		if !strings.Contains(out, "recorded 4") {
			t.Errorf("the summary says what was recorded:\n%s", out)
		}
	})
}

// AC2 — a read verb or a hook sends no kept entry. A regression guard: it
// passes before replay exists.
func TestReadsAndHooksDoNotReplay(t *testing.T) {
	f := &replayFake{}
	root := replayWorkspace(t, f)
	keep(t, "author", "update", "REQ-R-2", "--title", "New title", "--expected-fingerprint", "fp-1")
	serverBack(t, root)

	for _, args := range [][]string{
		{"your-move"},
		{"process", "next"},
		{"working-set", "pull", "REQ-R-2"},
		{"outage", "status"},
	} {
		runCLI(t, args...)
	}
	runCLIWithStdin(t, `{"session_id":"replay","hook_event_name":"SessionStart","source":"startup"}`,
		"your-move", "--hook", "SessionStart", "--deadline", "5")

	if posts := f.posts(); len(posts) != 0 {
		t.Errorf("reads and hooks send no kept write; posted %v", postPaths(posts))
	}
	if n := len(keptEntries(t, root)); n != 1 {
		t.Errorf("the kept write stays kept; %d entr(ies)", n)
	}
}

// AC3 — an entry that already landed counts as recorded, and nothing is
// written twice.
func TestAlreadyLandedCountsAsRecorded(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, root string, f *replayFake)
		post    string // the one write the entry may send
	}{
		{
			name: "evidence resent with its run id",
			prepare: func(t *testing.T, root string, f *replayFake) {
				keep(t, "factory", "evidence", "--pass", "REQ-R-1")
				f.on = func(_ *replayFake, r journeyRequest, _ url.Values) *replayReply {
					if r.path == "/api/v1/sync/evidence" {
						return &replayReply{body: data(map[string]any{"result": "updated", "run": map[string]any{"external_id": r.body["external_id"]}})}
					}
					return nil
				}
			},
			post: "/api/v1/sync/evidence",
		},
		{
			name: "answer whose first-wins winner has the same answer, options and source",
			prepare: func(t *testing.T, root string, f *replayFake) {
				gateSnapshot(t, root, ".modernpath/working-set/ENTRY-EPIC-R.md", "ENTRY-EPIC-R", "approval_request", pinC, pinS, time.Now().Add(-time.Hour))
				keep(t, "factory", "answer", "ENTRY-EPIC-R", "--text", "approve", "--options", "approve", "--source", "USER:2026-10-04:r")
				gate := governedGate("ENTRY-EPIC-R", pinC, pinS)
				gate["state"] = "answered"
				f.gates = map[string]map[string]any{"ENTRY-EPIC-R": gate}
				f.on = func(_ *replayFake, r journeyRequest, _ url.Values) *replayReply {
					if strings.HasSuffix(r.path, "/answer") {
						return &replayReply{status: 409, body: map[string]any{"error": map[string]any{
							"message": "already answered (first-wins)",
							"winner": map[string]any{"external_id": "ENTRY-EPIC-R", "answer": "approve",
								"chosen_option_keys": []any{"approve"}, "source_tag": "USER:2026-10-04:r"},
						}}}
					}
					return nil
				}
			},
			post: "/api/v1/sync/gates/ENTRY-EPIC-R/answer",
		},
		{
			name: "edit whose record already holds the kept values",
			prepare: func(t *testing.T, root string, f *replayFake) {
				keep(t, "author", "update", "REQ-R-2", "--title", "New title", "--expected-fingerprint", "fp-1")
				f.items = map[string]map[string]any{"REQ-R-2": {"kind": "system", "item": map[string]any{
					"external_id": "REQ-R-2", "title": "New title", "fingerprint": "fp-2"}}}
				f.on = func(_ *replayFake, r journeyRequest, _ url.Values) *replayReply {
					if r.path == "/api/v1/sync/author" {
						return conflict409("stale_fingerprint")
					}
					return nil
				}
			},
			post: "/api/v1/sync/author",
		},
		{
			name: "create whose record already holds the kept values",
			prepare: func(t *testing.T, root string, f *replayFake) {
				keep(t, "author", "backlog", "BACKLOG-R-1", "--kind", "backlog", "--title", "A discovery",
					"--observed", "seen during the outage", "--why-unrouted", "owner unclear")
				// The served shape: the store answers a backlog record's kind as
				// `kind` (Core.Sync.Backlog, workingset_render.go renderBacklogBody),
				// though `author backlog` sends it as `backlog_kind`.
				f.items = map[string]map[string]any{"BACKLOG-R-1": {"kind": "backlog", "item": map[string]any{
					"external_id": "BACKLOG-R-1", "kind": "backlog", "title": "A discovery",
					"observation": "seen during the outage", "why_unrouted": "owner unclear", "fingerprint": "fp-b"}}}
				f.on = func(_ *replayFake, r journeyRequest, _ url.Values) *replayReply {
					if r.path == "/api/v1/sync/author" {
						return conflict409("already_exists")
					}
					return nil
				}
			},
			post: "/api/v1/sync/author",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &replayFake{}
			root := replayWorkspace(t, f)
			tc.prepare(t, root, f)
			serverBack(t, root)

			code, out := runCLI(t, "outage", "replay")

			if code != 0 {
				t.Fatalf("exit %d, want 0:\n%s", code, out)
			}
			posts := f.posts()
			if len(posts) != 1 || posts[0].path != tc.post {
				t.Errorf("want the one kept write sent once (%s), posted %v", tc.post, postPaths(posts))
			}
			if n := len(keptEntries(t, root)); n != 0 {
				t.Errorf("an entry that already landed counts as recorded and is removed; %d remain", n)
			}
			for _, sub := range []string{"conflicted", "refused"} {
				if n := len(asideEntries(t, root, sub)); n != 0 {
					t.Errorf("an entry that already landed is not %s; %d there", sub, n)
				}
			}
			if !strings.Contains(out, "already there 1") {
				t.Errorf("the summary counts it as already there:\n%s", out)
			}
		})
	}

	t.Run("kept push whose files the store already holds", func(t *testing.T) {
		root, fx, _ := outagePushWorkspace(t)
		fx.requirements[0].(map[string]any)["boundary"] = "the reads and writes"
		serverBack(t, root)

		code, out := runCLI(t, "outage", "replay")

		if code != 0 {
			t.Fatalf("exit %d, want 0:\n%s", code, out)
		}
		for _, p := range fx.authorPosts {
			t.Errorf("a push whose files the store already holds writes nothing, posted %v", p)
		}
		if n := len(keptEntries(t, root)); n != 0 {
			t.Errorf("the kept push counts as recorded and is removed; %d remain", n)
		}
		if !strings.Contains(fx.lastSelectGet, "scope=EPIC-CLI-008") {
			t.Errorf("the kept push names its piece in the selection read, got %q", fx.lastSelectGet)
		}
	})
}

// outagePushFront, when set, answers a request to the push workspace's store
// first; true means it answered.
var outagePushFront func(w http.ResponseWriter, r *http.Request) bool

// pushSelection overrides the selection read; nil serves the fixture's.
type pushSelection func(scope string) *replayReply

// pushWorkspace pulls the scope fixture's EPIC-CLI-008 online, edits a member
// while the server is unavailable and keeps the push with --piece. The store
// is the working-set fixture behind a front that answers the probe and lets a
// test change the selection read.
func outagePushWorkspace(t *testing.T) (string, *wsFixture, *pushSelection) {
	t.Helper()
	fx := scopeFixture()
	backend := wsServe(t, fx)
	target, _ := url.Parse(backend.URL)
	proxy := httputil.NewSingleHostReverseProxy(target)
	var selection pushSelection
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if outagePushFront != nil && outagePushFront(w, r) {
			return
		}
		switch {
		case r.URL.Path == "/api/systems":
			healthyProbe(w, r)
			return
		case r.URL.Path == "/api/v1/sync/work-selection" && r.Method == "GET" && selection != nil:
			fx.lastSelectGet = r.URL.RawQuery
			reply := selection(r.URL.Query().Get("scope"))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(firstStatus(reply.status))
			_ = json.NewEncoder(w).Encode(reply.body)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(front.Close)

	root := replayWorkspaceAt(t, front.URL)
	if err := os.Remove(availabilityPath(root)); err != nil {
		t.Fatal(err)
	}
	env := &factoryEnv{Root: root, APIURL: front.URL, SystemID: 1, token: "t"}
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull --scope: %v", err)
	}
	freshUnavailable(t, root)
	edit(t, memberPath(filepath.Join(root, ".modernpath", "working-set", "EPIC-CLI-008"), "REQ-CROSS-310"), func(s string) string {
		return strings.Replace(s, "the reads", "the reads and writes", 1)
	})
	keep(t, "working-set", "push", "--piece", "EPIC-CLI-008")
	fx.authorPosts = nil
	return root, fx, &selection
}

// AC4 — a kept entry that conflicts or is refused moves to the conflicted or
// refused list with the server's reason; entries waiting on it move with it
// (CODE-OUTAGE-1-2), other entries continue, and no write is re-sent with a
// newer fingerprint.
func TestConflictIsReportedNotForced(t *testing.T) {
	type outcome struct{ sub, reason string }
	cases := []struct {
		name    string
		prepare func(t *testing.T, root string, f *replayFake)
		want    outcome
		posts   []string // the writes the replay may send, in order
	}{
		{
			name: "an edit whose record moved, with a second edit waiting on it",
			prepare: func(t *testing.T, root string, f *replayFake) {
				keep(t, "author", "update", "REQ-R-2", "--title", "New title", "--expected-fingerprint", "fp-1")
				keep(t, "author", "update", "REQ-R-2", "--title", "Newer title", "--expected-fingerprint", "fp-1b")
				keep(t, "factory", "evidence", "--pass", "REQ-R-1")
				f.items = map[string]map[string]any{"REQ-R-2": {"kind": "system", "item": map[string]any{
					"external_id": "REQ-R-2", "title": "Someone else's title", "fingerprint": "fp-other"}}}
				f.on = func(_ *replayFake, r journeyRequest, _ url.Values) *replayReply {
					if r.path == "/api/v1/sync/author" {
						return conflict409("stale_fingerprint")
					}
					return nil
				}
			},
			want:  outcome{"conflicted", "stale_fingerprint"},
			posts: []string{"/api/v1/sync/author REQ-R-2", "/api/v1/sync/evidence"},
		},
		{
			name: "a create the server refuses",
			prepare: func(t *testing.T, root string, f *replayFake) {
				keep(t, "author", "backlog", "BACKLOG-R-1", "--kind", "backlog", "--title", "A discovery",
					"--observed", "o", "--why-unrouted", "w")
				keep(t, "factory", "evidence", "--pass", "REQ-R-1")
				f.on = func(_ *replayFake, r journeyRequest, _ url.Values) *replayReply {
					if r.path == "/api/v1/sync/author" {
						return &replayReply{status: 422, body: map[string]any{"error": map[string]any{
							"details": map[string]any{"title": []any{"is reserved"}}}}}
					}
					return nil
				}
			},
			want:  outcome{"refused", "is reserved"},
			posts: []string{"/api/v1/sync/author BACKLOG-R-1", "/api/v1/sync/evidence"},
		},
		{
			name: "a governed answer whose gate's pins moved",
			prepare: func(t *testing.T, root string, f *replayFake) {
				gateSnapshot(t, root, ".modernpath/working-set/ENTRY-EPIC-R.md", "ENTRY-EPIC-R", "approval_request", pinC, pinS, time.Now().Add(-time.Hour))
				keep(t, "factory", "answer", "ENTRY-EPIC-R", "--text", "approve", "--source", "USER:2026-10-04:r")
				keep(t, "factory", "evidence", "--pass", "REQ-R-1")
				f.gates = map[string]map[string]any{"ENTRY-EPIC-R": governedGate("ENTRY-EPIC-R", strings.Repeat("e", 64), pinS)}
				f.on = func(_ *replayFake, r journeyRequest, _ url.Values) *replayReply {
					if strings.HasSuffix(r.path, "/answer") {
						if str(obj(r.body, "review"), "content_fingerprint") != pinC {
							t.Errorf("the replay must send the kept pins, never the read's: %v", r.body["review"])
						}
						return &replayReply{status: 422, body: map[string]any{"error": map[string]any{
							"details": map[string]any{"review": []any{"stale: the gate changed since it was reviewed"}}}}}
					}
					return nil
				}
			},
			want:  outcome{"conflicted", "the gate changed while the server was unavailable"},
			posts: []string{"/api/v1/sync/gates/ENTRY-EPIC-R/answer", "/api/v1/sync/evidence"},
		},
		{
			name: "a governed answer kept without a scope pin",
			prepare: func(t *testing.T, root string, f *replayFake) {
				gateSnapshot(t, root, ".modernpath/working-set/ENTRY-EPIC-R.md", "ENTRY-EPIC-R", "approval_request", pinC, "", time.Now().Add(-time.Hour))
				keep(t, "factory", "answer", "ENTRY-EPIC-R", "--text", "approve", "--source", "USER:2026-10-04:r")
				keep(t, "factory", "evidence", "--pass", "REQ-R-1")
				f.gates = map[string]map[string]any{"ENTRY-EPIC-R": governedGate("ENTRY-EPIC-R", pinC, pinS)}
			},
			want:  outcome{"conflicted", "scope"},
			posts: []string{"/api/v1/sync/evidence"},
		},
		{
			name: "an answer to a gate that is not governed whose content moved",
			prepare: func(t *testing.T, root string, f *replayFake) {
				gateSnapshot(t, root, ".modernpath/working-set/Q-R-1.md", "Q-R-1", "question", pinD, "", time.Now().Add(-time.Hour))
				keep(t, "factory", "answer", "Q-R-1", "--text", "yes", "--source", "USER:2026-10-04:q")
				keep(t, "factory", "evidence", "--pass", "REQ-R-1")
				f.gates = map[string]map[string]any{"Q-R-1": questionGate("Q-R-1", strings.Repeat("f", 64))}
			},
			want:  outcome{"conflicted", "the gate changed while the server was unavailable"},
			posts: []string{"/api/v1/sync/evidence"},
		},
		{
			name: "an answer someone else gave first",
			prepare: func(t *testing.T, root string, f *replayFake) {
				gateSnapshot(t, root, ".modernpath/working-set/Q-R-1.md", "Q-R-1", "question", pinD, "", time.Now().Add(-time.Hour))
				keep(t, "factory", "answer", "Q-R-1", "--text", "yes", "--source", "USER:2026-10-04:q")
				keep(t, "factory", "evidence", "--pass", "REQ-R-1")
				f.gates = map[string]map[string]any{"Q-R-1": questionGate("Q-R-1", pinD)}
				f.on = func(_ *replayFake, r journeyRequest, _ url.Values) *replayReply {
					if strings.HasSuffix(r.path, "/answer") {
						return &replayReply{status: 409, body: map[string]any{"error": map[string]any{
							"message": "already answered (first-wins)",
							"winner":  map[string]any{"external_id": "Q-R-1", "answer": "no", "source_tag": "USER:2026-10-04:other"},
						}}}
					}
					return nil
				}
			},
			want:  outcome{"conflicted", "USER:2026-10-04:other"},
			posts: []string{"/api/v1/sync/gates/Q-R-1/answer", "/api/v1/sync/evidence"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &replayFake{}
			root := replayWorkspace(t, f)
			tc.prepare(t, root, f)
			kept := keptEntries(t, root)
			serverBack(t, root)

			code, out := runCLI(t, "outage", "replay")

			if code != 0 {
				t.Errorf("a replay that reported its conflicts exits 0; exit %d:\n%s", code, out)
			}
			if got := postPaths(f.posts()); strings.Join(got, " | ") != strings.Join(tc.posts, " | ") {
				t.Errorf("posted %v, want %v — nothing re-sent, later entries continue", got, tc.posts)
			}
			// CODE-OUTAGE-1-2: an entry waiting on the one moved aside moves
			// with it, to the same list, with a reason naming what it waited on.
			waiting := map[string]string{}
			for _, k := range kept {
				if w := str(k.entry, "waits_on"); w != "" {
					waiting[k.name] = w
				}
			}
			aside := asideEntries(t, root, tc.want.sub)
			if len(aside) != 1+len(waiting) || aside[0].name != kept[0].name {
				t.Fatalf("want %s and the %d entr(ies) waiting on it in %s/, found %v", kept[0].name, len(waiting), tc.want.sub, aside)
			}
			if reason := str(aside[0].entry, "reason"); !strings.Contains(reason, tc.want.reason) {
				t.Errorf("the entry carries the reason %q, got %q", tc.want.reason, reason)
			}
			for _, a := range aside[1:] {
				w, ok := waiting[a.name]
				if !ok {
					t.Errorf("only the entries waiting on it move with it; %s moved", a.name)
				} else if reason := str(a.entry, "reason"); !strings.Contains(reason, w) {
					t.Errorf("an entry moved with the one it waited on names %s in its reason, got %q", w, reason)
				}
			}
			for _, s := range []string{tc.want.reason, "modernpath outage status"} {
				if !strings.Contains(out, s) {
					t.Errorf("the summary must name %q:\n%s", s, out)
				}
			}
			for _, k := range keptEntries(t, root) {
				t.Errorf("no entry is stranded in the journal; %s remains", k.name)
			}
		})
	}

	t.Run("a kept push whose piece is no longer held, with another piece held", func(t *testing.T) {
		root, fx, selection := outagePushWorkspace(t)
		// Two pieces were held; EPIC-CLI-008 has been released since. Without
		// a piece the server asks for one; for the kept piece it holds none.
		*selection = func(scope string) *replayReply {
			if scope == "" {
				return &replayReply{status: 422, body: map[string]any{"error": "you hold 2 pieces — name one with --piece"}}
			}
			return &replayReply{body: data(map[string]any{"current": nil})}
		}
		serverBack(t, root)

		code, out := runCLI(t, "outage", "replay")

		if code != 0 {
			t.Errorf("exit %d, want 0:\n%s", code, out)
		}
		if len(fx.authorPosts) != 0 {
			t.Errorf("a push whose piece is no longer held writes nothing, posted %v", fx.authorPosts)
		}
		aside := asideEntries(t, root, "conflicted")
		if len(aside) != 1 || !strings.Contains(str(aside[0].entry, "reason"), "the kept piece is no longer held") {
			t.Fatalf("the kept push is conflicted because its piece is no longer held, found %v", aside)
		}
		if n := len(asideEntries(t, root, "refused")); n != 0 {
			t.Errorf("a piece no longer held is a conflict, not a refusal; %d refused", n)
		}
		if !strings.Contains(out, "kept push") {
			t.Errorf("the summary names the kept push:\n%s", out)
		}
	})
}

// AC5 — when the signed-in user is not the person who kept the entries,
// nothing is sent, the command names who must sign in, and its own request
// proceeds.
func TestReplayOnlyForKeeper(t *testing.T) {
	f := &replayFake{}
	root := replayWorkspace(t, f)
	keep(t, "factory", "evidence", "--pass", "REQ-R-1")
	keep(t, "author", "update", "REQ-R-2", "--title", "New title", "--expected-fingerprint", "fp-1")
	serverBack(t, root)
	signedInAs(t, root, map[string]any{"sub": "user-other", "email": "other@example.com"})

	code, out := ownBacklog(t)

	if code != 0 {
		t.Fatalf("the command's own request proceeds; exit %d:\n%s", code, out)
	}
	posts := f.posts()
	if len(posts) != 1 || str(obj(posts[0].body, "record"), "external_id") != ownWrite {
		t.Errorf("only the command's own request is sent; posted %v", postPaths(posts))
	}
	for _, s := range []string{"dev@example.com", "the person who kept these writes"} {
		if !strings.Contains(out, s) {
			t.Errorf("the command must name who must sign in (%q):\n%s", s, out)
		}
	}
	if n := len(keptEntries(t, root)); n != 2 {
		t.Errorf("the kept writes stay kept; %d entr(ies)", n)
	}
}

// AC6 — an outage or a 429 during replay stops it, leaving that entry and the
// rest kept; an entry resent after the CLI stopped before removing it counts
// as recorded.
func TestReplayStopsOnOutageAndResumes(t *testing.T) {
	keepThree := func(t *testing.T, f *replayFake) string {
		root := replayWorkspace(t, f)
		keep(t, "factory", "evidence", "--pass", "REQ-R-1")
		keep(t, "author", "update", "REQ-R-2", "--title", "New title", "--expected-fingerprint", "fp-1")
		keep(t, "author", "update", "REQ-R-3", "--title", "Other title", "--expected-fingerprint", "fp-3")
		serverBack(t, root)
		return root
	}

	t.Run("the server goes down after the first entry", func(t *testing.T) {
		f := &replayFake{}
		f.on = func(f *replayFake, r journeyRequest, _ url.Values) *replayReply {
			if r.path == "/api/v1/sync/evidence" {
				f.setDown(true)
			}
			return nil
		}
		root := keepThree(t, f)
		names := []string{keptEntries(t, root)[1].name, keptEntries(t, root)[2].name}

		code, out := runCLI(t, "outage", "replay")

		if code != exitTempFail {
			t.Errorf("a replay stopped by an outage exits %d, got %d:\n%s", exitTempFail, code, out)
		}
		left := keptEntries(t, root)
		if len(left) != 2 || left[0].name != names[0] || left[1].name != names[1] {
			t.Fatalf("the entry the outage stopped and the rest stay kept; found %v", left)
		}
		for _, sub := range []string{"conflicted", "refused"} {
			if n := len(asideEntries(t, root, sub)); n != 0 {
				t.Errorf("an outage moves nothing to %s/; %d there", sub, n)
			}
		}

		// Back again: the rest are recorded.
		f.setDown(false)
		f.on = nil
		serverBack(t, root)
		code, out = runCLI(t, "outage", "replay")
		if code != 0 {
			t.Fatalf("exit %d, want 0:\n%s", code, out)
		}
		if n := len(keptEntries(t, root)); n != 0 {
			t.Errorf("the rest are recorded when the server is back; %d remain", n)
		}
	})

	t.Run("the server limits requests on the second entry", func(t *testing.T) {
		waits := recordLimitWaits(t)
		f := &replayFake{}
		f.on = func(_ *replayFake, r journeyRequest, _ url.Values) *replayReply {
			if r.path == "/api/v1/sync/author" {
				return &replayReply{status: 429, text: "rate limited", header: map[string]string{"Retry-After": "30"}}
			}
			return nil
		}
		root := keepThree(t, f)

		code, out := runCLI(t, "outage", "replay")

		if code != exitTempFail {
			t.Errorf("a replay stopped by a 429 exits %d, got %d:\n%s", exitTempFail, code, out)
		}
		if n := len(keptEntries(t, root)); n != 2 {
			t.Errorf("entries 2 and 3 stay kept; %d remain", n)
		}
		for _, sub := range []string{"conflicted", "refused"} {
			if n := len(asideEntries(t, root, sub)); n != 0 {
				t.Errorf("a 429 moves nothing to %s/; %d there", sub, n)
			}
		}
		// REQ-CROSS-535 AC4: the limited entry is sent again after each
		// wait; nothing after it is sent.
		for _, p := range f.posts() {
			if str(obj(p.body, "record"), "external_id") == "REQ-R-3" {
				t.Errorf("the replay stops at the 429 and sends nothing after it; posted %v", postPaths(f.posts()))
			}
		}
		if len(*waits) != 4 {
			t.Errorf("Retry-After 30 s: four waits fit in 2 minutes, got %v", *waits)
		}
		if !strings.Contains(out, "30 s") {
			t.Errorf("the summary names the retry time:\n%s", out)
		}
	})

	t.Run("an entry left after it was sent", func(t *testing.T) {
		f := &replayFake{gates: map[string]map[string]any{"Q-R-1": questionGate("Q-R-1", pinD)}}
		root := replayWorkspace(t, f)
		gateSnapshot(t, root, ".modernpath/working-set/Q-R-1.md", "Q-R-1", "question", pinD, "", time.Now().Add(-time.Hour))
		keep(t, "factory", "answer", "Q-R-1", "--text", "yes", "--options", "yes", "--source", "USER:2026-10-04:q")
		entry := keptEntries(t, root)[0]
		raw, err := os.ReadFile(filepath.Join(journalPath(root), entry.name))
		if err != nil {
			t.Fatal(err)
		}
		serverBack(t, root)
		if code, out := runCLI(t, "outage", "replay"); code != 0 || len(keptEntries(t, root)) != 0 {
			t.Fatalf("the first replay records the answer; exit %d:\n%s", code, out)
		}

		// The CLI stopped after sending and before removing: the entry is back.
		if err := os.WriteFile(filepath.Join(journalPath(root), entry.name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		gate := questionGate("Q-R-1", pinD)
		gate["state"] = "answered"
		f.gates["Q-R-1"] = gate
		f.on = func(_ *replayFake, r journeyRequest, _ url.Values) *replayReply {
			if strings.HasSuffix(r.path, "/answer") {
				return &replayReply{status: 409, body: map[string]any{"error": map[string]any{
					"message": "already answered (first-wins)",
					"winner": map[string]any{"external_id": "Q-R-1", "answer": "yes",
						"chosen_option_keys": []any{"yes"}, "source_tag": "USER:2026-10-04:q"},
				}}}
			}
			return nil
		}

		code, out := runCLI(t, "outage", "replay")

		if code != 0 {
			t.Fatalf("exit %d, want 0:\n%s", code, out)
		}
		if n := len(keptEntries(t, root)); n != 0 {
			t.Errorf("the resent entry counts as recorded; %d remain", n)
		}
		if n := len(asideEntries(t, root, "conflicted")); n != 0 {
			t.Errorf("the resent entry is not a conflict; %d conflicted", n)
		}
		if !strings.Contains(out, "already there 1") {
			t.Errorf("the summary counts it as already there:\n%s", out)
		}
	})
}

// authorRecords are the records of the author posts the fake saw, in order.
func authorRecords(f *replayFake) []map[string]any {
	var out []map[string]any
	for _, p := range f.posts() {
		if p.path == "/api/v1/sync/author" {
			out = append(out, obj(p.body, "record"))
		}
	}
	return out
}

// authorRow answers an author post with the row the store returns, under the
// record's kind, carrying fp in field.
func authorRow(r journeyRequest, field, fp string) *replayReply {
	rec := obj(r.body, "record")
	return &replayReply{body: data(map[string]any{str(rec, "kind"): map[string]any{
		"external_id": str(rec, "external_id"), field: fp,
	}})}
}

// AC4 (CODE-OUTAGE-1-1) — a second kept edit of a record is sent with the
// fingerprint the first one's write returned: in one replay, after a replay
// that stopped between them, and for a backlog record, whose rows carry
// content_fingerprint. An empty fingerprint is never sent in its place.
func TestChainedEditTakesReturnedFingerprint(t *testing.T) {
	keepChain := func(t *testing.T, f *replayFake, id string, extra ...string) string {
		t.Helper()
		root := replayWorkspace(t, f)
		keep(t, append([]string{"author", "update", id, "--title", "First", "--expected-fingerprint", "fp-1"}, extra...)...)
		keep(t, append([]string{"author", "update", id, "--title", "Second", "--expected-fingerprint", "fp-1b"}, extra...)...)
		serverBack(t, root)
		return root
	}
	byTitle := func(fps map[string]string) func(*replayFake, journeyRequest, url.Values) *replayReply {
		return func(_ *replayFake, r journeyRequest, _ url.Values) *replayReply {
			if r.path == "/api/v1/sync/author" {
				return authorRow(r, "fingerprint", fps[str(obj(r.body, "record"), "title")])
			}
			return nil
		}
	}
	secondSentWith := func(t *testing.T, f *replayFake, want string) {
		t.Helper()
		recs := authorRecords(f)
		if len(recs) == 0 || str(recs[len(recs)-1], "title") != "Second" {
			t.Fatalf("the second edit must be sent; author posts %v", recs)
		}
		if got := str(recs[len(recs)-1], "expected_fingerprint"); got != want {
			t.Errorf("the second edit must be sent with the first's returned fingerprint %q, got %q", want, got)
		}
	}

	t.Run("both edits recorded in one replay", func(t *testing.T) {
		f := &replayFake{}
		f.on = byTitle(map[string]string{"First": "fp-A", "Second": "fp-B"})
		root := keepChain(t, f, "REQ-R-2")

		code, out := runCLI(t, "outage", "replay")

		if code != 0 {
			t.Fatalf("exit %d, want 0:\n%s", code, out)
		}
		secondSentWith(t, f, "fp-A")
		if n := len(keptEntries(t, root)); n != 0 {
			t.Errorf("both edits are recorded; %d remain", n)
		}
	})

	t.Run("a 429 between them, then a later replay", func(t *testing.T) {
		recordLimitWaits(t)
		f := &replayFake{}
		f.on = func(_ *replayFake, r journeyRequest, _ url.Values) *replayReply {
			if r.path != "/api/v1/sync/author" {
				return nil
			}
			if str(obj(r.body, "record"), "title") == "First" {
				return authorRow(r, "fingerprint", "fp-A")
			}
			return &replayReply{status: 429, text: "rate limited", header: map[string]string{"Retry-After": "1"}}
		}
		root := keepChain(t, f, "REQ-R-2")
		if code, out := runCLI(t, "outage", "replay"); code != exitTempFail {
			t.Fatalf("the 429 stops the replay with exit %d, got %d:\n%s", exitTempFail, code, out)
		}
		if n := len(keptEntries(t, root)); n != 1 {
			t.Fatalf("the second edit stays kept; %d entr(ies)", n)
		}

		f.on = byTitle(map[string]string{"Second": "fp-B"})
		serverBack(t, root)
		code, out := runCLI(t, "outage", "replay")

		if code != 0 {
			t.Fatalf("exit %d, want 0:\n%s", code, out)
		}
		secondSentWith(t, f, "fp-A")
		if n := len(asideEntries(t, root, "conflicted")); n != 0 {
			t.Errorf("a valid chained edit is not conflicted; %d conflicted", n)
		}
	})

	t.Run("a backlog record read back with its content fingerprint", func(t *testing.T) {
		f := &replayFake{}
		f.items = map[string]map[string]any{"BACKLOG-R-5": {"kind": "backlog", "item": map[string]any{
			"external_id": "BACKLOG-R-5", "kind": "backlog", "title": "First", "content_fingerprint": "fp-c"}}}
		f.on = func(_ *replayFake, r journeyRequest, _ url.Values) *replayReply {
			if r.path != "/api/v1/sync/author" {
				return nil
			}
			if str(obj(r.body, "record"), "title") == "First" {
				return conflict409("stale_fingerprint")
			}
			return authorRow(r, "content_fingerprint", "fp-d")
		}
		keepChain(t, f, "BACKLOG-R-5", "--kind", "backlog")

		code, out := runCLI(t, "outage", "replay")

		if code != 0 {
			t.Fatalf("exit %d, want 0:\n%s", code, out)
		}
		secondSentWith(t, f, "fp-c")
	})
}

// AC2 (CODE-OUTAGE-1-4) — the hook-mode write verbs send no kept write: the
// Stop hook's `factory sync --if-quiescent` and the detached `focus --infer`.
// A regression guard: it passes before the fix.
func TestHookWriteVerbsDoNotReplay(t *testing.T) {
	for _, args := range [][]string{
		{"factory", "sync", "--if-quiescent", "--trigger", "Stop"},
		{"focus", "--infer", "REQ-R-2"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f := &replayFake{}
			root := replayWorkspace(t, f)
			keep(t, "author", "update", "REQ-R-2", "--title", "New title", "--expected-fingerprint", "fp-1")
			serverBack(t, root)

			_, out := runCLI(t, args...)

			if len(f.posts()) == 0 {
				t.Fatalf("the hook verb must send its own write for this guard to hold; nothing was posted:\n%s", out)
			}
			for _, p := range f.posts() {
				if p.path == "/api/v1/sync/author" {
					t.Errorf("a hook-mode write sends no kept write; posted %v", postPaths(f.posts()))
					break
				}
			}
			if n := len(keptEntries(t, root)); n != 1 {
				t.Errorf("the kept write stays kept; %d entr(ies)", n)
			}
		})
	}
}
