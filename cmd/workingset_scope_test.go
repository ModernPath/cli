package cmd

// REQ-CROSS-313 (SR-CLI-0084), EPIC-CLI-008: `working-set pull --scope
// [--for-review]` resolves the current work selection and materializes the
// scope as a directory in the CLI-owned authoring render.
//
// RED first: `workingSetPullScope` does not exist.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func scopeSelection() map[string]any {
	return map[string]any{
		"current": map[string]any{
			"scope_kind":        "epic",
			"scope_external_id": "EPIC-CLI-008",
			"members":           []any{"REQ-CROSS-310", "REQ-CROSS-311"},
			"fingerprint":       "agg-fp",
			"phase":             "build",
			"recon_revision":    "54611fd52",
		},
		"active_release": []any{map[string]any{"slug": "modernpath-v1-09", "status": "active"}},
	}
}

func scopeFixture() *wsFixture {
	return &wsFixture{
		workSelection: scopeSelection(),
		epics: []any{map[string]any{
			"external_id": "EPIC-CLI-008", "title": "Process navigation brain",
			"description": "the loop's navigation half", "owner": "core",
			"outcome_source": "USER:2026-09-01", "process_status": "IN_PROGRESS",
			"fingerprint": "epic-served-fp",
		}},
		requirements: []any{
			map[string]any{"external_id": "REQ-CROSS-310", "title": "parity", "context": "CROSS",
				"work_status": "IN_REVIEW", "boundary": "the reads", "rationale": "round-trip",
				"verification_method": "unit", "fingerprint": "sr310-fp"},
			map[string]any{"external_id": "REQ-CROSS-311", "title": "patch", "context": "CROSS",
				"work_status": "IN_REVIEW", "fingerprint": "sr311-fp"},
		},
		packetSections: []any{map[string]any{
			"scope_kind": "epic", "scope_external_id": "EPIC-CLI-008",
			"section_key": "reconnaissance", "content": "# recon\nthe surface",
			"content_fingerprint": "ps-fp-1", "authoring_context_id": "ctx-1",
		}},
	}
}

func scopeDir(env *factoryEnv) string {
	return filepath.Join(env.Root, ".modernpath", "working-set", "EPIC-CLI-008")
}

func reviewScopeRoot(env *factoryEnv) string {
	return filepath.Join(env.Root, ".modernpath", "working-set-reviews", "EPIC-CLI-008")
}

func onlyReviewSnapshot(t *testing.T, env *factoryEnv) string {
	t.Helper()
	entries, err := os.ReadDir(reviewScopeRoot(env))
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("expected one isolated review snapshot under %s; entries=%v err=%v", reviewScopeRoot(env), entries, err)
	}
	return filepath.Join(reviewScopeRoot(env), entries[0].Name())
}

func reviewDeliveryContext(aggregate string) map[string]any {
	return map[string]any{
		"packet_fingerprint": aggregate,
		"facts": map[string]any{
			"aggregate": aggregate,
			"scope":     map[string]any{"external_id": "EPIC-CLI-008", "kind": "epic"},
			"members":   []any{},
			"sections":  map[string]any{"required": []any{"reconnaissance", "red_strategy", "decisions"}},
		},
	}
}

func requiredPacketContext(keys ...string) map[string]any {
	required := make([]any, 0, len(keys))
	for _, key := range keys {
		required = append(required, key)
	}
	return map[string]any{"facts": map[string]any{"sections": map[string]any{"required": required}}}
}

// TestScopePullRejectsUnsafeRequiredPacketKeyBeforeWriting covers
// UR-CLI-DRAFT-PROTECTION-001#AC3 and REQ-CROSS-332#AC9.
func TestScopePullRejectsUnsafeRequiredPacketKeyBeforeWriting(t *testing.T) {
	fx := scopeFixture()
	fx.deliveryContext = requiredPacketContext("../escape")
	env := wsEnv(t, wsServe(t, fx))
	err := workingSetPullScope(env, false, wsNow)
	if err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("expected unsafe required packet key refusal, got %v", err)
	}
	if _, statErr := os.Stat(scopeDir(env)); !os.IsNotExist(statErr) {
		t.Fatalf("invalid required key wrote the authoring scope: stat err=%v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(env.Root, ".modernpath", "working-set", "escape.md")); !os.IsNotExist(statErr) {
		t.Fatalf("invalid required key escaped its scope: stat err=%v", statErr)
	}
}

// TestScopePullRejectsServedRequiredPacketFilenameCollision covers
// UR-CLI-DRAFT-PROTECTION-001#AC3 and REQ-CROSS-332#AC9.
func TestScopePullRejectsServedRequiredPacketFilenameCollision(t *testing.T) {
	fx := scopeFixture() // served reconnaissance and required 10-recon both map to 10-recon.md
	fx.deliveryContext = requiredPacketContext("reconnaissance", "10-recon")
	env := wsEnv(t, wsServe(t, fx))
	err := workingSetPullScope(env, false, wsNow)
	if err == nil || !strings.Contains(err.Error(), "10-recon") {
		t.Fatalf("expected served/required filename collision refusal, got %v", err)
	}
	if _, statErr := os.Stat(scopeDir(env)); !os.IsNotExist(statErr) {
		t.Fatalf("colliding required key wrote the authoring scope: stat err=%v", statErr)
	}
}

// TestScopePullRejectsRequiredPacketFilenameCollision covers
// REQ-CROSS-332#AC9 when two required keys alias one local path.
func TestScopePullRejectsRequiredPacketFilenameCollision(t *testing.T) {
	fx := scopeFixture()
	fx.packetSections = nil
	fx.deliveryContext = requiredPacketContext("reconnaissance", "10-recon")
	env := wsEnv(t, wsServe(t, fx))
	err := workingSetPullScope(env, false, wsNow)
	if err == nil || !strings.Contains(err.Error(), "10-recon") {
		t.Fatalf("expected required/required filename collision refusal, got %v", err)
	}
	if _, statErr := os.Stat(scopeDir(env)); !os.IsNotExist(statErr) {
		t.Fatalf("colliding required keys wrote the authoring scope: stat err=%v", statErr)
	}
}

func preparedReviewScope(t *testing.T) (*wsFixture, *factoryEnv, map[string]scopeTreeEntry) {
	t.Helper()
	fx := scopeFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("prepare authoring scope: %v", err)
	}
	return fx, env, scopeTreeBytes(t, scopeDir(env))
}

func readScopeFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func assertManualScopeRecoveryInstructions(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a refusal with manual recovery instructions")
	}
	for _, phrase := range []string{"rename the whole scope directory", ".modernpath/working-set/", "pull a fresh scope", "compare and reapply"} {
		if !strings.Contains(err.Error(), phrase) {
			t.Fatalf("manual recovery refusal should include %q, got: %v", phrase, err)
		}
	}
}

type scopeTreeEntry struct {
	dir     bool
	content []byte
}

// scopeTreeBytes snapshots every path and regular file in the scope, including
// CLI metadata such as SELECTION.md, .context and fingerprint sidecars.
func scopeTreeBytes(t *testing.T, root string) map[string]scopeTreeEntry {
	t.Helper()
	entries := map[string]scopeTreeEntry{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			entries[rel] = scopeTreeEntry{dir: true}
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		entries[rel] = scopeTreeEntry{content: content}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot scope tree %s: %v", root, err)
	}
	return entries
}

func TestScopePullRefusesModifiedItemWithoutWriting(t *testing.T) {
	for _, name := range []string{"EPIC-CLI-008.md", "members/REQ-CROSS-310.md"} {
		t.Run(name, func(t *testing.T) {
			fx := scopeFixture()
			env := wsEnv(t, wsServe(t, fx))
			if err := workingSetPullScope(env, false, wsNow); err != nil {
				t.Fatal(err)
			}
			dir := scopeDir(env)
			path := filepath.Join(dir, filepath.FromSlash(name))
			content := readScopeFile(t, path) + "\nauthor's unsaved change\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			before := scopeTreeBytes(t, dir)
			err := workingSetPullScope(env, false, wsNow)
			if err == nil || !strings.Contains(err.Error(), filepath.Base(name)) {
				t.Fatalf("expected refusal naming the edited item %s, got %v", name, err)
			}
			if after := scopeTreeBytes(t, dir); !reflect.DeepEqual(after, before) {
				t.Fatal("refused item refresh changed the authoring tree")
			}
		})
	}
}

// TestScopePullRefusesModifiedServedPacketWithoutWriting covers
// UR-CLI-DRAFT-PROTECTION-001#AC2 (upper) and REQ-CROSS-332#AC3 (lower).
func TestScopePullRefusesModifiedServedPacketWithoutWriting(t *testing.T) {
	fx := scopeFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("initial pull: %v", err)
	}

	dir := scopeDir(env)
	servedPath := filepath.Join(dir, "packet", "10-recon.md")
	if err := os.WriteFile(servedPath, []byte("author's draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := scopeTreeBytes(t, dir)

	err := workingSetPullScope(env, false, wsNow.Add(time.Minute))
	if err == nil {
		t.Fatal("pull overwrote a modified served packet file; expected a refusal")
	}
	if !strings.Contains(err.Error(), "10-recon.md") {
		t.Fatalf("refusal should identify the protected path, got: %v", err)
	}
	if after := scopeTreeBytes(t, dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("refused pull changed the scope tree\nbefore: %#v\nafter:  %#v", before, after)
	}
}

// TestScopePullRequiredReadFailurePreservesAuthoringTree covers
// UR-CLI-DRAFT-PROTECTION-001#AC3 and REQ-CROSS-332#AC9. A changed item makes
// the pre-packet writes observable when the required packet read fails.
func TestScopePullRequiredReadFailurePreservesAuthoringTree(t *testing.T) {
	fx := scopeFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("initial pull: %v", err)
	}

	fx.epics[0].(map[string]any)["title"] = "changed title from store"
	fx.packetSectionsStatus = 500
	dir := scopeDir(env)
	before := scopeTreeBytes(t, dir)
	if err := workingSetPullScope(env, false, wsNow.Add(time.Minute)); err == nil {
		t.Fatal("pull accepted a failed required packet read")
	}
	if after := scopeTreeBytes(t, dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("failed required read changed the scope tree\nbefore: %#v\nafter:  %#v", before, after)
	}
}

// TestScopePull404PreservesUnservedAndUntrackedPacketFiles covers
// REQ-CROSS-332#AC7: only clean, baseline-known managed packet files may be
// removed when the endpoint is absent.
// TestScopePull404PreservesUnservedAndUntrackedPacketFiles covers REQ-CROSS-332#AC7.
func TestScopePull404PreservesUnservedAndUntrackedPacketFiles(t *testing.T) {
	fx := scopeFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	dir := scopeDir(env)
	packetDir := filepath.Join(dir, "packet")
	unserved := []byte("author draft for a key not served yet\n")
	untracked := []byte("local notes\n")
	if err := os.WriteFile(filepath.Join(packetDir, "30-red-strategy.md"), unserved, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packetDir, "local-notes.txt"), untracked, 0o644); err != nil {
		t.Fatal(err)
	}

	fx.packetSectionsStatus = 404
	if err := workingSetPullScope(env, false, wsNow.Add(time.Minute)); err != nil {
		t.Fatalf("clean 404 pull: %v", err)
	}
	for name, want := range map[string][]byte{"30-red-strategy.md": unserved, "local-notes.txt": untracked} {
		got, err := os.ReadFile(filepath.Join(packetDir, name))
		if err != nil {
			t.Fatalf("404 removed untouched %s: %v", name, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("404 changed untouched %s: got %q want %q", name, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(packetDir, "10-recon.md")); !os.IsNotExist(err) {
		t.Fatalf("404 should clean the clean baseline-known served section, stat error=%v", err)
	}
	for _, name := range []string{"40-decisions.md", "20-enrichment-REQ-CROSS-310.md", "20-enrichment-REQ-CROSS-311.md"} {
		if _, err := os.Stat(filepath.Join(packetDir, name)); !os.IsNotExist(err) {
			t.Fatalf("404 should clean the clean baseline-known stub %s, stat error=%v", name, err)
		}
	}
}

// TestScopePull404RefusesModifiedManagedPacketWithoutWriting covers
// UR-CLI-DRAFT-PROTECTION-001#AC2 and REQ-CROSS-332#AC7.
func TestScopePull404RefusesModifiedManagedPacketWithoutWriting(t *testing.T) {
	fx := scopeFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	dir := scopeDir(env)
	if err := os.WriteFile(filepath.Join(dir, "packet", "10-recon.md"), []byte("local draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := scopeTreeBytes(t, dir)
	fx.packetSectionsStatus = 404
	err := workingSetPullScope(env, false, wsNow.Add(time.Minute))
	if err == nil {
		t.Fatal("404 cleanup removed a modified managed packet file; expected refusal")
	}
	if !strings.Contains(err.Error(), "10-recon.md") {
		t.Fatalf("refusal should name the protected path, got: %v", err)
	}
	if after := scopeTreeBytes(t, dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("refused 404 changed the scope tree\nbefore: %#v\nafter:  %#v", before, after)
	}
}

// TestScopePull404ProtectsAcceptedStubAfterEditWithoutWriting covers
// REQ-CROSS-332#AC7/AC10 and UR-CLI-DRAFT-PROTECTION-001#AC2/AC3: once a
// scaffolded stub is accepted by the store, a later local edit is served state
// and must block 404 cleanup without changing any scope bytes.
func TestScopePull404ProtectsAcceptedStubAfterEditWithoutWriting(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	const name = "30-red-strategy.md"
	marker := packetStubMarker("red_strategy", "epic", "EPIC-CLI-008")
	content := marker + "\naccepted red strategy\n"
	if err := os.WriteFile(filepath.Join(dir, "packet", name), []byte(content), 0o644); err != nil {
		t.Fatalf("fill scaffolded stub: %v", err)
	}
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("push filled stub: %v", err)
	}
	if action, rec := packetPost(fx, "red_strategy"); rec == nil || action != "create" {
		t.Fatalf("expected accepted create for filled stub; action=%q record=%v", action, rec)
	}
	if err := os.WriteFile(filepath.Join(dir, "packet", name), []byte(content+"local edit after accepted push\n"), 0o644); err != nil {
		t.Fatalf("edit accepted packet section: %v", err)
	}
	before := scopeTreeBytes(t, dir)
	fx.packetSectionsStatus = 404
	err := workingSetPullScope(env, false, wsNow.Add(time.Minute))
	if err == nil {
		t.Fatal("404 cleanup accepted edits to a packet section previously created by push")
	}
	if !strings.Contains(err.Error(), name) {
		t.Fatalf("refusal should name the protected path %s, got: %v", name, err)
	}
	if after := scopeTreeBytes(t, dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("refused 404 changed the full scope tree\nbefore: %#v\nafter:  %#v", before, after)
	}
}

// TestScopePull404RefusesDeletedBaselinePacketWithoutWriting covers
// REQ-CROSS-332#AC7 and #AC10.
func TestScopePull404RefusesDeletedBaselinePacketWithoutWriting(t *testing.T) {
	fx := scopeFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	dir := scopeDir(env)
	if err := os.Remove(filepath.Join(dir, "packet", "10-recon.md")); err != nil {
		t.Fatalf("delete baseline-known packet file: %v", err)
	}
	before := scopeTreeBytes(t, dir)
	fx.packetSectionsStatus = 404
	err := workingSetPullScope(env, false, wsNow.Add(time.Minute))
	if err == nil {
		t.Fatal("404 cleanup silently accepted deletion of a baseline-known packet file")
	}
	if after := scopeTreeBytes(t, dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("refused 404 changed the scope tree\nbefore: %#v\nafter:  %#v", before, after)
	}
}

// TestScopePull404RejectsNonCanonicalBaselinePathWithoutWriting covers
// REQ-CROSS-332#AC9/AC10: a local baseline is data, not authority to unlink an
// arbitrary path during absent-packet cleanup.
func TestScopePull404RejectsNonCanonicalBaselinePathWithoutWriting(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	outside := filepath.Join(env.Root, workingSetDir, "draft-protection-escape-sentinel.md")
	if err := os.WriteFile(outside, []byte("outside sentinel\n"), 0o644); err != nil {
		t.Fatalf("write controlled outside sentinel: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	raw, err := os.ReadFile(filepath.Join(dir, scopedDraftBaselineFile))
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	var baseline scopedDraftBaseline
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatalf("decode baseline: %v", err)
	}
	baseline.Files["packet/../../draft-protection-escape-sentinel.md"] = scopedDraftBaselineEntry{
		SHA256: sha256Hex([]byte("outside sentinel\n")), Origin: "served-packet",
	}
	raw, err = json.Marshal(baseline)
	if err != nil {
		t.Fatalf("encode baseline: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, scopedDraftBaselineFile), raw, 0o644); err != nil {
		t.Fatalf("write unsafe baseline: %v", err)
	}
	before := scopeTreeBytes(t, dir)
	beforeOutside, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("read outside sentinel before pull: %v", err)
	}
	fx.packetSectionsStatus = 404
	err = workingSetPullScope(env, false, wsNow.Add(time.Minute))
	if err == nil {
		t.Fatal("unsafe baseline path must refuse 404 cleanup")
	}
	if after := scopeTreeBytes(t, dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("unsafe baseline refusal changed the scope tree\nbefore: %#v\nafter:  %#v", before, after)
	}
	if after, err := os.ReadFile(outside); err != nil || !reflect.DeepEqual(after, beforeOutside) {
		t.Fatalf("unsafe baseline refusal changed the outside sentinel: content=%q err=%v", after, err)
	}
}

// TestScopePull404RejectsUnknownBaselineOriginWithoutWriting covers
// REQ-CROSS-332#AC9/AC10: cleanup accepts only CLI-managed packet origins.
func TestScopePull404RejectsUnknownBaselineOriginWithoutWriting(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	before := scopeTreeBytes(t, dir)
	raw, err := os.ReadFile(filepath.Join(dir, scopedDraftBaselineFile))
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	var baseline scopedDraftBaseline
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatalf("decode baseline: %v", err)
	}
	entry := baseline.Files[filepath.Join("packet", "10-recon.md")]
	entry.Origin = "unknown-origin"
	baseline.Files[filepath.Join("packet", "10-recon.md")] = entry
	raw, err = json.Marshal(baseline)
	if err != nil {
		t.Fatalf("encode baseline: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, scopedDraftBaselineFile), raw, 0o644); err != nil {
		t.Fatalf("write invalid baseline: %v", err)
	}
	before = scopeTreeBytes(t, dir)
	fx.packetSectionsStatus = 404
	err = workingSetPullScope(env, false, wsNow.Add(time.Minute))
	if err == nil {
		t.Fatal("unknown baseline origin must refuse 404 cleanup")
	}
	if after := scopeTreeBytes(t, dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("unknown-origin refusal changed the scope tree\nbefore: %#v\nafter:  %#v", before, after)
	}
}

// TestScopePullLegacyBaselineRefusalIncludesManualRecovery covers
// REQ-CROSS-332#AC9 and #AC12.
func TestScopePullLegacyBaselineRefusalIncludesManualRecovery(t *testing.T) {
	fx := scopeFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	dir := scopeDir(env)
	if err := os.WriteFile(filepath.Join(dir, "packet", "10-recon.md"), []byte("legacy author draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, scopedDraftBaselineFile)); err != nil {
		t.Fatalf("remove CLI baseline to model an older scope directory: %v", err)
	}
	before := scopeTreeBytes(t, dir)
	err := workingSetPullScope(env, false, wsNow.Add(time.Minute))
	assertManualScopeRecoveryInstructions(t, err)
	if after := scopeTreeBytes(t, dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("legacy refusal changed the scope tree\nbefore: %#v\nafter:  %#v", before, after)
	}
}

func assertScopePullProtectsDeletedManagedFile(t *testing.T, relative string, packetAbsent bool) {
	t.Helper()
	fx := scopeFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	dir := scopeDir(env)
	path := filepath.Join(dir, relative)
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove managed file: %v", err)
	}
	if packetAbsent {
		fx.packetSectionsStatus = 404
	}
	before := scopeTreeBytes(t, dir)
	err = workingSetPullScope(env, false, wsNow.Add(time.Minute))
	if err == nil {
		t.Fatal("pull silently recreated a baseline-known local deletion")
	}
	if !strings.Contains(err.Error(), filepath.Base(path)) {
		t.Fatalf("refusal should identify deleted path, got: %v", err)
	}
	assertManualScopeRecoveryInstructions(t, err)
	for _, want := range []string{"nothing was written", "Restore this file exactly as last pulled and retry", "no saved copy"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("deleted-file notice must explain %q: %v", want, err)
		}
	}
	if after := scopeTreeBytes(t, dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("refused pull changed the scope tree\nbefore: %#v\nafter:  %#v", before, after)
	}
	if err := os.WriteFile(path, saved, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := workingSetPullScope(env, false, wsNow.Add(2*time.Minute)); err != nil {
		t.Fatalf("restoring the last pulled file must allow retry: %v", err)
	}
}

// TestScopePullProtectsDeletedBaselineItem covers REQ-CROSS-332#AC3 and #AC10.
func TestScopePullProtectsDeletedBaselineItem(t *testing.T) {
	assertScopePullProtectsDeletedManagedFile(t, "EPIC-CLI-008.md", false)
}

// TestScopePullProtectsDeletedBaselineServedPacket covers REQ-CROSS-332#AC3 and #AC10.
func TestScopePullProtectsDeletedBaselineServedPacket(t *testing.T) {
	assertScopePullProtectsDeletedManagedFile(t, filepath.Join("packet", "10-recon.md"), false)
}

// TestScopePullProtectsDeletedBaselineStub covers REQ-CROSS-332#AC3 and #AC10.
func TestScopePullProtectsDeletedBaselineStub(t *testing.T) {
	assertScopePullProtectsDeletedManagedFile(t, filepath.Join("packet", "30-red-strategy.md"), false)
}

func TestScopePull404DeletedFileRecoveryNotice(t *testing.T) {
	for _, name := range []string{"10-recon.md", "30-red-strategy.md"} {
		t.Run(name, func(t *testing.T) {
			assertScopePullProtectsDeletedManagedFile(t, filepath.Join("packet", name), true)
		})
	}
}

// TestUnservedDraftSurvivesRepeatedPullsAndLaterServing covers
// REQ-CROSS-332#AC5 and #AC10.
func TestUnservedDraftSurvivesRepeatedPullsAndLaterServing(t *testing.T) {
	fx := scopeFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	dir := scopeDir(env)
	draft := []byte("unserved author draft\n")
	draftPath := filepath.Join(dir, "packet", "30-red-strategy.md")
	if err := os.WriteFile(draftPath, draft, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := workingSetPullScope(env, false, wsNow.Add(time.Minute)); err != nil {
		t.Fatalf("repeat pull before section is served: %v", err)
	}
	if got, err := os.ReadFile(draftPath); err != nil || !reflect.DeepEqual(got, draft) {
		t.Fatalf("unserved draft changed: bytes=%q err=%v", got, err)
	}

	fx.packetSections = append(fx.packetSections, map[string]any{
		"scope_kind": "epic", "scope_external_id": "EPIC-CLI-008",
		"section_key": "red_strategy", "content": "server copy", "content_fingerprint": "ps-fp-2",
	})
	before := scopeTreeBytes(t, dir)
	err := workingSetPullScope(env, false, wsNow.Add(2*time.Minute))
	if err == nil {
		t.Fatal("later serving silently replaced an unserved local draft")
	}
	if !strings.Contains(err.Error(), "local edits") {
		t.Fatalf("untouched baseline entry was not retained to identify local edits: %v", err)
	}
	if after := scopeTreeBytes(t, dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("refused later serving changed the scope tree\nbefore: %#v\nafter:  %#v", before, after)
	}
}

// TestScopePullRequiredDeliveryContextFailurePreservesAuthoringTree covers
// UR-CLI-DRAFT-PROTECTION-001#AC3 and REQ-CROSS-332#AC9.
func TestScopePullRequiredDeliveryContextFailurePreservesAuthoringTree(t *testing.T) {
	fx := scopeFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	fx.epics[0].(map[string]any)["title"] = "changed title from store"
	fx.deliveryContextStatus = 500
	dir := scopeDir(env)
	before := scopeTreeBytes(t, dir)
	if err := workingSetPullScope(env, false, wsNow.Add(time.Minute)); err == nil {
		t.Fatal("pull swallowed a failed required delivery-context read")
	}
	if after := scopeTreeBytes(t, dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("failed required delivery-context read changed the scope tree\nbefore: %#v\nafter:  %#v", before, after)
	}
}

func assertMalformedScopeSelectionRefused(t *testing.T, mutate func(map[string]any)) {
	t.Helper()
	fx := scopeFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	dir := scopeDir(env)
	mutate(fx.workSelection["current"].(map[string]any))
	before := scopeTreeBytes(t, dir)
	if err := workingSetPullScope(env, false, wsNow.Add(time.Minute)); err == nil {
		t.Fatal("pull accepted malformed scope-selection input")
	}
	if after := scopeTreeBytes(t, dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("malformed selection changed the scope tree\nbefore: %#v\nafter:  %#v", before, after)
	}
}

// TestScopePullRejectsNonStringSelectionMemberBeforeWriting covers
// UR-CLI-DRAFT-PROTECTION-001#AC3 and REQ-CROSS-332#AC9.
func TestScopePullRejectsNonStringSelectionMemberBeforeWriting(t *testing.T) {
	assertMalformedScopeSelectionRefused(t, func(current map[string]any) {
		current["members"] = []any{"REQ-CROSS-310", 42}
	})
}

// TestScopePullRejectsUnknownScopeKindBeforeWriting covers
// UR-CLI-DRAFT-PROTECTION-001#AC3 and REQ-CROSS-332#AC9.
func TestScopePullRejectsUnknownScopeKindBeforeWriting(t *testing.T) {
	assertMalformedScopeSelectionRefused(t, func(current map[string]any) {
		current["scope_kind"] = "not-a-supported-kind"
	})
}

// These malformed-response guards are regression checks for the staging
// parser added in the first GREEN slice; they are not claimed as new RED.
// TestScopePullRejectsMalformedPacketContentsBeforeWriting covers
// REQ-CROSS-332#AC9.
func TestScopePullRejectsMalformedPacketContentsBeforeWriting(t *testing.T) {
	fx := scopeFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	dir := scopeDir(env)
	before := scopeTreeBytes(t, dir)
	fx.packetSections = []any{map[string]any{"section_key": "reconnaissance", "content": 42}}
	if err := workingSetPullScope(env, false, wsNow.Add(time.Minute)); err == nil {
		t.Fatal("pull accepted a packet section without string content")
	}
	if after := scopeTreeBytes(t, dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("malformed packet response changed the scope tree\nbefore: %#v\nafter:  %#v", before, after)
	}
}

// TestScopePullRejectsPacketFilenameCollisionBeforeWriting covers
// REQ-CROSS-332#AC9.
func TestScopePullRejectsPacketFilenameCollisionBeforeWriting(t *testing.T) {
	fx := scopeFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	dir := scopeDir(env)
	before := scopeTreeBytes(t, dir)
	fx.packetSections = []any{
		map[string]any{"section_key": "enrichment:REQ-CROSS-310", "content": "first"},
		map[string]any{"section_key": "20-enrichment-REQ-CROSS-310", "content": "collision"},
	}
	if err := workingSetPullScope(env, false, wsNow.Add(time.Minute)); err == nil {
		t.Fatal("pull accepted packet keys that alias one filename")
	}
	if after := scopeTreeBytes(t, dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("colliding packet response changed the scope tree\nbefore: %#v\nafter:  %#v", before, after)
	}
}

func TestREQCROSS313PullScopeMaterializesTheScopeDirectory(t *testing.T) {
	fx := scopeFixture()
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull --scope: %v", err)
	}
	dir := scopeDir(env)

	// the scope item, carrying its SERVED fingerprint in the header
	scope := readScopeFile(t, filepath.Join(dir, "EPIC-CLI-008.md"))
	if !strings.Contains(scope, "epic-served-fp") {
		t.Fatalf("scope file does not carry the served fingerprint:\n%s", scope)
	}

	// members materialized WITHOUT the caller naming them (resolved from selection)
	for _, m := range []string{"REQ-CROSS-310", "REQ-CROSS-311"} {
		if _, err := os.Stat(filepath.Join(dir, "members", m+".md")); err != nil {
			t.Fatalf("member %s not materialized: %v", m, err)
		}
	}

	// the selection projection
	if _, err := os.Stat(filepath.Join(dir, "SELECTION.md")); err != nil {
		t.Fatalf("SELECTION.md not written: %v", err)
	}

	// the packet section, mapped to its canonical file with content
	recon := readScopeFile(t, filepath.Join(dir, "packet", "10-recon.md"))
	if !strings.Contains(recon, "the surface") {
		t.Fatalf("recon packet file missing content:\n%s", recon)
	}

	// findings projection renders "unavailable" (the /sync/findings read is 404
	// until SR-315), and the pull does not fail
	findings := readScopeFile(t, filepath.Join(dir, "findings", "COLD-REVIEW.md"))
	if !strings.Contains(strings.ToLower(findings), "unavailable") {
		t.Fatalf("findings projection should be unavailable:\n%s", findings)
	}
}

func TestREQCROSS313ForReviewIsReadOnlyAndCarriesAReviewContextId(t *testing.T) {
	fx := scopeFixture()
	fx.deliveryContext = reviewDeliveryContext(strings.Repeat("a", 64))
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPullScope(env, true, wsNow); err != nil {
		t.Fatalf("pull --scope --for-review: %v", err)
	}
	dir := onlyReviewSnapshot(t, env)

	member := readScopeFile(t, filepath.Join(dir, "members", "REQ-CROSS-310.md"))
	// review render carries no editable field block — every block is read-only
	if strings.Contains(member, editableFieldMarker) {
		t.Fatalf("--for-review member carries an editable block:\n%s", member)
	}

	// the directory is stamped with a review-context id the push engine refuses
	ctx := readScopeFile(t, filepath.Join(dir, contextFile))
	if !strings.Contains(ctx, "review") {
		t.Fatalf("context stamp is not review mode:\n%s", ctx)
	}
	if !strings.Contains(ctx, "context_id:") {
		t.Fatalf("context stamp carries no context id:\n%s", ctx)
	}
}

func TestReviewPullWritesIsolatedSnapshotWithoutChangingAuthoringTree(t *testing.T) {
	fx, env, before := preparedReviewScope(t)
	fx.deliveryContext = reviewDeliveryContext(strings.Repeat("a", 64))
	if err := workingSetPullScope(env, true, wsNow); err != nil {
		t.Fatalf("pull --scope --for-review: %v", err)
	}
	if after := scopeTreeBytes(t, scopeDir(env)); !reflect.DeepEqual(after, before) {
		t.Fatal("review pull changed the authoring tree")
	}
	snapshotDir := onlyReviewSnapshot(t, env)
	manifestBytes, err := os.ReadFile(filepath.Join(snapshotDir, "MANIFEST.json"))
	if err != nil {
		t.Fatalf("read review manifest: %v", err)
	}
	var manifest reviewSnapshotManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("decode review manifest: %v", err)
	}
	if manifest.StoreURL != env.APIURL || manifest.SystemID != env.SystemID || manifest.ScopeKind != "epic" || manifest.ScopeExternalID != "EPIC-CLI-008" || manifest.ContextID != filepath.Base(snapshotDir) || manifest.AggregateFingerprint != strings.Repeat("a", 64) {
		t.Fatalf("review manifest is missing its store/system/context/scope/aggregate binding: %+v", manifest)
	}
	digest, err := reviewSnapshotDigest(manifest)
	if err != nil || digest != manifest.SnapshotDigest {
		t.Fatalf("review manifest digest mismatch: got %q want %q err=%v", manifest.SnapshotDigest, digest, err)
	}
	for name, want := range manifest.Files {
		content, err := os.ReadFile(filepath.Join(snapshotDir, filepath.FromSlash(name)))
		if err != nil || sha256Hex(content) != want {
			t.Fatalf("review manifest file digest mismatch for %s: want %s err=%v", name, want, err)
		}
	}
}

func TestReviewPullRefusesMissingAggregateBeforeSnapshotWrite(t *testing.T) {
	fx, env, before := preparedReviewScope(t)
	fx.deliveryContext = reviewDeliveryContext("")
	err := workingSetPullScope(env, true, wsNow)
	if err == nil {
		t.Fatal("review pull should refuse when no aggregate fingerprint is available")
	}
	if after := scopeTreeBytes(t, scopeDir(env)); !reflect.DeepEqual(after, before) {
		t.Fatalf("missing aggregate changed authoring tree\nbefore: %#v\nafter:  %#v", before, after)
	}
	if _, statErr := os.Stat(reviewScopeRoot(env)); !os.IsNotExist(statErr) {
		t.Fatalf("missing aggregate should leave no review snapshot, stat err=%v", statErr)
	}
}

func TestReviewPullRefusesSelectionDriftBeforeSnapshotWrite(t *testing.T) {
	fx, env, before := preparedReviewScope(t)
	fx.deliveryContext = reviewDeliveryContext(strings.Repeat("a", 64))
	fx.workSelectionReads = 0
	fx.afterWorkSelectionRead = func(read int) {
		if read == 1 {
			current := fx.workSelection["current"].(map[string]any)
			current["fingerprint"] = strings.Repeat("b", 64)
		}
	}
	err := workingSetPullScope(env, true, wsNow)
	if err == nil {
		t.Fatal("review pull should refuse when the selection changes during snapshot reads")
	}
	if after := scopeTreeBytes(t, scopeDir(env)); !reflect.DeepEqual(after, before) {
		t.Fatalf("selection drift changed authoring tree\nbefore: %#v\nafter:  %#v", before, after)
	}
	if _, statErr := os.Stat(reviewScopeRoot(env)); !os.IsNotExist(statErr) {
		t.Fatalf("selection drift should leave no review snapshot, stat err=%v", statErr)
	}
}

func TestReviewPullRefusesItemDriftBeforeSnapshotWrite(t *testing.T) {
	fx, env, before := preparedReviewScope(t)
	fx.deliveryContext = reviewDeliveryContext(strings.Repeat("a", 64))
	fx.afterExactItemsRead = func(ids []string) {
		if contains(ids, "REQ-CROSS-310") {
			fx.requirements[0].(map[string]any)["fingerprint"] = strings.Repeat("c", 64)
		}
	}
	err := workingSetPullScope(env, true, wsNow)
	if err == nil {
		t.Fatal("review pull should refuse when a selected item changes during snapshot reads")
	}
	if after := scopeTreeBytes(t, scopeDir(env)); !reflect.DeepEqual(after, before) {
		t.Fatalf("item drift changed authoring tree\nbefore: %#v\nafter:  %#v", before, after)
	}
	if _, statErr := os.Stat(reviewScopeRoot(env)); !os.IsNotExist(statErr) {
		t.Fatalf("item drift should leave no review snapshot, stat err=%v", statErr)
	}
}

func TestReviewPullRefusesPacketDriftBeforeSnapshotWrite(t *testing.T) {
	fx, env, before := preparedReviewScope(t)
	fx.deliveryContext = reviewDeliveryContext(strings.Repeat("a", 64))
	fx.packetSectionsReads = 0
	fx.afterPacketSectionsRead = func(read int) {
		if read == 1 {
			fx.packetSections[0].(map[string]any)["content_fingerprint"] = strings.Repeat("d", 64)
		}
	}
	err := workingSetPullScope(env, true, wsNow)
	if err == nil {
		t.Fatal("review pull should refuse when a packet section changes during snapshot reads")
	}
	if after := scopeTreeBytes(t, scopeDir(env)); !reflect.DeepEqual(after, before) {
		t.Fatalf("packet drift changed authoring tree\nbefore: %#v\nafter:  %#v", before, after)
	}
	if _, statErr := os.Stat(reviewScopeRoot(env)); !os.IsNotExist(statErr) {
		t.Fatalf("packet drift should leave no review snapshot, stat err=%v", statErr)
	}
}

func TestReviewPullRefusesRequiredFactsDriftBeforeSnapshotWrite(t *testing.T) {
	fx, env, before := preparedReviewScope(t)
	fx.deliveryContext = reviewDeliveryContext(strings.Repeat("a", 64))
	fx.deliveryContextReads = 0
	fx.afterDeliveryContextRead = func(read int) {
		if read == 1 {
			facts := fx.deliveryContext["facts"].(map[string]any)
			sections := facts["sections"].(map[string]any)
			sections["required"] = []any{"reconnaissance", "changed"}
		}
	}
	err := workingSetPullScope(env, true, wsNow)
	if err == nil {
		t.Fatal("review pull should refuse when required delivery facts change during snapshot reads")
	}
	if after := scopeTreeBytes(t, scopeDir(env)); !reflect.DeepEqual(after, before) {
		t.Fatalf("required-facts drift changed authoring tree\nbefore: %#v\nafter:  %#v", before, after)
	}
	if _, statErr := os.Stat(reviewScopeRoot(env)); !os.IsNotExist(statErr) {
		t.Fatalf("required-facts drift should leave no review snapshot, stat err=%v", statErr)
	}
}

func TestREQCROSS313OrdinaryPullStampsAnAuthoringContextId(t *testing.T) {
	fx := scopeFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatal(err)
	}
	ctx := readScopeFile(t, filepath.Join(scopeDir(env), contextFile))
	if !strings.Contains(ctx, "authoring") {
		t.Fatalf("ordinary pull should stamp authoring mode:\n%s", ctx)
	}
}

// #1 (external review): a served identifier is a filesystem path component only
// after it is proven a single safe component. A scope external id, a member id,
// or a section key carrying `../` or a separator must be refused before any
// join, never allowed to escape the working-set directory.
func TestPullScopeRefusesTraversingScopeId(t *testing.T) {
	fx := scopeFixture()
	sel := fx.workSelection["current"].(map[string]any)
	sel["scope_external_id"] = "../../pwned"
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPullScope(env, false, wsNow); err == nil {
		t.Fatal("pull --scope accepted a traversing scope id; expected a refusal")
	}
	// nothing was written outside the working-set directory
	escaped := filepath.Join(env.Root, ".modernpath", "pwned.md")
	if _, err := os.Stat(escaped); err == nil {
		t.Fatalf("a file escaped to %s", escaped)
	}
}

func TestPullScopeRefusesTraversingMemberId(t *testing.T) {
	fx := scopeFixture()
	sel := fx.workSelection["current"].(map[string]any)
	sel["members"] = []any{"../../../etc-pwned"}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPullScope(env, false, wsNow); err == nil {
		t.Fatal("pull --scope accepted a traversing member id; expected a refusal")
	}
}

// A real error from the packet-sections endpoint (auth, network, 5xx) must fail
// the pull, not be swallowed as "honest absence" — otherwise a reused scope dir
// keeps its previously-pulled packet files and stamps them into the new context.
func TestPullScopePropagatesPacketSectionServerError(t *testing.T) {
	fx := scopeFixture()
	fx.packetSectionsStatus = 500
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPullScope(env, false, wsNow); err == nil {
		t.Fatal("a 500 from packet-sections must fail the pull, not be swallowed as absence")
	}
}

// A 404 is a genuinely absent endpoint (an older server): honest absence, so the
// pull succeeds — but any packet files a prior pull left in the reused scope dir
// must be cleaned, never carried into the new context.
func TestPullScopeCleansStalePacketFilesWhenTheEndpointIsAbsent(t *testing.T) {
	fx := scopeFixture()
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("first pull: %v", err)
	}
	dir := scopeDir(env)
	if entries, _ := os.ReadDir(filepath.Join(dir, "packet")); len(entries) == 0 {
		t.Fatal("precondition: the first pull should have written packet files")
	}

	fx.packetSectionsStatus = 404
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("an absent (404) packet-sections endpoint is honest absence and must not fail the pull: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "packet")); len(entries) != 0 {
		t.Fatalf("stale packet files must be cleaned on an absent endpoint, found %d", len(entries))
	}
}

// REQ-CROSS-332 — an authoring pull scaffolds the canonical packet stubs the
// phase table requires and the store does not serve: the fixed three plus one
// enrichment per selected SR member the index knows as a system requirement. A
// stub never overwrites a local file; a UR member and an unknown member get
// none; --for-review and a 404 scaffold nothing. RED: pull writes only served
// sections today.

// scaffoldFixture is an epic scope with one SR member and one UR member, and the
// store serves no packet sections.
func scaffoldFixture() *wsFixture {
	sel := scopeSelection()
	sel["current"].(map[string]any)["members"] = []any{"REQ-CROSS-310", "UR-CLI-008"}
	return &wsFixture{
		workSelection: sel,
		epics: []any{map[string]any{
			"external_id": "EPIC-CLI-008", "title": "Process navigation brain",
			"fingerprint": "epic-served-fp",
		}},
		requirements: []any{map[string]any{
			"external_id": "REQ-CROSS-310", "title": "sr", "context": "CROSS",
			"work_status": "IN_REVIEW", "fingerprint": "sr310-fp",
		}},
		userRequirements: []any{wsUserReq("UR-CLI-008", "the user outcome")},
		packetSections:   nil,
	}
}

func packetMdCount(t *testing.T, dir string) int {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Join(dir, "packet"))
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			n++
		}
	}
	return n
}

func TestREQCROSS332PullScaffoldsTheRequiredStubsWhenTheStoreServesNone(t *testing.T) {
	fx := scaffoldFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull: %v", err)
	}
	dir := scopeDir(env)
	// SR-CLI-028-001 (EPIC-CLI-028, D3): the state inventory joins the scaffold set.
	want := []string{"10-recon.md", "15-state-inventory.md", "30-red-strategy.md", "40-decisions.md", "20-enrichment-REQ-CROSS-310.md"}
	for _, f := range want {
		content := readScopeFile(t, filepath.Join(dir, "packet", f))
		if !unfilledPacketSection(content, sectionKeyFromFile(f), "epic", "EPIC-CLI-008") {
			t.Fatalf("%s must be scaffolded as an unfilled stub, got:\n%s", f, content)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "packet", "20-enrichment-UR-CLI-008.md")); err == nil {
		t.Fatal("a user-requirement member must get no enrichment stub")
	}
	if got := packetMdCount(t, dir); got != len(want) {
		t.Fatalf("packet/ must hold exactly %d stubs, found %d", len(want), got)
	}
}

func TestREQCROSS332PullScaffoldsOnlyTheMissingKeysBesideServedSections(t *testing.T) {
	fx := scopeFixture() // serves reconnaissance; members 310 and 311 are both SRs
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull: %v", err)
	}
	dir := scopeDir(env)
	recon := readScopeFile(t, filepath.Join(dir, "packet", "10-recon.md"))
	if !strings.Contains(recon, "the surface") {
		t.Fatalf("a served section must be written whole, got:\n%s", recon)
	}
	if unfilledPacketSection(recon, "reconnaissance", "epic", "EPIC-CLI-008") {
		t.Fatal("a served section must not be scaffolded over with a stub")
	}
	for _, f := range []string{"30-red-strategy.md", "40-decisions.md", "20-enrichment-REQ-CROSS-310.md", "20-enrichment-REQ-CROSS-311.md"} {
		content := readScopeFile(t, filepath.Join(dir, "packet", f))
		if !unfilledPacketSection(content, sectionKeyFromFile(f), "epic", "EPIC-CLI-008") {
			t.Fatalf("%s must be a stub, got:\n%s", f, content)
		}
	}
}

func TestREQCROSS332PullScaffoldsTheScopeSRForASingleSRScope(t *testing.T) {
	sel := scopeSelection()
	cur := sel["current"].(map[string]any)
	cur["scope_kind"] = "single_sr"
	cur["scope_external_id"] = "REQ-CROSS-310"
	cur["members"] = []any{}
	fx := &wsFixture{
		workSelection: sel,
		requirements: []any{map[string]any{
			"external_id": "REQ-CROSS-310", "title": "sr", "context": "CROSS",
			"work_status": "IN_REVIEW", "fingerprint": "sr310-fp",
		}},
		packetSections: nil,
	}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull: %v", err)
	}
	dir := filepath.Join(env.Root, ".modernpath", "working-set", "REQ-CROSS-310")
	for _, f := range []string{"10-recon.md", "30-red-strategy.md", "40-decisions.md", "20-enrichment-REQ-CROSS-310.md"} {
		if _, err := os.Stat(filepath.Join(dir, "packet", f)); err != nil {
			t.Fatalf("a single_sr scope must scaffold %s: %v", f, err)
		}
	}
}

func TestREQCROSS332PullGivesAnUnknownMemberNoStub(t *testing.T) {
	sel := scopeSelection()
	sel["current"].(map[string]any)["members"] = []any{"REQ-CROSS-310", "REQ-CROSS-999"}
	fx := &wsFixture{
		workSelection: sel,
		epics:         []any{map[string]any{"external_id": "EPIC-CLI-008", "title": "e", "fingerprint": "epic-served-fp"}},
		requirements: []any{map[string]any{
			"external_id": "REQ-CROSS-310", "title": "sr", "context": "CROSS",
			"work_status": "IN_REVIEW", "fingerprint": "sr310-fp",
		}},
		packetSections: nil,
	}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull: %v", err)
	}
	dir := scopeDir(env)
	if _, err := os.Stat(filepath.Join(dir, "packet", "20-enrichment-REQ-CROSS-310.md")); err != nil {
		t.Fatalf("the known SR member must get a stub: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "packet", "20-enrichment-REQ-CROSS-999.md")); err == nil {
		t.Fatal("an unknown member must get no enrichment stub (the store never requires it)")
	}
}

func TestREQCROSS332PullNeverOverwritesALocalFileForAnUnservedKey(t *testing.T) {
	fx := scopeFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("first pull: %v", err)
	}
	dir := scopeDir(env)
	filled := "the real red strategy the author wrote\n"
	if err := os.WriteFile(filepath.Join(dir, "packet", "30-red-strategy.md"), []byte(filled), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("second pull: %v", err)
	}
	if got := readScopeFile(t, filepath.Join(dir, "packet", "30-red-strategy.md")); got != filled {
		t.Fatalf("a local file for an unserved key must be left byte-identical, got:\n%s", got)
	}
}

func TestREQCROSS332ForReviewPullScaffoldsNothing(t *testing.T) {
	fx := scaffoldFixture()
	fx.deliveryContext = reviewDeliveryContext(strings.Repeat("a", 64))
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, true, wsNow); err != nil {
		t.Fatalf("for-review pull: %v", err)
	}
	if got := packetMdCount(t, onlyReviewSnapshot(t, env)); got != 0 {
		t.Fatalf("--for-review must scaffold no stubs, found %d", got)
	}
}

func TestREQCROSS332AnAbsentEndpointScaffoldsNothing(t *testing.T) {
	fx := scaffoldFixture()
	fx.packetSectionsStatus = 404
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if got := packetMdCount(t, scopeDir(env)); got != 0 {
		t.Fatalf("a 404 endpoint must scaffold nothing, found %d stub(s)", got)
	}
}

// Guard (green once REQ-CROSS-331 is in): a pull immediately followed by a push
// posts nothing — the scaffolded stubs are all unfilled, and push skips them.
func TestREQCROSS332PullThenPushPostsNothing(t *testing.T) {
	fx := scaffoldFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull: %v", err)
	}
	fx.authorPosts = nil
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("push: %v", err)
	}
	if len(fx.authorPosts) != 0 {
		t.Fatalf("a pull then a push with no edits must post nothing, got %v", fx.authorPosts)
	}
}
