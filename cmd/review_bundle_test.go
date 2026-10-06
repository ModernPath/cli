package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-CROSS-449 (EPIC-CLI-TURNS): a review pull writes one REVIEW.md holding
// the whole packet — the aggregate header, the epic, the sections, the URs
// with scenarios and the SRs with statement, rationale, boundary and
// verification method — each under an `id · fingerprint` heading, the members
// taken from the epic's served membership, and stamps the aggregate in
// .context. An authoring pull writes neither.

func reviewBundleFixture() *wsFixture {
	sel := scopeSelection()
	current := sel["current"].(map[string]any)
	current["scope_external_id"] = "EPIC-B"
	current["members"] = []any{} // the frozen list is empty; the epic holds three
	return &wsFixture{
		workSelection:   sel,
		deliveryContext: map[string]any{"packet_fingerprint": strings.Repeat("a", 64), "facts": map[string]any{"aggregate": strings.Repeat("a", 64), "sections": map[string]any{"required": []any{}}}},
		epics: []any{map[string]any{
			"external_id": "EPIC-B", "title": "The bundle epic", "description": "one read for the reviewer",
			"process_status": "PLANNED", "fingerprint": "epic-b-fp",
			"requirement_external_ids":      []any{"REQ-B-1", "REQ-B-2"},
			"user_requirement_external_ids": []any{"UR-B-1"},
		}},
		userRequirements: []any{map[string]any{
			"external_id": "UR-B-1", "title": "Reviewer reads once", "description": "the reviewer reads the packet in one read",
			"work_status": "PROPOSED", "fingerprint": "ur-b-fp",
			"scenarios": []any{map[string]any{"external_id": "AS-1", "kind": "scenario",
				"given": "a pulled scope", "when": "the reviewer starts", "then": "one file holds the packet"}},
		}},
		requirements: []any{
			map[string]any{"external_id": "REQ-B-1", "title": "Bundle", "work_status": "PROPOSED", "fingerprint": "sr-b1-fp",
				"description": "the pull writes REVIEW.md", "rationale": "twenty reads before any code",
				"boundary": "cmd/workingset.go only", "verification_method": "Go tests on the bundle"},
			map[string]any{"external_id": "REQ-B-2", "title": "Stamp", "work_status": "PROPOSED", "fingerprint": "sr-b2-fp",
				"description": "the pull stamps the aggregate", "rationale": "drift is refused",
				"boundary": "the .context file", "verification_method": "Go tests on the stamp"},
		},
		packetSections: []any{
			map[string]any{"section_key": "decisions", "content": "DC-1 the decisions body", "content_fingerprint": "ps-dec-fp"},
			map[string]any{"section_key": "enrichment:REQ-B-1", "content": "the enrichment body", "content_fingerprint": "ps-enr-fp"},
			map[string]any{"section_key": "reconnaissance", "content": "the recon body", "content_fingerprint": "ps-recon-fp"},
		},
	}
}

func TestREQCROSS449ReviewPullWritesTheBundleInOrder(t *testing.T) {
	fx := reviewBundleFixture()
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPullScope(env, true, wsNow); err != nil {
		t.Fatalf("pull --scope --for-review: %v", err)
	}
	dir := latestReviewTestDirectory(t, env.Root, "EPIC-B")
	bundle := readScopeFile(t, filepath.Join(dir, "REVIEW.md"))

	header := bundle
	if i := strings.Index(bundle, "EPIC-B · epic-b-fp"); i > 0 {
		header = bundle[:i]
	}
	if !strings.Contains(header, "epic:EPIC-B") || !strings.Contains(header, strings.Repeat("a", 64)) {
		t.Errorf("the header names the scope and the aggregate the pull saw:\n%s", header)
	}
	order := []string{
		"EPIC-B · epic-b-fp", "one read for the reviewer",
		"reconnaissance · ps-recon-fp", "the recon body",
		"enrichment:REQ-B-1 · ps-enr-fp", "the enrichment body",
		"decisions · ps-dec-fp", "DC-1 the decisions body",
		"UR-B-1 · ur-b-fp", "the reviewer reads the packet in one read", "GIVEN a pulled scope",
		"REQ-B-1 · sr-b1-fp", "the pull writes REVIEW.md", "twenty reads before any code", "cmd/workingset.go only", "Go tests on the bundle",
		"REQ-B-2 · sr-b2-fp", "the pull stamps the aggregate", "drift is refused", "the .context file", "Go tests on the stamp",
	}
	at := 0
	for _, want := range order {
		i := strings.Index(bundle[at:], want)
		if i < 0 {
			t.Fatalf("REVIEW.md must hold %q after position %d, in order:\n%s", want, at, bundle)
		}
		at += i + len(want)
	}
	for _, label := range []string{"Rationale", "Boundary", "Verification method"} {
		if !strings.Contains(bundle, label) {
			t.Errorf("each SR names its %s:\n%s", label, bundle)
		}
	}

	stamp := readScopeFile(t, filepath.Join(dir, contextFile))
	if !strings.Contains(stamp, "mode: review") || !strings.Contains(stamp, "aggregate: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") {
		t.Errorf(".context carries the aggregate the review pull saw:\n%s", stamp)
	}
	// The per-file layout stays, now with the served members.
	if _, err := os.Stat(filepath.Join(dir, "members", "REQ-B-1.md")); err != nil {
		t.Errorf("the member files are still written: %v", err)
	}
}

func TestREQCROSS449AuthoringPullWritesNoBundleAndNoAggregate(t *testing.T) {
	fx := reviewBundleFixture()
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull --scope: %v", err)
	}
	dir := filepath.Join(env.Root, workingSetDir, "EPIC-B")
	if _, err := os.Stat(filepath.Join(dir, "REVIEW.md")); err == nil {
		t.Errorf("an authoring pull writes no REVIEW.md")
	}
	if stamp := readScopeFile(t, filepath.Join(dir, contextFile)); strings.Contains(stamp, "aggregate:") {
		t.Errorf("an authoring pull stamps no aggregate:\n%s", stamp)
	}
}

// REQ-CROSS-461 (EPIC-CLI-EDGES): the bundle shows each requirement's statement
// sources — the served citations, "—" for an empty list, the not-served marker
// without the key — for every member and for a requirement scope record. The
// epic block is unchanged.
func TestREQCROSS461BundleShowsEachRequirementsSources(t *testing.T) {
	fx := reviewBundleFixture()
	epic := fx.epics[0].(map[string]any)
	epic["requirement_external_ids"] = []any{"REQ-B-1", "REQ-B-2", "REQ-B-3"}
	epic["source_citations"] = []any{map[string]any{"kind": "user", "ref": "USER:epic-src"}}
	fx.userRequirements[0].(map[string]any)["source_citations"] = []any{
		map[string]any{"kind": "user", "ref": "USER:2026-09-28:ur"},
	}
	fx.requirements[0].(map[string]any)["source_citations"] = []any{
		map[string]any{"kind": "user", "ref": "USER:2026-09-28:b1"},
		map[string]any{"kind": "code", "ref": "cmd/review_bundle.go:140"},
	}
	fx.requirements[1].(map[string]any)["source_citations"] = []any{}
	fx.requirements = append(fx.requirements, map[string]any{
		"external_id": "REQ-B-3", "title": "Unsourced", "work_status": "PROPOSED", "fingerprint": "sr-b3-fp",
		"description": "a record served without the key",
	})
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPullScope(env, true, wsNow); err != nil {
		t.Fatalf("pull --scope --for-review: %v", err)
	}
	bundle := readScopeFile(t, filepath.Join(latestReviewTestDirectory(t, env.Root, "EPIC-B"), "REVIEW.md"))

	block := func(from, to string) string {
		i := strings.Index(bundle, from)
		if i < 0 {
			t.Fatalf("REVIEW.md has no %q:\n%s", from, bundle)
		}
		rest := bundle[i:]
		if to != "" {
			if j := strings.Index(rest, to); j > 0 {
				rest = rest[:j]
			}
		}
		return rest
	}
	for _, c := range []struct{ from, to, want string }{
		{"UR-B-1 · ur-b-fp", "## System requirements", "- **Sources:** user: USER:2026-09-28:ur"},
		{"REQ-B-1 · sr-b1-fp", "REQ-B-2 ·", "- **Sources:** user: USER:2026-09-28:b1 · code: cmd/review_bundle.go:140"},
		{"REQ-B-2 · sr-b2-fp", "REQ-B-3 ·", "- **Sources:** —"},
		{"REQ-B-3 · sr-b3-fp", "", "- **Sources:** " + notServed},
	} {
		if b := block(c.from, c.to); !strings.Contains(b, c.want) {
			t.Errorf("under %s the bundle must show %q:\n%s", c.from, c.want, b)
		}
	}
	if epicBlock := block("## Epic", "## Packet sections"); strings.Contains(epicBlock, "Sources") {
		t.Errorf("the epic block is unchanged — no Sources line:\n%s", epicBlock)
	}
}

// REQ-CROSS-471 (EPIC-SEARCH-CONTEXT): an epic created from a chat carries what
// the chat found in its shared_context; the review bundle and the scope pull's
// epic file list those found references by kind and name, once, and a record
// without them shows no such list.
func TestREQCROSS471BundleAndScopePullListTheEpicsFoundReferences(t *testing.T) {
	fx := reviewBundleFixture()
	epic := fx.epics[0].(map[string]any)
	epic["shared_context"] = map[string]any{
		"version": 1, "accepted_context_revision": 3,
		"decisions": []any{}, "assumptions": []any{}, "open_questions": []any{},
		"references": []any{
			map[string]any{"kind": "system", "system_id": 4},
			map[string]any{"kind": "found", "ref_kind": "pattern", "target_ref": "pattern:7", "name": "Idempotent Retry"},
			map[string]any{"kind": "found", "ref_kind": "file", "target_ref": "file:4:lib/billing.ex", "name": "lib/billing.ex"},
		},
	}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPullScope(env, true, wsNow); err != nil {
		t.Fatalf("pull --scope --for-review: %v", err)
	}
	dir := latestReviewTestDirectory(t, env.Root, "EPIC-B")
	bundle := readScopeFile(t, filepath.Join(dir, "REVIEW.md"))

	epicBlock := bundle
	if i := strings.Index(bundle, "## Packet sections"); i > 0 {
		epicBlock = bundle[:i]
	}
	for _, want := range []string{"**Found in chat.**", "- pattern · Idempotent Retry", "- file · lib/billing.ex"} {
		if !strings.Contains(epicBlock, want) {
			t.Errorf("the bundle's epic block must list %q:\n%s", want, epicBlock)
		}
	}
	if n := strings.Count(bundle, "Idempotent Retry"); n != 1 {
		t.Errorf("each found reference is listed once, got %d:\n%s", n, bundle)
	}

	epicFile := readScopeFile(t, filepath.Join(dir, "EPIC-B.md"))
	if !strings.Contains(epicFile, "## found_in_chat") || !strings.Contains(epicFile, "- pattern · Idempotent Retry") {
		t.Errorf("the scope pull's epic file lists what the chat found:\n%s", epicFile)
	}

	plain := reviewBundleFixture()
	env = wsEnv(t, wsServe(t, plain))
	if err := workingSetPullScope(env, true, wsNow); err != nil {
		t.Fatalf("pull --scope --for-review: %v", err)
	}
	if b := readScopeFile(t, filepath.Join(latestReviewTestDirectory(t, env.Root, "EPIC-B"), "REVIEW.md")); strings.Contains(b, "Found in chat") {
		t.Errorf("an epic without found references shows no list:\n%s", b)
	}
}

func TestREQCROSS461SingleSRScopeBundleShowsTheScopeRecordsSources(t *testing.T) {
	sel := scopeSelection()
	cur := sel["current"].(map[string]any)
	cur["scope_kind"] = "single_sr"
	cur["scope_external_id"] = "REQ-CROSS-310"
	cur["members"] = []any{}
	fx := &wsFixture{
		workSelection:   sel,
		deliveryContext: reviewDeliveryContext(strings.Repeat("a", 64)),
		requirements: []any{map[string]any{
			"external_id": "REQ-CROSS-310", "title": "sr", "context": "CROSS",
			"work_status": "PROPOSED", "fingerprint": "sr310-fp", "description": "the single SR",
			"source_citations": []any{map[string]any{"kind": "user", "ref": "USER:2026-09-28:single"}},
		}},
	}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, true, wsNow); err != nil {
		t.Fatalf("pull --scope --for-review: %v", err)
	}
	bundle := readScopeFile(t, filepath.Join(latestReviewTestDirectory(t, env.Root, "REQ-CROSS-310"), "REVIEW.md"))
	if !strings.Contains(bundle, "- **Sources:** user: USER:2026-09-28:single") {
		t.Fatalf("a single-SR scope record shows its sources:\n%s", bundle)
	}
}

// REQ-CROSS-464 (EPIC-CLI-DELTA): a review pull stamps each record's and
// packet section's fingerprint as `reviewed: <key> <fingerprint>` lines, and
// `--since <trace>` renders a delta bundle against the fingerprints that trace
// recorded: the header names the trace's revision beside HEAD, the changed
// items are shown in full, then the open findings, the previous verdict and
// the unchanged ids with fingerprints only.

func TestREQCROSS464ReviewPullStampsEachReviewedFingerprint(t *testing.T) {
	fx := reviewBundleFixture()
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, true, wsNow); err != nil {
		t.Fatalf("pull --scope --for-review: %v", err)
	}
	dir := latestReviewTestDirectory(t, env.Root, "EPIC-B")
	stamp := readScopeFile(t, filepath.Join(dir, contextFile))
	for _, want := range []string{
		"reviewed: EPIC-B epic-b-fp", "reviewed: UR-B-1 ur-b-fp", "reviewed: REQ-B-1 sr-b1-fp", "reviewed: REQ-B-2 sr-b2-fp",
		"reviewed: reconnaissance ps-recon-fp", "reviewed: enrichment:REQ-B-1 ps-enr-fp", "reviewed: decisions ps-dec-fp",
	} {
		if !strings.Contains(stamp, want+"\n") {
			t.Errorf("the stamp carries %q:\n%s", want, stamp)
		}
	}
	// The existing readers match by their own prefixes and ignore the new lines.
	if mode, ctx := readContextStamp(dir); mode != "review" || !strings.HasPrefix(ctx, "review-") {
		t.Errorf("readContextStamp still parses: %q %q", mode, ctx)
	}
	if agg := readContextAggregate(dir); agg != strings.Repeat("a", 64) {
		t.Errorf("readContextAggregate still parses the full aggregate: %q", agg)
	}
	if kind := stampedScopeKind(dir); kind != "epic" {
		t.Errorf("stampedScopeKind still parses: %q", kind)
	}
	if scope := readStampField(dir, "scope"); scope != "epic:EPIC-B" {
		t.Errorf("readStampField still parses: %q", scope)
	}
}

// sinceWorkspace binds a cobra workspace to the fixture, makes it a git
// repository with one commit, and returns its HEAD.
func sinceWorkspace(t *testing.T, fx *wsFixture) (string, string) {
	t.Helper()
	cobraWorkspace(t, wsServe(t, fx))
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, gitCommitAll(t, root, "init")
}

// priorTrace is a recorded cold review of EPIC-B that read REQ-B-1 at an older
// fingerprint and everything else as the fixture serves it now.
func priorTrace(revision string) map[string]any {
	return map[string]any{
		"external_id": "CR-TRACE-EPIC-B-1", "purpose": "cold-review", "gate_class": "trace", "kind": "trace",
		"state": "fail", "verdict": "FAIL", "body_md": "the prior round's verdict body",
		"exact_scope": []any{"EPIC-B", "REQ-B-1", "REQ-B-2", "UR-B-1"}, "application_revision": revision,
		"reviewed_fingerprints": map[string]any{
			"EPIC-B": "epic-b-fp", "UR-B-1": "ur-b-fp", "REQ-B-1": "sr-b1-OLD", "REQ-B-2": "sr-b2-fp",
			"reconnaissance": "ps-recon-fp", "enrichment:REQ-B-1": "ps-enr-fp", "decisions": "ps-dec-fp",
		},
	}
}

func between(s, from, to string) string {
	i := strings.Index(s, from)
	if i < 0 {
		return ""
	}
	rest := s[i+len(from):]
	if j := strings.Index(rest, to); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

func TestREQCROSS464SincePullRendersTheDeltaBundle(t *testing.T) {
	fx := reviewBundleFixture()
	oldRev := strings.Repeat("1", 40)
	fx.gate = priorTrace(oldRev)
	fx.findings = []any{
		map[string]any{"external_id": "F-B-1", "category": "correctness", "disposition": "OPEN", "body": "the open finding body"},
		map[string]any{"external_id": "F-B-2", "category": "contract", "disposition": "RESOLVED", "body": "the resolved finding body"},
	}
	root, head := sinceWorkspace(t, fx)

	out, err := runRoot(t, "working-set", "pull", "--scope", "--for-review", "--since", "CR-TRACE-EPIC-B-1")
	if err != nil {
		t.Fatalf("pull --since: %v\n%s", err, out)
	}
	dir := latestReviewTestDirectory(t, root, "EPIC-B")
	bundle := readScopeFile(t, filepath.Join(dir, "REVIEW.md"))

	cut := strings.Index(bundle, "## ")
	if cut < 0 {
		t.Fatalf("REVIEW.md has no sections:\n%s", bundle)
	}
	header := bundle[:cut]
	for _, want := range []string{"CR-TRACE-EPIC-B-1", oldRev, head, "re-verify", strings.Repeat("a", 64)} {
		if !strings.Contains(header, want) {
			t.Errorf("the header names %q (the trace, its revision, HEAD, the re-verify note, the full aggregate):\n%s", want, header)
		}
	}
	changed := between(bundle, "## What changed since the last review", "\n## ")
	for _, want := range []string{"REQ-B-1", "sr-b1-OLD", "sr-b1-fp", "the pull writes REVIEW.md", "cmd/workingset.go only", "Go tests on the bundle"} {
		if !strings.Contains(changed, want) {
			t.Errorf("the changed SR is shown in full with its old and new fingerprints (%q missing):\n%s", want, changed)
		}
	}
	unchanged := between(bundle, "## Unchanged since the last review", "\n## ")
	for _, want := range []string{"EPIC-B · epic-b-fp", "UR-B-1 · ur-b-fp", "REQ-B-2 · sr-b2-fp", "reconnaissance · ps-recon-fp", "decisions · ps-dec-fp"} {
		if !strings.Contains(unchanged, want) {
			t.Errorf("the unchanged items are listed as id and fingerprint (%q missing):\n%s", want, unchanged)
		}
	}
	for _, absent := range []string{"the pull stamps the aggregate", "the recon body", "DC-1 the decisions body", "the resolved finding body"} {
		if strings.Contains(bundle, absent) {
			t.Errorf("the delta bundle does not render %q:\n%s", absent, bundle)
		}
	}
	if open := between(bundle, "## Open findings", "\n## "); !strings.Contains(open, "F-B-1") || !strings.Contains(open, "the open finding body") {
		t.Errorf("the open findings are listed:\n%s", bundle)
	}
	if prev := between(bundle, "## Previous verdict", "\n## "); !strings.Contains(prev, "FAIL") || !strings.Contains(prev, "the prior round's verdict body") {
		t.Errorf("the previous verdict is shown:\n%s", bundle)
	}
	if agg := readContextAggregate(dir); agg != strings.Repeat("a", 64) {
		t.Errorf("the stamp aggregate stays the full current one, got %q", agg)
	}
}

func TestREQCROSS464SincePullAtHEADAsksNoReVerify(t *testing.T) {
	fx := reviewBundleFixture()
	root, head := sinceWorkspace(t, fx)
	fx.gate = priorTrace(head)
	out, err := runRoot(t, "working-set", "pull", "--scope", "--for-review", "--since", "CR-TRACE-EPIC-B-1")
	if err != nil {
		t.Fatalf("pull --since: %v\n%s", err, out)
	}
	bundle := readScopeFile(t, filepath.Join(latestReviewTestDirectory(t, root, "EPIC-B"), "REVIEW.md"))
	if !strings.Contains(bundle, "What changed since the last review") {
		t.Fatalf("a delta bundle is written:\n%s", bundle)
	}
	if strings.Contains(bundle, "re-verify") {
		t.Errorf("the revision did not move, so no re-verify note:\n%s", bundle)
	}
}

func TestREQCROSS464SinceIsRefused(t *testing.T) {
	noMap := priorTrace("r")
	delete(noMap, "reviewed_fingerprints")
	entry := priorTrace("r")
	entry["purpose"] = "entry"
	other := priorTrace("r")
	other["exact_scope"] = []any{"EPIC-OTHER", "REQ-O-1"}
	for _, tc := range []struct {
		name  string
		trace map[string]any
		args  []string
		want  string
	}{
		{"a trace without reviewed fingerprints", noMap, nil, "reviewed fingerprints"},
		{"a trace that is not a cold review", entry, nil, "not a cold review"},
		{"another scope's trace", other, nil, "EPIC-OTHER"},
		{"the by-id narrow form", priorTrace("r"), []string{"working-set", "pull", "REQ-B-1", "--for-review", "--since", "CR-TRACE-EPIC-B-1"}, "narrow"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := reviewBundleFixture()
			fx.gate = tc.trace
			root, _ := sinceWorkspace(t, fx)
			args := tc.args
			if args == nil {
				args = []string{"working-set", "pull", "--scope", "--for-review", "--since", "CR-TRACE-EPIC-B-1"}
			}
			out, err := runRoot(t, args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refused naming %q, got %v\n%s", tc.want, err, out)
			}
			if _, statErr := os.Stat(filepath.Join(root, reviewSnapshotDir, "EPIC-B")); statErr == nil {
				t.Errorf("a refused --since pull writes no REVIEW.md")
			}
		})
	}
}

func latestReviewTestDirectory(t *testing.T, root, scope string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, reviewSnapshotDir, scope))
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one review snapshot for %s: %v %v", scope, entries, err)
	}
	return filepath.Join(root, reviewSnapshotDir, scope, entries[0].Name())
}
