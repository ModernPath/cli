package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fatih/color"
	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/platform"
	"github.com/modernpath/cli/internal/zitadel"
)

// REQ-CROSS-405: the interactive picker labelled each system
// "<name> - <description>" and nothing else, so two systems that share a name
// — the common shape when a workspace is re-created — were two identical rows
// and the choice was a coin flip. The non-tty path has always printed the id
// (chooseSystemWithoutTerminal); the picker must carry it too.
func TestSystemPickerLabelsCarryTheSystemId(t *testing.T) {
	items := systemPickerItems([]api.System{
		{ID: 41, Name: "ModernPath", Description: "the platform"},
		{ID: 77, Name: "ModernPath", SystemType: "service"},
	})
	if len(items) != 3 || items[0] != "+ New project" {
		t.Fatalf("the picker still offers a new project first, got %v", items)
	}
	if !strings.Contains(items[1], "41") || !strings.Contains(items[2], "77") {
		t.Fatalf("every label must carry its system id, got %v", items[1:])
	}
	if items[1] == items[2] {
		t.Fatalf("two identically named systems must not render the same label: %q", items[1])
	}
	for _, want := range []string{"ModernPath", "the platform"} {
		if !strings.Contains(items[1], want) {
			t.Errorf("the label must keep %q, got %q", want, items[1])
		}
	}
	// A system with no description still falls back to its type, as before.
	if !strings.Contains(items[2], "service") {
		t.Errorf("a description-less system keeps its type, got %q", items[2])
	}
}

// REQ-CROSS-405 — `init` reaches a signed-in, bound workspace or names the
// step; the api-client verbs carry the credential statements the factory
// verbs already have. A fresh checkout used to reach the listing with no
// credential and print the server's 401.

const initGoodToken = "good-token"

// initFakeServer answers the health check, lists systems for the good bearer
// and serves an export job whose download is a one-file zip. It records every
// request path so a test can assert what was — and was not — sent.
type initFakeServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string
	systems  []map[string]any
	// listStatus, when non-zero, is answered on the listing regardless of
	// the bearer (S4: a refused listing).
	listStatus int
}

func newInitFakeServer(t *testing.T, systems ...map[string]any) *initFakeServer {
	t.Helper()
	if len(systems) == 0 {
		systems = []map[string]any{{"id": 1, "name": "Demo System", "slug": "demo-system", "description": "one"}}
	}
	f := &initFakeServer{systems: systems}
	f.Server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.Close)
	return f
}

// newInitPlatformFakeServer is newInitFakeServer registered as a shared
// platform API host, so platform.Prepare rewrites core's `/api/...` paths onto
// the `/api/ex` prefix the Gateway routes and the fail-closed `/api/ex/_health`
// case applies. It reproduces the production topology (api.modernpath.ai) that
// an httptest server otherwise never matches, since platformOrigins is a fixed
// baked-in set. ProjectID is left empty so an opaque bearer is attached
// unchanged (no JWT audience pre-check); the edge authorizes any bearer it is
// sent.
func newInitPlatformFakeServer(t *testing.T, systems ...map[string]any) *initFakeServer {
	t.Helper()
	f := newInitFakeServer(t, systems...)
	u, err := url.Parse(f.URL)
	if err != nil {
		t.Fatalf("parse fake server URL: %v", err)
	}
	restore := platform.SetPlatformOriginsForTest(map[string]zitadel.Profile{
		u.Scheme + "://" + u.Host: {APIURL: f.URL},
	})
	t.Cleanup(restore)
	return f
}

func (f *initFakeServer) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	f.mu.Unlock()

	authorized := func() bool {
		if r.Header.Get("Authorization") == "Bearer "+initGoodToken {
			return true
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
		return false
	}
	reply := func(code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}

	p := r.URL.Path
	switch {
	case p == "/_health":
		// A non-platform host (a dev server, an operator --api-url) answers
		// health unauthenticated; platform.Prepare leaves this path alone.
		reply(http.StatusOK, map[string]string{"status": "ok"})
	case p == "/api/ex/_health":
		// The platform edge fails closed: it authenticates every route it
		// fronts, core's health check included, so an unauthenticated probe is
		// answered 401 no matter how healthy the server is (REQ-CROSS-290/405).
		// A reachability preflight that omits the stored bearer dies here.
		if authorized() {
			reply(http.StatusOK, map[string]string{"status": "ok"})
		}
	case (p == "/api/systems" || p == "/api/ex/systems") && r.Method == http.MethodGet:
		if f.listStatus != 0 {
			reply(f.listStatus, map[string]string{"error": http.StatusText(f.listStatus)})
			return
		}
		if authorized() {
			reply(http.StatusOK, f.systems)
		}
	case strings.HasSuffix(p, "/export/jobs") && r.Method == http.MethodPost:
		if authorized() {
			base := p + "/job1"
			reply(http.StatusAccepted, map[string]string{"id": "job1", "status": "queued", "poll_path": base, "download_path": base + "/file"})
		}
	case strings.HasSuffix(p, "/export/jobs/job1"):
		if authorized() {
			reply(http.StatusOK, map[string]string{"id": "job1", "status": "ready", "download_path": p + "/file"})
		}
	case strings.HasSuffix(p, "/export/jobs/job1/file"):
		if authorized() {
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			entry, _ := zw.Create(".modernpath/modernpath/README.md")
			_, _ = entry.Write([]byte("# demo\n"))
			_ = zw.Close()
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(buf.Bytes())
		}
	case p == "/api/search" || p == "/api/docs/read" || p == "/api/files/read":
		if authorized() {
			reply(http.StatusOK, map[string]any{"data": map[string]any{}})
		}
	default:
		reply(http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func (f *initFakeServer) saw(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.requests {
		if strings.HasPrefix(r, prefix) {
			return true
		}
	}
	return false
}

func (f *initFakeServer) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// initTestSetup enters a fresh temp workspace, points the global --api-url at
// the fake server, forces non-interactive stdin and resets init's flags.
func initTestSetup(t *testing.T, srv *initFakeServer) string {
	t.Helper()
	root := chdirTemp(t)
	savedURL, savedID, savedName, savedForce, savedTTY := apiURL, systemIDFlag, systemNameFlag, force, stdinIsTerminal
	apiURL, systemIDFlag, systemNameFlag, force = srv.URL, 0, "", false
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() {
		apiURL, systemIDFlag, systemNameFlag, force, stdinIsTerminal = savedURL, savedID, savedName, savedForce, savedTTY
	})
	return root
}

func writeInitAuth(t *testing.T, root, token string, expiry time.Time) {
	t.Helper()
	auth := map[string]string{"token": token}
	if !expiry.IsZero() {
		auth["expires_at"] = expiry.UTC().Format(time.RFC3339)
	}
	data, _ := json.Marshal(auth)
	writeFactoryTestFile(t, root, ".modernpath/.gitignore", "auth.json\nconfig.json\n")
	writeFactoryTestFile(t, root, ".modernpath/auth.json", string(data))
}

// runCapturing runs fn with stdout and stderr (and the color writers the
// print helpers use) captured into one string.
func runCapturing(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, _ := os.Pipe()
	savedOut, savedErr, savedCOut, savedCErr := os.Stdout, os.Stderr, color.Output, color.Error
	os.Stdout, os.Stderr, color.Output, color.Error = w, w, w, w
	err := fn()
	_ = w.Close()
	os.Stdout, os.Stderr, color.Output, color.Error = savedOut, savedErr, savedCOut, savedCErr
	out, _ := io.ReadAll(r)
	return string(out), err
}

func boundSystemID(t *testing.T, root string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".modernpath", "config.json"))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		SystemID int `json:"system_id"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg.SystemID
}

// S1 — no credential: init names the auth command for this server, lists
// nothing and writes no binding.
func TestInitWithoutCredentialNamesAuthAndListsNothing(t *testing.T) {
	srv := newInitFakeServer(t)
	root := initTestSetup(t, srv)
	systemIDFlag = 1

	out, err := runCapturing(t, func() error { return initCmd.RunE(initCmd, nil) })
	if err == nil {
		t.Fatalf("init with no credential must exit non-zero\n%s", out)
	}
	repair := authRepairCommand(srv.URL)
	if !strings.Contains(err.Error(), repair) {
		t.Fatalf("init must name %q as the next step, got: %v\n%s", repair, err, out)
	}
	if srv.saw("GET /api/systems") {
		t.Fatalf("init listed systems with no credential: %v", srv.requests)
	}
	if got := boundSystemID(t, root); got != 0 {
		t.Fatalf("init wrote a binding (system %d) with no credential", got)
	}
}

// S1, expired arm — a stored token whose expiry has passed counts as missing
// and the refusal names the expiry.
func TestInitWithExpiredCredentialNamesExpiryAndListsNothing(t *testing.T) {
	srv := newInitFakeServer(t)
	root := initTestSetup(t, srv)
	systemIDFlag = 1
	expiry := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	writeInitAuth(t, root, "stale-token", expiry)

	out, err := runCapturing(t, func() error { return initCmd.RunE(initCmd, nil) })
	if err == nil {
		t.Fatalf("init with an expired credential must exit non-zero\n%s", out)
	}
	for _, want := range []string{expiry.Format(time.RFC3339), authRepairCommand(srv.URL)} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal must carry %q, got: %v", want, err)
		}
	}
	if srv.saw("GET /api/systems") {
		t.Fatalf("init listed systems with an expired credential: %v", srv.requests)
	}
	if got := boundSystemID(t, root); got != 0 {
		t.Fatalf("init wrote a binding (system %d) with an expired credential", got)
	}
}

// S2 — signed in with one system: bound, and factory status prints it.
func TestInitSignedInWithOneSystemBindsIt(t *testing.T) {
	srv := newInitFakeServer(t)
	root := initTestSetup(t, srv)
	writeInitAuth(t, root, initGoodToken, time.Now().Add(time.Hour))

	out, err := runCapturing(t, func() error { return initCmd.RunE(initCmd, nil) })
	if err != nil {
		t.Fatalf("signed-in init with one system: %v\n%s", err, out)
	}
	if got := boundSystemID(t, root); got != 1 {
		t.Fatalf("bound system = %d, want 1\n%s", got, out)
	}
	status, err := runCapturing(t, func() error { return factoryStatusCmd.RunE(factoryStatusCmd, nil) })
	if err != nil {
		t.Fatalf("factory status after init: %v\n%s", err, status)
	}
	if !strings.Contains(status, "System:  Demo System (ID: 1)") {
		t.Fatalf("factory status must print the bound system, got:\n%s", status)
	}
}

// S3 — several systems: --system-id binds that one; with no flag and no
// terminal, init lists them and names --system-id instead of prompting.
func TestInitWithSeveralSystemsBindsTheFlaggedOneOrNamesTheFlag(t *testing.T) {
	two := []map[string]any{
		{"id": 1, "name": "Alpha", "slug": "alpha"},
		{"id": 2, "name": "Beta", "slug": "beta"},
	}
	srv := newInitFakeServer(t, two...)
	root := initTestSetup(t, srv)
	writeInitAuth(t, root, initGoodToken, time.Now().Add(time.Hour))

	out, err := runCapturing(t, func() error { return initCmd.RunE(initCmd, nil) })
	if err == nil {
		t.Fatalf("non-interactive init with several systems and no flag must stop\n%s", out)
	}
	if !strings.Contains(err.Error(), "--system-id") {
		t.Fatalf("the refusal must name --system-id, got: %v", err)
	}
	for _, name := range []string{"Alpha", "Beta"} {
		if !strings.Contains(out, name) {
			t.Fatalf("init must list the systems to choose from; %q missing in:\n%s", name, out)
		}
	}
	if got := boundSystemID(t, root); got != 0 {
		t.Fatalf("init bound system %d without a choice", got)
	}

	systemIDFlag = 2
	out, err = runCapturing(t, func() error { return initCmd.RunE(initCmd, nil) })
	if err != nil {
		t.Fatalf("init --system-id 2: %v\n%s", err, out)
	}
	if got := boundSystemID(t, root); got != 2 {
		t.Fatalf("bound system = %d, want 2", got)
	}
}

// S4 — the listing refused: the step and the next verb are named and no
// partial binding is left behind.
func TestInitListingRefusedNamesStepAndLeavesNoBinding(t *testing.T) {
	t.Run("401 with a stored bearer", func(t *testing.T) {
		srv := newInitFakeServer(t)
		srv.listStatus = http.StatusUnauthorized
		root := initTestSetup(t, srv)
		systemIDFlag = 1
		writeInitAuth(t, root, initGoodToken, time.Now().Add(time.Hour))

		out, err := runCapturing(t, func() error { return initCmd.RunE(initCmd, nil) })
		if err == nil {
			t.Fatalf("a refused listing must fail init\n%s", out)
		}
		var cred credentialError
		if !errors.As(err, &cred) {
			t.Fatalf("a 401 on the listing must read as a credential statement, got: %v", err)
		}
		if !strings.Contains(err.Error(), authRepairCommand(srv.URL)) {
			t.Fatalf("the statement must name the repair command, got: %v", err)
		}
		if got := boundSystemID(t, root); got != 0 {
			t.Fatalf("init left a binding (system %d) after a refused listing", got)
		}
	})
	t.Run("503", func(t *testing.T) {
		srv := newInitFakeServer(t)
		srv.listStatus = http.StatusServiceUnavailable
		root := initTestSetup(t, srv)
		systemIDFlag = 1
		writeInitAuth(t, root, initGoodToken, time.Now().Add(time.Hour))

		out, err := runCapturing(t, func() error { return initCmd.RunE(initCmd, nil) })
		if err == nil {
			t.Fatalf("a refused listing must fail init\n%s", out)
		}
		for _, want := range []string{"list", "modernpath init"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("the refusal must name the step and the verb that continues (%q), got: %v", want, err)
			}
		}
		if got := boundSystemID(t, root); got != 0 {
			t.Fatalf("init left a binding (system %d) after a refused listing", got)
		}
	})
}

// S6 — a bound checkout without --force keeps the early return and sends
// nothing.
func TestInitOnABoundCheckoutReturnsEarly(t *testing.T) {
	srv := newInitFakeServer(t)
	root := initTestSetup(t, srv)
	writeFactoryTestFile(t, root, ".modernpath/config.json", fmt.Sprintf(`{"api_url":%q,"system_id":1,"system_name":"Demo System"}`, srv.URL))

	out, err := runCapturing(t, func() error { return initCmd.RunE(initCmd, nil) })
	if err != nil {
		t.Fatalf("init on a bound checkout: %v", err)
	}
	if !strings.Contains(out, "Already initialized") {
		t.Fatalf("the early return must stand, got:\n%s", out)
	}
	if n := srv.requestCount(); n != 0 {
		t.Fatalf("init on a bound checkout sent %d request(s), want none", n)
	}
}

// S5 — the api-client verbs: with no credential they name the auth command
// before any request; with a rejected token they print the credential
// statement and the repair command, never `HTTP 401` alone.
func TestAPIClientVerbsCarryTheCredentialStatements(t *testing.T) {
	verbs := []struct {
		name string
		run  func() error
	}{
		{"docs sync", func() error { return docsSyncCmd.RunE(docsSyncCmd, nil) }},
		{"search", func() error { return searchCmd.RunE(searchCmd, []string{"gateway"}) }},
		{"read-doc", func() error { return readDocCmd.RunE(readDocCmd, nil) }},
	}
	for _, v := range verbs {
		t.Run(v.name+" without a credential", func(t *testing.T) {
			srv := newInitFakeServer(t)
			root := initTestSetup(t, srv)
			writeFactoryTestFile(t, root, ".modernpath/config.json", fmt.Sprintf(`{"api_url":%q,"system_id":1,"system_name":"Demo System"}`, srv.URL))

			out, err := runCapturing(t, v.run)
			if err == nil {
				t.Fatalf("%s with no credential must exit non-zero\n%s", v.name, out)
			}
			if !strings.Contains(err.Error(), authRepairCommand(srv.URL)) {
				t.Fatalf("%s must name the auth command, got: %v\n%s", v.name, err, out)
			}
			if n := srv.requestCount(); n != 0 {
				t.Fatalf("%s sent %d request(s) with no credential, want none: %v", v.name, n, srv.requests)
			}
		})
		t.Run(v.name+" with a rejected token", func(t *testing.T) {
			srv := newInitFakeServer(t)
			root := initTestSetup(t, srv)
			writeFactoryTestFile(t, root, ".modernpath/config.json", fmt.Sprintf(`{"api_url":%q,"system_id":1,"system_name":"Demo System"}`, srv.URL))
			writeInitAuth(t, root, "revoked-token", time.Now().Add(time.Hour))

			out, err := runCapturing(t, v.run)
			if err == nil {
				t.Fatalf("%s on a rejected token must exit non-zero\n%s", v.name, out)
			}
			combined := out + "\n" + err.Error()
			if !strings.Contains(combined, "rejected the session token") || !strings.Contains(combined, authRepairCommand(srv.URL)) {
				t.Fatalf("%s must render the 401 as a credential statement with the repair command, got:\n%s", v.name, combined)
			}
			if strings.Contains(out, "HTTP 401") {
				t.Fatalf("%s printed the bare status line:\n%s", v.name, out)
			}
		})
	}
}

// The factory verbs already refuse before any request; this stays as the
// regression guard the enrichment names.
func TestFactoryVerbsStillRefuseBeforeAnyRequestWithoutACredential(t *testing.T) {
	srv := newInitFakeServer(t)
	root := initTestSetup(t, srv)
	writeFactoryTestFile(t, root, ".modernpath/config.json", fmt.Sprintf(`{"api_url":%q,"system_id":1}`, srv.URL))

	_, err := factoryEnvLoad()
	var cred credentialError
	if !errors.As(err, &cred) {
		t.Fatalf("factoryEnvLoad without a credential must return a credentialError, got: %v", err)
	}
	if n := srv.requestCount(); n != 0 {
		t.Fatalf("factory pre-check sent %d request(s) with no credential", n)
	}
}

// S1, terminal arm — with a terminal and no credential, init runs the
// sign-in for this server and lists with the credential it stored.
func TestInitWithATerminalRunsTheSignInBeforeListing(t *testing.T) {
	srv := newInitFakeServer(t)
	root := initTestSetup(t, srv)
	systemIDFlag = 1
	stdinIsTerminal = func() bool { return true }

	savedSignIn := signInForServer
	t.Cleanup(func() { signInForServer = savedSignIn })
	var signedInTo string
	signInForServer = func(baseURL string) error {
		signedInTo = baseURL
		if srv.saw("GET /api/systems") {
			t.Fatal("the listing ran before the sign-in")
		}
		writeInitAuth(t, root, initGoodToken, time.Now().Add(time.Hour))
		return nil
	}

	out, err := runCapturing(t, func() error { return initCmd.RunE(initCmd, nil) })
	if err != nil {
		t.Fatalf("init with a terminal and no credential: %v\n%s", err, out)
	}
	if signedInTo != srv.URL {
		t.Fatalf("sign-in ran for %q, want %q", signedInTo, srv.URL)
	}
	if got := boundSystemID(t, root); got != 1 {
		t.Fatalf("bound system = %d, want 1\n%s", got, out)
	}
}

// Review follow-through (PR #487, finding 8): with no binding at all, the
// api-client verbs name `modernpath init` — the on-ramp — not a system id a
// fresh checkout does not have; nothing is sent.
func TestAPIClientVerbsNameInitWhenUnbound(t *testing.T) {
	verbs := []struct {
		name string
		run  func() error
	}{
		{"docs sync", func() error { return docsSyncCmd.RunE(docsSyncCmd, nil) }},
		{"search", func() error { return searchCmd.RunE(searchCmd, []string{"gateway"}) }},
		{"read-doc", func() error { return readDocCmd.RunE(readDocCmd, nil) }},
	}
	for _, v := range verbs {
		t.Run(v.name, func(t *testing.T) {
			srv := newInitFakeServer(t)
			initTestSetup(t, srv) // a fresh temp dir: no .modernpath at all
			out, err := runCapturing(t, v.run)
			if err == nil || !strings.Contains(err.Error(), "modernpath init") {
				t.Fatalf("%s unbound must name modernpath init, got: %v\n%s", v.name, err, out)
			}
			if n := srv.requestCount(); n != 0 {
				t.Fatalf("%s sent %d request(s) unbound", v.name, n)
			}
		})
	}
}

// S7 (REQ-CROSS-405, production report 2026-09-15) — on a fail-closed platform
// host, a signed-in `init` reaches the bound workspace: its reachability
// preflight carries the stored bearer instead of dying with a bare HTTP 401.
// The edge authenticates core's health route, so a tokenless probe is answered
// 401 no matter how healthy the server is. `factory connect`, whose probe sends
// the bearer, bound fine — `init` did not, because Client.HealthCheck omits it.
func TestInitOnFailClosedPlatformHostBindsWithStoredCredential(t *testing.T) {
	srv := newInitPlatformFakeServer(t)
	root := initTestSetup(t, srv)
	systemIDFlag = 1
	writeInitAuth(t, root, initGoodToken, time.Now().Add(time.Hour))

	out, err := runCapturing(t, func() error { return initCmd.RunE(initCmd, nil) })
	if err != nil {
		t.Fatalf("signed-in init on a fail-closed platform host must bind, got: %v\n%s", err, out)
	}
	if strings.Contains(out, "HTTP 401") || strings.Contains(out, "health check failed") {
		t.Fatalf("init surfaced a bare health-check 401 on a platform host:\n%s", out)
	}
	if got := boundSystemID(t, root); got != 1 {
		t.Fatalf("bound system = %d, want 1\n%s", got, out)
	}
	// The preflight must have carried the bearer: the fail-closed edge only
	// answers /api/ex/_health for a probe that sent one.
	if !srv.saw("GET /api/ex/_health") {
		t.Fatalf("init never probed the platform health path: %v", srv.requests)
	}
}

// S8 (REQ-CROSS-405) — on a fail-closed platform host with NO stored credential,
// init signs in BEFORE the reachability probe. The probe would be answered 401
// if it ran first (no bearer yet), so this guards the credential-before-probe
// order that TestInitOnFailClosedPlatformHostBindsWithStoredCredential cannot:
// NewClient back-fills a stored token regardless of order, so only the sign-in
// path exercises the ordering.
func TestInitOnFailClosedPlatformHostSignsInBeforeProbing(t *testing.T) {
	srv := newInitPlatformFakeServer(t)
	root := initTestSetup(t, srv)
	systemIDFlag = 1
	stdinIsTerminal = func() bool { return true }

	savedSignIn := signInForServer
	t.Cleanup(func() { signInForServer = savedSignIn })
	signInForServer = func(baseURL string) error {
		if srv.saw("GET /api/ex/_health") {
			t.Fatal("the reachability probe ran before sign-in")
		}
		writeInitAuth(t, root, initGoodToken, time.Now().Add(time.Hour))
		return nil
	}

	out, err := runCapturing(t, func() error { return initCmd.RunE(initCmd, nil) })
	if err != nil {
		t.Fatalf("init with a terminal and no credential on a platform host: %v\n%s", err, out)
	}
	if strings.Contains(out, "HTTP 401") {
		t.Fatalf("init surfaced a bare 401:\n%s", out)
	}
	if got := boundSystemID(t, root); got != 1 {
		t.Fatalf("bound system = %d, want 1\n%s", got, out)
	}
}

// S9 (REQ-CROSS-405) — a credential the platform edge rejects at the health
// preflight renders as the credential statement with the repair command, never
// a bare HTTP 401 (the acceptance is absolute, and the preflight is now the
// first server contact). The token is locally fresh, so initCredential passes
// it through; only the server rejects it.
func TestInitPlatformHealthRejectedCredentialRendersStatement(t *testing.T) {
	srv := newInitPlatformFakeServer(t)
	root := initTestSetup(t, srv)
	systemIDFlag = 1
	writeInitAuth(t, root, "revoked-token", time.Now().Add(time.Hour))

	out, err := runCapturing(t, func() error { return initCmd.RunE(initCmd, nil) })
	if err == nil {
		t.Fatalf("a rejected credential must fail init\n%s", out)
	}
	var cred credentialError
	if !errors.As(err, &cred) {
		t.Fatalf("a 401 at the health preflight must read as a credential statement, got: %v", err)
	}
	if !strings.Contains(err.Error(), authRepairCommand(srv.URL)) {
		t.Fatalf("the statement must name the repair command, got: %v", err)
	}
	if strings.Contains(out, "HTTP 401") {
		t.Fatalf("init printed the bare status line:\n%s", out)
	}
	if got := boundSystemID(t, root); got != 0 {
		t.Fatalf("init left a binding (system %d) after a rejected credential", got)
	}
}
