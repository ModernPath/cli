package cmd

// REQ-CROSS-277 (EPIC-NEXT-003) — the SessionStart brief hook.
//
// It prints the compact personal brief once per agent session through the
// documented hookSpecificOutput.additionalContext envelope, and {} on every
// other path — a repeat, a compaction, a failure, an unbound workspace, a
// deadline — always exiting 0, so the hook never costs the user their session.
// The idempotence key is the payload's session_id, stamped in
// .modernpath/session-briefs.log (gitignored, content-free — the sync-hooks.log
// precedent). The per-prompt context hook is unchanged.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const briefLogName = "session-briefs.log"

// The agent's SessionStart payload (Claude Code). Any field may be absent;
// unparseable input behaves as a payload with no session id.
type sessionStartPayload struct {
	SessionID     string `json:"session_id"`
	HookEventName string `json:"hook_event_name"`
	Source        string `json:"source"`
}

type briefFetch struct {
	data map[string]any
	err  error
}

func runBriefHook(event string, in io.Reader, out io.Writer, deadline time.Duration, loadEnv func() (*factoryEnv, error)) {
	raw, _ := io.ReadAll(in)
	var p sessionStartPayload
	_ = json.Unmarshal(raw, &p)

	env, err := loadEnv()
	root := "."
	if env != nil {
		root = env.Root
	}
	sid := orNone(p.SessionID)
	src := orNone(p.Source)

	// A compaction is mid-work, not a start — it never briefs, even an unseen id.
	if p.Source == "compact" {
		fmt.Fprint(out, "{}")
		briefLog(root, sid, src, "skipped", "compact")
		return
	}
	// Once per session id.
	if p.SessionID != "" && briefDelivered(root, p.SessionID) {
		fmt.Fprint(out, "{}")
		briefLog(root, sid, src, "already-briefed", "")
		return
	}
	if err != nil || env == nil {
		fmt.Fprint(out, "{}")
		briefLog(root, sid, src, "failed", "no workspace config")
		return
	}

	done := make(chan briefFetch, 1)
	go func() {
		data, ferr := fetchFeed(env, "active")
		done <- briefFetch{data, ferr}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			fmt.Fprint(out, "{}")
			briefLog(root, sid, src, "failed", r.err.Error())
			return
		}
		fmt.Fprint(out, briefEnvelope(event, renderCompactBrief(r.data)))
		detail := ""
		if p.SessionID == "" {
			detail = "no-session-id"
		}
		briefLog(root, sid, src, "delivered", detail)
	case <-time.After(deadline):
		// The in-flight request is abandoned: a brief that arrives after the
		// agent has started is worse than none — the turn was paid for either way.
		fmt.Fprint(out, "{}")
		briefLog(root, sid, src, "failed", "deadline exceeded")
	}
}

// renderCompactBrief is the top three items and the three CTA lines under a
// name header — the hook's smaller form of the terminal brief.
func renderCompactBrief(data map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Your move — %s\n\n", str(feedMap(data, "person"), "name"))
	items := feedList(data, "items")
	for i, raw := range briefSlice(items, 0, 3) {
		renderBriefItem(&b, i+1, feedMapOf(raw))
	}
	renderBriefCtas(&b, data, items)
	return strings.TrimRight(b.String(), "\n")
}

func briefEnvelope(event, context string) string {
	out, err := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     event,
			"additionalContext": context,
		},
	})
	if err != nil {
		return "{}"
	}
	return string(out)
}

// briefDelivered reports whether this session id already has a delivered line —
// a content grep, not an mtime compare, so the key is the fact, not the time.
func briefDelivered(root, sid string) bool {
	b, err := os.ReadFile(filepath.Join(root, ".modernpath", briefLogName))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.Contains(line, "· "+sid+" ·") && strings.Contains(line, "delivered") {
			return true
		}
	}
	return false
}

// briefLog appends `<ts> · <session|none> · <source|none> · <outcome> · <detail>`;
// the log is append-only and content-free (the sync-hooks.log precedent).
func briefLog(root, sid, src, outcome, detail string) {
	line := fmt.Sprintf("%s · %s · %s · %s", time.Now().UTC().Format(time.RFC3339), sid, src, outcome)
	if detail != "" {
		line += " · " + detail
	}
	line += "\n"

	path := filepath.Join(root, ".modernpath", briefLogName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
