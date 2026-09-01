// REQ-CROSS-029: install writes what the workspace must ignore.
package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddToGitignoreKeepsProcessPackageVersioned(t *testing.T) {
	root := t.TempDir()
	gitignore := filepath.Join(root, ".gitignore")
	before := "node_modules/\n\n# old ModernPath rule\n.modernpath/\n"
	if err := os.WriteFile(gitignore, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := addToGitignore(root); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(gitignore)
	if err != nil {
		t.Fatal(err)
	}
	text := string(first)
	for _, want := range []string{
		"node_modules/",
		"/.modernpath/*",
		"!/.modernpath/rdd/",
		"!/.modernpath/rdd/**",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("updated .gitignore is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "\n.modernpath/\n") {
		t.Fatalf("broad legacy rule still hides the process package:\n%s", text)
	}

	if err := addToGitignore(root); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(gitignore)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("normalizing .gitignore must be idempotent")
	}
}
