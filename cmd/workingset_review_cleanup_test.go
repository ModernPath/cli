package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func narrowReviewScope(t *testing.T, existingAuthoring bool) (*wsFixture, *factoryEnv, string) {
	t.Helper()
	fx := scopeFixture()
	current := fx.workSelection["current"].(map[string]any)
	current["scope_kind"] = "single_sr"
	current["scope_external_id"] = "REQ-CROSS-310"
	current["members"] = []any{}
	fx.packetSections = nil
	fx.requirements[0].(map[string]any)["lane_class"] = "wording"
	env := wsEnv(t, wsServe(t, fx))
	if existingAuthoring {
		if err := workingSetPullScope(env, false, wsNow); err != nil {
			t.Fatal(err)
		}
	}
	if err := workingSetPullForReview(env, []string{"REQ-CROSS-310"}, wsNow); err != nil {
		t.Fatalf("narrow review pull: %v", err)
	}
	dir := filepath.Join(env.Root, workingSetDir, "REQ-CROSS-310")
	if mode, _ := readContextStamp(dir); mode != "review" {
		t.Fatalf("narrow pull must enter review mode, got %q", mode)
	}
	if !strings.Contains(readScopeFile(t, filepath.Join(dir, reviewBundleFile)), "small-change lane, narrow review") {
		t.Fatal("fixture must contain the actual narrow review bundle")
	}
	return fx, env, dir
}

func TestAuthoringPullRemovesNarrowReviewBundle(t *testing.T) {
	for _, existingAuthoring := range []bool{false, true} {
		name := "first_authoring_pull"
		if existingAuthoring {
			name = "refresh"
		}
		t.Run(name, func(t *testing.T) {
			fx, env, dir := narrowReviewScope(t, existingAuthoring)
			fx.requirements[0].(map[string]any)["title"] = "updated after the review"
			if err := workingSetPullScope(env, false, wsNow.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(dir, reviewBundleFile)); !os.IsNotExist(err) {
				t.Fatalf("successful authoring pull retained a stale review bundle: %v", err)
			}
			if mode, _ := readContextStamp(dir); mode != "authoring" {
				t.Fatalf("successful pull must enter authoring mode, got %q", mode)
			}
			if !strings.Contains(readScopeFile(t, filepath.Join(dir, "REQ-CROSS-310.md")), "updated after the review") {
				t.Fatal("authoring pull did not write current requirement content")
			}
		})
	}
}

func TestAuthoringPullPreservesNarrowReviewBundleOnFailure(t *testing.T) {
	for _, failure := range []string{"required_read", "local_edit", "file_write"} {
		t.Run(failure, func(t *testing.T) {
			fx, env, dir := narrowReviewScope(t, true)
			bundlePath := filepath.Join(dir, reviewBundleFile)
			bundle := readScopeFile(t, bundlePath)
			switch failure {
			case "required_read":
				fx.packetSectionsStatus = 500
			case "local_edit":
				edit(t, filepath.Join(dir, "REQ-CROSS-310.md"), func(s string) string {
					return s + "\nunsaved author draft\n"
				})
			case "file_write":
				path := filepath.Join(dir, "findings", "COLD-REVIEW.md")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			before := scopeTreeBytes(t, dir)
			if err := workingSetPullScope(env, false, wsNow.Add(time.Minute)); err == nil {
				t.Fatalf("expected authoring pull failure for %s", failure)
			}
			if got := readScopeFile(t, bundlePath); got != bundle {
				t.Fatal("failed authoring pull changed the previous review bundle")
			}
			if failure != "file_write" && !reflect.DeepEqual(before, scopeTreeBytes(t, dir)) {
				t.Fatal("preflight refusal changed the scope tree")
			}
		})
	}
}
