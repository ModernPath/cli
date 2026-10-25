package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	requirementsCmd.AddCommand(newRequirementObsoleteCommand(cliEnvLoad))
}

const maxObsoleteBatchItems = 200

func newRequirementObsoleteCommand(load func() (*cliEnv, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "obsolete [<uuid>]",
		Short: "Obsolete unpublished, unowned native requirements",
		Args:  cobra.MaximumNArgs(1),
		Long: `Mark a PROPOSED, BLOCKED, or DEFERRED requirement obsolete. The server records the authenticated actor and reason, and rejects active Epic ownership or published requirements.

With --file, obsolete up to 200 requirements in one all-or-nothing request. The file holds
{"reason"?: string, "items": [{"kind": "user"|"system", "id": <uuid>, "reason"?: string}]};
an item's own reason overrides the batch reason, and one of them is required. Either every
item is obsoleted (or reported already_obsolete), or nothing changes and the refusal names
the failing item's index and identity. --file cannot be combined with a positional id,
--kind or --reason.`,
		Example: `  modernpath requirements obsolete <uuid> --kind system --reason "The interface was removed"
  modernpath requirements obsolete --file obsolete.json`,
	}
	kind, reason, file := "", "", ""
	cmd.Flags().StringVar(&kind, "kind", "", "requirement kind: user or system (required without --file)")
	cmd.Flags().StringVar(&reason, "reason", "", "why the requirement is obsolete (required without --file)")
	cmd.Flags().StringVar(&file, "file", "", "batch JSON object file, or - for stdin")
	cmd.Flags().Bool("json", false, "emit JSON")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("file") {
			if len(args) > 0 || cmd.Flags().Changed("kind") || cmd.Flags().Changed("reason") {
				return fmt.Errorf("--file cannot be combined with a positional id, --kind or --reason")
			}
			return runRequirementObsoleteBatch(cmd, load, file)
		}
		if len(args) != 1 {
			return fmt.Errorf("requires a requirement UUID, or --file")
		}
		if !cmd.Flags().Changed("kind") || !cmd.Flags().Changed("reason") {
			return fmt.Errorf("--kind and --reason are required without --file")
		}
		if kind != "user" && kind != "system" {
			return fmt.Errorf("--kind must be user or system")
		}
		if !requirementUUIDPattern.MatchString(args[0]) {
			return fmt.Errorf("requirement id must be a native UUID")
		}
		id := strings.ToLower(args[0])
		reason = strings.TrimSpace(reason)
		if reason == "" || len(reason) > 2000 {
			return fmt.Errorf("--reason must contain 1 to 2000 characters")
		}
		env, err := load()
		if err != nil {
			return err
		}
		route := fmt.Sprintf("/api/v1/systems/%d/requirements/%s/%s/obsolete", env.SystemID, kind, url.PathEscape(id))
		status, body, err := env.write("POST", route, map[string]string{"reason": reason})
		if err != nil {
			return err
		}
		if status < 200 || status >= 300 {
			return serverRefusal("requirement obsoletion refused", status, body)
		}
		data, ok := body["data"].(map[string]any)
		if !ok {
			return fmt.Errorf("requirement obsoletion response is missing its data object")
		}
		if data["work_status"] != "OBSOLETE" || data["id"] != id {
			return fmt.Errorf("requirement obsoletion response does not confirm the requested requirement")
		}
		return writeRecord(cmd, map[string]any{"id": data["id"], "work_status": data["work_status"], "result": data["result"]})
	}
	return cmd
}

func validObsoleteReason(value any) bool {
	text, ok := value.(string)
	return ok && strings.TrimSpace(text) != "" && len(text) <= 2000
}

// runRequirementObsoleteBatch validates the batch file the way the server
// does, so a malformed batch fails before the workspace is loaded.
func runRequirementObsoleteBatch(cmd *cobra.Command, load func() (*cliEnv, error), path string) error {
	input, err := readObjectInput(cmd, path)
	if err != nil {
		return err
	}
	rawItems, ok := input["items"].([]any)
	if !ok || len(rawItems) == 0 {
		return fmt.Errorf("--file must contain a nonempty items list")
	}
	if len(rawItems) > maxObsoleteBatchItems {
		return fmt.Errorf("--file lists %d items; a batch obsoletes at most %d", len(rawItems), maxObsoleteBatchItems)
	}
	batchReason, hasBatchReason := input["reason"]
	if hasBatchReason {
		if _, isString := batchReason.(string); !isString {
			return fmt.Errorf("reason must be a string")
		}
	}
	payload := map[string]any{}
	if hasBatchReason {
		payload["reason"] = batchReason
	}
	seen := map[string]int{}
	items := make([]map[string]any, 0, len(rawItems))
	for index, raw := range rawItems {
		item, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("item %d must be an object with kind and id", index)
		}
		kind, _ := item["kind"].(string)
		if kind != "user" && kind != "system" {
			return fmt.Errorf("item %d: kind must be user or system", index)
		}
		id, _ := item["id"].(string)
		if !requirementUUIDPattern.MatchString(id) {
			return fmt.Errorf("item %d: id must be a native requirement UUID", index)
		}
		id = strings.ToLower(id)
		key := kind + " " + id
		if first, dup := seen[key]; dup {
			return fmt.Errorf("item %d repeats item %d (%s)", index, first, key)
		}
		seen[key] = index
		own, hasOwn := item["reason"]
		if hasOwn {
			if _, isString := own.(string); !isString {
				return fmt.Errorf("item %d: reason must be a string", index)
			}
		}
		if !validObsoleteReason(own) && !validObsoleteReason(batchReason) {
			return fmt.Errorf("item %d (%s) needs a reason of 1 to 2000 characters, its own or the batch reason", index, key)
		}
		sent := map[string]any{"kind": kind, "id": id}
		if hasOwn {
			sent["reason"] = own
		}
		items = append(items, sent)
	}
	payload["items"] = items

	env, err := load()
	if err != nil {
		return err
	}
	route := fmt.Sprintf("/api/v1/systems/%d/requirements/obsolete", env.SystemID)
	status, body, err := env.write("POST", route, payload)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return serverRefusal("requirement obsoletion refused", status, body)
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		return fmt.Errorf("requirement obsoletion response is missing its data object")
	}
	results, ok := data["items"].([]any)
	if !ok || len(results) != len(items) {
		return fmt.Errorf("requirement obsoletion response does not list every requested item")
	}
	receipts := make([]map[string]any, 0, len(results))
	for index, raw := range results {
		result, _ := raw.(map[string]any)
		outcome := str(result, "result")
		if fmt.Sprint(result["index"]) != fmt.Sprint(index) || result["kind"] != items[index]["kind"] ||
			result["id"] != items[index]["id"] || result["work_status"] != "OBSOLETE" ||
			(outcome != "obsoleted" && outcome != "already_obsolete") {
			return fmt.Errorf("requirement obsoletion response does not confirm item %d", index)
		}
		receipts = append(receipts, map[string]any{
			"index": index, "kind": result["kind"], "id": result["id"],
			"work_status": result["work_status"], "result": outcome,
		})
	}
	if machine, _ := cmd.Flags().GetBool("json"); machine {
		return writeCommandJSON(cmd, receipts)
	}
	for _, receipt := range receipts {
		fmt.Fprintf(cmd.OutOrStdout(), "item %d  %s %s  %s\n", receipt["index"], receipt["kind"], receipt["id"], receipt["result"])
	}
	return nil
}
