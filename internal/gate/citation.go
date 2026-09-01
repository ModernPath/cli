package gate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	codeOrTestsFieldRe = regexp.MustCompile(`^\s*-\s+\*\*(?:Code|Tests):\*\*`)
	anyCitedPathRe     = regexp.MustCompile("`([A-Za-z0-9_][A-Za-z0-9_./-]*\\.(?:exs|ex|tsx|ts|js|mjs|go|py|json|yml|yaml|sh))`")
)

// CheckCitedPaths — REQ-CROSS-165: a path cited as implementing or covering a
// requirement resolves to a real file.
//
// Scoped to `- **Code:**` and `- **Tests:**` fields. That is not tidiness: a
// ledger's prose deliberately names paths that must NOT exist — a template row
// citing `domain/user.ts`, a gap stated as *"there is no x.py"*, an acceptance
// criterion about *"a new `src/organisms/Foo.tsx`"*. Every such case in the real
// corpus sits in a Statement or a GIVEN clause and never in a declaration field,
// so the field is what separates a citation from an illustration.
//
// Resolution falls back to the working tree, because gitignored files are real:
// `.modernpath/auth.json` holds credentials, is absent from `git ls-files`, and
// resolving against the index alone reported that correct citation as broken.
func CheckCitedPaths(root string) ([]Violation, error) {
	dir := filepath.Join(root, "tasks")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var byBase map[string][]string
	resolve := func(p string) bool {
		if _, err := os.Stat(filepath.Join(root, p)); err == nil {
			return true
		}
		if byBase == nil {
			byBase = map[string][]string{}
			_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if info.IsDir() {
					switch info.Name() {
					case ".git", "node_modules", "_build", "deps", ".elixir_ls", "coverage":
						return filepath.SkipDir
					}
					return nil
				}
				rel, err := filepath.Rel(root, path)
				if err == nil {
					byBase[info.Name()] = append(byBase[info.Name()], rel)
				}
				return nil
			})
		}
		for _, c := range byBase[filepath.Base(p)] {
			if c == p || strings.HasSuffix(c, string(filepath.Separator)+p) {
				return true
			}
		}
		return false
	}

	var out []Violation
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		rel := filepath.Join("tasks", e.Name())
		seen := map[string]bool{}
		for i, line := range strings.Split(string(body), "\n") {
			if !codeOrTestsFieldRe.MatchString(line) {
				continue
			}
			for _, m := range anyCitedPathRe.FindAllStringSubmatch(line, -1) {
				p := m[1]
				if seen[p] || resolve(p) {
					seen[p] = true
					continue
				}
				seen[p] = true
				out = append(out, Violation{
					Rule: "unresolved-citation", File: rel, Subject: p,
					Detail: fmt.Sprintf("line %d cites %s, which resolves to no file — a citation nobody can follow", i+1, p),
				})
			}
		}
	}
	return out, nil
}
