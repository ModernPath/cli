package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// EPIC-CLI-027 on the EPIC-CLI-TURNS batch paths: a findings file carries
// introduced_by, a dispositions file and a review file carry resolution and
// widens, and each runs the single flags' rules — the kind a RESOLVED
// disposition must name, widens only with a packet edit, and the
// finding_resolution capability check before any write.

func TestSRCLI027BatchFindingCarriesIntroducedBy(t *testing.T) {
	store := newFakeAuthorStore().withFindingResolution()
	store.contexts["EPIC-A"] = map[string]any{"packet_fingerprint": "agg-A"}
	store.seedFinding("F-1", "epic:EPIC-A", "RESOLVED", "correctness", "major")
	cobraWorkspace(t, store.serve(t))
	file := writePlanFile(t, "findings.json", `[
 {"id":"F-2","scope":"epic:EPIC-A","category":"contract","severity":"major","owner":"core","source":"r","body":"two","introduced_by":"F-1"}]`)

	if out, err := runRoot(t, "process", "findings", "add", "--file", file); err != nil {
		t.Fatalf("findings add --file with introduced_by: %v\n%s", err, out)
	}
	if got := findingCreates(store)["F-2"]["introduced_by"]; got != "F-1" {
		t.Errorf("the file's introduced_by reaches the record, got %v", got)
	}
}

func TestSRCLI027BatchIntroducedByNeedsTheCapability(t *testing.T) {
	store := newFakeAuthorStore() // no contract: finding_resolution is not advertised
	store.contexts["EPIC-A"] = map[string]any{"packet_fingerprint": "agg-A"}
	cobraWorkspace(t, store.serve(t))
	file := writePlanFile(t, "findings.json", `[
 {"id":"F-2","scope":"epic:EPIC-A","category":"contract","severity":"major","introduced_by":"F-1"},
 {"id":"F-3","scope":"epic:EPIC-A","category":"contract","severity":"major"}]`)

	out, err := runRoot(t, "process", "findings", "add", "--file", file)
	if err == nil || !strings.Contains(out, "finding_resolution") {
		t.Fatalf("a linked finding is refused without the capability, naming it, got %v\n%s", err, out)
	}
	creates := findingCreates(store)
	if _, ok := creates["F-2"]; ok {
		t.Errorf("the linked finding must not be posted, posted %v", creates["F-2"])
	}
	if _, ok := creates["F-3"]; !ok {
		t.Errorf("a refusal does not stop the rest: F-3 wants a create, got %v", creates)
	}
}

func TestSRCLI027BatchDispositionRunsTheResolutionRules(t *testing.T) {
	store := newFakeAuthorStore().withFindingResolution()
	for _, id := range []string{"F-1", "F-2", "F-3", "F-4", "F-5"} {
		store.seedFinding(id, "epic:EPIC-A", "OPEN", "correctness", "major")
	}
	cobraWorkspace(t, store.serve(t))
	file := writePlanFile(t, "dispositions.json", `[
 {"id":"F-1","scope":"epic:EPIC-A","from":"OPEN","disposition":"RESOLVED","ref":"abc123"},
 {"id":"F-2","scope":"epic:EPIC-A","from":"OPEN","disposition":"RESOLVED","resolution":"decision","ref":"not a user line"},
 {"id":"F-3","scope":"epic:EPIC-A","from":"OPEN","disposition":"RESOLVED","resolution":"scope","ref":"EPIC-SPLIT","widens":"USER:2026-09-29:ok"},
 {"id":"F-4","scope":"epic:EPIC-A","from":"OPEN","disposition":"RESOLVED","resolution":"packet-edit","ref":"abc123","widens":"USER:2026-09-29:accepted"},
 {"id":"F-5","scope":"epic:EPIC-A","from":"OPEN","disposition":"REJECTED","resolution":"scope","ref":"out of scope"}]`)

	out, err := runRoot(t, "process", "findings", "disposition", "--file", file)
	if err == nil {
		t.Fatalf("the refused entries make the call exit non-zero\n%s", out)
	}
	for _, id := range []string{"F-1", "F-2", "F-3", "F-5"} {
		if n := len(store.postsFor(id, "update")); n != 0 {
			t.Errorf("%s breaks a resolution rule and must not be written, got %d update(s)", id, n)
		}
	}
	for _, want := range []string{"--resolution is required on RESOLVED", "USER:", "--widens", "goes with --disposition RESOLVED only"} {
		if !strings.Contains(out, want) {
			t.Errorf("each refusal names its rule (%q):\n%s", want, out)
		}
	}
	upd := store.postsFor("F-4", "update")
	if len(upd) != 1 {
		t.Fatalf("a valid entry still runs: F-4 wants one update, got %v", store.posts)
	}
	rec, _ := upd[0].body["record"].(map[string]any)
	if rec["resolution_kind"] != "packet_edit" || rec["widening_source"] != "USER:2026-09-29:accepted" || rec["disposition_ref"] != "abc123" {
		t.Errorf("the entry's kind, widening and reference reach the record, got %v", rec)
	}
	if !strings.Contains(out, "OPEN → RESOLVED/packet-edit") {
		t.Errorf("the result line names the kind:\n%s", out)
	}
}

func TestSRCLI027BatchResolutionNeedsTheCapability(t *testing.T) {
	store := newFakeAuthorStore() // no contract route
	store.seedFinding("F-1", "epic:EPIC-A", "OPEN", "correctness", "major")
	cobraWorkspace(t, store.serve(t))
	file := writePlanFile(t, "dispositions.json", `[
 {"id":"F-1","scope":"epic:EPIC-A","from":"OPEN","disposition":"RESOLVED","resolution":"packet-edit"}]`)

	out, err := runRoot(t, "process", "findings", "disposition", "--file", file)
	if err == nil || !strings.Contains(out, "finding_resolution") {
		t.Fatalf("a kind is refused when the server does not advertise finding_resolution, got %v\n%s", err, out)
	}
	if len(store.posts) != 0 {
		t.Errorf("nothing is posted, posted %v", store.posts)
	}
}

func TestSRCLI027ReviewFileBreakingAResolutionRuleWritesNothing(t *testing.T) {
	store := reviewStore(reviewAggStamped, "F-OLD")
	store.seedFinding("F-OLD", "epic:EPIC-R", "OPEN", "correctness", "major")
	cobraWorkspace(t, store.serve(t))
	writeReviewStamp(t, "review", reviewAggStamped)
	file := writePlanFile(t, "review.json", `{"verdict":"PASS","body":"b","source":"RUN:2026-09-29:cold-review",
 "findings":[{"id":"F-NEW","category":"traceability","severity":"note","owner":"core","source":"r","body":"n"}],
 "dispositions":[{"id":"F-OLD","from":"OPEN","disposition":"RESOLVED","ref":"abc123"}]}`)

	out, err := runRoot(t, "process", "review", "record", "--file", file, "--scope", "EPIC-R", "--review-context", "review-ctx-1")
	if err == nil || !strings.Contains(err.Error(), "--resolution is required on RESOLVED") || !strings.Contains(err.Error(), "nothing was written") {
		t.Fatalf("a RESOLVED disposition without its kind refuses the whole review, got %v\n%s", err, out)
	}
	if len(store.posts) != 0 {
		t.Errorf("the refusal comes before any write, posted %v", postActions(store))
	}
}

// REQ-CROSS-447 with SR-CLI-027-004: the text page cuts across the scope
// groups of --all, a group's heading and rounds print only when one of its
// rows is on the page, and --json with --limit says how many rows there are.
func TestREQCROSS447FindingsPageCutsAcrossScopeGroups(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/findings", func(w http.ResponseWriter, r *http.Request) {
		rows := []any{}
		for _, scope := range []string{"EPIC-A", "EPIC-B", "EPIC-C"} {
			for i := 0; i < 3; i++ {
				rows = append(rows, map[string]any{"external_id": fmt.Sprintf("F-%s-%d", scope, i), "disposition": "OPEN",
					"category": "scope", "severity": "minor", "scope_kind": "epic", "scope_external_id": scope,
					"inserted_at": fmt.Sprintf("2026-09-01T00:00:0%dZ", i)})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"findings": rows}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cobraWorkspace(t, srv)

	out, err := runRoot(t, "process", "findings", "list", "--all", "--limit", "3", "--offset", "2")
	if err != nil {
		t.Fatalf("findings list: %v", err)
	}
	if n := countLines(out, "OPEN"); n != 3 {
		t.Errorf("--limit 3 prints three rows, printed %d:\n%s", n, out)
	}
	if !strings.Contains(out, "▸ epic:EPIC-A") || !strings.Contains(out, "▸ epic:EPIC-B") || strings.Contains(out, "EPIC-C") {
		t.Errorf("only the groups on the page print their heading:\n%s", out)
	}
	if !strings.Contains(out, "round 1 · context unattributed · 3 finding(s)") {
		t.Errorf("a group's round summary counts all its rows, not only the page's:\n%s", out)
	}
	if !strings.Contains(out, "showing 3 of 9 · --offset 5 for more") {
		t.Errorf("the footer names the total and the next offset:\n%s", out)
	}

	out, err = runRoot(t, "process", "findings", "list", "--all", "--json", "--limit", "4")
	if err != nil {
		t.Fatalf("findings list --json: %v", err)
	}
	var obj struct {
		Findings []any `json:"findings"`
		Rounds   []any `json:"rounds"`
		Total    int   `json:"total"`
		HasMore  bool  `json:"has_more"`
	}
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("--json is one object: %v\n%s", err, out)
	}
	if len(obj.Findings) != 4 || obj.Total != 9 || !obj.HasMore || len(obj.Rounds) != 3 {
		t.Errorf("--json --limit 4 carries four rows, the total, has_more and every round, got %d rows, total %d, has_more %v, %d rounds",
			len(obj.Findings), obj.Total, obj.HasMore, len(obj.Rounds))
	}
}
