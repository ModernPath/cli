package cmd

// REQ-CROSS-535 (EPIC-CLI-OUTAGE) — an outage is told apart from a refusal and
// remembered. A store request that fails in transport, times out, or gets a
// gateway 502/503/504 that core did not write is a candidate; the bounded
// reachability probe confirms it. Only a confirmed outage is recorded in
// .modernpath/server-availability.json and ends the command with exit 75.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
)

// exitTempFailure is EX_TEMPFAIL: the command may succeed when run again later.
const exitTempFailure = 75

const availabilityFileName = "server-availability.json"

const (
	availabilityUp   = "available"
	availabilityDown = "unavailable"
)

// availabilityState is the workspace's last known server availability.
type availabilityState struct {
	State       string    `json:"state"`
	Since       time.Time `json:"since"`
	LastChecked time.Time `json:"last_checked"`
	Reason      string    `json:"reason,omitempty"`
	// NextCheck ends the window in which nothing is sent; Confirmations
	// counts the confirmed failures that set it (REQ-CROSS-536 AC1).
	NextCheck     time.Time `json:"next_check,omitzero"`
	Confirmations int       `json:"confirmations,omitempty"`
}

// REQ-CROSS-536: the window after a confirmed outage in which nothing calls
// the server — 15 s, doubling with each further confirmed failure up to 5
// minutes.
const (
	outageFirstWindow = 15 * time.Second
	outageMaxWindow   = 5 * time.Minute
)

func outageWindowFor(confirmations int) time.Duration {
	w := outageFirstWindow
	for i := 1; i < confirmations && w < outageMaxWindow; i++ {
		w *= 2
	}
	if w > outageMaxWindow {
		w = outageMaxWindow
	}
	return w
}

func availabilityStatePath(root string) string {
	return filepath.Join(root, config.ConfigDir, availabilityFileName)
}

// loadAvailability reads the state; ok is false when there is none.
func loadAvailability(root string) (availabilityState, bool) {
	var st availabilityState
	raw, err := os.ReadFile(availabilityStatePath(root))
	if err != nil || json.Unmarshal(raw, &st) != nil {
		return availabilityState{}, false
	}
	return st, true
}

func writeAvailability(root string, st availabilityState) error {
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(availabilityStatePath(root), append(raw, '\n'))
}

// recordUnavailable stores a confirmed outage and returns since: the start of
// the outage already on record, or now.
func recordUnavailable(root, reason string, now time.Time) time.Time {
	now = now.UTC()
	since := now
	if root == "" {
		return since
	}
	confirmations := 1
	if st, ok := loadAvailability(root); ok && st.State == availabilityDown {
		if !st.Since.IsZero() {
			since = st.Since
		}
		confirmations = st.Confirmations + 1
		if confirmations < 2 {
			confirmations = 2
		}
	}
	_ = writeAvailability(root, availabilityState{State: availabilityDown, Since: since, LastChecked: now, Reason: reason,
		NextCheck: now.Add(outageWindowFor(confirmations)), Confirmations: confirmations})
	return since
}

// clearUnavailable marks a recorded outage over. It never creates the file:
// a workspace that has seen no outage keeps no state.
func clearUnavailable(root string, now time.Time) {
	if root == "" {
		return
	}
	if st, ok := loadAvailability(root); ok && st.State == availabilityDown {
		now = now.UTC()
		_ = writeAvailability(root, availabilityState{State: availabilityUp, Since: now, LastChecked: now})
	}
}

// serverUnavailableError is a confirmed outage: not a refusal, nothing the
// request did wrong.
type serverUnavailableError struct {
	since time.Time
	cause string
}

func (e serverUnavailableError) Error() string {
	return fmt.Sprintf("ModernPath server unavailable since %s (%s). This is not a refusal: your work continues; retry when the server is back.",
		e.since.Local().Format("15:04"), e.cause)
}

// serverLimitingError is a 429: the server is up and asks the client to wait.
type serverLimitingError struct {
	retryAfter int // seconds; 0 when the server named none
}

func (e serverLimitingError) Error() string {
	if e.retryAfter <= 0 {
		return "The ModernPath server is limiting requests; retry shortly."
	}
	return fmt.Sprintf("The ModernPath server is limiting requests; retry after %d s.", e.retryAfter)
}

// retryAfterSeconds reads Retry-After as seconds or as an HTTP date.
func retryAfterSeconds(h string, now time.Time) int {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	if n, err := strconv.Atoi(h); err == nil && n >= 0 {
		return n
	}
	if at, err := http.ParseTime(h); err == nil {
		if d := at.Sub(now); d > 0 {
			return int((d + time.Second - 1) / time.Second)
		}
	}
	return 0
}

// coreErrorBody reports a body core wrote: a JSON object whose error is an
// object. A gateway's plain text or a proxy's {"error": "…"} is not.
func coreErrorBody(raw []byte) bool {
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		return false
	}
	_, ok := body["error"].(map[string]any)
	return ok
}

// gatewayAnswer reports a 502/503/504 core did not write: no core error body
// and no store revision header.
func gatewayAnswer(status int, header http.Header, raw []byte) bool {
	switch status {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
	default:
		return false
	}
	return header.Get("x-modernpath-store-revision") == "" && !coreErrorBody(raw)
}

// outageCause names a transport failure or a gateway status in a few words.
func outageCause(err error, status int) string {
	if err == nil {
		return fmt.Sprintf("gateway answered %d %s", status, http.StatusText(status))
	}
	var ne interface{ Timeout() bool }
	if errors.As(err, &ne) && ne.Timeout() {
		return "no answer before the timeout"
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return "connection refused"
	case strings.Contains(msg, "connection reset"):
		return "connection reset"
	case strings.Contains(msg, "no such host"):
		return "server name not found"
	case strings.Contains(strings.ToLower(msg), "tls"), strings.Contains(msg, "x509"):
		return "TLS failure"
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return msg
}

// probeOutageCause reports whether a probe error fails the way an outage does:
// in transport, by timeout, or with a gateway answer, and names the cause. Any
// other answer — a 401, a refusal — means the server is there.
func probeOutageCause(err error) (string, bool) {
	var se *api.StatusError
	switch {
	case isNetworkFailure(err):
		return outageCause(err, 0), true
	case errors.As(err, &se) && gatewayAnswer(se.Code, se.Header, []byte(se.Body)):
		return outageCause(nil, se.Code), true
	}
	return "", false
}

// isNetworkFailure reports a request that failed on the way: any failure the
// HTTP client reports (a *url.Error), or a timeout. A URL that does not parse
// or names no usable scheme is a configuration error, never an outage
// (REQ-CROSS-535 AC5, CODE-OUTAGE-3-3), and an error after an answer — a
// body that does not decode — is not a network failure (CODE-OUTAGE-4-3).
func isNetworkFailure(err error) bool {
	var ue *url.Error
	var ne net.Error
	switch {
	case err == nil:
		return false
	case errors.As(err, &ue):
		return ue.Op != "parse" && !strings.Contains(ue.Err.Error(), "unsupported protocol scheme")
	case errors.As(err, &ne) && ne.Timeout():
		return true
	}
	return false
}

// probeConfirmsOutage runs the reachability probe once per env and reports
// whether it fails the way an outage does.
func (e *factoryEnv) probeConfirmsOutage() bool {
	if e.probeDone {
		return e.probeDown
	}
	e.probeDone = true
	if _, err := listSystemsFn(e.APIURL, e.token); err != nil {
		_, e.probeDown = probeOutageCause(err)
	}
	return e.probeDown
}

// confirmOutage turns a candidate into serverUnavailableError when the probe
// fails too; nil means the server answered the probe and the caller returns
// what it would have returned before.
func (e *factoryEnv) confirmOutage(err error, status int) error {
	if !e.probeConfirmsOutage() {
		return nil
	}
	cause := outageCause(err, status)
	return serverUnavailableError{since: recordUnavailable(e.Root, cause, time.Now()), cause: cause}
}

// workspaceRoot is the workspace found from the current directory, or "" when
// there is none. A filesystem walk only: it never touches the network.
func workspaceRoot() string {
	if cfgDir, err := config.FindConfigDir(); err == nil && cfgDir != "" {
		return filepath.Dir(cfgDir)
	}
	return ""
}

// isUnavailable reports whether err is a known or confirmed outage.
func isUnavailable(err error) bool {
	var outage serverUnavailableError
	return errors.As(err, &outage)
}

// knownUnavailable returns the recorded outage while its window is open: until
// next_check, or 15 s after the last check for a state written without one.
func knownUnavailable(root string, now time.Time) (serverUnavailableError, bool) {
	if root == "" {
		return serverUnavailableError{}, false
	}
	st, ok := loadAvailability(root)
	if !ok || st.State != availabilityDown {
		return serverUnavailableError{}, false
	}
	next := st.NextCheck
	if next.IsZero() {
		next = st.LastChecked.Add(outageFirstWindow)
	}
	if !now.Before(next) {
		return serverUnavailableError{}, false
	}
	return serverUnavailableError{since: st.Since, cause: firstNonEmpty(st.Reason, "recorded outage")}, true
}

// outageCredentialLoad is credentialLoad for the store verbs: while an outage
// is known it skips the refresh and returns the env with the outage, so
// nothing is sent. Any other error comes with a nil env.
//
// An expired token that could not be refreshed does not end outage mode
// while an outage is on record: inside the window the env is returned with
// the outage, after it the probe asks the server, and only an answer means
// a new sign-in is needed (REQ-CROSS-536 AC5, CODE-OUTAGE-3-7).
func outageCredentialLoad() (*factoryEnv, error) {
	root := workspaceRoot()
	known, down := knownUnavailable(root, time.Now())
	env, err := credentialLoad(!down)
	var cred credentialError
	if err != nil && errors.As(err, &cred) && cred.expired && outageOnRecord(root) {
		stale, lerr := credentialLoadExpired()
		if lerr != nil {
			return nil, err
		}
		if down {
			stale.probeDone, stale.probeDown = true, true
			return stale, known
		}
		if _, perr := stale.reachabilityProbe(); isUnavailable(perr) {
			return stale, perr
		}
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	if down {
		env.probeDone, env.probeDown = true, true
		return env, known
	}
	return env, nil
}

// outageOnRecord reports an outage in the availability state, whether or not
// its window is still open.
func outageOnRecord(root string) bool {
	if root == "" {
		return false
	}
	st, ok := loadAvailability(root)
	return ok && st.State == availabilityDown
}

// reachabilityProbe runs the bounded probe. A failure that looks like an
// outage confirms it: recorded and returned as serverUnavailableError. Any
// other error is the probe's own, for the caller to fail open on.
func (e *factoryEnv) reachabilityProbe() ([]api.System, error) {
	systems, err := listSystemsFn(e.APIURL, e.token)
	if err == nil {
		return systems, nil
	}
	if cause, down := probeOutageCause(err); down {
		e.probeDone, e.probeDown = true, true
		return nil, serverUnavailableError{since: recordUnavailable(e.Root, cause, time.Now()), cause: cause}
	}
	return nil, err
}

const outageJournalDir = "outage-journal"

// keptWriteCount is the number of process writes kept on this machine: the
// entry files at the top of the outage journal.
func keptWriteCount(root string) int {
	entries, err := os.ReadDir(filepath.Join(root, config.ConfigDir, outageJournalDir))
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".json") {
			n++
		}
	}
	return n
}

// outageHookLine is the context hook's one line while an outage is fresh or
// kept writes wait; "" when neither holds. An outage whose window has closed
// names no outage: the hook asks the server again.
func outageHookLine(root string) string {
	if root == "" {
		return ""
	}
	kept := keptWriteCount(root)
	writes := fmt.Sprintf("%d process writes", kept)
	if kept == 1 {
		writes = "1 process write"
	}
	if known, down := knownUnavailable(root, time.Now()); down {
		return fmt.Sprintf("ModernPath server unavailable since %s; %s kept on this machine. Run `modernpath outage replay` once it is back.",
			known.since.Local().Format("15:04"), writes)
	}
	if kept > 0 {
		return fmt.Sprintf("%s kept on this machine wait to be recorded. Run `modernpath outage replay` to record them.", writes)
	}
	return ""
}
