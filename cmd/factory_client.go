package cmd

import (
	"bytes"
	"encoding/json"
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

func (e *factoryEnv) call(method, apiPath string, payload any) (int, map[string]any, error) {
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
		timeout = 120 * time.Second
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
	return resp.StatusCode, decoded, nil
}
