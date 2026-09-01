package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// REQ-CROSS-176: a long export poll says it is still alive.
//
// `docs sync` runs a server-side export job and polls it for up to 30 minutes,
// printing only when the job's status string CHANGES. A large system can sit in
// one status for minutes while the server builds the zip — during which the CLI
// prints nothing and is indistinguishable from a hang. Reported as "sync
// sometimes jams; it typically completes in a few seconds" (USER:2026-08-15):
// the fast case is a cached export, the "jam" is a rebuild with no output.
//
// The fix is a heartbeat: when the status has NOT changed for
// exportHeartbeatEvery, report it again with the elapsed time, so a working
// wait and a dead one stop looking identical.
func TestDownloadExportHeartbeatsDuringLongUnchangedStatus(t *testing.T) {
	prevPoll, prevBeat := exportPollInterval, exportHeartbeatEvery
	exportPollInterval, exportHeartbeatEvery = time.Millisecond, 3*time.Millisecond
	defer func() { exportPollInterval, exportHeartbeatEvery = prevPoll, prevBeat }()

	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && strings.Contains(r.URL.Path, "/export/jobs"):
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{
				"id":            "job1",
				"poll_path":     "/api/systems/7/export/jobs/job1",
				"download_path": "/api/systems/7/export/jobs/job1/file",
			})
		case strings.HasSuffix(r.URL.Path, "/file"):
			w.Write([]byte("PK\x03\x04zip"))
		default:
			polls++
			st := "generating"
			if polls > 8 {
				st = "ready"
			}
			json.NewEncoder(w).Encode(map[string]string{"status": st})
		}
	}))
	defer srv.Close()

	var calls []string
	client := &Client{BaseURL: srv.URL, Token: "t", HTTPClient: srv.Client()}
	if _, err := client.DownloadExportWithProgress(7, func(status, progress string) {
		calls = append(calls, status+"|"+progress)
	}); err != nil {
		t.Fatal(err)
	}

	generating := 0
	elapsed := false
	for _, c := range calls {
		if strings.HasPrefix(c, "generating|") {
			generating++
			if strings.Contains(c, "elapsed") {
				elapsed = true
			}
		}
	}
	if generating < 2 {
		t.Fatalf("progress reported %d time(s) during 8 polls of one unchanged status — silence reads as a hang (calls: %v)", generating, calls)
	}
	if !elapsed {
		t.Fatalf("no heartbeat carries the elapsed time — the reader cannot tell waiting from dead (calls: %v)", calls)
	}
}
