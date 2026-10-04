package cmd

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------- evidence

var (
	evidenceKind   string
	evidenceLog    string
	evidenceTotals string
	evidencePass   string
	evidenceFail   string
	evidenceSkip   string
	evidenceRole   string
)

// buildEvidenceResults turns the --pass/--fail/--skip id lists into per-target
// result maps. When role is non-empty it is stamped on every result: a "RED"
// result is what Core.RDD.Reconcile.red_recorded? requires to promote an SR
// (reconcile.ex). Leaving role empty omits the key entirely, so a caller that
// passes no --role sends exactly the pre-role payload.
func buildEvidenceResults(pass, fail, skip, role string) []map[string]any {
	// The server matches the role exactly (reconcile.ex promotes on "RED") and
	// validates nothing, so a lower-case role posts fine and promotes nothing.
	role = strings.ToUpper(strings.TrimSpace(role))
	targetType := func(id string) string {
		switch {
		case strings.Contains(id, "#AC"):
			return "criterion"
		case strings.HasPrefix(id, "EPIC-"):
			return "epic"
		default:
			return "requirement"
		}
	}
	results := []map[string]any{}
	for _, pair := range []struct{ ids, result string }{{pass, "pass"}, {fail, "fail"}, {skip, "skip"}} {
		for _, id := range strings.Split(pair.ids, ",") {
			if id = strings.TrimSpace(id); id != "" {
				m := map[string]any{"target_external_id": id, "target_type": targetType(id), "result": pair.result}
				if role != "" {
					m["role"] = role
				}
				results = append(results, m)
			}
		}
	}
	return results
}

// evidenceOpts carries the `factory evidence` flags to the runnable body so the
// command is testable without cobra (REQ-CROSS-378).
type evidenceOpts struct {
	kind, log, totals, pass, fail, skip, role, revision string
}

var (
	evidenceRevision string
	evidenceFile     string
)

var factoryEvidenceCmd = &cobra.Command{
	Use:   "evidence",
	Short: "Post a test/CI run as evidence (sha-pinned; feeds Done-decays)",
	Long: `Post one run as evidence for the items it exercised. --pass, --fail and
--skip take item ids (requirements and epics). Every result is pinned to the
repository HEAD at record time, or to --revision <commit> when given.

--role is RED for a red-first run, or empty for a passing one; it is never
lower or upper — the trace class is decided by the trace, not the evidence.
The model:
  1. record RED with --fail <SR> --role RED while HEAD is at the RED commit,
     or later with --revision <red-commit>
  2. record the passing run with --pass <SR> after the fix
Reconcile needs the RED: TODO -> IN_PROGRESS does not happen without a
recorded RED, and a passing lower trace with no RED before it is reported
as a FAIL. Currency is role-aware: a RED never shadows a passing result,
and the server warns at record time about a pass with no RED before it or
a RED recorded after a pass. An epic needs evidence of its own — without
it the epic reads :claimed and its completion gate is refused. So does the
user requirement a completion gate will name (UR-<suffix> for EPIC-<suffix>):
a run posted on the epic or the SRs does not cover the UR; --pass the UR too,
or the gate refuses "not yet". Evidence for
completion is pinned to the delivered (merged) revision, not the branch head.

--file <runs.json> records several runs in one call: a JSON array of
{pass, fail, skip (id lists), role, revision, kind, log, totals (an object
of counts)}. Each entry is one run, posted on its own; its run id is derived
from the whole entry (the resolved revision, role, kind, log and each target
with its outcome), so running the same file again updates the same runs and
records any that failed before, while two different results never share an
id. Each run id is printed with the server's result; a refused run is
reported and the rest continue, and the command exits non-zero when any
failed. --file cannot be combined with the per-run flags.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if evidenceFile != "" {
			for _, f := range []string{"pass", "fail", "skip", "role", "revision", "log", "totals", "kind"} {
				if cmd.Flags().Changed(f) {
					return fmt.Errorf("--file carries each run's --%s; give it in the file, not as a flag", f)
				}
			}
		}
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		if evidenceFile != "" {
			return factoryEvidenceFile(env, evidenceFile)
		}
		return factoryEvidenceRun(env, evidenceOpts{
			kind: evidenceKind, log: evidenceLog, totals: evidenceTotals,
			pass: evidencePass, fail: evidenceFail, skip: evidenceSkip,
			role: evidenceRole, revision: evidenceRevision,
		})
	},
}

func newEvidenceRunID(now time.Time, sha string) string {
	// Separate observations need distinct IDs; replaying the same payload keeps
	// its ID and uses the server's existing idempotency contract (REQ-CMP-013).
	return "RUN-" + now.UTC().Format("2006-01-02T15-04-05Z") + "-" + sha + "-" + rand.Text()
}

func factoryEvidenceRun(env *factoryEnv, o evidenceOpts) error {
	_, err := postEvidenceRun(env, o, false)
	return err
}

// evidenceEntryRunID derives a run id from everything that makes a result
// what it is — the resolved revision, role, kind, log and each target with its
// outcome (REQ-CROSS-445). The same entry posted again keeps its id, which the
// server treats as an update; a different outcome, kind or log never shares it.
func evidenceEntryRunID(sha, revision, role, kind, log string, results []map[string]any) string {
	targets := make([]string, 0, len(results))
	for _, r := range results {
		targets = append(targets, str(r, "target_external_id")+"="+str(r, "result"))
	}
	sort.Strings(targets)
	sum := sha256.Sum256([]byte(strings.Join([]string{revision, role, kind, log, strings.Join(targets, ",")}, "\n")))
	return "RUN-" + sha + "-" + hex.EncodeToString(sum[:])[:16]
}

// evidenceFileEntry is one run in a --file (REQ-CROSS-445).
type evidenceFileEntry struct {
	Pass     []string       `json:"pass"`
	Fail     []string       `json:"fail"`
	Skip     []string       `json:"skip"`
	Role     string         `json:"role"`
	Revision string         `json:"revision"`
	Kind     string         `json:"kind"`
	Log      string         `json:"log"`
	Totals   map[string]int `json:"totals"`
}

// factoryEvidenceFile posts each entry as its own run with a derived id, keeps
// going past a refused one, and exits non-zero when any failed.
func factoryEvidenceFile(env *factoryEnv, path string) error {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("--file: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var entries []evidenceFileEntry
	if err := dec.Decode(&entries); err != nil {
		return fmt.Errorf("--file %s: expected a JSON array of runs: %w", path, err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("--file %s holds no runs", path)
	}
	failed := 0
	for i, e := range entries {
		totals := make([]string, 0, len(e.Totals))
		for k, v := range e.Totals {
			totals = append(totals, fmt.Sprintf("%s=%d", k, v))
		}
		sort.Strings(totals)
		kind := e.Kind
		if kind == "" {
			kind = "local_test"
		}
		runID, err := postEvidenceRun(env, evidenceOpts{
			kind: kind, log: e.Log, totals: strings.Join(totals, ","),
			pass: strings.Join(e.Pass, ","), fail: strings.Join(e.Fail, ","), skip: strings.Join(e.Skip, ","),
			role: e.Role, revision: e.Revision,
		}, true)
		if err != nil {
			failed++
			// A batch row, like the recorded ones: stdout, so the report reads whole.
			fmt.Printf("✗ run %d %s failed: %v\n", i+1, runID, err)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d runs failed; the others were recorded — run the same file again to record the missing ones", failed, len(entries))
	}
	return nil
}

// postEvidenceRun posts one run and returns its id. derivedID names the run
// from its content (a --file entry) instead of the time and a random part.
func postEvidenceRun(env *factoryEnv, o evidenceOpts, derivedID bool) (string, error) {
	results := buildEvidenceResults(o.pass, o.fail, o.skip, o.role)
	if len(results) == 0 {
		return "", fmt.Errorf("no targets — give at least --pass or --fail")
	}

	totals := map[string]any{}
	for _, pair := range strings.Split(o.totals, ",") {
		if k, v, found := strings.Cut(pair, "="); found {
			if n, err := strconv.Atoi(v); err == nil {
				totals[k] = n
			}
		}
	}

	// REQ-CROSS-378: --revision pins the run to a named commit — the RED commit,
	// recorded after the fix landed — without a checkout. The run's sha is the
	// short form; every result carries the full revision so the row says exactly
	// what was tested. Default: HEAD, as before.
	rev := "HEAD"
	if o.revision != "" {
		rev = o.revision
	}
	full := gitOut(env.Root, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if full == "" {
		return "", fmt.Errorf("--revision %q is not a commit in this repository — give a sha, tag or branch git resolves", rev)
	}
	sha := gitOut(env.Root, "rev-parse", "--short", full)
	for _, r := range results {
		r["revision"] = full
	}
	runID := newEvidenceRunID(time.Now(), sha)
	if derivedID {
		runID = evidenceEntryRunID(sha, full, strings.ToUpper(strings.TrimSpace(o.role)), o.kind, o.log, results)
	}
	status, body, err := env.call("POST", "/api/v1/sync/evidence", map[string]any{
		"system_id":   env.SystemID,
		"external_id": runID,
		"kind":        o.kind,
		"sha":         sha,
		"branch":      gitOut(env.Root, "rev-parse", "--abbrev-ref", "HEAD"),
		"ran_at":      time.Now().UTC().Format(time.RFC3339),
		"runner":      map[string]any{"kind": "agent", "agent_slug": "modernpath-cli"},
		"totals":      totals,
		"log_ref":     o.log,
		"results":     results,
	})
	if err != nil {
		return runID, err
	}
	if status != 200 {
		return runID, serverRefusal("", status, body)
	}
	run, _ := dataOf(body)["run"].(map[string]any)
	printSuccess("evidence %s: %s (%d targets, sha %s)", str(dataOf(body), "result"), str(run, "external_id"), len(results), sha)
	// REQ-CROSS-378: the server's record-time warnings (a pass with no RED before
	// it; a RED that a passing result already outranks) are printed, never
	// swallowed — a warning is not a refusal.
	for _, w := range stringSlice(dataOf(body)["warnings"]) {
		printWarning("%s", w)
	}
	return runID, nil
}
