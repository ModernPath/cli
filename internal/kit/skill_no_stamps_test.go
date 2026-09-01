package kit

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// REQ-CROSS-136: a shipped skill carries no dated provenance stamps.
//
// `RUN:<date>` and `USER:<date>` are *status* — evidence that something was
// observed or decided on a day. They belong in the ledger row and the epic
// record, which is where the process already requires them. In a skill they are
// pure cost: the file is loaded into context on every use, the date changes
// nothing about the instruction, and a rule written as "on RUN:… a pass found X"
// reads as an incident report rather than something to do.
//
// 106 of them had accumulated across the kit before this test existed.
func TestSkillsCarryNoDatedStamps(t *testing.T) {
	stamp := regexp.MustCompile(`(RUN|USER):\d{4}-\d{2}-\d{2}`)
	roots := []string{filepath.Join("assets", "skills"), filepath.Join("assets", "rdd")}

	checked := 0
	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
				return err
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			checked++
			for i, line := range strings.Split(string(body), "\n") {
				if m := stamp.FindString(line); m != "" {
					t.Errorf("%s:%d carries %s — dated provenance belongs in the ledger row, not in a prompt loaded on every use", path, i+1, m)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if checked == 0 {
		t.Fatal("no skill files walked — the check would pass vacuously")
	}
}
