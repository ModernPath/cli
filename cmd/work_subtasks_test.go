// The mux mirrors the canonical Task endpoint and embedded Subtasks.
package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func subtasksTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/work/tasks/abc-123", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"id":    "abc-123",
				"code":  "TASK-001",
				"title": "Sandbox task",
				"subtasks": []map[string]interface{}{
					{"id": "s1", "code": "SUB-001", "title": "First subtask", "status": "todo", "estimated_points": 3},
					{"id": "s2", "code": "SUB-002", "title": "Second subtask", "status": "in_progress", "estimated_points": 5},
				},
			},
		})
	})
	mux.HandleFunc("/api/work/tasks/missing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"Task not found"}`))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestFetchSubtasksUsesTheRouteTheServerHas(t *testing.T) {
	server := subtasksTestServer(t)

	subtasks, err := fetchSubtasks(server.URL, "abc-123")
	if err != nil {
		t.Fatalf("fetchSubtasks against the real route shape failed: %v", err)
	}
	if len(subtasks) != 2 {
		t.Fatalf("expected the task's 2 subtasks, got %d", len(subtasks))
	}
	if subtasks[0].Code != "SUB-001" || subtasks[1].Status != "in_progress" {
		t.Fatalf("subtasks not mapped: %+v", subtasks)
	}
	if subtasks[1].EstimatedPoints != 5 {
		t.Fatalf("estimated_points not mapped: %+v", subtasks[1])
	}
}

func TestFetchSubtasksNamesTheFailureOnUnknownTask(t *testing.T) {
	server := subtasksTestServer(t)

	_, err := fetchSubtasks(server.URL, "missing")
	if err == nil {
		t.Fatal("an unknown task must be an error, not an empty success")
	}
	if !strings.Contains(err.Error(), "Task not found") {
		t.Fatalf("the error should carry the server's reason, got: %v", err)
	}
}
