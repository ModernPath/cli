package cmd

// Epic 462 (H5a), TestCase TC-H5-S12, SR ff34255d criterion e890aa40: the
// refusal glossary of the rendered CLI reference has a row keyed
// release_closed for `roadmap apply`, with and without --dry-run, when a
// set_selection names an Initiative under a released or archived Modernization
// Release and nothing was written; its remedy says the Initiative's decisions
// can no longer change and that another Initiative cannot take them with
// `move: true`, because they stay with the closed Release (Epic 529, E1b: the
// product owner's word of 4 Nov 2026 excludes the move the remedy offered
// before). The row for `roadmap accept-lower-level` stays as it is. Before
// Epic 529 the remedy offered the move, so cell 4 fails.

import (
	"strings"
	"testing"
)

const h5AcceptLowerLevelRow = "| `proof_changed` / `proof_not_below` / `release_closed` | server 422 | `roadmap accept-lower-level` for a row whose levels differ from the file's, a row proved as agreed or not proved yet, or an Initiative under a closed Release; nothing was written | read the row again with `roadmap proof --initiative <id> --json` and send the levels it gives; only a row below its agreed level, under an open Release or none, takes an acceptance |"

// f064eb86-4f7f-413b-9ae4-07f85859bd43
func TestH5_GlossaryNamesReleaseClosedForRoadmapApplysSetSelection(t *testing.T) {
	loop := renderedLoop()

	// SR ff34255d criterion e890aa40: the row, cell by cell.
	cells := strings.Split(strings.Trim(glossaryRow(t, loop, "release_closed"), "| "), " | ")
	want := []string{
		"`release_closed`",
		"server 422",
		"`roadmap apply`, with or without `--dry-run`, for a `set_selection` that names an Initiative under a released or archived Modernization Release; nothing was written",
		"this Initiative's decisions can no longer change, and another Initiative cannot take them with `move: true`: they stay with the closed Release",
	}
	if len(cells) != len(want) {
		t.Fatalf("the release_closed row has %d cells, want %d: %q", len(cells), len(want), cells)
	}
	for i := range want {
		if cells[i] != want[i] {
			t.Errorf("cell %d of the release_closed row:\n got %q\nwant %q", i, cells[i], want[i])
		}
	}

	// The row for roadmap accept-lower-level stays as it is.
	if got := glossaryRow(t, loop, "proof_changed"); got != h5AcceptLowerLevelRow {
		t.Errorf("the accept-lower-level row changed:\n got %q\nwant %q", got, h5AcceptLowerLevelRow)
	}
}
