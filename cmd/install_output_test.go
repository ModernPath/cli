package cmd

import (
	"encoding/json"
	"testing"
)

func TestInstallJSONReturnsWriteReceipt(t *testing.T) {
	t.Chdir(t.TempDir())
	stdout, stderr, err := runAskStreams(t, "install", "--json")
	if err != nil {
		t.Fatalf("install --json: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var receipt map[string]any
	if err := json.Unmarshal([]byte(stdout), &receipt); err != nil {
		t.Fatalf("install stdout must be JSON only, got %q: %v", stdout, err)
	}
	if receipt["installed"] != true || receipt["written"] == nil {
		t.Fatalf("unexpected install receipt: %#v", receipt)
	}
	if stderr != "" {
		t.Fatalf("successful JSON install should not emit diagnostics: %s", stderr)
	}
}
