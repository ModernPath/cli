package cmd

// SR-CROSS-324 (GAP-013): a store-backed corpus read the process gates consume.
//
// After the store-backed flip the tasks/*-REQUIREMENTS.md ledgers are retired by
// declaration, so the three corpus-verification gates lose their file population.
// This command gives them the same corpus from the store: it reuses the
// GET /api/v1/sync/requirements read the working set already uses (cmd/workingset.go
// fetchRequirementLists), merges system and user requirements, excludes DERIVED,
// and — with --json — prints ONLY a JSON array of {external_id, kind, work_status,
// detail_md} to stdout, so a gate can JSON.parse stdout directly. Warnings go to
// stderr (REQ-CROSS-121). detail_md is the verbatim requirement detail block,
// carrying the `- **Status:**` and `- **Tests:**` bullets the gates parse.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/spf13/cobra"
)

var requirementsCorpusJSON bool

var requirementsCorpusCmd = &cobra.Command{
	Use:   "requirements-corpus",
	Short: "Emit the store's requirement corpus (SR+UR) for the process gates",
	Long: `Read the workspace's requirement corpus from the bound store and emit it for the
corpus-verification gates.

Reuses the GET /api/v1/sync/requirements read the working set already consumes,
merges system and user requirements into one list, excludes DERIVED candidates,
and — with --json — prints a JSON array of {external_id, kind, work_status,
detail_md} to stdout and nothing else, so a gate can JSON.parse stdout directly.
Warnings go to stderr (REQ-CROSS-121). detail_md is the verbatim requirement
detail block, carrying the - **Status:** and - **Tests:** bullets the gates parse.

The read is scoped to the bound system.`,
	RunE: runRequirementsCorpus,
}

func init() {
	requirementsCorpusCmd.Flags().BoolVar(&requirementsCorpusJSON, "json", false,
		"emit the corpus as a JSON array (owns stdout; REQ-CROSS-121)")
	rootCmd.AddCommand(requirementsCorpusCmd)
}

// corpusEntry is one requirement as the gates consume it. kind distinguishes a
// system requirement ("system") from a user requirement ("user"): the store
// serves the two under separate envelope keys (data.requirements /
// data.user_requirements) rather than as a per-row field, so kind is derived
// from which list a requirement came from.
type corpusEntry struct {
	ExternalID string `json:"external_id"`
	Kind       string `json:"kind"`
	WorkStatus string `json:"work_status"`
	DetailMD   string `json:"detail_md"`
}

// buildRequirementsCorpus fetches SR+UR once, merges them, drops DERIVED, and
// returns the entries sorted by external_id plus any data-quality warnings.
//
// DERIVED is already excluded server-side on the default (candidate-free) read;
// the explicit skip keeps "DERIVED excluded by default" a property of the command
// itself, not of the endpoint it happens to call.
func buildRequirementsCorpus(env *factoryEnv) ([]corpusEntry, []string, error) {
	srs, urs, err := fetchRequirementLists(env,
		fmt.Sprintf("/api/v1/sync/requirements?system_id=%d", env.SystemID))
	if err != nil {
		return nil, nil, err
	}

	entries := make([]corpusEntry, 0, len(srs)+len(urs))
	var warnings []string
	collect := func(list []any, kind string) {
		for _, r := range list {
			m, _ := r.(map[string]any)
			id := str(m, "external_id")
			if id == "" {
				continue
			}
			if str(m, "work_status") == "DERIVED" {
				continue
			}
			detail := str(m, "detail_md")
			if detail == "" {
				warnings = append(warnings, fmt.Sprintf("%s has no detail_md", id))
			}
			entries = append(entries, corpusEntry{
				ExternalID: id,
				Kind:       kind,
				WorkStatus: str(m, "work_status"),
				DetailMD:   detail,
			})
		}
	}
	collect(srs, "system")
	collect(urs, "user")
	sort.Slice(entries, func(i, j int) bool { return entries[i].ExternalID < entries[j].ExternalID })
	return entries, warnings, nil
}

// emitRequirementsCorpus writes the corpus as a JSON array to out and any
// warnings to errOut. out receives exactly the JSON array (REQ-CROSS-121):
// warnings never touch out, so a gate's JSON.parse on stdout cannot throw. An
// empty corpus is emitted as [], never null.
func emitRequirementsCorpus(out, errOut io.Writer, env *factoryEnv) error {
	entries, warnings, err := buildRequirementsCorpus(env)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		fmt.Fprintf(errOut, "requirements-corpus: %s\n", w)
	}
	blob, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(out, string(blob))
	return nil
}

func runRequirementsCorpus(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	env, err := factoryEnvLoad()
	if err != nil {
		return err
	}
	if requirementsCorpusJSON {
		return emitRequirementsCorpus(os.Stdout, os.Stderr, env)
	}
	// Default (no --json): a short human summary. The JSON array — the gates'
	// contract — is behind --json, mirroring `coverage`/`datamodel export`.
	entries, warnings, err := buildRequirementsCorpus(env)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "requirements-corpus: %s\n", w)
	}
	fmt.Printf("%d requirement(s) in the corpus for system %d (use --json to emit them)\n",
		len(entries), env.SystemID)
	return nil
}
