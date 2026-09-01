package rdd

// The WORKLIST loop-status columns are a sentence, not a token. Nine cells in
// the tracked corpus run past a hundred characters — the longest is 315 units —
// and the carrier cut every one of them at 120, keeping a head that still read
// as a complete status. These pin the whole chain: the extractor's cell, the
// epic payload built from it, and the cap the fidelity arm measures.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/opschema"
)

// epicFieldMaxLength reads one epic-payload field's declared maxLength out of
// the vendored wire contract — the artifact the lockstep tests keep byte-equal
// with contracts/sync/v1.schema.json and the server's priv copy.
func epicFieldMaxLength(t *testing.T, field string) (int, bool) {
	t.Helper()
	var doc struct {
		Defs map[string]struct {
			Properties map[string]struct {
				MaxLength *int `json:"maxLength"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(opschema.Raw(), &doc); err != nil {
		t.Fatal(err)
	}
	p, ok := doc.Defs["upsert_epic_payload"].Properties[field]
	if !ok || p.MaxLength == nil {
		return 0, false
	}
	return *p.MaxLength, true
}

// worklistRowWithStatuses renders one rollup row carrying the given cells.
func worklistRowWithStatuses(upper, lower string) string {
	return cvWorklistHeader +
		"| EPIC-CV-001 | [record](epics/EPIC-CV-001-only.md) | UR-CV-001 | SCN-CV-001 | REQ-CV-001 | T1 | " +
		upper + " | " + lower + " | IN_REVIEW | — | — |\n"
}

// The corpus's longest cell shape: an evidence sentence naming what was
// verified, where, and what remains. Past 300 units it is still nowhere near a
// pathological payload, and cutting it left the reader with the claim and
// without the qualification that changed its meaning.
const longStatusCell = "LOWER_VERIFIED at the delivered revision: the focused suite and the " +
	"boundary regression both pass on the cleaned content, the readback compares every payload " +
	"field rather than a sample of them, and the one shortfall that remains is recorded as a " +
	"named carry-forward rather than closed quietly at the gate."

func TestALongLoopStatusCellRidesWhole(t *testing.T) {
	if n := utf16Len(longStatusCell); n < 300 || n > loopStatusCap {
		t.Fatalf("fixture must be a real-sized cell under the cap, got %d units (cap %d)", n, loopStatusCap)
	}
	epics := ParseWorklist(worklistRowWithStatuses(longStatusCell, longStatusCell),
		[]string{"EPIC-CV-001-only.md"}, map[string]bool{})
	if len(epics) != 1 {
		t.Fatalf("want one rollup row, got %d", len(epics))
	}
	if epics[0].Upper != longStatusCell {
		t.Fatalf("the extractor cut the upper cell: carried %d units of %d",
			utf16Len(epics[0].Upper), utf16Len(longStatusCell))
	}
	p := BuildEpicOp(epics[0], "# EPIC-CV-001 — t\n").Payload
	if p["upper_loop_status"] != longStatusCell {
		t.Fatalf("the payload cut the upper cell: carried %d units of %d",
			utf16Len(p["upper_loop_status"].(string)), utf16Len(longStatusCell))
	}
	if p["lower_loop_status"] != longStatusCell {
		t.Fatalf("the payload cut the lower cell: carried %d units of %d",
			utf16Len(p["lower_loop_status"].(string)), utf16Len(longStatusCell))
	}
}

// The cap still exists, and it still cuts: headroom is not the absence of a
// bound. A cell past it is cut and the fidelity arm reports the cut, so the
// number stays checkable rather than becoming a claim nobody measures.
func TestTheLoopStatusCapStillCutsPastItsBound(t *testing.T) {
	over := strings.Repeat("y", loopStatusCap+40)
	epics := ParseWorklist(worklistRowWithStatuses("—", over),
		[]string{"EPIC-CV-001-only.md"}, map[string]bool{})
	if len(epics) != 1 {
		t.Fatalf("want one rollup row, got %d", len(epics))
	}
	if n := utf16Len(epics[0].Lower); n != loopStatusCap {
		t.Fatalf("a cell past the cap must be cut to it, got %d units", n)
	}
}

// The server column the cells land in must be wide enough for the cap the
// builder applies. `epics.upper_loop_status` was varchar(255) — narrower than
// the corpus's own longest cell — so raising the cap without widening the
// column would move a silent cut to a rejected batch.
func TestTheCapFitsTheDeclaredWireContract(t *testing.T) {
	for _, field := range []string{"upper_loop_status", "lower_loop_status"} {
		max, ok := epicFieldMaxLength(t, field)
		if !ok {
			t.Fatalf("the wire contract declares no maxLength for %s — the carrier's width must be stated where both builders and the server can read it", field)
		}
		if max < loopStatusCap {
			t.Fatalf("the contract declares %s at %d units while the builder carries %d", field, max, loopStatusCap)
		}
	}
}
