package kit

import "testing"

// REQ-CROSS-144: the citation audit ships with the skill that prescribes it.
// C8 was re-derived by hand five times in one session and was wrong three of
// those (a regex matching .ex inside .exs, a truncated display, and a sweep that
// covered the derived subset rather than docs/**). A procedure that specific
// belongs in a file, not in a paragraph the next pass reimplements.
//
// The skill that prescribes it is now the package's rdd-audit (promoted from
// the workspace set), so the script rides the recursive assets/rdd mapping to
// the canonical .modernpath/rdd path, and the legacy .claude location is
// retired rather than left to go stale beside it.
func TestCitationAuditIsInstalled(t *testing.T) {
	const asset = "assets/rdd/skills/rdd-audit/audit-citations.mjs"
	target, ok := TargetForAsset(asset)
	if !ok {
		t.Fatalf("%s has no install target", asset)
	}
	if want := ".modernpath/rdd/skills/rdd-audit/audit-citations.mjs"; target != want {
		t.Errorf("target = %q, want %q", target, want)
	}
	if _, err := assets.ReadFile(asset); err != nil {
		t.Errorf("asset not embedded: %v", err)
	}

	retired, err := RetiredTargets()
	if err != nil {
		t.Fatal(err)
	}
	legacyRetired := false
	for _, target := range retired {
		if target == ".claude/skills/rdd-reverse-engineer/audit-citations.mjs" {
			legacyRetired = true
		}
	}
	if !legacyRetired {
		t.Error("legacy .claude/skills/rdd-reverse-engineer/audit-citations.mjs is not retired — upgrades leave a stale copy of the auditor installed")
	}
}
