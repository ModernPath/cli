package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/platform"
)

type lifecycleRefusal struct {
	code   string
	status int
	err    error
}

func (e lifecycleRefusal) Error() string { return e.err.Error() }
func (e lifecycleRefusal) Unwrap() error { return e.err }

func lifecycleWithPreparation(ctx context.Context, cfg *config.Config, route string, body map[string]string, mode string) (*lifecycleAnswer, error) {
	return lifecycleWithPreparationTimeout(ctx, cfg, route, body, mode, defaultAnalysisTimeout, false)
}

func lifecycleWithPreparationTimeout(ctx context.Context, cfg *config.Config, route string, body map[string]string, mode string, timeout time.Duration, implicitStart bool) (*lifecycleAnswer, error) {
	answer, err := postLifecycleContext(ctx, cfg, route, body)
	var refusal lifecycleRefusal
	if !errors.As(err, &refusal) || refusal.status != http.StatusConflict || (refusal.code != "source_materials_changed" && refusal.code != "source_materials_not_ready") {
		return answer, err
	}
	prepCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := newAuthenticatedClient(cfg)
	// The stream has its own context deadline; the ordinary client timeout would
	// cut a long-running preparation short after two minutes.
	client.client.Timeout = 0
	if mode == "" || implicitStart {
		persistedMode, readErr := preparationSystemMode(prepCtx, client, cfg.SystemID)
		if readErr != nil {
			return nil, readErr
		}
		if implicitStart && persistedMode != mode {
			return nil, fmt.Errorf("%w; the system uses %s; pass --mode %s to prepare it", err, persistedMode, persistedMode)
		}
		if mode == "" {
			mode = persistedMode
		}
	}
	if err := prepareSourceMaterials(prepCtx, client, cfg.SystemID, mode); err != nil {
		return nil, err
	}
	return postLifecycleContext(ctx, cfg, route, body)
}

func preparationRequest(ctx context.Context, client *authenticatedClient, method, url string, body io.Reader, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	platform.Prepare(req)
	req.Header.Set("Accept", accept)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if err := platform.Authorize(req, client.token); err != nil {
		return nil, err
	}
	return client.client.Do(req)
}

func preparationSystemMode(ctx context.Context, client *authenticatedClient, systemID int) (string, error) {
	resp, err := preparationRequest(ctx, client, http.MethodGet, fmt.Sprintf("%s/api/systems/%d", client.baseURL, systemID), nil, "application/json")
	if err != nil {
		return "", fmt.Errorf("read system mode: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", pushRefusal(resp, raw)
	}
	var system struct {
		AnalysisMode string `json:"analysis_mode"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&system); err != nil {
		return "", fmt.Errorf("read system mode: %w", err)
	}
	if err := validateAnalysisMode(system.AnalysisMode); err != nil {
		return "", fmt.Errorf("invalid system analysis mode: %w", err)
	}
	return system.AnalysisMode, nil
}

func prepareSourceMaterials(ctx context.Context, client *authenticatedClient, systemID int, mode string) error {
	payload, _ := json.Marshal(map[string]string{"analysis_mode": mode})
	base := fmt.Sprintf("%s/api/systems/%d", client.baseURL, systemID)
	resp, err := preparationRequest(ctx, client, http.MethodPost, base+"/source-materials/prepare", bytes.NewReader(payload), "application/json")
	if err != nil {
		return fmt.Errorf("start source preparation: %w", err)
	}
	raw, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return fmt.Errorf("read source preparation: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return pushRefusal(resp, raw)
	}
	var envelope struct {
		Data struct {
			JobID  string `json:"job_id"`
			Mode   string `json:"analysis_mode"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return fmt.Errorf("invalid source preparation response")
	}
	_, jobErr := canonicalRunID(envelope.Data.JobID)
	if jobErr != nil || envelope.Data.Mode != mode || !slices.Contains([]string{"queued", "running", "retrying", "completed", "failed"}, envelope.Data.Status) {
		return fmt.Errorf("invalid source preparation response")
	}
	return waitForPreparation(ctx, client, base, envelope.Data.JobID, mode)
}

func waitForPreparation(ctx context.Context, client *authenticatedClient, base, jobID, mode string) error {
	resp, err := preparationRequest(ctx, client, http.MethodGet, base+"/analysis/events", nil, "text/event-stream")
	if err != nil {
		return fmt.Errorf("source preparation stream: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return pushRefusal(resp, raw)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return fmt.Errorf("invalid source preparation stream content type")
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var event string
	var data []string
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if event == "source_materials_status" || event == "source_materials_progress" || event == "source_materials_complete" || event == "source_materials_failed" {
				var status struct {
					JobID   string `json:"job_id"`
					Status  string `json:"status"`
					Mode    string `json:"analysis_mode"`
					Reason  string `json:"reason"`
					Message string `json:"message"`
				}
				if json.Unmarshal([]byte(strings.Join(data, "\n")), &status) != nil || status.JobID == "" || status.Status == "" {
					return fmt.Errorf("invalid source preparation event")
				}
				if status.JobID == jobID {
					if status.Mode != mode {
						return fmt.Errorf("source preparation mode changed")
					}
					switch status.Status {
					case "completed":
						if event == "source_materials_failed" {
							return fmt.Errorf("invalid source preparation event")
						}
						return nil
					case "failed":
						reason, message := strings.TrimSpace(status.Reason), strings.TrimSpace(status.Message)
						if reason == "" {
							if message == "" {
								return fmt.Errorf("source preparation failed")
							}
							return errors.New(message)
						}
						if message == "" {
							return errors.New(reason)
						}
						if strings.HasPrefix(message, reason+":") {
							if message == "" {
								return fmt.Errorf("source preparation failed")
							}
							return errors.New(message)
						}
						return fmt.Errorf("%s: %s", reason, message)
					case "queued", "running", "retrying":
						if event == "source_materials_complete" || event == "source_materials_failed" {
							return fmt.Errorf("invalid source preparation event")
						}
					default:
						return fmt.Errorf("invalid source preparation status %q", status.Status)
					}
				}
			}
			event = ""
			data = nil
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("source preparation stream: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("source preparation stream: %w", err)
	}
	return fmt.Errorf("source preparation stream ended before job %s completed", jobID)
}
