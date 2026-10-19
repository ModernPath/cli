package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

var requirementsListStatus string
var requirementsListKind string
var requirementsListContext string
var requirementsListQuery string
var requirementsListLimit int
var requirementsListOrder string
var requirementsListCursor string
var requirementsListJSON bool

var requirementsCmd = &cobra.Command{
	Use:   "requirements",
	Short: "Create and find native requirements",
}

var requirementsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List bounded requirement ID and title rows",
	Args:  cobra.NoArgs,
	Long: `List a bounded page of native system and user requirement ID and title rows.

Use --cursor from a previous response to continue with the same filters.`,
	RunE: runRequirementsList,
}

func init() {
	requirementsListCmd.Flags().StringVar(&requirementsListStatus, "status", "", "exact work_status")
	requirementsListCmd.Flags().StringVar(&requirementsListKind, "kind", "", "requirement kind: system or user")
	requirementsListCmd.Flags().StringVar(&requirementsListContext, "context", "", "exact context code")
	requirementsListCmd.Flags().StringVar(&requirementsListQuery, "query", "", "literal case-insensitive substring in UUID, display reference, title, or description")
	requirementsListCmd.Flags().IntVar(&requirementsListLimit, "limit", 20, "page size (1–200)")
	requirementsListCmd.Flags().StringVar(&requirementsListOrder, "order", "id", "server ordering: id or title")
	requirementsListCmd.Flags().StringVar(&requirementsListCursor, "cursor", "", "continue after a prior page")
	requirementsListCmd.Flags().BoolVar(&requirementsListJSON, "json", false, "emit items and next_cursor as JSON")
	requirementsCmd.AddCommand(requirementsListCmd)
	rootCmd.AddCommand(requirementsCmd)
}

type requirementDiscoveryItem struct {
	ID                   string  `json:"id"`
	RequirementKind      string  `json:"requirement_kind"`
	SystemID             int64   `json:"system_id"`
	DisplayRef           string  `json:"display_ref"`
	WorkStatus           *string `json:"work_status"`
	Context              *string `json:"context"`
	Title                string  `json:"title"`
	TitleTruncated       bool    `json:"title_truncated"`
	Description          *string `json:"description"`
	DescriptionTruncated bool    `json:"description_truncated"`
}

var requirementUUIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type requirementDiscoveryPage struct {
	Items      []requirementDiscoveryItem `json:"items"`
	NextCursor *string                    `json:"next_cursor"`
}

func runRequirementsList(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true
	if err := validateRequirementsListFlags(cmd); err != nil {
		return err
	}

	env, err := cliEnvLoad()
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
	query.Set("order", requirementsListOrder)

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
	if err := writeRequirementDiscoveryHuman(cmd.OutOrStdout(), page); err != nil {
		return err
	}
	if page.NextCursor != nil {
		_, err := fmt.Fprintf(cmd.ErrOrStderr(), "Next cursor: %s\n", *page.NextCursor)
		return err
	}
	return nil
}

func validateRequirementsListFlags(cmd *cobra.Command) error {
	if cmd.Flags().Changed("status") {
		if requirementsListStatus == "" || !slices.Contains(requirementWorkStatuses(), requirementsListStatus) {
			return fmt.Errorf("invalid --status %q; use %s", requirementsListStatus, strings.Join(requirementWorkStatuses(), ", "))
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
	if requirementsListOrder != "id" && requirementsListOrder != "title" {
		return fmt.Errorf("invalid --order %q; use id or title", requirementsListOrder)
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
	return []string{"PROPOSED", "IN_PROGRESS", "IN_REVIEW", "DONE", "BLOCKED", "DEFERRED", "OBSOLETE"}
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
	for _, key := range []string{"id", "requirement_kind", "system_id", "display_ref", "work_status", "context", "title", "title_truncated", "description", "description_truncated"} {
		if _, ok := m[key]; !ok {
			if key == "id" {
				return requirementDiscoveryItem{}, fmt.Errorf("server does not serve immutable requirement UUIDs (missing %q); upgrade the server before listing requirements", key)
			}
			return requirementDiscoveryItem{}, fmt.Errorf("missing %q", key)
		}
	}
	if _, retired := m["external_id"]; retired {
		return requirementDiscoveryItem{}, fmt.Errorf("server returned retired requirement field %q", "external_id")
	}
	for _, forbidden := range []string{"detail_md", "criteria", "relations"} {
		if _, ok := m[forbidden]; ok {
			return requirementDiscoveryItem{}, fmt.Errorf("unexpected full-content field %q", forbidden)
		}
	}
	id, ok := m["id"].(string)
	if !ok || !requirementUUIDPattern.MatchString(id) {
		return requirementDiscoveryItem{}, fmt.Errorf("id is not a UUID")
	}
	kind, ok := m["requirement_kind"].(string)
	if !ok || (kind != "system_requirement" && kind != "user_requirement") {
		return requirementDiscoveryItem{}, fmt.Errorf("requirement_kind is not system_requirement or user_requirement")
	}
	systemNumber, ok := m["system_id"].(json.Number)
	systemID, integerErr := systemNumber.Int64()
	if !ok || integerErr != nil || systemID < 1 {
		return requirementDiscoveryItem{}, fmt.Errorf("system_id is not a positive integer")
	}
	displayRef, ok := m["display_ref"].(string)
	if !ok || strings.TrimSpace(displayRef) == "" {
		return requirementDiscoveryItem{}, fmt.Errorf("display_ref is not a non-empty string")
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
		ID: id, RequirementKind: kind, SystemID: int64(systemID), DisplayRef: displayRef,
		WorkStatus: workStatus, Context: context,
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
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id": item.ID, "requirement_kind": item.RequirementKind,
			"display_ref": item.DisplayRef, "title": item.Title,
		})
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{"items": items, "next_cursor": page.NextCursor})
}

func writeRequirementDiscoveryHuman(out io.Writer, page requirementDiscoveryPage) error {
	if len(page.Items) == 0 {
		fmt.Fprintln(out, "No matching requirements.")
	} else {
		for _, item := range page.Items {
			title := strings.Join(strings.Fields(item.Title), " ")
			fmt.Fprintf(out, "%s %s\n", item.DisplayRef, title)
		}
	}
	return nil
}

func displayNullable(value *string) string {
	if value == nil {
		return "—"
	}
	return *value
}
