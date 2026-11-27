package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/fatih/color"
	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/kit"
)

// Epic 422 test setup: one update-check fixture per test. Every test gets its
// own user cache folder, clock, terminal answers, environment and counting
// release server, so no result depends on another test or on the machine.

const (
	ucRunning = "0.26.0" // the released CLI under test
	ucLatest  = "0.27.1" // the newer CLI release
)

// ucStart is the fixture clock's starting time.
var ucStart = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

func ucSwap[T any](t *testing.T, target *T, value T) {
	t.Helper()
	saved := *target
	*target = value
	t.Cleanup(func() { *target = saved })
}

// ucSetVersion stamps the running CLI, including the copy cobra's --version
// prints, and restores both afterwards.
func ucSetVersion(t *testing.T, version string) {
	t.Helper()
	savedVersion, savedRoot := Version, rootCmd.Version
	Version, rootCmd.Version = version, version
	t.Cleanup(func() { Version, rootCmd.Version = savedVersion, savedRoot })
}

// ucOwnCacheFolder gives a test its own user cache folder.
func ucOwnCacheFolder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ucSwap(t, &userCacheFolder, func() (string, error) { return dir, nil })
	return dir
}

// ucWait says when the fake API of the command under test answers. Every
// hold also ends when the client goes away, so a run that never gets what a
// hold waits for fails on its assertions instead of hanging.
type ucWait int

const (
	ucNoWait            ucWait = iota // at once
	ucWaitReceived                    // once the release server has the background request
	ucWaitFinished                    // once the background lookup has finished
	ucWaitPrepareLookup               // the System list, once the release server has prepare's own request
	ucWaitHeld                        // once the channel given to holdAPIUntil closes
)

type ucFixture struct {
	t        *testing.T
	cache    string // the user cache folder
	releases *ucReleaseServer
	api      *httptest.Server

	mu             sync.Mutex
	now            time.Time
	env            map[string]string
	stdoutTerminal bool
	stderrTerminal bool
	asked          []int
	started        []<-chan struct{}
	apiWait        ucWait
	apiHold        <-chan struct{} // ucWaitHeld: the API answers once it closes
	listed         chan struct{}   // closes once the API has served the System list in this run
	waiting        chan struct{}   // closes once this run's end-of-run wait has started
	waits          int             // end-of-run waits after a cut-off, in every run so far
}

func newUCFixture(t *testing.T) *ucFixture {
	t.Helper()
	f := &ucFixture{t: t, now: ucStart, env: map[string]string{}, listed: make(chan struct{}), waiting: make(chan struct{})}
	f.cache = ucOwnCacheFolder(t)
	ucSwap(t, &updateCheckClock, f.clock)
	ucSwap(t, &updateCheckEnv, f.getenv)
	ucSwap(t, &streamIsTerminal, f.isTerminal)
	ucSwap(t, &backgroundLookupStarted, f.lookupStarted)
	ucSwap(t, &backgroundLookupWaitStarted, f.waitStarted)
	ucSwap(t, &runningBinaryPath, func() (string, error) { return "/opt/elsewhere/bin/modernpath", nil })
	ucSwap(t, &resolvedHomeFolder, func() (string, error) { return "/home/dev", nil })
	ucSwap(t, &resolvedLocalAppData, func() (string, error) { return "", fmt.Errorf("LOCALAPPDATA is not set") })
	f.releases = newUCReleaseServer(t)
	ucSwap(t, &releaseLookupURL, f.releases.URL+"/repos/ModernPath/cli/releases/latest")
	ucSetVersion(t, ucRunning)
	// Registered last, so it runs first: no lookup outlives the seams it read.
	t.Cleanup(f.waitLookups)
	return f
}

func (f *ucFixture) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *ucFixture) time() time.Time { return f.clock() }

func (f *ucFixture) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

func (f *ucFixture) setTime(at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = at
}

func (f *ucFixture) getenv(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.env[key]
}

func (f *ucFixture) setEnv(key, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.env[key] = value
}

func (f *ucFixture) terminals(stdout, stderr bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stdoutTerminal, f.stderrTerminal = stdout, stderr
}

func (f *ucFixture) isTerminal(fd int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, fd)
	switch fd {
	case int(syscall.Stdout):
		return f.stdoutTerminal
	case int(syscall.Stderr):
		return f.stderrTerminal
	}
	return false
}

// askedDescriptors returns the descriptors the terminal check was asked about.
func (f *ucFixture) askedDescriptors() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.asked...)
}

func (f *ucFixture) lookupStarted(done <-chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, done)
}

// lookupsStarted counts the background lookups run() has started.
func (f *ucFixture) lookupsStarted() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.started)
}

// waitLookups waits until every background lookup started so far has ended.
func (f *ucFixture) waitLookups() {
	f.mu.Lock()
	started := append([]<-chan struct{}(nil), f.started...)
	f.mu.Unlock()
	for _, done := range started {
		<-done
	}
}

func (f *ucFixture) setAPIWait(mode ucWait) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.apiWait = mode
}

// holdAPIUntil makes the API answer only once hold closes.
func (f *ucFixture) holdAPIUntil(hold <-chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.apiWait, f.apiHold = ucWaitHeld, hold
}

// newRun gives the next run its own signals.
func (f *ucFixture) newRun() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listed, f.waiting = make(chan struct{}), make(chan struct{})
}

// waitStarted is the end-of-run wait hook: it counts the wait and lets a
// release server that answers only during the wait go on.
func (f *ucFixture) waitStarted() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.waits++
	ucClose(f.waiting)
}

// waitsStarted counts the end-of-run waits after a cut-off so far.
func (f *ucFixture) waitsStarted() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.waits
}

// waitSignal closes once the current run's end-of-run wait has started.
func (f *ucFixture) waitSignal() <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.waiting
}

// listedSignal closes once the API has served the System list in the current
// run.
func (f *ucFixture) listedSignal() <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listed
}

func (f *ucFixture) systemListServed() {
	f.mu.Lock()
	defer f.mu.Unlock()
	ucClose(f.listed)
}

// ucClose closes ch unless it is closed already; the caller holds the lock
// that guards it.
func ucClose(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}

// answerWhen holds the release answer until the channel signal returns, read
// when the request arrives, closes. It also returns when the client goes away
// or the test frees the server.
func (f *ucFixture) answerWhen(signal func() <-chan struct{}, then http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-signal():
			then(w, r)
		case <-r.Context().Done():
		case <-f.releases.freed:
		}
	}
}

func (f *ucFixture) binary(path string) {
	f.t.Helper()
	ucSwap(f.t, &runningBinaryPath, func() (string, error) { return path, nil })
}

// folder is the CLI's own folder under the user cache folder.
func (f *ucFixture) folder() string { return filepath.Join(f.cache, "modernpath") }

func (f *ucFixture) writeFile(name, content string) {
	f.t.Helper()
	if err := os.MkdirAll(f.folder(), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.folder(), name), []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// writeCache writes release-latest.json as the CLI writes it.
func (f *ucFixture) writeCache(tag string, fetchedAt time.Time) {
	f.t.Helper()
	f.writeFile("release-latest.json", fmt.Sprintf(`{"tag":%q,"fetched_at":%q}`+"\n", tag, fetchedAt.UTC().Format(time.RFC3339)))
}

func (f *ucFixture) readFile(name string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(f.folder(), name))
	return string(raw), err == nil
}

// cacheEntry reads release-latest.json.
func (f *ucFixture) cacheEntry() (tag string, fetchedAt time.Time, ok bool) {
	raw, found := f.readFile("release-latest.json")
	if !found {
		return "", time.Time{}, false
	}
	var entry struct {
		Tag       string `json:"tag"`
		FetchedAt string `json:"fetched_at"`
	}
	if json.Unmarshal([]byte(raw), &entry) != nil {
		return "", time.Time{}, false
	}
	at, err := time.Parse(time.RFC3339, entry.FetchedAt)
	if err != nil {
		return "", time.Time{}, false
	}
	return entry.Tag, at, true
}

// connectedRepo binds a temporary repository to System 17 on a fake API and
// makes it the working directory, so run() records history as it does in a
// connected repository. With withKit the process kit is installed, and with a
// reachable session and checked documents prepare then exits 0 unless the
// release state says otherwise.
func (f *ucFixture) connectedRepo(withKit bool) string {
	t := f.t
	t.Helper()
	f.api = httptest.NewServer(http.HandlerFunc(f.serveAPI))
	t.Cleanup(f.api.Close)
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Join(root, ".modernpath", "example"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".modernpath", "example", "docs_push_manifest.json"), []byte(`{"generated_at":"2026-10-19T12:00:00Z"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteConfig(&config.Config{APIURL: f.api.URL, SystemID: 17, SystemName: "Example", SystemSlug: "example"}); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteAuth(&config.Auth{Token: "test-token", Actor: "alice@example.test"}); err != nil {
		t.Fatal(err)
	}
	if withKit {
		if _, err := kit.Install(root, kit.Generated{Target: cliReferenceTarget, Body: []byte(renderCLIReference(rootCmd))}); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// serveAPI answers the reads status and prepare make. In the wait modes it
// holds the command until the background lookup run() started has reached
// the release server or has finished, until the release server has
// prepare's own request, or until the test's hold closes.
func (f *ucFixture) serveAPI(w http.ResponseWriter, r *http.Request) {
	if !f.holdAPI(r) {
		return // the client went away
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/_health":
		w.Header().Set("x-modernpath-store-revision", "abc1234")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	case "/api/systems":
		_, _ = io.WriteString(w, `[{"id":17,"name":"Example"}]`)
		f.systemListServed()
	case "/api/systems/17/documents":
		_, _ = io.WriteString(w, `{"data":{"documents":[{"id":2,"updated_at":"2026-10-18T12:00:00"}],"next_cursor":null}}`)
	default:
		http.NotFound(w, r)
	}
}

// holdAPI holds one API request as the wait mode says. It reports false when
// the client went away first.
func (f *ucFixture) holdAPI(r *http.Request) bool {
	f.mu.Lock()
	mode, hold := f.apiWait, f.apiHold
	started := append([]<-chan struct{}(nil), f.started...)
	f.mu.Unlock()
	gone := r.Context().Done()
	switch {
	case mode == ucWaitFinished:
		for _, done := range started {
			select {
			case <-done:
			case <-gone:
				return false
			}
		}
	case mode == ucWaitReceived && len(started) > 0:
		select {
		case <-f.releases.receivedSignal():
		case <-started[len(started)-1]:
		case <-gone:
			return false
		}
	case mode == ucWaitPrepareLookup && r.URL.Path == "/api/systems":
		// prepare's own hold: no background lookup takes part.
		select {
		case <-f.releases.receivedSignal():
		case <-gone:
			return false
		}
	case mode == ucWaitHeld:
		select {
		case <-hold:
		case <-gone:
			return false
		}
	}
	return true
}

type ucRun struct {
	code           int
	stdout, stderr string
}

// run drives run(args), the function main calls, with stdout and stderr in
// files, and waits for any background lookup it started before it returns.
func (f *ucFixture) run(args ...string) ucRun {
	t := f.t
	t.Helper()
	f.releases.newRun()
	f.newRun()
	dir := t.TempDir()
	open := func(name string) *os.File {
		file, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return file
	}
	stdin, stdout, stderr := open("stdin"), open("stdout"), open("stderr")
	savedIn, savedOut, savedErr := os.Stdin, os.Stdout, os.Stderr
	savedColorOut, savedColorErr, savedNoColor := color.Output, color.Error, color.NoColor
	os.Stdin, os.Stdout, os.Stderr = stdin, stdout, stderr
	color.Output, color.Error, color.NoColor = stdout, stderr, true
	resetTreeFlags(rootCmd)
	code := run(args)
	resetTreeFlags(rootCmd)
	rootCmd.SetArgs(nil)
	os.Stdin, os.Stdout, os.Stderr = savedIn, savedOut, savedErr
	color.Output, color.Error, color.NoColor = savedColorOut, savedColorErr, savedNoColor
	f.waitLookups()
	for _, file := range []*os.File{stdin, stdout, stderr} {
		_ = file.Close()
	}
	read := func(name string) string {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	return ucRun{code: code, stdout: read("stdout"), stderr: read("stderr")}
}

// prepare runs prepare's handler directly, as the unit tests of its report do.
func (f *ucFixture) prepare(jsonOutput bool) (string, error) {
	f.t.Helper()
	return ucPrepare(f.t, jsonOutput)
}

func ucPrepare(t *testing.T, jsonOutput bool) (string, error) {
	t.Helper()
	var out bytes.Buffer
	prepareCmd.SetOut(&out)
	defer prepareCmd.SetOut(nil)
	if err := prepareCmd.Flags().Set("json", fmt.Sprint(jsonOutput)); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = prepareCmd.Flags().Set("json", "false") }()
	err := runPrepare(prepareCmd, nil)
	return out.String(), err
}

// ucReleaseServer stands in for GitHub's release lookup and counts requests.
type ucReleaseServer struct {
	*httptest.Server
	mu       sync.Mutex
	count    int
	answer   http.HandlerFunc
	received chan struct{} // closed by the first request of the current run
	freed    chan struct{}
	freeOnce sync.Once
}

func newUCReleaseServer(t *testing.T) *ucReleaseServer {
	t.Helper()
	s := &ucReleaseServer{answer: ucTag("v" + ucLatest), received: make(chan struct{}), freed: make(chan struct{})}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.count++
		answer := s.answer
		select {
		case <-s.received:
		default:
			close(s.received)
		}
		s.mu.Unlock()
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "" {
			t.Errorf("the release lookup must be a public GET, got %s with Authorization %q", r.Method, r.Header.Get("Authorization"))
		}
		answer(w, r)
	}))
	t.Cleanup(s.Close)
	t.Cleanup(s.free) // runs before Close, so no held handler blocks it
	return s
}

func (s *ucReleaseServer) requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

func (s *ucReleaseServer) set(answer http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.answer = answer
}

func (s *ucReleaseServer) newRun() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.received = make(chan struct{})
}

func (s *ucReleaseServer) receivedSignal() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.received
}

// free lets held handlers go on.
func (s *ucReleaseServer) free() { s.freeOnce.Do(func() { close(s.freed) }) }

// late holds the answer until the test frees the server, then sends it. It
// sends nothing when the client goes away first.
func (s *ucReleaseServer) late(then http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-s.freed:
			then(w, r)
		case <-r.Context().Done():
		}
	}
}

// after holds the answer for d, then sends it, unless the client goes away or
// the test frees the server first.
func (s *ucReleaseServer) after(d time.Duration, then http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-timer.C:
			then(w, r)
		case <-r.Context().Done():
		case <-s.freed:
		}
	}
}

// never holds the request without answering until the client gives up or the
// test frees the server.
func (s *ucReleaseServer) never() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-s.freed:
		case <-r.Context().Done():
		}
	}
}

// ucBlockingTransport holds every request inside the client until its
// context ends, so no request goes out. entered closes when the first request
// reaches it.
type ucBlockingTransport struct {
	entered chan struct{}
	once    sync.Once
}

func (b *ucBlockingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	b.once.Do(func() { close(b.entered) })
	<-r.Context().Done()
	return nil, r.Context().Err()
}

// ucAttempt decodes release-attempt.json, and fails the test with why it
// should be there when it is missing.
func ucAttempt(t *testing.T, f *ucFixture, why string) map[string]any {
	t.Helper()
	raw, ok := f.readFile("release-attempt.json")
	if !ok {
		t.Fatalf("release-attempt.json is missing; %s", why)
	}
	var attempt map[string]any
	if err := json.Unmarshal([]byte(raw), &attempt); err != nil {
		t.Fatalf("release-attempt.json is not a JSON object: %v\n%s", err, raw)
	}
	return attempt
}

func ucTag(tag string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"tag_name":%q,"name":"modernpath %s"}`, tag, tag)
	}
}

func ucStatus(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		_, _ = io.WriteString(w, `{"message":"no"}`)
	}
}

func ucBody(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}
}

// ucCLILine is the CLI: line of prepare's text report.
func ucCLILine(t *testing.T, report string) string {
	t.Helper()
	var found []string
	for _, line := range strings.Split(report, "\n") {
		if strings.HasPrefix(line, "CLI: ") {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want one CLI: line, got %q in:\n%s", found, report)
	}
	return found[0]
}

// ucNextSteps are the Next: lines of prepare's text report, in order.
func ucNextSteps(report string) []string {
	var steps []string
	for _, line := range strings.Split(report, "\n") {
		if step, ok := strings.CutPrefix(line, "Next: "); ok {
			steps = append(steps, step)
		}
	}
	return steps
}

// ucReport decodes prepare's JSON report.
func ucReport(t *testing.T, raw string) map[string]any {
	t.Helper()
	var report map[string]any
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		t.Fatalf("prepare --json is not one JSON object: %v\n%s", err, raw)
	}
	return report
}

// ucRelease is the cli_release object of a JSON report, or nil.
func ucRelease(report map[string]any) map[string]any {
	release, _ := report["cli_release"].(map[string]any)
	return release
}

func ucSteps(report map[string]any) []string {
	raw, _ := report["next_steps"].([]any)
	steps := make([]string, 0, len(raw))
	for _, step := range raw {
		steps = append(steps, fmt.Sprint(step))
	}
	return steps
}

func ucIndex(list []string, value string) int {
	for i, item := range list {
		if item == value {
			return i
		}
	}
	return -1
}

// ucLines are the non-empty lines of a stream.
func ucLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// ucPlatformUpgrade is the first upgrade line prepare gives on this machine
// for an install it doesn't recognize.
func ucPlatformUpgrade(goos string) string {
	if goos == "windows" {
		return "scoop update modernpath"
	}
	return "brew update && brew upgrade modernpath"
}

const (
	ucBrew          = "brew update && brew upgrade modernpath"
	ucScoop         = "scoop update modernpath"
	ucInstallScript = "curl -fsSL https://raw.githubusercontent.com/ModernPath/cli/main/scripts/install.sh | bash"
	ucInstaller     = "irm https://raw.githubusercontent.com/ModernPath/cli/main/scripts/install.ps1 | iex"
	ucPointer       = "Upgrade it the way you installed it; modernpath prepare lists the commands."
)

// ucNotice is the update notice line, as printed with its ⚠.
func ucNotice(latest, version, upgrade string) string {
	if upgrade == ucPointer {
		return fmt.Sprintf("⚠ modernpath %s is available (you have %s). %s", latest, version, ucPointer)
	}
	return fmt.Sprintf("⚠ modernpath %s is available (you have %s). Upgrade: %s", latest, version, upgrade)
}
