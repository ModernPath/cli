package cmd

import (
	"fmt"

	"strings"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------- check

var (
	laneCheckCommit string
	laneCheckBase   string
)

var processLaneCheckCmd = &cobra.Command{
	Use:   "check <SR> --commit <sha> [--base <sha>]",
	Short: "Post the small change's delivered file list; the server records the eligibility verdict",
	Long: `Check that a delivered small change is small enough for the lane. The
change is what reached the default branch: --commit is the merge or squash
commit, compared with its first parent; for a rebase delivery, give the
commit it started from with --base. The commit must be on the default
branch (origin/HEAD, else origin/main, main or master).

This CLI reports the changed files (git diff --name-only over the range);
the server cannot read your repository, so it judges the list you report:
at most five non-test source files, none in an excluded area. It records
the result on the SR, and this prints it. The first full report for a
commit counts: a later report for the same commit that leaves files out,
or uses another base, is refused. A FAIL names each offending file and
exits non-zero: the change leaves the lane.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if laneCheckCommit == "" {
			return fmt.Errorf("--commit <sha> is required: the commit that delivered the change to the default branch")
		}
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processLaneCheck(env, args[0], laneCheckCommit, laneCheckBase)
	},
}

// laneDefaultBranch is the ref the delivered change must be reachable from.
func laneDefaultBranch(root string) (string, error) {
	if ref := gitOut(root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); ref != "" {
		return ref, nil
	}
	for _, ref := range []string{"origin/main", "origin/master", "main", "master"} {
		if gitOut(root, "rev-parse", "--verify", "--quiet", ref+"^{commit}") != "" {
			return ref, nil
		}
	}
	return "", fmt.Errorf("no default branch found (origin/HEAD, origin/main, origin/master, main or master) — the lane checks the change delivered to it; nothing was written")
}

// laneIsAncestor is gitIsAncestor (process_ceremony.go) with any git failure
// read as "not an ancestor": the lane has already resolved both commits.
func laneIsAncestor(root, a, b string) bool {
	ok, err := gitIsAncestor(root, a, b)
	return err == nil && ok
}

func processLaneCheck(env *factoryEnv, sr, commit, base string) error {
	root := env.Root
	full := gitOut(root, "rev-parse", "--verify", "--quiet", commit+"^{commit}")
	if full == "" {
		return fmt.Errorf("--commit %s is not a commit in this repository — fetch the default branch and give the delivering commit; nothing was written", commit)
	}
	branch, err := laneDefaultBranch(root)
	if err != nil {
		return err
	}
	if !laneIsAncestor(root, full, branch) {
		return fmt.Errorf("%s is not reachable from the default branch %s — lane check reads the change delivered to it; merge it (and fetch) first; nothing was written", full[:12], branch)
	}
	from, baseFull := "", ""
	if base != "" {
		baseFull = gitOut(root, "rev-parse", "--verify", "--quiet", base+"^{commit}")
		if baseFull == "" {
			return fmt.Errorf("--base %s is not a commit in this repository; nothing was written", base)
		}
		if baseFull == full || !laneIsAncestor(root, baseFull, full) {
			return fmt.Errorf("--base %s is not an ancestor of %s — a rebase delivery names the commit its range starts from; nothing was written", baseFull[:12], full[:12])
		}
		from = baseFull
	} else {
		from = gitOut(root, "rev-parse", "--verify", "--quiet", full+"^1")
		if from == "" {
			return fmt.Errorf("%s has no first parent — name the delivery's base with --base; nothing was written", full[:12])
		}
	}
	rng := full + "^1.." + full
	if baseFull != "" {
		rng = baseFull + ".." + full
	}
	diff := gitOut(root, "diff", "--name-only", from, full)
	var files []string
	for _, f := range strings.Split(diff, "\n") {
		if f = strings.TrimSpace(f); f != "" {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return fmt.Errorf("the range %s delivers no file — name the commit that delivered %s; nothing was written", rng, sr)
	}

	if status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/lane/eligibility-rules?system_id=%d", env.SystemID), nil); err == nil && status == 200 {
		rules := dataOf(body)
		fmt.Printf("rules (the server applies them): at most %v non-test source files; %d default excluded globs plus the authorization's; tests: %s\n",
			rules["max_non_test_files"], len(stringSlice(rules["default_excluded_globs"])), str(rules, "test_paths"))
	}
	fmt.Printf("lane check of %s over %s on %s: %d file(s)\n", sr, rng, branch, len(files))

	record := map[string]any{"external_id": sr, "commit": full, "files": files}
	if baseFull != "" {
		record["base"] = baseFull
	}
	data, err := authorPost(env, map[string]any{"action": "lane_eligibility", "record": record})
	if err != nil {
		return err
	}
	res, _ := data["lane_eligibility"].(map[string]any)
	verdict := str(res, "verdict")
	nonTest, tests := stringSlice(res["non_test_files"]), stringSlice(res["test_files"])
	fmt.Printf("%s: %s (server verdict) — %d non-test source file(s), %d test file(s) not counted\n", sr, verdict, len(nonTest), len(tests))
	for _, f := range nonTest {
		fmt.Printf("  %s\n", f)
	}
	var offending []string
	for _, raw := range anyList(res["offending"]) {
		o, _ := raw.(map[string]any)
		offending = append(offending, fmt.Sprintf("%s — %s", str(o, "file"), str(o, "reason")))
	}
	if len(offending) > 0 {
		fmt.Println("offending:")
		for _, o := range offending {
			fmt.Printf("  ✗ %s\n", o)
		}
	}
	fmt.Printf("  range: %s\n  delivered revision: %s\n  trace: %s\n", firstNonEmpty(str(res, "range"), rng), firstNonEmpty(str(res, "delivered_revision"), full), str(res, "trace"))
	if verdict != "PASS" {
		return fmt.Errorf("%s is not eligible for the lane (%s): %s — it leaves the lane for single-SR scope with its full packet and review", sr, firstNonEmpty(verdict, "no verdict"), strings.Join(offending, "; "))
	}
	return nil
}
