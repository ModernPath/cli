// SR-SY-1403 (EPIC-SYNC-014) — the user-requirement layer, extracted.
// `requirements/*-USER-REQUIREMENTS.md` is what the UR/SCN derivation pass
// writes: one `## UR-<AREA>-NNN — <title>` block per user requirement, plain
// bullets (Actor, Statement, Intended use, Source, Validation, Status), and
// the acceptance scenarios as `### SCN-…` blocks with a gherkin fence.
// One analyzed estate holds 102 URs and 186 SCNs in this shape, and none of
// it left the workspace — compliance_user_requirements stayed empty and the
// traceability face showed 596 orphan SRs (`RUN:2026-08-16`).
package rdd

import (
	"fmt"
	"regexp"
	"strings"
)

// FileSCN is one resolved acceptance-scenario block: the id and the gherkin
// fence body (or, when a block has no fence, its prose).
type FileSCN struct {
	ID   string
	Body string
}

// FileUR is one `## UR-…` block of a user-requirements file.
type FileUR struct {
	ID        string
	Title     string
	Statement string // re-flowed to one line
	Status    string // normalized vocabulary term; PROPOSED when absent
	Source    string // the Source bullet's body, re-flowed
	Scenarios []FileSCN
}

var (
	urFileHeadRe  = regexp.MustCompile(`(?m)^##\s+(UR-[A-Z][A-Z0-9]*-\d+)\s*[—–-]\s*(.+)$`)
	scnFileHeadRe = regexp.MustCompile(`(?m)^###\s+(SCN-[A-Z][A-Z0-9]*-\d+)`)
	nextH2Re      = regexp.MustCompile(`(?m)^##\s`)
	nextH3orH2Re  = regexp.MustCompile(`(?m)^###?\s`)
	urBulletRe    = regexp.MustCompile(`(?m)^-\s+`)
	urFieldRe     = regexp.MustCompile(`^(Actor|Statement|Intended use|Source|Validation|Status):[ \t]*([\s\S]*)$`)
	gherkinRe     = regexp.MustCompile("(?s)```(?:gherkin)?\n(.*?)```")
	scnIDGlobalRe = regexp.MustCompile(`SCN-[A-Z][A-Z0-9]*-\d+`)
)

// ParseUserRequirements parses one user-requirements file. rel is the
// workspace-relative path, used only in warnings. Validation ids that resolve
// to no `### SCN-…` block in the same file are reported, never invented.
func ParseUserRequirements(rel, content string) ([]FileUR, []string) {
	var warnings []string

	// Every SCN block in the file, id → fence body (or prose when no fence).
	scns := map[string]FileSCN{}
	scnHeads := scnFileHeadRe.FindAllStringSubmatchIndex(content, -1)
	for _, loc := range scnHeads {
		id := content[loc[2]:loc[3]]
		body := content[loc[1]:]
		if next := nextH3orH2Re.FindStringIndex(body); next != nil {
			body = body[:next[0]]
		}
		if fence := gherkinRe.FindStringSubmatch(body); fence != nil {
			body = fence[1]
		}
		scns[id] = FileSCN{ID: id, Body: strings.TrimSpace(body)}
	}

	var urs []FileUR
	heads := urFileHeadRe.FindAllStringSubmatchIndex(content, -1)
	for _, loc := range heads {
		id := content[loc[2]:loc[3]]
		title := strings.TrimSpace(content[loc[4]:loc[5]])

		block := content[loc[1]:]
		if next := nextH2Re.FindStringIndex(block); next != nil {
			block = block[:next[0]]
		}
		// The bullets live above the first SCN heading.
		bullets := block
		if scn := scnFileHeadRe.FindStringIndex(bullets); scn != nil {
			bullets = bullets[:scn[0]]
		}

		ur := FileUR{ID: id, Title: title, Status: "PROPOSED"}
		var validation string
		for _, piece := range urBulletRe.Split(bullets, -1) {
			m := urFieldRe.FindStringSubmatch(piece)
			if m == nil {
				continue
			}
			value := strings.Join(strings.Fields(m[2]), " ")
			switch m[1] {
			case "Statement":
				ur.Statement = value
			case "Source":
				ur.Source = value
			case "Validation":
				validation = value
			case "Status":
				if s := normalizeStatus(value); s != "" {
					ur.Status = s
				}
			}
		}

		for _, scnID := range scnIDGlobalRe.FindAllString(validation, -1) {
			scn, ok := scns[scnID]
			if !ok {
				warnings = append(warnings, fmt.Sprintf(
					"user requirements: %s names %s in %s's Validation line, but the file has no matching '### %s' block — the scenario is not synced",
					rel, scnID, id, scnID))
				continue
			}
			ur.Scenarios = append(ur.Scenarios, scn)
		}
		urs = append(urs, ur)
	}
	return urs, warnings
}

// looksLikeUserRequirements is the loud-gap test for a file that was read and
// yielded nothing: it mentions UR ids, so silence would hide a parse failure.
func looksLikeUserRequirements(content string) bool {
	return regexp.MustCompile(`(?m)^##\s+UR-`).MatchString(content)
}

// ---------------------------------------------------------------- gherkin

// gherkinGWT splits a fence body into its Given/When/Then clauses. And/But
// lines fold into the clause they continue, keeping their own keyword, so
// "Then a And b" reads as the two assertions it is. A body without both a
// Given and a Then is not GWT and stays whole as a statement.
func gherkinGWT(body string) (given, when, then string, ok bool) {
	var cur *string
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "" || strings.HasPrefix(line, "Scenario"):
			continue
		case strings.HasPrefix(line, "Given "):
			given = strings.TrimPrefix(line, "Given ")
			cur = &given
		case strings.HasPrefix(line, "When "):
			when = strings.TrimPrefix(line, "When ")
			cur = &when
		case strings.HasPrefix(line, "Then "):
			then = strings.TrimPrefix(line, "Then ")
			cur = &then
		case cur != nil:
			*cur += " " + line
		}
	}
	if given == "" || then == "" {
		return "", "", "", false
	}
	return given, when, then, true
}

// ---------------------------------------------------------------- op builder

// urSourceCitations reads a UR's Source bullet: the anchor `REQ-*` rows as doc
// citations, then the whole-path code/test references through the SR-SY-1401
// extractor. Untagged paths in a Source line are documentation.
func urSourceCitations(source string) []any {
	out := []any{}
	seen := map[string]bool{}
	add := func(kind, ref string) {
		key := kind + "\x00" + ref
		if ref == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, map[string]any{"kind": kind, "ref": ref})
	}
	for _, id := range reqIDGlobalRe.FindAllString(source, -1) {
		add("doc", id)
	}
	refs, _ := extractCitationRefs(source)
	for _, r := range refs {
		kind := r.kind
		if kind == "" {
			kind = "doc"
		}
		add(kind, r.ref)
	}
	return out
}

// BuildLedgerUserRequirementOp emits one file-derived user requirement as a
// requirement of kind "user", its SCNs riding as scenario-kind criteria —
// namespaced by their UR exactly as requirement criteria and epic scenarios
// are, because acceptance_criteria.external_id is unique per tenant.
func BuildLedgerUserRequirementOp(ur FileUR) Op {
	ctx := ""
	if m := urCtxRe.FindStringSubmatch(ur.ID); m != nil {
		ctx = m[1]
	}

	criteria := []any{}
	for _, scn := range ur.Scenarios {
		c := map[string]any{
			"external_id": ur.ID + "#" + scn.ID,
			"position":    len(criteria) + 1,
			"kind":        "scenario",
		}
		if given, when, then, ok := gherkinGWT(scn.Body); ok {
			c["given"] = given
			if when != "" {
				c["when"] = when
			}
			c["then"] = then
		} else if statement := strings.Join(strings.Fields(scn.Body), " "); statement != "" {
			c["statement"] = statement
		} else {
			continue // an empty block carries no observable behavior
		}
		criteria = append(criteria, c)
	}

	payload := map[string]any{
		"external_id":      ur.ID,
		"kind":             "user",
		"title":            capRunes(ur.Title, 200),
		"description":      ur.Statement,
		"context":          ctx,
		"stage":            nil,
		"work_status":      ur.Status,
		"source_citations": urSourceCitations(ur.Source),
		"criteria":         criteria,
	}
	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	return Op{Type: "upsert_requirement", Payload: payload}
}
