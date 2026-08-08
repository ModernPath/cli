package config

// REQ-CROSS-017 (EPIC-SYNC-005): current_release is the machine-readable
// release selector factory sync reads. ReadConfig hand-copies fields from the
// legacy struct — a field missing from that copy block silently drops on
// read, so the round-trip (including the legacy architecture_* path) is
// pinned here.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".modernpath"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCurrentReleaseRoundTrip(t *testing.T) {
	chdirTemp(t)

	cfg := &Config{APIURL: "http://localhost:4000", SystemID: 46543, CurrentRelease: "modernpath-v1-09"}
	if err := WriteConfig(cfg); err != nil {
		t.Fatal(err)
	}

	got, err := ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.CurrentRelease != "modernpath-v1-09" {
		t.Fatalf("current_release dropped on round-trip: got %q", got.CurrentRelease)
	}
	if got.SystemID != 46543 {
		t.Fatalf("system_id: got %d", got.SystemID)
	}
}

func TestCurrentReleaseUnsetStaysEmptyAndOmitted(t *testing.T) {
	dir := chdirTemp(t)

	if err := WriteConfig(&Config{APIURL: "http://localhost:4000", SystemID: 1}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".modernpath", ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) == "" || strings.Contains(string(raw), "current_release") {
		t.Fatalf("unset current_release must be omitted from the file, got: %s", raw)
	}

	got, err := ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.CurrentRelease != "" {
		t.Fatalf("unset current_release must read empty, got %q", got.CurrentRelease)
	}
}

func TestCurrentReleaseSurvivesLegacyArchitectureRead(t *testing.T) {
	dir := chdirTemp(t)

	legacy := `{"api_url":"http://localhost:4000","architecture_id":45,"architecture_slug":"modernpath","current_release":"modernpath-v1-09"}`
	if err := os.WriteFile(filepath.Join(dir, ".modernpath", ConfigFile), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.SystemID != 45 {
		t.Fatalf("legacy architecture_id migration broke: got %d", got.SystemID)
	}
	if got.CurrentRelease != "modernpath-v1-09" {
		t.Fatalf("current_release dropped on the legacy read path: got %q", got.CurrentRelease)
	}
}
