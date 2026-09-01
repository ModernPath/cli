package rdd

// REQ-CROSS-248 (EPIC-CLI-003 T11): recorded epic acceptances import
// attributable. Two corpus carriers feed this: the record's approval-register
// tables (| Approval id | Approver | Role | Source | Scope | Decision |) and
// the WORKLIST row's Human-approval cell. Every non-pending acceptance lands
// as its own answered gate — no last-wins — and exactly one completion
// acceptance feeds the epic's single approval quartet, in this precedence:
// the record's own granted line (the pre-existing carrier), then the
// register's completion row, then the WORKLIST cell.

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	approvalRowIDRe    = regexp.MustCompile(`^(?:APP|APPROVE)-[A-Z0-9-]+$`)
	pendingDecisionRe  = regexp.MustCompile(`(?i)pending|^\s*[—–-]?\s*$`)
	specDecisionRe     = regexp.MustCompile(`(?i)specification|spec\b`)
	// A cell that says the COMPLETION decision has not been made yet. The bare
	// word will not do here: pendingDecisionRe reads a register table's
	// Decision cell, where "pending" is the whole content, but a WORKLIST cell
	// quotes the deciding human, and those sentences carry the word while
	// meaning the opposite. Both of these are acceptances a bare-token match
	// would invert:
	//
	//   APPROVE-EPIC-SYNC-011  approved `USER:2026-08-13` ("accept now all pending …")
	//   APPROVE-EPIC-DIG-004   approved `USER:2026-08-13` ("… all pending …")
	//
	// Only the qualifier form states that completion itself is outstanding.
	completionOutstandingRe = regexp.MustCompile(
		`(?i)\b(?:completion|delivery|acceptance|approval)\s+(?:approval\s+)?(?:is\s+)?pending\b` +
			`|\bpending\s+(?:completion|delivery|acceptance)\b`)
	scopeTokenRe       = regexp.MustCompile(`[A-Z][A-Z0-9]*-[A-Z0-9][A-Za-z0-9.-]*`)
	approverTagRe      = regexp.MustCompile(`(?i)\s*\(\s*(?:USER|RUN):[^)]*\)\s*$`)
	worklistApproverRe = regexp.MustCompile(`(?i)\bapproved by\s+([^:\n]+)\s*:`)
)

type approvalDecl struct {
	id       string
	approver string
	role     string
	source   string
	scope    string
	decision string
}

// parseApprovalRegisters reads every register row anywhere in the record: a
// table row whose id cell is APP-*/APPROVE-*, columns mapped by header name.
func parseApprovalRegisters(recordText string) []approvalDecl {
	var out []approvalDecl
	var header []string
	lines := strings.Split(recordText, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := decisionCells(trimmed)
		if len(cells) == 0 || decisionSeparatorRow(cells) {
			continue
		}
		first := strip(cells[0])
		if !approvalRowIDRe.MatchString(first) {
			// Only a row followed by a Markdown divider is a header. A
			// SPEC-APPROVE-* data row belongs to the sibling spec builder and
			// must not erase the column map for a following completion row.
			if i+1 < len(lines) {
				next := strings.TrimSpace(lines[i+1])
				if strings.HasPrefix(next, "|") && decisionSeparatorRow(decisionCells(next)) {
					header = lowerCells(cells)
				}
			}
			continue
		}
		d := approvalDecl{id: first}
		approverCell := strip(cellByName(header, cells, 1, "approver", "approved by", "decider"))
		d.approver = strings.TrimSpace(approverTagRe.ReplaceAllString(approverCell, ""))
		roleCell := strip(cellByName(header, cells, 2, "role"))
		d.role = roleCell
		d.source = cellByName(header, cells, 3, "source", "sources", "basis")
		d.scope = cellByName(header, cells, 4, "scope")
		d.decision = strip(cellByName(header, cells, 5, "decision", "state", "status"))
		// Compact tables put the USER tag beside the human or the decision
		// instead of dedicating a Source column. Preserve the authored carrier
		// as the source rather than losing its attribution.
		if !userTagRe.MatchString(d.source) {
			for _, carrier := range []string{approverCell, roleCell, d.decision} {
				if !userTagRe.MatchString(carrier) {
					continue
				}
				if strings.TrimSpace(d.source) == "" {
					d.source = carrier
				} else {
					d.source += " · " + carrier
				}
				break
			}
		}
		out = append(out, d)
	}
	return out
}

// BuildAcceptanceGateOps emits one answered gate per non-pending register row
// and returns the completion row's quartet contribution, if any.
func BuildAcceptanceGateOps(epic Epic, recordText string) ([]Op, map[string]any) {
	originRef := epic.Record
	if originRef == "" {
		originRef = epic.ID
	}
	var ops []Op
	var quartet map[string]any
	for _, d := range parseApprovalRegisters(recordText) {
		if pendingDecisionRe.MatchString(d.decision) {
			continue // a pending row asserts nothing
		}

		var body strings.Builder
		fmt.Fprintf(&body, "Approval — %s", d.decision)
		if d.approver != "" {
			fmt.Fprintf(&body, "\n\nApprover — %s", d.approver)
			if d.role != "" {
				fmt.Fprintf(&body, " (%s)", d.role)
			}
		}
		if d.scope != "" {
			fmt.Fprintf(&body, "\n\nScope — %s", d.scope)
		}
		if d.source != "" {
			fmt.Fprintf(&body, "\n\nSource — %s", d.source)
		}
		fmt.Fprintf(&body, "\n\nRecorded as %s in %s.", d.id, originRef)

		payload := map[string]any{
			"external_id":   d.id,
			"kind":          "approval_request",
			"title":         capRunes(fmt.Sprintf("%s — %s", epic.ID, d.decision), 200),
			"body_md":       capRunes(body.String(), 6000),
			"origin":        "workspace",
			"origin_ref":    originRef,
			"state":         "answered",
			"answer":        capRunes(d.decision, 1200),
			"applied_state": "applied",
		}
		stampAcceptanceDates(payload, d.source, originRef)

		sources := resolveDecisionSources(d.source, nil)
		if d.approver != "" {
			label := d.approver
			if d.role != "" {
				label += " (" + d.role + ")"
			}
			sources = append(sources, map[string]any{"kind": "note", "ref": "Approver — " + label})
		}
		payload["sources"] = sources

		scope := []any{epic.ID}
		for _, tok := range scopeTokenRe.FindAllString(d.scope, -1) {
			if tok != epic.ID {
				scope = append(scope, tok)
			}
		}
		payload["exact_scope"] = scope

		payload["content_hash"] = ContentHash(payload)
		payload["actor"] = actor
		answerer := map[string]any{"kind": "human"}
		if d.approver != "" {
			answerer["name"] = d.approver
		}
		payload["answerer"] = answerer
		ops = append(ops, Op{Type: "upsert_gate", Payload: payload})

		// The latest completion acceptance — approved, not a specification
		// round, and USER-dated — feeds the quartet. Equal dates keep the
		// first row deterministically. An undated row cannot fill approved_at,
		// so its acceptance lives in the gate alone.
		if !specDecisionRe.MatchString(d.id+" "+d.scope+" "+d.decision) &&
			strings.Contains(strings.ToLower(d.decision), "approved") &&
			userTagRe.MatchString(d.source) {
			candidate := quartetOf(d.approver, d.source, d.decision)
			candidateTag, _ := candidate["source_tag"].(string)
			currentTag, _ := quartet["source_tag"].(string)
			if quartet == nil || candidateTag > currentTag {
				quartet = candidate
			}
		}
	}
	return ops, quartet
}

// BuildWorklistAcceptanceGate carries a Human-approval cell that no other
// acceptance record states — the only recorded acceptance for most DONE
// rollup rows.
func BuildWorklistAcceptanceGate(epic Epic) (Op, map[string]any, bool) {
	cell := strings.TrimSpace(epic.ApprovalCell)
	tag := approvalTagOf(cell)
	if tag == "" {
		return Op{}, nil, false
	}
	approver := worklistApproverOf(cell)
	// APPROVE-<epic> IS the completion-acceptance gate, so `answered` on it
	// asserts that a human accepted completion. When the cell records only a
	// specification approval and says completion is still outstanding, the
	// gate is born open instead: the decision is genuinely awaited, and the
	// cell's own words stay in the body — a guard flags, it does not delete.
	outstanding := completionOutstandingRe.MatchString(cell)
	payload := map[string]any{
		"external_id":   "APPROVE-" + epic.ID,
		"kind":          "approval_request",
		"title":         capRunes(epic.ID+" — recorded acceptance", 200),
		"body_md":       capRunes(fmt.Sprintf("Human approval — %s\n\nRecorded in WORKLIST.md.", cell), 6000),
		"origin":        "workspace",
		"origin_ref":    "WORKLIST.md",
		"state":         "answered",
		"answer":        capRunes(cell, 1200),
		"applied_state": "applied",
	}
	if outstanding {
		payload["state"] = "open"
		delete(payload, "answer")
		delete(payload, "applied_state")
	}
	stampAcceptanceDates(payload, cell, "WORKLIST.md")
	if outstanding {
		// The tag dates the specification approval, not an acceptance of
		// completion; an open gate has no answer date.
		delete(payload, "answered_at")
	}
	sources := []any{map[string]any{"kind": "user", "ref": tag}}
	if approver != "" {
		sources = append(sources, map[string]any{"kind": "note", "ref": "Approver — " + approver})
	}
	payload["sources"] = sources
	payload["exact_scope"] = []any{epic.ID}
	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	if !outstanding {
		answerer := map[string]any{"kind": "human"}
		if approver != "" {
			answerer["name"] = approver
		}
		payload["answerer"] = answerer
	}
	var quartet map[string]any
	if !specDecisionRe.MatchString(cell) && !outstanding {
		quartet = quartetOf(approver, cell, cell)
	}
	return Op{Type: "upsert_gate", Payload: payload}, quartet, true
}

func worklistApproverOf(cell string) string {
	m := worklistApproverRe.FindStringSubmatch(strip(cell))
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func stampAcceptanceDates(payload map[string]any, tagSpace, docRef string) {
	if tag := approvalTagOf(tagSpace); tag != "" {
		payload["opened_at"] = tag[5:] + "T00:00:00.000000Z"
		payload["answered_at"] = tag[5:] + "T00:00:00.000000Z"
		payload["source_tag"] = tag
	} else {
		payload["opened_at"] = "2026-07-01T00:00:00.000000Z"
		payload["answered_at"] = "2026-07-01T00:00:00.000000Z"
		payload["source_tag"] = "DOC:" + docRef
	}
}

func quartetOf(approver, source, basis string) map[string]any {
	q := map[string]any{"basis": capAtWordBoundary(strip(basis), 300)}
	if tag := approvalTagOf(source); tag != "" {
		q["approved_at"] = tag[5:] + "T00:00:00.000000Z"
		q["source_tag"] = tag
	}
	if approver != "" {
		q["approver_name"] = approver
	}
	return q
}

// applyEpicApproval fills the epic op's approval quartet when the record's
// own granted line supplied none — never overwriting, only adding the
// approver name to an existing quartet when it lacks one. Re-hashes, since
// the approval is content.
func applyEpicApproval(op Op, quartet map[string]any) Op {
	if quartet == nil {
		return op
	}
	if existing, ok := op.Payload["approval"].(map[string]any); ok {
		if _, has := existing["approver_name"]; !has {
			if name, ok := quartet["approver_name"]; ok && quartet["source_tag"] == existing["source_tag"] {
				existing["approver_name"] = name
				return rehashOp(op)
			}
		}
		return op
	}
	op.Payload["approval"] = quartet
	return rehashOp(op)
}

func rehashOp(op Op) Op {
	a := op.Payload["actor"]
	delete(op.Payload, "content_hash")
	delete(op.Payload, "actor")
	op.Payload["content_hash"] = ContentHash(op.Payload)
	op.Payload["actor"] = a
	return op
}

// cellReferencesGated reports whether the WORKLIST cell merely points at a
// register row that already produced a gate — a reference, not a second fact.
func cellReferencesGated(cell string, accOps []Op) bool {
	for _, op := range accOps {
		if id, _ := op.Payload["external_id"].(string); id != "" && strings.Contains(cell, id) {
			return true
		}
	}
	return false
}
