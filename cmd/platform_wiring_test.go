package cmd

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// prepareWindow is how many lines after a request is constructed the guard
// will look for its platform.Prepare call. Every current site sets it on the
// next line or the one after the error check.
const prepareWindow = 8

// scannedDirs are the packages that build outbound requests against the
// configured API base URL.
var scannedDirs = []string{".", filepath.Join("..", "internal", "api")}

// A request built against the configured API base URL must go through
// platform.Prepare, or it addresses core's `/api/...` prefix directly on the
// shared platform host — where core lives under `/api/ex` — and 404s. This
// guard exists because the wiring is one line per call site and a new command
// is the easy place to forget it. It reports the site; it changes nothing.
func TestEveryOutboundRequestGoesThroughPlatformPrepare(t *testing.T) {
	var missing []string

	for _, dir := range scannedDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			for _, line := range unpreparedRequests(t, path) {
				missing = append(missing, line)
			}
		}
	}

	if len(missing) > 0 {
		t.Errorf("these requests never reach platform.Prepare, so they address core's /api prefix\n"+
			"directly on the shared platform host and 404 there:\n  %s",
			strings.Join(missing, "\n  "))
	}
}

// REQ-CROSS-291: the bearer is attached in one place, so the local
// project-audience pre-check cannot be bypassed by a new call site that sets
// the header itself. Fifteen sites did their own Header.Set before this
// guard existed — the count is exactly why the check could not live in
// internal/api alone.
//
// Like the Prepare guard above, this reports the site and changes nothing.
func TestNoDirectAuthorizationHeaderOutsidePlatform(t *testing.T) {
	var direct []string

	for _, dir := range scannedDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			direct = append(direct, directAuthorizationSets(t, path)...)
		}
	}

	if len(direct) > 0 {
		t.Errorf("these sites set the Authorization header directly instead of calling\n"+
			"platform.Authorize, so the project-audience pre-check never runs for them:\n  %s",
			strings.Join(direct, "\n  "))
	}
}

// directAuthorizationSets returns "file:line" for every Header.Set/Add of the
// Authorization header in path.
func directAuthorizationSets(t *testing.T, path string) []string {
	t.Helper()

	var out []string
	for i, line := range readLines(t, path) {
		if strings.Contains(line, `Header.Set("Authorization"`) || strings.Contains(line, `Header.Add("Authorization"`) {
			out = append(out, filepath.ToSlash(path)+":"+strconv.Itoa(i+1))
		}
	}
	return out
}

// unpreparedRequests returns "file:line" for every http.NewRequest in path
// with no platform.Prepare within prepareWindow lines after it.
func unpreparedRequests(t *testing.T, path string) []string {
	t.Helper()

	lines := readLines(t, path)

	var out []string
	for i, line := range lines {
		if !strings.Contains(line, "http.NewRequest(") {
			continue
		}
		found := false
		for j := i + 1; j < len(lines) && j <= i+prepareWindow; j++ {
			if strings.Contains(lines[j], "platform.Prepare(") {
				found = true
				break
			}
		}
		if !found {
			out = append(out, filepath.ToSlash(path)+":"+strconv.Itoa(i+1))
		}
	}
	return out
}

// The Prepare guard above matches http.NewRequest, and for a year that was the
// only way the CLI built a request. It is not: net/http's shorthand —
// client.Get(url), client.Post(...) on a *plain* *http.Client, or the
// package-level http.Get — sends a request nobody can call Prepare on, because
// no *http.Request ever surfaces. Three health probes were written that way and
// all three were wrong on a platform host (`env`, `env test`, `import`); none
// were visible to the guard that exists to catch exactly that.
//
// A wrapper type whose own Get calls Prepare — cmd/docs.go's
// authenticatedClient, used by 12 call sites — is correct and must not be
// flagged, so the guard resolves the receiver: only a variable this file
// assigned from &http.Client{} counts.
func TestNoRequestShorthandOnAPlainHTTPClient(t *testing.T) {
	var shorthand []string

	for _, dir := range scannedDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			shorthand = append(shorthand, unpreparableShorthand(t, path)...)
		}
	}

	if len(shorthand) > 0 {
		t.Errorf("these sites send a request through net/http shorthand on a plain *http.Client,\n"+
			"so no *http.Request exists to pass through platform.Prepare or platform.Authorize\n"+
			"— build the request and use client.Do instead:\n  %s",
			strings.Join(shorthand, "\n  "))
	}
}

// shorthandMethods are the *http.Client methods that construct and send in one
// call, leaving no request to prepare.
var shorthandMethods = []string{"Get(", "Post(", "PostForm(", "Head("}

// unpreparableShorthand returns "file:line" for every shorthand send in path
// whose receiver is a plain *http.Client, plus every package-level http.Get and
// friends (which are always plain, being http.DefaultClient).
func unpreparableShorthand(t *testing.T, path string) []string {
	t.Helper()

	lines := readLines(t, path)

	// Receivers assigned a plain &http.Client{} somewhere in this file.
	plain := map[string]bool{}
	for _, line := range lines {
		name, rest, ok := strings.Cut(strings.TrimSpace(line), " :=")
		if !ok || strings.Contains(name, " ") {
			continue
		}
		if strings.Contains(rest, "&http.Client{") {
			plain[name] = true
		}
	}

	var out []string
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		for _, method := range shorthandMethods {
			if strings.Contains(line, "http."+method) {
				out = append(out, filepath.ToSlash(path)+":"+strconv.Itoa(i+1))
				break
			}
			receiver, found := callReceiver(line, method)
			if !found || !plain[receiver] {
				continue
			}
			out = append(out, filepath.ToSlash(path)+":"+strconv.Itoa(i+1))
			break
		}
	}
	return out
}

// callReceiver returns the identifier a `<receiver>.<method>` call in line is
// made on, and whether the line contains such a call at all.
func callReceiver(line, method string) (string, bool) {
	idx := strings.Index(line, "."+method)
	if idx < 0 {
		return "", false
	}
	start := idx
	for start > 0 && (isIdentByte(line[start-1])) {
		start--
	}
	if start == idx {
		return "", false
	}
	return line[start:idx], true
}

func isIdentByte(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

// readLines reads path into a line slice, shared by both guards.
func readLines(t *testing.T, path string) []string {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return lines
}
