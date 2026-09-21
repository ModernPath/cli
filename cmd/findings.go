package cmd

// REQ-CROSS-315 (SR-CLI-0086): `process findings add|list|disposition`. A finding
// is a store record; `add` carries the review-context id read from the scope
// directory's `.context` stamp (SR-313) so the cold-review predicate can judge
// independence. list/disposition are the read and the guarded disposition edit.

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

var (
	findingsScope               string
	findingsExternalID          string
	findingsSeverity            string
	findingsCategory            string
	findingsBody                string
	findingsSource              string
	findingsOwner               string
	findingsAggregate           string
	findingsDisposition         string
	findingsDispositionRef      string
	findingsExpectedFingerprint string
)

// splitScope parses `<kind>:<external-id>` (e.g. `epic:EPIC-CLI-008`).
func splitScope(s string) (kind, ext string) {
	if i := strings.Index(s, ":"); i > 0 {
		return s[:i], s[i+1:]
	}
	return "", ""
}

// normalizeScopeToken resolves one --scope token to its bare external id. It
// accepts both the bare id (`REQ-CROSS-358`) and the `kind:ext` form
// (`single_sr:REQ-CROSS-358`): splitScope yields an empty ext for a token with
// no `kind:` prefix, in which case the token is already bare and kept as-is.
func normalizeScopeToken(s string) string {
	if _, ext := splitScope(s); ext != "" {
		return ext
	}
	return s
}

// normalizeScopeTokens normalizes an exact_scope value token by token — the
// repeatable --scope flag arrives as []string, a JSON round-trip as []any — so
// both the bare id and the kind:ext form reach the store as the bare id the
// server keys on. The bool reports whether a scope value was present, so a value
// of another shape (or an absent scope) is left untouched.
func normalizeScopeTokens(v any) ([]string, bool) {
	var ids []string
	switch t := v.(type) {
	case []string:
		ids = t
	case []any:
		for _, e := range t {
			if s, ok := e.(string); ok {
				ids = append(ids, s)
			}
		}
	default:
		return nil, false
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, normalizeScopeToken(id))
	}
	return out, true
}

// reviewContextID reads the review-context id the scope directory was pulled
// under (`--for-review` stamps it in `.context`); empty when there is no stamp.
func reviewContextID(env *factoryEnv, scopeExt string) string {
	mode, ctxID := readContextStamp(filepath.Join(env.Root, workingSetDir, scopeExt))
	// A review context is stamped only for a --for-review pull (#8). An ordinary
	// authoring pull carries no review context, so a finding or verdict recorded
	// from it is never counted as independent.
	if mode != "review" {
		return ""
	}
	return ctxID
}

// reviewContextForScope reads the review-context id the reviewed scope was pulled
// under, for a cold-review verdict whose exact scope names the item(s) reviewed
// (#8). The working-set directory of the first scope carrying a `.context` stamp
// wins; empty when none was pulled --for-review.
func reviewContextForScope(env *factoryEnv, exactScope any) string {
	var ids []string
	switch v := exactScope.(type) {
	case []string:
		ids = v
	case []any:
		for _, e := range v {
			if s, ok := e.(string); ok {
				ids = append(ids, s)
			}
		}
	}
	for _, id := range ids {
		if ctx := reviewContextID(env, id); ctx != "" {
			return ctx
		}
	}
	return ""
}

// The store's finding vocabularies, named in the refusal when a flag is missing
// (REQ-CROSS-385): the CLI never picks the most blocking pair on the caller's
// behalf.
var (
	findingCategories         = []string{"correctness", "security", "data_loss", "contract", "traceability", "testability", "feasibility", "scope", "other"}
	findingBlockingCategories = []string{"correctness", "security", "data_loss", "contract", "traceability", "testability"}
	findingSeverities         = []string{"critical", "major", "minor", "note"}
)

func processFindingsAdd(env *factoryEnv) error {
	kind, ext := splitScope(findingsScope)
	if kind == "" || ext == "" {
		return fmt.Errorf("--scope is required as <kind>:<external-id> (e.g. epic:EPIC-CLI-008)")
	}
	if findingsCategory == "" || findingsSeverity == "" {
		return fmt.Errorf("--category and --severity are both required — the CLI never defaults to the most blocking pair. --category: %s (%s block the cold-review gate while OPEN or DEFERRED); --severity: %s (a note never blocks, whatever its category)",
			strings.Join(findingCategories, ", "), strings.Join(findingBlockingCategories, ", "), strings.Join(findingSeverities, ", "))
	}
	record := map[string]any{
		"kind":              "finding",
		"external_id":       findingsExternalID,
		"severity":          findingsSeverity,
		"category":          findingsCategory,
		"disposition":       "OPEN",
		"source":            findingsSource,
		"owner":             findingsOwner,
		"body":              findingsBody,
		"scope_kind":        kind,
		"scope_external_id": ext,
		"review_context_id": reviewContextID(env, ext),
	}
	// Derive the packet-revision provenance when --aggregate is omitted (#23), so a
	// finding is never created without knowing which reviewed packet it describes.
	agg := findingsAggregate
	if agg == "" {
		if dc, err := readDeliveryContext(env); err == nil {
			agg = dc.Data.PacketFingerprint
		}
	}
	// A finding is pinned to the packet it describes at birth. If the aggregate
	// could not be derived (the delivery-context read failed) and --aggregate was
	// not given, refuse rather than posting an unpinned finding that would still
	// count as material and block the scope.
	if agg == "" {
		return fmt.Errorf("could not determine the packet aggregate fingerprint (delivery-context read failed and --aggregate was not given) — a finding must be pinned to the packet it describes; pass --aggregate")
	}
	record["aggregate_fingerprint"] = agg
	if _, err := authorPost(env, map[string]any{"action": "create", "record": record}); err != nil {
		return err
	}
	printSuccess("finding %s recorded on %s", findingsExternalID, findingsScope)
	return nil
}

func processFindingsDisposition(env *factoryEnv) error {
	if findingsDisposition == "" {
		return fmt.Errorf("--disposition is required (OPEN, RESOLVED, DEFERRED, REJECTED)")
	}
	// The fingerprint guard is MANDATORY: a finding disposition is fingerprint-
	// guarded server-side, so a bare disposition change without it is refused.
	if findingsExpectedFingerprint == "" {
		return fmt.Errorf("--expected-fingerprint is required — a finding disposition is fingerprint-guarded; run `process findings list` to read the finding's current fingerprint")
	}
	record := map[string]any{
		"kind":                 "finding",
		"external_id":          findingsExternalID,
		"disposition":          findingsDisposition,
		"expected_fingerprint": findingsExpectedFingerprint,
	}
	// An empty reference means "not supplied" — omit it so a bare disposition
	// change never blanks a stored reference (REQ-CROSS-315).
	if findingsDispositionRef != "" {
		record["disposition_ref"] = findingsDispositionRef
	}
	if _, err := authorPost(env, map[string]any{"action": "update", "record": record}); err != nil {
		return err
	}
	printSuccess("finding %s → %s", findingsExternalID, findingsDisposition)
	return nil
}

func processFindingsList(env *factoryEnv) error {
	path := fmt.Sprintf("/api/v1/sync/findings?system_id=%d", env.SystemID)
	if findingsScope != "" {
		path += "&scope=" + findingsScope
	}
	status, body, err := env.call("GET", path, nil)
	if err != nil {
		return err
	}
	if status != 200 {
		return fmt.Errorf("findings list returned HTTP %d", status)
	}
	data, _ := body["data"].(map[string]any)
	rows, _ := data["findings"].([]any)
	if len(rows) == 0 {
		fmt.Println("no findings for this scope")
		return nil
	}
	for _, r := range rows {
		f, _ := r.(map[string]any)
		mark := ""
		if f["material"] == true {
			mark = " [material]"
		}
		if f["independent"] == false {
			mark += " [not-independent]"
		}
		fmt.Printf("  %-10v %-14v %-8v %v%s\n", f["disposition"], f["category"], f["severity"], f["external_id"], mark)
		// The full fingerprint, so a disposition can pass it to --expected-fingerprint
		// (the guard is mandatory).
		if fp := str(f, "content_fingerprint"); fp != "" {
			fmt.Printf("             fingerprint: %s\n", fp)
		}
	}
	printFindingRounds(rows)
	return nil
}

// findingRound is one review round: the findings filed under one review
// context, in the order the rounds were first filed (REQ-CROSS-385).
type findingRound struct {
	context string
	first   string // inserted_at of the round's earliest finding
	rows    []map[string]any
}

// findingRounds groups rows by review_context_id, ordered by each round's first
// inserted_at; findings filed with no review context form one "unattributed"
// round.
func findingRounds(rows []any) []findingRound {
	byCtx := map[string]*findingRound{}
	var order []*findingRound
	sorted := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		if f, ok := r.(map[string]any); ok {
			sorted = append(sorted, f)
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool { return str(sorted[i], "inserted_at") < str(sorted[j], "inserted_at") })
	for _, f := range sorted {
		ctx := str(f, "review_context_id")
		if ctx == "" {
			ctx = "unattributed"
		}
		round, ok := byCtx[ctx]
		if !ok {
			round = &findingRound{context: ctx, first: str(f, "inserted_at")}
			byCtx[ctx] = round
			order = append(order, round)
		}
		round.rows = append(round.rows, f)
	}
	out := make([]findingRound, 0, len(order))
	for _, r := range order {
		out = append(out, *r)
	}
	return out
}

// printFindingRounds — REQ-CROSS-385: counts per review round, and the
// convergence flag when the newest round's material findings are all
// traceability: the mechanical sign that the review is auditing the document
// rather than the change (PROCESS.md §Planning and readiness).
func printFindingRounds(rows []any) {
	rounds := findingRounds(rows)
	if len(rounds) == 0 {
		return
	}
	fmt.Println()
	for i, round := range rounds {
		open, material, notes := 0, 0, 0
		for _, f := range round.rows {
			if str(f, "disposition") == "OPEN" {
				open++
			}
			if f["material"] == true {
				material++
			}
			if str(f, "severity") == "note" {
				notes++
			}
		}
		fmt.Printf("  round %d · context %s · %d finding(s) · open %d · material %d · notes %d\n",
			i+1, round.context, len(round.rows), open, material, notes)
	}
	newest := rounds[len(rounds)-1]
	materialTotal, traceability := 0, 0
	for _, f := range newest.rows {
		if f["material"] == true {
			materialTotal++
			if str(f, "category") == "traceability" {
				traceability++
			}
		}
	}
	if materialTotal > 0 && traceability == materialTotal {
		fmt.Printf("  ⚠ round %d audits the document, not the change: every material finding is traceability — cut the packet to what the change needs and proceed (PROCESS.md §Planning and readiness)\n", len(rounds))
	}
}

var processFindingsCmd = &cobra.Command{
	Use:   "findings",
	Short: "Record, list, and disposition cold-review findings",
}

var processFindingsAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Record a finding on a scope (carries the directory's review-context id)",
	Long: `Record one cold-review finding. --category takes one of the nine names
the server accepts: correctness, security, data_loss, contract, traceability,
testability, feasibility, scope, other — an unlisted one is refused with
"category: is invalid" and the refusal does not list them. The categories
that block the cold-review gate while OPEN or DEFERRED are
correctness, security, data_loss, contract,
traceability and testability; feasibility, scope and other are reported and
do not block by themselves. Both --category and --severity are required —
the CLI never defaults to the most blocking pair. An
out-of-scope, pre-existing finding is REJECTED, not deferred.

A finding about the packet's own wording, counts or citations that would not
change what gets built — the code, the tests, the interfaces, the risks — is
filed with --severity note and never blocks, whatever its category
(REQ-CROSS-385); traceability is material only when a builder or a gate would
act on the wrong citation. process findings list reports counts per review
round and flags a round whose material findings are all traceability.

--body is a pointer, not the write-up: the prose belongs in the packet
section the finding points at. --id is bounded at 255 characters; a value
over the bound is refused as a 422 naming the field and the limit, and
nothing is written.

--aggregate is the full packet aggregate the finding was raised against
(process next -v); --scope is <kind>:<external-id>, kind epic or single_sr.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processFindingsAdd(env)
	},
}

var processFindingsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List findings for a scope (or all)",
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processFindingsList(env)
	},
}

var processFindingsDispositionCmd = &cobra.Command{
	Use:   "disposition",
	Short: "Change a finding's disposition (OPEN/RESOLVED/DEFERRED/REJECTED)",
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processFindingsDisposition(env)
	},
}

func init() {
	processFindingsAddCmd.Flags().StringVar(&findingsScope, "scope", "", "the scope as <kind>:<external-id>")
	processFindingsAddCmd.Flags().StringVar(&findingsExternalID, "id", "", "the finding external id")
	processFindingsAddCmd.Flags().StringVar(&findingsSeverity, "severity", "", "critical|major|minor|note (required; a note never blocks)")
	processFindingsAddCmd.Flags().StringVar(&findingsCategory, "category", "", "the finding category (required; one of the nine names)")
	processFindingsAddCmd.Flags().StringVar(&findingsBody, "body", "", "the finding body")
	processFindingsAddCmd.Flags().StringVar(&findingsSource, "source", "", "the source")
	processFindingsAddCmd.Flags().StringVar(&findingsOwner, "owner", "", "the owner")
	processFindingsAddCmd.Flags().StringVar(&findingsAggregate, "aggregate", "", "the packet aggregate fingerprint it was raised against")

	processFindingsListCmd.Flags().StringVar(&findingsScope, "scope", "", "the scope as <kind>:<external-id>")

	processFindingsDispositionCmd.Flags().StringVar(&findingsExternalID, "id", "", "the finding external id")
	processFindingsDispositionCmd.Flags().StringVar(&findingsDisposition, "disposition", "", "OPEN|RESOLVED|DEFERRED|REJECTED")
	processFindingsDispositionCmd.Flags().StringVar(&findingsDispositionRef, "ref", "", "the disposition reference")
	processFindingsDispositionCmd.Flags().StringVar(&findingsExpectedFingerprint, "expected-fingerprint", "", "guard: the finding's current fingerprint")

	processFindingsCmd.AddCommand(processFindingsAddCmd, processFindingsListCmd, processFindingsDispositionCmd)
	processCmd.AddCommand(processFindingsCmd)
}
