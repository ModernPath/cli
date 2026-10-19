package kit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Installed guidance records requirements directly and keeps coverage descriptive.
func TestReverseEngineerSkillCarriesCurrentCoverageAndAuthorityRules(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	skill := installedReverseEngineeringInstructions(t, root)
	for _, marker := range []string{
		"Record requirements, source file references and typed Traces atomically.",
		"New requirements start as PROPOSED.",
		"Local working-set files may hold projections and development inputs.",
		"Do not hash files, inventories, documents or reports.",
		"Missing tests do not block recording.",
	} {
		if !strings.Contains(skill, marker) {
			t.Errorf("installed reverse-engineering guidance is missing actionable rule %q", marker)
		}
	}
}

func TestInstalledCoverageReferenceDefinesInventoryAndCompletionChecks(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".claude", "skills", "rdd-reverse-engineer", "references", "coverage.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	coverage := strings.Join(strings.Fields(string(body)), " ")
	for _, marker := range []string{
		"Counts need an explicit denominator",
		"Coverage is descriptive.",
		"do not block recording grounded requirements and traces.",
		"Do not create a run, frozen inventory, hash, source-capture requirement or passing coverage threshold.",
	} {
		if !strings.Contains(coverage, marker) {
			t.Errorf("installed coverage reference is missing actionable check %q", marker)
		}
	}
}
