package cmd

import (
	"fmt"
	"sort"
	"strings"
)

// REQ-CROSS-372 (EPIC-CLI-016): a server refusal reaches the operator as the
// server wrote it. Every non-2xx body the store returns is one of a few shapes —
// `error.details` (a field → messages map from a legality check or changeset),
// `error.reason` (a conflict sentence), `error.message` (+ `winner` on a
// first-wins answer), a bare `error` string, or an undecodable body the
// transport kept raw under `error.reason` — and this is the one place that
// turns any of them into prose. `details` render as sorted `field: message`
// lines so a two-field refusal reads the same on every run.
// An empty `what` reads "server <status>: <text>" — the bare form the read
// and sync sites used. REQ-CROSS-380 (EPIC-CLI-018): every site routes here,
// so a bodyless refusal reads "no body" plus the request reference the
// transport kept, never <nil>.
func serverRefusal(what string, status int, body map[string]any) error {
	if what == "" {
		return fmt.Errorf("server %d: %s", status, refusalText(body))
	}
	return fmt.Errorf("%s (server %d): %s", what, status, refusalText(body))
}

func refusalText(body map[string]any) string {
	text := refusalBodyText(body)
	if ref := str(body, "request_id"); ref != "" {
		return text + " (ref " + ref + ")"
	}
	return text
}

func refusalBodyText(body map[string]any) string {
	if body == nil {
		return "(no body)"
	}
	switch errVal := body["error"].(type) {
	case string:
		return errVal
	case map[string]any:
		if details, ok := errVal["details"].(map[string]any); ok && len(details) > 0 {
			return detailLines(details)
		}
		var parts []string
		if reason := str(errVal, "reason"); reason != "" {
			parts = append(parts, reason)
			if sentence := reasonSentence(reason); sentence != "" {
				parts = append(parts, sentence)
			}
			if reason == "release_open" {
				parts = append(parts, openReleaseRefusal(errVal))
			}
		}
		if message := str(errVal, "message"); message != "" {
			parts = append(parts, message)
		}
		if winner, ok := errVal["winner"].(map[string]any); ok {
			parts = append(parts, fmt.Sprintf("winner: %q (%s)", str(winner, "answer"), str(winner, "source_tag")))
		}
		if len(parts) > 0 {
			return strings.Join(parts, " — ")
		}
		return fmt.Sprint(errVal)
	case nil:
		// PR445-07: a non-store body (a gateway's {"message": …}) still has a
		// sentence to print before falling back to the whole map.
		var parts []string
		if message := str(body, "message"); message != "" {
			parts = append(parts, message)
		}
		if reason := str(body, "reason"); reason != "" {
			parts = append(parts, reason)
		}
		if len(parts) > 0 {
			return strings.Join(parts, " — ")
		}
		return fmt.Sprint(body)
	default:
		return fmt.Sprint(errVal)
	}
}

func openReleaseRefusal(errBody map[string]any) string {
	var parts []string
	if releases, ok := errBody["open_releases"].([]any); ok {
		for _, value := range releases {
			release, ok := value.(map[string]any)
			if !ok {
				continue
			}
			name, slug, status := str(release, "name"), str(release, "slug"), str(release, "status")
			if name == "" {
				name = "(unnamed release)"
			}
			if slug == "" {
				slug = "unknown slug"
			}
			if status == "" {
				status = "unknown status"
			}
			parts = append(parts, fmt.Sprintf("%s (%s): %s", name, slug, status))
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "no readable incumbent release details were supplied")
	}
	parts = append(parts, "Retry `modernpath factory release activate <slug> --close-current` only if you intend to close these releases")
	return strings.Join(parts, "; ")
}

func detailLines(details map[string]any) string {
	fields := make([]string, 0, len(details))
	for field := range details {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	lines := make([]string, 0, len(fields))
	for _, field := range fields {
		var msgs []string
		switch v := details[field].(type) {
		case []any:
			for _, m := range v {
				msgs = append(msgs, fmt.Sprint(m))
			}
		case []string:
			msgs = append(msgs, v...)
		default:
			msgs = append(msgs, fmt.Sprint(v))
		}
		lines = append(lines, field+": "+strings.Join(msgs, "; "))
	}
	return strings.Join(lines, "\n")
}

// reasonSentence says what a bare server reason token means for the person at
// the keyboard and what to run next (REQ-CROSS-407). The token stays in the
// output so the refusal glossary still keys on it. No duration is stated for a
// locked PIN: the server serves none, and the CLI prints no server constant.
func reasonSentence(reason string) string {
	switch reason {
	case "pin_required":
		return "the release PIN of the signed-in person is required; it is set in Mission Control — pass it with --pin"
	case "pin_locked":
		return "repeated failures locked the PIN for a while — retry later"
	}
	return ""
}
