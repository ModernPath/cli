package cmd

// REQ-CROSS-315 (SR-CLI-0086): `process findings add|list|disposition`. `add`
// posts the typed finding payload carrying the review-context id read from the
// scope directory's `.context` stamp (SR-313), so the cold-review predicate can
// judge independence. RED first: the `process findings` leaves do not exist.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func findingsFixture(t *testing.T, captured *map[string]any) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sync/author" {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, captured)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"finding": map[string]any{
					"external_id": "F-1", "disposition": "OPEN", "fingerprint": "fp1"}},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	root := t.TempDir()
	dir := filepath.Join(root, ".modernpath/working-set/EPIC-F")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".context"),
		[]byte("# working-set context\n\nmode: review\ncontext_id: review-abc123\nscope: epic:EPIC-F\npulled_at: now\n"),
		0o644)
	return srv, root
}

func TestREQCROSS315ProcessFindingsAddPostsTypedPayloadWithReviewContext(t *testing.T) {
	var captured map[string]any
	srv, root := findingsFixture(t, &captured)

	env := &factoryEnv{Root: root, APIURL: srv.URL, SystemID: 4, token: "t"}
	findingsScope = "epic:EPIC-F"
	findingsExternalID = "F-1"
	findingsSeverity = "major"
	findingsCategory = "correctness"
	findingsBody = "the cold-review gate has no computable predicate"
	findingsSource = "the reviewer"
	findingsOwner = "core"
	findingsAggregate = "agg-x"

	if err := processFindingsAdd(env); err != nil {
		t.Fatalf("process findings add failed: %v", err)
	}

	if captured["action"] != "create" {
		t.Fatalf("expected action create, got %v", captured["action"])
	}
	rec, _ := captured["record"].(map[string]any)
	if rec["kind"] != "finding" {
		t.Fatalf("expected kind finding, got %v", rec["kind"])
	}
	if rec["review_context_id"] != "review-abc123" {
		t.Fatalf("review_context_id must come from the directory .context, got %v", rec["review_context_id"])
	}
	if rec["scope_kind"] != "epic" || rec["scope_external_id"] != "EPIC-F" {
		t.Fatalf("scope not parsed into kind+id: %v", rec)
	}
	if rec["category"] != "correctness" || rec["severity"] != "major" {
		t.Fatalf("typed fields not carried: %v", rec)
	}
}

func TestREQCROSS315ProcessFindingsDispositionPostsUpdate(t *testing.T) {
	var captured map[string]any
	srv, root := findingsFixture(t, &captured)

	env := &factoryEnv{Root: root, APIURL: srv.URL, SystemID: 4, token: "t"}
	findingsExternalID = "F-1"
	findingsDisposition = "RESOLVED"
	findingsDispositionRef = "delivered in REQ-CROSS-319"
	findingsExpectedFingerprint = "fp1"

	if err := processFindingsDisposition(env); err != nil {
		t.Fatalf("process findings disposition failed: %v", err)
	}

	if captured["action"] != "update" {
		t.Fatalf("expected action update, got %v", captured["action"])
	}
	rec, _ := captured["record"].(map[string]any)
	if rec["kind"] != "finding" || rec["disposition"] != "RESOLVED" || rec["disposition_ref"] != "delivered in REQ-CROSS-319" {
		t.Fatalf("disposition payload wrong: %v", rec)
	}
}

// #11: dispositioning without --ref must NOT send disposition_ref at all — a
// present "" key overwrites a stored reference server-side.
func TestREQCROSS315ProcessFindingsDispositionOmitsEmptyRef(t *testing.T) {
	var captured map[string]any
	srv, root := findingsFixture(t, &captured)

	env := &factoryEnv{Root: root, APIURL: srv.URL, SystemID: 4, token: "t"}
	findingsExternalID = "F-1"
	findingsDisposition = "RESOLVED"
	findingsDispositionRef = "" // no --ref supplied
	findingsExpectedFingerprint = "fp1"

	if err := processFindingsDisposition(env); err != nil {
		t.Fatalf("process findings disposition failed: %v", err)
	}

	rec, _ := captured["record"].(map[string]any)
	if _, ok := rec["disposition_ref"]; ok {
		t.Fatalf("disposition_ref must be omitted when no --ref is given, got %v", rec["disposition_ref"])
	}
}

// The fingerprint guard is mandatory: a disposition without --expected-fingerprint
// must be refused BEFORE any POST, so two stale-read callers cannot clobber.
func TestREQCROSS315ProcessFindingsDispositionRequiresFingerprint(t *testing.T) {
	var captured map[string]any
	srv, root := findingsFixture(t, &captured)

	env := &factoryEnv{Root: root, APIURL: srv.URL, SystemID: 4, token: "t"}
	findingsExternalID = "F-1"
	findingsDisposition = "RESOLVED"
	findingsDispositionRef = ""
	findingsExpectedFingerprint = ""

	if err := processFindingsDisposition(env); err == nil {
		t.Fatal("a disposition without --expected-fingerprint must be refused")
	}
	if captured != nil {
		t.Fatalf("a refused disposition must not POST, but captured %v", captured)
	}
}

// External PR #299 review (#8): a review context must be stamped only when the
// scope directory was pulled --for-review; an ordinary authoring pull must carry
// no review context, or its findings could later count as independent.
func TestFindingsAddOmitsReviewContextForAuthoringPull(t *testing.T) {
	var captured map[string]any
	srv, root := findingsFixture(t, &captured)
	dir := filepath.Join(root, ".modernpath/working-set/EPIC-F")
	_ = os.WriteFile(filepath.Join(dir, ".context"),
		[]byte("# working-set context\n\nmode: authoring\ncontext_id: author-xyz\nscope: epic:EPIC-F\npulled_at: now\n"), 0o644)

	env := &factoryEnv{Root: root, APIURL: srv.URL, SystemID: 4, token: "t"}
	findingsScope = "epic:EPIC-F"
	findingsExternalID = "F-8"
	findingsSeverity = "major"
	findingsCategory = "correctness"
	findingsBody = "b"
	findingsAggregate = "agg-x"

	if err := processFindingsAdd(env); err != nil {
		t.Fatalf("add failed: %v", err)
	}
	rec, _ := captured["record"].(map[string]any)
	if rc := rec["review_context_id"]; rc != "" && rc != nil {
		t.Fatalf("an authoring-mode pull must not stamp a review context, got %v", rc)
	}
}

// When the aggregate cannot be derived — the delivery-context read fails and
// --aggregate was not given — the add must refuse rather than post an unpinned
// finding (which would still count material and block the scope).
func TestFindingsAddRefusesWhenAggregateCannotBeDerived(t *testing.T) {
	var posted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/sync/author":
			posted = true
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"finding": map[string]any{"external_id": "F-NA", "fingerprint": "fp"}}})
		case "/api/v1/sync/delivery-context":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".modernpath/working-set/EPIC-F"), 0o755)

	env := &factoryEnv{Root: root, APIURL: srv.URL, SystemID: 4, token: "t"}
	findingsScope = "epic:EPIC-F"
	findingsExternalID = "F-NA"
	findingsSeverity = "major"
	findingsCategory = "correctness"
	findingsBody = "b"
	findingsAggregate = ""

	if err := processFindingsAdd(env); err == nil {
		t.Fatal("add must refuse when the aggregate cannot be derived and --aggregate is absent")
	}
	if posted {
		t.Fatal("a refused add must not POST an unpinned finding")
	}
}

// External PR #299 review (#23): a finding must carry its packet-revision
// provenance. When --aggregate is omitted, derive the current aggregate from the
// delivery-context rather than creating a finding with no provenance.
func TestFindingsAddDerivesAggregateFromDeliveryContext(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/sync/author":
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &captured)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"finding": map[string]any{"external_id": "F-23", "fingerprint": "fp"}}})
		case "/api/v1/sync/delivery-context":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"packet_fingerprint": "agg-live"}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	root := t.TempDir()
	dir := filepath.Join(root, ".modernpath/working-set/EPIC-F")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".context"),
		[]byte("mode: review\ncontext_id: review-abc\n"), 0o644)

	env := &factoryEnv{Root: root, APIURL: srv.URL, SystemID: 4, token: "t"}
	findingsScope = "epic:EPIC-F"
	findingsExternalID = "F-23"
	findingsSeverity = "major"
	findingsCategory = "correctness"
	findingsBody = "b"
	findingsAggregate = ""

	if err := processFindingsAdd(env); err != nil {
		t.Fatalf("add failed: %v", err)
	}
	rec, _ := captured["record"].(map[string]any)
	if rec["aggregate_fingerprint"] != "agg-live" {
		t.Fatalf("aggregate must be derived from delivery-context, got %v", rec["aggregate_fingerprint"])
	}
}

// REQ-CROSS-385 (EPIC-CLI-018): `findings add` requires an explicit --category
// and --severity — never the most blocking pair by default — and names the
// vocabularies in its refusal; nothing is posted.
func TestREQCROSS385FindingsAddRequiresExplicitCategoryAndSeverity(t *testing.T) {
	var captured map[string]any
	srv, root := findingsFixture(t, &captured)
	env := &factoryEnv{Root: root, APIURL: srv.URL, SystemID: 4, token: "t"}
	findingsScope = "epic:EPIC-F"
	findingsExternalID = "F-2"
	findingsBody = "b"
	findingsSource = "s"
	findingsAggregate = "agg-x"
	findingsCategory = ""
	findingsSeverity = ""
	defer func() { findingsCategory, findingsSeverity = "", "" }()

	err := processFindingsAdd(env)
	if err == nil {
		t.Fatal("add without --category/--severity must be refused")
	}
	for _, want := range []string{"--category", "--severity", "correctness", "traceability", "feasibility", "note"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must name %q, got %v", want, err)
		}
	}
	if len(captured) != 0 {
		t.Fatalf("nothing may be posted on a refusal, got %v", captured)
	}
}

func findingsListServer(t *testing.T, rows []map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/sync/findings" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"findings": rows}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func findingRow(id, disposition, category, severity, ctx, at string, material bool) map[string]any {
	return map[string]any{
		"external_id": id, "disposition": disposition, "category": category, "severity": severity,
		"material": material, "independent": true, "review_context_id": ctx, "inserted_at": at,
		"content_fingerprint": "fp-" + id,
	}
}

// `findings list` reports counts per review round (a round is one review
// context, ordered by first inserted_at) and flags a round whose material
// findings are all traceability — the mechanical sign that the review is
// auditing the document rather than the change.
func TestREQCROSS385FindingsListReportsRoundsAndFlagsADocumentAudit(t *testing.T) {
	srv := findingsListServer(t, []map[string]any{
		findingRow("N-R2-01", "OPEN", "other", "note", "review-r2", "2026-09-13T11:05:00Z", false),
		findingRow("F-R1-01", "RESOLVED", "correctness", "major", "review-r1", "2026-09-13T10:00:00Z", false),
		findingRow("F-R2-01", "OPEN", "traceability", "major", "review-r2", "2026-09-13T11:00:00Z", true),
		findingRow("F-R1-02", "OPEN", "contract", "minor", "review-r1", "2026-09-13T10:01:00Z", true),
	})
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}
	findingsScope = "epic:EPIC-F"

	var err error
	out := captureOut(t, func() { err = processFindingsList(env) })
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{
		"round 1", "review-r1", "round 2", "review-r2",
		"audits the document",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("list must report per-round counts and the convergence flag (%q):\n%s", want, out)
		}
	}
	if strings.Index(out, "review-r1") > strings.Index(out, "review-r2") {
		t.Fatalf("rounds must be ordered by first inserted_at:\n%s", out)
	}
}

func TestREQCROSS385FindingsListDoesNotFlagARoundAboutTheChange(t *testing.T) {
	srv := findingsListServer(t, []map[string]any{
		findingRow("F-R1-01", "OPEN", "correctness", "major", "review-r1", "2026-09-13T10:00:00Z", true),
		findingRow("F-R1-02", "OPEN", "traceability", "major", "review-r1", "2026-09-13T10:01:00Z", true),
	})
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}
	findingsScope = "epic:EPIC-F"

	var err error
	out := captureOut(t, func() { err = processFindingsList(env) })
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "round 1") || strings.Contains(out, "audits the document") {
		t.Fatalf("a round with a correctness finding audits the change; no flag:\n%s", out)
	}
}
