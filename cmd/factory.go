package cmd

// factory — the Mission Control / workspace-sync verbs (REQ-CROSS-011,
// SERVER-SYNC-DESIGN §2.3), folded into the product CLI per USER:2026-07-23
// ("let's not reinvent the wheel"). Auth and binding are the CLI's own:
// `.modernpath/config.json` (APIURL + SystemID) and `.modernpath/auth.json`
// (Bearer), with a gitignored `.modernpath/mp_api_key` fallback (X-API-Key)
// for headless/dev use. Ledger parsing stays in the workspace's tested
// Node op-builder (`mission-control/cli/ops-dump.js`) — the CLI shells to it
// for op JSON and owns everything else.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/manifest"
	"github.com/modernpath/cli/internal/opschema"
	"github.com/modernpath/cli/internal/rdd"
	"github.com/spf13/cobra"
)

var factoryCmd = &cobra.Command{
	Use:     "factory",
	Aliases: []string{"mc"},
	Short:   "Mission Control: sync the workspace, answer gates, track the loop",
	Long: `The workspace<->server datasync and decision surface (Mission Control).

Bind once with 'modernpath factory connect --system <id>', then:
  release   select the current release; sync stamps + scopes to it
  sync      push requirements/epics/gates/events (idempotent, hash-diffed)
  gates     the open decision queue
  answer    record a USER: decision (first-wins on the server)
  pull      server-born answers -> workspace records (--apply acks the echo)
  evidence  post a test/CI run as evidence
  drift     compare evidence shas to the working tree (Done decays)
  watch     the daemon: sync + heartbeat + pull --apply on a cadence`,
}

// ---------------------------------------------------------------- plumbing

type factoryEnv struct {
	Root           string // workspace root (parent of .modernpath)
	APIURL         string
	SystemID       int
	CurrentRelease string // REQ-CROSS-017: the envelope release stamp ("" = unscoped)
	token          string
	isAPIKey       bool
}

func factoryEnvLoad() (*factoryEnv, error) {
	cfgDir, err := config.FindConfigDir()
	if err != nil || cfgDir == "" {
		return nil, fmt.Errorf("not connected — run 'modernpath factory connect --system <id>' in the workspace root")
	}

	cfg, err := config.ReadConfig()
	if err != nil {
		return nil, err
	}
	if cfg.SystemID == 0 {
		return nil, fmt.Errorf("no system_id in %s/config.json — run 'modernpath factory connect --system <id>'", config.ConfigDir)
	}

	env := &factoryEnv{
		Root:           filepath.Dir(cfgDir),
		APIURL:         cfg.APIURL,
		SystemID:       cfg.SystemID,
		CurrentRelease: cfg.CurrentRelease,
	}
	if env.APIURL == "" {
		env.APIURL = config.DefaultAPIURL
	}

	// the CLI's own auth first; the gitignored dev API key as fallback
	if auth, err := config.ReadAuth(); err == nil && auth.Token != "" {
		env.token = auth.Token
	} else if key, err := os.ReadFile(filepath.Join(cfgDir, "mp_api_key")); err == nil {
		env.token = strings.TrimSpace(string(key))
		env.isAPIKey = true
	} else {
		return nil, fmt.Errorf("no credentials — 'modernpath auth' or put a platform API key in %s/mp_api_key", config.ConfigDir)
	}
	return env, nil
}

func (e *factoryEnv) call(method, apiPath string, payload any) (int, map[string]any, error) {
	var body *bytes.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(raw)
	} else {
		body = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, e.APIURL+apiPath, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.isAPIKey {
		req.Header.Set("X-API-Key", e.token)
	} else {
		req.Header.Set("Authorization", "Bearer "+e.token)
	}

	resp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	var decoded map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	return resp.StatusCode, decoded, nil
}

// dumpOps shells to the workspace's Node op-builder — the ledger parsing
// stays in one tested place instead of a second Go implementation.
func (e *factoryEnv) dumpOps(args ...string) (map[string]any, error) {
	script := filepath.Join(e.Root, "mission-control", "cli", "ops-dump.js")
	if _, err := os.Stat(script); err != nil {
		return nil, fmt.Errorf("this workspace has no mission-control extractor (%s missing) — factory sync runs only in sync-enabled workspaces", script)
	}

	out, err := exec.Command("node", append([]string{script}, args...)...).Output()
	if err != nil {
		return nil, fmt.Errorf("ops-dump failed (is node installed?): %w", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		return nil, fmt.Errorf("ops-dump returned invalid JSON: %w", err)
	}
	return decoded, nil
}

// factoryLegacyExtractor: shell to the workspace's node op-builder instead of
// the CLI-bundled parsers (the transition escape hatch, REQ-CROSS-013).
var factoryLegacyExtractor bool

// workspaceOps builds the sync op batch. Default: the CLI-bundled parsers
// driven by .modernpath/manifest.json (defaults = the modernpath-v1 layout —
// REQ-CROSS-013; no mission-control/ copy needed). Ops are validated against
// the vendored op schema before they can reach the wire (REQ-CROSS-012).
// warnings carries the loud mandated-gap report (D2: never a silent skip).
func (e *factoryEnv) workspaceOps() (ops []map[string]any, warnings []string, err error) {
	if factoryLegacyExtractor {
		dump, err := e.dumpOps()
		if err != nil {
			return nil, nil, err
		}
		opsAny, _ := dump["ops"].([]any)
		for _, o := range opsAny {
			if m, ok := o.(map[string]any); ok {
				ops = append(ops, m)
			}
		}
		return ops, nil, nil
	}

	m, fromFile, err := manifest.Load(e.Root)
	if err != nil {
		return nil, nil, err
	}
	if !fromFile {
		// zero-config: defaults match this workspace's layout
		m = manifest.Default()
	}
	data, warnings := rdd.Snapshot(e.Root, m)
	built := rdd.BuildOps(data, func(rel string) string { return rdd.ReadEpicRecord(e.Root, rel) },
		time.Now().UTC().Format("2006-01-02"))
	for _, op := range built {
		ops = append(ops, map[string]any{"type": op.Type, "payload": op.Payload})
	}
	if err := opschema.ValidateOps(ops); err != nil {
		return nil, warnings, fmt.Errorf("built ops fail schema v%d validation: %w", opschema.SchemaVersion, err)
	}
	return ops, warnings, nil
}

// workspaceTracePaths returns each requirement's traced file paths (drift's
// diff scope) from the same extraction path sync uses.
func (e *factoryEnv) workspaceTracePaths() (map[string][]string, error) {
	if factoryLegacyExtractor {
		dump, err := e.dumpOps("--trace-paths")
		if err != nil {
			return nil, err
		}
		tracePaths, _ := dump["tracePaths"].(map[string]any)
		out := map[string][]string{}
		for id, pathsAny := range tracePaths {
			list, _ := pathsAny.([]any)
			for _, p := range list {
				if s, ok := p.(string); ok {
					out[id] = append(out[id], s)
				}
			}
		}
		return out, nil
	}

	m, _, err := manifest.Load(e.Root)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	data, _ := rdd.Snapshot(e.Root, m)
	for _, req := range data.Reqs {
		if paths := rdd.ExtractTracePaths(req.Detail); len(paths) > 0 {
			out[req.ID] = paths
		}
	}
	return out, nil
}

func printGapWarnings(warnings []string) {
	for _, w := range warnings {
		printWarning("%s", w)
	}
}

func dataOf(decoded map[string]any) map[string]any {
	if d, ok := decoded["data"].(map[string]any); ok {
		return d
	}
	return map[string]any{}
}

func str(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func machineFingerprint(root string) string {
	host, _ := os.Hostname()
	return host + ":" + root
}

func gitOut(root string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ---------------------------------------------------------------- connect / status

var factoryConnectSystem int

var factoryConnectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Bind this workspace to a System (writes .modernpath/config.json)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if factoryConnectSystem == 0 {
			return fmt.Errorf("usage: modernpath factory connect --system <id> [--api-url <url>]")
		}
		cfg, _ := config.ReadConfig()
		if cfg == nil {
			cfg = &config.Config{}
		}
		cfg.SystemID = factoryConnectSystem
		if apiURL != "" {
			cfg.APIURL = apiURL
		}
		if cfg.APIURL == "" {
			cfg.APIURL = config.LocalAPIURL
		}
		if err := config.WriteConfig(cfg); err != nil {
			return err
		}
		printSuccess("connected: system %d via %s (.modernpath/config.json)", cfg.SystemID, cfg.APIURL)
		return nil
	},
}

var factoryStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the binding, credentials source, and pending op count",
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		source := "auth.json (bearer)"
		if env.isAPIKey {
			source = ".modernpath/mp_api_key (X-API-Key)"
		}
		release := env.CurrentRelease
		if release == "" {
			release = "(none — sync runs unscoped; set one with 'factory release use <slug>')"
		}
		fmt.Printf("workspace: %s\nserver:    %s\nsystem:    %d\nrelease:   %s\nauth:      %s\n", env.Root, env.APIURL, env.SystemID, release, source)

		if ops, warnings, err := env.workspaceOps(); err == nil {
			printGapWarnings(warnings)
			fmt.Printf("pending:   %d ops on next sync (server hash-diffs; unchanged ops are no-ops)\n", len(ops))
		}
		if _, fromFile, err := manifest.Load(env.Root); err == nil {
			if fromFile {
				fmt.Printf("manifest:  %s\n", manifest.Path(env.Root))
			} else {
				fmt.Println("manifest:  defaults (modernpath-v1 layout; write one with 'factory manifest init')")
			}
		}
		return nil
	},
}

// ---------------------------------------------------------------- release

// factory release use|show|clear — the workspace-level release selector
// (REQ-CROSS-017). Writes only local config; the server materializes the
// release (find-or-create by slug) on the first scoped sync. The tracked
// source of truth is process/releases.md — keep the two in step.
var factoryReleaseCmd = &cobra.Command{
	Use:   "release",
	Short: "Select the current release factory sync stamps and scopes to",
}

var factoryReleaseUseCmd = &cobra.Command{
	Use:   "use <slug>",
	Short: "Set current_release in .modernpath/config.json (e.g. modernpath-v1-09)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		slug := strings.TrimSpace(args[0])
		if slug == "" {
			return fmt.Errorf("usage: modernpath factory release use <slug>")
		}
		cfg, err := config.ReadConfig()
		if err != nil {
			return err
		}
		cfg.CurrentRelease = slug
		if err := config.WriteConfig(cfg); err != nil {
			return err
		}
		printSuccess("current release: %s (.modernpath/config.json — mirror of the active row in process/releases.md)", slug)
		return nil
	},
}

var factoryReleaseShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the current release",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.ReadConfig()
		if err != nil {
			return err
		}
		if cfg.CurrentRelease == "" {
			fmt.Println("(none — sync runs unscoped; set one with 'factory release use <slug>')")
		} else {
			fmt.Println(cfg.CurrentRelease)
		}
		return nil
	},
}

var factoryReleaseClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Unset the current release (sync runs unscoped; existing stamps stay)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.ReadConfig()
		if err != nil {
			return err
		}
		cfg.CurrentRelease = ""
		if err := config.WriteConfig(cfg); err != nil {
			return err
		}
		printSuccess("current release cleared — sync runs unscoped (server stamps are left untouched)")
		return nil
	},
}

// ---------------------------------------------------------------- sync

var factorySyncDryRun bool
var factorySyncJSON bool

var (
	factorySyncIfQuiescent bool
	factorySyncTrigger     string
	factorySyncMinInterval time.Duration
)

var factorySyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Push the workspace state: typed op batch + projections + heartbeat",
	RunE: func(cmd *cobra.Command, args []string) error {
		// EPIC-SYNC-009: the hook path — gated, logged, never errors out
		if factorySyncIfQuiescent {
			return factorySyncQuiescent(factorySyncTrigger, factorySyncMinInterval)
		}

		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		return factorySyncRun(env, factorySyncDryRun)
	},
}

func factorySyncRun(env *factoryEnv, dryRun bool) error {
	ops, warnings, err := env.workspaceOps()
	if err != nil {
		return err
	}
	printGapWarnings(warnings)
	// --json owns stdout: a prose banner ahead of the batch makes it unparseable
	// by the very tools the flag exists for.
	if dryRun && factorySyncJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"schema_version": opschema.SchemaVersion,
			"system_id":      env.SystemID,
			"release":        env.CurrentRelease,
			"ops":            ops,
		})
	}
	if env.CurrentRelease == "" {
		// D2 doctrine: never a silent skip — unscoped sync proceeds (it never
		// un-stamps anything) but says so loudly (REQ-CROSS-017).
		printWarning("no current release — syncing unscoped; set one with 'modernpath factory release use <slug>' (registry: process/releases.md)")
		printInfo("factory sync: %d ops (schema v%d)", len(ops), opschema.SchemaVersion)
	} else {
		printInfo("factory sync: %d ops (schema v%d, release %s)", len(ops), opschema.SchemaVersion, env.CurrentRelease)
	}

	if dryRun {
		// A ten-line list of ids cannot answer "did the field I changed come
		// out right?" — which is the only question a dry run is for. --json
		// emits the batch the sync would send, so it can be read back before
		// it lands rather than after (PROCESS §1.10).
		for i, op := range ops {
			if i >= 10 {
				fmt.Printf("  … %d more (use --json for the full batch)\n", len(ops)-10)
				break
			}
			payload, _ := op["payload"].(map[string]any)
			fmt.Printf("  %s  %s\n", str(op, "type"), str(payload, "external_id"))
		}
		return nil
	}

	batchBody := map[string]any{
		"schema_version": opschema.SchemaVersion,
		"system_id":      env.SystemID,
		"ops":            ops,
	}
	if env.CurrentRelease != "" {
		batchBody["release"] = env.CurrentRelease
	}
	status, body, err := env.call("POST", "/api/v1/sync/batch", batchBody)
	if err != nil {
		return err
	}
	if status != 200 {
		return fmt.Errorf("server %d: %v", status, body["error"])
	}

	counts := map[string]int{}
	if results, ok := dataOf(body)["results"].([]any); ok {
		for _, r := range results {
			m, _ := r.(map[string]any)
			counts[str(m, "result")]++
		}
	}
	parts := make([]string, 0, len(counts))
	for k, v := range counts {
		parts = append(parts, fmt.Sprintf("%d %s", v, k))
	}
	sort.Strings(parts)
	printSuccess("ok: %s", strings.Join(parts, ", "))

	// idempotent server-side projections (board history + approvals)
	_, _, _ = env.call("POST", "/api/v1/sync/project", map[string]any{"system_id": env.SystemID})

	// factory session heartbeat — the loop is an observable server entity
	hbStatus, hbBody, _ := env.call("POST", "/api/v1/sync/heartbeat", map[string]any{
		"system_id":           env.SystemID,
		"workspace_ref":       filepath.Base(env.Root),
		"machine_fingerprint": machineFingerprint(env.Root),
		"branch":              gitOut(env.Root, "rev-parse", "--abbrev-ref", "HEAD"),
		"agent_slug":          "factory-loop",
		"current_ref":         gitOut(env.Root, "log", "-1", "--format=%s"),
		"capabilities":        map[string]any{"verbs": []string{"sync", "gates", "answer", "pull", "evidence", "drift", "watch"}},
	})
	if hbStatus == 200 {
		if session, ok := dataOf(hbBody)["session"].(map[string]any); ok {
			printInfo("session: %s (%s)", str(dataOf(hbBody), "result"), str(session, "current_ref"))
		}
	}
	return nil
}

// ---------------------------------------------------------------- gates / answer

var factoryGatesCmd = &cobra.Command{
	Use:   "gates",
	Short: "The open decision queue (questions, decisions, approvals)",
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/gates?system_id=%d", env.SystemID), nil)
		if err != nil {
			return err
		}
		if status != 200 {
			return fmt.Errorf("server %d: %v", status, body["error"])
		}
		gates, _ := dataOf(body)["gates"].([]any)
		if len(gates) == 0 {
			printSuccess("no open gates — the queue is clear")
			return nil
		}
		for _, g := range gates {
			m, _ := g.(map[string]any)
			fmt.Printf("\n%s  [%s]  %s\n", str(m, "external_id"), str(m, "kind"), str(m, "title"))
			if rec := str(m, "recommendation"); rec != "" {
				fmt.Printf("  recommends: %.120s\n", rec)
			}
			if options, ok := m["options"].([]any); ok {
				for _, o := range options {
					om, _ := o.(map[string]any)
					fmt.Printf("  - %s: %.100s\n", str(om, "key"), str(om, "label"))
				}
			}
		}
		fmt.Printf("\n%d open — answer with: modernpath factory answer <id> --text \"…\" [--options k1,k2]\n", len(gates))
		return nil
	},
}

var (
	answerText    string
	answerOptions string
	answerSource  string
)

var factoryAnswerCmd = &cobra.Command{
	Use:   "answer <external_id>",
	Short: "Record a USER: decision on a gate (first-wins on the server)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		if answerText == "" {
			return fmt.Errorf("--text is required — the answer is recorded verbatim as the USER: decision")
		}
		payload := map[string]any{"system_id": env.SystemID, "answer": answerText}
		if answerOptions != "" {
			payload["chosen_option_keys"] = strings.Split(answerOptions, ",")
		}
		if answerSource != "" {
			payload["source_tag"] = answerSource
		}
		status, body, err := env.call("POST", "/api/v1/sync/gates/"+args[0]+"/answer", payload)
		if err != nil {
			return err
		}
		switch status {
		case 200:
			gate, _ := dataOf(body)["gate"].(map[string]any)
			printSuccess("answered %s (%s): %s", str(gate, "external_id"), str(gate, "source_tag"), str(gate, "answer"))
		case 409:
			errMap, _ := body["error"].(map[string]any)
			winner, _ := errMap["winner"].(map[string]any)
			return fmt.Errorf("already answered (first-wins) — winner: %q (%s)", str(winner, "answer"), str(winner, "source_tag"))
		default:
			return fmt.Errorf("server %d: %v", status, body["error"])
		}
		return nil
	},
}

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
		return fmt.Errorf("server %d: %v", status, body["error"])
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

	jsonlPath := filepath.Join(env.Root, "mission-control", "answers.jsonl")
	mdPath := filepath.Join(env.Root, "mission-control", "ANSWERS.md")

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
				return fmt.Errorf("spec ack failed for %s: %d %v (%v)", externalID, aStatus, aBody["error"], err)
			}
			printSuccess("pulled server spec edit → %s (marker reset to SPEC-DRAFT)", externalID)
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
			return fmt.Errorf("ack failed for %s: %d %v (%v)", externalID, aStatus, aBody["error"], err)
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

// ---------------------------------------------------------------- image generation (EPIC-DEC-001)

var imagePurpose string

var factoryImageCmd = &cobra.Command{
	Use:   "image <prompt>",
	Short: "Generate an image via the platform (tenant-stored; prints the URL)",
	Long: "EPIC-DEC-001 (REQ-AGT-027): generates through the governed image service\n" +
		"(Nano Banana 2 Lite first) — the backend stores the image in the tenant's\n" +
		"storage and returns a URL; the CLI never handles bytes.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}

		payload := map[string]any{"prompt": args[0]}
		if imagePurpose != "" {
			payload["purpose"] = imagePurpose
		}

		status, body, err := env.call("POST", "/api/v1/images", payload)
		if err != nil {
			return err
		}
		if status != 200 {
			return fmt.Errorf("server %d: %v", status, body["error"])
		}

		data := dataOf(body)
		printSuccess("image generated: %s", str(data, "model"))
		fmt.Printf("  url:      %s%s\n", env.APIURL, str(data, "url"))
		fmt.Printf("  size:     %v bytes · latency: %v ms\n", data["byte_size"], data["latency_ms"])
		return nil
	},
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

// ---------------------------------------------------------------- evidence

var (
	evidenceKind   string
	evidenceLog    string
	evidenceTotals string
	evidencePass   string
	evidenceFail   string
	evidenceSkip   string
)

var factoryEvidenceCmd = &cobra.Command{
	Use:   "evidence report",
	Short: "Post a test/CI run as evidence (sha-pinned; feeds Done-decays)",
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}

		targetType := func(id string) string {
			switch {
			case strings.Contains(id, "#AC"):
				return "criterion"
			case strings.HasPrefix(id, "EPIC-"):
				return "initiative"
			default:
				return "requirement"
			}
		}

		results := []map[string]any{}
		for flagValue, result := range map[string]string{evidencePass: "pass", evidenceFail: "fail", evidenceSkip: "skip"} {
			for _, id := range strings.Split(flagValue, ",") {
				if id = strings.TrimSpace(id); id != "" {
					results = append(results, map[string]any{"target_external_id": id, "target_type": targetType(id), "result": result})
				}
			}
		}
		if len(results) == 0 {
			return fmt.Errorf("no targets — give at least --pass or --fail")
		}

		totals := map[string]any{}
		for _, pair := range strings.Split(evidenceTotals, ",") {
			if k, v, found := strings.Cut(pair, "="); found {
				if n, err := strconv.Atoi(v); err == nil {
					totals[k] = n
				}
			}
		}

		sha := gitOut(env.Root, "rev-parse", "--short", "HEAD")
		status, body, err := env.call("POST", "/api/v1/sync/evidence", map[string]any{
			"system_id":   env.SystemID,
			"external_id": "RUN-" + time.Now().UTC().Format("2006-01-02T15-04-05Z") + "-" + sha,
			"kind":        evidenceKind,
			"sha":         sha,
			"branch":      gitOut(env.Root, "rev-parse", "--abbrev-ref", "HEAD"),
			"ran_at":      time.Now().UTC().Format(time.RFC3339),
			"runner":      map[string]any{"kind": "agent", "agent_slug": "modernpath-cli"},
			"totals":      totals,
			"log_ref":     evidenceLog,
			"results":     results,
		})
		if err != nil {
			return err
		}
		if status != 200 {
			return fmt.Errorf("server %d: %v", status, body["error"])
		}
		run, _ := dataOf(body)["run"].(map[string]any)
		printSuccess("evidence %s: %s (%d targets, sha %s)", str(dataOf(body), "result"), str(run, "external_id"), len(results), sha)
		return nil
	},
}

// ---------------------------------------------------------------- drift

var driftReport bool

var factoryDriftCmd = &cobra.Command{
	Use:   "drift",
	Short: "Compare each target's evidence sha to the working tree (Done decays)",
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}

		status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/evidence/latest?system_id=%d", env.SystemID), nil)
		if err != nil {
			return err
		}
		if status != 200 {
			return fmt.Errorf("server %d: %v", status, body["error"])
		}

		tracePaths, err := env.workspaceTracePaths()
		if err != nil {
			return err
		}

		head := gitOut(env.Root, "rev-parse", "--short", "HEAD")
		drifted := 0

		targets, _ := dataOf(body)["targets"].([]any)
		for _, t := range targets {
			m, _ := t.(map[string]any)
			if str(m, "result") != "pass" || str(m, "sha") == "" || str(m, "sha") == "unknown" {
				continue
			}
			baseID := strings.SplitN(str(m, "target_external_id"), "#AC", 2)[0]
			paths := tracePaths[baseID]
			if len(paths) == 0 {
				continue
			}

			// evidence sha -> working tree, plus untracked: uncommitted AND
			// brand-new files are drift too
			changedSet := map[string]bool{}
			for _, line := range strings.Split(gitOut(env.Root, "diff", "--name-only", str(m, "sha")), "\n") {
				if line != "" {
					changedSet[line] = true
				}
			}
			for _, line := range strings.Split(gitOut(env.Root, "ls-files", "--others", "--exclude-standard"), "\n") {
				if line != "" {
					changedSet[line] = true
				}
			}

			matched := []string{}
			for changed := range changedSet {
				for _, pStr := range paths {
					if pStr != "" && (changed == pStr || strings.HasSuffix(changed, "/"+pStr) || strings.Contains(changed, pStr)) {
						matched = append(matched, changed)
						break
					}
				}
			}
			if len(matched) == 0 {
				continue
			}
			drifted++
			sort.Strings(matched)
			fmt.Printf("\n%s  evidence at %s — %d traced file(s) changed since:\n", str(m, "target_external_id"), str(m, "sha"), len(matched))
			for i, f := range matched {
				if i >= 5 {
					break
				}
				fmt.Printf("  ~ %s\n", f)
			}

			if driftReport {
				if len(matched) > 20 {
					matched = matched[:20]
				}
				_, _, _ = env.call("POST", "/api/v1/sync/evidence/drift", map[string]any{
					"system_id": env.SystemID, "target_external_id": str(m, "target_external_id"),
					"changed": matched, "head": head, "since": str(m, "sha"),
				})
				printInfo("  -> drift_detected reported (state stale until re-verified)")
			}
		}

		if drifted == 0 {
			printSuccess("no drift — evidence still covers what is on disk")
		} else if !driftReport {
			fmt.Printf("\n%d drifted — record with: modernpath factory drift --report\n", drifted)
		}
		return nil
	},
}

// ---------------------------------------------------------------- watch

var (
	watchInterval int
	watchCycles   int
)

var factoryWatchCmd = &cobra.Command{
	Use:   "watch",
	Short: "The daemon: sync + heartbeat + pull --apply on a cadence; SIGINT closes the session",
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}

		closeSession := func() {
			_, _, _ = env.call("POST", "/api/v1/sync/session/close", map[string]any{
				"system_id":           env.SystemID,
				"workspace_ref":       filepath.Base(env.Root),
				"machine_fingerprint": machineFingerprint(env.Root),
			})
		}

		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-sigs
			fmt.Println("\nclosing session…")
			closeSession()
			os.Exit(0)
		}()

		for cycle := 1; ; cycle++ {
			printInfo("=== watch cycle %d (%s) ===", cycle, time.Now().UTC().Format(time.RFC3339))
			if err := factorySyncRun(env, false); err != nil {
				printWarning("sync failed: %v — retrying next cycle", err)
			}
			if err := factoryPullRun(env, true); err != nil {
				printWarning("pull failed: %v — retrying next cycle", err)
			}
			if watchCycles > 0 && cycle >= watchCycles {
				break
			}
			time.Sleep(time.Duration(watchInterval) * time.Second)
		}
		closeSession()
		printSuccess("watch ended — session closed")
		return nil
	},
}

// ---------------------------------------------------------------- wiring

func init() {
	factoryConnectCmd.Flags().IntVar(&factoryConnectSystem, "system", 0, "System id to bind this workspace to")

	factorySyncCmd.Flags().BoolVar(&factorySyncDryRun, "dry-run", false, "print the ops without sending")
	factorySyncCmd.Flags().BoolVar(&factorySyncJSON, "json", false, "with --dry-run: emit the full op batch as JSON")

	factoryAnswerCmd.Flags().StringVar(&answerText, "text", "", "the answer, recorded verbatim as the USER: decision")
	factoryAnswerCmd.Flags().StringVar(&answerOptions, "options", "", "chosen option keys, comma-separated")
	factoryAnswerCmd.Flags().StringVar(&answerSource, "source", "", "override the USER:<date> source tag")

	factoryPullCmd.Flags().BoolVar(&pullApply, "apply", false, "record the answers in the workspace and ack the echo")

	factoryEvidenceCmd.Flags().StringVar(&evidenceKind, "kind", "local_test", "run kind: ci|local_test|browser_verification|manual")
	factoryEvidenceCmd.Flags().StringVar(&evidenceLog, "log", "", "log reference (command line, CI url)")
	factoryEvidenceCmd.Flags().StringVar(&evidenceTotals, "totals", "", "totals, e.g. passed=478,failed=0")
	factoryEvidenceCmd.Flags().StringVar(&evidencePass, "pass", "", "passing target ids, comma-separated")
	factoryEvidenceCmd.Flags().StringVar(&evidenceFail, "fail", "", "failing target ids, comma-separated")
	factoryEvidenceCmd.Flags().StringVar(&evidenceSkip, "skip", "", "skipped target ids, comma-separated")

	factoryWatchCmd.Flags().IntVar(&watchInterval, "interval", 120, "seconds between cycles")
	factoryWatchCmd.Flags().IntVar(&watchCycles, "cycles", 0, "stop after N cycles (0 = forever)")

	factoryCmd.PersistentFlags().BoolVar(&factoryLegacyExtractor, "legacy-extractor", false,
		"shell to the workspace's node op-builder (mission-control/cli/ops-dump.js) instead of the bundled parsers")

	factoryReleaseCmd.AddCommand(factoryReleaseUseCmd, factoryReleaseShowCmd, factoryReleaseClearCmd)

	factorySyncCmd.Flags().BoolVar(&factorySyncIfQuiescent, "if-quiescent", false,
		"hook mode: sync only when the workspace is coherent; log outcomes, never error (EPIC-SYNC-009)")
	factorySyncCmd.Flags().StringVar(&factorySyncTrigger, "trigger", "manual", "trigger label for the hook log")
	factorySyncCmd.Flags().DurationVar(&factorySyncMinInterval, "min-interval", 60*time.Second,
		"debounce: skip when the last successful sync is younger than this")

	factoryCmd.AddCommand(factoryConnectCmd, factoryStatusCmd, factorySyncCmd, factoryGatesCmd,
		factoryAnswerCmd, factoryPullCmd, factoryEvidenceCmd, factoryDriftCmd, factoryWatchCmd,
		factoryManifestCmd, factoryReleaseCmd, factoryImageCmd)
	factoryImageCmd.Flags().StringVar(&imagePurpose, "purpose", "", "context tag stored with the image (e.g. decision-brief)")
	rootCmd.AddCommand(factoryCmd)
}
