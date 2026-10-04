package rdd

import (
	"regexp"

	"strings"
)

// ---------------------------------------------------------------- rdd-review-queue-v1

var (
	// §245.5: docs/85 writes items at both heading depths — 13 as H2. The
	// head case is matched before the H2 terminator, so an `## RQ-` line
	// starts a block instead of ending one.
	rqHeadRe  = regexp.MustCompile(`^##+\s+RQ-`)
	rqIDRe    = regexp.MustCompile(`RQ-\d+[a-z]?`)
	h2NotH3Re = regexp.MustCompile(`^##[^#]`)
	// §245.7: DONE / FIXED / ANSWERED close a block too — word-bounded, so
	// UNANSWERED stays a finding.
	rqClosedRe    = regexp.MustCompile(`(?i)APPROVED|RESOLVED|✅|\b(?:DONE|FIXED|ANSWERED)\b`)
	rqDelegatedRe = regexp.MustCompile(`(?i)DELEGATED`)
	rqDecisionRe  = regexp.MustCompile(`(?i)DECISION NEEDED`)
	dateRe        = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
	rqTitleIDRe   = regexp.MustCompile(`^RQ-\d+[a-z]?\s*`)
	rqTitleParRe  = regexp.MustCompile(`^\([^)]*\)\s*`)
	rqTitleDashRe = regexp.MustCompile(`^[—–-]\s*`)
	rqTitleDecRe  = regexp.MustCompile(`(?i)\s*—\s*DECISION NEEDED.*$`)
	recommendRe   = regexp.MustCompile(`(?i)[^.\n]*recommend[^.\n]*\.`)
	optionMarkRe  = regexp.MustCompile(`\*{0,2}\((i{1,3}|iv|v|[a-z]|\d{1,2})\)\*{0,2}\s+`)
	headPrefixRe  = regexp.MustCompile(`^##+\s*`)
)

type rqBlock struct {
	id      string
	heading string
	lineNo  int
	body    string
}

// ParseReviewQueue parses docs/85-loop-review-queue.md (rdd-review-queue-v1).
func ParseReviewQueue(content string) []RQ {
	lines := strings.Split(content, "\n")
	var blocks []rqBlock
	var cur *rqBlock
	for i, line := range lines {
		switch {
		case rqHeadRe.MatchString(line):
			if cur != nil {
				blocks = append(blocks, *cur)
			}
			id := rqIDRe.FindString(line)
			if id == "" {
				id = "RQ-?"
			}
			cur = &rqBlock{id: id, heading: headPrefixRe.ReplaceAllString(line, ""), lineNo: i + 1}
		case h2NotH3Re.MatchString(line):
			if cur != nil {
				blocks = append(blocks, *cur)
				cur = nil
			}
		default:
			if cur != nil {
				cur.body += line + "\n"
			}
		}
	}
	if cur != nil {
		blocks = append(blocks, *cur)
	}

	byID := map[string]*RQ{}
	var order []string
	for _, b := range blocks {
		state := "finding"
		switch {
		case rqClosedRe.MatchString(b.heading):
			state = "closed"
		case rqDelegatedRe.MatchString(b.heading):
			state = "delegated"
		case rqDecisionRe.MatchString(b.heading):
			state = "decision-needed"
		}
		entry := RQ{
			ID:             b.id,
			Title:          cleanTitle(b.heading),
			Heading:        capRunes(strip(b.heading), 240),
			Date:           dateRe.FindString(b.heading),
			State:          state,
			Line:           b.lineNo,
			Body:           capRunes(strings.TrimSpace(b.body), bodyCap),
			Pool:           idMentions(b.body, "REQ", "EPIC"),
			Options:        parseOptions(b.body),
			Recommendation: parseRecommendation(b.body),
		}
		prev, dup := byID[b.id]
		if !dup {
			e := entry
			byID[b.id] = &e
			order = append(order, b.id)
			continue
		}
		// duplicate id: the closed block wins; pools union; bodies join
		winner := prev
		if prev.State != "closed" && entry.State == "closed" {
			e := entry
			winner = &e
		}
		pool := prev.Pool
		seen := map[string]bool{}
		for _, p := range pool {
			seen[p] = true
		}
		for _, p := range entry.Pool {
			if !seen[p] {
				pool = append(pool, p)
			}
		}
		winner.Pool = pool
		winner.Body = capRunes(
			"### "+prev.Heading+"\n\n"+prev.Body+"\n\n---\n\n### "+entry.Heading+"\n\n"+entry.Body,
			bodyCap,
		)
		byID[b.id] = winner
	}

	out := make([]RQ, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out
}

func cleanTitle(head string) string {
	t := rqTitleIDRe.ReplaceAllString(head, "")
	t = rqTitleParRe.ReplaceAllString(t, "")
	t = rqTitleDashRe.ReplaceAllString(t, "")
	t = rqTitleDecRe.ReplaceAllString(t, "")
	return capRunes(strip(t), 160)
}

func parseOptions(body string) []Option {
	text := strings.ReplaceAll(body, "\n", " ")
	marks := optionMarkRe.FindAllStringSubmatchIndex(text, -1)
	var opts []Option
	seen := map[string]bool{}
	for i, m := range marks {
		end := m[1]
		var chunk string
		if i+1 < len(marks) {
			chunk = text[end:marks[i+1][0]]
		} else {
			stop := end + 240
			if stop > len(text) {
				stop = len(text)
			}
			chunk = text[end:stop]
		}
		cut := strings.TrimSpace(splitAfterSentence(chunk))
		label := "(" + text[m[2]:m[3]] + ")"
		if len([]rune(cut)) >= 8 && !seen[label] {
			seen[label] = true
			opts = append(opts, Option{Label: label, Text: capRunes(strip(cut), 220)})
		}
	}
	if len(opts) >= 2 && len(opts) <= 10 {
		return opts
	}
	return nil
}

// splitAfterSentence = chunk.split(/(?<=[.;?])\s/)[0] — RE2 has no lookbehind,
// so scan for the first [.;?] immediately followed by whitespace.
func splitAfterSentence(chunk string) string {
	for i := 0; i < len(chunk)-1; i++ {
		c := chunk[i]
		if (c == '.' || c == ';' || c == '?') && isSpaceByte(chunk[i+1]) {
			return chunk[:i+1]
		}
	}
	return chunk
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v'
}

func parseRecommendation(body string) string {
	m := recommendRe.FindString(body)
	if m == "" {
		return ""
	}
	return capRunes(strings.TrimSpace(strip(m)), 300)
}
