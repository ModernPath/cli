package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const membershipSR = "0951fc74-235d-4041-98ea-0dfa2a870e7d"
const membershipUR = "d04e4a91-9585-43b0-811a-f90de731ae8d"

func TestMembershipFailureIdentifiesRefusedRequirement(t *testing.T) {
	posts := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			listMembershipReleases(w)
			return
		}
		posts++
		w.WriteHeader(422)
		json.NewEncoder(w).Encode(map[string]any{"error": "release_exempt", "requirement": map[string]any{"kind": "user_requirement", "id": membershipUR, "reason": "release_exempt"}})
	}))
	defer s.Close()
	_, err := runMembership(t, s, "move-requirements", "next", "--file", membershipInput(t, map[string]any{"requirements": membershipSelection()}), "--json")
	if err == nil || !strings.Contains(err.Error(), "user_requirement:"+membershipUR) || !strings.Contains(err.Error(), "release_exempt") {
		t.Fatalf("missing refused requirement: %v", err)
	}
	if posts != 1 {
		t.Fatalf("refusal was retried: %d requests", posts)
	}
}

func membershipInput(t *testing.T, v any) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "input.json")
	b, _ := json.Marshal(v)
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	return p
}
func membershipSelection() []any {
	return []any{map[string]any{"kind": "system_requirement", "id": membershipSR}, map[string]any{"kind": "user_requirement", "id": membershipUR}}
}
func membershipReceipt() map[string]any {
	return map[string]any{
		"system_id": 4, "target": map[string]any{"id": 2, "slug": "next", "system_id": 4},
		"counts":       map[string]any{"move": 2, "unchanged": 0},
		"requirements": []any{map[string]any{"kind": "system_requirement", "id": membershipSR, "from_release_id": nil, "action": "move"}, map[string]any{"kind": "user_requirement", "id": membershipUR, "from_release_id": 1, "action": "move"}},
		"apply":        map[string]any{"system_id": 4, "target_release_id": 2, "requirements": []any{map[string]any{"kind": "system_requirement", "id": membershipSR, "expected_release_id": nil}, map[string]any{"kind": "user_requirement", "id": membershipUR, "expected_release_id": 1}}},
	}
}
func listMembershipReleases(w http.ResponseWriter) {
	fmt.Fprint(w, `{"data":{"items":[{"id":1,"system_id":4,"slug":"current","status":"active","is_base":false},{"id":2,"system_id":4,"slug":"next","status":"planned","is_base":false}]}}`)
}
func runMembership(t *testing.T, s *httptest.Server, args ...string) (string, error) {
	t.Helper()
	c := newReleasesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, s, 4), nil })
	c.SetArgs(args)
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SilenceErrors = true
	c.SilenceUsage = true
	err := c.Execute()
	return out.String(), err
}
func TestReleaseRequirementPreviewAndReviewedApply(t *testing.T) {
	var paths []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			listMembershipReleases(w)
			return
		}
		paths = append(paths, r.URL.Path)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["system_id"] != float64(4) {
			t.Error("missing System")
		}
		if strings.HasSuffix(r.URL.Path, "/move") {
			if _, ok := body["requirements"].([]any)[0].(map[string]any)["expected_release_id"]; !ok {
				t.Error("missing reviewed null")
			}
		}
		receipt := membershipReceipt()
		for _, raw := range receipt["requirements"].([]any) {
			row := raw.(map[string]any)
			row["title"] = "Review selected behavior"
			row["work_status"] = "PROPOSED"
		}
		json.NewEncoder(w).Encode(map[string]any{"data": receipt})
	}))
	defer s.Close()
	out, err := runMembership(t, s, "move-requirements", "next", "--file", membershipInput(t, map[string]any{"requirements": membershipSelection()}), "--json")
	if err != nil {
		t.Fatalf("preview failed: %v", err)
	}
	if !strings.Contains(out, "Review selected behavior") || !strings.Contains(out, "PROPOSED") {
		t.Fatalf("preview omitted review context: %s", out)
	}
	if len(paths) != 1 || paths[0] != "/api/compliance/releases/2/requirements/preview" {
		t.Fatalf("wrong operation %v", paths)
	}
	p := filepath.Join(t.TempDir(), "review.json")
	os.WriteFile(p, []byte(out), 0600)
	if _, err := runMembership(t, s, "move-requirements", "2", "--file", p, "--apply", "--json"); err != nil {
		t.Fatalf("apply failed %v", err)
	}
	if len(paths) != 2 || paths[1] != "/api/compliance/releases/2/requirements/move" {
		t.Fatalf("wrong operations %v", paths)
	}
}
func TestReleaseMembersPaginatesAndValidatesExactRelease(t *testing.T) {
	for _, scenario := range []string{"complete", "foreign release", "duplicate", "truncated", "changed total"} {
		t.Run(scenario, func(t *testing.T) {
			pages := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/compliance/releases" {
					listMembershipReleases(w)
					return
				}
				if r.Method != "GET" || r.URL.Query().Get("release_id") != "2" || r.URL.Query().Get("system_id") != "4" {
					t.Errorf("unscoped %s", r.URL)
				}
				pages++
				total := 101
				rows := []any{}
				if pages == 1 {
					for i := 0; i < 100; i++ {
						rows = append(rows, map[string]any{"id": fmt.Sprintf("00000000-0000-4000-8000-%012d", i), "kind": "system_requirement", "system_id": 4, "release_id": 2})
					}
					if scenario == "truncated" {
						rows = rows[:99]
					}
					if scenario == "foreign release" {
						rows[0].(map[string]any)["release_id"] = 3
					}
				} else {
					id := membershipUR
					if scenario == "duplicate" {
						id = "00000000-0000-4000-8000-000000000000"
					}
					rows = []any{map[string]any{"id": id, "kind": "user_requirement", "system_id": 4, "release_id": 2}}
					if scenario == "duplicate" {
						rows[0].(map[string]any)["kind"] = "system_requirement"
					}
					if scenario == "changed total" {
						total = 102
					}
				}
				json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"system": map[string]any{"id": 4}, "requirements": map[string]any{"items": rows, "total_count": total}}})
			}))
			defer s.Close()
			out, err := runMembership(t, s, "members", "next", "--all", "--json")
			if scenario == "complete" {
				if err != nil || pages != 2 {
					t.Fatalf("pages %d err %v", pages, err)
				}
				var v map[string]any
				json.Unmarshal([]byte(out), &v)
				if len(v["items"].([]any)) != 101 {
					t.Fatal("incomplete")
				}
			} else if err == nil {
				t.Fatal("bad inventory claimed complete")
			}
		})
	}
}

func TestReleaseRequirementApplyRejectsInvalidReviewBeforeMutation(t *testing.T) {
	for _, scenario := range []string{"raw", "target", "system", "missing source", "altered source", "duplicate", "empty"} {
		t.Run(scenario, func(t *testing.T) {
			input := membershipReceipt()
			apply := input["apply"].(map[string]any)
			switch scenario {
			case "raw":
				input = map[string]any{"requirements": membershipSelection()}
			case "target":
				apply["target_release_id"] = 3
			case "system":
				apply["system_id"] = 5
			case "missing source":
				delete(apply["requirements"].([]any)[0].(map[string]any), "expected_release_id")
			case "altered source":
				apply["requirements"].([]any)[0].(map[string]any)["expected_release_id"] = 17
			case "duplicate":
				apply["requirements"] = []any{apply["requirements"].([]any)[0], apply["requirements"].([]any)[0]}
			case "empty":
				apply["requirements"] = []any{}
			}
			writes := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					listMembershipReleases(w)
				} else {
					writes++
					fmt.Fprint(w, `{}`)
				}
			}))
			defer s.Close()
			_, err := runMembership(t, s, "move-requirements", "next", "--file", membershipInput(t, input), "--apply", "--json")
			if err == nil || writes != 0 {
				t.Fatalf("invalid review: %v, writes %d", err, writes)
			}
		})
	}
}

func TestReleaseRequirementRefusalsAndInvalidReceiptsFailWithoutRetry(t *testing.T) {
	for _, scenario := range []string{"refusal", "foreign System", "wrong target", "missing row", "duplicate row", "wrong counts", "wrong source", "bad reusable selection", "missing source", "unexpected row"} {
		t.Run(scenario, func(t *testing.T) {
			receipt := membershipReceipt()
			switch scenario {
			case "foreign System":
				receipt["system_id"] = 5
			case "wrong target":
				receipt["target"].(map[string]any)["id"] = 7
			case "missing row":
				receipt["requirements"] = receipt["requirements"].([]any)[:1]
			case "duplicate row":
				receipt["requirements"] = []any{receipt["requirements"].([]any)[0], receipt["requirements"].([]any)[0]}
			case "wrong counts":
				receipt["counts"].(map[string]any)["move"] = 1
			case "wrong source":
				receipt["requirements"].([]any)[0].(map[string]any)["from_release_id"] = 18
			case "bad reusable selection":
				receipt["apply"].(map[string]any)["target_release_id"] = 7
			case "missing source":
				delete(receipt["requirements"].([]any)[0].(map[string]any), "from_release_id")
			case "unexpected row":
				receipt["requirements"].([]any)[0].(map[string]any)["id"] = "11111111-1111-4111-8111-111111111111"
			}
			requests := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					listMembershipReleases(w)
					return
				}
				requests++
				if scenario == "refusal" {
					w.WriteHeader(409)
					fmt.Fprint(w, `{"error":"requirement_source_changed"}`)
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"data": receipt})
			}))
			defer s.Close()
			_, err := runMembership(t, s, "move-requirements", "next", "--file", membershipInput(t, map[string]any{"requirements": membershipSelection()}), "--json")
			if err == nil || requests != 1 {
				t.Fatalf("bad receipt: err %v requests %d", err, requests)
			}
		})
	}
}

func TestReleaseRequirement1462ItemsUseOneMutationRequest(t *testing.T) {
	selected := []any{}
	reviewed := []any{}
	rows := []any{}
	for i := 0; i < 1462; i++ {
		kind := "system_requirement"
		if i >= 1358 {
			kind = "user_requirement"
		}
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
		selected = append(selected, map[string]any{"kind": kind, "id": id})
		reviewed = append(reviewed, map[string]any{"kind": kind, "id": id, "expected_release_id": nil})
		rows = append(rows, map[string]any{"kind": kind, "id": id, "from_release_id": nil, "action": "move"})
	}
	receipt := map[string]any{"system_id": 4, "target": map[string]any{"id": 2, "system_id": 4}, "requirements": rows, "counts": map[string]any{"move": 1462, "unchanged": 0}, "apply": map[string]any{"system_id": 4, "target_release_id": 2, "requirements": reviewed}}
	mutations := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			listMembershipReleases(w)
			return
		}
		mutations++
		if !strings.HasSuffix(r.URL.Path, "/move") {
			t.Errorf("unexpected operation %s", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if len(body["requirements"].([]any)) != 1462 {
			t.Error("selection was chunked or truncated")
		}
		json.NewEncoder(w).Encode(map[string]any{"data": receipt})
	}))
	defer s.Close()
	if _, err := runMembership(t, s, "move-requirements", "next", "--file", membershipInput(t, receipt), "--apply", "--json"); err != nil {
		t.Fatal(err)
	}
	if mutations != 1 {
		t.Fatalf("mutations %d", mutations)
	}
}

func TestReleaseMembersEmptyScopeAndIncompleteFirstPage(t *testing.T) {
	for _, total := range []int{0, 101} {
		t.Run(fmt.Sprint(total), func(t *testing.T) {
			pages := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/compliance/releases" {
					listMembershipReleases(w)
					return
				}
				pages++
				rows := []any{}
				if total > 0 {
					for i := 0; i < 100; i++ {
						rows = append(rows, map[string]any{"kind": "system_requirement", "id": fmt.Sprintf("00000000-0000-4000-8000-%012d", i), "system_id": 4, "release_id": 2})
					}
				}
				json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"system": map[string]any{"id": 4}, "requirements": map[string]any{"items": rows, "total_count": total}}})
			}))
			defer s.Close()
			out, err := runMembership(t, s, "members", "next", "--json")
			if err != nil || pages != 1 {
				t.Fatalf("err %v pages %d", err, pages)
			}
			var got map[string]any
			json.Unmarshal([]byte(out), &got)
			if got["complete"] != (total == 0) {
				t.Fatalf("wrong completeness %v", got["complete"])
			}
		})
	}
}

func TestReleaseRequirementMutationDoesNotRetryServerLimiting(t *testing.T) {
	previousSleep := limitSleep
	previousNotice := slowCallNoticeTo
	defer func() { limitSleep = previousSleep; slowCallNoticeTo = previousNotice }()
	limitSleep = func(_ time.Duration) {}
	slowCallNoticeTo = io.Discard
	requests := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			listMembershipReleases(w)
			return
		}
		requests++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":"rate_limited"}`)
	}))
	defer s.Close()
	_, err := runMembership(t, s, "move-requirements", "next", "--file", membershipInput(t, membershipReceipt()), "--apply", "--json")
	if err == nil || requests != 1 {
		t.Fatalf("mutation retried: %d requests, %v", requests, err)
	}
}

func TestReleaseRequirementOriginalPreviewCanBeRetriedWhenAlreadyAtTarget(t *testing.T) {
	mutations := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			listMembershipReleases(w)
			return
		}
		receipt := membershipReceipt()
		if strings.HasSuffix(r.URL.Path, "/move") {
			mutations++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			rows := body["requirements"].([]any)
			if rows[0].(map[string]any)["expected_release_id"] != nil || rows[1].(map[string]any)["expected_release_id"] != float64(1) {
				t.Error("retry did not retain the original reviewed sources")
			}
			if mutations == 2 {
				receipt["counts"] = map[string]any{"move": 0, "unchanged": 2}
				for _, raw := range receipt["requirements"].([]any) {
					row := raw.(map[string]any)
					row["from_release_id"] = 2
					row["action"] = "unchanged"
				}
				for _, raw := range receipt["apply"].(map[string]any)["requirements"].([]any) {
					raw.(map[string]any)["expected_release_id"] = 2
				}
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"data": receipt})
	}))
	defer server.Close()
	output, err := runMembership(t, server, "move-requirements", "next", "--file", membershipInput(t, map[string]any{"requirements": membershipSelection()}), "--json")
	if err != nil {
		t.Fatal(err)
	}
	reviewed := filepath.Join(t.TempDir(), "original-preview.json")
	if err := os.WriteFile(reviewed, []byte(output), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		output, err = runMembership(t, server, "move-requirements", "next", "--file", reviewed, "--apply", "--json")
		if err != nil {
			t.Fatalf("apply %d: %v", i, err)
		}
	}
	var receipt map[string]any
	if err := json.Unmarshal([]byte(output), &receipt); err != nil {
		t.Fatal(err)
	}
	counts := receipt["counts"].(map[string]any)
	if mutations != 2 || counts["move"] != float64(0) || counts["unchanged"] != float64(2) {
		t.Fatalf("retry receipt %v, mutation requests %d", counts, mutations)
	}
}

func TestReleaseRequirementAmbiguousNumericTargetRequiresExplicitSelection(t *testing.T) {
	for _, by := range []string{"auto", "id", "slug"} {
		t.Run(by, func(t *testing.T) {
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					fmt.Fprint(w, `{"data":{"items":[{"id":2,"system_id":4,"slug":"next","status":"planned","is_base":false},{"id":3,"system_id":4,"slug":"2","status":"planned","is_base":false}]}}`)
					return
				}
				posts++
				target := 2
				if by == "slug" {
					target = 3
				}
				if r.URL.Path != fmt.Sprintf("/api/compliance/releases/%d/requirements/preview", target) {
					t.Errorf("wrong target %s", r.URL.Path)
				}
				receipt := membershipReceipt()
				receipt["target"].(map[string]any)["id"] = target
				receipt["apply"].(map[string]any)["target_release_id"] = target
				json.NewEncoder(w).Encode(map[string]any{"data": receipt})
			}))
			defer server.Close()
			_, err := runMembership(t, server, "move-requirements", "2", "--by", by, "--file", membershipInput(t, map[string]any{"requirements": membershipSelection()}), "--json")
			if by == "auto" {
				if err == nil || !strings.Contains(err.Error(), "ambiguous") || posts != 0 {
					t.Fatalf("ambiguity not refused before operation: %v posts %d", err, posts)
				}
			} else if err != nil || posts != 1 {
				t.Fatalf("explicit %s failed: %v posts %d", by, err, posts)
			}
		})
	}
}

func TestReleaseRequirementLostMutationResponseIsNotRetried(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			listMembershipReleases(w)
			return
		}
		attempts++
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/move") {
			t.Errorf("unexpected mutation %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		conn.Close()
	}))
	defer server.Close()
	_, err := runMembership(t, server, "move-requirements", "next", "--file", membershipInput(t, membershipReceipt()), "--apply", "--json")
	if err == nil || !strings.Contains(err.Error(), "inspect membership before retrying") || attempts != 1 {
		t.Fatalf("uncertain mutation result: %v attempts %d", err, attempts)
	}
}

// Epic 334 (C1), test case 6487fdc2, criterion 64808f51 (its command-line
// clause) and scenario 303fe4bd: a selected requirement that an Epic took
// moves with that Epic, so moving it alone out of the Epic's Release answers
// 409 selected_requirement_held (contract §8). releases move-requirements, in
// preview and with --apply, prints the server's sentence with the requirement
// and the Epic, not only "requirement membership refused (HTTP 409)".
func TestC1_6487fdc2_MoveRequirementsPrintsTheHeldRequirementSentence(t *testing.T) {
	const sentence = "UR-7 Export invoices moves with the Epic Billing cutover, which took it. Move the Epic to move it."
	for _, scenario := range []struct {
		name, path string
		input      func(t *testing.T) string
		args       []string
	}{
		{"preview", "/api/compliance/releases/2/requirements/preview",
			func(t *testing.T) string {
				return membershipInput(t, map[string]any{"requirements": membershipSelection()})
			}, nil},
		{"apply", "/api/compliance/releases/2/requirements/move",
			func(t *testing.T) string { return membershipInput(t, membershipReceipt()) },
			[]string{"--apply"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var posts []string
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					listMembershipReleases(w)
					return
				}
				posts = append(posts, r.URL.Path)
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]any{
					"error":       "selected_requirement_held",
					"requirement": map[string]any{"kind": "user_requirement", "id": membershipUR, "display_ref": "UR-7", "title": "Export invoices"},
					"epic":        map[string]any{"id": 31, "title": "Billing cutover"},
					"message":     sentence,
				})
			}))
			defer s.Close()
			out, err := runMembership(t, s, append([]string{"move-requirements", "next", "--file", scenario.input(t), "--json"}, scenario.args...)...)

			// Guards: the refusal ends the command with an error after one request, never retried.
			if err == nil {
				t.Fatalf("releases move-requirements %s of a held requirement ended without an error:\n%s", scenario.name, out)
			}
			if len(posts) != 1 || posts[0] != scenario.path {
				t.Errorf("releases move-requirements %s sent %v; want one POST %s", scenario.name, posts, scenario.path)
			}
			text := out + "\n" + err.Error()
			for _, part := range []struct{ what, text string }{
				{"the server's message", sentence},
				{"the requirement's display_ref", "UR-7"},
				{"the requirement's title", "Export invoices"},
				{"the Epic's title", "Billing cutover"},
			} {
				if !strings.Contains(text, part.text) {
					t.Errorf("releases move-requirements %s does not print %s %q: %s", scenario.name, part.what, part.text, text)
				}
			}
			if strings.TrimSpace(err.Error()) == "requirement membership refused (HTTP 409)" {
				t.Errorf("releases move-requirements %s prints only the status: %v", scenario.name, err)
			}
		})
	}
}
