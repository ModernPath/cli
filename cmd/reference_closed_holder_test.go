package cmd

// Epic 529 (E1b), TestCase 10c8319e, SR 1dd91c2f criterion 35c0268d: the CLI
// guide (modernpath-core/docs/cli.md and the kit's copy) says that a row that
// says move for a decision of an Initiative under a closed Modernization
// Release is refused with selection_row_closed, also when the row is held,
// and that the decision stays with the closed Release; and the refusal
// glossary of the rendered CLI reference has a row keyed selection_row_closed
// whose clearing says the decision stays with that Release while it stays
// closed. Before the build neither text had the code, so both checks fail.

import (
	"os"
	"strings"
	"testing"
)

// 10c8319e-0e63-4701-8fd2-309a28ebd258
func TestE1b_CLIGuideSaysAMoveCannotTakeADecisionOfAClosedRelease(t *testing.T) {
	sentence := "A row that says move for a decision of an Initiative under a closed Modernization Release is refused with selection_row_closed, also when the row is held; the decision stays with the closed Release."
	for _, path := range []string{"../../../docs/cli.md", "../internal/kit/assets/cli-guide/cli.md"} {
		body, err := os.ReadFile(path)
		// docs/cli.md lies outside this module; the core image tests the module
		// alone. Its identical kit copy is always checked.
		if os.IsNotExist(err) && path == "../../../docs/cli.md" {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(c1FoldedText(string(body)), sentence) {
			t.Errorf("%s does not say %q", path, sentence)
		}
	}
}

// 10c8319e-0e63-4701-8fd2-309a28ebd258
func TestE1b_GlossaryHasARowForSelectionRowClosed(t *testing.T) {
	loop := renderedLoop()
	cells := strings.Split(strings.Trim(glossaryRow(t, loop, "selection_row_closed"), "| "), " | ")
	want := []string{
		"`selection_row_closed`",
		"server 422",
		"`roadmap apply`, with or without `--dry-run`, for a `set_selection` row with `move: true` for a decision of an Initiative stored in a released or archived Modernization Release, also when the row is held; nothing was written",
		"the decision stays with that Release while it stays closed",
	}
	if len(cells) != len(want) {
		t.Fatalf("the selection_row_closed row has %d cells, want %d: %q", len(cells), len(want), cells)
	}
	for i := range want {
		if cells[i] != want[i] {
			t.Errorf("cell %d of the selection_row_closed row:\n got %q\nwant %q", i, cells[i], want[i])
		}
	}
}
