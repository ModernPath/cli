package cmd

// REQ-CROSS-449 (EPIC-CLI-TURNS): a review pull writes one REVIEW.md holding
// the whole packet, so the reviewer reads it in one read instead of ~20. The
// per-file layout stays; the bundle is a projection of the same reads.

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

const reviewBundleFile = "REVIEW.md"

// reviewScopeIndex reads the scope item, then the members the epic's served
// membership names (SRs then URs) — two direct item reads. The selection's
// frozen list is only the fallback for a scope whose read serves no
// membership (a single_sr scope, or an older server).
func reviewScopeIndex(env *factoryEnv, scopeExt string, frozen []string) (map[string]scopeRecord, []string, error) {
	idx, err := scopeIndex(env, []string{scopeExt})
	if err != nil {
		return nil, nil, err
	}
	members := frozen
	if rec, ok := idx[scopeExt]; ok && rec.kind == "epic" {
		members = servedMembers(rec.payload, frozen)
	}
	for _, mid := range members {
		if unsafeSnapshotName(mid) {
			return nil, nil, fmt.Errorf("member id %q is not a safe path component — refusing to pull", mid)
		}
	}
	if len(members) == 0 {
		return idx, members, nil
	}
	more, err := scopeIndex(env, members)
	if err != nil {
		return nil, nil, err
	}
	for id, rec := range more {
		idx[id] = rec
	}
	return idx, members, nil
}

// sectionOrder is the reading order of the packet files: recon, the
// enrichments, the RED strategy, the decisions, then any extra section.
func sectionOrder(rows []map[string]any) []map[string]any {
	out := append([]map[string]any{}, rows...)
	sort.SliceStable(out, func(i, j int) bool {
		return packetFileName(str(out[i], "section_key")) < packetFileName(str(out[j], "section_key"))
	})
	return out
}

func renderReviewBundle(scopeKind, scopeExt, aggregate, ctxID string, now time.Time,
	idx map[string]scopeRecord, members []string, sections []map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# REVIEW — %s\n\n", scopeExt)
	fmt.Fprintf(&b, "- **Scope:** %s:%s\n", scopeKind, scopeExt)
	fmt.Fprintf(&b, "- **Packet aggregate:** %s\n", orMarker(aggregate, "«not served — `process review record` refuses this pull»"))
	fmt.Fprintf(&b, "- **Review context:** %s\n", ctxID)
	fmt.Fprintf(&b, "- **Pulled at:** %s\n\n", now.Format(time.RFC3339))
	b.WriteString("The whole packet in one file, in reading order: the scope, the packet sections, the user requirements, then the system requirements. Each heading names the id and the fingerprint the pull read. The per-file copies beside it hold the same content.\n\n")

	if rec, ok := idx[scopeExt]; ok {
		if rec.kind == "epic" {
			b.WriteString("## Epic\n\n")
			writeBundleEpic(&b, scopeExt, rec.payload)
		} else {
			b.WriteString("## Scope\n\n")
			writeBundleRequirement(&b, scopeExt, rec)
		}
	} else {
		fmt.Fprintf(&b, "## Scope\n\n%s — not served by the item read.\n\n", scopeExt)
	}

	b.WriteString("## Packet sections\n\n")
	if len(sections) == 0 {
		b.WriteString("None served.\n\n")
	}
	for _, sm := range sectionOrder(sections) {
		fmt.Fprintf(&b, "### %s · %s\n\n%s\n\n", str(sm, "section_key"), orMarker(str(sm, "content_fingerprint"), "—"),
			strings.TrimRight(str(sm, "content"), "\n"))
	}

	var urs, srs, missing []string
	for _, id := range members {
		rec, ok := idx[id]
		switch {
		case !ok:
			missing = append(missing, id)
		case rec.kind == "user":
			urs = append(urs, id)
		default:
			srs = append(srs, id)
		}
	}
	b.WriteString("## User requirements\n\n")
	if len(urs) == 0 {
		b.WriteString("None in scope.\n\n")
	}
	for _, id := range urs {
		writeBundleRequirement(&b, id, idx[id])
	}
	b.WriteString("## System requirements\n\n")
	if len(srs) == 0 {
		b.WriteString("None in scope.\n\n")
	}
	for _, id := range srs {
		writeBundleRequirement(&b, id, idx[id])
	}
	if len(missing) > 0 {
		fmt.Fprintf(&b, "## Members not served by the item read\n\n%s\n", strings.Join(missing, ", "))
	}
	return b.String()
}

// REQ-CROSS-464 (EPIC-CLI-DELTA): a review pull stamps what it read, item by
// item, and a later round's pull renders only what changed since the previous
// review trace. The trace still pins the full aggregate.

// reviewedStampPrefix starts each per-item line of a review stamp. The other
// stamp readers match by their own prefixes and skip these lines.
const reviewedStampPrefix = "reviewed: "

type reviewedEntry struct {
	key, fingerprint string
	render           func(b *strings.Builder)
}

// reviewedFingerprints is every record and packet section the bundle holds,
// in its reading order — the scope, the sections, the URs, then the SRs —
// each with the fingerprint the pull read and how to render it in full. An
// item served without a fingerprint is left out.
func reviewedFingerprints(scopeExt string, idx map[string]scopeRecord, members []string, sections []map[string]any) []reviewedEntry {
	var out []reviewedEntry
	add := func(key, fp string, render func(b *strings.Builder)) {
		if key != "" && fp != "" {
			out = append(out, reviewedEntry{key: key, fingerprint: fp, render: render})
		}
	}
	if rec, ok := idx[scopeExt]; ok {
		add(scopeExt, str(rec.payload, "fingerprint"), func(b *strings.Builder) {
			if rec.kind == "epic" {
				writeBundleEpic(b, scopeExt, rec.payload)
			} else {
				writeBundleRequirement(b, scopeExt, rec)
			}
		})
	}
	for _, sm := range sectionOrder(sections) {
		sm := sm
		add(str(sm, "section_key"), str(sm, "content_fingerprint"), func(b *strings.Builder) {
			fmt.Fprintf(b, "### %s · %s\n\n%s\n\n", str(sm, "section_key"), str(sm, "content_fingerprint"), strings.TrimRight(str(sm, "content"), "\n"))
		})
	}
	for _, wantUR := range []bool{true, false} {
		for _, id := range members {
			rec, ok := idx[id]
			if !ok || (rec.kind == "user") != wantUR {
				continue
			}
			id := id
			add(id, str(rec.payload, "fingerprint"), func(b *strings.Builder) { writeBundleRequirement(b, id, rec) })
		}
	}
	return out
}

// readContextReviewed reads the per-item fingerprints a review pull stamped;
// empty for a stamp an older CLI wrote.
func readContextReviewed(dir string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(readContextFile(dir), "\n") {
		if rest, ok := strings.CutPrefix(line, reviewedStampPrefix); ok {
			if key, fp, ok := strings.Cut(strings.TrimSpace(rest), " "); ok && key != "" && fp != "" {
				out[key] = strings.TrimSpace(fp)
			}
		}
	}
	return out
}

// readSinceTrace reads the previous review a delta bundle is taken against and
// refuses one it cannot be: not a cold review, another scope's, or one
// recorded without per-item fingerprints.
func readSinceTrace(env *factoryEnv, traceID, scopeExt string) (map[string]any, error) {
	status, body, err := env.call("GET",
		fmt.Sprintf("/api/v1/sync/gates/%s?system_id=%d", url.PathEscape(traceID), env.SystemID), nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, gateShowError(status, body, traceID)
	}
	trace, _ := dataOf(body)["gate"].(map[string]any)
	if purpose := str(trace, "purpose"); purpose != "cold-review" {
		return nil, fmt.Errorf("%s is not a cold review (purpose %s) — --since takes the previous cold-review trace of %s; nothing was pulled", traceID, presentPin(purpose), scopeExt)
	}
	if scope := stringSlice(trace["exact_scope"]); len(scope) == 0 || scope[0] != scopeExt {
		return nil, fmt.Errorf("%s reviewed %s, not %s — --since takes a previous cold review of this scope; nothing was pulled", traceID, presentPin(strings.Join(scope, ", ")), scopeExt)
	}
	if m, _ := trace["reviewed_fingerprints"].(map[string]any); len(m) == 0 {
		return nil, fmt.Errorf("%s carries no reviewed fingerprints (it was recorded before they were, or by an older CLI) — pull without --since for a full review; nothing was pulled", traceID)
	}
	return trace, nil
}

type deltaInput struct {
	scopeKind, scopeExt, aggregate, ctxID, head string
	now                                         time.Time
	idx                                         map[string]scopeRecord
	members                                     []string
	sections                                    []map[string]any
	reviewed                                    []reviewedEntry
	prior                                       map[string]any
}

// renderDeltaReviewBundle is REVIEW.md for a later round: the header names the
// previous trace's code revision beside HEAD, then what changed (in full, no
// text diff — prior content is not stored), the open findings, the previous
// verdict, and the unchanged items as id and fingerprint.
func renderDeltaReviewBundle(env *factoryEnv, in deltaInput) string {
	traceID := str(in.prior, "external_id")
	priorFP := map[string]string{}
	if m, ok := in.prior["reviewed_fingerprints"].(map[string]any); ok {
		for k, v := range m {
			if s, ok := v.(string); ok {
				priorFP[k] = s
			}
		}
	}
	revision := str(in.prior, "application_revision")

	var b strings.Builder
	fmt.Fprintf(&b, "# REVIEW — %s (since %s)\n\n", in.scopeExt, traceID)
	fmt.Fprintf(&b, "- **Scope:** %s:%s\n", in.scopeKind, in.scopeExt)
	fmt.Fprintf(&b, "- **Packet aggregate:** %s\n", orMarker(in.aggregate, "«not served — `process review record` refuses this pull»"))
	fmt.Fprintf(&b, "- **Review context:** %s\n", in.ctxID)
	fmt.Fprintf(&b, "- **Pulled at:** %s\n", in.now.Format(time.RFC3339))
	fmt.Fprintf(&b, "- **Since:** %s (verdict %s)\n", traceID, orMarker(str(in.prior, "verdict"), "—"))
	fmt.Fprintf(&b, "- **Code revision at that review:** %s\n", orMarker(revision, "«not recorded»"))
	fmt.Fprintf(&b, "- **Code revision now (HEAD):** %s\n\n", orMarker(in.head, "«not a git checkout»"))
	b.WriteString("A later review round. Only what changed since the previous review trace is shown in full; review the changes, the open findings and whatever the changes touch among the unchanged items. The review still pins the full packet aggregate above.\n\n")
	if revision == "" || in.head == "" || revision != in.head {
		fmt.Fprintf(&b, "**The code revision moved or cannot be compared** (%s → %s): re-verify the citations of the unchanged items against HEAD.\n\n",
			orMarker(revision, "not recorded"), orMarker(in.head, "unknown"))
	}

	b.WriteString("## What changed since the last review\n\n")
	current := map[string]bool{}
	var unchanged []reviewedEntry
	changed := 0
	for _, e := range in.reviewed {
		current[e.key] = true
		old, known := priorFP[e.key]
		switch {
		case known && old == e.fingerprint:
			unchanged = append(unchanged, e)
			continue
		case known:
			fmt.Fprintf(&b, "Changed: %s was reviewed at %s, now %s.\n\n", e.key, old, e.fingerprint)
		default:
			fmt.Fprintf(&b, "Added since the last review: %s, now %s.\n\n", e.key, e.fingerprint)
		}
		changed++
		e.render(&b)
	}
	var removed []string
	for k := range priorFP {
		if !current[k] {
			removed = append(removed, k)
		}
	}
	sort.Strings(removed)
	for _, k := range removed {
		fmt.Fprintf(&b, "Removed since the last review: %s, reviewed at %s.\n\n", k, priorFP[k])
		changed++
	}
	if changed == 0 {
		b.WriteString("Nothing changed since the last review.\n\n")
	}

	b.WriteString("## Open findings\n\n")
	scope := in.scopeKind + ":" + in.scopeExt
	if findings, err := readScopeFindings(env, scope); err != nil {
		fmt.Fprintf(&b, "The findings read failed: %v\n\n", err)
	} else {
		ids := make([]string, 0, len(findings))
		for id, f := range findings {
			if d := str(f, "disposition"); d == "OPEN" || d == "DEFERRED" {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		if len(ids) == 0 {
			b.WriteString("None open.\n\n")
		}
		for _, id := range ids {
			f := findings[id]
			fmt.Fprintf(&b, "- **%s** [%s/%s] %s\n", id, str(f, "category"), str(f, "disposition"), str(f, "body"))
		}
		if len(ids) > 0 {
			b.WriteString("\n")
		}
	}

	b.WriteString("## Previous verdict\n\n")
	fmt.Fprintf(&b, "**%s** (%s)\n\n", orMarker(str(in.prior, "verdict"), "—"), traceID)
	if body := strings.TrimSpace(str(in.prior, "body_md")); body != "" {
		b.WriteString(body + "\n\n")
	}

	b.WriteString("## Unchanged since the last review\n\n")
	if len(unchanged) == 0 {
		b.WriteString("None.\n\n")
	}
	for _, e := range unchanged {
		fmt.Fprintf(&b, "- %s · %s\n", e.key, e.fingerprint)
	}
	var missing []string
	for _, id := range in.members {
		if _, ok := in.idx[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(&b, "\n## Members not served by the item read\n\n%s\n", strings.Join(missing, ", "))
	}
	return b.String()
}

func orMarker(v, marker string) string {
	if v == "" {
		return marker
	}
	return v
}

func writeBundleEpic(b *strings.Builder, id string, m map[string]any) {
	fmt.Fprintf(b, "### %s · %s\n\n", id, orMarker(str(m, "fingerprint"), "—"))
	fmt.Fprintf(b, "- **Title:** %s\n", orMarker(str(m, "title"), "—"))
	fmt.Fprintf(b, "- **Status:** %s\n", orMarker(str(m, "process_status"), "—"))
	fmt.Fprintf(b, "- **Outcome source:** %s\n", orMarker(str(m, "outcome_source"), "—"))
	fmt.Fprintf(b, "- **Members:** %s\n\n", memberUnion(m, "—"))
	for _, f := range []struct{ key, label string }{{"description", "Description"}, {"scope", "Scope"}} {
		if v := str(m, f.key); v != "" {
			fmt.Fprintf(b, "**%s.** %s\n\n", f.label, v)
		}
	}
	// REQ-CROSS-471: what the chat that created the epic found, from the
	// epic's shared_context snapshot.
	if found := foundInChat(m); len(found) > 0 {
		b.WriteString("**Found in chat.**\n\n")
		b.WriteString(dashList(found))
		b.WriteString("\n\n")
	}
}

// foundInChat lists an epic's found references ("kind · name") from its
// shared_context, in the order the epic holds them.
func foundInChat(m map[string]any) []string {
	shared, _ := m["shared_context"].(map[string]any)
	refs, _ := shared["references"].([]any)
	out := []string{}
	for _, r := range refs {
		ref, _ := r.(map[string]any)
		if str(ref, "kind") != "found" {
			continue
		}
		name := str(ref, "name")
		if name == "" {
			name = str(ref, "target_ref")
		}
		out = append(out, orMarker(str(ref, "ref_kind"), "found")+" · "+name)
	}
	return out
}

func dashList(items []string) string {
	lines := make([]string, len(items))
	for i, item := range items {
		lines[i] = "- " + item
	}
	return strings.Join(lines, "\n")
}

func writeBundleRequirement(b *strings.Builder, id string, rec scopeRecord) {
	m := rec.payload
	fmt.Fprintf(b, "### %s · %s\n\n", id, orMarker(str(m, "fingerprint"), "—"))
	fmt.Fprintf(b, "- **Title:** %s\n", orMarker(str(m, "title"), "—"))
	fmt.Fprintf(b, "- **Status:** %s\n", orMarker(str(m, "work_status"), "—"))
	if parents := stringSlice(m["parent_external_ids"]); len(parents) > 0 {
		fmt.Fprintf(b, "- **Parents:** %s\n", strings.Join(parents, ", "))
	}
	// REQ-CROSS-461: the statement's sources, rendered as the per-file copy
	// renders them (REQ-CROSS-381).
	fmt.Fprintf(b, "- **Sources:** %s\n", citationLine(m, notServed))
	b.WriteString("\n")
	fmt.Fprintf(b, "**Statement.** %s\n\n", orMarker(str(m, "description"), "—"))
	if rec.kind != "user" {
		for _, f := range []struct{ key, label string }{
			{"rationale", "Rationale"}, {"boundary", "Boundary"}, {"verification_method", "Verification method"},
		} {
			fmt.Fprintf(b, "**%s.** %s\n\n", f.label, orMarker(str(m, f.key), "—"))
		}
	}
	if scenarios := scenariosFromPayload(m); len(scenarios) > 0 {
		label := "Acceptance criteria"
		if rec.kind == "user" {
			label = "Scenarios"
		}
		fmt.Fprintf(b, "**%s.**\n\n", label)
		for _, sc := range scenarios {
			fmt.Fprintf(b, "- %s\n", sc)
		}
		b.WriteString("\n")
	}
}
