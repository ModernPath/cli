// REQ-CROSS-503 (EPIC-CLI-029): `modernpath analysis start | reanalyze |
// reset | status` drive the analysis of the bound system through the
// lifecycle routes the UI uses, print every refusal once by its error code,
// and `status` reads the repository and the current run, reporting a missing
// run (404, the state after an import) as "no run" rather than an error.
//
// The fake core below answers with the shapes the server on this branch
// sends (SystemGitSourceAnalysisApiController, AnalysisRunApiController,
// RepositoryApiController). docs_test.go uses it too.
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// lifecycleStub is a fake core that records every request and answers each
// "METHOD /path" from a table; anything else is a 404, so a request to a
// route the server does not have is visible in the test.
type lifecycleStub struct {
	server   *httptest.Server
	mu       sync.Mutex
	answers  map[string]stubAnswer
	requests []stubRequest
	// closing holds the "METHOD /path" keys the stub answers on a connection
	// it then closes without saying so, a little after the answer: a server
	// whose unknown route raises and takes the kept-alive connection with it.
	closing map[string]bool
}

type stubAnswer struct {
	status int
	body   any
}

type stubRequest struct {
	method string
	path   string
	body   map[string]any
}

func newLifecycleStub(t *testing.T) *lifecycleStub {
	t.Helper()
	s := &lifecycleStub{answers: map[string]stubAnswer{}, closing: map[string]bool{}}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := stubRequest{method: r.Method, path: r.URL.Path}
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			_ = json.Unmarshal(raw, &req.body)
		}
		s.mu.Lock()
		s.requests = append(s.requests, req)
		answer, ok := s.answers[r.Method+" "+r.URL.Path]
		closing := s.closing[r.Method+" "+r.URL.Path]
		s.mu.Unlock()
		if !ok {
			answer = stubAnswer{http.StatusNotFound, map[string]any{"error": "not_found", "message": "no such route"}}
		}
		if closing {
			s.answerAndDrop(w, answer)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(answer.status)
		_ = json.NewEncoder(w).Encode(answer.body)
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *lifecycleStub) answer(method, path string, status int, body any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.answers[method+" "+path] = stubAnswer{status, body}
}

// dropConnectionAfter makes the stub close the connection a moment after it
// answers method and path, with no Connection: close header, like a server
// that answered a request and then crashed its connection handler.
func (s *lifecycleStub) dropConnectionAfter(method, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closing[method+" "+path] = true
}

// answerAndDrop writes answer straight onto the hijacked connection, as a
// complete keep-alive response, waits long enough for the client to reuse the
// connection, and closes it.
func (s *lifecycleStub) answerAndDrop(w http.ResponseWriter, answer stubAnswer) {
	conn, rw, err := w.(http.Hijacker).Hijack()
	if err != nil {
		panic(err)
	}
	body, _ := json.Marshal(answer.body)
	fmt.Fprintf(rw, "HTTP/1.1 %d %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s",
		answer.status, http.StatusText(answer.status), len(body), body)
	_ = rw.Flush()
	time.Sleep(100 * time.Millisecond)
	_ = conn.Close()
}

// requested returns the first request for method and path, or nil.
func (s *lifecycleStub) requested(method, path string) *stubRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.requests {
		if s.requests[i].method == method && s.requests[i].path == path {
			return &s.requests[i]
		}
	}
	return nil
}

// posts lists every POST path, for asserting what was (not) written.
func (s *lifecycleStub) posts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var paths []string
	for _, r := range s.requests {
		if r.method == http.MethodPost {
			paths = append(paths, r.path)
		}
	}
	return paths
}

// bindLifecycleWorkspace binds a temporary checkout to the stub's system 42 —
// with the repository id `import --local` records, unless repositoryID is 0
// — and returns the checkout directory as the CLI sees it.
func bindLifecycleWorkspace(t *testing.T, s *lifecycleStub, repositoryID int) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	cfg := map[string]any{"api_url": s.server.URL, "system_id": 42, "system_name": "Legacy Estate", "system_slug": "legacy-estate"}
	if repositoryID != 0 {
		cfg["repository_id"] = repositoryID
	}
	writeJSON(t, dir, ".modernpath/config.json", cfg)
	writeFactoryTestFile(t, dir, ".modernpath/auth.json", `{"token":"t"}`)
	t.Chdir(dir)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return cwd
}

// withoutTerminal makes stdin look like a CI job's: no terminal to prompt on.
func withoutTerminal(t *testing.T) {
	t.Helper()
	saved := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() { stdinIsTerminal = saved })
}

// runVerb runs the CLI and returns stdout and stderr together, so a test can
// count how often a message was printed.
func runVerb(t *testing.T, args ...string) (string, error) {
	t.Helper()
	stdout, stderr, err := runAskStreams(t, args...)
	return stdout + stderr, err
}

// lifecycleAccepted is the 202 body the lifecycle Start, Reanalyze and Reset
// answer with: ids are strings, `started` lists the repositories.
func lifecycleAccepted(runID, status, mode string) map[string]any {
	return map[string]any{"data": map[string]any{
		"analysis_mode": mode,
		"started":       []any{map[string]any{"repository_id": "7", "repository_name": "Legacy Estate"}},
		"run": map[string]any{
			"id": runID, "status": status, "source_revision": "1927f502c38d5f207b565d5e7594c37725a8505d",
			"repository_ids": []any{"7"},
		},
	}}
}

func refusal(code, message string) map[string]any {
	return map[string]any{"error": code, "message": message}
}

const (
	analyzePath   = "/api/systems/42/git-sources/analyze"
	resetPath     = "/api/systems/42/git-sources/reset"
	reanalyzePath = "/api/systems/42/git-sources/7/reanalyze"
	repoReadPath  = "/api/systems/42/repositories/7"
	runReadPath   = "/api/systems/42/analysis/runs/current"
)

// AC3: start posts the analysis mode to the lifecycle Start and prints the
// run it answers with; --mode picks the other mode.
func TestAnalysisStartPostsTheModeAndPrintsTheRun(t *testing.T) {
	for _, tc := range []struct {
		args []string
		mode string
	}{
		{[]string{"analysis", "start"}, "independent_repos"},
		{[]string{"analysis", "start", "--mode", "unified_workspace"}, "unified_workspace"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			s := newLifecycleStub(t)
			s.answer(http.MethodPost, analyzePath, http.StatusAccepted, lifecycleAccepted("15", "queued", tc.mode))
			bindLifecycleWorkspace(t, s, 7)

			out, err := runVerb(t, tc.args...)
			if err != nil {
				t.Fatalf("%v: %v\n%s", tc.args, err, out)
			}
			req := s.requested(http.MethodPost, analyzePath)
			if req == nil {
				t.Fatalf("start must POST %s, posted %v\n%s", analyzePath, s.posts(), out)
			}
			if req.body["analysis_mode"] != tc.mode {
				t.Errorf("analysis_mode = %v, want %s", req.body["analysis_mode"], tc.mode)
			}
			for _, want := range []string{"run 15", "queued", tc.mode} {
				if !strings.Contains(out, want) {
					t.Errorf("output must name %q:\n%s", want, out)
				}
			}
		})
	}
}

// AC3: a 409 or 422 from the Start is printed once by its error code and
// message, and the command exits non-zero.
func TestAnalysisStartPrintsARefusalOnceByName(t *testing.T) {
	for _, tc := range []struct {
		status        int
		code, message string
	}{
		{http.StatusConflict, "analysis_already_complete", "analysis is already complete"},
		{http.StatusUnprocessableEntity, "sources_required", "add at least one repository"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			s := newLifecycleStub(t)
			s.answer(http.MethodPost, analyzePath, tc.status, refusal(tc.code, tc.message))
			bindLifecycleWorkspace(t, s, 7)

			out, err := runVerb(t, "analysis", "start")
			if err == nil {
				t.Fatalf("a %d refusal must exit non-zero\n%s", tc.status, out)
			}
			if s.requested(http.MethodPost, analyzePath) == nil {
				t.Fatalf("start must POST %s, posted %v", analyzePath, s.posts())
			}
			if n := strings.Count(out, tc.code); n != 1 {
				t.Errorf("the refusal must be printed once by its code %q, printed %d times:\n%s", tc.code, n, out)
			}
			if !strings.Contains(out, tc.message) {
				t.Errorf("the refusal must carry the server's message %q:\n%s", tc.message, out)
			}
		})
	}
}

// AC3: reanalyze posts to the repository's lifecycle Reanalyze route and
// prints the run; a refusal is printed by name and exits non-zero.
func TestAnalysisReanalyzePostsTheRepository(t *testing.T) {
	s := newLifecycleStub(t)
	s.answer(http.MethodPost, reanalyzePath, http.StatusAccepted, lifecycleAccepted("16", "queued", "independent_repos"))
	bindLifecycleWorkspace(t, s, 7)

	out, err := runVerb(t, "analysis", "reanalyze", "7")
	if err != nil {
		t.Fatalf("analysis reanalyze 7: %v\n%s", err, out)
	}
	if s.requested(http.MethodPost, reanalyzePath) == nil {
		t.Fatalf("reanalyze must POST %s, posted %v", reanalyzePath, s.posts())
	}
	if !strings.Contains(out, "run 16") {
		t.Errorf("output must name the run:\n%s", out)
	}

	s.answer(http.MethodPost, reanalyzePath, http.StatusConflict, refusal("source_materials_not_ready", "prepare the source materials before starting analysis"))
	out, err = runVerb(t, "analysis", "reanalyze", "7")
	if err == nil || strings.Count(out, "source_materials_not_ready") != 1 {
		t.Errorf("a 409 must exit non-zero and print its code once, err=%v:\n%s", err, out)
	}
}

// AC3: reset --yes posts to the lifecycle Reset — the mode only when given —
// and prints the run that restarted.
func TestAnalysisResetWithYesPostsAndPrintsTheRun(t *testing.T) {
	for _, tc := range []struct {
		args []string
		mode any
	}{
		{[]string{"analysis", "reset", "--yes"}, nil},
		{[]string{"analysis", "reset", "--yes", "--mode", "unified_workspace"}, "unified_workspace"},
	} {
		t.Run(strings.Join(tc.args[2:], " "), func(t *testing.T) {
			s := newLifecycleStub(t)
			s.answer(http.MethodPost, resetPath, http.StatusAccepted, lifecycleAccepted("17", "queued", "independent_repos"))
			bindLifecycleWorkspace(t, s, 7)
			withoutTerminal(t)

			out, err := runVerb(t, tc.args...)
			if err != nil {
				t.Fatalf("%v: %v\n%s", tc.args, err, out)
			}
			req := s.requested(http.MethodPost, resetPath)
			if req == nil {
				t.Fatalf("reset must POST %s, posted %v", resetPath, s.posts())
			}
			if req.body["analysis_mode"] != tc.mode {
				t.Errorf("analysis_mode = %v, want %v", req.body["analysis_mode"], tc.mode)
			}
			if !strings.Contains(out, "run 17") {
				t.Errorf("output must name the restarted run:\n%s", out)
			}
		})
	}
}

// AC3: reset deletes analysis results, so without --yes and without a
// terminal it refuses before any request, says what it would delete and
// that --yes is required.
func TestAnalysisResetRefusesWithoutATerminalOrYes(t *testing.T) {
	s := newLifecycleStub(t)
	s.answer(http.MethodPost, resetPath, http.StatusAccepted, lifecycleAccepted("17", "queued", "independent_repos"))
	bindLifecycleWorkspace(t, s, 7)
	withoutTerminal(t)

	out, err := runVerb(t, "analysis", "reset")
	if err == nil {
		t.Fatalf("reset without --yes and without a terminal must exit non-zero\n%s", out)
	}
	if posts := s.posts(); len(posts) != 0 {
		t.Errorf("a refused reset must send nothing, posted %v", posts)
	}
	for _, want := range []string{"--yes is required for unattended runs", "generated documentation"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal must say %q:\n%s", want, out)
		}
	}
}

// AC4: status prints the repository's analysis, documentation and embedding
// status, its source revision and refresh mark, and the current run with its
// phase and the stage of its steps.
func TestAnalysisStatusPrintsTheRunAndTheRepository(t *testing.T) {
	s := newLifecycleStub(t)
	s.answer(http.MethodGet, repoReadPath, http.StatusOK, map[string]any{"data": map[string]any{
		"id": 7, "name": "Legacy Estate", "provider": "local", "repository_url": nil,
		"analysis_status": "in_progress", "documentation_status": "pending", "embedding_status": "not_started",
		"source_revision": "1927f502c38d5f207b565d5e7594c37725a8505d", "source_kind": "upload",
		"source_sealed_at": "2026-09-30T10:00:00Z",
		"source_refresh":   map[string]any{"status": "completed", "session_id": "push:1", "at": "2026-09-30T11:00:00Z"},
	}})
	s.answer(http.MethodGet, runReadPath, http.StatusOK, map[string]any{"data": map[string]any{
		"run": map[string]any{
			"id": 15, "system_id": 42, "status": "running", "analysis_mode": "independent_repos",
			"source_revision": "1927f502c38d5f207b565d5e7594c37725a8505d", "target_repository_ids": []any{7},
		},
		"steps": []any{
			map[string]any{"id": 1, "stage": "blueprint", "state": "completed", "repository_id": 7},
			map[string]any{"id": 2, "stage": "module", "state": "completed", "repository_id": 7},
			map[string]any{"id": 3, "stage": "module", "state": "running", "repository_id": 7},
			map[string]any{"id": 4, "stage": "module", "state": "queued", "repository_id": 7},
		},
		"repositories": []any{map[string]any{"repository_id": 7, "state": "running", "recovering": false}},
	}})
	bindLifecycleWorkspace(t, s, 7)

	out, err := runVerb(t, "analysis", "status")
	if err != nil {
		t.Fatalf("analysis status: %v\n%s", err, out)
	}
	if s.requested(http.MethodGet, repoReadPath) == nil || s.requested(http.MethodGet, runReadPath) == nil {
		t.Fatalf("status must read %s and %s", repoReadPath, runReadPath)
	}
	for _, want := range []string{
		"Legacy Estate", "in_progress", "pending", "not_started", // the three statuses
		"1927f502c38d", "upload", // source revision and kind
		"completed at 2026-09-30T11:00:00Z", // the refresh mark
		"run 15", "running", "independent_repos",
		"phase", "module", "blueprint",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status must print %q:\n%s", want, out)
		}
	}
}

// AC4, D7: after an import the current-run read answers 404; status reports
// that as no run, still prints the repository read, and exits 0.
func TestAnalysisStatusReportsAMissingRunAsNoRun(t *testing.T) {
	s := newLifecycleStub(t)
	s.answer(http.MethodGet, repoReadPath, http.StatusOK, map[string]any{"data": map[string]any{
		"id": 7, "name": "Legacy Estate", "analysis_status": "in_progress", "documentation_status": "pending",
		"embedding_status": "pending", "source_revision": "1927f502c38d5f207b565d5e7594c37725a8505d",
		"source_kind": "upload", "source_refresh": nil,
	}})
	s.answer(http.MethodGet, runReadPath, http.StatusNotFound, refusal("not_found", "No analysis run for this system"))
	bindLifecycleWorkspace(t, s, 7)

	out, err := runVerb(t, "analysis", "status")
	if err != nil {
		t.Fatalf("a missing run is not an error: %v\n%s", err, out)
	}
	for _, want := range []string{"no analysis run recorded", "in_progress", "1927f502c38d"} {
		if !strings.Contains(out, want) {
			t.Errorf("status must print %q:\n%s", want, out)
		}
	}
}
