package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fatih/color"
)

// REQ-CROSS-396: diagnostics distinguish expected sync retirement from hooks
// that remain configured, without hiding broken configuration or other families.
func TestHookDiagnosticsRespectProcessStore(t *testing.T) {
	for _, command := range []string{"status", "doctor"} {
		for _, key := range []string{"claude", "codex", "pi"} {
			for _, fixture := range []struct {
				name   string
				state  string
				store  bool
				nested bool
			}{
				{name: "store installed", state: "absent", store: true},
				{name: "store after flip", state: "configured", store: true},
				{name: "store legacy", state: "legacy", store: true},
				{name: "store partial", state: "partial", store: true},
				{name: "store invalid", state: "invalid config", store: true},
				{name: "nested store installed", state: "absent", store: true, nested: true},
				{name: "file absent", state: "absent"},
				{name: "file configured", state: "configured"},
			} {
				if key == "pi" && fixture.state == "partial" {
					continue // Pi has one extension, not a family of JSON event entries.
				}
				t.Run(command+"/"+key+"/"+fixture.name, func(t *testing.T) {
					root := chdirTemp(t)
					if err := os.Mkdir(filepath.Join(root, ".modernpath"), 0o755); err != nil {
						t.Fatal(err)
					}
					if fixture.nested {
						nested := filepath.Join(root, "src")
						if err := os.Mkdir(nested, 0o755); err != nil {
							t.Fatal(err)
						}
						if err := os.Chdir(nested); err != nil {
							t.Fatal(err)
						}
					}
					agent := hookAgents[key]
					if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
						t.Fatal(err)
					}
					write := func(path, contents string) {
						t.Helper()
						if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
							t.Fatal(err)
						}
					}
					write(agent.configPath, `{}`)
					// Installed fixtures exercise the actual installer. The flip occurs
					// afterwards so its configured sync remains for diagnostics to find.
					if fixture.store && fixture.state != "configured" {
						mustWriteStoreBackedMarker(t, root)
					}
					switch fixture.state {
					case "configured":
						if err := installForAgent(agent); err != nil {
							t.Fatal(err)
						}
						if fixture.store {
							mustWriteStoreBackedMarker(t, root)
						}
					case "absent":
						if fixture.store {
							if err := installForAgent(agent); err != nil {
								t.Fatal(err)
							}
						}
					case "legacy":
						if key == "pi" {
							write(piExtensionPath(agent), "// foreign extension\n// "+syncHookMarker+"\n")
						} else {
							write(agent.configPath, `{"hooks":{"Stop":[{"hooks":[{"command":"sh modernpath-sync.sh"}]}]}}`)
						}
					case "partial":
						write(agent.configPath, `{"hooks":{"Stop":[{"hooks":[{"command":"modernpath factory sync --if-quiescent --trigger Stop"}]}]}}`)
					case "invalid config":
						if key == "pi" {
							// A directory is unreadable as extension source even as root.
							if err := os.Mkdir(piExtensionPath(agent), 0o755); err != nil {
								t.Fatal(err)
							}
						} else {
							write(agent.configPath, `not json`)
						}
					}

					// Doctor's binary preflight is real but isolated from installed
					// binaries, git, credentials, network calls and model sessions.
					binDir := t.TempDir()
					stub := filepath.Join(binDir, "modernpath")
					write(stub, "#!/bin/sh\nexit 0\n")
					if err := os.Chmod(stub, 0o755); err != nil {
						t.Fatal(err)
					}
					t.Setenv("PATH", binDir)
					output := captureHookDiagnostic(t, func() error {
						if command == "doctor" {
							return runHooksDoctor(hooksTestCmd(), nil)
						}
						return runHooksStatus(hooksTestCmd(), nil)
					})
					if command == "status" {
						_, output, _ = strings.Cut(output, agent.name+":\n")
						output, _, _ = strings.Cut(output, "\n\n")
					} else {
						var lines []string
						for _, line := range strings.Split(output, "\n") {
							if strings.Contains(line, agent.name+": ") {
								lines = append(lines, line)
							}
						}
						output = strings.Join(lines, "\n")
					}
					syncLabel := "Sync hooks: "
					if command == "doctor" {
						syncLabel = agent.name + ": sync "
					}
					var syncLines []string
					for _, line := range strings.Split(output, "\n") {
						if strings.Contains(line, syncLabel) {
							syncLines = append(syncLines, line)
						}
					}
					if len(syncLines) != 1 {
						t.Fatalf("want one sync report, got %d:\n%s", len(syncLines), output)
					}
					line := syncLines[0]
					wantSeverity := "⚠"
					var want []string
					if fixture.store {
						want = []string{syncLabel + "retired (store-backed)"}
						switch fixture.state {
						case "absent":
							wantSeverity = "→"
							for _, stale := range []string{"not installed", "not configured", "NOT wired", "hooks install"} {
								if strings.Contains(line, stale) {
									t.Errorf("expected retirement must not request repair (%s): %s", stale, line)
								}
							}
						case "invalid config":
							want = append(want, "invalid config", "cannot inspect")
						default:
							want = append(want, "still installed", fixture.state, "modernpath hooks install --"+key)
						}
					} else {
						state := "not installed"
						if command == "doctor" {
							state = "hook NOT wired"
						}
						if key == "codex" {
							state = "not configured"
						}
						if fixture.state == "configured" {
							wantSeverity, state = "✓", "installed"
							if command == "doctor" {
								state = "hook wired"
							}
							if key == "codex" {
								state = "configured"
							}
						}
						want = []string{syncLabel + state}
						if strings.Contains(line, "retired") {
							t.Errorf("file-backed sync must retain its existing report: %s", line)
						}
					}
					if !strings.HasPrefix(strings.TrimSpace(line), wantSeverity+" ") {
						t.Errorf("want severity %s: %s", wantSeverity, line)
					}
					for _, text := range want {
						if !strings.Contains(line, text) {
							t.Errorf("sync report missing %q: %s", text, line)
						}
					}
					if fixture.state != "configured" && !(fixture.store && fixture.state == "absent") {
						for _, family := range []string{"context", "process gate"} {
							found := false
							for _, other := range strings.Split(output, "\n") {
								found = found || (strings.HasPrefix(strings.TrimSpace(other), "⚠ ") && strings.Contains(strings.ToLower(other), family))
							}
							if !found {
								t.Errorf("missing unrelated %s warning:\n%s", family, output)
							}
						}
					}
					if key == "codex" && !strings.Contains(output, "/hooks") {
						t.Errorf("missing Codex trust guidance:\n%s", output)
					}
					if key == "pi" && !strings.Contains(output, "project is trusted") {
						t.Errorf("missing Pi trust guidance:\n%s", output)
					}
				})
			}
		}
	}
}

// Capture the actual section headers and both diagnostic streams in order.
// The helpers' severity prefixes distinguish informational reports from warnings.
func captureHookDiagnostic(t *testing.T, run func() error) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "hook-diagnostic-*")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	func() {
		stdout, stderr := os.Stdout, os.Stderr
		colorOut, colorErr, noColor := color.Output, color.Error, color.NoColor
		defer func() {
			os.Stdout, os.Stderr = stdout, stderr
			color.Output, color.Error, color.NoColor = colorOut, colorErr, noColor
		}()
		os.Stdout, os.Stderr = file, file
		color.Output, color.Error, color.NoColor = file, file, true
		if err := run(); err != nil {
			t.Errorf("diagnostic command failed: %v", err)
		}
	}()
	raw, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
