package cmd

// REQ-CROSS-315 (SR-CLI-0086): `process findings add|list|disposition`. A finding
// is a store record; `add --review-context` pins its provenance to a validated
// immutable review snapshot so the cold-review predicate can judge independence.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

var (
	findingsScope               string
	findingsAll                 bool
	findingsPage                listPage
	findingsExternalID          string
	findingsSeverity            string
	findingsCategory            string
	findingsBody                string
	findingsSource              string
	findingsOwner               string
	findingsAggregate           string
	findingsReviewContext       string
	findingsDisposition         string
	findingsDispositionRef      string
	findingsExpectedFingerprint string
	findingsFile                string
	findingsFrom                string
	findingsResolution          string
	findingsWidens              string
	findingsIntroducedBy        string
	findingsJSON                bool
)

// The resolution kinds a RESOLVED disposition names (SR-CLI-027-001): the flag
// spelling and the kind the store records.
var findingResolutionKinds = map[string]string{
	"packet-edit": "packet_edit",
	"scope":       "scope",
	"decision":    "decision",
}

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

// The store's finding vocabularies, named in the refusal when a flag is missing
// (REQ-CROSS-385): the CLI never picks the most blocking pair on the caller's
// behalf.
var (
	findingCategories         = []string{"correctness", "security", "data_loss", "contract", "traceability", "testability", "feasibility", "scope", "other"}
	findingBlockingCategories = []string{"correctness", "security", "data_loss", "contract", "traceability", "testability"}
	findingSeverities         = []string{"critical", "major", "minor", "note"}
)

// validateFindingVocabulary refuses a missing or unlisted category or
// severity, naming the set — the server's bare "is invalid" is never the only
// answer (REQ-CROSS-385, REQ-CROSS-424).
func validateFindingVocabulary(category, severity string) error {
	if category == "" || severity == "" {
		return fmt.Errorf("--category and --severity are both required — the CLI never defaults to the most blocking pair. --category: %s (%s block the cold-review gate while OPEN or DEFERRED); --severity: %s (a note never blocks, whatever its category)",
			strings.Join(findingCategories, ", "), strings.Join(findingBlockingCategories, ", "), strings.Join(findingSeverities, ", "))
	}
	if !contains(findingSeverities, severity) {
		return fmt.Errorf("--severity %q is not a severity — one of %s (a note never blocks, whatever its category)",
			severity, strings.Join(findingSeverities, ", "))
	}
	if !contains(findingCategories, category) {
		return fmt.Errorf("--category %q is not a category — one of %s (%s block the cold-review gate while OPEN or DEFERRED)",
			category, strings.Join(findingCategories, ", "), strings.Join(findingBlockingCategories, ", "))
	}
	return nil
}

// checkFindingEntry runs the local rules on one finding before any write —
// the flag form and every batch path alike: the vocabulary (REQ-CROSS-424)
// and, for a link to an earlier finding, the server capability. The link is a
// field a server without finding_resolution would drop silently (the field
// take), so it is refused first, as a resolution kind is (SR-CLI-027-003).
func checkFindingEntry(env *factoryEnv, e findingEntry) error {
	if err := validateFindingVocabulary(e.Category, e.Severity); err != nil {
		return err
	}
	if e.IntroducedBy != "" {
		if err := env.requireServerCapability("author.finding", "finding_resolution", "the server does not record which earlier resolution a finding falls on"); err != nil {
			return err
		}
	}
	return nil
}

func processFindingsAdd(env *factoryEnv) error {
	if findingsFile != "" {
		var entries []findingEntry
		if err := readJSONFile(findingsFile, &entries); err != nil {
			return err
		}
		pin := findingPin{}
		if findingsReviewContext != "" {
			if len(entries) == 0 {
				return fmt.Errorf("a review snapshot batch needs at least one finding with a scope")
			}
			selected, err := resolveReviewSnapshot(env, findingsReviewContext, []string{entries[0].Scope}, findingsAggregate)
			if err != nil {
				return err
			}
			// Validate the whole batch before its first write. One selected
			// snapshot describes one scope, even when other scopes are held.
			for _, entry := range entries {
				if strings.TrimSpace(entry.ID) == "" {
					return fmt.Errorf("each finding needs an id — nothing was written")
				}
				kind, ext := splitScope(entry.Scope)
				if kind != selected.Manifest.ScopeKind || ext != selected.Manifest.ScopeExternalID {
					return fmt.Errorf("finding %s scope %q does not match the selected review snapshot — nothing was written", entry.ID, entry.Scope)
				}
				if entry.Aggregate != "" && entry.Aggregate != selected.Manifest.AggregateFingerprint {
					return fmt.Errorf("finding %s aggregate pin does not match the selected review snapshot — nothing was written", entry.ID)
				}
				if err := checkFindingEntry(env, entry); err != nil {
					return err
				}
			}
			pin = findingPin{aggregate: selected.Manifest.AggregateFingerprint, contextID: selected.Manifest.ContextID, snapshot: selected}
		}
		if failed := addFindingEntries(env, entries, pin); failed > 0 {
			return fmt.Errorf("%d of %d finding(s) were not recorded — see the lines above; a re-run adds only the missing ones", failed, len(entries))
		}
		return nil
	}
	kind, ext := splitScope(findingsScope)
	if kind == "" || ext == "" {
		return fmt.Errorf("--scope is required as <kind>:<external-id> (e.g. epic:EPIC-CLI-008)")
	}
	entry := findingEntry{ID: findingsExternalID, Category: findingsCategory, Severity: findingsSeverity,
		Source: findingsSource, Owner: findingsOwner, Body: findingsBody, IntroducedBy: findingsIntroducedBy}
	if err := checkFindingEntry(env, entry); err != nil {
		return err
	}
	record := findingRecord(entry, kind, ext, "")
	var selected *selectedReviewSnapshot
	if findingsReviewContext != "" {
		resolved, err := resolveReviewSnapshot(env, findingsReviewContext, []string{findingsScope}, findingsAggregate)
		if err != nil {
			return err
		}
		selected = resolved
		record["review_context_id"] = selected.Manifest.ContextID
		record["body"] = appendReviewSnapshotProvenance(findingsBody, selected)
	}
	// Derive the packet-revision provenance when --aggregate is omitted (#23), so a
	// finding is never created without knowing which reviewed packet it describes.
	agg := findingsAggregate
	if selected != nil {
		agg = selected.Manifest.AggregateFingerprint
	} else if agg == "" {
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

// findingEntry is one finding of `findings add --file` and of a review file
// (REQ-CROSS-443, REQ-CROSS-450): the add flags' fields.
type findingEntry struct {
	ID        string `json:"id"`
	Scope     string `json:"scope"`
	Category  string `json:"category"`
	Severity  string `json:"severity"`
	Owner     string `json:"owner"`
	Source    string `json:"source"`
	Body      string `json:"body"`
	Aggregate string `json:"aggregate"`
	// IntroducedBy names the earlier finding of the same scope whose
	// resolution introduced the mechanism this one faults (SR-CLI-027-003).
	IntroducedBy string `json:"introduced_by"`
}

// dispositionEntry is one disposition of `findings disposition --file` and of
// a review file: the write happens only while the finding is still in From.
type dispositionEntry struct {
	ID          string `json:"id"`
	Scope       string `json:"scope"`
	From        string `json:"from"`
	Disposition string `json:"disposition"`
	Ref         string `json:"ref"`
	// Resolution is the kind a RESOLVED disposition names — packet-edit,
	// scope or decision, the --resolution spelling (SR-CLI-027-001); Widens is
	// the USER: source of a packet edit that widened a member (SR-CLI-027-002).
	Resolution string `json:"resolution"`
	Widens     string `json:"widens"`
}

// findingPin overrides where a batch's findings are pinned: `review record`
// pins them to the aggregate and review context its pull stamped. Empty
// aggregates fall back to the entry or its scope's delivery-context read.
// An absent context never inherits the authoring directory's stamp.
type findingPin struct {
	snapshot  *selectedReviewSnapshot
	aggregate string
	contextID string
}

func findingRecord(e findingEntry, kind, ext, contextID string) map[string]any {
	record := map[string]any{
		"kind":              "finding",
		"external_id":       e.ID,
		"severity":          e.Severity,
		"category":          e.Category,
		"disposition":       "OPEN",
		"source":            e.Source,
		"owner":             e.Owner,
		"body":              e.Body,
		"scope_kind":        kind,
		"scope_external_id": ext,
		"review_context_id": contextID,
	}
	if e.IntroducedBy != "" {
		record["introduced_by"] = e.IntroducedBy
	}
	return record
}

// readJSONFile reads a findings, dispositions or review file. A key the
// shape does not know is refused, naming every one, so a misspelt field is
// never dropped silently (PR #694 review, #8).
func readJSONFile(path string, into any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("%s is not the expected JSON: %w", path, err)
	}
	if unknown := unknownJSONKeys(doc, reflect.TypeOf(into), ""); len(unknown) > 0 {
		return fmt.Errorf("%s has unknown key(s) %s — check the spelling; nothing was written", path, strings.Join(unknown, ", "))
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("%s is not the expected JSON: %w", path, err)
	}
	return nil
}

// unknownJSONKeys lists the object keys in doc that the Go type t does not
// name, as paths (findings[0].bodyy).
func unknownJSONKeys(doc any, t reflect.Type, at string) []string {
	for t != nil && (t.Kind() == reflect.Pointer) {
		t = t.Elem()
	}
	if t == nil {
		return nil
	}
	var out []string
	switch v := doc.(type) {
	case []any:
		if t.Kind() != reflect.Slice && t.Kind() != reflect.Array {
			return nil
		}
		for i, e := range v {
			out = append(out, unknownJSONKeys(e, t.Elem(), fmt.Sprintf("%s[%d]", at, i))...)
		}
	case map[string]any:
		if t.Kind() != reflect.Struct {
			return nil
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "-" || !f.IsExported() {
				continue
			}
			if name == "" {
				name = f.Name
			}
			fields[strings.ToLower(name)] = f.Type
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			path := k
			if at != "" {
				path = at + "." + k
			}
			ft, ok := fields[strings.ToLower(k)]
			if !ok {
				out = append(out, path)
				continue
			}
			out = append(out, unknownJSONKeys(v[k], ft, path)...)
		}
	}
	return out
}

// readScopeFindings reads the findings recorded on one scope (<kind>:<ext>),
// keyed by external id.
func readScopeFindings(env *factoryEnv, scope string) (map[string]map[string]any, error) {
	path := fmt.Sprintf("/api/v1/sync/findings?system_id=%d&scope=%s", env.SystemID, url.QueryEscape(scope))
	status, body, err := env.call("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, serverRefusal("findings list", status, body)
	}
	rows, _ := dataOf(body)["findings"].([]any)
	out := map[string]map[string]any{}
	for _, r := range rows {
		if f, ok := r.(map[string]any); ok {
			out[str(f, "external_id")] = f
		}
	}
	return out, nil
}

// scopeReads caches one findings read and one aggregate read per scope, so a
// batch costs two reads per scope, not per finding.
type scopeReads struct {
	env        *factoryEnv
	findings   map[string]map[string]map[string]any
	findErr    map[string]error
	aggregates map[string]string
	aggErr     map[string]error
}

func newScopeReads(env *factoryEnv) *scopeReads {
	return &scopeReads{env: env, findings: map[string]map[string]map[string]any{}, findErr: map[string]error{},
		aggregates: map[string]string{}, aggErr: map[string]error{}}
}

func (r *scopeReads) scopeFindings(scope string) (map[string]map[string]any, error) {
	if rows, ok := r.findings[scope]; ok || r.findErr[scope] != nil {
		return rows, r.findErr[scope]
	}
	rows, err := readScopeFindings(r.env, scope)
	r.findings[scope], r.findErr[scope] = rows, err
	return rows, err
}

// aggregate reads the scope's own packet aggregate — the delivery context of
// that piece, not the held one — so it works while several pieces are held.
func (r *scopeReads) aggregate(ext string) (string, error) {
	if agg, ok := r.aggregates[ext]; ok || r.aggErr[ext] != nil {
		return agg, r.aggErr[ext]
	}
	dc, err := readDeliveryContextFor(r.env, ext)
	agg := ""
	if err == nil {
		agg = dc.Data.PacketFingerprint
		if agg == "" {
			err = fmt.Errorf("the delivery context of %s serves no packet aggregate — a finding must be pinned to the packet it describes", ext)
		}
	}
	r.aggregates[ext], r.aggErr[ext] = agg, err
	return agg, err
}

// addFindingEntries records each finding not yet recorded on its scope,
// printing one line per entry, and returns how many failed. A failure never
// stops the rest unless the selected snapshot changes; a re-run adds only
// what is missing.
func addFindingEntries(env *factoryEnv, entries []findingEntry, pin findingPin) int {
	reads := newScopeReads(env)
	failed := 0
	fail := func(id string, err error) {
		failed++
		fmt.Printf("  %-16s failed — %v\n", id, err)
	}
	for i, e := range entries {
		kind, ext := splitScope(e.Scope)
		if e.ID == "" || kind == "" || ext == "" {
			fail(e.ID, fmt.Errorf("each finding needs an id and a scope as <kind>:<external-id>"))
			continue
		}
		if err := checkFindingEntry(env, e); err != nil {
			fail(e.ID, err)
			continue
		}
		existing, err := reads.scopeFindings(e.Scope)
		if err != nil {
			fail(e.ID, err)
			continue
		}
		if _, ok := existing[e.ID]; ok {
			fmt.Printf("  %-16s skipped — already recorded on %s\n", e.ID, e.Scope)
			continue
		}
		agg := pin.aggregate
		if agg == "" {
			agg = e.Aggregate
		}
		if agg == "" {
			if agg, err = reads.aggregate(ext); err != nil {
				fail(e.ID, err)
				continue
			}
		}
		ctxID := pin.contextID
		record := findingRecord(e, kind, ext, ctxID)
		if pin.snapshot != nil {
			if err := revalidateReviewSnapshot(env, pin.snapshot); err != nil {
				fail(e.ID, err)
				return failed + len(entries) - i - 1
			}
			record["body"] = appendReviewSnapshotProvenance(e.Body, pin.snapshot)
		}
		record["aggregate_fingerprint"] = agg
		data, err := authorPost(env, map[string]any{"action": "create", "record": record})
		if err != nil {
			fail(e.ID, err)
			continue
		}
		row, _ := data["finding"].(map[string]any)
		fmt.Printf("  %-16s added on %s  %s\n", e.ID, e.Scope, str(row, "fingerprint"))
	}
	return failed
}

// applyDispositionEntries sets each listed finding's disposition, writing only
// while the finding is still in the disposition the entry expects (From) and
// guarding the write on the fingerprint just read. A finding already in the
// target disposition is reported unchanged. An entry breaking the resolution
// rules (SR-CLI-027-001/002) is refused before any read. It prints one line
// per entry and returns how many were refused or failed. Ordinary refusals
// continue; a changed selected snapshot stops the remaining writes.
func applyDispositionEntries(env *factoryEnv, entries []dispositionEntry, snapshot *selectedReviewSnapshot) int {
	reads := newScopeReads(env)
	failed := 0
	fail := func(id, format string, a ...any) {
		failed++
		fmt.Printf("  %-16s refused — %s\n", id, fmt.Sprintf(format, a...))
	}
	for i, e := range entries {
		if e.ID == "" || e.Scope == "" || e.From == "" || e.Disposition == "" {
			fail(e.ID, "each disposition needs id, scope, from and disposition")
			continue
		}
		kind, err := checkDisposition(e.Disposition, e.Resolution, e.Widens, e.Ref)
		if err != nil {
			fail(e.ID, "%v", err)
			continue
		}
		rows, err := reads.scopeFindings(e.Scope)
		if err != nil {
			fail(e.ID, "%v", err)
			continue
		}
		row, ok := rows[e.ID]
		if !ok {
			fail(e.ID, "no finding %s is recorded on %s", e.ID, e.Scope)
			continue
		}
		current := str(row, "disposition")
		if current == e.Disposition {
			fmt.Printf("  %-16s unchanged — already %s\n", e.ID, current)
			continue
		}
		if current != e.From {
			fail(e.ID, "its current disposition is %s, the file expects %s — not written", current, e.From)
			continue
		}
		if snapshot != nil {
			if err := revalidateReviewSnapshot(env, snapshot); err != nil {
				fail(e.ID, "%v", err)
				return failed + len(entries) - i - 1
			}
		}
		fp, err := postDisposition(env, e.ID, e.Disposition, kind, e.Widens, e.Ref, str(row, "content_fingerprint"))
		if err != nil {
			fail(e.ID, "%v", err)
			continue
		}
		label := e.Disposition
		if kind != "" {
			label += "/" + e.Resolution
		}
		fmt.Printf("  %-16s %s → %s  %s\n", e.ID, current, label, fp)
	}
	return failed
}

// checkReviewEntries runs every local rule on a review file's findings and
// dispositions before any write, so a review that breaks one is refused whole
// rather than half-recorded (REQ-CROSS-450, SR-CLI-027-001..003).
func checkReviewEntries(env *factoryEnv, findings []findingEntry, dispositions []dispositionEntry) error {
	for _, f := range findings {
		if err := checkFindingEntry(env, f); err != nil {
			return fmt.Errorf("finding %s: %v — nothing was written", f.ID, err)
		}
	}
	for _, d := range dispositions {
		kind, err := checkDisposition(d.Disposition, d.Resolution, d.Widens, d.Ref)
		if err == nil && kind != "" {
			err = requireResolutionCapability(env)
		}
		if err != nil {
			return fmt.Errorf("disposition %s: %v — nothing was written", d.ID, err)
		}
	}
	return nil
}

// resolutionKind — SR-CLI-027-001: the local rules on --resolution, refused
// before any request. RESOLVED requires a kind naming the three; scope names
// its record in --ref; decision names its USER: source in --ref; a kind with
// any other disposition is refused. Returns the kind the store records.
func resolutionKind(disposition, resolution, ref string) (string, error) {
	if resolution != "" && disposition != "RESOLVED" {
		return "", fmt.Errorf("--resolution names how a finding resolved and goes with --disposition RESOLVED only (got %q)", disposition)
	}
	if disposition == "RESOLVED" && resolution == "" {
		return "", fmt.Errorf("--resolution is required on RESOLVED: packet-edit (the packet was clarified), scope (a scope action — --ref names its record) or decision (--ref is its USER: source)")
	}
	if resolution == "" {
		return "", nil
	}
	kind, ok := findingResolutionKinds[resolution]
	if !ok {
		return "", fmt.Errorf("--resolution %q is not a resolution kind — one of packet-edit, scope, decision", resolution)
	}
	if kind == "scope" && ref == "" {
		return "", fmt.Errorf("--resolution scope names its record — pass --ref <the split epic, deferral or backlog record>")
	}
	if kind == "decision" && !strings.HasPrefix(ref, "USER:") {
		return "", fmt.Errorf("--resolution decision names its human source — pass --ref USER:<date>:<why>")
	}
	return kind, nil
}

// checkDisposition runs the local rules on one disposition — the flag form
// and every batch path alike — before any request: the resolution kind
// (SR-CLI-027-001) and the widening (SR-CLI-027-002). Returns the kind the
// store records.
func checkDisposition(disposition, resolution, widens, ref string) (string, error) {
	kind, err := resolutionKind(disposition, resolution, ref)
	if err != nil {
		return "", err
	}
	// --widens states, on the human's word, that the packet edit widened a
	// member the server would otherwise refuse the edit for. Its local rules
	// run before the contract read (F-PR701-01).
	if widens != "" {
		if kind != "packet_edit" {
			return "", fmt.Errorf("--widens states that a packet edit widened a member and goes with --resolution packet-edit only")
		}
		if !strings.HasPrefix(widens, "USER:") {
			return "", fmt.Errorf("--widens names the human who accepted the widening — pass USER:<date>:<why>")
		}
	}
	return kind, nil
}

// requireResolutionCapability — a server without finding_resolution would
// drop the kind silently (the field take) and record a bare RESOLVED: refuse
// first.
func requireResolutionCapability(env *factoryEnv) error {
	return env.requireServerCapability("author.finding", "finding_resolution", "the server does not record resolution kinds")
}

// postDisposition sends one guarded disposition update. An empty disposition
// is a reference-only change on a finding already RESOLVED (SR-CLI-027-001
// D10); kind and widens come from checkDisposition.
func postDisposition(env *factoryEnv, id, disposition, kind, widens, ref, expected string) (string, error) {
	record := map[string]any{
		"kind":                 "finding",
		"external_id":          id,
		"expected_fingerprint": expected,
	}
	if disposition != "" {
		record["disposition"] = disposition
	}
	if kind != "" {
		if err := requireResolutionCapability(env); err != nil {
			return "", err
		}
		record["resolution_kind"] = kind
	}
	if widens != "" {
		record["widening_source"] = widens
	}
	// An empty reference means "not supplied" — omit it so a bare disposition
	// change never blanks a stored reference (REQ-CROSS-315).
	if ref != "" {
		record["disposition_ref"] = ref
	}
	data, err := authorPost(env, map[string]any{"action": "update", "record": record})
	if err != nil {
		return "", err
	}
	row, _ := data["finding"].(map[string]any)
	return str(row, "fingerprint"), nil
}

func processFindingsDisposition(env *factoryEnv) error {
	if findingsFile != "" {
		var entries []dispositionEntry
		if err := readJSONFile(findingsFile, &entries); err != nil {
			return err
		}
		if failed := applyDispositionEntries(env, entries, nil); failed > 0 {
			return fmt.Errorf("%d of %d disposition(s) were not written — see the lines above", failed, len(entries))
		}
		return nil
	}
	// SR-CLI-027-001 (D10): a reference-only change on a finding already
	// RESOLVED is sent with --ref alone — no disposition, no kind — so the
	// server applies it under the immutability rule (REQ-CROSS-315) and a row
	// resolved before the kind existed keeps its path.
	if findingsDisposition == "" && findingsDispositionRef == "" {
		return fmt.Errorf("--disposition is required (OPEN, RESOLVED, DEFERRED, REJECTED) — or --ref alone to change the reference of a finding already RESOLVED")
	}
	kind, err := checkDisposition(findingsDisposition, findingsResolution, findingsWidens, findingsDispositionRef)
	if err != nil {
		return err
	}
	// The fingerprint guard is MANDATORY: a finding disposition is fingerprint-
	// guarded server-side. Without --expected-fingerprint the CLI reads it for
	// the named --scope (REQ-CROSS-443); with neither it refuses before any POST.
	expected := findingsExpectedFingerprint
	if expected == "" {
		if findingsScope == "" {
			return fmt.Errorf("--expected-fingerprint is required — a finding disposition is fingerprint-guarded; pass it from `process findings list`, or pass --scope <kind>:<external-id> and the CLI reads it")
		}
		// A fingerprint read just before the write guards nothing: the
		// disposition the caller saw is the guard, as in the --file form
		// (PR #694 review, #8).
		if findingsFrom == "" {
			return fmt.Errorf("--from is required with --scope — give the disposition you saw (OPEN, RESOLVED, DEFERRED or REJECTED); the finding is written only while it is still in it, so another session's decision is never overwritten")
		}
		rows, err := readScopeFindings(env, findingsScope)
		if err != nil {
			return err
		}
		row, ok := rows[findingsExternalID]
		if !ok {
			return fmt.Errorf("no finding %s is recorded on %s", findingsExternalID, findingsScope)
		}
		switch current := str(row, "disposition"); {
		case current == findingsDisposition:
			printInfo("finding %s unchanged — already %s", findingsExternalID, current)
			return nil
		case current != findingsFrom:
			return fmt.Errorf("finding %s is %s, not %s — someone changed it after you read it; read it again with `process findings list` and decide; nothing was written", findingsExternalID, current, findingsFrom)
		}
		expected = str(row, "content_fingerprint")
	}
	if _, err := postDisposition(env, findingsExternalID, findingsDisposition, kind, findingsWidens, findingsDispositionRef, expected); err != nil {
		return err
	}
	switch {
	case findingsDisposition == "":
		printSuccess("finding %s reference → %s", findingsExternalID, findingsDispositionRef)
	case kind != "":
		printSuccess("finding %s → %s/%s", findingsExternalID, findingsDisposition, findingsResolution)
	default:
		printSuccess("finding %s → %s", findingsExternalID, findingsDisposition)
	}
	return nil
}

// REQ-CROSS-424: the list is scoped to the piece you hold unless --scope
// names one by hand or --all asks for the system. The scope is composed from
// the caller-scoped selection read (--piece when several are held), so the
// read never sends a bare id and never falls back to the whole system by
// omission — the cause of the 1549-line answer (BACKLOG-TOOL-9).
// SR-CLI-027-004: the rows are grouped by scope — rounds and flags per scope,
// a heading per group under --all — and --json writes the same rows and
// rounds as one object; everything goes through the command's writer.
// REQ-CROSS-447: the rows are paged across the groups in the order served;
// each round summary counts all of its scope's rows.
func processFindingsList(env *factoryEnv, out io.Writer) error {
	if err := findingsPage.validate(); err != nil {
		return err
	}
	scope := findingsScope
	if scope == "" && !findingsAll {
		kind, ext, err := heldPieceScope(env, processPiece)
		if err != nil {
			return err
		}
		scope = kind + ":" + ext
	}
	path := fmt.Sprintf("/api/v1/sync/findings?system_id=%d", env.SystemID)
	if scope != "" {
		path += "&scope=" + url.QueryEscape(scope)
	}
	status, body, err := env.call("GET", path, nil)
	if err != nil {
		return err
	}
	if status != 200 {
		return serverRefusal("findings list", status, body)
	}
	data, _ := body["data"].(map[string]any)
	rows, _ := data["findings"].([]any)
	groups := groupFindingsByScope(rows)
	total := 0
	for _, g := range groups {
		total += len(g.rows)
	}
	start, end := findingsPage.window(total, findingsJSON)
	if findingsJSON {
		return writeFindingsJSON(out, groups, start, end, total)
	}
	if len(rows) == 0 {
		if scope == "" {
			fmt.Fprintln(out, "no findings on this system")
		} else {
			fmt.Fprintf(out, "no findings for %s\n", scope)
		}
		return nil
	}
	at, shown := 0, 0
	for _, g := range groups {
		lo, hi := at, at+len(g.rows)
		at = hi
		if hi <= start || lo >= end {
			continue
		}
		if scope == "" {
			// --all: a heading per scope group; a single-scope listing has none.
			if shown > 0 {
				fmt.Fprintln(out)
			}
			fmt.Fprintf(out, "▸ %s\n", g.scope)
		}
		shown++
		for _, f := range g.rows[max(start, lo)-lo : min(end, hi)-lo] {
			mark := ""
			if f["material"] == true {
				mark = " [material]"
			}
			if f["independent"] == false {
				mark += " [not-independent]"
			}
			fmt.Fprintf(out, "  %-10v %-14v %-8v %v%s\n", dispositionLabel(f), f["category"], f["severity"], f["external_id"], mark)
			// The full fingerprint, so a disposition can pass it to --expected-fingerprint
			// (the guard is mandatory).
			if fp := str(f, "content_fingerprint"); fp != "" {
				fmt.Fprintf(out, "             fingerprint: %s\n", fp)
			}
			if verbose {
				printFindingDetail(out, f)
			}
		}
		printFindingRounds(out, g.rounds, g.counts)
	}
	fmt.Fprint(out, pageFooter(start, end, total))
	return nil
}

// findingScopeGroup is one scope's rows with its rounds and their counts —
// the unit the human render and the JSON both walk (SR-CLI-027-004).
type findingScopeGroup struct {
	scope  string // <scope_kind>:<scope_external_id>
	rows   []map[string]any
	rounds []findingRound
	counts []findingRoundCounts
}

// groupFindingsByScope groups the served rows by scope in the order the read
// serves them (scope_kind, scope_external_id, external_id), computing the
// rounds and their counts per scope so round numbering and the flags never
// cross scopes (BACKLOG-TOOL-225).
func groupFindingsByScope(rows []any) []findingScopeGroup {
	byScope := map[string]*findingScopeGroup{}
	var order []*findingScopeGroup
	for _, r := range rows {
		f, ok := r.(map[string]any)
		if !ok {
			continue
		}
		key := str(f, "scope_kind") + ":" + str(f, "scope_external_id")
		g, ok := byScope[key]
		if !ok {
			g = &findingScopeGroup{scope: key}
			byScope[key] = g
			order = append(order, g)
		}
		g.rows = append(g.rows, f)
	}
	out := make([]findingScopeGroup, 0, len(order))
	for _, g := range order {
		anyRows := make([]any, 0, len(g.rows))
		for _, f := range g.rows {
			anyRows = append(anyRows, f)
		}
		g.rounds = findingRounds(anyRows)
		g.counts = roundCounts(g.rounds)
		out = append(out, *g)
	}
	return out
}

// findingsJSONRound is one round of one scope as --json serves it; the flags
// are the two convergence warnings, computed per round (the human render
// prints the document flag for the newest round only).
type findingsJSONRound struct {
	Scope                string   `json:"scope"`
	Round                int      `json:"round"`
	ReviewContextID      string   `json:"review_context_id"`
	Findings             int      `json:"findings"`
	Open                 int      `json:"open"`
	Material             int      `json:"material"`
	Notes                int      `json:"notes"`
	Widened              int      `json:"widened"`
	OnEarlierResolutions int      `json:"on_earlier_resolutions"`
	Flags                []string `json:"flags"`
}

// writeFindingsJSON — SR-CLI-027-004: one object, {findings, rounds}, where
// findings are the served rows each with its round and rounds are one object
// per scope and round; an empty result is the same object with empty arrays.
func writeFindingsJSON(out io.Writer, groups []findingScopeGroup, start, end, total int) error {
	findings := []map[string]any{}
	rounds := []findingsJSONRound{}
	at := 0
	for _, g := range groups {
		roundOf := map[string]int{}
		for i, round := range g.rounds {
			for _, f := range round.rows {
				roundOf[str(f, "external_id")] = i + 1
			}
			c := g.counts[i]
			flags := []string{}
			if c.documentAudit {
				flags = append(flags, "document")
			}
			if c.onEarlierOnly {
				flags = append(flags, "earlier_resolutions")
			}
			rounds = append(rounds, findingsJSONRound{
				Scope: g.scope, Round: i + 1, ReviewContextID: round.context, Findings: len(round.rows),
				Open: c.open, Material: c.material, Notes: c.notes, Widened: c.widened,
				OnEarlierResolutions: c.onEarlier, Flags: flags,
			})
		}
		for _, f := range g.rows {
			// REQ-CROSS-447: an explicit --limit/--offset pages the rows;
			// the rounds always count every row.
			at++
			if at <= start || at > end {
				continue
			}
			row := make(map[string]any, len(f)+1)
			for k, v := range f {
				row[k] = v
			}
			row["round"] = roundOf[str(f, "external_id")]
			findings = append(findings, row)
		}
	}
	blob, err := json.MarshalIndent(map[string]any{"findings": findings, "rounds": rounds, "total": total, "has_more": end < total}, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(blob))
	return err
}

// dispositionLabel — SR-CLI-027-001: a RESOLVED row prints its resolution kind
// after the disposition (RESOLVED/packet-edit); a null kind — a row resolved
// before the kind existed — prints RESOLVED as before.
func dispositionLabel(f map[string]any) string {
	d := str(f, "disposition")
	if kind := str(f, "resolution_kind"); d == "RESOLVED" && kind != "" {
		return d + "/" + strings.ReplaceAll(kind, "_", "-")
	}
	return d
}

// printFindingDetail prints under a row what `add` wrote: body, source, owner,
// scope, aggregate and disposition reference, each as served.
func printFindingDetail(out io.Writer, f map[string]any) {
	fmt.Fprintf(out, "             body: %s\n", fieldOr(f, "body", "—"))
	fmt.Fprintf(out, "             source: %s\n", fieldOr(f, "source", "—"))
	fmt.Fprintf(out, "             owner: %s\n", fieldOr(f, "owner", "—"))
	fmt.Fprintf(out, "             scope: %s:%s\n", fieldOr(f, "scope_kind", "—"), fieldOr(f, "scope_external_id", "—"))
	fmt.Fprintf(out, "             aggregate: %s\n", fieldOr(f, "aggregate_fingerprint", "—"))
	fmt.Fprintf(out, "             disposition ref: %s\n", fieldOr(f, "disposition_ref", "—"))
	// SR-CLI-027-002: the USER: source that widened a member under a packet edit.
	fmt.Fprintf(out, "             widening source: %s\n", fieldOr(f, "widening_source", "—"))
}

// heldPieceScope resolves the piece the caller holds — the one named by
// --piece when several are held — to its scope kind and external id from the
// caller-scoped selection read. The server's several-pieces refusal surfaces
// naming --piece; a caller holding nothing is told to name a scope or --all.
func heldPieceScope(env *factoryEnv, piece string) (string, string, error) {
	path := fmt.Sprintf("/api/v1/sync/work-selection?system_id=%d", env.SystemID)
	if piece != "" {
		path += "&scope=" + url.QueryEscape(piece)
	}
	status, body, err := env.call("GET", path, nil)
	if err != nil {
		return "", "", err
	}
	if status != 200 {
		if ok, refusal := heldPiecesRefusal(body); ok {
			return "", "", refusal
		}
		return "", "", serverRefusal("work-selection read", status, body)
	}
	current, _ := dataOf(body)["current"].(map[string]any)
	kind, ext := str(current, "scope_kind"), str(current, "scope_external_id")
	if kind == "" || ext == "" {
		// The caller-scoped read serves no current row both when nothing is
		// held and when --piece names a piece the caller does not hold; say
		// which (PR #618 review, finding 5).
		if piece != "" {
			return "", "", fmt.Errorf("you do not hold %s — name a piece you hold with --piece, a scope with --scope <kind>:<external-id>, or --all for the system's findings", piece)
		}
		return "", "", fmt.Errorf("you hold no current piece — name a scope with --scope <kind>:<external-id>, or --all for the system's findings")
	}
	return kind, ext, nil
}

// findingRound is one review round: the findings filed under one review
// context, in the order the rounds were first filed (REQ-CROSS-385).
type findingRound struct {
	context string
	first   string // inserted_at of the round's earliest finding
	rows    []map[string]any
}

// findingRoundCounts — REQ-CROSS-385 and SR-CLI-027-003: the counts one round
// line prints, and whether the round's material findings all fall on
// mechanisms an earlier round's resolutions introduced.
type findingRoundCounts struct {
	open, material, notes int
	widened               int // resolved with a widening source (SR-CLI-027-002)
	onEarlier             int // introduced_by names a finding of an earlier round
	documentAudit         bool
	onEarlierOnly         bool
}

// roundCounts computes each round's counts over one scope's rounds; a
// finding that names a finding of its own round counts in neither new
// column, and a link to an unknown or later finding is not counted.
func roundCounts(rounds []findingRound) []findingRoundCounts {
	roundOf := map[string]int{}
	for i, round := range rounds {
		for _, f := range round.rows {
			if id := str(f, "external_id"); id != "" {
				roundOf[id] = i
			}
		}
	}
	out := make([]findingRoundCounts, len(rounds))
	for i, round := range rounds {
		c := &out[i]
		materialOnEarlier, traceability := 0, 0
		for _, f := range round.rows {
			earlier := false
			if by := str(f, "introduced_by"); by != "" {
				if r, ok := roundOf[by]; ok && r < i {
					earlier = true
				}
			}
			if str(f, "disposition") == "OPEN" {
				c.open++
			}
			if f["material"] == true {
				c.material++
				if earlier {
					materialOnEarlier++
				}
				if str(f, "category") == "traceability" {
					traceability++
				}
			}
			if str(f, "severity") == "note" {
				c.notes++
			}
			if str(f, "widening_source") != "" {
				c.widened++
			}
			if earlier {
				c.onEarlier++
			}
		}
		c.documentAudit = c.material > 0 && traceability == c.material
		c.onEarlierOnly = c.material > 0 && materialOnEarlier == c.material
	}
	return out
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
// rather than the change (PROCESS.md §Planning and readiness). SR-CLI-027-003
// appends the widened and on-earlier-resolutions counts, and flags any round
// whose material findings all fall on mechanisms an earlier round's
// resolutions introduced: the packet was reviewed incomplete (PROCESS.md
// §Entry packet).
func printFindingRounds(out io.Writer, rounds []findingRound, counts []findingRoundCounts) {
	if len(rounds) == 0 {
		return
	}
	fmt.Fprintln(out)
	for i, round := range rounds {
		c := counts[i]
		fmt.Fprintf(out, "  round %d · context %s · %d finding(s) · open %d · material %d · notes %d · widened %d · on earlier resolutions %d\n",
			i+1, round.context, len(round.rows), c.open, c.material, c.notes, c.widened, c.onEarlier)
	}
	if counts[len(counts)-1].documentAudit {
		fmt.Fprintf(out, "  ⚠ round %d audits the document, not the change: every material finding is traceability — cut the packet to what the change needs and proceed (PROCESS.md §Planning and readiness)\n", len(rounds))
	}
	for i, c := range counts {
		if c.onEarlierOnly {
			fmt.Fprintf(out, "  ⚠ round %d faults mechanisms an earlier round's resolutions introduced — the packet was reviewed incomplete; return it to rdd-plan for that mechanism's own reconnaissance (PROCESS.md §Entry packet)\n", i+1)
		}
	}
}

var processFindingsCmd = &cobra.Command{
	Use:   "findings",
	Short: "Record, list, and disposition cold-review findings",
}

var processFindingsAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Record a finding on a scope (optionally pinned to a review snapshot)",
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

--introduced-by <F-id> names the earlier finding of the same scope whose
resolution introduced the mechanism this finding faults (SR-CLI-027-003);
the server refuses an id it does not hold on the scope, or the finding
itself, and a server that does not advertise finding_resolution is refused
before any request. process findings list counts, per round, the findings
resolved with a widening and those on earlier resolutions, and flags a
round whose material findings all fall on earlier resolutions: the packet
was reviewed incomplete (PROCESS.md §Entry packet).

--aggregate is the full packet aggregate the finding was raised against
(process next -v); --review-context selects an immutable snapshot created by
working-set pull --scope --for-review; when selected, its aggregate is used
and its context id and digest are added to the existing finding body. Without
the selector, an ordinary finding is not attributed to any review snapshot.
--scope is <kind>:<external-id>, kind epic or single_sr.

--file <findings.json> records a whole review's findings in one call: a JSON
array of {id, scope, category, severity, owner, source, body, introduced_by}
(introduced_by as --introduced-by, optional). Each finding
is pinned to the aggregate of its own scope, read for that scope, so it works
while you hold several pieces. Ids the scope already holds are skipped, so a
re-run after a partial failure adds only what is missing. Each result is
printed, and the call exits non-zero when any finding was not recorded.
With --review-context, every entry must name the selected snapshot's scope;
command and entry aggregate pins must match it. Snapshot integrity and all
entry scopes and pins are checked before any write. Each finding retains the
selected context, aggregate and snapshot digest.`,

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
	Long: `List the findings of the piece you hold (--piece when several), of
--scope <kind>:<external-id>, or of the whole system with --all. Each row
prints its disposition (RESOLVED/<kind> once resolved), category, severity,
id and full fingerprint; -v adds body, source, owner, scope, aggregate,
disposition reference and widening source. Below the rows, one line per
review round counts the findings, the open, material and note ones, those
resolved with a widening and those on earlier resolutions, and a round is
flagged when its material findings are all traceability (the review audits
the document) or all fall on mechanisms an earlier round's resolutions
introduced (the packet was reviewed incomplete).

--all groups the rows and the rounds by scope (SR-CLI-027-004): a heading
per scope, round numbering and flags per scope. --json writes one object
to stdout and nothing else: findings (the rows as the store serves them,
each with material, independent and its round) and rounds (one object per
scope and round: scope, round, review_context_id, findings, open, material,
notes, widened, on_earlier_resolutions, flags — the flags per round as
strings, document and earlier_resolutions); -v adds nothing, and an empty
result is the same object with empty arrays. The shape is the same across
--all, --scope and the held-piece default.

--limit and --offset page the rows (REQ-CROSS-447): text prints at most 50
by default with a footer naming the next offset; --json carries every row
unless --limit is given, with total and has_more. The round summary always
counts every row of its scope.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processFindingsList(env, cmd.OutOrStdout())
	},
}

var processFindingsDispositionCmd = &cobra.Command{
	Use:   "disposition",
	Short: "Change a finding's disposition (OPEN/RESOLVED/DEFERRED/REJECTED)",
	Long: `Change one finding's disposition. The write is fingerprint-guarded:
pass --expected-fingerprint from process findings list, or pass --scope
<kind>:<external-id> with --from <the disposition you saw>, and the CLI
writes only while the finding is still in it. A finding already in the
target disposition is reported unchanged.

A RESOLVED disposition names how it resolved (SR-CLI-027-001, PROCESS.md
§Entry packet): --resolution packet-edit when the packet was clarified,
scope for a scope action (--ref names its record: a split epic, a deferral,
a backlog record), or decision for a human decision (--ref is its USER:
source). RESOLVED without --resolution, scope without --ref, decision
without a USER: ref, and --resolution with any other disposition are refused
before any request. A server that does not advertise finding_resolution
refuses before any request too: deploy the server first.

A packet edit does not change a requirement (SR-CLI-027-002): the server
snapshots each scoped member's content hash when a finding is raised, and a
packet-edit resolution is refused naming every member that changed since —
resolve it as scope or decision instead, or state the widening on the
human's word with --widens USER:<date>:<why>, which the server stores and
findings list -v prints. A finding raised before the snapshot existed is
not compared.

A finding already RESOLVED changes only its reference (REQ-CROSS-315): send
--ref with --expected-fingerprint and no --disposition, and the server keeps
the stored disposition and kind. A finding resolved before the kind existed
keeps its null kind on that path.

--file <dispositions.json> sets several in one call: a JSON array of
{id, scope, from, disposition, ref, resolution, widens}; resolution and
widens take the --resolution and --widens values and follow their rules,
checked per entry before any request. Only the listed findings change, and
each is written only while its current disposition is still "from"; a
mismatch is refused and reported, and the rest still run. A finding already
in the target disposition is reported unchanged. The call exits non-zero
when any disposition was not written. There is no flag that resolves every
open finding: a disposition names its finding.`,
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
	processFindingsAddCmd.Flags().StringVar(&findingsIntroducedBy, "introduced-by", "", "the earlier finding (same scope) whose resolution introduced the mechanism this one faults")
	processFindingsAddCmd.Flags().StringVar(&findingsFile, "file", "", "a JSON array of findings to record in one call")
	processFindingsAddCmd.Flags().StringVar(&findingsReviewContext, "review-context", "", "select an immutable review snapshot context id")

	processFindingsListCmd.Flags().StringVar(&findingsScope, "scope", "", "the scope as <kind>:<external-id>")
	processFindingsListCmd.Flags().BoolVar(&findingsAll, "all", false, "the whole system's findings, not only the piece you hold, grouped per scope")
	processFindingsListCmd.Flags().BoolVar(&findingsJSON, "json", false, "one JSON object on stdout: the rows (findings) and the per-scope round summary (rounds)")
	addPageFlags(processFindingsListCmd, &findingsPage)

	processFindingsDispositionCmd.Flags().StringVar(&findingsExternalID, "id", "", "the finding external id")
	processFindingsDispositionCmd.Flags().StringVar(&findingsDisposition, "disposition", "", "OPEN|RESOLVED|DEFERRED|REJECTED")
	processFindingsDispositionCmd.Flags().StringVar(&findingsDispositionRef, "ref", "", "the disposition reference (alone: a reference-only change on a RESOLVED finding)")
	processFindingsDispositionCmd.Flags().StringVar(&findingsResolution, "resolution", "", "with RESOLVED: packet-edit|scope|decision — how the finding resolved")
	processFindingsDispositionCmd.Flags().StringVar(&findingsWidens, "widens", "", "with --resolution packet-edit: USER:<date>:<why> — the packet edit widened a member on the human's word")
	processFindingsDispositionCmd.Flags().StringVar(&findingsExpectedFingerprint, "expected-fingerprint", "", "guard: the finding's current fingerprint (read for --scope when omitted)")
	processFindingsDispositionCmd.Flags().StringVar(&findingsScope, "scope", "", "the finding's scope as <kind>:<external-id>; the CLI reads the fingerprint")
	processFindingsDispositionCmd.Flags().StringVar(&findingsFrom, "from", "", "with --scope: the disposition you saw; written only while the finding is still in it")
	processFindingsDispositionCmd.Flags().StringVar(&findingsFile, "file", "", "a JSON array of {id, scope, from, disposition, ref, resolution, widens}")

	processFindingsCmd.AddCommand(processFindingsAddCmd, processFindingsListCmd, processFindingsDispositionCmd)
	processCmd.AddCommand(processFindingsCmd)
}
