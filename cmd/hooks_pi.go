package cmd

// Pi (https://pi.dev) has no Claude-style hooks JSON. Context hooks land
// as one generated TypeScript extension that calls the installed CLI — the
// same "no tracked script" rule as the other agents. Skills are a settings
// pointer at the copies `modernpath install` already writes under
// `.claude/skills/`.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	piExtensionName    = "modernpath.ts"
	piManagedHeader    = "// modernpath-managed"
	piSkillsPath       = "../.claude/skills"
	piSkillsMarker     = ".claude/skills"
	piContextEventName = "UserPromptSubmit"
)

func piExtensionPath(agent agentConfig) string {
	return filepath.Join(agent.hooksDir, piExtensionName)
}

func installPi(agent agentConfig) error {
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", agent.hooksDir, err)
	}
	if err := writePiExtension(agent, !hooksNoContext); err != nil {
		return err
	}
	return mergePiSkills(agent)
}

func writePiExtension(agent agentConfig, enabled bool) error {
	path := piExtensionPath(agent)
	if !enabled {
		_ = os.Remove(path)
		return nil
	}
	return os.WriteFile(path, []byte(piExtensionSource()), 0o644)
}

func mergePiSkills(agent agentConfig) error {
	settings, err := readSettingsForMerge(agent.configPath)
	if err != nil {
		return err
	}
	skills, _ := settings["skills"].([]interface{})
	if !piSkillsListed(skills) {
		skills = append(skills, piSkillsPath)
	}
	settings["skills"] = skills
	return writeSettingsFile(agent.configPath, settings)
}

func piSkillsListed(skills []interface{}) bool {
	for _, entry := range skills {
		text, _ := entry.(string)
		if strings.Contains(text, piSkillsMarker) {
			return true
		}
	}
	return false
}

func uninstallPi(agent agentConfig) (removed []string, err error) {
	if ok, err := uninstallPiRetiredHooks(agent); err != nil {
		return removed, err
	} else if ok {
		removed = append(removed, agent.name+" (retired hooks)")
	}
	if ok, err := uninstallPiContext(agent); err != nil {
		return removed, err
	} else if ok {
		removed = append(removed, agent.name+" (context)")
	}
	if ok, err := uninstallPiSkills(agent); err != nil {
		return removed, err
	} else if ok {
		removed = append(removed, agent.name+" (skills)")
	}
	return removed, nil
}

func uninstallPiRetiredHooks(agent agentConfig) (bool, error) {
	if !hasRetiredPiHook(agent) {
		return false, nil
	}
	if err := os.Remove(piExtensionPath(agent)); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return true, nil
}

func hasRetiredPiHook(agent agentConfig) bool {
	raw, err := os.ReadFile(piExtensionPath(agent))
	if err != nil {
		return false
	}
	text := string(raw)
	if !strings.Contains(text, piManagedHeader) {
		return false
	}
	return strings.Contains(text, "check --hook") ||
		strings.Contains(text, "your-move --hook") ||
		strings.Contains(text, `["check", "--hook"`) ||
		strings.Contains(text, `["your-move", "--hook"`)
}

func uninstallPiContext(agent agentConfig) (bool, error) {
	if piExtensionState(agent, contextHookMarker) != hookStateConfigured {
		return false, nil
	}
	if err := os.Remove(piExtensionPath(agent)); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return true, nil
}

func uninstallPiSkills(agent agentConfig) (bool, error) {
	settings, err := readSettingsForMerge(agent.configPath)
	if err != nil {
		return false, err
	}
	skills, _ := settings["skills"].([]interface{})
	if !piSkillsListed(skills) {
		return false, nil
	}
	kept := make([]interface{}, 0, len(skills))
	for _, entry := range skills {
		text, _ := entry.(string)
		if strings.Contains(text, piSkillsMarker) {
			continue
		}
		kept = append(kept, entry)
	}
	if len(kept) == 0 {
		delete(settings, "skills")
	} else {
		settings["skills"] = kept
	}
	if err := writeSettingsFile(agent.configPath, settings); err != nil {
		return false, fmt.Errorf("cannot write %s: %w", agent.configPath, err)
	}
	return true, nil
}

func piExtensionState(agent agentConfig, marker string) hookFamilyState {
	raw, err := os.ReadFile(piExtensionPath(agent))
	if os.IsNotExist(err) {
		return hookStateAbsent
	}
	if err != nil {
		return hookStateInvalid
	}
	text := string(raw)
	matches := strings.Contains(text, marker) ||
		(marker == contextHookMarker && strings.Contains(text, `["context", "--hook", "UserPromptSubmit"]`))
	if !strings.Contains(text, piManagedHeader) {
		if matches {
			return hookStateLegacy
		}
		return hookStateAbsent
	}
	if matches {
		return hookStateConfigured
	}
	return hookStateAbsent
}

func reportPiStatus(agent agentConfig) {
	if contextFamilyInstalled(agent) {
		printSuccess("  Context hook: installed (%s)\n", piExtensionPath(agent))
	} else {
		printWarning("  Context hook: not installed\n")
	}
	if piSkillsInstalled(agent) {
		printSuccess("  Skills: pointed at %s\n", piSkillsPath)
	} else {
		printWarning("  Skills: not pointed at %s\n", piSkillsPath)
	}
	printInfo("  Trust/execution: Pi loads project .pi/ only after the project is trusted.\n")
}

func piSkillsInstalled(agent agentConfig) bool {
	settings, err := readSettingsForMerge(agent.configPath)
	if err != nil {
		return false
	}
	skills, _ := settings["skills"].([]interface{})
	return piSkillsListed(skills)
}

func piExtensionSource() string {
	return piManagedHeader + ` — rewritten by modernpath hooks install. Do not edit.
import { spawnSync } from "node:child_process";

export default function (pi) {
  pi.on("before_agent_start", (event) => {
    if (typeof event.prompt !== "string" || !event.prompt) return;
    try {
      const result = spawnSync("modernpath", ["context", "--hook", "UserPromptSubmit"], {
        input: JSON.stringify({ prompt: event.prompt }),
        encoding: "utf8", stdio: ["pipe", "pipe", "ignore"], timeout: 30000,
      });
      if (result.status !== 0 || result.error) return;
      const context = JSON.parse(result.stdout || "{}")?.hookSpecificOutput?.additionalContext || "";
      if (!context) return;
      return { message: { customType: "modernpath", content: context, display: false } };
    } catch { return; }
  });
}
`
}
