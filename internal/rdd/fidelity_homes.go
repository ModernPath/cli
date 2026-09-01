package rdd

// REQ-CROSS-246 (EPIC-CLI-003 T10): the landing-home count groups. The merged
// landing model holds column families the corpus needs and the op path does
// not feed yet; before these groups an unfed home read as silence, which the
// flip's zero-silent-loss criterion cannot tell from "covered". Each group
// states its population beside its name (an undefined population is not a
// measurement) and takes its coverage from the ops the same build emitted, so
// the number moves the moment a carrier lands and never before.
//
// These are measurement groups, not losses: an unfed home is the disclosed
// pre-carrier state the flip gate reads, not a per-record accept key.

import (
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/modernpath/cli/internal/manifest"
)

const (
	homeDecisionsGroup   = "landing home: decision records (D-*/DEC-* declarations in decision-family sections → decision-gate ops)"
	homeAcceptancesGroup = "landing home: epic acceptances (WORKLIST Human-approval tags + approval-register rows → acceptance-carrying epic ops)"
	homeEvidenceGroup    = "landing home: historical evidence (RUN: tokens in ledgers, records, WORKLIST → evidence results built)"
	homeVerifRefsGroup   = "landing home: verification references (evidence-map data rows → verification_refs entries on criteria)"
	homeVerifRowsGroup   = "landing home: evidence rows resolved (SCN-led rows in an evidence map or carrying backticked refs → refs landing on the named criterion)"
	homeScenariosGroup   = "landing home: scenario records (captured scenario definitions → upsert_scenario ops)"
	homeBacklogGroup     = "landing home: backlog typed fields (backlog/gap rows with labeled facts → payloads carrying a typed field)"
	homeArchiveGroup     = "landing home: process-record archive (retired-family files → upsert_process_record ops)"
	// REQ-CROSS-264. The denominator is CORPUS-defined — declarations read from
	// the epic records and WORKLIST — never the gate being widened, which would
	// compare the parser against itself and stay at 100% whatever landed. The
	// exclusion classes are named in the group's own note.
	homeTasksGroup = "landing home: task records (declarations in epic records + WORKLIST rollup/work rows → upsert_task ops; excludes the 26 standalone TASK-*.md files and the ids appearing only in evidence-family sections)"
	// REQ-CROSS-266. The denominator is the ROW-side population: a scenario
	// declared in several tables contributes several cells and one row, so only
	// the row-side figure can reconcile against the store.
	// REQ-CROSS-265. Corpus-defined: rows under a DECLARING header whose cell
	// says something. The existing scenarios group compares the parser against
	// itself and cannot show this movement.
	homeEdgesGroup       = "landing home: scenario→requirement edges (scenario rows declaring under Realizes/Requirement headers → payloads carrying required_requirement_external_ids; SR-only and bare-token cells are named exclusions, unresolvable ids named losses)"
	homeConclusionsGroup = "landing home: scenario evidence conclusions (scenario rows whose source declares UPPER_VALIDATED/LOWER_VERIFIED → payloads carrying evidence_conclusion; a token outside the status family is a mention, named not claimed)"
)

var (
	// A decision-family section heading: `## Decisions`, `## Design decisions`,
	// `### Decision summary`, … — any heading whose text contains "decision".
	decisionHeadRe = regexp.MustCompile(`(?i)^##+\s+.*decision`)
	// A declaration inside such a section: a table row led by a D-* id, or a
	// bullet/bold lead. The id keeps its verbatim local suffix (letter
	// families like D-061a included).
	// Both id families, matching the builder's own declaration matchers. A
	// denominator that reaches less than the builder does measures the parser
	// against itself: while this counted `D-` alone it read 143/143 — 100% —
	// with 288 gates minted from declarations it never counted, so a break in
	// DEC-* handling would not have moved the number.
	decisionRowRe = []*regexp.Regexp{
		regexp.MustCompile("^\\|\\s*`?\\*{0,2}((?:DEC|D)-[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*)"),
		regexp.MustCompile(`^-\s+\*\*((?:DEC|D)-[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*)\*\*`),
	}
	runTokenRe = regexp.MustCompile(`RUN:\d{4}-\d{2}-\d{2}`)
	// An evidence-map section heading and its data rows.
	evidenceMapHeadRe  = regexp.MustCompile(`(?i)^##+\s+.*evidence map`)
	worklistApprovalRe = regexp.MustCompile("(?m)^\\|[^\n]*\\|\\s*(APP(?:ROVE)?-[A-Z0-9-]+)[^|\n]*`?USER:\\d{4}-\\d{2}-\\d{2}")
	// An approval-register row: an APP-*/APPROVE-* id cell on a row whose
	// state cell says approved (never "(pending)").
	registerApprovalRe = regexp.MustCompile(`(?im)^\|\s*(APP(?:ROVE)?-[A-Z0-9-]+)\s*\|[^\n]*\bapproved\b[^\n]*$`)
)

func (r *FidelityReport) scanLandingHomes(root string, m *manifest.Manifest, data Data, records map[string]string, ops []Op) {
	// coverage sources, from the ops this build actually emitted
	processRecordOps, decisionGateIDs := 0, map[string]bool{}
	acceptanceEpics, verifRefEntries := 0, 0
	scenarioPayloads := map[string]map[string]any{}
	backlogPayloads := map[string]map[string]any{}
	for _, op := range ops {
		switch op.Type {
		case "upsert_scenario":
			if id, _ := op.Payload["external_id"].(string); id != "" {
				scenarioPayloads[id] = op.Payload
			}
		case "upsert_process_record":
			processRecordOps++
		case "upsert_gate":
			if kind, _ := op.Payload["kind"].(string); kind == "decision" {
				if id, _ := op.Payload["external_id"].(string); strings.HasPrefix(id, "D-") {
					decisionGateIDs[id] = true
				}
			}
		case "upsert_epic":
			if _, ok := op.Payload["approval"]; ok {
				acceptanceEpics++
			}
			verifRefEntries += verifRefsIn(op.Payload, "scenarios")
		case "upsert_requirement":
			verifRefEntries += verifRefsIn(op.Payload, "criteria")
		case "upsert_backlog_record":
			if id, _ := op.Payload["external_id"].(string); id != "" {
				backlogPayloads[id] = op.Payload
			}
		}
	}

	// --- decisions: census over every epic record's decision-family sections
	decisions := FidelityCount{Group: homeDecisionsGroup}
	for _, text := range records {
		for _, id := range decisionDeclarations(text) {
			decisions.Rows++
			_ = id // coverage below is by count: the minted ids are epic-scoped
		}
	}
	decisions.Ops = min(len(decisionGateIDs), decisions.Rows)
	r.Counts = append(r.Counts, decisions)

	// --- acceptances: every WORKLIST Human-approval cell carrying a USER:
	// tag (the parsed cell, so tag-only cells count — the 119-DONE-epics
	// population) plus the records' register rows
	acceptances := FidelityCount{Group: homeAcceptancesGroup}
	for _, e := range data.Epics {
		if userTagRe.MatchString(e.ApprovalCell) {
			acceptances.Rows++
		}
	}
	for _, text := range records {
		acceptances.Rows += len(registerApprovalRe.FindAllString(text, -1))
	}
	acceptances.Ops = min(acceptanceEpics+registerGateCount(ops), acceptances.Rows)
	r.Counts = append(r.Counts, acceptances)

	// --- historical evidence: RUN: token occurrences
	evidence := FidelityCount{Group: homeEvidenceGroup}
	for _, family := range []string{manifest.DocRequirements, manifest.DocWorklist} {
		for _, file := range m.Resolve(root, family) {
			evidence.Rows += len(runTokenRe.FindAllString(readFileOr(file), -1))
		}
	}
	for _, text := range records {
		evidence.Rows += len(runTokenRe.FindAllString(text, -1))
	}
	evidence.Ops = min(BuildEvidenceImport(data, records).Occurrences, evidence.Rows)
	r.Counts = append(r.Counts, evidence)

	// --- verification references: evidence-map rows, resolution measured by
	// the same parser the builder runs — an unresolvable row is NAMED
	verifRefs := FidelityCount{Group: homeVerifRefsGroup}
	for _, text := range records {
		emap := ParseEvidenceMapRefs(text)
		verifRefs.Rows += emap.rows
		verifRefs.Ops += emap.resolved
		verifRefs.MissingFromOps = append(verifRefs.MissingFromOps, emap.unresolved...)
	}
	_ = verifRefEntries // the payload-side entry count backs the round-trip suites
	r.Counts = append(r.Counts, verifRefs)

	// --- evidence rows resolved: the same edge counted against a
	// corpus-defined denominator. The group above takes its rows from the
	// section gate it measures and so can never show that gate widening;
	// this one counts the rows the corpus writes and names, by file and
	// line, every one whose refs reach no criterion.
	r.Counts = append(r.Counts, evidenceRowCensus(data, records, ops))

	// --- scenario records: each distinct definition is covered only when the
	// emitted row carries the expected identity, content, lifecycle and source
	// position. Counts alone let an active row stand in for an obsolete one.
	scenarios := FidelityCount{Group: homeScenariosGroup}
	expectedScenarios := map[string]map[string]any{}
	requirementInventory := map[string]bool{}
	for _, op := range ops {
		if op.Type == "upsert_requirement" {
			if id, _ := op.Payload["external_id"].(string); id != "" {
				requirementInventory[id] = true
			}
		}
	}
	for _, e := range data.Epics {
		realizes := parseScenarioRealizes(records[e.ID], requirementInventory).bySCN
		addExpected := func(items []any, sourcePath string) {
			for _, raw := range items {
				item, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				id, _ := item["external_id"].(string)
				if id == "" || expectedScenarios[id] != nil {
					continue
				}
				expected := map[string]any{
					"external_id": id, "declared_owner_type": "epic",
					"declared_owner_external_id": e.ID, "source_path": sourcePath,
				}
				for _, key := range []string{"given", "when", "then", "statement", "status", "evidence_conclusion", "source_line", "source_raw"} {
					if value, present := item[key]; present {
						expected[key] = value
					}
				}
				scn := id
				if i := strings.LastIndex(id, "#"); i >= 0 {
					scn = id[i+1:]
				}
				if ids := realizes[scn]; len(ids) > 0 {
					expected["required_requirement_external_ids"] = ids
				}
				expectedScenarios[id] = expected
			}
		}
		sourcePath := e.Record
		if sourcePath == "" {
			sourcePath = e.ID
		}
		addExpected(parseScenarioRecords(records[e.ID], e.ID), sourcePath)
		for _, spec := range e.Specs {
			addExpected(parseScenarioRecords(spec.Content, e.ID), spec.Rel)
		}
	}
	scenarios.Rows = len(expectedScenarios)
	for id, expected := range expectedScenarios {
		payload := scenarioPayloads[id]
		if payload == nil {
			scenarios.MissingFromOps = append(scenarios.MissingFromOps, id)
			continue
		}
		complete := true
		for key, want := range expected {
			if got, ok := payload[key]; !ok || !reflect.DeepEqual(got, want) {
				scenarios.MissingFromOps = append(scenarios.MissingFromOps, id+"."+key)
				complete = false
			}
		}
		if complete {
			scenarios.Ops++
		}
	}
	for id := range scenarioPayloads {
		if expectedScenarios[id] == nil {
			scenarios.ExtraInOps = append(scenarios.ExtraInOps, id)
		}
	}
	sort.Strings(scenarios.MissingFromOps)
	sort.Strings(scenarios.ExtraInOps)
	r.Counts = append(r.Counts, scenarios)

	// --- backlog typed fields: a row is covered only when every parsed fact
	// reaches its matching payload field. raw_body preserves bytes but cannot
	// substitute for a missing structured field.
	backlog := FidelityCount{Group: homeBacklogGroup}
	for _, row := range data.Backlog {
		expected := backlogTypedExpectation(row)
		if len(expected) == 0 {
			continue
		}
		backlog.Rows++
		payload := backlogPayloads[row.ExternalID]
		complete := payload != nil
		for key, want := range expected {
			if got, ok := payload[key]; !ok || !reflect.DeepEqual(got, want) {
				backlog.MissingFromOps = append(backlog.MissingFromOps, row.ExternalID+"."+key)
				complete = false
			}
		}
		if complete {
			backlog.Ops++
		}
	}
	sort.Strings(backlog.MissingFromOps)
	r.Counts = append(r.Counts, backlog)

	// --- process-record archive: the archival inventory against its two
	// carrier kinds — the byte archive, plus the spec files that ride their
	// epic payloads by design (planning_artifacts stays the spec home)
	archive := FidelityCount{Group: homeArchiveGroup}
	for _, c := range r.Counts {
		if strings.HasPrefix(c.Group, "archival-coverage") {
			archive.Rows = c.Rows
		}
	}
	specCarried := map[string]bool{}
	for _, op := range ops {
		if op.Type != "upsert_epic" {
			continue
		}
		if specs, ok := op.Payload["specs"].([]any); ok {
			for _, raw := range specs {
				if sm, ok := raw.(map[string]any); ok {
					if id, _ := sm["external_id"].(string); id != "" {
						specCarried[id] = true
					}
				}
			}
		}
	}
	archive.Ops = min(processRecordOps+len(specCarried), archive.Rows)
	r.Counts = append(r.Counts, archive)

	r.scanTaskHome(data, records, ops)
	r.scanConclusionHome(data, records, ops)
	r.scanEdgeHome(data, records, requirementInventory, ops)
}

// scanEdgeHome measures REQ-CROSS-265's carrier. The denominator is
// CORPUS-defined — rows whose declaring column declares something — and every
// row that produced no edge is named in its own class, so the shortfall is
// readable rather than a bare percentage.
func (r *FidelityReport) scanEdgeHome(data Data, records map[string]string, known map[string]bool, ops []Op) {
	carried := map[string]bool{}
	for _, op := range ops {
		if op.Type != "upsert_scenario" {
			continue
		}
		if len(asAnySlice(op.Payload["required_requirement_external_ids"])) == 0 {
			continue
		}
		if id, _ := op.Payload["external_id"].(string); id != "" {
			carried[id] = true
		}
	}

	// Units are SCENARIOS on both sides. A scenario declared in several tables
	// contributes several rows and one record, so a row-side numerator against
	// a scenario-side denominator could not reconcile — the same unit mistake
	// the conclusions group avoids.
	group := FidelityCount{Group: homeEdgesGroup}
	declaring := map[string]bool{}
	for _, e := range data.Epics {
		texts := []string{records[e.ID]}
		for _, spec := range e.Specs {
			texts = append(texts, spec.Content)
		}
		for _, text := range texts {
			res := parseScenarioRealizes(text, known)
			for _, scn := range res.declaredIDs {
				declaring[e.ID+"#"+scn] = true
			}
			// The other declaring shape: a reference written between the id and
			// the triple, which is the Realizes column in the notation a bullet
			// record uses. Leaving it out of the denominator would measure the
			// table carrier against a population that includes neither form's
			// scenarios consistently.
			for _, raw := range parseScenarioRecords(text, e.ID) {
				item, _ := raw.(map[string]any)
				if item == nil || len(asAnySlice(item["declared_refs"])) == 0 {
					continue
				}
				if id, _ := item["external_id"].(string); id != "" {
					declaring[id] = true
				}
			}
			group.MissingFromOps = append(group.MissingFromOps, res.srOnly...)
			group.MissingFromOps = append(group.MissingFromOps, res.bareToken...)
			group.MissingFromOps = append(group.MissingFromOps, res.unresolved...)
		}
	}
	for id := range declaring {
		group.Rows++
		if carried[id] {
			group.Ops++
		}
	}
	sort.Strings(group.MissingFromOps)
	r.Counts = append(r.Counts, group)
}

// scanConclusionHome measures REQ-CROSS-266's carrier. The denominator is
// CORPUS-defined: every scenario row whose own source text declares one of the
// two conclusion tokens. Measured against the parser's own output the group
// would read 100% whatever landed, which is the failure mode the landing-home
// groups exist to avoid.
func (r *FidelityReport) scanConclusionHome(data Data, records map[string]string, ops []Op) {
	carried := map[string]string{}
	for _, op := range ops {
		if op.Type != "upsert_scenario" {
			continue
		}
		id, _ := op.Payload["external_id"].(string)
		if id == "" {
			continue
		}
		if c, _ := op.Payload["evidence_conclusion"].(string); c != "" {
			carried[id] = c
		}
	}

	group := FidelityCount{Group: homeConclusionsGroup}
	seen := map[string]bool{}
	declaring := func(items []any) {
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			if item == nil {
				continue
			}
			id, _ := item["external_id"].(string)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			source, _ := item["source_raw"].(string)
			if !evidenceConclusionRe.MatchString(source) {
				// An id declared in several shapes keeps the richer shape's
				// source text, so a conclusion stated by a table row can
				// survive on a row whose stored source is the heading block
				// that outranked it. Carried, but outside a denominator
				// defined by the stored source — named rather than folded in,
				// because a denominator that moves with the parser is not a
				// corpus measurement any more.
				if carried[id] != "" {
					group.ExtraInOps = append(group.ExtraInOps,
						id+": conclusion declared by a shape the richer one replaced; carried, outside the source-text denominator")
				}
				continue
			}
			group.Rows++
			if carried[id] != "" {
				group.Ops++
				continue
			}
			// Named, never silent: the token is in the row but not in a
			// status-family cell, so it states some other item's evidence.
			group.MissingFromOps = append(group.MissingFromOps,
				id+": the conclusion token sits outside the status family — carried verbatim in source_raw, claimed as no evidence fact")
		}
	}
	for _, e := range data.Epics {
		declaring(parseScenarioRecords(records[e.ID], e.ID))
		for _, spec := range e.Specs {
			declaring(parseScenarioRecords(spec.Content, e.ID))
		}
	}
	sort.Strings(group.MissingFromOps)
	sort.Strings(group.ExtraInOps)
	r.Counts = append(r.Counts, group)
}

// scanTaskHome measures REQ-CROSS-264's carrier: corpus declarations against
// the ops this same build emitted. A declaration whose epic prefix resolves to
// nothing is a NAMED loss inside the group — the guard flags, it does not
// delete — and the shadowed within-epic duplicates are flagged as hygiene at
// their own lines.
func (r *FidelityReport) scanTaskHome(data Data, records map[string]string, ops []Op) {
	emitted := map[string]bool{}
	for _, op := range ops {
		if op.Type != "upsert_task" {
			continue
		}
		if id, _ := op.Payload["external_id"].(string); id != "" {
			emitted[id] = true
		}
	}
	census := TaskDeclarations(data, records)
	tasks := FidelityCount{Group: homeTasksGroup}
	for _, d := range census.Decls {
		tasks.Rows++
		if emitted[d.ExternalID()] {
			tasks.Ops++
			continue
		}
		tasks.MissingFromOps = append(tasks.MissingFromOps, d.ExternalID())
	}
	for _, loss := range census.UnresolvedEpic {
		tasks.Rows++
		tasks.MissingFromOps = append(tasks.MissingFromOps, loss)
	}
	for id := range emitted {
		found := false
		for _, d := range census.Decls {
			if d.ExternalID() == id {
				found = true
				break
			}
		}
		if !found {
			tasks.ExtraInOps = append(tasks.ExtraInOps, id)
		}
	}
	sort.Strings(tasks.MissingFromOps)
	sort.Strings(tasks.ExtraInOps)
	r.Counts = append(r.Counts, tasks)

	for _, sh := range census.Shadowed {
		r.Hygiene = append(r.Hygiene, FidelityHygiene{
			File: sh.SourcePath, Line: sh.Line, Detail: sh.Detail,
		})
	}
	// D-T4-5: a digit-width-mismatched range is a corpus-drift flag, not a
	// row the op path failed to carry — nothing was ever expanded from it —
	// so it joins Shadowed in Hygiene rather than Counts.
	for _, mm := range census.MismatchedRanges {
		r.Hygiene = append(r.Hygiene, FidelityHygiene{
			File: mm.SourcePath, Line: mm.Line, Detail: mm.Detail,
		})
	}
}

func backlogTypedExpectation(row BacklogRow) map[string]any {
	expected := map[string]any{}
	for key, value := range map[string]string{
		"raised_at":       row.RaisedAt,
		"raised_by":       row.RaisedBy,
		"disposition":     row.Disposition,
		"disposition_ref": row.DispositionRef,
		"candidate_route": row.CandidateRoute,
		"why_unrouted":    row.WhyUnrouted,
		"gap_kind":        row.GapKind,
	} {
		if value != "" {
			expected[key] = value
		}
	}
	if len(row.AffectedIDs) > 0 {
		ids := make([]any, len(row.AffectedIDs))
		for i, id := range row.AffectedIDs {
			ids[i] = id
		}
		expected["affected_external_ids"] = ids
	}
	if len(expected) == 0 {
		return expected
	}
	if row.RawBody != "" {
		expected["raw_body"] = row.RawBody
	}
	return expected
}

// registerGateCount counts the answered acceptance gates the batch emits —
// the register rows' own carriers.
func registerGateCount(ops []Op) int {
	n := 0
	for _, op := range ops {
		if op.Type != "upsert_gate" {
			continue
		}
		if kind, _ := op.Payload["kind"].(string); kind != "approval_request" {
			continue
		}
		if state, _ := op.Payload["state"].(string); state == "answered" {
			n++
		}
	}
	return n
}

// decisionDeclarations is the census REQ-CROSS-247's emitter deepens: every
// D-* declaration in a decision-family section, verbatim local suffix kept.
func decisionDeclarations(recordText string) []string {
	var out []string
	inSection := false
	for _, line := range strings.Split(recordText, "\n") {
		if strings.HasPrefix(line, "#") {
			inSection = decisionHeadRe.MatchString(line)
			continue
		}
		if !inSection {
			continue
		}
		for _, re := range decisionRowRe {
			if m := re.FindStringSubmatch(line); m != nil {
				out = append(out, m[1])
				break
			}
		}
	}
	return out
}

// sectionDataRows counts the table data rows of every section whose heading
// matches. Header rows are un-counted when their separator row appears; a
// separator-less table keeps its first row counted, erring toward a larger
// denominator — the honest direction for a coverage instrument.
func sectionDataRows(text string, headRe *regexp.Regexp) int {
	count, inSection := 0, false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "#") {
			inSection = headRe.MatchString(line)
			continue
		}
		if !inSection {
			continue
		}
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "|") {
			continue
		}
		body := strings.TrimSpace(strings.Trim(t, "|"))
		if body == "" {
			continue
		}
		if strings.Trim(body, "-:| ") == "" {
			// the separator under a header — the row counted just before it
			// was the header, not data
			if count > 0 {
				count--
			}
			continue
		}
		count++
	}
	return count
}

// readFileOr returns a file's content, or "" when it cannot be read — a
// census over an absent file is an empty census, never an error.
func readFileOr(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func verifRefsIn(payload map[string]any, listKey string) int {
	total := 0
	items, _ := payload[listKey].([]any)
	for _, raw := range items {
		if item, ok := raw.(map[string]any); ok {
			if refs, ok := item["verification_refs"].([]any); ok {
				total += len(refs)
			}
		}
	}
	return total
}

func stringsOf(payload map[string]any, key string) string {
	s, _ := payload[key].(string)
	return s
}

// EvidenceImport is what the historical-evidence phase will post to
// POST /api/v1/sync/evidence — runs with per-target results. REQ-CROSS-249
// (T12) fills the builder; until then the report honestly counts zero built
// results against the corpus's RUN: tokens.
type EvidenceImport struct {
	Runs    []map[string]any
	Results []map[string]any
	// Occurrences counts every corpus token the results cover — a line
	// repeating one tag, and an identical line repeated elsewhere, collapse
	// into that line's single result, a transform the coverage arithmetic
	// must not read as loss.
	Occurrences int
	// Collapsed counts the occurrences that landed on a line result which
	// already existed. Disclosed rather than silent: a collapse nobody counts
	// reads as "covered everything".
	Collapsed int
}

// BuildEvidenceImport parses the corpus's historical RUN: tokens into
// evidence payloads. T12's implementation lands in evidence_import.go; this
// declaration moved there.
