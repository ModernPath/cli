package gate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/modernpath/cli/internal/rdd"
)

var (
	worklistEpicRowRe = regexp.MustCompile(`^\|\s*(EPIC-[A-Z]+-[\d.]+)[^|]*\|`)
	// Either shape is in use: a markdown link, or a plain backticked path.
	epicRecordLinkRe = regexp.MustCompile("\\((epics/[^)]+\\.md)\\)|`(epics/[^`]+\\.md)`")
	doneCellRe       = regexp.MustCompile(`(?i)\bDONE\b`)
	// No USER:-tag pattern lives here any more: matching one was the restatement
	// that let the work-list cell drift from the rule the record is held to.
)

// CheckApprovals verifies that every epic the work-list calls DONE has a
// recorded human approval carrying a USER: source.
//
// This exists because the claim and the evidence were never compared.
// EPIC-MC-001 sat at "IN_REVIEW — awaiting APPROVE-EPIC-MC-001" for a day while
// no gate op was ever emitted for it: there was nothing to approve, and no
// surface said so. The check is the comparison nobody was making.
//
// "Approved" means whatever the sync extractor means by it — the judgment is
// delegated to rdd.ApprovalLineOf rather than reimplemented, because two
// answers to one rule inevitably drift apart.
func CheckApprovals(root string) ([]Violation, error) {
	body, err := os.ReadFile(filepath.Join(root, "WORKLIST.md"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read WORKLIST.md: %w", err)
	}

	var found []Violation
	for _, line := range strings.Split(string(body), "\n") {
		m := worklistEpicRowRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		cells := strings.Split(line, "|")
		// The Overall status cell is the 9th after the leading empty one. Rows
		// that are shaped differently are skipped rather than guessed at.
		if len(cells) < 10 || !doneCellRe.MatchString(cells[9]) {
			continue
		}
		id := m[1]

		// REQ-CROSS-167 (`RUN:2026-09-01`): the row's own cells are read FIRST.
		//
		// This sat below the record-link branch, which appends a violation and
		// continues — so a DONE row recording a sourced approval in its cells was
		// reported unverifiable whenever it linked no record. That is the very
		// shape the row describes as fixed, repaired for the link-present path
		// only. On the live corpus it produced five false reports, each against a
		// row carrying `approved USER:…`, found by turning on
		// TestRealWorkspaceApprovals.
		//
		// Both cells count. Most rows record the approval in the Approval cell,
		// but the Overall cell legitimately carries it inline —
		// "**DONE — approved `USER:2026-07-21`**" — and criterion 2 says that
		// satisfies the gate. Each is judged by the same rule as the record, not
		// by a bare USER: tag: a tag proves someone wrote a date, not that they
		// approved.
		if rdd.CompletionApprovalIn(cells[9]) != "" {
			continue
		}
		if len(cells) > 10 && rdd.CompletionApprovalIn(cells[10]) != "" {
			continue
		}

		link := epicRecordLinkRe.FindStringSubmatch(line)
		if link == nil {
			found = append(found, Violation{
				Rule:    "approval-before-done",
				File:    "WORKLIST.md",
				Subject: id,
				Detail:  fmt.Sprintf("%s is DONE but its row links no epic record, so its approval cannot be verified", id),
			})
			continue
		}
		rel := link[1]
		if rel == "" {
			rel = link[2] // the backticked alternative
		}

		// Approval is recorded in one of two places in practice: the epic
		// record's own section, or the work-list row's Approval cell. Both are
		// real — the cell is how most of this corpus records it — so either
		// satisfies the gate.
		//
		// The cell is judged by the same rule as the record, not by a bare
		// USER: tag: a tag proves someone wrote a date, not that they approved.
		// A cell reading "SPEC-APPROVE-EPIC-X (approved USER:…)" or "pending —
		// awaiting confirm" satisfied the old test, which put the majority of
		// this corpus outside the rule the comment above promises to delegate.
		// A cell that does not satisfy it falls through to the record, which
		// may carry the real approval.
		record, err := os.ReadFile(filepath.Join(root, rel))
		if os.IsNotExist(err) {
			found = append(found, Violation{
				Rule:    "approval-before-done",
				File:    "WORKLIST.md",
				Subject: id,
				Detail:  fmt.Sprintf("%s is DONE but its record %s does not exist", id, rel),
			})
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", rel, err)
		}

		if rdd.ApprovalLineOf(string(record)) == "" {
			found = append(found, Violation{
				Rule:    "approval-before-done",
				File:    rel,
				Subject: id,
				Detail:  fmt.Sprintf("%s is DONE with no approval carrying a USER: source", id),
			})
		}
	}

	sort.Slice(found, func(i, j int) bool { return found[i].Detail < found[j].Detail })
	return found, nil
}

// CheckAll runs every gate and returns the violations together, so a caller
// reports one complete picture rather than the first thing that happens to fail.
func CheckAll(root string) ([]Violation, error) {
	ledgers, err := CheckLedgers(root)
	if err != nil {
		return nil, err
	}
	approvals, err := CheckApprovals(root)
	if err != nil {
		return nil, err
	}
	return append(ledgers, approvals...), nil
}

// baselinePath is deliberately NOT under the local-state portion of
// .modernpath/, which holds credentials and machine state and is gitignored in
// every workspace that uses it. (.modernpath/rdd is a separate, versioned
// process package.) A baseline that is not committed is a per-developer
// baseline: everyone sees the same backlog, everyone re-accepts it locally, and
// the shared contract the gate is supposed to create never exists.
const (
	baselinePath       = ".modernpath/rdd/gate-baseline"
	legacyBaselinePath = ".claude/gate-baseline"
)

// Baseline is the set of violations a repository already had when the gate was
// introduced. Adopting a gate on an existing codebase is only workable if the
// backlog is separable from new breakage: without this, the first run blocks
// every commit and the gate gets disabled instead of obeyed.
type Baseline map[string]bool

// LoadBaseline reads the accepted-violation list. A missing file means no
// baseline, which is the correct default for a new repository.
func LoadBaseline(root string) (Baseline, error) {
	body, err := os.ReadFile(filepath.Join(root, baselinePath))
	if os.IsNotExist(err) {
		body, err = os.ReadFile(filepath.Join(root, legacyBaselinePath))
	}
	if os.IsNotExist(err) {
		return Baseline{}, nil
	}
	if err != nil {
		return nil, err
	}
	b := Baseline{}
	for _, line := range strings.Split(string(body), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			b[line] = true
		}
	}
	return b, nil
}

// MigrateLegacyBaseline gives hook adapters an agent-neutral process-data
// path while preserving repositories that adopted the original Claude path.
// The legacy file is left untouched; removing project data is not an install
// operation.
func MigrateLegacyBaseline(root string) error {
	current := filepath.Join(root, baselinePath)
	if _, err := os.Stat(current); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	legacy := filepath.Join(root, legacyBaselinePath)
	body, err := os.ReadFile(legacy)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(current), 0o755); err != nil {
		return err
	}
	return os.WriteFile(current, body, 0o644)
}

// WriteBaseline records the current violations as accepted.
func WriteBaseline(root string, violations []Violation) error {
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(baselinePath)), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# Violations accepted when the gate was adopted. New ones still block.\n")
	b.WriteString("# Remove a line to start enforcing that rule for that file.\n")
	keys := make([]string, 0, len(violations))
	for _, v := range violations {
		keys = append(keys, v.Key())
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(k + "\n")
	}
	return os.WriteFile(filepath.Join(root, baselinePath), []byte(b.String()), 0o644)
}

// Unbaselined returns the violations that are not already accepted.
func Unbaselined(violations []Violation, b Baseline) []Violation {
	var out []Violation
	for _, v := range violations {
		if !b[v.Key()] {
			out = append(out, v)
		}
	}
	return out
}
