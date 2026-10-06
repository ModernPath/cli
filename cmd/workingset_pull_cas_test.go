package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stagedPacketPullForTest(t *testing.T, env *factoryEnv, dir string) scopedPullPlan {
	t.Helper()
	packet, err := stagePacketSections(env, dir, "epic", "EPIC-CLI-008", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan := scopedPullPlan{files: map[string]scopedPullFile{}}
	plan.addPacket(packet)
	return plan
}

// REQ-CROSS-332: a partial pull must retain the old CAS for every section
// whose file was not refreshed, so push cannot overwrite a concurrent update.
func TestScopePullPacketWriteFailureRetainsCASAndConflictsOnPush(t *testing.T) {
	for _, failedKey := range []string{"reconnaissance", "decisions"} {
		t.Run(failedKey, func(t *testing.T) {
			fx := scopeFixture()
			fx.enforcePacketCAS = true
			fx.packetSections = append(fx.packetSections, map[string]any{
				"section_key": "decisions", "content": "old decisions\n", "content_fingerprint": "old-decisions-fp",
			})
			env, dir := pulledScope(t, fx)
			oldPins := readPacketFingerprints(dir)
			oldBody := readScopeFile(t, filepath.Join(dir, "packet", packetFileName(failedKey)))
			for _, section := range fx.packetSections {
				section := section.(map[string]any)
				key := str(section, "section_key")
				section["content"] = "new server " + key + "\n"
				section["content_fingerprint"] = "new-" + key + "-fp"
			}
			plan := stagedPacketPullForTest(t, env, dir)
			blocked := filepath.Join(dir, "packet", packetFileName(failedKey))
			err := applyScopedPullPlanWithWriter(dir, plan, func(path string, content []byte) error {
				if path == blocked {
					return &os.PathError{Op: "rename", Path: path, Err: os.ErrPermission}
				}
				return atomicWrite(path, content)
			})
			if !errors.Is(err, os.ErrPermission) {
				t.Fatalf("expected packet write failure, got %v", err)
			}
			if got := readScopeFile(t, blocked); got != oldBody {
				t.Fatalf("failed write changed packet content: %q", got)
			}
			pins := readPacketFingerprints(dir)
			if got := pins[failedKey]; got != oldPins[failedKey] {
				t.Errorf("failed section CAS advanced from %q to %q", oldPins[failedKey], got)
			}
			if failedKey == "decisions" {
				if got := pins["reconnaissance"]; got != "new-reconnaissance-fp" {
					t.Errorf("completed section CAS was not checkpointed: %q", got)
				}
				assertScopedBaselineMatchesFile(t, dir, "packet/10-recon.md")
			}
			if err := os.WriteFile(blocked, []byte(oldBody+"local draft\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			err = workingSetPush(env, false)
			if err == nil || !strings.Contains(err.Error(), "conflict") {
				t.Errorf("push after failed refresh must conflict with newer server content: %v", err)
			}
			wantPosts := 1
			if failedKey == "reconnaissance" {
				wantPosts = 2 // neither section was refreshed
			}
			if len(fx.authorPosts) != wantPosts {
				t.Fatalf("expected %d attempted packet updates, got %v", wantPosts, fx.authorPosts)
			}
			for _, post := range fx.authorPosts {
				record := post["record"].(map[string]any)
				key := str(record, "section_key")
				if got := str(record, "expected_fingerprint"); got != oldPins[key] {
					t.Errorf("push adopted the unread server version for %s: %q", key, got)
				}
			}
			if err := os.WriteFile(blocked, []byte(oldBody), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := workingSetPullScope(env, false, wsNow); err != nil {
				t.Fatalf("retry must complete the packet refresh: %v", err)
			}
			for _, key := range []string{"reconnaissance", "decisions"} {
				if got := readPacketFingerprints(dir)[key]; got != "new-"+key+"-fp" {
					t.Errorf("retry left stale CAS for %s: %q", key, got)
				}
				assertScopedBaselineMatchesFile(t, dir, filepath.Join("packet", packetFileName(key)))
			}
		})
	}
}

func TestScopePullPacketMetadataFailureKeepsConservativeCAS(t *testing.T) {
	for _, failedMetadata := range []string{scopedDraftBaselineFile, ".served-fingerprints.json"} {
		t.Run(failedMetadata, func(t *testing.T) {
			fx := scopeFixture()
			env, dir := pulledScope(t, fx)
			oldPin := readPacketFingerprints(dir)["reconnaissance"]
			fx.packetSections[0].(map[string]any)["content"] = "new server body\n"
			fx.packetSections[0].(map[string]any)["content_fingerprint"] = "new-server-fp"
			plan := stagedPacketPullForTest(t, env, dir)
			bodyWritten := false
			err := applyScopedPullPlanWithWriter(dir, plan, func(path string, content []byte) error {
				if filepath.Base(path) == failedMetadata && (bodyWritten || failedMetadata == ".served-fingerprints.json") {
					return &os.PathError{Op: "rename", Path: path, Err: os.ErrPermission}
				}
				if err := atomicWrite(path, content); err != nil {
					return err
				}
				if filepath.Base(path) == "10-recon.md" {
					bodyWritten = true
				}
				return nil
			})
			if !errors.Is(err, os.ErrPermission) {
				t.Fatalf("expected metadata failure, got %v", err)
			}
			if got := readPacketFingerprints(dir)["reconnaissance"]; got != oldPin {
				t.Errorf("failed metadata checkpoint advanced CAS to %q", got)
			}
			if failedMetadata == ".served-fingerprints.json" {
				if !bodyWritten {
					t.Fatal("CAS was written before its matching body")
				}
				assertScopedBaselineMatchesFile(t, dir, "packet/10-recon.md")
				if err := workingSetPullScope(env, false, wsNow); err != nil {
					t.Fatalf("retry after CAS-save failure: %v", err)
				}
				if got := readPacketFingerprints(dir)["reconnaissance"]; got != "new-server-fp" {
					t.Fatalf("retry did not save matching CAS: %q", got)
				}
			}
		})
	}
}

// REQ-CROSS-332 AC7/AC10 and UR-CLI-DRAFT-PROTECTION-001 AC2:
// unfinished cleanup must retain CAS for every packet file still on disk.
func TestScopePullCleanupFailureRetainsCASAndConflictsOnPush(t *testing.T) {
	fx := scopeFixture()
	fx.enforcePacketCAS = true
	fx.packetSections = append(fx.packetSections, map[string]any{
		"section_key": "decisions", "content": "old decisions\n", "content_fingerprint": "old-decisions-fp",
	})
	env, dir := pulledScope(t, fx)
	oldPins := readPacketFingerprints(dir)
	fx.packetSectionsStatus = 404
	plan := stagedPacketPullForTest(t, env, dir)
	err := applyScopedPullPlanWithWriter(dir, plan, func(path string, content []byte) error {
		if filepath.Base(path) == scopedDraftBaselineFile {
			return &os.PathError{Op: "rename", Path: path, Err: os.ErrPermission}
		}
		return atomicWrite(path, content)
	})
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("expected baseline failure during cleanup, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "packet", "10-recon.md")); !os.IsNotExist(err) {
		t.Fatalf("cleanup must have removed the first packet file: %v", err)
	}
	if got := readScopeFile(t, filepath.Join(dir, "packet", "40-decisions.md")); got != "old decisions\n" {
		t.Fatalf("unfinished cleanup changed the remaining packet: %q", got)
	}
	if got := readPacketFingerprints(dir)["decisions"]; got != oldPins["decisions"] {
		t.Errorf("unfinished cleanup lost CAS: got %q want %q", got, oldPins["decisions"])
	}
	fx.packetSectionsStatus = 0
	fx.packetSections[1].(map[string]any)["content"] = "new server decisions\n"
	fx.packetSections[1].(map[string]any)["content_fingerprint"] = "new-decisions-fp"
	writePacketFile(t, dir, "40-decisions.md", "local decisions draft\n")
	err = workingSetPush(env, false)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("push after failed cleanup must conflict: err=%v posts=%v", err, fx.authorPosts)
	}
	if len(fx.authorPosts) != 1 {
		t.Fatalf("expected one conflicted packet update, got %v", fx.authorPosts)
	}
	record := fx.authorPosts[0]["record"].(map[string]any)
	if got := str(record, "expected_fingerprint"); got != oldPins["decisions"] {
		t.Fatalf("push adopted newer server CAS: %q", got)
	}
}
