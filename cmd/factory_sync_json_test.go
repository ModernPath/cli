package cmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/fatih/color"
)

// B11b (regression of REQ-CROSS-210): `factory sync --no-docs --dry-run --json`
// must emit ONLY the JSON document on stdout. The --no-docs dropped-count
// banner (printInfo → color.Output → stdout) must not precede the JSON, or the
// payload the flag exists to feed to tools will not parse.
func TestSyncNoDocsDryRunJSONStdoutIsPureJSON(t *testing.T) {
	savedNoDocs, savedJSON := factorySyncNoDocs, factorySyncJSON
	defer func() { factorySyncNoDocs, factorySyncJSON = savedNoDocs, savedJSON }()
	factorySyncNoDocs = true
	factorySyncJSON = true

	root := t.TempDir()
	migrateCorpus(t, root)
	// An ADR becomes an upsert_document op (CollectDocuments), so --no-docs
	// drops >0 and the banner fires — without a document op the test would pass
	// vacuously.
	if err := os.MkdirAll(filepath.Join(root, "docs", "adr"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "adr", "0001-x.md"), []byte("# ADR-0001\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := &factoryEnv{Root: root, SystemID: 1}

	// The JSON encoder writes os.Stdout; printInfo writes color.Output. The bug
	// is a banner landing on stdout ahead of the JSON, so capture both into one
	// pipe, in order.
	r, w, _ := os.Pipe()
	savedStdout, savedColor := os.Stdout, color.Output
	os.Stdout, color.Output = w, w
	runErr := factorySyncRun(env, true)
	_ = w.Close()
	os.Stdout, color.Output = savedStdout, savedColor
	if runErr != nil {
		t.Fatalf("factorySyncRun: %v", runErr)
	}
	out, _ := io.ReadAll(r)

	if !json.Valid(out) {
		t.Fatalf("--no-docs --dry-run --json stdout is not pure JSON (banner leaked ahead of it?):\n%s", out)
	}
}
