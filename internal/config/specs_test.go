package config

import "testing"

func TestEpicSpecsDirName(t *testing.T) {
	cases := []struct {
		id    int
		title string
		want  string
	}{
		{157, "Simplify roles and navigation", "157-simplify-roles-and-navigation"},
		{169, "OAuth login", "169-oauth-login"},
		{42, "", "42"},
	}

	for _, tc := range cases {
		if got := EpicSpecsDirName(tc.id, tc.title); got != tc.want {
			t.Fatalf("EpicSpecsDirName(%d, %q) = %q, want %q", tc.id, tc.title, got, tc.want)
		}
	}
}

func TestEpicSpecsRelPath(t *testing.T) {
	want := "tasks/157-simplify-roles-and-navigation"
	if got := EpicSpecsRelPath(157, "Simplify roles and navigation"); got != want {
		t.Fatalf("EpicSpecsRelPath() = %q, want %q", got, want)
	}
}

func TestResolveEpicSpecsRelPath(t *testing.T) {
	cfg := &Config{
		EpicID:       157,
		EpicName:     "Simplify roles and navigation",
		EpicSpecsDir: "specs/157-simplify-roles-and-navigation",
	}
	if got := ResolveEpicSpecsRelPath(cfg); got != "tasks/157-simplify-roles-and-navigation" {
		t.Fatalf("ResolveEpicSpecsRelPath() = %q", got)
	}
}

func TestNormalizeEpicWorkspaceRelPath(t *testing.T) {
	if got := NormalizeEpicWorkspaceRelPath("specs/157-simplify-roles-and-navigation"); got != "tasks/157-simplify-roles-and-navigation" {
		t.Fatalf("NormalizeEpicWorkspaceRelPath() = %q", got)
	}
	if got := NormalizeEpicWorkspaceRelPath("tasks/157-simplify-roles-and-navigation"); got != "tasks/157-simplify-roles-and-navigation" {
		t.Fatalf("NormalizeEpicWorkspaceRelPath() = %q", got)
	}
}
