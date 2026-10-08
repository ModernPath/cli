package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// workingSetPullReviewSnapshot brackets the item and packet reads with exact
// scope aggregate reads. The store does not provide a transaction token, so
// this detects read drift across the bounded fetch sequence without claiming
// global transactional consistency. Findings remain an informational read.
func workingSetPullReviewSnapshot(env *factoryEnv, now time.Time) error {
	return workingSetPullReviewSnapshotSince(env, "", now)
}

func equalStringLists(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func reviewAggregate(env *factoryEnv, scopeExt string) (*deliveryContextResponse, error) {
	resp, err := readDeliveryContextFor(env, scopeExt)
	if err != nil {
		return nil, fmt.Errorf("could not read review aggregate for %s: %w", scopeExt, err)
	}
	if resp.Data.Facts == nil {
		return nil, fmt.Errorf("review aggregate facts are unavailable for %s — refusing to create a review snapshot", scopeExt)
	}
	decoded, decodeErr := hex.DecodeString(resp.Data.PacketFingerprint)
	if decodeErr != nil || len(decoded) != sha256.Size || resp.Data.PacketFingerprint != strings.ToLower(resp.Data.PacketFingerprint) {
		return nil, fmt.Errorf("review aggregate for %s is not a full 64-character lowercase SHA-256 fingerprint — refusing to create a review snapshot", scopeExt)
	}
	if resp.Data.Facts.Aggregate != resp.Data.PacketFingerprint {
		return nil, fmt.Errorf("review aggregate and delivery facts disagree for %s — refusing to create a review snapshot", scopeExt)
	}
	if resp.Data.Facts.Sections.Required == nil {
		return nil, fmt.Errorf("required section facts are unavailable for %s — refusing to create a review snapshot", scopeExt)
	}
	return resp, nil
}

func reviewItemIdentity(index map[string]scopeRecord) ([]byte, error) {
	identity := make(map[string]any, len(index))
	for id, item := range index {
		identity[id] = map[string]any{"kind": item.kind, "payload": item.payload}
	}
	return json.Marshal(identity)
}

func reviewPacketIdentity(packet stagedPacketSections) ([]byte, error) {
	identity := map[string]any{"absent": packet.absent, "fingerprints": packet.fingerprints}
	files := make(map[string]any, len(packet.files))
	for name, file := range packet.files {
		files[name] = map[string]any{"content": string(file.content), "origin": file.origin}
	}
	identity["files"] = files
	return json.Marshal(identity)
}

func reviewSnapshotDigest(manifest reviewSnapshotManifest) (string, error) {
	manifest.SnapshotDigest = ""
	raw, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	return sha256Hex(raw), nil
}

func selectionScope(payload map[string]any) (map[string]any, string, string, []string, error) {
	current, _ := payload["current"].(map[string]any)
	if current == nil {
		return nil, "", "", nil, fmt.Errorf("no current work selection — `working-set select` a scope first")
	}
	scopeExt := str(current, "scope_external_id")
	if scopeExt == "" || unsafeSnapshotName(scopeExt) {
		return nil, "", "", nil, fmt.Errorf("the selection has no safe scope id — refusing to pull")
	}
	scopeKind := str(current, "scope_kind")
	if scopeKind != "epic" && scopeKind != "single_sr" {
		return nil, "", "", nil, fmt.Errorf("the current selection has unsupported scope kind %q — refusing to pull", scopeKind)
	}
	members, err := selectionMembers(current["members"])
	if err != nil {
		return nil, "", "", nil, err
	}
	for _, member := range members {
		if unsafeSnapshotName(member) {
			return nil, "", "", nil, fmt.Errorf("selection member id %q is not a safe path component — refusing to pull", member)
		}
	}
	return current, scopeExt, scopeKind, members, nil
}

func writeReviewSnapshot(scopeRoot, ctxID string, plan scopedPullPlan) error {
	if err := os.MkdirAll(scopeRoot, 0o755); err != nil {
		return err
	}
	finalDir := filepath.Join(scopeRoot, ctxID)
	if _, err := os.Lstat(finalDir); err == nil {
		return fmt.Errorf("review snapshot %s already exists — refusing to replace an immutable snapshot", ctxID)
	} else if !os.IsNotExist(err) {
		return err
	}
	tempDir, err := os.MkdirTemp(scopeRoot, ".staging-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	for name, file := range plan.files {
		path := filepath.Join(tempDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, file.content, 0o444); err != nil {
			return err
		}
	}
	if err := os.Rename(tempDir, finalDir); err != nil {
		return fmt.Errorf("could not publish immutable review snapshot: %w", err)
	}
	printSuccess("pulled review snapshot %s → %s", ctxID, filepath.Join(reviewSnapshotDir, filepath.Base(scopeRoot), ctxID))
	return nil
}

type reviewSnapshotManifest struct {
	StoreURL             string            `json:"store_url"`
	SystemID             int               `json:"system_id"`
	Version              int               `json:"version"`
	ScopeKind            string            `json:"scope_kind"`
	ScopeExternalID      string            `json:"scope_external_id"`
	ContextID            string            `json:"context_id"`
	AggregateFingerprint string            `json:"aggregate_fingerprint"`
	SelectionFingerprint string            `json:"selection_fingerprint,omitempty"`
	ReconRevision        string            `json:"recon_revision,omitempty"`
	RequiredSections     []string          `json:"required_sections"`
	Files                map[string]string `json:"files"`
	SnapshotDigest       string            `json:"snapshot_digest"`
}

func workingSetPullReviewSnapshotSince(env *factoryEnv, since string, now time.Time) error {
	selectionBefore, err := fetchWorkSelection(env)
	if err != nil {
		return err
	}
	current, scopeExt, scopeKind, members, err := selectionScope(selectionBefore)
	if err != nil {
		return err
	}
	var prior map[string]any
	if since != "" {
		prior, err = readSinceTrace(env, since, scopeExt)
		if err != nil {
			return err
		}
	}
	aggregateBefore, err := reviewAggregate(env, scopeExt)
	if err != nil {
		return err
	}
	selectionMembersBefore := append([]string(nil), members...)
	dir := filepath.Join(env.Root, reviewSnapshotDir, scopeExt)
	itemsBefore, members, err := reviewScopeIndex(env, scopeExt, members)
	if err != nil {
		return err
	}
	packetBefore, err := stagePacketSections(env, dir, scopeKind, scopeExt, false, nil)
	if err != nil {
		return err
	}
	itemsAfter, servedMembersAfter, err := reviewScopeIndex(env, scopeExt, selectionMembersBefore)
	if err != nil {
		return err
	}
	packetAfter, err := stagePacketSections(env, dir, scopeKind, scopeExt, false, nil)
	if err != nil {
		return err
	}
	aggregateFacts, err := reviewAggregate(env, scopeExt)
	if err != nil {
		return err
	}
	selectionAfter, err := fetchWorkSelection(env)
	if err != nil {
		return err
	}
	currentAfter, scopeExtAfter, scopeKindAfter, membersAfter, err := selectionScope(selectionAfter)
	if err != nil {
		return err
	}
	aggregateAfter, err := reviewAggregate(env, scopeExt)
	if err != nil {
		return err
	}
	selectionBeforeBytes, _ := json.Marshal(current)
	selectionAfterBytes, _ := json.Marshal(currentAfter)
	itemsBeforeBytes, err := reviewItemIdentity(itemsBefore)
	if err != nil {
		return err
	}
	itemsAfterBytes, err := reviewItemIdentity(itemsAfter)
	if err != nil {
		return err
	}
	packetBeforeBytes, err := reviewPacketIdentity(packetBefore)
	if err != nil {
		return err
	}
	packetAfterBytes, err := reviewPacketIdentity(packetAfter)
	if err != nil {
		return err
	}
	requiredBefore, _ := json.Marshal(aggregateBefore.Data.Facts.Sections.Required)
	requiredFacts, _ := json.Marshal(aggregateFacts.Data.Facts.Sections.Required)
	requiredAfter, _ := json.Marshal(aggregateAfter.Data.Facts.Sections.Required)
	if scopeExt != scopeExtAfter || scopeKind != scopeKindAfter || !equalStringLists(selectionMembersBefore, membersAfter) || !equalStringLists(members, servedMembersAfter) || !bytes.Equal(selectionBeforeBytes, selectionAfterBytes) {
		return fmt.Errorf("work selection changed while reading %s — refusing to create a review snapshot", scopeExt)
	}
	if !bytes.Equal(itemsBeforeBytes, itemsAfterBytes) {
		return fmt.Errorf("selected items changed while reading %s — refusing to create a review snapshot", scopeExt)
	}
	if !bytes.Equal(packetBeforeBytes, packetAfterBytes) {
		return fmt.Errorf("packet sections changed while reading %s — refusing to create a review snapshot", scopeExt)
	}
	if aggregateBefore.Data.PacketFingerprint != aggregateFacts.Data.PacketFingerprint || aggregateBefore.Data.PacketFingerprint != aggregateAfter.Data.PacketFingerprint || !bytes.Equal(requiredBefore, requiredFacts) || !bytes.Equal(requiredBefore, requiredAfter) {
		return fmt.Errorf("aggregate or required section facts changed while reading %s — refusing to create a review snapshot", scopeExt)
	}

	ctxID := newContextID("review")
	plan := scopedPullPlan{files: map[string]scopedPullFile{}}
	if sr, ok := itemsBefore[scopeExt]; ok {
		content := scopeItemContent(reviewItemRecord(sr, members), "review", ctxID, true, env, now)
		plan.add(scopeExt+".md", []byte(content), "item")
	}
	for _, member := range members {
		if item, ok := itemsBefore[member]; ok {
			content := scopeItemContent(reviewItemRecord(item, nil), "review", ctxID, true, env, now)
			plan.add(filepath.Join("members", member+".md"), []byte(content), "item")
		}
	}
	plan.add("SELECTION.md", []byte(renderSelectionBody(selectionBefore, nil)), "selection")
	for name, file := range packetBefore.files {
		plan.add(filepath.Join("packet", name), file.content, file.origin)
	}
	if packetBefore.fingerprints != nil {
		fingerprints, _ := json.Marshal(packetBefore.fingerprints)
		plan.add(filepath.Join("packet", ".served-fingerprints.json"), fingerprints, "server-cas")
	}
	// Findings are informational and are not covered by Core's aggregate.
	plan.add(filepath.Join("findings", "COLD-REVIEW.md"), []byte(renderFindingsProjection(env, scopeKind, scopeExt)), "findings")
	reviewed := reviewedFingerprints(scopeExt, itemsBefore, members, packetBefore.sections)
	stamp := fmt.Sprintf("# working-set context\n\nmode: review\ncontext_id: %s\nscope: %s:%s\npulled_at: %s\naggregate: %s\n",
		ctxID, scopeKind, scopeExt, now.Format(time.RFC3339), aggregateBefore.Data.PacketFingerprint)
	for _, r := range reviewed {
		stamp += fmt.Sprintf("reviewed: %s %s\n", r.key, r.fingerprint)
	}
	plan.add(contextFile, []byte(stamp), "context")
	bundle := renderReviewBundle(scopeKind, scopeExt, aggregateBefore.Data.PacketFingerprint, ctxID, now, itemsBefore, members, packetBefore.sections)
	if prior != nil {
		bundle = renderDeltaReviewBundle(env, deltaInput{scopeKind: scopeKind, scopeExt: scopeExt, aggregate: aggregateBefore.Data.PacketFingerprint, ctxID: ctxID, now: now, idx: itemsBefore, members: members, sections: packetBefore.sections, reviewed: reviewed, prior: prior, head: gitHead(env.Root)})
	}
	plan.add(reviewBundleFile, []byte(bundle), "review")

	fileDigests := make(map[string]string, len(plan.files))
	for name, file := range plan.files {
		fileDigests[name] = sha256Hex(file.content)
	}
	manifest := reviewSnapshotManifest{
		StoreURL: env.APIURL, SystemID: env.SystemID,
		Version: 1, ScopeKind: scopeKind, ScopeExternalID: scopeExt, ContextID: ctxID,
		AggregateFingerprint: aggregateBefore.Data.PacketFingerprint,
		SelectionFingerprint: str(current, "fingerprint"), ReconRevision: str(current, "recon_revision"),
		RequiredSections: append([]string(nil), aggregateBefore.Data.Facts.Sections.Required...),
		Files:            fileDigests,
	}
	manifest.SnapshotDigest, err = reviewSnapshotDigest(manifest)
	if err != nil {
		return err
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	plan.add("MANIFEST.json", append(manifestBytes, '\n'), "manifest")
	return writeReviewSnapshot(dir, ctxID, plan)
}
