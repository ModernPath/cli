package cmd

// REQ-CROSS-446 (EPIC-CLI-TURNS): `process next` with several held pieces and
// no --piece prints one block per piece — scope, phase, why, the skill to run
// and the gates waiting on it — and names the --piece remedy, instead of
// refusing. Reads: the held-work read, one delivery-context read per piece and
// one open-gates read; any that fails is named in its block, never fatal.

import (
	"fmt"
	"slices"
	"strings"
)

func printHeldPiecesSummary(env *factoryEnv, pieces []string) {
	rows := map[string]heldPiece{}
	if held, ok := readHeldPieces(env); ok {
		for _, p := range held {
			rows[p.ScopeExternalID] = p
		}
	}
	gates, gatesErr := fetchList(env, fmt.Sprintf("/api/v1/sync/gates?system_id=%d", env.SystemID), "gates")

	fmt.Printf("you hold %d current pieces — one block each. The scoped verbs (process check, advance, enter, complete, findings list, review record) need --piece <id> to name one.\n",
		len(pieces))
	for _, id := range pieces {
		fmt.Println()
		row := rows[id]
		if row.ScopeKind != "" {
			fmt.Printf("%s (%s)\n", id, row.ScopeKind)
		} else {
			fmt.Println(id)
		}
		resp, err := readDeliveryContextFor(env, id)
		if err != nil {
			fmt.Printf("  unreadable:     %v\n", err)
		} else {
			d := resp.Data
			div := ""
			if d.Divergence {
				div = "  ⚠ diverges"
			}
			fmt.Printf("  phase:          derived %s · declared %s%s\n", orNone(d.DerivedPhase), orNone(d.DeclaredPhase), div)
			if d.DerivedReason != "" {
				fmt.Printf("  why:            %s\n", d.DerivedReason)
			}
			if d.Skill != "" {
				fmt.Printf("  run:            %s\n", d.Skill)
			}
			if d.Lane == "defect" {
				fmt.Println("  lane:           customer-blocking defect")
			}
		}
		switch waiting := gatesNaming(gates, id); {
		case gatesErr != nil:
			fmt.Printf("  waiting gates:  unreadable (%v)\n", gatesErr)
		case len(waiting) == 0:
			fmt.Println("  waiting gates:  none")
		default:
			fmt.Printf("  waiting gates:  %s\n", strings.Join(waiting, "; "))
		}
		if deltas := row.deltas(); len(deltas) > 0 {
			fmt.Printf("  moved:          %s\n", strings.Join(deltas, " · "))
		}
	}
	fmt.Printf("\nroute one: process next --piece <id> (%s)\n", strings.Join(pieces, ", "))
}

// gatesNaming renders the open gates whose exact scope names the piece.
func gatesNaming(gates []any, piece string) []string {
	var out []string
	for _, g := range gates {
		gm, _ := g.(map[string]any)
		scope, _ := normalizeScopeTokens(gm["exact_scope"])
		if !slices.Contains(scope, piece) {
			continue
		}
		line := str(gm, "external_id")
		if state := str(gm, "state"); state != "" {
			line += " (" + state + ")"
		}
		if title := str(gm, "title"); title != "" {
			line += " — " + title
		}
		out = append(out, line)
	}
	return out
}
