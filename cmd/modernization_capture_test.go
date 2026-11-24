package cmd

// Epic 390 (D5), test case TC-D5-4: `modernpath modernization record` sends an
// entry with no criterion, and its help, the CLI guide and the CLI reference
// describe capture-only entries and runs of the Release's own System (SR-D5-1,
// criterion 3). PROCESS.md's sentence on historical results is the session's
// phrase test, recorded with its edit.
//
// On slice 1 the command refuses an empty criterion_ids before sending, so the
// assertion on the sent payload fails, and the described sentences do not
// exist yet. The refusals of an entry without criterion_ids, with a
// criterion_ids that is not a list and with a criterion that is not a UUID
// hold on every slice and guard the rule.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/modernpath/cli/internal/kit"
)

// A full commit id in lower case that no checkout holds, so the command sends
// the capture without a chain from any folder.
const captureNoCheckoutSHA = "0123456789abcdef0123456789abcdef01234567"

// plainText folds whitespace and strips code marks, so a help text or a guide
// may wrap or mark its words.
func plainText(text string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(text, "`", "")), " ")
}

// modernizationGuideSection is the CLI guide's section "Modernization test
// history", from its heading to the next heading, as plain text. The kit's
// copy equals modernpath-core/docs/cli.md (TestEmbeddedCLIGuideMatchesCanonicalSource).
func modernizationGuideSection(t *testing.T) string {
	t.Helper()
	body, err := fs.ReadFile(kit.Assets(), "assets/cli-guide/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	var section []string
	found := false
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "#") {
			if found {
				break
			}
			found = strings.TrimSpace(line) == "### Modernization test history"
			if !found {
				continue
			}
		}
		if found {
			section = append(section, line)
		}
	}
	if !found {
		t.Fatal("the CLI guide has no section ### Modernization test history")
	}
	return plainText(strings.Join(section, "\n"))
}

// referenceRow is the refusal glossary's row whose first cell is `code`, as
// plain text, or "".
func referenceRow(code string) string {
	for _, line := range strings.Split(referenceLoop, "\n") {
		if strings.HasPrefix(line, "| `"+code+"`") {
			return plainText(line)
		}
	}
	return ""
}

// referencePassRow is the pass-to-command map's row for the named pass.
func referencePassRow(pass string) string {
	for _, line := range strings.Split(referenceLoop, "\n") {
		if strings.HasPrefix(line, "| "+pass+" |") {
			return plainText(line)
		}
	}
	return ""
}

// recordCapture runs `modernization record --release-id 17 --file <file>` from
// a workspace folder that is not a git checkout, with the given tests list,
// and returns what the server received, the error and the stderr text.
func recordCapture(t *testing.T, tests string) (map[string]any, string, error) {
	t.Helper()
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/compliance/releases/17/modernization/test-runs" {
			t.Errorf("request = %s %s; want the capture alone", r.Method, r.URL)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"data":{"run":{"id":"run-1","sha":%q,"system_id":4},"results":[]}}`, captureNoCheckoutSHA)
	}))
	t.Cleanup(server.Close)
	env := nativeCLIEnv(t, server, 4)
	file := filepath.Join(t.TempDir(), "capture.json")
	if err := os.WriteFile(file, []byte(fmt.Sprintf(`{"sha":%q,"tests":%s}`, captureNoCheckoutSHA, tests)), 0o600); err != nil {
		t.Fatal(err)
	}
	command := newModernizationCommand(func() (*cliEnv, error) { return env, nil })
	command.SetArgs([]string{"record", "--release-id", "17", "--file", file, "--json"})
	var out, errOut bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&errOut)
	err := command.Execute()
	return received, errOut.String(), err
}

func TestD5ModernizationRecordSendsACaptureOnlyEntry(t *testing.T) {
	guide := modernizationGuideSection(t)

	t.Run("an entry whose criterion_ids is an empty list is sent", func(t *testing.T) {
		received, stderr, err := recordCapture(t, `[{"test_case_id":"22222222-2222-4222-8222-222222222222","result":"pass","criterion_ids":[]}]`)
		if err != nil {
			t.Fatalf("modernization record refused the capture-only entry: %v\n%s", err, stderr)
		}
		if received == nil {
			t.Fatal("no capture was sent")
		}
		if received["sha"] != captureNoCheckoutSHA || len(received) != 3 {
			t.Errorf("payload = %#v; want sha, tests and system_id", received)
		}
		tests, _ := received["tests"].([]any)
		if len(tests) != 1 {
			t.Fatalf("sent tests = %v; want one entry", received["tests"])
		}
		entry, _ := tests[0].(map[string]any)
		if !reflect.DeepEqual(entry["criterion_ids"], []any{}) {
			t.Errorf("sent criterion_ids = %#v; want an empty list", entry["criterion_ids"])
		}
	})

	for name, tests := range map[string]string{
		"an entry without criterion_ids":             `[{"test_case_id":"22222222-2222-4222-8222-222222222222","result":"pass"}]`,
		"an entry whose criterion_ids is not a list": `[{"test_case_id":"22222222-2222-4222-8222-222222222222","result":"pass","criterion_ids":"none"}]`,
		"a criterion that is not a UUID":             `[{"test_case_id":"22222222-2222-4222-8222-222222222222","result":"pass","criterion_ids":["criterion"]}]`,
	} {
		t.Run(name+" is refused before sending (guard)", func(t *testing.T) {
			received, _, err := recordCapture(t, tests)
			if err == nil || !strings.Contains(err.Error(), "criterion_ids") {
				t.Errorf("error = %v; want a refusal naming criterion_ids", err)
			}
			if received != nil {
				t.Errorf("the refused file was sent: %v", received)
			}
		})
	}

	t.Run("the help describes capture-only entries and runs of the Release's own System", func(t *testing.T) {
		group := newModernizationCommand(nil)
		record, _, err := group.Find([]string{"record"})
		if err != nil {
			t.Fatal(err)
		}
		help := plainText(record.Long)
		for _, phrase := range []string{
			"capture test case",
			"criterion_ids is an empty list",
			"one System",
			"legacy System",
			"the Release's own System",
			"not_a_capture_test_case",
		} {
			if !strings.Contains(help, phrase) {
				t.Errorf("modernization record --help lacks %q", phrase)
			}
		}
		history, _, err := group.Find([]string{"history"})
		if err != nil {
			t.Fatal(err)
		}
		if usage := plainText(history.Flags().Lookup("system-id").Usage); !strings.Contains(usage, "the legacy System or the Release's own System") {
			t.Errorf("modernization history --system-id help = %q; want it to name the legacy System or the Release's own System", usage)
		}
	})

	t.Run("the CLI guide's section on modernization test history describes them", func(t *testing.T) {
		for _, phrase := range []string{
			"capture test case",
			"criterion_ids",
			"empty list",
			"one System",
			"the Release's own System",
			"not_a_capture_test_case",
		} {
			if !strings.Contains(guide, phrase) {
				t.Errorf("the CLI guide's section on modernization test history lacks %q", phrase)
			}
		}
	})

	t.Run("the CLI reference's rows name the capture's refusals and the capture's command", func(t *testing.T) {
		if row := referenceRow("not_a_capture_test_case"); !strings.Contains(row, "modernization record") {
			t.Errorf("the `not_a_capture_test_case` row does not name modernization record: %s", row)
		}
		if row := referenceRow("invalid_verification_endpoint"); row == "" || !strings.Contains(row, "modernization record") {
			t.Errorf("reference-loop.md has no `invalid_verification_endpoint` row naming modernization record: %s", row)
		}
		row := referencePassRow("Modernize a legacy system")
		if !strings.Contains(row, "modernization record") || strings.Contains(row, "evidence record --file (reserve") {
			t.Errorf("the Modernize row does not record the capture with modernization record: %s", row)
		}
	})
}

// Epic 460 (H3), TestCase TC-H3-S12, SR-7 criterion 0: modernization record
// sends commit_details, the commit's first message line cut to 200 code
// points, its author name cut to 255 and its author date, read with git from
// the folder it runs in when that folder holds the commit, else from the
// folder --legacy-checkout names; with neither it sends none and says so. A
// --legacy-checkout that is not a git checkout is refused before anything is
// sent. When the server refuses the capture with
// invalid_modernization_results, which an older server answers for the
// unknown key, the command sends it once more without commit_details and, if
// that is stored, says the server keeps no commit details yet. The new
// server's invalid_commit_details is not sent again.
//
// The repositories are built under pinGit's configuration (the commit chain's
// test), each commit with its own author and author date. Before the Epic the
// flag is parsed and not used: no commit_details is sent, a folder that is no
// checkout is not refused, and a refusal is not sent again, so the
// assertions fail. That invalid_commit_details, and a refusal of a capture
// which sent no details, are not sent again holds before and after (guards).

// h3CaptureServer answers the Release read with no stop commit and each
// capture in turn with the refusal code at its index ("" or none stores it),
// and keeps the captures it received.
type h3CaptureServer struct {
	t        *testing.T
	server   *httptest.Server
	refusals []string
	mu       sync.Mutex
	requests []string
	captures []map[string]any
}

func newH3CaptureServer(t *testing.T, refusals ...string) *h3CaptureServer {
	t.Helper()
	s := &h3CaptureServer{t: t, refusals: refusals}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.server.Close)
	return s
}

func (s *h3CaptureServer) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r.Method+" "+r.URL.Path)
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/compliance/releases/17":
		fmt.Fprint(w, `{"data":{"id":17,"name":"Cutover","status":"active","release_type":"modernization","legacy_system_id":4,"modernization":{"chain_stops":[]}}}`)
	case r.Method == http.MethodPost && r.URL.Path == "/api/compliance/releases/17/modernization/test-runs":
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			s.t.Errorf("the capture body is not JSON: %v", err)
		}
		s.captures = append(s.captures, body)
		if at := len(s.captures) - 1; at < len(s.refusals) && s.refusals[at] != "" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprintf(w, `{"error":%q}`, s.refusals[at])
			return
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"data":{"run":{"id":"run-1","sha":%q,"system_id":4},"results":[]}}`, fmt.Sprint(body["sha"]))
	default:
		s.t.Errorf("unexpected request %s %s", r.Method, r.URL)
		http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
	}
}

func (s *h3CaptureServer) received() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.captures...)
}

func (s *h3CaptureServer) paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

// h3RunRecord runs modernization record for sha from dir with the extra
// flags, and returns stderr and the error.
func h3RunRecord(t *testing.T, server *h3CaptureServer, dir, sha string, flags ...string) (string, error) {
	t.Helper()
	env := nativeCLIEnv(t, server.server, 4)
	file := filepath.Join(t.TempDir(), "capture.json")
	content := fmt.Sprintf(`{"sha":%q,"tests":[{"test_case_id":%q,"result":"pass","criterion_ids":[%q]}]}`, sha, chainTestCaseID, chainCriterionID)
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	command := newModernizationCommand(func() (*cliEnv, error) { return env, nil })
	command.SetArgs(append([]string{"record", "--release-id", "17", "--file", file, "--json"}, flags...))
	var out, errOut bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&errOut)
	err := command.Execute()
	return errOut.String(), err
}

// h3CommitAs commits in r with the given author name and author date.
func h3CommitAs(t *testing.T, r *chainRepo, content, message, author, date string) string {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", author)
	t.Setenv("GIT_AUTHOR_DATE", date)
	return r.commit(t, "f", content, message)
}

func TestH3_ModernizationRecordSendsTheCommitDetails(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is needed to build the test repositories: %v", err)
	}
	pinGit(t)

	// The legacy repository: a commit whose first paragraph runs over two
	// lines, so its first line differs from git's %s, and a commit whose
	// first line and author pass the limits in characters of two and four
	// bytes.
	legacy := newChainRepo(t)
	h3CommitAs(t, legacy, "a\n", "Start", "Ada Legacy", "2026-03-01T10:00:00+02:00")
	scripts := h3CommitAs(t, legacy, "b\n", "Add the capture scripts for order totals\nand the dunning runs\n\nThe body says more.", "Ada Legacy", "2026-03-04T05:06:07+02:00")
	longSubject := strings.Repeat("é𝄞", 125)
	longAuthor := strings.Repeat("Ä𝄞", 150)
	long := h3CommitAs(t, legacy, "c\n", longSubject+"\n\nBody.", longAuthor, "2026-03-05T06:07:08-05:00")
	// The new system's repository holds none of the legacy commits; a third
	// repository holds none either.
	newSystem := buildStraightRepo(t, 2)
	other := buildStraightRepo(t, 1)
	plain := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(plain))

	scriptsDetails := map[string]any{"subject": "Add the capture scripts for order totals", "author": "Ada Legacy", "committed_at": "2026-03-04T05:06:07+02:00"}

	t.Run("from the folder that holds the commit: the first message line, the author and the author date", func(t *testing.T) {
		server := newH3CaptureServer(t)
		stderr, err := h3RunRecord(t, server, legacy.dir, scripts)
		if err != nil {
			t.Fatalf("modernization record: %v\n%s", err, stderr)
		}
		captures := server.received()
		if len(captures) != 1 {
			t.Fatalf("captures = %d; want one", len(captures))
		}
		if got := captures[0]["commit_details"]; !reflect.DeepEqual(got, scriptsDetails) {
			t.Errorf("commit_details = %#v; want %#v", got, scriptsDetails)
		}
		if _, ok := captures[0]["commit_chain"]; !ok {
			t.Errorf("the capture from the folder that holds the commit carries no commit_chain (guard): %v", captures[0])
		}
	})

	t.Run("the first message line is cut to 200 code points and the author to 255", func(t *testing.T) {
		server := newH3CaptureServer(t)
		stderr, err := h3RunRecord(t, server, legacy.dir, long)
		if err != nil {
			t.Fatalf("modernization record: %v\n%s", err, stderr)
		}
		captures := server.received()
		if len(captures) != 1 {
			t.Fatalf("captures = %d; want one", len(captures))
		}
		details, _ := captures[0]["commit_details"].(map[string]any)
		want := map[string]any{
			"subject":      string([]rune(longSubject)[:200]),
			"author":       string([]rune(longAuthor)[:255]),
			"committed_at": "2026-03-05T06:07:08-05:00",
		}
		if !reflect.DeepEqual(details, want) {
			t.Errorf("commit_details = %#v; want the subject cut to 200 and the author to 255 code points: %#v", details, want)
		}
	})

	t.Run("else from the folder --legacy-checkout names, with no commit chain", func(t *testing.T) {
		server := newH3CaptureServer(t)
		stderr, err := h3RunRecord(t, server, newSystem.dir, scripts, "--legacy-checkout", legacy.dir)
		if err != nil {
			t.Fatalf("modernization record: %v\n%s", err, stderr)
		}
		captures := server.received()
		if len(captures) != 1 {
			t.Fatalf("captures = %d; want one", len(captures))
		}
		if got := captures[0]["commit_details"]; !reflect.DeepEqual(got, scriptsDetails) {
			t.Errorf("commit_details = %#v; want %#v", got, scriptsDetails)
		}
		if _, ok := captures[0]["commit_chain"]; ok {
			t.Errorf("a capture of a commit this folder does not hold carries commit_chain (guard)")
		}
		if strings.Contains(stderr, "carries no commit details") {
			t.Errorf("stderr says the run carries no commit details:\n%s", stderr)
		}
	})

	const neither = "the run carries no commit details: neither this folder nor --legacy-checkout holds commit "
	for name, flags := range map[string][]string{
		"without --legacy-checkout":                          nil,
		"with a --legacy-checkout that lacks the commit too": {"--legacy-checkout", other.dir},
	} {
		t.Run("neither folder holds the commit, "+name+": no commit_details, and it says so", func(t *testing.T) {
			server := newH3CaptureServer(t)
			stderr, err := h3RunRecord(t, server, newSystem.dir, scripts, flags...)
			if err != nil {
				t.Fatalf("modernization record: %v\n%s", err, stderr)
			}
			captures := server.received()
			if len(captures) != 1 {
				t.Fatalf("captures = %d; want one", len(captures))
			}
			if _, ok := captures[0]["commit_details"]; ok {
				t.Errorf("the capture carries commit_details: %v", captures[0]["commit_details"])
			}
			if !strings.Contains(stderr, neither+scripts+".\n") {
				t.Errorf("stderr lacks %q:\n%s", neither+scripts+".", stderr)
			}
		})
	}

	t.Run("a --legacy-checkout that is not a git checkout is refused before anything is sent", func(t *testing.T) {
		server := newH3CaptureServer(t)
		_, err := h3RunRecord(t, server, newSystem.dir, scripts, "--legacy-checkout", plain)
		if want := "--legacy-checkout " + plain + " is not a git checkout."; err == nil || err.Error() != want {
			t.Errorf("error = %v; want %q", err, want)
		}
		if paths := server.paths(); len(paths) != 0 {
			t.Errorf("requests = %v; want none", paths)
		}
	})

	t.Run("invalid_modernization_results: sent once more without commit_details, and it says so", func(t *testing.T) {
		server := newH3CaptureServer(t, "invalid_modernization_results")
		stderr, err := h3RunRecord(t, server, newSystem.dir, scripts, "--legacy-checkout", legacy.dir)
		if err != nil {
			t.Fatalf("modernization record: %v\n%s", err, stderr)
		}
		captures := server.received()
		if len(captures) != 2 {
			t.Fatalf("captures = %d; want the capture with commit_details, then once more without", len(captures))
		}
		if _, ok := captures[0]["commit_details"]; !ok {
			t.Errorf("the first capture carries no commit_details: %v", captures[0])
		}
		if _, ok := captures[1]["commit_details"]; ok {
			t.Errorf("the second capture carries commit_details: %v", captures[1])
		}
		first := map[string]any{}
		for key, value := range captures[0] {
			if key != "commit_details" {
				first[key] = value
			}
		}
		if !reflect.DeepEqual(first, captures[1]) {
			t.Errorf("the second capture differs from the first in more than commit_details:\n%v\n%v", captures[0], captures[1])
		}
		if want := "the server keeps no commit details yet; the run was recorded without them.\n"; !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	})

	t.Run("invalid_modernization_results twice: sent once more only, and the refusal stands", func(t *testing.T) {
		server := newH3CaptureServer(t, "invalid_modernization_results", "invalid_modernization_results")
		stderr, err := h3RunRecord(t, server, newSystem.dir, scripts, "--legacy-checkout", legacy.dir)
		if err == nil || !strings.Contains(err.Error(), "invalid_modernization_results") {
			t.Errorf("error = %v; want the server's invalid_modernization_results", err)
		}
		if captures := server.received(); len(captures) != 2 {
			t.Errorf("captures = %d; want two", len(captures))
		}
		if strings.Contains(stderr, "keeps no commit details") {
			t.Errorf("stderr says the run was recorded:\n%s", stderr)
		}
	})

	t.Run("invalid_commit_details is not sent again (guard)", func(t *testing.T) {
		server := newH3CaptureServer(t, "invalid_commit_details")
		stderr, err := h3RunRecord(t, server, newSystem.dir, scripts, "--legacy-checkout", legacy.dir)
		if err == nil || !strings.Contains(err.Error(), "invalid_commit_details") {
			t.Errorf("error = %v; want the server's invalid_commit_details", err)
		}
		if captures := server.received(); len(captures) != 1 {
			t.Errorf("captures = %d; want one", len(captures))
		}
		if strings.Contains(stderr, "keeps no commit details") {
			t.Errorf("stderr says the run was recorded:\n%s", stderr)
		}
	})

	t.Run("a capture that sent no commit_details is not sent again (guard)", func(t *testing.T) {
		server := newH3CaptureServer(t, "invalid_modernization_results")
		_, err := h3RunRecord(t, server, newSystem.dir, scripts)
		if err == nil || !strings.Contains(err.Error(), "invalid_modernization_results") {
			t.Errorf("error = %v; want the server's invalid_modernization_results", err)
		}
		if captures := server.received(); len(captures) != 1 {
			t.Errorf("captures = %d; want one", len(captures))
		}
	})
}

// Epic 460 (H3), DESIGN.md section 10, in TestCase TC-H3-S12's file:
// modernization history shows each passing revision's message, author and
// date, and how many of its recorded results passed, when the server gives
// them (recorded, passed, subject, author, committed_at); a revision whose
// details are null says that its message and author are not recorded and
// gives the day its results were recorded. --json passes the fields through.
//
// Before the Epic the text prints the list as JSON, so the line assertions
// fail; the JSON pass-through holds before and after (guard).
const h3HistoryReply = `{"data":{"passing_revisions":[
 {"revision":"3a4f7f3ef49c2dbd911514344bc141794d3f924d","recorded_at":"2026-11-20T09:30:00.000000Z","recorded":3,"passed":3,
  "subject":"Add the capture scripts for order totals","author":"Ada Legacy","committed_at":"2026-03-04T05:06:07+02:00"},
 {"revision":"0123456789abcdef0123456789abcdef01234567","recorded_at":"2026-11-21T10:00:00.000000Z","recorded":2,"passed":2,
  "subject":null,"author":null,"committed_at":null}],
 "results_recorded":true}}`

func h3RunHistory(t *testing.T, flags ...string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/compliance/modernization/history" || r.URL.Query().Get("system_id") != "41" {
			t.Errorf("history request = %s %s", r.Method, r.URL.String())
		}
		_, _ = w.Write([]byte(h3HistoryReply))
	}))
	t.Cleanup(server.Close)
	command := newModernizationCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	command.SetArgs(append([]string{"history", "--system-id", "41"}, flags...))
	var out bytes.Buffer
	command.SetOut(&out)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestH3_ModernizationHistoryShowsEachPassingRevisionsDetails(t *testing.T) {
	t.Run("text: each passing revision with its results, message, author and date", func(t *testing.T) {
		out := h3RunHistory(t)
		for _, want := range []string{
			"passing_revisions:\n",
			"  3a4f7f3ef49c2dbd911514344bc141794d3f924d  3 of 3 passed · Add the capture scripts for order totals · Ada Legacy · 2026-03-04\n",
			"  0123456789abcdef0123456789abcdef01234567  2 of 2 passed · message and author not recorded · results recorded 2026-11-21\n",
			"results_recorded: true\n",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("modernization history lacks %q:\n%s", want, out)
			}
		}
	})
	t.Run("--json passes the new fields through (guard)", func(t *testing.T) {
		var data map[string]any
		if err := json.Unmarshal([]byte(h3RunHistory(t, "--json")), &data); err != nil {
			t.Fatal(err)
		}
		revisions, _ := data["passing_revisions"].([]any)
		if len(revisions) != 2 {
			t.Fatalf("passing_revisions = %v; want two", data["passing_revisions"])
		}
		first, _ := revisions[0].(map[string]any)
		for key, want := range map[string]any{"recorded": float64(3), "passed": float64(3), "subject": "Add the capture scripts for order totals", "author": "Ada Legacy", "committed_at": "2026-03-04T05:06:07+02:00"} {
			if first[key] != want {
				t.Errorf("passing_revisions[0].%s = %v; want %v", key, first[key], want)
			}
		}
		second, _ := revisions[1].(map[string]any)
		for _, key := range []string{"subject", "author", "committed_at"} {
			if value, present := second[key]; !present || value != nil {
				t.Errorf("passing_revisions[1].%s = %v (present %v); want null", key, value, present)
			}
		}
	})
}
