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
