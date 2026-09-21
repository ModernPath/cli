package cmd

// REQ-CROSS-377 (EPIC-CLI-017): a transition is given as --from and --to,
// inferred from the purpose when omitted, and never recorded as a fragment. An
// unquoted arrow is a shell redirect: the CLI once accepted the truncated
// value, printed success, and wrote it onto an immutable trace.

import (
	"errors"
	"strings"
	"testing"
)

func TestAuthorTraceComposesFromTo(t *testing.T) {
	got, err := resolveTransition("trace", "lower", "", "build", "verify")
	if err != nil || got != "build->verify" {
		t.Fatalf("--from build --to verify must compose build->verify, got %q, %v", got, err)
	}
}

func TestAuthorTraceRefusesAFragmentBeforeWriting(t *testing.T) {
	// "a->b->c" is nit 6 of the PR #452 review: a pair has exactly one arrow.
	for _, fragment := range []string{"build-", "build", "->verify", "build->", "a->b->c"} {
		got, err := resolveTransition("trace", "lower", fragment, "", "")
		if err == nil {
			t.Fatalf("--transition %q must be refused, got %q", fragment, got)
		}
		if !strings.Contains(err.Error(), "FROM->TO") || !strings.Contains(err.Error(), fragment) {
			t.Fatalf("the refusal must name the expected form and the value: %v", err)
		}
	}
}

func TestAuthorTraceAcceptsAWellFormedTransitionFlag(t *testing.T) {
	for _, ok := range []string{"plan->entry", "PROPOSED -> TODO"} {
		got, err := resolveTransition("trace", "cold-review", ok, "", "")
		if err != nil || got != ok {
			t.Fatalf("a well-formed --transition is recorded as given, got %q, %v", got, err)
		}
	}
}

func TestAuthorTraceInfersTheTransitionFromThePurpose(t *testing.T) {
	cases := map[string]string{
		"cold-review": "plan->entry",
		"entry":       "PROPOSED->TODO",
		"lower":       "build->verify",
		"completion":  "IN_REVIEW->DONE",
	}
	for purpose, want := range cases {
		got, err := resolveTransition("trace", purpose, "", "", "")
		if err != nil || got != want {
			t.Fatalf("purpose %s must infer %s, got %q, %v", purpose, want, got, err)
		}
	}
	_, err := resolveTransition("trace", "custom", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "--from") {
		t.Fatalf("an uninferable trace purpose must name --from/--to as the remedy, got %v", err)
	}
}

func TestAuthorGateInfersEntryTransition(t *testing.T) {
	got, err := resolveTransition("gate", "entry", "", "", "")
	if err != nil || got != "PROPOSED->TODO" {
		t.Fatalf("an entry gate infers PROPOSED->TODO, got %q, %v", got, err)
	}
	got, err = resolveTransition("gate", "completion", "", "", "")
	if err != nil || got != "IN_REVIEW->DONE" {
		t.Fatalf("a completion gate infers IN_REVIEW->DONE, got %q, %v", got, err)
	}
	// A question gate declares no transition — that stays legal and empty.
	got, err = resolveTransition("gate", "question", "", "", "")
	if err != nil || got != "" {
		t.Fatalf("an ungoverned gate keeps an empty transition, got %q, %v", got, err)
	}
}

func TestAuthorTraceRefusesMixedTransitionFlags(t *testing.T) {
	if _, err := resolveTransition("trace", "lower", "build->verify", "build", "verify"); err == nil {
		t.Fatal("--transition together with --from/--to must be refused")
	}
	if _, err := resolveTransition("trace", "lower", "", "build", ""); err == nil {
		t.Fatal("--from without --to must be refused")
	}
}

// N-CLI017-R2 / N-CLI017-R1-06: the inferred entry source is PROPOSED; when the
// server refuses because the scope is not in that FROM state, the rendering
// hints the as-built source instead of leaving the operator to guess.
func TestAuthorGateHintsPendingVerificationOnAnInferredEntryRefusal(t *testing.T) {
	refusal := errors.New("author refused (server 422): exact_scope: REQ-X are not in the transition's FROM state (PROPOSED)")
	got := withEntrySourceHint(refusal, true)
	if !strings.Contains(got.Error(), "--from PENDING_VERIFICATION --to TODO") {
		t.Fatalf("the refusal must hint the as-built entry source: %v", got)
	}
	if got := withEntrySourceHint(refusal, false); strings.Contains(got.Error(), "PENDING_VERIFICATION") {
		t.Fatalf("an explicit transition gets no hint: %v", got)
	}
}
