package cmd

// UR-CLI-021 (EPIC-CLI-021) — upper RED, one test per acceptance scenario,
// run through the fixture server. Opening a session, the first thing read
// answers: do I hold work and what moved on it (or what to pick up), is the
// tool current, and which server revision is production serving. Reads only.
//
// SCN-STALE-001..005 run through the freshness seams of REQ-CROSS-416.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modernpath/cli/internal/api"
)

var errStub = errors.New("stub: no systems read")

func heldPieceFixture(scope, phase string, extra map[string]any) map[string]any {
	p := map[string]any{
		"scope_external_id":      scope,
		"scope_kind":             "epic",
		"phase":                  phase,
		"derived_phase":          phase,
		"lane":                   "",
		"taken_at":               "2026-09-19T10:00:00Z",
		"sections_written_since": []any{},
		"members_added":          []any{},
		"members_removed":        []any{},
		"entry_pin_behind":       false,
		"gates_answered_since":   []any{},
	}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

func briefFor(t *testing.T, fx *wsFixture, sid string) (string, *factoryEnv) {
	t.Helper()
	env := wsEnv(t, wsServe(t, fx))
	var out bytes.Buffer
	runBriefHook("SessionStart", strings.NewReader(`{"session_id":"`+sid+`","source":"startup"}`), &out, 2*time.Second, hookLoader(env))
	return out.String(), env
}

// SCN-BRIEF-001 — idle: no active piece → "nothing in flight" leads, then the picks.
func TestSCNBRIEF001IdleLeadsWithNothingInFlightThenThePicks(t *testing.T) {
	out, _ := briefFor(t, &wsFixture{feed: feedFixture()}, "idle")
	if !strings.Contains(strings.ToLower(out), "nothing in flight") {
		t.Fatalf("an idle brief must say nothing is in flight:\n%s", out)
	}
	if strings.Index(strings.ToLower(out), "nothing in flight") > strings.Index(out, "1. [Decision] RQ-268") {
		t.Fatalf("the idle line leads the picks:\n%s", out)
	}
}

// SCN-BRIEF-002 — a section written or the member set changed after the
// selection was taken: the brief names the piece, the section and the member.
func TestSCNBRIEF002HeldPieceNamesSectionsAndMembersThatMoved(t *testing.T) {
	out, _ := briefFor(t, &wsFixture{feed: feedFixture(), held: []any{
		heldPieceFixture("EPIC-X-001", "plan", map[string]any{
			"sections_written_since": []any{"reconnaissance"},
			"members_added":          []any{"REQ-X-002"},
		}),
	}}, "moved")
	for _, needle := range []string{"EPIC-X-001", "reconnaissance", "REQ-X-002"} {
		want(t, out, needle)
	}
}

// SCN-BRIEF-003 — a gate on the held scope answered after the selection was
// taken: the brief surfaces the gate and its answer.
func TestSCNBRIEF003HeldPieceSurfacesTheGateAnsweredWhileAway(t *testing.T) {
	out, _ := briefFor(t, &wsFixture{feed: feedFixture(), held: []any{
		heldPieceFixture("EPIC-X-001", "entry", map[string]any{
			"gates_answered_since": []any{map[string]any{"external_id": "ENTRY-EPIC-X-001", "answer": "approve", "answered_at": "2026-09-19T11:00:00Z"}},
		}),
	}}, "gate")
	want(t, out, "ENTRY-EPIC-X-001")
	want(t, out, "approve")
}

// SCN-BRIEF-004 — the packet aggregate advanced past the applied entry pin:
// the brief warns that the in-flight build is behind its pin, naming the piece.
func TestSCNBRIEF004HeldPieceWarnsWhenTheBuildIsBehindItsEntryPin(t *testing.T) {
	out, _ := briefFor(t, &wsFixture{feed: feedFixture(), held: []any{
		heldPieceFixture("EPIC-X-001", "build", map[string]any{"entry_pin_behind": true}),
	}}, "pin")
	want(t, out, "EPIC-X-001")
	want(t, out, "behind its entry pin")
}

// SCN-BRIEF-005 — reads only: no write reaches the store and the only files
// touched are the brief log and the release cache under .modernpath/
// (USER:2026-09-19). The deadline test in yourmove_hook_test.go stays the
// bound's regression gate.
func TestSCNBRIEF005TheBriefWritesNoStoreRecordAndNoTrackedFile(t *testing.T) {
	fx := &wsFixture{feed: feedFixture(), held: []any{heldPieceFixture("EPIC-X-001", "build", nil)}}
	_, env := briefFor(t, fx, "ro")
	for _, r := range fx.requests {
		if !strings.HasPrefix(r, "GET ") {
			t.Fatalf("the brief must only read; saw %s", r)
		}
	}
	allowed := map[string]bool{
		filepath.Join(".modernpath", briefLogName):          true,
		filepath.Join(".modernpath", "release-latest.json"): true,
	}
	_ = filepath.Walk(env.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(env.Root, path)
		if !allowed[rel] {
			t.Errorf("the brief touched %s", rel)
		}
		return nil
	})
}

// SCN-SERVED-001 — both status verbs name the store revision production is
// serving and the contract it advertises, without any write.
func TestSCNSERVED001StatusVerbsNameTheServedRevisionAndContract(t *testing.T) {
	saved := listSystemsFn
	listSystemsFn = func(apiURL, token string) ([]api.System, error) { return nil, errStub }
	t.Cleanup(func() { listSystemsFn = saved })

	fx := &wsFixture{storeRevision: "abc123", contractVersion: 1, workSelection: map[string]any{"current": nil}}
	statusWorkspace(t, wsServe(t, fx))

	out, err := runCapturing(t, func() error { return factoryStatusCmd.RunE(factoryStatusCmd, nil) })
	if err != nil {
		t.Fatalf("factory status: %v\n%s", err, out)
	}
	want(t, out, "server:    store abc123 · contract 1")

	out, err = runCapturing(t, func() error { return runStatus(statusCmd, nil) })
	if err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	want(t, out, "store abc123 · contract 1")
	for _, r := range fx.requests {
		if !strings.HasPrefix(r, "GET ") {
			t.Fatalf("status must only read; saw %s", r)
		}
	}
}

// SCN-STALE-001 — the running build is below the latest published release:
// one freshness line says so and names the upgrade.
func TestSCNSTALE001BriefSaysTheBuildIsBehindTheLatestRelease(t *testing.T) {
	pinFreshnessFresh(t)
	fx := &wsFixture{feed: feedFixture(), contractVersion: 1}
	env := wsEnv(t, wsServe(t, fx))
	writeReleaseCache(t, env.Root, "v9.9.9", freshnessNow())
	var out bytes.Buffer
	runBriefHook("SessionStart", strings.NewReader(`{"session_id":"rel","source":"startup"}`), &out, 2*time.Second, hookLoader(env))
	want(t, out.String(), "behind the latest release")
	want(t, out.String(), "9.9.9")
	want(t, out.String(), "upgrade")
	want(t, readBriefLog(t, env.Root), "freshness:release")
}

// SCN-STALE-002 — the build's stamped commit is an ancestor of, not equal to,
// the last commit touching the CLI source: the line names install-local.sh.
func TestSCNSTALE002BriefSaysTheBuildIsBehindTheCheckedOutSource(t *testing.T) {
	pinFreshnessFresh(t)
	freshnessGit = stubGit(t, "modernpath-core/tools/modernpath/go.mod", strings.Repeat("b", 40), "", errors.New("exit 1"))
	env := wsEnv(t, wsServe(t, &wsFixture{feed: feedFixture(), contractVersion: 1}))
	var out bytes.Buffer
	runBriefHook("SessionStart", strings.NewReader(`{"session_id":"src","source":"startup"}`), &out, 2*time.Second, hookLoader(env))
	want(t, out.String(), "behind the checked-out CLI source")
	want(t, out.String(), "install-local.sh")
}

// SCN-STALE-003 — install --check reports drifted tool-owned files: the line
// names the kit drift and modernpath install.
func TestSCNSTALE003BriefSaysTheKitDrifted(t *testing.T) {
	pinFreshnessFresh(t)
	freshnessKitCheck = func(root string) ([]string, error) { return []string{".modernpath/rdd/PROCESS.md"}, nil }
	env := wsEnv(t, wsServe(t, &wsFixture{feed: feedFixture(), contractVersion: 1}))
	if err := os.MkdirAll(filepath.Join(env.Root, ".modernpath", "rdd"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	runBriefHook("SessionStart", strings.NewReader(`{"session_id":"kit","source":"startup"}`), &out, 2*time.Second, hookLoader(env))
	want(t, out.String(), "drifted")
	want(t, out.String(), "modernpath install")
}

// SCN-STALE-004 — the server advertises a newer sync contract: the line says
// so and names rebuild and install.
func TestSCNSTALE004BriefSaysTheServerSpeaksANewerContract(t *testing.T) {
	pinFreshnessFresh(t)
	env := wsEnv(t, wsServe(t, &wsFixture{feed: feedFixture(), contractVersion: 2}))
	var out bytes.Buffer
	runBriefHook("SessionStart", strings.NewReader(`{"session_id":"ctr","source":"startup"}`), &out, 2*time.Second, hookLoader(env))
	want(t, out.String(), "sync contract 2")
	want(t, out.String(), "rebuild")
}

// SCN-STALE-005 — everything current, or the release lookup offline or
// hanging: no line, and the hook is not delayed beyond one second by it.
func TestSCNSTALE005FreshOrOfflineIsSilentAndQuick(t *testing.T) {
	pinFreshnessFresh(t)
	env := wsEnv(t, wsServe(t, &wsFixture{feed: feedFixture(), contractVersion: 1}))
	var out bytes.Buffer
	runBriefHook("SessionStart", strings.NewReader(`{"session_id":"fresh","source":"startup"}`), &out, 2*time.Second, hookLoader(env))
	if strings.Contains(out.String(), "⚠") {
		t.Fatalf("a current build prints no freshness line:\n%s", out.String())
	}
	want(t, readBriefLog(t, env.Root), "freshness:fresh")

	releaseLookupURL = releaseServer(t, 200, "v9.9.9", 3*time.Second)
	env = wsEnv(t, wsServe(t, &wsFixture{feed: feedFixture(), contractVersion: 1}))
	start := time.Now()
	out.Reset()
	runBriefHook("SessionStart", strings.NewReader(`{"session_id":"hang","source":"startup"}`), &out, 5*time.Second, hookLoader(env))
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Fatalf("a hanging release lookup delayed the brief by %v", elapsed)
	}
	want(t, out.String(), "## Your move —")
	if strings.Contains(out.String(), "⚠") {
		t.Fatalf("a hanging lookup is silent:\n%s", out.String())
	}
}
