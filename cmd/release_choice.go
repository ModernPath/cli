package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// addReleaseChoiceFlag exposes the reviewed, server-prompted decision for
// requirement lifecycle actions. The file contains the release_choice object.
func addReleaseChoiceFlag(c *cobra.Command) {
	c.Flags().String("release-choice", "", "Reviewed Release choice JSON file (move or keep); required only when prompted")
}

func readReleaseChoice(cmd *cobra.Command) (map[string]any, error) {
	path, _ := cmd.Flags().GetString("release-choice")
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	choice, err := readObjectInput(cmd, path)
	if err != nil {
		return nil, fmt.Errorf("--release-choice: %w", err)
	}
	action, _ := choice["action"].(string)
	if action != "move" && action != "keep" {
		return nil, fmt.Errorf("--release-choice action must be move or keep")
	}
	target, ok := choice["target_release_id"]
	if !ok || !positiveJSONInteger(target) {
		return nil, fmt.Errorf("--release-choice target_release_id must be a positive integer")
	}
	requirements, ok := choice["requirements"].([]any)
	if !ok || len(requirements) == 0 {
		return nil, fmt.Errorf("--release-choice requirements must be a non-empty array copied from the prompt")
	}
	for i, raw := range requirements {
		row, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("--release-choice requirements[%d] must be an object", i)
		}
		kind, _ := row["kind"].(string)
		id, _ := row["id"].(string)
		if (kind != "user_requirement" && kind != "system_requirement") || !reviewUUIDPattern.MatchString(id) {
			return nil, fmt.Errorf("--release-choice requirements[%d] must copy kind and requirement UUID from the prompt", i)
		}
		expected, exists := row["expected_release_id"]
		if !exists || (expected != nil && !positiveJSONInteger(expected)) {
			return nil, fmt.Errorf("--release-choice requirements[%d].expected_release_id must be the prompted positive Release ID or null", i)
		}
	}
	return choice, nil
}

func positiveJSONInteger(v any) bool {
	switch n := v.(type) {
	case float64:
		return n > 0 && n == float64(int64(n))
	case int:
		return n > 0
	case int64:
		return n > 0
	case json.Number:
		i, err := n.Int64()
		return err == nil && i > 0
	default:
		return false
	}
}

// releaseChoiceRequired renders the server-owned prompt without choosing for
// the person. The IDs and expected placements are the values the response
// requires the reviewed choice file to echo.
func releaseChoiceRequired(body map[string]any) error {
	if body["error"] != "release_choice_required" {
		return nil
	}
	choice, _ := body["release_choice"].(map[string]any)
	target, _ := choice["target_release"].(map[string]any)
	lines := []string{fmt.Sprintf("release_choice_required: choose whether to move the listed requirements to Release %s (%s). No transition was applied.",
		acceptanceText(target["id"]), orDash(str(target, "name")))}
	rows, _ := choice["requirements"].([]any)
	optionalCount := 0
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		classification := "optional other-Release requirement"
		if moveRequired, exists := row["move_required"].(bool); !exists || moveRequired {
			classification = "mandatory move (must move to the active Release)"
		} else {
			optionalCount++
		}
		lines = append(lines, fmt.Sprintf("- %s %s: %s; current Release %s (%s)",
			orDash(str(row, "display_ref")), orDash(str(row, "id")),
			classification,
			acceptanceText(row["release_id"]), orDash(str(row, "release_name"))))
	}
	guidance := `Mandatory rows always move. Pass --release-choice <file> with {"action":"move","target_release_id":<prompted id>,"requirements":[{"kind":"user_requirement|system_requirement","id":"<prompted UUID>","expected_release_id":<prompted id or null}]}. Echo every prompted requirement (do not send move_required; the server is authoritative); a stale choice will be prompted again.`
	if optionalCount > 0 {
		guidance = `Choose move or keep once for all requirements already in another Release; mandatory rows always move. Pass --release-choice <file> with {"action":"move"|"keep","target_release_id":<prompted id>,"requirements":[{"kind":"user_requirement|system_requirement","id":"<prompted UUID>","expected_release_id":<prompted id or null}]}. Echo every prompted requirement (do not send move_required; the server is authoritative); a stale choice will be prompted again.`
	}
	lines = append(lines, guidance)
	return fmt.Errorf("%s", strings.Join(lines, "\n"))
}
