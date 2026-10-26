package cmd

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func init() { epicsCmd.AddCommand(newEpicGatesCommand(cliEnvLoad)) }

func newEpicGatesCommand(load func() (*cliEnv, error)) *cobra.Command {
	group := &cobra.Command{Use: "gates", Short: "Check named RDD preconditions and record Epic verification and reviews", Long: epicGatesHelp}
	check := &cobra.Command{Use: "check <epic-id>", Short: "Record a gate check and return readiness and every missing condition", Args: cobra.ExactArgs(1)}
	check.Flags().String("phase", "plan-ready", "plan-ready | implementation | completion-review | completion")
	check.Flags().Bool("json", false, "Emit the complete readiness report as JSON")
	check.Flags().String("git-sha", "", "Full Git SHA for the audit record (defaults to checkout HEAD)")
	check.RunE = func(cmd *cobra.Command, args []string) error {
		phase, _ := cmd.Flags().GetString("phase")
		switch phase {
		case "plan-ready", "implementation", "completion-review", "completion":
		default:
			return fmt.Errorf("invalid phase %q", phase)
		}
		revision, _ := cmd.Flags().GetString("git-sha")
		provenance, err := epicCheckGitProvenance(revision, cmd.Flags().Changed("git-sha"))
		if err != nil {
			return err
		}
		provenance["phase"] = phase
		data, err := epicGateCall(cmd, load, args[0], "POST", "/gates/check", provenance)
		if err != nil {
			return err
		}
		machine, _ := cmd.Flags().GetBool("json")
		if machine {
			if err := writeCommandJSON(cmd, data); err != nil {
				return err
			}
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Epic %s · %s · ready: %v\n", args[0], phase, data["ready"])
			if blockers, ok := data["blockers"].([]any); ok {
				for _, raw := range blockers {
					if b, ok := raw.(map[string]any); ok {
						fmt.Fprintf(cmd.OutOrStdout(), "- %s: %s\n", b["code"], b["message"])
					}
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "RDD skills: %v\n", data["skills"])
		}
		if data["ready"] != true {
			return fmt.Errorf("Epic %s has unresolved %s conditions", args[0], phase)
		}
		return nil
	}
	group.AddCommand(check)
	for _, op := range []struct {
		name, suffix, help string
		write              bool
	}{
		{"review-bundle", "/review-bundle", "Read current content and evidence for separate review", false},
		{"presentation", "/completion-presentation", "Read current content, reviews and completion readiness", false},
		{"review", "/phase-reviews", "Record an attributable independent plan or implementation review", true},
		{"resolve", "/resolve-finding", "Record the resolution of a material review finding", true},
		{"decision", "/answer-decision", "Record an explicit answer to a declared human decision", true},
		{"declare-decision", "/declare-decision", "Retain a decision question and issue its identity for the plan", true},
		{"recover-legacy", "/recover-legacy", "Apply an explicit human decision to return an active legacy plan to planning", true},
		{"candidate", "/candidates", "Declare a new candidate revision, requiring fresh verification and review", true},
		{"start-execution", "/executions", "Register candidate obligations before executing tests", true},
	} {
		op := op
		c := &cobra.Command{Use: op.name + " <epic-id>", Short: op.help, Args: cobra.ExactArgs(1)}
		c.Flags().Bool("json", false, "Emit JSON")
		if op.name == "review-bundle" {
			c.Flags().String("phase", "", "Include Epic context, reviews and readiness for this phase")
		}
		if op.write {
			c.Flags().String("file", "-", "JSON input file, or - for standard input")
		}
		c.RunE = func(cmd *cobra.Command, args []string) error {
			var payload any
			method := "GET"
			suffix := op.suffix
			if op.write {
				method = "POST"
				file, _ := cmd.Flags().GetString("file")
				object, err := commandObject(cmd, file)
				if err != nil {
					return err
				}
				payload = object
				// {"items":[...]} declares or answers a batch in one call (Epic 321).
				if _, batch := object["items"]; batch && (op.name == "decision" || op.name == "declare-decision") {
					suffix += "s"
				}
			}
			if op.name == "start-execution" {
				// Epic 323: {"items":[...]} registers several executions in one call.
				if object, ok := payload.(map[string]any); ok {
					if _, batch := object["items"]; batch {
						suffix += "/batch"
					}
				}
			}
			if op.name == "review-bundle" {
				phase, _ := cmd.Flags().GetString("phase")
				if phase != "" {
					switch phase {
					case "plan-ready", "implementation", "completion-review", "completion":
						suffix += "?phase=" + phase
					default:
						return fmt.Errorf("invalid phase %q", phase)
					}
				}
			}
			data, err := epicGateCall(cmd, load, args[0], method, suffix, payload)
			if err != nil {
				return err
			}
			return writeCommandJSON(cmd, data)
		}
		group.AddCommand(c)
	}
	return group
}

func epicGateCall(cmd *cobra.Command, load func() (*cliEnv, error), address, method, suffix string, payload any) (map[string]any, error) {
	id, err := strconv.ParseInt(address, 10, 64)
	if err != nil || id <= 0 {
		return nil, fmt.Errorf("Epic ID must be a positive integer")
	}
	env, err := load()
	if err != nil {
		return nil, err
	}
	var status int
	var response map[string]any
	path := fmt.Sprintf("/api/work/epics/%d%s", id, suffix)
	if method == "GET" {
		status, response, err = env.get(path)
	} else {
		status, response, err = env.write(method, path, payload)
	}
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		if problem, ok := response["error"].(map[string]any); ok && problem["gate"] != nil {
			_ = writeCommandJSON(cmd, problem["gate"])
		}
		return nil, serverRefusal("Epic gate", status, response)
	}
	data, ok := response["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Epic gate response is missing data")
	}
	return data, nil
}

var fullGitSHA = regexp.MustCompile(`(?i)^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

func epicCheckGitProvenance(override string, explicit bool) (map[string]any, error) {
	provenance := map[string]any{"git_sha": nil, "git_sha_source": "unavailable", "git_dirty": nil}
	if explicit {
		if !fullGitSHA.MatchString(override) {
			return nil, fmt.Errorf("--git-sha must be a full 40- or 64-character hexadecimal Git SHA")
		}
		provenance["git_sha"] = strings.ToLower(override)
		provenance["git_sha_source"] = "explicit"
		return provenance, nil
	}
	head := gitOut(".", "rev-parse", "HEAD")
	if !fullGitSHA.MatchString(head) {
		return provenance, nil
	}
	provenance["git_sha"] = strings.ToLower(head)
	provenance["git_sha_source"] = "checkout"
	status, err := exec.Command("git", "status", "--porcelain").Output()
	if err == nil {
		provenance["git_dirty"] = len(strings.TrimSpace(string(status))) > 0
	}
	return provenance, nil
}

const epicGatesHelp = `Named Epic transition checks, tied to the RDD delivery skills.

check <id> --phase plan-ready|implementation|completion-review|completion [--git-sha <full-sha>] [--json]
records the result and blockers in the Audit Trail without changing Epic lifecycle.
The record includes the signed-in actor, server time and caller-supplied Git SHA.
By default the CLI sends checkout HEAD and known dirty status. --git-sha supplies
an explicit full SHA with unknown dirty status. Outside Git, SHA is unavailable.
A SHA is trace information; the server does not verify it against a repository.
Passing and blocked checks are recorded; a blocked check exits nonzero.
A passing check grants no permission: each transition rechecks in its transaction.

Before approval, plan.verification declares author_context, decisions (UUIDs), criteria
({id, changed, test_case_id, boundary}), checks ({id, name, test_case_id, boundary}),
and boundaries ({name, applicable, reason when false}). Criteria/checks need distinct IDs;
check IDs are allocated on save when omitted. Obligations refer to test cases.
Optional verification.mode defaults to implementation. Explicit existing_behavior
requires only existing_behavior_reason. It requires current implementation Traces
before approval and selected completed passing test results. It requires no source
revision or candidate, and compares no historical runs or RED/GREEN ordering.
Read epics plan --help for the declaration. Current reviews and both human
approvals remain required.

Legacy active plans without obligations fail closed; preserve their approved scope.
For any active approved plan, an explicit human decision lets the current approver
run epics plan return-to-planning --reason "USER:...".
It withdraws approval, releases ownership and requires fresh review, approval and
registered verification. DONE stays closed; changed work uses a follow-up Epic.
For an explicit human recovery decision, the current approver can run recover-legacy
with {"source":"USER:...","reason":"..."}. This returns
only a legacy active plan without verification to PROPOSED, releases requirement
ownership and retains the old approval in audit. It does not publish content;
planning, review and fresh human implementation approval must follow.

All record commands take --file <JSON>, or standard input by default:
  candidate: {"revision":"tested-code-revision"}
  start-execution: {"revision":"tested-code-revision","obligation_ids":["uuid"]}
  existing-behavior execution: {"obligation_ids":["uuid"]}
  or several at once: {"items":[{"revision":"...","obligation_ids":["uuid"]}]}
Register before executing. With several obligations, register the batch, run
the tests, finalize the batch. A batch holds at most 50 items, is all-or-nothing,
and its items cannot share an obligation or a test case; never register RED and
GREEN for the same obligation in one batch. Finalize the returned run_id through evidence record --file:
  {"id":"<returned run_id>","status":"complete","native_verification":{
    "tests":[{"test_case_id":"<TestCase UUID>","result":"pass",
      "detail":{"executed_test_case":{"revision":0}}}],
    "criteria":[{"criterion_id":"<criterion UUID>","test_keys":["<TestCase UUID>"]}]}}
One actual outcome is stored per executed test. Project checks select their
TestCase through the approved plan; no extra criterion conclusion is
stored. Required test coverage comes from the links. A failed assertion
is fail; setup/build failures are error, never RED. In ordinary implementation, failure finalization must
precede registration of the passing run. A newer incomplete or nonpassing
execution blocks older passes. Passing evidence must belong to the current
candidate identity and revision. Same-SHA redeclaration needs new evidence.
Existing-behavior verification uses selected results without comparing runs or
requiring a candidate or source revision.
No candidate author context, role, procedure snapshot, runner, command,
environment or raw output is uploaded.

Ordinary implementation planning accepts implements traces or plan-only planned declarations for
files that do not yet exist. Completion requires scoped real code endpoints.
review-bundle and presentation are current reads, with each test and execution
included once by record ID. Normal review history retains findings,
coverage and resolutions without saved presentations. Record the actual
separately performed review:
  review: {"phase":"plan|implementation","reviewer_context":"separate context",
    "criteria":["uuid"],"obligations":["uuid"],"reason":"review conclusion",
    "findings":[],"scope_implemented":false}
Implementation reviews set scope_implemented=true. Findings have severity
material|note and description; the server assigns identities.
  resolve: {"review_id":"uuid","finding_id":"uuid","reason":"resolution"}
  declare-decision: {"question":"Question requiring a human decision"}
  decision: {"decision_id":"declared UUID","answer":"human decision","source":"USER:..."}
declare-decision and decision also take a batch, {"items":[<object>, ...]}: 1 to 200
distinct items written in input order in one call, all or nothing. A batch declare
prints {"items":[{"index","id","question"}]}. A refused batch writes nothing and
names the failing item: <code> — item <index> (<identity>) — <message>.

Review context attests to a separately performed review. Comparing context
strings cannot prove independent work. The candidate has no author context.
Changes to requirements, tests, results or Traces invalidate review applicability through existing records
and server ordering. Unknown applicability blocks. No fingerprints, ETags or
reviewed-state fields are introduced.

Human implementation and completion decisions remain separate. Apply them with
epics plan approve-implementation or approve-completion --reason "USER:...".
Use return-to-implementation --reason for a not-yet decision even when readiness
fails. These commands check current database state under locks and upload no
plan or presentation. They do not merge or deploy. To change active scope, the
current approver explicitly returns it to planning and obtains fresh approval.
`
