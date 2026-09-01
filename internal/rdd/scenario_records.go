package rdd

// REQ-CROSS-252 (EPIC-CLI-003 T11): scenario definitions are first-class
// records. One upsert_scenario op per declared definition — the record's own
// shapes via the same parser the epic payload uses, plus the spec files'
// definitions, which until now survived only as opaque spec content. Ids stay
// epic-namespaced ("EPIC-X#SCN-Y") exactly as the criterion rows are, and the
// declared owner is the epic — ownership is a declaration, never an
// inference.
//
// REQ-CROSS-265 (tranche 4): the requirement edge reads every DECLARING header
// — `Realizes` and the `Requirement` family — and every id it reads is
// validated against the requirements this same build emitted. An id that
// resolves to nothing is a named loss rather than a dangling edge.

import (
	"fmt"
	"regexp"
	"strings"
)

// An epic-local thin-SR id. Its own class because zero `SR-*` requirement rows
// exist in the store: a cell naming only these declares an edge to nothing.
var srIDRe = regexp.MustCompile(`\bSR-[A-Z][A-Za-z0-9]*-\d+[a-z]?\b`)

// BuildScenarioOps emits the epic's scenario records: record-declared
// definitions first, then spec-file definitions, deduplicated by id (a spec
// restating a record's scenario is one scenario).
//
// known is the requirement inventory this build emitted — the set an edge may
// point at.
func BuildScenarioOps(epic Epic, recordText string, known map[string]bool) []Op {
	edges := parseScenarioRealizes(recordText, known)
	seen := map[string]bool{}
	var ops []Op

	emit := func(items []any, sourcePath string) {
		for _, raw := range items {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			extID, _ := item["external_id"].(string)
			if extID == "" || seen[extID] {
				continue
			}
			seen[extID] = true

			payload := map[string]any{
				"external_id":                extID,
				"declared_owner_type":        "epic",
				"declared_owner_external_id": epic.ID,
				"source_path":                sourcePath,
			}
			for _, k := range []string{"title", "given", "when", "then", "statement", "status", "evidence_conclusion", "source_line", "source_raw"} {
				if v, ok := item[k]; ok && v != "" {
					payload[k] = v
				}
			}
			scn := extID
			if i := strings.LastIndex(extID, "#"); i >= 0 {
				scn = extID[i+1:]
			}
			// The declaring column first, then any reference the definition
			// wrote inline — the same declaration in the shape that record
			// uses. Both go through the same resolution rule: an id nothing
			// emitted is not an edge.
			ids := edges.bySCN[scn]
			for _, ref := range asAnySlice(item["declared_refs"]) {
				id, _ := ref.(string)
				if id == "" || containsAny(ids, id) {
					continue
				}
				if !resolvesToRequirement(known, id) {
					continue
				}
				ids = append(ids, id)
			}
			if len(ids) > 0 {
				payload["required_requirement_external_ids"] = ids
			}
			payload["content_hash"] = ContentHash(payload)
			payload["actor"] = actor
			ops = append(ops, Op{Type: "upsert_scenario", Payload: payload})
		}
	}

	src := epic.Record
	if src == "" {
		src = epic.ID
	}
	emit(parseScenarioRecords(recordText, epic.ID), src)
	for _, spec := range epic.Specs {
		emit(parseScenarioRecords(spec.Content, epic.ID), spec.Rel)
	}
	return ops
}

// resolvesToRequirement reports whether an id names a requirement this build
// emitted. A nil inventory means the caller supplied none — the focused parser
// tests — and every id is admitted; every production caller supplies one.
func resolvesToRequirement(known map[string]bool, id string) bool {
	return known == nil || known[id]
}

// scenarioEdgeResult is one record's parsed scenario→requirement edges, plus
// the row-level classes the coverage instrument reports rather than drops.
type scenarioEdgeResult struct {
	bySCN map[string][]any
	// declaring counts rows under a declaring header whose cell says something.
	declaring int
	// srOnly are rows naming only epic-local SR-* thin-SR ids. Zero SR-*
	// requirements exist in the store, so admitting them would be an edge to
	// nothing; corpus correction is the recovery path.
	srOnly []string
	// bareToken are rows carrying a context token that is not an id in any
	// family. Admitting them needs an id-minting rule, which is a decision.
	bareToken []string
	// unresolved are ids read from a declaring column that name no emitted
	// requirement — the disclosed correction for the dangling families.
	unresolved []string
	// carried are the SCN ids that ended with at least one edge.
	carried map[string]bool
	// declaredIDs are every SCN id whose declaring column stated something —
	// the corpus-defined denominator, edges or not.
	declaredIDs []string
}

// declaringEdgeHeader reports whether a column heading DECLARES the scenario's
// requirement. Two forms are measured in the corpus: `Realizes` (33 rows) and
// the `Requirement` family (124 rows, `System requirements` among them). A
// prose column is never one, which is what keeps a requirement id written
// inside a Summary or Gherkin cell a mention rather than an invented edge.
func declaringEdgeHeader(h string) bool {
	l := strings.ToLower(strings.TrimSpace(h))
	return strings.Contains(l, "realizes") || strings.Contains(l, "requirement")
}

// parseScenarioRealizes maps SCN ids to the requirement ids their table row
// declares — read only from a column the header names, never scraped from
// prose, with ranges and slash alternates expanded by the membership token
// rules every other carrier uses.
func parseScenarioRealizes(recordText string, known map[string]bool) scenarioEdgeResult {
	res := scenarioEdgeResult{bySCN: map[string][]any{}, carried: map[string]bool{}}
	var header []string
	var declaring []int
	for _, line := range strings.Split(recordText, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			header, declaring = nil, nil
			continue
		}
		cells := decisionCells(trimmed)
		if len(cells) == 0 || decisionSeparatorRow(cells) {
			continue
		}
		if header == nil {
			header = lowerCells(cells)
			declaring = nil
			for i, h := range header {
				if i > 0 && declaringEdgeHeader(h) {
					declaring = append(declaring, i)
				}
			}
			continue
		}
		if len(declaring) == 0 {
			continue
		}
		scn := scnTokenRe.FindString(cells[0])
		if scn == "" {
			continue
		}
		var ids []any
		stated, sawSR, sawText, sawID := false, false, false, false
		for _, col := range declaring {
			if col >= len(cells) {
				continue
			}
			cell := cells[col]
			if strings.TrimSpace(strings.Trim(strings.TrimSpace(cell), "—–- ")) == "" {
				continue // an empty cell declares nothing; it is not a shortfall
			}
			stated = true
			if srIDRe.MatchString(cell) {
				sawSR = true
			}
			sawText = true
			var parsed []string
			addRequirementMembershipTokens(cell, map[string]bool{}, &parsed)
			if len(parsed) > 0 {
				sawID = true
			}
			for _, id := range parsed {
				if containsAny(ids, id) {
					continue
				}
				if !resolvesToRequirement(known, id) {
					res.unresolved = append(res.unresolved,
						fmt.Sprintf("%s → %s: the id names no emitted requirement", scn, id))
					continue
				}
				ids = append(ids, id)
			}
		}
		if !stated {
			continue
		}
		res.declaring++
		res.declaredIDs = append(res.declaredIDs, scn)
		if len(ids) > 0 {
			res.bySCN[scn] = ids
			res.carried[scn] = true
			continue
		}
		switch {
		case sawID:
			// The row DID declare ids; they simply resolve to nothing. That is
			// the unresolved class above, already named per id — not a
			// vocabulary problem to be reported a second time.
		case sawSR:
			res.srOnly = append(res.srOnly,
				fmt.Sprintf("%s: declares only epic-local SR-* thin-SR ids; no SR-* requirement exists to point at", scn))
		case sawText:
			res.bareToken = append(res.bareToken,
				fmt.Sprintf("%s: declares a bare context token; admitting it needs an id-minting rule, which is a decision", scn))
		}
	}
	return res
}

func containsAny(items []any, s string) bool {
	for _, it := range items {
		if it == s {
			return true
		}
	}
	return false
}

func asAnySlice(v any) []any {
	list, _ := v.([]any)
	return list
}
