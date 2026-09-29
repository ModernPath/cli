package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
