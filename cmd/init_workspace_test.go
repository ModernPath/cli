package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/modernpath/cli/internal/api"
)

func TestDiscoverGitRepos(t *testing.T) {
	base := t.TempDir()

	for _, name := range []string{"alpha", "beta", ".hidden", "not-git"} {
		dir := filepath.Join(base, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	if err := os.MkdirAll(filepath.Join(base, "alpha", ".git"), 0o755); err != nil {
		t.Fatalf("mkdir alpha git: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(base, "beta", ".git"), 0o755); err != nil {
		t.Fatalf("mkdir beta git: %v", err)
	}

	repos, err := discoverGitRepos(base)
	if err != nil {
		t.Fatalf("discoverGitRepos failed: %v", err)
	}

	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}

	names := map[string]bool{repos[0].Name: true, repos[1].Name: true}
	if !names["alpha"] || !names["beta"] {
		t.Fatalf("unexpected repo names: %#v", repos)
	}
}

func TestMatchLocalRepoFolder(t *testing.T) {
	local := []localGitRepo{
		{Name: "modernpath-platform", Path: "modernpath-platform"},
		{Name: "modernpath-webapp", Path: "modernpath-webapp"},
	}

	tests := []struct {
		name   string
		member api.WorkspaceMember
		want   string
	}{
		{
			name:   "exact slug",
			member: api.WorkspaceMember{Slug: "modernpath-platform", FullName: "ModernPath/modernpath-platform"},
			want:   "modernpath-platform",
		},
		{
			name:   "github full name tail",
			member: api.WorkspaceMember{Slug: "webapp", FullName: "ModernPath/modernpath-webapp"},
			want:   "modernpath-webapp",
		},
		{
			name:   "missing",
			member: api.WorkspaceMember{Slug: "missing", FullName: "org/missing"},
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchLocalRepoFolder(local, tt.member)
			if got != tt.want {
				t.Fatalf("matchLocalRepoFolder() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFilterUnifiedWorkspaceSystems(t *testing.T) {
	systems := []api.System{
		{ID: 1, Name: "Single", AnalysisMode: "independent_repos"},
		{ID: 2, Name: "Workspace", AnalysisMode: "unified_workspace"},
		{ID: 3, Name: "Legacy workspace", WorkspaceMembers: []api.WorkspaceMember{{Slug: "a"}}},
	}

	filtered := filterUnifiedWorkspaceSystems(systems)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 workspace systems, got %d", len(filtered))
	}
}
