package kit

import (
	"os"
	"path/filepath"
	"testing"
)

// Every skill in assets/ is installable. A skill the CLI does not write is a
// skill that exists only in the repository that authored it.
func TestEverySkillIsInInstallTargets(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("assets", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join("assets", "skills", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			key := "assets/skills/" + e.Name() + "/" + f.Name()
			seen++
			if _, ok := installTargets[key]; !ok {
				t.Errorf("%s is not in installTargets — `modernpath install` will not write it", key)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no skill assets walked — the check would pass vacuously")
	}
}
