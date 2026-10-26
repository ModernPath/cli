package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
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
		return serverRefusalError{status: status, msg: fmt.Sprintf("server %d: %s", status, refusalText(body))}
	}
	return serverRefusalError{status: status, msg: fmt.Sprintf("%s (server %d): %s", what, status, refusalText(body))}
}

// serverRefusalError keeps the status, so outage replay can tell a server
// fault (5xx) from a refusal (REQ-CROSS-539 AC6, CODE-OUTAGE-4-1).
type serverRefusalError struct {
	status int
	msg    string
}

func (e serverRefusalError) Error() string { return e.msg }

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
		// Epic 345: a coverage refusal (already_on_roadmap, not_linkable)
		// names the covered records by their native ids.
		if ids := refusalIDs(body["ids"]); ids != "" {
			return errVal + ": " + ids
		}
		return errVal
	case map[string]any:
		if text, ok := batchRefusalText(body, errVal); ok {
			return text
		}
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
			if reason == "incumbent_has_unfinished_work" {
				parts = append(parts, unfinishedWorkRefusal(errVal))
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
		if code := str(errVal, "code"); code != "" {
			return code
		}
		return fmt.Sprint(errVal)
	case nil:
		// Epic 345: a changeset refusal without `error` ({errors: {field:
		// [messages]}}) prints its field lines.
		if fields, ok := body["errors"].(map[string]any); ok && len(fields) > 0 {
			return detailLines(fields)
		}
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

// batchRefusalText renders a refused batch item (Epics 321-323): the body
// carries {error:{code,message}, index, item} and reads
// "<code> — item <index> (<identity>) — <message>".
func batchRefusalText(body, errVal map[string]any) (string, bool) {
	code := str(errVal, "code")
	var index string
	switch v := body["index"].(type) {
	case json.Number:
		index = v.String()
	case float64:
		index = strconv.FormatInt(int64(v), 10)
	}
	if code == "" || index == "" {
		return "", false
	}
	text := code + " — item " + index
	if identity := batchItemIdentity(body["item"]); identity != "" {
		text += " (" + identity + ")"
	}
	if message := str(errVal, "message"); message != "" {
		text += " — " + message
	}
	return text, true
}

func refusalIDs(value any) string {
	ids, ok := value.([]any)
	if !ok || len(ids) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Sprint(id))
	}
	return strings.Join(parts, ", ")
}

func batchItemIdentity(value any) string {
	item, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	if item["kind"] != nil && item["id"] != nil {
		return fmt.Sprint(item["kind"]) + " " + fmt.Sprint(item["id"])
	}
	if item["run_id"] != nil {
		return "run " + fmt.Sprint(item["run_id"])
	}
	if item["epic_id"] != nil {
		return "Epic " + fmt.Sprint(item["epic_id"])
	}
	for _, key := range []string{"obligation_ids", "obligations"} {
		if ids, ok := item[key].([]any); ok {
			parts := make([]string, 0, len(ids))
			for _, id := range ids {
				parts = append(parts, fmt.Sprint(id))
			}
			return "obligations " + strings.Join(parts, ",")
		}
	}
	if item["decision_id"] != nil {
		return "decision " + fmt.Sprint(item["decision_id"])
	}
	if _, ok := item["question"]; ok {
		return "question"
	}
	return ""
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
	return strings.Join(parts, "; ")
}

// SR-ROADMAP-015: activation with --close-current is refused while the active
// release still holds unfinished epics or system requirements. Retrying cannot
// help: each unfinished item needs a destination in the close dialog first.
func unfinishedWorkRefusal(errBody map[string]any) string {
	var names []string
	if releases, ok := errBody["open_releases"].([]any); ok {
		for _, value := range releases {
			release, ok := value.(map[string]any)
			if !ok {
				continue
			}
			name, slug := str(release, "name"), str(release, "slug")
			switch {
			case name != "" && slug != "":
				names = append(names, fmt.Sprintf("%s (%s)", name, slug))
			case slug != "":
				names = append(names, slug)
			case name != "":
				names = append(names, name)
			}
		}
	}
	subject := "the active release"
	if len(names) > 0 {
		subject = strings.Join(names, ", ")
	}
	return subject + " still has unfinished work; open it in the product, choose Close release and give each unfinished epic and system requirement a destination, then activate again"
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
