package cmd

import (
	"fmt"
	"net/url"

	"strings"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------- approve

var laneApproveText string

var processLaneApproveCmd = &cobra.Command{
	Use:   "approve <gate> [--text <decision>]",
	Short: "Answer a lane authorization as the signed-in workspace admin (one call)",
	Long: `Approve an open lane authorization of this System as the signed-in user.
--text is recorded as your decision (default "approve").

Only a workspace admin (or a platform superuser) signed in with this CLI can
approve it; the approver is always the signed-in user. It refuses before any
write when the gate is not a lane authorization. factory answer cannot
answer a lane authorization.

This approves a standing authorization for the whole System, so the kit asks
before it runs, and a delegated agent cannot run it.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processLaneApprove(env, args[0], laneApproveText)
	},
}

// processLaneApprove reads the gate, refuses anything but a lane
// authorization before any write, and posts one lane_approve action.
func processLaneApprove(env *factoryEnv, gateID, text string) error {
	if unsafeSnapshotName(gateID) {
		return fmt.Errorf("%q is not a plain external id; nothing was written", gateID)
	}
	status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/gates/%s?system_id=%d", url.PathEscape(gateID), env.SystemID), nil)
	if err != nil {
		return err
	}
	if status != 200 {
		return gateShowError(status, body, gateID)
	}
	g, _ := dataOf(body)["gate"].(map[string]any)
	if purpose := str(g, "purpose"); purpose != "lane_authorization" {
		return fmt.Errorf("%s is a %s gate, not a lane_authorization — process lane approve answers only a lane authorization; answer other gates with factory answer; nothing was written", gateID, presentPin(purpose))
	}
	req := map[string]any{"action": "lane_approve", "record": map[string]any{"external_id": gateID}}
	if t := strings.TrimSpace(text); t != "" {
		req["text"] = t
	}
	data, err := authorPost(env, req)
	if err != nil {
		return err
	}
	row, _ := data["lane_approval"].(map[string]any)
	printSuccess("approved the lane authorization %s (%s, channel %s): %s", gateID,
		firstNonEmpty(str(row, "state"), "answered"), firstNonEmpty(str(row, "answer_channel"), "cli"), firstNonEmpty(str(row, "answer"), "approve"))
	fmt.Println("small changes of its classes can now enter with `process lane enter <SR>`; every application appears in your feed")
	return nil
}
