package rdd

// REQ-CROSS-249 (EPIC-CLI-003 T12): historical RUN: evidence. Every RUN:
// token the corpus carries — ledger cells and detail blocks, WORKLIST rollup
// cells, epic records — becomes one evidence result on a run whose identity
// IS the historical tag, with kind migration and explicit
// inherited_unverified validity, so a re-import upserts the same runs and
// derived live evidence state never reads history as current (the
// D4-successor decision, USER:2026-08-24).
//
// Outcome mapping is conservative: GREEN/pass-counted context reads pass, a
// RED context records the expected failure, and a token that states no
// outcome is skip — never pass. The token's own line rides verbatim as
// raw_evidence.
//
// REQ-CROSS-260 replaced the (run, target) key with the distinct
// (run, target, LINE CONTENT) triple. Under the old key the first citing
// line's verdict stood for every later citation of that tag on that record and
// the later lines were appended into merged raw text, then capped: measured on
// the corpus, 834 lines discarded into 376 merged groups, 89 of them hiding a
// stated outcome behind a silent first line, 8 recording pass over a later RED,
// and 198 results losing 263,715 runes to a cap that existed only here — the
// store column is unbounded text.

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var (
	runTagRe     = regexp.MustCompile(`RUN:\d{4}-\d{2}-\d{2}(?::[A-Za-z0-9._-]+)?`)
	greenCtxRe   = regexp.MustCompile(`(?i)\bGREEN\b|\bpass(?:es|ed|ing)?\b|\b\d+/\d+\b|\bok\b`)
	redCtxRe     = regexp.MustCompile(`(?i)\bRED\b|\bfail(?:s|ed|ing)?\b`)
)

// LineEvidenceToken is the stable identity of one historical corpus line: a
// fixed-width digest of the line's content, carried in the result's
// test_case_ref.
//
// This is a contract of USE, stated plainly. That column is the stable
// test-case identity slot in the store's extended result identity, and the
// process asks evidence for a stable test-case identity — here the digest IS
// the stable identity of a historical corpus line. A reader of test_case_ref on
// a migration-kind run sees line tokens, not test names.
//
// Fixed width is the point: a raw line cannot ride the identity because the
// column is varchar(255), and a truncated line would collide two different
// lines into one identity — the loss this whole change removes.
func LineEvidenceToken(line string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(line)))
	return "line:" + hex.EncodeToString(sum[:])[:16]
}

// BuildEvidenceImport parses the corpus's historical RUN: tokens into the
// runs and per-line results the migrate run posts to the evidence endpoint.
func BuildEvidenceImport(data Data, records map[string]string) EvidenceImport {
	var imp EvidenceImport
	runSeen := map[string]bool{}
	// One result per distinct (run, target, line content). Repeated
	// occurrences of one tag within a single line — and an identical line
	// repeated elsewhere on the same record — collapse into that one line's
	// result, counted in Collapsed. A per-OCCURRENCE token would give the 92
	// corpus lines that repeat one tag identical extended identities, and the
	// store's identity index would hard-refuse the whole evidence post.
	resultAt := map[string]bool{}

	addToken := func(tag, target, targetType, line string) {
		imp.Occurrences++
		runID := "HIST-" + tag
		if !runSeen[runID] {
			runSeen[runID] = true
			imp.Runs = append(imp.Runs, map[string]any{
				"external_id": runID,
				"kind":        "migration",
				"ran_at":      tag[4:14] + "T00:00:00Z",
			})
		}
		text := strings.TrimSpace(line)
		token := LineEvidenceToken(text)
		key := runID + "|" + targetType + "|" + target + "|" + token
		if resultAt[key] {
			imp.Collapsed++
			return
		}
		result := "skip"
		switch {
		case redCtxRe.MatchString(line):
			// RED beats GREEN in a mixed line: "RED first, then green" is a
			// red-first record, and the pass belongs to the later tag
			result = "fail"
		case greenCtxRe.MatchString(line):
			result = "pass"
		}
		resultAt[key] = true
		imp.Results = append(imp.Results, map[string]any{
			"run_external_id":    runID,
			"target_external_id": target,
			"target_type":        targetType,
			"result":             result,
			"role":               "historical",
			"validity":           "inherited_unverified",
			// The store's extended result identity spans the target clause,
			// this reference and the role. The reference is the per-line
			// discriminator, and it is a FIXED-WIDTH digest rather than the
			// line itself: the column is varchar(255) with no changeset length
			// guard, and 927 of the corpus's ~1,790 token-bearing lines are
			// longer than that.
			"test_case_ref": token,
			"raw_evidence":  text,
		})
	}

	scanText := func(text, target, targetType string) {
		for _, line := range strings.Split(text, "\n") {
			for _, tag := range runTagRe.FindAllString(line, -1) {
				addToken(tag, target, targetType, line)
			}
		}
	}

	for _, q := range data.Reqs {
		scanText(strings.Join([]string{q.Title, q.Source, q.Tests, q.Code, q.UR}, "\n"), q.ID, "requirement")
		scanText(q.Detail, q.ID, "requirement")
	}
	for _, e := range data.Epics {
		scanText(strings.Join([]string{e.Upper, e.Lower, e.ApprovalCell}, "\n"), e.ID, "epic")
		scanText(records[e.ID], e.ID, "epic")
	}
	return imp
}
