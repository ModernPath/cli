package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// authorizeServer answers reverse-engineering calls and keeps what was posted.
type authorizeServer struct {
	status   int
	reason   string
	requests int
	call     string
	body     map[string]any
}

func (s *authorizeServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sync/contract" {
			w.WriteHeader(404)
			return
		}
		s.requests++
		s.call = r.Method + " " + r.URL.Path
		s.body = nil
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		_ = decoder.Decode(&s.body)
		if s.status != 0 {
			w.WriteHeader(s.status)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": s.reason})
			return
		}
		_, _ = w.Write([]byte(`{"data":{"id":"7f3c-recorded-by-server","mode":"baseline"}}`))
	}))
	t.Cleanup(server.Close)
	return server
}

const authorizePreflight = `{"data":{"system_id":4,"requirement_count":0,"recommended_mode":"baseline","modes":["baseline","derived"],
"corpus_fingerprint":"sha256:corpus","documents_truncated":false,"documents":[
{"kind":"system_doc","id":"00000000-0000-0000-0000-000000000001","version":1,"title":"Domain","fingerprint":"aa"},
{"kind":"system_doc","id":"00000000-0000-0000-0000-000000000002","version":2,"title":"Architecture","fingerprint":"bb"}]}}`

func authorizeWrite(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// authorizeFiles saves an inventory and a preflight the way an operator does:
// the output of the two commands, unchanged.
func authorizeFiles(t *testing.T) (string, string, reverseInventory) {
	t.Helper()
	root := t.TempDir()
	for path, body := range map[string]string{"a.txt": "alpha", "lib/b.txt": "beta"} {
		target := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	inventory, err := buildReverseInventory([]string{"catalog=" + root})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(inventory)
	if err != nil {
		t.Fatal(err)
	}
	return authorizeWrite(t, "inventory.json", string(raw)), authorizeWrite(t, "preflight.json", authorizePreflight), inventory
}

// authorizeArgs is a complete assembled call; drop removes one flag with its
// value and set replaces values.
func authorizeArgs(inventory, preflight string, drop string, set map[string]string) []string {
	flags := [][2]string{
		{"--inventory", inventory}, {"--preflight", preflight}, {"--mode", "baseline"},
		{"--source", "USER:test:approved"}, {"--key", "run-1"}, {"--documents", "all"},
	}
	args := []string{"authorize"}
	for _, flag := range flags {
		if flag[0] == drop {
			continue
		}
		if value, ok := set[flag[0]]; ok {
			flag[1] = value
		}
		args = append(args, flag[0], flag[1])
	}
	return args
}

func TestSRRDDONBOARD012AuthorizeAssemblesTheRequest(t *testing.T) {
	inventoryFile, preflightFile, inventory := authorizeFiles(t)
	state := &authorizeServer{}
	out, err := reCommand(t, state.start(t), "", authorizeArgs(inventoryFile, preflightFile, "", nil)...)
	if err != nil {
		t.Fatalf("assembled authorization refused: %v: %s", err, out)
	}
	if state.requests != 1 || state.call != "POST /api/v1/systems/4/reverse-engineering/runs" {
		t.Fatalf("wrong request: %d %s", state.requests, state.call)
	}
	keys := make([]string, 0, len(state.body))
	for key := range state.body {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if !reflect.DeepEqual(keys, []string{"authorization_source", "corpus_fingerprint", "documents", "key", "mode", "repositories"}) {
		t.Fatalf("request does not hold exactly the six fields: %v", keys)
	}
	if state.body["key"] != "run-1" || state.body["mode"] != "baseline" || state.body["authorization_source"] != "USER:test:approved" || state.body["corpus_fingerprint"] != "sha256:corpus" {
		t.Fatalf("request does not carry the flags and the preflight fingerprint: %v", state.body)
	}
	raw, _ := json.Marshal(state.body["repositories"])
	var repositories []reverseRepository
	if err := json.Unmarshal(raw, &repositories); err != nil || !reflect.DeepEqual(repositories, inventory.Repositories) {
		t.Fatalf("repositories are not the inventory file's: %v %s", err, raw)
	}
	var preflight struct {
		Data struct {
			Documents []any `json:"documents"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(strings.NewReader(authorizePreflight))
	decoder.UseNumber()
	if err := decoder.Decode(&preflight); err != nil || len(preflight.Data.Documents) != 2 {
		t.Fatalf("fixture preflight does not list two documents: %v", err)
	}
	if !reflect.DeepEqual(state.body["documents"], any(preflight.Data.Documents)) {
		t.Fatalf("documents are not the preflight file's:\n got %v\nwant %v", state.body["documents"], preflight.Data.Documents)
	}
	if !strings.Contains(out, "7f3c-recorded-by-server") {
		t.Fatalf("the server's response is not printed: %s", out)
	}
}

func TestSRRDDONBOARD012DocumentChoiceIsExplicit(t *testing.T) {
	inventoryFile, preflightFile, _ := authorizeFiles(t)
	state := &authorizeServer{}
	server := state.start(t)
	if _, err := reCommand(t, server, "", authorizeArgs(inventoryFile, preflightFile, "--documents", nil)...); err == nil || !strings.Contains(err.Error(), "--documents is required") || state.requests != 0 {
		t.Fatalf("the document choice must be given: %v requests=%d", err, state.requests)
	}
	count := func(args []string) int {
		t.Helper()
		if out, err := reCommand(t, server, "", args...); err != nil {
			t.Fatalf("authorization refused: %v: %s", err, out)
		}
		list, ok := state.body["documents"].([]any)
		if !ok {
			t.Fatalf("documents is not a list: %v", state.body["documents"])
		}
		return len(list)
	}
	if n := count(authorizeArgs(inventoryFile, preflightFile, "", map[string]string{"--documents": "none"})); n != 0 {
		t.Fatalf("none sent %d documents", n)
	}
	if n := count(authorizeArgs(inventoryFile, preflightFile, "", nil)); n != 2 {
		t.Fatalf("all sent %d documents, want 2", n)
	}
	bare := authorizeWrite(t, "preflight.json", `{"data":{"corpus_fingerprint":"sha256:corpus"}}`)
	if n := count(authorizeArgs(inventoryFile, bare, "", nil)); n != 0 {
		t.Fatalf("a preflight without a document list sent %d documents", n)
	}
	truncated := authorizeWrite(t, "preflight.json", `{"data":{"corpus_fingerprint":"sha256:corpus","documents":[],"documents_truncated":true}}`)
	before := state.requests
	if _, err := reCommand(t, server, "", authorizeArgs(inventoryFile, truncated, "", nil)...); err == nil || !strings.Contains(err.Error(), "truncated") || state.requests != before {
		t.Fatalf("all with a truncated document list must refuse: %v", err)
	} else if !strings.Contains(err.Error(), "--documents none") || !strings.Contains(err.Error(), "--file") {
		t.Fatalf("the truncated refusal must name both ways forward: %v", err)
	}
	if n := count(authorizeArgs(inventoryFile, truncated, "", map[string]string{"--documents": "none"})); n != 0 {
		t.Fatalf("none with a truncated list sent %d documents", n)
	}
}

func TestSRRDDONBOARD012RefusesIncompleteOrInconsistentInput(t *testing.T) {
	inventoryFile, preflightFile, inventory := authorizeFiles(t)
	cut := inventory
	cut.Repositories = []reverseRepository{inventory.Repositories[0]}
	cut.Repositories[0].Files = cut.Repositories[0].Files[:1]
	cutRaw, _ := json.Marshal(cut)
	type row struct {
		name string
		args []string
		want []string
	}
	rows := []row{
		{"mode", authorizeArgs(inventoryFile, preflightFile, "", map[string]string{"--mode": "other"}), []string{"baseline or derived"}},
		{"documents", authorizeArgs(inventoryFile, preflightFile, "", map[string]string{"--documents": "some"}), []string{"all or none"}},
		{"source", authorizeArgs(inventoryFile, preflightFile, "", map[string]string{"--source": "nobody"}), []string{"USER:"}},
		// The server answers a longer source with authorization_source_required,
		// which says the source is missing. The local refusal says what is wrong.
		{"long source", authorizeArgs(inventoryFile, preflightFile, "", map[string]string{"--source": "USER:" + strings.Repeat("s", 251)}), []string{"--source", "255"}},
		// The limit is bytes, as on the server: 131 characters, 257 bytes.
		{"long multibyte source", authorizeArgs(inventoryFile, preflightFile, "", map[string]string{"--source": "USER:" + strings.Repeat("ä", 126)}), []string{"--source", "255"}},
		{"key", authorizeArgs(inventoryFile, preflightFile, "", map[string]string{"--key": strings.Repeat("k", 256)}), []string{"--key", "255"}},
		{"empty inventory", authorizeArgs(authorizeWrite(t, "inventory.json", `{"repositories":[],"file_count":0,"byte_count":0}`), preflightFile, "", nil), []string{"no repository"}},
		{"cut inventory", authorizeArgs(authorizeWrite(t, "inventory.json", string(cutRaw)), preflightFile, "", nil), []string{"catalog", "snapshot digest"}},
		{"no fingerprint", authorizeArgs(inventoryFile, authorizeWrite(t, "preflight.json", `{"data":{"documents":[]}}`), "", nil), []string{"corpus_fingerprint"}},
		{"stdin twice", authorizeArgs("-", "-", "", nil), []string{"stdin"}},
		// A file that is not the command's output says which file and what to save.
		{"swapped files", authorizeArgs(preflightFile, inventoryFile, "", nil), []string{"inventory file", "reverse-engineer inventory", "unchanged"}},
		{"broken preflight", authorizeArgs(inventoryFile, authorizeWrite(t, "preflight.json", "not json"), "", nil), []string{"preflight file", "reverse-engineer preflight", "unchanged"}},
	}
	for _, flag := range []string{"--inventory", "--preflight", "--mode", "--source", "--key"} {
		rows = append(rows, row{"missing " + flag, authorizeArgs(inventoryFile, preflightFile, flag, nil), []string{flag + " is required"}})
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			state := &authorizeServer{}
			_, err := reCommand(t, state.start(t), "", tc.args...)
			if err == nil {
				t.Fatal("the authorization was sent")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("refusal does not say %q: %v", want, err)
				}
			}
			if state.requests != 0 {
				t.Fatalf("a refused authorization reached the server: %d requests", state.requests)
			}
		})
	}
	// The longest source and key the server takes are sent.
	state := &authorizeServer{}
	longest := map[string]string{"--source": "USER:" + strings.Repeat("s", 250), "--key": strings.Repeat("k", 255)}
	if out, err := reCommand(t, state.start(t), "", authorizeArgs(inventoryFile, preflightFile, "", longest)...); err != nil || state.requests != 1 {
		t.Fatalf("a 255-byte source and key must be sent: %v: %s", err, out)
	}
}

func TestSRRDDONBOARD012FileFormCannotBeMixed(t *testing.T) {
	state := &authorizeServer{}
	server := state.start(t)
	for _, flag := range [][2]string{
		{"--inventory", "inventory.json"}, {"--preflight", "preflight.json"}, {"--mode", "baseline"},
		{"--source", "USER:test:approved"}, {"--key", "run-1"}, {"--documents", "all"},
	} {
		if _, err := reCommand(t, server, "", "authorize", "--file", "authorization.json", flag[0], flag[1]); err == nil || !strings.Contains(err.Error(), "either --file or --inventory") {
			t.Fatalf("--file with %s must refuse: %v", flag[0], err)
		}
	}
	if _, err := reCommand(t, server, "", "authorize"); err == nil || !strings.Contains(err.Error(), "--preflight") {
		t.Fatalf("no input must refuse and name both forms: %v", err)
	}
	if state.requests != 0 {
		t.Fatalf("a refused authorization reached the server: %d requests", state.requests)
	}
}

func TestSRRDDONBOARD012HelpDescribesBothForms(t *testing.T) {
	state := &authorizeServer{}
	out, err := reCommand(t, state.start(t), "", "authorize", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"--inventory", "--preflight", "--documents",
		"key, mode, authorization_source, corpus_fingerprint, repositories, documents",
		"stale_corpus", "document_not_authorized", "document_snapshot_too_large",
		// paths, hashes and sizes are sent; file content is not
		"No file content is uploaded",
		// help sorts the flags, so "the flags above" would name only one
		"instead of the other flags",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("help does not mention %q:\n%s", want, out)
		}
	}
}

func TestSRRDDONBOARD012ServerRefusalsSayWhatToDo(t *testing.T) {
	inventoryFile, preflightFile, _ := authorizeFiles(t)
	for _, tc := range []struct {
		status int
		reason string
		next   string
	}{
		{409, "stale_corpus", "save preflight again"},
		{422, "document_not_authorized", "save preflight again"},
		{422, "document_snapshot_too_large", "--documents none"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			state := &authorizeServer{status: tc.status, reason: tc.reason}
			_, err := reCommand(t, state.start(t), "", authorizeArgs(inventoryFile, preflightFile, "", nil)...)
			if err == nil || state.requests != 1 {
				t.Fatalf("the server's refusal was not reached once: %v requests=%d", err, state.requests)
			}
			for _, want := range []string{tc.reason, "nothing was recorded", tc.next} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("refusal does not say %q: %v", want, err)
				}
			}
		})
	}
	// Any other refusal is passed through as the server gave it.
	state := &authorizeServer{status: 409, reason: "idempotency_conflict"}
	_, err := reCommand(t, state.start(t), "", authorizeArgs(inventoryFile, preflightFile, "", nil)...)
	if err == nil || !strings.Contains(err.Error(), "idempotency_conflict") || strings.Contains(err.Error(), "nothing was recorded") {
		t.Fatalf("another refusal must be passed through unchanged: %v", err)
	}
}

// Regression guard: the file form is what it was. It reaches the mode refusal
// that SR-RDD-ONBOARD-008 states, and a server refusal keeps its text.
func TestSRRDDONBOARD012FileFormStillAuthorizes(t *testing.T) {
	complete := `{"key":"run-1","mode":"baseline","authorization_source":"USER:test:approved","corpus_fingerprint":"sha256:corpus","repositories":[{"key":"catalog"}],"documents":[]}`
	state := &authorizeServer{}
	server := state.start(t)
	if out, err := reCommand(t, server, complete, "authorize", "--file", "-"); err != nil || state.requests != 1 {
		t.Fatalf("the file form refused: %v: %s", err, out)
	}
	var want map[string]any
	decoder := json.NewDecoder(strings.NewReader(complete))
	decoder.UseNumber()
	_ = decoder.Decode(&want)
	if !reflect.DeepEqual(state.body, want) {
		t.Fatalf("the file form changed the request:\n got %v\nwant %v", state.body, want)
	}
	for _, input := range []string{`{"key":"run-1"}`, `{"key":"run-1","mode":"other"}`} {
		if _, err := reCommand(t, server, input, "authorize", "--file", "-"); err == nil || !strings.Contains(err.Error(), "baseline or derived") || state.requests != 1 {
			t.Fatalf("a file without an explicit mode must refuse before the server: %s %v", input, err)
		}
	}
	refused := &authorizeServer{status: 409, reason: "stale_corpus"}
	if _, err := reCommand(t, refused.start(t), complete, "authorize", "--file", "-"); err == nil || err.Error() != "reverse-engineering refused (server 409): stale_corpus" {
		t.Fatalf("the file form's server refusal changed: %v", err)
	}
}

// Regression guard: the guidance belongs to authorize. publish returns the
// same reason in the middle of a run, where it would be wrong advice.
func TestSRRDDONBOARD012OtherCommandsKeepTheirRefusals(t *testing.T) {
	refused := &authorizeServer{status: 409, reason: "stale_corpus"}
	_, err := reCommand(t, refused.start(t), `{"requirements":[]}`, "publish", "--run", "r", "--group", "g", "--file", "-")
	if err == nil || err.Error() != "reverse-engineering refused (server 409): stale_corpus" {
		t.Fatalf("publish's server refusal changed: %v", err)
	}
}
