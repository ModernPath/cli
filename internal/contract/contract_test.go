package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// REQ-CROSS-390 — one fixture pins the served capability names on both sides:
// the embedded copy must byte-match the canonical contracts/sync file, and
// every name in it must be implemented by this build, so a rename on the
// server can never lock every binary out of a write unnoticed.

func canonicalPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "../../../../../contracts/sync/capabilities.json")
}

func TestEmbeddedCapabilitiesMatchTheCanonicalContract(t *testing.T) {
	canonical, err := os.ReadFile(canonicalPath(t))
	if err != nil {
		t.Skipf("no monorepo contracts/ sibling: %v", err)
	}
	if strings.TrimSpace(string(canonical)) != strings.TrimSpace(string(PinnedCapabilities)) {
		t.Fatal("internal/contract/capabilities.json drifted from contracts/sync/capabilities.json — edit the contracts/ file first, then copy")
	}
}

func TestEveryPinnedCapabilityIsImplemented(t *testing.T) {
	var pinned struct {
		Version      int                 `json:"version"`
		Capabilities map[string][]string `json:"capabilities"`
	}
	if err := json.Unmarshal(PinnedCapabilities, &pinned); err != nil {
		t.Fatalf("pinned fixture: %v", err)
	}
	if pinned.Version != Version {
		t.Fatalf("pinned version %d, this build speaks %d", pinned.Version, Version)
	}
	have := map[string]bool{}
	for _, name := range Implemented {
		have[name] = true
	}
	for write, names := range pinned.Capabilities {
		for _, name := range names {
			if !have[name] {
				t.Errorf("%s needs %q, which this build does not list as implemented", write, name)
			}
		}
	}
	if len(pinned.Capabilities) == 0 {
		t.Fatal("the pinned map must name at least one write")
	}
}
