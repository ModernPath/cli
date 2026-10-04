package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------- pull

var pullApply bool

var factoryPullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Server-born answers -> workspace records; --apply records + acks (tracked job)",
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		return factoryPullRun(env, pullApply)
	},
}

func factoryPullRun(env *factoryEnv, apply bool) error {
	status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/intents?system_id=%d", env.SystemID), nil)
	if err != nil {
		return err
	}
	if status != 200 {
		return serverRefusal("", status, body)
	}
	intents, _ := dataOf(body)["intents"].([]any)
	if len(intents) == 0 {
		printSuccess("no pending intents — workspace and server agree")
		return nil
	}

	for _, it := range intents {
		m, _ := it.(map[string]any)
		if str(m, "kind") == "spec_updated" {
			state := "clean"
			if b, _ := m["conflict"].(bool); b {
				state = "CONFLICT (both sides changed)"
			}
			fmt.Printf("\n%s  server-edited spec v%v — %s\n", str(m, "external_id"), m["version"], state)
			continue
		}
		fmt.Printf("\n%s  answered %s (%s)\n  Q: %s\n  A: %s\n",
			str(m, "external_id"), str(m, "answered_at"), str(m, "source_tag"), str(m, "title"), str(m, "answer"))
	}
	if !apply {
		fmt.Printf("\n%d pending — record + echo with: modernpath factory pull --apply\n", len(intents))
		return nil
	}

	jsonlPath := filepath.Join(env.Root, "answers.jsonl")
	mdPath := filepath.Join(env.Root, "ANSWERS.md")

	for _, it := range intents {
		m, _ := it.(map[string]any)
		externalID := str(m, "external_id")

		// EPIC-SYNC-007: server-edited specs write back to their workspace
		// file; a divergence refuses and stays pending (SCN-SY-072).
		if str(m, "kind") == "spec_updated" {
			conflict, _ := m["conflict"].(bool)
			status, err := applySpecIntent(env.Root, externalID, str(m, "content"), str(m, "synced_sha"), conflict)
			if err != nil {
				return fmt.Errorf("spec write-back failed for %s: %w", externalID, err)
			}
			if status == "conflict" {
				fmt.Printf("✗ CONFLICT %s — both sides changed since the last sync; resolve by hand, then re-run\n", externalID)
				continue
			}
			aStatus, aBody, err := env.call("POST", "/api/v1/sync/spec-intents/ack",
				map[string]any{"system_id": env.SystemID, "external_id": externalID})
			if err != nil || aStatus != 200 {
				return fmt.Errorf("spec ack failed for %s: %v (%v)", externalID, serverRefusal("", aStatus, aBody), err)
			}
			printSuccess("pulled server spec edit → %s (marker reset to SPEC-DRAFT)", externalID)
			continue
		}

		if str(m, "kind") == "rdd_pending_intent" {
			application, err := applyRDDIntent(env, m, "")
			if err != nil {
				return err
			}
			printSuccess("applied %s (%s)", externalID, str(application, "application_revision"))
			continue
		}

		// SCN-SY-022: every apply is a tracked, gate-linked factory job
		jobID := ""
		jStatus, jBody, _ := env.call("POST", "/api/v1/sync/jobs", map[string]any{
			"system_id":           env.SystemID,
			"workspace_ref":       filepath.Base(env.Root),
			"machine_fingerprint": machineFingerprint(env.Root),
			"kind":                "apply_decision",
			"label":               "apply " + externalID + " answer",
			"gate_external_id":    externalID,
		})
		if jStatus == 200 {
			if job, ok := dataOf(jBody)["job"].(map[string]any); ok {
				jobID = str(job, "id")
			}
		}

		now := time.Now().UTC().Format(time.RFC3339)
		record, _ := json.Marshal(map[string]any{
			"type": "answer", "id": externalID, "title": str(m, "title"), "answer": str(m, "answer"),
			"source": str(m, "source_tag"), "origin": "server-downsync", "ts": now,
		})
		appendFile(jsonlPath, string(record)+"\n")
		appendFile(mdPath, fmt.Sprintf("- **%s** · **%s** — %s _(via factory pull)_\n", str(m, "source_tag"), externalID, str(m, "answer")))

		jobRef := "answers.jsonl@" + now
		if jobID != "" {
			jobRef = "factory_job:" + jobID
		}
		aStatus, aBody, err := env.call("POST", "/api/v1/sync/intents/"+externalID+"/ack",
			map[string]any{"system_id": env.SystemID, "applied_state": "applied", "job_ref": jobRef})
		if err != nil || aStatus != 200 {
			return fmt.Errorf("ack failed for %s: %v (%v)", externalID, serverRefusal("", aStatus, aBody), err)
		}
		if jobID != "" {
			_, _, _ = env.call("POST", "/api/v1/sync/jobs/"+jobID+"/finish", map[string]any{
				"system_id": env.SystemID, "status": "done",
				"result_summary": "answer recorded in answers.jsonl + ANSWERS.md, acked applied",
				"log_ref":        "answers.jsonl@" + now,
			})
		}
		printSuccess("applied + acked %s (%s)", externalID, jobRef)
	}
	return nil
}

// ---------------------------------------------------------------- spec down-sync (EPIC-SYNC-007)

// contentSha mirrors Core.Sync.content_sha/1 — sha256 hex of the exact bytes.
func contentSha(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// applySpecIntent writes a server-edited spec back to its workspace path
// (SCN-SY-071) unless the local file diverged from the last synced bytes or
// the server flagged a both-sides conflict — then it refuses, clobbering
// nothing (SCN-SY-072). A successful write resets the owning epic's
// specification-status marker to SPEC-DRAFT so the spec gate re-opens
// (SCN-SY-073). Returns "applied" or "conflict".
func applySpecIntent(root, externalID, content, syncedSha string, serverConflict bool) (string, error) {
	if serverConflict {
		return "conflict", nil
	}

	path := filepath.Join(root, filepath.FromSlash(externalID))
	local, err := os.ReadFile(path)
	if err == nil && contentSha(string(local)) != syncedSha && string(local) != content {
		// local edits the server never saw — a human resolves, we refuse
		return "conflict", nil
	}
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}

	resetSpecMarker(root, externalID)
	return "applied", nil
}

// resetSpecMarker flips the epic's `## Specification status` marker line to
// SPEC-DRAFT (an edited spec is an unapproved spec). Best-effort: a missing
// or markerless EPIC.md (legacy) is left alone.
func resetSpecMarker(root, externalID string) {
	specDir := filepath.Dir(filepath.FromSlash(externalID)) // .../specs
	epicPath := filepath.Join(root, filepath.Dir(specDir), "EPIC.md")
	raw, err := os.ReadFile(epicPath)
	if err != nil {
		return
	}
	lines := strings.Split(string(raw), "\n")
	inSection := false
	changed := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			inSection = trimmed == "## Specification status"
			continue
		}
		if inSection && (strings.HasPrefix(trimmed, "SPEC-APPROVED") || strings.HasPrefix(trimmed, "SPEC-READY")) {
			lines[i] = "SPEC-DRAFT — server-edited spec pulled " + time.Now().UTC().Format("2006-01-02") + "; re-approval required."
			changed = true
			break
		}
	}
	if changed {
		_ = os.WriteFile(epicPath, []byte(strings.Join(lines, "\n")), 0o644)
	}
}

func appendFile(path, content string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(content)
}
