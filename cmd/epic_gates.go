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
		{"review-bundle", "/review-bundle", "Read the exact content and evidence an independent reviewer must cover", false},
		{"presentation", "/completion-presentation", "Read the exact bundle for human completion approval", false},
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
		if op.write {
			c.Flags().String("file", "-", "JSON input file, or - for standard input")
		}
		c.RunE = func(cmd *cobra.Command, args []string) error {
			var payload any
			method := "GET"
			if op.write {
				method = "POST"
				file, _ := cmd.Flags().GetString("file")
				object, err := commandObject(cmd, file)
				if err != nil {
					return err
				}
				payload = object
			}
			data, err := epicGateCall(cmd, load, args[0], method, op.suffix, payload)
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
Legacy active plans without obligations fail closed; preserve their approved scope.
For an explicit human recovery decision, the current approver can run recover-legacy
with {"plan":<exact approved plan>,"source":"USER:...","reason":"..."}. This returns
only a legacy active plan without verification to PROPOSED, releases requirement
ownership and retains the old approval in audit. It does not publish content;
planning, review and fresh human implementation approval must follow.

All write commands take --file <JSON>, or standard input by default:
  candidate: {"revision":"commit-sha","author_context":"build-session"}
  start-execution: {"revision":"sha","role":"RED|GREEN","obligation_ids":["uuid"],
                    "command":"test command","environment":{"runner":"local"},
                    "runner":{"kind":"agent","agent_slug":"codex"}}
Register before running tests, then use evidence record --file result.json with the
returned run_id as id. For example, a criterion GREEN finalization is:
  {"id":"<returned run_id>","status":"complete",
   "runner":{"kind":"agent","agent_slug":"codex"},"raw_evidence":"actual output",
   "results":[{"target_type":"criterion","target_id":"<criterion UUID>",
     "test_case_ref":"<test-case UUID>","role":"GREEN","revision":"sha",
     "result":"pass","detail":{"command":"test command"}}]}
Use the same runner attribution as registration. Report
criterion results, or target_type=epic, target_id=<id>, target_clause=<check-id>
for project checks; include test_case_ref, role, revision, actual report and command.
A RED needs result=fail and detail.failure_kind=assertion plus expected_assertion.
RED must finish before GREEN starts. A newer running, failing or skipped execution
blocks an older pass. Registered outcomes/applicability are immutable; exact retries
are allowed, corrections need a new execution. GREEN uses the declared candidate.

review-bundle <id> emits the exact snapshot to pass as review.presentation.
  review: {"phase":"plan|implementation","presentation":{...},
           "reviewer_context":"independent-session","criteria":["uuid"],
           "obligations":["uuid"],"reason":"review conclusion","findings":[],
           "scope_implemented":false}
Review all criteria/obligations; implementation reviews set scope_implemented=true.
Findings have severity=material|note and description. The server assigns finding IDs.
  resolve: {"review_id":"uuid","finding_id":"uuid","reason":"resolution"}
  decision: {"decision_id":"declared-uuid","answer":"human decision","source":"USER:..."}
  declare-decision: {"question":"Question requiring a human decision"}
The server retains the question and returns content.id. Add this UUID to
plan.verification.decisions and save the plan; do not invent decision identities.

Review independence is an attestation of a separate reviewer context, recorded
with the signed-in actor. Different context strings do not prove different humans.
Only the implementation approver's account can declare candidates and register
executions, including CI registrations. Another workspace using that account is
allowed; runner attribution identifies who or what actually executed the check.

Decision answers are PROPOSED-only. Each latest attributable answer is part of
review-bundle; replacing an answer requires renewed independent plan review.
Present and retain that bundle before human implementation approval, then use
epics plan approve-implementation --presentation-file with that exact file and
--file with its plan. Do not attach a fresh bundle to an earlier human answer.
Implementation approval freezes decision answers; changes become new approved work.

presentation <id> emits the exact human completion bundle. Show it to the human;
then pass it unchanged to epics plan approve-completion <id> --file <bundle.json>.
New candidate/evidence/review invalidates the presentation. Human implementation and
completion approvals remain separate; these commands do not merge or deploy.

Finalize a valid RED before registering GREEN. If GREEN was registered too early,
finalize the original valid RED, then register and execute a new GREEN. A candidate
is needed for GREEN; it is not required before registering RED.
`
