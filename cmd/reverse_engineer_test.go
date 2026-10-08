package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func reCommand(t *testing.T, server *httptest.Server, input string, args ...string) (string, error) {
	t.Helper()
	command := newReverseEngineerCommand(func() (*factoryEnv, error) { return wsEnv(t, server), nil })
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&out)
	command.SetIn(strings.NewReader(input))
	command.SetArgs(args)
	err := command.Execute()
	return out.String(), err
}

// reCommandStreams runs a reverse-engineer command with the output and error
// streams kept apart.
func reCommandStreams(t *testing.T, server *httptest.Server, input string, args ...string) (string, string, error) {
	t.Helper()
	command := newReverseEngineerCommand(func() (*factoryEnv, error) { return wsEnv(t, server), nil })
	var out, errOut bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&errOut)
	command.SetIn(strings.NewReader(input))
	command.SetArgs(args)
	err := command.Execute()
	return out.String(), errOut.String(), err
}

// receiptServer answers a publish with the given body.
func receiptServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sync/contract" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

// encodedReceipt is the output the publish command prints for a served body.
func encodedReceipt(t *testing.T, body string) string {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	_ = json.NewEncoder(&out).Encode(decoded)
	return out.String()
}

// SR-RDD-ONBOARD-022: a receipt that lists requirements without a context or
// without a test citation gets one warning line on the error stream; the
// receipt on the output stream is printed as before.
func TestSRRDDONBOARD022PublishWarnsAboutMissingContextAndTestCitation(t *testing.T) {
	body := `{"data":{"id":"receipt","result":{"created_requirements":3,"requirement_ids":["SR-A","SR-B","SR-C"],` +
		`"requirements_without_context":["SR-A","SR-B"],"requirements_without_test_citation":["SR-C"]}}}`
	out, errOut, err := reCommandStreams(t, receiptServer(t, body), `{"requirements":[]}`, "publish", "--run", "r", "--group", "g", "--file", "-")
	if err != nil {
		t.Fatalf("publish refused: %v", err)
	}
	if out != encodedReceipt(t, body) {
		t.Fatalf("the receipt on the output stream changed:\n got %q\nwant %q", out, encodedReceipt(t, body))
	}
	want := "warning: the group was published; requirements without a context: 2 (SR-A, SR-B); requirements without a test citation: 1 (SR-C)\n"
	if errOut != want {
		t.Fatalf("the error stream must hold one warning line with both counts:\n got %q\nwant %q", errOut, want)
	}

	body = `{"data":{"id":"receipt","result":{"requirements_without_context":[],"requirements_without_test_citation":["SR-C","SR-D"]}}}`
	_, errOut, err = reCommandStreams(t, receiptServer(t, body), `{"requirements":[]}`, "publish", "--run", "r", "--group", "g", "--file", "-")
	want = "warning: the group was published; requirements without a context: 0; requirements without a test citation: 2 (SR-C, SR-D)\n"
	if err != nil || errOut != want {
		t.Fatalf("one non-empty list still warns with both counts:\n got %q %v\nwant %q", errOut, err, want)
	}
}

// Regression guard for SR-RDD-ONBOARD-022: no warning when both lists are
// empty, when the receipt predates them, or when it holds only one of them;
// the receipt on the output stream is printed as before.
func TestSRRDDONBOARD022PublishPrintsNoWarningWithoutGaps(t *testing.T) {
	for name, body := range map[string]string{
		"both lists empty": `{"data":{"id":"receipt","result":{"requirements_without_context":[],"requirements_without_test_citation":[]}}}`,
		"older receipt":    `{"data":{"id":"receipt","result":{"created_requirements":2,"requirement_ids":["SR-A","SR-B"]}}}`,
		"one list only":    `{"data":{"id":"receipt","result":{"requirements_without_context":["SR-A"]}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			out, errOut, err := reCommandStreams(t, receiptServer(t, body), `{"requirements":[]}`, "publish", "--run", "r", "--group", "g", "--file", "-")
			if err != nil || errOut != "" {
				t.Fatalf("no warning is printed: %q %v", errOut, err)
			}
			if out != encodedReceipt(t, body) {
				t.Fatalf("the receipt on the output stream changed:\n got %q\nwant %q", out, encodedReceipt(t, body))
			}
		})
	}
}

// refusalServer answers every reverse-engineering call with one refusal.
func refusalServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sync/contract" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

// Regression guard for SR-RDD-ONBOARD-020: publish prints every field of a
// refused citation's detail, the reason word included, and a bare refusal
// from an older server as before.
func TestSRRDDONBOARD020PublishPrintsTheRefusedCitation(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"citation position",
			`{"error":{"code":"source_not_authorized","details":{"reason":"source_not_authorized","cause":"capture_of_another_run","requirement":"SR-A","citation_index":2}}}`,
			"reverse-engineering refused (server 422): cause: capture_of_another_run\ncitation_index: 2\nreason: source_not_authorized\nrequirement: SR-A"},
		{"deleted test record",
			`{"error":{"code":"source_not_authorized","details":{"reason":"source_not_authorized","cause":"test_record_deleted","requirement":"SR-B","source_file_id":"6f1c2b9e-0000-4000-8000-000000000003"}}}`,
			"reverse-engineering refused (server 422): cause: test_record_deleted\nreason: source_not_authorized\nrequirement: SR-B\nsource_file_id: 6f1c2b9e-0000-4000-8000-000000000003"},
		{"older server", `{"error":"source_not_authorized"}`, "reverse-engineering refused (server 422): source_not_authorized"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := reCommand(t, refusalServer(t, 422, tc.body), `{"requirements":[]}`, "publish", "--run", "r", "--group", "g", "--file", "-")
			if err == nil || err.Error() != tc.want {
				t.Fatalf("printed refusal:\n got %v\nwant %s", err, tc.want)
			}
		})
	}
}

// Regression guard for SR-RDD-ONBOARD-021: refresh-traces prints the reason
// word and the failing rule, with the entry position where one applies.
func TestSRRDDONBOARD021RefreshTracesPrintsTheFailingRule(t *testing.T) {
	input := `{"corpus_fingerprint":"graph","requirements":[{"kind":"system","external_id":"SR-A","expected_fingerprint":"content"}]}`
	for _, tc := range []struct{ name, body, want string }{
		{"entry rule",
			`{"error":{"code":"invalid_trace_refresh","details":{"reason":"invalid_trace_refresh","rule":"duplicate_external_id","entry_index":1}}}`,
			"reverse-engineering refused (server 422): entry_index: 1\nreason: invalid_trace_refresh\nrule: duplicate_external_id"},
		{"group key",
			`{"error":{"code":"invalid_trace_refresh","details":{"reason":"invalid_trace_refresh","rule":"group_key"}}}`,
			"reverse-engineering refused (server 422): reason: invalid_trace_refresh\nrule: group_key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := reCommand(t, refusalServer(t, 422, tc.body), input, "refresh-traces", "--run", "r", "--group", "g", "--file", "-")
			if err == nil || err.Error() != tc.want {
				t.Fatalf("printed refusal:\n got %v\nwant %s", err, tc.want)
			}
		})
	}
}

func TestSRRDDONBOARD008StructuredPublication(t *testing.T) {
	var received map[string]any
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sync/contract" {
			w.WriteHeader(404)
			return
		}
		if r.Method != "PUT" || r.URL.Path != "/api/v1/systems/4/reverse-engineering/runs/run-id/groups/catalog" {
			t.Errorf("wrong target: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer t" {
			t.Error("actor token missing")
		}
		posts++
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		_ = decoder.Decode(&received)
		_, _ = w.Write([]byte(`{"data":{"id":"receipt","result":{"created_requirements":1}}}`))
	}))
	defer server.Close()
	input := `{"requirements":[{"kind":"system","external_id":"SR-NEW","source_citations":[{"kind":"document","document_id":9007199254740993}],"criteria":[{"statement":"preserve nested fields"}]}]}`
	out, err := reCommand(t, server, input, "publish", "--run", "run-id", "--group", "catalog", "--file", "-")
	if err != nil || posts != 1 || !strings.Contains(out, "receipt") {
		t.Fatalf("publication: %s %v posts=%d", out, err, posts)
	}
	encoded, _ := json.Marshal(received)
	if !strings.Contains(string(encoded), "9007199254740993") || strings.Contains(string(encoded), "release_id") {
		t.Fatalf("payload altered: %s", encoded)
	}
}

func TestSRRDDONBOARD002ReadCapturedDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/systems/4/reverse-engineering/runs/run-id/documents/doc-id" {
			t.Errorf("wrong captured document target: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":{"content":"original document","version":1}}`))
	}))
	defer server.Close()
	out, err := reCommand(t, server, "", "read-document", "--run", "run-id", "--document", "doc-id")
	if err != nil || !strings.Contains(out, "original document") {
		t.Fatalf("document readback: %s %v", out, err)
	}
}

func TestSRRDDONBOARD007StoredCoverage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/systems/4/reverse-engineering/runs/run-id/coverage" {
			t.Errorf("wrong coverage route: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":{"inventory":{"included_files":2},"coverage":{"governed_files":1,"candidate_files":0}}}`))
	}))
	defer server.Close()
	out, err := reCommand(t, server, "", "coverage", "--run", "run-id")
	if err != nil || !strings.Contains(out, "governed_files") {
		t.Fatalf("coverage: %s %v", out, err)
	}
}

// requestLog answers each path with its body and records every request, the
// sync contract read included, as "METHOD /path?query".
func requestLog(t *testing.T, bodies map[string]string, requests *[]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*requests = append(*requests, r.Method+" "+r.URL.RequestURI())
		body, ok := bodies[r.URL.RequestURI()]
		if r.Method != "GET" || !ok {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

// SR-RDD-ONBOARD-029: coverage --system calls the system coverage read of the
// bound system and prints its body; --files asks for the file list; --run
// reads one run as before.
func TestSRRDDONBOARD029CoverageSystemCallsTheSystemRead(t *testing.T) {
	system := `{"data":{"scope":"system_inventory_against_current_system_graph","totals":{"included_files":3}}}`
	listed := `{"data":{"scope":"system_inventory_against_current_system_graph","files":[{"path":"src/a.xml"}]}}`
	run := `{"data":{"scope":"authorized_run_inventory_against_current_system_graph"}}`
	bodies := map[string]string{
		"/api/v1/systems/4/reverse-engineering/coverage":             system,
		"/api/v1/systems/4/reverse-engineering/coverage?files=true":  listed,
		"/api/v1/systems/4/reverse-engineering/runs/run-id/coverage": run,
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
		path string
	}{
		{"system", []string{"coverage", "--system"}, system, "GET /api/v1/systems/4/reverse-engineering/coverage"},
		{"system with files", []string{"coverage", "--system", "--files"}, listed, "GET /api/v1/systems/4/reverse-engineering/coverage?files=true"},
		{"one run", []string{"coverage", "--run", "run-id"}, run, "GET /api/v1/systems/4/reverse-engineering/runs/run-id/coverage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests []string
			out, err := reCommand(t, requestLog(t, bodies, &requests), "", tc.args...)
			if err != nil {
				t.Fatalf("coverage refused: %v", err)
			}
			if out != encodedReceipt(t, tc.want) {
				t.Fatalf("the body is not printed as received:\n got %q\nwant %q", out, encodedReceipt(t, tc.want))
			}
			if !reflect.DeepEqual(requests, []string{tc.path}) {
				t.Fatalf("requests: %v, want exactly [%s]", requests, tc.path)
			}
		})
	}
}

// SR-RDD-ONBOARD-029: coverage with neither --run nor --system, or with both,
// refuses before any call and names the two forms; --files belongs to the
// system form.
func TestSRRDDONBOARD029CoverageRefusesNeitherOrBothForms(t *testing.T) {
	for name, args := range map[string][]string{
		"neither":          {"coverage"},
		"both":             {"coverage", "--run", "run-id", "--system"},
		"files with --run": {"coverage", "--run", "run-id", "--files"},
	} {
		t.Run(name, func(t *testing.T) {
			var requests []string
			_, err := reCommand(t, requestLog(t, map[string]string{}, &requests), "", args...)
			if err == nil {
				t.Fatal("coverage was accepted")
			}
			for _, form := range []string{"--run", "--system"} {
				if !strings.Contains(err.Error(), form) {
					t.Fatalf("the refusal does not name %s: %v", form, err)
				}
			}
			if len(requests) != 0 {
				t.Fatalf("a refused coverage reached the server: %v", requests)
			}
		})
	}
}

// SR-RDD-ONBOARD-031: runs calls the list read of the bound system and prints
// its body as received; --cursor asks for the page after a printed next_cursor.
func TestSRRDDONBOARD031RunsPrintsTheListRead(t *testing.T) {
	first := `{"data":{"runs":[{"id":"r2","key":"area-two"},{"id":"r1","key":"area-one"}],"page_size":50,"next_cursor":"r1"}}`
	next := `{"data":{"runs":[{"id":"r0","key":"area-zero"}],"page_size":50,"next_cursor":null}}`
	bodies := map[string]string{
		"/api/v1/systems/4/reverse-engineering/runs":           first,
		"/api/v1/systems/4/reverse-engineering/runs?cursor=r1": next,
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
		path string
	}{
		{"first page", []string{"runs"}, first, "GET /api/v1/systems/4/reverse-engineering/runs"},
		{"named page", []string{"runs", "--cursor", "r1"}, next, "GET /api/v1/systems/4/reverse-engineering/runs?cursor=r1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests []string
			out, err := reCommand(t, requestLog(t, bodies, &requests), "", tc.args...)
			if err != nil {
				t.Fatalf("runs refused: %v", err)
			}
			if out != encodedReceipt(t, tc.want) {
				t.Fatalf("the body is not printed as received:\n got %q\nwant %q", out, encodedReceipt(t, tc.want))
			}
			if !reflect.DeepEqual(requests, []string{tc.path}) {
				t.Fatalf("requests: %v, want exactly [%s]", requests, tc.path)
			}
		})
	}
}

// SR-RDD-ONBOARD-031: runs --all follows next_cursor until the last page and
// prints one body holding every run in the order served; a cursor the server
// hands back twice stops the command instead of looping.
func TestSRRDDONBOARD031RunsAllFollowsEveryPage(t *testing.T) {
	var requests []string
	server := requestLog(t, map[string]string{
		"/api/v1/systems/4/reverse-engineering/runs":           `{"data":{"runs":[{"id":"r3"},{"id":"r2"}],"page_size":2,"next_cursor":"r2"}}`,
		"/api/v1/systems/4/reverse-engineering/runs?cursor=r2": `{"data":{"runs":[{"id":"r1"}],"page_size":2,"next_cursor":null}}`,
	}, &requests)
	out, err := reCommand(t, server, "", "runs", "--all")
	if err != nil {
		t.Fatalf("runs --all refused: %v", err)
	}
	want := encodedReceipt(t, `{"data":{"runs":[{"id":"r3"},{"id":"r2"},{"id":"r1"}],"page_size":2,"next_cursor":null}}`)
	if out != want {
		t.Fatalf("every page's runs in one body:\n got %q\nwant %q", out, want)
	}
	if !reflect.DeepEqual(requests, []string{
		"GET /api/v1/systems/4/reverse-engineering/runs",
		"GET /api/v1/systems/4/reverse-engineering/runs?cursor=r2",
	}) {
		t.Fatalf("pages requested: %v", requests)
	}

	requests = nil
	looping := requestLog(t, map[string]string{
		"/api/v1/systems/4/reverse-engineering/runs":           `{"data":{"runs":[{"id":"r2"}],"next_cursor":"r2"}}`,
		"/api/v1/systems/4/reverse-engineering/runs?cursor=r2": `{"data":{"runs":[{"id":"r1"}],"next_cursor":"r2"}}`,
	}, &requests)
	if _, err := reCommand(t, looping, "", "runs", "--all"); err == nil || len(requests) != 2 {
		t.Fatalf("a repeated cursor must stop the command after the repeat: %v %v", err, requests)
	}
}

// SR-RDD-ONBOARD-031: runs is listed with the other reverse-engineer commands
// and, as a read, never consults the server's capability contract.
func TestSRRDDONBOARD031RunsIsListedAndNeedsNoCapability(t *testing.T) {
	command, _, err := rootCmd.Find([]string{"reverse-engineer", "runs"})
	if err != nil || command.Name() != "runs" {
		t.Fatalf("runs is not a reverse-engineer command: %v", err)
	}
	var requests []string
	server := requestLog(t, map[string]string{
		"/api/v1/sync/contract":                      `{"data":{"version":1,"capabilities":{"reverse_engineering.authorize":["not-in-this-build"]}}}`,
		"/api/v1/systems/4/reverse-engineering/runs": `{"data":{"runs":[],"page_size":50,"next_cursor":null}}`,
	}, &requests)
	if _, err := reCommand(t, server, "", "runs"); err != nil {
		t.Fatalf("runs refused against a contract this build lacks: %v", err)
	}
	if !reflect.DeepEqual(requests, []string{"GET /api/v1/systems/4/reverse-engineering/runs"}) {
		t.Fatalf("runs consulted more than the list read: %v", requests)
	}
}

func TestSRRDDONBOARD008RefusalsDoNotRetryOrWidenScope(t *testing.T) {
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sync/contract" {
			w.WriteHeader(404)
			return
		}
		writes++
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"error":"stale_corpus"}`))
	}))
	defer server.Close()
	_, err := reCommand(t, server, `{"requirements":[]}`, "publish", "--run", "r", "--group", "g", "--file", "-")
	if err == nil || !strings.Contains(err.Error(), "stale_corpus") || writes != 1 {
		t.Fatalf("refusal: %v writes=%d", err, writes)
	}
	for _, input := range []string{`{"system_id":99}`, `{"tenant_id":99}`, `{"mode":"baseline"} {}`, `[]`} {
		_, err = reCommand(t, server, input, "authorize", "--file", "-")
		if err == nil || writes != 1 {
			t.Fatalf("invalid intent reached server: %s %v", input, err)
		}
	}
}

func TestSRRDDONBOARD008CommandsAndStableCapabilities(t *testing.T) {
	for _, name := range []string{"preflight", "inventory", "authorize", "capture-source", "source-status", "publish", "status", "coverage", "runs", "candidates", "preview", "decide", "read-source"} {
		command, _, err := rootCmd.Find([]string{"reverse-engineer", name})
		if err != nil || command.Name() != name {
			t.Errorf("missing command %s", name)
		}
	}
	for path, want := range map[string]string{
		"/api/v1/systems/54/reverse-engineering/runs":            "reverse_engineering.authorize",
		"/api/v1/systems/54/reverse-engineering/runs/r/groups/g": "reverse_engineering.publish",
		"/api/v1/systems/54/reverse-engineering/runs/r/sources":  "reverse_engineering.capture",
		"/api/v1/systems/54/candidate-sets/decisions":            "candidate_set.decide",
	} {
		if got := writeName(path, nil); got != want {
			t.Errorf("%s: %s != %s", path, got, want)
		}
	}
}

func TestSRRDDONBOARD007ExplicitMultiRepositoryInventoryAndImmutableBundle(t *testing.T) {
	parent := t.TempDir()
	for _, name := range []string{"catalog", "mobile"} {
		root := filepath.Join(parent, name)
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
		for _, filename := range []string{"view.jsp", "model.xml", "transform.xsl", "transform.xslt", ".env"} {
			if err := os.WriteFile(filepath.Join(root, filename), []byte("source"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	manifest, err := buildReverseInventory([]string{"catalog=" + filepath.Join(parent, "catalog"), "mobile=" + filepath.Join(parent, "mobile")})
	if err != nil || len(manifest.Repositories) != 2 {
		t.Fatalf("inventory: %+v %v", manifest, err)
	}
	for _, repo := range manifest.Repositories {
		if len(repo.Files) != 4 || len(repo.Exclusions) != 1 || repo.Revision != "unversioned" || !repo.Dirty {
			t.Fatalf("incomplete inventory: %+v", repo)
		}
	}
	repo := manifest.Repositories[0]
	files, err := reverseSourceBundle(repo, filepath.Join(parent, repo.Key))
	if err != nil || len(files) != 4 {
		t.Fatalf("bundle: %+v %v", files, err)
	}
	if err := os.WriteFile(filepath.Join(parent, repo.Key, "view.jsp"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := reverseSourceBundle(repo, filepath.Join(parent, repo.Key)); err == nil {
		t.Fatal("changed authorized bytes accepted")
	}
	if err := os.Symlink(filepath.Join(parent, "mobile", "model.xml"), filepath.Join(parent, "catalog", "escape.xml")); err != nil {
		t.Fatal(err)
	}
	if _, err := buildReverseInventory([]string{"catalog=" + filepath.Join(parent, "catalog")}); err == nil {
		t.Fatal("symlink entered inventory")
	}
}

func TestSRRDDONBOARD007GitWorktreeAndIgnoredFiles(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git: %s %v", out, err)
		}
	}
	git("init", "-q")
	git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "fixture")
	worktree := filepath.Join(t.TempDir(), "checkout")
	git("worktree", "add", "--detach", worktree)
	if err := os.WriteFile(filepath.Join(worktree, ".gitignore"), []byte("ignored.xml\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"visible.jsp", "ignored.xml"} {
		if err := os.WriteFile(filepath.Join(worktree, path), []byte("bytes"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := buildReverseInventory([]string{"worktree=" + worktree})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(manifest.Repositories[0].Files)
	if !strings.Contains(string(encoded), "visible.jsp") || strings.Contains(string(encoded), "ignored.xml") || len(manifest.Repositories[0].Revision) != 40 {
		t.Fatalf("wrong worktree inventory: %+v", manifest)
	}
}

// SR-RDD-ONBOARD-024: a root that is not a Git repository leaves a .claude
// directory out as one subtree exclusion with the agent-metadata reason.
func TestSRRDDONBOARD024NonGitInventoryLeavesClaudeOut(t *testing.T) {
	root := t.TempDir()
	scopeWrite(t, root, map[string]string{
		"view.jsp":                     "source",
		".claude/settings.json":        "{}",
		".claude/skills/rdd/SKILL.md":  "skill",
		"pkg/.claude/agents/review.md": "agent",
		"pkg/model.xml":                "model",
		"AGENTS.md":                    "managed",
		"CLAUDE.md":                    "managed",
	})
	manifest, err := buildReverseInventory([]string{"plain=" + root})
	if err != nil {
		t.Fatalf("inventory refused: %v", err)
	}
	repo := manifest.Repositories[0]
	if got := scopePaths(repo.Files); !reflect.DeepEqual(got, []string{"AGENTS.md", "CLAUDE.md", "pkg/model.xml", "view.jsp"}) {
		t.Fatalf("files under .claude entered the inventory: %v", got)
	}
	for _, path := range []string{".claude", "pkg/.claude"} {
		if !scopeExcluded(repo.Exclusions, path, "subtree", "agent workspace metadata") {
			t.Fatalf("%s is not one subtree exclusion with the agent-metadata reason: %+v", path, repo.Exclusions)
		}
	}
	for _, exclusion := range repo.Exclusions {
		if strings.HasPrefix(exclusion.Path, ".claude/") || strings.HasPrefix(exclusion.Path, "pkg/.claude/") {
			t.Fatalf("a path inside an excluded .claude directory is listed again: %+v", exclusion)
		}
	}
}

func TestReverseEngineerRefreshTraces(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sync/contract" {
			w.WriteHeader(404)
			return
		}
		calls++
		if r.Method != "PUT" || r.URL.Path != "/api/v1/systems/4/reverse-engineering/runs/captured-run/trace-refreshes/current" {
			t.Errorf("wrong target: %s %s", r.Method, r.URL.Path)
		}
		var input map[string]any
		_ = json.NewDecoder(r.Body).Decode(&input)
		if input["corpus_fingerprint"] != "graph" {
			t.Errorf("missing graph pin: %v", input)
		}
		if r.Header.Get("Authorization") != "Bearer t" {
			t.Error("actor token missing")
		}
		_, _ = w.Write([]byte(`{"data":{"id":"refresh-receipt","result":{"created_links":2}}}`))
	}))
	defer server.Close()
	out, err := reCommand(t, server, `{"corpus_fingerprint":"graph","requirements":[{"kind":"system","external_id":"SR-EXISTING","expected_fingerprint":"content"}]}`,
		"refresh-traces", "--run", "captured-run", "--group", "current", "--file", "-")
	if err != nil || calls != 1 || !strings.Contains(out, "refresh-receipt") {
		t.Fatalf("refresh: %s %v calls=%d", out, err, calls)
	}
	if got := writeName("/api/v1/systems/4/reverse-engineering/runs/captured-run/trace-refreshes/current", nil); got != "reverse_engineering.refresh_traces" {
		t.Fatalf("wrong write capability: %s", got)
	}
}
