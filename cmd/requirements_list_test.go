package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// UR-CLI-DISCOVERY-001: requirements list uses the authenticated compact
// discovery endpoint and follows its opaque cursor when the user supplies it.
func TestRequirementsListUserFlow(t *testing.T) {
	var requests []url.Values
	var rawQueries []string
	legacyRequests := 0
	mux := http.NewServeMux()
	addDiscoveryTestSystems(mux)
	mux.HandleFunc("/api/v1/sync/requirements", func(w http.ResponseWriter, r *http.Request) {
		legacyRequests++
		http.Error(w, "legacy endpoint must not be called", http.StatusInternalServerError)
	})
	mux.HandleFunc("/api/v1/sync/requirements/discovery", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer t" {
			http.Error(w, "unauthorized discovery request", http.StatusUnauthorized)
			return
		}
		query := r.URL.Query()
		requests = append(requests, query)
		rawQueries = append(rawQueries, r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")

		var page map[string]any
		switch query.Get("cursor") {
		case "":
			page = map[string]any{
				"data": map[string]any{
					"items": []map[string]any{
						{"id": "11111111-1111-4111-8111-111111111111", "requirement_kind": "system_requirement", "system_id": 7, "display_ref": "SR-16-1", "work_status": "TODO", "context": "SECURITY", "title": "Firewall system requirement", "title_truncated": false, "description": "Firewall for the workspace.", "description_truncated": false},
						{"id": "22222222-2222-4222-8222-222222222222", "requirement_kind": "user_requirement", "system_id": 7, "display_ref": "UR-4-1", "work_status": "TODO", "context": "SECURITY", "title": "Firewall user requirement", "title_truncated": false, "description": "Firewall for workspace users.", "description_truncated": false},
					},
					"next_cursor": "opaque-cursor-2",
				},
			}
		case "opaque-cursor-2":
			page = map[string]any{
				"data": map[string]any{
					"items": []map[string]any{
						{"id": "33333333-3333-4333-8333-333333333333", "requirement_kind": "system_requirement", "system_id": 7, "display_ref": "SR-17-1", "work_status": "TODO", "context": "SECURITY", "title": "Firewall audit requirement", "title_truncated": false, "description": "Firewall audit for the workspace.", "description_truncated": false},
					},
					"next_cursor": nil,
				},
			}
		default:
			http.Error(w, "unexpected cursor", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(page)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cobraWorkspace(t, srv)

	queryText := "firewall%_ &"
	firstOutput, err := runRoot(t, "requirements", "list", "--query", queryText, "--context", "SECURITY", "--limit", "2", "--order", "title", "--json")
	if err != nil {
		t.Fatalf("requirements list first page: %v", err)
	}
	var first map[string]json.RawMessage
	if err := json.Unmarshal([]byte(firstOutput), &first); err != nil {
		t.Fatalf("first page is not JSON: %v\n%s", err, firstOutput)
	}
	assertJSONKeys(t, first, []string{"items", "next_cursor"})
	var firstItems []map[string]json.RawMessage
	if err := json.Unmarshal(first["items"], &firstItems); err != nil {
		t.Fatalf("first-page items are not a JSON array: %v", err)
	}
	if len(firstItems) != 2 {
		t.Fatalf("first page has %d items, want 2", len(firstItems))
	}
	for i, wantKind := range []string{"system_requirement", "user_requirement"} {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(mustJSON(t, firstItems[i]), &item); err != nil {
			t.Fatalf("item %d is malformed: %v", i, err)
		}
		assertJSONKeys(t, item, []string{"display_ref", "id", "requirement_kind", "title"})
		var id, kind string
		_ = json.Unmarshal(item["id"], &id)
		_ = json.Unmarshal(item["requirement_kind"], &kind)
		if id == "" || kind != wantKind {
			t.Errorf("item %d identity = %s/%s, want UUID/%s", i, id, kind, wantKind)
		}
	}
	var next string
	if err := json.Unmarshal(first["next_cursor"], &next); err != nil || next != "opaque-cursor-2" {
		t.Fatalf("first next_cursor = %q, err=%v", next, err)
	}

	secondOutput, err := runRoot(t, "requirements", "list", "--query", queryText, "--context", "SECURITY", "--limit", "2", "--order", "title", "--cursor", "opaque-cursor-2", "--json")
	if err != nil {
		t.Fatalf("requirements list continuation: %v", err)
	}
	var second requirementDiscoveryPage
	if err := json.Unmarshal([]byte(secondOutput), &second); err != nil {
		t.Fatalf("continuation is not JSON: %v\n%s", err, secondOutput)
	}
	if len(second.Items) != 1 || second.Items[0].DisplayRef != "SR-17-1" || second.NextCursor != nil {
		t.Fatalf("continuation page = %+v, want exhausted SR-17-1 page", second)
	}
	if len(requests) != 2 {
		t.Fatalf("discovery requests = %d, want exactly two pages", len(requests))
	}
	wantBase := url.Values{"system_id": {"1"}, "q": {queryText}, "context": {"SECURITY"}, "limit": {"2"}}
	wantBase.Set("order", "title")
	if !reflect.DeepEqual(requests[0], wantBase) {
		t.Errorf("first-page query = %v, want %v", requests[0], wantBase)
	}
	if len(rawQueries) == 2 && rawQueries[0] != wantBase.Encode() {
		t.Errorf("first-page query was not safely encoded: %q, want %q", rawQueries[0], wantBase.Encode())
	}
	wantContinuation := url.Values{
		"system_id": {"1"}, "q": {queryText}, "context": {"SECURITY"}, "limit": {"2"}, "cursor": {"opaque-cursor-2"}, "order": {"title"},
	}
	if !reflect.DeepEqual(requests[1], wantContinuation) {
		t.Errorf("continuation query = %v, want %v", requests[1], wantContinuation)
	}
	if len(rawQueries) == 2 && rawQueries[1] != wantContinuation.Encode() {
		t.Errorf("continuation query was not safely encoded: %q, want %q", rawQueries[1], wantContinuation.Encode())
	}
	if legacyRequests != 0 {
		t.Fatalf("legacy requirements endpoint requests = %d, want none", legacyRequests)
	}
}

func TestRequirementsListUnsupportedServerDoesNotFallback(t *testing.T) {
	legacyRequests := 0
	mux := http.NewServeMux()
	addDiscoveryTestSystems(mux)
	mux.HandleFunc("/api/v1/sync/requirements", func(w http.ResponseWriter, _ *http.Request) {
		legacyRequests++
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cobraWorkspace(t, srv)

	_, err := runRoot(t, "requirements", "list", "--json")
	if err == nil || !strings.Contains(err.Error(), "does not support requirement discovery") {
		t.Fatalf("unsupported endpoint error = %v, want clear discovery support error", err)
	}
	if legacyRequests != 0 {
		t.Fatalf("legacy requirements endpoint requests = %d, want none", legacyRequests)
	}
}

func TestRequirementsListRejectsInvalidFlagsBeforeRequest(t *testing.T) {
	requests := 0
	mux := http.NewServeMux()
	addDiscoveryTestSystems(mux)
	mux.HandleFunc("/api/v1/sync/requirements/discovery", func(w http.ResponseWriter, _ *http.Request) {
		requests++
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cobraWorkspace(t, srv)

	_, err := runRoot(t, "requirements", "list", "--status", "NOT_A_STATUS")
	if err == nil || !strings.Contains(err.Error(), "invalid --status") {
		t.Fatalf("invalid status error = %v", err)
	}
	_, err = runRoot(t, "requirements", "list", "--status", "DERIVED")
	if err == nil || !strings.Contains(err.Error(), "invalid --status") {
		t.Fatalf("retired status error = %v", err)
	}
	_, err = runRoot(t, "requirements", "list", "--kind", "epic")
	if err == nil || !strings.Contains(err.Error(), "invalid --kind") {
		t.Fatalf("invalid kind error = %v", err)
	}
	_, err = runRoot(t, "requirements", "list", "--limit", "201")
	if err == nil || !strings.Contains(err.Error(), "between 1 and 200") {
		t.Fatalf("invalid limit error = %v", err)
	}
	_, err = runRoot(t, "requirements", "list", "--order", "bogus")
	if err == nil || !strings.Contains(err.Error(), "invalid --order") {
		t.Fatalf("invalid order error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("invalid flags sent %d discovery requests, want none", requests)
	}
}

func TestRequirementsListRejectsMalformedResponse(t *testing.T) {
	mux := http.NewServeMux()
	addDiscoveryTestSystems(mux)
	mux.HandleFunc("/api/v1/sync/requirements/discovery", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"items":[{"external_id":"REQ-BAD"}],"next_cursor":null}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cobraWorkspace(t, srv)

	_, err := runRoot(t, "requirements", "list", "--json")
	if err == nil || !strings.Contains(err.Error(), "upgrade the server before listing requirements") {
		t.Fatalf("malformed response error = %v", err)
	}
}

func TestRequirementsListAcceptsUUIDRequirementIdentityAndDisplayReference(t *testing.T) {
	const requirementID = "8f5ec2c0-57c1-4a9f-aeb0-a8dfc4d12a9c"
	item, err := decodeRequirementDiscoveryItem(map[string]any{
		"id": requirementID, "requirement_kind": "system_requirement", "system_id": json.Number("7"),
		"display_ref": "SR-16-1", "title": "Preserve the requirement identity",
		"work_status": "TODO", "context": "CMP", "description": "A UUID-backed requirement.",
		"title_truncated": false, "description_truncated": false,
	})
	if err != nil {
		t.Fatalf("requirements list decoder rejected UUID-backed response: %v", err)
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("decoded requirement identity is not JSON: %v", err)
	}
	var output map[string]any
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatalf("decoded requirement identity is malformed JSON: %v", err)
	}
	if output["id"] != requirementID || output["requirement_kind"] != "system_requirement" || output["system_id"] != float64(7) || output["display_ref"] != "SR-16-1" {
		t.Fatalf("requirement identity/presentation = %#v, want UUID, kind, System and canonical display reference", output)
	}
	if _, exists := output["external_id"]; exists {
		t.Fatalf("active requirement response still exposes external_id: %#v", output)
	}
}

func TestRequirementsListHumanOutputShowsSummariesAndContinuation(t *testing.T) {
	mux := http.NewServeMux()
	addDiscoveryTestSystems(mux)
	mux.HandleFunc("/api/v1/sync/requirements/discovery", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"items": []map[string]any{{
				"id": "44444444-4444-4444-8444-444444444444", "requirement_kind": "system_requirement", "system_id": 7,
				"display_ref": "SR-18-1", "work_status": nil,
				"context": nil, "title": "Bounded title", "title_truncated": true,
				"description": "Bounded description", "description_truncated": true,
			}},
			"next_cursor": "opaque-next",
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cobraWorkspace(t, srv)

	output, err := runRoot(t, "requirements", "list")
	if err != nil {
		t.Fatalf("requirements list: %v", err)
	}
	for _, want := range []string{"SR-18-1 Bounded title"} {
		if !strings.Contains(output, want) {
			t.Errorf("human output omitted %q:\n%s", want, output)
		}
	}
}

func addDiscoveryTestSystems(mux *http.ServeMux) {
	mux.HandleFunc("/api/systems", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t" {
			http.Error(w, "missing bearer", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 1, "name": "Test system"}})
	})
}

func assertJSONKeys(t *testing.T, object map[string]json.RawMessage, want []string) {
	t.Helper()
	got := make([]string, 0, len(object))
	for key := range object {
		got = append(got, key)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON keys = %v, want exactly %v", got, want)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestRequirementsListFiltersCurrentLifecycleStatus(t *testing.T) {
	mux := http.NewServeMux()
	addDiscoveryTestSystems(mux)
	called := false
	mux.HandleFunc("/api/v1/sync/requirements/discovery", func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Query().Get("status") != "DONE" {
			t.Errorf("status filter = %q", r.URL.Query().Get("status"))
		}
		_, _ = w.Write([]byte(`{"data":{"items":[],"next_cursor":null}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	cobraWorkspace(t, srv)
	if _, err := runRoot(t, "requirements", "list", "--status", "DONE", "--json"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("the server must apply the lifecycle status filter")
	}
}
