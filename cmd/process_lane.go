package cmd

// REQ-CROSS-458 (EPIC-RDD-LANE): the small-change lane's CLI verbs. A lane
// authorization is a standing human decision per System, answered once by a
// workspace admin in the web app or with `process lane approve`; a small change of a class it covers enters
// by applying it after its own narrow independent review, its eligibility is
// the server's verdict over the diff it delivered, and small changes complete
// together in one lane-batch gate with per-item rejection (PROCESS.md,
// Small-change lane). Every stand-in for the per-change human answer is
// checked by the server; these verbs read the facts, refuse before any write
// when one is unmet, and otherwise print the server's refusal verbatim.
//
// The single-SR aggregate a narrow review pins is served only for a piece the
// caller holds (the delivery-context read), so the verbs that need it hold the
// SR as a single_sr work selection, taking it when it is not yet held. A
// selection is not a process record.

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// laneNarrowSource is the source that marks a cold-review trace as the lane's
// narrow review (the server matches its ref, case-insensitively).
var laneNarrowSource = map[string]any{"kind": "lane", "ref": "LANE:narrow"}

var processLaneCmd = &cobra.Command{
	Use:   "lane",
	Short: "The small-change lane: authorize, review, enter, check and complete small changes",
	Long: `The small-change lane scales review and gates to a small change (PROCESS.md,
Small-change lane). With no subcommand it prints this System's lane
authorizations: the current one and any waiting for an answer (a read).

The sequence for one small change:

  author apply --file sr.yaml                   # the SR with lane_class and a source
  working-set pull <SR> --for-review            # the reviewer reads REVIEW.md
  process lane review <SR> --file review.json   # the narrow review, one pass
  process lane enter <SR>                       # PROPOSED->TODO by the authorization
  factory evidence --file runs.json             # RED, then the passing run
  process advance --all --piece <SR> --log <run>
  process lane check <SR> --commit <merged sha> # after delivery to the default branch
  process lane complete --log <run>             # one lane-batch gate for many
  process lane complete --apply                 # after the answer

A lane authorization is prepared with process lane authorize and answered
once by a workspace admin, in the web app or with process lane approve <gate>.
factory answer cannot answer it.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processLaneShow(env)
	},
}

// ---------------------------------------------------------------- reads

// listGates reads the System's gates in one state (open, answered, all …).
func listGates(env *factoryEnv, state string) ([]map[string]any, error) {
	status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/gates?system_id=%d&state=%s", env.SystemID, url.QueryEscape(state)), nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, serverRefusal("gate read", status, body)
	}
	rows, err := listFromData(body, "gates")
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		if m, ok := r.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

// laneAuthorizations returns this System's lane authorizations among gates.
func laneAuthorizations(env *factoryEnv, gates []map[string]any) []map[string]any {
	system := fmt.Sprintf("system:%d", env.SystemID)
	var out []map[string]any
	for _, g := range gates {
		if str(g, "purpose") == "lane_authorization" && slices.Contains(stringSlice(g["exact_scope"]), system) {
			out = append(out, g)
		}
	}
	return out
}

// currentLaneAuthorization is the newest answered lane authorization of the
// System — the server's own order. The server re-verifies it on every
// application and names the current one when this one is stale.
func currentLaneAuthorization(env *factoryEnv) (map[string]any, error) {
	gates, err := listGates(env, "answered")
	if err != nil {
		return nil, err
	}
	var best map[string]any
	for _, g := range laneAuthorizations(env, gates) {
		if best == nil || str(g, "answered_at") > str(best, "answered_at") {
			best = g
		}
	}
	return best, nil
}

func processLaneShow(env *factoryEnv) error {
	gates, err := listGates(env, "all")
	if err != nil {
		return err
	}
	auths := laneAuthorizations(env, gates)
	if len(auths) == 0 {
		fmt.Println("no lane authorization on this System — prepare one with `process lane authorize`; a workspace admin answers it in the web app or with `process lane approve <gate>`")
		return nil
	}
	current, err := currentLaneAuthorization(env)
	if err != nil {
		return err
	}
	for _, g := range auths {
		mark := "  "
		if current != nil && str(g, "external_id") == str(current, "external_id") {
			mark = "* "
		}
		line := fmt.Sprintf("%s%s  %s", mark, str(g, "external_id"), str(g, "state"))
		if at := str(g, "answered_at"); at != "" {
			line += "  answered " + at
			if who := firstNonEmpty(str(g, "answerer_name"), str(g, "answerer_email")); who != "" {
				line += " by " + who
			}
		}
		if t := str(g, "title"); t != "" {
			line += "  — " + t
		}
		fmt.Println(line)
	}
	if current != nil {
		fmt.Printf("current: %s (* above); the server re-verifies its expiry, cap and appliers on every application\n", str(current, "external_id"))
	} else {
		fmt.Println("no answered lane authorization — a workspace admin answers the open one in the web app (Mission Control) or with `process lane approve <gate>`")
	}
	return nil
}

// holdsPiece reports whether the caller holds `piece` as a current selection.
func holdsPiece(env *factoryEnv, piece string) bool {
	status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/work-selection?system_id=%d&scope=%s", env.SystemID, url.QueryEscape(piece)), nil)
	if err != nil || status != 200 {
		return false
	}
	current, _ := dataOf(body)["current"].(map[string]any)
	return str(current, "scope_external_id") == piece
}

// holdLanePiece takes the SR as a single_sr piece when the caller does not
// hold it: the single-SR aggregate is served only for a held piece.
func holdLanePiece(env *factoryEnv, sr, phase string) error {
	if holdsPiece(env, sr) {
		return nil
	}
	return workingSetSelect(env, wsSelectOpts{scope: sr, kind: "single_sr", phase: phase}, time.Now())
}

// singleSRAggregate is the SR's single_sr packet aggregate and its facts,
// read for the held piece.
func singleSRAggregate(env *factoryEnv, sr string) (string, *deliveryFacts, error) {
	resp, err := readDeliveryContextFor(env, sr)
	if err != nil {
		return "", nil, err
	}
	if resp.Data.PacketFingerprint == "" {
		return "", nil, fmt.Errorf("the store serves no single-SR aggregate for %s — it is read for a piece you hold; check `process next --piece %s`", sr, sr)
	}
	return resp.Data.PacketFingerprint, resp.Data.Facts, nil
}

// laneSR reads the SR and refuses anything but a system requirement.
func readLaneSR(env *factoryEnv, sr string) (map[string]any, error) {
	if unsafeSnapshotName(sr) {
		return nil, fmt.Errorf("%q is not a plain external id", sr)
	}
	items, err := fetchDirectItems(env, []string{sr}, false, false)
	if err != nil {
		return nil, err
	}
	item, ok := items[sr]
	if !ok {
		return nil, fmt.Errorf("%s is not served by this System — nothing was written", sr)
	}
	if item.kind != "system" {
		return nil, fmt.Errorf("%s is a %s record — the small-change lane takes one system requirement; nothing was written", sr, item.kind)
	}
	return item.payload, nil
}

func refuseWithoutLaneClass(sr string, rec map[string]any) error {
	if str(rec, "lane_class") == "" {
		return fmt.Errorf("%s has no lane_class — set it with `author update %s --lane-class <defect_with_failing_test|wording|presentation|dependency_patch>` before the narrow review, which pins it; nothing was written", sr, sr)
	}
	return nil
}

// laneSectionFingerprints reads the SR's single_sr packet sections: the
// rows, and "key=fingerprint" pairs in key order.
func laneSectionFingerprints(env *factoryEnv, sr string) ([]map[string]any, string, error) {
	status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/packet-sections?system_id=%d&scope=single_sr:%s", env.SystemID, url.QueryEscape(sr)), nil)
	if err != nil {
		return nil, "", err
	}
	if status == 404 {
		return nil, "none", nil
	}
	if status != 200 {
		return nil, "", serverRefusal("packet-sections read", status, body)
	}
	raw, err := listFromData(body, "packet_sections")
	if err != nil {
		return nil, "", err
	}
	var rows []map[string]any
	var pairs []string
	for _, r := range raw {
		m, _ := r.(map[string]any)
		if m == nil || str(m, "section_key") == "" {
			continue
		}
		rows = append(rows, m)
		pairs = append(pairs, str(m, "section_key")+"="+str(m, "content_fingerprint"))
	}
	sort.Strings(pairs)
	if len(pairs) == 0 {
		return rows, "none", nil
	}
	return rows, strings.Join(pairs, ";"), nil
}

// ---------------------------------------------------------------- review pull

// workingSetPullForReview is `working-set pull <SR> --for-review`: a read that
// renders one SR read-only under a fresh review context for the lane's narrow
// review. The stamp records the SR's content fingerprint and its packet
// sections; with those unchanged, the single-SR aggregate is the one the
// reviewer read, which `process lane review` checks before it records.
func workingSetPullForReview(env *factoryEnv, ids []string, now time.Time) error {
	for _, id := range ids {
		if unsafeSnapshotName(id) {
			return fmt.Errorf("%q is not a plain external id — refusing to pull", id)
		}
	}
	items, err := fetchDirectItems(env, ids, false, false)
	if err != nil {
		return err
	}
	for _, id := range ids {
		item, ok := items[id]
		if !ok {
			return fmt.Errorf("%s is not served by %s (unknown external id)", id, env.APIURL)
		}
		if item.kind != "system" {
			return fmt.Errorf("%s is a %s record — a by-id review pull takes a system requirement (the small-change lane's narrow review); review an epic with `working-set pull --scope --for-review`", id, item.kind)
		}
		sections, sectionPairs, err := laneSectionFingerprints(env, id)
		if err != nil {
			return err
		}
		agg := ""
		if holdsPiece(env, id) {
			if dc, err := readDeliveryContextFor(env, id); err == nil {
				agg = dc.Data.PacketFingerprint
			}
		}
		ctxID := newContextID("review")
		dir := filepath.Join(env.Root, workingSetDir, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := atomicWrite(filepath.Join(dir, reviewBundleFile), []byte(renderLaneReviewBundle(id, item.payload, ctxID, sections, now))); err != nil {
			return err
		}
		stamp := fmt.Sprintf("# working-set context\n\nmode: review\ncontext_id: %s\nscope: single_sr:%s\npulled_at: %s\ncontent: %s\nsections: %s\n",
			ctxID, id, now.Format(time.RFC3339), str(item.payload, "fingerprint"), sectionPairs)
		if agg != "" {
			stamp += "aggregate: " + agg + "\n"
		}
		if err := atomicWrite(filepath.Join(dir, contextFile), []byte(stamp)); err != nil {
			return err
		}
		printSuccess("review pull %s → %s (review context %s)", id, filepath.Join(workingSetDir, id, reviewBundleFile), ctxID)
	}
	return nil
}

func renderLaneReviewBundle(id string, rec map[string]any, ctxID string, sections []map[string]any, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# REVIEW — %s (small-change lane, narrow review)\n\n", id)
	fmt.Fprintf(&b, "- **Scope:** single_sr:%s\n", id)
	fmt.Fprintf(&b, "- **Lane class:** %s\n", orMarker(str(rec, "lane_class"), "«not set — `process lane review` refuses it»"))
	fmt.Fprintf(&b, "- **Content fingerprint:** %s\n", orMarker(str(rec, "fingerprint"), "—"))
	fmt.Fprintf(&b, "- **Review context:** %s\n", ctxID)
	fmt.Fprintf(&b, "- **Pulled at:** %s\n\n", now.Format(time.RFC3339))
	b.WriteString("One narrow pass, no rounds: check that the statement, boundary, RED plan and lane class describe one small change the lane may take (PROCESS.md, Small-change lane). A blocking finding is a FAIL, and the change leaves the lane for single-SR scope.\n\n")
	b.WriteString("## System requirement\n\n")
	writeBundleRequirement(&b, id, scopeRecord{kind: "system", payload: rec})
	b.WriteString("## Packet sections\n\n")
	if len(sections) == 0 {
		b.WriteString("None — the SR record is the small change's packet.\n")
	}
	for _, sm := range sectionOrder(sections) {
		fmt.Fprintf(&b, "### %s · %s\n\n%s\n\n", str(sm, "section_key"), orMarker(str(sm, "content_fingerprint"), "—"), strings.TrimRight(str(sm, "content"), "\n"))
	}
	return b.String()
}

// laneFreeGateID is the first id in the <base>, <base>-R2… series no gate
// holds. Unlike freeGateID it steps past an open or answered gate: a lane
// authorization stays answered while it is current and its successor takes
// the next id, and a second batch may open while the first waits.
func laneFreeGateID(env *factoryEnv, base string) (string, error) {
	for n := 1; n <= 50; n++ {
		id := base
		if n > 1 {
			id = fmt.Sprintf("%s-R%d", base, n)
		}
		status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/gates/%s?system_id=%d", url.PathEscape(id), env.SystemID), nil)
		if err != nil {
			return "", err
		}
		if status == 200 {
			continue
		}
		if em, ok := body["error"].(map[string]any); ok && status == 404 && strings.HasPrefix(str(em, "message"), "no such gate") {
			return id, nil
		}
		return "", gateShowError(status, body, id)
	}
	return "", fmt.Errorf("no free id in the %s series after 50", base)
}

// readStampField reads one `key: value` line of a working-set stamp.
func readStampField(dir, key string) string {
	for _, line := range strings.Split(readContextFile(dir), "\n") {
		if v, ok := strings.CutPrefix(line, key+": "); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ---------------------------------------------------------------- authorize

var (
	laneAuthClasses  []string
	laneAuthAppliers []string
	laneAuthExpires  string
	laneAuthCap      int
	laneAuthExclude  []string
	laneAuthFile     string
	laneAuthID       string
	laneAuthTitle    string
)

type laneAuthTerms struct {
	Classes       []string `json:"classes"`
	ExcludedGlobs []string `json:"excluded_globs"`
	Appliers      []int    `json:"appliers"`
	ExpiresAt     string   `json:"expires_at"`
	DailyCap      int      `json:"daily_cap"`
}

var processLaneAuthorizeCmd = &cobra.Command{
	Use:   "authorize --classes <c,…> --appliers <user id,…> --expires <date> --cap <n> [--exclude <glob>]… | --file <lane.json>",
	Short: "Prepare a lane authorization for this System; a workspace admin answers it in the web app or with process lane approve",
	Long: `Ask for a lane authorization for this System. It names the classes of
small change it covers (defect_with_failing_test, wording, presentation,
dependency_patch), the user ids who may apply it, when it expires (a date,
a date and time, or <n>d from now; at most 30 days), a daily cap (1 to 10)
and any extra paths the lane must not touch. --file reads the same terms
from a JSON file, for example:

  {"classes": ["wording"], "appliers": [7], "expires_at": "20d",
   "daily_cap": 5, "excluded_globs": ["docs/legal/**"]}

A workspace admin answers it once: in the web app (Mission Control), or with
process lane approve <gate>. factory answer, an API token or an integration
cannot answer it. The server checks every term and prints why it refuses
one.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		terms, err := laneAuthorizeTerms(time.Now().UTC())
		if err != nil {
			return err
		}
		return processLaneAuthorize(env, terms)
	},
}

// parseLaneExpiry reads RFC 3339, a date (its UTC midnight) or <n>d from now.
func parseLaneExpiry(v string, now time.Time) (string, error) {
	v = strings.TrimSpace(v)
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	if n, ok := strings.CutSuffix(v, "d"); ok {
		if days, err := strconv.Atoi(n); err == nil && days > 0 {
			return now.Add(time.Duration(days) * 24 * time.Hour).Truncate(time.Second).Format(time.RFC3339), nil
		}
	}
	return "", fmt.Errorf("--expires %q: give an RFC 3339 time, a date (YYYY-MM-DD, read as its UTC midnight) or <n>d from now; nothing was written", v)
}

func laneAuthorizeTerms(now time.Time) (laneAuthTerms, error) {
	var t laneAuthTerms
	if laneAuthFile != "" {
		if err := readJSONFile(filepath.Clean(laneAuthFile), &t); err != nil {
			return t, fmt.Errorf("--file: %w", err)
		}
	} else {
		for _, c := range laneAuthClasses {
			if c = strings.TrimSpace(c); c != "" {
				t.Classes = append(t.Classes, c)
			}
		}
		for _, a := range laneAuthAppliers {
			n, err := strconv.Atoi(strings.TrimSpace(a))
			if err != nil || n <= 0 {
				return t, fmt.Errorf("--appliers %q: appliers are user ids (positive integers); nothing was written", a)
			}
			t.Appliers = append(t.Appliers, n)
		}
		t.ExpiresAt, t.DailyCap, t.ExcludedGlobs = laneAuthExpires, laneAuthCap, laneAuthExclude
	}
	switch {
	case len(t.Classes) == 0:
		return t, fmt.Errorf("--classes is required: the classes the authorization covers (defect_with_failing_test, wording, presentation, dependency_patch); nothing was written")
	case len(t.Appliers) == 0:
		return t, fmt.Errorf("--appliers is required: the user ids who may apply the authorization; nothing was written")
	case t.ExpiresAt == "":
		return t, fmt.Errorf("--expires is required: at most 30 days from now; nothing was written")
	case t.DailyCap == 0:
		return t, fmt.Errorf("--cap is required: the daily cap of lane applications (1 to 10); nothing was written")
	}
	expires, err := parseLaneExpiry(t.ExpiresAt, now)
	if err != nil {
		return t, err
	}
	t.ExpiresAt = expires
	if t.ExcludedGlobs == nil {
		t.ExcludedGlobs = []string{}
	}
	return t, nil
}

func processLaneAuthorize(env *factoryEnv, t laneAuthTerms) error {
	id := laneAuthID
	if id != "" {
		if err := gateIDFree(env, id); err != nil {
			return err
		}
	} else {
		free, err := laneFreeGateID(env, "LANE-AUTH")
		if err != nil {
			return err
		}
		id = free
	}
	title := laneAuthTitle
	if title == "" {
		title = "Small-change lane for this System: " + strings.Join(t.Classes, ", ")
	}
	appliers := make([]string, len(t.Appliers))
	for i, a := range t.Appliers {
		appliers[i] = strconv.Itoa(a)
	}
	excluded := "the default excluded areas only"
	if len(t.ExcludedGlobs) > 0 {
		excluded = "the default excluded areas and " + strings.Join(t.ExcludedGlobs, ", ")
	}
	body := fmt.Sprintf("Authorizes the small-change lane on this System (PROCESS.md, Small-change lane).\n\n- Classes: %s\n- Excluded: %s\n- Appliers (user ids): %s\n- Expires: %s\n- Daily cap: %d\n\nAnswered once by a workspace admin, in the web app or with the developer CLI's process lane approve. Every application appears in the authorizer's feed; it can be withdrawn at any time.",
		strings.Join(t.Classes, ", "), excluded, strings.Join(appliers, ", "), t.ExpiresAt, t.DailyCap)
	fields := map[string]any{
		"title":       title,
		"gate_class":  "human",
		"gate_kind":   "approval_request",
		"purpose":     "lane_authorization",
		"exact_scope": []string{fmt.Sprintf("system:%d", env.SystemID)},
		"options":     []map[string]any{{"key": "approve", "label": "Authorize the small-change lane"}},
		"body_md":     body,
		"raw_payload": map[string]any{
			"classes": t.Classes, "excluded_globs": t.ExcludedGlobs, "appliers": t.Appliers,
			"expires_at": t.ExpiresAt, "daily_cap": t.DailyCap,
		},
	}
	if _, err := authorCreate(env, "gate", id, fields); err != nil {
		return err
	}
	fmt.Printf("lane authorization %s\n  classes: %s\n  excluded: %s\n  appliers: %s\n  expires: %s\n  daily cap: %d\n",
		id, strings.Join(t.Classes, ", "), excluded, strings.Join(appliers, ", "), t.ExpiresAt, t.DailyCap)
	fmt.Printf("%s is open: a workspace admin answers it once, in the web app (Mission Control) or with the developer CLI\n", id)
	printInfo("next: `modernpath process lane approve %s` as a workspace admin, or answer it in the web app", id)
	return nil
}

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

// ---------------------------------------------------------------- review

var laneReviewFile string

var processLaneReviewCmd = &cobra.Command{
	Use:   "review <SR> --file <review.json>",
	Short: "Record the small change's narrow independent review in one call",
	Long: `Record the lane's narrow review of one small change from the reviewer's
report file (the same file process review record reads). The SR must have
been pulled with working-set pull <SR> --for-review: that pull records what
the reviewer read.

It refuses before any write when the SR was not pulled for review, has no
lane class (set it before the review), changed since the review pull (pull
it again and review again), or when a PASS would leave a blocking finding
open. Otherwise it records the findings, their dispositions and the review
verdict for the SR as it was reviewed.

There is one pass and no second round: after a FAIL the change leaves the
lane and is planned as a single SR with its full packet and review.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if laneReviewFile == "" {
			return fmt.Errorf("--file <review.json> is required")
		}
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processLaneReview(env, args[0], laneReviewFile)
	},
}

func processLaneReview(env *factoryEnv, sr, file string) error {
	var review reviewFile
	if err := readJSONFile(file, &review); err != nil {
		return err
	}
	review.Verdict = strings.ToUpper(strings.TrimSpace(review.Verdict))
	if review.Verdict != "PASS" && review.Verdict != "FAIL" {
		return fmt.Errorf("the review's verdict must be PASS or FAIL (got %q) — nothing was written", review.Verdict)
	}
	if unsafeSnapshotName(sr) {
		return fmt.Errorf("%q is not a plain external id", sr)
	}
	dir := filepath.Join(env.Root, workingSetDir, sr)
	mode, ctxID := readContextStamp(dir)
	stampedContent, stampedSections, stampedAgg := readStampField(dir, "content"), readStampField(dir, "sections"), readContextAggregate(dir)
	if mode != "review" || ctxID == "" || readStampField(dir, "scope") != "single_sr:"+sr || (stampedContent == "" && stampedAgg == "") {
		return fmt.Errorf("%s carries no review-mode stamp — pull it with `working-set pull %s --for-review` before the review, and record from that pull; nothing was written", sr, sr)
	}
	rec, err := readLaneSR(env, sr)
	if err != nil {
		return err
	}
	if err := refuseWithoutLaneClass(sr, rec); err != nil {
		return err
	}
	if stampedContent != "" {
		_, sections, err := laneSectionFingerprints(env, sr)
		if err != nil {
			return err
		}
		if now := str(rec, "fingerprint"); now != stampedContent || sections != stampedSections {
			return fmt.Errorf("%s changed since the review pull (content %s, now %s; sections %s, now %s) — re-pull it with `working-set pull %s --for-review` and review again; nothing was written",
				sr, stampedContent, presentPin(now), stampedSections, sections, sr)
		}
	}
	scope := "single_sr:" + sr
	for i := range review.Findings {
		if review.Findings[i].Scope == "" {
			review.Findings[i].Scope = scope
		}
		if review.Findings[i].Scope != scope {
			return fmt.Errorf("finding %s names scope %s, but this review records %s — nothing was written", review.Findings[i].ID, review.Findings[i].Scope, scope)
		}
	}
	for i := range review.Dispositions {
		if review.Dispositions[i].Scope == "" {
			review.Dispositions[i].Scope = scope
		}
		if review.Dispositions[i].Scope != scope {
			return fmt.Errorf("disposition %s names scope %s, but this review records %s — nothing was written", review.Dispositions[i].ID, review.Dispositions[i].Scope, scope)
		}
	}
	if err := checkReviewEntries(env, review.Findings, review.Dispositions); err != nil {
		return err
	}
	var existing map[string]map[string]any
	if review.Verdict == "PASS" {
		if existing, err = readScopeFindings(env, scope); err != nil {
			return err
		}
	}

	if err := holdLanePiece(env, sr, "cold_review"); err != nil {
		return err
	}
	agg, facts, err := singleSRAggregate(env, sr)
	if err != nil {
		return err
	}
	if stampedAgg != "" && stampedAgg != agg {
		return fmt.Errorf("the single-SR aggregate of %s moved since the review pull (stamped %s, now %s) — re-pull it with `working-set pull %s --for-review` and review again; nothing was written", sr, stampedAgg, agg, sr)
	}
	if review.Verdict == "PASS" {
		var served []string
		if facts != nil {
			served = facts.ColdReview.OpenFindingIDs
		}
		if open := openAfterReview(served, existing, review); len(open) > 0 {
			return fmt.Errorf("the verdict is PASS, but material finding(s) would stay OPEN or DEFERRED on %s: %s — resolve or reject them in the file's dispositions, or record the verdict as FAIL; nothing was written", scope, strings.Join(open, ", "))
		}
	}

	failed := 0
	if len(review.Findings) > 0 {
		fmt.Println("findings:")
		failed += addFindingEntries(env, review.Findings, findingPin{aggregate: agg, contextID: ctxID})
	}
	if len(review.Dispositions) > 0 {
		fmt.Println("dispositions:")
		failed += applyDispositionEntries(env, review.Dispositions)
	}
	if failed > 0 {
		return fmt.Errorf("%d finding(s) or disposition(s) were not recorded — the narrow review was not recorded; fix them and re-run (what was recorded is skipped)", failed)
	}

	sources := []map[string]any{laneNarrowSource}
	sources = append(sources, traceSources(nonEmpty(review.Source))...)
	fields := map[string]any{
		"title":                "Narrow review of " + sr + " (small-change lane)",
		"purpose":              "cold-review",
		"transition":           "PROPOSED->TODO",
		"exact_scope":          []string{sr},
		"fingerprint":          agg,
		"verdict":              review.Verdict,
		"sources":              sources,
		"review_context_id":    ctxID,
		"application_revision": gitHead(env.Root),
	}
	if review.Body != "" {
		fields["body_md"] = review.Body
	}
	fmt.Println("trace:")
	if err := authorTrace(env, "LANE-REVIEW-"+sr+"-"+ctxID, fields); err != nil {
		return err
	}
	if review.Verdict == "FAIL" {
		fmt.Printf("%s failed its narrow review — it leaves the lane: plan it as single-SR scope with its full packet and review (there is no second narrow round)\n", sr)
		return nil
	}
	printInfo("next: `process lane enter %s`", sr)
	return nil
}

// ---------------------------------------------------------------- enter

var processLaneEnterCmd = &cobra.Command{
	Use:   "enter <SR>",
	Short: "Enter a small change by the current lane authorization (PROPOSED->TODO) in one call",
	Long: `Enter one small change (PROPOSED->TODO) under the System's current lane
authorization, in one call. The SR needs a lane class, a passing narrow
review of its current content, and an approved lane authorization. The
server checks that the authorization is current and covers the class, that
you may apply it, the daily cap, and that the review was independent; if it
refuses, its reason is printed.

If the SR already entered through the lane and changed afterwards (TODO to
IN_REVIEW), record a new narrow review of it first; enter then applies the
authorization again.

Nothing is written when the lane class, the passing review or the approved
authorization is missing. Afterwards you work on the SR in the build phase.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processLaneEnter(env, args[0])
	},
}

// narrowReviewCandidates are the trace ids that may hold the SR's narrow
// review: the one the last review pull's context names, and the facts' own.
func narrowReviewCandidates(env *factoryEnv, sr string, facts *deliveryFacts) []string {
	var ids []string
	if mode, ctx := readContextStamp(filepath.Join(env.Root, workingSetDir, sr)); mode == "review" && ctx != "" {
		ids = append(ids, "LANE-REVIEW-"+sr+"-"+ctx)
	}
	if facts != nil && facts.ColdReview.TraceExternalID != "" && !slices.Contains(ids, facts.ColdReview.TraceExternalID) {
		ids = append(ids, facts.ColdReview.TraceExternalID)
	}
	return ids
}

// narrowReviewTrace returns the passing narrow review trace among the
// candidates, pinned at `agg` when agg is given; nil when there is none.
func narrowReviewTrace(env *factoryEnv, sr string, candidates []string, agg string) (map[string]any, error) {
	for _, id := range candidates {
		status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/gates/%s?system_id=%d", url.PathEscape(id), env.SystemID), nil)
		if err != nil {
			return nil, err
		}
		if status != 200 {
			continue
		}
		g, _ := dataOf(body)["gate"].(map[string]any)
		if str(g, "purpose") != "cold-review" || !strings.EqualFold(str(g, "state"), "pass") || !slices.Contains(stringSlice(g["exact_scope"]), sr) {
			continue
		}
		pin := firstNonEmpty(str(g, "evaluated_scope_fingerprint"), str(g, "fingerprint"))
		if agg != "" && pin != agg {
			continue
		}
		for _, raw := range anyList(g["sources"]) {
			if src, ok := raw.(map[string]any); ok && strings.EqualFold(str(src, "ref"), "LANE:narrow") {
				return g, nil
			}
		}
	}
	return nil, nil
}

func processLaneEnter(env *factoryEnv, sr string) error {
	rec, err := readLaneSR(env, sr)
	if err != nil {
		return err
	}
	if err := refuseWithoutLaneClass(sr, rec); err != nil {
		return err
	}
	status := str(rec, "work_status")
	reapply := false
	switch status {
	case "PROPOSED":
	case "TODO", "IN_PROGRESS", "IN_REVIEW":
		reapply = true
	default:
		return fmt.Errorf("%s is %s — the lane enters a PROPOSED small change (or re-applies to an entered one that changed); nothing was written", sr, presentPin(status))
	}

	var facts *deliveryFacts
	held := holdsPiece(env, sr)
	if held {
		if dc, err := readDeliveryContextFor(env, sr); err == nil {
			facts = dc.Data.Facts
		}
	}
	candidates := narrowReviewCandidates(env, sr, facts)
	review, err := narrowReviewTrace(env, sr, candidates, "")
	if err != nil {
		return err
	}
	if review == nil {
		return fmt.Errorf("%s has no passing narrow review — pull it with `working-set pull %s --for-review`, have it reviewed and record it with `process lane review %s --file review.json`; nothing was written", sr, sr, sr)
	}
	auth, err := currentLaneAuthorization(env)
	if err != nil {
		return err
	}
	if auth == nil {
		return fmt.Errorf("there is no answered lane authorization on this System — prepare one with `process lane authorize` and have a workspace admin answer it in the web app or with `process lane approve <gate>`; nothing was written")
	}
	authID := str(auth, "external_id")

	if !held {
		if err := holdLanePiece(env, sr, "entry"); err != nil {
			return err
		}
	}
	agg, facts, err := singleSRAggregate(env, sr)
	if err != nil {
		return err
	}
	if review, err = narrowReviewTrace(env, sr, narrowReviewCandidates(env, sr, facts), agg); err != nil {
		return err
	}
	if review == nil {
		return fmt.Errorf("%s has no passing narrow review at its current aggregate %s — it changed after the review; re-pull it with `working-set pull %s --for-review`, review it again and record it with `process lane review %s`; nothing was written", sr, agg, sr, sr)
	}

	if reapply {
		fmt.Printf("re-apply %s to %s (%s, changed after its lane entry) at %s, reviewed by %s\n", authID, sr, status, agg, str(review, "external_id"))
		if _, err := authorPost(env, map[string]any{"action": "lane_reapply", "record": map[string]any{"external_id": sr}, "lane_ref": authID}); err != nil {
			return err
		}
		printSuccess("re-applied the lane authorization %s to %s at its current aggregate", authID, sr)
		return nil
	}

	fmt.Printf("enter %s PROPOSED->TODO by the lane authorization %s (class %s, narrow review %s at %s)\n", sr, authID, str(rec, "lane_class"), str(review, "external_id"), agg)
	data, err := authorPost(env, map[string]any{
		"action":   "advance",
		"record":   map[string]any{"kind": "requirement", "external_id": sr},
		"to":       "TODO",
		"expected": "PROPOSED",
		"lane_ref": authID,
	})
	if err != nil {
		return err
	}
	row, _ := data["requirement"].(map[string]any)
	printSuccess("entered %s → %s (basis %s, lane authorization %s)", sr, firstNonEmpty(str(row, "work_status"), "TODO"),
		firstNonEmpty(str(data, "transition_basis"), "lane_authorization"), authID)
	// The selection follows the SR into the build.
	if err := workingSetSelect(env, wsSelectOpts{scope: sr, kind: "single_sr", phase: "build"}, time.Now()); err != nil {
		printWarning("%s entered, but its selection was not moved to the build phase: %v", sr, err)
	}
	printInfo("next: the red-first build; record the runs with `factory evidence`, then `process advance %s --log <run>`", sr)
	return nil
}

// ---------------------------------------------------------------- check

var (
	laneCheckCommit string
	laneCheckBase   string
)

var processLaneCheckCmd = &cobra.Command{
	Use:   "check <SR> --commit <sha> [--base <sha>]",
	Short: "Post the small change's delivered file list; the server records the eligibility verdict",
	Long: `Check that a delivered small change is small enough for the lane. The
change is what reached the default branch: --commit is the merge or squash
commit, compared with its first parent; for a rebase delivery, give the
commit it started from with --base. The commit must be on the default
branch (origin/HEAD, else origin/main, main or master).

This CLI reports the changed files (git diff --name-only over the range);
the server cannot read your repository, so it judges the list you report:
at most five non-test source files, none in an excluded area. It records
the result on the SR, and this prints it. The first full report for a
commit counts: a later report for the same commit that leaves files out,
or uses another base, is refused. A FAIL names each offending file and
exits non-zero: the change leaves the lane.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if laneCheckCommit == "" {
			return fmt.Errorf("--commit <sha> is required: the commit that delivered the change to the default branch")
		}
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processLaneCheck(env, args[0], laneCheckCommit, laneCheckBase)
	},
}

// laneDefaultBranch is the ref the delivered change must be reachable from.
func laneDefaultBranch(root string) (string, error) {
	if ref := gitOut(root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); ref != "" {
		return ref, nil
	}
	for _, ref := range []string{"origin/main", "origin/master", "main", "master"} {
		if gitOut(root, "rev-parse", "--verify", "--quiet", ref+"^{commit}") != "" {
			return ref, nil
		}
	}
	return "", fmt.Errorf("no default branch found (origin/HEAD, origin/main, origin/master, main or master) — the lane checks the change delivered to it; nothing was written")
}

// laneIsAncestor is gitIsAncestor (process_ceremony.go) with any git failure
// read as "not an ancestor": the lane has already resolved both commits.
func laneIsAncestor(root, a, b string) bool {
	ok, err := gitIsAncestor(root, a, b)
	return err == nil && ok
}

func processLaneCheck(env *factoryEnv, sr, commit, base string) error {
	root := env.Root
	full := gitOut(root, "rev-parse", "--verify", "--quiet", commit+"^{commit}")
	if full == "" {
		return fmt.Errorf("--commit %s is not a commit in this repository — fetch the default branch and give the delivering commit; nothing was written", commit)
	}
	branch, err := laneDefaultBranch(root)
	if err != nil {
		return err
	}
	if !laneIsAncestor(root, full, branch) {
		return fmt.Errorf("%s is not reachable from the default branch %s — lane check reads the change delivered to it; merge it (and fetch) first; nothing was written", full[:12], branch)
	}
	from, baseFull := "", ""
	if base != "" {
		baseFull = gitOut(root, "rev-parse", "--verify", "--quiet", base+"^{commit}")
		if baseFull == "" {
			return fmt.Errorf("--base %s is not a commit in this repository; nothing was written", base)
		}
		if baseFull == full || !laneIsAncestor(root, baseFull, full) {
			return fmt.Errorf("--base %s is not an ancestor of %s — a rebase delivery names the commit its range starts from; nothing was written", baseFull[:12], full[:12])
		}
		from = baseFull
	} else {
		from = gitOut(root, "rev-parse", "--verify", "--quiet", full+"^1")
		if from == "" {
			return fmt.Errorf("%s has no first parent — name the delivery's base with --base; nothing was written", full[:12])
		}
	}
	rng := full + "^1.." + full
	if baseFull != "" {
		rng = baseFull + ".." + full
	}
	diff := gitOut(root, "diff", "--name-only", from, full)
	var files []string
	for _, f := range strings.Split(diff, "\n") {
		if f = strings.TrimSpace(f); f != "" {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return fmt.Errorf("the range %s delivers no file — name the commit that delivered %s; nothing was written", rng, sr)
	}

	if status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/lane/eligibility-rules?system_id=%d", env.SystemID), nil); err == nil && status == 200 {
		rules := dataOf(body)
		fmt.Printf("rules (the server applies them): at most %v non-test source files; %d default excluded globs plus the authorization's; tests: %s\n",
			rules["max_non_test_files"], len(stringSlice(rules["default_excluded_globs"])), str(rules, "test_paths"))
	}
	fmt.Printf("lane check of %s over %s on %s: %d file(s)\n", sr, rng, branch, len(files))

	record := map[string]any{"external_id": sr, "commit": full, "files": files}
	if baseFull != "" {
		record["base"] = baseFull
	}
	data, err := authorPost(env, map[string]any{"action": "lane_eligibility", "record": record})
	if err != nil {
		return err
	}
	res, _ := data["lane_eligibility"].(map[string]any)
	verdict := str(res, "verdict")
	nonTest, tests := stringSlice(res["non_test_files"]), stringSlice(res["test_files"])
	fmt.Printf("%s: %s (server verdict) — %d non-test source file(s), %d test file(s) not counted\n", sr, verdict, len(nonTest), len(tests))
	for _, f := range nonTest {
		fmt.Printf("  %s\n", f)
	}
	var offending []string
	for _, raw := range anyList(res["offending"]) {
		o, _ := raw.(map[string]any)
		offending = append(offending, fmt.Sprintf("%s — %s", str(o, "file"), str(o, "reason")))
	}
	if len(offending) > 0 {
		fmt.Println("offending:")
		for _, o := range offending {
			fmt.Printf("  ✗ %s\n", o)
		}
	}
	fmt.Printf("  range: %s\n  delivered revision: %s\n  trace: %s\n", firstNonEmpty(str(res, "range"), rng), firstNonEmpty(str(res, "delivered_revision"), full), str(res, "trace"))
	if verdict != "PASS" {
		return fmt.Errorf("%s is not eligible for the lane (%s): %s — it leaves the lane for single-SR scope with its full packet and review", sr, firstNonEmpty(verdict, "no verdict"), strings.Join(offending, "; "))
	}
	return nil
}

func anyList(v any) []any {
	items, _ := v.([]any)
	return items
}

// ---------------------------------------------------------------- complete

var (
	laneCompleteApply  bool
	laneCompleteLog    string
	laneCompleteKind   string
	laneCompleteGateID string
	laneCompleteDryRun bool
)

var processLaneCompleteCmd = &cobra.Command{
	Use:   "complete --log <run> | --apply [--gate <LANE-BATCH>]",
	Short: "Open one lane-batch gate over the eligible small changes, or apply an answered one",
	Long: `Complete small changes together with one approval. Without --apply it takes
every small change that is IN_REVIEW and passed process lane check at its
delivered commit, and is not already waiting in another batch. For each it
records the run you name with --log as the run you report (nothing checks
the run itself, and the approver is told so), then asks for one approval
that lists each change and its files. The approver can reject single
changes. The changes left out are listed with the reason. --dry-run prints
the batch and writes nothing.

With --apply, after the approval, each approved change becomes DONE; a
rejected change stays IN_REVIEW, to be fixed and put in a new batch.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		if laneCompleteApply {
			return processLaneApply(env, laneCompleteGateID)
		}
		if laneCompleteLog == "" {
			return fmt.Errorf("--log is required: the delivered run's reference (a CI url or the command) is what each member's evidence and completion trace cite")
		}
		return processLaneComplete(env, laneCompleteLog, firstNonEmpty(laneCompleteKind, "ci"), laneCompleteDryRun)
	},
}

type laneMember struct {
	id, class, revision, eligibility string
	files                            []string
	tests                            string
}

// eligibilityFiles reads the non-test files and the test count back from the
// server's eligibility trace body.
func eligibilityFiles(body string) (files []string, tests string) {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "Test files (not counted): "); ok {
			tests = v
			break
		}
		if strings.HasPrefix(line, "- `") && strings.HasSuffix(line, "`") {
			files = append(files, strings.TrimSuffix(strings.TrimPrefix(line, "- `"), "`"))
		}
	}
	return files, tests
}

func processLaneComplete(env *factoryEnv, log, kind string, dryRun bool) error {
	srs, _, err := fetchRequirementLists(env, fmt.Sprintf("/api/v1/sync/requirements?system_id=%d", env.SystemID))
	if err != nil {
		return err
	}
	gates, err := listGates(env, "all")
	if err != nil {
		return err
	}
	// The latest server eligibility trace per (SR, revision), and the members
	// an open or answered batch already names.
	type elig struct{ state, id, body string }
	eligibility := map[string]elig{}
	inBatch := map[string]string{}
	for _, g := range gates {
		switch str(g, "purpose") {
		case "lane-eligibility":
			for _, id := range stringSlice(g["exact_scope"]) {
				eligibility[id+"@"+str(g, "fingerprint")] = elig{strings.ToLower(str(g, "state")), str(g, "external_id"), str(g, "body_md")}
			}
		case "lane-batch":
			if s := str(g, "state"); s == "open" || s == "answered" {
				for _, id := range stringSlice(g["exact_scope"]) {
					inBatch[id] = str(g, "external_id") + " (" + s + ")"
				}
			}
		}
	}

	var members []laneMember
	var excluded []string
	for _, raw := range srs {
		r, _ := raw.(map[string]any)
		id, class, status, rev := str(r, "external_id"), str(r, "lane_class"), str(r, "work_status"), str(r, "delivered_revision")
		if class == "" || !slices.Contains([]string{"TODO", "IN_PROGRESS", "IN_REVIEW"}, status) {
			continue
		}
		e := eligibility[id+"@"+rev]
		switch {
		case inBatch[id] != "":
			excluded = append(excluded, fmt.Sprintf("%s — already named by %s", id, inBatch[id]))
		case status != "IN_REVIEW":
			excluded = append(excluded, fmt.Sprintf("%s — %s, not IN_REVIEW (process advance %s)", id, status, id))
		case rev == "" || e.state == "":
			excluded = append(excluded, fmt.Sprintf("%s — no eligibility check at its delivered revision (process lane check %s --commit <sha>)", id, id))
		case e.state != "pass":
			excluded = append(excluded, fmt.Sprintf("%s — eligibility %s at %s (%s): it leaves the lane", id, strings.ToUpper(e.state), laneShort(rev), e.id))
		default:
			files, tests := eligibilityFiles(e.body)
			members = append(members, laneMember{id: id, class: class, revision: rev, eligibility: e.id, files: files, tests: tests})
		}
	}
	sort.Slice(members, func(i, j int) bool { return members[i].id < members[j].id })
	sort.Strings(excluded)
	printExcluded := func() {
		if len(excluded) > 0 {
			fmt.Println("not in this batch:")
			for _, x := range excluded {
				fmt.Printf("  %s\n", x)
			}
		}
	}
	if len(members) == 0 {
		fmt.Println("nothing to complete: no small change is IN_REVIEW with a passing eligibility at its delivered revision")
		printExcluded()
		return nil
	}
	for _, m := range members {
		if gitOut(env.Root, "rev-parse", "--verify", "--quiet", m.revision+"^{commit}") == "" {
			return fmt.Errorf("%s's delivered revision %s is not a commit in this repository — fetch the default branch first; nothing was written", m.id, laneShort(m.revision))
		}
	}
	gateID, err := laneFreeGateID(env, "LANE-BATCH-"+time.Now().UTC().Format("20060102"))
	if err != nil {
		return err
	}
	ids := make([]string, len(members))
	for i, m := range members {
		ids[i] = m.id
	}
	fmt.Printf("lane batch %s over %d small change(s): %s\n", gateID, len(members), strings.Join(ids, ", "))
	for _, m := range members {
		fmt.Printf("  %s (%s) delivered at %s — %s; files: %s\n", m.id, m.class, laneShort(m.revision), m.eligibility, strings.Join(m.files, ", "))
	}
	printExcluded()
	if dryRun {
		fmt.Println("dry run — nothing posted")
		return nil
	}

	var traces []string
	for _, m := range members {
		if err := holdLanePiece(env, m.id, "completion"); err != nil {
			return err
		}
		agg, _, err := singleSRAggregate(env, m.id)
		if err != nil {
			return err
		}
		if err := factoryEvidenceRun(env, evidenceOpts{kind: kind, log: log, pass: m.id, revision: m.revision}); err != nil {
			return err
		}
		traceID, existing, _, err := freeTraceID(env, "LANE-COMPLETE-TRACE-"+m.id, agg)
		if err != nil {
			return err
		}
		if existing != "" {
			printInfo("%s already passes for %s at %s — reused", existing, m.id, agg)
			traces = append(traces, existing)
			continue
		}
		fields := map[string]any{
			"title":                fmt.Sprintf("%s completion trace — recorded by process lane complete at %s", m.id, laneShort(m.revision)),
			"purpose":              "completion",
			"transition":           "IN_REVIEW->DONE",
			"exact_scope":          []string{m.id},
			"fingerprint":          agg,
			"verdict":              "PASS",
			"sources":              traceSources([]string{"RUN:" + log}),
			"application_revision": m.revision,
			"body_md": fmt.Sprintf("Lane completion audit of %s at its delivered revision %s. Evidence: the %s run the agent reported (%s), naming %s; not verified. Eligibility: %s PASS (server verdict over the file list the CLI reported).",
				m.id, m.revision, kind, log, m.id, m.eligibility),
		}
		if err := authorTrace(env, traceID, fields); err != nil {
			return err
		}
		traces = append(traces, traceID)
	}

	var changes strings.Builder
	changes.WriteString("Approved small changes become DONE; a rejected one stays IN_REVIEW.\n")
	options := []map[string]any{{"key": "approve", "label": "Approve every small change not rejected"}}
	for _, m := range members {
		fmt.Fprintf(&changes, "- %s (%s, delivered at %s): %s", m.id, m.class, laneShort(m.revision), strings.Join(m.files, ", "))
		if m.tests != "" {
			fmt.Fprintf(&changes, "; %s test file(s)", m.tests)
		}
		changes.WriteString("\n")
		options = append(options, map[string]any{"key": "reject:" + m.id, "label": "Reject " + m.id})
	}
	brief := map[string]any{
		"what":                fmt.Sprintf("Complete %d small change(s): %s", len(members), strings.Join(ids, ", ")),
		"why_now":             fmt.Sprintf("Each is IN_REVIEW after a narrow independent review. The run is the run the agent reported (%s, %s); nothing verified it. Eligibility is the server's verdict over the file list the CLI reported at each delivered revision.", kind, log),
		"changes_if_approved": strings.TrimRight(changes.String(), "\n"),
		"risk_if_wrong":       "A wrong small change is marked DONE; reject it with reject:<SR> to keep it IN_REVIEW.",
		"recommendation":      "Approve, rejecting any change whose files or class you do not accept.",
	}
	fields := map[string]any{
		"title":                          fmt.Sprintf("Complete %d small change(s)", len(members)),
		"gate_class":                     "human",
		"gate_kind":                      "approval_request",
		"purpose":                        "lane-batch",
		"transition":                     "IN_REVIEW->DONE",
		"exact_scope":                    ids,
		"options":                        options,
		"recommended_option_key":         "approve",
		"brief":                          brief,
		"prerequisite_gate_external_ids": traces,
	}
	if _, err := authorCreate(env, "gate", gateID, fields); err != nil {
		return fmt.Errorf("%w\n  the runs and completion traces are recorded; fix the fact the refusal names and rerun — the traces are reused", err)
	}
	fmt.Printf("answer it in the web app (Mission Control) or with `factory answer %s --options approve[,reject:<SR>…]`: approve, rejecting any change you do not accept; then `process lane complete --apply`\n", gateID)
	return nil
}

func processLaneApply(env *factoryEnv, gateID string) error {
	var batches []map[string]any
	if gateID != "" {
		status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/gates/%s?system_id=%d", url.PathEscape(gateID), env.SystemID), nil)
		if err != nil {
			return err
		}
		if status != 200 {
			return gateShowError(status, body, gateID)
		}
		g, _ := dataOf(body)["gate"].(map[string]any)
		if str(g, "purpose") != "lane-batch" || str(g, "state") != "answered" {
			return fmt.Errorf("%s is a %s gate in state %s — --apply takes an answered lane-batch gate; nothing was written", gateID, presentPin(str(g, "purpose")), presentPin(str(g, "state")))
		}
		batches = append(batches, g)
	} else {
		gates, err := listGates(env, "answered")
		if err != nil {
			return err
		}
		for _, g := range gates {
			if str(g, "purpose") == "lane-batch" {
				batches = append(batches, g)
			}
		}
	}
	if len(batches) == 0 {
		return fmt.Errorf("there is no answered lane-batch gate to apply — open one with `process lane complete --log <run>` and answer it in the web app or with `factory answer <gate>`; nothing was written")
	}
	failed := 0
	for _, g := range batches {
		id, fp := str(g, "external_id"), str(g, "fingerprint")
		chosen := stringSlice(g["chosen_option_keys"])
		members := stringSlice(g["exact_scope"])
		fmt.Printf("── %s (answer: %s)\n", id, firstNonEmpty(str(g, "answer"), strings.Join(chosen, ", ")))
		items, err := fetchDirectItems(env, members, false, false)
		if err != nil {
			return err
		}
		approved := slices.Contains(chosen, "approve")
		for _, m := range members {
			status := str(items[m].payload, "work_status")
			switch {
			case slices.Contains(chosen, "reject:"+m):
				fmt.Printf("  ✗ %s rejected — it stays IN_REVIEW; fix it and include it in a new lane batch\n", m)
			case !approved:
				fmt.Printf("  %s not approved — the batch approved nothing\n", m)
			case status == "DONE":
				fmt.Printf("  %s already DONE\n", m)
			case status != "IN_REVIEW":
				fmt.Printf("  ✗ %s is %s, not IN_REVIEW — not advanced\n", m, presentPin(status))
				failed++
			default:
				if err := authorAdvance(env, "requirement", m, "DONE", "IN_REVIEW", id, fp, "approve", ""); err != nil {
					fmt.Printf("  ✗ %s not advanced: %v\n", m, err)
					failed++
				}
			}
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d approved small change(s) were not advanced — each reason is printed above", failed)
	}
	return nil
}

func init() {
	f := processLaneAuthorizeCmd.Flags()
	f.StringSliceVar(&laneAuthClasses, "classes", nil, "the classes covered: defect_with_failing_test, wording, presentation, dependency_patch (comma-separated or repeated)")
	f.StringSliceVar(&laneAuthAppliers, "appliers", nil, "the user ids who may apply it (comma-separated or repeated)")
	f.StringVar(&laneAuthExpires, "expires", "", "the expiry: a date, a date and time, or <n>d from now; at most 30 days")
	f.IntVar(&laneAuthCap, "cap", 0, "the daily cap of lane applications (1 to 10)")
	f.StringArrayVar(&laneAuthExclude, "exclude", nil, "a further excluded path glob (repeatable)")
	f.StringVar(&laneAuthFile, "file", "", "read the terms from a JSON file (see the example above)")
	f.StringVar(&laneAuthID, "id", "", "the gate id (default: the first free LANE-AUTH id)")
	f.StringVar(&laneAuthTitle, "title", "", "the gate title")

	processLaneApproveCmd.Flags().StringVar(&laneApproveText, "text", "", "your decision, recorded as written (default \"approve\")")

	processLaneReviewCmd.Flags().StringVar(&laneReviewFile, "file", "", "the reviewer's report file (JSON)")

	processLaneCheckCmd.Flags().StringVar(&laneCheckCommit, "commit", "", "the commit that delivered the change to the default branch (a merge or squash commit, or a rebase delivery's tip)")
	processLaneCheckCmd.Flags().StringVar(&laneCheckBase, "base", "", "for a rebase delivery: the commit the change started from")

	c := processLaneCompleteCmd.Flags()
	c.BoolVar(&laneCompleteApply, "apply", false, "mark the approved changes DONE after the approval")
	c.StringVar(&laneCompleteLog, "log", "", "the run you report for the delivered changes, such as a CI link (required to open a batch)")
	c.StringVar(&laneCompleteKind, "kind", "ci", "the evidence run kind")
	c.StringVar(&laneCompleteGateID, "gate", "", "with --apply: the batch to apply (default: every approved one)")
	c.BoolVar(&laneCompleteDryRun, "dry-run", false, "print the batch and write nothing")

	processLaneCmd.AddCommand(processLaneAuthorizeCmd, processLaneApproveCmd, processLaneReviewCmd, processLaneEnterCmd, processLaneCheckCmd, processLaneCompleteCmd)
	processCmd.AddCommand(processLaneCmd)
}

func laneShort(v string) string {
	if len(v) > 12 {
		return v[:12]
	}
	return v
}
