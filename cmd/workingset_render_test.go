package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/authoring/diff"
)

// REQ-CROSS-381 (EPIC-CLI-018): working-set pull renders every stored field a
// review reads — statement, sources, acceptance criteria, rationale, boundary,
// verification method, owner, stage, priority, detail — so a reviewer reading
// the snapshot sees what the store holds. A served-but-empty field is "—"; the
// «not served» marker is only ever a statement about an absent key.
func TestREQCROSS381PullRendersEveryServedReviewField(t *testing.T) {
	req := wsReq("REQ-RF-001", "Rendered fields")
	req["description"] = "the statement"
	req["source_citations"] = []any{map[string]any{"kind": "USER", "ref": "USER:2026-09-12:x"}}
	req["rationale"] = "because reviewers read it"
	req["boundary"] = "the renderer only"
	req["verification_method"] = "golden render"
	req["priority"] = "P1"
	req["owner"] = "cli"
	req["criteria"] = []any{map[string]any{"statement": "AC1 renders"}}
	req["detail_md"] = "## Problem\n\nprose here"
	req["parent_external_ids"] = []any{"UR-RF-001"}
	req["fingerprint"] = strings.Repeat("f", 64)
	epic := wsEpic("EPIC-RF-001", "Rendered epic")
	epic["owner"] = "jussi"
	epic["scope"] = "the CLI read"
	epic["outcome_source"] = "USER:2026-09-12:y"
	epic["process_status"] = "TODO"
	epic["requirement_external_ids"] = []any{"REQ-RF-001"}
	fx := &wsFixture{epics: []any{epic}, requirements: []any{req}}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPull(env, []string{"EPIC-RF-001", "REQ-RF-001"}, wsNow); err != nil {
		t.Fatalf("pull failed: %v", err)
	}

	raw, _ := os.ReadFile(filepath.Join(env.Root, workingSetDir, "REQ-RF-001.md"))
	got := string(raw)
	for _, want := range []string{
		"Statement / source:** the statement / USER: USER:2026-09-12:x",
		"Rationale:** because reviewers read it",
		"Boundary:** the renderer only",
		"Verification method:** golden render",
		"Priority:** P1",
		"Owner / release:** cli / modernpath-v1-09",
		"AC1 renders",
		"prose here",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("a served field must render (%q):\n%s", want, got)
		}
	}
	if strings.Contains(got, notServed) {
		t.Errorf("every reviewer field is served here — nothing may read as not served:\n%s", got)
	}

	raw, _ = os.ReadFile(filepath.Join(env.Root, workingSetDir, "EPIC-RF-001.md"))
	got = string(raw)
	for _, want := range []string{
		"Owner / release:** jussi / modernpath-v1-09",
		"Scope:** the CLI read",
		"Outcome source:** USER:2026-09-12:y",
		"Process status:** TODO",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("a served epic field must render (%q):\n%s", want, got)
		}
	}
	if strings.Contains(got, "Kind / status:** Epic / "+notServed) {
		t.Errorf("the epic has no `status` column; the slot must not claim one is withheld:\n%s", got)
	}
}

func TestREQCROSS381AServedEmptyFieldReadsAsEmptyNotWithheld(t *testing.T) {
	req := wsReq("REQ-RF-002", "Empty fields")
	req["description"] = "the statement"
	req["source_citations"] = []any{}
	req["rationale"] = ""
	fx := &wsFixture{requirements: []any{req}}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPull(env, []string{"REQ-RF-002"}, wsNow); err != nil {
		t.Fatalf("pull failed: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(env.Root, workingSetDir, "REQ-RF-002.md"))
	got := string(raw)
	for _, want := range []string{"Statement / source:** the statement / —", "Rationale:** —"} {
		if !strings.Contains(got, want) {
			t.Errorf("a served-but-empty field reads as empty (%q), never as withheld:\n%s", want, got)
		}
	}
}

// typedCitations are stored citations of every form: a reference, a typed
// identity without one, each typed fallback, and two legacy process sources.
func typedCitations() []any {
	return []any{
		map[string]any{"kind": "code", "ref": "lib/billing.ex"},
		map[string]any{"kind": "code", "repository_key": "app", "revision": "abc123", "path": "lib/typed.ex", "sha256": strings.Repeat("1", 64), "source_file_id": "6f1c2b9e-0000-4000-8000-000000000001"},
		map[string]any{"kind": "test", "path": "test/only_path_test.exs", "source_file_id": "6f1c2b9e-0000-4000-8000-000000000002"},
		map[string]any{"kind": "test", "source_file_id": "6f1c2b9e-0000-4000-8000-000000000003"},
		map[string]any{"kind": "document", "system_doc_id": "doc-77", "version": 3},
		map[string]any{"kind": "process_source", "id": "USER:2026-10-07:legacy"},
		map[string]any{"kind": "process_source", "source_tag": "USER:2026-10-07:tagged"},
	}
}

const typedCitationLine = "code: lib/billing.ex · code: app@abc123:lib/typed.ex · test: test/only_path_test.exs · " +
	"test: 6f1c2b9e-0000-4000-8000-000000000003 · document: doc-77 · process_source: USER:2026-10-07:legacy · " +
	"process_source: USER:2026-10-07:tagged"

// SR-RDD-ONBOARD-023: the by-id render shows one entry, with its kind, for
// every stored citation; one without a reference by its typed identity.
func TestSRRDDONBOARD023PullRendersEveryStoredCitation(t *testing.T) {
	req := wsReq("REQ-TC-001", "Typed citations")
	req["description"] = "the statement"
	req["source_citations"] = typedCitations()
	env := wsEnv(t, wsServe(t, &wsFixture{requirements: []any{req}}))
	if err := workingSetPull(env, []string{"REQ-TC-001"}, wsNow); err != nil {
		t.Fatalf("pull failed: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(env.Root, workingSetDir, "REQ-TC-001.md"))
	if want := "Statement / source:** the statement / " + typedCitationLine + "\n"; !strings.Contains(string(raw), want) {
		t.Fatalf("every stored citation must render (%q):\n%s", want, raw)
	}
}

// SR-RDD-ONBOARD-023: a dash only when the store holds no citation; a record
// whose citations all lack a reference does not read as having none.
func TestSRRDDONBOARD023DashOnlyWhenNoCitationIsStored(t *testing.T) {
	req := wsReq("REQ-TC-002", "Only typed citations")
	req["description"] = "the statement"
	req["source_citations"] = []any{
		map[string]any{"kind": "code", "repository_key": "app", "revision": "abc123", "path": "lib/typed.ex"},
		map[string]any{"kind": "test", "source_file_id": "6f1c2b9e-0000-4000-8000-000000000003"},
	}
	env := wsEnv(t, wsServe(t, &wsFixture{requirements: []any{req}}))
	if err := workingSetPull(env, []string{"REQ-TC-002"}, wsNow); err != nil {
		t.Fatalf("pull failed: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(env.Root, workingSetDir, "REQ-TC-002.md"))
	got := string(raw)
	if strings.Contains(got, "Statement / source:** the statement / —") {
		t.Fatalf("a record with stored citations reads as having none:\n%s", got)
	}
	if want := "Statement / source:** the statement / code: app@abc123:lib/typed.ex · test: 6f1c2b9e-0000-4000-8000-000000000003\n"; !strings.Contains(got, want) {
		t.Fatalf("the stored citations must render (%q):\n%s", want, got)
	}
}

// SR-RDD-ONBOARD-023: the review render — the bundle and the per-file copy
// beside it — shows the same entries as the by-id render.
func TestSRRDDONBOARD023ReviewRenderShowsEveryStoredCitation(t *testing.T) {
	fx := reviewBundleFixture()
	fx.requirements[0].(map[string]any)["source_citations"] = typedCitations()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, true, wsNow); err != nil {
		t.Fatalf("pull --scope --for-review: %v", err)
	}
	dir := latestReviewTestDirectory(t, env.Root, "EPIC-B")
	bundle := readScopeFile(t, filepath.Join(dir, "REVIEW.md"))
	if want := "- **Sources:** " + typedCitationLine + "\n"; !strings.Contains(bundle, want) {
		t.Errorf("the review bundle must show every stored citation (%q):\n%s", want, bundle)
	}
	member := readScopeFile(t, filepath.Join(dir, "members", "REQ-B-1.md"))
	for _, entry := range strings.Split(typedCitationLine, " · ") {
		if !strings.Contains(member, "- "+entry+"\n") {
			t.Errorf("the per-file review copy must list %q:\n%s", entry, member)
		}
	}
}

// SR-RDD-ONBOARD-023: a citation is labelled in the web app's order — the typed
// identity first, even when a differing reference is stored beside it, then
// the reference — in the by-id render, the review bundle and the per-file copy.
func TestSRRDDONBOARD023TypedIdentityLabelsACitationFirst(t *testing.T) {
	citations := []any{
		map[string]any{"kind": "code", "ref": "lib/stored_ref.ex", "repository_key": "web", "revision": "def456", "path": "lib/typed_with_ref.ex"},
		map[string]any{"kind": "code", "ref": "lib/billing.ex"},
		map[string]any{"kind": "code", "repository_key": "app", "revision": "abc123", "path": "lib/typed.ex"},
	}
	const line = "code: web@def456:lib/typed_with_ref.ex · code: lib/billing.ex · code: app@abc123:lib/typed.ex"

	req := wsReq("REQ-TC-003", "Typed identity first")
	req["description"] = "the statement"
	req["source_citations"] = citations
	env := wsEnv(t, wsServe(t, &wsFixture{requirements: []any{req}}))
	if err := workingSetPull(env, []string{"REQ-TC-003"}, wsNow); err != nil {
		t.Fatalf("pull failed: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(env.Root, workingSetDir, "REQ-TC-003.md"))
	if want := "Statement / source:** the statement / " + line + "\n"; !strings.Contains(string(raw), want) {
		t.Errorf("the by-id render must label the typed identity first (%q):\n%s", want, raw)
	}
	if strings.Contains(string(raw), "lib/stored_ref.ex") {
		t.Errorf("a citation with a typed identity must not be labelled by its reference:\n%s", raw)
	}

	fx := reviewBundleFixture()
	fx.requirements[0].(map[string]any)["source_citations"] = citations
	env = wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, true, wsNow); err != nil {
		t.Fatalf("pull --scope --for-review: %v", err)
	}
	dir := latestReviewTestDirectory(t, env.Root, "EPIC-B")
	if bundle := readScopeFile(t, filepath.Join(dir, "REVIEW.md")); !strings.Contains(bundle, "- **Sources:** "+line+"\n") {
		t.Errorf("the review bundle must label the typed identity first (%q):\n%s", line, bundle)
	}
	member := readScopeFile(t, filepath.Join(dir, "members", "REQ-B-1.md"))
	for _, entry := range strings.Split(line, " · ") {
		if !strings.Contains(member, "- "+entry+"\n") {
			t.Errorf("the per-file review copy must list %q:\n%s", entry, member)
		}
	}
}

// Regression guard for SR-RDD-ONBOARD-023: the editable citation list of a
// scope pull keeps only the citations push can send back, and the push plan
// derived from it still sends kind and reference only — a typed identity is
// never listed there, so an edit cannot drop it.
func TestSRRDDONBOARD023EditableCitationsAndPushPlanAreUnchanged(t *testing.T) {
	fx := reviewBundleFixture()
	fx.workSelection["current"].(map[string]any)["members"] = []any{"REQ-B-1"}
	fx.requirements[0].(map[string]any)["source_citations"] = typedCitations()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull --scope: %v", err)
	}
	member := readScopeFile(t, filepath.Join(env.Root, workingSetDir, "EPIC-B", "members", "REQ-B-1.md"))
	if !strings.Contains(member, "```authoring:citations\n- code: lib/billing.ex\n- process_source: USER:2026-10-07:tagged\n```") {
		t.Fatalf("the editable citation list must hold only the citations with a reference:\n%s", member)
	}
	for _, typed := range []string{"app@abc123", "test/only_path_test.exs", "doc-77", "USER:2026-10-07:legacy"} {
		if strings.Contains(member, typed) {
			t.Fatalf("a typed identity entered the editable list (%q):\n%s", typed, member)
		}
	}

	base := recordFromPayload(scopeRecord{kind: "system", payload: fx.requirements[0].(map[string]any)}, nil)
	if p, refusal := diff.Diff(base, base); refusal != nil || !p.Empty() {
		t.Fatalf("an unedited record must plan nothing: %+v %v", p, refusal)
	}
	edited := base
	edited.SourceCitations = append(append([]string{}, base.SourceCitations...), "test: test/new_test.exs")
	p, refusal := diff.Diff(base, edited)
	if refusal != nil {
		t.Fatalf("a citation edit was refused: %v", refusal)
	}
	want := []any{
		map[string]any{"kind": "code", "ref": "lib/billing.ex"},
		map[string]any{"kind": "process_source", "ref": "USER:2026-10-07:tagged"},
		map[string]any{"kind": "test", "ref": "test/new_test.exs"},
	}
	if got := buildPatchRecord("system", "REQ-B-1", "sr-b1-fp", "ctx", p)["source_citations"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("push must send kind and reference only:\n got %v\nwant %v", got, want)
	}
}

// REQ-CROSS-382 (EPIC-CLI-018): an epic pull renders every declared member,
// user requirements and system requirements alike, in declared order.
func TestREQCROSS382EpicPullRendersUserRequirementMembers(t *testing.T) {
	epic := wsEpic("EPIC-UR-001", "With a UR")
	epic["requirement_external_ids"] = []any{"REQ-1", "REQ-2"}
	epic["user_requirement_external_ids"] = []any{"UR-1"}
	fx := &wsFixture{epics: []any{epic}}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPull(env, []string{"EPIC-UR-001"}, wsNow); err != nil {
		t.Fatalf("pull failed: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(env.Root, workingSetDir, "EPIC-UR-001.md"))
	if !strings.Contains(string(raw), "Members:** REQ-1 · REQ-2 · UR-1") {
		t.Fatalf("the Members line must carry the UR beside the SRs:\n%s", raw)
	}
}

// The authoring render's members block reads the store's declared membership
// too, not the selection's frozen copy — so a push computes its membership ops
// against what the store holds.
func TestREQCROSS382ScopePullMembersBlockCarriesTheServedUnion(t *testing.T) {
	fx := scaffoldFixture()
	fx.workSelection["current"].(map[string]any)["members"] = []any{"REQ-CROSS-310"}
	fx.epics[0].(map[string]any)["requirement_external_ids"] = []any{"REQ-CROSS-310"}
	fx.epics[0].(map[string]any)["user_requirement_external_ids"] = []any{"UR-CLI-008"}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull: %v", err)
	}
	content := readScopeFile(t, filepath.Join(scopeDir(env), "EPIC-CLI-008.md"))
	if !strings.Contains(content, "UR-CLI-008") {
		t.Fatalf("the members block must list the served UR member:\n%s", content)
	}
}

// --- REQ-CROSS-489: a requirement pull shows its trace links ---

// tlServe wraps the fixture server and records the query of every named item
// read. With traces non-nil, each requirement entry is served with that
// traces key whether or not the client asked for it, so a read that must not
// carry traces is tested against a server that sends them.
func tlServe(t *testing.T, fx *wsFixture, queries *[]string, traces map[string]any) *httptest.Server {
	t.Helper()
	inner := wsServe(t, fx)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sync/items" {
			*queries = append(*queries, r.URL.RawQuery)
			if traces != nil && fx.items == nil {
				entries := legacyFixtureItems(fx, r.URL.Query()["ids[]"])
				for _, raw := range entries {
					entry := raw.(map[string]any)
					if kind := str(entry, "kind"); kind == "system" || kind == "user" {
						entry["traces"] = traces
					}
				}
				json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"items": entries, "missing": []any{}}})
				return
			}
		}
		inner.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func tlEntry(item map[string]any, traces map[string]any) map[string]any {
	entry := map[string]any{"kind": "system", "item": item, "gates": []any{}}
	if traces != nil {
		entry["traces"] = traces
	}
	return entry
}

func tlServedTraces() map[string]any {
	return map[string]any{
		"from": []any{
			map[string]any{"target_type": "file_analysis", "target_id": "00000000-0000-0000-0000-00000000002a",
				"external_id": nil, "label": "lib/trace_links.ex", "link_kind": "implements",
				"trace_status": "active", "authority": "confirmed", "stale": false},
			map[string]any{"target_type": "system_requirement", "target_id": "b1",
				"external_id": "REQ-TL-CANDIDATE", "label": "a candidate link", "link_kind": "relates",
				"trace_status": "active", "authority": "candidate", "stale": false},
		},
		"to": []any{
			map[string]any{"source_type": "test_case", "source_id": "c1",
				"external_id": "test/trace_links_test.exs", "label": "trace links render", "link_kind": "verifies",
				"trace_status": "stale", "authority": "confirmed", "stale": true},
			map[string]any{"source_type": "system_requirement", "source_id": "d1",
				"external_id": "REQ-TL-REJECTED", "label": "a rejected link", "link_kind": "derives",
				"trace_status": "active", "authority": "rejected", "stale": false},
		},
		"truncated": true,
	}
}

func tlPulledFile(t *testing.T, entry map[string]any) (*factoryEnv, string) {
	t.Helper()
	fx := &wsFixture{items: []any{entry}}
	env := wsEnv(t, wsServe(t, fx))
	id := str(entry["item"].(map[string]any), "external_id")
	if err := workingSetPull(env, []string{id}, wsNow); err != nil {
		t.Fatalf("pull: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(env.Root, workingSetDir, id+".md"))
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	return env, string(raw)
}

// The by-id pull and check ask for traces; nothing else does (D9).
func TestREQCROSS489ByIDPullAndCheckRequestTraceLinks(t *testing.T) {
	fx := &wsFixture{requirements: []any{wsReq("REQ-TL-001", "Traced")}}
	var queries []string
	env := wsEnv(t, tlServe(t, fx, &queries, nil))

	if err := workingSetPull(env, []string{"REQ-TL-001"}, wsNow); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if len(queries) != 1 || !strings.Contains(queries[0], "include_traces=true") {
		t.Errorf("a by-id pull must send include_traces=true, got %q", queries)
	}

	queries = nil
	if err := workingSetCheck(env, false, wsNow); err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(queries) != 1 || !strings.Contains(queries[0], "include_traces=true") {
		t.Errorf("check must send include_traces=true so it hashes what the pull wrote, got %q", queries)
	}
}

// Guard: a scope pull never asks for traces, and never renders them even when
// a server sends the key.
func TestREQCROSS489ScopePullDoesNotRequestTraceLinks(t *testing.T) {
	fx := scopeFixture()
	var queries []string
	env := wsEnv(t, tlServe(t, fx, &queries, tlServedTraces()))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull --scope: %v", err)
	}
	if len(queries) == 0 {
		t.Fatal("the scope pull made no named item read; the guard checks nothing")
	}
	for _, q := range queries {
		if strings.Contains(q, "include_traces") {
			t.Errorf("a scope pull must not request traces, got %q", q)
		}
	}
	member := readScopeFile(t, filepath.Join(scopeDir(env), "members", "REQ-CROSS-310.md"))
	if strings.Contains(member, "Trace links") {
		t.Errorf("a scope member file must not carry a Trace links section:\n%s", member)
	}
}

// The served key renders both directions: type, id (or path), label, link
// kind, authority, (stale), and a note when the server capped the list.
func TestREQCROSS489PullRendersTraceLinksInBothDirections(t *testing.T) {
	_, got := tlPulledFile(t, tlEntry(wsReq("REQ-TL-002", "Traced"), tlServedTraces()))

	section := got[strings.Index(got, "### Trace links")+1:]
	if !strings.Contains(got, "### Trace links") {
		t.Fatalf("a served traces key must render a Trace links section:\n%s", got)
	}
	for _, want := range []string{
		"- file_analysis lib/trace_links.ex · implements · confirmed\n",
		"- system_requirement REQ-TL-CANDIDATE — a candidate link · relates · candidate\n",
		"- test_case test/trace_links_test.exs — trace links render · verifies · confirmed (stale)\n",
		"- system_requirement REQ-TL-REJECTED — a rejected link · derives · rejected\n",
		"first 50 links per direction",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("the Trace links section must contain %q:\n%s", want, got)
		}
	}
	out, in := strings.Index(section, "**Outgoing**"), strings.Index(section, "**Incoming**")
	if out < 0 || in < out {
		t.Fatalf("Outgoing must precede Incoming:\n%s", got)
	}
	if !strings.Contains(section[out:in], "REQ-TL-CANDIDATE") || !strings.Contains(section[in:], "REQ-TL-REJECTED") {
		t.Errorf("each link must sit under its own direction:\n%s", got)
	}
	if strings.Index(got, "### Trace links") > strings.Index(got, "### Gates") {
		t.Errorf("Trace links render before Gates:\n%s", got)
	}
}

// A served-but-empty list reads "—"; an absent key reads the not-served
// marker; neither claims a cap.
func TestREQCROSS489EmptyAndAbsentTraceLinks(t *testing.T) {
	empty := map[string]any{"from": []any{}, "to": []any{}, "truncated": false}
	_, got := tlPulledFile(t, tlEntry(wsReq("REQ-TL-003", "Untraced"), empty))
	if !strings.Contains(got, "### Trace links\n\n**Outgoing**\n\n—\n\n**Incoming**\n\n—\n") {
		t.Errorf("empty served lists must each read —:\n%s", got)
	}
	if strings.Contains(got, "first 50") {
		t.Errorf("an uncapped list must not carry the truncated note:\n%s", got)
	}

	_, got = tlPulledFile(t, tlEntry(wsReq("REQ-TL-004", "Older server"), nil))
	if !strings.Contains(got, "### Trace links\n\n"+notServed+"\n") {
		t.Errorf("an absent traces key must read the not-served marker:\n%s", got)
	}
}

// Source identity folds traces in only when a list is non-empty (D11), so an
// untraced snapshot keeps its hash across the deploy.
func TestREQCROSS489SourceIdentityFoldsTracesOnlyWhenNonEmpty(t *testing.T) {
	identity := func(traces map[string]any) string {
		_, raw := tlPulledFile(t, tlEntry(wsReq("REQ-TL-005", "Identity"), traces))
		snapshot, err := parseWorkingSetSnapshot("REQ-TL-005.md", raw)
		if err != nil {
			t.Fatalf("parse snapshot: %v", err)
		}
		return snapshot.sourceIdentity
	}
	absent := identity(nil)
	empty := identity(map[string]any{"from": []any{}, "to": []any{}, "truncated": false})
	traced := identity(tlServedTraces())
	if empty != absent {
		t.Errorf("empty traces must keep the identity of no key: %s vs %s", empty, absent)
	}
	if traced == absent {
		t.Errorf("a non-empty trace list must change the Source identity")
	}
}
