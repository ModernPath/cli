package cmd

import (
	"regexp"
	"strings"
)

// refPattern matches a work reference in the external-id grammar,
// case-insensitively: REQ-<CTX>-NNN or EPIC-<AREA>-NNN. `[A-Z0-9]+` stops at
// the first '-', so a trailing branch slug (…-005-focus-state) is not absorbed,
// and `\d+` does not expand a range (…-225..228 yields only …-225).
var refPattern = regexp.MustCompile(`(?i)(REQ|EPIC)-[A-Z0-9]+-\d+`)

// ExtractRefs returns every REQ-*/EPIC-* work reference in text, upper-cased,
// in first-appearance order, deduplicated (REQ-PLN-135 §135.1). The source text
// is the caller's to discard once its refs are out — nothing else is kept.
func ExtractRefs(text string) []string {
	var out []string
	seen := map[string]bool{}

	for _, match := range refPattern.FindAllString(text, -1) {
		ref := strings.ToUpper(match)
		if !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	return out
}
