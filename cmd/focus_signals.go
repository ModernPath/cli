package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// REQ-PLN-135 §135.2/§135.3 (EPIC-NEXT-005) + REQ-PLN-143 §143.1 (EPIC-NEXT-009):
// the CLI-local hysteresis buffer and its rule. Nothing per prompt reaches the
// server — extraction and the decision run on the workstation, and only a
// conclusion travels.
//
// The buffer, .modernpath/focus-signals, holds the caller's current focus as a
// SET of lane lines — `current <ref> <set_by> <source> <observed_at>`, one per
// open ref (multi-lane; `current -` when empty) — plus at most five ref-bearing
// signal lines: `<observed_at> <source> <ref>[,<ref>…]`. Refs only: never the
// prompt, the commit subject or the branch name. It sits under .modernpath/,
// which `modernpath init` gitignores.

type focusSignal struct {
	at     time.Time
	source string
	refs   []string
}

// focusLane is one lane the server holds current for the caller — one per open
// ref (REQ-PLN-143 §143.1, multi-lane). The set replaces EPIC-NEXT-005's single
// current ref/set_by.
type focusLane struct {
	ref    string
	setBy  string
	source string
	at     time.Time
}

// focusConclusion is one ref the local rule concludes, with the source of the
// newest signal naming it. decideFocus returns all of them (multi-lane fan-out).
type focusConclusion struct {
	ref    string
	source string
}

type focusBuffer struct {
	current []focusLane
	signals []focusSignal
}

const focusSignalWindow = 5

func focusSignalsPath(root string) string {
	return filepath.Join(root, ".modernpath", "focus-signals")
}

func loadFocusBuffer(root string) (focusBuffer, error) {
	var buf focusBuffer

	data, err := os.ReadFile(focusSignalsPath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return buf, nil
		}
		return buf, err
	}

	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "current ") {
			// "current -" is the empty marker. "current <ref> <set_by>" is the
			// EPIC-NEXT-005 single-line shape (backward-compatible load); the
			// 4-payload "current <ref> <set_by> <source> <at>" is a §143.1 lane.
			fields := strings.Fields(line)
			if len(fields) >= 3 && fields[1] != "-" {
				lane := focusLane{ref: fields[1], setBy: fields[2]}
				if len(fields) >= 5 {
					if fields[3] != "-" {
						lane.source = fields[3]
					}
					lane.at, _ = time.Parse(time.RFC3339Nano, fields[4])
				}
				buf.current = append(buf.current, lane)
			}
			continue
		}

		fields := strings.SplitN(line, " ", 3)
		if len(fields) != 3 {
			continue
		}
		at, _ := time.Parse(time.RFC3339Nano, fields[0])
		buf.signals = append(buf.signals, focusSignal{
			at:     at,
			source: fields[1],
			refs:   strings.Split(fields[2], ","),
		})
	}
	return buf, nil
}

func (b focusBuffer) save(root string) error {
	dir := filepath.Join(root, ".modernpath")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	var sb strings.Builder
	if len(b.current) == 0 {
		sb.WriteString("current -\n")
	} else {
		for _, lane := range b.current {
			src := lane.source
			if src == "" {
				src = "-"
			}
			fmt.Fprintf(&sb, "current %s %s %s %s\n", lane.ref, lane.setBy, src, lane.at.UTC().Format(time.RFC3339Nano))
		}
	}
	for _, s := range b.signals {
		fmt.Fprintf(&sb, "%s %s %s\n", s.at.UTC().Format(time.RFC3339Nano), s.source, strings.Join(s.refs, ","))
	}

	// atomic: write a temp file beside the target, then rename over it, so a
	// concurrent reader never sees a half-written buffer (last writer wins).
	tmp, err := os.CreateTemp(dir, "focus-signals-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(sb.String()); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, focusSignalsPath(root))
}

// appendFocusSignal records one ref-bearing observation and keeps only the last
// five. An observation with no ref writes nothing (§135.2).
func appendFocusSignal(root, source string, refs []string, at time.Time) error {
	if len(refs) == 0 {
		return nil
	}

	buf, err := loadFocusBuffer(root)
	if err != nil {
		return err
	}
	buf.signals = append(buf.signals, focusSignal{at: at, source: source, refs: refs})
	if len(buf.signals) > focusSignalWindow {
		buf.signals = buf.signals[len(buf.signals)-focusSignalWindow:]
	}
	return buf.save(root)
}

// mergeFocusLane upserts one lane (by ref) into the buffer's current set
// (REQ-PLN-143 §143.1) — the writeback after a successful infer POST or a door
// declare. It keeps the caller's other lanes rather than replacing them. An
// empty ref is a no-op.
func mergeFocusLane(root, ref, setBy, source string, at time.Time) error {
	if ref == "" {
		return nil
	}
	buf, err := loadFocusBuffer(root)
	if err != nil {
		return err
	}
	lane := focusLane{ref: ref, setBy: setBy, source: source, at: at}
	for i := range buf.current {
		if buf.current[i].ref == ref {
			buf.current[i] = lane
			return buf.save(root)
		}
	}
	buf.current = append(buf.current, lane)
	return buf.save(root)
}

// setFocusLanes replaces the buffer's current set with the server's authoritative
// lanes from the heartbeat echo (REQ-PLN-143 §143.1). An empty set clears it.
func setFocusLanes(root string, lanes []focusLane) error {
	buf, err := loadFocusBuffer(root)
	if err != nil {
		return err
	}
	buf.current = lanes
	return buf.save(root)
}

// decideFocus applies the §135.3 rule per ref over the last five ref-bearing
// signals (REQ-PLN-143 §143.1 makes it multi-lane): every ref sighted in ≥ 2 of
// them that is NOT already a current lane is concluded. A current lane — declared
// (declared-wins) or inferred (keep-current) — suppresses only its OWN ref, so
// several refs conclude at once. Each conclusion's source is the newest signal
// naming it; the fan-out is ordered most-recently-observed first. It never
// mutates the buffer — the caller records a conclusion only on a successful POST.
func decideFocus(root string) []focusConclusion {
	buf, err := loadFocusBuffer(root)
	if err != nil {
		return nil
	}

	count := map[string]int{}
	for _, s := range buf.signals {
		for _, r := range s.refs {
			count[r]++
		}
	}

	held := map[string]bool{}
	for _, lane := range buf.current {
		held[lane.ref] = true
	}

	var out []focusConclusion
	seen := map[string]bool{}
	for i := len(buf.signals) - 1; i >= 0; i-- {
		for _, ref := range buf.signals[i].refs {
			if seen[ref] || held[ref] || count[ref] < 2 {
				continue
			}
			seen[ref] = true
			out = append(out, focusConclusion{ref: ref, source: buf.signals[i].source})
		}
	}
	return out
}

// focusInferSpawn posts a conclusion in a detached child process so the context
// hook never waits on the network (§135.4). Overridable in tests. Fire-and-
// forget: a dropped conclusion is retried on the next sync, since the buffer
// keeps the signals.
var focusInferSpawn = func(ref, source string) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	_ = exec.Command(exe, "focus", "--infer", ref, "--source", source).Start()
}

// recordFocusSignalFromPrompt is the context hook's focus work: extract refs
// from the prompt, append a refs-only signal, and spawn a detached conclusion
// for every ref the rule fires (multi-lane fan-out, §143.1). The prompt text is
// discarded — nothing but refs is kept, and no request is made unless a ref is
// confirmed (§135.4/§135.9).
func recordFocusSignalFromPrompt(root, prompt string) {
	refs := ExtractRefs(prompt)
	if len(refs) == 0 {
		return
	}
	if err := appendFocusSignal(root, "prompt", refs, time.Now()); err != nil {
		return
	}
	for _, c := range decideFocus(root) {
		focusInferSpawn(c.ref, c.source)
	}
}

// echoLaneAt reads a heartbeat echo entry's started_at (informational — the
// decision uses signal times, not lane times), zero when absent or unparseable.
func echoLaneAt(m map[string]any) time.Time {
	if s := str(m, "started_at"); s != "" {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// learnFocusCurrentFromEcho replaces the buffer's current set from a heartbeat
// response's focus echo (§135.5 / §143.1) — an ARRAY of lanes, the CLI's source
// of truth for per-ref declared-wins and keep-current. A missing, null, or
// non-array focus clears the set (safe: the array is the only expected shape).
func learnFocusCurrentFromEcho(root string, hbBody map[string]any) {
	var lanes []focusLane
	if arr, ok := dataOf(hbBody)["focus"].([]any); ok {
		for _, item := range arr {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			ref := str(m, "ref_external_id")
			if ref == "" {
				continue
			}
			lanes = append(lanes, focusLane{
				ref:    ref,
				setBy:  str(m, "set_by"),
				source: str(m, "source"),
				at:     echoLaneAt(m),
			})
		}
	}
	_ = setFocusLanes(root, lanes)
}

// recordFocusSignalsFromSync is factory sync's focus work: append the branch
// and last-commit-subject signals, learn the server's current lanes from the
// heartbeat echo, and post a conclusion inline for every ref the rule fires
// (sync is not on the hook's latency budget). Each posted lane is merged back.
func recordFocusSignalsFromSync(env *factoryEnv, hbBody map[string]any) {
	now := time.Now()
	for _, sig := range []struct{ source, text string }{
		{"branch", gitOut(env.Root, "rev-parse", "--abbrev-ref", "HEAD")},
		{"commit", gitOut(env.Root, "log", "-1", "--format=%s")},
	} {
		if refs := ExtractRefs(sig.text); len(refs) > 0 {
			_ = appendFocusSignal(env.Root, sig.source, refs, now)
		}
	}

	learnFocusCurrentFromEcho(env.Root, hbBody)

	for _, c := range decideFocus(env.Root) {
		if out, err := focusInfer(env, c.ref, c.source); err == nil && out.focus != nil {
			_ = mergeFocusLane(env.Root, str(out.focus, "ref_external_id"), str(out.focus, "set_by"), c.source, time.Now())
		}
	}
}
