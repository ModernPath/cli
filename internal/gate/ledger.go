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
	// Magnitude is the part of Detail that is identity rather than prose: the
	// numbers whose change makes this a different violation. Empty for rules
	// whose Subject already names the whole thing — see Key.
	Magnitude string
}

func (v Violation) String() string { return fmt.Sprintf("%s: %s — %s", v.Rule, v.File, v.Detail) }

// Key identifies a violation for baselining. It includes the Subject because
// six epics can break one rule in one file: keying on rule+file alone would
// accept the seventh silently, which is how a suppression list stops being a
// record of known debt and becomes a hole. It excludes Detail so that improving
// a message does not un-baseline anything.
//
// It does include Magnitude, where the rule sets one. status-hygiene does:
// accepting "DONE: rows say 2, Totals says 1" accepts a gap of exactly that
// size, and a key without the counts let that same gap widen forever inside its
// own baseline entry.
// The cost is churn: *any* movement of a baselined mismatch blocks until the
// baseline is rewritten, including movement toward correctness. That was the
// explicit trade. approval-before-done sets no Magnitude, so its existing
// baseline entries keep their keys.
func (v Violation) Key() string {
	k := v.Rule + "\t" + v.File + "\t" + v.Subject
	if v.Magnitude != "" {
		k += "\t" + v.Magnitude
	}
	return k
}

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
		found = append(found, checkOneLedger(rel, string(body))...)
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].File != found[j].File {
			return found[i].File < found[j].File
		}
		return found[i].Subject < found[j].Subject
	})
	return found, nil
}

func checkOneLedger(rel, body string) []Violation {
	totalsMatch := totalsRe.FindStringSubmatch(body)
	if totalsMatch == nil {
		// No dashboard yet — a freshly seeded context. Reporting it would train
		// people to ignore the gate, which costs more than the missing check.
		return nil
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
		return nil
	}

	// One violation per mismatched status, with the status as the subject and
	// the two counts as its magnitude. A single per-file violation would let a
	// baselined ledger drift arbitrarily further: the seventh broken status
	// looks identical to the six already accepted. Carrying the counts in the
	// key closes the rest of that hole: a baselined mismatch cannot widen inside
	// its own entry either.
	var found []Violation
	for _, status := range union(claimed, actual) {
		if claimed[status] != actual[status] {
			found = append(found, Violation{
				Rule:      "status-hygiene",
				File:      rel,
				Subject:   status,
				Detail:    fmt.Sprintf("%s: rows say %d, Totals says %d", status, actual[status], claimed[status]),
				Magnitude: fmt.Sprintf("rows=%d totals=%d", actual[status], claimed[status]),
			})
		}
	}
	return found
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

// ── Ledger shape (REQ-CROSS-147) ──────────────────────────────────────────

var (
	// What the op builder actually reads. A parenthetical qualifier inside the
	// bold is allowed (REQ-CROSS-137); a colon outside it is not.
	criteriaReadableRe = regexp.MustCompile(`^-\s+\*\*Acceptance criteria(?:\s*\([^)]*\))?:\*\*`)
	// Anything a human plausibly meant as that label.
	criteriaIntentRe = regexp.MustCompile(`(?i)^-\s+\*\*Acceptance criteria`)
	detailHeadRe     = regexp.MustCompile(`^###\s+(REQ-[A-Z]+-\d+)\b`)
)

// CheckLedgerShape catches two ways a ledger row loses content without anything
// failing. Both were found by their damage rather than by a check:
//
//   - a criteria label the parser cannot read — 139 criteria across 55 rows were
//     dropped this way, silently, for months (REQ-CROSS-137);
//   - a second detail block for a row that already has one, which leaves the
//     requirement with an empty description.
//
// Neither is a formatting preference. Both change what reaches the server.
func CheckLedgerShape(root string) ([]Violation, error) {
	dir := filepath.Join(root, "tasks")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Violation
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		rel := filepath.Join("tasks", e.Name())
		body, err := os.ReadFile(filepath.Join(root, e.Name()))
		if err != nil {
			body, err = os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				return nil, err
			}
		}
		seen := map[string]int{}
		current := ""
		for i, line := range strings.Split(string(body), "\n") {
			if m := detailHeadRe.FindStringSubmatch(line); m != nil {
				current = m[1]
				seen[current]++
				if seen[current] == 2 {
					out = append(out, Violation{
						Rule: "duplicate-detail-block", File: rel, Subject: current,
						Detail: fmt.Sprintf("%s has a second detail block at line %d — the requirement syncs with an empty description", current, i+1),
					})
				}
				continue
			}
			if criteriaIntentRe.MatchString(line) && !criteriaReadableRe.MatchString(line) {
				subj := current
				if subj == "" {
					subj = fmt.Sprintf("line %d", i+1)
				}
				out = append(out, Violation{
					Rule: "criteria-label-unreadable", File: rel, Subject: subj,
					Detail: fmt.Sprintf("%s: the acceptance-criteria label at line %d does not parse — every criterion beneath it is dropped. Write `- **Acceptance criteria:**`, qualifier inside the bold", subj, i+1),
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out, nil
}

// ── Elided citations (REQ-CROSS-148) ──────────────────────────────────────

// An ellipsis standing in for directories: `CODE:.../ai/proxy.ex`, or
// `apps/storage/.../user_requirement.ex`. Never legitimate — the author knew
// the path and left the reader one that resolves to nothing.
// It must END at a source file, and must not be an absolute path: an ellipsis
// inside `/var/folders/.../repos/` or a URL is prose, not a citation, and a
// commit gate that fails on prose gets switched off.
var elidedCitationRe = regexp.MustCompile(`(?:CODE:|` + "`" + `)(?:[A-Za-z0-9_.\-]+/)*\.\.\./[A-Za-z0-9_./\-]*\.(?:exs|tsx|ex|go|js|ts|py|rb|rs|java|kt|sql|yml|yaml|json|sh|heex)\b`)

// CheckElidedCitations scans the process corpora for citations whose directories
// have been replaced by an ellipsis. It is deliberately narrower than the full
// citation audit that ships with rdd-reverse-engineer: this one is a pure regex
// with no resolver, so it cannot produce a false failure, which is what a commit
// gate has to guarantee. The thorough sweep stays a script you run deliberately.
func CheckElidedCitations(root string) ([]Violation, error) {
	var out []Violation
	// The four process corpora, and deliberately NOT .claude/. Teaching material
	// has to exhibit the patterns it forbids — the reverse-engineering skill
	// carries two elided paths as worked examples, and a gate that failed on them
	// would force the documentation of a rule to violate it. Everything under
	// .claude/ is tool-owned instruction, not a claim about this codebase.
	//
	// Inside the four, describe the shape rather than showing it: three documents
	// tripped this gate on RUN:2026-08-14 by quoting the very pattern they were
	// describing, which is a real cost of the exclusion being drawn here.
	for _, dir := range []string{"docs", "epics", "tasks", "process"} {
		base := filepath.Join(root, dir)
		if _, err := os.Stat(base); os.IsNotExist(err) {
			continue
		}
		err := filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(p, ".md") {
				return err
			}
			body, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			for i, line := range strings.Split(string(body), "\n") {
				if m := elidedCitationRe.FindString(line); m != "" {
					out = append(out, Violation{
						Rule: "elided-citation", File: rel,
						Subject: fmt.Sprintf("line %d", i+1),
						Detail:  fmt.Sprintf("%s:%d cites %s… — write the path from the repository root; an elided path looks like a citation and resolves to nothing", rel, i+1, strings.TrimSuffix(m, "/")),
					})
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out, nil
}

var dashboardRowRe = regexp.MustCompile(`^\|\s*(REQ-[A-Z]+-\d+)\s*\|[^|]*\|[^|]*\|\s*([A-Z_]+)\s*\|`)

// CheckBlockRequired — REQ-CROSS-163: a row past PROPOSED carries a detail block.
//
// `rdd-ledger`'s criteria-first rule says no requirement enters IN_PROGRESS
// without a detail block carrying GIVEN/WHEN/THEN criteria. Nothing checked it.
// A hand sweep on RUN:2026-08-14 found it holding across 948 rows — which is the
// moment to mechanise it, not a reason to skip it: the failure is silent, and a
// blockless row syncs with an empty description and reads as a bare title
// everywhere it appears.
//
// PROPOSED and OBSOLETE are exempt. Both exemptions come from the sweep rather
// than from taste: PROPOSED means "not yet specified" (blocks are authored at
// SPECIFY), and the only four real exceptions in the corpus were OBSOLETE rows,
// each already naming what superseded it.
func CheckBlockRequired(root string) ([]Violation, error) {
	dir := filepath.Join(root, "tasks")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Violation
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		rel := filepath.Join("tasks", e.Name())
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		text := string(body)
		// detailHeadRe is anchored with `^` and no (?m), so it must be applied
		// per line — matching it against the whole file finds only a heading in
		// the first byte, which silently reports every row as blockless.
		lines := strings.Split(text, "\n")
		blocks := map[string]bool{}
		for _, line := range lines {
			if m := detailHeadRe.FindStringSubmatch(line); m != nil {
				blocks[m[1]] = true
			}
		}
		for _, line := range lines {
			m := dashboardRowRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			id, status := m[1], m[2]
			if status == "PROPOSED" || status == "OBSOLETE" || blocks[id] {
				continue
			}
			out = append(out, Violation{
				Rule: "missing-detail-block", File: rel, Subject: id,
				Detail: fmt.Sprintf("%s is %s with no detail block — it syncs with an empty description and reads as a bare title", id, status),
			})
		}
	}
	return out, nil
}
