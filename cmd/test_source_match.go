package cmd

import (
	"fmt"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Epic 427: epics build record matches test results to the TestCases of the
// Epic's plan. The first rule that gives a TestCase wins: a test_case_id
// trait or property, a TestCase UUID in the test name, a TestCase UUID on the
// line above a test declaration at HEAD, the build handoff's tests list.

var fullUUIDPattern = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// fullUUIDs returns the lower-case full UUIDs in text. A UUID inside a longer
// run of hex digits and dashes is not a full UUID, and a shorter prefix never
// matches.
func fullUUIDs(text string) []string {
	var ids []string
	for _, at := range fullUUIDPattern.FindAllStringIndex(text, -1) {
		if at[0] > 0 && isUUIDRune(text[at[0]-1]) || at[1] < len(text) && isUUIDRune(text[at[1]]) {
			continue
		}
		ids = append(ids, strings.ToLower(text[at[0]:at[1]]))
	}
	return ids
}

func isUUIDRune(c byte) bool {
	return c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// headRepo reads files of the checkout's HEAD commit.
type headRepo struct {
	root, head string
	tracked    map[string]bool
	contents   map[string][]string
}

func newHeadRepo(root, head string) (*headRepo, error) {
	out, err := exec.Command("git", "-C", root, "ls-tree", "-r", "-z", "--name-only", "--full-tree", head).Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-tree %s failed: %v", head, err)
	}
	repo := &headRepo{root: root, head: head, tracked: map[string]bool{}, contents: map[string][]string{}}
	for _, name := range strings.Split(string(out), "\x00") {
		if name != "" {
			repo.tracked[name] = true
		}
	}
	return repo, nil
}

func (r *headRepo) lines(file string) []string {
	if lines, ok := r.contents[file]; ok {
		return lines
	}
	out, err := exec.Command("git", "-C", r.root, "show", r.head+":"+file).Output()
	var lines []string
	if err == nil {
		lines = strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
	}
	r.contents[file] = lines
	return lines
}

func (r *headRepo) text(file string) string { return strings.Join(r.lines(file), "\n") }

// testDeclaration is a test declared on the line below a TestCase UUID.
type testDeclaration struct {
	testCase, file, name string
	line                 int
}

// scanTestCaseLines finds every line at HEAD that carries one of ids: the
// files that carry each id, and the test each such line declares.
func (r *headRepo) scanTestCaseLines(ids []string) (map[string][]string, []testDeclaration, error) {
	carriers := map[string][]string{}
	if len(ids) == 0 {
		return carriers, nil, nil
	}
	args := []string{"-C", r.root, "grep", "-I", "-n", "-z", "-i", "-F"}
	for _, id := range ids {
		args = append(args, "-e", id)
	}
	args = append(args, r.head, "--")
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			return nil, nil, fmt.Errorf("git grep for TestCase UUIDs failed: %v", err)
		}
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[strings.ToLower(id)] = true
	}
	seen := map[string]bool{}
	var declarations []testDeclaration
	for _, record := range strings.Split(string(out), "\n") {
		parts := strings.SplitN(record, "\x00", 3)
		if len(parts) != 3 {
			continue
		}
		file := strings.TrimPrefix(parts[0], r.head+":")
		lineNo, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		for _, id := range fullUUIDs(parts[2]) {
			if !wanted[id] {
				continue
			}
			if !seen[id+"\x00"+file] {
				seen[id+"\x00"+file] = true
				carriers[id] = append(carriers[id], file)
			}
			if name, ok := declarationBelow(r.lines(file), lineNo-1); ok {
				declarations = append(declarations, testDeclaration{testCase: id, file: file, name: name, line: lineNo + 1})
			}
		}
	}
	for id := range carriers {
		sort.Strings(carriers[id])
	}
	return carriers, declarations, nil
}

var (
	goTestDeclaration     = regexp.MustCompile(`^func\s+(Test\w*)\s*\(`)
	exunitTestDeclaration = regexp.MustCompile(`^test\s+"((?:[^"\\]|\\.)*)"`)
	jsTestDeclaration     = regexp.MustCompile(`^(?:it|test)\(\s*"((?:[^"\\]|\\.)*)"`)
	csharpMethod          = regexp.MustCompile(`^(?:[\w<>\[\],.?]+\s+)+(\w+)\s*(?:<[^>]*>)?\s*\(`)
	csharpTestAttribute   = regexp.MustCompile(`(?:^\[|,)\s*(?:[\w.]*\.)?(?:Fact|Theory|TestMethod|Test)(?:Attribute)?\s*(?:\(|\]|,)`)
	exunitTagLine         = regexp.MustCompile(`^@(?:tag|describetag)\b`)
)

func isAttributeLine(line string) bool {
	return strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]")
}

func isCommentLine(line string) bool {
	for _, prefix := range []string{"//", "#", "/*", "*"} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// declarationBelow is the test declared on the first line below lines[at]
// that is not blank, a comment, a C# attribute or an ExUnit @tag or
// @describetag line. Anything else there, such as a describe block, declares
// no test.
func declarationBelow(lines []string, at int) (string, bool) {
	if at < 0 || at >= len(lines) {
		return "", false
	}
	testAttribute := false
	// Attributes directly above the UUID line belong to the same method.
	for j := at; j >= 0; j-- {
		line := strings.TrimSpace(lines[j])
		if isAttributeLine(line) {
			testAttribute = testAttribute || csharpTestAttribute.MatchString(line)
			continue
		}
		if j == at || isCommentLine(line) {
			continue
		}
		break
	}
	inBlock := false
	if open := strings.LastIndex(lines[at], "/*"); open >= 0 && !strings.Contains(lines[at][open+2:], "*/") {
		inBlock = true
	}
	for j := at + 1; j < len(lines); j++ {
		line := strings.TrimSpace(lines[j])
		if inBlock {
			inBlock = !strings.Contains(line, "*/")
			continue
		}
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "/*"):
			inBlock = !strings.Contains(line[2:], "*/")
			continue
		case isCommentLine(line):
			continue
		case isAttributeLine(line):
			testAttribute = testAttribute || csharpTestAttribute.MatchString(line)
			continue
		case exunitTagLine.MatchString(line):
			continue
		}
		for _, form := range []*regexp.Regexp{goTestDeclaration, exunitTestDeclaration, jsTestDeclaration} {
			if m := form.FindStringSubmatch(line); m != nil {
				return m[1], true
			}
		}
		if testAttribute {
			if m := csharpMethod.FindStringSubmatch(line); m != nil {
				return m[1], true
			}
		}
		return "", false
	}
	return "", false
}

// resultNamesDeclaration: the normalized result name equals the declared
// name or ends with a space or " > " followed by it.
func resultNamesDeclaration(normalized, declared string) bool {
	return normalized == declared || strings.HasSuffix(normalized, " "+declared) || strings.HasSuffix(normalized, " > "+declared)
}

// declarationName is the name under which result names the declaration, and
// whether it does. A TRX result that carries a method name names it only when
// that name equals the declared name, whatever its testName says: an xUnit
// DisplayName is free text (Epic 428). Any other result keeps the name rule.
func declarationName(result testResult, declared string) (string, bool) {
	if result.Format == "trx" && result.Method != "" {
		return result.Method, result.Method == declared
	}
	return result.Normalized, resultNamesDeclaration(result.Normalized, declared)
}

var (
	goModuleLine = regexp.MustCompile(`(?m)^\s*module\s+"?([^\s"]+)"?`)
	csNamespace  = regexp.MustCompile(`(?m)^\s*namespace\s+([\w.]+)`)
	csType       = regexp.MustCompile(`\b(?:class|struct|interface|record(?:\s+(?:class|struct))?)\s+(\w+)`)
	genericArity = regexp.MustCompile("`\\d+")
	elixirModule = regexp.MustCompile(`(?m)^\s*defmodule\s+([\w.]+)`)
	classDecl    = regexp.MustCompile(`\bclass\s+(\w+)`)
	packageDecl  = regexp.MustCompile(`(?m)^\s*(?:package|namespace)\s+([\w.]+)`)
)

// goImportPath is the import path of file's directory under the module
// path of the go.mod above it at HEAD.
func (r *headRepo) goImportPath(file string) (string, bool) {
	dir := path.Dir(file)
	for d := dir; ; d = path.Dir(d) {
		mod := path.Join(d, "go.mod")
		if r.tracked[mod] {
			m := goModuleLine.FindStringSubmatch(r.text(mod))
			if m == nil {
				return "", false
			}
			rel := ""
			if d != "." && dir != d {
				rel = strings.TrimPrefix(dir, d+"/")
			} else if d == "." && dir != "." {
				rel = dir
			}
			if rel == "" {
				return m[1], true
			}
			return m[1] + "/" + rel, true
		}
		if d == "." || d == "/" {
			return "", false
		}
	}
}

func submatches(pattern *regexp.Regexp, text string) []string {
	var out []string
	for _, m := range pattern.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	return out
}

// declaresDotNetType: className's namespace and type, without generic arity
// and with the last nested type, is a type declared in file.
func (r *headRepo) declaresDotNetType(file, className string) bool {
	if className == "" {
		return false
	}
	className = genericArity.ReplaceAllString(className, "")
	outer, typeName := className, ""
	if nested := strings.Split(className, "+"); len(nested) > 1 {
		outer, typeName = nested[0], nested[len(nested)-1]
	}
	namespace := ""
	if i := strings.LastIndex(outer, "."); i >= 0 {
		namespace = outer[:i]
		if typeName == "" {
			typeName = outer[i+1:]
		}
	} else if typeName == "" {
		typeName = outer
	}
	text := r.text(file)
	namespaces := submatches(csNamespace, text)
	if namespace == "" && len(namespaces) > 0 || namespace != "" && !contains(namespaces, namespace) {
		return false
	}
	return contains(submatches(csType, text), typeName)
}

// declaresModule: name, without an Elixir. prefix, is a module or class
// declared in file.
func (r *headRepo) declaresModule(file, name string) bool {
	name = strings.TrimPrefix(name, "Elixir.")
	if name == "" {
		return false
	}
	text := r.text(file)
	if contains(submatches(elixirModule, text), name) {
		return true
	}
	prefix, last := "", name
	if i := strings.LastIndex(name, "."); i >= 0 {
		prefix, last = name[:i], name[i+1:]
	}
	if !contains(submatches(classDecl, text), last) {
		return false
	}
	return prefix == "" || contains(submatches(packageDecl, text), prefix)
}

// joinResultPath is p, a path a result file names, joined to the result
// entry's root and made repository-relative.
func (r *headRepo) joinResultPath(root, p string) string {
	p = filepath.ToSlash(p)
	if path.IsAbs(p) || filepath.IsAbs(p) {
		if rel, err := filepath.Rel(r.root, filepath.FromSlash(p)); err == nil {
			return filepath.ToSlash(rel)
		}
		return p
	}
	return path.Clean(path.Join(root, p))
}

// resultFromFile: the result comes from the declaring file's own unit.
func (r *headRepo) resultFromFile(result testResult, file string) bool {
	switch result.Format {
	case "go-test-json":
		importPath, ok := r.goImportPath(file)
		return ok && result.Unit == importPath
	case "trx":
		return r.declaresDotNetType(file, result.Unit)
	case "junit":
		if result.UnitFile != "" && r.joinResultPath(result.Root, result.UnitFile) == file {
			return true
		}
		if result.Unit != "" && r.joinResultPath(result.Root, result.Unit) == file {
			return true
		}
		return r.declaresModule(file, result.Unit)
	}
	return false
}

func resultUnitKey(result testResult) string {
	if result.UnitFile != "" {
		return result.Unit + " (file " + result.UnitFile + ")"
	}
	return result.Unit
}

// handoffTestEntry is the build handoff's tests list entry.
type handoffTestEntry struct {
	TestCaseID    string   `json:"test_case_id"`
	RepositoryKey string   `json:"repository_key,omitempty"`
	File          string   `json:"file"`
	Names         []string `json:"names"`
}

// resultMatch is the TestCases each result matched and the files whose
// declarations the source rule matched, per TestCase.
type resultMatch struct {
	testCases     [][]string
	declaredFiles map[string][]string
}

func matchResults(repo *headRepo, results []testResult, declarations []testDeclaration, tests []handoffTestEntry) (resultMatch, error) {
	match := resultMatch{testCases: make([][]string, len(results)), declaredFiles: map[string][]string{}}
	for i, result := range results {
		if ids := fullUUIDs(result.TestCaseID); len(ids) > 0 {
			match.testCases[i] = []string{ids[0]}
		} else if ids := fullUUIDs(result.Name); len(ids) > 0 {
			match.testCases[i] = []string{ids[0]}
		}
	}
	early := make([]bool, len(results))
	for i := range results {
		early[i] = match.testCases[i] != nil
	}
	for _, declaration := range declarations {
		var matched []int
		units := map[string][]string{}
		var unitOrder []string
		for i, result := range results {
			if early[i] {
				continue
			}
			name, names := declarationName(result, declaration.name)
			if !names || !repo.resultFromFile(result, declaration.file) {
				continue
			}
			matched = append(matched, i)
			key := resultUnitKey(result)
			if _, ok := units[key]; !ok {
				unitOrder = append(unitOrder, key)
			}
			if !contains(units[key], name) {
				units[key] = append(units[key], name)
			}
		}
		if len(matched) == 0 {
			continue
		}
		if len(unitOrder) > 1 || len(units[unitOrder[0]]) > 1 {
			var named []string
			for _, unit := range unitOrder {
				for _, name := range units[unit] {
					named = append(named, fmt.Sprintf("%q in %s", name, unit))
				}
			}
			return resultMatch{}, fmt.Errorf("TestCase %s: the test %q declared at %s:%d matches more than one result unit or name: %s; name the result in the build handoff's tests list or the test name; nothing was sent",
				declaration.testCase, declaration.name, declaration.file, declaration.line, strings.Join(named, ", "))
		}
		for _, i := range matched {
			if !contains(match.testCases[i], declaration.testCase) {
				match.testCases[i] = append(match.testCases[i], declaration.testCase)
			}
		}
		if !contains(match.declaredFiles[declaration.testCase], declaration.file) {
			match.declaredFiles[declaration.testCase] = append(match.declaredFiles[declaration.testCase], declaration.file)
		}
	}
	for i, result := range results {
		if match.testCases[i] != nil {
			continue
		}
		for _, entry := range tests {
			if contains(entry.Names, result.Name) {
				id := strings.ToLower(entry.TestCaseID)
				if !contains(match.testCases[i], id) {
					match.testCases[i] = append(match.testCases[i], id)
				}
			}
		}
	}
	return match, nil
}

// outcomeRank orders outcomes: several results of one TestCase give the
// first present of error, fail, inconclusive, skip and pass.
var outcomeRank = map[string]int{"error": 5, "fail": 4, "inconclusive": 3, "skip": 2, "pass": 1}

func worseOutcome(current, next string) string {
	if outcomeRank[next] > outcomeRank[current] {
		return next
	}
	return current
}
