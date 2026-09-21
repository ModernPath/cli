package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// GAP-013: after the authority flip the tracked ledgers are retired, so
// `modernpath coverage` has no tasks/*-REQUIREMENTS.md to measure. It must
// stand down (like `modernpath check`), not fail as though the workspace were
// misconfigured — coverage then lives in the server store.
func TestCoverageStandsDownForStoreBackedWorkspace(t *testing.T) {
	saved := coverageRoot
	defer func() { coverageRoot = saved }()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "process"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "process", "store-backed.md"), []byte("# Store-backed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	coverageRoot = root
	if err := runCoverage(coverageCmd, nil); err != nil {
		t.Fatalf("runCoverage on a store-backed workspace with no ledgers: got %v; want nil (stand-down)", err)
	}
}

// The stand-down must be narrow: no marker means a genuine "no ledgers here"
// error (e.g. run from the wrong directory), never a silent pass.
func TestCoverageStillErrorsWithoutMarker(t *testing.T) {
	saved := coverageRoot
	defer func() { coverageRoot = saved }()

	coverageRoot = t.TempDir() // no ledgers, no marker
	if err := runCoverage(coverageCmd, nil); err == nil {
		t.Fatal("runCoverage with no ledgers and no marker: got nil; want an error (not a store-backed stand-down)")
	}
}

// The stand-down returned before the --json branch and printed prose, so
// `modernpath coverage --json` on a store-backed workspace emitted no JSON
// document at all — and the onboarding contract tells agents to read
// `summary.citations.*` from exactly that output. --json owns stdout
// (REQ-CROSS-121): the stand-down must be a document too, saying so.
func TestCoverageStandDownHonoursJSON(t *testing.T) {
	savedRoot, savedJSON := coverageRoot, coverageJSON
	defer func() { coverageRoot, coverageJSON = savedRoot, savedJSON }()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "process"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "process", "store-backed.md"), []byte("# Store-backed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	coverageRoot, coverageJSON = root, true

	var err error
	out := captureOut(t, func() { err = runCoverage(coverageCmd, nil) })
	if err != nil {
		t.Fatalf("stand-down must exit clean: %v", err)
	}
	var doc map[string]any
	if jerr := json.Unmarshal([]byte(out), &doc); jerr != nil {
		t.Fatalf("--json must own stdout with one JSON document, got %q: %v", out, jerr)
	}
	if doc["store_backed"] != true {
		t.Fatalf("the document must say the workspace is store-backed, got %v", doc)
	}
	if _, has := doc["reason"]; !has {
		t.Fatalf("the document must carry the stand-down reason, got %v", doc)
	}
}
