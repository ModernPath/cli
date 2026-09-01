package gate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	testsFieldRe = regexp.MustCompile(`^\s*-\s+\*\*Tests:\*\*`)
	citedPathRe  = regexp.MustCompile("`([A-Za-z0-9_][A-Za-z0-9_./-]*\\.(?:exs|ex|tsx|ts|js|mjs|go|py))`")
	testPathRe   = regexp.MustCompile(`(_test\.|\.test\.|\.spec\.|(^|/)test_)`)
	reqIDRe      = regexp.MustCompile(`REQ-[A-Z]+-\d+`)
)

// CheckTestTraceability — REQ-CROSS-164: a test cited as covering evidence names
// a requirement (PROCESS.md §6).
//
// The population is the one REQ-CROSS-149 settled on after four sweeps of the
// same corpus gave 12, 35, 45 and 123: a **test file** cited in a `- **Tests:**`
// field. That field is the ledger's own declaration that a file is covering
// evidence. Widening it to any cited file answers a different question and
// reports non-tests; narrowing it to "names the requirement that cites it" makes
// this bookkeeping rather than traceability.
//
// Deliberately NOT reported here: a citation that resolves to no file. That is a
// citation defect with its own fix, and reporting one fault under two rule names
// teaches the reader to skim the output.
func CheckTestTraceability(root string) ([]Violation, error) {
	dir := filepath.Join(root, "tasks")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var index map[string][]string // basename -> repo-relative paths, built once on first miss
	resolve := func(p string) string {
		if _, err := os.Stat(filepath.Join(root, p)); err == nil {
			return p
		}
		if index == nil {
			index = map[string][]string{}
			_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					if info != nil && info.IsDir() {
						switch info.Name() {
						case ".git", "node_modules", "_build", "deps", ".elixir_ls":
							return filepath.SkipDir
						}
					}
					return nil
				}
				rel, err := filepath.Rel(root, path)
				if err != nil {
					return nil
				}
				index[info.Name()] = append(index[info.Name()], rel)
				return nil
			})
		}
		for _, cand := range index[filepath.Base(p)] {
			if cand == p || strings.HasSuffix(cand, string(filepath.Separator)+p) {
				return cand
			}
		}
		return ""
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
		for _, line := range strings.Split(string(body), "\n") {
			if !testsFieldRe.MatchString(line) {
				continue
			}
			for _, m := range citedPathRe.FindAllStringSubmatch(line, -1) {
				p := m[1]
				if !testPathRe.MatchString(p) || seen[p] {
					continue
				}
				seen[p] = true
				actual := resolve(p)
				if actual == "" {
					continue // absent file — a citation defect, not this rule's
				}
				content, err := os.ReadFile(filepath.Join(root, actual))
				if err != nil || reqIDRe.Match(content) {
					continue
				}
				out = append(out, Violation{
					Rule: "untraced-test", File: rel, Subject: actual,
					Detail: fmt.Sprintf("%s is cited as covering evidence but names no requirement — PROCESS.md §6", actual),
				})
			}
		}
	}
	return out, nil
}
