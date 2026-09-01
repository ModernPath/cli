package rdd

// SR-SY-1401 (EPIC-SYNC-014) — citation refs are whole, repo-relative paths.
// The eval (`RUN:2026-08-16`): REQ-DEAL-028's payload arrived as
// [{"kind":"code","ref":"/deals/page.tsx"},…] — the Next.js route-group path
// `…/[locale]/(app)/deals/page.tsx` split at its parens into garbage the server
// can never match against a FileAnalysis relative_path. The reference semantics
// are ported from cmd/coverage.go (REQ-CROSS-179, PR #65): backtick-exact
// content minus CODE:/TEST: prefix and :line/:range suffix (comma lists
// included); bare tokens paren-aware with unbalanced-trailing-paren trimming.

import (
	"encoding/json"
	"strings"
	"testing"
)

func citationsOf(t *testing.T, detail string) string {
	t.Helper()
	op := BuildRequirementOp(Req{
		ID: "REQ-DEAL-028", Title: "t", Ctx: "DEAL",
		Status: "PENDING_VERIFICATION", Detail: detail,
	})
	blob, err := json.Marshal(op.Payload["source_citations"])
	if err != nil {
		t.Fatal(err)
	}
	return string(blob)
}

// The field shape: a backticked route-group path, and CODE:-tagged backticked
// paths with a trailing prose parenthetical on the line.
func TestRouteGroupCitationSurvivesWhole(t *testing.T) {
	detail := strings.Join([]string{
		"- **Status:** PENDING_VERIFICATION",
		"- **Statement:** The deals page lists the user's deals.",
		"- **Tests:** —",
		"- **Code:** `app-web/src/app/[locale]/(app)/deals/page.tsx`, `CODE:app-web/src/components/deals/my-deals-page.tsx`, `CODE:app-web/src/lib/deal-constants.ts` (STATE_BADGE_CLASSES, ACTIVE_DEAL_STATES)",
	}, "\n")
	body := citationsOf(t, detail)

	for _, want := range []string{
		`"app-web/src/app/[locale]/(app)/deals/page.tsx"`,
		`"app-web/src/components/deals/my-deals-page.tsx"`,
		`"app-web/src/lib/deal-constants.ts"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("whole path %s missing from citations: %s", want, body)
		}
	}
	for _, never := range []string{`"/deals/page.tsx"`, `"ref":"CODE"`, `"app)"`} {
		if strings.Contains(body, never) {
			t.Errorf("mangled fragment %s still emitted: %s", never, body)
		}
	}
}

// A backticked comma-range list is a suffix, not part of the path identity —
// the server matches FileAnalysis rows by exact relative_path.
func TestCommaRangeSuffixFoldsOffTheRef(t *testing.T) {
	detail := "- **Code:** `CODE:x/schema.prisma:16-35,382-387`\n"
	body := citationsOf(t, detail)
	if !strings.Contains(body, `"x/schema.prisma"`) {
		t.Fatalf("suffixed citation lost its path: %s", body)
	}
	if strings.Contains(body, "16-35") {
		t.Fatalf("the range suffix rode into the ref: %s", body)
	}
}

// A bare (unbackticked) CODE: token inside a prose paren cites the path, not
// the closing paren; a route-group path's balanced parens stay untouched.
func TestBareTokenIsParenAware(t *testing.T) {
	detail := "- **Code:** guarded before creation (see CODE:a/b.ts) in the wizard\n" +
		"- **Tests:** covered by TEST:src/app/(app)/x.test.ts:12 today\n"
	body := citationsOf(t, detail)
	if !strings.Contains(body, `{"kind":"code","ref":"a/b.ts"}`) {
		t.Fatalf("prose-paren citation wrong: %s", body)
	}
	if !strings.Contains(body, `{"kind":"test","ref":"src/app/(app)/x.test.ts"}`) {
		t.Fatalf("balanced-paren test citation wrong: %s", body)
	}
	if strings.Contains(body, `a/b.ts)`) {
		t.Fatalf("unbalanced trailing paren kept: %s", body)
	}
}

// An untagged backticked path keeps exact-content semantics: suffix folded,
// content otherwise verbatim.
func TestBacktickedBarePathDropsLineSuffix(t *testing.T) {
	detail := "- **Tests:** `tests/test_admin.py:470-475` (opened and run)\n"
	body := citationsOf(t, detail)
	if !strings.Contains(body, `{"kind":"test","ref":"tests/test_admin.py"}`) {
		t.Fatalf("backticked test path wrong: %s", body)
	}
	if strings.Contains(body, "470-475\"") {
		t.Fatalf("line suffix survived in the ref: %s", body)
	}
}

// The explicit tag names the axis: a TEST:-tagged file is a test citation even
// on a Code line, and vice versa.
func TestExplicitTagWinsOverTheLabel(t *testing.T) {
	detail := "- **Code:** `TEST:tests/deal.test.ts` proves it; `CODE:src/lib/deal.ts`\n"
	body := citationsOf(t, detail)
	if !strings.Contains(body, `{"kind":"test","ref":"tests/deal.test.ts"}`) {
		t.Fatalf("TEST: tag ignored: %s", body)
	}
	if !strings.Contains(body, `{"kind":"code","ref":"src/lib/deal.ts"}`) {
		t.Fatalf("CODE: tag ignored: %s", body)
	}
}

// A backticked command is prose, not a citation — but a path inside it is
// still mined, exactly as before the port.
func TestCommandSpansStillYieldTheirPaths(t *testing.T) {
	detail := "- **Tests:** `mix test apps/core/test/foo_test.exs` green on retry\n"
	body := citationsOf(t, detail)
	if !strings.Contains(body, `"apps/core/test/foo_test.exs"`) {
		t.Fatalf("path inside a command span lost: %s", body)
	}
	if strings.Contains(body, `"mix test`) {
		t.Fatalf("the command itself became a citation: %s", body)
	}
}
