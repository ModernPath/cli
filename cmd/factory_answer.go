package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var (
	gatesKind     string
	gatesState    string
	gatesJSON     bool
	gatesAudit    bool
	gatesPage     listPage
	answerText    string
	answerOptions string
	answerSource  string
)

var factoryAnswerCmd = &cobra.Command{
	Use:   "answer <external_id>",
	Short: "Record a USER: decision on a gate (first-wins on the server)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		return factoryAnswer(env, args[0], answerText, answerOptions, answerSource)
	},
}

// factoryAnswer records a USER: decision on a gate. Extracted from the cobra
// handler (REQ-CROSS-372) so the refusal rendering is testable against a stub
// server; behavior unchanged by the extraction.
func factoryAnswer(env *factoryEnv, externalID, text, options, source string) error {
	if text == "" {
		return fmt.Errorf("--text is required — the answer is recorded verbatim as the USER: decision")
	}
	payload := map[string]any{"system_id": env.SystemID, "answer": text}
	if options != "" {
		payload["chosen_option_keys"] = strings.Split(options, ",")
	}
	if source != "" {
		payload["source_tag"] = source
	}
	// An entry/completion gate is "governed": Core.Gates.answer routes it to
	// answer_reviewed, which refuses unless the answer carries a review
	// submission proving it is made against the current reviewed state (the
	// gate's own content_fingerprint + evaluated_scope_fingerprint). No prior
	// CLI path sent it, so governed gates could only be answered in Mission
	// Control. Fetch the gate and, when governed, attach that review; a non-
	// governed gate takes none (the server refuses a review on one).
	if gs, gb, ge := env.call("GET",
		fmt.Sprintf("/api/v1/sync/gates/%s?system_id=%d", externalID, env.SystemID), nil); ge == nil && gs == 200 {
		if g, ok := dataOf(gb)["gate"].(map[string]any); ok {
			// REQ-CROSS-372: the server's own signal decides — answer_readiness
			// .requires_review — with the purpose as the fallback for a server
			// that predates it (REQ-CROSS-354 later changes the predicate in
			// one place, on the server).
			readiness, _ := g["answer_readiness"].(map[string]any)
			requires, served := readiness["requires_review"].(bool)
			purpose := str(g, "purpose")
			if (served && requires) || (!served && (purpose == "entry" || purpose == "completion")) {
				payload["review"] = map[string]any{
					"content_fingerprint":         str(g, "content_fingerprint"),
					"evaluated_scope_fingerprint": str(g, "evaluated_scope_fingerprint"),
				}
			}
		}
	}
	status, body, err := env.call("POST", "/api/v1/sync/gates/"+externalID+"/answer", payload)
	if err != nil {
		return err
	}
	switch status {
	case 200:
		gate, _ := dataOf(body)["gate"].(map[string]any)
		printSuccess("answered %s (%s): %s", str(gate, "external_id"), str(gate, "source_tag"), str(gate, "answer"))
	default:
		// REQ-CROSS-372: the server's sentence, verbatim — a first-wins 409 still
		// carries "already answered (first-wins)" and its winner; any other
		// refusal keeps the arm the server named instead of being replaced.
		return serverRefusal("answer refused", status, body)
	}
	return nil
}
