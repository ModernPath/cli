package rdd

// REQ-CROSS-222 §6 — the Tests and Code cells travel as raw text exactly.
// The parsed citations are additive: what splitEvidenceRefs extracts does not
// change, and what it leaves behind no longer disappears.

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// rawExtra returns the value of a named extra column, and whether it is there.
func rawExtra(p map[string]any, name string) (string, bool) {
	extras, _ := p["extra_columns"].([]map[string]any)
	for _, e := range extras {
		if n, _ := e["name"].(string); n == name {
			v, _ := e["value"].(string)
			return v, true
		}
	}
	return "", false
}

func citationRefs(p map[string]any, kind string) []string {
	var out []string
	cits, _ := p["source_citations"].([]any)
	for _, c := range cits {
		cm, _ := c.(map[string]any)
		if k, _ := cm["kind"].(string); k == kind {
			ref, _ := cm["ref"].(string)
			out = append(out, ref)
		}
	}
	return out
}

// A cell that mixes backticked references with prose keeps both: the refs are
// parsed as before, and the cell itself rides whole.
func TestEvidenceCellsRideRawBesideTheParsedRefs(t *testing.T) {
	tests := "`a_test.exs` 13/13 — RED first; live probe verified"
	code := "`a.ex` · BFF passthrough not built"
	p := BuildRequirementOp(Req{
		ID: "REQ-XX-001", Title: "Raw cells", Ctx: "XX", Status: "PROPOSED",
		Tests: tests, Code: code,
	}).Payload

	got, ok := rawExtra(p, "tests_raw")
	if !ok || got != tests {
		t.Fatalf("tests_raw = %q (present=%v), want the cell exactly: %q", got, ok, tests)
	}
	got, ok = rawExtra(p, "code_raw")
	if !ok || got != code {
		t.Fatalf("code_raw = %q (present=%v), want the cell exactly: %q", got, ok, code)
	}

	// Additive, never a replacement: the parsed refs are what they always were.
	if refs := citationRefs(p, "test"); len(refs) != 1 || refs[0] != "a_test.exs" {
		t.Fatalf("parsed test refs changed: %v", refs)
	}
	if refs := citationRefs(p, "code"); len(refs) != 1 || refs[0] != "a.ex" {
		t.Fatalf("parsed code refs changed: %v", refs)
	}
}

// Inner spacing is content: the raw carrier collapses nothing the row parser
// left standing.
func TestRawEvidenceCellsPreserveInnerSpacing(t *testing.T) {
	cell := "`a.ex`  ·  two   spaces kept"
	p := BuildRequirementOp(Req{ID: "REQ-XX-002", Title: "Spacing", Ctx: "XX", Status: "PROPOSED", Code: cell}).Payload
	if got, _ := rawExtra(p, "code_raw"); got != cell {
		t.Fatalf("code_raw = %q, want %q", got, cell)
	}
}

// The reference splitter caps a backtick-free cell at 200 runes. The raw
// carrier does not inherit that cap — preservation and parsing are separate
// jobs, and only one of them is allowed to shorten anything.
func TestRawEvidenceCellsAreUncapped(t *testing.T) {
	long := "no test written yet: " + strings.Repeat("evidence pending ", 30)
	long = strings.TrimSpace(long)
	if utf8.RuneCountInString(long) <= 200 {
		t.Fatalf("fixture must exceed the 200-rune splitter cap, got %d", utf8.RuneCountInString(long))
	}
	p := BuildRequirementOp(Req{ID: "REQ-XX-003", Title: "Long cell", Ctx: "XX", Status: "PROPOSED", Tests: long}).Payload

	got, ok := rawExtra(p, "tests_raw")
	if !ok || got != long {
		t.Fatalf("tests_raw truncated or missing: present=%v len=%d want len=%d", ok, utf8.RuneCountInString(got), utf8.RuneCountInString(long))
	}
	// The parsed ref keeps its cap — this pins the parsing side unchanged. The
	// fixture is prose ("no test written yet: …"), not a citation, so it rides
	// as a note rather than a fabricated test reference: a verdict sentence
	// with no backticks is not evidence of a test.
	refs := citationRefs(p, "note")
	if len(refs) != 1 || utf8.RuneCountInString(refs[0]) != 200 {
		t.Fatalf("parsed ref must stay capped at 200 runes: %d refs, first %d runes", len(refs), utf8.RuneCountInString(refs[0]))
	}
}

// A dash cell is absence, the same convention every other column uses. Absence
// carries nothing, so an unchanged corpus gains no keys and no hash churn.
func TestPlaceholderEvidenceCellsCarryNothing(t *testing.T) {
	for _, placeholder := range []string{"", "—", "-", "–", "  —  "} {
		p := BuildRequirementOp(Req{
			ID: "REQ-XX-004", Title: "Empty cells", Ctx: "XX", Status: "PROPOSED",
			Tests: placeholder, Code: placeholder,
		}).Payload
		if _, ok := p["extra_columns"]; ok {
			t.Fatalf("placeholder %q produced extra_columns: %v", placeholder, p["extra_columns"])
		}
	}
	// One side present, the other absent: only the present one is carried.
	p := BuildRequirementOp(Req{
		ID: "REQ-XX-005", Title: "Half", Ctx: "XX", Status: "PROPOSED",
		Tests: "—", Code: "`a.ex`",
	}).Payload
	if _, ok := rawExtra(p, "tests_raw"); ok {
		t.Fatal("an em-dash Tests cell must carry nothing")
	}
	if got, ok := rawExtra(p, "code_raw"); !ok || got != "`a.ex`" {
		t.Fatalf("code_raw = %q (present=%v)", got, ok)
	}
}

// §6 is unconditional. A row whose detail block supplies the citations still
// wrote something in its cells, and the cells are what the ledger shows.
func TestRawEvidenceCellsRideEvenWhenTheDetailBlockCarriesEvidence(t *testing.T) {
	detail := strings.Join([]string{
		"- **Statement:** It does the thing.",
		"- **Tests:** `TEST:a_test.exs:TestThing`",
		"- **Code:** `a.ex`",
	}, "\n")
	p := BuildRequirementOp(Req{
		ID: "REQ-XX-006", Title: "Block and cells", Ctx: "XX", Status: "PROPOSED",
		Detail: detail, Tests: "`a_test.exs` 13/13 green", Code: "`a.ex` partial",
	}).Payload
	if got, _ := rawExtra(p, "tests_raw"); got != "`a_test.exs` 13/13 green" {
		t.Fatalf("tests_raw = %q, want the cell even with a detail block", got)
	}
	if got, _ := rawExtra(p, "code_raw"); got != "`a.ex` partial" {
		t.Fatalf("code_raw = %q, want the cell even with a detail block", got)
	}
	// The detail block still owns the parsed citations.
	if refs := citationRefs(p, "test"); len(refs) != 1 || refs[0] != "a_test.exs:TestThing" {
		t.Fatalf("parsed refs must still come from the detail block: %v", refs)
	}
}

// Order is content identity: the ledger's own unrecognized columns come first,
// then Tests, then Code — so two runs over an unchanged row hash the same.
func TestRawEvidenceCellsFollowTheLedgerExtras(t *testing.T) {
	p := BuildRequirementOp(Req{
		ID: "REQ-XX-007", Title: "Order", Ctx: "XX", Status: "PROPOSED",
		Extras: []NamedCell{{Name: "Lane", Value: "fast"}},
		Tests:  "t.go", Code: "c.go",
	}).Payload
	extras, _ := p["extra_columns"].([]map[string]any)
	if len(extras) != 3 {
		t.Fatalf("extra_columns = %v, want Lane + both raw cells", extras)
	}
	want := []struct{ name, value string }{{"Lane", "fast"}, {"tests_raw", "t.go"}, {"code_raw", "c.go"}}
	for i, w := range want {
		if extras[i]["name"] != w.name || extras[i]["value"] != w.value {
			t.Fatalf("extra_columns[%d] = %v, want %s=%s", i, extras[i], w.name, w.value)
		}
	}
}

// A ledger column literally headed `tests_raw` collides with the reserved name.
// Nothing prevents it today, and this pins what happens so a change is a
// decision rather than an accident: the row carries BOTH entries, the ledger
// column first, and a name-keyed reader — the fidelity check included — reads
// the ledger column and reports the evidence cell as uncarried. Loudly wrong
// beats silently wrong; the raw text is still in the payload either way.
func TestReservedRawNamesCollideVisiblyWithALedgerColumn(t *testing.T) {
	p := BuildRequirementOp(Req{
		ID: "REQ-XX-008", Title: "Collision", Ctx: "XX", Status: "PROPOSED",
		Extras: []NamedCell{{Name: "tests_raw", Value: "LEDGER-COLUMN"}},
		Tests:  "`a_test.exs` real",
	}).Payload

	extras, _ := p["extra_columns"].([]map[string]any)
	if len(extras) != 2 {
		t.Fatalf("extra_columns = %v, want the ledger column AND the raw cell", extras)
	}
	if extras[0]["name"] != "tests_raw" || extras[0]["value"] != "LEDGER-COLUMN" {
		t.Fatalf("the ledger column must come first: %v", extras[0])
	}
	if extras[1]["name"] != "tests_raw" || extras[1]["value"] != "`a_test.exs` real" {
		t.Fatalf("the raw cell must still be carried: %v", extras[1])
	}
	// First-wins for a name-keyed reader: the raw cell is shadowed, and the
	// report says the cell reaches no payload verbatim.
	if got, _ := rawExtra(p, "tests_raw"); got != "LEDGER-COLUMN" {
		t.Fatalf("a name-keyed read returns %q, want the first entry", got)
	}
}
