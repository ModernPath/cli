package rdd

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	// A completion gate named on the line. Case-sensitive on purpose: these ids
	// are uppercase by convention, and a miss here would let the specification
	// veto drop a real approval. The leading class is what keeps
	// SPEC-APPROVE-EPIC-X from counting as its own completion gate.
	completionGateRe = regexp.MustCompile(`(^|[^A-Za-z-])(APPROVE-|APP-[A-Z]+-\d)`)

	// Markdown emphasis and the brackets a decision is written in. Stripped
	// from both ends so the decision itself can be read from the start.
	decisionNoiseRe = regexp.MustCompile("^[\\s*_`()\\[\\]]+|[\\s*_`()\\[\\]]+$")

	// A decision that was not granted. Anchored to the start of the segment it
	// judges — mid-text these words routinely mean something else, as in the
	// batch approval whose own words are "accept now all pending requirements".
	decisionWithheldRe = regexp.MustCompile(`(?i)^(pending|awaiting|blocked|deferred|rejected|declined|withdrawn|changes requested|not approved|no decision)\b`)

	// The refusals that are unambiguous WHEREVER they sit — no granted line
	// quotes "not approved" or "no decision" about itself — so in prose they
	// veto mid-line, where the anchored set cannot reach.
	decisionRefusedAnywhereRe = regexp.MustCompile(`(?i)\b(not approved|no decision|changes requested|rejected|declined|withdrawn)\b`)

	// The one deferral the corpus writes mid-cell: a decision that grants the
	// specification gate and names a later review for the completion one.
	decisionDeferredRe = regexp.MustCompile(`(?i)final approval\s+(at|after|pending|to be|remains)`)

	// A granted decision, as a decision column writes it.
	decisionGrantedRe = regexp.MustCompile(`(?i)^(approved|approve\b|accepted|signed[ -]off|sign-off)`)

	// The same vocabulary anywhere in a prose line, where the grant follows a
	// leading tag ("USER:… — Approved") or label ("**…recorded:** … option
	// `approve`") rather than opening the line.
	decisionGrantedAnywhereRe = regexp.MustCompile(`(?i)\b(approved|approve|accepted|signed[ -]off|sign-off)\b`)

	// A gate identifier in prose, stripped before the decision vocabulary is
	// read: the APPROVE inside APPROVE-EPIC-X names the gate, it does not
	// answer it. Case-sensitive like completionGateRe — the ids are uppercase
	// by convention, and a lowercase "approve" must keep meaning the decision.
	gateIDProseRe = regexp.MustCompile(`\b(SPEC-)?(APPROVE|APP)(-[A-Z0-9]+)+\b|\bSPEC-APPROVE\b`)
)

type requirementMembership struct {
	IDs        []string
	Recognized bool
	Line       int
}

// requirementMembershipOf is the one declaration reader used by epic payloads
// and both gate builders. Sections are read first in document order, followed
// by `**Realizes:**`/`**Requirements:**` bold-label lines; ids retain
// declaration order and duplicates collapse.
func requirementMembershipOf(text string) requirementMembership {
	result := requirementMembership{IDs: []string{}}
	seen := map[string]bool{}
	mark := func(offset int) {
		result.Recognized = true
		line := 1 + strings.Count(text[:offset], "\n")
		if result.Line == 0 || line < result.Line {
			result.Line = line
		}
	}
	add := func(source string) {
		addMembershipTokens(source, seen, &result.IDs, memberTokenRe)
	}

	for _, loc := range membershipSectionRe.FindAllStringIndex(text, -1) {
		mark(loc[0])
		body := text[loc[1]:]
		if end := membershipNextH2Re.FindStringIndex(body); end != nil {
			body = body[:end[0]]
		}
		add(body)
	}
	for _, loc := range membershipTableSectionRe.FindAllStringIndex(text, -1) {
		body := text[loc[1]:]
		if end := membershipNextH2Re.FindStringIndex(body); end != nil {
			body = body[:end[0]]
		}
		// Only a section that actually yields a member row is a declaration, so
		// a prose-only section stays unrecognized exactly as before.
		if addMembershipTableRows(body, seen, &result.IDs) {
			mark(loc[0])
		}
	}
	for _, loc := range membershipRealizesRe.FindAllStringSubmatchIndex(text, -1) {
		mark(loc[0])
		add(text[loc[2]:loc[3]])
	}
	return result
}

// addMembershipTableRows takes one member per data row — the first cell whose
// WHOLE content is a REQ- id — and reports whether any row yielded.
//
// Whole-cell, not "first id in the row", because these tables carry prose cells
// that cite requirements they do not declare: an evidence cell reading
// "5 REQ-SYS-005 tests" is a citation, and harvesting it makes the epic a member
// of a requirement it only mentions. Scanning cells left to right also steps over
// a leading display/task id column (SR-AF-001, SR-CLI-0081) without matching it.
// A header or separator row has no such cell and is skipped.
func addMembershipTableRows(body string, seen map[string]bool, out *[]string) bool {
	found := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		for _, cell := range strings.Split(strings.Trim(trimmed, "|"), "|") {
			cell = strings.TrimSpace(cell)
			if !membershipWholeCellReqRe.MatchString(cell) {
				continue
			}
			if !seen[cell] {
				seen[cell] = true
				*out = append(*out, cell)
				found = true
			}
			break
		}
	}
	return found
}

// addMembershipTokens harvests declared member ids from an already-scoped
// declaration string. `re` selects the vocabulary: memberTokenRe (REQ- and SR-
// kinds) for epic membership, reqOnlyMemberTokenRe (REQ- only) for the
// acceptance-scenario realizes reader, which deliberately treats an SR-only or
// bare-token row as a counted non-edge rather than a member.
func addMembershipTokens(source string, seen map[string]bool, ids *[]string, re *regexp.Regexp) {
	// Parenthetical annotations are commentary, not membership. Repeat so
	// separate annotations on the same declaration all disappear.
	for membershipParenRe.MatchString(source) {
		source = membershipParenRe.ReplaceAllString(source, " ")
	}
	source = membershipReassignRe.ReplaceAllString(source, " ")
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			*ids = append(*ids, id)
		}
	}

	for _, match := range re.FindAllStringSubmatch(source, -1) {
		id := match[1]
		// Carried verbatim — never re-parsed into prefix+number, so a display
		// id (SR-16-1) keeps its revision and an SR-/REQ- member keeps its kind.
		add(id)

		tail := memberTailNumRe.FindStringSubmatch(id)
		if tail == nil {
			continue
		}
		stem := id[:len(id)-len(tail[0])] // everything up to the trailing number
		first, err := strconv.Atoi(tail[1])
		if err != nil {
			continue
		}
		pad := func(n, width int) string { return fmt.Sprintf("%s%0*d", stem, width, n) }

		endPrefix, endDigits, alternate := match[2], match[3], match[4]
		if alternate != "" { // an a/b sibling under the same stem
			if n, err := strconv.Atoi(alternate); err == nil {
				add(pad(n, len(alternate)))
			}
			continue
		}
		if endDigits == "" { // no range
			continue
		}
		last, err := strconv.Atoi(endDigits)
		if err != nil {
			continue
		}
		if endPrefix != "" && endPrefix != stem {
			// A range whose end names another prefix is two ids, not a run under
			// the first: REQ-CROSS-001..REQ-UI-005 declares exactly those two.
			add(fmt.Sprintf("%s%0*d", endPrefix, len(endDigits), last))
			continue
		}
		if last < first {
			add(pad(last, len(endDigits)))
			continue
		}
		width := len(tail[1])
		if len(endDigits) > width {
			width = len(endDigits)
		}
		for n := first + 1; n <= last; n++ {
			add(pad(n, width))
		}
	}
}

func requirementIDsOf(text string) []string {
	return requirementMembershipOf(text).IDs
}
