package cmd

import (
	"encoding/json"
	"os"
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

func TestPiInstallWritesExtensionAndSkillsPointer(t *testing.T) {
	chdirTemp(t)
	agent := selectPiOnly(t)
	if err := os.WriteFile(".modernpath-initialized-stub", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// installForAgent is the bound path; it does not consult IsInitialized.
	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}

	source := settingsText(t, piExtensionPath(agent))
	if !strings.Contains(source, piManagedHeader) {
		t.Fatalf("extension is not CLI-owned:\n%s", source)
	}
	for _, marker := range []string{contextHookMarker, syncHookMarker, gateHookMarker, briefHookMarker} {
		if !strings.Contains(source, marker) {
			t.Fatalf("Pi install omitted %q:\n%s", marker, source)
		}
	}
	if !strings.Contains(source, `"session_start"`) || !strings.Contains(source, `"before_agent_start"`) ||
		!strings.Contains(source, `"tool_call"`) || !strings.Contains(source, `"session_shutdown"`) {
		t.Fatalf("Pi extension is missing the event wiring:\n%s", source)
	}

	settings := settingsText(t, agent.configPath)
	if !strings.Contains(settings, piSkillsPath) {
		t.Fatalf("skills pointer missing: %s", settings)
	}
	if contextFamilyState(agent) != hookStateConfigured ||
		syncFamilyState(agent) != hookStateConfigured ||
		gateFamilyState(agent) != hookStateConfigured ||
		briefFamilyState(agent) != hookStateConfigured {
		t.Fatal("every family should read as configured after a full Pi install")
	}
	if !piSkillsInstalled(agent) {
		t.Fatal("skills pointer should report installed")
	}
}

// REQ-CROSS-395: store-backed installation retires only Pi's file-sync family.
func TestPiStoreBackedInstallOmitsSync(t *testing.T) {
	for _, tc := range []struct {
		name      string
		reinstall bool
		subdir    bool
	}{
		{name: "fresh install"},
		{name: "reinstall after flip", reinstall: true},
		{name: "reinstall from subdirectory", reinstall: true, subdir: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := chdirTemp(t)
			if tc.subdir {
				for _, dir := range []string{".modernpath", "src"} {
					if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Chdir(filepath.Join(root, "src")); err != nil {
					t.Fatal(err)
				}
			}
			agent := hookAgents["pi"]
			if tc.reinstall {
				if err := installForAgent(agent); err != nil {
					t.Fatal(err)
				}
				if !syncFamilyInstalled(agent) {
					t.Fatal("file-backed install must wire sync before the flip")
				}
			}

			mustWriteStoreBackedMarker(t, root)
			if err := installForAgent(agent); err != nil {
				t.Fatal(err)
			}
			source := settingsText(t, piExtensionPath(agent))
			if strings.Contains(source, syncHookMarker) || strings.Contains(source, "modernpathSync") {
				t.Fatalf("store-backed install must omit sync commands and event handlers:\n%s", source)
			}
			for _, marker := range []string{contextHookMarker, gateHookMarker, briefHookMarker} {
				if !strings.Contains(source, marker) {
					t.Errorf("store-backed install dropped %q", marker)
				}
			}
			if !piSkillsInstalled(agent) {
				t.Error("store-backed install must keep the skills pointer")
			}
		})
	}
}

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

func TestPiUnboundInstallArmsOnlyTheGateAndSkills(t *testing.T) {
	chdirTemp(t)
	agent := selectPiOnly(t)

	if err := runHooksInstall(hooksTestCmd(), nil); err != nil {
		t.Fatalf("the gate needs no server: %v", err)
	}

	source := settingsText(t, piExtensionPath(agent))
	if !strings.Contains(source, gateHookMarker) {
		t.Fatalf("unbound Pi install did not arm the gate:\n%s", source)
	}
	if strings.Contains(source, syncHookMarker) || strings.Contains(source, contextHookMarker) {
		t.Fatalf("server-dependent families leaked into an unbound install:\n%s", source)
	}
	if !piSkillsInstalled(agent) {
		t.Fatal("skills pointer still belongs on an unbound install")
	}
}

func TestPiUninstallRemovesFamiliesAndKeepsForeignSettings(t *testing.T) {
	chdirTemp(t)
	agent := hookAgents["pi"]
	if err := os.MkdirAll(filepath.Dir(agent.configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agent.configPath, []byte(`{"theme":"mono","skills":["./mine"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}

	if err := runHooksUninstall(hooksTestCmd(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(piExtensionPath(agent)); !os.IsNotExist(err) {
		t.Fatal("uninstall left the generated extension")
	}
	got := settingsText(t, agent.configPath)
	if strings.Contains(got, piSkillsMarker) {
		t.Fatalf("skills pointer survived uninstall: %s", got)
	}
	if !strings.Contains(got, `"theme"`) || !strings.Contains(got, "./mine") {
		t.Fatalf("uninstall took the user's own settings: %s", got)
	}
}

func TestPiNoFlagsOmitThoseFamiliesFromTheExtension(t *testing.T) {
	chdirTemp(t)
	agent := hookAgents["pi"]
	hooksNoSync = true
	hooksNoBrief = true
	t.Cleanup(func() {
		hooksNoSync = false
		hooksNoBrief = false
	})
	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}
	source := settingsText(t, piExtensionPath(agent))
	if strings.Contains(source, syncHookMarker) || strings.Contains(source, briefHookMarker) {
		t.Fatalf("--no-sync/--no-brief still wrote those families:\n%s", source)
	}
	if !strings.Contains(source, contextHookMarker) || !strings.Contains(source, gateHookMarker) {
		t.Fatalf("remaining families were dropped:\n%s", source)
	}
}

func TestPiStatusAndDoctorAgreeAfterInstall(t *testing.T) {
	chdirTemp(t)
	agent := hookAgents["pi"]
	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}
	families := hookFamiliesFor(agent)
	for _, name := range []string{"context", "sync", "process gate", "brief"} {
		f, ok := familyByName(families, name)
		if !ok || !f.applies || f.state != hookStateConfigured {
			t.Fatalf("%s family after Pi install: %+v (found=%v)", name, f, ok)
		}
	}

	out := captureCLIOutput(t)
	if err := runHooksStatus(hooksTestCmd(), nil); err != nil {
		t.Fatal(err)
	}
	text := out()
	for _, want := range []string{
		"Context hook: installed",
		"Sync hooks: installed",
		"Process gate: armed",
		"Session brief: installed",
		"Skills: pointed at",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("hooks status missing %q in:\n%s", want, text)
		}
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
