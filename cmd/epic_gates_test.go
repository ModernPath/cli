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

func TestEpicExecutionHelpNamesExecutableFinalization(t *testing.T) {
	if strings.Contains(epicGatesHelp, "evidence report") || !strings.Contains(epicGatesHelp, "evidence record --file") || !strings.Contains(epicGatesHelp, `"status":"complete"`) || !strings.Contains(epicGatesHelp, `"id":"<returned run_id>"`) {
		t.Fatal("execution help must show executable finalization of the registered run")
	}
}

func TestEpicGateBlockersAndRegistrationContract(t *testing.T) {
	for _, operation := range []string{"check", "start-execution"} {
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
