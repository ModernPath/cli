package cmd

// REQ-CROSS-415 (EPIC-CLI-021) — the SessionStart brief's held-work block.
//
// One caller-scoped read, GET /api/v1/sync/work-selection/held, answers
// whether the person holds work and what moved on each piece since it was
// taken: packet sections written, members added or removed, gates answered,
// an entry pin the packet has passed. The brief is mode-aware on it — idle
// (nothing in flight, then the picks) or continuing (one line per piece with
// a delta, then a count of unchanged pieces) — and the REQ-CROSS-317 phase
// line is rendered from the same read, so the hook stays at two HTTP reads
// and the several-pieces refusal never reaches a session start (D1a, D11).
// A pure read; a failure drops the block, never the brief.

import (
	"encoding/json"
	"fmt"
	"strings"
)

type heldGate struct {
	ExternalID string `json:"external_id"`
	Purpose    string `json:"purpose"`
	Answer     string `json:"answer"`
	AnsweredAt string `json:"answered_at"`
}

type heldPiece struct {
	ScopeExternalID      string     `json:"scope_external_id"`
	ScopeKind            string     `json:"scope_kind"`
	Phase                string     `json:"phase"`
	DerivedPhase         string     `json:"derived_phase"`
	Divergence           bool       `json:"divergence"`
	Lane                 string     `json:"lane"`
	TakenAt              string     `json:"taken_at"`
	SectionsWrittenSince []string   `json:"sections_written_since"`
	MembersAdded         []string   `json:"members_added"`
	MembersRemoved       []string   `json:"members_removed"`
	EntryPinBehind       bool       `json:"entry_pin_behind"`
	GatesAnsweredSince   []heldGate `json:"gates_answered_since"`
}

type heldResponse struct {
	Data struct {
		Pieces []heldPiece `json:"pieces"`
	} `json:"data"`
}

// readHeldPieces is the one held-work read. ok is false when the read failed
// or the server does not serve it yet (a pre-REQ-CROSS-415 server answers
// 404): the brief then carries no held block rather than a wrong one.
func readHeldPieces(env *factoryEnv) (pieces []heldPiece, ok bool) {
	status, body, err := env.call("GET",
		fmt.Sprintf("/api/v1/sync/work-selection/held?system_id=%d", env.SystemID), nil)
	if err != nil || status != 200 {
		return nil, false
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, false
	}
	var resp heldResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, false
	}
	if resp.Data.Pieces == nil {
		resp.Data.Pieces = []heldPiece{}
	}
	return resp.Data.Pieces, true
}

func (p heldPiece) moved() bool {
	return len(p.SectionsWrittenSince) > 0 || len(p.MembersAdded) > 0 ||
		len(p.MembersRemoved) > 0 || len(p.GatesAnsweredSince) > 0 || p.EntryPinBehind
}

// heldMode names the brief's mode for the log line: idle, or continuing:<n>.
func heldMode(pieces []heldPiece, ok bool) string {
	switch {
	case !ok:
		return "held-unread"
	case len(pieces) == 0:
		return "idle"
	default:
		return fmt.Sprintf("continuing:%d", len(pieces))
	}
}

// renderHeldBlock is the block above the picks. Idle: one plain line.
// Continuing: one line per piece that moved (what moved, named by section
// key, member id and gate id — never diff prose), then one count line for
// the pieces that did not; every piece line carries its phase line.
func renderHeldBlock(pieces []heldPiece, ok bool) string {
	if !ok {
		return ""
	}
	if len(pieces) == 0 {
		return "You hold nothing in flight — the picks below are the best next work.\n"
	}
	var b strings.Builder
	unchanged := 0
	for _, p := range pieces {
		if !p.moved() {
			unchanged++
			continue
		}
		fmt.Fprintf(&b, "- %s%s: %s\n", p.ScopeExternalID, phaseSuffix(p), strings.Join(p.deltas(), " · "))
	}
	switch {
	case unchanged == len(pieces) && unchanged == 1:
		p := pieces[0]
		fmt.Fprintf(&b, "You hold %s%s — nothing moved on it since you took it.\n", p.ScopeExternalID, phaseSuffix(p))
	case unchanged == len(pieces):
		fmt.Fprintf(&b, "You hold %d pieces — nothing moved on them since you took them: %s.\n",
			unchanged, strings.Join(pieceNames(pieces), ", "))
	case unchanged == 1:
		fmt.Fprintf(&b, "1 other held piece is unchanged.\n")
	case unchanged > 1:
		fmt.Fprintf(&b, "%d other held pieces are unchanged.\n", unchanged)
	}
	return b.String()
}

// phaseSuffix is the REQ-CROSS-317 line, per piece: derived and declared
// phase, whether they diverge, and the defect lane when on it.
func phaseSuffix(p heldPiece) string {
	if p.DerivedPhase == "" && p.Phase == "" {
		return ""
	}
	s := " (derived " + orNone(p.DerivedPhase) + " · declared " + orNone(p.Phase)
	if p.Divergence {
		s += " · ⚠ diverges"
	}
	if p.Lane == "defect" {
		s += " · lane: customer-blocking defect"
	}
	return s + ")"
}

func (p heldPiece) deltas() []string {
	var parts []string
	if len(p.SectionsWrittenSince) > 0 {
		parts = append(parts, "sections written: "+strings.Join(p.SectionsWrittenSince, ", "))
	}
	if len(p.MembersAdded) > 0 {
		parts = append(parts, "members added: "+strings.Join(p.MembersAdded, ", "))
	}
	if len(p.MembersRemoved) > 0 {
		parts = append(parts, "members removed: "+strings.Join(p.MembersRemoved, ", "))
	}
	for _, g := range p.GatesAnsweredSince {
		line := "gate " + g.ExternalID + " answered"
		if g.Answer != "" {
			line += " " + g.Answer
		}
		parts = append(parts, line)
	}
	if p.EntryPinBehind {
		parts = append(parts, "⚠ the build is behind its entry pin and will block — `modernpath process reapply-entry`")
	}
	return parts
}

func pieceNames(pieces []heldPiece) []string {
	names := make([]string, 0, len(pieces))
	for _, p := range pieces {
		names = append(names, p.ScopeExternalID)
	}
	return names
}
