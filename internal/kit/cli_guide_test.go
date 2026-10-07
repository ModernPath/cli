package kit

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddedCLIGuideMatchesCanonicalSource(t *testing.T) {
	root := repoRoot(t)
	if root == "" {
		t.Skip("not the kit-owning workspace")
	}

	canonical, err := os.ReadFile(filepath.Join(root, "modernpath-core", "docs", "cli.md"))
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := assets.ReadFile("assets/cli-guide/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(embedded, canonical) {
		t.Fatal("embedded CLI guide differs from modernpath-core/docs/cli.md")
	}
}
