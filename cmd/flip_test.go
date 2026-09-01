package cmd

// REQ-CROSS-225 — focused lower evidence for the flip MACHINERY. The flip
// commit itself is completion-gate material (§225.5): these pin the tooling
// that prepares it. RED first.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// migrateFlipDeclaration registers the genuine flip gate in the fixture store
// — human, approval-request, purposed for this direction, scoped to this
// system, answered with the approving option key — and returns its id. The
// store checks the same predicates the server does, so a flip that stops
// satisfying one is refused here too, and its fingerprint is "fp1" with the
// stored answer "approved".
func migrateFlipDeclaration(st *migrateStore, env *factoryEnv, id string) string {
	if st.declarationGates == nil {
		st.declarationGates = map[string]*declarationGate{}
	}
	st.declarationGates[id] = satisfyingDeclarationGate(env.SystemID, "store_backed_activation", "fp1")
	return id
}

// §225.1/.2 + §227.1 — the flip refuses without an attributable source,
// re-verifies via the import run, activates the declaration with the
// source, and freezes the retired list in the tracked marker.
func TestMigrateFlipVerifiesActivatesAndWritesMarker(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	if err := migrateFlip(env, "", "", ""); err == nil || !strings.Contains(err.Error(), "--gate") {
		t.Fatalf("the flip must refuse without the completion gate reference, got: %v", err)
	}
	if len(st.declarations) != 0 {
		t.Fatalf("a refused flip must not touch the declaration: %v", st.declarations)
	}

	if err := migrateFlip(env, migrateFlipDeclaration(st, env, "APPROVE-EPIC-CLI-003"), "fp1", "approved"); err != nil {
		t.Fatalf("flip failed: %v", err)
	}
	if len(st.applied) < 2 {
		t.Fatal("the flip must re-verify through the import run (two passes)")
	}
	last := st.declarations[len(st.declarations)-1]
	if last["state"] != "active" || last["gate_ref"] != "APPROVE-EPIC-CLI-003" ||
		last["gate_fingerprint"] != "fp1" {
		t.Fatalf("the flip must activate via the exact answered gate, got %v", last)
	}

	marker, err := os.ReadFile(filepath.Join(env.Root, "process", "store-backed.md"))
	if err != nil {
		t.Fatalf("the flip must write the tracked marker: %v", err)
	}
	for _, want := range []string{
		"retired: tasks/*-REQUIREMENTS.md",
		"retired: WORKLIST.md",
		"retired: PROGRESS.md",
		"retired: BACKLOG.md",
		"retired: process/gap-register.md",
		"retired: process/08-open-questions.md",
		"retired: epics/**",
		"USER:fixture-gate-answer (gate APPROVE-EPIC-CLI-003)",
	} {
		if !strings.Contains(string(marker), want) {
			t.Fatalf("marker must freeze %q:\n%s", want, string(marker))
		}
	}
}

// REQ-CROSS-259 — the declaration now rides the answer actually given. The
// server checks gate identity AND that the caller echoes the stored answer,
// exactly as the advance path already required; a flip that sends no echo is
// refused with 409 and moves no authority.
func TestMigrateFlipEchoesTheStoredAnswerOnActivation(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	if err := migrateFlip(env, migrateFlipDeclaration(st, env, "FLIP-STORE-BACKED"), "fp1", "approved"); err != nil {
		t.Fatalf("the genuine flip gate must activate end-to-end: %v", err)
	}
	last := st.declarations[len(st.declarations)-1]
	if last["gate_answer"] != "approved" {
		t.Fatalf("activation must echo the gate's stored answer, got %v", last["gate_answer"])
	}
}

// The echo is a required argument, not an optional nicety: without it the
// server refuses, so the CLI refuses first and says which flag is missing
// rather than spending a full re-verification import on a call that cannot land.
func TestMigrateFlipRefusesWithoutTheStoredAnswerEcho(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	migrateFlipDeclaration(st, env, "FLIP-STORE-BACKED")

	err := migrateFlip(env, "FLIP-STORE-BACKED", "fp1", "")
	if err == nil || !strings.Contains(err.Error(), "--gate-answer") {
		t.Fatalf("the flip must refuse without the answer echo, naming the flag, got: %v", err)
	}
	if len(st.applied) != 0 {
		t.Fatalf("the refusal must come before the re-verification import, got %d batch(es)", len(st.applied))
	}
	if len(st.declarations) != 0 {
		t.Fatalf("a refused flip must not touch the declaration: %v", st.declarations)
	}
}

// A gate that fails a declaration predicate is refused server-side, and the
// refusal names the predicate. The CLI must surface that verbatim and write no
// marker — an operator told only "activation refused" cannot tell a wrong
// purpose from a foreign scope from an unanswered gate.
func TestMigrateFlipSurfacesADeclarationPredicateRefusal(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	// The corpus's own approval gate: answered, but purposed for nothing —
	// exactly the 414-gate population that could move store authority before.
	st.declarationGates = map[string]*declarationGate{
		"APPROVE-EPIC-MR-001": {
			gateClass: "human", kind: "approval_request", purpose: "",
			exactScope: []string{"system:4"}, chosenOptionKeys: []string{"approve"},
			answer: "approved", fingerprint: "fp1", sourceTag: "USER:fixture-gate-answer",
		},
	}

	err := migrateFlip(env, "APPROVE-EPIC-MR-001", "fp1", "approved")
	if err == nil {
		t.Fatal("a gate with no declaration purpose must not activate store authority")
	}
	if !strings.Contains(err.Error(), "the gate's purpose is") {
		t.Fatalf("the refusal must carry the server's own sentence, not a dumped map: %v", err)
	}
	// …and say what a gate would have to be, so the operator's next move is
	// authoring the right gate rather than guessing at the predicate set.
	if !strings.Contains(err.Error(), "purposed store_backed_activation") {
		t.Fatalf("the refusal must state the predicates a declaration gate satisfies, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(env.Root, "process", "store-backed.md")); !os.IsNotExist(statErr) {
		t.Fatalf("a refused activation must not write the retirement marker, stat err = %v", statErr)
	}
}

// migrateFlipPopulatedStore leaves the fixture in the state a flip attempt that
// activated server-side and then died before the marker write leaves behind:
// the store holds the corpus, and the declaration reads active.
func migrateFlipPopulatedStore(t *testing.T, env *factoryEnv, st *migrateStore) {
	t.Helper()
	if err := migrateRun(env); err != nil {
		t.Fatalf("the fixture store must be populated before a retry means anything: %v", err)
	}
	st.declaredState = "active"
}

// REQ-CROSS-256 — the retry re-verifies. An active declaration closes the batch
// and evidence WRITE channels for the system; it closes no read. So the retry
// runs the read-only half of the import's verification — the fidelity gate, the
// duplicate-id refusal and the full id-and-field readback — instead of skipping
// verification altogether and rewriting the marker from the current tree.
func TestMigrateFlipRetryReVerifiesTheCorpusWithoutWriting(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	migrateFlipPopulatedStore(t, env, st)
	batchesBefore := len(st.applied)

	said := captureCLIOutput(t)
	if err := migrateFlip(env, migrateFlipDeclaration(st, env, "APPROVE-EPIC-CLI-003"), "fp1", "approved"); err != nil {
		t.Fatalf("a retried flip over an unchanged corpus must complete: %v", err)
	}
	if len(st.applied) != batchesBefore {
		t.Fatalf("the retry must post no batch write (the channel is closed server-side), got %d new batch(es)",
			len(st.applied)-batchesBefore)
	}
	for _, want := range []string{"verify requirements:", "verify epics:", "verify gates:", "verify documents:"} {
		if !strings.Contains(said(), want) {
			t.Fatalf("the retry must verify every read surface (missing %q):\n%s", want, said())
		}
	}
	if _, err := os.Stat(filepath.Join(env.Root, "process", "store-backed.md")); err != nil {
		t.Fatalf("a retry that re-verified must still write the marker: %v", err)
	}
}

// The half that makes the re-verification worth running: a corpus that drifted
// after the store was populated refuses the flip, names the drifted record, and
// leaves the retirement marker untouched — the marker is the irreversible half,
// so its absence is the assertion that matters.
func TestMigrateFlipRetryRefusesADriftedCorpusAndWritesNoMarker(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	migrateFlipPopulatedStore(t, env, st)
	migrateCorpusDriftTitle(t, env.Root)

	err := migrateFlip(env, migrateFlipDeclaration(st, env, "APPROVE-EPIC-CLI-003"), "fp1", "approved")
	if err == nil {
		t.Fatal("a retried flip over a drifted corpus must refuse")
	}
	if !strings.Contains(err.Error(), "REQ-MR-001") {
		t.Fatalf("the refusal must name the drifted record, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(env.Root, "process", "store-backed.md")); !os.IsNotExist(statErr) {
		t.Fatalf("a refused flip must not write the retirement marker, stat err = %v", statErr)
	}
}

// A refusal with no way out is a dead end: once the declaration is active the
// store refuses the import too, so the message has to name the only legal exit
// — clearing the declaration on an answered gate.
func TestMigrateFlipRetryRefusalNamesTheClearedStateExit(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	migrateFlipPopulatedStore(t, env, st)
	migrateCorpusDriftTitle(t, env.Root)

	err := migrateFlip(env, migrateFlipDeclaration(st, env, "APPROVE-EPIC-CLI-003"), "fp1", "approved")
	if err == nil {
		t.Fatal("the fixture must refuse for this to mean anything")
	}
	for _, want := range []string{"cleared", "gate"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must name the cleared-state exit path (missing %q): %v", want, err)
		}
	}
}

// Activation records the corpus revision it was verified at. Content
// re-verification is what gates the retry; the recorded revision is the audit
// trail — the declaration stores no revision at all today, so a reader of the
// declaration cannot say which tree was verified into it.
func TestMigrateFlipRecordsTheVerifiedCorpusRevision(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	commitCorpus(t, env.Root)
	sha := gitOut(env.Root, "rev-parse", "HEAD")
	if sha == "" {
		t.Fatal("the fixture must have a real revision for this to mean anything")
	}

	if err := migrateFlip(env, migrateFlipDeclaration(st, env, "APPROVE-EPIC-CLI-003"), "fp1", "approved"); err != nil {
		t.Fatalf("flip failed: %v", err)
	}
	last := st.declarations[len(st.declarations)-1]
	if last["state"] != "active" {
		t.Fatalf("the last declaration write must be the activation, got %v", last)
	}
	if last["corpus_revision"] != sha {
		t.Fatalf("activation must record the corpus revision it verified (%s), got %v", sha, last["corpus_revision"])
	}
}

// migrateCorpusDriftTitle changes a requirement's title in the corpus after the
// store has been populated: the ops now carry a title the store does not serve.
func migrateCorpusDriftTitle(t *testing.T, root string) {
	t.Helper()
	p := filepath.Join(root, "tasks", "MR-REQUIREMENTS.md")
	content, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	drifted := strings.Replace(string(content),
		"| REQ-MR-001 | First |", "| REQ-MR-001 | First, renamed after the import |", 1)
	if drifted == string(content) {
		t.Fatal("the drift edit matched nothing — the fixture ledger changed shape")
	}
	if err := os.WriteFile(p, []byte(drifted), 0o644); err != nil {
		t.Fatal(err)
	}
}

// §225.4 — modernpath check recognizes a store-backed workspace: an absent
// ledger is the declared configuration, never a "nothing was checked" error.
func TestCheckRecognizesStoreBackedWorkspace(t *testing.T) {
	root := t.TempDir()
	if hasProcessRecords(root) {
		t.Fatal("fixture must have no ledgers")
	}
	if storeBackedWorkspace(root) {
		t.Fatal("no marker yet — not store-backed")
	}
	if err := os.MkdirAll(filepath.Join(root, "process"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "process", "store-backed.md"),
		[]byte("# Store-backed declaration\nretired: tasks/*-REQUIREMENTS.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !storeBackedWorkspace(root) {
		t.Fatal("the marker must make the workspace read as store-backed")
	}
}
