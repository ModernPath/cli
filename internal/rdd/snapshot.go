// Snapshot — manifest-driven workspace extraction (REQ-CROSS-013,
// TASK-SY-405): resolve each declared doc type, run its bundled parser, and
// report mandated gaps loudly (D2: honest gap, never silent skip).
package rdd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/modernpath/cli/internal/manifest"
)

// Snapshot parses every document the manifest declares under root.
// warnings carries the loud-report lines: missing mandated types, files a
// parser rejected, and — since 2026-08-12 — when a file is read successfully and
// yields nothing that the reader would have expected to find. An extractor that
// recognises nothing otherwise reports nothing, which is indistinguishable from
// a workspace that has nothing.
//
// Optional types with no files at all stay silent (enrichment).
// looksLikeQuestions reports whether a document mentions question identifiers at
// all — the cheap test for "this file had something to say and we heard none of it".
func looksLikeQuestions(content string) bool {
	return regexp.MustCompile(`(?m)(?:^|\s)(?:OQ|Q)-[A-Za-z0-9-]+`).MatchString(content)
}

func Snapshot(root string, m *manifest.Manifest) (Data, []string) {
	var data Data
	var warnings []string

	for _, docType := range m.MissingMandated(root) {
		spec := m.Documents[docType]
		warnings = append(warnings, fmt.Sprintf(
			"MANDATED document type %q matches no files (globs: %v) — the factory needs it; sync continues with what exists",
			docType, spec.Globs()))
	}

	for _, file := range m.Resolve(root, manifest.DocRequirements) {
		content, err := os.ReadFile(file)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("requirements: cannot read %s: %v", rel(root, file), err))
			continue
		}
		// REQ-CROSS-223: a ledger files capability gaps beside the requirements
		// they block. They are backlog records, so they are collected here and
		// carried with the rest of the backlog — before the row check below,
		// because a ledger may hold gap blocks and no requirement rows.
		if ledgerContext(file) != "" {
			data.Backlog = append(data.Backlog, ParseLedgerGaps(rel(root, file), string(content))...)
		}
		reqs := ParseLedger(file, string(content))
		if reqs == nil {
			// Two different failures reached the same message, and it named only
			// the first: a path the context regex cannot read, and a readable
			// path whose rows carry ids the row regex does not (RUN:2026-08-23 —
			// `tasks/NFR-REQUIREMENTS.md` held 26 `NFR-PERF-001`-style rows and
			// was reported as an unrecognised *path*, sending the reader to the
			// filename while the ids were the cause).
			if LedgerContext(file) == "" {
				warnings = append(warnings, fmt.Sprintf(
					"requirements: %s is not a recognised ledger path — expected <CTX>-REQUIREMENTS.md or <ctx>/REQUIREMENTS.md — skipped", rel(root, file)))
			} else {
				warnings = append(warnings, fmt.Sprintf(
					"requirements: %s parsed 0 rows — a dashboard row must start `| REQ-<CTX>-NNN |`%s — skipped",
					rel(root, file), firstUnparsedRowHint(string(content))))
			}
			continue
		}
		data.Reqs = append(data.Reqs, reqs...)
	}

	// REQ-CROSS-286: resolve Candidate-packet relations across the whole corpus
	// before anything reads a parent. Relations cross ledgers, so this cannot
	// run inside ParseLedger, and it must run BEFORE the multi-parent warning
	// below so a packet-filled parent is judged on the same terms as a column.
	if filled, unresolved, unrepresentable := ApplyPacketRelations(data.Reqs); filled > 0 || len(unresolved) > 0 || len(unrepresentable) > 0 {
		if filled > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"requirements: %d parent link(s) read from Candidate packets (REQ-CROSS-286)", filled))
		}
		// Naming an id that does not exist is a corpus defect, not a parse
		// failure — say so rather than dropping it silently.
		if len(unresolved) > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"requirements: %d Candidate-packet relation(s) name ids no row carries: %s",
				len(unresolved), strings.Join(unresolved, ", ")))
		}
		// The payload carries a parent only for a system requirement. Naming a
		// user requirement as a child is a real declaration with nowhere to go,
		// and saying nothing would make the reported count a lie.
		if len(unrepresentable) > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"requirements: %d Candidate-packet relation(s) are not representable (%s) — the payload carries only SR→UR parentage, so a UR child or an SR parent is not synced",
				len(unrepresentable), strings.Join(unrepresentable, ", ")))
		}
	}

	// SR-SY-1402: a `- **UR:** UR-A · UR-B` line names several parents; one
	// edge rides the payload. Say which, and what was left behind. §245.9:
	// several PARENTS means several UR-shaped tokens — interpunct-joined
	// provenance prose names none and warrants no warning.
	for _, req := range data.Reqs {
		if len(allURRefs(req.UR)) > 1 {
			warnings = append(warnings, fmt.Sprintf(
				"requirements: %s names multiple user requirements (%s) — only the first (%s) is synced as parent_external_id",
				req.ID, strings.Join(strings.Fields(req.UR), " "), firstURRef(req.UR)))
		}
	}

	// SR-SY-1403: the user-requirement layer. Optional — no files, no noise —
	// but a file that mentions UR headings and parses to nothing is a loud gap.
	for _, file := range m.Resolve(root, manifest.DocUserRequirements) {
		content, err := os.ReadFile(file)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("user requirements: cannot read %s: %v", rel(root, file), err))
			continue
		}
		urs, warns := ParseUserRequirements(rel(root, file), string(content))
		warnings = append(warnings, warns...)
		if len(urs) == 0 && looksLikeUserRequirements(string(content)) {
			warnings = append(warnings, fmt.Sprintf(
				"user requirements: %s mentions UR headings but none parsed — expected '## UR-<AREA>-NNN — <title>' blocks", rel(root, file)))
		}
		data.UserReqs = append(data.UserReqs, urs...)
	}

	// REQ-CROSS-237: the gate flat-file. Optional — most workspaces have none —
	// but when it exists its confirmation gates are the only exit a DERIVED
	// corpus has, so they must reach the store.
	for _, file := range m.Resolve(root, manifest.DocGates) {
		content, err := os.ReadFile(file)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("gates: cannot read %s: %v", rel(root, file), err))
			continue
		}
		records := ParseGateFile(string(content))
		if len(records) == 0 {
			if strings.Contains(string(content), "## GATE-") {
				warnings = append(warnings, fmt.Sprintf(
					"gates: %s mentions GATE- headings but none parsed — expected '## GATE-<AREA>-<NNN> — <purpose>' sections", rel(root, file)))
			}
			continue
		}
		data.Gates = append(data.Gates, records...)
		data.GatesRef = rel(root, file)

		// Say what will and will not reach the store. The defect this row fixes
		// was invisible precisely because 23 gates went missing in silence.
		_, gateWarnings := BuildGateFileOps(records, rel(root, file))
		for _, w := range gateWarnings {
			warnings = append(warnings, "gates: "+w)
		}
	}

	if files := m.Resolve(root, manifest.DocWorklist); len(files) > 0 {
		content, err := os.ReadFile(files[0])
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("worklist: cannot read %s: %v", rel(root, files[0]), err))
		} else {
			names, isDir := epicDirListing(root, m)
			data.Epics = ParseWorklist(string(content), names, isDir)
			// REQ-CROSS-264: the rows the epic set cannot carry — dropped
			// rollup rows and the Work Rows / Blocked-Deferred sections.
			data.WorklistTaskRows = ParseWorklistTaskRows(string(content))
			// Worklist record links ("[record](epics/…)") are relative to the
			// worklist file, not the sync root. When the manifest points one
			// level up (../WORKLIST.md), give each epic a root-resolvable
			// RecordFS for reading — but leave Record untouched: it is payload
			// identity (origin_ref), and rebasing it would change gate hashes
			// between roots (SCN-SY-046).
			if base := filepath.Dir(rel(root, files[0])); base != "." {
				for i := range data.Epics {
					if data.Epics[i].Record != "" {
						data.Epics[i].RecordFS = filepath.Join(base, data.Epics[i].Record)
					}
				}
			}
			warnings = append(warnings, collectEpicSpecs(root, data.Epics)...)
		}
	}

	if files := m.Resolve(root, manifest.DocReviewQueue); len(files) > 0 {
		if content, err := os.ReadFile(files[0]); err == nil {
			data.RQs = ParseReviewQueue(string(content))
		}
	}

	// REQ-CROSS-223: unrouted triage discoveries and capability gaps.
	for _, spec := range []struct{ docType, kind string }{
		{manifest.DocBacklog, "backlog"},
		{manifest.DocGapRegister, "gap"},
	} {
		for _, file := range m.Resolve(root, spec.docType) {
			if content, err := os.ReadFile(file); err == nil {
				data.Backlog = append(data.Backlog, ParseBacklogRows(rel(root, file), string(content), spec.kind)...)
			}
		}
	}
	// Every mapped file, not just the first: a manifest may point this document
	// type at several (an open-questions inventory AND a gap register). Reading
	// files[0] alone silently stopped parsing the inventory when a second path
	// was added — its gates froze at their last-seen text with no error.
	for _, f := range m.Resolve(root, manifest.DocOpenQuestions) {
		if content, err := os.ReadFile(f); err == nil {
			got := ParseOpenQuestions(string(content))
			// A file that exists, is read, and yields nothing is the failure mode
			// silence hides: sixty questions parsed as zero while the human queue
			// reported itself clear (`RUN:2026-08-12`). If it looks like it holds
			// questions, say so rather than syncing an empty queue.
			if len(got) == 0 && looksLikeQuestions(string(content)) {
				warnings = append(warnings, fmt.Sprintf(
					"open questions: %s mentions question ids but none parsed — the human queue will look clear when it is not; expected a table of OQ rows or '## Q-001 — text' sections",
					rel(root, f)))
			}
			data.OQs = append(data.OQs, got...)
		}
	}

	data.Commits = ParseGitLog(root)
	// REQ-CROSS-084: phase D's output and the guides that steered it.
	data.Docs = CollectDocuments(root)

	// D5 archival carriers, resolved through the manifest so a binding that
	// relocates its worklist/review-queue/open-questions files still carries
	// them whole — hardcoded root paths silently dropped relocated files.
	data.ArchivalFiles = archivalFileRels(root, m)

	return data, warnings
}

// archivalFileRels resolves the flip-retired carrier files as
// workspace-relative paths (the payload identity BuildOps and the fidelity
// checker share).
//
// The population is every file the flip retires, from the same list the
// report measures and the flip freezes. A carrier list of named process
// files covered three of them and left the ledgers, the backlog, the gap
// register and everything beside an epic record with nothing — and after the
// flip a retired file with no carrier exists nowhere.
//
// The manifest-resolved worklist, review queue and open questions come first
// and by name: a binding may relocate them outside the retired families, and
// they must ride either way.
//
// Files another carrier already holds — an epic record's verbatim document,
// an epic spec riding its epic payload — stay in this list; BuildOps drops
// them, because it is the side that knows which carriers it emitted.
func archivalFileRels(root string, m *manifest.Manifest) []string {
	seen := map[string]bool{}
	var rels []string
	add := func(r string) {
		r = filepath.ToSlash(r)
		if r == "" || r == "." || seen[r] {
			return
		}
		seen[r] = true
		rels = append(rels, r)
	}
	for _, docType := range []string{
		manifest.DocWorklist,
		manifest.DocReviewQueue,
		manifest.DocOpenQuestions,
	} {
		for _, file := range m.Resolve(root, docType) {
			add(rel(root, file))
		}
	}
	for _, r := range retiredFilesOnDisk(root) {
		add(r)
	}
	return rels
}

// collectEpicSpecs loads each folder epic's specs/*.md (REQ-CROSS-023,
// EPIC-SYNC-006), sorted by name. Rel (payload identity) derives from the
// canonical Record path; bytes resolve via RecordFS when set (SCN-SY-046).
// Warnings: content over the wire cap (truncated at op build) and a
// SPEC-READY marker over an empty specs/ (no gate will be emitted — honest
// gap, never a silent skip).
func collectEpicSpecs(root string, epics []Epic) []string {
	var warnings []string
	for i := range epics {
		e := &epics[i]
		if !strings.HasSuffix(e.Record, "/EPIC.md") {
			continue
		}
		readRecord := e.Record
		if e.RecordFS != "" {
			readRecord = e.RecordFS
		}
		relDir := strings.TrimSuffix(e.Record, "/EPIC.md")
		fsSpecsDir := filepath.Join(root, filepath.Dir(readRecord), "specs")

		entries, err := os.ReadDir(fsSpecsDir)
		if err == nil {
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
					names = append(names, entry.Name())
				}
			}
			sort.Strings(names)
			for _, name := range names {
				raw, err := os.ReadFile(filepath.Join(fsSpecsDir, name))
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("epic %s: cannot read spec %s: %v", e.ID, name, err))
					continue
				}
				content := string(raw)
				if capRunes(content, specContentCap) != content {
					warnings = append(warnings, fmt.Sprintf(
						"epic %s: spec %s exceeds the %d-char wire cap and will be TRUNCATED in the sync payload", e.ID, name, specContentCap))
				}
				e.Specs = append(e.Specs, EpicSpec{Rel: relDir + "/specs/" + name, Name: name, Content: content})
			}
		}

		marker := ""
		if raw, err := os.ReadFile(filepath.Join(root, readRecord)); err == nil {
			text := string(raw)
			marker, _ = SpecApprovalLineOf(text)

			// REQ-CROSS-114: a record whose `## Specification approval` table
			// grants the gate while its `## Specification status` still says
			// SPEC-DRAFT (or SPEC-DERIVED) contradicts itself. The conservative
			// reading is what syncs; the contradiction is said out loud, because
			// a silently resolved one is how this defect class survives.
			if marker != "approved" && specApprovalRowOf(text) != "" {
				warnings = append(warnings, fmt.Sprintf(
					"epic %s records a granted specification approval, but its Specification status marker does not say SPEC-APPROVED — the marker is what syncs, so the approval is NOT published; make the two agree", e.ID))
			}
		}

		if len(e.Specs) == 0 && (marker == "ready" || marker == "approved") {
			warnings = append(warnings, fmt.Sprintf(
				"epic %s is %s but specs/ is empty or missing — no spec-approval gate will be emitted (an approval over nothing is dishonest)", e.ID, strings.ToUpper("spec-"+marker)))
		}

		// D5 enforcement signal (REQ-CROSS-024): implementation before the
		// spec approval is a process violation — warn loudly, never silently.
		if e.State == "in-progress" && marker != "approved" && (len(e.Specs) > 0 || marker != "") {
			warnings = append(warnings, fmt.Sprintf(
				"epic %s is IN_PROGRESS without an approved specification — the spec-approval gate (SPEC-APPROVE-%s) must pass before implementation (CLAUDE.md §6B)", e.ID, e.ID))
		}
	}
	return warnings
}

// epicDirListing reproduces the extractor's epics-directory view (name list +
// which entries are directories) from the manifest's epics globs.
func epicDirListing(root string, m *manifest.Manifest) ([]string, map[string]bool) {
	seen := map[string]bool{}
	isDir := map[string]bool{}
	var names []string
	for _, f := range m.Resolve(root, manifest.DocEpics) {
		name := filepath.Base(f)
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
		if info, err := os.Stat(f); err == nil && info.IsDir() {
			isDir[name] = true
		}
	}
	sort.Strings(names) // readdir order
	return names, isDir
}

func rel(root, file string) string {
	if r, err := filepath.Rel(root, file); err == nil {
		return r
	}
	return file
}
