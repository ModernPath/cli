package cmd

// REQ-CROSS-315 (SR-CLI-0086): `process findings add|list|disposition`. `add`
// posts the typed finding payload carrying the review-context id read from the
// scope directory's `.context` stamp (SR-313), so the cold-review predicate can
// judge independence. RED first: the `process findings` leaves do not exist.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/contract"
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
		// SR-CLI-027-001: a RESOLVED disposition needs the server to advertise
		// finding_resolution; this server records resolution kinds.
		if r.URL.Path == "/api/v1/sync/contract" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"version": 1, "capabilities": map[string]any{"author.finding": []string{"finding_resolution"}}}})
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
	findingsResolution = "packet-edit" // SR-CLI-027-001: RESOLVED names its kind
	findingsDispositionRef = "delivered in REQ-CROSS-319"
	findingsExpectedFingerprint = "fp1"
	t.Cleanup(func() { findingsResolution = "" })

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
	findingsResolution = "packet-edit"
	findingsDispositionRef = "" // no --ref supplied
	findingsExpectedFingerprint = "fp1"
	t.Cleanup(func() { findingsResolution = "" })

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

// listOut runs the list against a buffer — the command writer a terminal run
// passes — so the tests read exactly what the command wrote (SR-CLI-027-004).
func listOut(env *factoryEnv) (string, error) {
	var buf bytes.Buffer
	err := processFindingsList(env, &buf)
	return buf.String(), err
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

	out, err := listOut(env)
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

	out, err := listOut(env)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "round 1") || strings.Contains(out, "audits the document") {
		t.Fatalf("a round with a correctness finding audits the change; no flag:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// SR-CLI-027-001 (EPIC-CLI-027): a RESOLVED disposition names its resolution
// kind. The CLI refuses a RESOLVED without --resolution, a scope kind without
// --ref, a decision kind without a USER: ref, and a kind on any other
// disposition, before any request; a reference-only change on a settled row
// is sent without a disposition; a server that does not advertise
// finding_resolution refuses before any request; the list prints the kind.
// RED first: --resolution is an unknown flag, --disposition is still required,
// no server-capability check exists, this build does not implement the name,
// and the list prints the disposition alone.
// ---------------------------------------------------------------------------

// findingsServer serves the author write, the findings read and the contract
// advertisement: contractStatus 0 answers 404 (a server older than the read),
// otherwise the status with `capabilities` under data.
type findingsServer struct {
	srv            *httptest.Server
	contractStatus int
	capabilities   map[string]any
	rows           []map[string]any
	selection      map[string]any // served as data.current on GET /sync/work-selection
	captured       map[string]any
	authorHits     int
}

func newFindingsServer(t *testing.T) *findingsServer {
	t.Helper()
	s := &findingsServer{contractStatus: 200, capabilities: map[string]any{
		"author.update":  []string{"edit_fingerprint"},
		"author.finding": []string{"finding_resolution"},
	}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/contract", func(w http.ResponseWriter, r *http.Request) {
		if s.contractStatus == 0 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(s.contractStatus)
		if s.contractStatus != 200 {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "contract unavailable"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"version": 1, "capabilities": s.capabilities}})
	})
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		s.authorHits++
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &s.captured)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"finding": map[string]any{"external_id": "F-1", "fingerprint": "fp1"}}})
	})
	mux.HandleFunc("/api/v1/sync/findings", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"findings": s.rows}})
	})
	mux.HandleFunc("/api/v1/sync/work-selection", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"current": s.selection}})
	})
	mux.HandleFunc("/api/v1/sync/delivery-context", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"packet_fingerprint": "agg-live"}})
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

// findingsCommand runs `process findings <args>` through the root command from
// a bound temp workspace, so a flag this build does not know is refused by
// cobra before any handler runs, and the handler otherwise sees exactly what a
// terminal would pass.
func findingsCommand(t *testing.T, apiURL string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	restore := stubListSystemsFn(func(apiURL, token string) ([]api.System, error) { return []api.System{{ID: 4}}, nil })
	t.Cleanup(restore)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".modernpath"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, ".modernpath", "config.json"),
		fmt.Sprintf(`{"api_url":%q,"system_id":4,"system_name":"T","system_slug":"t"}`, apiURL))
	writeFile(t, filepath.Join(root, ".modernpath", "auth.json"), `{"token":"t"}`)
	t.Chdir(root)
	resetTreeFlags(rootCmd)
	rootCmd.SetArgs(append([]string{"process", "findings"}, args...))
	var err error
	out := captureOut(t, func() { err = rootCmd.Execute() })
	rootCmd.SetArgs(nil)
	resetTreeFlags(rootCmd)
	return out, err
}

func TestSRCLI027001FindingResolutionRequiresAKindOnResolved(t *testing.T) {
	s := newFindingsServer(t)
	_, err := findingsCommand(t, s.srv.URL, "disposition", "--id", "F-1", "--disposition", "RESOLVED", "--expected-fingerprint", "fp1")
	if err == nil {
		t.Fatal("RESOLVED without --resolution must be refused")
	}
	for _, want := range []string{"--resolution", "packet-edit", "scope", "decision"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must name %q, got %v", want, err)
		}
	}
	if s.authorHits != 0 {
		t.Fatalf("a local refusal posts nothing, posted %d", s.authorHits)
	}
}

func TestSRCLI027001FindingResolutionScopeNeedsARefAndDecisionAUserRef(t *testing.T) {
	s := newFindingsServer(t)
	_, err := findingsCommand(t, s.srv.URL, "disposition", "--id", "F-1", "--disposition", "RESOLVED", "--resolution", "scope", "--expected-fingerprint", "fp1")
	if err == nil || !strings.Contains(err.Error(), "--ref") {
		t.Fatalf("--resolution scope without --ref must be refused naming --ref, got %v", err)
	}
	_, err = findingsCommand(t, s.srv.URL, "disposition", "--id", "F-1", "--disposition", "RESOLVED", "--resolution", "decision", "--ref", "BACKLOG-TOOL-1", "--expected-fingerprint", "fp1")
	if err == nil || !strings.Contains(err.Error(), "USER:") {
		t.Fatalf("--resolution decision with a non-USER: ref must be refused naming USER:, got %v", err)
	}
	if s.authorHits != 0 {
		t.Fatalf("a local refusal posts nothing, posted %d", s.authorHits)
	}
	_, err = findingsCommand(t, s.srv.URL, "disposition", "--id", "F-1", "--disposition", "RESOLVED", "--resolution", "decision", "--ref", "USER:2026-09-28: accepted", "--expected-fingerprint", "fp1")
	if err != nil {
		t.Fatalf("a decision with a USER: ref is sent: %v", err)
	}
	rec, _ := s.captured["record"].(map[string]any)
	if rec["resolution_kind"] != "decision" || rec["disposition"] != "RESOLVED" {
		t.Fatalf("the kind is posted with the disposition, got %v", rec)
	}
}

func TestSRCLI027001FindingResolutionKindIsRefusedOnAnyOtherDisposition(t *testing.T) {
	s := newFindingsServer(t)
	_, err := findingsCommand(t, s.srv.URL, "disposition", "--id", "F-1", "--disposition", "DEFERRED", "--resolution", "packet-edit", "--expected-fingerprint", "fp1")
	if err == nil || !strings.Contains(err.Error(), "RESOLVED") {
		t.Fatalf("--resolution with DEFERRED must be refused naming RESOLVED, got %v", err)
	}
	if s.authorHits != 0 {
		t.Fatalf("a local refusal posts nothing, posted %d", s.authorHits)
	}
}

func TestSRCLI027001FindingResolutionReferenceOnlyShapeSendsNoDispositionAndNoKind(t *testing.T) {
	s := newFindingsServer(t)
	_, err := findingsCommand(t, s.srv.URL, "disposition", "--id", "F-1", "--ref", "corrected pointer", "--expected-fingerprint", "fp1")
	if err != nil {
		t.Fatalf("a reference-only change on a settled finding is sent without --disposition: %v", err)
	}
	rec, _ := s.captured["record"].(map[string]any)
	if _, ok := rec["disposition"]; ok {
		t.Fatalf("the reference-only shape posts no disposition, got %v", rec)
	}
	if _, ok := rec["resolution_kind"]; ok {
		t.Fatalf("the reference-only shape posts no kind, got %v", rec)
	}
	if rec["disposition_ref"] != "corrected pointer" || rec["expected_fingerprint"] != "fp1" {
		t.Fatalf("the reference and the guard are posted, got %v", rec)
	}
}

func TestSRCLI027001FindingResolutionSentAsPacketEditKind(t *testing.T) {
	s := newFindingsServer(t)
	_, err := findingsCommand(t, s.srv.URL, "disposition", "--id", "F-1", "--disposition", "RESOLVED", "--resolution", "packet-edit", "--ref", "10-recon.md", "--expected-fingerprint", "fp1")
	if err != nil {
		t.Fatalf("disposition: %v", err)
	}
	rec, _ := s.captured["record"].(map[string]any)
	if rec["resolution_kind"] != "packet_edit" {
		t.Fatalf("packet-edit is posted as the stored kind packet_edit, got %v", rec)
	}
}

func TestSRCLI027001FindingResolutionRefusesAServerWithoutTheCapability(t *testing.T) {
	s := newFindingsServer(t)
	s.capabilities = map[string]any{"author.update": []string{"edit_fingerprint"}}
	_, err := findingsCommand(t, s.srv.URL, "disposition", "--id", "F-1", "--disposition", "RESOLVED", "--resolution", "packet-edit", "--expected-fingerprint", "fp1")
	if err == nil || !strings.Contains(err.Error(), "the server does not record resolution kinds") || !strings.Contains(err.Error(), "deploy the server first") {
		t.Fatalf("a server that does not advertise finding_resolution must refuse before any request, got %v", err)
	}
	if s.authorHits != 0 {
		t.Fatalf("nothing is posted to a server without the capability, posted %d", s.authorHits)
	}

	// A 404 contract read is a server older than the advertisement: not advertised.
	s.contractStatus = 0
	_, err = findingsCommand(t, s.srv.URL, "disposition", "--id", "F-1", "--disposition", "RESOLVED", "--resolution", "packet-edit", "--expected-fingerprint", "fp1")
	if err == nil || !strings.Contains(err.Error(), "the server does not record resolution kinds") {
		t.Fatalf("a 404 contract read reads as not advertised, got %v", err)
	}
	if s.authorHits != 0 {
		t.Fatalf("nothing is posted, posted %d", s.authorHits)
	}
}

func TestSRCLI027001FindingResolutionReportsAFailedContractReadNotAMissingCapability(t *testing.T) {
	s := newFindingsServer(t)
	s.contractStatus = 503
	_, err := findingsCommand(t, s.srv.URL, "disposition", "--id", "F-1", "--disposition", "RESOLVED", "--resolution", "packet-edit", "--expected-fingerprint", "fp1")
	if err == nil || !strings.Contains(err.Error(), "503") || strings.Contains(err.Error(), "does not record resolution kinds") {
		t.Fatalf("a non-200 non-404 contract read is a failed read naming the status, got %v", err)
	}
	if s.authorHits != 0 {
		t.Fatalf("nothing is posted after a failed contract read, posted %d", s.authorHits)
	}

	// A transport failure is reported as one.
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	_, err = findingsCommand(t, deadURL, "disposition", "--id", "F-1", "--disposition", "RESOLVED", "--resolution", "packet-edit", "--expected-fingerprint", "fp1")
	if err == nil || !strings.Contains(err.Error(), "contract") || strings.Contains(err.Error(), "does not record resolution kinds") {
		t.Fatalf("a transport failure on the contract read is a failed read, not a missing capability, got %v", err)
	}
}

// A build whose Implemented list lacks finding_resolution is refused on
// `findings add` — the create keyed author.finding — by its own contract
// check, naming the name and the rebuild path (USER:2026-09-27: the wider
// lock-out is accepted). This build implements the name.
func TestSRCLI027001FindingResolutionPreCapabilityBuildIsRefusedOnAdd(t *testing.T) {
	if missing := contract.Missing([]string{"finding_resolution"}); len(missing) != 0 {
		t.Fatalf("this build must implement finding_resolution, missing %v", missing)
	}
	orig := contract.Implemented
	t.Cleanup(func() { contract.Implemented = orig })
	var stripped []string
	for _, n := range orig {
		if n != "finding_resolution" {
			stripped = append(stripped, n)
		}
	}
	contract.Implemented = stripped

	s := newFindingsServer(t)
	_, err := findingsCommand(t, s.srv.URL, "add", "--scope", "epic:EPIC-F", "--id", "F-OLD", "--category", "correctness", "--severity", "major", "--body", "b", "--aggregate", "agg")
	if err == nil || !strings.Contains(err.Error(), "finding_resolution") || !strings.Contains(err.Error(), "rebuild") {
		t.Fatalf("a pre-capability build is refused on findings add naming finding_resolution and the rebuild path, got %v", err)
	}
	if s.authorHits != 0 {
		t.Fatalf("nothing is posted by a refused build, posted %d", s.authorHits)
	}
}

func TestSRCLI027001FindingResolutionListPrintsTheKindAfterResolved(t *testing.T) {
	kinded := findingRow("F-K", "RESOLVED", "correctness", "major", "review-r1", "2026-09-13T10:00:00Z", false)
	kinded["resolution_kind"] = "packet_edit"
	legacy := findingRow("F-L", "RESOLVED", "contract", "minor", "review-r1", "2026-09-13T10:01:00Z", false)
	legacy["resolution_kind"] = nil
	srv := findingsListServer(t, []map[string]any{kinded, legacy})
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}
	findingsScope = "epic:EPIC-F"
	t.Cleanup(func() { findingsScope = "" })

	out, err := listOut(env)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "RESOLVED/packet-edit") {
		t.Fatalf("a RESOLVED row prints its kind after the disposition:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "F-L") && strings.Contains(line, "RESOLVED/") {
			t.Fatalf("a null kind prints RESOLVED alone:\n%s", out)
		}
	}
}

// ---------------------------------------------------------------------------
// SR-CLI-027-002 (EPIC-CLI-027): `findings disposition --widens 'USER:…'`
// states that a packet edit widened a member; the server stores it as the
// widening source. A value that does not begin with USER:, or --widens with a
// resolution other than packet-edit, is refused before any request; -v
// prints the widening source of a widened row.
// RED first: --widens is an unknown flag and -v prints no such line.
// ---------------------------------------------------------------------------

func TestSRCLI027002WidensIsSentWithAPacketEdit(t *testing.T) {
	s := newFindingsServer(t)
	_, err := findingsCommand(t, s.srv.URL, "disposition", "--id", "F-1", "--disposition", "RESOLVED", "--resolution", "packet-edit",
		"--widens", "USER:2026-09-28: the criterion was added on purpose", "--expected-fingerprint", "fp1")
	if err != nil {
		t.Fatalf("disposition --widens: %v", err)
	}
	rec, _ := s.captured["record"].(map[string]any)
	if rec["widening_source"] != "USER:2026-09-28: the criterion was added on purpose" || rec["resolution_kind"] != "packet_edit" {
		t.Fatalf("the widening source is posted with the packet edit, got %v", rec)
	}
}

func TestSRCLI027002WidensRefusesANonUserSourceAndAnotherKind(t *testing.T) {
	s := newFindingsServer(t)
	// F-PR701-01: the local rules run before the contract read — an unreachable
	// server still answers with the --widens refusal.
	s.contractStatus = 503
	_, err := findingsCommand(t, s.srv.URL, "disposition", "--id", "F-1", "--disposition", "RESOLVED", "--resolution", "packet-edit",
		"--widens", "because the reviewer said so", "--expected-fingerprint", "fp1")
	if err == nil || !strings.Contains(err.Error(), "USER:") {
		t.Fatalf("--widens without a USER: source must be refused naming USER:, got %v", err)
	}
	s.contractStatus = 200
	_, err = findingsCommand(t, s.srv.URL, "disposition", "--id", "F-1", "--disposition", "RESOLVED", "--resolution", "scope", "--ref", "EPIC-CLI-028",
		"--widens", "USER:2026-09-28: stray", "--expected-fingerprint", "fp1")
	if err == nil || !strings.Contains(err.Error(), "packet-edit") {
		t.Fatalf("--widens with a scope resolution must be refused naming packet-edit, got %v", err)
	}
	if s.authorHits != 0 {
		t.Fatalf("a local refusal posts nothing, posted %d", s.authorHits)
	}
}

func TestSRCLI027002VerbosePrintsTheWideningSource(t *testing.T) {
	widened := findingRow("F-WD", "RESOLVED", "correctness", "major", "review-r1", "2026-09-13T10:00:00Z", false)
	widened["resolution_kind"] = "packet_edit"
	widened["widening_source"] = "USER:2026-09-28: the criterion was added on purpose"
	srv := findingsListServer(t, []map[string]any{widened})
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}
	findingsScope, verbose = "epic:EPIC-F", true
	t.Cleanup(func() { findingsScope, verbose = "", false })

	out, err := listOut(env)
	if err != nil {
		t.Fatalf("list -v: %v", err)
	}
	if !strings.Contains(out, "widening source: USER:2026-09-28: the criterion was added on purpose") {
		t.Fatalf("-v prints the widening source of a widened row:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// SR-CLI-027-003 (EPIC-CLI-027): `findings add --introduced-by <F-id>` names
// the earlier finding whose resolution introduced the mechanism this one
// faults, refused before any request against a server that does not
// advertise finding_resolution; each round line counts `widened` and `on
// earlier resolutions`, and a round whose material findings all fall on
// earlier resolutions is flagged as the sign the packet was reviewed
// incomplete (PROCESS.md §Entry packet).
// RED first: --introduced-by is an unknown flag, the round line carries
// neither count, and no such flag prints.
// ---------------------------------------------------------------------------

func TestSRCLI027003IntroducedByIsPosted(t *testing.T) {
	s := newFindingsServer(t)
	_, err := findingsCommand(t, s.srv.URL, "add", "--scope", "epic:EPIC-F", "--id", "F-R2-01", "--category", "correctness", "--severity", "major",
		"--body", "the compare added by R1-04 misses a single_sr", "--aggregate", "agg", "--introduced-by", "F-R1-04")
	if err != nil {
		t.Fatalf("add --introduced-by: %v", err)
	}
	rec, _ := s.captured["record"].(map[string]any)
	if rec["introduced_by"] != "F-R1-04" {
		t.Fatalf("the earlier finding is posted as introduced_by, got %v", rec)
	}
}

func TestSRCLI027003IntroducedByRefusesAServerWithoutTheCapability(t *testing.T) {
	s := newFindingsServer(t)
	s.capabilities = map[string]any{"author.update": []string{"edit_fingerprint"}}
	_, err := findingsCommand(t, s.srv.URL, "add", "--scope", "epic:EPIC-F", "--id", "F-R2-01", "--category", "correctness", "--severity", "major",
		"--body", "b", "--aggregate", "agg", "--introduced-by", "F-R1-04")
	if err == nil || !strings.Contains(err.Error(), "deploy the server first") {
		t.Fatalf("--introduced-by against a server without finding_resolution must refuse before any request, got %v", err)
	}
	if s.authorHits != 0 {
		t.Fatalf("nothing is posted — the field take would drop the link silently, posted %d", s.authorHits)
	}
}

func introducedRow(id, disposition, category, severity, ctx, at string, material bool, introducedBy, widening string) map[string]any {
	r := findingRow(id, disposition, category, severity, ctx, at, material)
	if introducedBy != "" {
		r["introduced_by"] = introducedBy
	}
	if widening != "" {
		r["resolution_kind"] = "packet_edit"
		r["widening_source"] = widening
	}
	return r
}

func roundLine(t *testing.T, out string, n int) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, fmt.Sprintf("round %d ·", n)) {
			return line
		}
	}
	t.Fatalf("no round %d line in:\n%s", n, out)
	return ""
}

func TestSRCLI027003RoundLineCountsWidenedAndOnEarlierResolutions(t *testing.T) {
	srv := findingsListServer(t, []map[string]any{
		introducedRow("F-R1-01", "RESOLVED", "correctness", "major", "review-r1", "2026-09-13T10:00:00Z", false, "", "USER:2026-09-13: widened"),
		introducedRow("F-R1-02", "RESOLVED", "contract", "minor", "review-r1", "2026-09-13T10:01:00Z", false, "", ""),
		introducedRow("F-R2-01", "OPEN", "correctness", "major", "review-r2", "2026-09-14T10:00:00Z", true, "F-R1-01", ""),
		introducedRow("F-R2-02", "OPEN", "other", "note", "review-r2", "2026-09-14T10:01:00Z", false, "F-R2-01", ""),
	})
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}
	findingsScope = "epic:EPIC-F"
	t.Cleanup(func() { findingsScope = "" })

	out, err := listOut(env)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if line := roundLine(t, out, 1); !strings.Contains(line, "widened 1") || !strings.Contains(line, "on earlier resolutions 0") {
		t.Fatalf("round 1 counts one widened row and none on earlier resolutions:\n%s", line)
	}
	// F-R2-02 names a finding of its own round: counted in neither.
	if line := roundLine(t, out, 2); !strings.Contains(line, "widened 0") || !strings.Contains(line, "on earlier resolutions 1") {
		t.Fatalf("round 2 counts one row on an earlier resolution and none widened:\n%s", line)
	}
}

func TestSRCLI027003FlagsARoundWhoseMaterialFindingsAllFallOnEarlierResolutions(t *testing.T) {
	srv := findingsListServer(t, []map[string]any{
		introducedRow("F-R1-01", "RESOLVED", "correctness", "major", "review-r1", "2026-09-13T10:00:00Z", false, "", ""),
		introducedRow("F-R2-01", "OPEN", "correctness", "major", "review-r2", "2026-09-14T10:00:00Z", true, "F-R1-01", ""),
		introducedRow("F-R2-02", "OPEN", "contract", "major", "review-r2", "2026-09-14T10:01:00Z", true, "F-R1-01", ""),
		introducedRow("F-R2-N1", "OPEN", "other", "note", "review-r2", "2026-09-14T10:02:00Z", false, "", ""),
	})
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}
	findingsScope = "epic:EPIC-F"
	t.Cleanup(func() { findingsScope = "" })

	out, err := listOut(env)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "round 2 faults mechanisms an earlier round's resolutions introduced") || !strings.Contains(out, "reviewed incomplete") {
		t.Fatalf("a round whose material findings all name an earlier round is flagged:\n%s", out)
	}
}

func TestSRCLI027003DoesNotFlagARoundWhereAMaterialFindingNamesItsOwnRound(t *testing.T) {
	srv := findingsListServer(t, []map[string]any{
		introducedRow("F-R1-01", "RESOLVED", "correctness", "major", "review-r1", "2026-09-13T10:00:00Z", false, "", ""),
		introducedRow("F-R2-01", "OPEN", "correctness", "major", "review-r2", "2026-09-14T10:00:00Z", true, "F-R1-01", ""),
		introducedRow("F-R2-02", "OPEN", "contract", "major", "review-r2", "2026-09-14T10:01:00Z", true, "F-R2-01", ""),
	})
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}
	findingsScope = "epic:EPIC-F"
	t.Cleanup(func() { findingsScope = "" })

	out, err := listOut(env)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if strings.Contains(out, "faults mechanisms an earlier round") {
		t.Fatalf("a material finding on its own round keeps the round unflagged:\n%s", out)
	}
	if line := roundLine(t, out, 2); !strings.Contains(line, "on earlier resolutions 1") {
		t.Fatalf("the count still names the one row on an earlier resolution:\n%s", line)
	}
}

// ---------------------------------------------------------------------------
// SR-CLI-027-004 (EPIC-CLI-027): `findings list --json` writes one object —
// the served rows each with material, independent and round, and one round
// object per scope and round with its counts and flags — through the
// command's writer and nothing else; an empty result is the same object with
// empty arrays; -v adds nothing. `--all` groups rows and rounds by scope with
// a heading per group and round numbering restarting per scope; a single
// scope renders as today with no heading; the shape is the same across
// --all, --scope and the held-piece default.
// RED first: --json is an unknown flag, --all prints one round sequence with
// no heading, and the prints go to os.Stdout.
// ---------------------------------------------------------------------------

// findingsCommandOut runs `process findings <args>` with the root command's
// writer captured separately from os.Stdout, so a test can tell what came
// through the command writer from what bypassed it.
func findingsCommandOut(t *testing.T, apiURL string, args ...string) (cmdOut, stdout string, err error) {
	t.Helper()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	t.Cleanup(func() { rootCmd.SetOut(nil) })
	stdout, err = findingsCommand(t, apiURL, args...)
	return buf.String(), stdout, err
}

func twoScopeRows() []map[string]any {
	a1 := introducedRow("F-A-R1-01", "RESOLVED", "correctness", "major", "review-a1", "2026-09-13T10:00:00Z", false, "", "USER:2026-09-13: widened")
	a2 := introducedRow("F-A-R2-01", "OPEN", "correctness", "major", "review-a2", "2026-09-14T10:00:00Z", true, "F-A-R1-01", "")
	b1 := introducedRow("F-B-R1-01", "OPEN", "traceability", "major", "review-b9", "2026-09-15T10:00:00Z", true, "", "")
	for _, r := range []map[string]any{a1, a2} {
		r["scope_kind"], r["scope_external_id"] = "epic", "EPIC-A"
	}
	b1["scope_kind"], b1["scope_external_id"] = "single_sr", "REQ-B"
	return []map[string]any{b1, a1, a2}
}

func decodeFindingsJSON(t *testing.T, out string) (findings []map[string]any, rounds []map[string]any) {
	t.Helper()
	var obj struct {
		Findings []map[string]any `json:"findings"`
		Rounds   []map[string]any `json:"rounds"`
	}
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("--json must write one JSON object, got %v:\n%s", err, out)
	}
	if obj.Findings == nil || obj.Rounds == nil {
		t.Fatalf("--json must carry findings and rounds as arrays, got:\n%s", out)
	}
	return obj.Findings, obj.Rounds
}

func TestSRCLI027004JSONWritesOneObjectWithFindingsAndRoundsThroughTheCommandWriter(t *testing.T) {
	s := newFindingsServer(t)
	s.rows = twoScopeRows()
	cmdOut, stdout, err := findingsCommandOut(t, s.srv.URL, "list", "--scope", "epic:EPIC-A", "--json")
	if err != nil {
		t.Fatalf("list --json: %v", err)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("--json writes nothing but the object, and through the command writer; os.Stdout got:\n%s", stdout)
	}
	findings, rounds := decodeFindingsJSON(t, cmdOut)
	if len(findings) != 3 {
		t.Fatalf("the served rows are carried as findings, got %d", len(findings))
	}
	for _, f := range findings {
		for _, key := range []string{"external_id", "material", "independent", "round", "scope_kind", "scope_external_id", "content_fingerprint"} {
			if _, ok := f[key]; !ok {
				t.Fatalf("each finding carries %q, got %v", key, f)
			}
		}
	}
	if len(rounds) != 3 {
		t.Fatalf("one round object per scope and round: two for EPIC-A, one for REQ-B, got %v", rounds)
	}
	for _, r := range rounds {
		for _, key := range []string{"scope", "round", "review_context_id", "findings", "open", "material", "notes", "widened", "on_earlier_resolutions", "flags"} {
			if _, ok := r[key]; !ok {
				t.Fatalf("each round carries %q, got %v", key, r)
			}
		}
		if _, ok := r["flags"].([]any); !ok {
			t.Fatalf("flags is an array of strings, got %v", r["flags"])
		}
	}
	// EPIC-A round 2's only material finding names round 1: flagged earlier_resolutions;
	// REQ-B's only material finding is traceability: flagged document.
	byScopeRound := map[string]map[string]any{}
	for _, r := range rounds {
		byScopeRound[fmt.Sprintf("%v#%v", r["scope"], r["round"])] = r
	}
	if r := byScopeRound["epic:EPIC-A#2"]; r == nil || fmt.Sprint(r["flags"]) != "[earlier_resolutions]" || fmt.Sprint(r["on_earlier_resolutions"]) != "1" {
		t.Fatalf("EPIC-A round 2 is flagged earlier_resolutions with one row on an earlier resolution, got %v", r)
	}
	if r := byScopeRound["epic:EPIC-A#1"]; r == nil || fmt.Sprint(r["widened"]) != "1" {
		t.Fatalf("EPIC-A round 1 counts one widened row, got %v", r)
	}
	if r := byScopeRound["single_sr:REQ-B#1"]; r == nil || fmt.Sprint(r["flags"]) != "[document]" {
		t.Fatalf("REQ-B round 1 is flagged document, got %v", r)
	}

	// -v adds nothing under --json.
	verboseOut, _, err := findingsCommandOut(t, s.srv.URL, "list", "--scope", "epic:EPIC-A", "--json", "-v")
	if err != nil {
		t.Fatalf("list --json -v: %v", err)
	}
	if verboseOut != cmdOut {
		t.Fatalf("-v adds nothing under --json:\n%s\nvs\n%s", verboseOut, cmdOut)
	}
}

func TestSRCLI027004JSONEmptyResultIsTheSameObjectWithEmptyArrays(t *testing.T) {
	s := newFindingsServer(t)
	cmdOut, stdout, err := findingsCommandOut(t, s.srv.URL, "list", "--scope", "epic:EPIC-NONE", "--json")
	if err != nil {
		t.Fatalf("list --json (empty): %v", err)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("an empty result prints no human line under --json, got:\n%s", stdout)
	}
	findings, rounds := decodeFindingsJSON(t, cmdOut)
	if len(findings) != 0 || len(rounds) != 0 {
		t.Fatalf("an empty result is the same object with empty arrays, got %s", cmdOut)
	}
}

func TestSRCLI027004AllGroupsRowsAndRoundsPerScopeWithAHeading(t *testing.T) {
	s := newFindingsServer(t)
	s.rows = twoScopeRows()
	cmdOut, _, err := findingsCommandOut(t, s.srv.URL, "list", "--all")
	if err != nil {
		t.Fatalf("list --all: %v", err)
	}
	a := strings.Index(cmdOut, "▸ epic:EPIC-A")
	b := strings.Index(cmdOut, "▸ single_sr:REQ-B")
	if a < 0 || b < 0 {
		t.Fatalf("--all prints a heading per scope group:\n%s", cmdOut)
	}
	// Round numbering restarts per scope: EPIC-A has rounds 1 and 2, REQ-B has round 1 again.
	if strings.Count(cmdOut, "round 1 ·") != 2 || strings.Count(cmdOut, "round 2 ·") != 1 {
		t.Fatalf("round numbering restarts per scope:\n%s", cmdOut)
	}
	// The flags are computed per scope: REQ-B's document audit does not leak onto EPIC-A.
	epicA := cmdOut[a:]
	if b > a {
		epicA = cmdOut[a:b]
	}
	if strings.Contains(epicA, "audits the document") || !strings.Contains(epicA, "faults mechanisms an earlier round") {
		t.Fatalf("EPIC-A carries its own flag and not REQ-B's:\n%s", epicA)
	}

	// A single scope renders with no heading.
	single, _, err := findingsCommandOut(t, s.srv.URL, "list", "--scope", "epic:EPIC-A")
	if err != nil {
		t.Fatalf("list --scope: %v", err)
	}
	if strings.Contains(single, "▸") {
		t.Fatalf("a single-scope listing prints no heading:\n%s", single)
	}
}

func TestSRCLI027004JSONShapeIsTheSameAcrossAllScopeAndTheHeldDefault(t *testing.T) {
	s := newFindingsServer(t)
	s.rows = twoScopeRows()
	s.selection = map[string]any{"scope_kind": "epic", "scope_external_id": "EPIC-A"}
	shapes := map[string]string{}
	for name, args := range map[string][]string{
		"all":   {"list", "--all", "--json"},
		"scope": {"list", "--scope", "single_sr:REQ-B", "--json"},
		"held":  {"list", "--json"},
	} {
		cmdOut, _, err := findingsCommandOut(t, s.srv.URL, args...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		findings, rounds := decodeFindingsJSON(t, cmdOut)
		if len(findings) == 0 || len(rounds) == 0 {
			t.Fatalf("%s serves rows and rounds, got %s", name, cmdOut)
		}
		keys := make([]string, 0, len(rounds[0]))
		for k := range rounds[0] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		shapes[name] = strings.Join(keys, ",")
	}
	if shapes["all"] != shapes["scope"] || shapes["scope"] != shapes["held"] {
		t.Fatalf("the round object has one shape across --all, --scope and the held default: %v", shapes)
	}
}
