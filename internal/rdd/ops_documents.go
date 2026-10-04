package rdd

import ()

// BuildDocumentOp carries one explanatory document or discovery guide. Identity
// is the workspace-relative path; the hash is over the content, so an unchanged
// document is an unchanged op and costs nothing on re-sync.
func BuildDocumentOp(d Document) Op {
	payload := map[string]any{
		"external_id":   d.Path,
		"name":          d.Name,
		"document_type": d.Type,
		"provenance":    d.Provenance,
		"content_md":    d.Content,
	}
	payload["content_hash"] = ContentHash(payload)
	return Op{Type: "upsert_document", Payload: payload}
}
