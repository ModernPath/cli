package rdd

import (
	"fmt"
	"regexp"
	"strconv"
)

// Allocation is the next free requirement id and the evidence behind it.
type Allocation struct {
	Next      string // the id to use
	LedgerMax int    // highest seen in the workspace's ledgers (0 = none)
	ServerMax int    // highest the server serves (0 = none, or unknown)
	// ServerAhead is true when the server governs an id the ledgers do not.
	// This is the collision case: allocating from the file alone lands on a row
	// that already exists and that no local query can see.
	ServerAhead bool
	// Basis says what the answer rests on, because an offline allocation is a
	// weaker claim than an online one and a reader cannot tell them apart from
	// the id alone.
	Basis string
}

var idTailRe = regexp.MustCompile(`^REQ-([A-Z]+)-(\d+)$`)

// NextID returns the next free id for a context, consulting both the ledgers and
// whatever the server serves.
//
// REQ-CROSS-140: taking the ledger's maximum and adding one is the obvious
// approach and it is wrong — the ledger file is not the only writer. Sync is
// deliberately not a replace-set, so the server can hold rows no checkout has
// ever contained.
//
// A nil server list means "not consulted", not "the server has nothing": the
// distinction is the whole reason Basis exists.
func NextID(context string, ledger, server []string) Allocation {
	maxOf := func(ids []string) int {
		best := 0
		for _, id := range ids {
			m := idTailRe.FindStringSubmatch(id)
			if m == nil || m[1] != context {
				continue
			}
			if n, err := strconv.Atoi(m[2]); err == nil && n > best {
				best = n
			}
		}
		return best
	}

	a := Allocation{LedgerMax: maxOf(ledger), Basis: "ledger only"}
	if server != nil {
		a.ServerMax = maxOf(server)
		a.Basis = "ledger + server"
		a.ServerAhead = a.ServerMax > a.LedgerMax
	}

	high := a.LedgerMax
	if a.ServerMax > high {
		high = a.ServerMax
	}
	a.Next = fmt.Sprintf("REQ-%s-%03d", context, high+1)
	return a
}
