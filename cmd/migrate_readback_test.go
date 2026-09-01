package cmd

// REQ-CROSS-224 — readback hardening: the import proves what it wrote, for
// every op type it wrote and every field it sent.
//
// The old verification asked two questions: does every emitted id read back,
// and do four named fields of ONE op type still match. Everything else — every
// epic field, every gate field, every backlog field, and documents entirely —
// was written and never read. These tests pin the widened arm the same way the
// original was pinned: a fixture that breaks exactly one thing must fail the
// run naming it, and an intact fixture must stay quiet.

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/rdd"
)

// The byte archive is emitted by every run and must read back like every
// other kind (§225.6 successor). An archived file the store does not serve
// must fail the run naming it.
func TestMigrateRunFailsWhenAnArchivedFileDoesNotReadBack(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}, dropServe: "WORKLIST.md"}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	err := migrateRun(env)
	if err == nil || !strings.Contains(err.Error(), "WORKLIST.md") {
		t.Fatalf("an archived file that does not read back must fail the run naming it, got: %v", err)
	}
}

// Presence is not content. The archive surface serves content_sha256 — the
// sha of the stored bytes — so a file whose stored bytes are not what was
// sent is caught without shipping the bytes back.
func TestMigrateRunFailsWhenArchivedBytesDoNotMatch(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}, mangleArchiveSha: "WORKLIST.md"}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	err := migrateRun(env)
	if err == nil || !strings.Contains(err.Error(), "WORKLIST.md") {
		t.Fatalf("an archive whose stored bytes differ must fail the run naming it, got: %v", err)
	}
	if !strings.Contains(err.Error(), "content_sha256") {
		t.Fatalf("the failure must name the sha mismatch — the byte identity: %v", err)
	}
}

// The archive entry's revision is store-owned. Under REQ-CROSS-253's
// first-seen identity the row records the revision where its bytes FIRST
// appeared, while every later run's op carries the current revision — so
// comparing the two reports the corpus and the store disagreeing about
// something the corpus is not allowed to say, on every run, forever. That is
// the same category as a frozen gate answer, and it gets the same treatment.
// Measured on the localhost store before this exemption: 241 of 245 archive
// rows mismatched on this field alone, which is the whole of the run's
// process_record verification failure.
func TestMigrateRunTreatsAFirstSeenArchiveRevisionAsStoreOwned(t *testing.T) {
	st := &migrateStore{
		hashes:      map[string]string{},
		mangleField: [3]string{"WORKLIST.md", "archive_revision", "an-earlier-revision"},
	}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	commitCorpus(t, env.Root)

	if err := migrateRun(env); err != nil {
		t.Fatalf("a first-seen archive_revision is store-owned, not a mismatch: %v", err)
	}
}

// REQ-CROSS-253 keeps one archive generation per distinct content, so once a
// file has changed the surface serves several rows under one external_id.
// "The store serves back what the corpus sent" then means the sent bytes are
// among them — comparing against whichever row the served list happened to end
// on reports a mismatch against a different, equally valid generation. Live on
// the localhost store this was the whole residue after the frozen-revision
// exemption: 233 of 237 files verified, and the four failures were exactly the
// four that legitimately carry three versions each.
func TestMigrateRunVerifiesAgainstTheMatchingArchiveGeneration(t *testing.T) {
	st := &migrateStore{
		hashes:                 map[string]string{},
		extraArchiveGeneration: "WORKLIST.md",
	}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	commitCorpus(t, env.Root)

	if err := migrateRun(env); err != nil {
		t.Fatalf("an older generation beside the current one is history, not a mismatch: %v", err)
	}
}

// ...and the exemption is conditional on the bytes agreeing. A row whose sha
// does NOT match what was sent is a real problem, and a differing revision
// must not buy it silence — otherwise the exemption would hide exactly the
// loss the archive exists to prevent.
func TestMigrateRunStillFailsWhenBytesDifferEvenIfTheRevisionAlsoDiffers(t *testing.T) {
	st := &migrateStore{
		hashes:           map[string]string{},
		mangleArchiveSha: "WORKLIST.md",
		mangleField:      [3]string{"WORKLIST.md", "archive_revision", "an-earlier-revision"},
	}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	commitCorpus(t, env.Root)

	err := migrateRun(env)
	if err == nil || !strings.Contains(err.Error(), "content_sha256") {
		t.Fatalf("a byte mismatch must still fail naming content_sha256: %v", err)
	}
}

// The archive revision is read from the corpus's own git history, so a
// fixture with no repository never carries the field and cannot exercise it —
// the first version of these two tests passed for exactly that reason. Only
// the tests that need the field pay for the repository; adding it to the
// shared fixture would start emitting a commit-burst op and move every other
// test's counts.
func commitCorpus(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "fixture@example.invalid"},
		{"config", "user.name", "fixture"},
		{"add", "-A"},
		{"-c", "commit.gpgsign=false", "commit", "-q", "-m", "corpus fixture"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

// REQ-CROSS-255: a read surface that cannot be reached leaves every emitted id
// of that type unverified, and a verification that could not be completed is a
// FAILURE of the run — never a warning under a success line. The disclosure
// half of the old rule survives (the loop still finishes every other surface);
// what does not survive is "not fatal": one unreachable surface unverified
// ~1,310 ids on the real corpus while the run printed `✓ verified` to stdout
// and exited 0, so a piped consumer saw only the success.
//
// The surface here is served by no handler at all — the deployment that serves
// none of a required read surface. It fails loudly naming it; there is no
// optional-surface skip.
func TestMigrateRunFailsWhenAReadSurfaceCannotBeReached(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}, noDocumentSurface: true}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	said := captureCLIOutput(t)
	err := migrateRun(env)
	if err == nil {
		t.Fatal("a read surface that cannot be reached must fail the run")
	}
	if !strings.Contains(err.Error(), "documents") {
		t.Fatalf("the refusal must name the surface it could not reach, got: %v", err)
	}
	if !strings.Contains(err.Error(), "UNVERIFIED") {
		t.Fatalf("the refusal must state that the emitted ids of that type are unverified, got: %v", err)
	}
	// The half that matters to a piped consumer: the total-verification claim
	// must be absent, not merely contradicted on stderr.
	if strings.Contains(said(), "every emitted id reads back") {
		t.Fatalf("a run that could not reach a surface must not print the total-verification success line:\n%s", said())
	}
}

// The accumulate-don't-abort half, and the arm that forces the shape: two
// unreachable surfaces are named in ONE refusal, and the per-surface verify
// lines for the surfaces that WERE reachable still appear — proof the loop ran
// to completion instead of returning on the first failure. A one-line `return`
// at the swallow site satisfies the headline arm above and fails this one.
func TestMigrateRunNamesEveryUnreachableSurfaceInOneRefusal(t *testing.T) {
	st := &migrateStore{
		hashes:          map[string]string{},
		offlineSurfaces: map[string]bool{"documents": true, "backlog": true},
	}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	said := captureCLIOutput(t)
	err := migrateRun(env)
	if err == nil {
		t.Fatal("two unreachable read surfaces must fail the run")
	}
	for _, want := range []string{"documents", "backlog"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("one refusal must name every unreachable surface (missing %q): %v", want, err)
		}
	}
	for _, want := range []string{"verify epics:", "verify requirements:", "verify gates:"} {
		if !strings.Contains(said(), want) {
			t.Fatalf("the reachable surfaces must still be verified — the loop finishes, it does not abort (missing %q):\n%s", want, said())
		}
	}
}

// "Could not be checked" and "did not read back" are different facts, and a fix
// that dumps every id of an unreachable type into the missing list produces a
// technically-failing but misleading refusal. The wording has to keep them
// apart.
func TestMigrateRunSaysUnverifiedRatherThanMissingForAnUnreachableSurface(t *testing.T) {
	st := &migrateStore{
		hashes:          map[string]string{},
		offlineSurfaces: map[string]bool{"requirements": true},
	}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	said := captureCLIOutput(t)
	err := migrateRun(env)
	if err == nil {
		t.Fatal("an unreachable requirements surface must fail the run")
	}
	if !strings.Contains(err.Error(), "requirements") || !strings.Contains(err.Error(), "UNVERIFIED") {
		t.Fatalf("the refusal must name the surface and state its ids are unverified, got: %v", err)
	}
	if strings.Contains(err.Error(), "did not read back") {
		t.Fatalf("ids behind an unreachable surface were not checked — reporting them as not-read-back is a different, false claim: %v", err)
	}
	if strings.Contains(said(), "every emitted id reads back") {
		t.Fatalf("the total-verification line must be absent when the requirements surface went unread:\n%s", said())
	}
}

// A run refused by verification has written its batch (that is the honest state
// and it was already true), but it must claim nothing: no store-backed seed, no
// historical evidence, and no durable run record.
func TestMigrateRunRefusedByVerificationRecordsNothing(t *testing.T) {
	st := &migrateStore{
		hashes:          map[string]string{},
		offlineSurfaces: map[string]bool{"gates": true},
	}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	if err := migrateRun(env); err == nil {
		t.Fatal("the fixture must refuse for this to mean anything")
	}
	if len(st.declarations) != 0 {
		t.Fatalf("a run that could not verify must not seed the store-backed declaration: %v", st.declarations)
	}
	if len(st.evidenceRuns) != 0 {
		t.Fatalf("a run that could not verify must post no historical evidence and no run record, got %d", len(st.evidenceRuns))
	}
}

// The positive: every surface reachable, behavior unchanged — the success line
// prints and states how many surfaces it actually reached, so the claim is
// checkable rather than asserted.
func TestMigrateRunPrintsTheSuccessLineWhenEverySurfaceIsReached(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	said := captureCLIOutput(t)
	if err := migrateRun(env); err != nil {
		t.Fatalf("an intact fixture must verify: %v", err)
	}
	if !strings.Contains(said(), "every emitted id reads back") {
		t.Fatalf("a fully reached verification must still print the success line:\n%s", said())
	}
	if !strings.Contains(said(), "read surface(s) reached") {
		t.Fatalf("the run must state how many read surfaces it reached:\n%s", said())
	}
}

// The full-field arm, on a type the old four-field spot check never looked at:
// an epic field served with a changed value fails the run naming id and field.
func TestMigrateRunFailsOnEpicFieldMismatch(t *testing.T) {
	st := &migrateStore{
		hashes:      map[string]string{},
		mangleField: [3]string{"EPIC-MR-001", "process_status", "DONE"},
	}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	err := migrateRun(env)
	if err == nil || !strings.Contains(err.Error(), "EPIC-MR-001.process_status") {
		t.Fatalf("an epic field the store no longer serves back must fail naming id and field, got: %v", err)
	}
}

// The same arm on the backlog type: nothing about the check is
// requirement-specific, and a per-type field list is what let epics, gates and
// backlog records go unverified in the first place.
func TestMigrateRunFailsOnBacklogFieldMismatch(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	// The backlog id is content-derived, so it is discovered from the batch
	// rather than written down — a hard-coded id would pin a hash, not a rule.
	if err := migrateRun(env); err != nil {
		t.Fatalf("baseline run must pass: %v", err)
	}
	var backlogID string
	for k := range st.hashes {
		if strings.HasPrefix(k, "upsert_backlog_record|") {
			backlogID = strings.SplitN(k, "|", 2)[1]
		}
	}
	if backlogID == "" {
		t.Fatal("the fixture corpus must emit a backlog record")
	}
	st.mangleField = [3]string{backlogID, "title", "MANGLED"}

	err := migrateRun(env)
	if err == nil || !strings.Contains(err.Error(), backlogID+".title") {
		t.Fatalf("a backlog field mismatch must fail naming id and field, got: %v", err)
	}
}

// A field no read surface serves is UNVERIFIED, which is not the same as
// verified — and not a failure either. Reporting it as a mismatch would make
// the run unusable against a server whose read surface is thinner than its
// write surface, which is exactly the situation documents are in.
func TestMigrateRunTreatsUnservedFieldsAsUnverifiedNotFailed(t *testing.T) {
	st := &migrateStore{
		hashes:    map[string]string{},
		dropField: [2]string{"EPIC-MR-001", "process_status"},
	}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	if err := migrateRun(env); err != nil {
		t.Fatalf("a field the surface does not serve must not fail the run: %v", err)
	}
}

// A store-born row is not a corpus record gone missing. The document surface
// is tenant-wide and carries analysis output too; the reverse direction must
// report only rows this import could have written.
func TestMigrateRunDoesNotReportAnalysisDocumentsAsCorpusRows(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	ops, _, err := env.workspaceOps()
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateRun(env); err != nil {
		t.Fatal(err)
	}
	_, storeOnly, err := migrateVerifyIDs(env, ops, migrateGateStatesBeforeRun(env))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range storeOnly {
		if id == "analysis-born-document" {
			t.Fatal("a document born of analysis must not be reported as a store-only corpus record")
		}
	}
}

// The epics surface serves scenario items without position and spec items as
// metadata only, so the payload's nested keys `scenarios[].position`,
// `specs[].position` and `specs[].content_md` reach no served value at all.
// Failing on that makes the check unusable against the only server there is;
// passing quietly on it claims a verification that never happened. So: the run
// passes, and names every nested field it could not check.
func TestMigrateRunDisclosesNestedFieldsNoReadSurfaceServes(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	said := captureCLIOutput(t)
	if err := migrateRun(env); err != nil {
		t.Fatalf("a nested field no served element carries must not fail the run: %v", err)
	}
	for _, want := range []string{
		"upsert_epic.scenarios[].position",
		"upsert_epic.specs[].position",
		"upsert_epic.specs[].content_md",
	} {
		if !strings.Contains(said(), want) {
			t.Fatalf("the unverified summary must name %q:\n%s", want, said())
		}
	}
	// Disclosed as UNVERIFIED, in the summary that says so — not merely
	// mentioned somewhere in the run's chatter.
	if !strings.Contains(said(), "not served by any read surface") {
		t.Fatalf("the nested skips must ride the unverified summary:\n%s", said())
	}
}

// The other side of the population rule, at fixture scale: the surface serves
// position on the first scenario item and on no other. It CAN serve the field,
// and for the rest it did not — a loss, and the run fails naming it.
func TestMigrateRunFailsWhenOnlySomeServedItemsCarryANestedField(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}, keepFirstScenarioPosition: "EPIC-MR-001"}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	err := migrateRun(env)
	if err == nil || !strings.Contains(err.Error(), "EPIC-MR-001.scenarios") {
		t.Fatalf("a nested field only some served items carry must fail naming id and field, got: %v", err)
	}
}

// And the value itself. Whatever the disclosure excuses, it never excuses a
// served value that is not what was sent — a scenario whose text the store
// changed still fails, three levels down.
func TestMigrateRunFailsWhenANestedValueIsDropped(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}, mangleFirstScenarioText: "EPIC-MR-001"}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	err := migrateRun(env)
	if err == nil || !strings.Contains(err.Error(), "EPIC-MR-001.scenarios") {
		t.Fatalf("a nested value the store no longer serves back must fail naming id and field, got: %v", err)
	}
}

// An empty payload string is not content. Ecto casts "" away before it reaches
// a column, so the store answers with null — and comparing "" against null as a
// difference reports a loss where nothing was ever there to lose. The rule has
// to be exact: an EMPTY string against null round-trips, and the next test
// proves a non-empty one still does not.
func TestMigrateRunTreatsAnEmptyPayloadStringServedAsNullAsRoundTripped(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}, serveNull: [2]string{"REQ-MR-001", "description"}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	// The fixture row carries no detail block, so its description really is the
	// empty string — asserted, because a payload that stopped sending the field
	// would make this test pass while proving nothing.
	ops, _, err := env.workspaceOps()
	if err != nil {
		t.Fatal(err)
	}
	if got := payloadFieldOf(t, ops, "REQ-MR-001", "description"); got != "" {
		t.Fatalf("the fixture must send an EMPTY description for this to mean anything, got %q", got)
	}

	if err := migrateRun(env); err != nil {
		t.Fatalf("an empty payload string served back as null must round-trip: %v", err)
	}
}

// The negative arm, and the one that keeps the rule from being a hole: a
// NON-empty string served as null is content the store did not keep, and the
// run still fails naming it.
func TestMigrateRunStillFailsWhenANonEmptyStringIsServedAsNull(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}, serveNull: [2]string{"REQ-MR-001", "title"}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	ops, _, err := env.workspaceOps()
	if err != nil {
		t.Fatal(err)
	}
	if got := payloadFieldOf(t, ops, "REQ-MR-001", "title"); got == "" {
		t.Fatal("the fixture must send a NON-empty title for this to mean anything")
	}

	err = migrateRun(env)
	if err == nil || !strings.Contains(err.Error(), "REQ-MR-001.title") {
		t.Fatalf("a non-empty string served as null must fail naming id and field, got: %v", err)
	}
}

// The same equivalence where the field is absent rather than null. A served row
// that simply omits the key is the other shape a never-written column takes,
// and an empty payload string is not evidence of anything to verify there — so
// it is neither a mismatch nor an unverified field to disclose.
func TestMigrateRunDoesNotDiscloseAnEmptyPayloadStringAsUnverified(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}, dropField: [2]string{"REQ-MR-001", "description"}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	said := captureCLIOutput(t)
	if err := migrateRun(env); err != nil {
		t.Fatalf("an absent served field whose payload value is empty must not fail: %v", err)
	}
	if strings.Contains(said(), "upsert_requirement.description") {
		t.Fatalf("an empty payload string is nothing to verify — it must not be counted as an unverified field:\n%s", said())
	}
}

// The store owns an answer once it has one: Core.Sync's S6 rule drops the whole
// answer block for a gate whose stored state is already "answered", so the repo
// cannot reopen or rewrite one. Those fields are therefore UNWRITABLE, not lost
// — comparing them reports the corpus and the store disagreeing about something
// the corpus is not allowed to say. They are skipped, and the skip is named.
//
// REQ-CROSS-257 sharpens WHICH gates that covers: the ones the store ALREADY
// held answered before this run, not the ones this run wrote itself. The
// fixture therefore pre-seeds the gate as an earlier import's row.
func TestMigrateRunSkipsTheFrozenAnswerBlockOfAGateTheStoreHeldAnswered(t *testing.T) {
	st := &migrateStore{
		hashes:           map[string]string{},
		preAnsweredGates: []string{"APPROVE-EPIC-MR-001"},
		mangleField:      [3]string{"APPROVE-EPIC-MR-001", "answer", "the answer the SERVER holds"},
	}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	migrateCorpusAnsweredApproval(t, env.Root)

	said := captureCLIOutput(t)
	if err := migrateRun(env); err != nil {
		t.Fatalf("a frozen answer block must not fail the run: %v", err)
	}
	if !strings.Contains(said(), "upsert_gate.answer[frozen]") {
		t.Fatalf("the unverified summary must name the frozen skip class:\n%s", said())
	}
}

// REQ-CROSS-257 — the from-empty import is self-proving. The freeze exists
// because the write path physically cannot rewrite an answer the store already
// held; it says nothing about an answer block the run itself just wrote. Keying
// the skip on the SERVED state made even a from-empty run skip comparing the
// answers it wrote one moment earlier — the one field family the wipe-and-
// reimport decision is about, unverified by construction.
//
// So: an empty store, a corpus gate carrying an answered block, and a served
// answer field that is not what was sent must FAIL the run naming gate and
// field.
func TestMigrateRunComparesTheAnswerBlockOfAGateItWroteItself(t *testing.T) {
	st := &migrateStore{
		hashes:      map[string]string{},
		mangleField: [3]string{"APPROVE-EPIC-MR-001", "answered_at", "1999-01-01T00:00:00Z"},
	}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	migrateCorpusAnsweredApproval(t, env.Root)

	err := migrateRun(env)
	if err == nil {
		t.Fatal("an answer block this run wrote itself must be compared, not frozen-skipped")
	}
	if !strings.Contains(err.Error(), "APPROVE-EPIC-MR-001.answered_at") {
		t.Fatalf("the failure must name the gate and the field, got: %v", err)
	}
}

// A from-empty run is a claim about the target, and the claim is checkable
// before anything is written. A target still holding process rows for the
// system refuses, naming what it found — the answer blocks would hit the
// server's freeze and the run would prove nothing.
func TestMigrateRunFromEmptyRefusesAPopulatedTarget(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	if err := migrateRun(env); err != nil {
		t.Fatalf("the target must be populated for this to mean anything: %v", err)
	}
	batchesBefore := len(st.applied)

	migrateRunFromEmpty = true
	t.Cleanup(func() { migrateRunFromEmpty = false })

	err := migrateRun(env)
	if err == nil {
		t.Fatal("a from-empty run against a populated target must refuse")
	}
	for _, want := range []string{"requirements", "epics", "from-empty"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must name the counts it found (missing %q): %v", want, err)
		}
	}
	if len(st.applied) != batchesBefore {
		t.Fatalf("the refusal must come before any write, got %d new batch(es)", len(st.applied)-batchesBefore)
	}
}

// And the positive, or the precondition would be a way to never import: a
// cleared target takes the from-empty run.
func TestMigrateRunFromEmptyProceedsAgainstAClearedTarget(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	migrateRunFromEmpty = true
	t.Cleanup(func() { migrateRunFromEmpty = false })

	if err := migrateRun(env); err != nil {
		t.Fatalf("a cleared target must take the from-empty run: %v", err)
	}
	if len(st.applied) < 2 {
		t.Fatalf("the from-empty run must still apply and prove idempotence, got %d passes", len(st.applied))
	}
}

// And the negative: the freeze is a fact about a gate the store has ALREADY
// answered. A gate the store serves as still OPEN has a writable answer block,
// so a changed answer there is a real loss and still fails.
func TestMigrateRunStillFailsOnAChangedAnswerOfAnOpenGate(t *testing.T) {
	st := &migrateStore{
		hashes:        map[string]string{},
		serveGateOpen: "APPROVE-EPIC-MR-001",
		mangleField:   [3]string{"APPROVE-EPIC-MR-001", "answer", "the answer the SERVER holds"},
	}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	migrateCorpusAnsweredApproval(t, env.Root)

	err := migrateRun(env)
	if err == nil || !strings.Contains(err.Error(), "APPROVE-EPIC-MR-001.answer") {
		t.Fatalf("a changed answer on a gate the store serves as open must fail naming id and field, got: %v", err)
	}
}

// payloadFieldOf reads one payload field out of the emitted batch. The
// equivalence tests above are only meaningful against the value actually sent,
// so they read it rather than assume it.
func payloadFieldOf(t *testing.T, ops []map[string]any, id, field string) string {
	t.Helper()
	for _, op := range ops {
		p, _ := op["payload"].(map[string]any)
		if got, _ := p["external_id"].(string); got != id {
			continue
		}
		v, has := p[field]
		if !has {
			t.Fatalf("the fixture payload for %s carries no %s field at all", id, field)
		}
		s, ok := v.(string)
		if !ok {
			t.Fatalf("%s.%s is %T, not the string this rule is about", id, field, v)
		}
		return s
	}
	t.Fatalf("no op for %s in the batch", id)
	return ""
}

// Idempotence is a claim about ONE writer. A hook-triggered `factory sync`
// running from a different binary applies its own payloads for the same ids,
// and the two clients then flip the store's shadow hashes forever. The import
// takes the lock the hooks already honour, and refuses rather than racing one
// that is already held — before it posts anything.
func TestMigrateRunRefusesWhileAnotherSyncHoldsTheLock(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	if err := os.MkdirAll(filepath.Dir(syncLockPath(env.Root)), 0o755); err != nil {
		t.Fatal(err)
	}
	held := acquireSyncLock(env.Root)
	if held == nil {
		t.Fatal("the fixture must be able to take the lock first")
	}
	defer held()

	err := migrateRun(env)
	if err == nil || !strings.Contains(err.Error(), "sync.lock") {
		t.Fatalf("a held sync lock must refuse the run naming the lock, got: %v", err)
	}
	if len(st.applied) != 0 {
		t.Fatalf("the refusal must come before anything is posted, got %d batch(es)", len(st.applied))
	}
}

// And it HOLDS the lock while it runs, which is the half that actually
// protects the import: the hook path skips while a sync is in flight, so a
// Stop firing mid-run must find the lock taken.
func TestMigrateRunHoldsTheSyncLockForItsDuration(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	// The probe answers "could a hook take the lock right now", and a probe
	// that CANNOT take it for some other reason — no .modernpath directory to
	// write into — would answer "held" whether the import held it or not. So
	// the directory exists, and the probe is proven able to take the lock
	// before the run starts.
	if err := os.MkdirAll(filepath.Dir(syncLockPath(env.Root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if release := acquireSyncLock(env.Root); release == nil {
		t.Fatal("the probe must be able to take the lock before the run holds it")
	} else {
		release()
	}
	// Probed from inside the batch handler: mid-run is the only moment where
	// the answer means anything.
	st.lockProbeRoot = env.Root

	if err := migrateRun(env); err != nil {
		t.Fatalf("migrate run failed: %v", err)
	}
	if !st.lockWasHeldDuringBatch {
		t.Fatal("a hook firing mid-import must find the sync lock taken")
	}
	// And released afterwards, or every later hook skips forever.
	if _, err := os.Stat(syncLockPath(env.Root)); !os.IsNotExist(err) {
		t.Fatalf("the lock must be released when the run ends, stat err = %v", err)
	}
}

// When the rerun does change records, the message must separate the two
// reasons it can: a corpus that had not landed (a third pass settles it) from
// a second writer (the same ids change again, and again).
func TestMigrateRunNamesTheSecondWriterSignature(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}, foreignWriterID: "REQ-MR-001"}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	err := migrateRun(env)
	if err == nil || !strings.Contains(err.Error(), "not idempotent") {
		t.Fatalf("a rerun that changes records must fail the run, got: %v", err)
	}
	if !strings.Contains(err.Error(), "second writer") {
		t.Fatalf("an id that changes again on a third pass must be named as a second writer, got: %v", err)
	}
}

// The other side of that fork: a rerun that changes records ONCE and then
// settles is not a second writer, and must not be reported as one.
func TestMigrateRunDoesNotBlameASecondWriterForAOneOffChange(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}, staleOnceID: "REQ-MR-001"}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	err := migrateRun(env)
	if err == nil || !strings.Contains(err.Error(), "not idempotent") {
		t.Fatalf("a rerun that changes a record must still fail the run, got: %v", err)
	}
	if strings.Contains(err.Error(), "second writer") {
		t.Fatalf("a one-off change must not be blamed on a second writer: %v", err)
	}
	if !strings.Contains(err.Error(), "third pass settled") {
		t.Fatalf("the message must say the third pass settled, got: %v", err)
	}
}

// The comparison rule itself. Containment, not equality: server-side
// enrichment is not loss, and a missing element, key or value is.
func TestServedValueMustContainWhatWasSent(t *testing.T) {
	cases := []struct {
		name      string
		want, got any
		ok        bool
		// skips is what the comparison must DISCLOSE it did not verify.
		// Without it "systematically unserved" and "silently accepted" are the
		// same passing test, which is the failure mode this whole rule exists
		// to avoid — so every skip-arm names its paths exactly.
		skips []string
	}{
		{"equal scalars", "a", "a", true, nil},
		{"changed scalar", "a", "b", false, nil},
		// The store applies no whitespace normalization on stored text
		// (none exists in the sync write path), so a collapsed string is a
		// changed string — accepting it would let a newline-mangling store
		// verify clean.
		{"whitespace-collapsed text is a real difference", "a\n\n  b", "a b", false, nil},
		{"truncated text", "the whole sentence", "the whole", false, nil},
		// An empty string is cast away before it reaches a column, so null is
		// what it round-trips into — the same absence, spelled the store's way.
		{"an empty string against null is an unwritten column, not a loss", "", nil, true, nil},
		{"a non-empty string against null is a loss", "the sentence", nil, false, nil},
		{"an empty string a served map omits entirely",
			map[string]any{"k": ""}, map[string]any{"other": "v"}, true, nil},
		{"a non-empty value a served map omits is still a loss",
			map[string]any{"k": "v"}, map[string]any{"other": "v"}, false, nil},
		{"int against the float JSON serves", 3, 3.0, true, nil},
		{"different number", 3, 4.0, false, nil},
		{"bool", true, true, true, nil},
		{"list round-trips", []string{"a", "b"}, []any{"a", "b"}, true, nil},
		{"list gained an element server-side", []string{"a"}, []any{"a", "b"}, true, nil},
		{"list lost an element", []string{"a", "b"}, []any{"a"}, false, nil},
		{"map gained a key server-side",
			map[string]any{"k": "v"}, map[string]any{"k": "v", "id": 7.0}, true, nil},
		// A LONE served map has no population to argue from: one absence is
		// indistinguishable from a loss, so the conservative reading holds. The
		// list arms below are where the evidence for the other reading exists.
		{"a lone served map that lost a key is a loss",
			map[string]any{"k": "v"}, map[string]any{"other": "v"}, false, nil},
		{"nested list of maps",
			[]any{map[string]any{"external_id": "SCN-1", "position": 1}},
			[]any{map[string]any{"external_id": "SCN-1", "position": 1.0, "id": 9.0}}, true, nil},
		// Inside a list the served elements ARE a population. A key that NO
		// element carries is a field of the shape the read surface does not
		// serve at all — unverifiable, so it is disclosed by path, not failed.
		{"a key no served element carries is systematically unserved",
			[]any{
				map[string]any{"external_id": "SCN-1", "position": 1},
				map[string]any{"external_id": "SCN-2", "position": 2},
			},
			[]any{
				map[string]any{"external_id": "SCN-1"},
				map[string]any{"external_id": "SCN-2"},
			}, true, []string{"f[].position"}},
		// The same population says the opposite here: the surface CAN serve the
		// key, and for one element it did not. That is a real loss.
		{"a key only some served elements carry is a loss",
			[]any{
				map[string]any{"external_id": "SCN-1", "position": 1},
				map[string]any{"external_id": "SCN-2", "position": 2},
			},
			[]any{
				map[string]any{"external_id": "SCN-1", "position": 1.0},
				map[string]any{"external_id": "SCN-2"},
			}, false, nil},
		// Systematic absence excuses the KEY, never the value: an element whose
		// served value changed still fails. The skip is still disclosed —
		// position really is unserved here — because what a surface does not
		// serve is a fact about the surface, not a verdict on the record.
		{"a systematically unserved key does not excuse a changed value",
			[]any{
				map[string]any{"external_id": "SCN-1", "then": "the store holds it", "position": 1},
				map[string]any{"external_id": "SCN-2", "then": "and serves it back", "position": 2},
			},
			[]any{
				map[string]any{"external_id": "SCN-1", "then": "the store holds it"},
				map[string]any{"external_id": "SCN-2", "then": "MANGLED"},
			}, false, []string{"f[].position"}},
		// Two unserved keys, the epics surface's real pair on spec items.
		{"every unserved key is named, not just the first",
			[]any{
				map[string]any{"external_id": "specs/a.md", "name": "a.md", "position": 1, "content_md": "# A"},
				map[string]any{"external_id": "specs/b.md", "name": "b.md", "position": 2, "content_md": "# B"},
			},
			[]any{
				map[string]any{"external_id": "specs/a.md", "name": "a.md", "artifact_type": "other"},
				map[string]any{"external_id": "specs/b.md", "name": "b.md", "artifact_type": "other"},
			}, true, []string{"f[].content_md", "f[].position"}},
		// Nothing served is no population at all: with no element to compare
		// against, every key would read as unserved and the list would verify
		// clean while holding nothing.
		{"an empty served list discloses nothing and verifies nothing",
			[]any{map[string]any{"external_id": "SCN-1", "position": 1}},
			[]any{}, false, nil},
		{"nested value changed",
			[]any{map[string]any{"external_id": "SCN-1"}},
			[]any{map[string]any{"external_id": "SCN-2"}}, false, nil},
		{"type changed under the value", "3", 3.0, false, nil},
	}
	for _, tc := range cases {
		skips := &skipCollector{}
		if got := payloadRoundTrips(tc.want, tc.got, "f", skips); got != tc.ok {
			t.Errorf("%s: payloadRoundTrips(%#v, %#v) = %v, want %v", tc.name, tc.want, tc.got, got, tc.ok)
		}
		var named []string
		for n := range skips.names {
			named = append(named, n)
		}
		sort.Strings(named)
		if strings.Join(named, ",") != strings.Join(tc.skips, ",") {
			t.Errorf("%s: disclosed skips %v, want %v", tc.name, named, tc.skips)
		}
	}
}

// The retired list the flip freezes and the population the fidelity report
// proves a carrier for must be the same list. A second copy is how the proof
// ends up covering different files than the deletion — so this fails the
// moment anyone reintroduces a local literal that drifts.
func TestRetiredPathFamiliesHaveOneDefinition(t *testing.T) {
	if len(retiredPathFamilies) == 0 {
		t.Fatal("the flip must know which paths it retires")
	}
	if len(retiredPathFamilies) != len(rdd.RetiredPathFamilies) {
		t.Fatalf("the flip retires %d families, the report measures %d",
			len(retiredPathFamilies), len(rdd.RetiredPathFamilies))
	}
	for i, fam := range retiredPathFamilies {
		if fam != rdd.RetiredPathFamilies[i] {
			t.Fatalf("the flip's retired list drifted from the report's population at %d: %q vs %q",
				i, fam, rdd.RetiredPathFamilies[i])
		}
	}
}

// The lock protects the run; this warning protects everything after it, and it
// is the half that has to survive a development workspace. Two builds of a dev
// tree both answer `--version` with "dev", so the identity question — is the
// binary the hooks invoke THIS file? — is the only one whose answer means
// anything here.
func TestMigrateWarnsWhenTheHooksInvokeADifferentBinary(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(hookLogPath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hookLogPath(root), []byte("a hook has run here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A `modernpath` on PATH that is NOT this test binary, answering with the
	// same "dev" string the running build reports — the shape a version
	// comparison alone calls identical and lets through.
	dir := t.TempDir()
	other := filepath.Join(dir, "modernpath")
	if err := os.WriteFile(other, []byte("#!/bin/sh\necho 'modernpath version dev'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	said := captureCLIOutput(t)
	migrateWarnForeignWriter(root)
	if !strings.Contains(said(), "different binary") {
		t.Fatalf("a hook binary that is not this one must be flagged even when both report %q:\n%s", Version, said())
	}
}

// And the negative, so the warning stays worth reading: when the hooks resolve
// to this very file — an installed shim symlinked at it included — there is no
// second writer and nothing to say.
func TestMigrateIsSilentWhenTheHooksInvokeThisBinary(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(hookLogPath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hookLogPath(root), []byte("a hook has run here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Skip("no executable path on this platform")
	}
	dir := t.TempDir()
	if err := os.Symlink(self, filepath.Join(dir, "modernpath")); err != nil {
		t.Skip("cannot symlink here")
	}
	t.Setenv("PATH", dir)

	said := captureCLIOutput(t)
	migrateWarnForeignWriter(root)
	if strings.Contains(said(), "different binary") {
		t.Fatalf("the binary the hooks invoke IS this one; warning about it trains the reader to ignore it:\n%s", said())
	}
}

// REQ-CROSS-249: the run posts the corpus's historical RUN: tokens as
// migration-kind evidence runs whose every result is explicitly
// inherited_unverified — while the declaration is merely seeded.
func TestMigrateRunPostsHistoricalEvidenceExplicitly(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	// a ledger token with a verdict
	ledger := filepath.Join(env.Root, "tasks", "MR-REQUIREMENTS.md")
	body, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	amended := strings.Replace(string(body),
		"| REQ-MR-001 | First | MVP | PROPOSED | UR-MR-001 | doc-a | — | — |",
		"| REQ-MR-001 | First | MVP | PROPOSED | UR-MR-001 | doc-a | `RUN:2026-08-20:mr-green` 3/3 GREEN | — |", 1)
	if err := os.WriteFile(ledger, []byte(amended), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := migrateRun(env); err != nil {
		t.Fatalf("migrate run failed: %v", err)
	}

	var hist map[string]any
	for _, run := range st.evidenceRuns {
		if id, _ := run["external_id"].(string); strings.HasPrefix(id, "HIST-RUN:2026-08-20") {
			hist = run
		}
	}
	if hist == nil {
		t.Fatalf("no historical evidence run posted; got %d runs", len(st.evidenceRuns))
	}
	if hist["kind"] != "migration" {
		t.Fatalf("kind = %v", hist["kind"])
	}
	results, _ := hist["results"].([]any)
	if len(results) == 0 {
		t.Fatalf("the run must carry its results")
	}
	res, _ := results[0].(map[string]any)
	if res["validity"] != "inherited_unverified" {
		t.Fatalf("validity = %v — the importer must state it explicitly", res["validity"])
	}
	if res["result"] != "pass" || res["target_external_id"] != "REQ-MR-001" {
		t.Fatalf("result row = %v", res)
	}
	sort.Strings(nil) // keep the sort import honest if fixtures change
}

// REQ-CROSS-262 — the importing-account fallback is not a resolution and must
// not be compared as one. The importer resolves a declared approver name
// against the provisioned users and keeps the importing account when it
// cannot; the declared name is never persisted, so what serves back for such a
// row is the importing account's own name. Comparing that against the corpus's
// declared name hard-fails 169 of 177 real approvals — a guard flagging what it
// cannot verify, not deleting the run.
func TestMigrateRunDoesNotHardFailOnAnImportingAccountFallbackApproval(t *testing.T) {
	st := &migrateStore{
		hashes:               map[string]string{},
		epicApprovalFallback: "EPIC-MR-001",
	}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	migrateCorpusAnsweredApproval(t, env.Root)
	migrateCorpusNamedApprover(t, env.Root)

	said := captureCLIOutput(t)
	if err := migrateRun(env); err != nil {
		t.Fatalf("an importing-account fallback is named residue, not a mismatch: %v", err)
	}
	if !strings.Contains(said(), "approver_name") {
		t.Fatalf("the unverifiable approver name must be disclosed, not silently excused:\n%s", said())
	}
}

// And the sensitivity that keeps the exemption from being a hole: an approval
// the store says it RESOLVED to a named human, serving a different name, is a
// real loss and still fails.
func TestMigrateRunStillFailsWhenAResolvedApprovalNameDiffers(t *testing.T) {
	st := &migrateStore{
		hashes:             map[string]string{},
		mangleApprovalName: "EPIC-MR-001",
	}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	migrateCorpusAnsweredApproval(t, env.Root)
	migrateCorpusNamedApprover(t, env.Root)

	err := migrateRun(env)
	if err == nil || !strings.Contains(err.Error(), "EPIC-MR-001.approval") {
		t.Fatalf("a resolved approval whose name did not round-trip must fail naming it, got: %v", err)
	}
}

// Gate attribution is a first-class count group, never a silent skip. The gate
// surface serves the answerer's user id and no basis of its own, so the client
// reconstructs the three classes: resolved to the declared human, the
// importing-account fallback, and no declared name at all. The importing
// account is identified from the served approval that names it as the
// fallback. Fallback and no-name rows are named residue — the run stays green.
func TestMigrateRunCountsGateAttributionAsAFirstClassGroup(t *testing.T) {
	st := &migrateStore{
		hashes:               map[string]string{},
		epicApprovalFallback: "EPIC-MR-001",
	}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	migrateCorpusAnsweredApproval(t, env.Root)
	migrateCorpusNamedApprover(t, env.Root)

	// The importing account keeps one gate's answer; the other resolved to a
	// real human.
	st.gateAnswererIDs = map[string]any{
		"APP-MR-001":          st.importingAccountID,
		"APPROVE-EPIC-MR-001": 42.0,
	}

	said := captureCLIOutput(t)
	if err := migrateRun(env); err != nil {
		t.Fatalf("fallback attribution must not fail the run: %v", err)
	}
	for _, want := range []string{
		"gate attribution",
		"1 resolved to the declared human",
		"1 importing-account fallback",
		"0 with no declared name",
	} {
		if !strings.Contains(said(), want) {
			t.Fatalf("the attribution count group must state %q:\n%s", want, said())
		}
	}
	if !strings.Contains(said(), "named residue") {
		t.Fatalf("the group must say the fallback class is residue, not a mismatch:\n%s", said())
	}
}

// The third class, on its own: a gate the store holds with no answerer at all.
// It is counted, not skipped — an uncounted class is indistinguishable from a
// verified one.
func TestMigrateRunCountsGatesWithNoDeclaredAnswerer(t *testing.T) {
	st := &migrateStore{
		hashes:               map[string]string{},
		epicApprovalFallback: "EPIC-MR-001",
	}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	migrateCorpusAnsweredApproval(t, env.Root)
	migrateCorpusNamedApprover(t, env.Root)

	said := captureCLIOutput(t)
	if err := migrateRun(env); err != nil {
		t.Fatalf("migrate run failed: %v", err)
	}
	if !strings.Contains(said(), "2 with no declared name") {
		t.Fatalf("gates the store serves without an answerer must be counted:\n%s", said())
	}
}

// The honest edge: with no served approval naming the importing account, the
// client cannot tell a fallback from a resolution. It says so rather than
// guessing — claiming resolution it cannot establish is the failure mode the
// whole group exists to prevent.
func TestMigrateRunDisclosesUndeterminedGateAttribution(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	migrateCorpusAnsweredApproval(t, env.Root)
	migrateCorpusNamedApprover(t, env.Root)
	st.gateAnswererIDs = map[string]any{"APP-MR-001": 99.0}

	said := captureCLIOutput(t)
	if err := migrateRun(env); err != nil {
		t.Fatalf("migrate run failed: %v", err)
	}
	if !strings.Contains(said(), "1 undetermined") {
		t.Fatalf("an attribution the client cannot classify must be disclosed as undetermined:\n%s", said())
	}
}

// migrateCorpusEvidenceLines gives the fixture requirement a detail block whose
// citing lines are the shapes that matter: several lines carrying ONE tag
// (which must become several results with their own verdicts), one line
// repeating that tag (which must become one), and — FIRST, so the mangle arm
// lands on it — a line stating no outcome at all, whose result is skip. The
// latest-state surface filters skip rows out entirely, so the imported
// population is exactly the one nothing served.
func migrateCorpusEvidenceLines(t *testing.T, root string) {
	t.Helper()
	p := filepath.Join(root, "tasks", "MR-REQUIREMENTS.md")
	content, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	tag := "`RUN:2026-08-20:mr-lines`"
	block := "\n### REQ-MR-001 — First\n" +
		"- **Evidence:** cited " + tag + " with no stated outcome\n" +
		"- **Evidence:** RED first " + tag + " — the focused failure stood\n" +
		"- **Evidence:** then GREEN " + tag + " — 12/12 passing\n" +
		"- **Evidence:** rollup " + tag + " " + tag + " " + tag + " — 3/3 GREEN\n"
	if err := os.WriteFile(p, append(content, []byte(block)...), 0o644); err != nil {
		t.Fatal(err)
	}
}

// REQ-CROSS-260 — the run posts one result per distinct citing line, the store
// accepts the post (the fixture refuses a duplicate extended identity exactly
// as the store does), and the collapse of the repeated-tag line is disclosed
// rather than silent.
func TestMigrateRunPostsOneEvidenceResultPerCorpusLine(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	migrateCorpusEvidenceLines(t, env.Root)

	said := captureCLIOutput(t)
	if err := migrateRun(env); err != nil {
		t.Fatalf("migrate run failed: %v", err)
	}

	var hist map[string]any
	for _, run := range st.evidenceRuns {
		if id, _ := run["external_id"].(string); id == "HIST-RUN:2026-08-20:mr-lines" {
			hist = run
		}
	}
	if hist == nil {
		t.Fatalf("the historical run must be posted; got %d run(s)", len(st.evidenceRuns))
	}
	results, _ := hist["results"].([]any)
	if len(results) != 4 {
		t.Fatalf("four distinct citing lines must post four results, got %d", len(results))
	}
	refs := map[string]bool{}
	verdicts := map[string]int{}
	for _, raw := range results {
		res, _ := raw.(map[string]any)
		ref, _ := res["test_case_ref"].(string)
		if ref == "" {
			t.Fatalf("every posted result carries its line identity: %v", res)
		}
		refs[ref] = true
		v, _ := res["result"].(string)
		verdicts[v]++
	}
	if len(refs) != 4 {
		t.Fatalf("the four results must carry four distinct identities, got %v", refs)
	}
	// Every outcome class rides, skip included — the surface that exists today
	// filters exactly those out, which is why the imported population is
	// invisible on it.
	if verdicts["skip"] != 1 || verdicts["fail"] != 1 || verdicts["pass"] != 2 {
		t.Fatalf("each line keeps its own verdict — want 1 skip, 1 fail, 2 pass, got %v", verdicts)
	}
	if !strings.Contains(said(), "collapsed") {
		t.Fatalf("the repeated-tag line's collapse must be disclosed:\n%s", said())
	}
}

// The readback arm: posted raw text equals served raw text, per result. Without
// it the preserved-raw-text claim is asserted by the importer and proved by
// nothing — no surface served any evidence-result field at all.
func TestMigrateRunReadsBackTheEvidenceItPosted(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	migrateCorpusEvidenceLines(t, env.Root)

	said := captureCLIOutput(t)
	if err := migrateRun(env); err != nil {
		t.Fatalf("migrate run failed: %v", err)
	}
	if st.evidenceReads == 0 {
		t.Fatal("the run must READ the imported-evidence surface, not assume the post landed")
	}
	if !strings.Contains(said(), "evidence") {
		t.Fatalf("the evidence readback must say what it verified:\n%s", said())
	}
}

// And the sensitivity: a served raw text that is not what was posted fails the
// run naming the run. Otherwise the readback would pass against a store that
// truncated every line — the exact loss it exists to catch.
func TestMigrateRunFailsWhenServedEvidenceRawTextDiffers(t *testing.T) {
	st := &migrateStore{
		hashes:            map[string]string{},
		mangleEvidenceRaw: [2]string{"HIST-RUN:2026-08-20:mr-lines", "…truncated by the store…"},
	}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	migrateCorpusEvidenceLines(t, env.Root)

	err := migrateRun(env)
	if err == nil {
		t.Fatal("raw text the store did not keep must fail the run")
	}
	if !strings.Contains(err.Error(), "HIST-RUN:2026-08-20:mr-lines") {
		t.Fatalf("the failure must name the run whose text did not round-trip, got: %v", err)
	}
}

// The evidence surface is required like every other read surface: absent, it is
// a verification failure, not an optional-surface skip (REQ-CROSS-255's rule).
func TestMigrateRunFailsWhenTheEvidenceReadSurfaceCannotBeReached(t *testing.T) {
	st := &migrateStore{
		hashes:          map[string]string{},
		offlineSurfaces: map[string]bool{"evidence": true},
	}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	migrateCorpusEvidenceLines(t, env.Root)

	err := migrateRun(env)
	if err == nil {
		t.Fatal("an unreachable evidence read surface must fail the run")
	}
	if !strings.Contains(err.Error(), "evidence") {
		t.Fatalf("the refusal must name the surface, got: %v", err)
	}
}
