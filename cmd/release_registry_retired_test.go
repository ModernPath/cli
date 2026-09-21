package cmd

// REQ-CROSS-341 (EPIC-CLI-010): no store-backed release message names the
// retired process/releases.md registry (REQ-CROSS-329 retired it — the store
// now serves the active release and its USER: source). Multi-instance by
// design: the zero-active selection line AND the `release use` success line
// both named it, so fixing one alone cannot green this.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/modernpath/cli/internal/config"
)

func TestNoStoreBackedMessageNamesTheRetiredRegistry(t *testing.T) {
	retired := []string{"process/releases.md", "the registry must hold", "registry:"}

	// (1) the zero-active selection line rendered under the store-backed read.
	zeroActive := renderSelectionBody(map[string]any{"active_release": []any{}}, nil)
	for _, bad := range retired {
		if strings.Contains(zeroActive, bad) {
			t.Errorf("zero-active selection line names the retired registry (%q):\n%s", bad, zeroActive)
		}
	}

	// (2) the `release use` success line.
	t.Chdir(t.TempDir())
	if err := config.WriteConfig(&config.Config{APIURL: "https://api.modernpath.ai"}); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	var buf bytes.Buffer
	oldOut, oldNoColor := color.Output, color.NoColor
	color.Output, color.NoColor = &buf, true
	t.Cleanup(func() { color.Output, color.NoColor = oldOut, oldNoColor })

	if err := factoryReleaseUseCmd.RunE(factoryReleaseUseCmd, []string{"modernpath-v1-09"}); err != nil {
		t.Fatalf("release use failed: %v", err)
	}
	for _, bad := range retired {
		if strings.Contains(buf.String(), bad) {
			t.Errorf("`release use` success line names the retired registry (%q):\n%s", bad, buf.String())
		}
	}
}
