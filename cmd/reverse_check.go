package cmd

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

const reverseCheckHelp = `Check staged reverse-engineer record files before recording them.

Reads every *.json file of each --dir in lexical order as one set in the
reverse-engineer record input shape. It needs no sign-in and no bound System.
<dir>/recorded/<name>.receipt is the receipt of <dir>/<name>, and
<dir>/recorded/<name> the copy kept when it was recorded (record --dir writes
both). An entry with only kind and id is a reference to an existing
requirement: it is left out of the content checks and counts as present.

Checks that fail:
  ids                   a requirement or criterion without a UUID; one UUID
                        with two different contents in the set
  receipts              an empty or unparsable receipt; a receipt without its copy
  changed-after-record  a recorded file that differs from its copy: make a new
                        file with new UUIDs instead of editing it
  distinct-statements   two descriptions equal, exactly or once their title
                        words are removed: a sentence template
  template-scenarios    one criterion frame (its text without the title words)
                        shared by more than three requirements
  actor-outcome         a user requirement with no actor or intended_use, a
                        catch-all actor, or several roles joined by "or" or "/"
  field-limits          an actor, context or context_name over 255 characters
Warnings:
  ur-without-sr         a user requirement no system requirement in the set
                        serves through a derives trace
  trace-endpoints       a trace endpoint UUID not in the set
--served-by-store takes a UUID, or @file with one UUID per line, that already
exists in the store; it silences both warnings for that UUID.

With --repo key=dir the command also reports coverage: the tracked files below
dir (git ls-files, paths relative to the repository top level) that an
implements or verifies trace in the set names (linked), that an --exclude glob
removes (excluded), or neither (unresolved); system requirements without a
verifies trace; and user requirements without a serving system requirement.
Coverage is descriptive and never changes the exit code.

The store does not enforce this check. Any failing check exits non-zero.
--self-test runs every check on an embedded known-bad set and a good set.`

//go:embed reverse_check_known_bad
var reverseCheckFixtures embed.FS

const reverseCheckFieldLimit = 255

func reverseCheckCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "check",
		Short: "Check staged record files for content problems and coverage, offline",
		Long:  reverseCheckHelp,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dirs, _ := cmd.Flags().GetStringArray("dir")
			selfTest, _ := cmd.Flags().GetBool("self-test")
			asJSON, _ := cmd.Flags().GetBool("json")
			if selfTest {
				if len(dirs) > 0 {
					return fmt.Errorf("--self-test runs on the embedded sets; leave out --dir")
				}
				return reverseCheckSelfTest(cmd)
			}
			if len(dirs) == 0 {
				return fmt.Errorf("give one or more --dir folders of staged record files, or --self-test")
			}
			served, err := reverseCheckServed(cmd)
			if err != nil {
				return err
			}
			var set reCheckSet
			for _, dir := range dirs {
				if err := set.load(os.DirFS(dir), dir); err != nil {
					return err
				}
			}
			report := reCheckReport{Checks: set.run(served)}
			repos, _ := cmd.Flags().GetStringArray("repo")
			if len(repos) > 0 {
				excludes, _ := cmd.Flags().GetStringArray("exclude")
				if err := report.addCoverage(&set, repos, excludes, served); err != nil {
					return err
				}
			}
			if asJSON {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(report); err != nil {
					return err
				}
			} else {
				report.print(cmd)
			}
			if failed := report.failed(); len(failed) > 0 {
				return fmt.Errorf("check failed: %s", strings.Join(failed, ", "))
			}
			return nil
		},
	}
	command.Flags().StringArray("dir", nil, "folder of staged record files (repeatable)")
	command.Flags().StringArray("repo", nil, "key=dir: report coverage of the tracked files below dir for repository_key key (repeatable)")
	command.Flags().StringArray("exclude", nil, "glob of repository paths left out of coverage, ** allowed (repeatable)")
	command.Flags().StringArray("served-by-store", nil, "UUID already in the store, or @file of UUIDs (repeatable)")
	command.Flags().Bool("self-test", false, "run every check on the embedded known-bad and good sets")
	command.Flags().Bool("json", false, "emit the report as one JSON object")
	return command
}

func reverseCheckServed(cmd *cobra.Command) (map[string]bool, error) {
	values, _ := cmd.Flags().GetStringArray("served-by-store")
	served := map[string]bool{}
	for _, value := range values {
		if !strings.HasPrefix(value, "@") {
			served[strings.TrimSpace(value)] = true
			continue
		}
		body, err := os.ReadFile(strings.TrimPrefix(value, "@"))
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(string(body), "\n") {
			if id := strings.TrimSpace(line); id != "" {
				served[id] = true
			}
		}
	}
	return served, nil
}

// reCheckFile is one staged file with what record --dir kept for it.
type reCheckFile struct {
	Dir, Name    string
	Raw          []byte
	Payload      map[string]any
	ParseErr     error
	Receipt      []byte
	HasReceipt   bool
	Copy         []byte
	HasCopy      bool
	Requirements []map[string]any
	Traces       []map[string]any
}

func (f *reCheckFile) label() string { return filepath.Join(f.Dir, f.Name) }

type reCheckSet struct{ Files []*reCheckFile }

func (s *reCheckSet) load(fsys fs.FS, label string) error {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		f := &reCheckFile{Dir: label, Name: name}
		if f.Raw, err = fs.ReadFile(fsys, name); err != nil {
			return err
		}
		f.ParseErr = json.Unmarshal(f.Raw, &f.Payload)
		f.Requirements = reCheckMaps(f.Payload["requirements"])
		f.Traces = reCheckMaps(f.Payload["traces"])
		if body, err := fs.ReadFile(fsys, path.Join("recorded", name+".receipt")); err == nil {
			f.Receipt, f.HasReceipt = body, true
		}
		if body, err := fs.ReadFile(fsys, path.Join("recorded", name)); err == nil {
			f.Copy, f.HasCopy = body, true
		}
		s.Files = append(s.Files, f)
	}
	return nil
}

func reCheckMaps(value any) []map[string]any {
	list, _ := value.([]any)
	var out []map[string]any
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func reCheckString(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// reCheckIsReference reports an entry that only names an existing requirement.
func reCheckIsReference(r map[string]any) bool {
	return reCheckString(r, "id") != "" && reCheckString(r, "title") == ""
}

// reCheckCanonical renders JSON with sorted keys, so equal content compares equal.
func reCheckCanonical(value any) string {
	body, _ := json.Marshal(value)
	return string(body)
}

func reCheckNormalizedJSON(raw []byte) (string, bool) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return reCheckCanonical(value), true
}

type reCheckRequirement struct {
	File  *reCheckFile
	Entry map[string]any
}

func (r reCheckRequirement) key() string {
	if id := reCheckString(r.Entry, "id"); id != "" {
		return id
	}
	return r.File.Name + ":" + reCheckString(r.Entry, "title")
}

func (r reCheckRequirement) label() string {
	id, title := reCheckString(r.Entry, "id"), reCheckString(r.Entry, "title")
	switch {
	case id == "":
		return fmt.Sprintf("%q (%s, no id)", title, r.File.Name)
	case title == "":
		return id
	default:
		return fmt.Sprintf("%s %q", id, title)
	}
}

func (s *reCheckSet) requirements() (content, all []reCheckRequirement) {
	for _, f := range s.Files {
		for _, r := range f.Requirements {
			req := reCheckRequirement{File: f, Entry: r}
			all = append(all, req)
			if !reCheckIsReference(r) {
				content = append(content, req)
			}
		}
	}
	return content, all
}

func (s *reCheckSet) traces() []map[string]any {
	var out []map[string]any
	for _, f := range s.Files {
		out = append(out, f.Traces...)
	}
	return out
}

type reCheckFinding struct {
	Detail string `json:"detail"`
}

type reCheckResult struct {
	Name     string           `json:"name"`
	Level    string           `json:"level"`
	Status   string           `json:"status"`
	Findings []reCheckFinding `json:"findings,omitempty"`
}

func (r *reCheckResult) add(format string, args ...any) {
	r.Findings = append(r.Findings, reCheckFinding{Detail: fmt.Sprintf(format, args...)})
}

func (r *reCheckResult) settle() {
	switch {
	case len(r.Findings) == 0:
		r.Status = "PASS"
	case r.Level == "warning":
		r.Status = "WARN"
	default:
		r.Status = "FAIL"
	}
}

func (s *reCheckSet) run(served map[string]bool) []reCheckResult {
	content, all := s.requirements()
	results := []reCheckResult{
		s.checkIDs(content),
		s.checkReceipts(),
		s.checkChangedAfterRecord(),
		checkDistinctStatements(content),
		checkTemplateScenarios(content),
		checkActorOutcome(content),
		checkFieldLimits(content),
		checkURWithoutSR(content, all, s.traces(), served),
		checkTraceEndpoints(all, s.traces(), served),
	}
	for i := range results {
		results[i].settle()
	}
	return results
}

func (s *reCheckSet) checkIDs(content []reCheckRequirement) reCheckResult {
	result := reCheckResult{Name: "ids", Level: "fail"}
	for _, f := range s.Files {
		if f.ParseErr != nil {
			result.add("%s is not valid JSON: %v", f.label(), f.ParseErr)
		}
	}
	seen := map[string]string{}
	seenCriterion := map[string]string{}
	for _, r := range content {
		id := reCheckString(r.Entry, "id")
		if id == "" {
			result.add("%s has no id: give every requirement a UUID so a rerun reuses it", r.label())
		} else if !requirementUUIDPattern.MatchString(id) {
			result.add("%s is not a UUID: give every requirement a UUID so a rerun reuses it", id)
		} else if prior, ok := seen[id]; ok && prior != reCheckCanonical(r.Entry) {
			result.add("%s appears with two different contents in the set (%s)", id, r.File.label())
		} else {
			seen[id] = reCheckCanonical(r.Entry)
		}
		for _, c := range reCheckMaps(r.Entry["criteria"]) {
			cid := reCheckString(c, "id")
			if cid == "" {
				result.add("a criterion of %s has no id", r.label())
				continue
			}
			if !requirementUUIDPattern.MatchString(cid) {
				result.add("criterion %s of %s is not a UUID: give every criterion a UUID so a rerun reuses it", cid, r.label())
				continue
			}
			if prior, ok := seenCriterion[cid]; ok && prior != reCheckCanonical(c) {
				result.add("criterion %s appears with two different contents in the set (%s)", cid, r.File.label())
			} else {
				seenCriterion[cid] = reCheckCanonical(c)
			}
		}
	}
	return result
}

func (s *reCheckSet) checkReceipts() reCheckResult {
	result := reCheckResult{Name: "receipts", Level: "fail"}
	for _, f := range s.Files {
		if !f.HasReceipt {
			continue
		}
		if _, ok := reCheckNormalizedJSON(f.Receipt); !ok {
			result.add("%s has an empty or unparsable receipt", f.label())
		}
		if !f.HasCopy {
			result.add("%s has a receipt without its copy in recorded/", f.label())
		}
	}
	return result
}

func (s *reCheckSet) checkChangedAfterRecord() reCheckResult {
	result := reCheckResult{Name: "changed-after-record", Level: "fail"}
	for _, f := range s.Files {
		if !f.HasReceipt || !f.HasCopy {
			continue
		}
		if _, ok := reCheckNormalizedJSON(f.Receipt); !ok {
			continue
		}
		staged, okStaged := reCheckNormalizedJSON(f.Raw)
		copied, okCopy := reCheckNormalizedJSON(f.Copy)
		if !okStaged || !okCopy || staged != copied {
			result.add("%s changed after it was recorded: make a new file with new UUIDs", f.label())
		}
	}
	return result
}

var reCheckNonWord = regexp.MustCompile(`[^\p{L}\p{N}]+`)

func reCheckWords(text string) []string {
	return strings.Fields(reCheckNonWord.ReplaceAllString(strings.ToLower(text), " "))
}

// reCheckWithoutTitle removes the requirement's title words from text: what is
// left is the sentence frame a template repeats.
func reCheckWithoutTitle(text, title string) string {
	drop := map[string]bool{}
	for _, word := range reCheckWords(title) {
		drop[word] = true
	}
	var kept []string
	for _, word := range reCheckWords(text) {
		if !drop[word] {
			kept = append(kept, word)
		}
	}
	return strings.Join(kept, " ")
}

// reCheckGroups returns, per key, the distinct requirements sharing it when
// more than min do.
func reCheckGroups(byKey map[string][]reCheckRequirement, min int) [][]reCheckRequirement {
	var groups [][]reCheckRequirement
	for key, members := range byKey {
		if key == "" {
			continue
		}
		seen := map[string]bool{}
		var distinct []reCheckRequirement
		for _, m := range members {
			if !seen[m.key()] {
				seen[m.key()] = true
				distinct = append(distinct, m)
			}
		}
		if len(distinct) > min {
			sort.Slice(distinct, func(i, j int) bool { return distinct[i].key() < distinct[j].key() })
			groups = append(groups, distinct)
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i][0].key() < groups[j][0].key() })
	return groups
}

func reCheckLabels(group []reCheckRequirement) string {
	labels := make([]string, 0, len(group))
	for _, r := range group {
		labels = append(labels, r.label())
	}
	return strings.Join(labels, "; ")
}

func checkDistinctStatements(content []reCheckRequirement) reCheckResult {
	result := reCheckResult{Name: "distinct-statements", Level: "fail"}
	exact, framed := map[string][]reCheckRequirement{}, map[string][]reCheckRequirement{}
	for _, r := range content {
		description := reCheckString(r.Entry, "description")
		exact[strings.Join(reCheckWords(description), " ")] = append(exact[strings.Join(reCheckWords(description), " ")], r)
		frame := reCheckWithoutTitle(description, reCheckString(r.Entry, "title"))
		framed[frame] = append(framed[frame], r)
	}
	reported := map[string]bool{}
	for _, group := range reCheckGroups(exact, 1) {
		result.add("the same statement: %s", reCheckLabels(group))
		reported[reCheckLabels(group)] = true
	}
	for _, group := range reCheckGroups(framed, 1) {
		if !reported[reCheckLabels(group)] {
			result.add("the same statement once their title words are removed, a sentence template: %s", reCheckLabels(group))
		}
	}
	return result
}

func checkTemplateScenarios(content []reCheckRequirement) reCheckResult {
	result := reCheckResult{Name: "template-scenarios", Level: "fail"}
	frames := map[string][]reCheckRequirement{}
	for _, r := range content {
		title := reCheckString(r.Entry, "title")
		for _, c := range reCheckMaps(r.Entry["criteria"]) {
			text := strings.Join([]string{reCheckString(c, "given"), reCheckString(c, "when"), reCheckString(c, "then"), reCheckString(c, "statement")}, " ")
			frame := reCheckWithoutTitle(text, title)
			frames[frame] = append(frames[frame], r)
		}
	}
	for _, group := range reCheckGroups(frames, 3) {
		result.add("%d requirements share one criterion once their title words are removed: %s", len(group), reCheckLabels(group))
	}
	return result
}

// reCheckCatchAllActors name no traced caller.
var reCheckCatchAllActors = map[string]bool{
	"user": true, "users": true, "system": true, "customer": true, "customers": true,
	"operator": true, "operators": true, "admin": true, "administrator": true,
	"actor": true, "client": true, "caller": true, "end user": true, "app user": true,
}

func checkActorOutcome(content []reCheckRequirement) reCheckResult {
	result := reCheckResult{Name: "actor-outcome", Level: "fail"}
	for _, r := range content {
		if reCheckString(r.Entry, "kind") != "user_requirement" {
			continue
		}
		actor := strings.TrimSpace(reCheckString(r.Entry, "actor"))
		normal := strings.Join(reCheckWords(actor), " ")
		for _, article := range []string{"the ", "a ", "an "} {
			normal = strings.TrimPrefix(normal, article)
		}
		switch {
		case actor == "":
			result.add("%s has no actor", r.label())
		case reCheckCatchAllActors[normal]:
			result.add("%s has the catch-all actor %q: name the traced caller", r.label(), actor)
		case strings.Contains(" "+strings.ToLower(actor)+" ", " or ") || strings.Contains(actor, "/"):
			result.add("%s joins several roles in its actor %q: one user requirement per actor", r.label(), actor)
		}
		if strings.TrimSpace(reCheckString(r.Entry, "intended_use")) == "" {
			result.add("%s has no outcome (intended_use)", r.label())
		}
	}
	return result
}

func checkFieldLimits(content []reCheckRequirement) reCheckResult {
	result := reCheckResult{Name: "field-limits", Level: "fail"}
	for _, r := range content {
		for _, field := range []string{"actor", "context", "context_name"} {
			if n := utf8.RuneCountInString(reCheckString(r.Entry, field)); n > reverseCheckFieldLimit {
				result.add("%s: %s is %d characters long; the server refuses more than %d", r.label(), field, n, reverseCheckFieldLimit)
			}
		}
	}
	return result
}

// reCheckSystemIDs returns the system requirement UUIDs the set holds,
// references included.
func reCheckSystemIDs(all []reCheckRequirement) map[string]bool {
	ids := map[string]bool{}
	for _, r := range all {
		if reCheckString(r.Entry, "kind") == "system_requirement" && reCheckString(r.Entry, "id") != "" {
			ids[reCheckString(r.Entry, "id")] = true
		}
	}
	return ids
}

func reCheckURsWithoutSR(content, all []reCheckRequirement, traces []map[string]any, served map[string]bool) []reCheckRequirement {
	systems := reCheckSystemIDs(all)
	servedURs := map[string]bool{}
	for _, t := range traces {
		if reCheckString(t, "link_kind") == "derives" && reCheckString(t, "source_type") == "user_requirement" &&
			reCheckString(t, "target_type") == "system_requirement" {
			if target := reCheckString(t, "target_id"); systems[target] || served[target] {
				servedURs[reCheckString(t, "source_id")] = true
			}
		}
	}
	var out []reCheckRequirement
	for _, r := range content {
		id := reCheckString(r.Entry, "id")
		if reCheckString(r.Entry, "kind") == "user_requirement" && !servedURs[id] && !served[id] {
			out = append(out, r)
		}
	}
	return out
}

func checkURWithoutSR(content, all []reCheckRequirement, traces []map[string]any, served map[string]bool) reCheckResult {
	result := reCheckResult{Name: "ur-without-sr", Level: "warning"}
	for _, r := range reCheckURsWithoutSR(content, all, traces, served) {
		result.add("%s has no derives trace to a system requirement in the set or named by --served-by-store", r.label())
	}
	return result
}

func checkTraceEndpoints(all []reCheckRequirement, traces []map[string]any, served map[string]bool) reCheckResult {
	result := reCheckResult{Name: "trace-endpoints", Level: "warning"}
	known := map[string]bool{}
	for _, r := range all {
		if id := reCheckString(r.Entry, "id"); id != "" {
			known[id] = true
		}
		for _, c := range reCheckMaps(r.Entry["criteria"]) {
			if id := reCheckString(c, "id"); id != "" {
				known[id] = true
			}
		}
	}
	reported := map[string]bool{}
	for _, t := range traces {
		for _, side := range []string{"source", "target"} {
			id := reCheckString(t, side+"_id")
			if id == "" || reCheckString(t, side+"_type") == "source_file" || known[id] || served[id] || reported[id] {
				continue
			}
			reported[id] = true
			result.add("trace endpoint %s %s is not in the set or named by --served-by-store", reCheckString(t, side+"_type"), id)
		}
	}
	return result
}

type reCheckCoverage struct {
	Key        string   `json:"key"`
	Dir        string   `json:"dir"`
	Files      int      `json:"files"`
	Linked     int      `json:"linked"`
	Excluded   int      `json:"excluded"`
	Unresolved []string `json:"unresolved"`
	Note       string   `json:"note,omitempty"`
}

type reCheckReport struct {
	Checks         []reCheckResult   `json:"checks"`
	Coverage       []reCheckCoverage `json:"coverage,omitempty"`
	SRsWithoutTest []string          `json:"system_requirements_without_verifies_trace,omitempty"`
	URsWithoutSR   []string          `json:"user_requirements_without_system_requirement,omitempty"`
}

func (r *reCheckReport) failed() []string {
	var names []string
	for _, c := range r.Checks {
		if c.Status == "FAIL" {
			names = append(names, c.Name)
		}
	}
	return names
}

// reCheckGlob turns an --exclude glob into a pattern over repository paths:
// ** crosses directories, and **/ also matches no directory at all; * and ?
// stay inside one.
func reCheckGlob(glob string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); i++ {
		switch {
		case strings.HasPrefix(glob[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(glob[i:], "**"):
			b.WriteString(".*")
			i++
		case glob[i] == '*':
			b.WriteString("[^/]*")
		case glob[i] == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(glob[i])))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

func reCheckGitFiles(dir string) ([]string, error) {
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output(); err != nil || len(bytes.TrimSpace(out)) == 0 {
		return nil, fmt.Errorf("not inside a git checkout")
	}
	out, err := exec.Command("git", "-C", dir, "ls-files", "-z", "--full-name").Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files failed: %v", err)
	}
	var files []string
	for _, name := range strings.Split(string(out), "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}
	return files, nil
}

func (r *reCheckReport) addCoverage(set *reCheckSet, repos, excludes []string, served map[string]bool) error {
	var patterns []*regexp.Regexp
	for _, glob := range excludes {
		pattern, err := reCheckGlob(glob)
		if err != nil {
			return fmt.Errorf("--exclude %q: %v", glob, err)
		}
		patterns = append(patterns, pattern)
	}
	traces := set.traces()
	for _, spec := range repos {
		key, dir, ok := strings.Cut(spec, "=")
		if !ok || key == "" || dir == "" {
			return fmt.Errorf("--repo takes key=dir, got %q", spec)
		}
		cov := reCheckCoverage{Key: key, Dir: dir, Unresolved: []string{}}
		linked, used := map[string]bool{}, false
		for _, t := range traces {
			kind := reCheckString(t, "link_kind")
			if reCheckString(t, "repository_key") != key {
				continue
			}
			used = true
			if (kind == "implements" || kind == "verifies") && (reCheckString(t, "source_type") == "source_file" || reCheckString(t, "target_type") == "source_file") {
				linked[reCheckString(t, "path")] = true
			}
		}
		files, err := reCheckGitFiles(dir)
		if err != nil {
			cov.Note = err.Error()
			r.Coverage = append(r.Coverage, cov)
			continue
		}
		cov.Files = len(files)
		if !used {
			cov.Note = "no trace in the set uses this key"
		}
		for _, file := range files {
			excluded := false
			for _, pattern := range patterns {
				if pattern.MatchString(file) {
					excluded = true
					break
				}
			}
			switch {
			case excluded:
				cov.Excluded++
			case linked[file]:
				cov.Linked++
			default:
				cov.Unresolved = append(cov.Unresolved, file)
			}
		}
		sort.Strings(cov.Unresolved)
		r.Coverage = append(r.Coverage, cov)
	}
	content, all := set.requirements()
	// A test verifies a system requirement directly or through one of its
	// criteria, which is where required tests attach.
	verified := map[string]bool{}
	for _, t := range traces {
		if reCheckString(t, "link_kind") == "verifies" {
			verified[reCheckString(t, "target_id")] = true
		}
	}
	r.SRsWithoutTest = []string{}
	for _, req := range content {
		if reCheckString(req.Entry, "kind") != "system_requirement" {
			continue
		}
		tested := verified[reCheckString(req.Entry, "id")]
		for _, c := range reCheckMaps(req.Entry["criteria"]) {
			tested = tested || verified[reCheckString(c, "id")]
		}
		if !tested {
			r.SRsWithoutTest = append(r.SRsWithoutTest, req.label())
		}
	}
	r.URsWithoutSR = []string{}
	for _, req := range reCheckURsWithoutSR(content, all, traces, served) {
		r.URsWithoutSR = append(r.URsWithoutSR, reCheckString(req.Entry, "id"))
	}
	return nil
}

const reCheckUnresolvedShown = 50

func (r *reCheckReport) print(cmd *cobra.Command) {
	out := cmd.OutOrStdout()
	for _, c := range r.Checks {
		fmt.Fprintf(out, "%s  %s\n", c.Status, c.Name)
		for _, f := range c.Findings {
			fmt.Fprintf(out, "      %s\n", f.Detail)
		}
	}
	if r.Coverage == nil {
		return
	}
	fmt.Fprintln(out, "\nCoverage (descriptive; it does not change the exit code):")
	for _, cov := range r.Coverage {
		if cov.Note != "" && cov.Files == 0 {
			fmt.Fprintf(out, "%s: %s (%d files)\n", cov.Key, cov.Note, cov.Files)
			continue
		}
		note := ""
		if cov.Note != "" {
			note = " (" + cov.Note + ")"
		}
		fmt.Fprintf(out, "%s: %d files, %d linked, %d excluded, %d unresolved%s\n", cov.Key, cov.Files, cov.Linked, cov.Excluded, len(cov.Unresolved), note)
		for i, file := range cov.Unresolved {
			if i == reCheckUnresolvedShown {
				fmt.Fprintf(out, "  and %d more (--json lists all)\n", len(cov.Unresolved)-reCheckUnresolvedShown)
				break
			}
			fmt.Fprintf(out, "  %s\n", file)
		}
	}
	fmt.Fprintf(out, "system requirements without a verifies trace: %d\n", len(r.SRsWithoutTest))
	for _, label := range r.SRsWithoutTest {
		fmt.Fprintf(out, "  %s\n", label)
	}
	fmt.Fprintf(out, "user requirements without a serving system requirement: %d\n", len(r.URsWithoutSR))
	for _, id := range r.URsWithoutSR {
		fmt.Fprintf(out, "  %s\n", id)
	}
}

func reverseCheckSelfTest(cmd *cobra.Command) error {
	out := cmd.OutOrStdout()
	bad, err := fs.Sub(reverseCheckFixtures, "reverse_check_known_bad")
	if err != nil {
		return err
	}
	good, err := fs.Sub(bad, "good")
	if err != nil {
		return err
	}
	var badSet, goodSet reCheckSet
	if err := badSet.load(bad, "known-bad"); err != nil {
		return err
	}
	if err := goodSet.load(good, "good"); err != nil {
		return err
	}
	var problems []string
	for _, c := range badSet.run(map[string]bool{}) {
		if c.Status == "PASS" {
			fmt.Fprintf(out, "MISSED    %s\n", c.Name)
			problems = append(problems, "missed "+c.Name)
		} else {
			fmt.Fprintf(out, "DETECTED  %s\n", c.Name)
			for _, f := range c.Findings {
				fmt.Fprintf(out, "          %s\n", f.Detail)
			}
		}
	}
	for _, c := range goodSet.run(map[string]bool{}) {
		if c.Status != "PASS" {
			fmt.Fprintf(out, "GOOD SET  %s %s\n", c.Status, c.Name)
			problems = append(problems, "good set "+c.Name)
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("self-test failed: %s", strings.Join(problems, ", "))
	}
	return nil
}
