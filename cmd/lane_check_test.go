package cmd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// REQ-CROSS-455 / REQ-CROSS-458 (EPIC-RDD-LANE): `process lane check <SR>
// --commit <sha> [--base <sha>]` reads the change delivered to the default
// branch — a merge or squash commit against its first parent, or a rebase
// delivery's base-to-tip range — posts its file list with the range, and
// prints the server's verdict. It refuses a commit the default branch does
// not reach and a base that is not the commit's ancestor, before any write.

type laneRepo struct {
	merge, side, base, tip, excluded string
}

func laneWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// laneCheckRepo builds, in the workspace: a merge delivery (two files through
// a --no-ff merge), a linear rebase delivery (base..tip), a commit on a branch
// the default branch never reaches, and a merge into an excluded area.
func laneCheckRepo(t *testing.T) laneRepo {
	t.Helper()
	var r laneRepo
	laneGit(t, "init", "-q", "-b", "main")
	laneWrite(t, "README.md", "root\n")
	laneGit(t, "add", "README.md")
	laneGit(t, "commit", "-q", "-m", "root")

	laneGit(t, "checkout", "-q", "-b", "feature")
	laneWrite(t, "src/empty_state.tsx", "copy\n")
	laneWrite(t, "src/empty_state_test.go", "package src\n")
	laneGit(t, "add", "src")
	laneGit(t, "commit", "-q", "-m", "fix the copy")
	laneGit(t, "checkout", "-q", "main")
	laneGit(t, "merge", "-q", "--no-ff", "feature", "-m", "merge the copy fix")
	r.merge = laneGit(t, "rev-parse", "HEAD")

	r.base = r.merge
	laneWrite(t, "src/label.tsx", "label\n")
	laneGit(t, "add", "src/label.tsx")
	laneGit(t, "commit", "-q", "-m", "rebased one")
	laneWrite(t, "src/label_test.go", "package src\n")
	laneGit(t, "add", "src/label_test.go")
	laneGit(t, "commit", "-q", "-m", "rebased two")
	r.tip = laneGit(t, "rev-parse", "HEAD")

	laneGit(t, "checkout", "-q", "-b", "side")
	laneWrite(t, "src/side.tsx", "side\n")
	laneGit(t, "add", "src/side.tsx")
	laneGit(t, "commit", "-q", "-m", "never merged")
	r.side = laneGit(t, "rev-parse", "HEAD")
	laneGit(t, "checkout", "-q", "main")

	laneGit(t, "checkout", "-q", "-b", "auth")
	laneWrite(t, "oidc-bff/main.go", "package main\n")
	laneGit(t, "add", "oidc-bff")
	laneGit(t, "commit", "-q", "-m", "touch the auth boundary")
	laneGit(t, "checkout", "-q", "main")
	laneGit(t, "merge", "-q", "--no-ff", "auth", "-m", "merge the auth change")
	r.excluded = laneGit(t, "rev-parse", "HEAD")
	return r
}

func laneCheckStore(t *testing.T) (*laneStore, laneRepo) {
	l := newLaneStore(t)
	l.seedSR(laneSR, "wording")
	l.seedAuthorization("LANE-AUTH-1")
	l.entered[laneSR] = l.aggregate(laneSR)
	l.setStatus(laneSR, "IN_REVIEW")
	laneWorkspace(t, l)
	return l, laneCheckRepo(t)
}

func TestLaneCheckPostsTheMergeDeliveryAgainstItsFirstParent(t *testing.T) {
	l, repo := laneCheckStore(t)

	out, err := runRoot(t, "process", "lane", "check", laneSR, "--commit", repo.merge[:10])
	if err != nil {
		t.Fatalf("lane check: %v\n%s", err, out)
	}
	posts := l.lanePosts("lane_eligibility")
	if len(posts) != 1 {
		t.Fatalf("check posts one lane_eligibility, got %d", len(posts))
	}
	rec, _ := posts[0]["record"].(map[string]any)
	if rec["external_id"] != laneSR || rec["commit"] != repo.merge {
		t.Errorf("the check names the SR and the full delivering commit %s, posted %v", repo.merge, rec)
	}
	if b, present := rec["base"]; present && b != nil {
		t.Errorf("a merge delivery posts no base, posted %v", b)
	}
	files := stringSlice(rec["files"])
	slices.Sort(files)
	if !slices.Equal(files, []string{"src/empty_state.tsx", "src/empty_state_test.go"}) {
		t.Errorf("the files are git diff --name-only over <sha>^1..<sha>, posted %v", files)
	}
	for _, want := range []string{"PASS", repo.merge + "^1.." + repo.merge} {
		if !strings.Contains(out, want) {
			t.Errorf("the output must show %q:\n%s", want, out)
		}
	}
}

func TestLaneCheckPostsARebaseDeliveryFromItsBase(t *testing.T) {
	l, repo := laneCheckStore(t)

	if out, err := runRoot(t, "process", "lane", "check", laneSR, "--commit", repo.tip, "--base", repo.base); err != nil {
		t.Fatalf("lane check --base: %v\n%s", err, out)
	}
	rec, _ := l.lanePosts("lane_eligibility")[0]["record"].(map[string]any)
	files := stringSlice(rec["files"])
	slices.Sort(files)
	if rec["base"] != repo.base || !slices.Equal(files, []string{"src/label.tsx", "src/label_test.go"}) {
		t.Errorf("a rebase delivery posts the base and the files of base..tip, posted %v", rec)
	}
}

func TestLaneCheckPrintsAFailingVerdictWithEachOffendingFile(t *testing.T) {
	_, repo := laneCheckStore(t)

	out, err := runRoot(t, "process", "lane", "check", laneSR, "--commit", repo.excluded)
	if err == nil {
		t.Fatalf("a FAIL verdict exits non-zero\n%s", out)
	}
	for _, want := range []string{"FAIL", "oidc-bff/main.go", "excluded area"} {
		if !strings.Contains(out+err.Error(), want) {
			t.Errorf("the failing verdict must show %q:\n%s\n%v", want, out, err)
		}
	}
}

func TestLaneCheckRefusesAnUndeliveredCommitOrABaseThatIsNotItsAncestor(t *testing.T) {
	l, repo := laneCheckStore(t)

	if _, err := runRoot(t, "process", "lane", "check", laneSR, "--commit", repo.side); err == nil || !strings.Contains(err.Error(), "not reachable from the default branch") {
		t.Errorf("a commit the default branch does not reach is refused, got %v", err)
	}
	if _, err := runRoot(t, "process", "lane", "check", laneSR, "--commit", repo.merge, "--base", repo.tip); err == nil || !strings.Contains(err.Error(), "is not an ancestor of") {
		t.Errorf("a base that is not the commit's ancestor is refused, got %v", err)
	}
	if _, err := runRoot(t, "process", "lane", "check", laneSR, "--commit", "0000000deadbeef"); err == nil || !strings.Contains(err.Error(), "is not a commit") {
		t.Errorf("an unknown commit is refused, got %v", err)
	}
	if _, err := runRoot(t, "process", "lane", "check", laneSR); err == nil || !strings.Contains(err.Error(), "--commit") {
		t.Errorf("a check without --commit is refused naming it, got %v", err)
	}
	if n := len(l.lanePosts("lane_eligibility")); n != 0 {
		t.Fatalf("a refused check posts nothing, got %d", n)
	}
}
