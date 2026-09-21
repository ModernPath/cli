package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modernpath/cli/internal/storeback"
)

// REQ-CROSS-406 (EPIC-CLI-020): `install --store-backed --source USER:…`
// declares a bound workspace with no file ledgers store-backed in one step.
// The server records the activation gate born answered by the signed-in
// person and sets the system's process-store state active in one action;
// the CLI then writes the marker through the writer `migrate flip` uses,
// with an empty retired list. A workspace that has ledgers goes through the
// flip, which re-verifies the import first.

// writeStoreBackedMarker writes process/store-backed.md — the one shape both
// the flip and the one-step declaration produce: the heading, the read/write
// sentences, the accepted source with its gate, the server and system, and
// one `retired:` line per retired ledger family (none for a system that never
// had ledgers).
func writeStoreBackedMarker(root, apiURL, sourceTag, gateRef string, systemID int, families []string) error {
	var b strings.Builder
	b.WriteString("# Store-backed declaration\n\n")
	b.WriteString("This workspace's process store is the server. The files below are\n")
	b.WriteString("retired: read state via `modernpath working-set pull` and `your-move`,\n")
	b.WriteString("write via `modernpath author`. The dual-authority guard\n")
	b.WriteString("(scripts/check-store-backed.sh) gates on this list.\n\n")
	fmt.Fprintf(&b, "- **Accepted:** %s (gate %s)\n", sourceTag, gateRef)
	fmt.Fprintf(&b, "- **Server:** %s · system %d\n\n", apiURL, systemID)
	for _, p := range families {
		fmt.Fprintf(&b, "retired: %s\n", p)
	}
	markerPath := filepath.Join(root, storeback.MarkerRel)
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(markerPath, []byte(b.String()), 0o644)
}

// declareStoreBacked runs the declaration for the workspace at root, before the
// kit install that follows withholds the ledger skill under the marker. It
// reads the server's state first and completes whichever half is missing:
// both (a fresh system), the server half (a hand-written marker), or the
// workspace half (a declaration recorded from another checkout). Both done
// is reported as already declared.
func declareStoreBacked(root, source string) error {
	if !strings.HasPrefix(source, "USER:") {
		return fmt.Errorf("install --store-backed declares a decision — pass --source USER:<date>:<who decided and why>")
	}
	if hasProcessRecords(root) {
		return fmt.Errorf("this workspace has file ledgers under tasks/ — a workspace with ledgers is declared store-backed by `modernpath migrate flip`, which re-verifies the import first; --store-backed is for a system that never had them")
	}
	env, err := factoryEnvLoad()
	if err != nil {
		return err
	}
	if env.Root != root {
		return fmt.Errorf("run `install --store-backed` from the workspace root %s — the marker and the kit are written there", env.Root)
	}

	status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/store-backed?system_id=%d", env.SystemID), nil)
	if err != nil {
		return err
	}
	if status != 200 {
		return serverRefusal("store-backed state read refused", status, body)
	}
	served, _ := dataOf(body)["process_store"].(map[string]any)
	state, _ := served["state"].(string)
	markerPresent := storeback.Active(root)

	switch {
	case state == "active" && markerPresent:
		fmt.Printf("store-backed: already declared — server state active (gate %s), marker %s present; nothing to do\n",
			str(served, "gate_ref"), storeback.MarkerRel)
		return nil

	case state == "active":
		// The server half was recorded elsewhere (another checkout, or a flip
		// whose marker write failed): only the workspace half is missing.
		if err := writeStoreBackedMarker(root, env.APIURL, str(served, "source_tag"), str(served, "gate_ref"), env.SystemID, nil); err != nil {
			return err
		}
		fmt.Printf("store-backed: the server already held the declaration (gate %s) — completed the workspace half: wrote %s\n",
			str(served, "gate_ref"), storeback.MarkerRel)
		return nil
	}

	// nil, seeded or cleared: the server decides; seeded and cleared refuse by
	// name and the refusal is surfaced verbatim.
	data, err := authorPost(env, map[string]any{"action": "store_backed_declare", "source": source})
	if err != nil {
		return err
	}
	decl, _ := data["store_backed_declaration"].(map[string]any)
	gateRef, sourceTag := str(decl, "gate_ref"), str(decl, "source_tag")
	if markerPresent {
		fmt.Printf("store-backed: the marker %s was already present — completed the server half: gate %s born answered (%s), state active\n",
			storeback.MarkerRel, gateRef, sourceTag)
		return nil
	}
	if err := writeStoreBackedMarker(root, env.APIURL, sourceTag, gateRef, env.SystemID, nil); err != nil {
		return err
	}
	printSuccess("store-backed: declared — gate %s born answered (%s), server state active, marker %s written with no retired files\n",
		gateRef, sourceTag, storeback.MarkerRel)
	return nil
}
