// Package contract is the CLI's side of the sync contract handshake
// (REQ-CROSS-390, EPIC-CLI-019): the contract version this build speaks and
// the capabilities it implements, compared before every store write against
// what the server advertises, so a stale binary refuses by name instead of
// authoring a record the process no longer means.
package contract

import _ "embed"

// Version is the sync contract version this build speaks.
const Version = 1

// Implemented names every server-advertised capability this build honours.
// A name is added here in the same change that lands the behaviour it names;
// the server's map (CoreHttpApi.SyncContract) and the pinned fixture move
// together with it.
var Implemented = []string{
	"source_scoped_onboarding", // explicit mode and immutable inventory/run/group receipts
	"exact_candidate_set",      // typed requirement and selected-link decisions
	"advance_gate_ref",         // author advance names the gate it rides (EPIC-CLI-016)
	"backlog_record",           // author backlog / kind backlog (REQ-CROSS-393)
	"batch_multi_status",       // a sync batch reports and skips a failing op (REQ-CROSS-386)
	"edit_fingerprint",         // author update carries --expected-fingerprint (EPIC-CLI-007)
	"entry_gate_members",       // an entry gate names every member it moves (REQ-CROSS-371)
	"evidence_revision",        // factory evidence pins a revision (REQ-CROSS-378)
	"governed_answer_review",   // factory answer attaches the review a governed gate needs
	"review_context",           // a cold-review verdict carries its review context (EPIC-CLI-008)
	"selection_piece",          // working-set --piece names the held selection (REQ-CROSS-345)
	"trace_pin_default",        // a loop trace pins to the full aggregate (REQ-CROSS-376)
}

// Missing returns the required names this build does not implement.
func Missing(required []string) []string {
	have := map[string]bool{}
	for _, name := range Implemented {
		have[name] = true
	}
	var missing []string
	for _, name := range required {
		if !have[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

// PinnedCapabilities is the canonical capability map (contracts/sync/
// capabilities.json), embedded so a rename on either side fails a test.
//
//go:embed capabilities.json
var PinnedCapabilities []byte
