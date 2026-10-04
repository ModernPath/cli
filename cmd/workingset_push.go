package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/authoring"
	"github.com/modernpath/cli/internal/authoring/diff"
)

// workingSetPush parses each item file in the current scope directory, diffs it
// against the freshly-read store snapshot, assembles one atomic `author patch`
// per changed record under the fingerprint recorded at pull, puts each changed
// packet section whole, prints the op plan, and refuses — naming the file — an
// edit it cannot type (SR-CLI-0085). A 409 on one record leaves that file
// unpushed and reports every other file independently; a `--for-review`
// directory is refused outright.
func workingSetPush(env *factoryEnv, dryRun bool) error {
	payload, err := fetchWorkSelection(env)
	if err != nil {
		return err
	}
	current, _ := payload["current"].(map[string]any)
	if current == nil {
		return fmt.Errorf("no current work selection — nothing to push")
	}
	scopeExt := str(current, "scope_external_id")
	scopeKind := str(current, "scope_kind")
	dir := filepath.Join(env.Root, workingSetDir, scopeExt)

	mode, ctxID := readContextStamp(dir)
	if mode == "review" {
		return fmt.Errorf("%s is a --for-review directory (context %s) — review pulls are read-only and never push; pull without --for-review to author", scopeExt, ctxID)
	}

	members := stringSlice(current["members"])
	ids := append([]string{scopeExt}, members...)
	idx, err := scopeIndex(env, ids)
	if err != nil {
		return err
	}

	type itemFile struct{ path, rel, ext string }
	items := []itemFile{}
	if _, err := os.Stat(filepath.Join(dir, scopeExt+".md")); err == nil {
		items = append(items, itemFile{filepath.Join(dir, scopeExt+".md"), scopeExt + ".md", scopeExt})
	}
	memberSet := map[string]bool{}
	for _, m := range members {
		memberSet[m] = true
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "members"))
	var outOfScopeFiles, outOfScopeSkips []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		ext := strings.TrimSuffix(e.Name(), ".md")
		if !memberSet[ext] {
			// Resolve only stale local filenames exactly, preserving the existing
			// skip-known/refuse-unknown behavior without a system-wide index.
			outOfScopeFiles = append(outOfScopeFiles, ext)
			continue
		}
		items = append(items, itemFile{filepath.Join(dir, "members", e.Name()), filepath.Join("members", e.Name()), ext})
	}
	if len(outOfScopeFiles) > 0 {
		stale, err := scopeIndex(env, outOfScopeFiles)
		if err != nil {
			return fmt.Errorf("could not verify out-of-scope member files: %w", err)
		}
		for id, record := range stale {
			idx[id] = record
		}
		for _, id := range outOfScopeFiles {
			if _, known := idx[id]; known {
				// A record the store holds outside the selection is not pushed
				// through this scope; say so rather than drop it silently.
				outOfScopeSkips = append(outOfScopeSkips, id)
				continue
			}
			items = append(items, itemFile{filepath.Join(dir, "members", id+".md"), filepath.Join("members", id+".md"), id})
		}
	}

	var plan []string
	var skipped []string
	var conflicts []string
	var pushed []itemFile
	mutationItems := map[string]scopeRecord{}

	// PLAN PASS — read, parse and diff every item and packet section BEFORE any
	// POST, so a malformed or unreadable later file cannot abort the command after
	// an earlier file's mutation has already committed. No write happens until the
	// whole working set validates.
	var writes []*recordWrite
	fileOf := map[string]itemFile{}
	for _, it := range items {
		raw, err := os.ReadFile(it.path)
		if err != nil {
			return err
		}
		// REQ-CROSS-442: push never creates a record; `author apply` is the only
		// create path. An unknown id stops the push here, before any write.
		base, known := idx[it.ext]
		if !known {
			return fmt.Errorf("%s names %s, which the store does not know — push never creates a record. Create it with `author apply --file <plan>`, then pull and push. Nothing was written", it.rel, it.ext)
		}
		var fallback []string
		if base.kind == "epic" {
			fallback = members
		}
		editedRec, perr := authoring.Parse(base.kind, itemBody(string(raw)))
		if perr != nil {
			return fmt.Errorf("%s: cannot parse the authoring grammar (%v)", it.rel, perr)
		}
		editedRec.ExternalID = it.ext
		w, werr := planRecordWrite(it.rel, &base, editedRec, fallback)
		if werr != nil {
			return werr
		}
		if !w.changes() {
			continue
		}
		// The CAS expectation is the fingerprint recorded at pull.
		w.expected = servedFingerprint(string(raw))
		plan = append(plan, w.describe())
		writes = append(writes, w)
		fileOf[it.ext] = it
	}

	var servedScopeMembers []string
	if sr, ok := idx[scopeExt]; ok {
		servedScopeMembers = servedMembers(sr.payload, nil)
	}
	defer printOutOfScopeSkips(scopeExt, outOfScopeSkips, servedScopeMembers)

	plannedSections, err := planPacketSections(env, dir, scopeKind, scopeExt, &plan, &skipped)
	if err != nil {
		return err
	}

	// A dry run prints the full plan and writes nothing. The re-stamp plan is
	// judged at the CURRENT scope context (the patches above are not applied).
	if dryRun {
		if stale, serr := staleUnchangedSections(env, dir, scopeKind, scopeExt, plannedKeys(plannedSections), wsPushRestamp); serr == nil {
			for _, ps := range stale {
				plan = append(plan, fmt.Sprintf("packet section %s (would re-stamp — unchanged content, stale scope context)", ps.key))
			}
		}
		printPlan(plan, skipped, dryRun)
		return nil
	}

	// APPLY PASS — every file validated; now issue the writes.
	results, wconf, werr := applyRecordWrites(env, ctxID, writes)
	if werr != nil {
		return werr
	}
	conflicts = append(conflicts, wconf...)
	for _, w := range writes {
		res := results[w.ext]
		if res.conflict || (!res.created && !res.patched) {
			continue
		}
		pushed = append(pushed, fileOf[w.ext])
		if res.item != nil && res.patched {
			mutationItems[w.ext] = *res.item
		}
	}

	pconf, err := applyPacketSections(env, dir, ctxID, scopeKind, scopeExt, plannedSections)
	if err != nil {
		return err
	}
	conflicts = append(conflicts, pconf...)

	// REQ-CROSS-384 (EPIC-CLI-018): a member patch above moved the packet's scope
	// context, and an earlier push may have left sections behind at an older
	// one. Re-put every unchanged section the server now reports as stale so it
	// carries the current stamp — after the patches, never after a 409 on one
	// (the context that patch would have moved has not moved).
	var restamped []string
	if len(conflicts) == 0 {
		stale, serr := staleUnchangedSections(env, dir, scopeKind, scopeExt, plannedKeys(plannedSections), wsPushRestamp)
		if serr != nil {
			return serr
		}
		rconf, rerr := applyPacketSections(env, dir, ctxID, scopeKind, scopeExt, stale)
		if rerr != nil {
			return rerr
		}
		conflicts = append(conflicts, rconf...)
		for _, ps := range stale {
			restamped = append(restamped, ps.key)
			plan = append(plan, fmt.Sprintf("packet section %s (re-stamped — unchanged content, scope context moved)", ps.key))
		}
	}

	// Re-render each pushed item file from the FRESH store state, so operation
	// markers (a `- withdraw X`) and server-normalized fields do not linger in the
	// body and re-emit on the next push. One re-fetch reflects every patch.
	if len(pushed) > 0 {
		pushedIDs := make([]string, 0, len(pushed))
		for _, it := range pushed {
			pushedIDs = append(pushedIDs, it.ext)
		}
		fresh := mutationItems
		// Each mutation response is canonical and fingerprint-current. When
		// several records changed together, fetch the exact set once more so
		// later membership/relation writes are reflected in earlier files.
		if len(pushed) > 1 || len(fresh) != len(pushed) {
			var ferr error
			fresh, ferr = scopeIndex(env, pushedIDs)
			if ferr != nil {
				return fmt.Errorf("push applied %d record(s), but canonical refresh failed; re-pull before editing again: %w", len(pushed), ferr)
			}
		}
		for _, it := range pushed {
			rec, ok := fresh[it.ext]
			if !ok {
				return fmt.Errorf("push applied %d record(s), but canonical refresh omitted %s; re-pull before editing again", len(pushed), it.ext)
			}
			var r authoring.Record
			if rec.kind == "epic" {
				r = recordFromPayload(rec, members)
			} else {
				r = recordFromPayload(rec, nil)
			}
			if err := atomicWrite(it.path, []byte(scopeItemContent(r, mode, ctxID, false, env, time.Now()))); err != nil {
				return fmt.Errorf("push applied %d record(s), but could not refresh %s; re-pull before editing again: %w", len(pushed), it.rel, err)
			}
		}
	}

	printPlan(plan, skipped, dryRun)
	if len(restamped) > 0 {
		fmt.Printf("push: re-stamped %d section(s) whose scope context moved: %s\n", len(restamped), strings.Join(restamped, ", "))
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("%d file(s) conflicted — the store moved since pull: %s; re-pull those and retry", len(conflicts), strings.Join(conflicts, ", "))
	}
	return nil
}

func plannedKeys(planned []plannedSection) map[string]bool {
	keys := map[string]bool{}
	for _, ps := range planned {
		keys[ps.key] = true
	}
	return keys
}

func servedFingerprint(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "- **Served fingerprint:** ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "- **Served fingerprint:** "))
		}
	}
	return ""
}

// itemBody returns the file below its snapshot header (the round-trip domain):
// everything after the blank line that follows the `**Context:**` header bullet.
func itemBody(content string) string {
	i := strings.Index(content, "**Context:**")
	if i < 0 {
		return content
	}
	rest := content[i:]
	j := strings.Index(rest, "\n\n")
	if j < 0 {
		return ""
	}
	return content[i+j+2:]
}

func patchKind(renderKind string) string {
	if renderKind == "epic" {
		return "epic"
	}
	return "requirement"
}

func buildPatchRecord(kind, ext, fp, ctxID string, p *diff.Patch) map[string]any {
	rec := map[string]any{
		"kind":                 patchKind(kind),
		"external_id":          ext,
		"expected_fingerprint": fp,
		"authoring_context_id": ctxID,
	}
	for k, v := range p.Fields {
		rec[k] = v
	}
	if p.CitationsSet {
		rec["source_citations"] = citationMaps(p.Citations)
	}
	if p.CriteriaSet {
		rec["criteria"] = p.Criteria
	}
	if len(p.Relations) > 0 {
		ops := make([]any, len(p.Relations))
		for i, r := range p.Relations {
			ops[i] = map[string]any{"mode": r.Mode, "parents": []any{r.Target}}
		}
		rec["relations"] = ops
	}
	if len(p.Members) > 0 {
		ops := make([]any, len(p.Members))
		for i, m := range p.Members {
			ops[i] = map[string]any{"mode": m.Mode, "member_external_ids": []any{m.Target}}
		}
		rec["members"] = ops
	}
	return rec
}

func postAuthor(env *factoryEnv, body map[string]any) (int, map[string]any, error) {
	body["system_id"] = env.SystemID
	body["actor"] = authorActor
	return env.call("POST", "/api/v1/sync/author", body)
}

func describePatch(ext string, p *diff.Patch) string {
	parts := []string{}
	if len(p.Fields) > 0 {
		keys := make([]string, 0, len(p.Fields))
		for k := range p.Fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts = append(parts, "fields "+strings.Join(keys, ","))
	}
	if p.CitationsSet {
		parts = append(parts, "source citations")
	}
	if p.CriteriaSet {
		parts = append(parts, "criteria")
	}
	for _, r := range p.Relations {
		parts = append(parts, fmt.Sprintf("relation %s %s", r.Mode, r.Target))
	}
	for _, m := range p.Members {
		parts = append(parts, fmt.Sprintf("member %s %s", m.Mode, m.Target))
	}
	return fmt.Sprintf("patch %s: %s", ext, strings.Join(parts, "; "))
}

// printOutOfScopeSkips names each members/ file push left alone because the
// frozen selection does not hold its record. The scope's served membership
// may hold it (a member declared after the selection was taken), so the line
// says which, and never that a served member is not one.
func printOutOfScopeSkips(scopeExt string, ids, served []string) {
	isServed := map[string]bool{}
	for _, m := range served {
		isServed[m] = true
	}
	for _, id := range ids {
		what := id + " is not in the frozen selection"
		if isServed[id] {
			what = id + " is a member of " + scopeExt + " but not in the frozen selection"
		}
		fmt.Printf("push: skipped members/%s.md — %s; re-select %s with it among `--members` (`working-set select`), then pull the scope, to push it\n", id, what, scopeExt)
	}
}

func printPlan(plan, skipped []string, dryRun bool) {
	if len(plan) == 0 && len(skipped) == 0 {
		printSuccess("push: nothing to do — every file is unchanged")
		return
	}
	// Only unfilled stubs and nothing to push: name them, and say so — not "every
	// file is unchanged", which would hide that a stub was silently left behind
	// (REQ-CROSS-331). A plan of strings cannot tell a skip from an op, so the
	// skip lines and their count are carried separately.
	if len(plan) == 0 {
		for _, l := range skipped {
			fmt.Println("  " + l)
		}
		fmt.Printf("push: nothing to push — %d unfilled packet stub(s) skipped\n", len(skipped))
		return
	}
	head := "push op plan"
	if dryRun {
		head = "push --dry-run op plan (nothing applied)"
	}
	fmt.Println(head + ":")
	for _, l := range plan {
		fmt.Println("  " + l)
	}
	for _, l := range skipped {
		fmt.Println("  " + l)
	}
}
