package rdd

import (
	"regexp"
	"strings"
)

// specContentCap mirrors the schema's $defs.spec content_md maxLength — the
// builders cap-and-warn, so an oversize spec can never reach the wire
// (REQ-CROSS-023; warning emitted at snapshot time, never in the payload).
const specContentCap = 65536

// `(?m)` is deliberately absent: with it, `$` would match the first line end
// and truncate a wrapped paragraph to its first line.
// Both headings are legitimate: workspace-authored epics say "User outcome",
// and the reverse-engineering skill writes "User requirement". Sixteen epics on
// a real system synced with an empty description because only the first was
// recognised, and nothing reported it (`RUN:2026-08-12`).
var epicUserOutcomeRe = regexp.MustCompile(`(?s)(?:^|\n)##\s+User (?:outcome|requirement)[^\n]*\n(.*?)(\n##|$)`)

// The rollup recovery carrier is narrower than the regular declaration
// reader: its id comes from WORKLIST, so its other half must be an actual
// `## User outcome` statement in that row's named record. `User requirement`
// headings and rollup prose do not substitute for that missing half.
var rollupUserOutcomeRe = regexp.MustCompile(`(?s)(?:^|\n)##\s+User outcome[^\n]*\n(.*?)(\n##|$)`)

// ParseEpicDescription — the epic's "## User outcome" section, first paragraph,
// as one line. 105 of 139 records carry the heading; the rest get no
// description rather than an invented one (same honesty rule as
// ParseDescription).
func ParseEpicDescription(recordText string) string {
	if m := epicUserOutcomeRe.FindStringSubmatch(recordText); m != nil {
		para := strings.TrimSpace(strings.SplitN(strings.TrimSpace(m[1]), "\n\n", 2)[0])
		return capRunes(strings.Join(strings.Fields(para), " "), 600)
	}
	// No `User outcome` heading. 34 of 190 records are in this shape and they
	// landed with no description at all, which leaves the app an id and a
	// title. Two fallbacks, ordered by how much each assumes.

	// The user requirement the existing parsers already read. Nine of the 34
	// declare their outcome as a labelled bullet — `- **User outcome
	// (UR-SYNC-001):** …`, `- **UR:** …` — and ParseEpicUserRequirement reads
	// those today. Reusing it invents nothing: the same sentence, from a
	// parser that already recognizes the shape.
	if ur, ok := ParseEpicUserRequirement(recordText); ok && ur.Statement != "" {
		return capRunes(strings.Join(strings.Fields(ur.Statement), " "), 600)
	}

	// Otherwise the record's opening paragraph, and only when it is prose.
	// Twenty-six records open with a real description; the remainder open with
	// a metadata banner (`- **Status:** … · **Owner:** …`). A list is never a
	// description, so a leading list marker disqualifies the paragraph rather
	// than filling the field with metadata — better empty than wrong.
	return epicLeadProse(recordText)
}

// epicLeadProse is the first paragraph between the record's H1 and its first
// level-2 section, when that paragraph is prose. A `**Bold** lead` is prose; a
// `- ` or `* ` list item is not.
func epicLeadProse(recordText string) string {
	var lead []string
	for i, line := range strings.Split(recordText, "\n") {
		if i == 0 && strings.HasPrefix(line, "# ") {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			break
		}
		lead = append(lead, line)
	}
	para := strings.TrimSpace(strings.SplitN(strings.TrimSpace(strings.Join(lead, "\n")), "\n\n", 2)[0])
	if para == "" {
		return ""
	}
	if strings.HasPrefix(para, "- ") || strings.HasPrefix(para, "* ") ||
		strings.HasPrefix(para, "+ ") || strings.HasPrefix(para, "|") {
		return "" // a list or a table states facts about the work, not the outcome
	}
	return capRunes(strings.Join(strings.Fields(para), " "), 600)
}

func parseRollupUserOutcome(recordText string) string {
	m := rollupUserOutcomeRe.FindStringSubmatch(recordText)
	if m == nil {
		return ""
	}
	para := strings.TrimSpace(strings.SplitN(strings.TrimSpace(m[1]), "\n\n", 2)[0])
	return capRunes(strings.Join(strings.Fields(para), " "), 600)
}

func rollupUserRequirements(rawCell, recordText string) []UserRequirement {
	statement := parseRollupUserOutcome(recordText)
	if statement == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []UserRequirement
	for _, id := range urTokenRe.FindAllString(rawCell, -1) {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, UserRequirement{ID: id, Statement: statement})
	}
	return out
}

func BuildEpicOp(epic Epic, recordText string) Op {
	title := epic.ID
	if h1 := epicH1Re.FindStringSubmatch(recordText); h1 != nil {
		title = strings.TrimSpace(h1[1])
	}
	membership := requirementMembershipOf(recordText)
	idsAny := make([]any, len(membership.IDs))
	for i, id := range membership.IDs {
		idsAny[i] = id
	}
	payload := map[string]any{
		"external_id": epic.ID,
		"title":       title,
	}
	// No declaration is an authoritative empty set. A recognized declaration
	// that parses to zero is doubt: omit the field so sync preserves stored
	// links, while the fidelity report names the anomaly.
	if !membership.Recognized || len(membership.IDs) > 0 {
		payload["requirement_external_ids"] = idsAny
	}
	// REQ-PLN-054 TASK-MC-107: the user outcome is what an epic IS about; without
	// it the app can only show an id and a title.
	if desc := ParseEpicDescription(recordText); desc != "" {
		payload["description"] = desc
	}
	// REQ-CROSS-028: the loop state WORKLIST already records, into the two
	// fields the contract has always had. Absent cells stay absent — an epic
	// with nothing recorded keeps the hash it had before this shipped.
	// statusCell again here, not only in ParseWorklist: the placeholder rule is
	// a property of the payload, so it holds for any caller building an op.
	if upper := statusCell(epic.Upper); upper != "" {
		payload["upper_loop_status"] = upper
	}
	if lower := statusCell(epic.Lower); lower != "" {
		payload["lower_loop_status"] = lower
	}
	// REQ-CROSS-223: the epic's PROCESS.md lifecycle — the board status axis
	// is set-once and the loop columns are never-a-re-mapping, so the exact
	// Overall-status token rides its own field. Absent cells stay absent.
	if epic.ProcessStatus != "" {
		payload["process_status"] = epic.ProcessStatus
	}
	// Mapping audit: the epic→UR edge the corpus declares in
	// every record's User-outcome section finally gets a payload carrier —
	// epic_user_requirements had no sync writer.
	// Every declaration is a membership: a record that names two user
	// requirements has two edges, and the list is what carries them.
	// The edge is declared in two places: the record's own outcome section and
	// the WORKLIST row's user-requirements cell. Reading only the record left
	// 18 user requirements whose source_citations name an epic with no matching
	// edge — the store contradicting itself inside one row. URCell's contract
	// already covers this: it "names the denominator even when the named epic
	// record does not repeat the id". A membership edge is not a statement, and
	// only statements were ever barred from this cell.
	// Record order first, then cell order, so an epic whose record already
	// names its UR keeps its exact payload — and its content hash.
	{
		ids := make([]string, 0, 4)
		seen := map[string]bool{}
		addUR := func(id string) {
			if id == "" || seen[id] {
				return
			}
			seen[id] = true
			ids = append(ids, id)
		}
		for _, ur := range ParseEpicUserRequirements(recordText) {
			addUR(ur.ID)
		}
		cell := epic.URCell
		for membershipParenRe.MatchString(cell) {
			cell = membershipParenRe.ReplaceAllString(cell, " ")
		}
		for _, id := range urMemberTokenRe.FindAllString(cell, -1) {
			addUR(id)
		}
		if len(ids) > 0 {
			payload["user_requirement_external_ids"] = ids
		}
	}
	if scenarios := ParseScenarios(recordText, epic.ID); len(scenarios) > 0 {
		// REQ-CROSS-250: the evidence map's clause↔test edges ride each
		// scenario criterion as verification_refs.
		emap := ParseEvidenceMapRefs(recordText)
		for _, raw := range scenarios {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id, _ := item["external_id"].(string)
			if i := strings.LastIndex(id, "#"); i >= 0 {
				if refs := emap.refsBySCN[id[i+1:]]; len(refs) > 0 {
					list := make([]any, len(refs))
					for j, ref := range refs {
						list[j] = map[string]any{"kind": ref["kind"], "ref": ref["ref"]}
					}
					item["verification_refs"] = list
				}
			}
		}
		payload["scenarios"] = scenarios
	}
	// Presence rule (REQ-CROSS-023): folder-form epics ALWAYS carry the key —
	// present-empty archives all sync-owned artifacts server-side; file-form
	// epics never touch them.
	if folderForm(epic) {
		specs := make([]any, len(epic.Specs))
		for i, s := range epic.Specs {
			specs[i] = map[string]any{
				"external_id": s.Rel,
				"name":        s.Name,
				"position":    i + 1,
				"content_md":  capRunes(s.Content, specContentCap),
			}
		}
		payload["specs"] = specs
	}
	if line := approvalLineOf(recordText); line != "" {
		tag := approvalTagOf(line)
		payload["approval"] = map[string]any{
			"approved_at": tag[5:] + "T00:00:00.000000Z",
			"basis":       capAtWordBoundary(line, 300),
			"source_tag":  tag,
		}
	}
	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	return Op{Type: "upsert_epic", Payload: payload}
}

// hasEpicEvidence reports whether an epic records any loop evidence. An em-dash
// or a blank cell is absence, not a status, so neither counts.
func hasEpicEvidence(e Epic) bool {
	for _, cell := range []string{e.Upper, e.Lower} {
		t := strings.TrimSpace(cell)
		t = strings.Trim(t, "—-– ")
		if t != "" {
			return true
		}
	}
	return false
}
