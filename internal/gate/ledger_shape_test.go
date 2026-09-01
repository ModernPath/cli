package gate

import (
	"os"
	"path/filepath"
	"testing"
)

// REQ-CROSS-147: two ledger shapes that fail silently, both found the hard way.
//
// A criteria label the parser cannot read cost 139 criteria across 55 rows,
// undetected for months (REQ-CROSS-137). A second detail block for a row that
// already has one leaves the requirement with an empty description, and three
// were written in one session by someone who had documented the hazard.
// Neither produces an error anywhere; the ledger simply syncs with less in it.
func TestCheckLedgerShape(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, root, "tasks/CTX-REQUIREMENTS.md", `# Dashboard — CTX
Totals: 2 DONE

| ID | Title | Stage | Status |
|----|-------|-------|--------|
| REQ-CTX-001 | fine | MVP | DONE |
| REQ-CTX-002 | also fine | MVP | DONE |

### REQ-CTX-001 — fine
- **Status:** DONE
- **Acceptance criteria:** (from x_test.go)
  - GIVEN a thing THEN it holds.

### REQ-CTX-002 — also fine
- **Status:** DONE
- **Acceptance criteria (as-built):**
  - GIVEN a thing THEN it holds.
`)
	if v, err := CheckLedgerShape(root); err != nil || len(v) != 0 {
		t.Fatalf("clean ledger: got %d violations %v, err %v", len(v), v, err)
	}

	write(t, root, "tasks/BAD-REQUIREMENTS.md", `# Dashboard — BAD
Totals: 2 DONE

| REQ-BAD-001 | broken label | MVP | DONE |
| REQ-BAD-002 | duplicated | MVP | DONE |

### REQ-BAD-001 — broken label
- **Acceptance criteria** (each maps to a test):
  - GIVEN a thing THEN it holds.

### REQ-BAD-002 — duplicated
- **Acceptance criteria:**
  - GIVEN a thing THEN it holds.

### REQ-BAD-002 — duplicated again
- **Acceptance criteria:**
  - GIVEN another thing THEN it holds.
`)
	v, err := CheckLedgerShape(root)
	if err != nil {
		t.Fatal(err)
	}
	rules := map[string]int{}
	for _, x := range v {
		rules[x.Rule]++
	}
	if rules["criteria-label-unreadable"] != 1 {
		t.Errorf("criteria-label-unreadable: got %d, want 1 (%v)", rules["criteria-label-unreadable"], v)
	}
	if rules["duplicate-detail-block"] != 1 {
		t.Errorf("duplicate-detail-block: got %d, want 1 (%v)", rules["duplicate-detail-block"], v)
	}
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
