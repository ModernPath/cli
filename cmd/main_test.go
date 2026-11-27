package cmd

import (
	"errors"
	"fmt"
	"os"
	"testing"
)

// TestMain gives every test in the package safe defaults for the update check
// (Epic 422): a temporary user cache folder, no terminal, an environment
// without CI and MODERNPATH_NO_UPDATE_CHECK, an unrecognized install path and a
// release lookup that nothing answers (nothing listens on port 1). A test that
// forgets to replace one of them therefore can't reach GitHub, a developer's
// real cache folder or the CI setting of the shell that runs the tests. Each
// update-check test sets what it needs, with its own cache folder.
func TestMain(m *testing.M) {
	cache, err := os.MkdirTemp("", "modernpath-cmd-test-cache-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	userCacheFolder = func() (string, error) { return cache, nil }
	streamIsTerminal = func(int) bool { return false }
	updateCheckEnv = func(string) string { return "" }
	releaseLookupURL = "http://127.0.0.1:1/repos/ModernPath/cli/releases/latest"
	runningBinaryPath = func() (string, error) { return "/nonexistent/test-install/modernpath", nil }
	resolvedHomeFolder = func() (string, error) { return "", errors.New("no home folder in tests") }
	resolvedLocalAppData = func() (string, error) { return "", errors.New("no LOCALAPPDATA in tests") }
	code := m.Run()
	_ = os.RemoveAll(cache)
	os.Exit(code)
}
