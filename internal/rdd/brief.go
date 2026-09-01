package rdd

// EPIC-DEC-001 (REQ-PLN-047, D-DEC-1 workspace-first): the **Brief:** block —
// a gate source's plain-language executive summary, authored at gate-open.
// Was mirrored byte-for-byte by a node op-builder in the workspace; that
// mirror is retired and this is now the only implementation.

import (
	"regexp"
	"strings"
)

var briefMarkerRe = regexp.MustCompile(`\*\*Brief:\*\*`)
var briefLineRe = regexp.MustCompile(`^\s*[-*]\s*(What|Why now|Changes if approved|Risk if wrong|Recommendation|Image)\s*:\s*(.+)$`)

var briefKeyMap = map[string]string{
	"What":                "what",
	"Why now":             "why_now",
	"Changes if approved": "changes_if_approved",
	"Risk if wrong":       "risk_if_wrong",
	"Recommendation":      "recommendation",
}

// parseBrief scans the text after the first **Brief:** marker for the labeled
// bullet lines. Returns (nil, "") when there is no block or the keystone
// "What" line is missing. The optional "Image" line becomes the second return.
func parseBrief(text string) (map[string]string, string) {
	loc := briefMarkerRe.FindStringIndex(text)
	if loc == nil {
		return nil, ""
	}

	brief := map[string]string{}
	image := ""
	started := false
	for _, line := range strings.Split(text[loc[1]:], "\n") {
		m := briefLineRe.FindStringSubmatch(line)
		if m == nil {
			if started && strings.TrimSpace(line) != "" {
				break // the block ends at the first non-bullet content line
			}
			if started && strings.TrimSpace(line) == "" {
				break
			}
			continue
		}
		started = true
		value := capRunes(strings.TrimSpace(m[2]), 2000)
		if m[1] == "Image" {
			image = capRunes(value, 500)
		} else {
			brief[briefKeyMap[m[1]]] = value
		}
	}

	if brief["what"] == "" {
		return nil, ""
	}
	return brief, image
}

// addGateBrief attaches brief keys to a gate payload ONLY when a brief exists —
// brief-less gates keep byte-identical payloads (hash stability).
func addGateBrief(payload map[string]any, sourceText string) {
	brief, image := parseBrief(sourceText)
	if brief == nil {
		return
	}
	briefAny := map[string]any{}
	for k, v := range brief {
		briefAny[k] = v
	}
	payload["brief"] = briefAny
	if image != "" {
		payload["brief_image_url"] = image
	}
}

// ── Rich options (REQ-PLN-048) ─────────────────────────────────────────────

var richOptionHeadRe = regexp.MustCompile(`\*\*Option\s+\(([0-9a-zA-Z]+)\)\s+—\s+(.+?):\*\*`)
var richOptionLineRe = regexp.MustCompile(`^\s*[-*]\s*(Outcome|Pros|Cons|Reversibility|Recommended)\s*:\s*(.+)$`)

// parseRichOptions scans **Option (n) — Label:** blocks. Each yields
// {key,label,body,outcome,pros,cons,reversibility,recommended,
// recommendation_rationale}; exactly one option may carry recommended — the
// first wins, later flags are dropped (REQ-PLN-048). Returns nil when no
// blocks exist.
func parseRichOptions(text string) []map[string]any {
	heads := richOptionHeadRe.FindAllStringSubmatchIndex(text, -1)
	if len(heads) == 0 {
		return nil
	}

	var opts []map[string]any
	recommendedSeen := false
	for i, h := range heads {
		end := len(text)
		if i+1 < len(heads) {
			end = heads[i+1][0]
		}
		key := "(" + text[h[2]:h[3]] + ")"
		label := capRunes(strings.TrimSpace(text[h[4]:h[5]]), 120)
		opt := map[string]any{"key": key, "label": label}

		started := false
		for _, line := range strings.Split(text[h[1]:end], "\n") {
			m := richOptionLineRe.FindStringSubmatch(line)
			if m == nil {
				if started && strings.TrimSpace(line) != "" {
					break
				}
				continue
			}
			started = true
			value := capRunes(strings.TrimSpace(m[2]), 2000)
			switch m[1] {
			case "Outcome":
				opt["outcome"] = value
				opt["body"] = value
			case "Pros":
				opt["pros"] = value
			case "Cons":
				opt["cons"] = value
			case "Reversibility":
				if value == "one_way" || value == "two_way" {
					opt["reversibility"] = value
				}
			case "Recommended":
				if !recommendedSeen {
					recommendedSeen = true
					opt["recommended"] = true
					opt["recommendation_rationale"] = value
				}
			}
		}

		if opt["outcome"] == nil {
			continue // an option without an outcome is not an option
		}
		opts = append(opts, opt)
	}

	if len(opts) < 2 {
		return nil // a single option is not a decision
	}
	return opts
}

// addRichGateOptions replaces the heuristic options when rich blocks exist;
// sets recommended_option_key from the recommended option.
func addRichGateOptions(payload map[string]any, sourceText string) {
	opts := parseRichOptions(sourceText)
	if opts == nil {
		return
	}
	arr := make([]any, len(opts))
	for i, o := range opts {
		arr[i] = o
		if o["recommended"] == true {
			payload["recommended_option_key"] = o["key"]
		}
	}
	payload["options"] = arr
}
