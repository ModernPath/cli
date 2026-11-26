package kit

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Epic 350 (UR-816; SR-5035 to SR-5039): the kit ships the PR review pass
// rdd-pr-review and its read-only reviewer rdd-pr-reviewer. The pass reviews
// a pull request against the Epics it delivers and the System's Active
// Engineering Constraints, returns the review in chat and writes nothing to
// the store. Phrases are compared on folded text with backticks removed, as
// the other skill tests compare them (buildRecordText).

const (
	rddPrReviewAsset     = "assets/rdd/skills/rdd-pr-review/SKILL.md"
	rddPrReviewerAsset   = "assets/agents/rdd-pr-reviewer.md"
	rddPrReviewerCodex   = "assets/agents-codex/rdd-pr-reviewer.toml"
	rddPrReviewerTarget  = ".claude/agents/rdd-pr-reviewer.md"
	rddPrReviewerCodexAt = ".codex/agents/rdd-pr-reviewer.toml"
)

func rddPrReviewText(t *testing.T) string {
	t.Helper()
	return buildRecordText(t, rddPrReviewAsset)
}

func rddPrReviewerText(t *testing.T) string {
	t.Helper()
	return buildRecordText(t, rddPrReviewerAsset)
}

// SR-5035 criterion 0a55db15: the gh read, the reviewed revision and the git
// fallback with its base rule.
// TestCase 4f3af08f-456b-4433-aada-8eb18608f5c3
func TestRddPrReviewReadsThePullRequest(t *testing.T) {
	requireBuildRecordPhrases(t, rddPrReviewAsset, rddPrReviewText(t), []string{
		"Read base and head SHAs, commits, changed files, checks, description and review comments with gh, and report the base and head reviewed",
		"If the head changes during the review, say which revision was reviewed and reassess the affected conclusions",
		"Without gh (not installed or not signed in), say so, read the branch and its base with git (the base the person names, else the repository's default branch from git symbolic-ref refs/remotes/origin/HEAD, and say which), and mark checks, description and review comments unavailable in the limits",
	})
}

// SR-5035 criterion 3fd7a23b: the Epic reference forms, the refused id, the
// three store reads and the ranking of criteria, plan body and description.
// TestCase 0e64e53d-4c7c-4958-b800-f5b68a065382
func TestRddPrReviewReadsEachDeliveredEpic(t *testing.T) {
	requireBuildRecordPhrases(t, rddPrReviewAsset, rddPrReviewText(t), []string{
		"An Epic is named as Epic <number> or Epic-<number>, case-insensitive, in the title, description, branch name or commit subjects; a bare #<number> is a pull request or issue number",
		"One the description claims the pull request delivers is reviewed as delivered; a mention it does not claim is context only, said so in the limits",
		"An id the CLI refuses as not belonging to the bound System is reported and skipped, never substituted",
		"For each delivered Epic read epics get <id> --full --json, epics plan show <id> --full --json and epics gates review-bundle <id> --phase implementation --json",
		"Hold the change to the approved criteria and the Active constraints; read the plan body for its exclusions, decisions and change boundary; the pull request description is evidence only, never a replacement for them",
	})
}

// SR-5035 criterion 28b2dbed: several Epics on one pull request, and a pull
// request with no Epic.
// TestCase dc438a40-64c4-4f57-a9f6-08d4914ed4de
func TestRddPrReviewSeveralEpicsAndNoEpic(t *testing.T) {
	requireBuildRecordPhrases(t, rddPrReviewAsset, rddPrReviewText(t), []string{
		"Retain each bundle as a file and hand the reviewer its path, never the JSON inline",
		"Several Epics get one bundle and one intent assessment each, plus the branch as a whole: do the changes agree, and does the branch carry work no Epic names",
		"A pull request that names no Epic is reviewed as meta-work against the repository's stated purpose; say so rather than invent criteria",
	})
}

// SR-5035 criterion 986400c1: no store write, no record, posting only on
// request.
// TestCase e809c819-61da-4ad8-b6b9-34b5a3e78e21
func TestRddPrReviewWritesNothingAndPostsOnlyOnRequest(t *testing.T) {
	requireBuildRecordPhrases(t, rddPrReviewAsset, rddPrReviewText(t), []string{
		"It writes nothing to the store, records no review, pull request number or link, and changes no requirement or Epic state",
		"It posts to GitHub only when the person asks and gh is set up; the review is always returned in chat",
		"It satisfies no gate and is not merge authorization",
	})
}

// SR-5036 criterion f7bd8dad: the bundle field, the direct read with the
// current-revision rule, the four assessment words and the scope clause.
// TestCase 64625e61-4230-417b-9c89-97af1700f98f
func TestRddPrReviewAssessesEachActiveConstraint(t *testing.T) {
	requireBuildRecordPhrases(t, rddPrReviewAsset, rddPrReviewText(t), []string{
		"Each bundle carries them in engineering_constraints (id, ref, number, title, statement, rationale, verification_method, revision, accepted_at, accepted_by: the current revision's text)",
		"With no Epic, read constraints list --state active --json and constraints get <ref> --json; the text assessed is the current revision (current_revision), not the row text when a change is pending, cited with its revision number",
		"say constraint coverage is unverified or empty and continue; never claim the constraints passed without reading them",
		"satisfied, violated, not applicable with a reason, or unverified with the missing evidence; within the scope its statement gives",
	})
	requireBuildRecordPhrases(t, rddPrReviewerAsset, rddPrReviewerText(t), []string{
		"For each Active constraint record one assessment: satisfied, violated, not applicable with a reason, or unverified with the missing evidence, within the scope its statement gives",
	})
}

// SR-5036 criterion 2096ab18: the EC-<number> citation with revision; a
// constraint-only finding is information, not a verdict (D4).
// TestCase dabe495a-38b9-40da-af3a-862cbf2b34c6
func TestRddPrReviewCitesAConstraintAsInformation(t *testing.T) {
	requireBuildRecordPhrases(t, rddPrReviewAsset, rddPrReviewText(t), []string{
		"cites a constraint as EC-<number> with the revision read, and treats a constraint-only finding as information, not a verdict, that blocks no gate",
	})
	requireBuildRecordPhrases(t, rddPrReviewerAsset, rddPrReviewerText(t), []string{
		"A finding that concerns a constraint cites it as EC-<number> with the revision read and says what the change does against it; a finding whose only ground is a constraint is information, not a verdict, and blocks no gate",
	})
}

// SR-5037 criterion e75d8fbd: the previously reviewed head and the
// incremental scope.
// TestCase cafd0f21-8cae-4bab-9734-a0c50ad3db94
func TestRddPrReviewRenewedReviewFindsThePreviousHead(t *testing.T) {
	requireBuildRecordPhrases(t, rddPrReviewAsset, rddPrReviewText(t), []string{
		"Identify the previously reviewed head from the earlier review in chat or the pull request's review comments; compare it with the current head for the incremental scope while keeping the full intent and the constraints in view",
	})
}

// SR-5037 criterion a7f6dd5e: each earlier finding fixed, unresolved or
// unverified; new files; revisited files; the full-review fallback.
// TestCase 9fd91734-9f9b-4b50-a601-ad169d438234
func TestRddPrReviewRenewedReviewReportsEachEarlierFinding(t *testing.T) {
	requireBuildRecordPhrases(t, rddPrReviewAsset, rddPrReviewText(t), []string{
		"Report each earlier finding as fixed, unresolved or unverified (a resolved thread alone proves nothing), fully review new files, and revisit reviewed files whose assumptions the fixes touch",
		"If the earlier review cannot be recovered, review in full and say the earlier fixes could not be verified",
	})
}

// SR-5038 criterion 885f252a: the agent asset with Read, Grep and Glob and no
// shell, its Codex rendering equal to codexAgentTOML, and both installed.
// TestCase ac65ba06-bba6-446e-89c7-e2345d01db4a
func TestKitInstallsTheRddPrReviewer(t *testing.T) {
	md, err := assets.ReadFile(rddPrReviewerAsset)
	if err != nil {
		t.Fatalf("the kit must embed %s: %v", rddPrReviewerAsset, err)
	}
	if !bytes.HasPrefix(md, []byte("---\nname: rdd-pr-reviewer\ndescription: ")) {
		t.Errorf("the reviewer frontmatter must open with name: rdd-pr-reviewer and a description; got %q", firstLines(string(md), 3))
	}
	if !bytes.Contains(md, []byte("tools: Read, Grep, Glob")) || bytes.Contains(md, []byte("Bash")) {
		t.Errorf("the reviewer carries Read, Grep, Glob and no shell:\n%s", md)
	}
	wantTOML, err := codexAgentTOML(md)
	if err != nil {
		t.Fatal(err)
	}
	gotTOML, err := assets.ReadFile(rddPrReviewerCodex)
	if err != nil {
		t.Fatalf("the kit must embed the Codex rendering %s: %v", rddPrReviewerCodex, err)
	}
	if !bytes.Equal(wantTOML, gotTOML) {
		t.Errorf("%s is stale; rerun with UPDATE_CODEX_AGENTS=1", rddPrReviewerCodex)
	}

	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, rddPrReviewerTarget))
	if err != nil {
		t.Fatalf("install must write %s: %v", rddPrReviewerTarget, err)
	}
	if !bytes.Equal(md, got) {
		t.Errorf("%s must equal the asset", rddPrReviewerTarget)
	}
	if _, err := os.Stat(filepath.Join(root, rddPrReviewerCodexAt)); err != nil {
		t.Errorf("install must write %s: %v", rddPrReviewerCodexAt, err)
	}
	drift, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 0 {
		t.Errorf("install --check reports drift after a fresh install: %v", drift)
	}
}

// SR-5038 criterion 21f62138: the reviewer works from the supplied facts,
// invokes no store command, writes nothing, and returns the report in order.
// TestCase 1208b13b-8bd4-465f-839f-46572cb46f33
func TestRddPrReviewerReviewsFromTheSuppliedFacts(t *testing.T) {
	phrases := []string{
		"The agent handling the task supplies the pull request facts (base and head, commits, changed files, checks), the diff, the retained review bundle of each delivered Epic",
		"Do not fetch a replacement bundle or invoke process-store commands",
		"Return, in this order: findings by severity, each with file and line, concrete trigger and consequence, the Epic intent or EC-<number> it rests on and a focused correction; what the pull request is about; how it implements that intent; an intent verdict per Epic and for the branch; the constraint assessment; the checks performed and the limits",
		"Write nothing: no edits, store writes, Git operations, comments or posts",
	}
	requireBuildRecordPhrases(t, rddPrReviewerAsset, rddPrReviewerText(t), phrases)
	requireBuildRecordPhrases(t, rddPrReviewerCodex, buildRecordText(t, rddPrReviewerCodex), phrases)
}

// SR-5039 criterion 1ee16c11: frontmatter, both installed copies byte-identical,
// install --check reports drift.
// TestCase 8f75c97b-1a9e-42ab-8549-ffafc63eca53
func TestRddPrReviewSkillInstallsToBothSkillTrees(t *testing.T) {
	body, err := assets.ReadFile(rddPrReviewAsset)
	if err != nil {
		t.Fatalf("the kit must embed %s: %v", rddPrReviewAsset, err)
	}
	if !strings.HasPrefix(string(body), "---\nname: rdd-pr-review\ndescription: ") {
		t.Errorf("the skill frontmatter must open with name: rdd-pr-review and a description; got %q", firstLines(string(body), 3))
	}
	if target, ok := TargetForAsset(rddPrReviewAsset); !ok || target != ".modernpath/rdd/skills/rdd-pr-review/SKILL.md" {
		t.Errorf("TargetForAsset = %q, %v", target, ok)
	}
	if claude, ok := ClaudeSkillTargetForAsset(rddPrReviewAsset); !ok || claude != ".claude/skills/rdd-pr-review/SKILL.md" {
		t.Errorf("ClaudeSkillTargetForAsset = %q, %v", claude, ok)
	}
	var assetPaths []string
	if err := fs.WalkDir(assets, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			assetPaths = append(assetPaths, p)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, managed := range managedSkillReferenceTargets(assetPaths) {
		if strings.Contains(managed, "rdd-pr-review") {
			t.Errorf("the skill-references manifest lists %s; rdd-pr-review ships no references", managed)
		}
	}

	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	canonical := installedKitFile(t, root, ".modernpath", "rdd", "skills", "rdd-pr-review", "SKILL.md")
	discovered := installedKitFile(t, root, ".claude", "skills", "rdd-pr-review", "SKILL.md")
	if !bytes.Equal(canonical, body) {
		t.Error("the installed .modernpath copy differs from the embedded asset")
	}
	if !bytes.Equal(canonical, discovered) {
		t.Error("the .claude/skills copy differs from the .modernpath/rdd/skills copy")
	}
	if drift, err := Check(root); err != nil || len(drift) != 0 {
		t.Fatalf("a fresh install must be clean: drift=%v err=%v", drift, err)
	}
	path := filepath.Join(root, ".claude", "skills", "rdd-pr-review", "SKILL.md")
	if err := os.WriteFile(path, append(discovered, []byte("\nedited\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	drift, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 1 || drift[0] != ".claude/skills/rdd-pr-review/SKILL.md" {
		t.Errorf("an edited copy must be reported as drift, got %v", drift)
	}
}

// SR-5039 criterion 9f70a8b6: listed in the managed AGENTS.md block (skill
// bullet and the reviewer sentence), the kit AGENTS.md required reading, the
// PROCESS.md phase table and §Delegated passes, and the pass-to-command map.
// The changelog entry is not checked here: a release cut moves it out of
// Unreleased into its release section.
// TestCase 64d09ef7-44d3-473a-82b3-d0f95c014f9b
func TestRddPrReviewIsNamedInTheProcessDocuments(t *testing.T) {
	read := func(parts ...string) string {
		body, err := os.ReadFile(filepath.Join(parts...))
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	block := read("assets", "agents-block.md")
	for _, want := range []string{"`rdd-pr-review`", "`.claude/agents/rdd-pr-reviewer.md`"} {
		if !strings.Contains(block, want) {
			t.Errorf("assets/agents-block.md does not name %s", want)
		}
	}
	agents := read("assets", "rdd", "AGENTS.md")
	if !strings.Contains(agents, "skills/rdd-pr-review/SKILL.md") {
		t.Error("assets/rdd/AGENTS.md required reading does not name skills/rdd-pr-review/SKILL.md")
	}
	process := read("assets", "rdd", "PROCESS.md")
	var row string
	for _, line := range strings.Split(process, "\n") {
		if strings.HasPrefix(line, "| Review a pull request | `rdd-pr-review` |") {
			row = line
		}
	}
	if row == "" {
		t.Error("assets/rdd/PROCESS.md phase table has no row | Review a pull request | `rdd-pr-review` |")
	}
	for _, phrase := range []string{"Review returned in chat", "findings by severity", "intent verdict per Epic and for the branch", "constraint assessment", "checks and limits", "nothing recorded, nothing posted unless asked"} {
		if !strings.Contains(row, phrase) {
			t.Errorf("the PROCESS.md rdd-pr-review row lacks %q: %s", phrase, row)
		}
	}
	delegated := processSection(t, process, "### Delegated passes")
	if !strings.Contains(foldBuildRecordText(delegated), "A PR review (rdd-pr-review) runs the same way") {
		t.Error("PROCESS.md §Delegated passes does not name the PR review pass")
	}
	loop := read("..", "..", "cmd", "reference-loop.md")
	var loopRow string
	for _, line := range strings.Split(loop, "\n") {
		if strings.HasPrefix(line, "| PR review | `rdd-pr-review` |") {
			loopRow = line
		}
	}
	if loopRow == "" {
		t.Fatal("cmd/reference-loop.md has no row | PR review | `rdd-pr-review` |")
	}
	for _, phrase := range []string{
		"`epics get <id> --full --json`", "`epics plan show <id> --full --json`",
		"`epics gates review-bundle <id> --phase implementation --json`",
		"`constraints list --state active --json`", "`constraints get <ref> --json`",
		"`rdd-pr-reviewer`", "returned in chat", "no review is recorded",
	} {
		if !strings.Contains(loopRow, phrase) {
			t.Errorf("the reference-loop rdd-pr-review row lacks %q", phrase)
		}
	}
}

// SR-5039 criterion 7f32fe08 and SR-4923 criterion 7cda81bb (revision 2): the
// cold review names rdd-pr-review as the home of the PR-review read, and no
// kit asset names the plan Automated Code Review Skills as its home.
// TestCase 992e2e42-893a-4ab6-841a-fdc4cc344e7a
func TestColdReviewNamesRddPrReviewAsTheHomeOfThePrReviewRead(t *testing.T) {
	requireBuildRecordPhrases(t, "assets/rdd/skills/rdd-cold-review/SKILL.md",
		buildRecordText(t, "assets/rdd/skills/rdd-cold-review/SKILL.md"),
		[]string{"the PR-review read of constraints belongs to rdd-pr-review and is outside this skill"})
	for _, asset := range []string{"assets/agents/rdd-cold-reviewer.md", "assets/agents-codex/rdd-cold-reviewer.toml"} {
		requireBuildRecordPhrases(t, asset, buildRecordText(t, asset),
			[]string{"The PR-review read of constraints belongs to rdd-pr-review and is outside this review"})
	}
	if err := fs.WalkDir(assets, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := assets.ReadFile(p)
		if err != nil {
			return err
		}
		if bytes.Contains(body, []byte("Automated Code Review Skills")) {
			t.Errorf("%s still names the plan Automated Code Review Skills as the home of the PR-review read", p)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// UR-816 scenario afdf65d7: a pull request delivering two Epics gets one
// review in chat covering each Epic's intent, the constraints and the branch,
// with the store unchanged. Checked on the installed skill text.
// TestCase aa4c5096-6a1c-4f13-9bce-ce700c4fef62
func TestRddPrReviewScenarioOneReviewPerPullRequest(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	installed := foldBuildRecordText(string(installedKitFile(t, root, ".claude", "skills", "rdd-pr-review", "SKILL.md")))
	requireBuildRecordPhrases(t, "installed .claude/skills/rdd-pr-review/SKILL.md", installed, []string{
		"Return the reviewer's report in chat, even with no findings, with the pull request link and the base and head reviewed, in its order: findings by severity; what the pull request is about; how it implements that intent; intent verdict per Epic and for the branch; constraint assessment per Active constraint with its revision; checks performed and limits",
		"one intent assessment each, plus the branch as a whole",
		"It writes nothing to the store, records no review, pull request number or link",
	})
}

// UR-816 scenario 89462cda: after fixes, a second run marks each earlier
// finding fixed, unresolved or unverified against the new head.
// TestCase baf23acd-298c-4ede-86b5-ed6a89f42edb
func TestRddPrReviewScenarioSecondRunAfterFixes(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	installed := foldBuildRecordText(string(installedKitFile(t, root, ".modernpath", "rdd", "skills", "rdd-pr-review", "SKILL.md")))
	requireBuildRecordPhrases(t, "installed .modernpath/rdd/skills/rdd-pr-review/SKILL.md", installed, []string{
		"Identify the previously reviewed head",
		"Report each earlier finding as fixed, unresolved or unverified",
	})
}
