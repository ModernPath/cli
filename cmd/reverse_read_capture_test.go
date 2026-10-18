package cmd

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// captureServer serves one capture of the given files, path to content, and
// counts the source-file reads. A path in corrupt is served with content that
// does not match its captured sha256.
func captureServer(t *testing.T, files map[string]string, corrupt map[string]bool) (*httptest.Server, *int) {
	t.Helper()
	var mu sync.Mutex
	reads := 0
	ids := map[string]string{}
	var rows []string
	i := 0
	for path, content := range files {
		i++
		id := fmt.Sprintf("file-%d", i)
		ids[id] = path
		sum := sha256.Sum256([]byte(content))
		rows = append(rows, fmt.Sprintf(`{"id":%q,"path":%q,"sha256":%q,"size":%d}`, id, path, hex.EncodeToString(sum[:]), len(content)))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/systems/4/reverse-engineering/sources/cap-1":
			_, _ = fmt.Fprintf(w, `{"data":{"id":"cap-1","repository_key":"Repo","files":[%s]}}`, strings.Join(rows, ","))
		case strings.HasPrefix(r.URL.Path, "/api/v1/systems/4/reverse-engineering/source-files/"):
			mu.Lock()
			reads++
			mu.Unlock()
			path := ids[strings.TrimPrefix(r.URL.Path, "/api/v1/systems/4/reverse-engineering/source-files/")]
			content := files[path]
			if corrupt[path] {
				content += "changed"
			}
			_, _ = fmt.Fprintf(w, `{"data":{"path":%q,"content_base64":%q,"truncated":false}}`, path, base64.StdEncoding.EncodeToString([]byte(content)))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	return server, &reads
}

func TestReadSourceCaptureWritesTheSelectedFilesAsPlainFiles(t *testing.T) {
	server, reads := captureServer(t, map[string]string{
		"src/Wash/Controller.cs": "class Controller {}",
		"src/Wash/Service.cs":    "class Service {}",
		"docs/readme.md":         "# readme",
	}, nil)
	out := t.TempDir()
	stdout, err := reCommand(t, server, "", "read-source", "--capture", "cap-1", "--out", out, "--prefix", "src/")
	if err != nil {
		t.Fatalf("read-source --capture: %v\n%s", err, stdout)
	}
	if !strings.Contains(stdout, "2 of 3 files selected; 2 written, 0 already present, 0 failed") {
		t.Fatalf("summary line: %s", stdout)
	}
	got, err := os.ReadFile(filepath.Join(out, "src", "Wash", "Service.cs"))
	if err != nil || string(got) != "class Service {}" {
		t.Fatalf("written file: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(out, "docs", "readme.md")); !os.IsNotExist(err) {
		t.Fatalf("a file outside --prefix was written")
	}
	// A second read keeps identical copies without fetching them again.
	before := *reads
	stdout, err = reCommand(t, server, "", "read-source", "--capture", "cap-1", "--out", out, "--prefix", "src/")
	if err != nil || !strings.Contains(stdout, "0 written, 2 already present") || *reads != before {
		t.Fatalf("rerun fetched again or rewrote: %s reads %d→%d %v", stdout, before, *reads, err)
	}
}

func TestReadSourceCaptureRefusesWhatItCannotVerify(t *testing.T) {
	server, _ := captureServer(t, map[string]string{
		"src/a.cs": "a",
		"src/b.cs": "b",
	}, map[string]bool{"src/b.cs": true})
	out := t.TempDir()
	list := filepath.Join(t.TempDir(), "paths.txt")
	if err := os.WriteFile(list, []byte("# wanted\nsrc/a.cs\nsrc/b.cs\nsrc/missing.cs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, err := reCommand(t, server, "", "read-source", "--capture", "cap-1", "--out", out, "--paths-from", list)
	if err == nil {
		t.Fatalf("a failed file must fail the command: %s", stdout)
	}
	for _, want := range []string{"1 written", "2 failed", "src/b.cs: content does not match the captured sha256", "src/missing.cs: not in this capture"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("missing %q in: %s", want, stdout)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "src", "b.cs")); !os.IsNotExist(err) {
		t.Fatalf("a file whose content does not match was written")
	}
}

func TestReadSourceCaptureKeepsPathsInsideTheFolder(t *testing.T) {
	server, _ := captureServer(t, map[string]string{"../outside.cs": "x"}, nil)
	out := t.TempDir()
	stdout, err := reCommand(t, server, "", "read-source", "--capture", "cap-1", "--out", out)
	if err == nil || !strings.Contains(stdout, "../outside.cs: path leaves the output folder") {
		t.Fatalf("an escaping path was not refused: %s %v", stdout, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(out), "outside.cs")); !os.IsNotExist(err) {
		t.Fatalf("a file was written outside --out")
	}
}

func TestReadSourceTakesOneFileOrOneCapture(t *testing.T) {
	server, _ := captureServer(t, map[string]string{"src/a.cs": "a"}, nil)
	for _, args := range [][]string{
		{"read-source", "--source", "file-1", "--capture", "cap-1", "--out", t.TempDir()},
		{"read-source", "--capture", "cap-1"},
		{"read-source"},
	} {
		if out, err := reCommand(t, server, "", args...); err == nil {
			t.Fatalf("%v was accepted: %s", args, out)
		}
	}
}

func TestCoverageSummaryPrintsCountsAndOnlyTheFilesToActOn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"run_id":"run-id","input_fingerprint":"sha256:x","assessments":{"unassessed_files":2,"unresolved_files":1},
			"per_repository":{"Repo":{"included_files":4}},
			"files":[
			  {"path":"a.cs","repository_key":"Repo","assessment":"reviewed","capture":"ready","governed":false},
			  {"path":"b.cs","repository_key":"Repo","assessment":"unassessed","capture":"ready","governed":true},
			  {"path":"c.cs","repository_key":"Repo","assessment":"unassessed","capture":"ready","governed":false},
			  {"path":"d.cs","repository_key":"Repo","assessment":"unresolved","capture":"ready","governed":true}]}}`))
	}))
	defer server.Close()
	out, err := reCommand(t, server, "", "coverage", "--run", "run-id", "--summary")
	if err != nil {
		t.Fatalf("coverage --summary: %v", err)
	}
	for _, want := range []string{"assessments: unassessed_files=2 unresolved_files=1", "files: 4; to act on: 2", "unassessed ready unlinked Repo:c.cs", "unresolved ready governed Repo:d.cs", "Repo={included_files=4}"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"a.cs", "b.cs", "fingerprint", "{\""} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("%q printed in:\n%s", unwanted, out)
		}
	}
}

func TestStatusSummaryKeepsTheLatestGroups(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"id":"run-id","key":"run-key","groups":[
			  {"key":"third","sequence":3,"result":{"created_requirements":1,"requirement_ids":["UR-X-1"]}},
			  {"key":"first","sequence":1,"result":{"created_requirements":2,"requirement_ids":["UR-X-2","SR-X-2"]}},
			  {"key":"second","sequence":2,"result":{"created_requirements":0,"requirement_ids":[]}}]}}`))
	}))
	defer server.Close()
	out, err := reCommand(t, server, "", "status", "--run", "run-id", "--summary", "--limit", "2")
	if err != nil {
		t.Fatalf("status --summary: %v", err)
	}
	if !strings.Contains(out, "groups: 3\n  … 1 earlier\n  - key=second") || !strings.Contains(out, "key=third sequence=3 result={created_requirements=1 requirement_ids=[1]}") || strings.Contains(out, "key=first") {
		t.Fatalf("status summary:\n%s", out)
	}
}
