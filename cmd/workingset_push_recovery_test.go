package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLegacyScopePushExplainsRecoveryBeforeAnyWrite(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	edit(t, memberPath(dir, "REQ-CROSS-310"), func(s string) string {
		return strings.Replace(s, "the reads", "the reads and writes", 1)
	})
	if err := os.Remove(filepath.Join(dir, scopedDraftBaselineFile)); err != nil {
		t.Fatal(err)
	}
	before := scopeTreeBytes(t, dir)
	err := workingSetPush(env, false)
	assertManualScopeRecoveryInstructions(t, err)
	for _, want := range []string{"no local draft baseline", "older CLI", "nothing was written"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("recovery notice must explain %q: %v", want, err)
		}
	}
	if len(fx.authorPosts) != 0 || !reflect.DeepEqual(before, scopeTreeBytes(t, dir)) {
		t.Fatal("legacy-scope refusal changed store records or local files")
	}
}

func TestAcceptedScaffoldCreateMetadataFailureThenRetryRecovers(t *testing.T) {
	fx := scaffoldFixture()
	env, dir := pulledScope(t, fx)
	const name = "packet/30-red-strategy.md"
	const body = "the red test plan\n"
	baselineBefore := scopedBaselineEntryForTest(t, dir, name)
	if baselineBefore.Origin != "stub" || readPacketFingerprints(dir)["red_strategy"] != "" {
		t.Fatal("fixture must start with a known scaffold and no packet CAS entry")
	}
	authored := readScopeFile(t, filepath.Join(dir, name)) + body
	writePacketFile(t, dir, "30-red-strategy.md", authored)
	manifest := packetFingerprintManifest(dir)
	priorManifest, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	_, contextID := readContextStamp(dir)
	var callbackErr error
	fx.afterAuthorPost = func(rec map[string]any) {
		if str(rec, "section_key") != "red_strategy" {
			return
		}
		fx.packetSections = []any{map[string]any{
			"section_key": "red_strategy", "content": body,
			"content_fingerprint": "accepted-scaffold-fp", "authoring_context_id": contextID,
		}}
		if err := os.Remove(manifest); err != nil {
			callbackErr = err
			return
		}
		callbackErr = os.Mkdir(manifest, 0o755)
	}
	err = workingSetPush(env, false)
	if callbackErr != nil {
		t.Fatal(callbackErr)
	}
	if err == nil || !strings.Contains(err.Error(), "CAS metadata could not be refreshed") {
		t.Fatalf("want accepted create followed by CAS metadata failure, got %v", err)
	}
	if len(fx.authorPosts) != 1 || fx.authorPosts[0]["action"] != "create" {
		t.Fatalf("want one accepted create, got %v", fx.authorPosts)
	}
	if got := scopedBaselineEntryForTest(t, dir, name); got != baselineBefore {
		t.Fatal("failed CAS refresh advanced the scaffold baseline")
	}
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, priorManifest, 0o644); err != nil {
		t.Fatal(err)
	}
	fx.afterAuthorPost = nil
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("retry should establish the accepted scaffold CAS without manual recovery: %v", err)
	}
	if len(fx.authorPosts) != 1 {
		t.Fatalf("retry repeated an accepted create: %v", fx.authorPosts)
	}
	if got := readScopeFile(t, filepath.Join(dir, name)); got != authored {
		t.Fatalf("retry changed authored bytes: %q", got)
	}
	if got := readPacketFingerprints(dir)["red_strategy"]; got != "accepted-scaffold-fp" {
		t.Fatalf("recovered CAS = %q", got)
	}
	assertScopedBaselineMatchesFile(t, dir, name)
	if got := scopedBaselineEntryForTest(t, dir, name); got.Origin != "served-packet" {
		t.Fatalf("accepted scaffold origin = %q", got.Origin)
	}
}

func TestItemOnlyPushWithAbsentPacketEndpoint(t *testing.T) {
	for _, restamp := range []bool{false, true} {
		name := "automatic"
		if restamp {
			name = "explicit_restamp"
		}
		t.Run(name, func(t *testing.T) {
			previous := wsPushRestamp
			wsPushRestamp = restamp
			t.Cleanup(func() { wsPushRestamp = previous })
			fx := scopeFixture()
			fx.packetSectionsStatus = 404
			env, dir := pulledScope(t, fx)
			if _, err := os.Stat(filepath.Join(dir, "packet")); !os.IsNotExist(err) {
				t.Fatalf("404 pull must leave no packet directory: %v", err)
			}
			edit(t, memberPath(dir, "REQ-CROSS-310"), func(s string) string {
				return strings.Replace(s, "the reads", "the reads and writes", 1)
			})
			canonical := canonicalScopeRequirement(fx, "the reads and writes", "item-only-fp")
			fx.authorSyncItems = map[string]any{"REQ-CROSS-310": map[string]any{
				"kind": "system", "item": canonical, "gates": []any{},
			}}
			fx.deliveryContext = map[string]any{"facts": map[string]any{
				"sections": map[string]any{"missing": []any{"reconnaissance"}},
			}}
			beforeDryRun := scopeTreeBytes(t, dir)
			if err := workingSetPush(env, true); err != nil {
				t.Fatalf("item-only dry run with absent packet endpoint: %v", err)
			}
			if len(fx.authorPosts) != 0 || !reflect.DeepEqual(scopeTreeBytes(t, dir), beforeDryRun) {
				t.Fatal("dry run wrote store records or local files")
			}
			if err := workingSetPush(env, false); err != nil {
				t.Fatalf("item-only push with absent packet endpoint: %v", err)
			}
			if len(fx.authorPosts) != 1 || patchPostFor(fx, "REQ-CROSS-310") == nil {
				t.Fatalf("want exactly one item patch, got %v", fx.authorPosts)
			}
			assertScopedBaselineMatchesFile(t, dir, "members/REQ-CROSS-310.md")
		})
	}
}

func TestScaffoldRetryRefusesConcurrentEditBeforeRestamping(t *testing.T) {
	fx := scaffoldFixture()
	env, dir := pulledScope(t, fx)
	writePacketFile(t, dir, "30-red-strategy.md", "accepted plan\n")
	fx.packetSections = []any{map[string]any{
		"section_key": "red_strategy", "content": "accepted plan\n",
		"content_fingerprint": "accepted-scaffold-fp",
	}}
	var plan, skipped []string
	writes, reconcile, err := planPacketSections(env, dir, "epic", "EPIC-CLI-008", &plan, &skipped)
	if err != nil || len(writes) != 0 || len(reconcile) != 1 {
		t.Fatalf("expected metadata reconciliation of accepted scaffold: writes=%v reconcile=%v err=%v", writes, reconcile, err)
	}
	writePacketFile(t, dir, "30-red-strategy.md", "concurrent author edit\n")
	before := scopeTreeBytes(t, dir)
	err = reconcilePacketSections(dir, "epic", "EPIC-CLI-008", reconcile)
	if err == nil || !strings.Contains(err.Error(), "changed during packet retry reconciliation") {
		t.Fatalf("expected concurrent-edit refusal, got %v", err)
	}
	if len(fx.authorPosts) != 0 || !reflect.DeepEqual(before, scopeTreeBytes(t, dir)) {
		t.Fatal("concurrent-edit refusal changed store records or local files")
	}
}

func TestPacketReadFailureRefusesBeforeItemWrites(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		packet bool
	}{
		{"absent_with_packet", 404, true},
		{"server_error_without_packet", 500, false},
		{"forbidden_without_packet", 403, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := scopeFixture()
			fx.packetSectionsStatus = 404
			env, dir := pulledScope(t, fx)
			edit(t, memberPath(dir, "REQ-CROSS-310"), func(s string) string {
				return strings.Replace(s, "the reads", "the reads and writes", 1)
			})
			if tc.packet {
				if err := os.Mkdir(filepath.Join(dir, "packet"), 0o755); err != nil {
					t.Fatal(err)
				}
				writePacketFile(t, dir, "notes.md", "unserved draft\n")
			}
			fx.packetSectionsStatus = tc.status
			before := scopeTreeBytes(t, dir)
			if err := workingSetPush(env, false); err == nil {
				t.Fatal("push must refuse an unavailable required packet read")
			}
			if len(fx.authorPosts) != 0 || !reflect.DeepEqual(before, scopeTreeBytes(t, dir)) {
				t.Fatal("packet read failure changed store records or local files")
			}
		})
	}
}

func TestPacketRetryMissingCASRefusesServedAndUnbaselinedFiles(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "served_baseline"
		if legacy {
			name = "unbaselined"
		}
		t.Run(name, func(t *testing.T) {
			fx := scopeFixture()
			env, dir := pulledScope(t, fx)
			if err := writePacketFingerprints(dir, map[string]string{}); err != nil {
				t.Fatal(err)
			}
			if legacy {
				path := filepath.Join(dir, scopedDraftBaselineFile)
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var baseline scopedDraftBaseline
				if err := json.Unmarshal(raw, &baseline); err != nil {
					t.Fatal(err)
				}
				delete(baseline.Files, "packet/10-recon.md")
				raw, err = json.Marshal(baseline)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, raw, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			before := scopeTreeBytes(t, dir)
			err := workingSetPush(env, false)
			assertManualScopeRecoveryInstructions(t, err)
			if len(fx.authorPosts) != 0 || !reflect.DeepEqual(before, scopeTreeBytes(t, dir)) {
				t.Fatal("missing legacy CAS refusal changed store records or local files")
			}
		})
	}
}

// REQ-CROSS-332 AC12: a missing pull-time CAS never authorizes replacing
// server content, including a section created after a local scaffold was pulled.
func TestChangedPacketMissingCASRefusesBeforeAnyWrite(t *testing.T) {
	for _, origin := range []string{"served", "stub", "untracked"} {
		t.Run(origin, func(t *testing.T) {
			fx := scopeFixture()
			if origin != "served" {
				fx.packetSections = nil
			}
			env, dir := pulledScope(t, fx)
			if origin == "untracked" {
				raw, err := os.ReadFile(filepath.Join(dir, scopedDraftBaselineFile))
				if err != nil {
					t.Fatal(err)
				}
				var baseline scopedDraftBaseline
				if err := json.Unmarshal(raw, &baseline); err != nil {
					t.Fatal(err)
				}
				delete(baseline.Files, "packet/10-recon.md")
				raw, err = json.Marshal(baseline)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, scopedDraftBaselineFile), raw, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := writePacketFingerprints(dir, map[string]string{}); err != nil {
				t.Fatal(err)
			}
			writePacketFile(t, dir, "10-recon.md", "local draft\n")
			edit(t, memberPath(dir, "REQ-CROSS-310"), func(s string) string {
				return strings.Replace(s, "the reads", "the reads and writes", 1)
			})
			fx.packetSections = []any{map[string]any{
				"section_key": "reconnaissance", "content": "new server content\n", "content_fingerprint": "new-server-fp",
			}}
			before := scopeTreeBytes(t, dir)
			err := workingSetPush(env, false)
			assertManualScopeRecoveryInstructions(t, err)
			if len(fx.authorPosts) != 0 || !reflect.DeepEqual(before, scopeTreeBytes(t, dir)) {
				t.Fatal("missing CAS refusal changed store records or local files")
			}
		})
	}
}
