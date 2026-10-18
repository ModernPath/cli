package cmd

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// reverse-engineer check runs the mechanical rows of the skill's content
// quality table over draft group files before they are published, so a
// session no longer writes its own check script for every run, and a batch of
// groups is checked together: a statement repeated across two files is found
// as surely as one repeated inside a file. It reads only the files; it makes
// no call and writes nothing.

const reverseCheckHelp = `Check draft group files before publishing them. The files have the shape
reverse-engineer publish --file takes: a JSON object whose requirements list
holds the user (kind user) and system (kind system) requirements of a group,
and its source_assessments list, when it has one.
Every file given is checked as one set, so a statement repeated across two
groups is found too. No network call is made and nothing is written.

The checks are the mechanical rows of the skill's content quality table:

  unique-ids           an external id is defined more than once in the set
  distinct-statements  two requirements share a description, or share it once
                       their own title words are removed (a sentence template)
  template-scenarios   a scenario or criterion reads the same, once its
                       requirement's title words are removed, on more than
                       three requirements
  actor-outcome        a user requirement has no actor, no outcome
                       (intended_use; publish stores no "outcome" key), or a
                       catch-all actor such as "user" or "customer or operator"
  domain-titles        a title is an identifier, a split identifier, a test
                       name or a UI control, not an outcome a user would name
  ur-sr-join           a user requirement no system requirement serves, or a
                       system requirement whose parents name no user
                       requirement in the set or in --existing-parents
  technical-srs        system requirements with no parent at all: technical
                       behavior no user outcome starts, listed so each one's
                       reason can be read (a warning)
  citation-precision   a citation names a migration, wiring, build, telemetry
                       file or a directory (a warning; it does not fail)
  assessment-reasons   a file assessment has no reason, or its reason is
                       shared by more than three files once hashes, paths
                       and numbers are removed (a generated sentence, not a
                       reading of the file). A reason in the ownership form
                       "Owned by <context>, run <run key>: ..." is exempt.
  unresolved-owned     a file of a repository the context owns
                       (--owned-repositories) is assessed unresolved: it is
                       the context's own work, not a disposition
  unresolved-remaining files assessed unresolved, per repository: remaining
                       work, never a completed assessment (a warning)

The checks do not replace reading the requirements against the code: the
skill's sample read and the independent read remain. --self-test runs the
checks against a built-in known-bad set with one seeded failure per check and
a known-good pair, and fails unless every seeded failure is found and the good
pair is clean. Exit status is 1 when a check fails; warnings do not fail.`

//go:embed reverse_check_known_bad.json
var reverseCheckKnownBad []byte

type reCheckCitation struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

type reCheckCriterion struct {
	ExternalID string `json:"external_id"`
	Given      string `json:"given"`
	When       string `json:"when"`
	Then       string `json:"then"`
	Statement  string `json:"statement"`
}

type reCheckRecord struct {
	File        string             `json:"-"`
	ExternalID  string             `json:"external_id"`
	Kind        string             `json:"kind"`
	Title       string             `json:"title"`
	Description string             `json:"description"`
	Actor       string             `json:"actor"`
	IntendedUse string             `json:"intended_use"`
	Outcome     *string            `json:"outcome"`
	Parents     []string           `json:"parent_external_ids"`
	Criteria    []reCheckCriterion `json:"criteria"`
	Citations   []reCheckCitation  `json:"source_citations"`
	Candidate   bool               `json:"candidate"`
}

type reCheckAssessment struct {
	File          string `json:"-"`
	RepositoryKey string `json:"repository_key"`
	Path          string `json:"path"`
	Outcome       string `json:"outcome"`
	Reason        string `json:"reason"`
}

func (a reCheckAssessment) id() string { return a.RepositoryKey + ":" + a.Path }

type reCheckFinding struct {
	IDs    []string `json:"ids"`
	Detail string   `json:"detail"`
}

type reCheckResult struct {
	Name     string           `json:"name"`
	Level    string           `json:"level"` // fail or warn
	Passed   bool             `json:"passed"`
	Findings []reCheckFinding `json:"findings"`
}

type reCheckReport struct {
	Files        []string        `json:"files"`
	Requirements int             `json:"requirements"`
	Assessments  int             `json:"assessments"`
	Checks       []reCheckResult `json:"checks"`
	Passed       bool            `json:"passed"`
}

func reverseCheckCommand() *cobra.Command {
	command := &cobra.Command{Use: "check", Short: "Check draft group files against the content quality rules before publishing (offline, read-only)", Long: reverseCheckHelp, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			files, _ := cmd.Flags().GetStringArray("file")
			dir, _ := cmd.Flags().GetString("dir")
			asJSON, _ := cmd.Flags().GetBool("json")
			selfTest, _ := cmd.Flags().GetBool("self-test")
			existing, _ := cmd.Flags().GetString("existing-parents")
			owned, _ := cmd.Flags().GetString("owned-repositories")
			if selfTest {
				if len(files) > 0 || dir != "" {
					return fmt.Errorf("--self-test checks the built-in known-bad set; give no --file or --dir")
				}
				return reverseCheckSelfTest(cmd.OutOrStdout())
			}
			if (len(files) > 0) == (dir != "") {
				return fmt.Errorf("give the draft groups with --file (repeatable) or --dir, not both")
			}
			if dir != "" {
				var err error
				if files, err = reverseGroupFiles(dir); err != nil {
					return err
				}
			}
			parents, err := reverseExistingParents(existing)
			if err != nil {
				return err
			}
			report, err := reverseCheckFiles(files, parents, reverseOwnedRepositories(owned))
			if err != nil {
				return err
			}
			return reverseCheckOutput(cmd.OutOrStdout(), report, asJSON)
		}}
	command.Flags().StringArray("file", nil, "draft group JSON file; repeat for several")
	command.Flags().String("dir", "", "folder of draft group JSON files (*.json, checked together)")
	command.Flags().String("existing-parents", "", "ids of already-published requirements this set may name as parents: a comma-separated list, or a file of ids")
	command.Flags().String("owned-repositories", "", "repository keys the context owns, comma-separated: an unresolved assessment of their files fails")
	command.Flags().Bool("json", false, "print the report as JSON")
	command.Flags().Bool("self-test", false, "run the checks against the built-in known-bad set and confirm each seeded failure is found")
	return command
}

// reverseGroupFiles lists a folder's *.json files in lexical order.
func reverseGroupFiles(dir string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s holds no *.json group files", dir)
	}
	sort.Strings(files)
	return files, nil
}

// reverseExistingParents reads --existing-parents: a file of ids when the
// value names one, otherwise a comma-separated list.
func reverseExistingParents(value string) (map[string]bool, error) {
	ids := map[string]bool{}
	if strings.TrimSpace(value) == "" {
		return ids, nil
	}
	text := value
	if info, err := os.Stat(value); err == nil && !info.IsDir() {
		body, err := os.ReadFile(value)
		if err != nil {
			return nil, err
		}
		text = string(body)
	}
	for _, id := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' || r == '\r' }) {
		ids[id] = true
	}
	return ids, nil
}

// reverseOwnedRepositories reads --owned-repositories, a comma-separated list.
func reverseOwnedRepositories(value string) map[string]bool {
	owned := map[string]bool{}
	for _, key := range strings.Split(value, ",") {
		if key = strings.TrimSpace(key); key != "" {
			owned[key] = true
		}
	}
	return owned
}

func reverseCheckFiles(files []string, existing, owned map[string]bool) (reCheckReport, error) {
	var records []reCheckRecord
	var assessments []reCheckAssessment
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			return reCheckReport{}, err
		}
		group, assessed, err := reverseCheckParse(body)
		if err != nil {
			return reCheckReport{}, fmt.Errorf("%s: %w", file, err)
		}
		for i := range group {
			group[i].File = file
		}
		for i := range assessed {
			assessed[i].File = file
		}
		records = append(records, group...)
		assessments = append(assessments, assessed...)
	}
	report := reverseCheckSet(records, assessments, existing, owned)
	report.Files = files
	return report, nil
}

func reverseCheckParse(body []byte) ([]reCheckRecord, []reCheckAssessment, error) {
	var group struct {
		Requirements []reCheckRecord     `json:"requirements"`
		Assessments  []reCheckAssessment `json:"source_assessments"`
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, nil, fmt.Errorf("not a group intent object: %w", err)
	}
	_, hasRequirements := probe["requirements"]
	_, hasAssessments := probe["source_assessments"]
	if !hasRequirements && !hasAssessments {
		return nil, nil, fmt.Errorf("not a group intent: it has no requirements list")
	}
	if err := json.Unmarshal(body, &group); err != nil {
		return nil, nil, fmt.Errorf("group: %w", err)
	}
	return group.Requirements, group.Assessments, nil
}

func reverseCheckRecords(records []reCheckRecord, existing map[string]bool) reCheckReport {
	return reverseCheckSet(records, nil, existing, map[string]bool{})
}

func reverseCheckSet(records []reCheckRecord, assessments []reCheckAssessment, existing, owned map[string]bool) reCheckReport {
	checks := []reCheckResult{
		checkUniqueIDs(records),
		checkDistinctStatements(records),
		checkTemplateScenarios(records),
		checkActorOutcome(records),
		checkDomainTitles(records),
		checkURSRJoin(records, existing),
		checkTechnicalSRs(records),
		checkCitationPrecision(records),
		checkAssessmentReasons(assessments),
		checkUnresolvedOwned(assessments, owned),
		checkUnresolvedRemaining(assessments),
	}
	passed := true
	for i := range checks {
		checks[i].Passed = len(checks[i].Findings) == 0
		if checks[i].Level == "fail" && !checks[i].Passed {
			passed = false
		}
	}
	return reCheckReport{Requirements: len(records), Assessments: len(assessments), Checks: checks, Passed: passed}
}

var reCheckNonWord = regexp.MustCompile(`[^a-z0-9]+`)

// reCheckWords lowercases text and splits it into words.
func reCheckWords(text string) []string {
	return strings.Fields(reCheckNonWord.ReplaceAllString(strings.ToLower(text), " "))
}

func reCheckNormal(text string) string { return strings.Join(reCheckWords(text), " ") }

// reCheckWithoutTitle removes the requirement's own title words from text: what
// is left is the sentence frame, which a template repeats.
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

// reCheckGroups returns each key's ids when more than min records share it,
// in a stable order.
func reCheckGroups(byKey map[string][]string, min int) [][]string {
	var groups [][]string
	for key, ids := range byKey {
		if key == "" {
			continue
		}
		ids = reCheckUnique(ids)
		if len(ids) > min {
			groups = append(groups, ids)
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i][0] < groups[j][0] })
	return groups
}

func reCheckUnique(ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func checkUniqueIDs(records []reCheckRecord) reCheckResult {
	result := reCheckResult{Name: "unique-ids", Level: "fail"}
	files := map[string][]string{}
	var order []string
	for _, r := range records {
		if _, seen := files[r.ExternalID]; !seen {
			order = append(order, r.ExternalID)
		}
		files[r.ExternalID] = append(files[r.ExternalID], filepath.Base(r.File))
	}
	for _, id := range order {
		if len(files[id]) > 1 {
			result.Findings = append(result.Findings, reCheckFinding{IDs: []string{id}, Detail: fmt.Sprintf("defined %d times (%s): one id, one requirement", len(files[id]), strings.Join(files[id], ", "))})
		}
	}
	return result
}

func checkDistinctStatements(records []reCheckRecord) reCheckResult {
	result := reCheckResult{Name: "distinct-statements", Level: "fail"}
	exact, framed := map[string][]string{}, map[string][]string{}
	for _, r := range records {
		exact[reCheckNormal(r.Description)] = append(exact[reCheckNormal(r.Description)], r.ExternalID)
		frame := reCheckWithoutTitle(r.Description, r.Title)
		framed[frame] = append(framed[frame], r.ExternalID)
	}
	reported := map[string]bool{}
	for _, ids := range reCheckGroups(exact, 1) {
		result.Findings = append(result.Findings, reCheckFinding{IDs: ids, Detail: "the same statement"})
		reported[strings.Join(ids, ",")] = true
	}
	for _, ids := range reCheckGroups(framed, 1) {
		if !reported[strings.Join(ids, ",")] {
			result.Findings = append(result.Findings, reCheckFinding{IDs: ids, Detail: "the same statement once their title words are removed: a sentence template"})
		}
	}
	return result
}

func checkTemplateScenarios(records []reCheckRecord) reCheckResult {
	result := reCheckResult{Name: "template-scenarios", Level: "fail"}
	frames := map[string][]string{}
	for _, r := range records {
		for _, c := range r.Criteria {
			text := strings.Join([]string{c.Given, c.When, c.Then, c.Statement}, " ")
			frame := reCheckWithoutTitle(text, r.Title)
			frames[frame] = append(frames[frame], r.ExternalID)
		}
	}
	for _, ids := range reCheckGroups(frames, 3) {
		result.Findings = append(result.Findings, reCheckFinding{IDs: ids, Detail: fmt.Sprintf("%d requirements share one scenario once their title words are removed", len(ids))})
	}
	return result
}

// reCheckCatchAllActors are actors that name no traced caller.
var reCheckCatchAllActors = map[string]bool{
	"user": true, "users": true, "system": true, "customer": true, "customers": true,
	"operator": true, "operators": true, "admin": true, "administrator": true,
	"actor": true, "client": true, "caller": true, "end user": true, "app user": true,
}

func checkActorOutcome(records []reCheckRecord) reCheckResult {
	result := reCheckResult{Name: "actor-outcome", Level: "fail"}
	for _, r := range records {
		if r.Kind != "user" {
			continue
		}
		actor := strings.TrimSpace(r.Actor)
		normal := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(reCheckNormal(actor), "the "), "a "), "an ")
		switch {
		case actor == "":
			result.Findings = append(result.Findings, reCheckFinding{IDs: []string{r.ExternalID}, Detail: "no actor"})
		case reCheckCatchAllActors[normal]:
			result.Findings = append(result.Findings, reCheckFinding{IDs: []string{r.ExternalID}, Detail: fmt.Sprintf("catch-all actor %q: name the traced caller", actor)})
		case strings.Contains(" "+strings.ToLower(actor)+" ", " or ") || strings.Contains(actor, "/"):
			result.Findings = append(result.Findings, reCheckFinding{IDs: []string{r.ExternalID}, Detail: fmt.Sprintf("actor %q joins several roles: one UR per actor", actor)})
		}
		if strings.TrimSpace(r.IntendedUse) == "" {
			detail := "no outcome (intended_use)"
			if r.Outcome != nil {
				detail = `outcome is under "outcome", which publish does not store: move it to intended_use`
			}
			result.Findings = append(result.Findings, reCheckFinding{IDs: []string{r.ExternalID}, Detail: detail})
		}
	}
	return result
}

var (
	reCheckFileExt      = regexp.MustCompile(`(?i)\.(cs|kt|kts|swift|java|ts|tsx|js|jsx|py|ex|exs|go|xml|json|ya?ml|sql|m|h)\b`)
	reCheckSnake        = regexp.MustCompile(`\b[A-Za-z0-9]+_[A-Za-z0-9_]+\b`)
	reCheckCodeSuffixes = []string{"Command", "Query", "Handler", "Controller", "Service", "ViewModel", "Fragment", "Activity", "Screen", "Processor", "Repository", "Function", "Async", "Dto", "Request", "Response", "Client", "Manager", "Provider", "Helper", "Impl"}
	reCheckUIWords      = []string{"Button", "Btn", "Fragment", "ViewController", "ViewModel", "TextField", "Checkbox", "Dropdown"}
	reCheckIdentifier   = regexp.MustCompile(`^(?:[A-Z][a-z0-9]+){2,}$|^[a-z]{2,}[a-z0-9]*(?:[A-Z][a-z0-9]+)+$`)
)

func checkDomainTitles(records []reCheckRecord) reCheckResult {
	result := reCheckResult{Name: "domain-titles", Level: "fail"}
	for _, r := range records {
		if why := reCheckTitleProblem(r.Title); why != "" {
			result.Findings = append(result.Findings, reCheckFinding{IDs: []string{r.ExternalID}, Detail: fmt.Sprintf("title %q %s", r.Title, why)})
		}
	}
	return result
}

func reCheckTitleProblem(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "is empty"
	}
	lower := strings.ToLower(title)
	switch {
	case strings.Contains(title, "()"):
		return "names a call"
	case reCheckFileExt.MatchString(title):
		return "names a file"
	case reCheckSnake.MatchString(title):
		return "holds a snake_case identifier"
	case strings.HasPrefix(lower, "test") || strings.HasPrefix(lower, "should "):
		return "is a test name"
	}
	words := strings.Fields(title)
	for _, word := range words {
		word = strings.Trim(word, ".,:;'\"")
		for _, ui := range reCheckUIWords {
			if strings.HasSuffix(word, ui) {
				return "names a UI control or class, not an outcome"
			}
		}
		if reCheckIdentifier.MatchString(word) {
			for _, suffix := range reCheckCodeSuffixes {
				if strings.HasSuffix(word, suffix) {
					return "names a code identifier"
				}
			}
		}
	}
	if len(words) == 1 && reCheckIdentifier.MatchString(words[0]) {
		return "is an identifier"
	}
	// A split identifier: every word capitalized and the last a code suffix,
	// such as "Start Session Command".
	if len(words) >= 2 {
		capitalized := true
		for _, word := range words {
			if word[0] < 'A' || word[0] > 'Z' {
				capitalized = false
			}
		}
		last := words[len(words)-1]
		for _, suffix := range reCheckCodeSuffixes {
			if capitalized && last == suffix {
				return "is a split identifier"
			}
		}
	}
	return ""
}

func checkURSRJoin(records []reCheckRecord, existing map[string]bool) reCheckResult {
	result := reCheckResult{Name: "ur-sr-join", Level: "fail"}
	urs, served := map[string]bool{}, map[string]bool{}
	for _, r := range records {
		if r.Kind == "user" {
			urs[r.ExternalID] = true
		}
	}
	for _, r := range records {
		// A system requirement with no parent is technical; checkTechnicalSRs lists it.
		if r.Kind != "system" || len(r.Parents) == 0 {
			continue
		}
		named := false
		for _, parent := range r.Parents {
			served[parent] = true
			if urs[parent] || existing[parent] {
				named = true
			}
		}
		if !named {
			detail := fmt.Sprintf("its parents %s are neither in this set nor in --existing-parents", strings.Join(r.Parents, ", "))
			result.Findings = append(result.Findings, reCheckFinding{IDs: []string{r.ExternalID}, Detail: detail})
		}
	}
	var unserved []string
	for id := range urs {
		if !served[id] && !existing[id] {
			unserved = append(unserved, id)
		}
	}
	sort.Strings(unserved)
	for _, id := range unserved {
		result.Findings = append(result.Findings, reCheckFinding{IDs: []string{id}, Detail: "no system requirement serves it"})
	}
	return result
}

func checkTechnicalSRs(records []reCheckRecord) reCheckResult {
	result := reCheckResult{Name: "technical-srs", Level: "warn"}
	for _, r := range records {
		if r.Kind == "system" && len(r.Parents) == 0 {
			result.Findings = append(result.Findings, reCheckFinding{IDs: []string{r.ExternalID}, Detail: "has no parent user requirement: its description says why no user outcome starts it"})
		}
	}
	return result
}

var (
	reCheckImpreciseFile = regexp.MustCompile(`(?i)(^|/)(migrations?|db/migrate)/|(^|/)(program\.cs|startup\.cs|appdelegate\.swift|scenedelegate\.swift|package\.json|podfile|build\.gradle(\.kts)?|settings\.gradle(\.kts)?|di\.kt|.*module\.kt|.*\.csproj|.*\.sln|.*\.xcodeproj)$|(telemetry|logging|logger|applicationinsights|serilog|instrumentation)`)
	reCheckExtensionless = map[string]bool{"dockerfile": true, "makefile": true, "jenkinsfile": true, "procfile": true, "gemfile": true}
)

func checkCitationPrecision(records []reCheckRecord) reCheckResult {
	result := reCheckResult{Name: "citation-precision", Level: "warn"}
	for _, r := range records {
		for _, c := range r.Citations {
			path := strings.TrimSpace(c.Path)
			if path == "" {
				continue
			}
			base := filepath.Base(path)
			switch {
			case strings.HasSuffix(path, "/") || (!strings.Contains(base, ".") && !reCheckExtensionless[strings.ToLower(base)]):
				result.Findings = append(result.Findings, reCheckFinding{IDs: []string{r.ExternalID}, Detail: fmt.Sprintf("cites a directory %s: cite the files that implement the behavior", path)})
			case reCheckImpreciseFile.MatchString(path):
				result.Findings = append(result.Findings, reCheckFinding{IDs: []string{r.ExternalID}, Detail: fmt.Sprintf("cites %s, a migration, wiring, build or telemetry file: confirm it implements the stated behavior", path)})
			}
		}
	}
	return result
}

// reCheckOwnership is the ownership form of a reason the skill names:
// "Owned by <context code>, run <run key>: ..." for a file another context
// owns and none of this context's requirements cites.
var reCheckOwnership = regexp.MustCompile(`^Owned by [A-Za-z0-9_-]+, run [A-Za-z0-9._-]+:`)

var (
	reCheckHex  = regexp.MustCompile(`(?i)\b[0-9a-f]{7,}\b`)
	reCheckPath = regexp.MustCompile(`\S*[/\\]\S*|\S+\.[A-Za-z0-9]{1,6}\b`)
	reCheckNum  = regexp.MustCompile(`\b\d+\b`)
)

// reCheckReasonShape is a reason with what differs from file to file removed:
// digests, paths, file names, numbers and the words of the file's own path.
// Two files whose reasons share a shape got one generated sentence.
func reCheckReasonShape(a reCheckAssessment) string {
	text := reCheckHex.ReplaceAllString(a.Reason, " ")
	text = reCheckPath.ReplaceAllString(text, " ")
	text = reCheckNum.ReplaceAllString(text, " ")
	own := map[string]bool{}
	for _, word := range reCheckWords(a.Path) {
		own[word] = true
	}
	var kept []string
	for _, word := range reCheckWords(text) {
		if !own[word] {
			kept = append(kept, word)
		}
	}
	return strings.Join(kept, " ")
}

func checkAssessmentReasons(assessments []reCheckAssessment) reCheckResult {
	result := reCheckResult{Name: "assessment-reasons", Level: "fail"}
	byShape := map[string][]string{}
	for _, a := range assessments {
		if strings.TrimSpace(a.Reason) == "" {
			result.Findings = append(result.Findings, reCheckFinding{IDs: []string{a.id()}, Detail: "the assessment has no reason"})
			continue
		}
		if reCheckOwnership.MatchString(a.Reason) {
			// An ownership assessment repeats by design: the owning context's
			// run reads the file and gives it its own disposition.
			continue
		}
		shape := reCheckReasonShape(a)
		if shape == "" {
			result.Findings = append(result.Findings, reCheckFinding{IDs: []string{a.id()}, Detail: "the reason says nothing beyond the file's path, digest or numbers"})
			continue
		}
		byShape[shape] = append(byShape[shape], a.id())
	}
	for _, ids := range reCheckGroups(byShape, 3) {
		result.Findings = append(result.Findings, reCheckFinding{IDs: ids,
			Detail: fmt.Sprintf("%d files share one reason once hashes, paths and numbers are removed; read each file and say what it is", len(ids))})
	}
	return result
}

func checkUnresolvedOwned(assessments []reCheckAssessment, owned map[string]bool) reCheckResult {
	result := reCheckResult{Name: "unresolved-owned", Level: "fail"}
	var ids []string
	for _, a := range assessments {
		if a.Outcome == "unresolved" && owned[a.RepositoryKey] {
			ids = append(ids, a.id())
		}
	}
	if len(ids) > 0 {
		result.Findings = append(result.Findings, reCheckFinding{IDs: ids,
			Detail: fmt.Sprintf("%d files of repositories this context owns are assessed unresolved: read each and link it, or assess it reviewed or unsupported", len(ids))})
	}
	return result
}

func checkUnresolvedRemaining(assessments []reCheckAssessment) reCheckResult {
	result := reCheckResult{Name: "unresolved-remaining", Level: "warn"}
	byRepository := map[string][]string{}
	for _, a := range assessments {
		if a.Outcome == "unresolved" {
			byRepository[a.RepositoryKey] = append(byRepository[a.RepositoryKey], a.id())
		}
	}
	keys := make([]string, 0, len(byRepository))
	for key := range byRepository {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result.Findings = append(result.Findings, reCheckFinding{IDs: byRepository[key],
			Detail: fmt.Sprintf("%s: %d files assessed unresolved are remaining work; report them with the unassessed files, never as assessed", key, len(byRepository[key]))})
	}
	return result
}

func reverseCheckOutput(w io.Writer, report reCheckReport, asJSON bool) error {
	if asJSON {
		if err := json.NewEncoder(w).Encode(report); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(w, "%d requirements in %d files\n\n", report.Requirements, len(report.Files))
		fmt.Fprintf(w, "%-20s  %-6s  %s\n", "CHECK", "RESULT", "FINDINGS")
		for _, check := range report.Checks {
			fmt.Fprintf(w, "%-20s  %-6s  %d\n", check.Name, reCheckVerdict(check), len(check.Findings))
		}
		for _, check := range report.Checks {
			if len(check.Findings) == 0 {
				continue
			}
			fmt.Fprintf(w, "\n%s (%s):\n", check.Name, reCheckVerdict(check))
			for _, finding := range check.Findings {
				fmt.Fprintf(w, "  - %s: %s\n", strings.Join(finding.IDs, ", "), finding.Detail)
			}
		}
	}
	if !report.Passed {
		var failed []string
		for _, check := range report.Checks {
			if check.Level == "fail" && !check.Passed {
				failed = append(failed, check.Name)
			}
		}
		return fmt.Errorf("content check failed: %s", strings.Join(failed, ", "))
	}
	return nil
}

func reCheckVerdict(check reCheckResult) string {
	switch {
	case check.Passed:
		return "PASS"
	case check.Level == "warn":
		return "WARN"
	}
	return "FAIL"
}

// reverseCheckSelfTest runs the checks over the built-in known-bad set. Each
// check must find its seeded ids, and the known-good pair must appear in no
// finding.
func reverseCheckSelfTest(w io.Writer) error {
	var fixture struct {
		Requirements []reCheckRecord     `json:"requirements"`
		Assessments  []reCheckAssessment `json:"source_assessments"`
		Owned        []string            `json:"owned_repositories"`
		Seeded       map[string][]string `json:"seeded"`
		Good         []string            `json:"good"`
	}
	if err := json.Unmarshal(reverseCheckKnownBad, &fixture); err != nil {
		return fmt.Errorf("built-in known-bad set: %w", err)
	}
	owned := map[string]bool{}
	for _, key := range fixture.Owned {
		owned[key] = true
	}
	report := reverseCheckSet(fixture.Requirements, fixture.Assessments, map[string]bool{}, owned)
	found := map[string]map[string]bool{}
	clean := true
	good := map[string]bool{}
	for _, id := range fixture.Good {
		good[id] = true
	}
	for _, check := range report.Checks {
		found[check.Name] = map[string]bool{}
		for _, finding := range check.Findings {
			for _, id := range finding.IDs {
				found[check.Name][id] = true
				if good[id] {
					clean = false
					fmt.Fprintf(w, "FALSE POSITIVE  %s flagged the known-good %s: %s\n", check.Name, id, finding.Detail)
				}
			}
		}
	}
	missed := 0
	for _, check := range report.Checks {
		seeded := fixture.Seeded[check.Name]
		var lost []string
		for _, id := range seeded {
			if !found[check.Name][id] {
				lost = append(lost, id)
			}
		}
		if len(seeded) == 0 {
			lost = append(lost, "(no seeded failure)")
		}
		if len(lost) > 0 {
			missed++
			fmt.Fprintf(w, "MISSED    %-20s  %s\n", check.Name, strings.Join(lost, ", "))
		} else {
			fmt.Fprintf(w, "DETECTED  %-20s  %s\n", check.Name, strings.Join(seeded, ", "))
		}
	}
	if missed > 0 || !clean {
		return fmt.Errorf("self-test failed: %d checks missed their seeded failure; known-good pair clean: %t", missed, clean)
	}
	fmt.Fprintln(w, "self-test passed: every seeded failure was found and the known-good pair is clean")
	return nil
}
