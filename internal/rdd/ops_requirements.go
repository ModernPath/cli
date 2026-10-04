package rdd

import (
	"regexp"
	"strings"
)

// REQ-CROSS-064: an as-built epic says so in its Specification status. Its
// requirements describe behaviour that ALREADY SHIPS, so they belong to the
// base and must never be stamped into a release — 800+ of them clogged the
// active release before this existed (`USER:2026-08-13`).
var specDerivedRe = regexp.MustCompile(`(?i)SPEC-DERIVED`)

func isAsBuiltEpic(recordText string) bool {
	m := specStatusSectionRe.FindStringSubmatch(recordText)
	if m == nil {
		return false
	}
	return specDerivedRe.MatchString(m[1])
}

func BuildRequirementOpWithExemption(req Req, exempt bool) Op {
	op := BuildRequirementOp(req)
	if !exempt {
		return op
	}
	// Re-hash: the exemption is content, not routing — a row that becomes
	// exempt must reach the server as a changed row.
	payload := op.Payload
	delete(payload, "content_hash")
	delete(payload, "actor")
	payload["release_exempt"] = true
	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	return Op{Type: "upsert_requirement", Payload: payload}
}

// urTokenRe is the shape a parent reference must have (§245.9). The UI
// ledger writes provenance prose in the UR column of as-built rows
// ("finding RUN:2026-08-12 · fixed RUN:2026-08-20"); reading those cells as
// ids minted parents no corpus record declares.
var urTokenRe = regexp.MustCompile(`\bUR-[A-Z][A-Z0-9]*-\d+[a-z]?\b`)

// urMemberTokenRe widens urTokenRe for the epic UR-MEMBERSHIP reader only. A
// membership cell legitimately names display-id members (UR-1-0, the fallback
// for a reverse-engineered UR) and two-segment ones (UR-44) that urTokenRe's
// letter-context, two-segment shape rejects. The required numeric tail, plus
// the caller stripping parentheticals first (as the SR-membership reader does),
// keeps provenance prose from minting phantom members. urTokenRe stays strict
// for firstURRef and the fidelity counters, which read prose-bearing cells.
var urMemberTokenRe = regexp.MustCompile(`\bUR-(?:[A-Za-z0-9]+-)*\d+[a-z]?\b`)

// firstURRef normalizes a UR cell or detail-line value to the single parent id
// the payload can carry: backticks off, whitespace trimmed, a ` · ` list
// folded to its first entry (SR-SY-1402), and only a UR-shaped token counts
// (§245.9) — decorations around it are dropped, prose yields nothing.
func firstURRef(raw string) string {
	ur := strings.TrimSpace(strings.ReplaceAll(raw, "`", ""))
	if i := strings.Index(ur, "·"); i >= 0 {
		ur = strings.TrimSpace(ur[:i])
	}
	return urTokenRe.FindString(ur)
}

func BuildRequirementOp(req Req) Op {
	// §245.2: a dash Stage cell is absence, exactly like Priority/Owner/Release.
	var stage any
	if s := dashless(req.Stage); s != "" {
		stage = s
	}
	payload := map[string]any{
		"external_id": req.ID,
		// The id's prefix decides this. Hardcoding "system" flattened every
		// user requirement into a system one on the way to the store — a UR
		// owns acceptance scenarios and upper evidence, an SR owns lower
		// evidence, and the server routes to a different table per kind.
		"kind":        RequirementKind(req.ID),
		"title":       req.Title,
		"context":     req.Ctx,
		"stage":       stage,
		"work_status": req.Status,
		"source_citations": append(
			ParseCitations(req.Source),
			evidenceCitations(req)...,
		),
		"criteria":    ParseCriteria(req.Detail, req.ID),
		"description": ParseDescription(req.Detail),
	}
	// The context's human name, when the ledger's H1 gives one. Omitted rather
	// than sent empty, so a workspace that never named its contexts sees no
	// content-hash churn (`USER:2026-08-12`).
	if req.CtxName != "" {
		payload["context_name"] = req.CtxName
	}
	// REQ-CROSS-049: the user requirement this row serves. Omitted rather than
	// sent null when the ledger has no UR column, so workspaces that never had
	// one see no content-hash churn.
	// An em-dash is how a ledger writes "no parent" — the same convention the
	// evidence columns use. Sending it as a literal id made 33 rows on a real
	// system report an unresolved parent that was never claimed (`RUN:2026-08-12`).
	// SR-SY-1402: a line naming several URs (`UR-A · UR-B`) folds to its FIRST —
	// parent_external_id is one edge; the Snapshot warns about the extras.
	if ur := firstURRef(req.UR); ur != "" {
		payload["parent_external_id"] = ur
	}

	// REQ-CROSS-222: full-fidelity capture for the one-time ledger import.
	// parent_external_id above stays the FIRST parent for consumers that read
	// one; the array is the FULL set whenever the row names a parent at all,
	// one included. Sending it only for multi-parent rows made absence
	// ambiguous — "no parent" and "exactly one parent" looked alike, so a
	// consumer reading the array alone silently lost 848 single-parent
	// relations. The server unions the two fields and dedups by ref, so the
	// repeated first parent writes one edge.
	if urs := allURRefs(req.UR); len(urs) > 0 {
		payload["parent_external_ids"] = urs
	}
	if req.Priority != "" {
		payload["priority"] = req.Priority
	}
	if req.Owner != "" {
		payload["owner"] = req.Owner
	}
	if req.Release != "" {
		payload["release_note"] = req.Release
	}
	// REQ-CROSS-222 §222.2 and §222.6 share one carrier: the ledger's
	// unrecognized columns first, then the two evidence cells verbatim.
	//
	// The cells need a raw home because splitEvidenceRefs answers a different
	// question — which references does this cell name? With a backtick present
	// it keeps only the backticked spans, so the counts, RED/GREEN notes,
	// review outcomes and scope qualifiers written beside a reference reached
	// no payload at all; with none it takes the cell whole but shortens it to
	// 200 runes. Both are right for a reference list and wrong for
	// preservation, so preservation is carried separately and the parsing is
	// left exactly as it was — additive, never a replacement.
	//
	// Named extras rather than columns of their own: the store already carries
	// this jsonb, casts it verbatim, and serves it back on the sync read
	// surface, so the raw text round-trips with no server change. Order is
	// content identity — a stable order keeps an unchanged row's hash stable.
	extras := make([]map[string]any, 0, len(req.Extras)+2)
	for _, e := range req.Extras {
		extras = append(extras, map[string]any{"name": e.Name, "value": e.Value})
	}
	for _, cell := range []struct{ name, text string }{
		{rawTestsCellExtra, req.Tests},
		{rawCodeCellExtra, req.Code},
	} {
		if v := dashless(cell.text); v != "" {
			extras = append(extras, map[string]any{"name": cell.name, "value": v})
		}
	}
	// §245.9: a UR cell saying more than the extracted token — provenance
	// prose, a decorated reference, a multi-parent list — is preserved
	// verbatim; a clean single token adds nothing.
	if raw := dashless(req.UR); raw != "" && raw != firstURRef(req.UR) {
		extras = append(extras, map[string]any{"name": rawURCellExtra, "value": raw})
	}
	if len(extras) > 0 {
		payload["extra_columns"] = extras
	}
	if req.Detail != "" {
		payload["detail_md"] = req.Detail
	}
	if s := strings.TrimSpace(req.Source); s != "" && s != "—" {
		payload["source_raw"] = s
	}
	// §222.4 as sharpened by §245.8: an OBSOLETE row names what superseded it
	// only when its source cell says so — the first requirement or epic id
	// AFTER an explicit "superseded by". A bare id mention is context, not a
	// supersession claim.
	if req.Status == "OBSOLETE" {
		if loc := supersededByRe.FindStringIndex(req.Source); loc != nil {
			if refs := idMentions(req.Source[loc[1]:], "REQ", "EPIC"); len(refs) > 0 {
				payload["superseding_ref"] = refs[0]
			}
		}
	}

	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor // after hashing — who synced is not content identity
	return Op{Type: "upsert_requirement", Payload: payload}
}

// allURRefs expands a UR cell's ` · `-separated list into every named parent
// (REQ-CROSS-222 §222.1: each one is a declared relation).
func allURRefs(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, "·") {
		// firstURRef returns only a UR-shaped token (§245.9), so dash
		// placeholders and prose fall out here by yielding nothing.
		if ur := firstURRef(part); ur != "" {
			out = append(out, ur)
		}
	}
	return out
}
