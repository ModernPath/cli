package cmd

import (
	"fmt"
	"net/url"

	"slices"
	"sort"

	"strings"
	"time"
)

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

func anyList(v any) []any {
	items, _ := v.([]any)
	return items
}

func laneShort(v string) string {
	if len(v) > 12 {
		return v[:12]
	}
	return v
}
