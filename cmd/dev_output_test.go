package cmd

import (
	"encoding/json"
	"testing"
)

func TestDevListJSONReturnsInstalledState(t *testing.T) {
	stdout, stderr, err := runAskStreams(t, "dev", "list", "--json")
	if err != nil {
		t.Fatalf("dev list --json: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("dev list stdout must be JSON only, got %q: %v", stdout, err)
	}
	if len(rows) == 0 || rows[0]["name"] == nil || rows[0]["installed"] == nil {
		t.Fatalf("agent rows must include name and installed state: %#v", rows)
	}
	if stderr != "" {
		t.Fatalf("successful JSON list should not emit diagnostics: %s", stderr)
	}
}

func TestDevSetupJSONReturnsReceipt(t *testing.T) {
	t.Chdir(t.TempDir())
	stdout, stderr, err := runAskStreams(t, "dev", "setup", "claude", "--json")
	if err != nil {
		t.Fatalf("dev setup --json: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var receipt map[string]any
	if err := json.Unmarshal([]byte(stdout), &receipt); err != nil {
		t.Fatalf("dev setup stdout must be JSON only, got %q: %v", stdout, err)
	}
	if receipt["configured"] != true || receipt["tool"] != "claude" || receipt["config_path"] == nil {
		t.Fatalf("unexpected setup receipt: %#v", receipt)
	}
	if stderr != "" {
		t.Fatalf("successful JSON setup should not emit diagnostics: %s", stderr)
	}
}

func TestDevRunJSONRequiresSetupOnly(t *testing.T) {
	stdout, stderr, err := runAskStreams(t, "dev", "run", "claude", "task", "--json")
	if err == nil || stdout != "" || stderr == "" {
		t.Fatalf("dev run --json should fail with a clear error and no stdout; got err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
}

func TestDevSetupDoesNotClaimToConfigureUnsupportedTool(t *testing.T) {
	t.Chdir(t.TempDir())
	stdout, _, err := runAskStreams(t, "dev", "setup", "codex", "--json")
	if err == nil || stdout != "" {
		t.Fatalf("manual setup must not emit a configured receipt: stdout=%q err=%v", stdout, err)
	}
}
