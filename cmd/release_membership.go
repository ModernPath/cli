package cmd

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

type releaseRequirement struct {
	kind, id string
	source   any
}

func (r releaseRequirement) key() string { return r.kind + ":" + r.id }

func requirementSelection(raw any, reviewed bool) ([]releaseRequirement, error) {
	rows, ok := raw.([]any)
	if !ok || len(rows) == 0 || len(rows) > 2000 {
		return nil, fmt.Errorf("requirements must contain 1–2000 distinct typed UUIDs")
	}
	seen := map[string]bool{}
	result := make([]releaseRequirement, 0, len(rows))
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid requirement item")
		}
		kind, _ := row["kind"].(string)
		id, _ := row["id"].(string)
		if (kind != "user_requirement" && kind != "system_requirement") || !requirementUUIDPattern.MatchString(id) {
			return nil, fmt.Errorf("each requirement needs its native kind and UUID")
		}
		for field := range row {
			if field != "kind" && field != "id" && !(reviewed && field == "expected_release_id") {
				return nil, fmt.Errorf("unsupported requirement field %q", field)
			}
		}
		item := releaseRequirement{kind: kind, id: strings.ToLower(id)}
		if reviewed {
			source, present := row["expected_release_id"]
			if !present || !nullableReleaseID(source) {
				return nil, fmt.Errorf("reviewed requirements need expected_release_id, including explicit null")
			}
			item.source = source
		}
		if seen[item.key()] {
			return nil, fmt.Errorf("duplicate requirement %s", item.key())
		}
		seen[item.key()] = true
		result = append(result, item)
	}
	return result, nil
}
func nullableReleaseID(v any) bool {
	if v == nil {
		return true
	}
	n, ok := numberAsInt(v)
	return ok && n > 0
}
func sameReleaseID(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	x, xok := numberAsInt(a)
	y, yok := numberAsInt(b)
	return xok && yok && x == y
}

func resolveMembershipTarget(env *cliEnv, requested, by string) (map[string]any, error) {
	if env.SystemID <= 0 {
		return nil, fmt.Errorf("release membership requires a bound System")
	}
	if by != "auto" && by != "id" && by != "slug" {
		return nil, fmt.Errorf("--by must be auto, id, or slug")
	}
	id, numericErr := strconv.ParseInt(requested, 10, 64)
	if by == "id" && (numericErr != nil || id <= 0) {
		return nil, fmt.Errorf("--by id requires a positive release ID")
	}
	rows, err := releaseList(env)
	if err != nil {
		return nil, err
	}
	var target map[string]any
	for _, raw := range rows {
		row := raw.(map[string]any)
		rid, _ := numberAsInt(row["id"])
		if (by != "slug" && numericErr == nil && id > 0 && rid == id) || (by != "id" && row["slug"] == requested) {
			if target != nil {
				return nil, fmt.Errorf("ambiguous release; use --by id or --by slug")
			}
			target = row
		}
	}
	if target == nil {
		return nil, fmt.Errorf("release not found in the bound System")
	}
	return target, nil
}

func newReleaseRequirementMove(load func() (*cliEnv, error)) *cobra.Command {
	c := &cobra.Command{Use: "move-requirements <id|slug>", Short: "Preview or apply an exact requirement selection to an open delivery release", Args: cobra.ExactArgs(1),
		Long: "Move selected user and system requirements between open releases, or initially assign requirements with no release or in Base. Defaults to a write-free preview. Save --json output, review it, then pass that same file with --apply. Content, lifecycle and evidence freshness are preserved. A failed request is never retried automatically."}
	c.Flags().String("file", "", "Selection JSON file, or - for stdin; --apply requires reviewed preview output")
	c.Flags().String("by", "auto", "Select by auto, id, or exact slug")
	c.Flags().Bool("apply", false, "Apply the exact reviewed preview instead of previewing")
	c.Flags().Bool("json", false, "Emit JSON; preview output is reusable as --apply input")
	c.RunE = func(c *cobra.Command, args []string) error {
		env, err := load()
		if err != nil {
			return err
		}
		by, _ := c.Flags().GetString("by")
		target, err := resolveMembershipTarget(env, args[0], by)
		if err != nil {
			return err
		}
		if target["is_base"] == true || (target["status"] != "planned" && target["status"] != "active") {
			return fmt.Errorf("target must be an open delivery release, not Base")
		}
		id, _ := numberAsInt(target["id"])
		path, _ := c.Flags().GetString("file")
		input, err := readObjectInput(c, path)
		if err != nil {
			return err
		}
		applying, _ := c.Flags().GetBool("apply")
		selectionObject := input
		action := "preview"
		if applying {
			action = "move"
			selectionObject, _ = input["apply"].(map[string]any)
			if selectionObject == nil {
				return fmt.Errorf("--apply requires the reviewed --json preview output")
			}
			if !sameReleaseID(selectionObject["system_id"], env.SystemID) || !sameReleaseID(selectionObject["target_release_id"], id) {
				return fmt.Errorf("reviewed preview names a different System or target")
			}
		} else {
			for field := range input {
				if field != "requirements" {
					return fmt.Errorf("preview input accepts only requirements; use --apply for a reviewed preview")
				}
			}
		}
		selected, err := requirementSelection(selectionObject["requirements"], applying)
		if err != nil {
			return err
		}
		if applying {
			if err := validateMembershipReceipt(input, target, env.SystemID, selected, true); err != nil {
				return fmt.Errorf("invalid reviewed preview: %w", err)
			}
		}
		items := make([]any, 0, len(selected))
		for _, r := range selected {
			row := map[string]any{"kind": r.kind, "id": r.id}
			if applying {
				row["expected_release_id"] = r.source
			}
			items = append(items, row)
		}
		status, body, err := env.sendOnce("POST", fmt.Sprintf("/api/compliance/releases/%d/requirements/%s", id, action), map[string]any{"system_id": env.SystemID, "requirements": items})
		if err != nil {
			return fmt.Errorf("requirement membership request failed; inspect membership before retrying: %w", err)
		}
		if status != 200 {
			return membershipFailure(status, body)
		}
		data, _ := body["data"].(map[string]any)
		if err := validateMembershipReceipt(data, target, env.SystemID, selected, applying); err != nil {
			return fmt.Errorf("invalid membership receipt; inspect membership before retrying: %w", err)
		}
		return writeRecord(c, data)
	}
	return c
}

func validateMembershipReceipt(data, target map[string]any, system int, requested []releaseRequirement, checkSources bool) error {
	if data == nil {
		return fmt.Errorf("missing data")
	}
	t, _ := data["target"].(map[string]any)
	if !sameReleaseID(data["system_id"], system) || !sameReleaseID(t["id"], target["id"]) || !sameReleaseID(t["system_id"], system) {
		return fmt.Errorf("different target or System")
	}
	rows, ok := data["requirements"].([]any)
	if !ok || len(rows) != len(requested) {
		return fmt.Errorf("missing or extra requirements")
	}
	wanted := map[string]releaseRequirement{}
	for _, r := range requested {
		wanted[r.key()] = r
	}
	seen := map[string]bool{}
	moving, unchanged := 0, 0
	actualSources := map[string]any{}
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid requirement row")
		}
		kind, _ := row["kind"].(string)
		id, _ := row["id"].(string)
		key := kind + ":" + strings.ToLower(id)
		expected, exists := wanted[key]
		if !exists || seen[key] {
			return fmt.Errorf("unexpected or duplicate requirement")
		}
		seen[key] = true
		source, present := row["from_release_id"]
		if !present || !nullableReleaseID(source) {
			return fmt.Errorf("invalid source membership")
		}
		if sameReleaseID(source, target["id"]) {
			if row["action"] != "unchanged" {
				return fmt.Errorf("invalid unchanged disposition")
			}
			unchanged++
		} else {
			if row["action"] != "move" {
				return fmt.Errorf("invalid move disposition")
			}
			moving++
		}
		if checkSources && !sameReleaseID(source, expected.source) && !sameReleaseID(source, target["id"]) {
			return fmt.Errorf("source differs from reviewed membership")
		}
		if to, present := row["to_release_id"]; present && !sameReleaseID(to, target["id"]) {
			return fmt.Errorf("different destination")
		}
		actualSources[key] = source
	}
	counts, _ := data["counts"].(map[string]any)
	m, mok := numberAsInt(counts["move"])
	u, uok := numberAsInt(counts["unchanged"])
	if !mok || !uok || m != int64(moving) || u != int64(unchanged) {
		return fmt.Errorf("incorrect movement counts")
	}
	apply, _ := data["apply"].(map[string]any)
	if !sameReleaseID(apply["system_id"], system) || !sameReleaseID(apply["target_release_id"], target["id"]) {
		return fmt.Errorf("invalid reusable apply scope")
	}
	applyRows, err := requirementSelection(apply["requirements"], true)
	if err != nil || len(applyRows) != len(requested) {
		return fmt.Errorf("invalid reusable apply selection")
	}
	for _, r := range applyRows {
		source, exists := actualSources[r.key()]
		if !exists || !sameReleaseID(source, r.source) {
			return fmt.Errorf("reusable apply differs from preview")
		}
	}
	return nil
}

func membershipFailure(status int, body map[string]any) error {
	code, _ := body["error"].(string)
	switch code {
	case "not_found", "forbidden", "no_creator", "items_required", "too_many_items", "duplicate_items", "invalid_item", "invalid_release_id", "expected_release_required", "release_not_open", "release_exempt", "requirement_source_changed":
		if item, ok := body["requirement"].(map[string]any); ok {
			kind, _ := item["kind"].(string)
			id, _ := item["id"].(string)
			if (kind == "system_requirement" || kind == "user_requirement") && requirementUUIDPattern.MatchString(id) && item["reason"] == code {
				return fmt.Errorf("requirement membership refused (HTTP %d): %s for %s:%s", status, code, kind, id)
			}
		}
		return fmt.Errorf("requirement membership refused (HTTP %d): %s", status, code)
	}
	if status == 404 {
		return fmt.Errorf("requirement membership endpoint unavailable; server support is required (HTTP 404)")
	}
	return fmt.Errorf("requirement membership refused (HTTP %d)", status)
}

func newReleaseMembers(load func() (*cliEnv, error)) *cobra.Command {
	c := &cobra.Command{Use: "members <id|slug>", Short: "Read exact user and system requirement membership in a release", Args: cobra.ExactArgs(1)}
	c.Flags().String("by", "auto", "Select by auto, id, or exact slug")
	c.Flags().Bool("all", false, "Read every page; separate pages are not one snapshot")
	c.Flags().Bool("json", false, "Emit JSON")
	c.RunE = func(c *cobra.Command, args []string) error {
		env, err := load()
		if err != nil {
			return err
		}
		by, _ := c.Flags().GetString("by")
		target, err := resolveMembershipTarget(env, args[0], by)
		if err != nil {
			return err
		}
		id, _ := numberAsInt(target["id"])
		all, _ := c.Flags().GetBool("all")
		items := []any{}
		seen := map[string]bool{}
		total := -1
		offset := 0
		for {
			q := url.Values{"system_id": {strconv.Itoa(env.SystemID)}, "release_id": {strconv.FormatInt(id, 10)}, "view": {"requirements"}, "limit": {"100"}, "offset": {strconv.Itoa(offset)}}
			status, body, err := env.get("/api/v1/mission-control/ledger?" + q.Encode())
			if err != nil {
				return err
			}
			if status != 200 {
				return membershipFailure(status, body)
			}
			data, _ := body["data"].(map[string]any)
			system, _ := data["system"].(map[string]any)
			page, _ := data["requirements"].(map[string]any)
			count, ok := numberAsInt(page["total_count"])
			rows, rowsOK := page["items"].([]any)
			if !sameReleaseID(system["id"], env.SystemID) || !ok || count < 0 || !rowsOK || int64(int(count)) != count {
				return fmt.Errorf("invalid Ledger membership page")
			}
			if total < 0 {
				total = int(count)
			} else if total != int(count) {
				return fmt.Errorf("membership changed between pages; retry with a quiescent inventory")
			}
			expected := total - offset
			if expected > 100 {
				expected = 100
			}
			if expected < 0 || len(rows) != expected {
				return fmt.Errorf("incomplete Ledger membership page; retry")
			}
			for _, raw := range rows {
				row, ok := raw.(map[string]any)
				if !ok {
					return fmt.Errorf("invalid membership row")
				}
				kind, _ := row["kind"].(string)
				uuid, _ := row["id"].(string)
				key := kind + ":" + strings.ToLower(uuid)
				if (kind != "user_requirement" && kind != "system_requirement") || !requirementUUIDPattern.MatchString(uuid) || !sameReleaseID(row["system_id"], env.SystemID) || !sameReleaseID(row["release_id"], id) || seen[key] {
					return fmt.Errorf("duplicate, invalid or foreign membership row")
				}
				seen[key] = true
				items = append(items, row)
			}
			offset += len(rows)
			if !all || offset == total {
				break
			}
		}
		complete := offset == total
		var next any
		if !complete {
			next = offset
		}
		return writeRecord(c, map[string]any{"system_id": env.SystemID, "target": selectKeys(target, []string{"id", "slug", "system_id"}), "items": items, "total_count": total, "complete": complete, "next_offset": next})
	}
	return c
}
