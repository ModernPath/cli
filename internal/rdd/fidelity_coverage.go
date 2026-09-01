package rdd

// REQ-CROSS-221, second arm — the fidelity report measured against the DISK,
// not against the parsed snapshot.
//
// Every check in fidelity.go reconciles what the parsers produced against what
// the op payloads carry. That arm cannot see a loss the parser took before the
// snapshot existed: a record the extractor deduplicated away, a scenario
// written in a shape the section regex does not read, a retired file no
// carrier ever names, a cell whose prose the reference splitter discarded, a
// second user requirement the epic declares and the payload never mentions.
// Those losses are invisible precisely because both sides of the existing
// comparison agree — they agree on a corpus that already lost the content.
//
// These instruments therefore start from the file system and the raw record
// text and diff BOTH directions against the emitted ops. They report; they
// change no payload and repair nothing. Every counter states the population it
// counted, because a coverage number without its population criteria is a
// claim, not a measurement.

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/modernpath/cli/internal/manifest"
)

// RetiredPathFamilies is the exact path population the authority flip retires
// — the tracked process-state files whose authority moves to the server store.
// It lives here rather than beside the flip command because the fidelity
// report is what proves the population has a carrier BEFORE the flip runs;
// two copies of the list would let the proof and the retirement disagree.
var RetiredPathFamilies = []string{
	"tasks/*-REQUIREMENTS.md",
	"WORKLIST.md",
	"PROGRESS.md",
	"BACKLOG.md",
	"process/gap-register.md",
	"process/08-open-questions.md",
	"epics/**",
	"docs/85-loop-review-queue.md",
}

// ---------------------------------------------------------------- epic records on disk

// Section headings, level 2 — the boundary ParseScenarios works in. The
// definition shapes themselves live beside the parser in ops.go, so the
// instrument and the parser can never disagree about what a definition is.
var h2HeadingRe = regexp.MustCompile(`^##\s+(.*)$`)

// scenarioSectionHeading reports whether a level-2 heading titles a section
// that DEFINES acceptance scenarios.
//
// Positive rule: the heading names scenarios. Negative rule: it does not mark
// the section as out of scope, and it is not one of the reference shapes that
// merely CITE scenario ids — an `## Evidence map` table, a coverage table, a
// traceability matrix. Those carry SCN ids in their first column and would
// otherwise be counted as uncaptured definitions, which is the false positive
// that makes a coverage number worthless.
//
// This is a vocabulary rule, not an alternation of the decorations seen so far.
// The records decorate the heading on both sides and keep inventing shapes — a
// numbered prefix in a record written as a numbered document, a "(SCN)" tag
// before the loop qualifier, a parenthetical naming the source feature file —
// and an unlisted decoration read as "this record has no scenarios" is a silent
// loss, whereas the wrong section is prevented by the negative rule below.
func scenarioSectionHeading(heading string) bool {
	return strings.Contains(strings.ToLower(heading), "scenario") && !excludedSectionHeading(heading)
}

// excludedSectionHeading reports whether a level-2 heading marks its section as
// out of this epic's acceptance — deferred, rejected, superseded — or as one of
// the reference shapes that cite scenario ids rather than define them.
//
// Hyphens read as spaces: "out-of-scope scenarios" and "out of scope scenarios"
// are the same heading, and a phrase that only matches one spelling would let
// the widened positive rule read a section the negative rule was written to
// keep out.
func excludedSectionHeading(heading string) bool {
	h := strings.ReplaceAll(strings.ToLower(heading), "-", " ")
	for _, out := range []string{"deferred", "out of scope", "rejected", "superseded", "evidence", "coverage", "traceability"} {
		if strings.Contains(h, out) {
			return true
		}
	}
	return false
}

// epicRecordFile is one file in the epic-record population, with the id its
// path claims.
type epicRecordFile struct {
	Rel string
	ID  string // "" = the path yields no EPIC-<AREA>-<N> id
}

// epicRecordsOnDisk enumerates the epic-record population by walking epics/:
// every top-level `epics/EPIC*.md` and every `epics/<dir>/EPIC.md`. This is
// the population the flip retires and the store must therefore carry — it is
// deliberately NOT data.Epics, which is already deduplicated first-wins and so
// cannot show a shadowed record.
//
// A directory whose name claims an epic id but which holds no EPIC.md is
// reported with an empty Rel: the id exists, the record does not.
func epicRecordsOnDisk(root string) (files []epicRecordFile, dirsWithoutRecord []string) {
	entries, err := os.ReadDir(filepath.Join(root, "epics"))
	if err != nil {
		return nil, nil
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			rel := path.Join("epics", name, "EPIC.md")
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
				if strings.HasPrefix(strings.ToUpper(name), "EPIC") {
					dirsWithoutRecord = append(dirsWithoutRecord, path.Join("epics", name))
				}
				continue
			}
			files = append(files, epicRecordFile{Rel: rel, ID: epicIDFromPath(rel)})
			continue
		}
		if !strings.HasSuffix(strings.ToLower(name), ".md") ||
			!strings.HasPrefix(strings.ToUpper(name), "EPIC") {
			continue
		}
		rel := path.Join("epics", name)
		files = append(files, epicRecordFile{Rel: rel, ID: epicIDFromPath(rel)})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Rel < files[j].Rel })
	sort.Strings(dirsWithoutRecord)
	return files, dirsWithoutRecord
}

// scanEpicRecordCoverage diffs the on-disk epic-record population against the
// epics that reach an op.
//
// Population: every `epics/EPIC*.md` and every `epics/*/EPIC.md` under the
// workspace root. Two failures are invisible to the snapshot-side count:
//
//   - a SHADOWED record — two files claim one epic id, the extractor keeps the
//     first, and the second file's entire content reaches no op while the count
//     of epics still reconciles;
//   - an UNPARSEABLE name — a file in the population whose path yields no
//     EPIC-<AREA>-<N> id, so no epic op can ever carry it.
//
// Both are lost content, so both are FAIL-level: LossLost blocks the report.
func (r *FidelityReport) scanEpicRecordCoverage(root string, epicOps map[string]bool, syncedRecord map[string]string) {
	files, dirsWithoutRecord := epicRecordsOnDisk(root)
	if len(files) == 0 && len(dirsWithoutRecord) == 0 {
		return
	}

	byID := map[string][]string{}
	for _, f := range files {
		if f.ID != "" {
			byID[f.ID] = append(byID[f.ID], f.Rel)
		}
	}

	count := FidelityCount{Group: "epic-records-on-disk (epics/EPIC*.md + epics/*/EPIC.md)", Rows: len(files)}
	for _, f := range files {
		switch {
		case f.ID == "":
			count.MissingFromOps = append(count.MissingFromOps, f.Rel)
			r.Losses = append(r.Losses, FidelityLoss{
				RecordID: f.Rel, Field: "epic-record-identity", Category: LossLost,
				Detail: "the file sits in the epic-record population but its path yields no EPIC-<AREA>-<N> id — no epic op can carry it, and epics/** retires at the flip",
			})
		case !epicOps[f.ID]:
			count.MissingFromOps = append(count.MissingFromOps, f.Rel)
			r.Losses = append(r.Losses, FidelityLoss{
				RecordID: f.Rel, Field: "epic-record-unsynced", Category: LossLost,
				Detail: fmt.Sprintf("the record claims %s and no epic op carries that id", f.ID),
			})
		case len(byID[f.ID]) > 1 && syncedRecord[f.ID] != "" && syncedRecord[f.ID] != f.Rel:
			// The id reconciles — via the OTHER file. This one is shadowed:
			// the extractor keeps first-wins, so nothing here reaches an op.
			count.MissingFromOps = append(count.MissingFromOps, f.Rel)
			r.Losses = append(r.Losses, FidelityLoss{
				RecordID: f.Rel, Field: "epic-record-shadowed", Category: LossLost,
				Detail: fmt.Sprintf("two records claim %s; the op path keeps %s first-wins, so nothing in this file reaches an epic op — its prose survives only as archival text, and the count of epics still reconciles, which is what makes the loss silent", f.ID, syncedRecord[f.ID]),
			})
		default:
			count.Ops++
		}
	}
	r.Counts = append(r.Counts, count)

	for _, dir := range dirsWithoutRecord {
		// A sibling `epics/<name>.md` claiming the same id IS the record: the
		// epic syncs with its real body, so nothing is missing here. What the
		// directory holds is measured by the archival diff, which names every
		// uncarried file — reporting it twice, as a record that does exist,
		// would be a finding nobody can act on.
		if id := epicIDFromPath(dir); id != "" && len(byID[id]) > 0 {
			continue
		}
		r.Losses = append(r.Losses, FidelityLoss{
			RecordID: dir, Field: "epic-record-missing", Category: LossLost,
			Detail: "the directory name claims an epic id but holds no EPIC.md — the epic syncs with an empty record body while whatever the folder does hold retires with epics/**",
		})
	}
}

// ---------------------------------------------------------------- duplicate identity

// noteDuplicateIdentity records a duplicate external identity as a FAIL-level
// loss, not only as hygiene drift.
//
// A duplicate id is not untidiness. The extractor keeps the first occurrence,
// so the second record's content never reaches an op — and because the id is
// present either way, every count in the report still reconciles. It is
// recorded against the SHADOWED occurrence, which is the content that is lost.
func (r *FidelityReport) noteDuplicateIdentity(recordID, field, detail string) {
	r.Losses = append(r.Losses, FidelityLoss{
		RecordID: recordID, Field: field, Category: LossLost, Detail: detail,
	})
}

// ---------------------------------------------------------------- scenario shapes

// fenceMaskedShape names the population in the report's own vocabulary: not a
// shape the parser reads badly, but a shape both the parser and every citation
// counter agree to skip.
const fenceMaskedShape = "declared as Gherkin inside a code fence (read by nothing, counted by nothing)"

// scenarioCoverageGroup names the definition-population count line. A
// constant so the report line and the checks that read it cannot drift apart
// as shapes are added to it.
const scenarioCoverageGroup = "scenario-definitions (epic records, counted by identity; table rows in the scenario section, bold `- **SCN-…**` bullets, `### SCN-…` heading blocks, and fenced `Scenario:`/`Scenario Outline:` declarations in that section — evidence-map, coverage and addendum citations excluded)"

// addendumGroupMarker names the second citation population. It is a constant so
// the report line and the checks that read it cannot drift apart.
const addendumGroupMarker = "scenario-shaped rows in an unrecognized section"

// scenarioCitationShape reports whether a shape carries REFERENCES rather than
// definitions. Two of them exist and they are different facts: one is a row in
// a table the vocabulary knows cites scenarios, the other is a row under a
// heading the vocabulary does not recognize at all.
func scenarioCitationShape(shape string) bool {
	return shape == "table-outside-a-section" || shape == "table-in-unnamed-section"
}

func scenarioCitationGroup(fileCount int) string {
	return fmt.Sprintf("  scenario-citation (NOT a definition): SCN id leading a table row under a heading that cites rather than defines — an evidence map, a coverage table, a traceability matrix — or under one that marks its section out of this epic's scope, in %d file(s)", fileCount)
}

func scenarioAddendumGroup(fileCount int) string {
	return fmt.Sprintf("  %s (NOT read as definitions): SCN id leading a table row under a heading the vocabulary recognizes as neither a scenario section nor a citation table — a numbered review addendum, a trace roll-up, in %d file(s)", addendumGroupMarker, fileCount)
}

// fenceMaskedScenarioIDs returns the Gherkin declarations inside code fences
// whose ids reach neither a scenario record nor any counted shape in the same
// record — the definitions that are invisible to both sides of the coverage
// comparison, which is what lets the record read as complete.
//
// carried = the ids ParseScenarios produced. mentioned = every id any counted
// shape in this record named, citations included: a fence that restates a
// scenario the evidence map already lists is a reference, not a masked
// definition. Out-of-scope sections are excluded on the same rule the unfenced
// shapes follow.
//
// The parser now READS a declaration inside the scenario section, so what this
// arm still measures is the population outside it — a sample quoted beside a
// design note, a record whose scenario heading the vocabulary does not open.
// Those stay unread on purpose and stay counted for the same reason.
func fenceMaskedScenarioIDs(recordText string, carried, mentioned map[string]bool) []string {
	if recordText == "" {
		return nil
	}
	lines := strings.Split(recordText, "\n")
	fenced := fencedLines(lines)
	heading := ""
	seen := map[string]bool{}
	var out []string
	for i, line := range lines {
		if !fenced[i] {
			if m := h2HeadingRe.FindStringSubmatch(line); m != nil {
				heading = strings.TrimSpace(m[1])
			}
			continue
		}
		m := fencedScenarioDeclRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		id := m[1]
		if carried[id] || mentioned[id] || seen[id] || excludedSectionHeading(heading) {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// countScenarioShapes classifies every SCN definition in one record's text by
// the shape it is written in, and returns the ids it found per shape so the
// caller can diff them against what ParseScenarios actually carried — a
// coverage arm that compares identities rather than two counts that can agree
// while naming different scenarios.
//
// Shapes:
//
//	table-in-read-section    a table row inside the scenario section the parser reads
//	table-in-other-section   a table row inside a second scenario-titled section
//	bullet                   `- **SCN-…**` — the bold run stops at the id
//	heading-block            `### SCN-…` plus its body to the next heading
//	fenced-gherkin           `Scenario:`/`Scenario Outline:` plus an id, inside a
//	                         fence in the scenario section, its body to the next
//	                         declaration or the fence's end
//	table-outside-a-section  a table row carrying an SCN id under a heading that
//	                         CITES scenarios — an evidence map, a coverage table, a
//	                         traceability matrix — or under one that marks its
//	                         section out of this epic's scope. NOT a definition;
//	                         counted so the population criteria are checkable.
//	table-in-unnamed-section a table row carrying an SCN id under a heading the
//	                         vocabulary recognizes as NEITHER — a numbered review
//	                         addendum, a trace roll-up. Also not read as a
//	                         definition, but for a different reason, and folding it
//	                         into the citation bucket labelled it the opposite of
//	                         what it is.
//
// This walk is deliberately its own, sharing the parser's regexes and heading
// rules but not its control flow: an instrument that calls the thing it
// measures can only ever report that the parser agrees with itself.
func countScenarioShapes(recordText string) map[string][]string {
	found := map[string][]string{}
	if recordText == "" {
		return found
	}
	lines := strings.Split(recordText, "\n")
	fenced := fencedLines(lines)

	heading := ""
	inSection, sectionSeen := false, false
	for i := 0; i < len(lines); i++ {
		if fenced[i] {
			// A Gherkin declaration inside the scenario section, which the
			// parser reads. Counted here so the coverage diff has both sides of
			// it; outside the section a fence is quoted and the fence arm owns
			// the population instead.
			if inSection {
				if m := fencedScenarioDeclRe.FindStringSubmatch(lines[i]); m != nil {
					found["fenced-gherkin"] = append(found["fenced-gherkin"], m[1])
				}
			}
			continue
		}
		line := lines[i]

		if m := h2HeadingRe.FindStringSubmatch(line); m != nil {
			heading = strings.TrimSpace(m[1])
			inSection = false
			if !sectionSeen && scenarioSectionHeading(heading) {
				inSection, sectionSeen = true, true
			}
			continue
		}
		excluded := excludedSectionHeading(heading)

		if m := scnHeadingDefRe.FindStringSubmatch(line); m != nil {
			j := i + 1
			for ; j < len(lines); j++ {
				if !fenced[j] && anyHeadingRe.MatchString(lines[j]) {
					break
				}
			}
			if !excluded {
				found["heading-block"] = append(found["heading-block"], m[1])
			}
			i = j - 1 // the body belongs to this scenario, not to the next one
			continue
		}
		if m := scnBulletDefRe.FindStringSubmatch(line); m != nil {
			if !excluded {
				found["bullet"] = append(found["bullet"], m[1])
			}
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") || isSeparatorRow(trimmed) {
			continue
		}
		cells := splitCells(line)
		first := ""
		for _, c := range cells {
			if strip(c) != "" {
				first = strip(c)
				break
			}
		}
		if !scnIDRe.MatchString(first) {
			continue
		}
		id := scnIDRe.FindString(first)
		switch {
		case inSection:
			found["table-in-read-section"] = append(found["table-in-read-section"], id)
		case scenarioSectionHeading(heading):
			found["table-in-other-section"] = append(found["table-in-other-section"], id)
		case excludedSectionHeading(heading):
			found["table-outside-a-section"] = append(found["table-outside-a-section"], id)
		default:
			found["table-in-unnamed-section"] = append(found["table-in-unnamed-section"], id)
		}
	}
	return found
}

// scanScenarioShapes measures scenario capture across the whole epic-record
// corpus, per shape.
//
// Population: every epic record the snapshot resolved, read as text. Counted
// as a DEFINITION is an SCN id that leads a table row inside a scenario-titled
// section, leads a bold bullet, or leads its own heading. NOT counted as a
// definition — the negative population, stated because it is what a naive
// matcher gets wrong — are SCN ids cited in an `## Evidence map` or coverage
// table, in a review addendum, in the WORKLIST Scenarios column, or in a
// ledger evidence cell: those reference a scenario defined elsewhere, and
// counting them would inflate the gap with rows that must never become
// scenarios.
//
// Spec files are counted separately: `epics/*/specs/*.md` ride the epic
// payload as opaque spec content, so a scenario defined there is preserved as
// text but reaches no scenario record. That is a known-unstructured carrier,
// reported rather than dropped — and the count includes the fenced declarations
// the spec files write, so the number a scoping decision reads is the whole
// population rather than the part that happens to be written as a table.
func scenarioOpIDsOf(ops []Op) map[string]bool {
	out := map[string]bool{}
	for _, op := range ops {
		if op.Type == "upsert_scenario" {
			if id, _ := op.Payload["external_id"].(string); id != "" {
				out[id] = true
			}
		}
	}
	return out
}

func (r *FidelityReport) scanScenarioShapes(epics []Epic, records map[string]string, scenarioOps map[string]bool) {
	shapes := []string{"table-in-read-section", "table-in-other-section", "bullet", "heading-block", "fenced-gherkin", "table-outside-a-section", "table-in-unnamed-section"}
	defs, missed := map[string]int{}, map[string]int{}
	files := map[string]map[string]bool{}
	missedFiles := map[string]map[string]bool{}
	for _, s := range shapes {
		files[s] = map[string]bool{}
		missedFiles[s] = map[string]bool{}
	}
	onDisk, captured := 0, 0
	recordsSeen := map[string]bool{}
	// The fence arm's own population: definitions the parser and every citation
	// counter skip together, because both treat a fenced line as quoted.
	maskedIDs, maskedFiles := 0, map[string]bool{}

	for _, e := range epics {
		if e.Record == "" || recordsSeen[e.Record] {
			continue
		}
		recordsSeen[e.Record] = true
		text := records[e.ID]
		if text == "" {
			continue
		}
		// The identities the parser carried, so coverage is a diff of ids
		// rather than two totals that can agree while naming different
		// scenarios.
		carried := map[string]bool{}
		for _, s := range ParseScenarios(text, e.ID) {
			id := s.(map[string]any)["external_id"].(string)
			carried[strings.TrimPrefix(id, e.ID+"#")] = true
		}
		captured += len(carried)

		shapesHere := countScenarioShapes(text)
		definedHere := map[string]bool{}
		mentioned := map[string]bool{}
		for shape, ids := range shapesHere {
			files[shape][e.Record] = true
			defs[shape] += len(ids)
			for _, id := range ids {
				mentioned[id] = true
			}
			if scenarioCitationShape(shape) {
				continue // a citation: nothing is expected to carry it
			}
			for _, id := range ids {
				definedHere[id] = true
				if !carried[id] {
					missed[shape]++
					missedFiles[shape][e.Record] = true
				}
			}
		}
		// A Gherkin declaration inside a fence is a definition by any reading,
		// and it is the one shape BOTH sides of this comparison agree to skip —
		// so the record reads as complete while the definitions are unseen. Only
		// ids that reach no scenario AND appear in no counted shape qualify: a
		// fence that restates a captured scenario, or quotes a trace diagram's
		// range of ids, is a reference and must stay out.
		if masked := fenceMaskedScenarioIDs(text, carried, mentioned); len(masked) > 0 {
			maskedIDs += len(masked)
			maskedFiles[e.Record] = true
		}
		// One id defined in two shapes is ONE scenario, so the population is
		// counted by identity: summing shape occurrences would demand more
		// scenarios than the record defines and read as a permanent shortfall.
		onDisk += len(definedHere)
	}

	// Spec-file definitions: opaque inside the spec's content_md — unless a
	// scenario record carries them (REQ-CROSS-252), which is what dissolves
	// this arm's residue.
	specDefs, specFiles, specCarried := 0, 0, 0
	for _, e := range epics {
		for _, spec := range e.Specs {
			ids := map[string]bool{}
			for shape, found := range countScenarioShapes(spec.Content) {
				if scenarioCitationShape(shape) {
					continue
				}
				for _, id := range found {
					ids[id] = true
				}
			}
			if len(ids) > 0 {
				specDefs += len(ids)
				specFiles++
				for id := range ids {
					if scenarioOps[e.ID+"#"+id] {
						specCarried++
					}
				}
			}
		}
	}

	if onDisk == 0 && specDefs == 0 && maskedIDs == 0 {
		return
	}

	r.Counts = append(r.Counts, FidelityCount{
		Group: scenarioCoverageGroup,
		Rows:  onDisk, Ops: captured,
	})

	for _, shape := range shapes {
		if defs[shape] == 0 {
			continue
		}
		if scenarioCitationShape(shape) {
			group := scenarioCitationGroup(len(files[shape]))
			if shape == "table-in-unnamed-section" {
				group = scenarioAddendumGroup(len(files[shape]))
			}
			r.Counts = append(r.Counts, FidelityCount{Group: group, Rows: defs[shape], Ops: 0})
			continue
		}
		r.Counts = append(r.Counts, FidelityCount{
			Group: fmt.Sprintf("  scenario-shape: %s, in %d file(s)", shape, len(files[shape])),
			Rows:  defs[shape], Ops: defs[shape] - missed[shape],
		})
		if missed[shape] > 0 {
			r.Losses = append(r.Losses, FidelityLoss{
				RecordID: "epic-records", Field: "scenario-shape:" + shape, Category: LossLost,
				Detail: fmt.Sprintf("%d scenario definition(s) across %d record(s) are written as %q and no scenario record carries their ids", missed[shape], len(missedFiles[shape]), shape),
			})
		}
	}
	if missed["table-in-other-section"] > 0 {
		r.Losses = append(r.Losses, FidelityLoss{
			RecordID: "epic-records", Field: "scenario-section-heading", Category: LossLost,
			Detail: fmt.Sprintf("%d scenario definition(s) sit in a scenario-titled section the parser does not read — a second scenario section in one record, or a heading whose decoration the heading rule rejects — so those definitions reach no scenario record", missed["table-in-other-section"]),
		})
	}
	if maskedIDs > 0 {
		r.Counts = append(r.Counts, FidelityCount{
			Group: fmt.Sprintf("  scenario-shape: %s, in %d file(s)", fenceMaskedShape, len(maskedFiles)),
			Rows:  maskedIDs, Ops: 0,
		})
		r.Losses = append(r.Losses, FidelityLoss{
			RecordID: "epic-records", Field: "scenario-shape:fence-masked", Category: LossLost,
			Detail: fmt.Sprintf("%d scenario definition(s) across %d record(s) are declared as Gherkin inside a code fence; the parser reads fenced lines as quoted material and so does every citation counter, so these reach no scenario record and are absent from the coverage total as well", maskedIDs, len(maskedFiles)),
		})
	}
	if specDefs > 0 {
		r.Counts = append(r.Counts, FidelityCount{
			Group: fmt.Sprintf("  scenario-shape: defined in an epic spec file, in %d file(s)", specFiles),
			Rows:  specDefs, Ops: specCarried,
		})
	}
	if specDefs > specCarried {
		r.Losses = append(r.Losses, FidelityLoss{
			RecordID: "epic-specs", Field: "scenario-definitions", Category: LossTransform,
			Detail: fmt.Sprintf("%d scenario definition(s) across %d spec file(s) survive only as opaque spec content_md — preserved as text, structured as nothing", specDefs-specCarried, specFiles),
		})
	}
}

// ---------------------------------------------------------------- backlog narratives

// narrativeCoverageGroup names the count line for the register's own prose
// blocks. A constant so the report and the checks that read it cannot drift.
const narrativeCoverageGroup = "gap-register narratives (`### <id>` blocks → the backlog record they expand)"

// backlogNotesContractCap mirrors the wire contract's declared maxLength for a
// backlog record's notes_md. Nothing on the path enforces it — the client
// validator caps epic specs only, and the server column is text — so an
// oversize payload would post and read as carried. Mirrored rather than
// imported so the report keeps naming the number if the schema moves.
const backlogNotesContractCap = 20000

// scanBacklogNarratives reconciles the register's `### <id>` narrative blocks
// against the backlog records they are supposed to expand, and flags a record
// whose notes exceed what the wire contract declares.
//
// Population: every level-3 block in the tracked backlog and gap-register files
// whose heading leads with an explicit record id. Matched = a block whose id is
// also a table row, so its body rides that record's notes. Unmatched blocks are
// named rather than ignored: the register's closed-gaps template is one, and a
// real one would be a gap with no row — a fact for a human, not a repair.
func (r *FidelityReport) scanBacklogNarratives(root string, m *manifest.Manifest, backlog []BacklogRow) {
	rowIDs := map[string]bool{}
	for _, b := range backlog {
		rowIDs[b.ExternalID] = true
	}

	count := FidelityCount{Group: narrativeCoverageGroup}
	for _, docType := range []string{manifest.DocBacklog, manifest.DocGapRegister} {
		for _, file := range m.Resolve(root, docType) {
			content, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			ids := make([]string, 0, 4)
			for id := range backlogNarratives(strings.Split(string(content), "\n")) {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				count.Rows++
				if rowIDs[id] {
					count.Ops++
				} else {
					count.MissingFromOps = append(count.MissingFromOps, id)
				}
			}
		}
	}
	if count.Rows > 0 {
		r.Counts = append(r.Counts, count)
	}

	for _, b := range backlog {
		if n := utf16Len(b.NotesMD); n > backlogNotesContractCap {
			r.Hygiene = append(r.Hygiene, FidelityHygiene{
				File: b.SourcePath, Line: b.Line,
				Detail: fmt.Sprintf("%s notes_md is %d units against the wire contract's declared %d — nothing on the path enforces that cap, so the payload posts whole and reads as carried; carried here rather than cut, and named so the contract and the corpus can be reconciled deliberately",
					b.ExternalID, n, backlogNotesContractCap),
			})
		}
	}
}

// ---------------------------------------------------------------- loop-status cap

// scanLoopStatusCap reports every WORKLIST loop-status cell the extractor cuts.
//
// Population: the Upper-status and Lower-status cells of every rollup row the
// extractor actually reads — same row filter, same first-wins dedup, same
// placeholder rule, so the instrument measures the cells that reach a payload
// rather than every cell in the file.
//
// The cut is invisible from every other angle: the epic count reconciles, the
// payload carries a status that reads as complete, and the sentence the cell
// finished survives only in a file the flip retires. That is the silent class,
// so it is reported per cell with both lengths rather than as a note about the
// cap existing.
//
// Its own walk rather than a comparison against Epic.Upper: those fields are
// already the cut values, so asking the parser what it produced could only ever
// report that the parser agrees with itself.
func (r *FidelityReport) scanLoopStatusCap(root string, m *manifest.Manifest) {
	files := m.Resolve(root, manifest.DocWorklist)
	if len(files) == 0 {
		return
	}
	content, err := os.ReadFile(files[0])
	if err != nil {
		return
	}
	relPath := rel(root, files[0])
	seen := map[string]bool{}
	for lineNo, line := range strings.Split(string(content), "\n") {
		if !worklistRowRe.MatchString(line) {
			continue
		}
		cells := splitCells(line)
		if len(cells) < 11 {
			continue
		}
		id := strings.SplitN(bracketStripRe.ReplaceAllString(cell(cells, 1), ""), " ", 2)[0]
		if strings.Contains(id, "..") || seen[id] {
			continue
		}
		seen[id] = true
		for _, col := range []struct {
			idx  int
			name string
		}{{7, "upper"}, {8, "lower"}} {
			raw := strip(cell(cells, col.idx))
			switch raw {
			case "", "—", "-", "–":
				continue // absence, not a cut
			}
			n := utf16Len(raw)
			if n <= loopStatusCap {
				continue
			}
			r.Losses = append(r.Losses, FidelityLoss{
				RecordID: id, Field: "loop-status:" + col.name, Category: LossTruncated,
				Detail: fmt.Sprintf("the %s loop-status cell at %s:%d is %d units and the payload carries the first %d — the remainder survives only in the corpus file, which the flip retires",
					col.name, relPath, lineNo+1, n, loopStatusCap),
			})
		}
	}
}

// ---------------------------------------------------------------- archival coverage

// matchesRetiredFamily reports whether a workspace-relative path falls in the
// population the flip retires.
func matchesRetiredFamily(relPath string) bool {
	relPath = filepath.ToSlash(relPath)
	for _, fam := range RetiredPathFamilies {
		if strings.HasSuffix(fam, "/**") {
			if strings.HasPrefix(relPath, strings.TrimSuffix(fam, "**")) {
				return true
			}
			continue
		}
		if ok, err := path.Match(fam, relPath); err == nil && ok {
			return true
		}
	}
	return false
}

// retiredFilesOnDisk enumerates every file the retired-path population matches,
// as workspace-relative slash paths. It walks only the directories the families
// name, so it costs nothing in a monorepo whose other trees are irrelevant.
func retiredFilesOnDisk(root string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(rel string) {
		rel = filepath.ToSlash(rel)
		if seen[rel] || !matchesRetiredFamily(rel) {
			return
		}
		seen[rel] = true
		out = append(out, rel)
	}

	for _, fam := range RetiredPathFamilies {
		switch {
		case strings.HasSuffix(fam, "/**"):
			dir := strings.TrimSuffix(fam, "/**")
			_ = filepath.WalkDir(filepath.Join(root, filepath.FromSlash(dir)), func(p string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				add(rel(root, p))
				return nil
			})
		case strings.ContainsAny(fam, "*?["):
			dir := path.Dir(fam)
			entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
			if err != nil {
				continue
			}
			for _, e := range entries {
				if !e.IsDir() {
					add(path.Join(dir, e.Name()))
				}
			}
		default:
			if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(fam))); err == nil && !info.IsDir() {
				add(fam)
			}
		}
	}
	sort.Strings(out)
	return out
}

// scanArchivalCoverage diffs the retired-path population against the carriers
// that would hold it after the flip, in both directions.
//
// Population: every FILE on disk matched by RetiredPathFamilies. A carrier is
// a document op (identified by its workspace-relative path, part documents
// folded back onto the base path) or an epic spec riding an epic payload.
//
// Forward: a retired file with no carrier is content the flip deletes from the
// repository and the store never received — the silent-loss class itself, so
// FAIL-level. Reverse: a carrier naming a path inside the retired population
// that no longer exists on disk is a stale carrier, reported so the diff is
// checkable from both ends rather than only from the side that happens to be
// shorter.
func (r *FidelityReport) scanArchivalCoverage(root string, carried map[string]bool) {
	files := retiredFilesOnDisk(root)
	if len(files) == 0 {
		return
	}

	count := FidelityCount{
		Group: "archival-coverage (files matched by the retired-path families → document/spec carriers)",
		Rows:  len(files),
	}
	var uncarried []string
	var uncarriedBytes int64
	for _, f := range files {
		if carried[f] {
			count.Ops++
			continue
		}
		count.MissingFromOps = append(count.MissingFromOps, f)
		uncarried = append(uncarried, f)
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(f))); err == nil {
			uncarriedBytes += info.Size()
		}
	}
	onDisk := map[string]bool{}
	for _, f := range files {
		onDisk[f] = true
	}
	for c := range carried {
		if matchesRetiredFamily(c) && !onDisk[c] {
			count.ExtraInOps = append(count.ExtraInOps, c)
		}
	}
	sort.Strings(count.ExtraInOps)
	r.Counts = append(r.Counts, count)

	for _, c := range count.ExtraInOps {
		r.Losses = append(r.Losses, FidelityLoss{
			RecordID: c, Field: "archival-carrier-stale", Category: LossTransform,
			Detail: "a carrier names this path inside the retired population, but no such file exists on disk — the store holds content the corpus no longer has",
		})
	}
	if len(uncarried) == 0 {
		return
	}

	examples := uncarried
	if len(examples) > 10 {
		examples = append(append([]string{}, examples[:10]...),
			fmt.Sprintf("… %d more (every path is listed under the archival-coverage count above)", len(uncarried)-10))
	}
	r.Losses = append(r.Losses, FidelityLoss{
		RecordID: "archival-coverage", Field: "uncarried-retired-files", Category: LossLost,
		Detail: fmt.Sprintf("%d of %d file(s) in the retired-path population reach no document or spec carrier — %d byte(s) that the flip removes from the repository and the store never received: %s",
			len(uncarried), len(files), uncarriedBytes, strings.Join(examples, ", ")),
	})
}

// ---------------------------------------------------------------- raw evidence cells

var (
	backtickSpanRe = regexp.MustCompile("`[^`]*`")
	// Connectors carry no evidence of their own; residue made only of them is
	// punctuation left behind by removing the references, not dropped content.
	cellConnectorRe = regexp.MustCompile(`(?i)\b(and|or|plus|via|with|per|the|a|in|on|at|of|to)\b`)
	alnumRe         = regexp.MustCompile(`[\p{L}\p{N}]`)
)

// evidenceCellResidue returns the text of a Tests/Code cell that the reference
// splitter discards: everything outside the backticked spans, once separators
// and bare connectors are removed. "" = the cell loses nothing.
//
// The rule mirrors splitEvidenceRefs exactly: with at least one backticked
// span present it keeps ONLY those spans, so any prose beside them is dropped.
// With no backticks at all it takes the cell whole, and nothing is lost — which
// is why a cell without backticks can never produce residue here.
func evidenceCellResidue(cellText string) string {
	c := strings.TrimSpace(cellText)
	if c == "" || c == "—" || c == "-" || c == "–" {
		return ""
	}
	if !backtickSpanRe.MatchString(c) {
		return "" // taken whole by the splitter — nothing outside to lose
	}
	rest := backtickSpanRe.ReplaceAllString(c, " ")
	probe := cellConnectorRe.ReplaceAllString(rest, " ")
	probe = strings.Map(func(r rune) rune {
		if strings.ContainsRune("·,;:/+&*()[]{}—–-", r) {
			return ' '
		}
		return r
	}, probe)
	if len(alnumRe.FindAllString(probe, -1)) < 4 {
		return ""
	}
	return strings.Join(strings.Fields(rest), " ")
}

// scanEvidenceCellRaw checks every Tests and Code cell that holds content
// against the payload that is supposed to carry it verbatim (REQ-CROSS-222 §6),
// and reports what the parsed reference list still shortens.
//
// The question is asked of the PAYLOAD, not of the parser: a cell counts as
// carried only when a named extra holds its exact text. That is what keeps the
// check alive after the carrier exists — remove the carrier and every cell with
// content is reported again, which is the regression this measures.
//
// Two shapes of raw loss are distinguished in the detail, because they read
// differently to whoever has to fix them: a cell mixing backticked references
// with prose loses the prose, while any other uncarried cell loses everything.
//
// Separately: a cell with no backticks becomes ONE parsed reference, shortened
// to 200 runes. The text itself survives in the raw carrier, so this is
// truncation of the parsed structure only — reported under `refs:` so the two
// are never confused, and only for rows that actually fall back to the cells.
func (r *FidelityReport) scanEvidenceCellRaw(reqs []Req, reqPayload map[string]map[string]any) {
	count := FidelityCount{Group: "evidence cells carried raw (Tests/Code columns)"}
	for _, q := range reqs {
		code, tests, _ := evidenceRefsFromDetail(q.Detail)
		readsCells := len(code) == 0 && len(tests) == 0
		hasCell, lost := false, false
		for _, pair := range []struct{ field, extra, text string }{
			{"tests-cell", rawTestsCellExtra, q.Tests},
			{"code-cell", rawCodeCellExtra, q.Code},
		} {
			text := dashless(pair.text)
			if text == "" {
				continue // a placeholder cell is absence, not content
			}
			hasCell = true
			if carried, ok := namedExtraValue(reqPayload[q.ID], pair.extra); !ok || carried != text {
				lost = true
				detail := fmt.Sprintf("the cell reaches no payload verbatim: %q", capRunes(text, 300))
				if residue := evidenceCellResidue(pair.text); residue != "" {
					detail = fmt.Sprintf("the cell mixes backticked references with prose and no raw carrier holds it; this text reaches no payload: %q", capRunes(residue, 300))
				}
				r.Losses = append(r.Losses, FidelityLoss{
					RecordID: q.ID, Field: "raw:" + pair.field, Category: LossLost, Detail: detail,
				})
			}
			// Ask the splitter what it produced rather than guessing from the
			// cell's shape. It falls back to the whole cell whenever it
			// extracted no backticked SPAN — a lone unmatched backtick
			// extracts none — and it cuts in UTF-16 units, not runes, so an
			// astral-plane cell is shortened well under 200 runes. Both cases
			// read as clean to a shape test and are real truncations.
			if readsCells {
				whole := strings.Join(strings.Fields(text), " ")
				refs := splitEvidenceRefs(pair.text)
				if len(refs) == 1 && refs[0] != whole && refs[0] == capRunes(whole, evidenceRefCap) {
					r.Losses = append(r.Losses, FidelityLoss{
						RecordID: q.ID, Field: "refs:" + pair.field, Category: LossTruncated,
						Detail: fmt.Sprintf("no backticked reference to parse, so the whole cell becomes one citation shortened to %d units (cell is %d runes); the full text rides the raw carrier",
							evidenceRefCap, utf8.RuneCountInString(whole)),
					})
				}
			}
		}
		if !hasCell {
			continue
		}
		count.Rows++
		if !lost {
			count.Ops++
		}
	}
	if count.Rows > 0 {
		r.Counts = append(r.Counts, count)
	}
}

// namedExtraValue returns the value a payload carries under a named extra.
func namedExtraValue(payload map[string]any, name string) (string, bool) {
	extras, _ := payload["extra_columns"].([]map[string]any)
	for _, e := range extras {
		if n, _ := e["name"].(string); n == name {
			v, _ := e["value"].(string)
			return v, true
		}
	}
	return "", false
}

// ---------------------------------------------------------------- epic UR membership

// declaredEpicURs collects EVERY user requirement an epic record declares, in
// each shape the corpus writes them.
//
// ParseEpicUserRequirement answers a different question — "which single UR does
// this epic's payload carry?" — and stops at the first one it can build. That
// is what the payload holds; this is what the record SAYS. The gap between the
// two is the membership loss, and it is invisible to any check that asks the
// parser twice.
//
// Every shape here reuses the parser's own regexes, so the same discipline
// applies: a passing mention (`## How UR-NOPE-001 relates to this epic`) is not
// a declaration, and never becomes one.
func declaredEpicURs(recordText string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}

	// Every data row of the linked-user-requirements table, not only the first.
	if sec := linkedURSectionRe.FindStringSubmatch(recordText); sec != nil {
		for _, line := range strings.Split(sec[1], "\n") {
			t := strings.TrimSpace(line)
			if m := linkedURRowRe.FindStringSubmatch(t); m != nil {
				add(m[1])
				continue
			}
			if m := linkedURBulletRe.FindStringSubmatch(t); m != nil {
				add(m[1])
			}
		}
	}
	// The `## User outcome (UR-…)` heading and the `**UR-…** — …` first line.
	if m := epicUserOutcomeRe.FindStringSubmatch(recordText); m != nil {
		if h := urHeadingIDRe.FindStringSubmatch(m[0]); h != nil {
			add(h[1])
		}
		for _, line := range strings.Split(m[1], "\n") {
			if id := urIDLineRe.FindStringSubmatch(strings.TrimSpace(line)); id != nil {
				add(id[1])
			}
		}
	}
	// Id-led headings: `## UR-ABS-1 — the outcome`. Matched on the heading line
	// alone: a pattern that also spans the body consumes the `\n##` ending it,
	// so a repeated scan resumes inside the next heading and reads every second
	// one. Under-counting here is not the safe direction it looks like — the
	// check reports declared-minus-carried, so an id missing from BOTH sides
	// cancels out and a real membership loss on that id is reported as nothing.
	for _, m := range idLedHeadingRe.FindAllStringSubmatch(recordText, -1) {
		add(m[1])
	}
	return out
}

// scanEpicURMembership diffs the user requirements each epic record DECLARES
// against the ones that epic's OWN payload carries.
//
// Population: every epic record the snapshot resolved. Declared = every id
// found by declaredEpicURs — every data row of a linked-user-requirements
// table, the id on a `## User outcome (UR-…)` heading, a `**UR-…** —` opening
// line, and every id-led `## UR-… —` heading. Carried = the ids in that epic
// payload's user_requirement_external_ids.
//
// The comparison is per epic on purpose. Asking only "does this UR exist
// anywhere in the batch?" answers a weaker question and hides the whole defect
// class: a second declared UR that another epic happens to define exists as an
// entity, yet THIS epic's membership edge to it is never written. The epic
// count reconciles, the UR count reconciles, and the relation is gone.
// A declared id that reaches no user-requirement op at all is the stronger
// case — entity and edges both — and the detail says which it is.
func (r *FidelityReport) scanEpicURMembership(epics []Epic, records map[string]string, epicPayload map[string]map[string]any, userReqOps map[string]bool) {
	count := FidelityCount{Group: "epic user-requirement membership (declared in the record → carried by that epic's payload)"}
	seenRecord := map[string]bool{}
	for _, e := range epics {
		text := records[e.ID]
		if text == "" || seenRecord[e.Record] {
			continue
		}
		seenRecord[e.Record] = true
		declared := declaredEpicURs(text)
		if len(declared) == 0 {
			continue
		}
		carried := map[string]bool{}
		if p := epicPayload[e.ID]; p != nil {
			if ids, ok := p["user_requirement_external_ids"].([]string); ok {
				for _, id := range ids {
					carried[id] = true
				}
			}
		}
		count.Rows += len(declared)
		var missing []string
		for _, id := range declared {
			if carried[id] {
				count.Ops++
				continue
			}
			where := "the id reaches no user-requirement op either, so the entity and its derives edges never exist"
			if userReqOps[id] {
				where = "the entity exists (another epic declares it), but this epic's membership edge to it is never written"
			}
			missing = append(missing, id+" ("+where+")")
			count.MissingFromOps = append(count.MissingFromOps, e.ID+" declares "+id)
		}
		if len(missing) > 0 {
			r.Losses = append(r.Losses, FidelityLoss{
				RecordID: e.ID, Field: "epic-ur-membership", Category: LossLost,
				Detail: fmt.Sprintf("the record declares %d user requirement(s) (%s); the payload carries %d. Uncarried: %s",
					len(declared), strings.Join(declared, ", "), len(carried), strings.Join(missing, "; ")),
			})
		}
	}
	if count.Rows > 0 {
		sort.Strings(count.MissingFromOps)
		r.Counts = append(r.Counts, count)
	}
}
