package rdd

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	// §245.1: the corpus joins H1 id and title with an em-dash, en-dash, or
	// plain hyphen — all spaced; an unspaced hyphen is part of the id.
	epicH1Re     = regexp.MustCompile(`(?m)^#\s+\S+\s+[—–-]\s+(.+)$`)
	reqSectionRe = regexp.MustCompile(`(?s)## Requirements in this epic\n(.*?)(\n##|$)`)
	// Membership declarations deliberately recognize only the four named H2
	// shapes plus a `**Realizes:**`/`**Requirements:**` bold label line.
	// Nearby System/User requirement sections are different relations and must
	// never leak into epic membership.
	membershipSectionRe = regexp.MustCompile(`(?m)^##[ \t]+(?:Requirements in this epic|Requirements realized|Linked requirements|Requirements)[ \t]*\r?\n`)
	membershipNextH2Re  = regexp.MustCompile(`(?m)^##[ \t]+`)
	// The newer RDD-shaped records declare membership as a TABLE under
	// `## System requirements`, `## System requirements and tasks` or
	// `## Members`. The discriminator is the ROW, not the heading: those same
	// headings also carry prose that merely cites ids (see the mixed-headings
	// exclusion), and the tables' own leading column is a display/task id —
	// 343 such tokens across 109 records, none naming a requirement record.
	// Reading data rows and taking each row's subject cell excludes both.
	membershipTableSectionRe = regexp.MustCompile(`(?m)^##[ \t]+(?:System requirements and tasks|System requirements|Members)[ \t]*\r?\n`)
	membershipWholeCellReqRe = regexp.MustCompile(`^REQ-[A-Z][A-Z0-9]*-\d+[a-z]?$`)
	// A fifth shape: a top-of-record metadata line — `**Requirements:**`
	// alongside the record's `**Release:**`/`**Specification:**` banner, or
	// `**Realizes:**` elsewhere in the body — is the same declaration as the
	// four headings above, just spelled as one bold-label line instead of a
	// section. EPIC-CLI-004, EPIC-CMP-903 and EPIC-CMP-904 declare membership
	// only this way and had 0 rows in epic_system_requirements until this was
	// recognized.
	membershipRealizesRe = regexp.MustCompile(`(?m)^[ \t]*(?:-[ \t]*)?\*\*(?:Realizes|Requirements):\*\*[ \t]*(.*)$`)
	membershipParenRe    = regexp.MustCompile(`\([^()\n]*\)`)
	membershipReassignRe = regexp.MustCompile(`\bREQ-[A-Z][A-Z0-9]*-\d+[ \t]+is[ \t]+EPIC-[A-Z0-9][A-Z0-9-]*\b`)
	// Epic membership members: REQ- (generic) or SR- display ids, matched as
	// opaque tokens and carried VERBATIM so a display id keeps its revision
	// (SR-16-1 is not SR-16 — the old REQ-only, re-parsing tokenizer both
	// dropped SR-prefixed members and mangled 3-segment display ids). An
	// explicit a..b / a…b range, or an a/b pair, on the trailing number expands
	// under the shared stem. The (?:REQ|SR)- anchor plus a required numeric tail
	// keep capitalized prose inside a membership section from minting phantom
	// members; UR members travel the separate user-requirement path.
	memberTokenRe   = regexp.MustCompile(`((?:REQ|SR)-(?:[A-Za-z0-9]+-)*\d+[a-z]?)(?:[ \t]*(?:\.\.|…)[ \t]*((?:REQ|SR)-(?:[A-Za-z0-9]+-)*)?(\d+)|/(\d+))?`)
	memberTailNumRe = regexp.MustCompile(`(\d+)[a-z]?$`)
	// reqOnlyMemberTokenRe is memberTokenRe restricted to REQ- ids, for the
	// acceptance-scenario realizes reader: an SR-only or bare-token row there is
	// a counted non-edge, not a member (an SR id names no scenario-realizes
	// target). Keeping it REQ-only preserves that relation's vocabulary while
	// epic membership widens to SR- kinds.
	reqOnlyMemberTokenRe = regexp.MustCompile(`(REQ-(?:[A-Za-z0-9]+-)*\d+[a-z]?)(?:[ \t]*(?:\.\.|…)[ \t]*(REQ-(?:[A-Za-z0-9]+-)*)?(\d+)|/(\d+))?`)
	// The completion approval's heading, in the three shapes the corpus and the
	// installed template use. Alternation rather than a tolerated prefix on
	// purpose: `## Specification approval` is a DIFFERENT gate living one
	// heading above this one in every template-shaped record, and reading it
	// here is the substitution REQ-CROSS-110 exists to prevent.
	approvalHeadingRe = regexp.MustCompile(`(?i)## (?:human |completion )?approval\n`)
	approvalPendingRe = regexp.MustCompile(`(?i)_pending`)
	userTagRe         = regexp.MustCompile(`USER:\d{4}-\d{2}-\d{2}`)
	supersededByRe    = regexp.MustCompile(`(?i)superseded\s+by`)
	approvedLineRe    = regexp.MustCompile(`(?m)^\*{0,2}APPROVED\b[^\n]*USER:\d{4}-\d{2}-\d{2}[^\n]*$`)
	reqIDGlobalRe     = regexp.MustCompile(`REQ-[A-Z][A-Z0-9]*-\d+`)
)

// approvalLineOf finds the epic record's recorded approval, if any: an
// "## Approval", "## Human approval" or "## Completion approval" section
// carrying a USER: tag (not marked pending), or a line starting with
// **APPROVED carrying one.
//
// EVERY matching section is scanned, and each section line by line, because a
// record migrating between conventions keeps its old `## Approval` stub —
// often still `_pending_` — above the section that actually records the grant,
// and the approval inside any one section is a bullet OR a markdown table
// whose first line is a header. Across sections, as within one, the newest
// date wins.
//
// EVERY returned line carries a USER: tag. Both other callers slice it
// positionally as tag[5:]; a tag-less line panics them mid-sync.
func approvalLineOf(text string) string {
	var best, bestTag string
	for _, loc := range approvalHeadingRe.FindAllStringIndex(text, -1) {
		body := text[loc[1]:]
		if end := strings.Index(body, "\n##"); end >= 0 {
			body = body[:end]
		}
		if approvalPendingRe.MatchString(body) {
			continue
		}
		line := latestApprovalLine(body)
		if line == "" {
			continue
		}
		if tag := approvalTagOf(line); bestTag == "" || tag > bestTag {
			best, bestTag = line, tag
		}
	}
	if best != "" {
		return best
	}
	// The fallback is judged by the same rule, not merely matched: it re-scans
	// the whole document, so without this it readmits the very line
	// latestApprovalLine has just rejected. It also joins a wrapped paragraph
	// the way a headed approval does: a fallback line lives outside every
	// "## Approval" heading (a stray "###" subsection, or none at all), so it
	// never reaches latestApprovalLine's own join, and was cut at its
	// author's line wrap the same way completion approvals used to be.
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if approvedLineRe.MatchString(line) && isCompletionApproval(line, "") {
			return joinWrappedApproval(lines, i, "")
		}
	}
	return ""
}

// latestApprovalLine picks the tagged line carrying the newest date, so a
// section listing a specification approval and a later completion approval
// yields the completion one. Choosing by date rather than by position is what
// keeps existing records unchanged: where the dates tie — the common case — the
// first tagged line still wins, which is what the previous reader returned.
func latestApprovalLine(section string) string {
	var best, bestTag string
	lines := strings.Split(section, "\n")
	header := ""
	for i, line := range lines {
		if isTableHeader(lines, i) {
			header = line
			continue
		}
		tag := approvalTagOf(line)
		if tag == "" || !isCompletionApproval(line, header) {
			continue
		}
		if bestTag == "" || tag > bestTag { // ISO-8601 dates sort lexically
			best, bestTag = joinWrappedApproval(lines, i, header), tag
		}
	}
	return best
}

// joinWrappedApproval reads the approval as its author wrote it: one logical
// unit, which in hand-wrapped markdown spans several physical lines. Taking
// only the first cut 28 of 154 stored bases mid-sentence — at 73-79
// characters, markdown wrap width — severing the quotation of the human's own
// words that IS the justification.
//
// The unit ends where a new one begins: a blank line, a list item, a table row
// or a heading. A table row is already complete, so it is never extended —
// otherwise one approval row would swallow the row beneath it; its own
// justification is read out by tableRowBasis instead.
func joinWrappedApproval(lines []string, i int, header string) string {
	first := strings.TrimSpace(lines[i])
	if strings.HasPrefix(first, "|") {
		return tableRowBasis(first, header)
	}
	out := []string{first}
	for _, next := range lines[i+1:] {
		t := strings.TrimSpace(next)
		if t == "" || strings.HasPrefix(t, "|") || strings.HasPrefix(t, "#") ||
			strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "+ ") {
			break
		}
		out = append(out, t)
	}
	return strings.Join(strings.Fields(strings.Join(out, " ")), " ")
}

// tableRowBasis is a table row's own justification: the cell its header names
// Decision, not the whole row's markup — the same column decisionGrantedIn
// reads to judge the row, so what gets stored is what the human decided
// rather than the id/approver/source columns around it. Measured: 12 of 157
// stored approval bases were the entire raw row, pipes and all, because
// nothing here read the row at all.
//
// Every caller already required the ROW to carry a USER: tag before reaching
// this point (approvalTagOf gates latestApprovalLine's loop); the Decision
// cell alone usually does not — the tag lives in a sibling Source or Approver
// column instead. approvalTagOf(the returned text) must still find one: two
// callers slice it positionally at tag[5:]. So the tag is appended when the
// decision cell doesn't already carry it, rather than lost.
func tableRowBasis(row, header string) string {
	col := decisionColumnOf(header)
	cells := splitCells(row)
	if col < 0 || col >= len(cells) {
		// No recognized Decision column. decisionGrantedIn already requires
		// one to grant the row, so this is defensive, not reachable through
		// latestApprovalLine today — keep the row rather than guessing at the
		// wrong cell.
		return row
	}
	cell := strings.Join(strings.Fields(cells[col]), " ")
	if cell == "" {
		return row
	}
	if userTagRe.MatchString(cell) {
		return cell
	}
	if tag := approvalTagOf(row); tag != "" {
		return cell + " (" + tag + ")"
	}
	return row
}

// isTableHeader — a table row directly above the |---| divider. Tracked while
// scanning a section so a row's decision can be read from the column its own
// header designates.
func isTableHeader(lines []string, i int) bool {
	return strings.HasPrefix(strings.TrimSpace(lines[i]), "|") &&
		i+1 < len(lines) && tableDividerRe.MatchString(lines[i+1])
}

// isCompletionApproval reports whether a line records a GRANTED COMPLETION
// approval — the two questions the reader used to skip.
//
// Which gate: a line naming only the specification gate answers a different
// question (implementation may START). It is skipped, unless the same line also
// names the completion gate — records write both on one line, and discarding
// those whole loses a real, attributed sign-off.
//
// What decision: a record writes the decision in a table's decision column or
// at the head of a prose line, and "pending", "changes requested" and "final
// approval at the pilot review" are decisions too — they are just not this one.
// Reading only for a USER: tag turns every one of them into an approval.
func isCompletionApproval(line, header string) bool {
	if specApprovalLineRe.MatchString(line) && !completionGateRe.MatchString(line) {
		return false
	}
	return decisionGrantedIn(line, header)
}

// isSpecApproval is the same pair of questions asked of a line found under
// `## Specification approval`, where the heading already establishes which gate
// is answered. Only the mirror veto is needed: a record that lists its
// completion approval under the specification heading has not approved its
// specification (REQ-CROSS-114).
func isSpecApproval(line, header string) bool {
	if completionGateRe.MatchString(line) && !specApprovalLineRe.MatchString(line) {
		return false
	}
	return decisionGrantedIn(line, header)
}

// decisionGrantedIn reads the DECISION a line records, independent of which
// gate it answers — the half both approval readers share. header is the table
// header row above the line, "" when the line sits in no table.
func decisionGrantedIn(line, header string) bool {
	if decisionDeferredRe.MatchString(line) {
		return false
	}

	if cells := splitCells(line); len(cells) > 2 {
		// A table row. The decision is the cell the HEADER designates, never
		// the first cell that happens to read as one — a Role cell saying
		// "sign-off authority" describes who may decide, not that they did.
		// Cells after the decision qualify it rather than make it: the
		// template's trailing Conditions column routinely says "pending
		// <remaining work>" about a row whose Decision cell granted. A table
		// designating no Decision column records no readable decision, which
		// sync makes visible; a guessed one can invent an approval.
		col := decisionColumnOf(header)
		if col < 0 || col >= len(cells) {
			return false
		}
		c := decisionNoiseRe.ReplaceAllString(cells[col], "")
		if decisionWithheldRe.MatchString(c) {
			return false
		}
		return decisionGrantedRe.MatchString(c)
	}

	// Prose. The gate's own id is its name, not its answer — stripped before
	// any vocabulary is read, or APPROVE-EPIC-X grants itself. The decision
	// leads the line, so the head decides the ambiguous withheld words — a
	// granted approval quoting one ("accept now all pending requirements") is
	// not a withheld one — while the unambiguous refusals ("not approved",
	// "no decision") veto wherever they sit. Granting additionally requires a
	// granted word SOMEWHERE on the line: a neutral tagged line (a brief
	// bullet, a pointer cell) records no decision, and no decision is no grant.
	prose := gateIDProseRe.ReplaceAllString(line, "")
	if decisionWithheldRe.MatchString(decisionNoiseRe.ReplaceAllString(prose, "")) {
		return false
	}
	if decisionRefusedAnywhereRe.MatchString(prose) {
		return false
	}
	return decisionGrantedAnywhereRe.MatchString(prose)
}

// decisionColumnOf is the index, in a line's splitCells cells, of the column
// the header names Decision — -1 when the header designates none. Header and
// row must be split by the same rule, or an escaped pipe in either moves the
// decision out from under its column.
func decisionColumnOf(header string) int {
	if header == "" {
		return -1
	}
	for i, cell := range splitCells(header) {
		if strings.EqualFold(decisionNoiseRe.ReplaceAllString(cell, ""), "decision") {
			return i
		}
	}
	return -1
}

// approvalTagOf is the tag that dates an approval line: the LATEST one it
// carries. A single row records the build sanction and, days later, the
// sign-off given after the evidence summary; taking the first stamps the
// approval with the date of permission to start.
func approvalTagOf(line string) string {
	var latest string
	for _, tag := range userTagRe.FindAllString(line, -1) {
		if tag > latest { // ISO-8601 dates sort lexically
			latest = tag
		}
	}
	return latest
}

// An approval answers one gate. A specification approval says implementation may
// START; a completion approval says it is finished and reviewed. Reading "an
// approval" without asking which one lets the first stand in for the second, so
// an epic whose only approval is its spec gate reads as completion-approved with
// nothing built.
//
// Skipping by date is not enough: where both exist the later one already wins,
// but where only the specification approval exists there is nothing later to
// prefer.
var specApprovalLineRe = regexp.MustCompile(`(?i)SPEC-APPROVE-|spec(?:ification)?(?:\s+approval|:)`)

var (
	specStatusSectionRe = regexp.MustCompile(`(?s)## Specification status\s*\n(.*?)(\n## |\z)`)

	// The template's home for the specification decision: who approved it, in
	// what role, on whose authority. The marker section above states the stage;
	// this section records the answer (REQ-CROSS-114).
	specApprovalSectionRe = regexp.MustCompile(`(?is)## specification approval\n(.*?)(\n##|$)`)
)

// SpecApprovalLineOf reports where an epic's specification stands, from the two
// places a record puts it.
//
// The `## Specification status` marker (REQ-CROSS-024, EPIC-SYNC-006) is the
// record's own statement of stage: the FIRST non-empty line of the section.
// "approved" requires a USER:YYYY-MM-DD tag on that line — a claimed approval
// without a source stays at "ready" (the gate stays open; same honesty rule as
// approvalLineOf), and ("", "") means a legacy or as-built record whose stage
// this reader does not recognize.
//
// A granted, sourced row under `## Specification approval` — the installed
// template's shape — is that missing source. It lifts a SPEC-READY record, and
// a record with no marker section at all, to approved and supplies the
// attributed line. It does NOT override the record's own stage: a SPEC-DRAFT or
// SPEC-DERIVED record that also carries a granted table contradicts itself, and
// the conservative reading is published while collectEpicSpecs warns about the
// contradiction rather than resolving it silently.
func SpecApprovalLineOf(text string) (marker string, line string) {
	marker, line = specStatusMarkerOf(text)
	if marker == "ready" || (marker == "" && !specStatusSectionRe.MatchString(text)) {
		if row := specApprovalRowOf(text); row != "" {
			return "approved", row
		}
	}
	return marker, line
}

// specApprovalRowOf is the granted, USER:-tagged line under
// `## Specification approval`, latest date winning — the same rule
// latestApprovalLine applies to the completion section.
func specApprovalRowOf(text string) string {
	section := specApprovalSectionRe.FindStringSubmatch(text)
	if section == nil {
		return ""
	}
	var best, bestTag string
	lines := strings.Split(section[1], "\n")
	header := ""
	for i, line := range lines {
		if isTableHeader(lines, i) {
			header = line
			continue
		}
		tag := approvalTagOf(line)
		if tag == "" || !isSpecApproval(line, header) {
			continue
		}
		if bestTag == "" || tag > bestTag { // ISO-8601 dates sort lexically
			best, bestTag = strings.TrimSpace(line), tag
		}
	}
	return best
}

func specStatusMarkerOf(text string) (marker string, line string) {
	section := specStatusSectionRe.FindStringSubmatch(text)
	if section == nil {
		return "", ""
	}
	for _, l := range strings.Split(section[1], "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		switch {
		case strings.HasPrefix(l, "SPEC-DRAFT"):
			return "draft", l
		case strings.HasPrefix(l, "SPEC-READY"):
			return "ready", l
		case strings.HasPrefix(l, "SPEC-APPROVED"):
			// REQ-CROSS-111: the marker is prose and prose wraps, so the tag may
			// sit on a continuation line of this same section — read from the
			// marker line alone, an epic approved and shipped had its spec gate
			// re-opened. Return the line that CARRIES the tag, so the caller
			// still receives the attributed evidence rather than a bare claim.
			if userTagRe.MatchString(l) {
				return "approved", l
			}
			for _, cont := range strings.Split(section[1], "\n") {
				if userTagRe.MatchString(cont) {
					return "approved", strings.TrimSpace(cont)
				}
			}
			return "ready", l
		default:
			return "", ""
		}
	}
	return "", ""
}

// BuildSpecApprovalGateOp — the pre-implementation specification gate
// (REQ-CROSS-024): SPEC-READY opens SPEC-APPROVE-<EPIC>, SPEC-APPROVED with a
// USER: tag closes it. Only folder-form epics WITH specs gate — an approval
// request over nothing is dishonest (the missing-specs case is warned at
// snapshot time). Holds the EPIC (the implementation-approval gate holds the
// requirements — different concern).
func BuildSpecApprovalGateOp(epic Epic, recordText string) (Op, bool) {
	marker, line := SpecApprovalLineOf(recordText)
	if !folderForm(epic) || len(epic.Specs) == 0 || (marker != "ready" && marker != "approved") {
		return Op{}, false
	}

	title := "Approve specs: " + epic.ID
	if h1 := epicH1Re.FindStringSubmatch(recordText); h1 != nil {
		title += " — " + strings.TrimSpace(h1[1])
	}

	var index strings.Builder
	index.WriteString("Specs awaiting approval:\n")
	for _, s := range epic.Specs {
		fmt.Fprintf(&index, "- %s (%d chars)\n", s.Rel, len([]rune(s.Content)))
	}
	index.WriteString("\n" + line + "\n")
	if ids := requirementIDsOfSection(recordText); len(ids) > 0 {
		index.WriteString("\nRequirements: " + strings.Join(ids, " · ") + "\n")
	}

	payload := map[string]any{
		"external_id": "SPEC-APPROVE-" + epic.ID,
		"kind":        "spec_approval",
		"title":       title,
		"body_md":     capRunes(index.String(), 6000),
		"origin":      "workspace",
		"origin_ref":  epic.Record,
		"opened_at":   "2026-08-09T00:00:00.000000Z",
		"options": []any{
			map[string]any{"key": "approve", "label": "Approve specs — implementation may start", "body": "Record the approval (USER: tag) in the record's Specification status; the loop may then write its first RED test."},
			map[string]any{"key": "request_changes", "label": "Request changes", "body": "Send the specs back with what must change before approval."},
			map[string]any{"key": "defer", "label": "Defer", "body": "Keep the epic at the specification stage."},
		},
		"recommendation": "The specs/ folder is the review pack — grounded claims only; ungrounded content is grounds for changes.",
		"holds": []any{
			map[string]any{"held_external_id": epic.ID, "held_entity_type": "epic", "note": "specs awaiting approval"},
		},
	}

	if marker == "approved" {
		tag := userTagRe.FindString(line)
		payload["state"] = "answered"
		payload["answered_at"] = tag[5:] + "T00:00:00.000000Z"
		payload["source_tag"] = tag
		payload["answer"] = capRunes(line, 300)
		payload["chosen_option_keys"] = []any{"approve"}
	} else {
		payload["state"] = "open"
	}

	addGateBrief(payload, recordText)
	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	if payload["state"] == "answered" {
		payload["answerer"] = map[string]any{"kind": "human"}
	}
	return Op{Type: "upsert_gate", Payload: payload}, true
}

// BuildApprovalGateOp — an epic at the human gate IS a queue item; a recorded
// approval closes the same gate as a repo-borne answer. Returns zero Op
// (ok=false) when the epic is neither.
func BuildApprovalGateOp(epic Epic, recordText string) (Op, bool) {
	approvalLine := approvalLineOf(recordText)
	awaiting := epic.State == "awaiting-approval"
	if !awaiting && approvalLine == "" {
		return Op{}, false
	}
	// REQ-CROSS-096: a completion gate over nothing is as dishonest as a spec
	// gate over nothing, and the spec side already refuses. An epic with no
	// upper or lower evidence has nothing to approve, so a work-list cell
	// reading "pending" must not open one.
	//
	// The refusal applies ONLY to a gate that would be newly opened: on this
	// corpus 14 of 22 existing approval gates belong to epics with no
	// Upper/Lower cells and are already ANSWERED, and withholding those would
	// rewrite history rather than prevent a dishonest ask.
	if approvalLine == "" && !hasEpicEvidence(epic) {
		return Op{}, false
	}

	title := "Approve: " + epic.ID
	if h1 := epicH1Re.FindStringSubmatch(recordText); h1 != nil {
		title += " — " + strings.TrimSpace(h1[1])
	}
	originRef := epic.Record
	if originRef == "" {
		originRef = "WORKLIST.md"
	}
	payload := map[string]any{
		"external_id": "APPROVE-" + epic.ID,
		"kind":        "approval_request",
		"title":       title,
		"body_md":     capRunes(recordText, 6000),
		"origin":      "workspace",
		"origin_ref":  originRef,
		"opened_at":   "2026-07-22T00:00:00.000000Z",
		"options": []any{
			map[string]any{"key": "approve", "label": "Approve — evidence reviewed", "body": "Record the approval (USER: tag); the loop materializes it into the epic record."},
			map[string]any{"key": "request_changes", "label": "Request changes", "body": "Send it back with what must change before the gate."},
			map[string]any{"key": "defer", "label": "Defer", "body": "Not now — keep it at the gate."},
		},
		"recommendation": "The record’s evidence map is the review pack — read it, then approve, request changes, or defer.",
	}
	addGateHolds(payload, requirementIDsOfSection(recordText), "awaiting the "+epic.ID+" gate")
	// The holds name the epic's REQUIREMENTS, which is what makes those rows
	// read "awaiting the <epic> gate". They do not name the epic, and
	// `Core.Planning.EpicDetail` selects an epic's gates by
	// `epic.code in exact_scope or id in held_gate_ids` — so without this the
	// gate is invisible on the page of the epic it approves. Same line
	// BuildWorklistAcceptanceGate carries; BuildSpecApprovalGateOp reaches the
	// same place with an epic-typed hold.
	payload["exact_scope"] = []any{epic.ID}

	if approvalLine != "" {
		tag := approvalTagOf(approvalLine)
		payload["state"] = "answered"
		payload["answered_at"] = tag[5:] + "T00:00:00.000000Z"
		payload["source_tag"] = tag
		payload["answer"] = capAtWordBoundary(approvalLine, 300)
		payload["chosen_option_keys"] = []any{"approve"}
	} else {
		payload["state"] = "open"
	}

	addGateBrief(payload, recordText)
	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	if payload["state"] == "answered" {
		payload["answerer"] = map[string]any{"kind": "human"}
	}
	return Op{Type: "upsert_gate", Payload: payload}, true
}

// requirementIDsOfSection remains as the gate-call compatibility boundary;
// all consumers now share the same declaration vocabulary and expansion.
func requirementIDsOfSection(text string) []string {
	return requirementIDsOf(text)
}

func addGateOptions(payload map[string]any, options []Option, recommendation string) {
	if len(options) > 0 {
		opts := make([]any, len(options))
		for i, o := range options {
			label := o.Label
			if label == "" {
				label = fmt.Sprintf("(%d)", i+1)
			}
			opts[i] = map[string]any{
				"key":   label,
				"label": capRunes(o.Text, 120),
				"body":  o.Text,
			}
		}
		payload["options"] = opts
	}
	if recommendation != "" {
		payload["recommendation"] = recommendation
	}
}

func addGateHolds(payload map[string]any, pool []string, note string) {
	if len(pool) == 0 {
		return
	}
	holds := make([]any, len(pool))
	for i, id := range pool {
		entityType := "requirement"
		if epicRefRe.MatchString(id) {
			entityType = "epic"
		}
		holds[i] = map[string]any{
			"held_external_id": id,
			"held_entity_type": entityType,
			"note":             note,
		}
	}
	payload["holds"] = holds
}

// ApprovalLineOf exposes the record's recorded approval to other packages
// (REQ-CROSS-030's gate). Exported deliberately rather than reimplemented: two
// answers to "is this epic approved?" would drift, and drift between two
// implementations of one rule is the defect this workspace keeps finding.
func ApprovalLineOf(recordText string) string { return approvalLineOf(recordText) }

// ApprovalTagOf exposes the tag that dates an approval line (see approvalTagOf).
func ApprovalTagOf(approvalLine string) string { return approvalTagOf(approvalLine) }

// CompletionApprovalIn returns the granted completion approval recorded in a
// fragment of a record — a work-list approval cell, or a section body — or ""
// when it records none. It is the same judgment ApprovalLineOf applies to a
// whole record, exposed so no caller has to restate the rule.
func CompletionApprovalIn(fragment string) string { return latestApprovalLine(fragment) }
