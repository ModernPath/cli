package rdd

// The authority flip deletes the retired-path population from the repository.
// After it, a retired file with no store carrier exists nowhere — so "every
// retired file has a carrier" is not a coverage nicety, it is the condition
// under which the flip is not a deletion.
//
// These tests fix the carrier contract: every file matched by
// RetiredPathFamilies rides the batch as document content that reassembles
// byte-identically to the file on disk, whatever its extension, whatever its
// size; and a file another carrier already holds is carried once, not twice.

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/manifest"
)

// documentCarriers folds the batch's document ops back onto their base paths:
// external_id → the concatenated content of part 1..N, in order.
func documentCarriers(t *testing.T, ops []Op) map[string]string {
	t.Helper()
	parts := map[string]map[int]string{}
	for _, op := range ops {
		// §225.6 successor: the byte archive is a carrier too — decoded here
		// so every byte-for-byte check below covers it, and a file claimed by
		// both kinds trips the same one-file-two-carriers guard.
		if op.Type == "upsert_process_record" {
			id, _ := op.Payload["external_id"].(string)
			enc, _ := op.Payload["raw_content"].(string)
			raw, err := base64.StdEncoding.DecodeString(enc)
			if err != nil {
				t.Fatalf("process record %s carries undecodable bytes: %v", id, err)
			}
			if parts[id] == nil {
				parts[id] = map[int]string{}
			}
			if _, dup := parts[id][1]; dup {
				t.Fatalf("two ops claim %s — one file, two carriers", id)
			}
			parts[id][1] = string(raw)
			continue
		}
		if op.Type != "upsert_document" {
			continue
		}
		id, _ := op.Payload["external_id"].(string)
		if id == "" {
			t.Fatalf("a document op carries no external_id: %+v", op.Payload)
		}
		base, n := id, 1
		if i := strings.Index(id, "#part"); i > 0 {
			base = id[:i]
			n = 0
			for _, r := range id[i+len("#part"):] {
				if r < '0' || r > '9' {
					t.Fatalf("unreadable part marker in %q", id)
				}
				n = n*10 + int(r-'0')
			}
		}
		if parts[base] == nil {
			parts[base] = map[int]string{}
		}
		if _, dup := parts[base][n]; dup {
			t.Fatalf("two document ops claim %s part %d — one file, two carriers", base, n)
		}
		content, _ := op.Payload["content_md"].(string)
		parts[base][n] = content
	}
	out := map[string]string{}
	for base, byPart := range parts {
		var b strings.Builder
		for n := 1; n <= len(byPart); n++ {
			chunk, ok := byPart[n]
			if !ok {
				t.Fatalf("%s is missing part %d — the carrier is partial", base, n)
			}
			b.WriteString(chunk)
		}
		out[base] = b.String()
	}
	return out
}

// writeArchivalCorpus lays down one file of every retired shape that no
// structured op carries: the root ledgers, the process-state files, and the
// epics/** material beside the records (task files, aux prose, a non-markdown
// contract). One file is deliberately far past the document cap.
func writeArchivalCorpus(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{
		"tasks/AR-REQUIREMENTS.md": "# REQUIREMENTS — AR\n\n" +
			"| ID | Title | Stage | Status | UR | Source | Tests | Code |\n" +
			"|---|---|---|---|---|---|---|---|\n" +
			"| REQ-AR-001 | A row | MVP | PROPOSED | UR-AR-001 | doc-a | — | — |\n",
		"WORKLIST.md":                 "# WORKLIST\n",
		"BACKLOG.md":                  "# Backlog\n\nA discovery nobody routed yet.\n",
		"PROGRESS.md":                 "# Progress\n\n| Context | Done |\n|---|---|\n| AR | 1 |\n",
		"process/gap-register.md":     "# Gap register\n\n### GAP-AR-1 — a capability nobody built\n\nNarrative.\n",
		"epics/README.md":             "# Epics\n\nHow this folder is organised.\n",
		"epics/EPIC-AR-001-x/EPIC.md": "# EPIC-AR-001 — Fixture\n\n## User outcome\n\nProse.\n",
		"epics/EPIC-AR-001-x/specs/architecture.md": "# Architecture spec\n\nGrounded claims.\n",
		"epics/EPIC-AR-001-x/DECISION-INTERVIEW.md": "# Decision interview\n\nQ: why? A: because.\n",
		// Not markdown, and not to be reshaped into any: significant
		// indentation and trailing structure survive or the contract is
		// no longer the contract.
		"epics/EPIC-AR-001-x/API-OPENAPI.yaml": "openapi: 3.1.0\ninfo:\n  title: AR\n  version: \"1\"\npaths:\n  /ar:\n    get:\n      responses:\n        \"200\":\n          description: ok\n",
		// Past the 262,144-unit document cap: the carrier must split and
		// reassemble, not cut.
		"epics/EPIC-AR-001-x/tasks/TASK-AR-001-big.md": "# TASK-AR-001\n\n" +
			strings.Repeat("Filler prose that pushes this task file far past the document cap. ", 6000),
	}
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return files
}

func TestEveryRetiredFileRidesTheBatchByteForByte(t *testing.T) {
	root := t.TempDir()
	files := writeArchivalCorpus(t, root)

	m := manifest.Default()
	data, _ := Snapshot(root, m)
	ops := BuildOps(data, func(rel string) string { return ReadEpicRecord(root, rel) }, "2026-08-22")

	carriers := documentCarriers(t, ops)
	// The epic's spec rides its epic payload, not a document — collect those
	// carriers too, so "carried once" can be checked across both kinds.
	specCarrier := map[string]string{}
	for _, op := range ops {
		if op.Type != "upsert_epic" {
			continue
		}
		specs, _ := op.Payload["specs"].([]any)
		for _, s := range specs {
			sm, _ := s.(map[string]any)
			id, _ := sm["external_id"].(string)
			content, _ := sm["content_md"].(string)
			specCarrier[id] = content
		}
	}

	var uncarried, doubleCarried, wrongContent []string
	for _, rel := range retiredFilesOnDisk(root) {
		want, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		doc, inDocs := carriers[rel]
		spec, inSpecs := specCarrier[rel]
		switch {
		case !inDocs && !inSpecs:
			uncarried = append(uncarried, rel)
		case inDocs && inSpecs:
			doubleCarried = append(doubleCarried, rel)
		case inDocs && doc != string(want):
			wrongContent = append(wrongContent, rel)
		case inSpecs && spec != string(want):
			wrongContent = append(wrongContent, rel)
		}
	}
	sort.Strings(uncarried)
	if len(uncarried) > 0 {
		t.Errorf("%d retired file(s) reach no carrier — the flip would delete content the store never received: %v",
			len(uncarried), uncarried)
	}
	if len(doubleCarried) > 0 {
		t.Errorf("%d retired file(s) ride two carriers: %v", len(doubleCarried), doubleCarried)
	}
	if len(wrongContent) > 0 {
		t.Errorf("%d carrier(s) do not reproduce the file byte for byte: %v", len(wrongContent), wrongContent)
	}

	// The non-markdown contract is carried verbatim: no reflow, no fence, no
	// markdown wrapper around bytes that are not markdown.
	yamlRel := "epics/EPIC-AR-001-x/API-OPENAPI.yaml"
	if got := carriers[yamlRel]; got != files[yamlRel] {
		t.Errorf("the yaml contract was transformed on its way into the payload:\n got %q\nwant %q", got, files[yamlRel])
	}

	// §225.6 successor: the over-cap file does NOT split — the byte archive
	// has no content cap, so the whole file rides one record and reassembly
	// seams stop existing.
	bigRel := "epics/EPIC-AR-001-x/tasks/TASK-AR-001-big.md"
	if got := carriers[bigRel]; got != files[bigRel] {
		t.Fatalf("the over-cap file must ride whole and unsplit: %d vs %d bytes", len(got), len(files[bigRel]))
	}
	for _, op := range ops {
		if id, _ := op.Payload["external_id"].(string); strings.HasPrefix(id, bigRel+"#part") {
			t.Fatalf("the byte archive must not split: %s", id)
		}
	}

	// And the instrument that measures this agrees: nothing uncarried, in
	// either direction.
	r := BuildFidelityReport(root, m, data, ops)
	for _, l := range r.Losses {
		if l.Field == "uncarried-retired-files" || l.Field == "archival-carrier-stale" {
			t.Errorf("archival coverage still reports a loss: %s — %s", l.Field, l.Detail)
		}
	}
	for _, c := range r.Counts {
		if strings.HasPrefix(c.Group, "archival-coverage") && c.Ops != c.Rows {
			t.Errorf("archival coverage: %d of %d retired files carried (missing %v)", c.Ops, c.Rows, c.MissingFromOps)
		}
	}
}

// A shadowed epic record — two files claiming one id, the extractor keeping
// the first — reaches no epic op. Its bytes must still ride, and the shadowing
// must still be reported: a raw archive preserves the prose, it does not
// resolve the identity collision.
func TestShadowedEpicRecordIsArchivedAndStillReported(t *testing.T) {
	root := t.TempDir()
	writeArchivalCorpus(t, root)
	shadow := "# EPIC-AR-001 — The other record claiming this id\n\nContent that no epic op carries.\n"
	abs := filepath.Join(root, "epics", "EPIC-AR-001-elsewhere.md")
	if err := os.WriteFile(abs, []byte(shadow), 0o644); err != nil {
		t.Fatal(err)
	}
	worklist := "# WORKLIST\n\n## Epics\n\n| Epic | State |\n|---|---|\n" +
		"| EPIC-AR-001 | [record](epics/EPIC-AR-001-x/EPIC.md) |\n" +
		"| EPIC-AR-001 | [record](epics/EPIC-AR-001-elsewhere.md) |\n"
	if err := os.WriteFile(filepath.Join(root, "WORKLIST.md"), []byte(worklist), 0o644); err != nil {
		t.Fatal(err)
	}

	m := manifest.Default()
	data, _ := Snapshot(root, m)
	ops := BuildOps(data, func(rel string) string { return ReadEpicRecord(root, rel) }, "2026-08-22")

	// Whichever of the two the extractor keeps, BOTH files' bytes ride: the
	// shadowed one is exactly the content that reaches no epic op.
	carriers := documentCarriers(t, ops)
	for rel, want := range map[string]string{
		"epics/EPIC-AR-001-elsewhere.md": shadow,
		"epics/EPIC-AR-001-x/EPIC.md":    "# EPIC-AR-001 — Fixture\n\n## User outcome\n\nProse.\n",
	} {
		if got := carriers[rel]; got != want {
			t.Errorf("%s did not ride byte for byte:\n got %q\nwant %q", rel, got, want)
		}
	}
	r := BuildFidelityReport(root, m, data, ops)
	reported := false
	for _, l := range r.Losses {
		if strings.HasPrefix(l.Field, "epic-record-shadowed") || strings.HasPrefix(l.Field, "duplicate-identity") {
			reported = true
		}
	}
	if !reported {
		t.Error("archiving the file silenced the identity collision — the raw archive is not an epic op, and the collision is still a loss")
	}
}

// A carrier list resolved only from the manifest carried three files. The
// population it must cover is the retired one, so the resolver answers from
// the same list the flip and the report use.
func TestArchivalCarrierListCoversTheRetiredPopulation(t *testing.T) {
	root := t.TempDir()
	writeArchivalCorpus(t, root)

	got := map[string]bool{}
	for _, rel := range archivalFileRels(root, manifest.Default()) {
		if got[rel] {
			t.Errorf("%s appears twice in the carrier list", rel)
		}
		got[rel] = true
	}
	var missing []string
	for _, rel := range retiredFilesOnDisk(root) {
		if !got[rel] {
			missing = append(missing, rel)
		}
	}
	if len(missing) > 0 {
		t.Errorf("the carrier list omits %d retired file(s): %v", len(missing), missing)
	}
}
