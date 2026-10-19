package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHooksStatusJSONIsACompactRecord(t *testing.T) {
	t.Chdir(t.TempDir())
	stdout, stderr, err := runAskStreams(t, "hooks", "status", "--json")
	if err != nil {
		t.Fatalf("hooks status --json: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("hooks status stdout must be JSON only, got %q: %v", stdout, err)
	}
	if result["initialized"] != false || len(result["agents"].([]any)) != 4 {
		t.Fatalf("unexpected status record: %#v", result)
	}
	if stderr != "" {
		t.Fatalf("successful JSON status should not emit diagnostics: %s", stderr)
	}
}

func TestHooksInstallJSONReturnsReceipt(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".modernpath"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".modernpath", "config.json"), []byte(`{"system_id":7}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	stdout, stderr, err := runAskStreams(t, "hooks", "install", "--all", "--json")
	if err != nil {
		t.Fatalf("hooks install --json: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("hooks install stdout must be JSON only, got %q: %v", stdout, err)
	}
	installed, installedOK := result["installed"].([]any)
	failed, failedOK := result["failed"].([]any)
	if !installedOK || !failedOK || len(installed) != 4 || len(failed) != 0 {
		t.Fatalf("unexpected install receipt: %#v", result)
	}
	if stderr != "" {
		t.Fatalf("successful JSON install should not emit diagnostics: %s", stderr)
	}
}
