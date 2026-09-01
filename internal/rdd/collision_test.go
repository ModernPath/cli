package rdd

import "testing"

// REQ-CROSS-171: an id that already means something else on the server is a
// collision, not an update.
//
// Several branches sync to one system with no id coordination. Each such row is
// simply whoever synced last, with no conflict and no event marking the
// overwrite (process/86). Reconcile compares ids only, so it cannot see that two
// branches mean different requirements by REQ-CROSS-096 — it reports both sides
// as present and agreeing.
func TestCollisionsDetectDivergentMeaning(t *testing.T) {
	t.Run("same id, different requirement, is a collision", func(t *testing.T) {
		built := map[string]string{"REQ-CROSS-096": "A completion gate over nothing is dishonest"}
		served := map[string]string{"REQ-CROSS-096": "`modernpath status` names which sync it reports"}
		got := Collisions(built, served)
		if len(got) != 1 || got[0].ID != "REQ-CROSS-096" {
			t.Fatalf("got %v, want one collision on REQ-CROSS-096", got)
		}
		if got[0].Local == got[0].Served {
			t.Fatal("the report must carry both meanings — the point is that they differ")
		}
	})

	t.Run("same id, same requirement, is an ordinary update", func(t *testing.T) {
		built := map[string]string{"REQ-CROSS-096": "A completion gate over nothing is dishonest"}
		served := map[string]string{"REQ-CROSS-096": "A completion gate over nothing is dishonest"}
		if got := Collisions(built, served); len(got) != 0 {
			t.Fatalf("got %v, want none", got)
		}
	})

	t.Run("cosmetic differences are not collisions", func(t *testing.T) {
		// Titles are re-flowed and re-cased on the way through; a collision is a
		// different requirement, not different whitespace or a trailing period.
		built := map[string]string{"REQ-X-1": "A  completion   gate over nothing."}
		served := map[string]string{"REQ-X-1": "a completion gate over nothing"}
		if got := Collisions(built, served); len(got) != 0 {
			t.Fatalf("got %v — normalisation must absorb case, spacing and trailing punctuation", got)
		}
	})

	t.Run("an id on only one side is not a collision", func(t *testing.T) {
		// That is stranded-or-not-served, which Reconcile already reports.
		built := map[string]string{"REQ-X-1": "here"}
		served := map[string]string{"REQ-X-2": "there"}
		if got := Collisions(built, served); len(got) != 0 {
			t.Fatalf("got %v, want none", got)
		}
	})
}
