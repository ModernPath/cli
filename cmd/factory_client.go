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

func (e *factoryEnv) call(method, apiPath string, payload any) (int, map[string]any, error) {
	status, body, err := e.send(method, apiPath, payload)
	// REQ-CROSS-538: a keeping verb's write that the outage stops is kept.
	if e.keep != nil && method != "GET" && isUnavailable(err) {
		return 0, nil, e.collect(method, apiPath, payload)
	}
	return status, body, err
}

func (e *factoryEnv) send(method, apiPath string, payload any) (int, map[string]any, error) {
	var waited time.Duration
	next := limitFirstWait
	for {
		status, body, err := e.sendOnce(method, apiPath, payload)
		var limiting serverLimitingError
		if !errors.As(err, &limiting) || hookMode(replayInvocation) {
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
func (e *factoryEnv) sendOnce(method, apiPath string, payload any) (int, map[string]any, error) {
	// REQ-CROSS-536: nothing is sent while an outage is known.
	if known, down := knownUnavailable(e.Root, time.Now()); down {
		return 0, nil, known
	}
	// REQ-CROSS-390: a write this build cannot mean is refused before it leaves.
	if err := e.checkWrite(method, apiPath, payload); err != nil {
		return 0, nil, err
	}
	var body *bytes.Reader
	if payload != nil {
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
		// REQ-CROSS-535: a transport failure or timeout is an outage only
		// when the probe fails too; otherwise the caller sees what it did.
		if outage := e.confirmOutage(err, 0); outage != nil {
			return 0, nil, outage
		}
		return 0, nil, err
	}
	defer resp.Body.Close()

	// REQ-CROSS-372: keep an undecodable body (a proxy or gateway error page)
	// as the refusal text instead of dropping it — every `server %d` formatter
	// then prints what the server sent, never <nil>.
	raw, _ := io.ReadAll(resp.Body)

	// REQ-CROSS-535: classified on the raw body, before the wrap below makes
	// every body look like core's. An unconfirmed gateway answer returns as
	// before, so the sync batch still retries its 504s.
	if gatewayAnswer(resp.StatusCode, resp.Header, raw) {
		if outage := e.confirmOutage(nil, resp.StatusCode); outage != nil {
			return 0, nil, outage
		}
	} else {
		clearUnavailable(e.Root, time.Now())
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded == nil {
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

	// REQ-CROSS-348: the store names its revision on every response; keep the
	// last one seen so a snapshot header can record the store it came from.
	if rev := resp.Header.Get("x-modernpath-store-revision"); rev != "" {
		e.storeRevision = rev
	}
	// REQ-CROSS-390: the served contract version, warned about once when newer.
	e.noteServedContract(resp.Header.Get("x-modernpath-contract"))

	// Reported here rather than at each call site: all eight `server %d`
	// formatters check err first, so one point covers every factory command.
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
