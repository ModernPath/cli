package cmd

// A dismissed historical gate is a decision, not an empty slot for the
// corpus's open gate. The only supported import exception is an exact,
// fingerprinted manifest that an authenticated human answered in the store.

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/modernpath/cli/internal/rdd"
)

type migrationGateResidue struct {
	ID, CorpusState, StoreState, StoreFingerprint string
}

func readMigrationGateResidue(path string, systemID int) ([]migrationGateResidue, string, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", "", fmt.Errorf("migration gate residue manifest: %w", err)
	}
	content := string(raw)
	if content == "" || !strings.HasSuffix(content, "\n") {
		return nil, "", "", fmt.Errorf("migration gate residue manifest must be nonempty and end in a newline")
	}
	var rows []migrationGateResidue
	declaredSystem, declaredCount := -1, -1
	for _, line := range strings.Split(strings.TrimSuffix(content, "\n"), "\n") {
		if strings.HasPrefix(line, "# system_id=") {
			declaredSystem, err = strconv.Atoi(strings.TrimPrefix(line, "# system_id="))
			if err != nil {
				return nil, "", "", fmt.Errorf("invalid residue system_id: %w", err)
			}
			continue
		}
		if strings.HasPrefix(line, "# review_count=") {
			declaredCount, err = strconv.Atoi(strings.TrimPrefix(line, "# review_count="))
			if err != nil {
				return nil, "", "", fmt.Errorf("invalid residue review_count: %w", err)
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 4 || parts[0] == "" || parts[1] != "open" || parts[2] != "dismissed" || len(parts[3]) != 64 {
			return nil, "", "", fmt.Errorf("invalid residue row %q: require ID, open, dismissed, 64-character store fingerprint", line)
		}
		if len(rows) > 0 && parts[0] <= rows[len(rows)-1].ID {
			return nil, "", "", fmt.Errorf("migration gate residue rows must be sorted and unique: %s", parts[0])
		}
		rows = append(rows, migrationGateResidue{parts[0], parts[1], parts[2], parts[3]})
	}
	if declaredSystem != systemID || declaredCount != len(rows) || len(rows) == 0 {
		return nil, "", "", fmt.Errorf("residue manifest scope/count mismatch: system %d (bound %d), declared %d (rows %d)", declaredSystem, systemID, declaredCount, len(rows))
	}
	return rows, fmt.Sprintf("%x", sha256.Sum256(raw)), content, nil
}

// validatedMigrationGateResidue returns the ONLY gate ops that may be omitted
// from a sync batch. All the other ops still post, and all gate fields other
// than state still have to match the current store before and after the run.
func validatedMigrationGateResidue(env *factoryEnv, ops []rdd.Op, manifestPath, gateID string) (map[string]migrationGateResidue, error) {
	if manifestPath == "" && gateID == "" {
		return nil, nil
	}
	if manifestPath == "" || gateID == "" {
		return nil, fmt.Errorf("migration gate residue requires both --residue-manifest and --residue-gate")
	}
	rows, digest, content, err := readMigrationGateResidue(manifestPath, env.SystemID)
	if err != nil {
		return nil, err
	}
	status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/gates/%s?system_id=%d", url.PathEscape(gateID), env.SystemID), nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("residue gate %s was not readable (status %d)", gateID, status)
	}
	gate, ok := dataOf(body)["gate"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("residue gate %s did not serve a gate record", gateID)
	}
	if str(gate, "external_id") != gateID || str(gate, "purpose") != "migration_residue" || str(gate, "gate_class") != "human" || str(gate, "state") != "answered" || str(gate, "answerer_kind") != "human" {
		return nil, fmt.Errorf("residue gate %s must be the exact answered, human migration_residue decision", gateID)
	}
	if !strings.HasPrefix(str(gate, "source_tag"), "USER:") || !strings.Contains(str(gate, "body_md"), "SHA-256: "+digest) || !strings.Contains(str(gate, "body_md"), content) {
		return nil, fmt.Errorf("residue gate %s does not carry this exact manifest, digest and USER: answer source", gateID)
	}
	chosen, _ := gate["chosen_option_keys"].([]any)
	if len(chosen) != 1 || chosen[0] != "preserve_dismissed" || str(gate, "answer") == "" {
		return nil, fmt.Errorf("residue gate %s was not answered preserve_dismissed", gateID)
	}
	scope, _ := gate["exact_scope"].([]any)
	wantScope := fmt.Sprintf("system:%d", env.SystemID)
	if len(scope) != 1 || scope[0] != wantScope {
		return nil, fmt.Errorf("residue gate %s must name only %s", gateID, wantScope)
	}
	items, err := fetchList(env, fmt.Sprintf("/api/v1/sync/gates?system_id=%d&state=all", env.SystemID), "gates")
	if err != nil {
		return nil, fmt.Errorf("cannot re-read store gates before import: %w", err)
	}
	served := map[string]map[string]any{}
	for _, item := range items {
		if row, ok := item.(map[string]any); ok {
			served[str(row, "external_id")] = row
		}
	}
	emitted := map[string]map[string]any{}
	for _, op := range ops {
		if op.Type == "upsert_gate" {
			emitted[str(op.Payload, "external_id")] = op.Payload
		}
	}
	approved := map[string]migrationGateResidue{}
	for _, row := range rows {
		got, sent := served[row.ID], emitted[row.ID]
		if got == nil || sent == nil || str(got, "state") != row.StoreState || str(sent, "state") != row.CorpusState || str(got, "fingerprint") != row.StoreFingerprint {
			return nil, fmt.Errorf("residue %s no longer matches the approved corpus/store states and fingerprint", row.ID)
		}
		for field, want := range sent {
			if field == "state" || field == "external_id" || field == "content_hash" || field == "actor" {
				continue
			}
			if actual, has := got[field]; has && !payloadRoundTrips(want, actual, "upsert_gate."+field, &skipCollector{}) {
				return nil, fmt.Errorf("residue %s.%s differs beyond the accepted state field", row.ID, field)
			}
		}
		approved[row.ID] = row
	}
	var actualDrift []string
	for id, sent := range emitted {
		if got := served[id]; got != nil && str(got, "state") != str(sent, "state") {
			actualDrift = append(actualDrift, id)
		}
	}
	if len(actualDrift) != len(approved) {
		sort.Strings(actualDrift)
		return nil, fmt.Errorf("gate-state differences changed since human review: now %d, approved %d: %s", len(actualDrift), len(approved), strings.Join(capList(actualDrift, 20), ", "))
	}
	for _, id := range actualDrift {
		if _, ok := approved[id]; !ok {
			return nil, fmt.Errorf("unapproved gate-state difference %s", id)
		}
	}
	printInfo("residue gate %s: %d exact dismissed decisions accepted; manifest SHA-256 %s\n", gateID, len(approved), digest)
	return approved, nil
}

func omitPreservedGateOps(ops []map[string]any, approved map[string]migrationGateResidue) []map[string]any {
	if len(approved) == 0 {
		return ops
	}
	kept := make([]map[string]any, 0, len(ops)-len(approved))
	for _, op := range ops {
		payload, _ := op["payload"].(map[string]any)
		if migrateOpKind(op) == "upsert_gate" {
			if _, ok := approved[str(payload, "external_id")]; ok {
				continue
			}
		}
		kept = append(kept, op)
	}
	return kept
}
