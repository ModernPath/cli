package kit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A generated file — the CLI reference the cmd package renders from its own
// command tree — is tool-owned like the embedded assets: Install writes it,
// Check reports an edited, stale or missing copy as drift, and a workspace
// installed by an older build shows the drift the moment a newer build checks
// it (tooling self-sufficiency plan C4/D3, USER:2026-09-12).
func TestInstallWritesGeneratedFilesAndCheckReportsTheirDrift(t *testing.T) {
	root := t.TempDir()
	ref := Generated{Target: ".modernpath/cli-reference.md", Body: []byte("# reference v1\n")}

	res, err := Install(root, ref)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, ref.Target))
	if err != nil {
		t.Fatalf("generated file not written: %v", err)
	}
	if string(got) != "# reference v1\n" {
		t.Fatalf("generated file body = %q", got)
	}
	found := false
	for _, w := range res.Written {
		found = found || w == ref.Target
	}
	if !found {
		t.Fatalf("Result.Written does not name the generated file: %v", res.Written)
	}

	drift, err := Check(root, ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 0 {
		t.Fatalf("a fresh install reports drift: %v", drift)
	}

	// A newer build renders different bytes: the installed copy is stale.
	newer := Generated{Target: ref.Target, Body: []byte("# reference v2\n")}
	drift, err = Check(root, newer)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 1 || !strings.HasPrefix(drift[0], ref.Target) {
		t.Fatalf("stale generated file not reported as drift: %v", drift)
	}

	// Missing entirely — a workspace installed before the file existed.
	if err := os.Remove(filepath.Join(root, ref.Target)); err != nil {
		t.Fatal(err)
	}
	drift, err = Check(root, ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 1 {
		t.Fatalf("missing generated file not reported as drift: %v", drift)
	}

	// Without a generated file passed, Check and Install behave as before.
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	if drift, err := Check(root); err != nil || len(drift) != 0 {
		t.Fatalf("plain Check after plain Install: drift=%v err=%v", drift, err)
	}
}
