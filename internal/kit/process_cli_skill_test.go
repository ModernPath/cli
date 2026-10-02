package kit

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The tooling skill is the one place the store-backed verb sequences live
// (tooling self-sufficiency plan, C1/C2, USER:2026-09-12). Six retrospectives
// showed the failure it exists to prevent: every session reverse-engineered
// the same sequence from server code, and the memories that recorded it went
// stale the day a verb changed. These checks keep the skill honest against the
// binary: every loop verb is named, every phase name is the one the CLI
// accepts, and every refusal the glossary attributes to the CLI is a string
// the CLI actually prints.

const processCliSkill = "assets/skills/mp-process-cli/SKILL.md"

func TestProcessCliSkillInstallsInBothModes(t *testing.T) {
	for _, storeBacked := range []bool{false, true} {
		root := t.TempDir()
		if storeBacked {
			if err := os.MkdirAll(filepath.Join(root, "process"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "process", "store-backed.md"), []byte("# store-backed\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := Install(root); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "mp-process-cli", "SKILL.md")); err != nil {
			t.Fatalf("store-backed=%v: the tooling skill is not installed: %v", storeBacked, err)
		}
		if Withheld(root, processCliSkill) {
			t.Fatalf("store-backed=%v: the tooling skill must never be withheld — its store-backed sections are marked", storeBacked)
		}
	}
}

func TestProcessCliSkillNamesEveryLoopVerb(t *testing.T) {
	body, err := os.ReadFile(processCliSkill)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, verb := range []string{
		"author requirement", "author epic", "author update", "author member",
		"author relate", "author trace", "author gate", "author gate-withdraw",
		"author advance", "author demote",
		"working-set select", "working-set pull", "working-set push", "working-set check",
		"process next", "process check", "process reconcile",
		"process findings add", "process findings list", "process findings disposition",
		"factory evidence", "factory answer", "factory gates", "factory status",
		"your-move",
		// the flags the sequences hinge on
		"--for-review", "--piece", "--prerequisite", "--gate-fingerprint",
		"--gate-answer", "--expected-fingerprint", "--role RED", "--put-down",
		"--replaces", "--aggregate", "-v",
		// the retire-and-reopen recipe and the piece-holding it relies on
		"--supersedes", "--suspend", "--resume", "--waiting-on",
		// REQ-CROSS-448: the batch verbs each phase block runs, with the
		// single-record verbs above kept as the fallback
		"author apply --file", "process review record --file", "process enter",
		"author advance --gate", "factory evidence --file", "process advance --all",
		"process findings add --file", "process findings disposition --file",
		"REVIEW.md",
	} {
		if !strings.Contains(s, verb) {
			t.Errorf("the tooling skill never names %q", verb)
		}
	}
	for _, phase := range []string{"source", "plan", "cold_review", "entry", "build", "verify", "completion", "triage"} {
		if !strings.Contains(s, "`"+phase+"`") {
			t.Errorf("the tooling skill never names the phase %q the CLI accepts", phase)
		}
	}
	for _, section := range []string{
		"## 1. Plan → cold review → entry",
		"## 2. Build → evidence → completion → apply",
		"## 3. The work-selection model",
		"## 4. The fingerprint model",
		"## 5. Refusal glossary",
		"## 6. Traps",
	} {
		if !strings.Contains(s, section) {
			t.Errorf("the tooling skill lost its section %q", section)
		}
	}
}

// Every glossary row attributed to the CLI must quote a substring of a string
// the CLI prints. The cmd package is read as source so the check needs no
// build tag or binary; a row attributed to the server is out of this check's
// reach and is left to the server's own tests.
func TestProcessCliSkillGlossaryRowsExistInTheBinary(t *testing.T) {
	body, err := os.ReadFile(processCliSkill)
	if err != nil {
		t.Fatal(err)
	}
	var sources strings.Builder
	matches, err := filepath.Glob(filepath.Join("..", "..", "cmd", "*.go"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no cmd sources found: %v", err)
	}
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sources.Write(src)
		sources.WriteByte('\n')
	}
	// Go sources carry the strings escaped; match on the unescaped form the
	// binary prints by unescaping the common escapes the messages use.
	src := strings.NewReplacer(`\"`, `"`, "\\`", "`", `\n`, "\n").Replace(sources.String())
	// Format verbs stand in for values; the skill writes the value as … or a
	// placeholder, so compare the literal head of each row up to its first
	// ellipsis, placeholder, or format verb.
	row := regexp.MustCompile("(?m)^\\| `([^|]+)` \\| cli \\|")
	checked := 0
	for _, m := range row.FindAllStringSubmatch(string(body), -1) {
		literal := strings.ReplaceAll(m[1], "\\`", "`")
		head := literal
		for _, cut := range []string{" …", "…", " <", " %"} {
			if i := strings.Index(head, cut); i >= 0 {
				head = head[:i]
			}
		}
		head = strings.TrimSpace(head)
		if len(head) < 12 {
			t.Errorf("glossary row %q is too short to check against the binary", literal)
			continue
		}
		checked++
		if !strings.Contains(src, head) {
			t.Errorf("glossary row %q (checked as %q) is not a string the CLI prints", literal, head)
		}
	}
	if checked < 10 {
		t.Fatalf("only %d cli glossary rows checked — the regexp or the table shape drifted", checked)
	}
}

// Five stuck completion gates on the production store (RUN:2026-09-13) were
// each caused by something the skill did not say: the UR is a named member of
// the completion gate but the epic pull does not list it; an unapprovable gate
// is retired by superseding it at the current aggregate; a scalar field over
// 255 characters is an empty 500; the findings category refusal names no
// vocabulary; a process repin moves no aggregate (REQ-CROSS-412). These markers
// keep those lessons in the skill rather than in one session's memory.
func TestProcessCliSkillCarriesTheCompletionGateLessons(t *testing.T) {
	body, err := os.ReadFile(processCliSkill)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, marker := range []string{
		// the UR is a named item of the completion gate — evidence, scope, apply
		"and its user requirement `UR-<epic>`",
		"lists only the SRs under\n    **Members**",
		"omits\n    members not yet DONE",
		"then the UR (also\n    `--kind requirement`), then the epic",
		// retire-and-reopen
		"### Retiring a gate that cannot be approved",
		"--supersedes COMPLETE-<scope>",
		"`superseded` in `factory gates <id>`",
		"no route derived — entry_origin_unavailable",
		// the caps
		"**255 characters** per bounded authoring field",
		"`should be at most 255 character(s)`",
		// the findings vocabulary, material six first
		"`correctness`, `security`, `data_loss`, `contract`, `traceability`, `testability`, `feasibility`, `scope`, `other`",
		// the stale-stamp and process-revision traps
		"`canonical_sections_incomplete`",
		"**A process repin moves no aggregate.**",
		"`full process_revision:`",
		// the several-pieces refusal, with its remedy
		"name one with ?scope=<id>` | server | an unscoped scope read or write under several pieces | `--piece <scope>`",
		// the nested-binding trap
		"carries its own `.modernpath/config.json`",
	} {
		if !strings.Contains(s, marker) {
			t.Errorf("the tooling skill lost the completion-gate lesson %q", marker)
		}
	}
}

// SR-RDD-ONBOARD-012: the agent operates the authorization, so the skill is
// where it learns to build it from the CLI's own outputs and what to show the
// person first. A customer's agent stopped at authorize because the only
// instruction was a prose list of fields for a hand-written file.
func TestProcessCliSkillGuidesTheAuthorization(t *testing.T) {
	body, err := os.ReadFile(processCliSkill)
	if err != nil {
		t.Fatal(err)
	}
	_, section, found := strings.Cut(string(body), "## Reverse-engineering onboarding (store-backed)")
	if !found {
		t.Fatal("the tooling skill lost its reverse-engineering onboarding section")
	}
	if next := strings.Index(section, "\n## "); next >= 0 {
		section = section[:next]
	}
	// Markers are matched on the words, not on where the lines wrap.
	section = strings.Join(strings.Fields(section), " ")
	// The first text let the agent drop the documents on its own.
	if old := "the person and authorize again with `--documents none`"; strings.Contains(section, old) {
		t.Errorf("the onboarding sequence again lets the agent drop the documents without asking: %q", old)
	}
	for _, marker := range []string{
		"authorize --inventory", "--preflight", "--documents all", "--documents none",
		"Before authorizing, show the person",
		// the recommendation, and the two preflight fields it is read from
		"recommended_mode", "documents_truncated", "Recommend",
		"stale_corpus", "document_not_authorized", "document_snapshot_too_large",
		// the saved files are what the person approves: no second run
		"do not run `inventory` or `preflight` again",
		// the commit and clean state the agent is asked to show
		"`repositories[].revision`",
		// one question; the agent proposes the run name
		"You propose the run name",
		// after a refusal the agent asks again when what the person saw has
		// changed, and always before it drops the documents
		// (USER:2026-10-02:authorize-refusal-ask-when-changed)
		"requirement_count",
		"show the difference and ask again",
		"only after they agree",
		"Ask once", "If nothing differs", "same run name",
		// second independent review: one inventory file for all repositories
		"one run has one inventory file",
		// the authorize response holds the whole run; it goes to a file
		"> run.json", "`data.id`",
		// a truncated document list leaves one choice, and it is still asked
		"offer `none` only",
		// the comparison is made between two files, not from memory
		"preflight-new.json", "except after a refusal", "compare the four fields",
		// the error text names the mechanism; any other or repeated refusal stops
		"is the mechanism, not permission",
		"stop and show the person the error",
		// PowerShell's own redirect re-encodes the output; cmd saves the bytes
		// (USER:2026-10-02:authorize-powershell-line-now-flag-later)
		"In PowerShell",
		"cmd /c \"modernpath reverse-engineer preflight > preflight.json\"",
		"was re-encoded when it was saved",
	} {
		if !strings.Contains(section, marker) {
			t.Errorf("the onboarding sequence does not guide the authorization: %q is missing", marker)
		}
	}
}
