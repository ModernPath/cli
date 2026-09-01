package cmd

// REQ-CROSS-228 (EPIC-CLI-003) — `modernpath author`: the PERMANENT
// intentional-authoring verb (the decided command surface). Once the tracked
// files retire, this is how a session creates a requirement, epic, or gate
// and advances a record — with creation and transition legality enforced by
// the server, never by client discipline. Distinct from the bulk sync
// channel, and accepted for a store-backed system.

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var (
	authorTitle            string
	authorContext          string
	authorStatus           string
	authorBody             string
	authorGateKind         string
	authorGatePurpose      string
	authorGateScope        []string
	authorGateOptions      []string
	authorGateTransition   string
	authorKind             string
	authorTo               string
	authorExpected         string
	authorGateRef          string
	authorGateFinger       string
	authorGateAnswer       string
	authorDecisionRef      string
	authorTraceFingerprint string
	authorTraceVerdict     string
	authorTraceSources     []string
	authorTracePrereqs     []string
	authorTraceRevision    string
)

var authorCmd = &cobra.Command{
	Use:   "author",
	Short: "Create or advance a process record on the store, legality server-enforced",
	Long: `Intentional single-record authoring (the permanent write path once the
tracked files retire):

  author requirement <id> --title … --context …   born PROPOSED (or DERIVED)
  author epic <id> --title …                      born in its entry state
  author gate <id> --title … [--gate-kind …]      born open, never answered
  author trace <id> --title … --verdict …          completed machine trace
  author advance <id> --kind … --to … --expected …

Automatic transitions flow; a human-gated transition demands --gate (the
exact ANSWERED gate naming the record) with --gate-fingerprint at its
current identity, or --decision USER:… for DEFERRED. The expected state
makes concurrent advances conflict instead of overwrite.`,
}

func authorEnv() (*factoryEnv, error) { return factoryEnvLoad() }

var authorRequirementCmd = &cobra.Command{
	Use:   "requirement <external-id>",
	Short: "Create a requirement (born DERIVED or PROPOSED — never further)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		fields := map[string]any{"title": authorTitle, "context": authorContext}
		if authorStatus != "" {
			fields["work_status"] = authorStatus
		}
		return authorCreate(env, "requirement", args[0], fields)
	},
}

var authorEpicCmd = &cobra.Command{
	Use:   "epic <external-id>",
	Short: "Create an epic in its entry state",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return authorCreate(env, "epic", args[0], map[string]any{"title": authorTitle})
	},
}

var authorGateCmd = &cobra.Command{
	Use:   "gate <external-id>",
	Short: "Open a decision gate (born open; answers arrive through the answer machinery)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		// A repeatable flag the caller never passed carries nothing — read it
		// only where cobra recorded it as set, so a reset default can never
		// read back as a user-supplied value.
		if !cmd.Flags().Changed("scope") {
			authorGateScope = nil
		}
		if !cmd.Flags().Changed("option") {
			authorGateOptions = nil
		}
		// Refuse a malformed option flag here rather than dropping it: a gate
		// silently born without its options is one the server refuses later,
		// or worse, one nobody can answer approvingly.
		if _, err := parseGateOptions(authorGateOptions); err != nil {
			return err
		}
		return authorCreate(env, "gate", args[0], authorGateFields())
	},
}

// REQ-CROSS-259: the store-backed declaration's gate carries a purpose, the
// exact scope naming its system, and the structured options its approving
// answer rides — the server refuses a declaration gate born without them, and
// a gate the server refuses is one nobody can clear the declaration with.
// REQ-CROSS-258 reads `transition` from the same take for its binding.
func authorGateFields() map[string]any {
	fields := map[string]any{"title": authorTitle, "gate_kind": authorGateKind}
	if authorBody != "" {
		fields["body_md"] = authorBody
	}
	if authorGatePurpose != "" {
		fields["purpose"] = authorGatePurpose
	}
	if authorGateTransition != "" {
		fields["transition"] = authorGateTransition
	}
	if len(authorGateScope) > 0 {
		fields["exact_scope"] = authorGateScope
	}
	if options, err := parseGateOptions(authorGateOptions); err == nil && len(options) > 0 {
		fields["options"] = options
	}
	return fields
}

// `--option key=label`. The key is the answer's carrier, so a flag with no key
// would build a gate that can only ever be answered in free text.
func parseGateOptions(raw []string) ([]map[string]any, error) {
	options := make([]map[string]any, 0, len(raw))
	for _, spec := range raw {
		key, label, found := strings.Cut(spec, "=")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			return nil, fmt.Errorf("--option %q: expected key=label — the option key is what an approving answer carries", spec)
		}
		options = append(options, map[string]any{"key": key, "label": strings.TrimSpace(label)})
	}
	return options, nil
}

var authorAdvanceCmd = &cobra.Command{
	Use:   "advance <external-id>",
	Short: "Advance a requirement or epic under server-enforced transition legality",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return authorAdvance(env, authorKind, args[0], authorTo, authorExpected,
			authorGateRef, authorGateFinger, authorGateAnswer, authorDecisionRef)
	},
}

var authorTraceCmd = &cobra.Command{
	Use:   "trace <external-id>",
	Short: "Record an immutable PASS/FAIL/STALE trace gate at an exact fingerprint",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		if !cmd.Flags().Changed("scope") {
			authorGateScope = nil
		}
		if !cmd.Flags().Changed("source") {
			authorTraceSources = nil
		}
		if !cmd.Flags().Changed("prerequisite") {
			authorTracePrereqs = nil
		}
		revision := authorTraceRevision
		if revision == "" {
			revision = gitHead(env.Root)
		}
		fields := map[string]any{
			"title":                          authorTitle,
			"purpose":                        authorGatePurpose,
			"transition":                     authorGateTransition,
			"exact_scope":                    authorGateScope,
			"fingerprint":                    authorTraceFingerprint,
			"verdict":                        strings.ToUpper(authorTraceVerdict),
			"sources":                        traceSources(authorTraceSources),
			"prerequisite_gate_external_ids": authorTracePrereqs,
			"application_revision":           revision,
		}
		if authorBody != "" {
			fields["body_md"] = authorBody
		}
		return authorTrace(env, args[0], fields)
	},
}

func traceSources(refs []string) []map[string]any {
	result := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		kind := strings.ToLower(strings.SplitN(ref, ":", 2)[0])
		result = append(result, map[string]any{"kind": kind, "ref": ref})
	}
	return result
}

func init() {
	for _, c := range []*cobra.Command{authorRequirementCmd, authorEpicCmd, authorGateCmd, authorTraceCmd} {
		c.Flags().StringVar(&authorTitle, "title", "", "record title (required)")
	}
	authorRequirementCmd.Flags().StringVar(&authorContext, "context", "", "bounded-context code (e.g. CROSS)")
	authorRequirementCmd.Flags().StringVar(&authorStatus, "status", "", "birth status: PROPOSED (default) or DERIVED")
	authorGateCmd.Flags().StringVar(&authorGateKind, "gate-kind", "question", "question | decision | approval_request")
	authorGateCmd.Flags().StringVar(&authorBody, "body", "", "gate body markdown")
	authorGateCmd.Flags().StringVar(&authorGatePurpose, "purpose", "",
		"what the gate decides (store_backed_activation | store_backed_clearing | …)")
	authorGateCmd.Flags().StringArrayVar(&authorGateScope, "scope", nil,
		"exact scope token the gate names, repeatable (e.g. system:243, REQ-CROSS-259)")
	authorGateCmd.Flags().StringArrayVar(&authorGateOptions, "option", nil,
		"structured option key=label, repeatable — an approving answer rides the key")
	authorGateCmd.Flags().StringVar(&authorGateTransition, "transition", "",
		"the transition this gate authorizes, e.g. PROPOSED->TODO")
	authorTraceCmd.Flags().StringVar(&authorBody, "body", "", "trace verdict details markdown")
	authorTraceCmd.Flags().StringVar(&authorGatePurpose, "purpose", "", "trace purpose, e.g. cold-review")
	authorTraceCmd.Flags().StringVar(&authorGateTransition, "transition", "", "transition held by the trace, e.g. plan->entry")
	authorTraceCmd.Flags().StringArrayVar(&authorGateScope, "scope", nil, "exact EPIC/UR/SR scope token, repeatable")
	authorTraceCmd.Flags().StringVar(&authorTraceFingerprint, "fingerprint", "", "exact packet/code fingerprint evaluated")
	authorTraceCmd.Flags().StringVar(&authorTraceVerdict, "verdict", "", "PASS | FAIL | STALE")
	authorTraceCmd.Flags().StringArrayVar(&authorTraceSources, "source", nil, "verdict source reference, repeatable")
	authorTraceCmd.Flags().StringArrayVar(&authorTracePrereqs, "prerequisite", nil, "prerequisite trace gate id, repeatable")
	authorTraceCmd.Flags().StringVar(&authorTraceRevision, "application-revision", "", "repository revision (default: HEAD)")
	authorAdvanceCmd.Flags().StringVar(&authorKind, "kind", "requirement", "requirement | epic")
	authorAdvanceCmd.Flags().StringVar(&authorTo, "to", "", "target status (required)")
	authorAdvanceCmd.Flags().StringVar(&authorExpected, "expected", "", "the record's current status (required; conflicts refuse)")
	authorAdvanceCmd.Flags().StringVar(&authorGateRef, "gate", "", "ANSWERED gate external id, for human-gated transitions")
	authorAdvanceCmd.Flags().StringVar(&authorGateFinger, "gate-fingerprint", "", "the gate's current content-shadow hash")
	authorAdvanceCmd.Flags().StringVar(&authorGateAnswer, "gate-answer", "", "the gate's stored answer, echoed — a transition rides only the answer actually given")
	authorAdvanceCmd.Flags().StringVar(&authorDecisionRef, "decision", "", "attributable USER:… reference, for DEFERRED")
	authorCmd.AddCommand(authorRequirementCmd, authorEpicCmd, authorGateCmd, authorTraceCmd, authorAdvanceCmd)
	rootCmd.AddCommand(authorCmd)
}
