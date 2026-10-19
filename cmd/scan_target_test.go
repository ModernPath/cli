package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestScanHonoursGlobalAPIURLOverride(t *testing.T) {
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits.Add(1)
		_, _ = io.WriteString(w, `{"success":true,"data":{"system_id":1,"system_name":"T","mode":"full","agents_used":["antivibe"],"summary":{"total_findings":0},"findings":[]}}`)
	}))
	defer target.Close()

	var configuredHits atomic.Int32
	configured := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		configuredHits.Add(1)
		http.Error(w, "wrong server", http.StatusInternalServerError)
	}))
	defer configured.Close()

	cobraWorkspace(t, configured)
	if _, err := runRoot(t, "--api-url", target.URL, "scan"); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if got := targetHits.Load(); got != 1 {
		t.Fatalf("override target received %d requests, want 1", got)
	}
	if got := configuredHits.Load(); got != 0 {
		t.Fatalf("configured target received %d requests despite --api-url override", got)
	}
}
