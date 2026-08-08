package rdd

// EPIC-SYNC-009 (REQ-CROSS-027, D-AS-2): the quiescence coherence gate. The
// workspace discipline updates a requirement's status in THREE places
// atomically (CLAUDE.md §5 #8) — so a half-done edit disagrees with its own
// Totals line by construction. Totals⇄rows disagreement = mid-edit = do not
// sync. Ledgers without a Totals line (legacy) are tolerated.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var totalsLineRe = regexp.MustCompile(`(?m)^Totals?:\s*(.+)$`)
var totalsPartRe = regexp.MustCompile(`(\d+)\s+([A-Z_]+)`)
var coherenceRowRe = regexp.MustCompile(`(?m)^\|\s*(REQ-[A-Z]+-\d+)\s*\|`)

// CheckLedgerCoherence returns one human-readable problem per incoherent
// ledger under tasks/ (empty = quiescent from the ledger perspective).
func CheckLedgerCoherence(root string) []string {
	files, _ := filepath.Glob(filepath.Join(root, "tasks", "*-REQUIREMENTS.md"))
	var problems []string

	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: unreadable (%v)", filepath.Base(file), err))
			continue
		}
		text := string(raw)

		totals := totalsLineRe.FindStringSubmatch(text)
		if totals == nil {
			continue // legacy ledger without a Totals line — nothing to cross-check
		}

		claimed := map[string]int{}
		for _, part := range totalsPartRe.FindAllStringSubmatch(totals[1], -1) {
			n, _ := strconv.Atoi(part[1])
			claimed[part[2]] = n
		}

		counted := map[string]int{}
		for _, line := range strings.Split(text, "\n") {
			if coherenceRowRe.MatchString(line) {
				cells := strings.Split(line, "|")
				if len(cells) > 4 {
					status := strings.TrimSpace(cells[4])
					if status != "" {
						counted[status]++
					}
				}
			}
		}

		for status, want := range claimed {
			if counted[status] != want {
				problems = append(problems,
					fmt.Sprintf("%s: Totals says %d %s, rows carry %d — mid-edit, not syncing",
						filepath.Base(file), want, status, counted[status]))
			}
		}
		for status, have := range counted {
			if _, tracked := claimed[status]; !tracked && have > 0 {
				problems = append(problems,
					fmt.Sprintf("%s: %d %s rows but the status is missing from Totals — mid-edit, not syncing",
						filepath.Base(file), have, status))
			}
		}
	}

	return problems
}
