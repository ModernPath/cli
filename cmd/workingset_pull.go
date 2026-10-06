package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// workingSetPullScope resolves the current work selection and materializes its
// scope as an editable directory in the authoring render (SR-CLI-0084). Item
// and section files are editable (read-only under --for-review); the selection
// and findings are projections. A scoped refresh checks CLI-written local
// content baselines before replacing authored files; server CAS remains separate.
func workingSetPullScope(env *factoryEnv, forReview bool, now time.Time) error {
	return workingSetPullScopeSince(env, forReview, "", now)
}

// workingSetPullScopeSince is the scope pull; with since (a previous
// cold-review trace, REQ-CROSS-464) a review pull writes the delta bundle.
func workingSetPullScopeSince(env *factoryEnv, forReview bool, since string, now time.Time) error {
	if forReview {
		return workingSetPullReviewSnapshotSince(env, since, now)
	}
	payload, err := fetchWorkSelection(env)
	if err != nil {
		return err
	}
	current, _ := payload["current"].(map[string]any)
	if current == nil {
		return fmt.Errorf("no current work selection — `working-set select` a scope first")
	}
	scopeExt := str(current, "scope_external_id")
	if scopeExt == "" {
		return fmt.Errorf("the current selection names no scope")
	}
	// #1 (external review): a served identifier becomes a path component only after
	// it is proven a single safe component — never let `../` or a separator escape
	// the working-set directory (unsafeSnapshotName already guards single-item pulls).
	if unsafeSnapshotName(scopeExt) {
		return fmt.Errorf("the selection's scope id %q is not a safe path component — refusing to pull", scopeExt)
	}
	scopeKind := str(current, "scope_kind")
	if scopeKind != "epic" && scopeKind != "single_sr" {
		return fmt.Errorf("the current selection has unsupported scope kind %q — refusing to pull", scopeKind)
	}
	members, err := selectionMembers(current["members"])
	if err != nil {
		return err
	}
	for _, mid := range members {
		if unsafeSnapshotName(mid) {
			return fmt.Errorf("selection member id %q is not a safe path component — refusing to pull", mid)
		}
	}
	idx, err := scopeIndex(env, append([]string{scopeExt}, members...))
	if err != nil {
		return err
	}

	mode := "authoring"
	ctxID := newContextID(mode)

	dir := filepath.Join(env.Root, workingSetDir, scopeExt)
	plan := scopedPullPlan{files: map[string]scopedPullFile{}}
	if sr, ok := idx[scopeExt]; ok {
		content := scopeItemContent(recordFromPayload(sr, members), mode, ctxID, false, env, now)
		plan.add(scopeExt+".md", []byte(content), "item")
	}

	for _, mid := range members {
		mr, ok := idx[mid]
		if !ok {
			continue // the selection may name a member the read has not caught up to
		}
		content := scopeItemContent(recordFromPayload(mr, nil), mode, ctxID, false, env, now)
		plan.add(filepath.Join("members", mid+".md"), []byte(content), "item")
	}

	// The scope directory's SELECTION.md is a projection for the authoring/review
	// context, not the preflight snapshot; the active-release source line rides
	// the top-level WORK-SELECTION.md (pullSelection), so pass nil here.
	plan.add("SELECTION.md", []byte(renderSelectionBody(payload, nil)), "selection")

	// REQ-CROSS-383 (EPIC-CLI-018): scaffold the keys the server's plan check
	// requires — one denominator for the scaffold and the check. A server that
	// serves no facts falls back to the frozen-list derivation, named as such.
	var required []string
	required, err = requiredSectionKeys(env, scopeExt, scopeKind, members, idx)
	if err != nil {
		return err
	}
	packet, err := stagePacketSections(env, dir, scopeKind, scopeExt, true, required)
	if err != nil {
		return err
	}
	plan.addPacket(packet)

	plan.add(filepath.Join("findings", "COLD-REVIEW.md"),
		[]byte(renderFindingsProjection(env, scopeKind, scopeExt)), "findings")

	stamp := fmt.Sprintf("# working-set context\n\nmode: %s\ncontext_id: %s\nscope: %s:%s\npulled_at: %s\n",
		mode, ctxID, scopeKind, scopeExt, now.Format(time.RFC3339))
	plan.add(contextFile, []byte(stamp), "context")
	if err := applyScopedPullPlan(dir, plan); err != nil {
		return err
	}
	// A narrow review bundle belongs to the previous review context.
	if err := os.Remove(filepath.Join(dir, reviewBundleFile)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("authoring files were refreshed, but the old review bundle could not be removed: %w", err)
	}

	printSuccess("pulled scope %s → %s (%s context %s)", scopeExt, filepath.Join(workingSetDir, scopeExt), mode, ctxID)
	return nil
}

func workingSetPull(env *factoryEnv, ids []string, now time.Time) error {
	var items []string
	wantSelection := false
	for _, id := range ids {
		// REQ-CROSS-430: the selection is addressed by the literal target and by
		// the file name it is written to, so the two spellings agree.
		if id == selectionTarget || id == selectionFile || id == strings.TrimSuffix(selectionFile, ".md") {
			wantSelection = true
			continue
		}
		items = append(items, id)
	}
	// REQ-CROSS-430: --piece narrows the caller-scoped reads (pull --scope, push,
	// check); direct by-id pulls do not use a caller-scoped selection, so the flag is refused
	// before any request rather than parsed and ignored.
	if wsPiece != "" && len(items) > 0 {
		return fmt.Errorf("--piece applies to pull --scope, push and check; a by-id pull does not use a selected piece — drop --piece")
	}

	var unknown, conflicts, refused []string
	if wantSelection {
		_, conflict, err := pullSelection(env, now, nil)
		if err != nil {
			return err
		}
		if conflict {
			conflicts = append(conflicts, selectionTarget)
			fmt.Printf("✗ CONFLICT %s — local edit preserved; fresh pull at %s.pulled; resolve by hand and re-pull\n",
				selectionFile, filepath.Join(workingSetDir, selectionFile))
		} else {
			printSuccess("pulled selection → %s", filepath.Join(workingSetDir, selectionFile))
		}
		if len(items) == 0 {
			if len(conflicts) > 0 {
				return fmt.Errorf("pull incomplete — conflicts: %s", strings.Join(conflicts, ", "))
			}
			return nil
		}
	}
	safeItems := make([]string, 0, len(items))
	for _, id := range items {
		if unsafeSnapshotName(id + ".md") {
			refused = append(refused, id)
			fmt.Printf("✗ %s — refused: an external id must be a plain file name, not a path\n", id)
			continue
		}
		safeItems = append(safeItems, id)
	}
	items = safeItems

	index, err := fetchDirectItems(env, items, workingSetIncludeCandidates, true)
	if err != nil {
		return err
	}
	for _, id := range items {
		item, ok := index[id]
		if !ok {
			unknown = append(unknown, id)
			fmt.Printf("✗ %s — not served by %s (unknown external id)\n", id, env.APIURL)
			continue
		}
		conflict, err := writeWorkingSetItem(env, item, item.gates, now)
		if err != nil {
			return err
		}
		if conflict {
			conflicts = append(conflicts, id)
			fmt.Printf("✗ CONFLICT %s — local edit preserved; fresh pull at %s.md.pulled; resolve by hand and re-pull\n",
				id, filepath.Join(workingSetDir, id))
			continue
		}
		printSuccess("pulled %s → %s", id, filepath.Join(workingSetDir, id+".md"))
	}
	var problems []string
	if len(unknown) > 0 {
		problems = append(problems, "unresolvable: "+strings.Join(unknown, ", "))
	}
	if len(conflicts) > 0 {
		problems = append(problems, "conflicts: "+strings.Join(conflicts, ", "))
	}
	if len(refused) > 0 {
		problems = append(problems, "refused (path-escaping id): "+strings.Join(refused, ", "))
	}
	if len(problems) > 0 {
		return fmt.Errorf("pull incomplete — %s", strings.Join(problems, "; "))
	}
	return nil
}
