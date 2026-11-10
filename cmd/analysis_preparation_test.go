package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modernpath/cli/internal/config"
)

// Exercise the real lifecycle and event-stream HTTP boundaries, including the
// snapshot sent when preparation finishes before the stream connects.
func TestAnalysisPreparationRecovery(t *testing.T) {
	for _, tc := range []struct{ name, verb, code, mode, event string }{
		{"start changed", "start", "source_materials_changed", "unified_workspace", "source_materials_status"},
		{"start not ready", "start", "source_materials_not_ready", "unified_workspace", "source_materials_complete"},
		{"reanalyze", "reanalyze", "source_materials_not_ready", "unified_workspace", "source_materials_status"},
		{"reanalyze changed", "reanalyze", "source_materials_changed", "unified_workspace", "source_materials_status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			prepared := false
			s := newLifecycleStub(t)
			s.server.Close()
			s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case analyzePath, reanalyzePath:
					calls++
					if calls == 1 {
						w.WriteHeader(409)
						_ = json.NewEncoder(w).Encode(refusal(tc.code, "prepare"))
						return
					}
					_ = json.NewEncoder(w).Encode(lifecycleAccepted("15", "running", tc.mode))
				case "/api/systems/42":
					_ = json.NewEncoder(w).Encode(map[string]any{"analysis_mode": tc.mode})
				case "/api/systems/42/source-materials/prepare":
					var body map[string]string
					_ = json.NewDecoder(r.Body).Decode(&body)
					if body["analysis_mode"] != tc.mode {
						t.Errorf("preparation mode = %q, want %q", body["analysis_mode"], tc.mode)
					}
					prepared = true
					w.WriteHeader(http.StatusAccepted)
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"job_id": "77", "status": "queued", "analysis_mode": tc.mode}})
				case "/api/systems/42/analysis/events":
					if r.Header.Get("Authorization") != "Bearer t" {
						t.Errorf("missing stream authorization")
					}
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "event: source_materials_complete\ndata: {\"job_id\":\"76\",\"status\":\"completed\"}\n\n")
					fmt.Fprintf(w, "event: %s\ndata: {\"job_id\":\"77\",\"status\":\"completed\",\"analysis_mode\":%q}\n\n", tc.event, tc.mode)
				default:
					http.NotFound(w, r)
				}
			}))
			bindLifecycleWorkspace(t, s, 7)
			args := []string{"analysis", tc.verb}
			if tc.verb == "start" {
				args = append(args, "--mode", tc.mode)
			} else {
				args = append(args, "7")
			}
			args = append(args, "--json")
			stdout, stderr, err := runAskStreams(t, args...)
			if err != nil {
				t.Fatalf("recovery: %v, stderr=%s", err, stderr)
			}
			var output map[string]any
			if json.Unmarshal([]byte(stdout), &output) != nil || output["analysis_mode"] != tc.mode {
				t.Errorf("stdout is not lifecycle JSON: %q", stdout)
			}
			if calls != 2 || !prepared {
				t.Errorf("lifecycle calls=%d prepared=%v, want 2 and true", calls, prepared)
			}
		})
	}
}

func TestAnalysisPreparationDoesNotRetryFailure(t *testing.T) {
	for _, event := range []string{
		"event: source_materials_failed\ndata: {\"job_id\":\"77\",\"status\":\"failed\",\"analysis_mode\":\"independent_repos\",\"reason\":\"source_materials_changed\"}\n\n",
		"event: source_materials_complete\ndata: invalid\n\n",
		"event: source_materials_complete\ndata: {\"job_id\":\"76\",\"status\":\"completed\"}\n\n",
	} {
		t.Run(strings.ReplaceAll(event[:29], " ", "_"), func(t *testing.T) {
			calls, preparations, streams := 0, 0, 0
			s := newLifecycleStub(t)
			s.server.Close()
			s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case analyzePath:
					calls++
					w.WriteHeader(409)
					_ = json.NewEncoder(w).Encode(refusal("source_materials_not_ready", "prepare"))
				case "/api/systems/42":
					_ = json.NewEncoder(w).Encode(map[string]string{"analysis_mode": "independent_repos"})
				case "/api/systems/42/source-materials/prepare":
					preparations++
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"job_id": "77", "status": "queued", "analysis_mode": "independent_repos"}})
				case "/api/systems/42/analysis/events":
					streams++
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, event)
				default:
					http.NotFound(w, r)
				}
			}))
			bindLifecycleWorkspace(t, s, 7)
			_, err := runVerb(t, "analysis", "start")
			if err == nil || calls != 1 || preparations != 1 || streams != 1 {
				t.Errorf("error=%v lifecycle calls=%d preparations=%d streams=%d", err, calls, preparations, streams)
			}
		})
	}
}

func TestAnalysisPreparationRefusalsAndRetryLimit(t *testing.T) {
	for _, tc := range []struct {
		verb, code string
		retry      bool
	}{
		{"start", "source_materials_changed", true}, {"reanalyze", "source_materials_not_ready", true},
		{"start", "analysis_already_complete", false}, {"reanalyze", "repository_not_found", false}, {"reset", "source_materials_not_ready", false},
	} {
		t.Run(tc.verb+"_"+tc.code, func(t *testing.T) {
			calls, preparations := 0, 0
			s := newLifecycleStub(t)
			s.server.Close()
			s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case analyzePath, reanalyzePath, resetPath:
					calls++
					w.WriteHeader(409)
					_ = json.NewEncoder(w).Encode(refusal(tc.code, "refused"))
				case "/api/systems/42":
					_ = json.NewEncoder(w).Encode(map[string]string{"analysis_mode": "independent_repos"})
				case "/api/systems/42/source-materials/prepare":
					preparations++
					w.WriteHeader(202)
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"job_id": "77", "status": "queued", "analysis_mode": "independent_repos"}})
				case "/api/systems/42/analysis/events":
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "event: source_materials_status\ndata: {\"job_id\":\"77\",\"status\":\"completed\",\"analysis_mode\":\"independent_repos\"}\n\n")
				default:
					http.NotFound(w, r)
				}
			}))
			bindLifecycleWorkspace(t, s, 7)
			args := []string{"analysis", tc.verb}
			if tc.verb == "reanalyze" {
				args = append(args, "7")
			}
			if tc.verb == "reset" {
				args = append(args, "--yes")
			}
			out, err := runVerb(t, args...)
			wantCalls, wantPreparations := 1, 0
			if tc.retry {
				wantCalls, wantPreparations = 2, 1
			}
			if err == nil || calls != wantCalls || preparations != wantPreparations || strings.Count(out, tc.code) != 1 {
				t.Errorf("error=%v calls=%d preparations=%d output=%s", err, calls, preparations, out)
			}
		})
	}
}

func TestPreparationStreamDeadlineAndWrongMode(t *testing.T) {
	for _, tc := range []struct {
		name, event string
		cancel      bool
	}{
		{"wrong mode", "event: source_materials_complete\ndata: {\"job_id\":\"77\",\"status\":\"completed\",\"analysis_mode\":\"unified_workspace\"}\n\n", false},
		{"cancelled", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				if tc.cancel {
					<-r.Context().Done()
					return
				}
				fmt.Fprint(w, tc.event)
			}))
			defer server.Close()
			timeout := time.Second
			if tc.cancel {
				timeout = 50 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			client := &authenticatedClient{client: server.Client(), baseURL: server.URL}
			err := waitForPreparation(ctx, client, server.URL, "77", "independent_repos")
			if tc.cancel && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected deadline, got %v", err)
			}
			if !tc.cancel && (err == nil || !strings.Contains(err.Error(), "mode changed")) {
				t.Fatalf("expected mode error, got %v", err)
			}
		})
	}
}

func TestAnalysisPreparationDeadlineDoesNotRetry(t *testing.T) {
	calls, streams := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case analyzePath:
			calls++
			w.WriteHeader(409)
			_ = json.NewEncoder(w).Encode(refusal("source_materials_not_ready", "prepare"))
		case "/api/systems/42/source-materials/prepare":
			w.WriteHeader(202)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"job_id": "77", "status": "queued", "analysis_mode": "independent_repos"}})
		case "/api/systems/42/analysis/events":
			streams++
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := lifecycleWithPreparation(ctx, &config.Config{APIURL: server.URL, SystemID: 42}, "analyze", map[string]string{"analysis_mode": "independent_repos"}, "independent_repos")
	if !errors.Is(err, context.DeadlineExceeded) || calls != 1 || streams != 1 {
		t.Errorf("error=%v calls=%d streams=%d", err, calls, streams)
	}
}

func TestAnalysisPreparationCancelDoesNotRetry(t *testing.T) {
	calls := 0
	connected := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case analyzePath:
			calls++
			w.WriteHeader(409)
			_ = json.NewEncoder(w).Encode(refusal("source_materials_not_ready", "prepare"))
		case "/api/systems/42/source-materials/prepare":
			w.WriteHeader(202)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"job_id": "77", "status": "queued", "analysis_mode": "independent_repos"}})
		case "/api/systems/42/analysis/events":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			close(connected)
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := lifecycleWithPreparation(ctx, &config.Config{APIURL: server.URL, SystemID: 42}, "analyze", map[string]string{"analysis_mode": "independent_repos"}, "independent_repos")
		result <- err
	}()
	<-connected
	cancel()
	err := <-result
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Errorf("error=%v lifecycle calls=%d", err, calls)
	}
}

func TestAnalysisInitialRequestCancellation(t *testing.T) {
	connected := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(connected)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := lifecycleWithPreparation(ctx, &config.Config{APIURL: server.URL, SystemID: 42}, "analyze", map[string]string{"analysis_mode": "independent_repos"}, "independent_repos")
		result <- err
	}()
	<-connected
	cancel()
	err := <-result
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestAnalysisImplicitModeRecoveryGuard(t *testing.T) {
	for _, tc := range []struct {
		name, persisted, explicit, requested string
		recover                              bool
	}{
		{"implicit unified", "unified_workspace", "", "independent_repos", false},
		{"implicit independent", "independent_repos", "", "independent_repos", true},
		{"explicit independent", "unified_workspace", "independent_repos", "independent_repos", true},
		{"explicit unified", "independent_repos", "unified_workspace", "unified_workspace", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, prepares := 0, 0
			s := newLifecycleStub(t)
			s.server.Close()
			s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case analyzePath:
					calls++
					var body map[string]string
					_ = json.NewDecoder(r.Body).Decode(&body)
					if body["analysis_mode"] != tc.requested {
						t.Errorf("lifecycle mode %q", body["analysis_mode"])
					}
					if calls == 1 {
						w.WriteHeader(409)
						_ = json.NewEncoder(w).Encode(refusal("source_materials_not_ready", "prepare"))
						return
					}
					_ = json.NewEncoder(w).Encode(lifecycleAccepted("15", "running", tc.requested))
				case "/api/systems/42":
					_ = json.NewEncoder(w).Encode(map[string]string{"analysis_mode": tc.persisted})
				case "/api/systems/42/source-materials/prepare":
					prepares++
					var body map[string]string
					_ = json.NewDecoder(r.Body).Decode(&body)
					if body["analysis_mode"] != tc.requested {
						t.Errorf("prepare mode %q", body["analysis_mode"])
					}
					w.WriteHeader(202)
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"job_id": "77", "status": "queued", "analysis_mode": tc.requested}})
				case "/api/systems/42/analysis/events":
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, "event: source_materials_complete\ndata: {\"job_id\":\"77\",\"status\":\"completed\",\"analysis_mode\":%q}\n\n", tc.requested)
				default:
					http.NotFound(w, r)
				}
			}))
			bindLifecycleWorkspace(t, s, 7)
			args := []string{"analysis", "start"}
			if tc.explicit != "" {
				args = append(args, "--mode", tc.explicit)
			}
			out, err := runVerb(t, args...)
			if tc.recover {
				if err != nil || calls != 2 || prepares != 1 {
					t.Errorf("err=%v calls=%d prepares=%d output=%s", err, calls, prepares, out)
				}
			} else if err == nil || calls != 1 || prepares != 0 || !strings.Contains(out, "--mode unified_workspace") || !strings.Contains(out, "source_materials_not_ready") {
				t.Errorf("err=%v calls=%d prepares=%d output=%s", err, calls, prepares, out)
			}
		})
	}
}

func TestAnalysisPreparationFailureMessage(t *testing.T) {
	for _, tc := range []struct{ reason, message, want string }{
		{"source_materials_changed", "The sources changed.", "source_materials_changed: The sources changed."},
		{"source_materials_changed", "source_materials_changed: The sources changed.", "source_materials_changed: The sources changed."},
		{"", "The sources changed.", "The sources changed."},
		{"source_materials_changed", "Source preparation failed: the sources changed.", "source_materials_changed: Source preparation failed: the sources changed."},
	} {
		t.Run(tc.message, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				payload, _ := json.Marshal(map[string]string{"job_id": "77", "status": "failed", "analysis_mode": "independent_repos", "reason": tc.reason, "message": tc.message})
				fmt.Fprintf(w, "event: source_materials_failed\ndata: %s\n\n", payload)
			}))
			defer server.Close()
			err := waitForPreparation(context.Background(), &authenticatedClient{client: server.Client()}, server.URL, "77", "independent_repos")
			if err == nil || err.Error() != tc.want {
				t.Errorf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestAnalysisPreparationRetryUsesCommandContext(t *testing.T) {
	retryStarted := make(chan struct{})
	release := make(chan struct{})
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case analyzePath:
			calls++
			if calls == 1 {
				w.WriteHeader(409)
				_ = json.NewEncoder(w).Encode(refusal("source_materials_not_ready", "prepare"))
				return
			}
			close(retryStarted)
			<-release
			_ = json.NewEncoder(w).Encode(lifecycleAccepted("15", "running", "independent_repos"))
		case "/api/systems/42/source-materials/prepare":
			w.WriteHeader(202)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"job_id": "77", "status": "queued", "analysis_mode": "independent_repos"}})
		case "/api/systems/42/analysis/events":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: source_materials_complete\ndata: {\"job_id\":\"77\",\"status\":\"completed\",\"analysis_mode\":\"independent_repos\"}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	result := make(chan error, 1)
	go func() {
		_, err := lifecycleWithPreparationTimeout(context.Background(), &config.Config{APIURL: server.URL, SystemID: 42}, "analyze", map[string]string{"analysis_mode": "independent_repos"}, "independent_repos", time.Second, false)
		result <- err
	}()
	select {
	case <-retryStarted:
	case err := <-result:
		close(release)
		t.Fatalf("preparation did not reach retry: %v", err)
	}
	timer := time.NewTimer(1200 * time.Millisecond)
	<-timer.C
	close(release)
	if err := <-result; err != nil {
		t.Fatalf("retry inherited preparation deadline: %v", err)
	}
}
