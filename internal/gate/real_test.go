package gate

import (
	"os"
	"path/filepath"
	"testing"
)

// REQ-CROSS-030: the process gates, exercised against real ledgers rather than
// fixtures — a gate that only passes on synthetic input proves nothing.
//
// Runs the gate against this workspace's own ledgers when pointed at it:
//
//	MP_GATE_ROOT=/path/to/workspace go test ./internal/gate -run Real -v
func TestRealWorkspaceLedgers(t *testing.T) {
	root := os.Getenv("MP_GATE_ROOT")
	if root == "" {
		t.Skip("set MP_GATE_ROOT to check a real workspace")
	}
	found, err := CheckLedgers(root)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "tasks"))
	t.Logf("checked %d files in tasks/", len(entries))
	for _, v := range found {
		t.Errorf("%s", v)
	}
	if len(found) == 0 {
		t.Log("all ledgers consistent")
	}
}

func TestRealWorkspaceApprovals(t *testing.T) {
	root := os.Getenv("MP_GATE_ROOT")
	if root == "" {
		t.Skip("set MP_GATE_ROOT")
	}
	found, err := CheckApprovals(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d DONE epics lack a sourced approval", len(found))
	for i, v := range found {
		if i < 12 {
			t.Logf("  %s", v.Detail)
		}
	}
}
