package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemapExportPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "flattens module angle markdown",
			in:   ".modernpath/my-app/architecture/configuration/modules/core-config/code_structure/code-structure-core-config-configuration.md",
			want: ".modernpath/my-app/architecture/configuration/modules/core-config/code-structure-core-config-configuration.md",
		},
		{
			name: "flattens module overview-angle markdown",
			in:   ".modernpath/my-app/architecture/configuration/modules/core-config/overview/overview-core-config-configuration.md",
			want: ".modernpath/my-app/architecture/configuration/modules/core-config/overview-core-config-configuration.md",
		},
		{
			name: "keeps module overview in place",
			in:   ".modernpath/my-app/architecture/configuration/modules/core-config/overview.md",
			want: ".modernpath/my-app/architecture/configuration/modules/core-config/overview.md",
		},
		{
			name: "keeps non-module paths unchanged",
			in:   ".modernpath/my-app/architecture/configuration/code_structure/code-structure-application-configuration.md",
			want: ".modernpath/my-app/architecture/configuration/code_structure/code-structure-application-configuration.md",
		},
		{
			name: "keeps deeper nested paths unchanged",
			in:   ".modernpath/my-app/architecture/configuration/modules/core-config/code_structure/extra/nested.md",
			want: ".modernpath/my-app/architecture/configuration/modules/core-config/code_structure/extra/nested.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := remapExportPath(tt.in)
			if got != tt.want {
				t.Fatalf("remapExportPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCleanSystemExport(t *testing.T) {
	base := t.TempDir()
	target := base + "/.modernpath/my-app/architecture/foo.md"

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := cleanSystemExport(base, "my-app"); err != nil {
		t.Fatalf("cleanSystemExport failed: %v", err)
	}

	if _, err := os.Stat(base + "/.modernpath/my-app"); !os.IsNotExist(err) {
		t.Fatalf("expected cleaned system subtree to be removed")
	}
}
