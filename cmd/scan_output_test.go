package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestScanJSONReturnsFindingsAndSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/systems/7/scan" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"system_id":7,"system_name":"Demo","mode":"full","agents_used":["antivibe"],"vibe_debt":null,"summary":{"total_findings":0,"by_severity":{},"by_category":{},"by_agent":{}},"findings":[]}}`))
	}))
	t.Cleanup(server.Close)
	cobraWorkspace(t, server, 7)
	stdout, stderr, err := runAskStreams(t, "scan", "--json")
	if err != nil {
		t.Fatalf("scan --json: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("scan stdout must be JSON only, got %q: %v", stdout, err)
	}
	if result["success"] != true || result["system_id"] != float64(7) || result["summary"] == nil || result["findings"] == nil {
		t.Fatalf("unexpected scan result: %#v", result)
	}
	if stderr != "" {
		t.Fatalf("successful JSON scan should not emit diagnostics: %s", stderr)
	}
}

func TestScanListAgentsJSONIsOnlyTheResult(t *testing.T) {
	stdout, stderr, err := runAskStreams(t, "scan", "--list-agents", "--json")
	if err != nil {
		t.Fatalf("scan --list-agents --json: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("agent listing stdout must be JSON only, got %q: %v", stdout, err)
	}
	if len(rows) != 3 || rows[0]["type"] != "antivibe" {
		t.Fatalf("unexpected agent list: %#v", rows)
	}
	if stderr != "" {
		t.Fatalf("JSON agent listing should not emit diagnostics: %s", stderr)
	}
}
