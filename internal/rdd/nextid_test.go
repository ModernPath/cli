package rdd

import "testing"

// REQ-CROSS-140: an id allocated from the ledger's maximum can land on one the
// server already governs. REQ-CROSS-137 was allocated that way while 095 and 096
// existed on the server in no branch or ref of the repository (RUN:2026-08-14).
func TestNextIDConsultsBothSides(t *testing.T) {
	ledger := []string{"REQ-CROSS-092", "REQ-CROSS-136", "REQ-CROSS-137"}
	server := []string{"REQ-CROSS-137", "REQ-CROSS-138", "REQ-CROSS-139"}

	got := NextID("CROSS", ledger, server)
	if got.Next != "REQ-CROSS-140" {
		t.Errorf("Next = %q, want REQ-CROSS-140 (server holds 139)", got.Next)
	}
	if got.LedgerMax != 137 || got.ServerMax != 139 {
		t.Errorf("maxima = ledger %d / server %d, want 137 / 139", got.LedgerMax, got.ServerMax)
	}
	if !got.ServerAhead {
		t.Error("ServerAhead must be true — allocating from the file alone would collide")
	}
}

// Offline is the common case in a fresh checkout, and it must not block work.
// It must say which basis it used, or a reader cannot tell a safe id from a
// guess.
func TestNextIDDegradesWhenServerUnknown(t *testing.T) {
	got := NextID("CROSS", []string{"REQ-CROSS-092"}, nil)
	if got.Next != "REQ-CROSS-093" {
		t.Errorf("Next = %q, want REQ-CROSS-093", got.Next)
	}
	if got.ServerAhead {
		t.Error("no server data is not the server being behind")
	}
	if got.Basis != "ledger only" {
		t.Errorf("Basis = %q, want %q", got.Basis, "ledger only")
	}
}

func TestNextIDIgnoresOtherContexts(t *testing.T) {
	got := NextID("CROSS", []string{"REQ-CROSS-001", "REQ-AGT-999"}, []string{"REQ-PLT-500"})
	if got.Next != "REQ-CROSS-002" {
		t.Errorf("Next = %q, want REQ-CROSS-002 — other contexts must not raise it", got.Next)
	}
}
