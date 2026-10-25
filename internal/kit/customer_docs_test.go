package kit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCustomerDocumentationOmitsRepositoryInternals(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "upgrade"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			const customerInstructions = "# Customer instructions\n\nRun make check.\n"
			if upgrade {
				legacy := customerInstructions + "\n" + BeginMarker + "\nModernPath/modernpath-v1 internal/kit/assets/rdd/\n" + EndMarker + "\n"
				for _, target := range []string{"AGENTS.md", "CLAUDE.md"} {
					if err := os.WriteFile(filepath.Join(root, target), []byte(legacy), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			result, err := Install(root)
			if err != nil {
				t.Fatal(err)
			}
			files := append(result.Written, result.Created...)
			files = append(files, result.Merged...)
			for _, file := range files {
				body, err := os.ReadFile(filepath.Join(root, file))
				if err != nil {
					t.Fatal(err)
				}
				assertCustomerDocumentation(t, file, string(body))
			}
			guide, err := assets.ReadFile("assets/cli-guide/cli.md")
			if err != nil {
				t.Fatal(err)
			}
			assertCustomerDocumentation(t, "bundled CLI guide", string(guide))
			if upgrade {
				for _, target := range []string{"AGENTS.md", "CLAUDE.md"} {
					body, err := os.ReadFile(filepath.Join(root, target))
					if err != nil || !strings.HasPrefix(string(body), customerInstructions) {
						t.Fatalf("customer instructions changed in %s: %v", target, err)
					}
				}
			}
		})
	}
}

func assertCustomerDocumentation(t *testing.T, name, body string) {
	t.Helper()
	for _, internal := range []string{
		"ModernPath/modernpath-v1", "req-driven-dev", "modernpath-core/",
		"tools/modernpath/", "internal/kit/", "apps/core/",
		"Contributor commands:", "../../docs/features/",
	} {
		if strings.Contains(body, internal) {
			t.Errorf("%s exposes repository internals: %q", name, internal)
		}
	}
}
