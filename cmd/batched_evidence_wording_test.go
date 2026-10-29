package cmd

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/kit"
)

// Epic 370: PROCESS.md and the reference loop describe current direct proof
// separately from historical modernization captures.

// batchedWordingRepoRoot is the monorepo root, four levels above this package.
var batchedWordingRepoRoot = filepath.Join("..", "..", "..", "..")

func TestPROCESSMdAndReferenceLoopDescribeCurrentDirectProof(t *testing.T) {
	read := func(t *testing.T, path string) string {
		t.Helper()
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	asset, err := fs.ReadFile(kit.Assets(), "assets/rdd/PROCESS.md")
	if err != nil {
		t.Fatal(err)
	}
	documents := []struct{ name, text string }{
		{"kit asset internal/kit/assets/rdd/PROCESS.md", string(asset)},
		{"cmd/reference-loop.md", referenceLoop},
		{"rendered CLI reference", renderCLIReference(rootCmd)},
	}
	// The installed copies exist only in the monorepo checkout. A module built
	// alone (the core image's go test stage) has the kit asset and the rendered
	// reference above; install --check covers the installed parity.
	installed := filepath.Join(batchedWordingRepoRoot, ".modernpath")
	if _, err := os.Stat(installed); err == nil {
		documents = append(documents,
			struct{ name, text string }{"installed .modernpath/rdd/PROCESS.md", read(t, filepath.Join(installed, "rdd", "PROCESS.md"))},
			struct{ name, text string }{"installed .modernpath/cli-reference.md", read(t, filepath.Join(installed, "cli-reference.md"))},
		)
	}
	required := []struct {
		rule    string
		phrases []string
	}{
		{"actual test execution", []string{"test"}},
		{"current outcome recording", []string{"current proof", "current outcome"}},
	}

	for _, document := range documents {
		t.Run(document.name, func(t *testing.T) {
			// Markdown wraps lines, so compare lowercased text with collapsed whitespace.
			text := strings.ToLower(strings.Join(strings.Fields(document.text), " "))
			for _, rule := range required {
				found := false
				for _, phrase := range rule.phrases {
					found = found || strings.Contains(text, phrase)
				}
				if !found {
					t.Errorf("%s must state the %s rule (one of %q)", document.name, rule.rule, rule.phrases)
				}
			}
		})
	}
}
