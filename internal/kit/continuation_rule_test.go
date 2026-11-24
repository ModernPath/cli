package kit

import (
	"os"
	"strings"
	"testing"
)

// Epic 458 (H1a), SR-2 criterion 7 (3efc2157): the delivery process text the
// CLI installs, and the copy installed in this workspace, say that an
// Initiative stays in its stored Release and that a close continues a partly
// done Initiative in a linked Initiative of the next Modernization Release,
// and neither says any more that an Initiative is shown under the earliest
// open Release of its Epics or that its Epics are free to span several
// Releases. The sentence ends in the owner's wording of 23 Nov 2026: the
// linked Initiative takes the Epics carried to that Release and the decisions
// not yet proved, and a close is refused when no Release can take them. The
// installed copy lies outside this module, so the module tested alone checks
// the kit copy only.
//
// 7a7cf88a-9fe1-4959-b865-3c5e34d096f1
func TestProcessStatesTheContinuationRule(t *testing.T) {
	texts := map[string]string{"kit PROCESS.md": roadmapText(t, "assets/rdd/PROCESS.md")}
	installed, err := os.ReadFile("../../../../../.modernpath/rdd/PROCESS.md")
	switch {
	case err == nil:
		plain := strings.NewReplacer("`", "", "*", "", "’", "'", "→", "->").Replace(string(installed))
		texts["installed PROCESS.md"] = strings.ToLower(strings.Join(strings.Fields(plain), " "))
	case !os.IsNotExist(err):
		t.Fatal(err)
	}

	for name, text := range texts {
		for _, phrase := range []string{
			"an initiative stays in its stored release",
			"a close continues a partly done initiative in a linked initiative of the next modernization release",
			"the linked initiative takes the epics carried to that release and the decisions that are not yet proved. a close is refused when no release can take them.",
		} {
			if !strings.Contains(text, phrase) {
				t.Errorf("%s is missing %q", name, phrase)
			}
		}
		for _, phrase := range []string{
			"the earliest open release of its epics",
			"span several releases",
			"takes its epics and decisions that are not finished",
		} {
			if strings.Contains(text, phrase) {
				t.Errorf("%s still says %q", name, phrase)
			}
		}
	}
}
