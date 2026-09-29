package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"

	"github.com/modernpath/cli/internal/opschema"
	"github.com/spf13/cobra"
)

var requirementsListStatus string
var requirementsListKind string
var requirementsListContext string
var requirementsListQuery string
var requirementsListLimit int
var requirementsListCursor string
var requirementsListJSON bool

var requirementsCmd = &cobra.Command{
	Use:   "requirements",
	Short: "Find synced requirements",
}

var requirementsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List one page of compact requirement summaries",
	Args:  cobra.NoArgs,
	Long: `List a bounded page of synced system and user requirement summaries.

Use --cursor from a previous response to continue with the same filters. Full
requirement content remains available through working-set pull.`,
	RunE: runRequirementsList,
}

func init() {
	requirementsListCmd.Flags().StringVar(&requirementsListStatus, "status", "", "exact work_status")
	requirementsListCmd.Flags().StringVar(&requirementsListKind, "kind", "", "requirement kind: system or user")
	requirementsListCmd.Flags().StringVar(&requirementsListContext, "context", "", "exact context code")
	requirementsListCmd.Flags().StringVar(&requirementsListQuery, "query", "", "literal case-insensitive substring in ID, title, or description")
	requirementsListCmd.Flags().IntVar(&requirementsListLimit, "limit", 50, "page size (1–200)")
	requirementsListCmd.Flags().StringVar(&requirementsListCursor, "cursor", "", "continue after a prior page")
	requirementsListCmd.Flags().BoolVar(&requirementsListJSON, "json", false, "emit items and next_cursor as JSON")
	requirementsCmd.AddCommand(requirementsListCmd)
	rootCmd.AddCommand(requirementsCmd)
}

type requirementDiscoveryItem struct {
	ExternalID           string  `json:"external_id"`
	Kind                 string  `json:"kind"`
	WorkStatus           *string `json:"work_status"`
	Context              *string `json:"context"`
	Title                string  `json:"title"`
	TitleTruncated       bool    `json:"title_truncated"`
	Description          *string `json:"description"`
	DescriptionTruncated bool    `json:"description_truncated"`
}

type requirementDiscoveryPage struct {
	Items      []requirementDiscoveryItem `json:"items"`
	NextCursor *string                    `json:"next_cursor"`
}

func runRequirementsList(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true
	if err := validateRequirementsListFlags(cmd); err != nil {
		return err
	}

	env, err := factoryEnvLoad()
	if err != nil {
		return err
	}

	query := url.Values{"system_id": {fmt.Sprint(env.SystemID)}}
	for flag, value := range map[string]string{
		"status":  requirementsListStatus,
		"kind":    requirementsListKind,
		"context": requirementsListContext,
		"query":   requirementsListQuery,
		"cursor":  requirementsListCursor,
	} {
		if value != "" {
			if flag == "query" {
				query.Set("q", value)
			} else {
				query.Set(flag, value)
			}
		}
	}
	query.Set("limit", fmt.Sprint(requirementsListLimit))

	status, body, err := env.call("GET", "/api/v1/sync/requirements/discovery?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	if status == 404 {
		return fmt.Errorf("server does not support requirement discovery (HTTP 404); update the server before using requirements list")
	}
	if status != 200 {
		return serverRefusal("", status, body)
	}

	page, err := decodeRequirementDiscoveryPage(body)
	if err != nil {
		return err
	}

	if requirementsListJSON {
		return writeRequirementDiscoveryJSON(cmd.OutOrStdout(), page)
	}
	return writeRequirementDiscoveryHuman(cmd.OutOrStdout(), page)
}

func validateRequirementsListFlags(cmd *cobra.Command) error {
	if cmd.Flags().Changed("status") {
		if requirementsListStatus == "" || !slices.Contains(requirementWorkStatuses(), requirementsListStatus) {
			return fmt.Errorf("invalid --status %q; use a work_status value from the sync contract", requirementsListStatus)
		}
	}
	if cmd.Flags().Changed("kind") && requirementsListKind != "system" && requirementsListKind != "user" {
		return fmt.Errorf("invalid --kind %q; use system or user", requirementsListKind)
	}
	if cmd.Flags().Changed("context") && strings.TrimSpace(requirementsListContext) == "" {
		return fmt.Errorf("--context must not be empty")
	}
	if cmd.Flags().Changed("query") && strings.TrimSpace(requirementsListQuery) == "" {
		return fmt.Errorf("--query must not be empty")
	}
	if requirementsListLimit < 1 || requirementsListLimit > 200 {
		return fmt.Errorf("--limit must be between 1 and 200")
	}
	if cmd.Flags().Changed("cursor") {
		if requirementsListCursor == "" {
			return fmt.Errorf("--cursor must not be empty")
		}
		if _, err := base64.RawURLEncoding.DecodeString(requirementsListCursor); err != nil {
			return fmt.Errorf("invalid --cursor: expected a URL-safe cursor from a previous page")
		}
	}
	return nil
}

func requirementWorkStatuses() []string {
	spec, ok := opschema.Field("upsert_requirement", "work_status")
	if !ok {
		return nil
	}
	return spec.Enum
}

func decodeRequirementDiscoveryPage(body map[string]any) (requirementDiscoveryPage, error) {
	data := dataOf(body)
	rawItems, ok := data["items"]
	if !ok {
		return requirementDiscoveryPage{}, fmt.Errorf("server response has no discovery items")
	}
	items, ok := rawItems.([]any)
	if !ok {
		return requirementDiscoveryPage{}, fmt.Errorf("server discovery items are not a list")
	}
	rawCursor, ok := data["next_cursor"]
	if !ok {
		return requirementDiscoveryPage{}, fmt.Errorf("server response has no next_cursor")
	}
	var cursor *string
	if rawCursor != nil {
		value, ok := rawCursor.(string)
		if !ok || value == "" {
			return requirementDiscoveryPage{}, fmt.Errorf("server next_cursor is not a string or null")
		}
		cursor = &value
	}

	page := requirementDiscoveryPage{Items: make([]requirementDiscoveryItem, 0, len(items)), NextCursor: cursor}
	for i, raw := range items {
		m, ok := raw.(map[string]any)
		if !ok {
			return requirementDiscoveryPage{}, fmt.Errorf("server discovery item %d is not an object", i)
		}
		item, err := decodeRequirementDiscoveryItem(m)
		if err != nil {
			return requirementDiscoveryPage{}, fmt.Errorf("server discovery item %d: %w", i, err)
		}
		page.Items = append(page.Items, item)
	}
	return page, nil
}

func decodeRequirementDiscoveryItem(m map[string]any) (requirementDiscoveryItem, error) {
	for _, key := range []string{"external_id", "kind", "work_status", "context", "title", "title_truncated", "description", "description_truncated"} {
		if _, ok := m[key]; !ok {
			return requirementDiscoveryItem{}, fmt.Errorf("missing %q", key)
		}
	}
	for _, forbidden := range []string{"detail_md", "criteria", "relations"} {
		if _, ok := m[forbidden]; ok {
			return requirementDiscoveryItem{}, fmt.Errorf("unexpected full-content field %q", forbidden)
		}
	}
	externalID, ok := m["external_id"].(string)
	if !ok || externalID == "" {
		return requirementDiscoveryItem{}, fmt.Errorf("external_id is not a non-empty string")
	}
	kind, ok := m["kind"].(string)
	if !ok || (kind != "system" && kind != "user") {
		return requirementDiscoveryItem{}, fmt.Errorf("kind is not system or user")
	}
	workStatus, err := nullableString(m["work_status"], "work_status")
	if err != nil {
		return requirementDiscoveryItem{}, err
	}
	context, err := nullableString(m["context"], "context")
	if err != nil {
		return requirementDiscoveryItem{}, err
	}
	title, ok := m["title"].(string)
	if !ok {
		return requirementDiscoveryItem{}, fmt.Errorf("title is not a string")
	}
	titleTruncated, ok := m["title_truncated"].(bool)
	if !ok {
		return requirementDiscoveryItem{}, fmt.Errorf("title_truncated is not a boolean")
	}
	description, err := nullableString(m["description"], "description")
	if err != nil {
		return requirementDiscoveryItem{}, err
	}
	descriptionTruncated, ok := m["description_truncated"].(bool)
	if !ok {
		return requirementDiscoveryItem{}, fmt.Errorf("description_truncated is not a boolean")
	}
	return requirementDiscoveryItem{
		ExternalID: externalID, Kind: kind, WorkStatus: workStatus, Context: context,
		Title: title, TitleTruncated: titleTruncated, Description: description,
		DescriptionTruncated: descriptionTruncated,
	}, nil
}

func nullableString(value any, name string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	text, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("%s is neither a string nor null", name)
	}
	return &text, nil
}

func writeRequirementDiscoveryJSON(out io.Writer, page requirementDiscoveryPage) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(page)
}

func writeRequirementDiscoveryHuman(out io.Writer, page requirementDiscoveryPage) error {
	if len(page.Items) == 0 {
		fmt.Fprintln(out, "No matching requirements.")
	} else {
		for _, item := range page.Items {
			fmt.Fprintf(out, "%s [%s / %s] %s", item.ExternalID, item.Kind, displayNullable(item.WorkStatus), item.Title)
			if item.TitleTruncated {
				fmt.Fprint(out, " … [title truncated]")
			}
			fmt.Fprintln(out)
			fmt.Fprintf(out, "  context: %s\n", displayNullable(item.Context))
			if item.Description != nil && *item.Description != "" {
				fmt.Fprintf(out, "  %s", *item.Description)
				if item.DescriptionTruncated {
					fmt.Fprint(out, " … [description truncated]")
				}
				fmt.Fprintln(out)
			}
		}
	}
	if page.NextCursor != nil {
		fmt.Fprintf(out, "Next cursor: %s\n", *page.NextCursor)
	}
	return nil
}

func displayNullable(value *string) string {
	if value == nil {
		return "—"
	}
	return *value
}
