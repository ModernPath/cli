package cmd

import (
	"strings"
	"testing"
)

// REQ-CROSS-101 — a bare total reads as an approval backlog when almost none of
// it is approvals. The queue mixes sign-off with questions and product
// decisions: different work, different cadence, one number.
func openGate(id, kind string) any {
	return map[string]any{"external_id": id, "kind": kind, "title": id}
}

func mixedQueue() []any {
	return []any{
		openGate("Q-1", "question"), openGate("Q-2", "question"), openGate("Q-3", "question"),
		openGate("D-1", "decision"),
		openGate("A-1", "approval_request"), openGate("A-2", "approval_request"),
	}
}

func TestGateFooterNamesEachKindAndItsCount(t *testing.T) {
	footer := gateQueueFooter(6, gateKindBreakdown(mixedQueue()), "")

	for _, want := range []string{"3 question", "2 approval_request", "1 decision"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("the footer must break the queue down by kind — missing %q in %q", want, footer)
		}
	}
}

func TestGateFooterSaysWhatAFilterNarrowedFrom(t *testing.T) {
	footer := gateQueueFooter(2, gateKindBreakdown(mixedQueue()), "approval_request")

	if !strings.Contains(footer, "2") {
		t.Fatalf("the filtered count must be shown: %q", footer)
	}
	if !strings.Contains(footer, "6") {
		t.Fatalf("a filtered view must still name the unfiltered total, or it hides the queue: %q", footer)
	}
}

// The queue is not clear — it has 6 gates, none of the requested kind. Saying
// "clear" would be the same misreport one level down.
func TestGateFooterOnAKindWithNoOpenGatesDoesNotReadAsAClearQueue(t *testing.T) {
	footer := gateQueueFooter(0, gateKindBreakdown(mixedQueue()), "roadblock")

	if strings.Contains(footer, "queue is clear") {
		t.Fatalf("an empty filter result must not claim the queue is clear: %q", footer)
	}
	if !strings.Contains(footer, "roadblock") {
		t.Fatalf("it must name the kind that matched nothing: %q", footer)
	}
	if !strings.Contains(footer, "6") {
		t.Fatalf("it must still say how many are actually waiting: %q", footer)
	}
}

func TestGatesCommandOffersAKindFlag(t *testing.T) {
	if factoryGatesCmd.Flags().Lookup("kind") == nil {
		t.Fatal("the approval queue can only be isolated by grepping — there is no --kind flag")
	}
}

// Guards — these describe the filter helper written with the seam, so they are
// expected green from the start. They are what stop the filter from silently
// dropping gates or from turning an unfiltered call into a filtered one.
func TestFilterWithNoKindReturnsTheWholeQueue(t *testing.T) {
	if got := filterGatesByKind(mixedQueue(), ""); len(got) != 6 {
		t.Fatalf("an unfiltered call must return every gate, got %d", len(got))
	}
}

func TestFilterReturnsOnlyTheRequestedKind(t *testing.T) {
	got := filterGatesByKind(mixedQueue(), "approval_request")
	if len(got) != 2 {
		t.Fatalf("want the 2 approval requests, got %d", len(got))
	}
	for _, g := range got {
		if g.(map[string]any)["kind"] != "approval_request" {
			t.Fatalf("filter leaked another kind: %v", g)
		}
	}
}

func TestBreakdownOrderIsStable(t *testing.T) {
	a := gateKindBreakdown(mixedQueue())
	b := gateKindBreakdown(mixedQueue())
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("breakdown order is not stable: %v vs %v", a, b)
		}
	}
	if a[0].kind != "question" || a[0].n != 3 {
		t.Fatalf("largest kind must lead: %v", a)
	}
}
