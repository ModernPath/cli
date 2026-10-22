package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/platform"
)

// REQ-CROSS-176: when a server call is slow enough to look like a hang, say
// something. Threshold and destination are variables so the tests can shrink
// one and capture the other.
var (
	slowCallNoticeAfter           = 10 * time.Second
	slowCallNoticeTo    io.Writer = os.Stderr
)

// REQ-CROSS-535 AC4: a request the server limits is sent again after a wait —
// Retry-After when given, else 2 s doubling — each wait at most 30 s and all
// of them at most 2 minutes. A hook never waits.
const (
	limitFirstWait = 2 * time.Second
	limitMaxWait   = 30 * time.Second
	limitAllWaits  = 2 * time.Minute
)

// limitSleep is the wait between limited requests; a variable so a test can
// record the waits instead of sleeping.
var limitSleep = time.Sleep

// defaultCallTimeout bounds a call whose env sets no callTimeout; a variable
// so a test can shrink it.
var defaultCallTimeout = 120 * time.Second

// get performs a read; GET requests cannot carry a payload.
func (e *cliEnv) get(apiPath string) (int, map[string]any, error) {
	return e.request(http.MethodGet, apiPath, nil)
}

func (e *cliEnv) write(method, apiPath string, payload any) (int, map[string]any, error) {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return e.request(method, apiPath, payload)
	default:
		return 0, nil, fmt.Errorf("write requires POST, PUT, PATCH or DELETE; got %s", method)
	}
}

func (e *cliEnv) request(method, apiPath string, payload any) (int, map[string]any, error) {
	var waited time.Duration
	next := limitFirstWait
	for {
		status, body, err := e.sendOnce(method, apiPath, payload)
		var limiting serverLimitingError
		if !errors.As(err, &limiting) || hookMode(os.Args[1:]) {
			return status, body, err
		}
		wait := next
		if limiting.retryAfter > 0 {
			wait = time.Duration(limiting.retryAfter) * time.Second
		}
		if wait > limitMaxWait {
			wait = limitMaxWait
		}
		if waited+wait > limitAllWaits {
			return status, body, err
		}
		fmt.Fprintf(slowCallNoticeTo, "… the server is limiting requests; trying %s %s again in %s\n", method, apiPath, wait)
		limitSleep(wait)
		waited += wait
		next *= 2
	}
}

// sendOnce sends one request.
func (e *cliEnv) sendOnce(method, apiPath string, payload any) (int, map[string]any, error) {
	if method == http.MethodGet && payload != nil {
		return 0, nil, fmt.Errorf("GET requests cannot carry a payload")
	}
	var body *bytes.Reader
	if method != http.MethodGet && method != http.MethodHead && payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(raw)
	} else {
		body = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, e.APIURL+apiPath, body)
	if err != nil {
		return 0, nil, err
	}
	platform.Prepare(req)
	req.Header.Set("Content-Type", "application/json")
	if err := platform.Authorize(req, e.token); err != nil {
		return 0, nil, err
	}

	// REQ-CROSS-176: a call that is taking long says so. Eight sequential
	// calls at 120s each can turn "a few seconds" into silent minutes on a
	// stalled connection, and a slow server is indistinguishable from a hung
	// CLI until this line exists.
	timeout := e.callTimeout
	if timeout == 0 {
		timeout = defaultCallTimeout
	}
	notice := time.AfterFunc(slowCallNoticeAfter, func() {
		fmt.Fprintf(slowCallNoticeTo, "… still waiting on %s %s (slow server or connection; times out at %s)\n", method, apiPath, timeout)
	})
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	notice.Stop()
	if err != nil {

		return 0, nil, err
	}
	defer resp.Body.Close()

	// REQ-CROSS-372: keep an undecodable body (a proxy or gateway error page)
	// as the refusal text instead of dropping it — every `server %d` formatter
	// then prints what the server sent, never <nil>.
	raw, _ := io.ReadAll(resp.Body)

	var decoded map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	decodeErr := decoder.Decode(&decoded)
	if decodeErr == nil {
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			decodeErr = fmt.Errorf("response contains more than one JSON value")
		}
	}
	if decodeErr != nil || decoded == nil {
		if resp.StatusCode < 300 {
			if decodeErr == nil {
				decodeErr = fmt.Errorf("response must be a JSON object")
			}
			return resp.StatusCode, nil, fmt.Errorf("invalid JSON response from server: %w", decodeErr)
		}
		if text := strings.TrimSpace(string(raw)); text != "" && resp.StatusCode >= 300 {
			decoded = map[string]any{"error": map[string]any{"reason": text}}
		} else if resp.StatusCode >= 300 {
			// REQ-CROSS-380: a bodyless refusal still has a status and a
			// reference; never leave the formatters a nil to print.
			decoded = map[string]any{"error": map[string]any{"reason": "no body"}}
		}
	}
	// REQ-CROSS-380: the reference rides the body so every formatter — all of
	// them route through serverRefusal — can cite it.
	if decoded != nil && resp.StatusCode >= 300 {
		if ref := resp.Header.Get("x-request-id"); ref != "" {
			if _, has := decoded["request_id"]; !has {
				decoded["request_id"] = ref
			}
		}
	}

	// Reported here rather than at each call site: all eight `server %d`
	// formatters check err first, so one point covers every API command.
	if resp.StatusCode == http.StatusUnauthorized {
		return resp.StatusCode, decoded, e.credentialRejected()
	}
	// REQ-CROSS-535: the edge limits requests and asks the client to wait.
	// Core's own 429 (a locked release PIN) carries its error body and stays
	// a refusal with its reason (REQ-CROSS-338/380).
	if resp.StatusCode == http.StatusTooManyRequests && !coreErrorBody(raw) {
		return resp.StatusCode, decoded, serverLimitingError{retryAfter: retryAfterSeconds(resp.Header.Get("Retry-After"), time.Now())}
	}
	return resp.StatusCode, decoded, nil
}
