package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRalphStateAcceptsLegacyEpicID(t *testing.T) {
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(".modernpath", 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"active":true,"epic_id":"legacy-task-id","task_title":"Legacy task"}`
	if err := os.WriteFile(filepath.Join(".modernpath", "ralph-state.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	state, err := loadRalphState()
	if err != nil {
		t.Fatal(err)
	}
	if state.TaskID != "legacy-task-id" {
		t.Fatalf("legacy epic_id was not loaded as task_id: got %q", state.TaskID)
	}
}

func TestShortTaskIDHandlesShortAndLongIDs(t *testing.T) {
	for input, want := range map[string]string{
		"":             "",
		"short":        "short",
		"12345678":     "12345678",
		"123456789abc": "12345678",
	} {
		if got := shortTaskID(input); got != want {
			t.Errorf("shortTaskID(%q): got %q, want %q", input, got, want)
		}
	}
}
