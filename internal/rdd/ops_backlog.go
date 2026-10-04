package rdd

import ()

// BuildBacklogOp serializes one BACKLOG.md discovery row or gap-register row
// (REQ-CROSS-223 §223.2: they gain a store home for the ledger import).
func BuildBacklogOp(b BacklogRow) Op {
	payload := map[string]any{
		"external_id": b.ExternalID,
		"kind":        b.Kind,
		"title":       b.Title,
		"source_path": b.SourcePath,
	}
	if b.NotesMD != "" {
		payload["notes_md"] = b.NotesMD
	}
	if b.Route != "" {
		payload["route"] = b.Route
	}
	// REQ-CROSS-251: the typed fields, present-only so absence never clears
	for key, value := range map[string]string{
		"raised_at":       b.RaisedAt,
		"raised_by":       b.RaisedBy,
		"disposition":     b.Disposition,
		"disposition_ref": b.DispositionRef,
		"candidate_route": b.CandidateRoute,
		"why_unrouted":    b.WhyUnrouted,
		"gap_kind":        b.GapKind,
		"raw_body":        b.RawBody,
	} {
		if value != "" {
			payload[key] = value
		}
	}
	if len(b.AffectedIDs) > 0 {
		ids := make([]any, len(b.AffectedIDs))
		for i, id := range b.AffectedIDs {
			ids[i] = id
		}
		payload["affected_external_ids"] = ids
	}
	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	return Op{Type: "upsert_backlog_record", Payload: payload}
}
