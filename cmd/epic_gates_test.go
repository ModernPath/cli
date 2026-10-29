package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestEpicNamedGateCommandExists(t *testing.T) {
	command, _, err := rootCmd.Find([]string{"epics", "gates", "check"})
	if err != nil || command.Name() != "check" {
		t.Fatalf("RDD readiness check must have its own command: command=%s err=%v", command.Name(), err)
	}
	if command.Flags().Lookup("phase") == nil || command.Flags().Lookup("json") == nil {
		t.Fatal("gate check must support named phases and machine-readable blockers")
	}
}

func TestEpicReviewBundlePhaseReadsOneContext(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != "GET" || r.URL.Path != "/api/work/epics/287/review-bundle" || r.URL.Query().Get("phase") != "completion-review" {
			t.Errorf("unexpected read: %s %s", r.Method, r.URL)
		}
		_, _ = w.Write([]byte(`{"data":{"epic":{"id":287},"review":{"plan":{}},"reviews":[],"readiness":{"phase":"completion-review","ready":false,"blockers":[]}}}`))
	}))
	defer server.Close()
	command := newEpicGatesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 105), nil })
	command.SetArgs([]string{"review-bundle", "287", "--phase", "completion-review", "--json"})
	var out bytes.Buffer
	command.SetOut(&out)
	if err := command.Execute(); err != nil || requests != 1 || !strings.Contains(out.String(), `"readiness"`) {
		t.Fatalf("phase read: err=%v requests=%d output=%s", err, requests, out.String())
	}

	invalid := newEpicGatesCommand(func() (*cliEnv, error) {
		t.Fatal("invalid phase must be rejected before loading the CLI environment")
		return nil, nil
	})
	invalid.SetArgs([]string{"review-bundle", "287", "--phase", "invalid", "--json"})
	invalid.SetOut(&bytes.Buffer{})
	invalid.SetErr(&bytes.Buffer{})
	if err := invalid.Execute(); err == nil || requests != 1 {
		t.Fatalf("invalid phase made a request: err=%v requests=%d", err, requests)
	}
}

// TestCase 5c45e583 (criterion bf04aa2d, Epic 348): the review bundle is a
// plain read; --since-review is gone.
func TestEpicReviewBundleRefusesSinceReviewAsAnUnknownFlagAndSendsOnlyPhase(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != "GET" || r.URL.Path != "/api/work/epics/287/review-bundle" || r.URL.RawQuery != "phase=plan-ready" {
			t.Errorf("unexpected read: %s %s", r.Method, r.URL)
		}
		_, _ = w.Write([]byte(`{"data":{"epic":{"id":287},"review":{"plan":{}},"reviews":[],"readiness":{"phase":"plan-ready","ready":false,"blockers":[]}}}`))
	}))
	defer server.Close()

	since := newEpicGatesCommand(func() (*cliEnv, error) {
		t.Fatal("--since-review must be refused before loading the CLI environment")
		return nil, nil
	})
	since.SetArgs([]string{"review-bundle", "287", "--since-review", "3a10f7b4-d083-4df6-b70c-df9cf6407238", "--json"})
	since.SetOut(&bytes.Buffer{})
	since.SetErr(&bytes.Buffer{})
	if err := since.Execute(); err == nil || !strings.Contains(err.Error(), "unknown flag: --since-review") {
		t.Fatalf("--since-review: err=%v", err)
	}

	command := newEpicGatesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 105), nil })
	command.SetArgs([]string{"review-bundle", "287", "--phase", "plan-ready", "--json"})
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&bytes.Buffer{})
	if err := command.Execute(); err != nil {
		t.Fatalf("review-bundle failed: %v", err)
	}
	var printed map[string]any
	if err := json.Unmarshal(out.Bytes(), &printed); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if requests != 1 || printed["readiness"] == nil {
		t.Fatalf("review-bundle read: requests=%d output=%s", requests, out.String())
	}
}

func TestEpicRecoveryAndDecisionCommandsRetainInput(t *testing.T) {
	for _, op := range []struct {
		name  string
		input map[string]any
	}{
		{"declare-decision", map[string]any{"question": "Which boundary?"}},
		{"recover-legacy", map[string]any{"plan": map[string]any{"body": "Exact approved scope"}, "source": "USER: recover", "reason": "Replan verification"}},
	} {
		t.Run(op.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input map[string]any
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
				}
				if r.Method != "POST" || r.URL.Path != "/api/work/epics/287/"+op.name || !reflect.DeepEqual(input, op.input) {
					t.Errorf("changed decision or recovery input: %s %s %#v", r.Method, r.URL.Path, input)
				}
				_, _ = w.Write([]byte(`{"data":{"id":"server-record"}}`))
			}))
			defer server.Close()
			input, _ := json.Marshal(op.input)
			command := newEpicGatesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 105), nil })
			command.SetArgs([]string{op.name, "287", "--file", "-", "--json"})
			command.SetIn(bytes.NewReader(input))
			var out bytes.Buffer
			command.SetOut(&out)
			if err := command.Execute(); err != nil || !strings.Contains(out.String(), "server-record") {
				t.Fatalf("command failed: %v %s", err, out.String())
			}
		})
	}
}

func TestEpicGatesHelpDescribesCurrentProof(t *testing.T) {
	if !strings.Contains(epicGatesHelp, "evidence record") || strings.Contains(epicGatesHelp, "start-execution") {
		t.Fatal("gate help must describe direct evidence recording")
	}
}

func TestEpicGateBlockersAndRegistrationContract(t *testing.T) {
	for _, operation := range []string{"check"} {
		t.Run(operation, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if operation == "check" {
					var input map[string]any
					_ = json.NewDecoder(r.Body).Decode(&input)
					if r.Method != "POST" || r.URL.Path != "/api/work/epics/287/gates/check" || input["phase"] != "completion-review" {
						t.Errorf("wrong check request: %s %s", r.Method, r.URL)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"ready": false, "skills": []string{"rdd-verify"}, "blockers": []any{map[string]any{"code": "passing_evidence_required", "message": "Criterion X lacks passing evidence"}, map[string]any{"code": "material_review_finding", "message": "Material finding Y is unresolved"}}}})
				} else {
					var input map[string]any
					_ = json.NewDecoder(r.Body).Decode(&input)
					if r.Method != "POST" || r.URL.Path != "/api/work/epics/287/executions" || input["role"] != "GREEN" {
						t.Errorf("wrong registration: %#v", input)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"run_id": "server-issued-id"}})
				}
			}))
			defer server.Close()
			command := newEpicGatesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 105), nil })
			args := []string{operation, "287", "--json"}
			if operation == "check" {
				args = append(args, "--phase", "completion-review")
			} else {
				args = append(args, "--file", "-")
				command.SetIn(strings.NewReader(`{"role":"GREEN","revision":"candidate"}`))
			}
			command.SetArgs(args)
			var out bytes.Buffer
			command.SetOut(&out)
			command.SetErr(&out)
			err := command.Execute()
			if operation == "check" {
				if err == nil || !strings.Contains(out.String(), "Criterion X") || !strings.Contains(out.String(), "Material finding Y") {
					t.Fatalf("lost blockers or success exit: %v %s", err, out.String())
				}
			} else if err != nil || !strings.Contains(out.String(), "server-issued-id") {
				t.Fatalf("registration failed: %v %s", err, out.String())
			}
		})
	}
}

func TestEpicCheckGitProvenance(t *testing.T) {
	for _, mode := range []string{"checkout", "dirty", "explicit", "unavailable", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			sha := strings.Repeat("a", 40)
			if mode == "checkout" || mode == "dirty" {
				for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-qm", "fixture"}} {
					c := exec.Command("git", args...)
					if out, err := c.CombinedOutput(); err != nil {
						t.Fatalf("git fixture: %v %s", err, out)
					}
				}
				out, err := exec.Command("git", "rev-parse", "HEAD").Output()
				if err != nil {
					t.Fatal(err)
				}
				sha = strings.TrimSpace(string(out))
				if mode == "dirty" {
					if err := os.WriteFile("changed", []byte("dirty"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var input map[string]any
				_ = json.NewDecoder(r.Body).Decode(&input)
				if r.Method != "POST" || r.URL.Path != "/api/work/epics/296/gates/check" {
					t.Errorf("wrong check transport: %s %s", r.Method, r.URL)
				}
				source := mode
				if mode == "dirty" {
					source = "checkout"
				}
				if input["git_sha_source"] != source {
					t.Errorf("source = %#v", input)
				}
				if mode == "unavailable" {
					if input["git_sha"] != nil || input["git_dirty"] != nil {
						t.Errorf("invented Git context: %#v", input)
					}
				} else {
					if input["git_sha"] != sha {
						t.Errorf("SHA = %#v", input)
					}
					if source == "checkout" {
						if input["git_dirty"] != (mode == "dirty") {
							t.Errorf("dirty = %#v", input)
						}
					} else if input["git_dirty"] != nil {
						t.Errorf("explicit SHA has invented dirty state: %#v", input)
					}
				}
				_, _ = w.Write([]byte(`{"data":{"ready":true,"blockers":[]}}`))
			}))
			defer server.Close()
			command := newEpicGatesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 105), nil })
			args := []string{"check", "296", "--json"}
			if mode == "explicit" {
				args = append(args, "--git-sha", sha)
			}
			if mode == "invalid" {
				args = append(args, "--git-sha", "not-a-sha")
			}
			command.SetArgs(args)
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			err := command.Execute()
			if mode == "invalid" {
				if err == nil || calls != 0 {
					t.Fatalf("invalid SHA reached server: calls=%d err=%v", calls, err)
				}
			} else if err != nil || calls != 1 {
				t.Fatalf("check failed: calls=%d err=%v", calls, err)
			}
		})
	}
}

// Epic 321: batched decisions. A file holding {"items":[...]} routes to the
// plural endpoint; the single-object form keeps its route and input.
func TestEpicBatchDecisionCommandsPostItemsToThePluralRouteAndPrintIdsInInputOrder(t *testing.T) {
	input := map[string]any{"items": []any{
		map[string]any{"question": "Q1"},
		map[string]any{"question": "Q2"},
	}}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var got map[string]any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		if r.Method != "POST" || r.URL.Path != "/api/work/epics/287/declare-decisions" || !reflect.DeepEqual(got, input) {
			t.Errorf("batch declare must post the unchanged items to /declare-decisions: %s %s %#v", r.Method, r.URL.Path, got)
		}
		_, _ = w.Write([]byte(`{"data":{"items":[{"index":0,"id":"11111111-1111-4111-8111-111111111111","question":"Q1"},{"index":1,"id":"22222222-2222-4222-8222-222222222222","question":"Q2"}]}}`))
	}))
	defer server.Close()
	body, _ := json.Marshal(input)
	command := newEpicGatesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 105), nil })
	command.SetArgs([]string{"declare-decision", "287", "--file", "-", "--json"})
	command.SetIn(bytes.NewReader(body))
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&out)
	if err := command.Execute(); err != nil {
		t.Fatalf("batch declare failed: %v %s", err, out.String())
	}
	if requests != 1 {
		t.Fatalf("batch declare must send exactly one request, sent %d", requests)
	}
	first := strings.Index(out.String(), "11111111-1111-4111-8111-111111111111")
	second := strings.Index(out.String(), "22222222-2222-4222-8222-222222222222")
	if first < 0 || second < 0 || first > second {
		t.Fatalf("batch declare must print the returned ids in input order: %s", out.String())
	}
}

func TestEpicBatchDecisionRefusalPrintsTheFailingItemIndex(t *testing.T) {
	const second = "33333333-3333-4333-8333-333333333333"
	input := map[string]any{"items": []any{
		map[string]any{"decision_id": "44444444-4444-4444-8444-444444444444", "answer": "Keep", "source": "USER: first"},
		map[string]any{"decision_id": second, "answer": "Drop", "source": "agent"},
	}}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var got map[string]any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		if r.Method != "POST" || r.URL.Path != "/api/work/epics/287/answer-decisions" || !reflect.DeepEqual(got, input) {
			t.Errorf("batch answer must post the unchanged items to /answer-decisions: %s %s %#v", r.Method, r.URL.Path, got)
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "invalid_human_decision", "message": "Use a declared decision ID, the human answer and its USER: source."},
			"index": 1,
			"item":  map[string]any{"decision_id": second},
		})
	}))
	defer server.Close()
	body, _ := json.Marshal(input)
	command := newEpicGatesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 105), nil })
	command.SetArgs([]string{"decision", "287", "--file", "-", "--json"})
	command.SetIn(bytes.NewReader(body))
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&out)
	err := command.Execute()
	if err == nil {
		t.Fatalf("a refused batch must exit nonzero: %s", out.String())
	}
	if requests != 1 {
		t.Fatalf("batch answer must send exactly one request, sent %d", requests)
	}
	want := "invalid_human_decision — item 1 (decision " + second + ") — Use a declared decision ID"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("refusal must name the code and the failing item:\nwant substring: %s\ngot: %v", want, err)
	}
}

func TestEpicSingleObjectDecisionCommandsRetainTheirInputAndRoute(t *testing.T) {
	for _, op := range []struct {
		name, suffix string
		input        map[string]any
	}{
		{"declare-decision", "/declare-decision", map[string]any{"question": "Which boundary?"}},
		{"decision", "/answer-decision", map[string]any{"decision_id": "55555555-5555-4555-8555-555555555555", "answer": "Keep", "source": "USER: keep"}},
	} {
		t.Run(op.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Error(err)
				}
				if r.Method != "POST" || r.URL.Path != "/api/work/epics/287"+op.suffix || !reflect.DeepEqual(got, op.input) {
					t.Errorf("single-object decision input or route changed: %s %s %#v", r.Method, r.URL.Path, got)
				}
				_, _ = w.Write([]byte(`{"data":{"id":"server-record"}}`))
			}))
			defer server.Close()
			body, _ := json.Marshal(op.input)
			command := newEpicGatesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 105), nil })
			command.SetArgs([]string{op.name, "287", "--file", "-", "--json"})
			command.SetIn(bytes.NewReader(body))
			var out bytes.Buffer
			command.SetOut(&out)
			if err := command.Execute(); err != nil || !strings.Contains(out.String(), "server-record") {
				t.Fatalf("single-object command failed: %v %s", err, out.String())
			}
		})
	}
}
