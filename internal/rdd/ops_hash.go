package rdd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------- canonical hash

// canonical mirrors ops.js canonical(): arrays in order, object keys sorted,
// scalars JSON-encoded the way JS JSON.stringify does.
func canonical(v any) string {
	switch val := v.(type) {
	case nil:
		return "null"
	case string:
		return jsString(val)
	case int:
		return strconv.Itoa(val)
	case float64:
		// JS JSON.stringify prints the shortest round-trip decimal; Go's 'g'
		// with -1 precision is the same algorithm for the plain range. The
		// only float the builders emit is a 0–100 percentage rounded to one
		// decimal (SR-CMP-9022), so exponent forms never arise.
		return strconv.FormatFloat(val, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(val)
	case []any:
		parts := make([]string, len(val))
		for i, item := range val {
			parts[i] = canonical(item)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case []string:
		parts := make([]string, len(val))
		for i, item := range val {
			parts[i] = canonical(item)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case []map[string]any:
		parts := make([]string, len(val))
		for i, item := range val {
			parts[i] = canonical(item)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = jsString(k) + ":" + canonical(val[k])
		}
		return "{" + strings.Join(parts, ",") + "}"
	default:
		panic(fmt.Sprintf("canonical: unsupported type %T", v))
	}
}

// jsString = JSON.stringify(s): escape `"` `\` and control chars only
// (no HTML escaping, no   handling — matching V8 for BMP text).
func jsString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(fmt.Sprintf(`\u%04x`, r))
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// ContentHash = ops.js contentHash: sha256 hex over the canonical form.
func ContentHash(payload map[string]any) string {
	sum := sha256.Sum256([]byte(canonical(payload)))
	return hex.EncodeToString(sum[:])
}
