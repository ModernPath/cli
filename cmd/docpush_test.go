package cmd

import "testing"

func TestInferTierAngleFromExportPath(t *testing.T) {
	cases := []struct {
		rel       string
		wantTier  string
		wantAngle string
	}{
		{"my-app/README.md", "architecture", "overview"},
		{"my-app/design-system.md", "architecture", "design_system"},
		{"my-app/tech-stack.md", "architecture", "tech_stack"},
		{"my-app/instructions/coding/standards.md", "instructions", "coding"},
		{"my-app/docs/architecture/overview/context.md", "architecture", "overview"},
		{"my-app/docs/code/analysis/deep-dive.md", "code", "analysis"},
		{"my-app/docs/subsystem/overview/legacy.md", "subsystem", "overview"},
		{"my-app/docs/_legacy/weird/angle/doc.md", "weird", "angle"},
		{"my-app/docs/_unscoped/subsystem/api/notes.md", "subsystem", "api"},
		{"my-app/docs/_unscoped/module/api/notes.md", "module", "api"},
		{"my-app/subsystems/INDEX.md", "architecture", "navigation"},
		{"my-app/subsystems/auth/overview.md", "subsystem", "overview"},
		{"my-app/subsystems/auth/docs/security/hardening.md", "subsystem", "security"},
		{"my-app/subsystems/auth/docs/code/static-analysis/results.md", "code", "static-analysis"},
		{"my-app/subsystems/core/modules/authn/docs/api/contracts.md", "module", "api"},
		{"my-app/subsystems/core/modules/authn/overview.md", "module", "overview"},
		{"architectures/legacy-slug/docs/architecture/overview/x.md", "architecture", "overview"},
		{"my-app/architecture/overview/context.md", "architecture", "overview"},
		{"my-app/architecture/system-context.md", "architecture", ""},
		{"my-app/architecture/auth/overview.md", "subsystem", "overview"},
		{"my-app/architecture/auth/auth-flow.md", "subsystem", ""},
		{"my-app/architecture/auth/api/flow.md", "subsystem", "api"},
		{"my-app/architecture/core/modules/authn/overview.md", "module", "overview"},
		{"my-app/architecture/core/modules/authn/api/contracts.md", "module", "api"},
		{"my-app/architecture/auth/code/static-analysis/results.md", "code", "static-analysis"},
		{"my-app/architecture/code/analysis/deep-dive.md", "code", "analysis"},
	}

	for _, tc := range cases {
		t.Run(tc.rel, func(t *testing.T) {
			gotTier, gotAngle := inferTierAngleFromExportPath(tc.rel)
			if gotTier != tc.wantTier || gotAngle != tc.wantAngle {
				t.Fatalf("inferTierAngleFromExportPath(%q) = (%q,%q), want (%q,%q)",
					tc.rel, gotTier, gotAngle, tc.wantTier, tc.wantAngle)
			}
		})
	}
}

func TestExportPathForDocPush(t *testing.T) {
	if got := exportPathForDocPush("my-app/architecture/a.md", "my-app"); got != "architecture/a.md" {
		t.Fatalf("got %q", got)
	}
	if exportPathForDocPush("other/architecture/a.md", "my-app") != "" {
		t.Fatal("expected empty when slug prefix mismatch")
	}
	if exportPathForDocPush("my-app/x.md", "") != "" {
		t.Fatal("expected empty slug")
	}
}

func TestShouldSkipDocPush(t *testing.T) {
	if !shouldSkipDocPush("x/BLUEPRINT.md") {
		t.Fatal("expected BLUEPRINT skipped")
	}
	if !shouldSkipDocPush("x/subsystems/INDEX.md") {
		t.Fatal("expected subsystems index skipped")
	}
	if !shouldSkipDocPush("x/architecture/INDEX.md") {
		t.Fatal("expected architecture index skipped")
	}
	if !shouldSkipDocPush("x/capabilities/foo.md") {
		t.Fatal("expected capabilities skipped")
	}
	if !shouldSkipDocPush("x/patterns/bar.md") {
		t.Fatal("expected patterns skipped")
	}
	if shouldSkipDocPush("x/architecture/auth/z.md") {
		t.Fatal("expected doc not skipped")
	}
}
