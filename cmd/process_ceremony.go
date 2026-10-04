package cmd

// Delivery ceremony facts and command registration. Each phase has its own implementation file.

import ()

// deliveryFacts is the additive `facts` block of the delivery-context read.
// A nil pointer means the server serves none (an undeployed server): every
// verb refuses before any write on nil (D14).
type deliveryFacts struct {
	Aggregate string `json:"aggregate"`
	Scope     struct {
		ExternalID string `json:"external_id"`
		Kind       string `json:"kind"`
		Status     string `json:"status"`
	} `json:"scope"`
	Members    []factMember `json:"members"`
	ColdReview struct {
		Verdict         string `json:"verdict"`
		Independent     bool   `json:"independent"`
		TraceExternalID string `json:"trace_external_id"`
		// REQ-CROSS-408: why the chosen verdict does not count, the open or
		// deferred material finding ids, and the scope's cold-review traces
		// pinned at other aggregates. nil on a server that predates them.
		IndependenceReason string   `json:"independence_reason"`
		OpenFindingIDs     []string `json:"open_finding_ids"`
		StaleTraces        []struct {
			ExternalID string `json:"external_id"`
			Aggregate  string `json:"aggregate"`
		} `json:"stale_traces"`
	} `json:"cold_review"`
	// REQ-CROSS-408: the completion facts with their reasons.
	Completion struct {
		DeliveredCurrentReconciled bool     `json:"delivered_current_reconciled"`
		GateAnswered               bool     `json:"gate_answered"`
		SettledGateExternalID      string   `json:"settled_gate_external_id"`
		MembersNotReviewed         []string `json:"members_not_reviewed"`
	} `json:"completion"`
	Sections struct {
		Complete bool     `json:"complete"`
		Missing  []string `json:"missing"`
		// REQ-CROSS-383: the canonical keys the plan check requires for the
		// scope, computed server-side from stored membership — the scaffold's
		// denominator. nil on a server that predates the key.
		Required []string `json:"required"`
	} `json:"sections"`
	EntryGate struct {
		Applied         bool   `json:"applied"`
		PinnedAggregate string `json:"pinned_aggregate"`
	} `json:"entry_gate"`
}

type factMember struct {
	ExternalID         string `json:"external_id"`
	Kind               string `json:"kind"`
	Status             string `json:"status"`
	ContentFingerprint string `json:"content_fingerprint"`
	EvidenceState      string `json:"evidence_state"`
	RedRecorded        bool   `json:"red_recorded"`
	LowerTracePass     bool   `json:"lower_trace_pass"`
	// The state the member's LIVE applied entry transition came from, or nil
	// when no live entry names it (a demotion retired it, or it never entered).
	// It is the fact behind an UNAVAILABLE evidence check and behind
	// `entry_origin_unavailable`, so a reason line can name the member.
	EntryOrigin *string `json:"entry_origin"`
	// REQ-CROSS-435: reopened by an applied defect demotion while no entry
	// gate names it — its lower trace returns it to review, so no remedy line
	// sends it to `process reenter`. False on a server that predates the key.
	DefectReopenWithoutEntry bool `json:"defect_reopen_without_entry"`
	// REQ-CROSS-459: whether the member's own entry is current at the
	// aggregate, by reconcile's own test (a live applied entry at the
	// aggregate, or a current lane entry). nil on a server that predates the
	// key: the verb then keeps the epic-level pin check.
	EntryCurrent *bool `json:"entry_current"`
}

func init() {
	processAdvanceCmd.Flags().BoolVar(&advanceAll, "all", false, "advance every system requirement of the --piece, reporting each one moved or why not")
}
