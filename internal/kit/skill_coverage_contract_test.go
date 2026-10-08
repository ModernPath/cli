package kit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-CROSS-178: the reverse-engineering skill carries the coverage contract.
//
// Twice in one day (2026-08-15) coverage discipline lived only in prose and
// failed: a sweep instructed at "5-10 rows per context" produced a 46-row
// "complete" ledger, and a "110/110 endpoints routed" claim audited to 65/110.
// The contract lives in the installed skill and its linked coverage reference —
// promoted there with the skill itself — and skill prose has ALSO been
// lost before (REQ-CROSS-174's pointerization). This test pins the
// load-bearing markers so the contract cannot be diluted or dropped without a
// red build.
func TestReverseEngineerSkillCarriesTheCoverageContract(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	s := installedReverseEngineeringInstructions(t, root)

	for _, marker := range []string{
		// Separate executable extraction checks from the citation count check.
		"Account for every item as extracted, already covered, excluded with a reason, or unresolved",
		"Distinct files cited by persisted rows, excluded with a reason, or unresolved",
		"does not enforce percentage coverage",
		"Counts and percentages describe the inventory; they do not define a passing gate",
		"command, output and exit code",
		"Missing denominators, unresolved mappings",
		"Do not shrink a denominator",
		"Zero recognized citations over nonempty expected input is a failed measurement",
		// Keep the complete sweep and authoritative coverage distinctions.
		"jobs/events/webhooks",
		"integrations and tests",
		"frozen inventory and authoritative read-back",
		"uncovered items for each inventory",
		"Validate any checker against known-good and known-bad inputs",
		"candidate links do not establish governed or verified behavior",
		"Unresolved in-scope work remains incomplete",
		"full sweep across remaining authorized contexts without per-context reapproval",
	} {
		if !strings.Contains(s, marker) {
			t.Errorf("coverage contract marker missing from the skill: %q — "+
				"the contract is being diluted; see REQ-CROSS-178 for why each marker exists", marker)
		}
	}
}

// installedNormalized reads one installed file with whitespace normalized, so
// markers match on the words and not on where the lines wrap.
func installedNormalized(t *testing.T, root string, parts ...string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(append([]string{root}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(strings.Fields(string(body)), " ")
}

func requireInstalledMarkers(t *testing.T, what, text string, markers []string) {
	t.Helper()
	for _, marker := range markers {
		if !strings.Contains(text, marker) {
			t.Errorf("%s: %q is missing", what, marker)
		}
	}
}

// SR-RDD-ONBOARD-014: the reverse-engineer skill states the run mode and points
// to the stop list; PROCESS.md, where an answer is never an instruction to
// enter the next phase, names an authorized run as continuing.
func TestReverseEngineerSkillStatesTheRunMode(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	requireInstalledMarkers(t, "the installed rdd-reverse-engineer does not state the run mode", installedReverseEngineeringInstructions(t, root), []string{
		"In the same question as the mode, the person chooses the run mode, written into the run's authorization source",
		"stops only for the stop list of the `mp-process-cli` onboarding",
		"or confirm, where it reports after each publication group and waits",
		// on resume an unreadable change file also means confirm, per group
		"the run is autonomous only when its source says so and the change file holds no confirm entry for it; otherwise, and also when the change file is unreadable, confirm after each publication group.",
		"Call this choice the run mode, never just the mode",
		"Preflight recommends derived whenever the system has a requirement, which in a planned area-by-area baseline is every run after the first.",
		"the person still chooses baseline or derived",
		// kept: the sweep continues without per-context reapproval
		"full sweep across remaining authorized contexts without per-context reapproval",
	})
	requireInstalledMarkers(t, "the installed PROCESS.md does not name the authorized run as continuing", installedNormalized(t, root, ".modernpath", "rdd", "PROCESS.md"), []string{
		"Under an autopilot grant the agent continues instead (§Autopilot). An authorized source-scoped reverse-engineering run, baseline or derived, continues within that authorization (§Source-scoped baseline onboarding).",
		// kept: the rule it points to
		"No per-row or per-context confirmation is required within this approved scope.",
	})
}

// SR-RDD-ONBOARD-015: the reverse-engineer skill states the rules that keep a
// run publishing in short form and points to the tooling skill for the checks.
func TestReverseEngineerSkillStatesRunCurrency(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	requireInstalledMarkers(t, "the installed rdd-reverse-engineer does not state what keeps a run publishing", installedReverseEngineeringInstructions(t, root), []string{
		"Of the corpus, a publication group is refused only for a collision with a record it names, never because the rest of the corpus changed.",
		"Runs publish one after another",
		"its source assessments included, and meets the finish condition, or after the person decided to leave it unfinished",
		"Fold a correction into a group before publishing it.",
		"A later run cites the source file ids of its own capture",
		"a file covered by a requirement from an earlier run is assessed as reviewed, naming that requirement",
		"citation changes and trace refreshes belong to verification",
		"The `mp-process-cli` onboarding gives the checks and the refusals.",
		// kept: a changed source scope needs a new authorization
		"materially changed source scope require fresh scope authorization",
	})
}

// SR-RDD-ONBOARD-017: the reverse-engineer skill carries the repository
// preparation in short form.
func TestReverseEngineerSkillPreparesTheRepository(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	requireInstalledMarkers(t, "the installed rdd-reverse-engineer does not prepare the repository", installedReverseEngineeringInstructions(t, root), []string{
		"Before the first inventory of a Git repository, prepare it as the `mp-process-cli` onboarding says.",
		"Commit the files `modernpath install` created or changed, by the person or by the agent after the person agreed",
		"a run captured dirty cannot be accepted as built, whatever is committed later, without a new authorized run with its own capture",
		"the tip of the remote default branch",
		"into the repository's local exclude file",
		"tracked files are listed whatever the ignore rules say, except symbolic links, private paths and files under `.claude`, which are left out and disclosed",
		"The repository stays clean and at one commit from the first inventory until acceptance, and the kit is not updated during a sweep.",
		"installed between runs and committed, never left uncommitted",
		"with the citations of its requirements moved to it",
	})
}

// SR-RDD-ONBOARD-018: the reverse-engineer skill records a confirmed context
// and the tests of each requirement, and compares with the existing corpus.
func TestReverseEngineerSkillRecordsContextAndTests(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	requireInstalledMarkers(t, "the installed rdd-reverse-engineer does not record contexts and tests", installedReverseEngineeringInstructions(t, root), []string{
		"Before a run's first publication, show the person the bounded contexts the run derived that the sweep has not confirmed yet, a code and a name each",
		"with analysis subsystems as an input only",
		"the person confirms or changes them, in either run mode",
		"This is a naming confirmation of the list, asked once per run, not an approval of content",
		"Record a confirmed context on every requirement created, never an unconfirmed one; a reuse entry carries none.",
		"Identify the tests that cover each group's code and cite them, naming the executed test",
		"list the requirements published without a test in the end report",
		"Compare the staged requirements with the existing corpus by the files they cite",
		// kept: no approval round follows publication
		"No row/context approval round follows publication",
	})
}

// SR-RDD-ONBOARD-019: the coverage reference states when one run is finished,
// and the reverse-engineer skill points to it.
func TestCoverageReferenceStatesTheFinishCondition(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	reference := installedNormalized(t, root, ".claude", "skills", "rdd-reverse-engineer", "references", "coverage.md")
	requireInstalledMarkers(t, "the installed coverage reference does not state the finish condition", reference, []string{
		"A run is finished when no file of its inventory is unresolved and every file is linked to a system requirement, or assessed as reviewed or unsupported with a reason.",
		"the completeness of the sweep over the run's files, not verification of behavior, and it is not execution evidence",
		"An unresolved assessment states that work remains; it is never used to complete a count.",
		"The finish condition is measured for one run. A link counts for it only when it was made through a capture of the same repository key and file list, whichever run made it, and a file cited only by a user requirement does not count as linked.",
		"although a requirement from an earlier run covers it, is assessed as reviewed with that requirement named in the reason",
		// kept: counts define no passing gate
		"Counts and percentages describe the inventory; they do not define a passing gate",
	})
	requireInstalledMarkers(t, "the installed rdd-reverse-engineer does not point to the finish condition", installedReverseEngineeringInstructions(t, root), []string{
		"A run is finished only when the finish condition of the coverage reference holds.",
	})
}
