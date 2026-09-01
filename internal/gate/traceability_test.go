package gate

import (
	"os"
	"path/filepath"
	"testing"
)

// REQ-CROSS-164: a test cited as covering evidence names a requirement.
//
// PROCESS.md §6 — "every test names its requirement id". Driven to 0 of 166 by
// hand on RUN:2026-08-14 (REQ-CROSS-149) and enforced by nothing, so it
// regresses the first time anyone adds a test and cites it.
//
// The population is deliberate and matches the one REQ-CROSS-149 settled on: a
// **test file** cited in a `- **Tests:**` field. That field is the ledger's own
// declaration that a file is covering evidence. Files cited elsewhere (Code:,
// prose) are out of scope — widening it was measured and gives a different,
// larger number answering a question nobody asked.
func TestCitedTestsNameARequirement(t *testing.T) {
	setup := func(t *testing.T, ledger string, files map[string]string) string {
		t.Helper()
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "tasks"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "tasks", "ABC-REQUIREMENTS.md"), []byte(ledger), 0o644); err != nil {
			t.Fatal(err)
		}
		for p, body := range files {
			full := filepath.Join(root, p)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return root
	}

	t.Run("a cited test naming no requirement is a violation", func(t *testing.T) {
		root := setup(t, "- **Tests:** `foo_test.exs`\n",
			map[string]string{"apps/core/test/foo_test.exs": "defmodule FooTest do\nend\n"})
		v, err := CheckTestTraceability(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(v) != 1 || v[0].Rule != "untraced-test" {
			t.Fatalf("got %v, want one untraced-test violation", v)
		}
	})

	t.Run("naming any requirement satisfies it", func(t *testing.T) {
		root := setup(t, "- **Tests:** `foo_test.exs`\n",
			map[string]string{"apps/core/test/foo_test.exs": "# REQ-ABC-009: the thing\n"})
		if v, _ := CheckTestTraceability(root); len(v) != 0 {
			t.Fatalf("got %v, want none — traceability, not bookkeeping", v)
		}
	})

	t.Run("a non-test file in a Tests: field is out of scope", func(t *testing.T) {
		// vitest.setup.ts and ops-dump.js are cited in real Tests: fields and are
		// not tests; flagging them would be noise the reader learns to ignore.
		root := setup(t, "- **Tests:** `vitest.setup.ts`\n",
			map[string]string{"frontend/vitest.setup.ts": "export {};\n"})
		if v, _ := CheckTestTraceability(root); len(v) != 0 {
			t.Fatalf("got %v, want none — not a test file", v)
		}
	})

	t.Run("a citation that resolves to nothing is not reported here", func(t *testing.T) {
		// Unresolvable paths are a citation defect, not a traceability one, and
		// reporting them twice under two rule names trains the reader to skim.
		root := setup(t, "- **Tests:** `gone_test.exs`\n", nil)
		if v, _ := CheckTestTraceability(root); len(v) != 0 {
			t.Fatalf("got %v, want none — absent files are a different rule", v)
		}
	})

	t.Run("a test cited outside a Tests: field is out of scope", func(t *testing.T) {
		root := setup(t, "- **Code:** `foo_test.exs`\n",
			map[string]string{"apps/core/test/foo_test.exs": "defmodule FooTest do\nend\n"})
		if v, _ := CheckTestTraceability(root); len(v) != 0 {
			t.Fatalf("got %v, want none — the Tests: field defines the population", v)
		}
	})
}
