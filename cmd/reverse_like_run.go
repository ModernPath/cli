package cmd

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// reverseOutOfScopeReason is the exclusion reason the inventory writes for a
// path outside the authorized scope, and the one a like-run reads back.
const reverseOutOfScopeReason = "outside the authorized scope"

// reverseLikeRun is an earlier run's area per repository: the paths to
// inventory as --path would take them, none for a whole repository.
type reverseLikeRun struct {
	runID   string
	earlier map[string]reverseRepository
	scopes  map[string][]string
}

// reverseLikeRunScopes reads the run and derives, for every repository it
// covered, the paths that hold its files. Every repository of the run must be
// declared, every declared repository must be in the run, and each must be a
// Git repository.
func reverseLikeRunScopes(env *factoryEnv, runID string, declarations []string) (*reverseLikeRun, error) {
	body, err := reverseCall(env, "GET", "/reverse-engineering/runs/"+url.PathEscape(runID), nil)
	if err != nil {
		return nil, fmt.Errorf("could not read run %s: %w", runID, err)
	}
	repositories, err := reverseAuthorizedRepositories(body)
	if err != nil {
		return nil, fmt.Errorf("could not read run %s: %w", runID, err)
	}
	declared := map[string]string{}
	for _, declaration := range declarations {
		if key, directory, ok := strings.Cut(declaration, "="); ok {
			declared[key] = directory
		}
	}
	like := &reverseLikeRun{runID: runID, earlier: map[string]reverseRepository{}, scopes: map[string][]string{}}
	for _, repository := range repositories {
		if _, ok := declared[repository.Key]; !ok {
			return nil, fmt.Errorf("run %s covered repository %q; declare it with --repository %s=<directory>", runID, repository.Key, repository.Key)
		}
		like.earlier[repository.Key] = repository
	}
	keys := make([]string, 0, len(declared))
	for key := range declared {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, ok := like.earlier[key]; !ok {
			return nil, fmt.Errorf("repository %q is not in run %s; --like-run inventories only the repositories that run covered", key, runID)
		}
	}
	for _, repository := range repositories {
		if repository.Revision == "unversioned" {
			return nil, fmt.Errorf("run %s recorded repository %s without Git; --like-run can only repeat the area of a Git repository", runID, repository.Key)
		}
		root, err := reverseRoot(declared[repository.Key])
		if err != nil {
			return nil, fmt.Errorf("repository %s: %w", repository.Key, err)
		}
		if _, err := os.Lstat(filepath.Join(root, ".git")); err != nil {
			return nil, fmt.Errorf("repository %s is not a Git repository; --like-run can only repeat the area of a Git repository", repository.Key)
		}
		area := reverseLikeRunArea(repository)
		if len(area) == 0 {
			continue
		}
		// A derived path that no longer holds a file is left out: its files
		// are named as missing. With none left the area cannot be inventoried.
		present := []string{}
		for _, path := range area {
			if reverseScopeMatches(root, path) {
				present = append(present, path)
			}
		}
		if len(present) == 0 {
			return nil, fmt.Errorf("none of the paths derived from run %s holds a file in repository %s now: %s", runID, repository.Key, strings.Join(area, ", "))
		}
		like.scopes[repository.Key] = present
	}
	return like, nil
}

// reverseLikeRunArea derives the paths an earlier run covered from its files
// and out-of-scope exclusions: for each file, the shallowest folder with no
// out-of-scope exclusion at or under it, or the file itself when every folder
// above it has one; a file in the repository root by its name. A run without
// out-of-scope exclusions covered the whole repository, and no path is
// derived. A folder named for a run that held a single file in it cannot be
// told apart from that file: the folder is derived.
func reverseLikeRunArea(repository reverseRepository) []string {
	left := []string{}
	for _, exclusion := range repository.Exclusions {
		if exclusion.Reason == reverseOutOfScopeReason {
			left = append(left, exclusion.Path)
		}
	}
	if len(left) == 0 {
		return nil
	}
	leaves := func(dir string) bool {
		for _, path := range left {
			if path == dir || strings.HasPrefix(path, dir+"/") {
				return true
			}
		}
		return false
	}
	seen := map[string]bool{}
	area := []string{}
	for _, file := range repository.Files {
		target := file.Path
		parts := strings.Split(file.Path, "/")
		for i := 1; i < len(parts); i++ {
			if dir := strings.Join(parts[:i], "/"); !leaves(dir) {
				target = dir
				break
			}
		}
		if !seen[target] {
			seen[target] = true
			area = append(area, target)
		}
	}
	sort.Strings(area)
	return area
}

// reverseScopeMatches reports whether a literal path names a tracked or
// unignored file now, as --path would list it.
func reverseScopeMatches(root, path string) bool {
	command := exec.Command("git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", path)
	command.Env = append(os.Environ(), "GIT_LITERAL_PATHSPECS=1")
	listed, err := command.Output()
	return err == nil && len(listed) > 0
}

// reverseLikeRunReport is what the error stream says for one repository: the
// area inventoried and every file that differs from the earlier run, by state.
// The earlier run's clean or dirty state is not compared.
func reverseLikeRunReport(runID string, scope []string, earlier, now reverseRepository) []string {
	area := "the whole repository"
	if len(scope) > 0 {
		area = strings.Join(scope, ", ")
	}
	lines := []string{fmt.Sprintf("repository %s: area from run %s: %s", now.Key, runID, area)}
	before := map[string]reverseFile{}
	for _, file := range earlier.Files {
		before[file.Path] = file
	}
	current := map[string]bool{}
	var changed, missing, added []string
	for _, file := range now.Files {
		current[file.Path] = true
		if old, ok := before[file.Path]; !ok {
			added = append(added, file.Path)
		} else if old.SHA256 != file.SHA256 || old.Size != file.Size {
			changed = append(changed, file.Path)
		}
	}
	for _, file := range earlier.Files {
		if !current[file.Path] {
			missing = append(missing, file.Path)
		}
	}
	total := len(changed) + len(missing) + len(added)
	if total == 0 {
		return append(lines, fmt.Sprintf("repository %s: no file differs from run %s", now.Key, runID))
	}
	verb := "files differ"
	if total == 1 {
		verb = "file differs"
	}
	detail := fmt.Sprintf("repository %s: %d %s from run %s: %d changed, %d missing, %d new", now.Key, total, verb, runID, len(changed), len(missing), len(added))
	for _, group := range []struct {
		state string
		paths []string
	}{{"changed", changed}, {"missing", missing}, {"new", added}} {
		sort.Strings(group.paths)
		for _, path := range group.paths {
			detail += "\n  " + group.state + " " + path
		}
	}
	return append(lines, detail)
}
