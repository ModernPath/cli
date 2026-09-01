package rdd

// REQ-CROSS-221 — ledger-import fidelity is measured, not asserted.
//
// The fidelity report reconciles the tracked corpus against the sync op path
// in both directions, itemizes every field the path drops, truncates, or
// transforms, and flags corpus hygiene drift. It writes nothing anywhere:
// the report is a gate input for the one-time server import (EPIC-CLI-003),
// not a repair tool — a guard flags, it does not delete.

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/modernpath/cli/internal/manifest"
)

// Loss categories (specs §221.3). Only the first three block (§221.4);
// excluded-by-design and gate-accepted residue inform without blocking.
const (
	LossLost      = "lost"        // never reaches an op payload
	LossTruncated = "truncated"   // reaches it shortened by a cap
	LossTransform = "transformed" // reaches it restructured; raw form not preserved
	LossExcluded  = "excluded by design"
)

// FidelityLoss names one field of one record the op path does not carry
// faithfully today.
type FidelityLoss struct {
	RecordID string // external id, or the file for corpus-level losses
	Field    string
	Category string
	Detail   string
}

// Key identifies a loss for the accepted-residue mechanism (§221.4).
func (l FidelityLoss) Key() string { return l.RecordID + "|" + l.Field }

// FidelityHygiene is one corpus-drift flag (§221.5): named, located, never
// repaired.
type FidelityHygiene struct {
	File   string
	Line   int
	Detail string
}

// FidelityCount reconciles one record group in both directions (§221.1).
type FidelityCount struct {
	Group          string   // ledger file, "worklist-epics", "open-questions", …
	Rows           int      // records in the corpus
	Ops            int      // records that reach an op payload
	MissingFromOps []string // corpus ids with no op
	ExtraInOps     []string // op ids with no corpus row in this group
}

// FidelityReport is the dry-run result. Building it writes nothing.
type FidelityReport struct {
	Counts  []FidelityCount
	Losses  []FidelityLoss
	Hygiene []FidelityHygiene
}

// Blocking returns the losses that must trip a non-zero exit (§221.4):
// lost/truncated/transformed entries not accepted by an applied human gate
// answer. Excluded-by-design entries never block.
func (r FidelityReport) Blocking(accepted map[string]bool) []FidelityLoss {
	var out []FidelityLoss
	for _, l := range r.Losses {
		switch l.Category {
		case LossLost, LossTruncated, LossTransform:
			if !accepted[l.Key()] {
				out = append(out, l)
			}
		}
	}
	return out
}

// Caps the op path applies today (mirrored, not imported, so the report
// keeps naming them even if a refactor moves the constants).
const (
	fidelityNoteCap     = 300   // note-citation cap, ops.go evidenceCitations
	fidelityGateBodyCap = 6000  // gate body_md cap
	fidelitySpecCap     = 65536 // epic spec content cap (schema maxLength)
)

var (
	fidelityRowRe    = regexp.MustCompile(`^\|\s*(REQ-[A-Z][A-Z0-9]*-\d+)\s*\|`)
	fidelityBlockRe  = regexp.MustCompile(`^###\s+(REQ-[A-Z][A-Z0-9]*-\d+)`)
	fidelityTotalRe  = regexp.MustCompile(`(\d+)\s+([A-Z_]+)`)
	fidelityLabelRe  = regexp.MustCompile(`^-\s+\*\*([^:*]+):\*\*`)
	fidelityBulletRe = regexp.MustCompile(`^\s+-\s+`)
)

// capturedDetailLabels are the own-bullet labels the op path reads today
// (Statement/Reason → description, Acceptance criteria → criteria,
// Tests/Evidence/Code → evidence citations, UR → parent, Status is the
// dashboard duplicate). Everything else is prose the payload never sees.
var capturedDetailLabels = map[string]bool{
	"status": true, "statement": true, "reason": true,
	"acceptance criteria": true, "tests": true, "evidence": true,
	"code": true, "ur": true,
}

// recognizedLedgerColumns mirrors ledgerColumns' header switch: a header the
// extractor does not recognize is a column carried only as a named extra.
func recognizedLedgerColumn(h string) bool {
	switch {
	case h == "id" || h == "req",
		h == "title" || h == "behaviour" || h == "behavior" || h == "requirement",
		h == "stage", h == "status",
		h == "ur" || h == "user requirement",
		h == "source" || h == "sources" || strings.HasPrefix(h, "src"),
		h == "tests" || h == "evidence" || h == "test",
		h == "code",
		// REQ-CROSS-222: first-class since the capture closure.
		h == "priority", h == "owner", h == "release":
		return true
	}
	return false
}

// BuildFidelityReport measures the corpus at root against the ops the sync
// path would emit for it. data and ops come from the same Snapshot/BuildOps
// pass the real sync uses, so the report measures the true pipeline, not a
// model of it.
func BuildFidelityReport(root string, m *manifest.Manifest, data Data, ops []Op) FidelityReport {
	var r FidelityReport

	// ---- index the emitted ops -------------------------------------------
	sysReqOps := map[string]bool{}  // upsert_requirement, kind system
	userReqOps := map[string]bool{} // upsert_requirement, kind user
	epicOps := map[string]bool{}
	questionGateOps := map[string]bool{}
	// Per-record payloads: a loss is claimed only when the emitted payload
	// actually lacks the field (REQ-CROSS-222 closed the capture — the
	// detectors measure the pipeline, not assumptions about it).
	reqPayload := map[string]map[string]any{}
	epicPayload := map[string]map[string]any{}
	backlogOps := map[string]bool{}
	gateOps := map[string]bool{}
	// Document carriers first: the gate-body decisions below need to know
	// which source files ride the batch whole, and document ops sit at the
	// end of the batch — a single-pass scan would decide before knowing.
	// "Whole" is measured, not assumed: an over-cap file splits into #partN
	// documents, and suppression demands the carried units cover the file —
	// a partial carrier suppressing losses is the silent-loss class itself
	// (WORKLIST.md and docs/85 were cap-cut live).
	docOps := map[string]bool{}
	docUnits := map[string]int{}
	// Carriers for the archival diff: a document op's path, plus every epic
	// spec riding an epic payload. A spec is a carrier too — its content_md
	// holds the file — so a diff that only knew about documents would report
	// every specs/*.md as uncarried and drown the real gap.
	carried := map[string]bool{}
	for _, op := range ops {
		if op.Type == "upsert_epic" {
			if specs, ok := op.Payload["specs"].([]any); ok {
				for _, s := range specs {
					sm, _ := s.(map[string]any)
					if id, _ := sm["external_id"].(string); id != "" {
						carried[id] = true
					}
				}
			}
		}
		if op.Type == "upsert_process_record" {
			// §225.6 successor: the byte archive is the whole-file carrier —
			// exact bytes by construction, so the carried units are the
			// decoded content itself.
			id, _ := op.Payload["external_id"].(string)
			if id != "" {
				docOps[id] = true
				carried[id] = true
				if enc, _ := op.Payload["raw_content"].(string); enc != "" {
					if raw, err := base64.StdEncoding.DecodeString(enc); err == nil {
						docUnits[id] += utf16Len(string(raw))
					}
				}
			}
		}
		if op.Type == "upsert_document" {
			id, _ := op.Payload["external_id"].(string)
			if id == "" {
				continue
			}
			base := id
			if i := strings.Index(id, "#part"); i > 0 {
				base = id[:i]
			}
			docOps[base] = true
			carried[base] = true
			if c, _ := op.Payload["content_md"].(string); c != "" {
				docUnits[base] += utf16Len(c)
			}
		}
	}
	carriedWhole := func(relPath string) bool {
		if !docOps[relPath] {
			return false
		}
		raw, err := os.ReadFile(filepath.Join(root, relPath))
		if err != nil {
			return false
		}
		return docUnits[relPath] >= utf16Len(string(raw))
	}
	// Resolved through the manifest like the builder's carrier list — the
	// checker probing hardcoded paths would share the builder's old blind
	// spot for a binding that relocates these files.
	carriedWholeDoc := func(docType, fallback string) bool {
		if files := m.Resolve(root, docType); len(files) > 0 {
			return carriedWhole(rel(root, files[0]))
		}
		return carriedWhole(fallback)
	}
	rqFileWhole := carriedWholeDoc(manifest.DocReviewQueue, "docs/85-loop-review-queue.md")
	oqFileWhole := carriedWholeDoc(manifest.DocOpenQuestions, "process/08-open-questions.md")
	// APPROVE-* gate bodies are composed from the epic record; whether a cut
	// there loses anything depends on the record's document carrier, which is
	// known in the epics pass below — so the decision is deferred, not made
	// here with half the facts.
	approveGateAtCap := map[string]bool{}
	for _, op := range ops {
		id, _ := op.Payload["external_id"].(string)
		switch op.Type {
		case "upsert_backlog_record":
			backlogOps[id] = true
		case "upsert_requirement":
			if kind, _ := op.Payload["kind"].(string); kind == "user" {
				userReqOps[id] = true
			} else {
				sysReqOps[id] = true
				reqPayload[id] = op.Payload
			}
		case "upsert_epic":
			epicOps[id] = true
			epicPayload[id] = op.Payload
		case "upsert_gate":
			gateOps[id] = true
			if kind, _ := op.Payload["kind"].(string); kind == "question" {
				questionGateOps[id] = true
			}
			if body, _ := op.Payload["body_md"].(string); utf16Len(body) >= fidelityGateBodyCap {
				switch {
				case strings.HasPrefix(id, "APPROVE-"):
					approveGateAtCap[id] = true
				case strings.HasPrefix(id, "RQ-"):
					// §246.3: an RQ body is never cut at the composition cap —
					// its real enforcement point is the 15,000-unit parse cap
					// (extract bodyCap). Between the two thresholds nothing was
					// lost; at the parse cap the loss names the cap that did it,
					// and the archived whole file still suppresses it.
					if !rqFileWhole && utf16Len(body) >= bodyCap {
						r.Losses = append(r.Losses, FidelityLoss{
							RecordID: id, Field: "gate-body", Category: LossTruncated,
							Detail: fmt.Sprintf("review-queue body is at the %d-unit parse cap — content past the cut survives only in the corpus file", bodyCap),
						})
					}
				default:
					r.Losses = append(r.Losses, FidelityLoss{
						RecordID: id, Field: "gate-body", Category: LossTruncated,
						Detail: fmt.Sprintf("gate body is at or beyond the %d-unit composition cap — content past the cut survives only in the corpus file", fidelityGateBodyCap),
					})
				}
			}
		}
	}

	// ---- per-ledger counts, columns, hygiene -----------------------------
	for _, file := range m.Resolve(root, manifest.DocRequirements) {
		relPath := rel(root, file)
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		lines := strings.Split(string(content), "\n")

		rowIDs := []string{}
		rowLine := map[string]int{}
		rowStatus := map[string]int{}
		blockIDs := map[string]bool{}
		var totalsLine int
		totals := map[string]int{}
		headerSeen := false
		var extraHdr []extraCol
		extraHasValue := map[string]bool{}

		reqs := ParseLedger(file, string(content))
		statusByID := map[string]string{}
		for _, q := range reqs {
			statusByID[q.ID] = strings.ToUpper(strip(q.Status))
		}

		headerWidth := 0
		for i, line := range lines {
			if mm := fidelityRowRe.FindStringSubmatch(line); mm != nil {
				rowIDs = append(rowIDs, mm[1])
				rowLine[mm[1]] = i + 1
				rowStatus[statusByID[mm[1]]]++
				// Mapping audit: a row narrower than its
				// header slides every later cell one column left — 14 real
				// rows lost their source cells to it, silently.
				if headerWidth > 0 && len(splitCells(line)) < headerWidth {
					r.Hygiene = append(r.Hygiene, FidelityHygiene{
						File: relPath, Line: i + 1,
						Detail: fmt.Sprintf("row %s is narrower than its header (%d cells vs %d) — later columns shift left", mm[1], len(splitCells(line)), headerWidth),
					})
				}
				if len(extraHdr) > 0 {
					cells := splitCells(line)
					for _, ec := range extraHdr {
						if dashless(cell(cells, ec.Idx)) != "" {
							extraHasValue[ec.Name] = true
						}
					}
				}
			}
			if mm := fidelityBlockRe.FindStringSubmatch(line); mm != nil {
				blockIDs[mm[1]] = true
			}
			if strings.HasPrefix(strings.TrimSpace(line), "Totals:") {
				totalsLine = i + 1
				for _, pair := range fidelityTotalRe.FindAllStringSubmatch(line, -1) {
					n, _ := strconv.Atoi(pair[1])
					totals[pair[2]] += n
				}
			}
			// The first recognizable header row: remember the unrecognized
			// columns; loss evaluation happens after the rows are scanned.
			l := strings.ToLower(strings.TrimSpace(line))
			if !headerSeen && (strings.HasPrefix(l, "| id ") || strings.HasPrefix(l, "|id ") ||
				strings.HasPrefix(l, "| req ") || strings.HasPrefix(l, "|req ")) {
				headerSeen = true
				headerWidth = len(splitCells(line))
				for j, raw := range splitCells(line) {
					name := strings.TrimSpace(raw)
					if name == "" || recognizedLedgerColumn(strings.ToLower(name)) {
						continue
					}
					extraHdr = append(extraHdr, extraCol{Name: name, Idx: j})
				}
			}
		}

		// An unrecognized column is a loss only when it holds values and no
		// row's payload carries it as a named extra.
		for _, ec := range extraHdr {
			if !extraHasValue[ec.Name] || columnCarriedAsExtra(reqPayload, ec.Name) {
				continue
			}
			r.Losses = append(r.Losses, FidelityLoss{
				RecordID: relPath, Field: "column:" + ec.Name, Category: LossLost,
				Detail: "this column reaches no payload — neither recognized nor carried as a named extra",
			})
		}

		count := FidelityCount{Group: relPath, Rows: len(rowIDs)}
		for _, id := range rowIDs {
			if sysReqOps[id] {
				count.Ops++
			} else {
				count.MissingFromOps = append(count.MissingFromOps, id)
			}
		}
		r.Counts = append(r.Counts, count)

		// Totals-line drift: counted statuses vs the line's claim (§221.5).
		if totalsLine > 0 {
			var diffs []string
			seen := map[string]bool{}
			for st, n := range rowStatus {
				seen[st] = true
				if totals[st] != n {
					diffs = append(diffs, fmt.Sprintf("%s counted %d, Totals says %d", st, n, totals[st]))
				}
			}
			for st, n := range totals {
				if !seen[st] && n != 0 {
					diffs = append(diffs, fmt.Sprintf("%s counted 0, Totals says %d", st, n))
				}
			}
			if len(diffs) > 0 {
				sort.Strings(diffs)
				r.Hygiene = append(r.Hygiene, FidelityHygiene{
					File: relPath, Line: totalsLine,
					Detail: "Totals line disagrees with the counted dashboard rows: " + strings.Join(diffs, "; "),
				})
			}
		}
		for _, id := range rowIDs {
			if !blockIDs[id] {
				r.Hygiene = append(r.Hygiene, FidelityHygiene{
					File: relPath, Line: rowLine[id],
					Detail: fmt.Sprintf("dashboard row %s has no detail block", id),
				})
			}
		}
	}

	// ---- per-requirement losses, measured against the emitted payload ----
	for _, q := range data.Reqs {
		p := reqPayload[q.ID]
		// §245.9: "several user requirements" means several UR-SHAPED tokens.
		// An interpunct alone also joins provenance prose in as-built UR
		// cells, which claims no parent at all — the raw cell rides ur_raw.
		if len(allURRefs(q.UR)) > 1 && (p == nil || p["parent_external_ids"] == nil) {
			r.Losses = append(r.Losses, FidelityLoss{
				RecordID: q.ID, Field: "ur-relations", Category: LossLost,
				Detail: fmt.Sprintf("row names several user requirements (%s); the payload declares only the first", strip(q.UR)),
			})
		}
		if strings.ToUpper(strip(q.Status)) == "OBSOLETE" && p == nil {
			r.Losses = append(r.Losses, FidelityLoss{
				RecordID: q.ID, Field: "row", Category: LossLost,
				Detail: "OBSOLETE row emits no op; the terminal decision has no store record",
			})
		}
		if dashless(strip(q.Source)) != "" && (p == nil || p["source_raw"] == nil) {
			r.Losses = append(r.Losses, FidelityLoss{
				RecordID: q.ID, Field: "source-citations", Category: LossTransform,
				Detail: "the source cell is parsed into typed citations; the raw cell text is not preserved",
			})
		}
		// With detail_md carried verbatim, prose, inline decorations, and
		// over-cap notes all survive in the preserved copy — losses only
		// when the payload lacks it.
		detailCarried := p != nil && p["detail_md"] != nil
		if q.Detail != "" && !detailCarried {
			currentLabel := ""
			for _, line := range strings.Split(q.Detail, "\n") {
				if mm := fidelityLabelRe.FindStringSubmatch(line); mm != nil {
					label := strings.TrimSpace(mm[1])
					currentLabel = strings.ToLower(label)
					if !capturedDetailLabels[currentLabel] {
						r.Losses = append(r.Losses, FidelityLoss{
							RecordID: q.ID, Field: "prose:" + label, Category: LossLost,
							Detail: "detail-block prose outside Statement/criteria/evidence reaches no payload",
						})
					}
					if currentLabel == "status" &&
						(strings.Contains(line, "**Priority:**") || strings.Contains(line, "**Owner:**")) {
						r.Losses = append(r.Losses, FidelityLoss{
							RecordID: q.ID, Field: "status-line-decorations", Category: LossLost,
							Detail: "inline Priority/Owner decorations on the Status line are dropped",
						})
					}
					continue
				}
				if (currentLabel == "tests" || currentLabel == "evidence") &&
					fidelityBulletRe.MatchString(line) && utf16Len(line) > fidelityNoteCap {
					r.Losses = append(r.Losses, FidelityLoss{
						RecordID: q.ID, Field: "note-citation", Category: LossTruncated,
						Detail: fmt.Sprintf("evidence note exceeds the %d-unit citation cap and is cut", fidelityNoteCap),
					})
				}
			}
		}
	}

	// ---- epics ------------------------------------------------------------
	// The record text, read once: the losses below measure the carrier, and the
	// disk-side instruments measure the shapes the parsers never reach. Reading
	// each record twice would let the two arms disagree about its content.
	records := map[string]string{}
	syncedRecord := map[string]string{}
	for _, e := range data.Epics {
		recordRel := e.RecordFS
		if recordRel == "" {
			recordRel = e.Record
		}
		if recordRel == "" {
			continue
		}
		syncedRecord[e.ID] = recordRel
		records[e.ID] = ReadEpicRecord(root, recordRel)
	}

	epicCount := FidelityCount{Group: "worklist-epics", Rows: len(data.Epics)}
	// Rows = epics whose record file has text; Ops = records carried verbatim
	// as documents. The two must agree for the record prose to survive the flip.
	recordDocCount := FidelityCount{Group: "epic-records"}
	for _, e := range data.Epics {
		membership := requirementMembershipOf(records[e.ID])
		if membership.Recognized && len(membership.IDs) == 0 {
			r.Hygiene = append(r.Hygiene, FidelityHygiene{
				File:   e.Record,
				Line:   membership.Line,
				Detail: fmt.Sprintf("epic %s has a recognized membership declaration but no members parsed; payload omits the field and preserves stored links", e.ID),
			})
		}
		if epicOps[e.ID] {
			epicCount.Ops++
		} else {
			epicCount.MissingFromOps = append(epicCount.MissingFromOps, e.ID)
		}
		// REQ-CROSS-223 closed the lifecycle carrier: a loss only when the
		// row states a PROCESS token the payload does not carry.
		if e.ProcessStatus != "" {
			if p := epicPayload[e.ID]; p == nil || p["process_status"] == nil {
				r.Losses = append(r.Losses, FidelityLoss{
					RecordID: e.ID, Field: "overall-status", Category: LossLost,
					Detail: "the WORKLIST Overall-status token does not reach the epic payload's process_status",
				})
			}
		}
		for _, spec := range e.Specs {
			if utf16Len(spec.Content) > fidelitySpecCap {
				r.Losses = append(r.Losses, FidelityLoss{
					RecordID: e.ID, Field: "spec:" + spec.Name, Category: LossTruncated,
					Detail: fmt.Sprintf("spec content exceeds the %d-unit cap and is cut at op build", fidelitySpecCap),
				})
			}
		}
		// The record BODY's carrier is its verbatim document op (D5 "lands as
		// preserved text"). Without one, the flip retires epics/** and the
		// prose survives only in git history — a named loss the gate must
		// accept; an APPROVE gate cut at the cap then loses content too.
		recordRel := e.RecordFS
		if recordRel == "" {
			recordRel = e.Record
		}
		if recordRel != "" {
			if text := records[e.ID]; text != "" {
				recordDocCount.Rows++
				size := utf16Len(text)
				switch {
				case docOps[e.Record]:
					recordDocCount.Ops++
					if size > documentContentCap {
						r.Losses = append(r.Losses, FidelityLoss{
							RecordID: e.ID, Field: "record-body", Category: LossTruncated,
							Detail: fmt.Sprintf("the record body (%d units) exceeds the document carrier's %d-unit cap", size, documentContentCap),
						})
					}
				case !gateOps["APPROVE-"+e.ID]:
					r.Losses = append(r.Losses, FidelityLoss{
						RecordID: e.ID, Field: "record-body", Category: LossLost,
						Detail: fmt.Sprintf("the record body (%d units) has no store carrier — no document op, no APPROVE gate, and epics/** retires at the flip", size),
					})
				case size > fidelityGateBodyCap:
					r.Losses = append(r.Losses, FidelityLoss{
						RecordID: e.ID, Field: "record-body", Category: LossTruncated,
						Detail: fmt.Sprintf("the record body (%d units) exceeds the APPROVE gate's %d-unit carrier — the remainder survives only in the corpus file", size, fidelityGateBodyCap),
					})
				}
				if approveGateAtCap["APPROVE-"+e.ID] && !docOps[e.Record] {
					r.Losses = append(r.Losses, FidelityLoss{
						RecordID: "APPROVE-" + e.ID, Field: "gate-body", Category: LossTruncated,
						Detail: fmt.Sprintf("gate body is at or beyond the %d-unit composition cap — content past the cut survives only in the corpus file", fidelityGateBodyCap),
					})
				}
			}
		}
	}
	r.Counts = append(r.Counts, epicCount)
	r.Counts = append(r.Counts, recordDocCount)

	// ---- open questions ---------------------------------------------------
	oqCount := FidelityCount{Group: "open-questions", Rows: len(data.OQs)}
	for _, oq := range data.OQs {
		if questionGateOps[oq.ID] {
			oqCount.Ops++
		} else {
			oqCount.MissingFromOps = append(oqCount.MissingFromOps, oq.ID)
		}
	}
	// Heading-form OQ bodies cap at parse (4,000 units) — below the gate
	// composition cap, so the gate-body detector never sees the cut. The cut
	// loses nothing while the questions file itself rides the batch whole.
	for _, oq := range data.OQs {
		if oqFileWhole {
			break
		}
		if utf16Len(oq.Raw) >= 4000 {
			r.Losses = append(r.Losses, FidelityLoss{
				RecordID: oq.ID, Field: "oq-body", Category: LossTruncated,
				Detail: "the question body is at or beyond the 4,000-unit parse cap — content past the cut survives only in the corpus file",
			})
		}
	}
	r.Counts = append(r.Counts, oqCount)

	// The docs/85 review queue (mapping documentation review):
	// finding-state blocks build no op BY DESIGN, but their file retires at
	// the flip — the drop is named per block, never silent.
	rqCount := FidelityCount{Group: "review-queue", Rows: len(data.RQs)}
	for _, rq := range data.RQs {
		if gateOps[rq.ID] {
			rqCount.Ops++
			continue
		}
		rqCount.MissingFromOps = append(rqCount.MissingFromOps, rq.ID)
		if rq.State == "finding" && !rqFileWhole {
			r.Losses = append(r.Losses, FidelityLoss{
				RecordID: rq.ID, Field: "finding-block", Category: LossLost,
				Detail: "finding-state review blocks build no op by design, and docs/85 retires at the flip — the observation survives only in the corpus file",
			})
		}
	}
	r.Counts = append(r.Counts, rqCount)

	// ---- user requirements ------------------------------------------------
	urMentioned := map[string]bool{}
	for _, q := range data.Reqs {
		for _, f := range strings.Split(q.UR, "·") {
			if id := strip(f); strings.HasPrefix(id, "UR-") {
				urMentioned[id] = true
			}
		}
	}
	for _, e := range data.Epics {
		for _, id := range urTokenRe.FindAllString(e.URCell, -1) {
			urMentioned[id] = true
		}
	}
	urCount := FidelityCount{Group: "user-requirements", Rows: len(urMentioned)}
	for id := range urMentioned {
		if userReqOps[id] {
			urCount.Ops++
		} else {
			urCount.MissingFromOps = append(urCount.MissingFromOps, id)
			// Mapping documentation review: an unextracted UR
			// loses its derives edges silently at ingest — a named loss, not
			// just a count line.
			r.Losses = append(r.Losses, FidelityLoss{
				RecordID: id, Field: "ur-record", Category: LossLost,
				Detail: "named by a ledger row or WORKLIST rollup but carried by no recognized UR source — the UR entity and its derives edges never reach the store",
			})
		}
	}
	sort.Strings(urCount.MissingFromOps)
	r.Counts = append(r.Counts, urCount)

	// Rollup recovery deliberately enters at PROPOSED: the epic's lifecycle is
	// not evidence for a UR the record never declared. Name that weaker status
	// without blocking the chosen carrier.
	seenURSource := map[string]bool{}
	for _, req := range data.Reqs {
		for _, id := range urTokenRe.FindAllString(req.UR, -1) {
			seenURSource[id] = true
		}
	}
	for _, ur := range data.UserReqs {
		seenURSource[ur.ID] = true
	}
	for _, e := range data.Epics {
		for _, ur := range ParseEpicUserRequirements(records[e.ID]) {
			seenURSource[ur.ID] = true
		}
	}
	for _, e := range data.Epics {
		for _, ur := range rollupUserRequirements(e.URCell, records[e.ID]) {
			if seenURSource[ur.ID] {
				continue
			}
			seenURSource[ur.ID] = true
			if epicStatus := urWorkStatus(e); epicStatus != "PROPOSED" && userReqOps[ur.ID] {
				r.Losses = append(r.Losses, FidelityLoss{
					RecordID: ur.ID, Field: "rollup-work-status", Category: LossExcluded,
					Detail: fmt.Sprintf("recovered from %s at PROPOSED while the epic rollup records %s; stronger UR state would invent evidence", e.ID, epicStatus),
				})
			}
		}
	}

	// ---- WORKLIST tables beyond the epic rollup ---------------------------
	r.scanWorklistExtras(root, m, docUnits, taskCarriedWorklistLines(data, records, ops))

	// ---- backlog and gap records (REQ-CROSS-223) --------------------------
	for _, group := range []struct{ name, kind string }{
		{"backlog", "backlog"},
		// Gap records come from two places now — the register's table and the
		// `### GAP-…` blocks a ledger files beside its requirements — so the
		// group is named for the kind rather than for one of its sources.
		{"gap records (register + ledger blocks)", "gap"},
	} {
		count := FidelityCount{Group: group.name}
		for _, b := range data.Backlog {
			if b.Kind != group.kind {
				continue
			}
			count.Rows++
			if backlogOps[b.ExternalID] {
				count.Ops++
			} else {
				count.MissingFromOps = append(count.MissingFromOps, b.ExternalID)
			}
		}
		r.Counts = append(r.Counts, count)
	}
	r.scanBacklogNarratives(root, m, data.Backlog)

	// ---- disk-side arm ----------------------------------------------------
	// Everything above compares the parsed snapshot against the payloads it
	// produced. These compare the FILES and the raw record text against the
	// same payloads, which is the only way to see a loss the parsers took
	// before the snapshot existed.
	r.scanEpicRecordCoverage(root, epicOps, syncedRecord)
	r.scanScenarioShapes(data.Epics, records, scenarioOpIDsOf(ops))
	r.scanLoopStatusCap(root, m)
	r.scanArchivalCoverage(root, carried)
	r.scanEvidenceCellRaw(data.Reqs, reqPayload)
	r.scanEpicURMembership(data.Epics, records, epicPayload, userReqOps)
	// Last of the arms, and deliberately so: it merges its parent entries into
	// the referenced-UR losses above rather than restating them, which it can
	// only do once every other arm has said what it has to say.
	r.scanUnresolvedRelations(epicPayload, reqPayload, sysReqOps, userReqOps)

	// ---- landing homes (REQ-CROSS-246) ------------------------------------
	// After every other count group, so the scenario and archival censuses it
	// reads are already on the report.
	r.scanLandingHomes(root, m, data, records, ops)

	// ---- excluded by design (D2) ------------------------------------------
	if _, err := os.Stat(filepath.Join(root, "PROGRESS.md")); err == nil {
		r.Losses = append(r.Losses, FidelityLoss{
			RecordID: "PROGRESS.md", Field: "derived-projection", Category: LossExcluded,
			Detail: "derived rollup; replaced by the store's own matrix — excluded from the import by decision D2",
		})
	}
	r.Losses = append(r.Losses, FidelityLoss{
		RecordID: "workspace-coverage", Field: "triage-detail", Category: LossExcluded,
		Detail: "coverage triage lists stay local by design; only aggregates sync",
	})

	return r
}

// columnCarriedAsExtra reports whether any emitted payload carries the named
// column in extra_columns — carried once is carried (REQ-CROSS-222 §222.2).
func columnCarriedAsExtra(reqPayload map[string]map[string]any, name string) bool {
	for _, p := range reqPayload {
		extras, _ := p["extra_columns"].([]map[string]any)
		for _, e := range extras {
			if n, _ := e["name"].(string); n == name {
				return true
			}
		}
	}
	return false
}

// rowRecordPath extracts the epics/ record path a rollup row links, whatever
// the link shape — "[record](epics/…)", "[name.md](epics/…)" or a backticked
// bare path. Empty when the row links no record.
func rowRecordPath(cells []string) string {
	for _, c := range cells {
		s := strip(c)
		i := strings.Index(s, "epics/")
		if i < 0 {
			continue
		}
		rest := s[i:]
		if j := strings.IndexAny(rest, ")`| \t"); j >= 0 {
			rest = rest[:j]
		}
		if rest != "epics/" {
			return rest
		}
	}
	return ""
}

// scanWorklistExtras flags rows in WORKLIST tables the op path never reads:
// everything outside the epic rollup. Documentation-shaped sections are
// excluded by design rather than lost.
func (r *FidelityReport) scanWorklistExtras(root string, m *manifest.Manifest, docUnits map[string]int, taskCarried map[int]bool) {
	files := m.Resolve(root, manifest.DocWorklist)
	if len(files) == 0 {
		return
	}
	content, err := os.ReadFile(files[0])
	if err != nil {
		return
	}
	relPath := rel(root, files[0])

	docSections := map[string]bool{
		"progress ownership": true, "status vocabulary": true,
		"completion roll-up rules": true,
	}

	section := ""
	rowsInTable := 0
	rollupSeen := map[string]int{}
	rollupRecord := map[string]string{}
	for lineNo, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "## ") {
			section = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "## ")))
			rowsInTable = 0
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		cells := splitCells(line)
		first := ""
		for _, c := range cells { // leading "|" yields an empty first segment
			if strip(c) != "" {
				first = strip(c)
				break
			}
		}
		// A row merely containing "---" in a cell is content, not a
		// separator — the shared judgment, not a substring probe.
		if isSeparatorRow(strings.TrimSpace(line)) {
			continue
		}
		rowsInTable++
		if rowsInTable == 1 { // header row
			continue
		}
		if first == "" || strings.EqualFold(first, "epic") {
			continue
		}
		// Rollup rows are counted in worklist-epics; here they are only
		// checked for duplicate ids — a duplicate row's second op would
		// ping-pong the store's shadow hash forever, so the extractor keeps
		// the first and this names the shadowed one (a guard flags).
		if strings.Contains(section, "rollup") {
			base := strings.SplitN(first, " ", 2)[0]
			recordPath := rowRecordPath(cells)
			if prev, dup := rollupSeen[base]; dup {
				// Only a real epic id can be a duplicate IDENTITY. Fast-lane rows
				// write a placeholder in the Epic column, and a note calling
				// eleven of those "duplicates" reports a collision that does not
				// exist — the flag stays, its claim does not.
				detail := fmt.Sprintf("duplicate WORKLIST rollup id %s (first at line %d) — only the first row syncs", base, prev)
				if !epicBaseRe.MatchString(base) {
					detail = fmt.Sprintf("fast-lane WORKLIST rollup row without an epic id — the %q placeholder is shared with the row at line %d, so this is a row with no identity rather than a duplicate one", base, prev)
				}
				r.Hygiene = append(r.Hygiene, FidelityHygiene{
					File: relPath, Line: lineNo + 1, Detail: detail,
				})
				// A duplicate id is a LOSS, not untidiness: everything this row
				// states — its record link, its loop status, its scenarios —
				// reaches no op, while the epic count still reconciles because
				// the id is present via the first row. Reported against the
				// shadowed row's location, which is the content that is lost.
				//
				// Only a real epic id can be a duplicate IDENTITY. Fast-lane
				// rows write "—" in the Epic column, and eleven of them in a
				// row are not eleven collisions — escalating that placeholder
				// would block the report on a shape that loses nothing.
				//
				// Two rows for the SAME record lose this row's cells; two
				// DIFFERENT records claiming one id shadow a whole record
				// file. A reader acts differently on each — merge the rows
				// versus rename an epic — so the detail says which it is.
				if epicBaseRe.MatchString(base) {
					detail := fmt.Sprintf("two records claim %s: this row (first at line %d) names %q, the first names %q; the extractor keeps the first, so this row's record is shadowed entirely",
						base, prev, recordPath, rollupRecord[base])
					if recordPath != "" && recordPath == rollupRecord[base] {
						detail = fmt.Sprintf("a second WORKLIST rollup row for the same record repeats %s (first at line %d); the extractor keeps the first, so this row's distinct loop-status and evidence cells reach no op — merge the rows", base, prev)
					}
					r.noteDuplicateIdentity(fmt.Sprintf("%s:%d", relPath, lineNo+1), "duplicate-identity:"+base, detail)
				}
			} else {
				rollupSeen[base] = lineNo + 1
				rollupRecord[base] = recordPath
			}
			continue
		}
		if docSections[section] {
			continue // vocabulary/ownership prose tables: not records
		}
		// REQ-CROSS-264: a work row whose task ids reached upsert_task ops has
		// a TYPED carrier now, not just a place in the byte archive. Named
		// here rather than left to the whole-file suppression below, because
		// the suppression proves preservation and this proves queryability —
		// and a row carrying no task id still falls through and is disclosed.
		if taskCarried[lineNo+1] {
			continue
		}
		// Carried whole as an archival document, the row is preserved text —
		// a partial carrier (units short of the file) suppresses nothing.
		if docUnits[relPath] >= utf16Len(string(content)) {
			continue
		}
		r.Losses = append(r.Losses, FidelityLoss{
			RecordID: first, Field: "worklist-row", Category: LossLost,
			Detail: fmt.Sprintf("row in WORKLIST section %q (%s) has no op path", section, relPath),
		})
	}
}
