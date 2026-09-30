package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/fatih/color"
)

// --- REQ-CROSS-467: ask names each source once ---
//
// A file source carries a path and no title, and the pretty renderer printed
// only the title, so every file came out as a bare "[file]" line. The server
// also lists a file once per tool call that read it.

const askSourcesBody = `{"success":true,"result":{"answer":"The guard validates tokens.","iterations":2,"sources":[
 {"type":"file","path":"lib/auth/guard.ex","file_id":1,"title":null,"relevance":null,"doc_id":null},
 {"type":"file","path":"lib/auth/guard.ex","file_id":1,"title":null,"relevance":null,"doc_id":null},
 {"type":"search","title":"Auth subsystem","doc_id":"d-1","relevance":0.9,"path":null,"file_id":null}
]}}`

func askServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mcp/tools/agentic_search", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(askSourcesBody))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// REQ-CROSS-469: ask's help says that curated patterns and capabilities are
// searched too, not only documentation and code.
func TestREQCROSS469AskHelpNamesPatternsAndCapabilities(t *testing.T) {
	if !strings.Contains(askCmd.Long, "patterns and capabilities") {
		t.Errorf("ask's description must say patterns and capabilities are covered:\n%s", askCmd.Long)
	}
}

func TestAskPrettyPrintsAFileSourcesPathOnce(t *testing.T) {
	cobraWorkspace(t, askServer(t))

	out, err := runRoot(t, "ask", "how are tokens validated?")
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if n := strings.Count(out, "lib/auth/guard.ex"); n != 1 {
		t.Fatalf("a titleless file source must print its path, once; got %d copies:\n%s", n, out)
	}
	if n := strings.Count(out, "Auth subsystem"); n != 1 {
		t.Fatalf("a titled source must print its title, once; got %d copies:\n%s", n, out)
	}
}

func TestAskMarkdownListsEachSourceOnce(t *testing.T) {
	cobraWorkspace(t, askServer(t))

	out, err := runRoot(t, "ask", "how are tokens validated?", "--format=markdown")
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if n := strings.Count(out, "lib/auth/guard.ex"); n != 1 {
		t.Fatalf("the file source must be listed once; got %d copies:\n%s", n, out)
	}
}

func TestAskJSONListsEachSourceOnceWithItsThreeKeys(t *testing.T) {
	cobraWorkspace(t, askServer(t))

	out, err := runRoot(t, "ask", "how are tokens validated?", "--format=json")
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	var got struct {
		Sources []map[string]any `json:"sources"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil {
		t.Fatalf("json output does not parse: %v\n%s", err, out)
	}
	if len(got.Sources) != 2 {
		t.Fatalf("each source must be listed once, got %d: %v", len(got.Sources), got.Sources)
	}
	for _, src := range got.Sources {
		for key := range src {
			if key != "title" && key != "type" && key != "path" {
				t.Fatalf("json sources keep only title, type and path; found %q in %v", key, src)
			}
		}
	}
}

// --- REQ-CROSS-490: ask polls an answer that outlives its request ---
//
// Every ask timed out in production: the server gave up at 130 s after the
// CLI had given up at 120 s. The server now answers a slow ask with a ticket,
// `status: "running"` and an `ask_id`, and `agentic_search_result` redeems it.

const askRunningBody = `{"success":true,"result":{"status":"running","ask_id":"42",
 "next":"Call agentic_search_result with this ask_id; do not ask again.",
 "answer":"Still working on this question (ask 42)."}}`

const askDoneBody = `{"success":true,"result":{"status":"done","partial":false,"question":"q",
 "answer":"The guard validates tokens.","iterations":2,"tool_calls_count":1,"sources":[
 {"type":"file","path":"lib/auth/guard.ex","file_id":1,"title":null,"relevance":null,"doc_id":null},
 {"type":"search","title":"Auth subsystem","doc_id":"d-1","relevance":0.9,"path":null,"file_id":null}
]}}`

const askFailedBody = `{"success":false,"error":"\"the ask failed: timed out\""}`

type askReply struct {
	code int
	body string
}

// askPollServer answers the ask with first, then each poll with the next of
// polls, repeating the last. It counts the polls and records the ask ids
// they carried.
func askPollServer(t *testing.T, first askReply, polls ...askReply) (*httptest.Server, *int32, *[]any) {
	t.Helper()
	var count int32
	var mu sync.Mutex
	var ids []any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mcp/tools/agentic_search", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(first.code)
		_, _ = w.Write([]byte(first.body))
	})
	mux.HandleFunc("/api/mcp/tools/agentic_search_result", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		ids = append(ids, req.Arguments["ask_id"])
		mu.Unlock()
		n := int(atomic.AddInt32(&count, 1))
		reply := polls[len(polls)-1]
		if n <= len(polls) {
			reply = polls[n-1]
		}
		w.WriteHeader(reply.code)
		_, _ = w.Write([]byte(reply.body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &count, &ids
}

// runAskStreams runs the CLI and returns what it wrote to stdout and to
// stderr separately: the progress line belongs on stderr only.
func runAskStreams(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	resetTreeFlags(rootCmd)
	t.Cleanup(func() { resetTreeFlags(rootCmd) })
	rootCmd.SetArgs(args)
	defer rootCmd.SetArgs(nil)

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr, oldColorOut, oldColorErr := os.Stdout, os.Stderr, color.Output, color.Error
	os.Stdout, color.Output = outW, outW
	os.Stderr, color.Error = errW, errW

	var wg sync.WaitGroup
	var stdout, stderr []byte
	wg.Add(2)
	go func() { defer wg.Done(); stdout, _ = io.ReadAll(outR) }()
	go func() { defer wg.Done(); stderr, _ = io.ReadAll(errR) }()

	err := rootCmd.Execute()

	_ = outW.Close()
	_ = errW.Close()
	wg.Wait()
	os.Stdout, os.Stderr, color.Output, color.Error = oldOut, oldErr, oldColorOut, oldColorErr
	return string(stdout), string(stderr), err
}

func TestREQCROSS490AskPollsARunningAskUntilDone(t *testing.T) {
	for _, format := range []string{"pretty", "markdown", "json"} {
		t.Run(format, func(t *testing.T) {
			srv, polls, ids := askPollServer(t, askReply{200, askRunningBody},
				askReply{200, askRunningBody}, askReply{200, askDoneBody})
			cobraWorkspace(t, srv)

			stdout, stderr, err := runAskStreams(t, "ask", "how are tokens validated?", "--format="+format)
			if err != nil {
				t.Fatalf("ask: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
			}
			if n := atomic.LoadInt32(polls); n != 2 {
				t.Fatalf("a running ask must be polled until done: want 2 polls, got %d", n)
			}
			for _, id := range *ids {
				if id != "42" {
					t.Fatalf("each poll must carry the ask id 42, got %v", *ids)
				}
			}
			if n := strings.Count(stdout, "The guard validates tokens."); n != 1 {
				t.Fatalf("the answer must be printed once, got %d:\n%s", n, stdout)
			}
			if n := strings.Count(stdout, "lib/auth/guard.ex"); n != 1 {
				t.Fatalf("each source must be printed once, got %d:\n%s", n, stdout)
			}
			if strings.Contains(stdout, "Still working") {
				t.Fatalf("progress and the ticket's note never reach stdout:\n%s", stdout)
			}
			progress := strings.Contains(stderr, "Still working (")
			if format == "pretty" && !progress {
				t.Fatalf("pretty output shows a progress line on stderr:\n%s", stderr)
			}
			if format != "pretty" && progress {
				t.Fatalf("%s output shows no progress line:\n%s", format, stderr)
			}
		})
	}
}

// Regression guard: an answer that comes back inline is printed as before,
// with no poll.
func TestREQCROSS490AskPrintsAnInlineAnswerWithoutPolling(t *testing.T) {
	srv, polls, _ := askPollServer(t, askReply{200, askDoneBody}, askReply{200, askDoneBody})
	cobraWorkspace(t, srv)

	stdout, _, err := runAskStreams(t, "ask", "how are tokens validated?")
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if n := atomic.LoadInt32(polls); n != 0 {
		t.Fatalf("an inline answer needs no poll, got %d", n)
	}
	if !strings.Contains(stdout, "The guard validates tokens.") {
		t.Fatalf("the answer must be printed:\n%s", stdout)
	}
}

func TestREQCROSS490AskReportsAFailedAskAndExitsNonZero(t *testing.T) {
	srv, _, _ := askPollServer(t, askReply{200, askRunningBody}, askReply{400, askFailedBody})
	cobraWorkspace(t, srv)

	stdout, stderr, err := runAskStreams(t, "ask", "how are tokens validated?")
	if err == nil {
		t.Fatalf("a failed ask must exit non-zero\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	all := stdout + stderr
	if !strings.Contains(all, "the ask failed: timed out") || !strings.Contains(all, "42") {
		t.Fatalf("a failed ask names its reason and its ask id:\n%s", all)
	}
}

func TestREQCROSS490AskStopsAtItsWaitLimitNamingTheAskID(t *testing.T) {
	t.Setenv("MODERNPATH_ASK_WAIT_LIMIT", "50ms")
	srv, _, _ := askPollServer(t, askReply{200, askRunningBody}, askReply{200, askRunningBody})
	cobraWorkspace(t, srv)

	stdout, stderr, err := runAskStreams(t, "ask", "how are tokens validated?", "--format=json")
	if err == nil {
		t.Fatalf("an ask still running at the limit must exit non-zero\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	all := stdout + stderr
	if !strings.Contains(all, "42") || !strings.Contains(all, "not ready") {
		t.Fatalf("the limit message names the ask id and says the answer was not ready:\n%s", all)
	}
}
