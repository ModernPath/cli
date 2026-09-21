package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// REQ-CROSS-396: repairing legacy sync must account for Pi's unowned extension.
func TestPiStoreBackedLegacySyncRepairHint(t *testing.T) {
	for _, command := range []string{"status", "doctor"} {
		t.Run(command, func(t *testing.T) {
			root := chdirTemp(t)
			mustWriteStoreBackedMarker(t, root)
			agent := hookAgents["pi"]
			if err := installForAgent(agent); err != nil {
				t.Fatal(err)
			}
			foreign := "// user-owned extension\n// " + syncHookMarker + "\n"
			if err := os.WriteFile(piExtensionPath(agent), []byte(foreign), 0o644); err != nil {
				t.Fatal(err)
			}
			if syncFamilyState(agent) != hookStateLegacy {
				t.Fatal("fixture must contain an unowned legacy sync extension")
			}
			settings := settingsText(t, agent.configPath)
			// Isolate doctor's binary preflight from the developer's installations.
			binDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(binDir, "modernpath"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", binDir)
			output := captureHookDiagnostic(t, func() error {
				if command == "doctor" {
					return runHooksDoctor(hooksTestCmd(), nil)
				}
				return runHooksStatus(hooksTestCmd(), nil)
			})
			if settingsText(t, piExtensionPath(agent)) != foreign || settingsText(t, agent.configPath) != settings {
				t.Error("diagnostics changed Pi files")
			}
			move := regexp.MustCompile(`move \.pi/extensions/modernpath\.ts to (\S+)`).FindStringSubmatch(output)
			if len(move) != 2 || !strings.Contains(output, "then run 'modernpath hooks install --pi'") {
				t.Fatalf("repair must name a safe backup destination before reinstall:\n%s", output)
			}
			backup := move[1]
			rel, err := filepath.Rel(agent.hooksDir, backup)
			if err != nil || !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				t.Fatalf("backup %q remains inside Pi's extension discovery directory", backup)
			}
			if err := os.Rename(piExtensionPath(agent), backup); err != nil {
				t.Fatal(err)
			}
			if err := installForAgent(agent); err != nil {
				t.Fatal(err)
			}
			if settingsText(t, backup) != foreign {
				t.Error("repair lost the user's extension")
			}
			entries, err := os.ReadDir(agent.hooksDir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != "modernpath.ts" || syncFamilyState(agent) != hookStateAbsent {
				t.Fatal("repair left an extension containing retired sync in Pi's discovery directory")
			}
		})
	}
}

// REQ-CROSS-396: Pi discovers extensions independently of its optional settings.
func TestPiDoctorWithoutSettings(t *testing.T) {
	for _, storeBacked := range []bool{false, true} {
		mode := "file-backed"
		if storeBacked {
			mode = "store-backed"
		}
		for _, state := range []string{"configured", "legacy", "absent"} {
			t.Run(mode+"/"+state, func(t *testing.T) {
				root := chdirTemp(t)
				agent := hookAgents["pi"]
				if err := installForAgent(agent); err != nil {
					t.Fatal(err)
				}
				if storeBacked {
					mustWriteStoreBackedMarker(t, root)
				}
				if state == "legacy" {
					source := strings.ReplaceAll(settingsText(t, piExtensionPath(agent)), piManagedHeader, "// user-owned")
					if err := os.WriteFile(piExtensionPath(agent), []byte(source), 0o644); err != nil {
						t.Fatal(err)
					}
				} else if state == "absent" {
					if err := os.Remove(piExtensionPath(agent)); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Remove(agent.configPath); err != nil {
					t.Fatal(err)
				}
				before, _ := os.ReadFile(piExtensionPath(agent))
				binDir := t.TempDir()
				if err := os.WriteFile(filepath.Join(binDir, "modernpath"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", binDir)
				output := captureHookDiagnostic(t, func() error { return runHooksDoctor(hooksTestCmd(), nil) })
				if storeBacked {
					if !strings.Contains(output, "Pi: sync retired (store-backed)") || strings.Contains(output, "Pi: no hooks configured") {
						t.Errorf("doctor must inspect Pi extensions without settings:\n%s", output)
					}
					if state != "absent" && !strings.Contains(output, "still installed (") {
						t.Errorf("doctor hid active retired sync:\n%s", output)
					}
				} else if !strings.Contains(output, "Pi: no hooks configured (.pi/settings.json)") || strings.Contains(output, "Pi: sync") {
					t.Errorf("file-backed diagnostics changed:\n%s", output)
				}
				after, _ := os.ReadFile(piExtensionPath(agent))
				if string(after) != string(before) {
					t.Error("doctor changed the extension")
				}
				if _, err := os.Stat(agent.configPath); !os.IsNotExist(err) {
					t.Error("doctor created settings")
				}
			})
		}
	}
}

// REQ-CROSS-395: explicit --no-sync and user settings survive a store-backed reinstall.
func TestPiStoreBackedReinstallPreservesSettings(t *testing.T) {
	for _, noSync := range []bool{false, true} {
		name := "default"
		if noSync {
			name = "no-sync"
		}
		t.Run(name, func(t *testing.T) {
			root := chdirTemp(t)
			agent := hookAgents["pi"]
			if err := installForAgent(agent); err != nil {
				t.Fatal(err)
			}
			if !syncFamilyInstalled(agent) {
				t.Fatal("file-backed installation did not wire sync")
			}
			if err := os.WriteFile(agent.configPath, []byte(`{"theme":"mono","skills":["./mine"]}`), 0o644); err != nil {
				t.Fatal(err)
			}
			foreignPath := filepath.Join(agent.hooksDir, "mine.ts")
			const foreign = "// user's other extension\n"
			if err := os.WriteFile(foreignPath, []byte(foreign), 0o644); err != nil {
				t.Fatal(err)
			}
			mustWriteStoreBackedMarker(t, root)
			hooksNoSync = noSync
			t.Cleanup(func() { hooksNoSync = false })
			for i := 0; i < 2; i++ {
				if err := installForAgent(agent); err != nil {
					t.Fatal(err)
				}
			}
			source := settingsText(t, piExtensionPath(agent))
			if strings.Contains(source, syncHookMarker) || strings.Contains(source, "modernpathSync") {
				t.Error("reinstall left retired sync handlers")
			}
			for _, marker := range []string{contextHookMarker, gateHookMarker, briefHookMarker} {
				if !strings.Contains(source, marker) {
					t.Errorf("reinstall dropped %q", marker)
				}
			}
			var settings map[string]interface{}
			if err := json.Unmarshal([]byte(settingsText(t, agent.configPath)), &settings); err != nil {
				t.Fatal(err)
			}
			want := map[string]interface{}{"theme": "mono", "skills": []interface{}{"./mine", piSkillsPath}}
			if !reflect.DeepEqual(settings, want) {
				t.Errorf("reinstall changed user settings or duplicated skills: got %#v, want %#v", settings, want)
			}
			if settingsText(t, foreignPath) != foreign {
				t.Error("reinstall changed another extension")
			}
		})
	}
}
