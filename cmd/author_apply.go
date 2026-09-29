package cmd

// REQ-CROSS-442 (EPIC-CLI-TURNS): `author apply --file` records an epic, its
// requirements, relations and membership from one file. The file is an input
// format for working-set push's write engine (PR #694 review): each plan
// record compiles into the authoring.Record push would read from an item
// file, the whole plan is validated before the first write, and each record
// is one atomic patch — fields, criteria, relations and membership together —
// carrying the authoring context. A record the store does not know is created,
// then patched. For a record that exists, the fingerprint the patch guards on
// is the one the plan pins (expected_fingerprint) or the one its working-set
// pull recorded, never one read at run time, so an edit made since is refused
// rather than overwritten. Legality and attribution stay with the server.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/modernpath/cli/internal/authoring"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type applyEpic struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// ExpectedFingerprint is the fingerprint the plan was written against.
	ExpectedFingerprint string `json:"expected_fingerprint"`
}

type applyRequirement struct {
	ID                 string   `json:"id"`
	Kind               string   `json:"kind"`
	Context            string   `json:"context"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	Rationale          string   `json:"rationale"`
	Boundary           string   `json:"boundary"`
	VerificationMethod string   `json:"verification_method"`
	Criteria           []any    `json:"criteria"`
	Parents            []string `json:"parents"`
	// REQ-CROSS-458: a small change's lane class (set before its narrow
	// review) and its source tags — the lane enters only a sourced SR.
	LaneClass string   `json:"lane_class"`
	Sources   []string `json:"sources"`
	// ExpectedFingerprint: as on the epic.
	ExpectedFingerprint string `json:"expected_fingerprint"`
}

type applyFile struct {
	Epic         *applyEpic         `json:"epic"`
	Requirements []applyRequirement `json:"requirements"`
	Members      []string           `json:"members"`
}

var (
	authorApplyFile     string
	authorApplyDryRun   bool
	authorApplyFromPull bool
)

var authorApplyCmd = &cobra.Command{
	Use:   "apply --file <plan.yaml|json>",
	Short: "Record an epic, its requirements, relations and membership from one file",
	Long: `Record a whole plan in one call. The file is YAML or JSON:

  epic: {id, title, description, expected_fingerprint}
  requirements:
    - {id, kind: ur|sr, context, title, description, rationale, boundary,
       verification_method, criteria: [...], parents: [UR ids],
       lane_class, sources: [USER:… source tags], expected_fingerprint}
  members: [requirement ids]

The plan goes through the same write engine as working-set push. Every
record is checked before the first write; one invalid record stops the whole
call and nothing is written. A record the store does not know is created,
then patched. Each record's changes — the fields that differ (an empty field
in the file is left alone), its criteria, relations and membership — go in
one atomic patch that carries the authoring context.

A record that already exists is guarded by the version the plan was written
against: its expected_fingerprint in the plan, or else the fingerprint of its
working-set pull (.modernpath/working-set/<id>.md or a scope pull's member
file). A record that would change and has neither is refused; so is one whose
fingerprint moved since — pull it again and keep the other change in the plan.
A record the plan would not change needs neither.

Each record's line shows its result and fingerprint; an updated record's line
shows <replaced> -> <new>. Running the same file again writes nothing and
says each record is unchanged. --dry-run prints the plan and writes nothing.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		plan, err := readApplyFile(authorApplyFile)
		if err != nil {
			return err
		}
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return runAuthorApply(env, plan, authorApplyDryRun)
	},
}

func init() {
	authorApplyCmd.Flags().StringVar(&authorApplyFile, "file", "", "the plan file, YAML or JSON (required)")
	authorApplyCmd.Flags().BoolVar(&authorApplyDryRun, "dry-run", false, "print the plan and write nothing")
	authorApplyCmd.Flags().BoolVar(&authorApplyFromPull, "from-pull", false, "no effect: the working-set pull is always used when the plan pins no fingerprint")
	_ = authorApplyCmd.Flags().MarkDeprecated("from-pull", "the working-set pull is always used when the plan pins no fingerprint")
	_ = authorApplyCmd.MarkFlagRequired("file")
	authorCmd.AddCommand(authorApplyCmd)
}

// readApplyFile reads YAML or JSON (JSON is YAML) into the plan, refusing an
// unknown field so a misspelt key is not silently dropped.
func readApplyFile(path string) (*applyFile, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("--file: %w", err)
	}
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("--file %s: %w", path, err)
	}
	asJSON, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("--file %s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(asJSON))
	dec.DisallowUnknownFields()
	var plan applyFile
	if err := dec.Decode(&plan); err != nil {
		return nil, fmt.Errorf("--file %s: %w", path, err)
	}
	if plan.Epic != nil && plan.Epic.ID == "" {
		return nil, fmt.Errorf("--file %s: the epic needs an id", path)
	}
	for i, r := range plan.Requirements {
		if r.ID == "" {
			return nil, fmt.Errorf("--file %s: requirement %d needs an id", path, i+1)
		}
		switch strings.ToLower(r.Kind) {
		case "ur", "sr", "":
		default:
			return nil, fmt.Errorf("--file %s: %s kind %q: expected ur or sr", path, r.ID, r.Kind)
		}
	}
	if len(plan.Members) > 0 && plan.Epic == nil {
		return nil, fmt.Errorf("--file %s: members need the epic they belong to", path)
	}
	return &plan, nil
}

func (p *applyFile) ids() []string {
	var ids []string
	if p.Epic != nil {
		ids = append(ids, p.Epic.ID)
	}
	for _, r := range p.Requirements {
		ids = append(ids, r.ID)
	}
	return ids
}

// snapshotFingerprint is the fingerprint a working-set pull recorded for id:
// the by-id snapshot (.modernpath/working-set/<id>.md) first, else a scope
// pull's member or scope file. Empty when the record was never pulled.
func snapshotFingerprint(root, id string) (string, string) {
	if unsafeSnapshotName(id) {
		return "", ""
	}
	dir := filepath.Join(root, workingSetDir)
	candidates := []string{filepath.Join(dir, id+".md")}
	scoped, _ := filepath.Glob(filepath.Join(dir, "*", "members", id+".md"))
	own, _ := filepath.Glob(filepath.Join(dir, id, id+".md"))
	sort.Strings(scoped)
	candidates = append(candidates, append(own, scoped...)...)
	for _, path := range candidates {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(raw), "\n") {
			for _, key := range []string{"- **Fingerprint:** ", "- **Served fingerprint:** "} {
				if v, ok := strings.CutPrefix(line, key); ok && strings.TrimSpace(v) != "" {
					return strings.TrimSpace(v), path
				}
			}
		}
	}
	return "", ""
}

// applyRecord is one plan record on its way into the engine.
type applyRecord struct {
	id, kind string // kind: the render kind, epic | user | system
	scalars  []authoring.Scalar
	parents  []string
	sources  []string
	members  []string
	criteria []any
	expected string
}

func (p *applyFile) records() []applyRecord {
	var out []applyRecord
	if e := p.Epic; e != nil {
		out = append(out, applyRecord{id: e.ID, kind: "epic", members: p.Members, expected: e.ExpectedFingerprint,
			scalars: []authoring.Scalar{{Key: "title", Value: e.Title}, {Key: "description", Value: e.Description}}})
	}
	for _, r := range p.Requirements {
		kind := "system"
		if strings.EqualFold(r.Kind, "ur") {
			kind = "user"
		}
		out = append(out, applyRecord{id: r.ID, kind: kind, parents: r.Parents, sources: r.Sources,
			criteria: r.Criteria, expected: r.ExpectedFingerprint,
			scalars: []authoring.Scalar{{Key: "title", Value: r.Title}, {Key: "context", Value: r.Context},
				{Key: "description", Value: r.Description}, {Key: "rationale", Value: r.Rationale},
				{Key: "boundary", Value: r.Boundary}, {Key: "verification_method", Value: r.VerificationMethod},
				{Key: "lane_class", Value: r.LaneClass}}})
	}
	return out
}

// edited is the record as push would read it from an edited item file: the
// served record, with each field the plan sets and each relation, member and
// source it adds. An empty field in the plan leaves the served value alone.
func (a applyRecord) edited(base *scopeRecord) authoring.Record {
	rec := authoring.Record{Kind: a.kind, ExternalID: a.id}
	held := map[string]bool{}
	if base != nil {
		rec = recordFromPayload(*base, nil)
		for _, r := range rec.Relations {
			held[r.Target] = true
		}
		held[str(base.payload, "parent_external_id")] = true
		for _, id := range stringSlice(base.payload["parent_external_ids"]) {
			held[id] = true
		}
	}
	for _, s := range a.scalars {
		if s.Value == "" {
			continue
		}
		set := false
		for i := range rec.Scalars {
			if rec.Scalars[i].Key == s.Key {
				rec.Scalars[i].Value, set = s.Value, true
			}
		}
		if !set {
			rec.Scalars = append(rec.Scalars, s)
		}
	}
	for _, p := range a.parents {
		if !held[p] {
			held[p] = true
			rec.Relations = append(rec.Relations, authoring.Relation{Direction: "derives", Target: p, Authority: "confirmed"})
		}
	}
	have := map[string]bool{}
	for _, m := range rec.Members {
		have[m] = true
	}
	for _, m := range a.members {
		if !have[m] {
			have[m] = true
			rec.Members = append(rec.Members, m)
		}
	}
	var served []any
	if base != nil {
		served = anyList(base.payload["source_citations"])
	}
	cited := map[string]bool{}
	for _, raw := range served {
		if c, ok := raw.(map[string]any); ok {
			cited[firstNonEmpty(str(c, "source_tag"), str(c, "ref"))] = true
		}
	}
	for _, tag := range a.sources {
		if !cited[tag] {
			cited[tag] = true
			rec.SourceCitations = append(rec.SourceCitations, "process_source: "+tag)
		}
	}
	return rec
}

// validate names what the server would refuse, before any write.
func (a applyRecord) validate(base *scopeRecord) []string {
	var out []string
	for _, s := range a.scalars {
		if s.Key == "lane_class" && s.Value != "" && a.kind != "system" {
			out = append(out, a.id+": only a system requirement carries a lane class")
		}
	}
	for i, raw := range a.criteria {
		c, ok := raw.(map[string]any)
		if !ok || strings.TrimSpace(str(c, "external_id")) == "" {
			out = append(out, fmt.Sprintf("%s: criterion %d needs an external_id — the replace-set keys on it", a.id, i+1))
		}
	}
	if base != nil && base.kind != a.kind {
		out = append(out, fmt.Sprintf("%s: the plan says %s, but the store holds a %s record", a.id, a.kind, base.kind))
	}
	return out
}

// servedCriteria is the acceptance set the read serves: criteria on an SR,
// scenarios on a UR.
func servedCriteria(p map[string]any) []any {
	for _, key := range []string{"criteria", "scenarios", "acceptance_scenarios"} {
		if items, ok := p[key].([]any); ok {
			return items
		}
	}
	return nil
}

// criteriaDiffer compares by external_id, and only on the keys the file sets:
// the store serves extra keys (ids, positions) the file never names.
func criteriaDiffer(served, want []any) bool {
	if len(served) != len(want) {
		return true
	}
	byID := map[string]map[string]any{}
	for _, s := range served {
		if m, ok := s.(map[string]any); ok {
			byID[str(m, "external_id")] = m
		}
	}
	for _, w := range want {
		wm, ok := w.(map[string]any)
		if !ok {
			return true
		}
		sm, ok := byID[str(wm, "external_id")]
		if !ok {
			return true
		}
		for key, v := range wm {
			a, _ := json.Marshal(v)
			b, _ := json.Marshal(sm[key])
			if !bytes.Equal(a, b) {
				return true
			}
		}
	}
	return false
}

func runAuthorApply(env *factoryEnv, plan *applyFile, dryRun bool) error {
	ids := plan.ids()
	current, err := scopeIndex(env, ids)
	if err != nil {
		return err
	}
	for id, item := range current {
		if item.kind != "epic" && item.kind != "system" && item.kind != "user" {
			return fmt.Errorf("%s is a %s record, not an epic or a requirement", id, item.kind)
		}
	}

	// PLAN PASS — every record is compiled, diffed and guarded before any write.
	var problems []string
	var writes []*recordWrite
	for _, a := range plan.records() {
		var base *scopeRecord
		if rec, ok := current[a.id]; ok {
			base = &rec
		}
		if bad := a.validate(base); len(bad) > 0 {
			problems = append(problems, bad...)
			continue
		}
		w, err := planRecordWrite("the plan", base, a.edited(base), nil)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if a.criteria != nil && (base == nil && len(a.criteria) > 0 || base != nil && criteriaDiffer(servedCriteria(base.payload), a.criteria)) {
			w.patch.Criteria, w.patch.CriteriaSet = a.criteria, true
		}
		if base != nil && w.changes() {
			now := str(base.payload, "fingerprint")
			want, from := a.expected, "the plan's expected_fingerprint"
			if want == "" {
				var path string
				if want, path = snapshotFingerprint(env.Root, a.id); want != "" {
					from = "its working-set pull (" + path + ")"
				}
			}
			switch {
			case want == "":
				problems = append(problems, fmt.Sprintf("%s would change, but the plan pins no expected_fingerprint and it has no working-set pull — run `working-set pull %s` before writing the plan, or pin the expected_fingerprint it was written against",
					a.id, a.id))
				continue
			case want != now:
				problems = append(problems, fmt.Sprintf("%s changed since %s: planned against %s, now %s — re-read it with `working-set pull %s`, keep the other change in the plan, and run it again",
					a.id, from, want, now, a.id))
				continue
			}
			w.expected = want
		}
		writes = append(writes, w)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d problem(s) in the plan; nothing was written:\n  %s", len(problems), strings.Join(problems, "\n  "))
	}

	width := 0
	for _, id := range ids {
		width = max(width, len(id))
	}
	byID := map[string]*recordWrite{}
	planned := 0
	for _, w := range writes {
		byID[w.ext] = w
		if w.changes() {
			planned++
		}
	}
	if dryRun {
		for _, id := range ids {
			what := "unchanged"
			if w := byID[id]; w != nil && w.changes() {
				what = w.describe()
			}
			fmt.Printf("%-*s  %s\n", width, id, what)
		}
		printInfo("dry run: %d record(s) to write, nothing written", planned)
		return nil
	}

	var changing []*recordWrite
	for _, w := range writes {
		if w.changes() {
			changing = append(changing, w)
		}
	}
	results := map[string]*writeResult{}
	var conflicts []string
	var werr error
	if len(changing) > 0 {
		results, conflicts, werr = applyRecordWrites(env, newContextID("authoring"), changing)
	}
	// A record apply updated moves its working-set pull to the fingerprint the
	// write returned, so the same session's next apply guards on it.
	notes := map[string]string{}
	for _, w := range changing {
		if res := results[w.ext]; res != nil && res.patched && !res.created && res.fingerprint != "" {
			notes[w.ext] = refreshPullSnapshots(env.Root, w.ext, w.expected, res.fingerprint)
		}
	}
	printApplyLines(ids, width, current, byID, results, notes)
	if werr != nil {
		return fmt.Errorf("%w — the run stopped there. %s", werr, applySummary(ids, byID, results))
	}
	if len(conflicts) > 0 {
		var moved, exist []string
		for _, id := range conflicts {
			if results[id].exists {
				exist = append(exist, id)
			} else {
				moved = append(moved, id)
			}
		}
		var parts []string
		if len(moved) > 0 {
			parts = append(parts, fmt.Sprintf("%d record(s) conflicted — the store moved since the fingerprint the plan guarded on: %s; run `working-set pull %s`, keep the other change in the plan, and apply it again",
				len(moved), strings.Join(moved, ", "), strings.Join(moved, " ")))
		}
		if len(exist) > 0 {
			parts = append(parts, fmt.Sprintf("%d record(s) already exist — created since the plan was read: %s; run `working-set pull %s` and apply the plan again",
				len(exist), strings.Join(exist, ", "), strings.Join(exist, " ")))
		}
		return fmt.Errorf("%s", strings.Join(parts, "; "))
	}
	return nil
}

// applySummary names what a stopped run wrote: the records written, a record
// created whose patch never ran with the pull that recovers it, and the
// records not written.
func applySummary(ids []string, byID map[string]*recordWrite, results map[string]*writeResult) string {
	var written, pending, not []string
	for _, id := range ids {
		w, res := byID[id], results[id]
		if w == nil || !w.changes() {
			continue
		}
		switch {
		case res == nil:
			not = append(not, id)
		case res.patchPending(w):
			pending = append(pending, id)
		case res.created:
			written = append(written, id+" (created)")
		case res.patched:
			written = append(written, id+" (updated)")
		default:
			not = append(not, id)
		}
	}
	var b strings.Builder
	if len(written) == 0 && len(pending) == 0 {
		b.WriteString("Nothing was written.")
	}
	if len(written) > 0 {
		fmt.Fprintf(&b, "Written: %s.", strings.Join(written, ", "))
	}
	if len(pending) > 0 {
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		fmt.Fprintf(&b, "Created, not patched: %s — run `working-set pull %s` to record its fingerprint, fix the refusal, then apply the plan again.",
			strings.Join(pending, ", "), strings.Join(pending, " "))
	}
	if len(not) > 0 {
		fmt.Fprintf(&b, " Not written: %s.", strings.Join(not, ", "))
	}
	return b.String()
}

func printApplyLines(ids []string, width int, current map[string]scopeRecord, byID map[string]*recordWrite, results map[string]*writeResult, notes map[string]string) {
	for _, id := range ids {
		res, w := results[id], byID[id]
		before := str(current[id].payload, "fingerprint")
		switch {
		case res == nil || (!res.created && !res.patched && !res.conflict):
			if w != nil && w.changes() {
				fmt.Printf("%-*s  not written\n", width, id)
			} else {
				fmt.Printf("%-*s  unchanged  %s\n", width, id, before)
			}
		case res.exists:
			fmt.Printf("%-*s  already exists — created since the plan was read; not written\n", width, id)
		case res.created && res.conflict:
			fmt.Printf("%-*s  created, not patched — conflict: the store moved since %s\n", width, id, w.expected)
		case res.patchPending(w):
			fmt.Printf("%-*s  created, not patched\n", width, id)
		case res.conflict:
			fmt.Printf("%-*s  conflict — the store moved since %s\n", width, id, w.expected)
		case res.created:
			fmt.Printf("%-*s  created  %s\n", width, id, res.fingerprint)
		default:
			fmt.Printf("%-*s  updated  %s -> %s%s\n", width, id, w.expected, res.fingerprint, notes[id])
		}
	}
}

// refreshPullSnapshots moves the working-set pull of id from old to the
// fingerprint its write returned, and returns a note for the success line.
//
// The by-id snapshot (.modernpath/working-set/<id>.md) is a read-only copy:
// its fingerprint line is rewritten, with the written-body hash, so the next
// pull does not take the change for a local edit. Its source identity stays
// at the pull, so `working-set check` still reports it stale against the
// store, and the note says its content is from before the write. A snapshot
// edited locally, or one pulled at another fingerprint, is left alone and the
// note says to re-pull it.
//
// A scope pull's file is never rewritten: push diffs it against the store,
// and a moved fingerprint over the old content would let the next push revert
// this write. The note says to re-pull the scope.
func refreshPullSnapshots(root, id, old, fp string) string {
	if unsafeSnapshotName(id) || old == "" {
		return ""
	}
	var notes []string
	dir := filepath.Join(root, workingSetDir)
	byID := filepath.Join(dir, id+".md")
	if raw, err := os.ReadFile(byID); err == nil {
		if !rewriteSnapshotFingerprint(byID, id, string(raw), old, fp) {
			notes = append(notes, fmt.Sprintf("re-pull it: `working-set pull %s` (its snapshot was not at %s or was edited)", id, old))
		} else {
			notes = append(notes, fmt.Sprintf("its snapshot now guards on %s, but its content is from before this write; re-pull (`working-set pull %s`) before copying from it", fp, id))
		}
	}
	scoped, _ := filepath.Glob(filepath.Join(dir, "*", "members", id+".md"))
	own, _ := filepath.Glob(filepath.Join(dir, id, id+".md"))
	if len(scoped)+len(own) > 0 {
		notes = append(notes, "re-pull its scope (`working-set pull --scope`) before the next push or apply")
	}
	if len(notes) == 0 {
		return ""
	}
	return " — " + strings.Join(notes, "; ")
}

// rewriteSnapshotFingerprint rewrites the first fingerprint line of a by-id
// snapshot from old to fp. It reports false, writing nothing, when the
// snapshot does not parse, was edited locally, or does not hold old.
func rewriteSnapshotFingerprint(path, id, content, old, fp string) bool {
	snap, err := parseWorkingSetSnapshot(id+".md", content)
	if err != nil || !snapshotBodyMatches(snap) {
		return false
	}
	const key = "- **Fingerprint:** "
	line := key + old + "\n"
	first := strings.Index(snap.body, key)
	if first < 0 || !strings.HasPrefix(snap.body[first:], line) {
		return false
	}
	body := snap.body[:first] + key + fp + "\n" + snap.body[first+len(line):]
	head := strings.TrimSuffix(content, snap.body)
	if len(head) == len(content) {
		return false
	}
	head = strings.Replace(head, writtenBodyKey+snap.writtenBody, writtenBodyKey+sha256Hex([]byte(body)), 1)
	return atomicWrite(path, []byte(head+body)) == nil
}
