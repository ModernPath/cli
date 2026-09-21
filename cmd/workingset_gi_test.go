package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// External PR #299 review (#15): after a smaller reselection, a stale member file
// for a REAL member (still in the store) must not be pushed — it is outside the
// current selection.
func TestPushSkipsAStaleMemberDroppedFromTheSelection(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx) // pulls 310.md and 311.md
	fx.workSelection["current"].(map[string]any)["members"] = []any{"REQ-CROSS-310"}

	edit(t, memberPath(dir, "REQ-CROSS-311"), func(s string) string {
		return strings.Replace(s, "\npatch\n", "\nchanged\n", 1)
	})

	fx.authorPosts = nil
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("push: %v", err)
	}
	if patchPostFor(fx, "REQ-CROSS-311") != nil {
		t.Fatal("push patched a stale member file outside the current selection (#15)")
	}
}

// External PR #299 review (#21): a successful push re-renders the item file from
// the fresh store state, so an operation marker (a withdraw) does not linger in
// the body and re-emit on the next push.
func TestPushRerendersTheFileClearingOperationMarkers(t *testing.T) {
	fx := scopeFixture()
	fx.requirements = []any{reqWith("REQ-CROSS-310", "the reads", []any{rel("declares", "UR-CLI-008", "confirmed")})}
	fx.workSelection["current"].(map[string]any)["members"] = []any{"REQ-CROSS-310"}
	env, dir := pulledScope(t, fx)

	edit(t, memberPath(dir, "REQ-CROSS-310"), func(s string) string {
		return strings.Replace(s, "- declares UR-CLI-008 [confirmed]", "- withdraw UR-CLI-008", 1)
	})

	fx.authorPosts = nil
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("push: %v", err)
	}
	if strings.Contains(readScopeFile(t, memberPath(dir, "REQ-CROSS-310")), "withdraw UR-CLI-008") {
		t.Fatal("the withdraw marker lingered in the file after push (#21)")
	}
}

// External PR #299 review (#22): a successful packet-section put refreshes the
// pull-time CAS sidecar with the server-returned fingerprint.
func TestPushRefreshesThePacketFingerprintSidecar(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)

	edit(t, filepath.Join(dir, "packet", "10-recon.md"), func(s string) string { return s + "\nmore" })

	fx.authorPosts = nil
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("push: %v", err)
	}
	fps := readPacketFingerprints(dir)
	if fps["reconnaissance"] == "ps-fp-1" || fps["reconnaissance"] == "" {
		t.Fatalf("sidecar not refreshed after the put (#22), got %q", fps["reconnaissance"])
	}
}

// External PR #299 review (#25): an extra section key whose file name collides
// with a canonical section's file is refused on pull.
func TestPullRefusesACollidingExtraSectionKey(t *testing.T) {
	fx := scopeFixture()
	fx.packetSections = append(fx.packetSections, map[string]any{
		"scope_kind": "epic", "scope_external_id": "EPIC-CLI-008",
		"section_key": "10-recon", "content": "collision", "content_fingerprint": "x", "authoring_context_id": "c",
	})
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err == nil {
		t.Fatal("pull accepted an extra section key colliding with a canonical file (#25)")
	}
}
