package rdd

// REQ-CROSS-264: the WORKLIST half of the tasks carrier.
//
// The Epic Rollup's Tasks cell rides Epic.TasksCell, harvested at index 6 in
// ParseWorklist BEFORE its two drops — both act on the row's epic IDENTITY (a
// range row is no epic; a duplicate row's epic is already present) and neither
// says anything about the tasks the dropped row names. This file reads what
// those drops leave behind, plus the Work Rows and Blocked/Deferred sections
// the `worklist-row` loss population assigns to this carrier.

import "strings"

// ParseWorklistTaskRows drops and dedupes nothing. The census downstream is
// the one place that decides first-wins, and an unresolvable epic is named
// there as a loss rather than attached to a phantom.
func ParseWorklistTaskRows(content string) []WorklistTaskRow {
	var rows []WorklistTaskRow
	section := ""
	var header []string
	seenRollup := map[string]bool{}
	for i, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "#") {
			section = strings.ToLower(strings.TrimLeft(line, "# "))
			header = nil
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") || isSeparatorRow(trimmed) {
			continue
		}
		cells := splitCells(trimmed)
		if len(cells) < 3 {
			continue
		}
		if strings.Contains(section, "rollup") {
			if len(cells) < 11 {
				continue
			}
			id := strings.SplitN(bracketStripRe.ReplaceAllString(cell(cells, 1), ""), " ", 2)[0]
			if id == "" || strings.EqualFold(id, "Epic") {
				continue
			}
			dropped := strings.Contains(id, "..") || seenRollup[id]
			seenRollup[id] = true
			if !dropped {
				continue // this row's cell rides Epic.TasksCell
			}
			rows = append(rows, WorklistTaskRow{
				EpicID: id, Cell: strings.TrimSpace(cell(cells, 6)),
				SourcePath: worklistPath, Line: i + 1, Raw: trimmed,
			})
			continue
		}
		if !strings.Contains(section, "work rows") && !strings.Contains(section, "blocked") {
			continue
		}
		if header == nil {
			header = lowerCells(cells)
			continue
		}
		// The epic is a declaration ON the row, never an inference: a row that
		// names none carries its tasks to no home, and the census says so.
		epicID := ""
		if ids := idMentions(trimmed, "EPIC"); len(ids) > 0 {
			epicID = ids[0]
		}
		rows = append(rows, WorklistTaskRow{
			EpicID: epicID, Cell: cell(cells, 1), Title: worklistRowTitle(header, cells),
			SourcePath: worklistPath, Line: i + 1, Raw: trimmed,
		})
	}
	return rows
}

// taskCarriedWorklistLines names the WORKLIST lines whose task ids all reached
// an `upsert_task` op. The `worklist-row` loss population is what item 5
// assigned this carrier: a row whose tasks are now first-class rows is
// carried, and the report attributes the closure to the CARRIER rather than
// leaving it to the byte archive's whole-file suppression — which proves
// preservation, not queryability. A row carrying no task id (the non-TASK id
// families) falls through and stays disclosed.
func taskCarriedWorklistLines(data Data, records map[string]string, ops []Op) map[int]bool {
	_ = records // the record half of the census rides its own count group
	emitted := map[string]bool{}
	for _, op := range ops {
		if op.Type != "upsert_task" {
			continue
		}
		if id, _ := op.Payload["external_id"].(string); id != "" {
			emitted[id] = true
		}
	}
	carried := map[int]bool{}
	for _, row := range data.WorklistTaskRows {
		if row.Line == 0 || row.EpicID == "" {
			continue
		}
		ids := expandTaskRefs(row.Cell)
		if len(ids) == 0 {
			continue // no task id: not this carrier's row
		}
		all := true
		for _, id := range ids {
			if !emitted[row.EpicID+"#"+id] {
				all = false
				break
			}
		}
		if all {
			carried[row.Line] = true
		}
	}
	return carried
}

// worklistRowTitle is the work row's own scope cell when its header names one
// — the same title rule a declaring row inside a record follows.
func worklistRowTitle(header, cells []string) string {
	for _, want := range []string{"scope", "title", "item", "notes", "reason"} {
		for i, h := range header {
			if i == 0 || i >= len(cells) {
				continue
			}
			if headerKey(h) == want {
				if v := strings.TrimSpace(cells[i]); v != "" {
					return v
				}
			}
		}
	}
	return ""
}
