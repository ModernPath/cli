package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func migrateOpsContainDocument(ops []map[string]any) bool {
	for _, op := range ops {
		if op["type"] == "upsert_document" {
			return true
		}
	}
	return false
}

// B11a: `migrate run` (and `migrate flip`, which delegates to it) gains the
// same --no-docs escape hatch factory sync has, so an environment whose
// embedding provider rejects documents can still import process state instead
// of 422-ing the whole batch.
func TestMigrateRunNoDocsDropsDocumentOps(t *testing.T) {
	saved := migrateNoDocs
	defer func() { migrateNoDocs = saved }()

	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	// docs/adr/*.md becomes an upsert_document op (CollectDocuments).
	if err := os.MkdirAll(filepath.Join(env.Root, "docs", "adr"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.Root, "docs", "adr", "0001-x.md"), []byte("# ADR-0001\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Fixture sanity: the corpus really does yield a document op, so the
	// assertion below is not vacuously true.
	ops, _, err := env.workspaceOps()
	if err != nil {
		t.Fatal(err)
	}
	if !migrateOpsContainDocument(ops) {
		t.Fatal("fixture must yield an upsert_document op (else the test is vacuous)")
	}

	// --no-docs: migrateRun must send no document op to the store.
	migrateNoDocs = true
	if err := migrateRun(env); err != nil {
		t.Fatalf("--no-docs run: %v", err)
	}
	for key := range st.payloads {
		if strings.HasPrefix(key, "upsert_document|") {
			t.Fatalf("--no-docs must drop every upsert_document op, but the store received %s", key)
		}
	}
}

// The flip's already-active retry path re-verifies through migrateReverify,
// which built its ops without the --no-docs filter: an operator who imported
// with --no-docs (an embedding provider rejecting documents) and re-ran the
// flip had every never-imported document id demanded back from the store, and
// the flip was refused with `migrate clear` as the only exit. The flag help
// says flip delegates to run; the retry must honour the flag the run did.
func TestMigrateFlipRetryHonoursNoDocs(t *testing.T) {
	saved := migrateNoDocs
	defer func() { migrateNoDocs = saved }()

	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	if err := os.MkdirAll(filepath.Join(env.Root, "docs", "adr"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.Root, "docs", "adr", "0001-x.md"), []byte("# ADR-0001\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	migrateNoDocs = true
	migrateFlipPopulatedStore(t, env, st) // imported with --no-docs: no document in the store
	for key := range st.payloads {
		if strings.HasPrefix(key, "upsert_document|") {
			t.Fatalf("fixture: --no-docs population must hold no document, got %s", key)
		}
	}

	if err := migrateFlip(env, migrateFlipDeclaration(st, env, "APPROVE-EPIC-CLI-003"), "fp1", "approved"); err != nil {
		t.Fatalf("a retried --no-docs flip must not demand the documents it was told to skip: %v", err)
	}
}
