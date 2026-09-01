// REQ-CROSS-029: the installed process kit and the commands that write it.
package cmd

// `modernpath new` posted the Epic's
// name as `name`, the core requires `title`, and every run answered
// 422 {"errors":{"title":["can't be blank"]}} — under a warning, a success
// banner and exit 0. So no system ever got its initial Epic, and the
// config it left behind named one that does not exist.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newTestServer serves the four endpoints `modernpath new` walks through. The
// Epic handler answers with whatever the caller asked for.
func newTestServer(t *testing.T, epicStatus int, epicBody string) (*httptest.Server, *map[string]interface{}) {
	t.Helper()
	captured := map[string]interface{}{}

	mux := http.NewServeMux()
	mux.HandleFunc("/_health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/tech-profiles", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]TechProfile{{ID: "profile-1", Name: "Elixir"}})
	})
	mux.HandleFunc("/api/systems", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(SystemResult{ID: 4242, Name: "Ledger", Description: "a ledger"})
	})
	mux.HandleFunc("/api/work/epics", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(epicStatus)
		_, _ = w.Write([]byte(epicBody))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, &captured
}

func TestCreateEpicPostsTheFieldTheCoreRequires(t *testing.T) {
	server, captured := newTestServer(t, http.StatusCreated, `{"data":{"id":7}}`)

	id, err := createEpic(server.URL, &SystemResult{ID: 4242, Name: "Ledger", Description: "a ledger"})
	if err != nil {
		t.Fatal(err)
	}
	if id != 7 {
		t.Fatalf("expected the created id back, got %d", id)
	}

	payload := *captured
	// CODE:apps/storage/lib/storage/schema/epic.ex:changeset —
	// validate_required([:title, :status]); `name` is not even cast.
	if got, _ := payload["title"].(string); got != epicTitle("Ledger") {
		t.Fatalf("Epic must be posted with a `title`, got %#v", payload["title"])
	}
	if _, present := payload["name"]; present {
		t.Fatalf("`name` is not a cast field — sending it is what produced the 422: %#v", payload)
	}
	// CODE:apps/storage/lib/storage/repositories/board_column_config_repo.ex:@defaults
	status, _ := payload["status"].(string)
	if status != "todo" && status != "in_progress" && status != "blocked" && status != "done" {
		t.Fatalf("status %q is no board column, so the card lands in none of them", status)
	}
}

func TestCreateEpicReportsARejection(t *testing.T) {
	server, _ := newTestServer(t, http.StatusUnprocessableEntity, `{"errors":{"title":["can't be blank"]}}`)

	if _, err := createEpic(server.URL, &SystemResult{ID: 4242, Name: "Ledger"}); err == nil {
		t.Fatal("a 422 must be an error, not a zero id the caller can mistake for success")
	}
}

// newRun drives the whole command non-interactively against a test server.
func newRun(t *testing.T, server *httptest.Server) error {
	t.Helper()
	chdirTemp(t)

	apiURL = server.URL
	projectName = "Ledger"
	projectDesc = "A double-entry ledger service for small finance teams."
	techProfile = "Elixir"
	projectType = "api"
	yesFlag = true
	t.Cleanup(func() {
		apiURL, projectName, projectDesc = "", "", ""
		techProfile, projectType, yesFlag = "", "", false
	})

	return runNew(&cobra.Command{}, nil)
}

func readNewConfig(t *testing.T) map[string]interface{} {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("ledger", ".modernpath", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestNewFailsWhenTheEpicIsRejected(t *testing.T) {
	server, _ := newTestServer(t, http.StatusUnprocessableEntity, `{"errors":{"title":["can't be blank"]}}`)

	err := newRun(t, server)
	if err == nil {
		t.Fatal("a rejected Epic must fail the command, not print a success banner over it")
	}
	if !strings.Contains(err.Error(), "422") {
		t.Fatalf("the failure must carry the server's rejection, got %v", err)
	}

	// The config must not name an Epic that was never created.
	cfg := readNewConfig(t)
	if _, present := cfg["initiative_id"]; present {
		t.Fatalf("config claims an Epic id after a failed creation: %#v", cfg)
	}
	if _, present := cfg["initiative_name"]; present {
		t.Fatalf("config names an Epic that does not exist: %#v", cfg)
	}
	if _, present := cfg["epic_id"]; present {
		t.Fatalf("config claims an Epic id after a failed creation: %#v", cfg)
	}
	if _, present := cfg["epic_name"]; present {
		t.Fatalf("config names an Epic that does not exist: %#v", cfg)
	}
	if got, _ := cfg["system_id"].(float64); got != 4242 {
		t.Fatalf("the system that WAS created must still be recorded, got %#v", cfg["system_id"])
	}
}

func TestNewRecordsTheEpicItCreated(t *testing.T) {
	server, _ := newTestServer(t, http.StatusCreated, `{"data":{"id":7}}`)

	if err := newRun(t, server); err != nil {
		t.Fatal(err)
	}

	cfg := readNewConfig(t)
	if got, _ := cfg["epic_id"].(float64); got != 7 {
		t.Fatalf("expected epic_id 7, got %#v", cfg["epic_id"])
	}
	if got, _ := cfg["epic_name"].(string); got != epicTitle("Ledger") {
		t.Fatalf("unexpected epic_name %#v", cfg["epic_name"])
	}
	if _, present := cfg["initiative_id"]; present {
		t.Fatalf("new config must not write the deleted initiative_id key: %#v", cfg)
	}
	if _, present := cfg["initiative_name"]; present {
		t.Fatalf("new config must not write the deleted initiative_name key: %#v", cfg)
	}
}
