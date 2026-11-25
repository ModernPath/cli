// REQ-CROSS-500 (EPIC-CLI-029, USER:2026-09-29 D6): `import --local` and
// `source push` read the server's upload limit (REQ-SYS-236,
// GET /api/import/capabilities) and compare the request body they are about to
// send with it before the POST. A body that does not fit is refused locally
// with the zip size, the limit, the largest top-level directories and an
// --exclude example; a 413 that arrives anyway is printed once with the same
// guidance; --max-size bounds the compressed zip.
//
// The fake core is lifecycleStub (analysis_test.go). import is driven from
// importViaUpload, the part after its confirmation; source push through the
// command tree.
package cmd

import (
	"errors"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/fatih/color"
)

const (
	capabilitiesPath = "/api/import/capabilities"
	importPath       = "/api/systems/import"
	pushArchivePath  = "/api/systems/42/repositories/7/source-archive"
)

// incompressible is n bytes that deflate cannot shrink, so a test controls
// the compressed size of a file.
func incompressible(n int, seed int64) string {
	b := make([]byte, n)
	_, _ = rand.New(rand.NewSource(seed)).Read(b)
	return string(b)
}

// estateTree has one directory, assets/, that dominates the compressed size.
func estateTree() map[string]string {
	return map[string]string{
		"assets/data.txt":    incompressible(3000, 1),
		"src/app/Program.cs": incompressible(600, 2),
		"README.md":          "# Legacy estate\n",
	}
}

func smallTree() map[string]string {
	return map[string]string{"src/app/Program.cs": "class Program {}\n", "README.md": "# Legacy estate\n"}
}

// packedZipSize is the size of the zip the CLI builds for dir.
func packedZipSize(t *testing.T, dir string) int {
	t.Helper()
	files, _, err := collectImportFiles(dir, importFilterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zipFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	return archive.Len()
}

func capabilities(limit int) map[string]any {
	return map[string]any{"upload_limit_bytes": limit, "app_url": nil}
}

// captureStreams runs fn with stdout, stderr and the color writers captured,
// and returns everything printed.
func captureStreams(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr, oldColorOut, oldColorErr := os.Stdout, os.Stderr, color.Output, color.Error
	os.Stdout, color.Output = outW, outW
	os.Stderr, color.Error = errW, errW

	var wg sync.WaitGroup
	var stdout, stderr []byte
	wg.Add(2)
	go func() { defer wg.Done(); stdout, _ = io.ReadAll(outR) }()
	go func() { defer wg.Done(); stderr, _ = io.ReadAll(errR) }()

	err := fn()

	_ = outW.Close()
	_ = errW.Close()
	wg.Wait()
	os.Stdout, os.Stderr, color.Output, color.Error = oldOut, oldErr, oldColorOut, oldColorErr
	return string(stdout) + string(stderr), err
}

// uploadVerb is one of the two commands that upload a packed tree.
type uploadVerb struct {
	name     string
	postPath string
	command  string // the command an --exclude example repeats
	setup    func(t *testing.T, s *lifecycleStub, tree map[string]string) string
	run      func(t *testing.T, s *lifecycleStub, dir string, maxSizeMB int) (string, error)
}

func importWorkspace(t *testing.T, s *lifecycleStub, tree map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	writeTree(t, dir, tree)
	t.Chdir(dir)
	s.answer(http.MethodPost, importPath, http.StatusCreated, map[string]any{"success": true, "data": map[string]any{
		"id": 42, "name": "Legacy Estate", "slug": "legacy-estate", "repository_id": 7,
	}})
	return dir
}

func runImportUpload(t *testing.T, s *lifecycleStub, dir string, maxSizeMB int) (string, error) {
	t.Helper()
	savedMax, savedExclude, savedNoIgnore := importMaxSizeMB, importExclude, importNoIgnore
	t.Cleanup(func() { importMaxSizeMB, importExclude, importNoIgnore = savedMax, savedExclude, savedNoIgnore })
	importMaxSizeMB, importExclude, importNoIgnore = maxSizeMB, nil, false
	return captureStreams(t, func() error { return importViaUpload(s.server.URL, "t", "Legacy Estate", dir) })
}

func pushWorkspace(t *testing.T, s *lifecycleStub, tree map[string]string) string {
	t.Helper()
	dir := bindLifecycleWorkspace(t, s, 7)
	writeTree(t, dir, tree)
	s.answer(http.MethodGet, "/api/systems/42/repositories/7", http.StatusOK, map[string]any{"data": uploadRepository("old", "push", "completed")})
	s.answer(http.MethodPost, pushArchivePath, http.StatusOK, map[string]any{
		"result": "superseded", "revision": parityDigest, "previous_revision": "old", "refresh_job_id": 5,
	})
	return dir
}

func runSourcePush(t *testing.T, s *lifecycleStub, dir string, maxSizeMB int) (string, error) {
	t.Helper()
	return runVerb(t, "source", "push", "--max-size", strconv.Itoa(maxSizeMB))
}

var uploadVerbs = []uploadVerb{
	{name: "import", postPath: importPath, command: "modernpath import --local", setup: importWorkspace, run: runImportUpload},
	{name: "source push", postPath: pushArchivePath, command: "modernpath source push", setup: pushWorkspace, run: runSourcePush},
}

// refusedOnce: the command fails, and its reason is marked as printed so
// Execute does not print it a second time.
func refusedOnce(t *testing.T, err error, out string) {
	t.Helper()
	if err == nil {
		t.Fatalf("the command must exit non-zero:\n%s", out)
	}
	var reported reportedError
	if !errors.As(err, &reported) {
		t.Errorf("the refusal must be printed once and returned as reportedError, got %T %v", err, err)
	}
	if strings.Contains(out, "Error:") {
		t.Errorf("cobra must not print the refusal a second time:\n%s", out)
	}
}

// AC1: a body over the server's limit — a zip over it, or a zip just under it
// whose multipart body is over it — is refused before the POST, naming the zip
// size, the limit, the largest directories and an --exclude example.
func TestUploadRefusesABodyOverTheLimitBeforeSending(t *testing.T) {
	for _, verb := range uploadVerbs {
		for _, tc := range []struct {
			name  string
			limit func(zipSize int) int
		}{
			{"zip over the limit", func(int) int { return 1000 }},
			{"zip under the limit, body over it", func(zipSize int) int { return zipSize + 10 }},
		} {
			t.Run(verb.name+"/"+tc.name, func(t *testing.T) {
				s := newLifecycleStub(t)
				dir := verb.setup(t, s, estateTree())
				zipSize := packedZipSize(t, dir)
				limit := tc.limit(zipSize)
				s.answer(http.MethodGet, capabilitiesPath, http.StatusOK, capabilities(limit))

				out, err := verb.run(t, s, dir, 100)
				refusedOnce(t, err, out)
				if s.requested(http.MethodPost, verb.postPath) != nil {
					t.Errorf("a body over the limit must not be uploaded:\n%s", out)
				}
				for _, want := range []string{
					groupDigits(int64(zipSize)) + " bytes",
					groupDigits(int64(limit)) + " bytes",
					"assets/",
					verb.command + " --exclude assets",
				} {
					if !strings.Contains(out, want) {
						t.Errorf("the refusal must name %q:\n%s", want, out)
					}
				}
				if n := strings.Count(out, "accepts at most"); n != 1 {
					t.Errorf("the limit must be stated once, stated %d times:\n%s", n, out)
				}
			})
		}
	}
}

// AC2: a 413 in either JSON shape the server sends — its own body_too_large
// with limit_bytes, or Plug's error view whose `error` is an object — prints
// one message with the same guidance, never the raw JSON, and exits non-zero.
func TestUploadPrintsA413OnceWithTheGuidance(t *testing.T) {
	for _, verb := range uploadVerbs {
		for _, tc := range []struct {
			name      string
			body      map[string]any
			wantLimit string
		}{
			{"body_too_large", map[string]any{
				"error":       "body_too_large",
				"message":     "The request body is larger than this route's limit of 60000000 bytes.",
				"limit_bytes": 60000000,
			}, "60,000,000 bytes"},
			{"error view", map[string]any{
				"errors":     map[string]any{"detail": "Request Entity Too Large"},
				"error":      map[string]any{"reason": "request_too_large", "message": "Request Entity Too Large"},
				"request_id": "F1",
			}, "90,000,000 bytes"},
		} {
			t.Run(verb.name+"/"+tc.name, func(t *testing.T) {
				s := newLifecycleStub(t)
				dir := verb.setup(t, s, estateTree())
				s.answer(http.MethodGet, capabilitiesPath, http.StatusOK, capabilities(90000000))
				s.answer(http.MethodPost, verb.postPath, http.StatusRequestEntityTooLarge, tc.body)

				out, err := verb.run(t, s, dir, 100)
				refusedOnce(t, err, out)
				if s.requested(http.MethodPost, verb.postPath) == nil {
					t.Fatalf("a body under the limit read must be sent:\n%s", out)
				}
				if n := strings.Count(out, "accepts at most"); n != 1 {
					t.Errorf("the 413 must be printed once, printed %d times:\n%s", n, out)
				}
				for _, want := range []string{tc.wantLimit, "assets/", verb.command + " --exclude assets"} {
					if !strings.Contains(out, want) {
						t.Errorf("the 413 must name %q:\n%s", want, out)
					}
				}
				if strings.Contains(out, `"error":`) {
					t.Errorf("the 413 must not be printed as raw JSON:\n%s", out)
				}
			})
		}
	}
	if !importCmd.SilenceErrors {
		t.Error("import must set SilenceErrors, or cobra prints its refusal a second time")
	}
}

// C1: a server that answers 404 to the read predates it, and its limit is the
// endpoint's 50,000,000 bytes; a read that fails with a 5xx falls back to the
// default 90,000,000. Either way the CLI says which limit it used, and a body
// under it is uploaded.
func TestUploadLimitWhenTheServerDoesNotPublishIt(t *testing.T) {
	for _, verb := range uploadVerbs {
		for _, tc := range []struct {
			name   string
			status int // 0: the stub has no capabilities route and answers 404
			want   []string
		}{
			{"404, a server that predates the read", 0, []string{"50,000,000 bytes", "predates"}},
			{"503, the read failed", http.StatusServiceUnavailable, []string{"90,000,000 bytes", "assuming"}},
		} {
			t.Run(verb.name+"/"+tc.name, func(t *testing.T) {
				s := newLifecycleStub(t)
				dir := verb.setup(t, s, smallTree())
				if tc.status != 0 {
					s.answer(http.MethodGet, capabilitiesPath, tc.status, refusal("unavailable", "try again later"))
				}

				out, err := verb.run(t, s, dir, 100)
				if err != nil {
					t.Fatalf("a small body must be uploaded: %v\n%s", err, out)
				}
				if s.requested(http.MethodPost, verb.postPath) == nil {
					t.Errorf("a small body must be uploaded:\n%s", out)
				}
				for _, want := range tc.want {
					if !strings.Contains(out, want) {
						t.Errorf("the output must say which limit was used, %q:\n%s", want, out)
					}
				}
			})
		}
	}
}

// C1, on a connection the server drops: a server that predates the read may
// raise on the unknown route and close the kept-alive connection a moment
// after its 404 (the Phoenix dev endpoint does; RUN:2026-10-04 against the
// local stack, every push failed with "connection reset by peer"). The
// capabilities read must not leave its connection for the upload, or the
// POST goes out on a socket the server is closing — and a POST that has
// written its body is never retried.
func TestUploadSurvivesAServerThatDropsTheConnectionAfterThe404(t *testing.T) {
	for _, verb := range uploadVerbs {
		t.Run(verb.name, func(t *testing.T) {
			s := newLifecycleStub(t)
			dir := verb.setup(t, s, smallTree())
			s.dropConnectionAfter(http.MethodGet, capabilitiesPath)

			out, err := verb.run(t, s, dir, 100)
			if err != nil {
				t.Fatalf("the upload must not share the capabilities read's connection: %v\n%s", err, out)
			}
			if s.requested(http.MethodPost, verb.postPath) == nil {
				t.Errorf("a small body must be uploaded:\n%s", out)
			}
			if !strings.Contains(out, "predates") {
				t.Errorf("the output must say the legacy limit was used:\n%s", out)
			}
		})
	}
}

// AC3: --max-size bounds the compressed zip, not the files before
// compression, and its help says so.
func TestMaxSizeBoundsTheCompressedZip(t *testing.T) {
	for _, verb := range uploadVerbs {
		t.Run(verb.name+"/a tree over the size uncompressed is uploaded", func(t *testing.T) {
			s := newLifecycleStub(t)
			dir := verb.setup(t, s, map[string]string{"src/generated.txt": strings.Repeat("a", 3<<20)})
			s.answer(http.MethodGet, capabilitiesPath, http.StatusOK, capabilities(90000000))

			out, err := verb.run(t, s, dir, 1)
			if err != nil {
				t.Fatalf("3 MiB that compress to a few kB fit --max-size 1: %v\n%s", err, out)
			}
			if s.requested(http.MethodPost, verb.postPath) == nil {
				t.Errorf("the compressed zip is under --max-size and must be uploaded:\n%s", out)
			}
		})
		t.Run(verb.name+"/a zip over the size is refused with the guidance", func(t *testing.T) {
			s := newLifecycleStub(t)
			dir := verb.setup(t, s, map[string]string{"assets/data.bin": incompressible(1200<<10, 3), "README.md": "# r\n"})
			s.answer(http.MethodGet, capabilitiesPath, http.StatusOK, capabilities(90000000))

			out, err := verb.run(t, s, dir, 1)
			refusedOnce(t, err, out)
			if s.requested(http.MethodPost, verb.postPath) != nil {
				t.Errorf("a zip over --max-size must not be uploaded:\n%s", out)
			}
			for _, want := range []string{"--max-size", "1,048,576 bytes", verb.command + " --exclude assets"} {
				if !strings.Contains(out, want) {
					t.Errorf("the refusal must name %q:\n%s", want, out)
				}
			}
		})
	}
	for _, args := range [][]string{{"import", "--help"}, {"source", "push", "--help"}} {
		out, err := runVerb(t, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !strings.Contains(out, "compressed") {
			t.Errorf("%v must say --max-size measures the compressed zip:\n%s", args, out)
		}
	}
}
