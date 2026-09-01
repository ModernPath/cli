package rdd

// REQ-CROSS-221, relation arm — the edges the batch claims and never defines.
//
// Every other instrument compares content: a field the payload carries, a file
// a carrier holds, a row an op reaches. This one compares REFERENCES. An epic
// op names its member requirements by external id and a requirement op names
// its parent user requirement the same way, and the server resolves each id
// against what the store holds. An id it cannot resolve is a no-op — silently
// for epic membership (Core.Sync's link_requirements skips it, and its own
// comment defers the counting to this report), and for a parent to a log line
// that reaches no HTTP response.
//
// So the claim arrives as nothing while every count still reconciles: the epic
// is present, the requirements that exist are present, and the edge that was
// asserted is visible in no number on either side. That is the silent-loss
// shape exactly, which is why these entries are lost/blocking rather than a
// note — a relation the corpus states and the store never receives is content
// the import did not carry.
//
// Keyed by the TARGET, never by the claimant. The residue is one fact — "this
// id reaches no op" — and one accept key states it however many epics or rows
// point at it. Per-claimant keys would grow the accept file a line per
// referencing row while repeating one fact, and the parent arm's targets are
// the same user requirements the referenced-UR arm already names, so its
// entries merge into those instead of doubling them.

import (
	"fmt"
	"sort"
	"strings"
)

const (
	unresolvedMembershipGroup = "epic requirement membership (ids the epic op names → requirement ops the batch emits)"
	unresolvedParentGroup     = "requirement parent references (ids a requirement op names → user-requirement ops the batch emits)"
	unresolvedEpicURGroup     = "epic user-requirement membership (ids the epic op names → user-requirement ops the batch emits)"
)

// scanUnresolvedRelations diffs both reference populations against the ops the
// same batch emits. Resolution is measured against the BATCH, which is what the
// import controls: a store-born row that happens to satisfy a reference is
// reported by the run's own store-only arm, and counting it here would let the
// corpus's claim read as carried on a fresh system and lost on a populated one.
func (r *FidelityReport) scanUnresolvedRelations(epicPayload, reqPayload map[string]map[string]any, sysReqOps, userReqOps map[string]bool) {
	membership := FidelityCount{Group: unresolvedMembershipGroup}
	claimedBy := map[string][]string{}
	for _, epicID := range sortedPayloadIDs(epicPayload) {
		for _, id := range payloadIDList(epicPayload[epicID], "requirement_external_ids") {
			membership.Rows++
			if sysReqOps[id] {
				membership.Ops++
				continue
			}
			claimedBy[id] = append(claimedBy[id], epicID)
			membership.MissingFromOps = append(membership.MissingFromOps, epicID+" declares "+id)
		}
	}
	sort.Strings(membership.MissingFromOps)
	r.Counts = append(r.Counts, membership)
	for _, id := range sortedKeysOf(claimedBy) {
		r.Losses = append(r.Losses, FidelityLoss{
			RecordID: id, Field: "epic-membership", Category: LossLost,
			Detail: fmt.Sprintf("named in the requirement set of %s and defined by no requirement op — the server resolves membership by external id and skips what it cannot find, so the claim reaches the store as nothing and the epic keeps no record of it",
				namedFew(claimedBy[id], 8)),
		})
	}

	// The parent arm shares its key with the referenced-UR arm above, so the
	// entries it would duplicate are the ones already stated. A target named
	// twice would need two accept keys for one fact.
	stated := map[string]bool{}
	for _, l := range r.Losses {
		stated[l.Key()] = true
	}
	parents := FidelityCount{Group: unresolvedParentGroup}
	citedBy := map[string][]string{}
	for _, reqID := range sortedPayloadIDs(reqPayload) {
		for _, parent := range parentRefsOf(reqPayload[reqID]) {
			parents.Rows++
			if userReqOps[parent] {
				parents.Ops++
				continue
			}
			citedBy[parent] = append(citedBy[parent], reqID)
			parents.MissingFromOps = append(parents.MissingFromOps, reqID+" parents on "+parent)
		}
	}
	sort.Strings(parents.MissingFromOps)
	r.Counts = append(r.Counts, parents)
	for _, id := range sortedKeysOf(citedBy) {
		if stated[id+"|ur-record"] {
			continue
		}
		r.Losses = append(r.Losses, FidelityLoss{
			RecordID: id, Field: "ur-record", Category: LossLost,
			Detail: fmt.Sprintf("named as the parent of %s and defined by no user-requirement op — the derives edge is never written, and the server records the unresolved id only in its own log",
				namedFew(citedBy[id], 8)),
		})
	}

	// The epic→UR arm. Membership by external id resolves exactly as the
	// requirement arm above does, so an id the batch never defines reaches the
	// store as nothing. It shares the `ur-record` key with the parent arm —
	// one unresolvable user requirement is one fact, however many epics claim
	// it and by whichever edge.
	for _, l := range r.Losses {
		stated[l.Key()] = true
	}
	epicURs := FidelityCount{Group: unresolvedEpicURGroup}
	claimedURBy := map[string][]string{}
	for _, epicID := range sortedPayloadIDs(epicPayload) {
		for _, id := range payloadIDList(epicPayload[epicID], "user_requirement_external_ids") {
			epicURs.Rows++
			if userReqOps[id] {
				epicURs.Ops++
				continue
			}
			claimedURBy[id] = append(claimedURBy[id], epicID)
			epicURs.MissingFromOps = append(epicURs.MissingFromOps, epicID+" declares "+id)
		}
	}
	sort.Strings(epicURs.MissingFromOps)
	r.Counts = append(r.Counts, epicURs)
	for _, id := range sortedKeysOf(claimedURBy) {
		if stated[id+"|ur-record"] {
			continue
		}
		r.Losses = append(r.Losses, FidelityLoss{
			RecordID: id, Field: "ur-record", Category: LossLost,
			Detail: fmt.Sprintf("named in the user-requirement set of %s and defined by no user-requirement op — the server resolves membership by external id and skips what it cannot find, so the epic keeps no record of the edge",
				namedFew(claimedURBy[id], 8)),
		})
	}
}

// parentRefsOf reads a requirement payload's parent claims in both fields the
// builder writes — the singular first parent and the full array — deduplicated
// in the order they were sent. Reading only the array would miss a payload
// shape that carries one and not the other, and the point of the arm is what
// the wire actually says.
func parentRefsOf(p map[string]any) []string {
	var out []string
	seen := map[string]bool{}
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	if one, _ := p["parent_external_id"].(string); one != "" {
		add(one)
	}
	switch ids := p["parent_external_ids"].(type) {
	case []string:
		for _, id := range ids {
			add(id)
		}
	case []any:
		for _, raw := range ids {
			id, _ := raw.(string)
			add(id)
		}
	}
	return out
}

// payloadIDList reads a list-of-ids payload field in either shape a builder may
// have left it in ([]string before JSON, []any after).
func payloadIDList(p map[string]any, field string) []string {
	var out []string
	switch ids := p[field].(type) {
	case []string:
		out = append(out, ids...)
	case []any:
		for _, raw := range ids {
			if id, _ := raw.(string); id != "" {
				out = append(out, id)
			}
		}
	}
	return out
}

func sortedPayloadIDs(payloads map[string]map[string]any) []string {
	out := make([]string, 0, len(payloads))
	for id := range payloads {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func sortedKeysOf(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// namedFew renders a claimant list, capped — a detail that names forty rows is
// a detail nobody reads, and the count group itemizes every one of them.
func namedFew(items []string, limit int) string {
	if len(items) <= limit {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:limit], ", ") + fmt.Sprintf(", … %d more", len(items)-limit)
}
