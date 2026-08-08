package rdd

// SCN-SY-033 upper-loop evidence — live parity: the bundled parsers must
// produce the same op set (external ids, op types, content hashes) as the
// node extractor on a real workspace. Runs only when pointed at one:
//
//	MP_PARITY_ROOT=/path/to/workspace go test ./internal/rdd -run Parity -v
//
// The workspace must carry mission-control/cli/ops-dump.js (the node side).

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/modernpath/cli/internal/manifest"
)

func TestLiveParityWithNodeExtractor(t *testing.T) {
	root := os.Getenv("MP_PARITY_ROOT")
	if root == "" {
		t.Skip("set MP_PARITY_ROOT to a workspace to run the live parity check")
	}

	nodeOut, err := exec.Command("node", filepath.Join(root, "mission-control/cli/ops-dump.js")).Output()
	if err != nil {
		t.Fatalf("node ops-dump failed: %v", err)
	}
	var nodeDump struct {
		Ops []struct {
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		} `json:"ops"`
	}
	if err := json.Unmarshal(nodeOut, &nodeDump); err != nil {
		t.Fatal(err)
	}

	m := manifest.Default()
	data, warnings := Snapshot(root, m)
	for _, w := range warnings {
		t.Logf("warning: %s", w)
	}
	goOps := BuildOps(data, func(rel string) string { return ReadEpicRecord(root, rel) },
		time.Now().UTC().Format("2006-01-02"))

	key := func(opType string, payload map[string]any) string {
		id, _ := payload["external_id"].(string)
		return opType + " " + id
	}
	nodeByKey := map[string]map[string]any{}
	for _, op := range nodeDump.Ops {
		nodeByKey[key(op.Type, op.Payload)] = op.Payload
	}
	goByKey := map[string]map[string]any{}
	for _, op := range goOps {
		goByKey[key(op.Type, op.Payload)] = op.Payload
	}

	mismatches := 0
	for k, nodePayload := range nodeByKey {
		goPayload, ok := goByKey[k]
		if !ok {
			t.Errorf("node op missing from Go set: %s", k)
			mismatches++
			continue
		}
		nodeHash, _ := nodePayload["content_hash"].(string)
		goHash, _ := goPayload["content_hash"].(string)
		if nodeHash != goHash && mismatches < 8 {
			detail := ""
			for field, nodeVal := range nodePayload {
				if field == "content_hash" || field == "actor" {
					continue
				}
				nodeC := canonical(normalizeJSON(nodeVal))
				goC := canonical(normalizeJSON(goPayload[field]))
				if nodeC != goC {
					at := 0
					for at < len(nodeC) && at < len(goC) && nodeC[at] == goC[at] {
						at++
					}
					lo, hi := at-60, at+80
					if lo < 0 {
						lo = 0
					}
					clip := func(s string) string {
						h := hi
						if h > len(s) {
							h = len(s)
						}
						if lo >= h {
							return ""
						}
						return s[lo:h]
					}
					detail += fmt.Sprintf("\n  field %q differs at byte %d:\n    node: …%s…\n    go:   …%s…", field, at, clip(nodeC), clip(goC))
				}
			}
			for field := range goPayload {
				if _, ok := nodePayload[field]; !ok && field != "content_hash" && field != "actor" {
					detail += fmt.Sprintf("\n  field %q only in go", field)
				}
			}
			t.Errorf("hash mismatch %s:%s", k, detail)
			mismatches++
		}
	}
	for k := range goByKey {
		if _, ok := nodeByKey[k]; !ok {
			t.Errorf("Go op not in node set: %s", k)
			mismatches++
		}
	}
	fmt.Printf("parity: node=%d go=%d ops, %d mismatches\n", len(nodeByKey), len(goByKey), mismatches)
}

// normalizeJSON maps json.Unmarshal's float64 numbers into the int form the
// Go builder uses, so canonical() can compare both sides.
func normalizeJSON(v any) any {
	switch val := v.(type) {
	case float64:
		return int(val)
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = normalizeJSON(item)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, item := range val {
			out[k] = normalizeJSON(item)
		}
		return out
	default:
		return v
	}
}
