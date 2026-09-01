package cmd

// REQ-CROSS-264 — the ninth read surface. Tasks read back like every other
// kind: an emitted id the store does not serve fails the run naming it, and
// the sync-born filter keeps product-born tasks (Board, MCP, derivation) out
// of a comparison they were never part of.
//
// `tasks` is the PRODUCT Task table, so the surface needs the same discipline
// `documents` needed — but not the same PREDICATE: an imported task is
// epic-parented, so it carries a NULL `system_id` and no `sync:`
// `source_revision`. Reusing the document predicate would drop every task and
// report the whole population as missing.

import (
	"strings"
	"testing"
)

func TestMigrateReadSurfacesCarriesTasks(t *testing.T) {
	env := &factoryEnv{SystemID: 42}
	surfaces := migrateReadSurfaces(env)
	var found *migrateReadSurface
	for i := range surfaces {
		if surfaces[i].opType == "upsert_task" {
			found = &surfaces[i]
		}
	}
	if found == nil {
		t.Fatal("upsert_task has no read surface — every emitted id of the type would go unverified")
	}
	if found.key != "tasks" {
		t.Fatalf("collection key = %q, want tasks", found.key)
	}
	if !strings.Contains(found.path, "/api/v1/sync/tasks?system_id=42") {
		t.Fatalf("path = %q", found.path)
	}
	if found.syncBorn == nil {
		t.Fatal("the tasks surface needs a sync-born predicate: the table is shared with the product")
	}
	syncBorn := map[string]any{
		"external_id": "EPIC-MR-001#T1", "source_path": "epics/EPIC-MR-001-rehearsal/EPIC.md",
		"sync_born": true,
	}
	if !found.syncBorn(syncBorn, env) {
		t.Fatal("an imported task must pass its own surface's filter — NULL system_id and all")
	}
	productBorn := map[string]any{"external_id": "BOARD-1", "sync_born": false}
	if found.syncBorn(productBorn, env) {
		t.Fatal("a product-born task must not enter the comparison")
	}
}

func TestMigrateRunFailsWhenATaskDoesNotReadBack(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}, dropServe: "EPIC-MR-001#T1"}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	err := migrateRun(env)
	if err == nil || !strings.Contains(err.Error(), "EPIC-MR-001#T1") {
		t.Fatalf("a task that does not read back must fail the run naming it, got: %v", err)
	}
}

func TestMigrateRunIgnoresProductBornTasksInBothDirections(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}, extraProductTask: true}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	if err := migrateRun(env); err != nil {
		t.Fatalf("a product-born task on the surface must not read as a corpus record gone missing: %v", err)
	}
	// The --from-empty precondition consumes the same list, so it must reach
	// the same verdict: a store holding only product-born tasks is empty of
	// process rows.
	st2 := &migrateStore{hashes: map[string]string{}, extraProductTask: true}
	srv2 := serveMigrateStore(t, st2)
	env2 := wsEnv(t, srv2)
	migrateCorpus(t, env2.Root)
	if err := migrateRefuseUnlessEmpty(env2); err != nil {
		t.Fatalf("--from-empty must not be refused by a product-born task: %v", err)
	}
}
