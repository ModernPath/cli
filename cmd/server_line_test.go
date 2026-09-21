package cmd

// REQ-CROSS-417 (EPIC-CLI-021) — lower RED. `factory status` and `status`
// print one server line naming the store revision the bound server serves
// and the contract it advertises, or why not, inside a 2 s bound, with no
// write. BACKLOG-TOOL-1: after a merge a session can tell whether production
// serves it without attempting a write.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modernpath/cli/internal/api"
)

func stubSystemsRead(t *testing.T) {
	t.Helper()
	saved := listSystemsFn
	listSystemsFn = func(apiURL, token string) ([]api.System, error) { return nil, errStub }
	t.Cleanup(func() { listSystemsFn = saved })
}

func runBothStatus(t *testing.T) (string, string) {
	t.Helper()
	fout, err := runCapturing(t, func() error { return factoryStatusCmd.RunE(factoryStatusCmd, nil) })
	if err != nil {
		t.Fatalf("factory status: %v\n%s", err, fout)
	}
	sout, err := runCapturing(t, func() error { return runStatus(statusCmd, nil) })
	if err != nil {
		t.Fatalf("status: %v\n%s", err, sout)
	}
	return fout, sout
}

func TestFactoryStatusPrintsServerLine(t *testing.T) {
	stubSystemsRead(t)
	fx := &wsFixture{storeRevision: "abc123", contractVersion: 1, workSelection: map[string]any{"current": nil}}
	statusWorkspace(t, wsServe(t, fx))
	fout, sout := runBothStatus(t)
	want(t, fout, "server:    store abc123 · contract 1")
	want(t, sout, "store abc123 · contract 1")
	for _, r := range fx.requests {
		if !strings.HasPrefix(r, "GET ") {
			t.Fatalf("a status read must not write; saw %s", r)
		}
	}
}

func TestServerLineSaysWhenTheRevisionIsNotServed(t *testing.T) {
	stubSystemsRead(t)
	statusWorkspace(t, wsServe(t, &wsFixture{contractVersion: 1, workSelection: map[string]any{"current": nil}}))
	fout, sout := runBothStatus(t)
	want(t, fout, "server:    store revision not served · contract 1")
	want(t, sout, "store revision not served · contract 1")
}

func TestServerLineIsBoundedWhenTheServerHangs(t *testing.T) {
	stubSystemsRead(t)
	fx := &wsFixture{storeRevision: "abc123", contractVersion: 1, contractDelay: 6 * time.Second, workSelection: map[string]any{"current": nil}}
	statusWorkspace(t, wsServe(t, fx))
	start := time.Now()
	fout, sout := runBothStatus(t)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("two status verbs took %v against a hanging contract read; each is bounded at 2 s", elapsed)
	}
	want(t, fout, "server:    not reachable (")
	want(t, sout, "not reachable (")
}

func TestServerLineNamesTheRefusedConnection(t *testing.T) {
	stubSystemsRead(t)
	srv := wsServe(t, &wsFixture{})
	statusWorkspace(t, srv)
	srv.Close()
	fout, sout := runBothStatus(t)
	want(t, fout, "server:    not reachable (")
	want(t, sout, "not reachable (")
}

func TestServerLineNamesTheSignInRemedyWhenUnsigned(t *testing.T) {
	stubSystemsRead(t)
	root := statusWorkspace(t, wsServe(t, &wsFixture{storeRevision: "abc123", contractVersion: 1}))
	if err := os.Remove(filepath.Join(root, ".modernpath", "auth.json")); err != nil {
		t.Fatal(err)
	}
	fout, sout := runBothStatus(t)
	want(t, fout, "server:    not signed in — `modernpath auth login`")
	want(t, sout, "not signed in — `modernpath auth login`")
}
