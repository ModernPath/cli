package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

// Epic 336 (D2): modernpath epics record-merge — record on a done Epic the
// merge commit of the last pull request that carried its change, once that
// pull request is merged (SR-D2-8). Core.Roadmap.Proof reads it as the Epic's
// floor: until it is recorded no captured run counts for the requirements the
// Epic takes or reaches, and after that a run counts when its commit is that
// merge commit or the chain its capture carries reaches it.

func init() {
	epicsCmd.AddCommand(newEpicsRecordMergeCommand(cliEnvLoad))
}

const epicsRecordMergeHelp = `Record on a done Epic the merge commit of the last pull request that carried
the change. It is recorded once that pull request is merged, before the run
that proves the kept or changed requirements is captured with modernization
record: until it
is recorded, no captured run counts for the requirements the Epic takes or
reaches, and after that a run counts when its commit is that merge commit or
the chain of commits its capture carries reaches it. Recording it again
replaces it. Any authorized workspace member records it, and only on a DONE
Epic.

--commit <sha> is the full 40- or 64-character commit id, in either case; the
server stores it in lower case. --agent <slug> names the coding agent that ran
the command, as the epics plan writes do; without it the signed-in person is
the actor. The command prints the recorded commit and, when the recording
replaced another, the commit it replaced; --json prints the server's answer.
A refusal prints the server's sentence: the Epic is not DONE, or the commit is
not a full id.`

func newEpicsRecordMergeCommand(load func() (*cliEnv, error)) *cobra.Command {
	command := &cobra.Command{
		Use:   "record-merge <epic-id>",
		Short: "Record a done Epic's merge commit, the floor the Proof tab reads runs from",
		Long:  epicsRecordMergeHelp,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil || id <= 0 {
				return fmt.Errorf("epic ID must be a positive integer")
			}
			commit, _ := cmd.Flags().GetString("commit")
			if commit == "" {
				return fmt.Errorf("--commit is required: the merge commit of the last pull request that carried the change, as its full id")
			}
			payload := map[string]any{"commit": commit}
			// SR-1641: --agent names the agent that ran the command; without it
			// the server attributes the write to the signed-in person.
			if agent, _ := cmd.Flags().GetString("agent"); agent != "" {
				payload["actor"] = map[string]any{"kind": "agent", "agent_slug": agent}
			}
			env, err := load()
			if err != nil {
				return err
			}
			cmd.SilenceUsage = true
			status, response, err := env.write("POST", fmt.Sprintf("/api/work/epics/%d/merge-commit", id), payload)
			if err != nil {
				return err
			}
			if status < 200 || status >= 300 {
				return serverRefusal("epics record-merge", status, response)
			}
			data, ok := response["data"].(map[string]any)
			if !ok {
				return fmt.Errorf("epics record-merge: the response is missing its data object")
			}
			if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
				return writeCommandJSON(cmd, data)
			}
			sha, _ := data["merge_commit_sha"].(string)
			fmt.Fprintf(cmd.OutOrStdout(), "recorded merge commit %s on Epic %d\n", sha, id)
			if replaced, ok := data["replaced_merge_commit_sha"].(string); ok && replaced != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "replaces %s\n", replaced)
			}
			return nil
		},
	}
	command.Flags().String("commit", "", "The merge commit of the last pull request that carried the Epic's change, as its full 40- or 64-character id (required)")
	_ = command.MarkFlagRequired("commit")
	command.Flags().String("agent", "", "Slug of the agent that ran this command; the signed-in person is the human behind it")
	command.Flags().Bool("json", false, "Emit JSON")
	return command
}
