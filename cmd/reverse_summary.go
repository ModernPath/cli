package cmd

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// A run's status and coverage are hundreds of kilobytes of JSON; an agent that
// reads them whole carries them into every later step. --summary prints the
// counts and only the rows an agent acts on, in a form that follows the
// server's fields instead of naming them, so a new count shows up unchanged.

const reverseSummaryUsage = "print counts and only the rows to act on, not the whole JSON"
const reverseLimitUsage = "with --summary, the most rows printed per list"

// reverseSummaryNoise are identifiers and digests no summary reader acts on.
func reverseSummaryNoise(key string) bool {
	switch key {
	case "tenant_id", "system_id", "run_id", "corpus_after", "snapshot_digest", "trace_ids", "document_snapshots":
		return true
	}
	return strings.HasSuffix(key, "_fingerprint")
}

// writeReverseSummary prints a run status or coverage response's data object.
func writeReverseSummary(w io.Writer, data map[string]any, limit int) {
	for _, key := range reverseSummaryKeys(data) {
		writeReverseSummaryValue(w, "", key, data[key], limit)
	}
}

// reverseSummaryKeys orders scalars first, then the rest, each alphabetically.
func reverseSummaryKeys(m map[string]any) []string {
	var keys []string
	for key := range m {
		if !reverseSummaryNoise(key) {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		si, sj := reverseScalar(m[keys[i]]), reverseScalar(m[keys[j]])
		if si != sj {
			return si
		}
		return keys[i] < keys[j]
	})
	return keys
}

func reverseScalar(value any) bool {
	switch value.(type) {
	case map[string]any, []any:
		return false
	}
	return true
}

func writeReverseSummaryValue(w io.Writer, indent, key string, value any, limit int) {
	switch v := value.(type) {
	case map[string]any:
		if reverseFlat(v) {
			fmt.Fprintf(w, "%s%s: %s\n", indent, key, reverseInline(v))
			return
		}
		fmt.Fprintf(w, "%s%s:\n", indent, key)
		for _, sub := range reverseSummaryKeys(v) {
			writeReverseSummaryValue(w, indent+"  ", sub, v[sub], limit)
		}
	case []any:
		writeReverseSummaryList(w, indent, key, v, limit)
	default:
		fmt.Fprintf(w, "%s%s: %v\n", indent, key, reverseScalarText(v))
	}
}

// reverseFlat is a map whose values print on one line: scalars, lists of
// scalars and maps of scalars. A list of objects gets its own rows.
func reverseFlat(m map[string]any) bool {
	for _, value := range m {
		switch v := value.(type) {
		case map[string]any:
			for _, inner := range v {
				if !reverseScalar(inner) {
					return false
				}
			}
		case []any:
			if len(v) > 0 && !reverseScalar(v[0]) {
				return false
			}
		}
	}
	return true
}

// reverseInline prints a map on one line; a list prints as its length.
func reverseInline(m map[string]any) string {
	var parts []string
	for _, key := range reverseSummaryKeys(m) {
		switch v := m[key].(type) {
		case map[string]any:
			parts = append(parts, key+"={"+reverseInline(v)+"}")
		case []any:
			parts = append(parts, fmt.Sprintf("%s=[%d]", key, len(v)))
		default:
			parts = append(parts, key+"="+reverseScalarText(v))
		}
	}
	return strings.Join(parts, " ")
}

func reverseScalarText(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%g", v)
	case string:
		return v
	}
	return fmt.Sprint(value)
}

func writeReverseSummaryList(w io.Writer, indent, key string, list []any, limit int) {
	if reverseFileRows(list) {
		writeReverseFileRows(w, indent, key, list, limit)
		return
	}
	if len(list) == 0 || reverseScalar(list[0]) {
		fmt.Fprintf(w, "%s%s: %d\n", indent, key, len(list))
		return
	}
	// The latest rows are the ones acted on, so a long list keeps its last ones,
	// in the order they were made whatever order the server sends.
	list = reverseChronological(list)
	fmt.Fprintf(w, "%s%s: %d\n", indent, key, len(list))
	start := 0
	if len(list) > limit {
		start = len(list) - limit
		fmt.Fprintf(w, "%s  … %d earlier\n", indent, start)
	}
	for _, item := range list[start:] {
		if m, ok := item.(map[string]any); ok {
			fmt.Fprintf(w, "%s  - %s\n", indent, reverseInline(m))
		}
	}
}

// reverseFileRows is a list of measured files: each row has a path and an
// assessment.
func reverseFileRows(list []any) bool {
	if len(list) == 0 {
		return false
	}
	row, ok := list[0].(map[string]any)
	if !ok {
		return false
	}
	_, path := row["path"]
	_, assessment := row["assessment"]
	return path && assessment
}

// writeReverseFileRows prints the count of files and, of them, the ones that
// keep a run from its finish condition: an unresolved assessment, a capture
// that is not ready, or no assessment and no governed link.
func writeReverseFileRows(w io.Writer, indent, key string, list []any, limit int) {
	var open []map[string]any
	for _, item := range list {
		row, _ := item.(map[string]any)
		assessment, _ := row["assessment"].(string)
		capture, _ := row["capture"].(string)
		governed, _ := row["governed"].(bool)
		if assessment == "unresolved" || (capture != "" && capture != "ready") || (assessment == "unassessed" && !governed) {
			open = append(open, row)
		}
	}
	fmt.Fprintf(w, "%s%s: %d; to act on: %d (unresolved, capture not ready, or unassessed with no governed link)\n", indent, key, len(list), len(open))
	for i, row := range open {
		if i == limit {
			fmt.Fprintf(w, "%s  … %d more: run without --summary for every file\n", indent, len(open)-limit)
			break
		}
		link := "unlinked"
		if governed, _ := row["governed"].(bool); governed {
			link = "governed"
		} else if candidate, _ := row["candidate"].(bool); candidate {
			link = "candidate"
		}
		fmt.Fprintf(w, "%s  %s %s %s %s:%s\n", indent, reverseScalarText(row["assessment"]), reverseScalarText(row["capture"]), link, reverseScalarText(row["repository_key"]), reverseScalarText(row["path"]))
	}
}

// reverseChronological orders rows by sequence, or else by inserted_at, when
// every row has one; otherwise it keeps the server's order.
func reverseChronological(list []any) []any {
	for _, field := range []string{"sequence", "inserted_at"} {
		ordered := true
		for _, item := range list {
			row, ok := item.(map[string]any)
			if !ok || row[field] == nil {
				ordered = false
				break
			}
		}
		if !ordered {
			continue
		}
		sorted := append([]any(nil), list...)
		sort.SliceStable(sorted, func(i, j int) bool {
			a, b := sorted[i].(map[string]any)[field], sorted[j].(map[string]any)[field]
			if x, ok := a.(float64); ok {
				y, _ := b.(float64)
				return x < y
			}
			return reverseScalarText(a) < reverseScalarText(b)
		})
		return sorted
	}
	return list
}
