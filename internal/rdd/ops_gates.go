package rdd

import (
	"fmt"
	"regexp"
	"strings"
)

// BuildRQGateOp — docs/85 decision requests → decision gates. Honesty rules
// as ops.js: answered only with a real source; closed-without-source dismissed.
func BuildRQGateOp(item RQ) Op {
	text := item.Heading + "\n" + item.Body
	userTag := userTagRe.FindString(text)
	closed := item.State == "closed"

	openedAt := "2026-07-01T00:00:00.000000Z"
	if item.Date != "" {
		openedAt = item.Date + "T00:00:00.000000Z"
	}
	originRef := "docs/85-loop-review-queue.md"
	if item.Line > 0 {
		originRef = fmt.Sprintf("%s:%d", originRef, item.Line)
	}
	title := item.Title
	if title == "" {
		title = item.ID
	}
	payload := map[string]any{
		"external_id": item.ID,
		"kind":        "decision",
		"title":       title,
		"body_md":     item.Body,
		"origin":      "workspace",
		"origin_ref":  originRef,
		"opened_at":   openedAt,
	}
	addGateOptions(payload, item.Options, item.Recommendation)
	addRichGateOptions(payload, item.Body)
	addGateBrief(payload, item.Body)

	switch {
	case closed && userTag != "":
		payload["state"] = "answered"
		// §245.3: the closure's own USER: date is the answer date; the
		// opened placeholder is not a fact about when a human decided.
		payload["answered_at"] = userTag[5:] + "T00:00:00.000000Z"
		payload["source_tag"] = userTag
		payload["answer"] = fmt.Sprintf("Resolved in the record — see %s in docs/85.", item.ID)
	case closed:
		payload["state"] = "dismissed"
	default:
		payload["state"] = "open"
	}
	addGateHolds(payload, item.Pool, "pooled behind "+item.ID)
	if item.EvaluatedScopeFingerprint != "" {
		payload["evaluated_scope_fingerprint"] = item.EvaluatedScopeFingerprint
	}

	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	if payload["state"] == "answered" {
		payload["answerer"] = map[string]any{"kind": "human"}
	}
	return Op{Type: "upsert_gate", Payload: payload}
}

// SR-MC-044 acceptance round 1, defect 3 (EPIC-MC-004): the flattened OQ body
// is capped at 1200 UTF-16 units at extraction, and on the live gate that cut
// the trailing `recommendation:` field to "recom…" — unrecoverable by any
// downstream parser. The heading-form OQ is the only gate flavor whose
// recommendation lives ONLY in the body (RQ/approval gates carry it as a
// first-class field), so the builder recomposes: the wire brief fields
// (why_now / changes_if_approved / risk_if_wrong / recommendation) move BEFORE
// the long technical detail, and the cap falls on the technical tail only,
// with an honest ellipsis. Bodies without a recommendation label keep the
// exact legacy composition — no hash churn where the defect cannot occur.
// Ported from the retired node builder's composeOqGateBody; the label
// vocabulary mirrors the frontend gateBrief.ts WIRE_LABEL.
const oqBodyCap = 1200

var oqWireLabelRe = regexp.MustCompile(
	`(?i)(?:(\*\*)?(why[ _]now|changes[ _]if[ _]approved|risk[ _]if[ _]wrong|recommendation|technical[ _]detail|affects)(\*\*)?\s*:|\*\*(technical[ _]detail)\*\*)`)

// utf16Len counts JS String.length units, the unit capRunes caps in.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// capAtWordBoundary is decisionTitle's honesty rule, generalized: n is a
// display choice over an unbounded text column (postgres `text`, no contract
// limit), so the cap stays, but a silent cut mid-word is not honest —
// measured, 9 of 157 stored approval bases sat at exactly 300 characters,
// indistinguishable from a basis that simply ended there. Cuts at a word
// boundary within the same proportion of n that decisionTitle uses for its
// own 200-unit cap (120, i.e. 3/5 of n), and marks the cut with "…"; the full
// text is never truncated elsewhere.
func capAtWordBoundary(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf16Len(s) <= n {
		return s
	}
	head := capRunes(s, n-1)
	if i := strings.LastIndexAny(head, " \t"); i > n*3/5 {
		head = head[:i]
	}
	return strings.TrimRight(head, " \t") + "…"
}

func composeOQGateBody(item OQ) string {
	if item.Raw == "" {
		return item.Full // table-form rows: the question cell, never long
	}
	flat := strings.Join(strings.Fields(item.Title+" "+item.Raw), " ")
	ms := oqWireLabelRe.FindAllStringSubmatchIndex(flat, -1)
	type seg struct {
		key  string
		text string
	}
	var segs []seg
	hasReco := false
	for i, m := range ms {
		key := "technical_detail" // the bare **Technical detail** alternative
		if m[4] >= 0 {
			key = strings.ToLower(strings.ReplaceAll(flat[m[4]:m[5]], " ", "_"))
		}
		end := len(flat)
		if i+1 < len(ms) {
			end = ms[i+1][0]
		}
		segs = append(segs, seg{key: key, text: strings.TrimSpace(flat[m[0]:end])})
		if key == "recommendation" {
			hasReco = true
		}
	}
	if !hasReco {
		return item.Full
	}
	briefKeys := []string{"why_now", "changes_if_approved", "risk_if_wrong", "recommendation"}
	head := []string{}
	if lead := strings.TrimSpace(flat[:ms[0][0]]); lead != "" {
		head = append(head, lead)
	}
	for _, want := range briefKeys {
		for _, s := range segs {
			if s.key == want {
				head = append(head, s.text)
			}
		}
	}
	var tail []string
	for _, s := range segs {
		isBrief := false
		for _, want := range briefKeys {
			if s.key == want {
				isBrief = true
			}
		}
		if !isBrief {
			tail = append(tail, s.text)
		}
	}
	headText := strings.Join(head, " ")
	if len(tail) == 0 {
		return headText
	}
	tailText := strings.Join(tail, " ")
	remaining := oqBodyCap - utf16Len(headText) - 1
	if remaining <= 0 {
		return headText + " …"
	}
	if utf16Len(tailText) <= remaining {
		return headText + " " + tailText
	}
	return headText + " " + capRunes(tailText, remaining) + "…"
}

// BuildOQGateOp — process/08 open questions → question gates.
func BuildOQGateOp(item OQ) Op {
	openedAt := "2026-07-01T00:00:00.000000Z"
	originRef := "process/08-open-questions.md"
	if item.Line > 0 {
		originRef = fmt.Sprintf("%s:%d", originRef, item.Line)
	}
	title := item.Title
	if title == "" {
		title = item.ID
	}
	payload := map[string]any{
		"external_id": item.ID,
		"kind":        "question",
		"title":       title,
		"body_md":     composeOQGateBody(item),
		"origin":      "workspace",
		"origin_ref":  originRef,
		"opened_at":   openedAt,
	}
	// REQ-PLN-059: the row's suggested default is the author's recommendation.
	// Only set when present — "recommendation attached" on a gate with none
	// would be a worse lie than showing nothing.
	if item.Suggested != "" {
		payload["recommendation"] = item.Suggested
	}
	if item.Resolved {
		userTag := userTagRe.FindString(item.Full)
		payload["state"] = "answered"
		// §245.3: date the answer from the resolution's own USER: tag; only
		// a tagless resolution keeps the opened placeholder.
		if userTag != "" {
			payload["answered_at"] = userTag[5:] + "T00:00:00.000000Z"
			payload["source_tag"] = userTag
		} else {
			payload["answered_at"] = openedAt
			payload["source_tag"] = "DOC:process/08-open-questions.md"
		}
		payload["answer"] = item.Full
	} else {
		payload["state"] = "open"
	}
	// REQ-CROSS-142: read the brief from Raw, not Full. Full is
	// strings.Join(strings.Fields(...), " "), which collapses every newline,
	// and briefLineRe is anchored at ^\s*[-*] — so a bullet list flattened onto
	// one line matches nothing. Raw exists for exactly this.
	addGateBrief(payload, item.Raw)
	addGateHolds(payload, item.Pool, "pooled behind "+item.ID)
	if item.EvaluatedScopeFingerprint != "" {
		payload["evaluated_scope_fingerprint"] = item.EvaluatedScopeFingerprint
	}

	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	if payload["state"] == "answered" {
		payload["answerer"] = map[string]any{"kind": "human"}
	}
	return Op{Type: "upsert_gate", Payload: payload}
}
