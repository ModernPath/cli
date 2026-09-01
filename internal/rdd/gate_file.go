package rdd

import (
	"fmt"
	"regexp"
	"strings"
)

// REQ-CROSS-237: `file-state/GATES.md` is the flat-file serialization of the
// process store's gate records, and nothing read it. A reverse-engineering pass
// wrote 23 confirmation gates, `factory sync` emitted zero `upsert_gate` ops,
// and `your-move` reported no open gates (`RUN:2026-08-24`). A DERIVED corpus
// whose confirmation gates never reach the store cannot be confirmed, and
// confirmation is the only exit PROCESS.md gives a candidate.

// GateRecord is one `## GATE-…` section of a gate flat-file.
type GateRecord struct {
	ID            string
	Title         string
	Kind          string // "human" | "trace"
	Purpose       string // the sub-kind: confirmation, entry, decision, …
	Transition    string
	Scope         []string
	Prerequisites []string
	Fingerprint   string
	State         string // as written: DRAFT | OPEN | ANSWERED | CLOSED | PENDING | PASS | FAIL | STALE
	Answer        string
	Actor         string
	Sources       string
	Brief         string
	Line          int
}

var (
	gateHeadingRe = regexp.MustCompile(`(?m)^## +(GATE-[A-Z0-9-]+)\s*(?:—|--|-)?\s*(.*)$`)
	gateFieldRe   = regexp.MustCompile(`(?m)^- \*\*([^:*]+):\*\*[ \t]*(.*)$`)
	gateIDRe      = regexp.MustCompile(`GATE-[A-Z0-9-]+`)
	// REQ-CROSS-237: a gate section ends at the NEXT h2 of any kind, not at the
	// next GATE heading. `file-state/GATES.md` also holds `## CRF-…` finding
	// records (req-driven-dev#13) and prose sections; bounding on GATE- alone
	// let a finding's fields overwrite the preceding gate's — a confirmation
	// gate reached the store carrying another record's scope.
	anyH2Re      = regexp.MustCompile(`(?m)^## `)
	scopeIDRe    = regexp.MustCompile(`(?:UR|SR|REQ|EPIC)-[A-Z0-9]+-\d+`)
	briefFenceRe = regexp.MustCompile("(?s)```markdown\\s*(.*?)```")
)

// ParseGateFile reads every `## GATE-…` section. Absence of the file is a fact
// about the workspace, never an error — the caller decides.
func ParseGateFile(content string) []GateRecord {
	locs := gateHeadingRe.FindAllStringSubmatchIndex(content, -1)
	if len(locs) == 0 {
		return nil
	}

	var out []GateRecord
	for i, loc := range locs {
		end := len(content)
		if next := anyH2Re.FindStringIndex(content[loc[1]:]); next != nil {
			end = loc[1] + next[0]
		}
		_ = i
		body := content[loc[1]:end]

		rec := GateRecord{
			ID:    content[loc[2]:loc[3]],
			Title: strings.TrimSpace(content[loc[4]:loc[5]]),
			Line:  strings.Count(content[:loc[0]], "\n") + 1,
		}

		for _, f := range gateFieldRe.FindAllStringSubmatch(body, -1) {
			label := strings.ToLower(strings.TrimSpace(f[1]))
			value := strings.TrimSpace(f[2])
			switch {
			case strings.HasPrefix(label, "kind"):
				rec.Kind, rec.Purpose = splitKind(value)
			case strings.HasPrefix(label, "transition"):
				rec.Transition = value
			case strings.HasPrefix(label, "exact scope"):
				rec.Scope = scopeIDRe.FindAllString(value, -1)
			case strings.HasPrefix(label, "prerequisite"):
				rec.Prerequisites = gateIDRe.FindAllString(value, -1)
			case strings.HasPrefix(label, "fingerprint"):
				rec.Fingerprint = value
			case strings.HasPrefix(label, "state"):
				rec.State = leadingToken(value)
			case strings.HasPrefix(label, "verdict"):
				rec.Answer = value
			case strings.HasPrefix(label, "actor"):
				rec.Actor = value
			case strings.HasPrefix(label, "sources"):
				rec.Sources = value
			}
		}

		if m := briefFenceRe.FindStringSubmatch(body); m != nil {
			rec.Brief = strings.TrimSpace(m[1])
		}

		// A template placeholder is a shape, not a record.
		if strings.Contains(rec.ID, "«") || strings.HasPrefix(rec.Title, "«") {
			continue
		}
		out = append(out, rec)
	}
	return out
}

// `human / confirmation` or `trace / confirmation prerequisite`
func splitKind(value string) (kind, purpose string) {
	parts := strings.SplitN(value, "/", 2)
	kind = strings.ToLower(strings.TrimSpace(parts[0]))
	if len(parts) == 2 {
		purpose = strings.ToLower(strings.TrimSpace(parts[1]))
	}
	if i := strings.Index(kind, " "); i > 0 {
		kind = kind[:i]
	}
	return kind, purpose
}

// The state cell carries prose after the token ("DRAFT — opens when a human…").
func leadingToken(value string) string {
	value = strings.TrimSpace(value)
	for i, r := range value {
		if r < 'A' || r > 'Z' {
			if r == '_' {
				continue
			}
			return value[:i]
		}
	}
	return value
}

// BuildGateFileOps turns confirmation gates into `upsert_gate` ops.
//
// PROCESS.md §Gates: "A human gate may become OPEN only after every
// prerequisite trace gate is PASS." That is a deterministic transition on
// non-human facts, so the builder applies it: a DRAFT human gate whose named
// prerequisites are all PASS in this same file is synced OPEN. One whose
// prerequisites are not met is NOT synced, and the caller is told — a gate that
// silently fails to arrive is what made this defect invisible for days.
//
// Trace gates are not synced: the store's gate vocabulary records human
// decisions, and a trace gate is the evidence a human gate rests on. They are
// read here only to decide whether the human gate they gate may open.
func BuildGateFileOps(records []GateRecord, originRef string) ([]Op, []string) {
	traceState := map[string]string{}
	for _, r := range records {
		if r.Kind == "trace" {
			traceState[r.ID] = strings.ToUpper(r.State)
		}
	}

	var ops []Op
	var warnings []string
	var opened []string

	for _, r := range records {
		if r.Kind != "human" {
			continue
		}

		state, answer := gateStoreState(r)
		if state == "" {
			warnings = append(warnings, fmt.Sprintf("%s has an unreadable state %q — not synced", r.ID, r.State))
			continue
		}

		if state == "open" {
			if blockers := unmetPrerequisites(r, traceState); len(blockers) > 0 {
				warnings = append(warnings, fmt.Sprintf(
					"%s stays DRAFT — prerequisite(s) not PASS: %s", r.ID, strings.Join(blockers, ", ")))
				continue
			}
			opened = append(opened, r.ID)
		}

		payload := map[string]any{
			"external_id": r.ID,
			"kind":        "approval_request",
			"title":       gateTitle(r),
			"body_md":     capRunes(gateBody(r), 6000),
			"origin":      "workspace",
			"origin_ref":  originRef,
			"state":       state,
			"opened_at":   gateGenesis,
		}
		if len(r.Scope) > 0 {
			scope := make([]any, 0, len(r.Scope))
			for _, id := range r.Scope {
				scope = append(scope, id)
			}
			payload["exact_scope"] = scope
		}
		if answer != "" {
			payload["answer"] = capRunes(answer, 1200)
			payload["answered_at"] = gateGenesis
		}
		payload["content_hash"] = ContentHash(payload)
		payload["actor"] = map[string]any{"kind": "agent", "agent_slug": "mp-cli"}
		ops = append(ops, Op{Type: "upsert_gate", Payload: payload})
	}

	if len(opened) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%d confirmation gate(s) had every prerequisite PASS and are synced OPEN for answering: %s",
			len(opened), strings.Join(opened, ", ")))
	}
	return ops, warnings
}

// The genesis constant keeps replay hash-stable; the file carries no clock the
// store can trust (EPIC-SYNC-006 precedent).
const gateGenesis = "2026-08-09T00:00:00.000000Z"

func gateStoreState(r GateRecord) (state, answer string) {
	switch strings.ToUpper(r.State) {
	case "OPEN", "DRAFT":
		return "open", ""
	case "ANSWERED", "CLOSED":
		return "answered", r.Answer
	case "SUPERSEDED":
		return "dismissed", r.Answer
	}
	return "", ""
}

func unmetPrerequisites(r GateRecord, traceState map[string]string) []string {
	var blockers []string
	for _, id := range r.Prerequisites {
		if state, known := traceState[id]; !known || state != "PASS" {
			blockers = append(blockers, id)
		}
	}
	return blockers
}

func gateTitle(r GateRecord) string {
	if r.Title != "" {
		return r.Title
	}
	return r.ID
}

func gateBody(r GateRecord) string {
	var b strings.Builder
	if r.Brief != "" {
		b.WriteString(r.Brief)
		b.WriteString("\n\n")
	}
	if r.Transition != "" {
		fmt.Fprintf(&b, "**Transition:** %s\n\n", r.Transition)
	}
	if len(r.Scope) > 0 {
		fmt.Fprintf(&b, "**Exact scope (%d):** %s\n\n", len(r.Scope), strings.Join(r.Scope, " "))
	}
	if r.Fingerprint != "" {
		fmt.Fprintf(&b, "**Fingerprint:** %s\n\n", r.Fingerprint)
	}
	if r.Sources != "" {
		fmt.Fprintf(&b, "**Sources:** %s\n", r.Sources)
	}
	return strings.TrimSpace(b.String())
}
