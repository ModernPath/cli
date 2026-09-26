package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNamedPullUsesOnlyTheRequestedDirectItems(t *testing.T) {
	fx := &wsFixture{items: []any{
		map[string]any{
			"kind":  "epic",
			"item":  wsEpic("EPIC-ONE", "One epic"),
			"gates": []any{wsGate("APPROVE-EPIC-ONE", "approval_request")},
		},
		map[string]any{
			"kind": "user",
			"item": map[string]any{
				"external_id": "UR-ONE", "title": "One user requirement", "description": "Only this record",
				"work_status": "TODO", "system_requirement_external_ids": []any{"SR-ONE"},
			},
			"gates": []any{},
		},
	}}
	env := wsEnv(t, wsServe(t, fx))

	pullErr := workingSetPull(env, []string{"EPIC-ONE", "UR-ONE"}, wsNow)
	query, err := url.ParseQuery(fx.lastItemsQuery)
	if err != nil {
		t.Fatalf("invalid direct-read query %q: %v", fx.lastItemsQuery, err)
	}
	if got, want := query["ids[]"], []string{"EPIC-ONE", "UR-ONE"}; !reflect.DeepEqual(got, want) {
		t.Errorf("direct read must name only requested IDs, got ids[]=%v want %v", got, want)
	}
	for _, forbidden := range []string{
		"GET /api/v1/sync/epics", "GET /api/v1/sync/requirements",
		"GET /api/v1/sync/gates", "GET /api/v1/sync/backlog",
	} {
		for _, request := range fx.requests {
			if request == forbidden {
				t.Errorf("named pull fetched unrelated system collection: %s; requests=%v", forbidden, fx.requests)
			}
		}
	}
	if pullErr != nil {
		t.Errorf("named pull failed: %v", pullErr)
	}
	if t.Failed() {
		return
	}

	for name, want := range map[string]string{
		"EPIC-ONE.md": "APPROVE-EPIC-ONE",
		"UR-ONE.md":   "One user requirement",
	} {
		got, err := os.ReadFile(filepath.Join(env.Root, workingSetDir, name))
		if err != nil {
			t.Errorf("direct payload did not materialize %s: %v", name, err)
			continue
		}
		if !strings.Contains(string(got), want) {
			t.Errorf("%s did not render direct payload field %q:\n%s", name, want, got)
		}
	}
}

func TestNamedRequirementPullDoesNotReadCollections(t *testing.T) {
	fx := &wsFixture{items: []any{
		map[string]any{
			"kind":  "system",
			"item":  wsReq("REQ-ONE", "One requirement"),
			"gates": []any{},
		},
	}}
	env := wsEnv(t, wsServe(t, fx))
	pullErr := workingSetPull(env, []string{"REQ-ONE"}, wsNow)
	for _, request := range fx.requests {
		switch request {
		case "GET /api/v1/sync/epics", "GET /api/v1/sync/requirements",
			"GET /api/v1/sync/gates", "GET /api/v1/sync/backlog":
			t.Errorf("one requirement pull fetched unrelated system collection %s; requests=%v", request, fx.requests)
		}
	}
	if pullErr != nil {
		t.Errorf("named requirement pull failed: %v", pullErr)
	}
}

func TestTracePinReadsContentHashFromExactRequirement(t *testing.T) {
	fx := &wsFixture{items: []any{
		map[string]any{
			"kind":  "system",
			"item":  map[string]any{"external_id": "REQ-PIN", "fingerprint": "served-current-hash"},
			"gates": []any{},
		},
	}}
	env := wsEnv(t, wsServe(t, fx))
	hashes, reason := readContentHashesFor(env, []string{"REQ-PIN"})
	if reason != "" || hashes["REQ-PIN"] != "served-current-hash" {
		t.Errorf("trace pin should read the named current requirement hash, got hashes=%v reason=%q", hashes, reason)
	}
	if fx.lastItemsQuery == "" {
		t.Error("trace pin did not use the direct item read")
	}
	for _, request := range fx.requests {
		if request == "GET /api/v1/sync/requirements" {
			t.Errorf("trace pin loaded the whole requirement collection: %v", fx.requests)
		}
	}
}

func TestDirectItemReadsChunkRequestsAtServerBound(t *testing.T) {
	fx := &wsFixture{items: []any{}}
	env := wsEnv(t, wsServe(t, fx))
	ids := make([]string, maxDirectItemIDs+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("REQ-BATCH-%03d", i)
	}
	if _, err := fetchDirectItems(env, ids, false); err != nil {
		t.Fatalf("bounded direct read failed: %v", err)
	}
	reads := 0
	for _, request := range fx.requests {
		if request == "GET /api/v1/sync/items" {
			reads++
		}
		if strings.Contains(request, "/api/v1/sync/epics") || strings.Contains(request, "/api/v1/sync/requirements") ||
			strings.Contains(request, "/api/v1/sync/gates") || strings.Contains(request, "/api/v1/sync/backlog") {
			t.Fatalf("batched direct read used a whole-system collection: %v", fx.requests)
		}
	}
	if reads != 2 {
		t.Fatalf("%d IDs should be split into 2 bounded requests, got %d: %v", len(ids), reads, fx.requests)
	}
}

func TestDirectItemReadsRespectEncodedRequestTargetBudget(t *testing.T) {
	const count = maxDirectItemIDs
	for _, tc := range []struct {
		name   string
		suffix string
	}{
		{name: "ascii", suffix: strings.Repeat("a", 190)},
		{name: "unicode", suffix: strings.Repeat("ø", 90)},
		{name: "escaped punctuation", suffix: strings.Repeat("/?#[]&=+", 24)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requestTargets []string
			seen := map[string]int{}
			mux := http.NewServeMux()
			mux.HandleFunc("/api/ex/api/v1/sync/items", func(w http.ResponseWriter, r *http.Request) {
				target := r.URL.RequestURI()
				requestTargets = append(requestTargets, target)
				if len(target) > 10_000 {
					w.WriteHeader(http.StatusRequestURITooLong)
					return
				}
				if r.URL.Query().Get("include") != "candidates" {
					t.Errorf("candidate include flag was lost: %q", target)
				}
				items := make([]any, 0)
				for _, id := range r.URL.Query()["ids[]"] {
					seen[id]++
					items = append(items, map[string]any{
						"kind":  "user",
						"item":  map[string]any{"external_id": id, "fingerprint": "current"},
						"gates": []any{},
					})
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"items": items}})
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)
			env := wsEnv(t, srv)
			env.APIURL = srv.URL + "/api/ex"
			ids := make([]string, count)
			for i := range ids {
				ids[i] = fmt.Sprintf("UR-%03d-%s", i, tc.suffix)
			}

			got, err := fetchDirectItems(env, ids, true)
			if err != nil {
				t.Fatalf("long valid IDs should be chunked below the request-target bound, got %v (targets: %v)", err, requestTargets)
			}
			if len(got) != count {
				t.Fatalf("resolved %d of %d exact IDs", len(got), count)
			}
			if len(requestTargets) < 2 {
				t.Fatalf("100 long IDs should split across multiple bounded targets, got %d", len(requestTargets))
			}
			for _, target := range requestTargets {
				if len(target) > 8_000 {
					t.Errorf("request target exceeds conservative byte budget: %d bytes", len(target))
				}
			}
			for _, id := range ids {
				if seen[id] != 1 {
					t.Errorf("ID sent %d times, want exactly once: %q", seen[id], id)
				}
			}
		})
	}
}
