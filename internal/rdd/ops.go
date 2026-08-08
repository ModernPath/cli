// Op-builder — Go port of mission-control/cli/ops.js (REQ-CROSS-013,
// TASK-SY-404). Same op shapes, same ordering, same content hashes: canonical
// JSON (sorted keys, JS JSON.stringify string escaping) sha256'd — so the
// switch from the node extractor is hash-stable and causes zero re-sync churn.
package rdd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Actor: P2 (attributable actors) — every sync write names its agent.
// Kept "mp-cli" for hash parity with the node op-builder.
var actor = map[string]any{"kind": "agent", "agent_slug": "mp-cli"}

// Op is one typed sync op. Payloads are map[string]any trees of
// string | int | nil | []any | map[string]any.
type Op struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

// ---------------------------------------------------------------- canonical hash

// canonical mirrors ops.js canonical(): arrays in order, object keys sorted,
// scalars JSON-encoded the way JS JSON.stringify does.
func canonical(v any) string {
	switch val := v.(type) {
	case nil:
		return "null"
	case string:
		return jsString(val)
	case int:
		return strconv.Itoa(val)
	case bool:
		return strconv.FormatBool(val)
	case []any:
		parts := make([]string, len(val))
		for i, item := range val {
			parts[i] = canonical(item)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case []string:
		parts := make([]string, len(val))
		for i, item := range val {
			parts[i] = canonical(item)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case []map[string]any:
		parts := make([]string, len(val))
		for i, item := range val {
			parts[i] = canonical(item)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = jsString(k) + ":" + canonical(val[k])
		}
		return "{" + strings.Join(parts, ",") + "}"
	default:
		panic(fmt.Sprintf("canonical: unsupported type %T", v))
	}
}

// jsString = JSON.stringify(s): escape `"` `\` and control chars only
// (no HTML escaping, no   handling — matching V8 for BMP text).
func jsString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(fmt.Sprintf(`\u%04x`, r))
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// ContentHash = ops.js contentHash: sha256 hex over the canonical form.
func ContentHash(payload map[string]any) string {
	sum := sha256.Sum256([]byte(canonical(payload)))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------- criteria + citations

var (
	criteriaHeadRe  = regexp.MustCompile(`^-\s+\*\*Acceptance criteria:\*\*`)
	fieldHeadRe     = regexp.MustCompile(`^-\s+\*\*`)
	criterionLineRe = regexp.MustCompile(`^\s+-\s+(.*\S)\s*$`)
	gwtRe           = regexp.MustCompile(`(?s)^GIVEN\s+(.*?)(?:\s+WHEN\s+(.*?))?\s+THEN\s+(.*)$`)
	citationSplitRe = regexp.MustCompile(`\s*[·,]\s*`)
	userRefRe       = regexp.MustCompile(`^USER:`)
	epicRefRe       = regexp.MustCompile(`^EPIC-`)
	ruleRefRe       = regexp.MustCompile(`^(INV|BR|RQ)-`)
)

// ParseCriteria pulls the acceptance-criteria bullets out of a ledger detail
// block: GWT parts (WHEN optional) or a plain statement.
func ParseCriteria(detail, reqID string) []any {
	lines := strings.Split(detail, "\n")
	start := -1
	for i, l := range lines {
		if criteriaHeadRe.MatchString(l) {
			start = i
			break
		}
	}
	if start == -1 {
		return []any{}
	}
	criteria := []any{}
	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if fieldHeadRe.MatchString(line) {
			break // next top-level field
		}
		m := criterionLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		text := m[1]
		position := len(criteria) + 1
		c := map[string]any{
			"external_id": fmt.Sprintf("%s#AC%d", reqID, position),
			"position":    position,
			"kind":        "criterion",
		}
		if g := gwtRe.FindStringSubmatch(text); g != nil {
			c["given"] = g[1]
			if g[2] != "" {
				c["when"] = g[2]
			}
			c["then"] = g[3]
		} else {
			c["statement"] = text
		}
		criteria = append(criteria, c)
	}
	return criteria
}

// ParseCitations turns a ledger Source cell into structured citations.
func ParseCitations(source string) []any {
	if source == "" || source == "—" {
		return []any{}
	}
	out := []any{}
	for _, s := range citationSplitRe.Split(source, -1) {
		ref := strings.TrimSpace(strings.ReplaceAll(s, "`", ""))
		if ref == "" {
			continue
		}
		kind := "doc"
		switch {
		case userRefRe.MatchString(ref):
			kind = "user"
		case epicRefRe.MatchString(ref):
			kind = "epic"
		case ruleRefRe.MatchString(ref):
			kind = "rule"
		}
		out = append(out, map[string]any{"kind": kind, "ref": ref})
	}
	return out
}

// ---------------------------------------------------------------- op builders

// REQ-PLN-054 / D-MC-6 (USER:2026-08-07): the row's one-line description.
// Preference order, decided from ledger coverage (63 of 201 blocks carry a
// Statement at decision time):
//  1. the detail block's **Statement:** line — already the one-line summary;
//  2. else the first acceptance criterion's THEN clause — the outcome half
//     reads as a summary where the full GIVEN/WHEN/THEN reads as a test;
//  3. else empty — a blank beats an invented sentence (#9).
var (
	statementLineRe = regexp.MustCompile(`(?m)^\s*-\s*\*\*Statement:\*\*\s*(.+?)\s*$`)
	thenClauseRe    = regexp.MustCompile(`(?i)\bTHEN\s+(.+)$`)
)

func ParseDescription(detail string) string {
	if detail == "" {
		return ""
	}
	if m := statementLineRe.FindStringSubmatch(detail); m != nil {
		return strings.TrimSpace(m[1])
	}
	for _, raw := range ParseCriteria(detail, "") {
		c, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		text, _ := c["then"].(string)
		if text == "" {
			// GWT criteria store parts; plain ones store the whole statement.
			if stmt, ok := c["statement"].(string); ok {
				if m := thenClauseRe.FindStringSubmatch(stmt); m != nil {
					text = m[1]
				}
			}
		}
		if text = strings.TrimSpace(text); text != "" {
			// Sentence-case the clause so it reads as prose, not a fragment.
			r := []rune(text)
			return strings.TrimSuffix(strings.ToUpper(string(r[0]))+string(r[1:]), ".") + "."
		}
	}
	return ""
}

func BuildRequirementOp(req Req) Op {
	var stage any
	if req.Stage != "" {
		stage = req.Stage
	}
	payload := map[string]any{
		"external_id":      req.ID,
		"kind":             "system",
		"title":            req.Title,
		"context":          req.Ctx,
		"stage":            stage,
		"work_status":      req.Status,
		"source_citations": ParseCitations(req.Source),
		"criteria":         ParseCriteria(req.Detail, req.ID),
		"description":      ParseDescription(req.Detail),
	}
	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor // after hashing — who synced is not content identity
	return Op{Type: "upsert_requirement", Payload: payload}
}

var (
	epicH1Re          = regexp.MustCompile(`(?m)^#\s+\S+\s+—\s+(.+)$`)
	reqSectionRe      = regexp.MustCompile(`(?s)## Requirements in this epic\n(.*?)(\n##|$)`)
	realizesLineRe    = regexp.MustCompile(`(?m)^-\s*\*\*Realizes:\*\*\s*(.+)$`)
	approvalSectionRe = regexp.MustCompile(`(?s)## Approval\n(.*?)(\n##|$)`)
	approvalPendingRe = regexp.MustCompile(`(?i)_pending`)
	userTagRe         = regexp.MustCompile(`USER:\d{4}-\d{2}-\d{2}`)
	approvedLineRe    = regexp.MustCompile(`(?m)^\*{0,2}APPROVED\b[^\n]*USER:\d{4}-\d{2}-\d{2}[^\n]*$`)
	reqIDGlobalRe     = regexp.MustCompile(`REQ-[A-Z]+-\d+`)
)

// approvalLineOf finds the epic record's recorded approval, if any: an
// "## Approval" section carrying a USER: tag (not marked pending), or a line
// starting with **APPROVED carrying one.
func approvalLineOf(text string) string {
	if section := approvalSectionRe.FindStringSubmatch(text); section != nil && !approvalPendingRe.MatchString(section[1]) {
		line := strings.SplitN(strings.TrimSpace(section[1]), "\n", 2)[0]
		if userTagRe.MatchString(line) {
			return line
		}
	}
	if m := approvedLineRe.FindString(text); m != "" {
		return m
	}
	return ""
}

func requirementIDsOf(text string) []string {
	linkSource := text
	if section := reqSectionRe.FindStringSubmatch(text); section != nil {
		linkSource = section[1]
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, id := range reqIDGlobalRe.FindAllString(linkSource, -1) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// specContentCap mirrors the schema's $defs.spec content_md maxLength — the
// builders cap-and-warn, so an oversize spec can never reach the wire
// (REQ-CROSS-023; warning emitted at snapshot time, never in the payload).
const specContentCap = 65536

// `(?m)` is deliberately absent: with it, `$` would match the first line end
// and truncate a wrapped paragraph to its first line.
var epicUserOutcomeRe = regexp.MustCompile(`(?s)(?:^|\n)##\s+User outcome[^\n]*\n(.*?)(\n##|$)`)

// ParseEpicDescription — the epic's "## User outcome" section, first paragraph,
// as one line. 105 of 139 records carry the heading; the rest get no
// description rather than an invented one (same honesty rule as
// ParseDescription).
func ParseEpicDescription(recordText string) string {
	m := epicUserOutcomeRe.FindStringSubmatch(recordText)
	if m == nil {
		return ""
	}
	para := strings.TrimSpace(strings.SplitN(strings.TrimSpace(m[1]), "\n\n", 2)[0])
	return capRunes(strings.Join(strings.Fields(para), " "), 600)
}

// folderForm reports whether the epic record is the folder convention
// (…/EPIC.md) — the presence rule for the specs payload key.
func folderForm(epic Epic) bool {
	return strings.HasSuffix(epic.Record, "/EPIC.md")
}

// ---------------------------------------------------------------- scenarios

var (
	scenarioSectionRe = regexp.MustCompile(`(?s)(?:^|\n)##\s+(?:BDD )?[Aa]cceptance scenarios[^\n]*\n(.*?)(\n## |\z)`)
	scenarioRowRe     = regexp.MustCompile(`^\s*\|`)
	tableDividerRe    = regexp.MustCompile(`^\s*\|[\s\-:|]+\|\s*$`)
	scnIDRe           = regexp.MustCompile(`\bSCN-[A-Z]+-\d+\b`)
)

// textColumns are the header names that hold the scenario's prose, in the order
// they are preferred. Six header shapes exist across the epic records; naming
// the column beats guessing, because an evidence cell can be longer than the
// summary it describes.
var textColumns = []string{"gherkin", "statement", "summary", "description"}

// ParseScenarios — the epic record's acceptance-scenario table as scenario-kind
// criteria (REQ-CROSS-028). The server has accepted these since M4 (Core.Sync
// routes payload `scenarios` through sync_criteria with owner :initiative_id);
// no builder has ever sent one. Returns an empty slice when the record has no
// table, so BuildEpicOp can omit the key and leave the content hash alone.
//
// Ids are namespaced by their epic — "EPIC-FE-034#SCN-AK-001" — exactly as
// requirement criteria are ("REQ-CMP-010#AC1"). SCN ids are scoped by AREA, not
// by epic (85 prefixes across the records), so two different epics working one
// area legitimately reach for the same id: SCN-AK-001 belongs to both
// EPIC-FE-034 and EPIC-PORTFOLIO-038. Since acceptance_criteria.external_id is
// unique per tenant, the bare id let the second sync steal the first's row and
// 9 epics served no scenarios at all (RUN:2026-08-07, found by reading beta
// back after deploy). Namespacing makes that impossible by construction.
func ParseScenarios(recordText, epicID string) []any {
	m := scenarioSectionRe.FindStringSubmatch(recordText)
	if m == nil {
		return []any{}
	}
	var header []string
	scenarios := []any{}
	for _, line := range strings.Split(m[1], "\n") {
		if !scenarioRowRe.MatchString(line) || tableDividerRe.MatchString(line) {
			continue
		}
		cells := splitCells(line)
		if len(cells) < 3 { // splitCells keeps the leading/trailing empties
			continue
		}
		cells = cells[1 : len(cells)-1]
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if header == nil {
			header = cells
			continue
		}

		idAt := -1
		for i, c := range cells {
			if scnIDRe.MatchString(c) {
				idAt = i
				break
			}
		}
		if idAt == -1 {
			continue // not a scenario row (a stray table, a note)
		}

		text := scenarioText(header, cells, idAt)
		if text == "" {
			continue
		}
		position := len(scenarios) + 1
		c := map[string]any{
			"external_id": epicID + "#" + scnIDRe.FindString(cells[idAt]),
			"position":    position,
			"kind":        "scenario",
		}
		if g := gwtRe.FindStringSubmatch(text); g != nil {
			c["given"] = g[1]
			if g[2] != "" {
				c["when"] = g[2]
			}
			c["then"] = g[3]
		} else {
			c["statement"] = text
		}
		scenarios = append(scenarios, c)
	}
	return scenarios
}

// scenarioText picks the prose cell: the named text column when the header has
// one, else the longest remaining cell.
func scenarioText(header, cells []string, idAt int) string {
	for _, want := range textColumns {
		for i, h := range header {
			if i != idAt && i < len(cells) && strings.ToLower(strings.TrimSpace(h)) == want {
				return cells[i]
			}
		}
	}
	best := ""
	for i, c := range cells {
		if i != idAt && len(c) > len(best) {
			best = c
		}
	}
	return best
}

func BuildEpicOp(epic Epic, recordText string) Op {
	title := epic.ID
	if h1 := epicH1Re.FindStringSubmatch(recordText); h1 != nil {
		title = strings.TrimSpace(h1[1])
	}
	ids := requirementIDsOf(recordText)
	idsAny := make([]any, len(ids))
	for i, id := range ids {
		idsAny[i] = id
	}
	payload := map[string]any{
		"external_id":              epic.ID,
		"title":                    title,
		"requirement_external_ids": idsAny,
	}
	// REQ-PLN-054 TASK-MC-107: the user outcome is what an epic IS about; without
	// it the app can only show an id and a title.
	if desc := ParseEpicDescription(recordText); desc != "" {
		payload["description"] = desc
	}
	// REQ-CROSS-028: the loop state WORKLIST already records, into the two
	// fields the contract has always had. Absent cells stay absent — an epic
	// with nothing recorded keeps the hash it had before this shipped.
	// statusCell again here, not only in ParseWorklist: the placeholder rule is
	// a property of the payload, so it holds for any caller building an op.
	if upper := statusCell(epic.Upper); upper != "" {
		payload["upper_loop_status"] = upper
	}
	if lower := statusCell(epic.Lower); lower != "" {
		payload["lower_loop_status"] = lower
	}
	if scenarios := ParseScenarios(recordText, epic.ID); len(scenarios) > 0 {
		payload["scenarios"] = scenarios
	}
	// Presence rule (REQ-CROSS-023): folder-form epics ALWAYS carry the key —
	// present-empty archives all sync-owned artifacts server-side; file-form
	// epics never touch them.
	if folderForm(epic) {
		specs := make([]any, len(epic.Specs))
		for i, s := range epic.Specs {
			specs[i] = map[string]any{
				"external_id": s.Rel,
				"name":        s.Name,
				"position":    i + 1,
				"content_md":  capRunes(s.Content, specContentCap),
			}
		}
		payload["specs"] = specs
	}
	if line := approvalLineOf(recordText); line != "" {
		tag := userTagRe.FindString(line)
		payload["approval"] = map[string]any{
			"approved_at": tag[5:] + "T00:00:00.000000Z",
			"basis":       capRunes(line, 300),
			"source_tag":  tag,
		}
	}
	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	return Op{Type: "upsert_epic", Payload: payload}
}

// BuildRQGateOp — docs/85 decision requests → decision gates. Honesty rules
// as ops.js: answered only with a real source; closed-without-source dismissed.
func BuildRQGateOp(item RQ) Op {
	text := item.Heading + "\n" + item.Body
	userTag := userTagRe.FindString(text)
	closed := item.State == "closed"

	openedAt := "2026-07-01T00:00:00.000000Z"
	if item.Date != "" {
		openedAt = item.Date + "T00:00:00.000000Z"
	}
	originRef := "docs/85-loop-review-queue.md"
	if item.Line > 0 {
		originRef = fmt.Sprintf("%s:%d", originRef, item.Line)
	}
	title := item.Title
	if title == "" {
		title = item.ID
	}
	payload := map[string]any{
		"external_id": item.ID,
		"kind":        "decision",
		"title":       title,
		"body_md":     item.Body,
		"origin":      "workspace",
		"origin_ref":  originRef,
		"opened_at":   openedAt,
	}
	addGateOptions(payload, item.Options, item.Recommendation)
	addRichGateOptions(payload, item.Body)
	addGateBrief(payload, item.Body)

	switch {
	case closed && userTag != "":
		payload["state"] = "answered"
		payload["answered_at"] = openedAt
		payload["source_tag"] = userTag
		payload["answer"] = fmt.Sprintf("Resolved in the record — see %s in docs/85.", item.ID)
	case closed:
		payload["state"] = "dismissed"
	default:
		payload["state"] = "open"
	}
	addGateHolds(payload, item.Pool, "pooled behind "+item.ID)

	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	if payload["state"] == "answered" {
		payload["answerer"] = map[string]any{"kind": "human"}
	}
	return Op{Type: "upsert_gate", Payload: payload}
}

// BuildOQGateOp — process/08 open questions → question gates.
func BuildOQGateOp(item OQ) Op {
	openedAt := "2026-07-01T00:00:00.000000Z"
	originRef := "process/08-open-questions.md"
	if item.Line > 0 {
		originRef = fmt.Sprintf("%s:%d", originRef, item.Line)
	}
	title := item.Title
	if title == "" {
		title = item.ID
	}
	payload := map[string]any{
		"external_id": item.ID,
		"kind":        "question",
		"title":       title,
		"body_md":     item.Full,
		"origin":      "workspace",
		"origin_ref":  originRef,
		"opened_at":   openedAt,
	}
	if item.Resolved {
		userTag := userTagRe.FindString(item.Full)
		payload["state"] = "answered"
		payload["answered_at"] = openedAt
		if userTag != "" {
			payload["source_tag"] = userTag
		} else {
			payload["source_tag"] = "DOC:process/08-open-questions.md"
		}
		payload["answer"] = item.Full
	} else {
		payload["state"] = "open"
	}
	addGateHolds(payload, item.Pool, "pooled behind "+item.ID)

	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	if payload["state"] == "answered" {
		payload["answerer"] = map[string]any{"kind": "human"}
	}
	return Op{Type: "upsert_gate", Payload: payload}
}

var specStatusSectionRe = regexp.MustCompile(`(?s)## Specification status\s*\n(.*?)(\n## |\z)`)

// SpecApprovalLineOf classifies the `## Specification status` marker
// (REQ-CROSS-024, EPIC-SYNC-006): the FIRST non-empty line of the section.
// Returns ("", "") for legacy records (no section / no recognizable marker).
// "approved" requires a USER:YYYY-MM-DD tag on the line — a claimed approval
// without a source stays at "ready" (the gate stays open; same honesty rule
// as approvalLineOf).
func SpecApprovalLineOf(text string) (marker string, line string) {
	section := specStatusSectionRe.FindStringSubmatch(text)
	if section == nil {
		return "", ""
	}
	for _, l := range strings.Split(section[1], "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		switch {
		case strings.HasPrefix(l, "SPEC-DRAFT"):
			return "draft", l
		case strings.HasPrefix(l, "SPEC-READY"):
			return "ready", l
		case strings.HasPrefix(l, "SPEC-APPROVED"):
			if userTagRe.MatchString(l) {
				return "approved", l
			}
			return "ready", l
		default:
			return "", ""
		}
	}
	return "", ""
}

// BuildSpecApprovalGateOp — the pre-implementation specification gate
// (REQ-CROSS-024): SPEC-READY opens SPEC-APPROVE-<EPIC>, SPEC-APPROVED with a
// USER: tag closes it. Only folder-form epics WITH specs gate — an approval
// request over nothing is dishonest (the missing-specs case is warned at
// snapshot time). Holds the EPIC (the implementation-approval gate holds the
// requirements — different concern).
func BuildSpecApprovalGateOp(epic Epic, recordText string) (Op, bool) {
	marker, line := SpecApprovalLineOf(recordText)
	if !folderForm(epic) || len(epic.Specs) == 0 || (marker != "ready" && marker != "approved") {
		return Op{}, false
	}

	title := "Approve specs: " + epic.ID
	if h1 := epicH1Re.FindStringSubmatch(recordText); h1 != nil {
		title += " — " + strings.TrimSpace(h1[1])
	}

	var index strings.Builder
	index.WriteString("Specs awaiting approval:\n")
	for _, s := range epic.Specs {
		fmt.Fprintf(&index, "- %s (%d chars)\n", s.Rel, len([]rune(s.Content)))
	}
	index.WriteString("\n" + line + "\n")
	if ids := requirementIDsOfSection(recordText); len(ids) > 0 {
		index.WriteString("\nRequirements: " + strings.Join(ids, " · ") + "\n")
	}

	payload := map[string]any{
		"external_id": "SPEC-APPROVE-" + epic.ID,
		"kind":        "spec_approval",
		"title":       title,
		"body_md":     capRunes(index.String(), 6000),
		"origin":      "workspace",
		"origin_ref":  epic.Record,
		"opened_at":   "2026-08-09T00:00:00.000000Z",
		"options": []any{
			map[string]any{"key": "approve", "label": "Approve specs — implementation may start", "body": "Record the approval (USER: tag) in the record's Specification status; the loop may then write its first RED test."},
			map[string]any{"key": "request_changes", "label": "Request changes", "body": "Send the specs back with what must change before approval."},
			map[string]any{"key": "defer", "label": "Defer", "body": "Keep the epic at the specification stage."},
		},
		"recommendation": "The specs/ folder is the review pack — grounded claims only; ungrounded content is grounds for changes.",
		"holds": []any{
			map[string]any{"held_external_id": epic.ID, "held_entity_type": "epic", "note": "specs awaiting approval"},
		},
	}

	if marker == "approved" {
		tag := userTagRe.FindString(line)
		payload["state"] = "answered"
		payload["answered_at"] = tag[5:] + "T00:00:00.000000Z"
		payload["source_tag"] = tag
		payload["answer"] = capRunes(line, 300)
		payload["chosen_option_keys"] = []any{"approve"}
	} else {
		payload["state"] = "open"
	}

	addGateBrief(payload, recordText)
	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	if payload["state"] == "answered" {
		payload["answerer"] = map[string]any{"kind": "human"}
	}
	return Op{Type: "upsert_gate", Payload: payload}, true
}

// BuildApprovalGateOp — an epic at the human gate IS a queue item; a recorded
// approval closes the same gate as a repo-borne answer. Returns zero Op
// (ok=false) when the epic is neither.
func BuildApprovalGateOp(epic Epic, recordText string) (Op, bool) {
	approvalLine := approvalLineOf(recordText)
	awaiting := epic.State == "awaiting-approval"
	if !awaiting && approvalLine == "" {
		return Op{}, false
	}

	title := "Approve: " + epic.ID
	if h1 := epicH1Re.FindStringSubmatch(recordText); h1 != nil {
		title += " — " + strings.TrimSpace(h1[1])
	}
	originRef := epic.Record
	if originRef == "" {
		originRef = "WORKLIST.md"
	}
	payload := map[string]any{
		"external_id": "APPROVE-" + epic.ID,
		"kind":        "approval_request",
		"title":       title,
		"body_md":     capRunes(recordText, 6000),
		"origin":      "workspace",
		"origin_ref":  originRef,
		"opened_at":   "2026-07-22T00:00:00.000000Z",
		"options": []any{
			map[string]any{"key": "approve", "label": "Approve — evidence reviewed", "body": "Record the approval (USER: tag); the loop materializes it into the epic record."},
			map[string]any{"key": "request_changes", "label": "Request changes", "body": "Send it back with what must change before the gate."},
			map[string]any{"key": "defer", "label": "Defer", "body": "Not now — keep it at the gate."},
		},
		"recommendation": "The record’s evidence map is the review pack — read it, then approve, request changes, or defer.",
	}
	addGateHolds(payload, requirementIDsOfSection(recordText), "awaiting the "+epic.ID+" gate")

	if approvalLine != "" {
		tag := userTagRe.FindString(approvalLine)
		payload["state"] = "answered"
		payload["answered_at"] = tag[5:] + "T00:00:00.000000Z"
		payload["source_tag"] = tag
		payload["answer"] = capRunes(approvalLine, 300)
		payload["chosen_option_keys"] = []any{"approve"}
	} else {
		payload["state"] = "open"
	}

	addGateBrief(payload, recordText)
	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	if payload["state"] == "answered" {
		payload["answerer"] = map[string]any{"kind": "human"}
	}
	return Op{Type: "upsert_gate", Payload: payload}, true
}

// requirementIDsOfSection: ONLY the "## Requirements in this epic" section
// (ops.js buildApprovalGateOp matches against ” when the section is absent —
// unlike buildEpicOp's whole-record fallback).
func requirementIDsOfSection(text string) []string {
	section := reqSectionRe.FindStringSubmatch(text)
	if section == nil {
		// Folder epics (EPIC-SYNC-006 convention) declare scope on a
		// **Realizes:** header line instead of the old section — read it so
		// approval-gate holds (and the impact strip they feed) survive the
		// convention change (RUN:2026-08-06 browser-leg finding).
		if realizes := realizesLineRe.FindStringSubmatch(text); realizes != nil {
			section = []string{"", realizes[1], ""}
		} else {
			return nil
		}
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, id := range reqIDGlobalRe.FindAllString(section[1], -1) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

func addGateOptions(payload map[string]any, options []Option, recommendation string) {
	if len(options) > 0 {
		opts := make([]any, len(options))
		for i, o := range options {
			label := o.Label
			if label == "" {
				label = fmt.Sprintf("(%d)", i+1)
			}
			opts[i] = map[string]any{
				"key":   label,
				"label": capRunes(o.Text, 120),
				"body":  o.Text,
			}
		}
		payload["options"] = opts
	}
	if recommendation != "" {
		payload["recommendation"] = recommendation
	}
}

func addGateHolds(payload map[string]any, pool []string, note string) {
	if len(pool) == 0 {
		return
	}
	holds := make([]any, len(pool))
	for i, id := range pool {
		entityType := "requirement"
		if epicRefRe.MatchString(id) {
			entityType = "epic"
		}
		holds[i] = map[string]any{
			"held_external_id": id,
			"held_entity_type": entityType,
			"note":             note,
		}
	}
	payload["holds"] = holds
}

// BuildCommitBurstOp folds the recent git log into one commit_burst event per
// CLOSED day (today's burst is still growing — it lands once the day closes).
func BuildCommitBurstOp(commits []Commit, today string) (Op, bool) {
	byDay := map[string][]Commit{}
	var days []string
	for _, c := range commits {
		if len(c.Date) < 10 {
			continue
		}
		day := c.Date[:10]
		if day >= today {
			continue
		}
		if _, ok := byDay[day]; !ok {
			days = append(days, day)
		}
		byDay[day] = append(byDay[day], c)
	}
	if len(byDay) == 0 {
		return Op{}, false
	}
	sort.Sort(sort.Reverse(sort.StringSlice(days)))

	events := make([]any, 0, len(days))
	for _, day := range days {
		dayCommits := byDay[day]
		ids := []string{}
		seen := map[string]bool{}
		for _, c := range dayCommits {
			for _, id := range c.IDs {
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
		}
		related := make([]any, len(ids))
		for i, id := range ids {
			related[i] = map[string]any{"external_id": id, "type": "requirement"}
		}
		subjects := []any{}
		for i, c := range dayCommits {
			if i == 5 {
				break
			}
			subjects = append(subjects, c.Subject)
		}
		events = append(events, map[string]any{
			"occurred_at":         day + "T23:59:59.000000Z",
			"event_type":          "commit_burst",
			"subject_external_id": day,
			"subject_type":        "commit_burst",
			"source_tag":          "GIT:" + day,
			"related":             related,
			"payload": map[string]any{
				"count":    len(dayCommits),
				"head":     dayCommits[0].Hash, // newest first
				"subjects": subjects,
			},
			"dedupe_key": "commit_burst:" + day,
		})
	}

	return Op{
		Type: "emit_events",
		Payload: map[string]any{
			"external_id": "EVENTS-COMMITS-" + today,
			"actor":       actor,
			"events":      events,
		},
	}, true
}

// ---------------------------------------------------------------- assembly

// BuildOps assembles the full op batch in dependency order: requirements,
// epics (which link them), gates (string refs — order-independent), burst.
// OBSOLETE rows are not synced; 'finding' RQs stay records, not gates.
func BuildOps(data Data, readRecord func(string) string, today string) []Op {
	var ops []Op
	for _, r := range data.Reqs {
		if r.Status == "OBSOLETE" {
			continue
		}
		ops = append(ops, BuildRequirementOp(r))
	}
	records := map[string]string{}
	for _, e := range data.Epics {
		text := ""
		if e.Record != "" && readRecord != nil {
			// RecordFS resolves from the sync root when the worklist lives
			// above it (SCN-SY-046); Record stays the payload identity.
			readPath := e.Record
			if e.RecordFS != "" {
				readPath = e.RecordFS
			}
			text = readRecord(readPath)
		}
		records[e.ID] = text
		ops = append(ops, BuildEpicOp(e, text))
	}
	for _, rq := range data.RQs {
		if rq.State == "finding" {
			continue
		}
		ops = append(ops, BuildRQGateOp(rq))
	}
	for _, oq := range data.OQs {
		ops = append(ops, BuildOQGateOp(oq))
	}
	for _, e := range data.Epics {
		if op, ok := BuildSpecApprovalGateOp(e, records[e.ID]); ok {
			ops = append(ops, op)
		}
		if op, ok := BuildApprovalGateOp(e, records[e.ID]); ok {
			ops = append(ops, op)
		}
	}
	if op, ok := BuildCommitBurstOp(data.Commits, today); ok {
		ops = append(ops, op)
	}
	return ops
}

// ---------------------------------------------------------------- trace paths

var traceLine = regexp.MustCompile(`^-\s+\*\*(Tests|Code):\*\*`)
var backtickRe = regexp.MustCompile("`([^`]+)`")
var toolPrefixRe = regexp.MustCompile(`^(npm|mix|node)\b`)

// ExtractTracePaths — the backticked file paths in a detail block's
// Tests:/Code: lines (the drift command's diff scope).
func ExtractTracePaths(detail string) []string {
	seen := map[string]bool{}
	var paths []string
	for _, line := range strings.Split(detail, "\n") {
		if !traceLine.MatchString(line) {
			continue
		}
		for _, m := range backtickRe.FindAllStringSubmatch(line, -1) {
			token := strings.TrimSpace(m[1])
			if !strings.ContainsAny(token, " \t") && strings.ContainsAny(token, "/.") && !toolPrefixRe.MatchString(token) {
				if !seen[token] {
					seen[token] = true
					paths = append(paths, token)
				}
			}
		}
	}
	sort.Strings(paths)
	return paths
}

// ApprovalLineOf exposes the record's recorded approval to other packages
// (REQ-CROSS-030's gate). Exported deliberately rather than reimplemented: two
// answers to "is this epic approved?" would drift, and drift between two
// implementations of one rule is the defect this workspace keeps finding.
func ApprovalLineOf(recordText string) string { return approvalLineOf(recordText) }
