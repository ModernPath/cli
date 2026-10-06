package cmd

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// REQ-CROSS-332: an absent withdrawal target is not evidence of an accepted
// write when the record's fingerprint is unchanged since pull.
func TestPushAbsentWithdrawalDoesNotBlockOtherChanges(t *testing.T) {
	for _, kind := range []string{"relation", "member"} {
		t.Run(kind, func(t *testing.T) {
			fx := scopeFixture()
			env, dir := pulledScope(t, fx)
			name, target := "members/REQ-CROSS-311.md", "UR-MISSING"
			if kind == "member" {
				name, target = "EPIC-CLI-008.md", "REQ-MISSING"
			}
			path := filepath.Join(dir, name)
			baseline := scopedBaselineEntryForTest(t, dir, name)
			edit(t, path, func(s string) string {
				if kind == "relation" {
					return s + "\n## relations\n```authoring:relations\n- withdraw " + target + "\n```\n"
				}
				return strings.Replace(s, "```authoring:members\n", "```authoring:members\n- withdraw "+target+"\n", 1)
			})
			draft := readScopeFile(t, path)
			if !strings.Contains(draft, "withdraw "+target) {
				t.Fatal("fixture did not add the withdrawal marker")
			}
			edit(t, memberPath(dir, "REQ-CROSS-310"), func(s string) string {
				return strings.Replace(s, "the reads", "the reads and writes", 1)
			})
			canonical := canonicalScopeRequirement(fx, "the reads and writes", "accepted-item-fp")
			fx.authorSyncItems = map[string]any{"REQ-CROSS-310": map[string]any{
				"kind": "system", "item": canonical, "gates": []any{},
			}}
			before := scopeTreeBytes(t, dir)
			for _, dryRun := range []bool{true, false} {
				var err error
				out := captureWarnings(t, func() { err = workingSetPush(env, dryRun) })
				if err != nil {
					t.Fatalf("absent %s blocks push (dryRun=%v): %v", kind, dryRun, err)
				}
				for _, want := range []string{name, target, "no-op", "remove the withdrawal marker"} {
					if !strings.Contains(out, want) {
						t.Errorf("missing-target notice must name %q: %s", want, out)
					}
				}
				if dryRun && (len(fx.authorPosts) != 0 || !reflect.DeepEqual(before, scopeTreeBytes(t, dir))) {
					t.Fatal("dry run wrote store records or local files")
				}
			}
			if len(fx.authorPosts) != 1 || patchPostFor(fx, "REQ-CROSS-310") == nil {
				t.Fatalf("want only the independent item update, got %v", fx.authorPosts)
			}
			if readScopeFile(t, path) != draft || scopedBaselineEntryForTest(t, dir, name) != baseline {
				t.Fatal("no-op withdrawal changed the draft or baseline")
			}
		})
	}
}
