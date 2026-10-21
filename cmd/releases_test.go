package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestReleaseActivationCommandIsAvailable(t *testing.T) {
	command, _, err := rootCmd.Find([]string{"releases", "activate"})
	if err != nil || command.Name() != "activate" {
		t.Fatalf("release activation command missing: %v", err)
	}
}

func TestNativeReleaseActivationHTTPContract(t *testing.T) {
	for _, scenario := range []struct {
		name, response string
		status         int
		wantError      bool
	}{
		{"success", `{"data":{"release":{"id":2,"system_id":4,"status":"active","slug":"next"},"closed_releases":[]}}`, 200, false},
		{"foreign receipt", `{"data":{"release":{"id":2,"system_id":5,"status":"active"}}}`, 200, true},
		{"wrong target", `{"data":{"release":{"id":7,"system_id":4,"status":"active"}}}`, 200, true},
		{"not active", `{"data":{"release":{"id":2,"system_id":4,"status":"planned"}}}`, 200, true},
		{"PIN refusal", `{"error":"invalid_pin"}`, 403, true},
		{"stale consent", `{"error":"incumbent_changed"}`, 409, true},
		{"retired endpoint", `{"error":"Not Found"}`, 404, true},
		{"secret echo", `{"error":"input 482913 was rejected","pin":"482913"}`, 500, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					if r.URL.Path != "/api/compliance/releases" || r.URL.Query().Get("system_id") != "4" || r.ContentLength > 0 {
						t.Errorf("unscoped list: %s", r.URL)
					}
					fmt.Fprint(w, `{"data":{"items":[{"id":1,"system_id":4,"slug":"current","status":"active"},{"id":2,"system_id":4,"slug":"next","status":"planned"}]}}`)
					return
				}
				posts++
				if r.Method != "POST" || r.URL.Path != "/api/compliance/releases/2/activate" {
					t.Errorf("activation request %s %s", r.Method, r.URL)
				}
				var payload map[string]any
				json.NewDecoder(r.Body).Decode(&payload)
				if payload["system_id"] != float64(4) || payload["expected_current_release_id"] != float64(1) || payload["close_current"] != true || payload["pin"] != "482913" || payload["reason"] != "cutover" {
					t.Errorf("incorrect activation request fields")
				}
				w.WriteHeader(scenario.status)
				fmt.Fprint(w, scenario.response)
			}))
			defer server.Close()
			cmd := newReleasesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
			cmd.SetArgs([]string{"activate", "next", "--close-current", "--pin-stdin", "--reason", "cutover", "--json"})
			cmd.SetIn(strings.NewReader("482913\n"))
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			err := cmd.Execute()
			if (err != nil) != scenario.wantError {
				t.Fatalf("error %v", err)
			}
			if posts != 1 {
				t.Fatalf("posts %d", posts)
			}
			if strings.Contains(out.String(), "482913") || (err != nil && strings.Contains(err.Error(), "482913")) {
				t.Fatal("PIN leaked")
			}
		})
	}
}

func TestReleaseActivationRequiresConsentBeforeReadingPIN(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Fatal("write without consent")
		}
		fmt.Fprint(w, `{"data":{"items":[{"id":1,"system_id":4,"status":"active"},{"id":2,"system_id":4,"slug":"next","status":"planned"}]}}`)
	}))
	defer server.Close()
	c := newReleasesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	c.SetArgs([]string{"activate", "next", "--pin-stdin"})
	c.SetIn(strings.NewReader("482913"))
	if err := c.Execute(); err == nil || !strings.Contains(err.Error(), "--close-current") {
		t.Fatalf("error %v", err)
	}
}

func TestReleasePinStatusUsesBodylessGET(t *testing.T) {
	for _, hasPIN := range []bool{false, true} {
		t.Run(fmt.Sprintf("has_pin=%t", hasPIN), func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodGet || r.URL.RequestURI() != "/api/compliance/pin" {
					t.Errorf("unexpected PIN status request: %s %s", r.Method, r.URL)
				}
				body, err := io.ReadAll(r.Body)
				if err != nil || len(body) != 0 {
					t.Errorf("PIN status GET must have no body: %q, error %v", body, err)
				}
				fmt.Fprintf(w, `{"data":{"has_pin":%t}}`, hasPIN)
			}))
			defer server.Close()
			c := newReleasesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
			c.SetArgs([]string{"pin", "status", "--json"})
			var out bytes.Buffer
			c.SetOut(&out)
			c.SilenceErrors = true
			c.SilenceUsage = true
			if err := c.Execute(); err != nil {
				t.Fatalf("PIN status failed: %v", err)
			}
			if requests != 1 {
				t.Fatalf("expected one PIN status request, got %d", requests)
			}
			var result map[string]any
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatalf("invalid PIN status output: %v", err)
			}
			if result["has_pin"] != hasPIN {
				t.Fatalf("has_pin = %v, want %t", result["has_pin"], hasPIN)
			}
		})
	}
}

func TestReleasePinSetUsesNativeEndpointAndNeverEchoesPIN(t *testing.T) {
	for _, pin := range []string{"482913", "123", "1234567", "abcd"} {
		t.Run(pin, func(t *testing.T) {
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writes++
				if r.Method != "POST" || r.URL.Path != "/api/compliance/pin" {
					t.Errorf("wrong PIN endpoint")
				}
				var payload map[string]any
				json.NewDecoder(r.Body).Decode(&payload)
				if payload["pin"] != pin {
					t.Error("PIN not transmitted correctly")
				}
				fmt.Fprint(w, `{"data":{"has_pin":true}}`)
			}))
			defer server.Close()
			c := newReleasesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
			c.SetArgs([]string{"pin", "set", "--pin-stdin", "--json"})
			c.SetIn(strings.NewReader(pin + "\n"))
			var out bytes.Buffer
			c.SetOut(&out)
			c.SetErr(&out)
			c.SilenceErrors = true
			err := c.Execute()
			if pin == "482913" {
				if err != nil || writes != 1 {
					t.Fatalf("error %v writes %d", err, writes)
				}
			} else if err == nil || writes != 0 {
				t.Fatal("invalid PIN accepted")
			}
			if strings.Contains(out.String(), pin) {
				t.Fatal("PIN echoed")
			}
		})
	}
}

func TestLegacyReleaseCommandsAreThinNativeCompatibility(t *testing.T) {
	for _, args := range [][]string{{"factory", "release", "activate"}, {"factory", "pin", "set"}} {
		c, _, err := rootCmd.Find(args)
		if err != nil || c.Name() != args[len(args)-1] {
			t.Fatalf("legacy command %v missing", args)
		}
	}
}

func TestReleaseSlugRefusalsDoNotActivateOrConsumePIN(t *testing.T) {
	for _, items := range []string{`[]`, `[{"id":2,"system_id":4,"slug":"next","status":"planned"},{"id":3,"system_id":4,"slug":"next","status":"planned"}]`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				t.Error("refused resolution performed a write")
			}
			fmt.Fprintf(w, `{"data":{"items":%s}}`, items)
		}))
		input := strings.NewReader("482913\n")
		c := newReleasesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
		c.SetArgs([]string{"activate", "next", "--pin-stdin"})
		c.SetIn(input)
		if err := c.Execute(); err == nil {
			t.Fatal("invalid resolution accepted")
		}
		if input.Len() != 7 {
			t.Fatal("refused resolution consumed the PIN")
		}
		server.Close()
	}
}

func TestReleaseNumericSlugResolution(t *testing.T) {
	for _, tc := range []struct {
		name, selector string
		flags          []string
		items          string
		wantID         int
	}{
		{"numeric slug", "2026", nil, `[{"id":7,"system_id":4,"slug":"2026","status":"planned"}]`, 7},
		{"conflicting ID and slug", "2026", nil, `[{"id":7,"system_id":4,"slug":"2026","status":"planned"},{"id":2026,"system_id":4,"slug":"other","status":"planned"}]`, 0},
		{"explicit slug", "2026", []string{"--by", "slug"}, `[{"id":7,"system_id":4,"slug":"2026","status":"planned"},{"id":2026,"system_id":4,"slug":"other","status":"planned"}]`, 7},
		{"explicit ID", "2026", []string{"--by", "id"}, `[{"id":7,"system_id":4,"slug":"2026","status":"planned"},{"id":2026,"system_id":4,"slug":"other","status":"planned"}]`, 2026},
		{"same row matches both", "7", nil, `[{"id":7,"system_id":4,"slug":"7","status":"planned"}]`, 7},
		{"invalid selection mode", "2026", []string{"--by", "invalid"}, `[]`, 0},
		{"invalid explicit ID", "next", []string{"--by", "id"}, `[]`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					fmt.Fprintf(w, `{"data":{"items":%s}}`, tc.items)
					return
				}
				writes++
				if tc.wantID == 0 || r.URL.Path != fmt.Sprintf("/api/compliance/releases/%d/activate", tc.wantID) {
					t.Errorf("unexpected activation: %s", r.URL.Path)
				}
				fmt.Fprintf(w, `{"data":{"release":{"id":%d,"system_id":4,"status":"active"}}}`, tc.wantID)
			}))
			defer server.Close()
			input := strings.NewReader("482913\n")
			c := newReleasesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
			c.SetArgs(append([]string{"activate", tc.selector, "--pin-stdin", "--close-current"}, tc.flags...))
			c.SetIn(input)
			err := c.Execute()
			if tc.wantID == 0 {
				if err == nil || writes != 0 || input.Len() != 7 {
					t.Fatalf("resolution must refuse before write/PIN: err=%v writes=%d remaining=%d", err, writes, input.Len())
				}
			} else if err != nil || writes != 1 {
				t.Fatalf("expected activation of %d: err=%v writes=%d", tc.wantID, err, writes)
			}
		})
	}
}

func TestReleasePINHiddenTerminal(t *testing.T) {
	if os.Getenv("MP_RELEASE_PIN_PTY_CHILD") == "1" {
		c := &cobra.Command{Use: "pin"}
		c.Flags().Bool("pin-stdin", false, "")
		pin, err := readReleasePIN(c, true)
		if err != nil || pin != "482913" {
			t.Fatalf("hidden PIN confirmation failed: %v", err)
		}
		fmt.Println("PIN accepted")
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX pseudo-terminal test")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 required for POSIX pseudo-terminal test")
	}
	script := `import os,pty,select,subprocess,sys,time
master,slave=pty.openpty()
env=dict(os.environ,MP_RELEASE_PIN_PTY_CHILD="1")
p=subprocess.Popen([sys.argv[1],"-test.run=^TestReleasePINHiddenTerminal$"],stdin=slave,stdout=slave,stderr=slave,env=env)
os.close(slave)
seen=b"";sent=0;deadline=time.monotonic()+10
try:
 while time.monotonic()<deadline:
  ready,_,_=select.select([master],[],[],0.1)
  if ready:
   try: chunk=os.read(master,4096)
   except OSError: break
   if not chunk: break
   seen+=chunk
   if sent==0 and b"Compliance PIN: " in seen:
    os.write(master,b"482913\n");sent=1
   elif sent==1 and b"Confirm PIN: " in seen:
    os.write(master,b"482913\n");sent=2
  if p.poll() is not None: break
 if p.poll() is None: p.kill()
 assert p.wait()==0, "hidden prompt child failed"
 assert b"482913" not in seen, "terminal echoed the PIN"
 assert sent==2 and b"PIN accepted" in seen, "PIN confirmation did not complete"
finally:
 os.close(master)
 if p.poll() is None: p.kill();p.wait()
`
	command := exec.Command(python, "-c", script, os.Args[0])
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pseudo-terminal check failed: %s", out)
	}
}
