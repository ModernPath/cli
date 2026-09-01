package rdd

// REQ-CROSS-253 (EPIC-CLI-003 T13): the retired-file byte archive. One
// upsert_process_record op per retired file — the exact bytes, base64 on the
// JSON wire, sha256 identity, unsplit — replacing the split-document
// workaround. Explanatory documents keep riding as documents; this op family
// exists for the files the flip retires, whose preservation must be
// byte-exact rather than searchable.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// BuildProcessRecordOp archives one file's exact bytes.
func BuildProcessRecordOp(path, recordType, content, revision string) Op {
	sum := sha256.Sum256([]byte(content))
	payload := map[string]any{
		"external_id":    path,
		"record_type":    recordType,
		"source_path":    path,
		"media_type":     "text/markdown",
		"raw_content":    base64.StdEncoding.EncodeToString([]byte(content)),
		"content_sha256": hex.EncodeToString(sum[:]),
		"size_bytes":     len(content),
	}
	if revision != "" {
		payload["archive_revision"] = revision
	}
	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	return Op{Type: "upsert_process_record", Payload: payload}
}
