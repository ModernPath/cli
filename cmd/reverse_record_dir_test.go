package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// rdStub answers each record request by the title of its first requirement:
// "ok…" records it, any other title names the status to answer with.
type rdStub struct {
	mu   sync.Mutex
	sent []string
}

func (s *rdStub) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Requirements []map[string]any `json:"requirements"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || len(payload.Requirements) == 0 {
			t.Errorf("record --dir sent an unreadable request: %v", err)
			return
		}
		title, _ := payload.Requirements[0]["title"].(string)
		s.mu.Lock()
		s.sent = append(s.sent, title)
		s.mu.Unlock()
		switch {
		case strings.HasPrefix(title, "ok"):
			fmt.Fprintf(w, `{"data":{"requirements":[{"id":%q,"kind":"system_requirement","display_number":7,"title":%q,"work_status":"PROPOSED"}],"traces":[]}}`, payload.Requirements[0]["id"], title)
		case title == "nodata":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case title == "edge429":
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			var status int
			fmt.Sscanf(title, "%d", &status)
			w.WriteHeader(status)
			fmt.Fprintf(w, `{"error":"refused %d"}`, status)
		}
	}
}

func (s *rdStub) titles() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.sent...)
}

func rdFile(t *testing.T, dir, name, id, title string) {
	t.Helper()
	requirement := map[string]any{"kind": "system_requirement", "title": title, "description": "Behavior of " + title}
	if id != "" {
		requirement["id"] = id
	}
	chkWrite(t, filepath.Join(dir, name), map[string]any{"requirements": []any{requirement}, "traces": []any{}})
}

func rdID(n int) string { return fmt.Sprintf("c2000000-0000-4000-8000-%012d", n) }

func rdRun(t *testing.T, env func() (*cliEnv, error), args ...string) (string, error) {
	t.Helper()
	command := newReverseEngineerCommand(env)
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&out)
	command.SetArgs(append([]string{"record"}, args...))
	err := command.Execute()
	return out.String(), err
}

func rdExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func rdNoTemp(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Join(dir, "recorded"))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("a temporary file was left behind: %s", e.Name())
		}
	}
}

func TestReverseRecordDirRecordsKeepsReceiptsAndResumes(t *testing.T) {
	stub := &rdStub{}
	server := httptest.NewServer(stub.handler(t))
	defer server.Close()
	env := func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil }
	dir := t.TempDir()
	rdFile(t, dir, "01-a.json", rdID(1), "ok a")
	rdFile(t, dir, "02-b.json", rdID(2), "422")
	rdFile(t, dir, "03-c.json", rdID(3), "ok c")
	chkWrite(t, filepath.Join(dir, "recorded", "03-c.json.refused"), `{"message":"stale","content":{}}`)

	out, err := rdRun(t, env, "--dir", dir)
	if err == nil {
		t.Fatalf("a run with a refused file exited 0:\n%s", out)
	}
	if got := stub.titles(); strings.Join(got, ",") != "ok a,422,ok c" {
		t.Fatalf("sent %v, want every file once in lexical order", got)
	}
	for _, name := range []string{"01-a.json", "03-c.json"} {
		if !rdExists(filepath.Join(dir, "recorded", name)) || !rdExists(filepath.Join(dir, "recorded", name+".receipt")) {
			t.Fatalf("%s has no copy and receipt in recorded/:\n%s", name, out)
		}
	}
	receipt, _ := os.ReadFile(filepath.Join(dir, "recorded", "01-a.json.receipt"))
	if !strings.Contains(string(receipt), `"display_number"`) || !strings.Contains(string(receipt), rdID(1)) {
		t.Fatalf("the receipt is not the record receipt: %s", receipt)
	}
	if rdExists(filepath.Join(dir, "recorded", "03-c.json.refused")) {
		t.Fatal("a success left a stale refusal note")
	}
	refused, _ := os.ReadFile(filepath.Join(dir, "recorded", "02-b.json.refused"))
	if !strings.Contains(string(refused), "refused 422") || !strings.Contains(string(refused), "Behavior of 422") {
		t.Fatalf("the refusal note lacks the message or the refused content: %s", refused)
	}
	if rdExists(filepath.Join(dir, "recorded", "02-b.json")) {
		t.Fatal("a refused file got a copy")
	}
	rdNoTemp(t, dir)
	for _, word := range []string{"recorded", "refused"} {
		if !strings.Contains(out, word) {
			t.Errorf("the summary does not say %q:\n%s", word, out)
		}
	}

	// A rerun sends nothing: recorded files and the unchanged refused file are
	// skipped, and the summary still counts the refusal left from before.
	before := len(stub.titles())
	out, _ = rdRun(t, env, "--dir", dir)
	if len(stub.titles()) != before || !strings.Contains(out, "skipped") {
		t.Fatalf("a rerun sent %v again:\n%s", stub.titles()[before:], out)
	}
	if !strings.Contains(out, "1 still refused from an earlier run") {
		t.Fatalf("the rerun summary hides the refusal left from before:\n%s", out)
	}
	out, _ = rdRun(t, env, "--dir", dir, "--json")
	if !strings.Contains(out, `"previously_refused": 1`) {
		t.Fatalf("--json does not count the refusal left from before:\n%s", out)
	}
	// --retry-refused sends the unchanged refused file.
	out, _ = rdRun(t, env, "--dir", dir, "--retry-refused")
	if got := stub.titles()[before:]; strings.Join(got, ",") != "422" {
		t.Fatalf("--retry-refused sent %v, want only the refused file:\n%s", got, out)
	}
	// A refused file fixed in place is sent, recorded, and loses its note.
	rdFile(t, dir, "02-b.json", rdID(2), "ok b")
	before = len(stub.titles())
	out, err = rdRun(t, env, "--dir", dir, "--json")
	if err != nil || strings.Join(stub.titles()[before:], ",") != "ok b" {
		t.Fatalf("the fixed refused file was not sent alone: %v %v\n%s", err, stub.titles()[before:], out)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("--json is not one JSON object: %v\n%s", err, out)
	}
	if rdExists(filepath.Join(dir, "recorded", "02-b.json.refused")) {
		t.Fatal("the fixed file kept its refusal note")
	}

	// A recorded file edited afterwards is reported, not sent.
	rdFile(t, dir, "01-a.json", rdID(1), "ok a edited")
	before = len(stub.titles())
	out, err = rdRun(t, env, "--dir", dir)
	if err == nil || len(stub.titles()) != before || !strings.Contains(out, "changed after record") {
		t.Fatalf("an edited recorded file was not reported as changed after record: %v\n%s", err, out)
	}
	// So is a recorded file whose copy was deleted.
	rdFile(t, dir, "01-a.json", rdID(1), "ok a")
	if err := os.Remove(filepath.Join(dir, "recorded", "03-c.json")); err != nil {
		t.Fatal(err)
	}
	out, err = rdRun(t, env, "--dir", dir)
	if err == nil || len(stub.titles()) != before || !strings.Contains(out, "03-c.json") || !strings.Contains(out, "changed after record") {
		t.Fatalf("a receipt without its copy was not reported: %v\n%s", err, out)
	}
}

func TestReverseRecordDirStopsOnRunWideFailures(t *testing.T) {
	restore := limitSleep
	limitSleep = func(time.Duration) {}
	defer func() { limitSleep = restore }()

	cases := []struct{ title, reason string }{
		{"401", "sign-in"}, {"403", "permission"}, {"400", "server-request"}, {"404", "server-request"},
		{"418", "server-request"}, {"500", "server"}, {"502", "server"}, {"nodata", "unknown-outcome"}, {"edge429", "rate-limit"},
	}
	for _, tc := range cases {
		t.Run(tc.title, func(t *testing.T) {
			stub := &rdStub{}
			server := httptest.NewServer(stub.handler(t))
			defer server.Close()
			dir := t.TempDir()
			rdFile(t, dir, "01-a.json", rdID(1), "ok a")
			rdFile(t, dir, "02-b.json", rdID(2), tc.title)
			rdFile(t, dir, "03-c.json", rdID(3), "ok c")
			out, err := rdRun(t, func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil }, "--dir", dir, "--json")
			if err == nil {
				t.Fatalf("a %s exited 0:\n%s", tc.title, out)
			}
			for _, title := range stub.titles() {
				if title == "ok c" {
					t.Fatalf("a %s did not stop the run before the next file:\n%s", tc.title, out)
				}
			}
			if !rdExists(filepath.Join(dir, "recorded", "01-a.json.receipt")) {
				t.Fatal("the earlier file lost its receipt")
			}
			if rdExists(filepath.Join(dir, "recorded", "02-b.json.refused")) {
				t.Fatalf("a %s was noted as a content refusal", tc.title)
			}
			if !strings.Contains(out, `"stopped_reason": "`+tc.reason+`"`) || !strings.Contains(out, "02-b.json") {
				t.Fatalf("a %s did not stop with reason %s naming the file:\n%s", tc.title, tc.reason, out)
			}
			if tc.title == "edge429" && finishCommandRun(err) != 75 {
				t.Fatalf("a rate limit does not exit 75: %T %v", err, err)
			}
		})
	}

	t.Run("connection", func(t *testing.T) {
		stub := &rdStub{}
		server := httptest.NewServer(stub.handler(t))
		env := nativeCLIEnv(t, server, 4)
		server.Close()
		dir := t.TempDir()
		rdFile(t, dir, "01-a.json", rdID(1), "ok a")
		out, err := rdRun(t, func() (*cliEnv, error) { return env, nil }, "--dir", dir, "--json")
		if err == nil || !strings.Contains(out, `"stopped_reason": "connection"`) || rdExists(filepath.Join(dir, "recorded", "01-a.json.refused")) {
			t.Fatalf("a connection failure did not stop cleanly: %v\n%s", err, out)
		}
	})

	t.Run("load", func(t *testing.T) {
		dir := t.TempDir()
		rdFile(t, dir, "01-a.json", rdID(1), "ok a")
		cases := []struct {
			err    error
			reason string
		}{
			{credentialError{err: errors.New("no bearer")}, "sign-in"},
			{&url.Error{Op: "Post", URL: "http://x", Err: errors.New("refused")}, "connection"},
		}
		for _, tc := range cases {
			out, err := rdRun(t, func() (*cliEnv, error) { return nil, tc.err }, "--dir", dir, "--json")
			if err == nil || !strings.Contains(out, `"stopped_reason": "`+tc.reason+`"`) {
				t.Fatalf("a load failure %v did not stop with reason %s: %v\n%s", tc.err, tc.reason, err, out)
			}
		}
	})

	t.Run("binding", func(t *testing.T) {
		stub := &rdStub{}
		server := httptest.NewServer(stub.handler(t))
		defer server.Close()
		dir := t.TempDir()
		rdFile(t, dir, "01-a.json", rdID(1), "ok a")
		// An unbound workspace: the real loader refuses with a binding error.
		cobraWorkspace(t, server, 0)
		out, err := rdRun(t, cliCredentialLoad, "--dir", dir, "--json")
		if err == nil || !strings.Contains(out, `"stopped_reason": "binding"`) || len(stub.titles()) != 0 {
			t.Fatalf("a missing binding did not stop before sending: %v\n%s", err, out)
		}
	})
}

func TestReverseRecordDirLocalChecks(t *testing.T) {
	stub := &rdStub{}
	server := httptest.NewServer(stub.handler(t))
	defer server.Close()
	env := func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil }

	dir := t.TempDir()
	rdFile(t, dir, "01-noid.json", "", "ok no id")
	chkWrite(t, filepath.Join(dir, "02-broken.json"), `{"requirements": [`)
	rdFile(t, dir, "03-c.json", rdID(3), "ok c")
	out, err := rdRun(t, env, "--dir", dir)
	if err == nil || strings.Join(stub.titles(), ",") != "ok c" {
		t.Fatalf("local input errors were sent or did not fail the run: %v %v\n%s", err, stub.titles(), out)
	}
	for _, name := range []string{"01-noid.json", "02-broken.json"} {
		if !rdExists(filepath.Join(dir, "recorded", name+".refused")) {
			t.Errorf("%s has no local refusal note:\n%s", name, out)
		}
	}

	// A 409 is a content refusal like a 422: noted, and the next file is sent.
	conflict := t.TempDir()
	rdFile(t, conflict, "01-x.json", rdID(9), "409")
	rdFile(t, conflict, "02-y.json", rdID(10), "ok y")
	before := len(stub.titles())
	out, err = rdRun(t, env, "--dir", conflict)
	if err == nil || strings.Join(stub.titles()[before:], ",") != "409,ok y" || !rdExists(filepath.Join(conflict, "recorded", "01-x.json.refused")) {
		t.Fatalf("a 409 was not noted as a content refusal: %v %v\n%s", err, stub.titles()[before:], out)
	}

	// A temporary file left by an interrupted write is no receipt: the file is sent.
	leftover := t.TempDir()
	rdFile(t, leftover, "01-z.json", rdID(11), "ok z")
	chkWrite(t, filepath.Join(leftover, "recorded", "01-z.json.receipt.123.tmp"), `{"requirements":[]}`)
	before = len(stub.titles())
	out, err = rdRun(t, env, "--dir", leftover)
	if err != nil || strings.Join(stub.titles()[before:], ",") != "ok z" {
		t.Fatalf("a leftover temporary file was read as a receipt: %v %v\n%s", err, stub.titles()[before:], out)
	}

	// A staged file that cannot be read stops the run as a local read.
	if os.Geteuid() != 0 {
		unreadable := t.TempDir()
		rdFile(t, unreadable, "01-u.json", rdID(12), "ok u")
		if err := os.Chmod(filepath.Join(unreadable, "01-u.json"), 0); err != nil {
			t.Fatal(err)
		}
		out, err = rdRun(t, env, "--dir", unreadable, "--json")
		if err == nil || !strings.Contains(out, `"stopped_reason": "local-read"`) {
			t.Fatalf("an unreadable staged file did not stop as local-read: %v\n%s", err, out)
		}
	}

	out, err = rdRun(t, env, "--dir", t.TempDir())
	if err != nil {
		t.Fatalf("an empty folder failed: %v\n%s", err, out)
	}
	if _, err := rdRun(t, env, "--dir", dir, "--file", "-"); err == nil {
		t.Error("--dir with --file was accepted")
	}
	if _, err := rdRun(t, env); err == nil {
		t.Error("record with neither --file nor --dir was accepted")
	}
}

func TestReverseRecordDirStopsOnAFailedLocalWrite(t *testing.T) {
	stub := &rdStub{}
	server := httptest.NewServer(stub.handler(t))
	defer server.Close()
	env := func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil }

	for _, blocked := range []string{"01-a.json", "01-a.json.receipt"} {
		t.Run(blocked, func(t *testing.T) {
			dir := t.TempDir()
			rdFile(t, dir, "01-a.json", rdID(1), "ok a")
			rdFile(t, dir, "02-b.json", rdID(2), "ok b")
			// A non-empty directory where the copy or receipt goes makes that write fail.
			obstacle := filepath.Join(dir, "recorded", blocked)
			chkWrite(t, filepath.Join(obstacle, "x"), "x")
			before := len(stub.titles())
			out, err := rdRun(t, env, "--dir", dir, "--json")
			if err == nil || !strings.Contains(out, `"stopped_reason": "local-write"`) {
				t.Fatalf("a failed %s write did not stop the run: %v\n%s", blocked, err, out)
			}
			if got := stub.titles()[before:]; strings.Join(got, ",") != "ok a" {
				t.Fatalf("the run went on after a failed local write: %v", got)
			}
			if info, err := os.Stat(filepath.Join(dir, "recorded", "01-a.json.receipt")); err == nil && !info.IsDir() {
				t.Fatal("a failed local write left a receipt")
			}
			rdNoTemp(t, dir)
			if err := os.RemoveAll(obstacle); err != nil {
				t.Fatal(err)
			}
			before = len(stub.titles())
			out, err = rdRun(t, env, "--dir", dir)
			if err != nil || strings.Join(stub.titles()[before:], ",") != "ok a,ok b" {
				t.Fatalf("the rerun did not send the file again and go on: %v %v\n%s", err, stub.titles()[before:], out)
			}
		})
	}
}
