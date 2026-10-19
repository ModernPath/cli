package cmd

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func addRecordReadFlags(cmd *cobra.Command) {
	cmd.Flags().String("fields", "", "Comma-separated public fields to read")
	cmd.Flags().Bool("full", false, "Read the complete public record")
}

func validateRecordSelection(cmd *cobra.Command, allowed []string) error {
	full, _ := cmd.Flags().GetBool("full")
	if !cmd.Flags().Changed("fields") {
		return nil
	}
	if full {
		return fmt.Errorf("--fields and --full are mutually exclusive")
	}
	raw, _ := cmd.Flags().GetString("fields")
	for _, field := range strings.Split(raw, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			return fmt.Errorf("--fields must not contain empty field names")
		}
		if !slices.Contains(allowed, field) {
			return fmt.Errorf("unknown field %q; available fields: %s", field, strings.Join(allowed, ", "))
		}
	}
	return nil
}

func selectRecordFields(cmd *cobra.Command, record map[string]any, defaults, allowed, required []string) (map[string]any, error) {
	if err := validateRecordSelection(cmd, allowed); err != nil {
		return nil, err
	}
	fields := append([]string(nil), defaults...)
	full, _ := cmd.Flags().GetBool("full")
	if full {
		fields = nil
		for _, field := range allowed {
			if _, exists := record[field]; exists {
				fields = append(fields, field)
			}
		}
	} else if cmd.Flags().Changed("fields") {
		raw, _ := cmd.Flags().GetString("fields")
		fields = strings.Split(raw, ",")
	}
	fields = append(fields, required...)
	selected := make(map[string]any, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		selected[field] = record[field]
	}
	return selected, nil
}

func writeRecord(cmd *cobra.Command, selected map[string]any) error {
	if machine, _ := cmd.Flags().GetBool("json"); machine {
		return writeCommandJSON(cmd, selected)
	}
	fields := make([]string, 0, len(selected))
	for field := range selected {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		value := selected[field]
		var rendered string
		switch v := value.(type) {
		case nil:
			rendered = "—"
		case string:
			rendered = v
		default:
			b, err := json.Marshal(v)
			if err != nil {
				return err
			}
			rendered = string(b)
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", field, rendered); err != nil {
			return err
		}
	}
	return nil
}
