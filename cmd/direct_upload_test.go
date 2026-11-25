// SR-5075-0 (Epic 528): when the capabilities read offers direct_upload,
// `import` and `source push` send the zip's size and md5 to
// POST /api/source-uploads, upload the zip in chunks to the GCS resumable
// session it answers, continue from the range GCS reports after an
// interrupted chunk, and complete through the existing endpoint with a JSON
// body naming the upload. Without direct_upload they post multipart as today.
//
// The fake core is lifecycleStub (analysis_test.go); the fake GCS session is
// gcsSession below.
package cmd

import (
	"archive/zip"
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const sourceUploadsPath = "/api/source-uploads"

// gcsSession is a GCS resumable upload session: it accepts chunks in order,
// answers 308 with the persisted range until the object is whole, and answers
// a status query (Content-Range: bytes */<size>) with the persisted range.
// interruptPut makes the n-th chunk PUT (1-based) persist only keep bytes of
// its chunk and drop the connection without an answer.
type gcsSession struct {
	t            *testing.T
	server       *httptest.Server
	mu           sync.Mutex
	data         []byte
	done         bool
	chunkPuts    int
	queries      int
	interruptPut int
	keep         int
	ranges       []string
	authHeaders  []string
}

func newGCSSession(t *testing.T) *gcsSession {
	t.Helper()
	g := &gcsSession{t: t}
	g.server = httptest.NewServer(http.HandlerFunc(g.handle))
	t.Cleanup(g.server.Close)
	return g
}

func (g *gcsSession) uri() string {
	return g.server.URL + "/upload/storage/v1/b/bucket/o?uploadType=resumable&upload_id=secret-session"
}

func (g *gcsSession) handle(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if r.Method != http.MethodPut {
		g.t.Errorf("the session takes PUT, got %s", r.Method)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	g.authHeaders = append(g.authHeaders, r.Header.Get("Authorization"))
	contentRange := r.Header.Get("Content-Range")
	g.ranges = append(g.ranges, contentRange)
	body, _ := io.ReadAll(r.Body)
	spec, total, ok := strings.Cut(strings.TrimPrefix(contentRange, "bytes "), "/")
	size, err := strconv.Atoi(total)
	if !ok || err != nil {
		g.t.Errorf("Content-Range %q must be bytes <range>/<size>", contentRange)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if spec == "*" {
		g.queries++
		g.answerProgress(w, size)
		return
	}
	first, last, _ := strings.Cut(spec, "-")
	from, _ := strconv.Atoi(first)
	to, _ := strconv.Atoi(last)
	if from != len(g.data) || to-from+1 != len(body) {
		g.t.Errorf("chunk %q (%d bytes) must continue at the persisted offset %d", contentRange, len(body), len(g.data))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if to+1 != size && len(body)%(256<<10) != 0 {
		g.t.Errorf("a chunk before the last must be a multiple of 256 KiB, got %d bytes", len(body))
	}
	g.chunkPuts++
	if g.chunkPuts == g.interruptPut {
		g.data = append(g.data, body[:g.keep]...)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
		return
	}
	g.data = append(g.data, body...)
	g.answerProgress(w, size)
}

func (g *gcsSession) answerProgress(w http.ResponseWriter, size int) {
	if len(g.data) == size {
		g.done = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"name":".upload-quarantine/tenant-1/x.zip","size":"%d"}`, size)
		return
	}
	if len(g.data) > 0 {
		w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", len(g.data)-1))
	}
	w.WriteHeader(http.StatusPermanentRedirect)
}

// smallChunks makes a chunk 512 KiB, a multiple of 256 KiB, so a test zip of
// a little over a megabyte takes three chunks.
func smallChunks(t *testing.T) {
	t.Helper()
	saved := directUploadChunkBytes
	directUploadChunkBytes = 512 << 10
	t.Cleanup(func() { directUploadChunkBytes = saved })
}

// chunkedTree packs to a zip of about 1.3 MiB.
func chunkedTree() map[string]string {
	return map[string]string{"src/blob.txt": incompressible(1300<<10, 7), "README.md": "# Legacy estate\n"}
}

func directCapabilities(maxBytes int) map[string]any {
	return map[string]any{"upload_limit_bytes": 90000000, "app_url": appURL, "direct_upload": map[string]any{"max_bytes": maxBytes}}
}

// directVerb runs one of the two upload commands through the command tree.
type directVerb struct {
	name     string
	postPath string
	setup    func(t *testing.T, s *lifecycleStub, tree map[string]string) string
	args     []string
}

func directImportWorkspace(t *testing.T, s *lifecycleStub, tree map[string]string) string {
	t.Helper()
	importCommandWorkspace(t, s)
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	writeTree(t, dir, tree)
	return dir
}

func directPushWorkspace(t *testing.T, s *lifecycleStub, tree map[string]string) string {
	t.Helper()
	dir := pushWorkspace(t, s, tree)
	gitCommitFixture(t, dir)
	return dir
}

var directVerbs = []directVerb{
	{name: "import", postPath: importPath, setup: directImportWorkspace, args: []string{"import", "--yes"}},
	{name: "source push", postPath: pushArchivePath, setup: directPushWorkspace, args: []string{"source", "push"}},
}

// packedDigests are the size and md5 (base64) of the zip the CLI builds for
// dir.
func packedDigests(t *testing.T, dir string) (int, string) {
	t.Helper()
	files, _, err := collectImportFiles(dir, importFilterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := packZip(files)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	data, err := io.ReadAll(archive.reader())
	if err != nil {
		t.Fatal(err)
	}
	m := md5.Sum(data)
	return len(data), base64.StdEncoding.EncodeToString(m[:])
}

func zipNames(t *testing.T, data []byte) []string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("the uploaded object must be the zip: %v", err)
	}
	var names []string
	for _, f := range reader.File {
		names = append(names, f.Name)
	}
	return sortedStrings(names)
}

// TestCase 18e166fd-e00c-4b74-af89-235c322da8ee
// Criterion 03d7d205: with direct_upload offered, both commands open a
// session with the zip's size and md5, PUT it in 256 KiB-multiple
// chunks to the session without the CLI's credential, continue from the
// range GCS reports after a chunk is cut off, complete through the existing
// endpoint with a JSON body naming the upload, and never print the session.
func TestDirectUploadSendsTheZipToTheSessionInChunksAndCompletes(t *testing.T) {
	for _, verb := range directVerbs {
		t.Run(verb.name, func(t *testing.T) {
			smallChunks(t)
			s := newLifecycleStub(t)
			dir := verb.setup(t, s, chunkedTree())
			gcs := newGCSSession(t)
			gcs.interruptPut, gcs.keep = 2, 256<<10
			s.answer(http.MethodGet, capabilitiesPath, http.StatusOK, directCapabilities(500000000))
			s.answer(http.MethodPost, sourceUploadsPath, http.StatusCreated, map[string]any{
				"upload": "7d3c1f9e-0000-4000-8000-000000000001", "session_uri": gcs.uri(),
			})
			size, md5sum := packedDigests(t, dir)

			out, err := runVerb(t, verb.args...)
			if err != nil {
				t.Fatalf("%s: %v\n%s", verb.name, err, out)
			}

			open := s.requested(http.MethodPost, sourceUploadsPath)
			if open == nil {
				t.Fatalf("the CLI must open a direct upload session:\n%s", out)
			}
			fields := 2 // size and md5; a push adds its System and repository
			if verb.name == "source push" {
				fields = 4
			}
			if open.body["size"] != float64(size) || open.body["md5"] != md5sum || len(open.body) != fields {
				t.Errorf("the session must declare only size %d and md5 %s (and a push its ids), got %v", size, md5sum, open.body)
			}
			if verb.name == "source push" && (open.body["system_id"] != float64(42) || open.body["repository_id"] != float64(7)) {
				t.Errorf("a push must name its System and repository, got %v", open.body)
			}
			if verb.name == "import" {
				if _, ok := open.body["system_id"]; ok {
					t.Errorf("an import names no System, got %v", open.body)
				}
			}

			gcs.mu.Lock()
			data, done, chunkPuts, queries := append([]byte(nil), gcs.data...), gcs.done, gcs.chunkPuts, gcs.queries
			ranges, auths := gcs.ranges, gcs.authHeaders
			gcs.mu.Unlock()
			if !done || len(data) != size {
				t.Fatalf("the session must receive the whole zip (%d bytes), got %d bytes, ranges %v", size, len(data), ranges)
			}
			if got := md5.Sum(data); base64.StdEncoding.EncodeToString(got[:]) != md5sum {
				t.Errorf("the uploaded object must be the declared zip")
			}
			if names := zipNames(t, data); !containsString(names, "src/blob.txt") || !containsString(names, "README.md") {
				t.Errorf("the uploaded zip holds %v", names)
			}
			if chunkPuts < 4 || queries < 1 {
				t.Errorf("the zip must go in chunks and resume after the cut chunk: %d chunk PUTs, %d status queries, ranges %v", chunkPuts, queries, ranges)
			}
			if !containsString(ranges, fmt.Sprintf("bytes %d-%d/%d", 768<<10, 1280<<10-1, size)) {
				t.Errorf("after the cut chunk the upload must continue at the reported offset %d, ranges %v", 768<<10, ranges)
			}
			for _, auth := range auths {
				if auth != "" {
					t.Errorf("the CLI's credential must not go to the storage session, got %q", auth)
				}
			}

			complete := s.requested(http.MethodPost, verb.postPath)
			if complete == nil || complete.body["upload"] != "7d3c1f9e-0000-4000-8000-000000000001" {
				t.Fatalf("the upload must complete through %s with a JSON body naming it, got %+v", verb.postPath, complete)
			}
			switch verb.name {
			case "import":
				if complete.body["name"] == "" || complete.body["name"] == nil {
					t.Errorf("an import complete must name the System, got %v", complete.body)
				}
			case "source push":
				head, _ := complete.body["git_head"].(string)
				if len(head) != 40 || complete.body["git_branch"] != "main" || complete.body["captured_at"] == nil {
					t.Errorf("a push complete must carry the git revision and captured_at, got %v", complete.body)
				}
				if dirty := complete.body["git_dirty"]; dirty != "true" && dirty != "false" {
					t.Errorf("git_dirty must be sent as the multipart push sends it, got %v", dirty)
				}
			}
			if strings.Contains(out, "secret-session") || strings.Contains(out, gcs.server.URL) {
				t.Errorf("the session URI must never be printed:\n%s", out)
			}
		})
	}
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// TestCase 18e166fd-e00c-4b74-af89-235c322da8ee
// Criterion 03d7d205: the complete answers as a multipart upload does, so
// the command prints the same result either way.
func TestDirectUploadPrintsTheSameResultAsAMultipartUpload(t *testing.T) {
	for _, verb := range directVerbs {
		t.Run(verb.name, func(t *testing.T) {
			result := func(direct bool) string {
				s := newLifecycleStub(t)
				verb.setup(t, s, smallTree())
				if direct {
					gcs := newGCSSession(t)
					s.answer(http.MethodGet, capabilitiesPath, http.StatusOK, directCapabilities(500000000))
					s.answer(http.MethodPost, sourceUploadsPath, http.StatusCreated, map[string]any{"upload": "u-1", "session_uri": gcs.uri()})
				} else {
					s.answer(http.MethodGet, capabilitiesPath, http.StatusOK, map[string]any{"upload_limit_bytes": 90000000, "app_url": appURL})
				}
				stdout, _, err := runAskStreams(t, verb.args...)
				if err != nil {
					t.Fatalf("%s (direct %v): %v\n%s", verb.name, direct, err, stdout)
				}
				if direct && s.requested(http.MethodPost, sourceUploadsPath) == nil {
					t.Fatalf("with direct_upload offered the zip must go to the session:\n%s", stdout)
				}
				// The result starts where the server's answer is reported.
				for _, marker := range []string{"System created:", "pushed "} {
					if i := strings.Index(stdout, marker); i >= 0 {
						return stdout[i:]
					}
				}
				t.Fatalf("no result in the output:\n%s", stdout)
				return ""
			}
			multipart, direct := result(false), result(true)
			if direct != multipart {
				t.Errorf("the direct upload must print what the multipart upload prints\nmultipart:\n%s\ndirect:\n%s", multipart, direct)
			}
		})
	}
}

// TestCase 18e166fd-e00c-4b74-af89-235c322da8ee
// Criterion 03d7d205: direct_upload.max_bytes replaces the 100 MiB default of
// --max-size when the flag is not given; a given --max-size still applies;
// without direct_upload the default applies as before.
func TestDirectUploadMaxBytesReplacesTheDefaultMaxSize(t *testing.T) {
	const zipSize = int64(150 << 20) // over the 100 MiB default
	direct := uploadCapabilities{limit: uploadLimit{bytes: 90000000}, direct: &directUploadOffer{maxBytes: 500000000}}
	plain := uploadCapabilities{limit: uploadLimit{bytes: 90000000}}

	if bound := uploadZipBound(defaultMaxSizeMB, false, direct); zipSize > bound.bytes {
		t.Errorf("without --max-size a %d-byte zip within direct_upload.max_bytes must not be refused; bound %d", zipSize, bound.bytes)
	}
	if bound := uploadZipBound(defaultMaxSizeMB, true, direct); zipSize <= bound.bytes {
		t.Errorf("an explicit --max-size 100 must still refuse a %d-byte zip; bound %d", zipSize, bound.bytes)
	}
	if bound := uploadZipBound(defaultMaxSizeMB, false, plain); zipSize <= bound.bytes {
		t.Errorf("without direct_upload the 100 MiB default must refuse a %d-byte zip; bound %d", zipSize, bound.bytes)
	}
	if bound := uploadZipBound(defaultMaxSizeMB, false, uploadCapabilities{direct: &directUploadOffer{maxBytes: 1000}}); bound.bytes != 1000 {
		t.Errorf("without --max-size the bound is direct_upload.max_bytes, got %d", bound.bytes)
	}
}

// TestCase 18e166fd-e00c-4b74-af89-235c322da8ee
// Criterion 03d7d205, through the commands: a zip over direct_upload.max_bytes
// is refused before a session is opened when --max-size is not given; a zip
// over a given --max-size is refused although direct_upload.max_bytes is
// larger.
func TestDirectUploadRefusesAZipOverItsBoundBeforeOpeningASession(t *testing.T) {
	for _, verb := range directVerbs {
		for _, tc := range []struct {
			name     string
			maxBytes int
			extra    []string
			want     string
		}{
			{"over direct_upload.max_bytes", 1000, nil, "1,000 bytes"},
			{"over a given --max-size", 500000000, []string{"--max-size", "1"}, "--max-size 1 MiB"},
		} {
			t.Run(verb.name+"/"+tc.name, func(t *testing.T) {
				s := newLifecycleStub(t)
				verb.setup(t, s, chunkedTree())
				s.answer(http.MethodGet, capabilitiesPath, http.StatusOK, directCapabilities(tc.maxBytes))
				s.answer(http.MethodPost, sourceUploadsPath, http.StatusCreated, map[string]any{"upload": "u-1", "session_uri": "http://127.0.0.1:1/never"})

				out, err := runVerb(t, append(append([]string{}, verb.args...), tc.extra...)...)
				if err == nil {
					t.Fatalf("the zip must be refused:\n%s", out)
				}
				if s.requested(http.MethodPost, sourceUploadsPath) != nil || s.requested(http.MethodPost, verb.postPath) != nil {
					t.Errorf("nothing may be uploaded for a zip over its bound:\n%s", out)
				}
				if !strings.Contains(out, tc.want) {
					t.Errorf("the refusal must name %q:\n%s", tc.want, out)
				}
			})
		}
	}
}

// TestCase 18e166fd-e00c-4b74-af89-235c322da8ee
// Criterion 03d7d205, otherwise branch: without direct_upload no session is
// opened, the zip is posted multipart as today, and a body over
// upload_limit_bytes is refused before sending.
func TestWithoutDirectUploadTheZipIsPostedMultipart(t *testing.T) {
	for _, verb := range directVerbs {
		t.Run(verb.name+"/posted multipart", func(t *testing.T) {
			s := newLifecycleStub(t)
			verb.setup(t, s, smallTree())
			s.answer(http.MethodGet, capabilitiesPath, http.StatusOK, capabilities(90000000))

			out, err := runVerb(t, verb.args...)
			if err != nil {
				t.Fatalf("%s: %v\n%s", verb.name, err, out)
			}
			if s.requested(http.MethodPost, sourceUploadsPath) != nil {
				t.Errorf("without direct_upload no session may be opened:\n%s", out)
			}
			post := s.requested(http.MethodPost, verb.postPath)
			if post == nil || post.body != nil {
				t.Errorf("the zip must be posted multipart (not JSON), got %+v", post)
			}
		})
		t.Run(verb.name+"/over upload_limit_bytes", func(t *testing.T) {
			s := newLifecycleStub(t)
			verb.setup(t, s, estateTree())
			s.answer(http.MethodGet, capabilitiesPath, http.StatusOK, capabilities(1000))

			out, err := runVerb(t, verb.args...)
			if err == nil || s.requested(http.MethodPost, verb.postPath) != nil {
				t.Fatalf("a body over upload_limit_bytes must be refused before sending: %v\n%s", err, out)
			}
			if !strings.Contains(out, "accepts at most 1,000 bytes") {
				t.Errorf("the refusal must name the limit:\n%s", out)
			}
		})
	}
}

// TestCase 18e166fd-e00c-4b74-af89-235c322da8ee
// Plan assumption (Epic 528): when the storage session cannot be reached, a
// zip that fits upload_limit_bytes is posted multipart; one that does not is
// refused, naming the storage host as unreachable.
func TestDirectUploadFallsBackToMultipartWhenStorageIsUnreachable(t *testing.T) {
	for _, verb := range directVerbs {
		for _, tc := range []struct {
			name     string
			limit    int
			fallback bool
		}{
			{"fits upload_limit_bytes", 90000000, true},
			{"over upload_limit_bytes", 100, false},
		} {
			t.Run(verb.name+"/"+tc.name, func(t *testing.T) {
				s := newLifecycleStub(t)
				verb.setup(t, s, smallTree())
				unreachable := httptest.NewServer(http.NotFoundHandler())
				deadURI := unreachable.URL + "/upload?upload_id=secret-session"
				unreachable.Close()
				s.answer(http.MethodGet, capabilitiesPath, http.StatusOK, map[string]any{
					"upload_limit_bytes": tc.limit, "direct_upload": map[string]any{"max_bytes": 500000000},
				})
				s.answer(http.MethodPost, sourceUploadsPath, http.StatusCreated, map[string]any{"upload": "u-1", "session_uri": deadURI})

				out, err := runVerb(t, verb.args...)
				post := s.requested(http.MethodPost, verb.postPath)
				if tc.fallback {
					if err != nil || post == nil || post.body != nil {
						t.Fatalf("a zip that fits must be posted multipart: %v %+v\n%s", err, post, out)
					}
				} else {
					if err == nil || post != nil {
						t.Fatalf("a zip over upload_limit_bytes must not be posted: %v\n%s", err, out)
					}
					if !strings.Contains(out, "unreachable") {
						t.Errorf("the refusal must say the storage host is unreachable:\n%s", out)
					}
				}
				if strings.Contains(out, "secret-session") {
					t.Errorf("the session URI must never be printed:\n%s", out)
				}
			})
		}
	}
}

// TestCase 18e166fd-e00c-4b74-af89-235c322da8ee
// A connection that breaks while the first chunk is sent reached storage: the
// upload asks the session what it holds and continues there, instead of
// treating storage as unreachable.
func TestDirectUploadResumesAFirstChunkCutOffMidBody(t *testing.T) {
	for _, verb := range directVerbs {
		t.Run(verb.name, func(t *testing.T) {
			smallChunks(t)
			s := newLifecycleStub(t)
			dir := verb.setup(t, s, chunkedTree())
			gcs := newGCSSession(t)
			gcs.interruptPut, gcs.keep = 1, 256<<10
			s.answer(http.MethodGet, capabilitiesPath, http.StatusOK, directCapabilities(500000000))
			s.answer(http.MethodPost, sourceUploadsPath, http.StatusCreated, map[string]any{"upload": "u-1", "session_uri": gcs.uri()})
			size, _ := packedDigests(t, dir)

			out, err := runVerb(t, verb.args...)
			if err != nil {
				t.Fatalf("%s: %v\n%s", verb.name, err, out)
			}
			gcs.mu.Lock()
			data, done, ranges := len(gcs.data), gcs.done, gcs.ranges
			gcs.mu.Unlock()
			if !done || data != size {
				t.Fatalf("the session must receive the whole zip (%d bytes), got %d, ranges %v", size, data, ranges)
			}
			if !containsString(ranges, fmt.Sprintf("bytes %d-%d/%d", 256<<10, 768<<10-1, size)) {
				t.Errorf("after the cut first chunk the upload must continue at the reported offset %d, ranges %v", 256<<10, ranges)
			}
			if complete := s.requested(http.MethodPost, verb.postPath); complete == nil || complete.body["upload"] != "u-1" {
				t.Errorf("the upload must complete directly, not multipart, got %+v\n%s", complete, out)
			}
			if strings.Contains(out, "unreachable") {
				t.Errorf("a cut chunk is not an unreachable host:\n%s", out)
			}
		})
	}
}

// TestCase 18e166fd-e00c-4b74-af89-235c322da8ee
// Plan assumption (Epic 528): a proxy or firewall that answers for the
// storage host (403 with its own page) is a blocked storage host too: a zip
// that fits upload_limit_bytes is posted multipart; one that does not is
// refused, naming the answer.
func TestDirectUploadFallsBackToMultipartWhenSomethingElseAnswersForStorage(t *testing.T) {
	for _, verb := range directVerbs {
		for _, tc := range []struct {
			name     string
			limit    int
			fallback bool
		}{
			{"fits upload_limit_bytes", 90000000, true},
			{"over upload_limit_bytes", 100, false},
		} {
			t.Run(verb.name+"/"+tc.name, func(t *testing.T) {
				s := newLifecycleStub(t)
				verb.setup(t, s, smallTree())
				proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.Copy(io.Discard, r.Body)
					w.Header().Set("Content-Type", "text/html")
					w.WriteHeader(http.StatusForbidden)
					_, _ = io.WriteString(w, "<html>Blocked by policy</html>")
				}))
				t.Cleanup(proxy.Close)
				s.answer(http.MethodGet, capabilitiesPath, http.StatusOK, map[string]any{
					"upload_limit_bytes": tc.limit, "direct_upload": map[string]any{"max_bytes": 500000000},
				})
				s.answer(http.MethodPost, sourceUploadsPath, http.StatusCreated, map[string]any{
					"upload": "u-1", "session_uri": proxy.URL + "/upload?upload_id=secret-session",
				})

				out, err := runVerb(t, verb.args...)
				post := s.requested(http.MethodPost, verb.postPath)
				if tc.fallback {
					if err != nil || post == nil || post.body != nil {
						t.Fatalf("a zip that fits must be posted multipart: %v %+v\n%s", err, post, out)
					}
				} else if err == nil || post != nil {
					t.Fatalf("a zip over upload_limit_bytes must not be posted: %v\n%s", err, out)
				}
				if !strings.Contains(out, "refused the upload (403 Forbidden)") {
					t.Errorf("the output must name the answer storage got:\n%s", out)
				}
				if strings.Contains(out, "secret-session") {
					t.Errorf("the session URI must never be printed:\n%s", out)
				}
			})
		}
	}
}

// The multipart form streams the zip from its file between the fields and
// the closing boundary, and is posted with its Content-Length, not chunked.
func TestMultipartFormStreamsTheZipWithItsLength(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, smallTree())
	files, _, err := collectImportFiles(dir, importFilterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := packZip(files)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	zipBytes, _ := io.ReadAll(archive.reader())

	var length int64
	var chunked []string
	var fields map[string]string
	var part []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		length, chunked, fields = r.ContentLength, r.TransferEncoding, map[string]string{}
		reader, err := r.MultipartReader()
		if err != nil {
			t.Errorf("the body must be multipart: %v", err)
			return
		}
		for {
			p, err := reader.NextPart()
			if err != nil {
				break
			}
			value, _ := io.ReadAll(p)
			if p.FormName() == "file" {
				part = value
			} else {
				fields[p.FormName()] = string(value)
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	send := sourceSend{
		client:   &authenticatedClient{client: server.Client(), baseURL: server.URL},
		caps:     uploadCapabilities{limit: uploadLimit{bytes: 90000000}},
		sizes:    uploadSizes{zip: archive},
		postURL:  server.URL + importPath,
		fileName: "upload.zip",
		form:     [][2]string{{"name", "Legacy"}},
	}
	form, err := send.multipartForm()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := send.postForm(form); err != nil {
		t.Fatal(err)
	}
	if length != form.size() || len(chunked) != 0 {
		t.Errorf("the form must be posted with its Content-Length %d, got %d (transfer encoding %v)", form.size(), length, chunked)
	}
	if !bytes.Equal(part, zipBytes) || fields["name"] != "Legacy" {
		t.Errorf("the form must carry the fields and the zip unchanged, got fields %v and %d zip bytes", fields, len(part))
	}
}

// A file that cannot be read stops the zip, naming it, instead of being
// packed as an empty file that would replace the source's copy.
func TestWriteZipRefusesAFileItCannotRead(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, smallTree())
	files, _, err := collectImportFiles(dir, importFilterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal(err)
	}

	err = writeZip(io.Discard, files)
	if err == nil || !strings.Contains(err.Error(), "cannot read README.md") || !strings.Contains(err.Error(), `--exclude "README.md"`) {
		t.Fatalf("an unreadable file must stop the zip, naming it and the --exclude that skips it, got %v", err)
	}
}
