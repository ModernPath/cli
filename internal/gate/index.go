package gate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// stateIndexDirs are the FLAT state directories whose README is a catalogue of
// what lives there. `epics/` is deliberately absent: its README is a convention
// document, not a list of 160 records, and requiring it to name them would make
// the rule noise on its first run.
var stateIndexDirs = []string{"process", "tasks"}

// CheckStateIndexes — REQ-CROSS-134: a state directory's README names every
// document in it.
//
// Three indexes were found stale in three days (RUN:2026-08-14): tasks/README.md
// listed 9 of 14 ledgers, process/README.md 4 of 7 files including the release
// registry, PROCESS.md §9 four of eight skills. Each rotted the same way — an
// index rots in the direction nothing contradicts, because no reader misses a
// row that is absent, and nothing else in the corpus disagrees with an omission.
func CheckStateIndexes(root string) ([]Violation, error) {
	var out []Violation
	for _, dir := range stateIndexDirs {
		readme := filepath.Join(root, dir, "README.md")
		body, err := os.ReadFile(readme)
		if os.IsNotExist(err) {
			continue // an index that does not exist makes no claim
		}
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			return nil, err
		}
		text := string(body)
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || name == "README.md" || !strings.HasSuffix(name, ".md") {
				continue
			}
			if strings.Contains(text, name) {
				continue
			}
			out = append(out, Violation{
				Rule:    "unlisted-state-file",
				File:    filepath.Join(dir, "README.md"),
				Subject: filepath.Join(dir, name),
				Detail:  fmt.Sprintf("%s exists but %s/README.md does not name it — an index rots in the direction nothing contradicts", name, dir),
			})
		}
	}
	return out, nil
}
