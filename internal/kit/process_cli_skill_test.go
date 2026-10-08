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
		// the authorize response holds the whole run; it goes to a file in
		// the run's sub-folder of the sweep's working folder
		"> .modernpath/reverse-engineering.runs/<folder>/run.json", "`data.id`",
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
		`cmd /c "modernpath reverse-engineer preflight > .modernpath\reverse-engineering.runs\<folder>\preflight.json"`,
		"was re-encoded when it was saved",
	} {
		if !strings.Contains(section, marker) {
			t.Errorf("the onboarding sequence does not guide the authorization: %q is missing", marker)
		}
	}
}

// normalizedSkillSection cuts the tooling skill from heading to the next
// second-level heading and normalizes whitespace, so markers match on the
// words and not on where the lines wrap.
func normalizedSkillSection(t *testing.T, heading string) string {
	t.Helper()
	body, err := os.ReadFile(processCliSkill)
	if err != nil {
		t.Fatal(err)
	}
	_, section, found := strings.Cut(string(body), heading)
	if !found {
		t.Fatalf("the tooling skill lost its section %q", heading)
	}
	if next := strings.Index(section, "\n## "); next >= 0 {
		section = section[:next]
	}
	return strings.Join(strings.Fields(section), " ")
}

func requireSkillMarkers(t *testing.T, what, text string, markers []string) {
	t.Helper()
	for _, marker := range markers {
		if !strings.Contains(text, marker) {
			t.Errorf("%s: %q is missing", what, marker)
		}
	}
}

const onboardingHeading = "## Reverse-engineering onboarding (store-backed)"

// SR-RDD-ONBOARD-014: a customer's agent stopped after the authorization and
// after every publication group, because nothing listed when a sweep must
// stop. The person chooses the run mode once per run; the stop list and the
// one planned question are fixed; a changed run mode survives the session.
func TestProcessCliSkillAsksTheRunMode(t *testing.T) {
	section := normalizedSkillSection(t, onboardingHeading)
	requireSkillMarkers(t, "the onboarding sequence does not ask the run mode", section, []string{
		// the run mode joins the one authorization question and the source
		"the run mode and what each choice means",
		"Ask once: one question that covers the mode, the documents, the run name and the run mode",
		"approved <mode>, documents <all|none>, run mode <autonomous|confirm>",
		"`run mode autonomous` or `run mode confirm`",
		// the stop list, its six conditions and the one planned question
		"In the autonomous run mode, stop and ask the person only when:",
		"a new run needs authorization;",
		"a refusal has no next step in this skill, or the same refusal comes a second time;",
		"the authorized source changed",
		"a publish would change an existing requirement;",
		"the run is no longer current:",
		// the stop case names each collision with a record from outside the run
		"a publish is refused for a collision with a record created or changed outside the run (an `external_id` the group creates already exists, a reuse entry names a requirement whose fingerprint moved, or a parent is missing or not governed;",
		"a sign-in or a permission is refused.",
		"The one planned question is the confirmation of bounded contexts new to the sweep, asked once per run before that run's first publish and not at all when the run adds none",
		"which you report once, at the end",
		// the confirm run mode
		"Confirm run mode: report after each publication group and wait for the person.",
		// resume: the stored source, the change file, confirm wins
		"reads the run mode from `data.authorization.authorization_source` of `status --run ID` and does not ask for it again",
		"The stored source cannot be changed",
		"`.modernpath/reverse-engineering.runs/run-mode-changes.json`: one entry per run, keyed by the run id",
		"A later session reads that file before the stored source.",
		"A run is autonomous only when its stored source says `run mode autonomous` and the change file is missing, or is readable and holds no entry for that run that says `confirm`.",
		"a source without the run-mode wording, such as one written through the file form or by hand;",
		"an unreadable change file",
		"A change to autonomous on a run authorized as confirm therefore holds for the current session only.",
		// the open-questions list is a file a resumed session continues
		"goes on the open-questions list, `.modernpath/reverse-engineering.runs/open-questions.md`",
		"A resumed session continues the list, and the end report is made from it",
		// the derived recommendation in an area-by-area baseline
		"Preflight recommends derived whenever the system has a requirement, so in a planned area-by-area baseline it recommends derived for every run after the first.",
		"the person still chooses baseline or derived",
		// the existing authorization statements stay (regression)
		"Ask once", "You propose the run name", "except after a refusal",
	})
	// The run mode is never shortened to mode: a run already has a mode,
	// baseline or derived.
	for _, short := range []string{"autonomous mode", "confirm mode"} {
		if strings.Contains(section, short) {
			t.Errorf("the onboarding sequence shortens the run mode to %q", short)
		}
	}
	// A source without the run-mode wording is described by how it was
	// written, never by when.
	if dated := "authorized before this rule"; strings.Contains(section, dated) {
		t.Errorf("the onboarding sequence describes a source by when it was written: %q", dated)
	}
	// The example source with the run mode fits the 255-byte limit with a long
	// name and the longest choices.
	example := strings.NewReplacer(
		"<date>", "2026-10-07",
		"<name>", strings.Repeat("n", 64),
		"<mode>", "baseline",
		"<all|none>", "none",
		"<autonomous|confirm>", "autonomous",
	).Replace("USER:<date>:<name> approved <mode>, documents <all|none>, run mode <autonomous|confirm>")
	if len(example) > 255 {
		t.Errorf("the example source with the run mode is %d bytes, over the 255-byte limit", len(example))
	}
}

// SR-RDD-ONBOARD-015, as built: publish refuses only a collision in the
// records a group names, and never compares the whole corpus. A customer's
// runs froze each other under the earlier rule; the skill now says what a
// publish checks, what the corpus fingerprint still guards, and the working
// rules that keep a sweep in order.
func TestProcessCliSkillStatesRunCurrency(t *testing.T) {
	section := normalizedSkillSection(t, onboardingHeading)
	requireSkillMarkers(t, "the onboarding sequence does not state what keeps a run publishing", section, []string{
		// what a publish checks, and that nothing else stops a run
		"A publish compares only the records its group names.",
		"nothing of the group is recorded",
		"Publish does not compare the system's corpus with what the run last saw",
		"another run's publish, an author edit, a context code, a trace refresh, an applied gate answer, a candidate decision or an acceptance does not stop a run",
		"A group that holds only source assessments is accepted at any time",
		"`corpus_after` on a receipt and `data.corpus_fingerprint` of preflight are records, not a check that publish makes",
		// what the corpus fingerprint covers and who compares it
		"The corpus fingerprint is what `authorize` and `refresh-traces` compare.",
		"a `refresh-traces` call that creates a link (also one made with the same run), a lifecycle change such as applying a gate answer",
		"Answering a gate without applying it does not change it.",
		// only a group that writes a row the fingerprint covers changes it
		"so it changes with every group that creates a requirement, a link or an epic (not a group of only source assessments or reuse entries), an author edit",
		"every group that creates a requirement, a link or an epic changes the corpus fingerprint that the next authorization is checked against",
		"a refresh needs the corpus fingerprint of a current preflight, and every group that creates a requirement, a link or an epic changes it.",
		// the working rules
		"Runs publish one after another. Authorize the next run only after the previous one has published every group, its source assessments included, so that it meets the finish condition of step 7, or after the person decided to leave it unfinished.",
		"Fold a correction to a requirement into its group before the group is published.",
		"Changes to published citations and `refresh-traces` belong to verification, after the run has published its last group",
		"Set contexts on requirements already published, with `author context --file`, after the run's last group and before verification records execution proof",
		// own-capture citations and files covered by an earlier run
		"A requirement cites the source file ids of the capture of the run it is published to, never those of an earlier run's capture of the same files",
		"although a requirement from an earlier run covers it, is assessed as `reviewed` with that requirement named in the reason",
	})
	// A group of only source assessments or reuse entries writes no row the
	// corpus fingerprint covers.
	for _, every := range []string{"every published group", "every group changes"} {
		if strings.Contains(section, every) {
			t.Errorf("the onboarding sequence says every group changes the corpus fingerprint: %q", every)
		}
	}
}

// SR-RDD-ONBOARD-016: one refusal word with several causes made a customer's
// agent create more runs. The glossary carries the sweep's server refusals,
// and the onboarding section lists each cause of source_not_authorized with
// the read that shows it.
func TestProcessCliSkillExplainsSweepRefusals(t *testing.T) {
	glossary := normalizedSkillSection(t, "## 5. Refusal glossary")
	requireSkillMarkers(t, "the refusal glossary does not explain the sweep refusals", glossary, []string{
		"| `source_not_authorized` (from `reverse-engineer publish`) | server |",
		"| `source_not_authorized` (from `reverse-engineer refresh-traces`) | server |",
		"| `stale_corpus` (from `reverse-engineer refresh-traces`) | server |",
		"| `existing_requirement_conflict` / `parent_not_found` / `parent_not_governed` (from `reverse-engineer publish`) | server |",
		"| `invalid_trace_refresh` | server |",
		// publish never answers stale_corpus; a collision is a stop
		"`reverse-engineer publish` never answers `stale_corpus`",
		"stop and show the person the refusal. Sending the same group again does not help, and another run does not clear it; do not authorize one without the person",
		// each collision, and the stop it is when the record came from outside the run
		"an `external_id` the group creates already exists in the system; a reuse entry names a requirement whose fingerprint moved from its `reuse_fingerprint`, or one that is missing, deleted, DERIVED or OBSOLETE; a parent is missing or deleted, or is not governed (DERIVED or OBSOLETE under a confirmed system requirement)",
		"Otherwise the record was created or changed outside the run and the run is no longer current (stop list item 5): stop and show the person the refusal.",
		// publish reads only the records the group names
		"Publish checks nothing of the corpus outside the records the group names",
		// stale_corpus on refresh-traces: the input is out of date
		"the input's `corpus_fingerprint` is not the current one",
		"`reverse-engineer preflight` again and its `data.corpus_fingerprint` in the input, with the same run and the same group key",
		// the accepted trace refresh input and the named rule
		"names the `rule` that failed",
		"a group key of 1 to 255 bytes; top-level keys exactly `corpus_fingerprint` and `requirements`; 1 to 500 entries, each exactly `kind: \"system\"`, `external_id` and a nonempty `expected_fingerprint`; no `external_id` twice",
		// the first failing citation refuses the group
		"The first failing citation refuses the whole group and nothing of the group is recorded",
	})
	section := normalizedSkillSection(t, onboardingHeading)
	requireSkillMarkers(t, "the onboarding sequence does not list the causes of source_not_authorized", section, []string{
		"### Refusals in a sweep",
		"names the `cause`, the `requirement` and the `citation_index`",
		"| `no_source_citations` |",
		"| `unresolved_not_authorized` |",
		"whose reason is blank",
		"| `invalid_citation` |",
		"| `not_from_ready_capture` |",
		"the repository was unlinked from the system, the capture is not ready, or its manifest is missing or revoked",
		"| `capture_of_another_run` |",
		"for example an earlier run's capture of the same file",
		"| `unsupported_kind` |",
		"| `source_identity_differs` |",
		"| `test_record_deleted` |",
		// the reads that show each cause
		"`source-status --capture <id>` for this run's capture shows `data.state`, `data.run_id` and `data.files[]`",
		"Find the entry of `data.files[]` by the cited `source_file_id`, never by path",
		"`read-source --source <id>` answers `not_found` or `source_unavailable`",
		"no read shows the deleted record",
		// the first failing citation, and the document refusal
		"The first failing citation refuses the whole group.",
		"refuses as `document_not_authorized` instead",
		"or the citation's `kind` is not `document`",
		// what the glossary covers, and where any other refusal goes
		"The refusal glossary has rows for `source_not_authorized`, `stale_corpus`, the collision refusals and `invalid_trace_refresh`; this section gives the causes of `source_not_authorized` and `document_not_authorized`, and step 4 the refusals of `authorize`. Any other refusal is stop-list item 2.",
		// the publication and refresh steps point to the entries
		"A refused publish: the glossary rows for `source_not_authorized` and the collision refusals, and \"Refusals in a sweep\" below.",
		"A refusal: the glossary rows for `stale_corpus`, `invalid_trace_refresh` and `source_not_authorized` from `refresh-traces`.",
	})
	// No cause is concluded by ruling the others out.
	for _, elimination := range []string{"by elimination", "rule out", "ruled out", "ruling out"} {
		if strings.Contains(section, elimination) {
			t.Errorf("the onboarding sequence concludes a refusal cause %q", elimination)
		}
	}
	// Neither the glossary nor the section claims a completeness it lacks.
	for _, claim := range []string{"checks nothing else of the corpus", "has a row for each server refusal a sweep meets"} {
		if strings.Contains(glossary+" "+section, claim) {
			t.Errorf("the skill claims %q", claim)
		}
	}
}

// SR-RDD-ONBOARD-017: an unignored inventory.json in the repository root was
// listed by the inventory writing it, and install output made every capture
// dirty. The repository is prepared first and every saved file lives in the
// sweep's working folder.
func TestProcessCliSkillPreparesTheRepository(t *testing.T) {
	section := normalizedSkillSection(t, onboardingHeading)
	requireSkillMarkers(t, "the onboarding sequence does not prepare the repository", section, []string{
		// the install commit and why
		"Before the first inventory of a Git repository, prepare the repository:",
		"Commit the files that `modernpath install` created or changed",
		"or you do after the person agreed to a commit in their repository",
		"an inventory of a dirty repository marks the whole capture dirty, and a run captured dirty cannot be accepted as built, whatever is committed later, without a new authorized run with its own capture",
		"the tip of the remote default branch (verification step 3 below)",
		// the delivery proof of a run at a newer tip names the run
		"Add `--run CAPTURE-RUN` for a run inventoried with `--path`, for a run whose capture holds files under `.claude`, which a new inventory leaves out, and for acceptance at a default-branch tip past the commit the run captured",
		"and at a newer tip the captured revision and whether it is an ancestor of the tip; that is shown, not required",
		// clean and at one commit; no kit update during a sweep, and its cost
		"The repository stays clean and at one commit from the first inventory until acceptance: commit nothing in it during a sweep.",
		"The kit is not updated during a sweep.",
		"install it between runs and commit the result before the next inventory; never leave it uncommitted",
		"Such a run can then be accepted as built only through a new authorized run with its own capture",
		"with the citations of its requirements moved to the new capture's file ids",
		// the local exclude file and what the inventory leaves out
		"into the repository's local exclude file",
		"Ignored files are left out of the inventory and disclosed as exclusions.",
		"Tracked files are listed whatever the ignore rules say, except symbolic links, private paths and files under `.claude`, which are left out and disclosed.",
		// the working folder and its name rule
		"`.modernpath/reverse-engineering.runs/` in the workspace root",
		"Its name has a dot, which no system slug can take",
		"Each run has its own sub-folder, `.modernpath/reverse-engineering.runs/<folder>/`",
		"Choose `<folder>` before the inventory, from letters, digits and hyphens, and propose the same text as the run name in step 4",
		"Do not save these files in the repository root or in a temp folder",
		// the ignore precondition
		"The folder is ignored by the rule `/.modernpath/*` that `modernpath install` writes into `.gitignore`.",
		"add `/.modernpath/reverse-engineering.runs/` to the local exclude file first",
		// every saved or read file has its path in the working folder
		"`mkdir -p .modernpath/reverse-engineering.runs/<folder>`",
		`New-Item -ItemType Directory -Force .modernpath\reverse-engineering.runs\<folder>`,
		"`> .modernpath/reverse-engineering.runs/<folder>/inventory.json`",
		"preflight > .modernpath/reverse-engineering.runs/<folder>/preflight.json",
		"--inventory .modernpath/reverse-engineering.runs/<folder>/inventory.json --preflight .modernpath/reverse-engineering.runs/<folder>/preflight.json",
		"> .modernpath/reverse-engineering.runs/<folder>/run.json",
		"--preflight .modernpath/reverse-engineering.runs/<folder>/preflight-new.json",
		"authorize --file .modernpath/reverse-engineering.runs/<folder>/authorization.json",
		"> .modernpath/reverse-engineering.runs/<folder>/capture-<key>.json",
		"--file .modernpath/reverse-engineering.runs/<folder>/group-<stable-key>.json",
		"coverage --run ID > .modernpath/reverse-engineering.runs/<folder>/coverage.json",
		"--file .modernpath/reverse-engineering.runs/<folder>/refresh-<stable-key>.json",
		`cmd /c "modernpath reverse-engineer preflight > .modernpath\reverse-engineering.runs\<folder>\preflight.json"`,
		// the saved-file rules stay (regression)
		"The saved file must hold the bytes the command printed.",
		"never edit it",
		"one run has one inventory file",
	})
	for _, bare := range []string{"> inventory.json", "> preflight.json", "> run.json", "--preflight preflight-new.json", "--file group.json", "--file refresh.json", "--file authorization.json"} {
		if strings.Contains(section, bare) {
			t.Errorf("the onboarding sequence still saves or reads %q outside the working folder", bare)
		}
	}
}

// SR-RDD-ONBOARD-017: delivery-proof refuses a dirty repository, so the input
// files of verification and of candidate decisions live in the run's
// sub-folder like every other file of the sweep, and test output is put in the
// local exclude file before the proof.
func TestProcessCliSkillKeepsVerificationInputsInTheWorkingFolder(t *testing.T) {
	section := normalizedSkillSection(t, onboardingHeading)
	requireSkillMarkers(t, "the onboarding sequence saves verification or decision inputs outside the working folder", section, []string{
		// the working-folder paragraph names them and says why
		"The input files of its verification and of candidate decisions go there too (execution, delivery, proof, acceptance, apply, selection and decision files), because `delivery-proof` refuses a dirty repository.",
		// test output from running the existing tests
		"Put any output the test run writes into the repository and Git does not ignore, such as reports or coverage files, into the local exclude file before step 3: `delivery-proof` refuses a dirty repository.",
		// every verification and decision input has its path in the run's sub-folder
		"execution-proof --file .modernpath/reverse-engineering.runs/<folder>/execution.json",
		"delivery-proof --file .modernpath/reverse-engineering.runs/<folder>/delivery.json",
		"proof-preview --file .modernpath/reverse-engineering.runs/<folder>/proof.json",
		"acceptance-open --file .modernpath/reverse-engineering.runs/<folder>/acceptance.json",
		"acceptance-apply --gate ASBUILT-… --file .modernpath/reverse-engineering.runs/<folder>/apply.json",
		"preview --file .modernpath/reverse-engineering.runs/<folder>/selection.json",
		"decide --file .modernpath/reverse-engineering.runs/<folder>/decision.json",
	})
	for _, bare := range []string{"--file execution.json", "--file delivery.json", "--file proof.json", "--file acceptance.json", "--file apply.json", "--file selection.json", "--file decision.json"} {
		if strings.Contains(section, bare) {
			t.Errorf("the onboarding sequence still reads %q from the workspace root", bare)
		}
	}
}

// SR-RDD-ONBOARD-018: all of a customer's published requirements showed as
// Unclassified, few cited a test, and a staged requirement duplicated an
// existing one. The contract and the publication step say what a published
// requirement carries.
func TestProcessCliSkillSaysWhatARequirementCarries(t *testing.T) {
	section := normalizedSkillSection(t, onboardingHeading)
	requireSkillMarkers(t, "the onboarding sequence does not say what a requirement carries", section, []string{
		// the context fields in the contract
		"Each requirement also carries `context`, the bounded-context code, and `context_name`, its name, each at most 255 characters. Heartbeat groups requirements by the code.",
		// contexts confirmed before each run's first publish, only new ones
		"after the source is captured and the behavior derived and before anything is published, show the person the contexts the run derived that are not yet in `.modernpath/reverse-engineering.runs/confirmed-contexts.json`, a code and a name each",
		"the analysis subsystems are an input only",
		"The person confirms or changes them, in either run mode.",
		"This is a naming confirmation of the list, asked once per run, not an approval of each context's content.",
		"A run whose derived contexts are all on the list asks nothing.",
		"Set a confirmed context on every requirement you create, never one the person has not confirmed. A reuse entry carries no context.",
		// the reused context list
		"It sits above the run folders, so every run of the sweep reads it before its first publish and asks only about the contexts it adds.",
		"When the file is missing or unreadable, ask the person to confirm the whole list again",
		// a requirement that fits no confirmed context
		"in the confirm run mode, ask. In the autonomous run mode, do not publish the group that holds it, nor any group whose requirements name a parent in a held group",
		"put the question on the open-questions list, continue with the other groups, report the run as unfinished, and publish the held groups after the person has confirmed the context",
		// tests, a user requirement's own citations, comparison, limits
		"When a test is cited and the executed test's name is known, name it in `test_case_ref`: the as-built proof binds a criterion to the executed test by that name.",
		"A user requirement needs a code citation and a test citation of its own for its upper proof",
		"Find and cite the tests that cover each group's code",
		"List the requirements published without a test in the end report.",
		// where the receipt lists them
		"the receipt lists them under `data.result` as `requirements_without_context` and `requirements_without_test_citation`.",
		"Compare the staged requirements with the existing ones by the files they cite",
		"reuse such a requirement or merge the staged one into it instead of publishing the same behavior under a new id",
		"One group holds at most 500 requirements, 100 criteria per requirement and 5000 source assessments.",
	})
	if old := "A test citation can retain `test_case_ref`"; strings.Contains(section, old) {
		t.Errorf("the contract still calls test_case_ref optional: %q", old)
	}
}

// SR-RDD-ONBOARD-019: a customer's coverage was reported complete with 710
// files marked unresolved. Step 7 says how to read the finish condition from
// the coverage output, field by field.
func TestProcessCliSkillReadsTheFinishCondition(t *testing.T) {
	section := normalizedSkillSection(t, onboardingHeading)
	requireSkillMarkers(t, "onboarding step 7 does not read the finish condition", section, []string{
		"A run is finished when `data.assessments.unresolved_files` is 0 and every entry of `data.files` has `governed` or `candidate` true, or an `assessment` of `reviewed` or `unsupported`.",
		"No single total answers it",
		"An `unresolved` assessment says that work remains; it is never used to complete a count.",
		"a file cited only by a user requirement is not linked",
		"The report is for one run: a link counts for it only when it was made through a capture of the same repository key and file list, whichever run made it.",
		"although a requirement from an earlier run covers it, is assessed as `reviewed` with that requirement named in the reason",
		"completeness of the sweep over this run's files, not verification",
		"counts and percentages define no passing gate",
		// preserved: coverage is not execution evidence
		"It is not execution evidence",
	})
}
