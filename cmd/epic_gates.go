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

var reviewUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

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
		{"confirm-removal", "/confirm-removal", "Record the domain owner's confirmation of a requirement the saved plan retires", true},
		{"recover-legacy", "/recover-legacy", "Apply an explicit human decision to return an active legacy plan to planning", true},
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

const epicGatesHelp = `Check Epic readiness and record independent reviews and human decisions.

check <id> --phase plan-ready|implementation|completion-review|completion records
current readiness in the Audit Trail. A passing check does not authorize an
implementation or completion transition; each is a separate human decision.

Record actual outcomes with evidence record. Each criterion is ready when its
required TestCases have passing current results and scoped source and test
Traces. Builds and regression suites remain development checks. A changed result
within review scope requires a renewed implementation review.

Record a separate review with --file:
  review: {"phase":"plan|implementation","reviewer_context":"separate context",
    "criteria":["uuid"],"obligations":["uuid"],"reason":"review conclusion",
    "findings":[],"scope_implemented":false}
  resolve: {"review_id":"uuid","finding_id":"uuid","reason":"resolution"}
  declare-decision: {"question":"Background and the question","title":"One-line title naming the choice","options":[{"key":"A","text":"First alternative","recommended":true},{"key":"B","text":"Second alternative"}],"addressee":"name@example.com"}
addressee is optional: the email of the workspace member the question waits for,
matched without regard to case or surrounding spaces; the reply names them as
addressee {id, name}. An email of no workspace member is refused and nothing is written.
title and options are optional. title is one line of 1 to 120 characters after
trimming, with no control character; Your move shows it on the decision card.
options are 2 to 6 options of "key" (1 to 8 ASCII letters or digits, unique,
case-sensitive), "text" (1 to 1000 characters) and an optional boolean
"recommended", true on at most one. Keep the background in the question text.
Any other shape is refused with invalid_human_decision and nothing is written.
  decision: {"decision_id":"declared UUID","answer":"human decision","source":"USER:..."}
  confirm-removal: {"requirement_kind":"user_requirement|system_requirement","requirement_id":"uuid","source":"USER:...","reason":"optional comment"}
confirm-removal records a removal_confirmation for one retire entry of the
saved plan of a PROPOSED Epic and prints the record. It counts while the saved
plan holds that entry with the same reason in the current planning cycle; a
changed reason or a return to planning needs a new confirmation.
declare-decision and decision also take a batch, {"items":[<object>, ...]}: 1 to 200
distinct items written in input order in one call, all or nothing. A batch declare
prints {"items":[{"index","id","question","title","options","addressee"}]}, with
addressee {id, name} only for an item that names one. A refused batch writes nothing and
names the failing item: <code> — item <index> (<identity>) — <field>: <message>,
the <field>: part only when the refusal names one. A refused single declare-decision
reads <code> — <field>: <message>.

Returning active work to planning requires the current approver's explicit
human decision. It releases requirement ownership and retains draft content,
prior decisions and proof for retained criterion identities. A new plan review
and implementation approval are required. Completion approval is separate.
`
