package cmd

// Rewriting an agent's settings file is a merge into a file the project owns.
// Two defects made every `hooks install` produce a diff the owner did not ask
// for, and one of them dropped a project-owned hook (RUN:2026-09-12, two
// workspaces): encoding/json HTML-escaped `>` and `&` inside every hook
// command, so a byte-stable file was rewritten with > on each install;
// and replacing an owned entry by marker dropped the whole entry, including a
// second, project-owned command the owner had placed in the same matcher
// group. The helpers here keep the project's bytes: top-level key order as
// found, no HTML escaping, and foreign commands salvaged into the replacement
// entry.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// writeSettingsFile marshals settings without HTML escaping, two-space
// indented, keeping the top-level key order of the file it replaces; keys that
// are new since then follow in sorted order. Nested objects are marshaled by
// encoding/json and so come out with sorted keys, which is stable across
// installs.
func writeSettingsFile(path string, settings map[string]interface{}) error {
	var order []string
	if existing, err := os.ReadFile(path); err == nil {
		order = topLevelKeyOrder(existing)
	}
	seen := map[string]bool{}
	var keys []string
	for _, k := range order {
		if _, ok := settings[k]; ok && !seen[k] {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	var rest []string
	for k := range settings {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	keys = append(keys, rest...)

	var out bytes.Buffer
	out.WriteString("{\n")
	for i, k := range keys {
		keyJSON, err := marshalNoEscape(k, "")
		if err != nil {
			return err
		}
		valueJSON, err := marshalNoEscape(settings[k], "  ")
		if err != nil {
			return err
		}
		fmt.Fprintf(&out, "  %s: %s", keyJSON, valueJSON)
		if i < len(keys)-1 {
			out.WriteString(",")
		}
		out.WriteString("\n")
	}
	out.WriteString("}\n")
	return os.WriteFile(path, out.Bytes(), 0o644)
}

// marshalNoEscape encodes v with two-space indentation, every line after the
// first prefixed so the value nests under a key at that depth, and with `<`,
// `>` and `&` left alone — they are shell syntax in hook commands, and the
// escaped form is a diff on every install.
func marshalNoEscape(v interface{}, prefix string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent(prefix, "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// topLevelKeyOrder reads the keys of a JSON object in file order. A file that
// is not an object yields no order, and the caller falls back to sorted keys.
func topLevelKeyOrder(data []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil
	}
	var order []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return order
		}
		key, ok := tok.(string)
		if !ok {
			return order
		}
		order = append(order, key)
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return order
		}
	}
	return order
}

// replaceOwnedEntry drops every entry in existing whose JSON mentions one of
// the markers, salvages any command inside a dropped entry that mentions none
// of them into the replacement entry's hooks, and appends the replacement. An
// owner who put a second command beside the tool's in the same matcher group
// keeps it across reinstalls instead of losing it silently.
func replaceOwnedEntry(existing []interface{}, entry map[string]interface{}, markers ...string) []interface{} {
	var kept []interface{}
	var salvaged []interface{}
	for _, e := range existing {
		raw, _ := json.Marshal(e)
		if !matchesAnyMarker(string(raw), markers) {
			kept = append(kept, e)
			continue
		}
		obj, _ := e.(map[string]interface{})
		cmds, _ := obj["hooks"].([]interface{})
		for _, c := range cmds {
			cr, _ := json.Marshal(c)
			if !matchesAnyMarker(string(cr), markers) {
				salvaged = append(salvaged, c)
			}
		}
	}
	if len(salvaged) > 0 {
		own, _ := entry["hooks"].([]interface{})
		entry["hooks"] = append(own, salvaged...)
	}
	return append(kept, interface{}(entry))
}
