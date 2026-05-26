package config

import "testing"

func TestInitiativeSpecsDirName(t *testing.T) {
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
		if got := InitiativeSpecsDirName(tc.id, tc.title); got != tc.want {
			t.Fatalf("InitiativeSpecsDirName(%d, %q) = %q, want %q", tc.id, tc.title, got, tc.want)
		}
	}
}

func TestInitiativeSpecsRelPath(t *testing.T) {
	want := "tasks/157-simplify-roles-and-navigation"
	if got := InitiativeSpecsRelPath(157, "Simplify roles and navigation"); got != want {
		t.Fatalf("InitiativeSpecsRelPath() = %q, want %q", got, want)
	}
}

func TestResolveInitiativeSpecsRelPath(t *testing.T) {
	cfg := &Config{
		InitiativeID:       157,
		InitiativeName:     "Simplify roles and navigation",
		InitiativeSpecsDir: "specs/157-simplify-roles-and-navigation",
	}
	if got := ResolveInitiativeSpecsRelPath(cfg); got != "tasks/157-simplify-roles-and-navigation" {
		t.Fatalf("ResolveInitiativeSpecsRelPath() = %q", got)
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
