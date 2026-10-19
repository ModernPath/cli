package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/fatih/color"
)

// cobraWorkspace binds a temporary workspace with the current native CLI
// config shape for tests that exercise the root command.
func cobraWorkspace(t *testing.T, srv *httptest.Server, boundSystemID ...int) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Join(dir, ".modernpath"), 0o755); err != nil {
		t.Fatal(err)
	}
	apiURL := srv.URL
	systemID := 1
	if len(boundSystemID) > 0 {
		systemID = boundSystemID[0]
	}
	config := `{"api_url":"` + apiURL + `","system_id":` + strconv.Itoa(systemID) + `,"system_name":"T","system_slug":"t"}`
	if err := os.WriteFile(filepath.Join(dir, ".modernpath", "config.json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".modernpath", "auth.json"), []byte(`{"token":"t"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
}

func captureOut(t *testing.T, fn func()) string {
	t.Helper()
	oldOut, oldErr, oldColorOut, oldColorErr := os.Stdout, os.Stderr, color.Output, color.Error
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr, color.Output, color.Error = w, w, w, w
	read := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(r)
		read <- data
	}()
	defer func() {
		os.Stdout, os.Stderr, color.Output, color.Error = oldOut, oldErr, oldColorOut, oldColorErr
	}()
	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return string(<-read)
}

func runRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	resetTreeFlags(rootCmd)
	rootCmd.SetArgs(args)
	defer func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	}()
	var err error
	out := captureOut(t, func() {
		rootCmd.SetOut(os.Stdout)
		rootCmd.SetErr(os.Stderr)
		beginCommandRun(args)
		err = rootCmd.Execute()
		finishCommandRun(err)
	})
	return out, err
}

func healthyProbe(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `[{"id":1,"name":"T"}]`)
}

// nativeCLIEnv writes current config/auth files and loads them through the
// production credential loader, so tests exercise the native workspace shape.
func nativeCLIEnv(t *testing.T, srv *httptest.Server, systemID int) *cliEnv {
	t.Helper()
	cobraWorkspace(t, srv, systemID)
	env, err := cliCredentialLoad()
	if err != nil {
		t.Fatal(err)
	}
	return env
}
