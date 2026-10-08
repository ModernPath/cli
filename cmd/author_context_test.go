package cmd

// SR-RDD-ONBOARD-034: `author context --file` sets the bounded context of
// many requirements with one set_context action, and `author update` sends
// --context-name next to --context.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/modernpath/cli/internal/contract"
)

// setContextServer serves the sync contract, the named item read and the author
// action, and records every request in order.
type setContextServer struct {
	mu           sync.Mutex
	capabilities map[string]any
	items        map[string]map[string]any // id -> {kind, fingerprint}
	status       int
	answer       map[string]any
	requests     []string
	posts        []map[string]any
}

func (s *setContextServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		// Loading the environment checks that the bound system is reachable
		// and goes on when the check cannot answer; it is not this command's read.
		if r.URL.Path == "/api/systems" {
			w.WriteHeader(404)
			return
		}
		s.requests = append(s.requests, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/api/v1/sync/contract":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"version": 1, "capabilities": s.capabilities}})
		case r.URL.Path == "/api/v1/sync/items":
			items := []any{}
			for _, id := range r.URL.Query()["ids[]"] {
				if item, ok := s.items[id]; ok {
					items = append(items, map[string]any{"kind": item["kind"], "gates": []any{},
						"item": map[string]any{"external_id": id, "fingerprint": item["fingerprint"]}})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"items": items}})
		case r.Method == "POST" && r.URL.Path == "/api/v1/sync/author":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.posts = append(s.posts, body)
			if s.status != 0 {
				w.WriteHeader(s.status)
			}
			_ = json.NewEncoder(w).Encode(s.answer)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newSetContextServer() *setContextServer {
	return &setContextServer{
		capabilities: map[string]any{"author.set_context": []string{"set_context"}},
		items: map[string]map[string]any{
			"SR-CAT-001": {"kind": "system", "fingerprint": "fp-sr-1"},
			"UR-CAT-002": {"kind": "user", "fingerprint": "fp-ur-2"},
			"SR-CAT-003": {"kind": "system", "fingerprint": "fp-sr-3"},
		},
		answer: map[string]any{"data": map[string]any{"set_context": map[string]any{"requirements": []any{
			map[string]any{"kind": "SR", "external_id": "SR-CAT-001", "context": "CAT", "context_name": "Catalog", "fingerprint": "fp-sr-1-new"},
			map[string]any{"kind": "UR", "external_id": "UR-CAT-002", "context": "CAT", "context_name": "Catalog", "fingerprint": "fp-ur-2-new"},
			map[string]any{"kind": "SR", "external_id": "SR-CAT-003", "context": "STO", "context_name": "Storefront", "fingerprint": "fp-sr-3-new"},
		}}}},
	}
}

func writeSetContextFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "contexts.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// sentEntries is the entries of the one set_context post, keyed by id, with
// the list-level context and name applied where an entry has none.
func sentSetContextEntries(t *testing.T, s *setContextServer) map[string]map[string]any {
	t.Helper()
	if len(s.posts) != 1 {
		t.Fatalf("want exactly one author request, got %d: %v", len(s.posts), s.posts)
	}
	post := s.posts[0]
	if post["action"] != "set_context" {
		t.Fatalf("want the set_context action, posted %v", post)
	}
	record, _ := post["record"].(map[string]any)
	raw, _ := record["entries"].([]any)
	out := map[string]map[string]any{}
	for _, item := range raw {
		entry, _ := item.(map[string]any)
		for _, key := range []string{"context", "context_name"} {
			if _, ok := entry[key]; !ok {
				entry[key] = record[key]
			}
		}
		out[str(entry, "external_id")] = entry
	}
	return out
}

func TestSRRDDONBOARD034ContextFileSendsOneRequestWithEveryEntry(t *testing.T) {
	s := newSetContextServer()
	cobraWorkspace(t, s.start(t))
	path := writeSetContextFile(t, `[
	  {"external_id": "SR-CAT-001", "context": "CAT", "context_name": "Catalog"},
	  {"external_id": "UR-CAT-002", "context": "CAT", "context_name": "Catalog"},
	  {"external_id": "SR-CAT-003", "context": "STO", "context_name": "Storefront"}
	]`)

	out, err := runRoot(t, "author", "context", "--file", path)
	if err != nil {
		t.Fatalf("author context: %v\n%s", err, out)
	}
	entries := sentSetContextEntries(t, s)
	want := map[string][4]string{
		"SR-CAT-001": {"SR", "fp-sr-1", "CAT", "Catalog"},
		"UR-CAT-002": {"UR", "fp-ur-2", "CAT", "Catalog"},
		"SR-CAT-003": {"SR", "fp-sr-3", "STO", "Storefront"},
	}
	if len(entries) != len(want) {
		t.Fatalf("want every entry in the one request, got %v", entries)
	}
	for id, w := range want {
		e := entries[id]
		got := [4]string{str(e, "kind"), str(e, "expected_fingerprint"), str(e, "context"), str(e, "context_name")}
		if got != w {
			t.Errorf("%s: sent %v, want kind/fingerprint/context/name %v", id, got, w)
		}
	}
	// SR-RDD-ONBOARD-034-AC2: the result names each requirement with its new fingerprint.
	for _, line := range []string{"SR-CAT-001", "fp-sr-1-new", "UR-CAT-002", "fp-ur-2-new", "SR-CAT-003", "fp-sr-3-new"} {
		if !strings.Contains(out, line) {
			t.Errorf("result does not show %q:\n%s", line, out)
		}
	}
}

func TestSRRDDONBOARD034ContextFileOfIDsTakesTheContextFlags(t *testing.T) {
	s := newSetContextServer()
	cobraWorkspace(t, s.start(t))
	path := writeSetContextFile(t, `["SR-CAT-001", "UR-CAT-002"]`)

	out, err := runRoot(t, "author", "context", "--file", path, "--context", "CAT", "--context-name", "Catalog")
	if err != nil {
		t.Fatalf("author context: %v\n%s", err, out)
	}
	entries := sentSetContextEntries(t, s)
	if len(entries) != 2 {
		t.Fatalf("want both ids in the one request, got %v", entries)
	}
	for _, id := range []string{"SR-CAT-001", "UR-CAT-002"} {
		e := entries[id]
		if str(e, "context") != "CAT" || str(e, "context_name") != "Catalog" || str(e, "expected_fingerprint") == "" {
			t.Errorf("%s: the flags' context and name and the read fingerprint must be sent, got %v", id, e)
		}
	}
}

func TestSRRDDONBOARD034ContextRefusalNamesEveryRequirementAsTheServerGaveIt(t *testing.T) {
	s := newSetContextServer()
	s.status = 422
	s.answer = map[string]any{"error": map[string]any{
		"details": map[string]any{
			"SR-CAT-001": []any{"the requirement carries current execution proof, which a new context would void"},
			"UR-CAT-002": []any{"the record's content has moved since that fingerprint"},
		},
		"reason": "SR-CAT-001: …; UR-CAT-002: …",
	}}
	cobraWorkspace(t, s.start(t))
	path := writeSetContextFile(t, `["SR-CAT-001", "UR-CAT-002"]`)

	_, err := runRoot(t, "author", "context", "--file", path, "--context", "CAT", "--context-name", "Catalog")
	if err == nil {
		t.Fatal("a refused list must fail the command")
	}
	for _, want := range []string{"SR-CAT-001", "current execution proof", "UR-CAT-002", "has moved since that fingerprint"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not say %q: %v", want, err)
		}
	}
}

func TestSRRDDONBOARD034ContextRefusesBeforeAnyRequestWithoutTheServerCapability(t *testing.T) {
	s := newSetContextServer()
	s.capabilities = map[string]any{"author.update": []string{"edit_fingerprint"}}
	cobraWorkspace(t, s.start(t))
	path := writeSetContextFile(t, `["SR-CAT-001"]`)

	_, err := runRoot(t, "author", "context", "--file", path, "--context", "CAT", "--context-name", "Catalog")
	if err == nil || !strings.Contains(err.Error(), "set_context") {
		t.Fatalf("want a refusal naming set_context, got %v", err)
	}
	for _, request := range s.requests {
		if request != "GET /api/v1/sync/contract" {
			t.Fatalf("only the contract may be read before the refusal, requests: %v", s.requests)
		}
	}
}

func TestSRRDDONBOARD034AuthorUpdateSendsContextName(t *testing.T) {
	srv, got := authorCapture(t)
	cobraWorkspace(t, srv)
	_, err := runRoot(t, "author", "update", "SR-CAT-001", "--context", "CAT", "--context-name", "Catalog",
		"--expected-fingerprint", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	record, _ := (*got)["record"].(map[string]any)
	if record["context"] != "CAT" || record["context_name"] != "Catalog" {
		t.Fatalf("author update must send --context and --context-name, sent %v", record)
	}
}

// A file that lacks a code or a name, lists an id twice or is not an array is
// refused before the environment is read.
func TestSRRDDONBOARD034ContextFileProblemsRefuseBeforeAnyRequest(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"no name", `["SR-CAT-001"]`, "no context name"},
		{"listed twice", `[{"external_id":"SR-CAT-001","context":"CAT","context_name":"Catalog"},"SR-CAT-001"]`, "more than once"},
		{"unknown field", `[{"external_id":"SR-CAT-001","contxt":"CAT"}]`, "unknown field"},
		{"not an array", `{"SR-CAT-001":"CAT"}`, "JSON array"},
		{"empty", `[]`, "lists no requirement"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSetContextServer()
			cobraWorkspace(t, s.start(t))
			_, err := runRoot(t, "author", "context", "--file", writeSetContextFile(t, tc.body), "--context", "CAT")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want a refusal saying %q, got %v", tc.want, err)
			}
			if len(s.requests) != 0 {
				t.Fatalf("a refused file reached the server: %v", s.requests)
			}
		})
	}
}

// A file of more than 500 requirements, the server's limit for one call, is
// refused before any request; a file of 500 is read.
func TestSRRDDONBOARD034ContextFileOverTheLimitRefusesBeforeAnyRequest(t *testing.T) {
	ids := func(n int) string {
		list := make([]string, n)
		for i := range list {
			list[i] = fmt.Sprintf("%q", fmt.Sprintf("SR-CAT-%04d", i+1))
		}
		return "[" + strings.Join(list, ",") + "]"
	}
	s := newSetContextServer()
	cobraWorkspace(t, s.start(t))
	_, err := runRoot(t, "author", "context", "--file", writeSetContextFile(t, ids(501)), "--context", "CAT", "--context-name", "Catalog")
	if err == nil || !strings.Contains(err.Error(), "lists 501 requirements: at most 500 requirements per call; split the file") {
		t.Fatalf("want a refusal naming the limit, got %v", err)
	}
	if len(s.requests) != 0 {
		t.Fatalf("a file over the limit reached the server: %v", s.requests)
	}

	entries, err := readContextEntries(writeSetContextFile(t, ids(500)), "CAT", "Catalog")
	if err != nil || len(entries) != 500 {
		t.Fatalf("a file of 500 requirements must be read, got %d entries: %v", len(entries), err)
	}
}

// An id the store does not serve as a requirement is named, and nothing is
// written.
func TestSRRDDONBOARD034ContextIdsTheStoreDoesNotServeRefuseBeforeWriting(t *testing.T) {
	s := newSetContextServer()
	s.items["EPIC-CAT"] = map[string]any{"kind": "epic", "fingerprint": "fp-epic"}
	cobraWorkspace(t, s.start(t))
	path := writeSetContextFile(t, `["SR-CAT-001", "SR-CAT-404", "EPIC-CAT"]`)

	_, err := runRoot(t, "author", "context", "--file", path, "--context", "CAT", "--context-name", "Catalog")
	if err == nil || !strings.Contains(err.Error(), "SR-CAT-404: not found") || !strings.Contains(err.Error(), "EPIC-CAT: not a requirement") {
		t.Fatalf("want each id the store does not serve named, got %v", err)
	}
	if len(s.posts) != 0 {
		t.Fatalf("nothing may be written when an id is not served: %v", s.posts)
	}
}

// Pin: the CLI's pinned capability list names set_context under
// author.set_context, and this build implements it.
func TestSRRDDONBOARD034PinnedCapabilityListNamesSetContext(t *testing.T) {
	var pinned struct {
		Capabilities map[string][]string `json:"capabilities"`
	}
	if err := json.Unmarshal(contract.PinnedCapabilities, &pinned); err != nil {
		t.Fatal(err)
	}
	if got := pinned.Capabilities["author.set_context"]; len(got) != 1 || got[0] != "set_context" {
		t.Fatalf("author.set_context must be pinned as [set_context], got %v", got)
	}
	if missing := contract.Missing([]string{"set_context"}); len(missing) != 0 {
		t.Fatalf("this build must implement set_context, missing %v", missing)
	}
}
