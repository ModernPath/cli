package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const standaloneRequirementID = "11111111-1111-4111-8111-111111111111"
const standaloneProposalID = "22222222-2222-4222-8222-222222222222"
const standaloneVersionID = "33333333-3333-4333-8333-333333333333"

// d96f82ff-a106-4745-8a3f-3430bf4124c9 — CLI transport portion, real server journey is separate.
func TestStandaloneProposalCompletionTransport(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload["proposal_id"] != standaloneProposalID || payload["proposal_generation"] != float64(2) || payload["no_user_requirement_reason"] != "Internal repair" {
			t.Errorf("payload=%#v", payload)
		}
		fmt.Fprintf(w, `{"data":{"id":%q,"requirement_kind":"system_requirement","system_id":4,"display_ref":"SR-3-8","title":"Changed","revision":8,"work_status":"DONE","result":"completed","version_id":%q,"proposal_id":%q,"proposal_generation":2,"approved_by_id":1,"approved_at":"2026-11-16T12:00:00Z","reason":"Approved"}}`, standaloneRequirementID, standaloneVersionID, standaloneProposalID)
	}))
	defer server.Close()
	cmd := newRequirementUpdateCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	cmd.SetArgs([]string{standaloneRequirementID, "--kind", "system", "--file", "-", "--reason", "Approved", "--json"})
	cmd.SetIn(strings.NewReader(fmt.Sprintf(`{"work_status":"DONE","source":"USER:approval","proposal_id":%q,"proposal_generation":2,"no_user_requirement_reason":"Internal repair"}`, standaloneProposalID)))
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("selected proposal completion should reach HTTP and accept its exact receipt: %v", err)
	}
	if requests != 1 || !strings.Contains(out.String(), standaloneVersionID) {
		t.Fatalf("requests=%d output=%s", requests, out.String())
	}
}

// d204d3cf-7826-4464-a178-823b11924518 — scoped raw CLI read and receipt identity.
func TestStandaloneVersionReadTransport(t *testing.T) {
	for _, wrong := range []string{"", "version", "requirement", "kind", "system"} {
		t.Run(wrong, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/v1/systems/4/requirements/system/"+standaloneRequirementID+"/versions/"+standaloneVersionID || r.URL.RawQuery != "" {
					t.Errorf("request=%s %s", r.Method, r.URL)
				}
				data := map[string]any{"id": standaloneVersionID, "requirement_id": standaloneRequirementID, "requirement_kind": "system_requirement", "system_id": 4, "content": map[string]any{"title": "Frozen"}, "criteria": []any{}, "traces": []any{map[string]any{"raw_endpoint": "unchanged"}}}
				switch wrong {
				case "version":
					data["id"] = standaloneProposalID
				case "requirement":
					data["requirement_id"] = standaloneProposalID
				case "kind":
					data["requirement_kind"] = "user_requirement"
				case "system":
					data["system_id"] = 5
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
			}))
			defer server.Close()
			c := newRequirementGetCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
			c.SetArgs([]string{standaloneRequirementID, "--kind", "system", "--version", standaloneVersionID, "--json"})
			var out bytes.Buffer
			c.SetOut(&out)
			err := c.Execute()
			if wrong != "" {
				if err == nil || strings.Contains(out.String(), "raw_endpoint") {
					t.Fatalf("wrong identity accepted: %v %s", err, out.String())
				}
			} else if err != nil || !strings.Contains(out.String(), "raw_endpoint") {
				t.Fatalf("raw version missing: %v %s", err, out.String())
			}
		})
	}
}

// d96f82ff-a106-4745-8a3f-3430bf4124c9 — current discovery has no mutation selectors.
func TestStandaloneProposalSaveDiscoverCancel(t *testing.T) {
	for _, op := range []string{"save", "get", "cancel"} {
		t.Run(op, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				want := map[string]string{"save": "PUT", "get": "GET", "cancel": "DELETE"}[op]
				if r.Method != want || r.URL.Path != "/api/v1/systems/4/requirements/system/"+standaloneRequirementID+"/proposal" || r.URL.RawQuery != "" {
					t.Errorf("request=%s %s", r.Method, r.URL)
				}
				if op == "get" {
					var b bytes.Buffer
					_, _ = b.ReadFrom(r.Body)
					if b.Len() != 0 {
						t.Error("discovery sent mutation body")
					}
				} else {
					var p map[string]any
					_ = json.NewDecoder(r.Body).Decode(&p)
					if p["reason"] != "Prepare repair" {
						t.Errorf("payload=%#v", p)
					}
				}
				result := "created"
				if op == "get" {
					result = "current"
				}
				if op == "cancel" {
					result = "cancelled"
				}
				fmt.Fprintf(w, `{"data":{"requirement_id":%q,"requirement_kind":"system_requirement","system_id":4,"proposal_id":%q,"proposal_generation":1,"baseline_revision":7,"content":{"title":"Proposed"},"criteria":[],"traces":[],"remove_traces":[],"result":%q}}`, standaloneRequirementID, standaloneProposalID, result)
			}))
			defer server.Close()
			c := newRequirementProposalCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
			args := []string{op, standaloneRequirementID, "--kind", "system", "--json"}
			if op != "get" {
				args = append(args, "--file", "-", "--reason", "Prepare repair")
				input := `{"expected_revision":7,"expected_generation":0,"content":{"title":"Proposed"},"criteria":[],"traces":[],"remove_traces":[]}`
				if op == "cancel" {
					input = fmt.Sprintf(`{"proposal_id":%q,"expected_generation":1}`, standaloneProposalID)
				}
				c.SetIn(strings.NewReader(input))
			}
			c.SetArgs(args)
			var out bytes.Buffer
			c.SetOut(&out)
			if err := c.Execute(); err != nil {
				t.Fatal(err)
			}
			if requests != 1 || !strings.Contains(out.String(), standaloneProposalID) {
				t.Fatalf("requests=%d output=%s", requests, out.String())
			}
		})
	}
}

// d3e7b364-441b-477e-9046-a82f5f0eb702 — named removal CLI transport portion.
func TestStandaloneTraceRemovalTransport(t *testing.T) {
	for _, result := range []string{"removed", "unchanged", "wrong"} {
		t.Run(result, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/api/v1/systems/4/requirements/system/"+standaloneRequirementID+"/traces/"+standaloneVersionID+"/remove" {
					t.Errorf("request=%s %s", r.Method, r.URL)
				}
				var p map[string]any
				_ = json.NewDecoder(r.Body).Decode(&p)
				if len(p) != 1 || p["reason"] != "Replacement exists" {
					t.Errorf("payload=%#v", p)
				}
				id := standaloneVersionID
				if result == "wrong" {
					id = standaloneProposalID
				}
				fmt.Fprintf(w, `{"data":{"trace_id":%q,"requirement_id":%q,"requirement_kind":"system_requirement","system_id":4,"result":%q}}`, id, standaloneRequirementID, result)
			}))
			defer server.Close()
			c := newTraceCommands(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
			c.SetArgs([]string{"remove", standaloneVersionID, "--requirement", standaloneRequirementID, "--kind", "system", "--reason", "Replacement exists", "--json"})
			var out bytes.Buffer
			c.SetOut(&out)
			err := c.Execute()
			if (err != nil) != (result == "wrong") {
				t.Fatalf("err=%v output=%s", err, out.String())
			}
		})
	}
}

func TestStandaloneStrictInputs(t *testing.T) {
	for _, input := range []string{
		`{"work_status":"DONE","proposal_generation":1}`,
		`{"work_status":"DONE","proposal_id":"22222222-2222-4222-8222-222222222222"}`,
		`{"work_status":"DONE","proposal_id":"22222222-2222-4222-8222-222222222222","proposal_generation":0}`,
		`{"work_status":"DONE","no_user_requirement_reason":"  "}`,
		`{"work_status":"DONE","no_user_requirement_reason":"Repair","scenario_id":"11111111-1111-4111-8111-111111111111"}`,
		`{"work_status":"DONE","user_requirement_id":"11111111-1111-4111-8111-111111111111"}`,
		`{"work_status":"DONE","title":"Must save first"}`,
		`{"work_status":"DONE","criteria":[]}`,
		`{"title":"Draft","proposal_generation":1}`,
	} {
		t.Run(input, func(t *testing.T) {
			p, err := decodeObject(strings.NewReader(input))
			if err != nil {
				t.Fatal(err)
			}
			if err := validateRequirementUpdate(p, "system", p["work_status"] == "DONE"); err == nil {
				t.Fatalf("invalid completion accepted: %s", input)
			}
		})
	}
	for _, input := range []string{
		`{"expected_generation":0}`,
		`{"expected_revision":1,"expected_generation":1}`,
		`{"expected_revision":1,"expected_generation":0,"work_status":"DONE"}`,
		`{"expected_revision":1,"expected_generation":0,"criteria":[{"status":"superseded"}]}`,
		`{"expected_revision":1,"expected_generation":0,"remove_traces":["invented"]}`,
		`{"expected_revision":1,"expected_generation":0,"content":{"source":"USER:approval"}}`,
	} {
		p, err := decodeObject(strings.NewReader(input))
		if err != nil {
			t.Fatal(err)
		}
		if err := validateProposalInput(p, "system", "save"); err == nil {
			t.Fatalf("invalid proposal accepted: %s", input)
		}
	}
}

func TestStandaloneEvidenceSelectors(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(fmt.Sprint(batch), func(t *testing.T) {
			item := fmt.Sprintf(`{"criterion_id":%q,"proposal_id":%q,"proposal_generation":2,"tests":[]}`, standaloneRequirementID, standaloneProposalID)
			input := item
			reply := fmt.Sprintf(`{"data":{"proposal_id":%q,"proposal_generation":2,"results":[],"traces":[]}}`, standaloneProposalID)
			if batch {
				input = `{"items":[` + item + `]}`
				reply = fmt.Sprintf(`{"data":{"items":[{"proposal_id":%q,"proposal_generation":2,"results":[],"traces":[]}]}}`, standaloneProposalID)
			}
			var calls []batchedCall
			server := batchedServer(t, 200, reply, &calls)
			if _, err := runBatchedEvidenceRecord(t, server, input); err != nil {
				t.Fatal(err)
			}
			if len(calls) != 1 {
				t.Fatalf("calls=%v", calls)
			}
			var badCalls []batchedCall
			bad := batchedServer(t, 200, `{"data":{"results":[]}}`, &badCalls)
			if _, err := runBatchedEvidenceRecord(t, bad, input); err == nil {
				t.Error("missing selected proposal receipt accepted")
			}
		})
	}
}

func TestStandaloneProposalReceiptRejectsMismatch(t *testing.T) {
	receipt := map[string]any{"requirement_id": standaloneRequirementID, "requirement_kind": "system_requirement", "system_id": 4, "proposal_id": standaloneProposalID, "proposal_generation": 2, "baseline_revision": 7, "content": map[string]any{}, "criteria": []any{}, "traces": []any{}, "remove_traces": []any{}, "result": "updated"}
	input := map[string]any{"expected_revision": 7, "expected_generation": 1, "proposal_id": standaloneProposalID}
	if err := validateProposalReceipt(receipt, "system", standaloneRequirementID, 4, input, "save"); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"requirement_id", "requirement_kind", "system_id", "proposal_id", "proposal_generation", "baseline_revision", "content", "criteria", "traces", "remove_traces", "result"} {
		copy := map[string]any{}
		for k, v := range receipt {
			copy[k] = v
		}
		copy[field] = nil
		if err := validateProposalReceipt(copy, "system", standaloneRequirementID, 4, input, "save"); err == nil {
			t.Errorf("receipt missing %s accepted", field)
		}
	}
}

func TestStandaloneEvidenceLatestContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/sync/evidence/latest" || r.URL.Query().Get("proposal_id") != standaloneProposalID || r.URL.Query().Get("proposal_generation") != "2" {
			t.Errorf("request=%s %s", r.Method, r.URL)
		}
		fmt.Fprint(w, `{"data":{"targets":[{"criterion_id":"accepted","writable":false,"stored_outcomes":[{"result":"pass","writable":false}]}],"next_cursor":null}}`)
	}))
	defer server.Close()
	c := newEvidenceCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	c.SetArgs([]string{"latest", "--proposal-id", standaloneProposalID, "--proposal-generation", "2", "--json"})
	var out bytes.Buffer
	c.SetOut(&out)
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"writable": false`) || !strings.Contains(out.String(), "stored_outcomes") {
		t.Fatalf("proposal proof metadata dropped: %s", out.String())
	}
}
