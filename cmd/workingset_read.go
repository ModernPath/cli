package cmd

import (
	"fmt"
	"net/url"
	"unicode/utf8"
)

const (
	maxDirectItemIDs = 100
	// Bandit defaults to a 10,000-byte request-line limit. Keep the encoded
	// request target below 7,000 bytes, leaving room for the method/version and
	// gateway prefix. Count bytes after URL escaping, including any API base path.
	maxDirectItemRequestTargetBytes = 7_000
)

// fetchList GETs one read endpoint and returns the named collection. A non-200
// carries the server's reason — never a bare status.
func fetchList(env *factoryEnv, path, key string) ([]any, error) {
	status, body, err := env.call("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, serverRefusal("", status, body)
	}
	return listFromData(body, key)
}

// listFromData extracts one named list from a decoded {"data":{…}} envelope.
// A 200 without the expected key is a changed envelope, not an empty list —
// reading it as empty overwrote a correct projection with "queue is clear" and
// reported every materialized file vanished.
func listFromData(body map[string]any, key string) ([]any, error) {
	raw, present := dataOf(body)[key]
	if !present {
		return nil, fmt.Errorf("server response has no %q — refusing to read a changed envelope as empty", key)
	}
	if raw == nil {
		return []any{}, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("server response %q is not a list — refusing to read a changed envelope as empty", key)
	}
	return items, nil
}

// fetchRequirementLists reads the requirements endpoint ONCE and returns both
// the system-requirement list (data.requirements) and the user-requirement list
// (data.user_requirements): the endpoint serves both keys in a single envelope
// (sync_api_controller requirements/2), so one GET indexes the whole tree.
func fetchRequirementLists(env *factoryEnv, path string) (srs []any, urs []any, err error) {
	status, body, err := env.call("GET", path, nil)
	if err != nil {
		return nil, nil, err
	}
	if status != 200 {
		return nil, nil, serverRefusal("", status, body)
	}
	if srs, err = listFromData(body, "requirements"); err != nil {
		return nil, nil, err
	}
	if urs, err = listFromData(body, "user_requirements"); err != nil {
		return nil, nil, err
	}
	return srs, urs, nil
}

// --- REQ-CROSS-216/218: working-set pull ---

type wsItem struct {
	id      string
	kind    string // "epic" | "system" | "user" | "backlog" | "gate"
	payload map[string]any
	gates   []any
	// REQ-CROSS-489: the served `traces` key ({from, to, truncated}), read-only
	// and outside the canonical payload; nil when the read did not serve it.
	traces map[string]any
}

// fetchDirectItems resolves only the supplied external IDs. The server returns
// canonical item payloads and each item's associated gate history in one
// bounded response; older servers fail clearly instead of falling back to
// downloading system collections. includeTraces asks for each requirement's
// trace links (REQ-CROSS-489); only the by-id pull and check set it.
func fetchDirectItems(env *factoryEnv, ids []string, includeCandidates, includeTraces bool) (map[string]wsItem, error) {
	unique := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || utf8.RuneCountInString(id) > 255 {
			return nil, fmt.Errorf("external id must be nonempty and at most 255 characters")
		}
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	index := make(map[string]wsItem, len(unique))
	baseValues := url.Values{}
	baseValues.Set("system_id", fmt.Sprint(env.SystemID))
	if includeCandidates {
		baseValues.Set("include", "candidates")
	}
	if includeTraces {
		baseValues.Set("include_traces", "true")
	}
	apiBasePath := ""
	if apiURL, err := url.Parse(env.APIURL); err == nil {
		apiBasePath = apiURL.EscapedPath()
	}

	for start := 0; start < len(unique); {
		values := cloneURLValues(baseValues)
		chunkSeen := map[string]bool{}
		next := start
		for next < len(unique) && next-start < maxDirectItemIDs {
			id := unique[next]
			candidate := cloneURLValues(values)
			candidate.Add("ids[]", id)
			candidatePath := "/api/v1/sync/items?" + candidate.Encode()
			if len(apiBasePath+candidatePath) > maxDirectItemRequestTargetBytes {
				if next == start {
					return nil, fmt.Errorf("direct item request target exceeds %d-byte budget for %q", maxDirectItemRequestTargetBytes, id)
				}
				break
			}
			values = candidate
			chunkSeen[id] = true
			next++
		}
		path := "/api/v1/sync/items?" + values.Encode()
		status, body, err := env.call("GET", path, nil)
		if err != nil {
			return nil, err
		}
		if status != 200 {
			return nil, serverRefusal("named item read", status, body)
		}
		rows, err := listFromData(body, "items")
		if err != nil {
			return nil, err
		}
		for _, raw := range rows {
			entry, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("server response item is not an object")
			}
			payload, ok := entry["item"].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("server response item has no canonical payload")
			}
			id := str(payload, "external_id")
			kind := str(entry, "kind")
			if id == "" || !validDirectItemKind(kind) || !chunkSeen[id] {
				return nil, fmt.Errorf("server response item has an invalid or unrequested identity")
			}
			gates, _ := entry["gates"].([]any)
			traces, _ := entry["traces"].(map[string]any)
			index[id] = wsItem{id: id, kind: kind, payload: payload, gates: gates, traces: traces}
		}
		start = next
	}
	return index, nil
}

func cloneURLValues(values url.Values) url.Values {
	clone := make(url.Values, len(values))
	for key, entries := range values {
		clone[key] = append([]string(nil), entries...)
	}
	return clone
}

func validDirectItemKind(kind string) bool {
	switch kind {
	case "epic", "system", "user", "backlog", "gate":
		return true
	default:
		return false
	}
}

func scopeIndex(env *factoryEnv, ids []string) (map[string]scopeRecord, error) {
	items, err := fetchDirectItems(env, ids, false, false)
	if err != nil {
		return nil, err
	}
	index := make(map[string]scopeRecord, len(items))
	for id, item := range items {
		index[id] = scopeRecord{kind: item.kind, payload: item.payload}
	}
	return index, nil
}

// --- REQ-CROSS-313 (SR-CLI-0084): scope-shaped pull with the authoring render ---

type scopeRecord struct {
	kind    string // "epic" | "system" | "user"
	payload map[string]any
}
