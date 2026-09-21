// SR-CROSS-324: `requirements-corpus --json` emits the four-field corpus JSON to
// stdout (SR+UR merged, DERIVED excluded) with warnings on stderr.
package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// rcServer serves the requirements envelope the store returns: system
// requirements under data.requirements and user requirements under
// data.user_requirements (sync_api_controller requirements/2). The real endpoint
// already excludes DERIVED by default, but the command must not rely on that
// alone, so this fixture includes one DERIVED SR to prove the command drops it.
func rcServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"requirements": []any{
				map[string]any{
					"external_id": "REQ-SMK-002",
					"work_status": "DONE",
					"detail_md":   "- **Status:** DONE\n- **Tests:** `smk_test.exs`\n",
				},
				// no detail_md -> a data-quality warning, which must land on stderr
				map[string]any{
					"external_id": "REQ-SMK-003",
					"work_status": "TODO",
				},
				// DERIVED -> excluded by default
				map[string]any{
					"external_id": "REQ-SMK-009",
					"work_status": "DERIVED",
					"detail_md":   "- **Status:** DERIVED\n",
				},
			},
			"user_requirements": []any{
				map[string]any{
					"external_id": "UR-SMK-001",
					"work_status": "IN_REVIEW",
					"detail_md":   "- **Status:** IN_REVIEW\n- **Tests:** `App.smk.test.tsx`\n",
				},
			},
		}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRequirementsCorpusEmitsTheFourFieldsAsJSONArray(t *testing.T) {
	srv := rcServer(t)
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}

	var out, errBuf bytes.Buffer
	if err := emitRequirementsCorpus(&out, &errBuf, env); err != nil {
		t.Fatalf("emitRequirementsCorpus: %v", err)
	}

	// --json owns stdout (REQ-CROSS-121): stdout must be exactly a JSON array, so
	// a gate's JSON.parse cannot throw. This unmarshal fails if any warning leaked.
	var got []corpusEntry
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not a JSON array: %v\nstdout:\n%s", err, out.String())
	}

	byID := map[string]corpusEntry{}
	for _, e := range got {
		byID[e.ExternalID] = e
	}

	// SR and UR merged into one array.
	if _, ok := byID["REQ-SMK-002"]; !ok {
		t.Errorf("system requirement REQ-SMK-002 missing from the corpus: %+v", got)
	}
	if _, ok := byID["UR-SMK-001"]; !ok {
		t.Errorf("user requirement UR-SMK-001 missing from the corpus: %+v", got)
	}
	// DERIVED excluded by default.
	if _, ok := byID["REQ-SMK-009"]; ok {
		t.Errorf("DERIVED requirement REQ-SMK-009 must be excluded by default: %+v", got)
	}

	// Each of the four fields carried.
	sr := byID["REQ-SMK-002"]
	if sr.Kind != "system" {
		t.Errorf("SR kind = %q, want %q", sr.Kind, "system")
	}
	if sr.WorkStatus != "DONE" {
		t.Errorf("SR work_status = %q, want %q", sr.WorkStatus, "DONE")
	}
	if sr.DetailMD == "" || !bytes.Contains([]byte(sr.DetailMD), []byte("- **Tests:**")) {
		t.Errorf("SR detail_md not carried verbatim: %q", sr.DetailMD)
	}
	ur := byID["UR-SMK-001"]
	if ur.Kind != "user" {
		t.Errorf("UR kind = %q, want %q", ur.Kind, "user")
	}
	if ur.WorkStatus != "IN_REVIEW" {
		t.Errorf("UR work_status = %q, want %q", ur.WorkStatus, "IN_REVIEW")
	}
}

func TestRequirementsCorpusWarningGoesToStderrNotStdout(t *testing.T) {
	srv := rcServer(t)
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}

	var out, errBuf bytes.Buffer
	if err := emitRequirementsCorpus(&out, &errBuf, env); err != nil {
		t.Fatalf("emitRequirementsCorpus: %v", err)
	}

	// The warning about the requirement with no detail_md must be on stderr…
	if !bytes.Contains(errBuf.Bytes(), []byte("REQ-SMK-003")) {
		t.Errorf("expected a stderr warning naming REQ-SMK-003, got stderr:\n%s", errBuf.String())
	}
	// …and must not corrupt stdout: it still parses as a JSON array.
	var got []corpusEntry
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("a warning corrupted stdout JSON: %v\nstdout:\n%s", err, out.String())
	}
	if bytes.Contains(out.Bytes(), []byte("has no detail_md")) {
		t.Errorf("warning text leaked into stdout:\n%s", out.String())
	}
}
