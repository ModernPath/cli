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
	// REQ-CROSS-415: the held-work read, or heldOK false when it failed.
	held   []heldPiece
	heldOK bool
	// REQ-CROSS-416: the contract version the server named on this run's
	// responses, carried in the result so it is never read from the shared
	// env after the deadline (F-CLI021-R1-08).
	servedContract string
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

	started := time.Now()
	done := make(chan briefFetch, 1)
	go func() {
		data, ferr := fetchFeed(env, "active")
		// REQ-CROSS-415 (EPIC-CLI-021): the second pure read — the caller's held
		// pieces and what moved on them, which also carries the REQ-CROSS-317
		// derived/declared-phase line (D11). Best-effort — a failure drops the
		// block, never the brief. Fetched in the same goroutine so it stays
		// within the deadline.
		held, ok := readHeldPieces(env)
		done <- briefFetch{data: data, err: ferr, held: held, heldOK: ok, servedContract: env.contractVersion}
	}()
	// REQ-CROSS-416: the local freshness inputs — kit drift, source
	// provenance, the release cache or one bounded GET — are gathered beside
	// the feed fetch, never after it.
	local := make(chan localFreshness, 1)
	go func() { local <- gatherLocalFreshness(root) }()

	// freshness is the line (or "") for the outcome at hand; it waits for the
	// local inputs only until the deadline and reads the contract only from
	// the fetch result. Past the deadline the local inputs are unknown and the
	// line is silent rather than late.
	freshness := func(servedContract string) (string, string) {
		var in localFreshness
		select {
		case in = <-local:
		case <-time.After(remaining(started, deadline)):
		}
		return briefFreshness(in, servedContract)
	}
	// envelopeFor is the failure branch's output: the freshness line alone
	// when one fired, else {} as before.
	envelopeFor := func(signal, line string) string {
		if line == "" {
			return "{}"
		}
		return briefEnvelope(event, freshnessNote(line))
	}

	select {
	case r := <-done:
		signal, line := freshness(r.servedContract)
		if r.err != nil {
			fmt.Fprint(out, envelopeFor(signal, line))
			briefLog(root, sid, src, "failed", r.err.Error()+" · "+freshnessDetail(signal))
			return
		}
		body := renderCompactBrief(r.data, renderHeldBlock(r.held, r.heldOK))
		if line != "" {
			body += "\n\n" + freshnessNote(line)
		}
		if note := storeBackedNote(root); note != "" {
			body += "\n\n_" + note + "_"
		}
		fmt.Fprint(out, briefEnvelope(event, body))
		detail := heldMode(r.held, r.heldOK) + " · " + freshnessDetail(signal)
		if p.SessionID == "" {
			detail = "no-session-id · " + detail
		}
		briefLog(root, sid, src, "delivered", detail)
	case <-time.After(deadline):
		// The in-flight request is abandoned: a brief that arrives after the
		// agent has started is worse than none — the turn was paid for either way.
		signal, line := freshness("")
		fmt.Fprint(out, envelopeFor(signal, line))
		briefLog(root, sid, src, "failed", "deadline exceeded · "+freshnessDetail(signal))
	}
}

func remaining(started time.Time, deadline time.Duration) time.Duration {
	if left := deadline - time.Since(started); left > 0 {
		return left
	}
	return 0
}

func freshnessNote(line string) string { return "_⚠ " + line + "_" }

func freshnessDetail(signal string) string {
	if signal == "" {
		return "freshness:fresh"
	}
	return "freshness:" + signal
}

// renderCompactBrief is the held-work block (REQ-CROSS-415), the top three
// items and the three CTA lines under a name header — the hook's smaller
// form of the terminal brief.
func renderCompactBrief(data map[string]any, held string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Your move — %s\n\n", str(feedMap(data, "person"), "name"))
	if held != "" {
		b.WriteString(held + "\n")
	}
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
