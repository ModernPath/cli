package cmd

import (
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// deliveredHead checks that HEAD is exactly the fetched tip of the remote
// default branch — the delivered revision (D2, amended for F-CLI017-R1-03) —
// and returns the full sha. A failed fetch is a refusal (N-CLI017-R2-01): a
// stale origin ref that happens to equal HEAD is the same mistake by another
// door. --no-fetch exists for offline fixtures only.
func deliveredHead(root string, noFetch bool) (string, error) {
	branch, err := remoteDefaultBranch(root, noFetch, "to confirm the delivered tip", "completion runs at the merged revision")
	if err != nil {
		return "", err
	}
	head := gitOut(root, "rev-parse", "HEAD")
	tip := gitOut(root, "rev-parse", "origin/"+branch)
	switch {
	case head == "" || tip == "":
		return "", fmt.Errorf("could not resolve HEAD (%q) or origin/%s (%q) — completion runs at the delivered revision", head, branch, tip)
	case head == tip:
		return head, nil
	}
	cmd := exec.Command("git", "merge-base", "--is-ancestor", head, tip)
	cmd.Dir = root
	if cmd.Run() == nil {
		behind := gitOut(root, "rev-list", "--count", head+".."+tip)
		unit := "commits"
		if behind == "1" {
			unit = "commit"
		}
		return "", fmt.Errorf("HEAD %s is %s %s behind the delivered tip origin/%s %s — pull before completing", head[:7], behind, unit, branch, tip[:7])
	}
	return "", fmt.Errorf("HEAD %s is not on the delivered branch origin/%s (tip %s) — merge and check out the merged revision before completing", head[:7], branch, tip[:7])
}

// remoteDefaultBranch resolves the remote default branch (refs/remotes/
// origin/HEAD, fallback main) and fetches it unless noFetch — the part of
// deliveredHead that `process enter` shares (SR-CLI-028-002). A failed fetch
// is a refusal: a stale origin ref is the wrong tip by another door.
func remoteDefaultBranch(root string, noFetch bool, purpose, consequence string) (string, error) {
	branch := "main"
	if ref := gitOut(root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); ref != "" {
		branch = strings.TrimPrefix(ref, "origin/")
	}
	if !noFetch {
		cmd := exec.Command("git", "fetch", "--quiet", "origin", branch)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("could not fetch origin/%s %s: %s — %s; use --no-fetch only for offline fixtures", branch, purpose, strings.TrimSpace(string(out)), consequence)
		}
	}
	return branch, nil
}

// selectionReconRevision reads the caller's selection for the scope and
// returns its recon_revision ("" when the take recorded none).
func selectionReconRevision(env *factoryEnv, scope string) (string, error) {
	payload, err := fetchWorkSelectionFor(env, scope)
	if err != nil {
		return "", err
	}
	current, _ := payload["current"].(map[string]any)
	return str(current, "recon_revision"), nil
}

// gitIsAncestor reports whether a is an ancestor of (or equal to) b. Exit 1
// is "not an ancestor"; any other failure (128: an unknown revision) is
// reported as an error so the caller can name the revision.
func gitIsAncestor(root, a, b string) (bool, error) {
	cmd := exec.Command("git", "merge-base", "--is-ancestor", a, b)
	cmd.Dir = root
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

// citationRe matches a CODE:/TEST: citation in packet text: the token after
// the prefix — spaces or tabs between them skipped, since packet text writes
// `CODE: <path>` as well as `CODE:<path>` — up to whitespace, a quote, a
// bracket or a separator. The auditor's regex (audit-citations.mjs) requires a
// known extension; this extractor is the CLI's own, so an extension-less path
// (VERSION, Makefile) is matched too.
var citationRe = regexp.MustCompile("(?:CODE|TEST):[ \\t]*([^\\s`'\"<>()\\[\\]{},;]+)")

// repoQualifierRe strips a `repo@rev:` qualifier from a cited path.
var repoQualifierRe = regexp.MustCompile(`^[^/:@\s]+@[^/:\s]+:`)

// lineSuffixRe strips a `:12`, `:L12`, `:12-14` or `:12–14` line suffix.
var lineSuffixRe = regexp.MustCompile(`:L?\d+(?:[-–]\d+)?$`)

// citedPaths returns the distinct repository paths the given packet sections
// cite as CODE: or TEST:, with an optional :line (or :line-line) suffix and a
// repo@rev: qualifier stripped, and trailing sentence punctuation dropped.
func citedPaths(sections []any) map[string]bool {
	paths := map[string]bool{}
	for _, sec := range sections {
		m, _ := sec.(map[string]any)
		for _, match := range citationRe.FindAllStringSubmatch(str(m, "content"), -1) {
			p := strings.TrimRight(match[1], ".:")
			p = repoQualifierRe.ReplaceAllString(p, "")
			p = lineSuffixRe.ReplaceAllString(p, "")
			p = strings.TrimRight(p, ".:")
			if p != "" && !strings.Contains(p, "…") {
				paths[p] = true
			}
		}
	}
	return paths
}

// reconDrift compares the remote default branch against the selection's
// reconnaissance revision (SR-CLI-028-002 C2, C3). A tip equal to the
// revision, or an ancestor of it, is current. Otherwise the paths changed
// from the merge-base to the tip (`recon...tip`, so a branch's own commits
// are not drift) are intersected with the paths the scope's stored packet
// sections cite; an empty intersection proceeds, a non-empty one refuses
// unless allow carries the human's USER: source, in which case the returned
// line is recorded on the gate body.
func reconDrift(env *factoryEnv, scope, scopeKind, recon, allow string, noFetch bool) (string, error) {
	root := env.Root
	// F-PR697-04: the revision comes from the store as a plain string; one
	// shaped like an option must not reach git as one.
	if strings.HasPrefix(recon, "-") || strings.ContainsAny(recon, " \t\n") {
		return "", fmt.Errorf("reconnaissance revision %q is not a revision — re-select with `working-set select %s --recon-revision <sha>`", recon, scope)
	}
	branch, err := remoteDefaultBranch(root, noFetch, "to compare the reconnaissance against", "entry compares its tip with the reconnaissance revision")
	if err != nil {
		return "", err
	}
	tip := gitOut(root, "rev-parse", "--verify", "--quiet", "origin/"+branch+"^{commit}")
	if tip == "" {
		return "", fmt.Errorf("could not resolve origin/%s — entry compares its tip with the reconnaissance revision %s", branch, recon)
	}
	full := gitOut(root, "rev-parse", "--verify", "--quiet", recon+"^{commit}")
	if full == "" {
		return "", fmt.Errorf("reconnaissance revision %s is not present in the repository — fetch it, or re-reconnoitre and record the revision with `working-set select %s --recon-revision <sha>`", recon, scope)
	}
	current := full == tip
	if !current {
		current, err = gitIsAncestor(root, tip, full)
		if err != nil {
			return "", fmt.Errorf("could not compare origin/%s %s with the reconnaissance revision %s: %v", branch, tip, recon, err)
		}
	}
	if current {
		fmt.Printf("reconnaissance %s current (origin/%s at %s)\n", recon, branch, tip[:7])
		if allow != "" {
			fmt.Println("  --allow-drift given but the reconnaissance is current — nothing to accept, nothing recorded")
		}
		return "", nil
	}
	// F-PR697-01: a diff that fails (no merge base — an orphan revision, a
	// shallow clone cut below it) is a refusal, never "no cited path changed".
	diffCmd := exec.Command("git", "diff", "--name-only", full+"..."+tip)
	diffCmd.Dir = root
	changed, err := diffCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("could not diff the reconnaissance revision %s against origin/%s %s: %s — the two share no history the check can compare; re-reconnoitre and record the revision with `working-set select %s --recon-revision <sha>`", recon, branch, tip, strings.TrimSpace(string(changed)), scope)
	}
	changedOut := string(changed)
	sections, err := fetchList(env,
		fmt.Sprintf("/api/v1/sync/packet-sections?system_id=%d&scope=%s:%s", env.SystemID, scopeKind, url.QueryEscape(scope)),
		"packet_sections")
	if err != nil {
		return "", err
	}
	cited := citedPaths(sections)
	var drifted []string
	for _, line := range strings.Split(changedOut, "\n") {
		if path := strings.TrimSpace(line); path != "" && cited[path] {
			drifted = append(drifted, path)
		}
	}
	sort.Strings(drifted)
	if len(drifted) == 0 {
		fmt.Printf("origin/%s moved to %s; no cited path changed since the reconnaissance %s\n", branch, tip, recon)
		return "", nil
	}
	if allow == "" {
		return "", fmt.Errorf("reconnaissance at %s is stale: origin/%s %s changed %d cited path(s): %s — re-reconnoitre and record the revision (`working-set select %s --recon-revision %s`) or accept the drift with --allow-drift USER:<date>:<why>", recon, branch, tip, len(drifted), strings.Join(drifted, ", "), scope, tip)
	}
	line := fmt.Sprintf("Reconnaissance drift accepted (%s): origin/%s %s; changed cited paths: %s", allow, branch, tip, strings.Join(drifted, ", "))
	fmt.Println(line)
	return line, nil
}
