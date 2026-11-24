package cmd

// Epic 462 (H5a), TestCase TC-H5-S12, SR ff34255d criterion e890aa40: the
// refusal glossary of the rendered CLI reference has a row keyed
// release_closed for `roadmap apply`, with and without --dry-run, when a
// set_selection names an Initiative under a released or archived Modernization
// Release and nothing was written; its remedy says the Initiative's decisions
// can no longer change and that an Initiative under an open Release can still
// take one of its legacy requirements that no approved Epic or written
// modernizes trace holds (review 3's note 10: by a move). The row for
// `roadmap accept-lower-level` stays as it is. Before the build the only
// release_closed key was inside that row, so glossaryRow finds no row keyed
// release_closed and the test fails.

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
		"this Initiative's decisions can no longer change; an Initiative under an open Release can still take one of its legacy requirements that no approved Epic or written modernizes trace holds, with a row of `move: true` in that Initiative's `set_selection`",
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
