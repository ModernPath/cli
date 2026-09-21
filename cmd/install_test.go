package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/kit"
)

// REQ-CROSS-406 — `install --store-backed --source USER:…` declares a bound
// workspace with no file ledgers store-backed in one step: the server records
// the activation gate born answered and sets the state active in one action;
// the CLI then writes the marker through the writer `migrate flip` uses.

const declareSource = "USER:2026-09-14:declare"

// declareServer fakes the two surfaces the declaration touches: the store-backed
// state read, and the authoring call. The state it serves moves to active when
// the action lands, so the CLI's "already declared" read is observable.
type declareServer struct {
	*httptest.Server
	mu          sync.Mutex
	state       string // "" is nil
	gateRef     string
	sourceTag   string
	authorPosts []map[string]any
	// refuse, when set, answers the authoring call with this 409 reason.
	refuse string
}

func newDeclareServer(t *testing.T) *declareServer {
	t.Helper()
	d := &declareServer{gateRef: "GATE-STORE-BACKED"}
	mux := http.NewServeMux()
	respond := func(w http.ResponseWriter, code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("/api/v1/sync/store-backed", func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		if r.Method != http.MethodGet {
			respond(w, http.StatusMethodNotAllowed, map[string]any{"error": "the declaration goes through the authoring call"})
			return
		}
		if d.state == "" {
			respond(w, 200, map[string]any{"data": map[string]any{"process_store": map[string]any{}}})
			return
		}
		respond(w, 200, map[string]any{"data": map[string]any{"process_store": map[string]any{
			"state": d.state, "gate_ref": d.gateRef, "source_tag": d.sourceTag,
		}}})
	})
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		d.mu.Lock()
		defer d.mu.Unlock()
		d.authorPosts = append(d.authorPosts, body)
		if d.refuse != "" {
			respond(w, http.StatusConflict, map[string]any{"error": map[string]any{"reason": d.refuse}})
			return
		}
		source, _ := body["source"].(string)
		changed := d.state != "active"
		if changed {
			d.state, d.sourceTag = "active", source
		}
		respond(w, 200, map[string]any{"data": map[string]any{"store_backed_declaration": map[string]any{
			"state": "active", "gate_ref": d.gateRef, "source_tag": d.sourceTag, "changed": changed,
		}}})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusNotFound, map[string]any{"error": "not found: " + r.URL.Path})
	})
	d.Server = httptest.NewServer(mux)
	t.Cleanup(d.Close)
	return d
}

func (d *declareServer) posts() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.authorPosts)
}

// declareWorkspace enters a bound, signed-in temp workspace with no ledgers and
// resets the install flags. The reachability probe is stubbed to the system.
func declareWorkspace(t *testing.T, srv *declareServer) string {
	t.Helper()
	root := chdirTemp(t)
	writeFactoryTestFile(t, root, ".modernpath/config.json", fmt.Sprintf(`{"api_url":%q,"system_id":7}`, srv.URL))
	writeInitAuth(t, root, "declare-token", time.Now().Add(time.Hour))

	saved := listSystemsFn
	listSystemsFn = func(string, string) ([]api.System, error) { return []api.System{{ID: 7}}, nil }
	savedStore, savedSource, savedCheck, savedDry := installStoreBacked, installSource, installCheck, installDryRun
	installStoreBacked, installSource, installCheck, installDryRun = true, "", false, false
	t.Cleanup(func() {
		listSystemsFn = saved
		installStoreBacked, installSource, installCheck, installDryRun = savedStore, savedSource, savedCheck, savedDry
	})
	return root
}

// goldenMarker is the marker `migrate flip` writes for an empty retired list —
// the one shape both paths share (C2). Anchored as bytes, not as a call to
// the shared writer.
func goldenMarker(apiURL string) string {
	return "# Store-backed declaration\n\n" +
		"This workspace's process store is the server. The files below are\n" +
		"retired: read state via `modernpath working-set pull` and `your-move`,\n" +
		"write via `modernpath author`. The dual-authority guard\n" +
		"(scripts/check-store-backed.sh) gates on this list.\n\n" +
		"- **Accepted:** " + declareSource + " (gate GATE-STORE-BACKED)\n" +
		"- **Server:** " + apiURL + " · system 7\n\n"
}

func readMarker(t *testing.T, root string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "process", "store-backed.md"))
	if os.IsNotExist(err) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b), true
}

// S1 — refused without a USER: source, nothing sent; with one, the action is
// posted, the marker is written in the shared shape, and the install that
// follows withholds the ledger skill.
func TestInstallStoreBackedDeclaresInOneStep(t *testing.T) {
	srv := newDeclareServer(t)
	root := declareWorkspace(t, srv)

	out, err := runCapturing(t, func() error { return installCmd.RunE(installCmd, nil) })
	if err == nil || !strings.Contains(err.Error(), "--source") {
		t.Fatalf("the declaration must refuse without a USER: source naming the flag, got: %v\n%s", err, out)
	}
	if srv.posts() != 0 {
		t.Fatalf("a refused declaration must send nothing, got %d authoring call(s)", srv.posts())
	}
	if _, ok := readMarker(t, root); ok {
		t.Fatal("a refused declaration must write no marker")
	}

	installSource = declareSource
	out, err = runCapturing(t, func() error { return installCmd.RunE(installCmd, nil) })
	if err != nil {
		t.Fatalf("install --store-backed: %v\n%s", err, out)
	}
	if srv.posts() != 1 {
		t.Fatalf("one authoring call expected, got %d", srv.posts())
	}
	post := srv.authorPosts[0]
	if post["action"] != "store_backed_declare" || post["source"] != declareSource {
		t.Fatalf("the action must be store_backed_declare with the source, got %v", post)
	}
	marker, ok := readMarker(t, root)
	if !ok {
		t.Fatalf("the marker must be written\n%s", out)
	}
	if marker != goldenMarker(srv.URL) {
		t.Fatalf("marker must be the flip's shape with an empty retired list:\n--- got ---\n%s\n--- want ---\n%s", marker, goldenMarker(srv.URL))
	}
	if _, err := os.Stat(filepath.Join(root, kit.LedgerSkillTarget)); !os.IsNotExist(err) {
		t.Fatalf("the install after the declaration must withhold the ledger skill (stat err %v)", err)
	}
	if !strings.Contains(out, "store-backed") {
		t.Fatalf("the output must say the workspace is now store-backed:\n%s", out)
	}
}

// S3 — with both halves done, nothing changes and the output says so.
func TestInstallStoreBackedIsANoOpWhenAlreadyDeclared(t *testing.T) {
	srv := newDeclareServer(t)
	root := declareWorkspace(t, srv)
	installSource = declareSource
	srv.state, srv.sourceTag = "active", declareSource
	writeFactoryTestFile(t, root, "process/store-backed.md", goldenMarker(srv.URL))

	out, err := runCapturing(t, func() error { return installCmd.RunE(installCmd, nil) })
	if err != nil {
		t.Fatalf("install --store-backed on a declared workspace: %v\n%s", err, out)
	}
	if srv.posts() != 0 {
		t.Fatalf("an already-declared workspace must post nothing, got %d", srv.posts())
	}
	if !strings.Contains(out, "already declared") {
		t.Fatalf("the output must say the workspace is already declared:\n%s", out)
	}
	marker, _ := readMarker(t, root)
	if marker != goldenMarker(srv.URL) {
		t.Fatalf("the marker must be left as it was:\n%s", marker)
	}
}

// S4 — a hand-written marker over a nil server state: the server half is
// completed and the output says which half.
func TestInstallStoreBackedCompletesTheServerHalfBehindAHandWrittenMarker(t *testing.T) {
	srv := newDeclareServer(t)
	root := declareWorkspace(t, srv)
	installSource = declareSource
	handWritten := "# Store-backed declaration\n\n- **Accepted:** USER:by-hand\n"
	writeFactoryTestFile(t, root, "process/store-backed.md", handWritten)

	out, err := runCapturing(t, func() error { return installCmd.RunE(installCmd, nil) })
	if err != nil {
		t.Fatalf("install --store-backed behind a hand-written marker: %v\n%s", err, out)
	}
	if srv.posts() != 1 {
		t.Fatalf("the server half must be completed with one authoring call, got %d", srv.posts())
	}
	if !strings.Contains(out, "server half") {
		t.Fatalf("the output must say the server half was completed:\n%s", out)
	}
	marker, _ := readMarker(t, root)
	if marker != handWritten {
		t.Fatalf("the hand-written marker must be left in place:\n%s", marker)
	}
}

// S4, the other half — an active server state with no marker: only the marker
// is written, from what the server serves, and the output says which half.
func TestInstallStoreBackedCompletesTheWorkspaceHalfBehindAnActiveState(t *testing.T) {
	srv := newDeclareServer(t)
	root := declareWorkspace(t, srv)
	installSource = declareSource
	srv.state, srv.sourceTag = "active", declareSource

	out, err := runCapturing(t, func() error { return installCmd.RunE(installCmd, nil) })
	if err != nil {
		t.Fatalf("install --store-backed with an active state and no marker: %v\n%s", err, out)
	}
	if srv.posts() != 0 {
		t.Fatalf("an active state must post nothing, got %d", srv.posts())
	}
	if !strings.Contains(out, "workspace half") {
		t.Fatalf("the output must say the workspace half was completed:\n%s", out)
	}
	marker, ok := readMarker(t, root)
	if !ok || marker != goldenMarker(srv.URL) {
		t.Fatalf("the marker must be written from the served declaration:\n%s", marker)
	}
}

// S5 — a tracked ledger present: the flip's path applies, nothing is sent.
func TestInstallStoreBackedRefusesWithLedgersPresent(t *testing.T) {
	srv := newDeclareServer(t)
	root := declareWorkspace(t, srv)
	installSource = declareSource
	writeFactoryTestFile(t, root, "tasks/CROSS-REQUIREMENTS.md", "| REQ-CROSS-001 | a row | MVP | TODO |\n")

	out, err := runCapturing(t, func() error { return installCmd.RunE(installCmd, nil) })
	if err == nil || !strings.Contains(err.Error(), "migrate flip") {
		t.Fatalf("ledgers present must refuse naming migrate flip, got: %v\n%s", err, out)
	}
	if srv.posts() != 0 {
		t.Fatalf("a refused declaration must send nothing, got %d", srv.posts())
	}
	if _, ok := readMarker(t, root); ok {
		t.Fatal("a refused declaration must write no marker")
	}
}

// S1b — the server's refusal (seeded, cleared) is surfaced verbatim and no
// marker is written.
func TestInstallStoreBackedSurfacesTheServerRefusal(t *testing.T) {
	srv := newDeclareServer(t)
	root := declareWorkspace(t, srv)
	installSource = declareSource
	srv.refuse = "this system was seeded by an import — declare it through `migrate flip`"

	out, err := runCapturing(t, func() error { return installCmd.RunE(installCmd, nil) })
	if err == nil || !strings.Contains(err.Error(), "migrate flip") {
		t.Fatalf("the server's refusal must be surfaced, got: %v\n%s", err, out)
	}
	if _, ok := readMarker(t, root); ok {
		t.Fatal("a refused declaration must write no marker")
	}
}

// Nit 13 — --store-backed is a declaration; combined with --dry-run or
// --check it is refused rather than silently ignored.
func TestInstallStoreBackedRefusesDryRunAndCheck(t *testing.T) {
	srv := newDeclareServer(t)
	declareWorkspace(t, srv)
	installSource = declareSource
	for _, flag := range []string{"dry-run", "check"} {
		t.Run(flag, func(t *testing.T) {
			installDryRun, installCheck = flag == "dry-run", flag == "check"
			t.Cleanup(func() { installDryRun, installCheck = false, false })
			_, err := runCapturing(t, func() error { return installCmd.RunE(installCmd, nil) })
			if err == nil || !strings.Contains(err.Error(), "--"+flag) {
				t.Fatalf("--store-backed with --%s must refuse naming the flag, got: %v", flag, err)
			}
			if srv.posts() != 0 {
				t.Fatalf("a refused combination must send nothing")
			}
		})
	}
}
