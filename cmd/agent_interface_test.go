package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentSearchFailureAndStructuredResults(t *testing.T) {
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			w.WriteHeader(500)
			w.Write([]byte(`{"error":"search unavailable"}`))
			return
		}
		w.Write([]byte(`{"data":{"docs":[{"id":47,"title":"Catalog","summary":"Full description","tier":"module"}],"files":[]}}`))
	}))
	defer server.Close()
	cobraWorkspace(t, server)
	out, err := runRoot(t, "search", "catalog", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if json.Unmarshal([]byte(out), &data) != nil || !strings.Contains(out, `"id": 47`) {
		t.Fatalf("not complete JSON: %s", out)
	}
	fail = true
	if _, err = runRoot(t, "search", "catalog"); err == nil {
		t.Fatal("HTTP 500 reported success")
	}
}

func TestAgentReadFileJSONPreservesContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"data":{"file_path":"src/catalog.ex","content":"raw bytes\nsecond line","total_lines":2}}`))
	}))
	defer server.Close()
	cobraWorkspace(t, server)
	out, err := runRoot(t, "read-file", "src/catalog.ex", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Content string `json:"content"`
	}
	if err = json.Unmarshal([]byte(out), &data); err != nil || data.Content != "raw bytes\nsecond line" {
		t.Fatalf("content changed: %s; %v", out, err)
	}
}

func TestAgentEpicsReadUsesExplicitID(t *testing.T) {
	paths := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		if r.URL.Path == "/api/work/epics/42" {
			w.Write([]byte(`{"data":{"id":42,"system_id":1,"title":"Explicit Epic"}}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()
	cobraWorkspace(t, server)
	out, err := runRoot(t, "epics", "get", "42", "--json")
	if err != nil || !strings.Contains(out, "Explicit Epic") {
		t.Fatalf("explicit read failed: %v %s paths=%v", err, out, paths)
	}
}

func TestAgentEpicCreateUsesBoundSystemWithoutSelectingIt(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		if r.Method != "POST" || r.URL.Path != "/api/work/epics" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"data":{"id":42,"target_architecture_id":1,"title":"Stored Epic","process_status":"PROPOSED"}}`))
	}))
	defer server.Close()
	cobraWorkspace(t, server)
	before, err := os.ReadFile(".modernpath/config.json")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "epic.json")
	if err := os.WriteFile(file, []byte(`{"title":"Stored Epic"}`), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := runRoot(t, "epics", "create", "--file", file, "--json")
	if err != nil || !strings.Contains(out, `"id": 42`) {
		t.Fatalf("create: %s %v", out, err)
	}
	if received["system_id"] != float64(1) || received["title"] != "Stored Epic" {
		t.Fatalf("wrong native payload: %#v", received)
	}
	after, err := os.ReadFile(".modernpath/config.json")
	if err != nil || string(before) != string(after) {
		t.Fatalf("create changed shared selection: %v", err)
	}
}

func TestAgentEpicUpdatePreservesPartialContentAndRejectsWrongSystem(t *testing.T) {
	wrongSystem := false
	writes := 0
	description := "Existing scope.\n\nReview findings: retain failed drafts."
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		if r.URL.Path != "/api/work/epics/42" {
			t.Errorf("unexpected path: %s", r.URL)
		}
		if r.Method == http.MethodPut {
			writes++
			var payload struct {
				Epic map[string]any `json:"epic"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if len(payload.Epic) != 1 || payload.Epic["description"] != description {
				t.Errorf("partial update changed content or unrelated fields: %#v", payload)
			}
		}
		systemID := 1
		if wrongSystem {
			systemID = 2
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": 42, "system_id": systemID, "description": description}})
	}))
	defer server.Close()
	cobraWorkspace(t, server)
	file := filepath.Join(t.TempDir(), "epic-update.json")
	input, _ := json.Marshal(map[string]any{"description": description})
	if err := os.WriteFile(file, input, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := runRoot(t, "epics", "update", "42", "--file", file, "--json")
	var receipt map[string]any
	decodeErr := json.Unmarshal([]byte(out), &receipt)
	if err != nil || decodeErr != nil || receipt["id"] != float64(42) || receipt["operation"] != "updated" || strings.Contains(out, "Review findings") || writes != 1 {
		t.Fatalf("update: %s %v writes=%d", out, err, writes)
	}
	wrongSystem = true
	if _, err := runRoot(t, "epics", "update", "42", "--file", file); err == nil {
		t.Fatal("updated an Epic outside the bound System")
	}
	if writes != 1 {
		t.Fatalf("scope mismatch reached update: %d writes", writes)
	}
	wrongSystem = false
	for _, invalid := range []string{`{}`, `{"system_id":2}`, `{"process_status":"DONE"}`, `{"description":42}`, `{"approved_at":"2026-10-20"}`, `{"summary_status":"ready"}`, `{"metadata":"invalid"}`, `{"evidence":"invalid"}`} {
		if err := os.WriteFile(file, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := runRoot(t, "epics", "update", "42", "--file", file); err == nil {
			t.Fatalf("accepted invalid update: %s", invalid)
		}
	}
	if writes != 1 {
		t.Fatalf("invalid payload reached update: %d writes", writes)
	}
}

func TestAgentEpicUpdateForwardsAuthoredPlanningMetadata(t *testing.T) {
	input := `{"assignee_id":1,"release_id":2,"start_date":"2026-10-20","target_date":"2026-11-06","outcome_source":"Reviewable draft scope","severity":"low","category":"planning","metadata":{"demo":true},"shared_context":{"version":1},"impact_assessment":{"version":1},"knowledge_config":{"pattern_status":"golden"},"evidence":[{"description":"Observed locally"}]}`
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		if r.Method == http.MethodPut {
			writes++
			var body struct {
				Epic map[string]any `json:"epic"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Epic["target_date"] != "2026-11-06" || body.Epic["assignee_id"] != float64(1) || len(body.Epic) != 12 {
				t.Errorf("authored metadata changed: %#v", body.Epic)
			}
		}
		_, _ = w.Write([]byte(`{"data":{"id":42,"system_id":1,"title":"Local demo"}}`))
	}))
	defer server.Close()
	cobraWorkspace(t, server)
	file := filepath.Join(t.TempDir(), "epic-fields.json")
	if err := os.WriteFile(file, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runRoot(t, "epics", "update", "42", "--file", file, "--json"); err != nil {
		t.Fatal(err)
	}
	if writes != 1 {
		t.Fatalf("expected one update, got %d", writes)
	}
}

func TestAgentEpicSpecsBundleRoundTripAndPartialFailure(t *testing.T) {
	failed := false
	var imported map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/systems":
			healthyProbe(w, r)
		case "/api/work/epics/42":
			w.Write([]byte(`{"data":{"id":42,"target_architecture_id":1}}`))
		case "/api/work/epics/42/export-specs":
			w.Write([]byte(`{"data":{"epic_id":42,"specs":{"architecture":[{"filename":"overview.md","content":{"text":"# Working content"}}]}}}`))
		case "/api/work/epics/42/import-specs":
			if err := json.NewDecoder(r.Body).Decode(&imported); err != nil {
				t.Error(err)
			}
			if failed {
				w.Write([]byte(`{"data":{"imported":0,"created":0,"updated":0,"errors":1}}`))
			} else {
				w.Write([]byte(`{"data":{"imported":1,"created":0,"updated":1,"errors":0}}`))
			}
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	cobraWorkspace(t, server)
	file := filepath.Join(t.TempDir(), "working", "specs.json")
	if _, err := runRoot(t, "epics", "specs", "pull", "42", "--file", file); err != nil {
		t.Fatal(err)
	}
	if _, err := runRoot(t, "epics", "specs", "push", "42", "--file", file); err != nil {
		t.Fatal(err)
	}
	rows, ok := imported["specs"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("invalid import: %#v", imported)
	}
	row := rows[0].(map[string]any)
	if row["content"] != "# Working content" || row["filename"] != "overview.md" || row["category"] != "architecture" {
		t.Fatalf("changed specification: %#v", row)
	}
	failed = true
	if _, err := runRoot(t, "epics", "specs", "push", "42", "--file", file); err == nil {
		t.Fatal("partial import reported success")
	}
}

func TestAgentEpicErrorsAndScopeDoNotReportSuccess(t *testing.T) {
	calledWrite := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		if r.URL.Path == "/api/work/epics/42" {
			w.Write([]byte(`{"data":{"id":42,"target_architecture_id":2}}`))
			return
		}
		if r.Method != "GET" {
			calledWrite = true
		}
		if strings.HasSuffix(r.URL.Path, "/plan") {
			w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"unavailable"}`))
	}))
	defer server.Close()
	cobraWorkspace(t, server)
	for _, args := range [][]string{{"epics", "list"}, {"epics", "plan", "show", "42"}, {"epics", "specs", "generate", "42"}} {
		if _, err := runRoot(t, args...); err == nil {
			t.Fatalf("reported success for %v", args)
		}
	}
	if calledWrite {
		t.Fatal("wrote to an Epic outside the bound System")
	}
	if _, _, err := rootCmd.Find([]string{"work", "select"}); err == nil {
		t.Fatal("retained shared active Epic selection")
	}
}
