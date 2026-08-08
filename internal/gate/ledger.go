// Package gate turns the process rules that must hold into checks that can
// fail (REQ-CROSS-030).
//
// The rules here were written down long before they were enforced, and written
// rules are context rather than configuration: an agent reads them, usually
// follows them, and occasionally does not. Every silent failure found in this
// workspace during the first week of August 2026 had the same shape — a step
// that ran, exited zero, and had no effect. A gate that blocks is the only
// mechanism that changes that.
package gate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Violation is one broken rule, named precisely enough to fix without hunting.
type Violation struct {
	Rule    string // short id, e.g. "status-hygiene"
	File    string // repository-relative path
	Subject string // the specific thing at fault (an epic id, a context) — see Key
	Detail  string // what disagrees with what
}

func (v Violation) String() string { return fmt.Sprintf("%s: %s — %s", v.Rule, v.File, v.Detail) }

// Key identifies a violation for baselining. It includes the Subject because
// six epics can break one rule in one file: keying on rule+file alone would
// accept the seventh silently, which is how a suppression list stops being a
// record of known debt and becomes a hole. It excludes Detail so that improving
// a message does not un-baseline anything.
func (v Violation) Key() string { return v.Rule + "\t" + v.File + "\t" + v.Subject }

var (
	// A dashboard row: | REQ-CTX-NNN | title | stage | STATUS | …
	ledgerRowRe = regexp.MustCompile(`^\|\s*REQ-[A-Z]+-\d+\s*\|`)
	totalsRe    = regexp.MustCompile(`(?m)^Totals:\s*(.+)$`)
	// "18 DONE" / "0 IN_PROGRESS" — the Totals line's own vocabulary.
	totalPairRe = regexp.MustCompile(`(\d+)\s+([A-Z_]+)`)
)

// CheckLedgers verifies that each ledger's dashboard rows agree with its
// Totals line — two of the three places a status lives. Drift between them is
// how a ledger starts lying: the rows are edited, the summary is not, and the
// summary is what people read.
//
// Returns nothing for a repository that has no tasks/ directory, so the check
// is safe to run from a hook in any repository.
func CheckLedgers(root string) ([]Violation, error) {
	dir := filepath.Join(root, "tasks")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read tasks/: %w", err)
	}

	var found []Violation
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		rel := filepath.Join("tasks", e.Name())
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", rel, err)
		}
		if v, ok := checkOneLedger(rel, string(body)); ok {
			found = append(found, v)
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].File < found[j].File })
	return found, nil
}

func checkOneLedger(rel, body string) (Violation, bool) {
	totalsMatch := totalsRe.FindStringSubmatch(body)
	if totalsMatch == nil {
		// No dashboard yet — a freshly seeded context. Reporting it would train
		// people to ignore the gate, which costs more than the missing check.
		return Violation{}, false
	}

	claimed := map[string]int{}
	for _, m := range totalPairRe.FindAllStringSubmatch(totalsMatch[1], -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		claimed[m[2]] = n
	}

	actual := map[string]int{}
	for _, line := range strings.Split(body, "\n") {
		if !ledgerRowRe.MatchString(line) {
			continue
		}
		cells := strings.Split(line, "|")
		// | ID | Title | Stage | Status | …  →  status is the 4th cell after the
		// leading empty one produced by the leading pipe.
		if len(cells) < 5 {
			continue
		}
		status := strings.TrimSpace(cells[4])
		if status == "" {
			continue
		}
		actual[status]++
	}
	if len(actual) == 0 {
		return Violation{}, false
	}

	var diffs []string
	for _, status := range union(claimed, actual) {
		if claimed[status] != actual[status] {
			diffs = append(diffs, fmt.Sprintf("%s: rows say %d, Totals says %d", status, actual[status], claimed[status]))
		}
	}
	if len(diffs) == 0 {
		return Violation{}, false
	}
	return Violation{
		Rule:    "status-hygiene",
		File:    rel,
		Subject: rel,
		Detail:  strings.Join(diffs, "; "),
	}, true
}

func union(a, b map[string]int) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range []map[string]int{a, b} {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}
