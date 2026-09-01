package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SR-NAM-201/205: the first-party CLI ships independently from Core, so its
// production sources must move in the same contract change as the server.
func TestCLIUsesCanonicalPlanningAPI(t *testing.T) {
	roots := []string{
		".",
		filepath.Join("..", "internal", "tasks"),
		filepath.Join("..", "internal", "agents"),
		filepath.Join("..", "internal", "config"),
	}
	forbidden := []string{
		"/api/work/initiatives",
		"?initiative_id=",
		`return "initiative"`,
		`json:"initiative"`,
		`json:"initiative_`,
		`json:"stories"`,
		"InitiativeID",
		"InitiativeName",
		"InitiativeSpecs",
	}
	configReadAliases := map[string]bool{
		`json:"initiative_`: true,
		"InitiativeID":      true,
		"InitiativeName":    true,
		"InitiativeSpecs":   true,
	}

	for _, root := range roots {
		paths, err := filepath.Glob(filepath.Join(root, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, token := range forbidden {
				// config.go owns the read-only migration adapter for config files
				// written before epic_* became canonical. Config serialization is
				// separately pinned to write only the canonical keys.
				if filepath.Base(path) == "config.go" && configReadAliases[token] {
					continue
				}
				if strings.Contains(string(source), token) {
					t.Errorf("%s still contains deleted planning dialect %q", path, token)
				}
			}
		}
	}
}
