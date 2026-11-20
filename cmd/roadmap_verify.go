package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func newRoadmapVerifyCommand(load func() (*cliEnv, error)) *cobra.Command {
	command := &cobra.Command{
		Use:   "verify",
		Short: "Check Epic assignments in the bound System",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := roadmapEnv(load)
			if err != nil {
				return err
			}
			path := "/api/v1/systems/" + strconv.Itoa(env.SystemID) + "/roadmap/verify"
			status, body, err := env.get(path)
			data, err := roadmapData(status, body, err)
			if err != nil {
				return err
			}

			violations, ok := data["violations"].([]any)
			if !ok || len(body) != 1 || len(data) != 1 {
				return fmt.Errorf("Roadmap verification response must contain only the data.violations array")
			}
			lines := make([]string, 0, len(violations))
			for index, raw := range violations {
				line, ok := formatRoadmapViolation(raw)
				if !ok {
					return fmt.Errorf("invalid Roadmap violation at position %d", index+1)
				}
				lines = append(lines, line)
			}

			if machine, _ := cmd.Flags().GetBool("json"); machine {
				if err := writeCommandJSON(cmd, map[string]any{"violations": violations}); err != nil {
					return err
				}
			} else if len(lines) == 0 {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "All Epics have valid Initiative assignments.")
				return err
			} else if _, err := fmt.Fprintln(cmd.OutOrStdout(), strings.Join(lines, "\n")); err != nil {
				return err
			}

			if len(lines) > 0 {
				return reportedError{fmt.Errorf("%d Epic assignment violations", len(lines))}
			}
			return nil
		},
	}
	command.Flags().Bool("json", false, "Emit JSON")
	return command
}

func formatRoadmapViolation(value any) (string, bool) {
	row, ok := value.(map[string]any)
	if !ok || len(row) != 3 {
		return "", false
	}
	id, validID := numberAsInt(row["epic_id"])
	problems, validProblems := row["problems"].([]any)
	initiatives, validInitiatives := row["initiative_ids"].([]any)
	if !validID || id <= 0 || !validProblems || len(problems) == 0 || !validInitiatives {
		return "", false
	}
	codes := make([]string, 0, len(problems))
	for _, problem := range problems {
		switch problem {
		case "missing_initiative", "multiple_initiatives", "release_mismatch":
			codes = append(codes, problem.(string))
		default:
			return "", false
		}
	}
	ids := make([]string, 0, len(initiatives))
	for _, initiative := range initiatives {
		id, ok := initiative.(string)
		if !ok || !requirementUUIDPattern.MatchString(id) {
			return "", false
		}
		ids = append(ids, id)
	}
	details := strings.Join(codes, ", ")
	if len(ids) > 0 {
		details += " (" + strings.Join(ids, ", ") + ")"
	}
	return fmt.Sprintf("Epic %d: %s", id, details), true
}
