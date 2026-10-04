package rdd

import (
	"regexp"
	"strings"
)

// ---------------------------------------------------------------- user requirements

// UserRequirement is the outcome an epic exists to deliver, written in its
// record as the reverse-engineering skill prescribes:
//
//	## User requirement
//
//	**UR-CON-001** — As a **public-sector organization** …, I want to …, so that …
//
//	**Actors:** `platform_admin` only (`CODE:…`)
//
// EPIC-SYNC-011 (`USER:2026-08-12`): sixteen of these sat in a real system's
// records while the platform held zero user requirements, because both builders
// wrote `kind: "system"` as a literal — leaving its whole traceability matrix as
// 874 orphans.
type UserRequirement struct {
	ID        string
	Statement string
	Actors    string
}

var (
	urIDLineRe = regexp.MustCompile(`^\*\*(UR-[A-Z][A-Z0-9]*-\d+)\*\*\s*(?:—|-|–)?\s*(.*)$`)
	urActorsRe = regexp.MustCompile(`(?m)^\*\*Actors:\*\*\s*(.+)$`)
	urCtxRe    = regexp.MustCompile(`^UR-([A-Z][A-Z0-9]*)-\d+$`)
	// `## User outcome (UR-SY-010)` — the id in the heading rather than the body.
	urHeadingIDRe = regexp.MustCompile(`##\s+User (?:outcome|requirement)[^\n]*\((UR-[A-Z][A-Z0-9]*-\d+)\)`)
	// "As a X, I want Y, so that Z" — Y is the title, the whole sentence the
	// description. The "so that" clause is the rationale, not the ask.
	urWantRe = regexp.MustCompile(`(?i),\s*I want\s+(?:to\s+)?(.+?)(?:,\s*so that\b.*)?$`)
)

// ParseEpicUserRequirement reads the record's user requirement, if it states
// one. A record with no section, or a section whose first bold token is not a
// UR id, yields nothing — a missing user requirement stays visible as missing,
// where an invented one is indistinguishable from a real one (#9 honesty rule,
// same as ParseDescription).
// REQ-CROSS-073: the third declaration shape. 52 user requirements — the whole
// EPIC-PORTFOLIO-* family — declare themselves only in a
// `## Linked user requirements` table. Their epics DO have a `## User outcome`
// heading, but without the id in it, so neither existing shape matched and no
// UserRequirement entity was ever built. Their requirements' UR column then
// named a parent the platform had no row for, leaving ~100 requirements as
// orphans in the compliance matrix with no `derives` trace possible.
//
// Scoped to that section on purpose: mining every table row that starts with a
// UR id would pull ids out of Source-material and cross-reference tables.
var (
	linkedURBulletRe  = regexp.MustCompile("^-\\s+`?(UR-[A-Z][A-Z0-9]*-\\d+)`?\\s+[—–-]\\s+(.+)$")
	linkedURSectionRe = regexp.MustCompile(`(?ms)^##+\s*Linked user requirements\s*$(.*?)(?:^##|\z)`)
	linkedURRowRe     = regexp.MustCompile(`^\|\s*(UR-[A-Z0-9]+-\d+)\s*\|([^|]*)\|`)
)

// REQ-CROSS-073, fourth shape: `- **User outcome (UR-SYNC-005):** <statement>`,
// a bullet with the colon inside the bold. The statement runs to the end of that
// bullet — stopping at the next one, so a "Blast radius" line beneath does not
// get swallowed into the user outcome.
var bulletOutcomeRe = regexp.MustCompile(`(?m)^[ \t]*-[ \t]*\*\*User outcome \((UR-[A-Z0-9]+-\d+)\)[: \t]*\*\*[: \t]*(.+)$`)

func parseBulletOutcome(recordText string) (UserRequirement, bool) {
	m := bulletOutcomeRe.FindStringSubmatch(recordText)
	if m == nil {
		return UserRequirement{}, false
	}
	return bulletOutcomeFrom(m)
}

func parseBulletOutcomes(recordText string) []UserRequirement {
	var out []UserRequirement
	for _, m := range bulletOutcomeRe.FindAllStringSubmatch(recordText, -1) {
		if ur, ok := bulletOutcomeFrom(m); ok {
			out = append(out, ur)
		}
	}
	return out
}

func bulletOutcomeFrom(m []string) (UserRequirement, bool) {
	statement := strings.Join(strings.Fields(m[2]), " ")
	if statement == "" {
		return UserRequirement{}, false
	}
	return UserRequirement{ID: m[1], Statement: capRunes(statement, 600)}, true
}

// REQ-CROSS-075, fifth shape: `## UR-ABS-1 — the user requirement`, the id
// leading its own heading, with the statement as the paragraph beneath. One
// corpus writes its 89 modern epics this way, and no "User outcome" heading exists in
// them at all.
//
// The em-dash (or hyphen) after the id is what makes this a DECLARATION rather
// than a passing mention: `## How UR-NOPE-001 relates to this epic` must not
// match, or every cross-reference heading becomes a user requirement.
var idLedOutcomeRe = regexp.MustCompile(`(?ms)^##+[ \t]*(UR-[A-Z0-9]+-\d+)[ \t]*[—–-][^\n]*\n(.*?)(?:\n##|\z)`)

func parseIDLedOutcome(recordText string) (UserRequirement, bool) {
	m := idLedOutcomeRe.FindStringSubmatch(recordText)
	if m == nil {
		return UserRequirement{}, false
	}
	return idLedOutcomeFrom(m)
}

// The heading alone, with the body found by index rather than consumed by the
// same pattern. idLedOutcomeRe swallows the `\n##` that ends the first body, so
// scanning it repeatedly would eat every second heading — the plural has to walk
// the headings itself. ops.js walks them the same way for the same reason.
var (
	idLedHeadingRe   = regexp.MustCompile(`(?m)^##+[ \t]*(UR-[A-Z0-9]+-\d+)[ \t]*[—–-][^\n]*$`)
	anyHeadingLineRe = regexp.MustCompile(`(?m)^##+`)
)

// parseIDLedOutcomes reads every id-led heading, not only the first: a record
// may declare one outcome per heading.
func parseIDLedOutcomes(recordText string) []UserRequirement {
	var out []UserRequirement
	for _, loc := range idLedHeadingRe.FindAllStringSubmatchIndex(recordText, -1) {
		body := recordText[loc[1]:]
		if next := anyHeadingLineRe.FindStringIndex(body); next != nil {
			body = body[:next[0]]
		}
		if ur, ok := idLedOutcomeFrom([]string{"", recordText[loc[2]:loc[3]], body}); ok {
			out = append(out, ur)
		}
	}
	return out
}

func idLedOutcomeFrom(m []string) (UserRequirement, bool) {
	// the first non-empty paragraph beneath the heading is the statement
	for _, para := range strings.Split(strings.TrimSpace(m[2]), "\n\n") {
		text := strings.Join(strings.Fields(strings.TrimSpace(para)), " ")
		text = strings.ReplaceAll(text, "**", "")
		text = strings.TrimSpace(text)
		if text == "" || strings.HasPrefix(text, "- ") || strings.HasSuffix(text, ":") {
			continue
		}
		return UserRequirement{ID: m[1], Statement: capRunes(text, 600)}, true
	}
	return UserRequirement{}, false
}

// parseLinkedURTable reads the first data row of the linked-user-requirements
// table: the id and the statement beside it.
func parseLinkedURTable(recordText string) (UserRequirement, bool) {
	rows := parseLinkedURTableRows(recordText)
	if len(rows) == 0 {
		return UserRequirement{}, false
	}
	return rows[0], true
}

// parseLinkedURTableRows reads EVERY data row of the section. One row is one
// declared membership; returning only the first kept the epic's leading user
// requirement and dropped the rest, so the epic→UR join read as complete while
// the second and later rows had no edge at all.
//
// The section bound is what keeps this honest: a requirement-traceability table
// and a source-material table also open their rows with a UR id, and neither
// declares membership.
func parseLinkedURTableRows(recordText string) []UserRequirement {
	sec := linkedURSectionRe.FindStringSubmatch(recordText)
	if sec == nil {
		return nil
	}
	var out []UserRequirement
	for _, line := range strings.Split(sec[1], "\n") {
		m := linkedURRowRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			// Mapping audit: the sixth declaration shape — a
			// BULLET (`- UR-XXX-NNN — statement`) — cost ten URs and seventy
			// derives edges.
			m = linkedURBulletRe.FindStringSubmatch(strings.TrimSpace(line))
		}
		if m == nil {
			continue
		}
		statement := strings.Join(strings.Fields(m[2]), " ")
		if statement == "" {
			continue
		}
		out = append(out, UserRequirement{ID: m[1], Statement: capRunes(statement, 600)})
	}
	return out
}

// parseUserOutcomeIDLines reads every `**UR-…** — statement` line inside the
// `## User outcome` section. The section's first line is the one the single-UR
// reader takes; a section listing several declares several.
func parseUserOutcomeIDLines(recordText string) []UserRequirement {
	m := epicUserOutcomeRe.FindStringSubmatch(recordText)
	if m == nil {
		return nil
	}
	var out []UserRequirement
	for _, line := range strings.Split(m[1], "\n") {
		id := urIDLineRe.FindStringSubmatch(strings.TrimSpace(line))
		if id == nil {
			continue
		}
		statement := strings.TrimSpace(id[2])
		if statement == "" {
			continue
		}
		out = append(out, UserRequirement{ID: id[1], Statement: capRunes(statement, 600)})
	}
	return out
}

// ParseEpicUserRequirements reads EVERY user requirement the record declares,
// in declaration order and deduplicated by id.
//
// ParseEpicUserRequirement answers a narrower question — which single one leads
// the record — and every one of its shapes stops at its first match. An epic
// that names two outcomes therefore carried one membership and one entity, and
// the join read as complete because the count of epics and the count of user
// requirements both reconciled.
//
// The leading declaration stays first, so a record whose payload already named
// a user requirement keeps naming the same one; the others join it behind. Each
// shape contributes only declarations that state BOTH an id and a statement — an
// id with no statement would need a statement invented for it, and an invented
// user requirement is indistinguishable from a real one.
func ParseEpicUserRequirements(recordText string) []UserRequirement {
	seen := map[string]bool{}
	var out []UserRequirement
	add := func(ur UserRequirement, ok bool) {
		if !ok || ur.ID == "" || ur.Statement == "" || seen[ur.ID] {
			return
		}
		seen[ur.ID] = true
		out = append(out, ur)
	}
	add(ParseEpicUserRequirement(recordText))
	for _, ur := range parseUserOutcomeIDLines(recordText) {
		add(ur, true)
	}
	for _, ur := range parseIDLedOutcomes(recordText) {
		add(ur, true)
	}
	for _, ur := range parseBulletOutcomes(recordText) {
		add(ur, true)
	}
	for _, ur := range parseLinkedURTableRows(recordText) {
		add(ur, true)
	}
	return out
}

func ParseEpicUserRequirement(recordText string) (UserRequirement, bool) {
	m := epicUserOutcomeRe.FindStringSubmatch(recordText)
	if m == nil {
		// No `User outcome` section at all — an id-led heading, a bullet or the
		// linked table may still declare it.
		if ur, ok := parseIDLedOutcome(recordText); ok {
			return ur, true
		}
		if ur, ok := parseBulletOutcome(recordText); ok {
			return ur, true
		}
		return parseLinkedURTable(recordText)
	}
	section := strings.TrimSpace(m[1])
	paras := strings.Split(section, "\n\n")
	head := strings.Join(strings.Fields(strings.TrimSpace(paras[0])), " ")

	var ur UserRequirement
	switch {
	case urIDLineRe.MatchString(head):
		// The skill's shape: **UR-CON-001** — As a … I want … so that …
		id := urIDLineRe.FindStringSubmatch(head)
		ur = UserRequirement{ID: id[1], Statement: capRunes(strings.TrimSpace(id[2]), 600)}

	case urHeadingIDRe.MatchString(m[0]):
		// This workspace's shape: `## User outcome (UR-SY-010)` with the
		// statement as prose beneath. Both are in daily use; reading only the
		// first left 40 epics here naming a UR and 4 extracting it
		// (`RUN:2026-08-12`).
		ur = UserRequirement{
			ID:        urHeadingIDRe.FindStringSubmatch(m[0])[1],
			Statement: capRunes(head, 600),
		}

	default:
		// REQ-CROSS-073: the heading exists but carries no id. Fall back to the
		// bullet shape, then to the linked-user-requirements table — the two
		// places the remaining families declare both id and statement.
		if ur, ok := parseBulletOutcome(recordText); ok {
			return ur, true
		}
		return parseLinkedURTable(recordText)
	}
	if a := urActorsRe.FindStringSubmatch(section); a != nil {
		ur.Actors = capRunes(strings.Join(strings.Fields(a[1]), " "), 300)
	}
	if ur.Statement == "" {
		return UserRequirement{}, false
	}
	return ur, true
}

// urWorkStatus maps the epic's WORKLIST state onto the ledger vocabulary. The
// user requirement is delivered by the epic, so it is exactly as done as the
// epic is — nothing is inferred beyond that.
func urWorkStatus(epic Epic) string {
	// The exact lifecycle token wins (mapping audit:
	// IN_REVIEW epics were yielding PROPOSED URs); the five-bucket state
	// stays the honest fallback for token-less rows.
	if epic.ProcessStatus != "" {
		return epic.ProcessStatus
	}
	return urWorkStatusBucket(epic.State)
}

func urWorkStatusBucket(state string) string {
	switch state {
	case "done":
		return "DONE"
	case "awaiting-approval":
		return "IN_REVIEW"
	case "in-progress":
		return "IN_PROGRESS"
	default:
		return "PROPOSED"
	}
}

// BuildUserRequirementOp emits the epic's user requirement as a requirement of
// kind "user". The server has routed that kind to UserRequirement all along
// (`requirement_schema/1`); nothing ever sent it.
func BuildUserRequirementOp(ur UserRequirement, epic Epic) Op {
	ctx := ""
	if m := urCtxRe.FindStringSubmatch(ur.ID); m != nil {
		ctx = m[1]
	}
	// A user requirement is written "As a X, I want Y, so that Z". The title is
	// the WANT — the thing being asked for — because a list of rows all
	// beginning "As a…" is unreadable, and the full sentence is the description.
	statement := strings.TrimSpace(strings.ReplaceAll(ur.Statement, "**", ""))
	title := statement
	if m := urWantRe.FindStringSubmatch(statement); m != nil {
		title = strings.TrimSpace(m[1])
		if r := []rune(title); len(r) > 0 {
			title = strings.ToUpper(string(r[0])) + string(r[1:])
		}
	}
	title = capRunes(strings.TrimRight(title, " ,."), 200)

	payload := map[string]any{
		"external_id":      ur.ID,
		"kind":             "user",
		"title":            title,
		"description":      statement,
		"context":          ctx,
		"stage":            nil,
		"work_status":      urWorkStatus(epic),
		"source_citations": []any{map[string]any{"kind": "epic", "ref": epic.ID}},
	}
	if ur.Actors != "" {
		payload["source_citations"] = append(payload["source_citations"].([]any),
			map[string]any{"kind": "note", "ref": "Actors: " + strings.ReplaceAll(ur.Actors, "**", "")})
	}
	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	return Op{Type: "upsert_requirement", Payload: payload}
}

// folderForm reports whether the epic record is the folder convention
// (…/EPIC.md) — the presence rule for the specs payload key.
func folderForm(epic Epic) bool {
	return strings.HasSuffix(epic.Record, "/EPIC.md")
}
