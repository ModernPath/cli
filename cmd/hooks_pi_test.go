package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func selectPiOnly(t *testing.T) agentConfig {
	t.Helper()
	hooksPi = true
	t.Cleanup(func() { hooksPi = false })
	return hookAgents["pi"]
}

// REQ-CROSS-395: store-backed installation retires only Pi's file-sync family.
func TestPiInstallMergesSkillsWithoutClobberingSettings(t *testing.T) {
	chdirTemp(t)
	agent := hookAgents["pi"]
	if err := os.MkdirAll(filepath.Dir(agent.configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := `{"theme":"mono","skills":["./mine"]}`
	if err := os.WriteFile(agent.configPath, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}
	got := settingsText(t, agent.configPath)
	if !strings.Contains(got, `"theme"`) || !strings.Contains(got, "./mine") || !strings.Contains(got, piSkillsPath) {
		t.Fatalf("Pi settings merge lost user keys or the skills pointer: %s", got)
	}
}

func TestPiInstallRefusesAMalformedSettingsFile(t *testing.T) {
	chdirTemp(t)
	agent := hookAgents["pi"]
	if err := os.MkdirAll(filepath.Dir(agent.configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	const broken = `{"skills": [}`
	if err := os.WriteFile(agent.configPath, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installForAgent(agent); err == nil {
		t.Fatal("malformed .pi/settings.json must not be treated as empty")
	}
	if got := settingsText(t, agent.configPath); got != broken {
		t.Fatalf("installer rewrote a file it could not parse:\n%s", got)
	}
}

func TestDetectInstalledAgentsFindsPi(t *testing.T) {
	chdirTemp(t)
	if err := os.Mkdir(".pi", 0o755); err != nil {
		t.Fatal(err)
	}
	got := detectInstalledAgents()
	found := false
	for _, key := range got {
		if key == "pi" {
			found = true
		}
	}
	if !found {
		t.Fatalf("detectInstalledAgents missed .pi: %v", got)
	}
}

func TestPiSkillsMergeIsIdempotent(t *testing.T) {
	chdirTemp(t)
	agent := hookAgents["pi"]
	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}
	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}
	raw := settingsText(t, agent.configPath)
	var settings map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		t.Fatal(err)
	}
	skills, _ := settings["skills"].([]interface{})
	count := 0
	for _, entry := range skills {
		text, _ := entry.(string)
		if strings.Contains(text, piSkillsMarker) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("reinstall duplicated the skills pointer (%d): %s", count, raw)
	}
}

// SR-1598: execute the actual generated source; Pi and model services are not test dependencies.
func TestPiGeneratedExtension(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node.js is required to verify the generated Pi extension (Pi itself is not required)")
	}
	extension := filepath.Join(t.TempDir(), "modernpath.mjs")
	if err := os.WriteFile(extension, []byte(piExtensionSource()), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"success", "empty", "malformed", "nonzero", "null-status", "spawn-error", "timeout", "throws", "custom"} {
		t.Run(scenario, func(t *testing.T) {
			out, err := exec.Command(node, "testdata/pi-extension-harness.mjs", extension, scenario).CombinedOutput()
			if err != nil {
				t.Fatalf("generated extension contract: %v\n%s", err, out)
			}
		})
	}
}

// SR-1599: all diagnostic surfaces must agree after real installation.
func TestPiInstalledContextDiagnostics(t *testing.T) {
	for _, settings := range []bool{true, false} {
		t.Run(fmt.Sprintf("settings=%t", settings), func(t *testing.T) {
			chdirTemp(t)
			agent := selectPiOnly(t)
			pathDir := t.TempDir()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(executable, filepath.Join(pathDir, "modernpath")); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", pathDir)
			if err := installPi(agent); err != nil {
				t.Fatal(err)
			}
			if !settings {
				if err := os.Remove(agent.configPath); err != nil {
					t.Fatal(err)
				}
			}
			t.Run("state", func(t *testing.T) {
				if got := contextFamilyState(agent); got != hookStateConfigured {
					t.Fatalf("fresh Pi install state = %s, want configured", got)
				}
			})
			t.Run("status", func(t *testing.T) {
				stdout, stderr, err := runAskStreams(t, "hooks", "status")
				if err != nil {
					t.Fatalf("status: %v %s", err, stderr)
				}
				got := stdout + stderr
				if !strings.Contains(got, "Pi: configured") || !strings.Contains(got, "trusted") {
					t.Fatalf("status must recognize Pi and explain trust:\n%s", got)
				}
			})
			t.Run("doctor-text", func(t *testing.T) {
				output := captureCLIOutput(t)
				cmd := hooksTestCmd()
				cmd.Flags().Bool("json", false, "")
				addRecordReadFlags(cmd)
				if err := runHooksDoctor(cmd, nil); err != nil {
					t.Fatal(err)
				}
				got := output()
				if !strings.Contains(got, "Pi: context hook wired") || !strings.Contains(got, "Pi: project .pi/ loads only after the project is trusted") {
					t.Fatalf("doctor must recognize Pi independently of optional settings and explain trust:\n%s", got)
				}
			})
			t.Run("doctor-json", func(t *testing.T) {
				stdout, stderr, err := runAskStreams(t, "hooks", "doctor", "--json")
				if err != nil {
					t.Fatalf("doctor JSON: %v %s", err, stderr)
				}
				var report struct {
					Agents []struct {
						Agent   string `json:"agent"`
						Context string `json:"context_hook"`
					} `json:"agents"`
				}
				if err := json.Unmarshal([]byte(stdout), &report); err != nil {
					t.Fatal(err)
				}
				for _, row := range report.Agents {
					if row.Agent == "Pi" {
						if row.Context != string(hookStateConfigured) {
							t.Fatalf("Pi JSON context = %s, want configured", row.Context)
						}
						return
					}
				}
				t.Fatal("Pi missing from doctor JSON")
			})
		})
	}
}

func TestPiContextRecognitionPreservesUnmanagedExtensions(t *testing.T) {
	chdirTemp(t)
	agent := hookAgents["pi"]
	if got := contextFamilyState(agent); got != hookStateAbsent {
		t.Fatalf("missing extension = %s", got)
	}
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"export default function () {}", piManagedHeader + "\nexport default function () {}", strings.Replace(piExtensionSource(), piManagedHeader, "// user-owned", 1)} {
		if err := os.WriteFile(piExtensionPath(agent), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := contextFamilyState(agent); got == hookStateConfigured {
			t.Fatalf("unrelated or unmanaged extension reported configured: %s", source)
		}
		if removed, err := uninstallPiContext(agent); err != nil || removed {
			t.Fatalf("uninstall removed unmanaged extension: %v %t", err, removed)
		}
		if got := settingsText(t, piExtensionPath(agent)); got != source {
			t.Fatal("uninstall changed unmanaged extension")
		}
	}
}

func TestPiUninstallRemovesGeneratedContextOnly(t *testing.T) {
	chdirTemp(t)
	agent := hookAgents["pi"]
	if err := os.MkdirAll(filepath.Dir(agent.configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agent.configPath, []byte(`{"theme":"mono","skills":["./mine"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installPi(agent); err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(agent.hooksDir, "custom.ts")
	if err := os.WriteFile(custom, []byte("// user extension"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := uninstallPi(agent); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(piExtensionPath(agent)); !os.IsNotExist(err) {
		t.Fatalf("generated context extension remains: %v", err)
	}
	if got := settingsText(t, custom); got != "// user extension" {
		t.Fatal("uninstall changed unrelated extension")
	}
	if got := settingsText(t, agent.configPath); !strings.Contains(got, "mono") || !strings.Contains(got, "./mine") || strings.Contains(got, piSkillsPath) {
		t.Fatalf("uninstall failed to preserve user settings: %s", got)
	}
}
