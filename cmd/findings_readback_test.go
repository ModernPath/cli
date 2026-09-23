package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// EPIC-CLI-023 — REQ-CROSS-424 (SCN-FIND-001, SCN-FIND-002): findings list
// narrows to the piece you hold (`--all` for the system), -v prints what was
// written, a non-200 is a refusal with the server's reason, and add refuses a
// severity or category outside the vocabulary before any request.

type findingsReadbackServer struct {
	srv           *httptest.Server
	selection     map[string]any // served under data on GET /sync/work-selection
	selectStatus  int
	selectBody    map[string]any
	rows          []map[string]any
	findingsQuery string
	findingsHits  int
	failFindings  bool
	authorHits    int
}

func newFindingsReadbackServer(t *testing.T) *findingsReadbackServer {
	t.Helper()
	s := &findingsReadbackServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/work-selection", func(w http.ResponseWriter, r *http.Request) {
		if s.selectStatus >= 400 {
			w.WriteHeader(s.selectStatus)
			_ = json.NewEncoder(w).Encode(s.selectBody)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": s.selection})
	})
	mux.HandleFunc("/api/v1/sync/findings", func(w http.ResponseWriter, r *http.Request) {
		s.findingsHits++
		s.findingsQuery = r.URL.RawQuery
		if s.failFindings {
			w.WriteHeader(500)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "findings store unavailable"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"findings": s.rows}})
	})
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		s.authorHits++
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"finding": map[string]any{"external_id": "F-1", "fingerprint": "fp"}}})
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

func readbackFindingRow() map[string]any {
	r := findingRow("F-RB-1", "OPEN", "correctness", "major", "review-ctx-1", "2026-09-21T10:00:00Z", true)
	r["body"] = "the chip claims what it does not hold"
	r["source"] = "cold review R2"
	r["owner"] = "core"
	r["scope_kind"], r["scope_external_id"] = "single_sr", "REQ-X"
	r["aggregate_fingerprint"] = strings.Repeat("a", 64)
	r["disposition_ref"] = "USER:2026-09-21"
	return r
}

func resetFindingsFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		findingsScope, findingsAll, processPiece = "", false, ""
		findingsSeverity, findingsCategory, findingsExternalID = "", "", ""
		verbose = false
	})
	findingsScope, findingsAll, processPiece = "", false, ""
}

func TestREQCROSS424ListNarrowsToTheHeldPiece(t *testing.T) {
	resetFindingsFlags(t)
	s := newFindingsReadbackServer(t)
	s.selection = map[string]any{"current": map[string]any{"scope_kind": "single_sr", "scope_external_id": "REQ-X"}}
	s.rows = []map[string]any{readbackFindingRow()}
	env := &factoryEnv{Root: t.TempDir(), APIURL: s.srv.URL, SystemID: 4, token: "t"}

	out := captureOut(t, func() {
		if err := processFindingsList(env); err != nil {
			t.Errorf("list: %v", err)
		}
	})
	if !strings.Contains(s.findingsQuery, "scope=single_sr%3AREQ-X") && !strings.Contains(s.findingsQuery, "scope=single_sr:REQ-X") {
		t.Fatalf("the list must send the held piece as its scope, sent %q", s.findingsQuery)
	}
	if !strings.Contains(out, "F-RB-1") {
		t.Fatalf("the served row must print:\n%s", out)
	}
	// Not verbose: the body stays out.
	if strings.Contains(out, "the chip claims") {
		t.Fatalf("the body prints only under -v:\n%s", out)
	}
}

func TestREQCROSS424PieceNamesTheHeldPieceWhenSeveral(t *testing.T) {
	resetFindingsFlags(t)
	s := newFindingsReadbackServer(t)
	s.selection = map[string]any{"current": map[string]any{"scope_kind": "epic", "scope_external_id": "EPIC-A"}}
	env := &factoryEnv{Root: t.TempDir(), APIURL: s.srv.URL, SystemID: 4, token: "t"}

	// Several held and none named: the server's refusal surfaces as the remedy.
	s.selectStatus, s.selectBody = 422, map[string]any{
		"error": "you hold several current selections (EPIC-A, EPIC-B) — name one with ?scope=<id>", "pieces": []any{"EPIC-A", "EPIC-B"}}
	err := processFindingsList(env)
	if err == nil || !strings.Contains(err.Error(), "--piece") {
		t.Fatalf("several held pieces must refuse naming --piece, got %v", err)
	}
	if s.findingsHits != 0 {
		t.Fatal("no findings read may happen before the scope is known")
	}

	// Named: the piece's scope is sent.
	s.selectStatus = 0
	processPiece = "EPIC-A"
	if err := processFindingsList(env); err != nil {
		t.Fatalf("list --piece: %v", err)
	}
	if !strings.Contains(s.findingsQuery, "scope=epic%3AEPIC-A") && !strings.Contains(s.findingsQuery, "scope=epic:EPIC-A") {
		t.Fatalf("--piece must resolve to the piece's scope, sent %q", s.findingsQuery)
	}
}

func TestREQCROSS424AllSendsNoScopeAndExplicitScopeStillNarrows(t *testing.T) {
	resetFindingsFlags(t)
	s := newFindingsReadbackServer(t)
	env := &factoryEnv{Root: t.TempDir(), APIURL: s.srv.URL, SystemID: 4, token: "t"}

	findingsAll = true
	if err := processFindingsList(env); err != nil {
		t.Fatalf("list --all: %v", err)
	}
	if strings.Contains(s.findingsQuery, "scope=") {
		t.Fatalf("--all must send no scope, sent %q", s.findingsQuery)
	}

	findingsAll, findingsScope = false, "epic:EPIC-Z"
	if err := processFindingsList(env); err != nil {
		t.Fatalf("list --scope: %v", err)
	}
	if !strings.Contains(s.findingsQuery, "EPIC-Z") {
		t.Fatalf("--scope must still narrow by hand, sent %q", s.findingsQuery)
	}
}

func TestREQCROSS424VerbosePrintsWhatWasWritten(t *testing.T) {
	resetFindingsFlags(t)
	s := newFindingsReadbackServer(t)
	s.rows = []map[string]any{readbackFindingRow()}
	env := &factoryEnv{Root: t.TempDir(), APIURL: s.srv.URL, SystemID: 4, token: "t"}
	findingsAll, verbose = true, true

	out := captureOut(t, func() {
		if err := processFindingsList(env); err != nil {
			t.Errorf("list -v: %v", err)
		}
	})
	for _, want := range []string{
		"body: the chip claims what it does not hold",
		"source: cold review R2",
		"owner: core",
		"scope: single_sr:REQ-X",
		"aggregate: " + strings.Repeat("a", 64),
		"disposition ref: USER:2026-09-21",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("-v must print %q, got:\n%s", want, out)
		}
	}
}

func TestREQCROSS424ANon200IsARefusalWithTheServersReason(t *testing.T) {
	resetFindingsFlags(t)
	s := newFindingsReadbackServer(t)
	s.failFindings = true
	env := &factoryEnv{Root: t.TempDir(), APIURL: s.srv.URL, SystemID: 4, token: "t"}
	findingsAll = true

	err := processFindingsList(env)
	if err == nil || !strings.Contains(err.Error(), "findings store unavailable") {
		t.Fatalf("a 500 must surface the server's reason, got %v", err)
	}
}

func TestREQCROSS424AddRefusesAnUnlistedSeverityOrCategoryLocally(t *testing.T) {
	resetFindingsFlags(t)
	s := newFindingsReadbackServer(t)
	env := &factoryEnv{Root: t.TempDir(), APIURL: s.srv.URL, SystemID: 4, token: "t"}
	findingsScope, findingsExternalID, findingsAggregate = "epic:EPIC-A", "F-9", "agg"
	t.Cleanup(func() { findingsAggregate = "" })

	findingsSeverity, findingsCategory = "blocking", "correctness"
	err := processFindingsAdd(env)
	if err == nil || !strings.Contains(err.Error(), "critical, major, minor, note") {
		t.Fatalf("--severity blocking must be refused naming the four severities, got %v", err)
	}

	findingsSeverity, findingsCategory = "major", "style"
	err = processFindingsAdd(env)
	if err == nil || !strings.Contains(err.Error(), "correctness, security, data_loss, contract, traceability, testability, feasibility, scope, other") {
		t.Fatalf("an unlisted category must be refused naming the nine, got %v", err)
	}
	if s.authorHits != 0 {
		t.Fatalf("a local refusal must post nothing, posted %d", s.authorHits)
	}
}

// PR #618 review (finding 5): --piece naming a piece the caller does not hold
// answers 200 with no current row; the refusal must say so, not "you hold no
// current piece".
func TestREQCROSS424PieceNotHeldIsNamedInTheRefusal(t *testing.T) {
	resetFindingsFlags(t)
	s := newFindingsReadbackServer(t)
	s.selection = map[string]any{"current": nil}
	env := &factoryEnv{Root: t.TempDir(), APIURL: s.srv.URL, SystemID: 4, token: "t"}
	processPiece = "REQ-B"

	err := processFindingsList(env)
	if err == nil || !strings.Contains(err.Error(), "you do not hold REQ-B") {
		t.Fatalf("naming an unheld piece must be refused naming it, got %v", err)
	}
	if s.findingsHits != 0 {
		t.Fatal("no findings read may happen for a piece the caller does not hold")
	}
}
