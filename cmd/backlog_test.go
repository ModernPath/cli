package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/opschema"
)

// EPIC-CLI-023 — REQ-CROSS-423 (SCN-LIST-001, SCN-VOCAB-001): `process backlog
// list` prints backlog, gap and tooling records newest first, filtered by kind
// and disposition, with a footer naming the read; a filter value outside the
// vocabulary is refused before any request, naming the accepted values.

func readbackBacklogRows() []any {
	return []any{
		map[string]any{"external_id": "BACKLOG-TOOL-16", "kind": "tooling", "disposition": "ROUTED to EPIC-CLI-023",
			"raised_by": "jussi@modernpath.ai", "raised_at": "2026-09-19T08:00:00.000000Z", "title": "no verb lists backlog records"},
		map[string]any{"external_id": "GAP-CLI-004", "kind": "gap", "disposition": "OPEN", "gap_kind": "specification",
			"raised_by": "cold review", "raised_at": "2026-09-21T10:30:00.000000Z", "title": "the trace read drops its verdict"},
		map[string]any{"external_id": "BACKLOG-CROSS-0007", "kind": "backlog", "disposition": "DEFERRED",
			"raised_by": "pasi@modernpath.ai", "raised_at": "2026-09-20T12:00:00.000000Z", "title": "a discovery"},
	}
}

func TestREQCROSS423BacklogListPrintsEveryRecordNewestFirstWithAFooter(t *testing.T) {
	fx := &wsFixture{backlog: readbackBacklogRows()}
	env := wsEnv(t, wsServe(t, fx))
	var out bytes.Buffer
	if err := processBacklogList(env, backlogListOpts{}, &out); err != nil {
		t.Fatalf("list: %v", err)
	}
	s := out.String()
	lines := strings.Split(strings.TrimSpace(s), "\n")
	idx := func(id string) int {
		for i, l := range lines {
			if strings.Contains(l, id) {
				return i
			}
		}
		t.Fatalf("%s not printed:\n%s", id, s)
		return -1
	}
	if !(idx("GAP-CLI-004") < idx("BACKLOG-CROSS-0007") && idx("BACKLOG-CROSS-0007") < idx("BACKLOG-TOOL-16")) {
		t.Fatalf("records must print newest first:\n%s", s)
	}
	for _, want := range []string{
		"tooling", "ROUTED to EPIC-CLI-023", "jussi@modernpath.ai", "2026-09-19", "no verb lists backlog records",
		"gap", "specification",
		"3 record(s) — read one with working-set pull <id>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the list must print %q, got:\n%s", want, s)
		}
	}
	if !strings.Contains(fx.lastBacklogQuery, "system_id=4") || strings.Contains(fx.lastBacklogQuery, "kind=") {
		t.Fatalf("no filter: only system_id is sent, got %q", fx.lastBacklogQuery)
	}
}

func TestREQCROSS423BacklogListFiltersReachTheQuery(t *testing.T) {
	fx := &wsFixture{backlog: []any{}}
	env := wsEnv(t, wsServe(t, fx))
	var out bytes.Buffer
	if err := processBacklogList(env, backlogListOpts{kind: "tooling", disposition: "ROUTED"}, &out); err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{"kind=tooling", "disposition=ROUTED"} {
		if !strings.Contains(fx.lastBacklogQuery, want) {
			t.Errorf("the filter must reach the query (%q), got %q", want, fx.lastBacklogQuery)
		}
	}
	if !strings.Contains(out.String(), "0 record(s)") {
		t.Errorf("an empty answer still counts, got:\n%s", out.String())
	}
}

func TestREQCROSS423BacklogListRefusesAValueOutsideTheVocabularyBeforeAnyRequest(t *testing.T) {
	fx := &wsFixture{backlog: []any{}}
	env := wsEnv(t, wsServe(t, fx))
	var out bytes.Buffer

	err := processBacklogList(env, backlogListOpts{kind: "other"}, &out)
	if err == nil || !strings.Contains(err.Error(), "backlog|gap|tooling") {
		t.Fatalf("--kind other must be refused naming backlog|gap|tooling, got %v", err)
	}
	err = processBacklogList(env, backlogListOpts{disposition: "parked"}, &out)
	if err == nil || !strings.Contains(err.Error(), "OPEN, DEFERRED, ROUTED to <id>, REJECTED with <source>, CLOSED by <id>, ACCEPTED with <source>") {
		t.Fatalf("--disposition parked must be refused naming the words, got %v", err)
	}
	// A prefix of a word (the common way to filter) is accepted.
	if err := processBacklogList(env, backlogListOpts{disposition: "ROUTED"}, &out); err != nil {
		t.Fatalf("ROUTED is a valid filter: %v", err)
	}
	if fx.backlogHits != 1 {
		t.Fatalf("the refusals must send nothing; hits = %d", fx.backlogHits)
	}
}

func TestREQCROSS423BacklogListIsWiredUnderProcessThroughCobra(t *testing.T) {
	fx := &wsFixture{backlog: readbackBacklogRows()}
	srv := wsServe(t, fx)
	cobraWorkspace(t, srv)
	out, err := runRoot(t, "process", "backlog", "list", "--kind", "gap")
	if err != nil {
		t.Fatalf("process backlog list: %v", err)
	}
	if !strings.Contains(out, "GAP-CLI-004") {
		t.Fatalf("the cobra path must print the served record:\n%s", out)
	}
	if !strings.Contains(fx.lastBacklogQuery, "kind=gap") {
		t.Fatalf("the cobra flag must reach the query, got %q", fx.lastBacklogQuery)
	}
}

// SCN-VOCAB-001: the author verb refuses a gap kind outside the vocabulary
// locally, naming the set — never posting for the server to say "is invalid".
func TestREQCROSS423AuthorBacklogRefusesALegacyGapKindLocally(t *testing.T) {
	srv, got := authorCapture(t)
	cobraWorkspace(t, srv)
	_, err := runRoot(t, "author", "backlog", "GAP-CLI-009", "--kind", "gap", "--title", "a gap",
		"--gap-kind", "ledger", "--affected-trace", "REQ-CROSS-1", "--consequence", "unprovable")
	if err == nil || !strings.Contains(err.Error(), "capability|specification") {
		t.Fatalf("--gap-kind ledger must be refused naming capability|specification, got %v", err)
	}
	if len(*got) != 0 {
		t.Fatalf("a local refusal must post nothing, posted %v", *got)
	}
}

// PR #618 review (finding 8): the CLI's local vocabulary is a copy of what the
// vendored schema (and the server's changeset) state. This pins the copy to
// the schema so the next vocabulary change cannot leave the CLI refusing what
// the server accepts.
func TestREQCROSS423LocalBacklogVocabularyMatchesTheVendoredSchema(t *testing.T) {
	gapKind, ok := opschema.Field("upsert_backlog_record", "gap_kind")
	if !ok {
		t.Fatal("the vendored schema declares no gap_kind on upsert_backlog_record")
	}
	if strings.Join(gapKind.Enum, "|") != strings.Join(backlogGapKinds, "|") {
		t.Fatalf("gap kinds drifted: schema %v, CLI %v", gapKind.Enum, backlogGapKinds)
	}
	disposition, ok := opschema.Field("upsert_backlog_record", "disposition")
	if !ok {
		t.Fatal("the vendored schema declares no disposition on upsert_backlog_record")
	}
	var alternatives []string
	alternatives = append(alternatives, backlogDispositionWords...)
	for _, p := range backlogDispositionPrefixes {
		alternatives = append(alternatives, p+".+")
	}
	if want := "^(" + strings.Join(alternatives, "|") + ")$"; disposition.Pattern != want {
		t.Fatalf("dispositions drifted: schema pattern %q, CLI words build %q", disposition.Pattern, want)
	}
}
