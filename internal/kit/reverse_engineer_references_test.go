package kit

import (
	"strings"
	"testing"
)

// embeddedText reads a packaged asset with its whitespace folded, so a phrase
// may wrap across lines in the Markdown source. A missing asset is reported
// and read as empty, so every phrase assertion still runs and reports.
func embeddedText(t *testing.T, path string) string {
	t.Helper()
	body, err := assets.ReadFile(path)
	if err != nil {
		t.Errorf("the kit must embed %s: %v", path, err)
		return ""
	}
	return strings.Join(strings.Fields(string(body)), " ")
}

func requirePhrases(t *testing.T, name, text string, phrases []string) {
	t.Helper()
	for _, phrase := range phrases {
		if !strings.Contains(text, phrase) {
			t.Errorf("%s is missing %q", name, phrase)
		}
	}
}

// forbidPhrases fails when wording a later rule replaced is still present.
func forbidPhrases(t *testing.T, name, text string, phrases []string) {
	t.Helper()
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			t.Errorf("%s still says %q", name, phrase)
		}
	}
}

const reverseEngineerSkillDir = "assets/rdd/skills/rdd-reverse-engineer/"

func TestRequirementQualityReferenceCarriesTheRules(t *testing.T) {
	requirePhrases(t, "SKILL.md", embeddedText(t, reverseEngineerSkillDir+"SKILL.md"), []string{
		"(references/requirement-quality.md)",
	})
	requirePhrases(t, "requirement-quality.md", embeddedText(t, reverseEngineerSkillDir+"references/requirement-quality.md"), []string{
		// What a user requirement is.
		"one actor reaching one outcome they would start on their own and notice if it broke",
		// An endpoint's intake is criteria, not a record.
		"An endpoint or controller is never a record of its own",
		// Failure paths in called code.
		"Follow each call a handler makes one level down",
		"a failure the handler does not catch",
		"a lookup that throws when two rows match",
		// Reuse only what is right; replace the rest.
		"Read a stored requirement against the code before reusing it",
		"only when it is PROPOSED with no Epic owner",
		"carries the derives traces the stored record had",
		"reports the refusal verbatim",
		"a published, blocked, deferred or Epic-owned record that fails is reused by reference and listed in the open questions",
		"a record the batch plan names as kept",
		"stage a replacement with new UUIDs and name the stored id it replaces",
		"`modernpath requirements obsolete <id> --kind user|system --reason \"replaced by <new id>\"`",
		"a failing kept record is listed in the open questions",
		"make the stored record obsolete",
		"source_type `source_file` (the test file) and target_type `system_requirement`",
		"Use the repository key the store already uses for the repository",
		"is several user requirements",
		"One user requirement per context or subsystem is under-derivation",
		"the real role, traced from who calls the entry point",
		"A call with no outside caller is a system requirement",
		// What a system requirement is.
		"one boundary's behavior serving a user requirement, with criteria a test could check",
		"served by every system requirement its outcome needs",
		"A user requirement with no serving system requirement is unfinished",
		"one system requirement, owned by one context",
		// Content rules.
		"surface inventory",
		"surface unit",
		"never from a count",
		"A script may enumerate and group; it never writes a title, statement, actor, outcome or scenario",
		"A UI control is a step",
		"One outcome across its layers",
		"A guard or validation is a criterion",
		"State the scope boundary",
		"Every side effect is a criterion with its trigger",
		"As built, not as intended",
		"add it to the open questions",
		"with units and where they are configured",
		"Give each platform its own scenario",
		"Live code only",
		"A citation is a file where the stated behavior is implemented or tested",
		"A cited test asserts the stated rule",
		"traced to the production code the test calls",
		// Link kinds, identities and reuse.
		"`derives`",
		"`implements`",
		"`verifies`",
		"Every requirement and criterion carries a UUID in the staged file",
		"a reference entry: only its `kind` and `id`",
		// The independent sample read.
		"at least 20 records of a context, or all of them when it has fewer",
		"good, weak, not a requirement, or wrong",
		"more than one weak in ten",
		// Check before record.
		"modernpath reverse-engineer check",
		"The store does not enforce this.",
		// Rules the sample reads showed small models need.
		"Name an actor or caller only when you found the code that calls in",
		"otherwise state the scope boundary",
		"Every refusal path of a handler is a criterion",
		"every event or record it saves, including one saved before a check",
		"A validator or guard is a criterion of the outcome it guards, never a requirement of its own",
		"A set of options is a limit of the outcomes it controls, never a requirement of its own",
		// The verdict rubric.
		"good states what the code does with its refusals, saved effects and limits",
		"weak is true but omits a refusal, side effect, unit or limit the code has, or cites a file that does not implement it",
		"not a requirement is a step, a guard, an option set or a restated function",
		"wrong contradicts the code or names a caller no code shows",
	})
	forbidPhrases(t, "requirement-quality.md", embeddedText(t, reverseEngineerSkillDir+"references/requirement-quality.md"), []string{
		"lists it for the person when that is refused",
	})
}

func TestBatchReferenceDescribesTheUnattendedSweep(t *testing.T) {
	requirePhrases(t, "SKILL.md", embeddedText(t, reverseEngineerSkillDir+"SKILL.md"), []string{
		"(references/batch.md)",
	})
	requirePhrases(t, "batch.md", embeddedText(t, reverseEngineerSkillDir+"references/batch.md"), []string{
		// The one batch question.
		"batch-plan.md",
		"never asked again",
		"an id prefix",
		"exclusion globs, each with one reason",
		"unattended: yes or no",
		"reads the batch folder instead",
		"`context` and `context_name`",
		// Brief and handoff.
		"at most 60 lines",
		"the worker's whole prompt",
		"`requirements search`",
		"`ready` or `partial:`",
		"surface units covered out of total",
		// Batch folder.
		".modernpath/reverse-engineering/<batch>/",
		"groups/recorded/",
		"batch-summary.md",
		"open-questions.md",
		// Recording and its results.
		"modernpath reverse-engineer record --dir",
		"Never loop over `record` in the shell",
		"Sign-in, permission, server-request or binding failure",
		"`local-write` or `local-read`",
		"Content refusal",
		"--retry-refused",
		"Check failure",
		"at most 3 times for the same file and reason",
		"Changed after record",
		"a new file with new UUIDs",
		"list it with the old id under `Replacements:` and make the old record obsolete in the end order",
		"Receipts only help resume; the store is the authority",
		"refers only to itself, earlier files in lexical order, or the store",
		"sign in again",
		// Workers, stop list, sample read and merge.
		"A worker never writes to the store",
		"restarted once with the same slice",
		"The batch stops only for",
		"`.claude/agents/rdd-cold-reviewer.md`",
		"`.codex/agents/rdd-cold-reviewer.toml`",
		"There is no Epic and no PASS or FAIL",
		"one line per record: id, verdict, reason",
		// The prompt carries the verdict rubric.
		"good states what the code does with its refusals, saved effects and limits",
		"weak is true but omits a refusal, side effect, unit or limit the code has, or cites a file that does not implement it",
		"not a requirement is a step, a guard, an option set or a restated function",
		"wrong contradicts the code or names a caller no code shows",
		// The stop list names every stop class.
		"sign-in, permission, server-request, binding, `local-write` or `local-read` failure",
		"gets a new read",
		"The store does not enforce this.",
		// Fixes go back to a worker; the first read is reported.
		"the coordinator fixes it from the code and the reviewer's reasons",
		"the first read's counts beside the final ones",
		// Records to keep and the end order.
		"stored requirements to keep",
		"run the sample read and record its fixes, make replaced stored records obsolete",
		"before writing `batch-summary.md`",
		"stops further obsolete calls, writes `batch-summary.md` with the refusal verbatim, then ends the batch",
		// Replacements of stored records.
		"replaces: <stored id>",
		"Replacements:",
		// The coordinator's finish.
		"at least 20 records of each context, or all of them when it has fewer",
		"Run `modernpath reverse-engineer check --dir <context>/groups --repo <key>=<dir>` for each context and copy its linked, excluded and unresolved counts",
		"`<dir>` is the context's own directory, never the repository root",
		"one `--exclude` per exclusion glob of `batch-plan.md`",
		"`<context>/handoff-<slice>.md` (worker, one per slice)",
		"Never hold a staged file back or move it out of `groups/`",
		"A file no handoff names is still staged work: ground it the same way and never delete it",
		"system requirements without a verifies trace and user requirements without a serving system requirement, each by id",
		"finished only when the sample has no not-a-requirement or wrong verdict and at most one weak in ten",
		"The batch is not done until `batch-summary.md` has every field for every context",
	})
	requirePhrases(t, "coverage.md", embeddedText(t, reverseEngineerSkillDir+"references/coverage.md"), []string{
		"finished or unfinished",
		"This label is descriptive, not a passing coverage threshold",
		"blocks neither recording nor the batch",
	})
	forbidPhrases(t, "batch.md", embeddedText(t, reverseEngineerSkillDir+"references/batch.md"), []string{
		"or is listed for the person",
	})
}

func TestReverseEngineerBaselineSentencesUnchanged(t *testing.T) {
	requirePhrases(t, "SKILL.md", embeddedText(t, reverseEngineerSkillDir+"SKILL.md"), []string{
		"Do not require source capture, an authorization run, publication groups, clean Git state, matching revisions, proof packets, as-built acceptance or mechanical quality/coverage gates before recording.",
		"(references/requirement-quality.md)",
		"(references/batch.md)",
	})
	requirePhrases(t, "PROCESS.md", embeddedText(t, "assets/rdd/PROCESS.md"), []string{
		"coverage gate or as-built acceptance before recording.",
	})
}

// Epic 340 (G1), test case e1a8ea3a: the batch reference starts its batch
// question from the System's list of contexts, and PROCESS.md says that every
// recorded requirement has a context from that list. The check runs this test
// by its name.
func TestG1FlowBatchReferenceAndProcessUseTheList(t *testing.T) {
	requirePhrases(t, "batch.md", embeddedText(t, reverseEngineerSkillDir+"references/batch.md"), []string{
		"Read the System's list of contexts before the batch question",
		"A context on the list keeps its code and the list's name",
	})
	requirePhrases(t, "PROCESS.md", embeddedText(t, "assets/rdd/PROCESS.md"), []string{
		"Every requirement reverse engineering records has a context from the System's one list of contexts",
	})
}
