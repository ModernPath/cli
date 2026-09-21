package cmd

// Pi (https://pi.dev) has no Claude-style hooks JSON. The four families land
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
	_, storeBacked := storeBackedFromCwd()
	if err := writePiExtension(agent, piFamilyFlags{
		context: !hooksNoContext,
		sync:    !hooksNoSync && !storeBacked,
		gate:    !hooksNoGate,
		brief:   !hooksNoBrief,
	}); err != nil {
		return err
	}
	if storeBacked && !hooksNoSync {
		printInfo("%s: auto-sync hooks retired (store-backed) — write process state with 'modernpath author'\n", agent.name)
	}
	return mergePiSkills(agent)
}

func installPiGateOnly(agent agentConfig) error {
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", agent.hooksDir, err)
	}
	if err := writePiExtension(agent, piFamilyFlags{gate: true}); err != nil {
		return err
	}
	return mergePiSkills(agent)
}

type piFamilyFlags struct {
	context bool
	sync    bool
	gate    bool
	brief   bool
}

func (f piFamilyFlags) any() bool {
	return f.context || f.sync || f.gate || f.brief
}

func writePiExtension(agent agentConfig, families piFamilyFlags) error {
	path := piExtensionPath(agent)
	if !families.any() {
		_ = os.Remove(path)
		return nil
	}
	return os.WriteFile(path, []byte(piExtensionSource(families)), 0o644)
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
	if ok, err := uninstallPiContext(agent); err != nil {
		return removed, err
	} else if ok {
		removed = append(removed, agent.name+" (context)")
	}
	if ok, err := uninstallPiSync(agent); err != nil {
		return removed, err
	} else if ok {
		removed = append(removed, agent.name+" (sync)")
	}
	if ok, err := uninstallPiGate(agent); err != nil {
		return removed, err
	} else if ok {
		removed = append(removed, agent.name+" (gate)")
	}
	if ok, err := uninstallPiBrief(agent); err != nil {
		return removed, err
	} else if ok {
		removed = append(removed, agent.name+" (brief)")
	}
	if ok, err := uninstallPiSkills(agent); err != nil {
		return removed, err
	} else if ok {
		removed = append(removed, agent.name+" (skills)")
	}
	return removed, nil
}

func uninstallPiContext(agent agentConfig) (bool, error) {
	return uninstallPiFamily(agent, contextHookMarker)
}

func uninstallPiSync(agent agentConfig) (bool, error) {
	return uninstallPiFamily(agent, syncHookMarker)
}

func uninstallPiGate(agent agentConfig) (bool, error) {
	return uninstallPiFamily(agent, gateHookMarker)
}

func uninstallPiBrief(agent agentConfig) (bool, error) {
	return uninstallPiFamily(agent, briefHookMarker)
}

func uninstallPiFamily(agent agentConfig, marker string) (bool, error) {
	if piExtensionState(agent, marker) != hookStateConfigured {
		return false, nil
	}
	body, err := os.ReadFile(piExtensionPath(agent))
	if err != nil {
		return false, nil
	}
	flags := piFamiliesFromSource(string(body))
	switch marker {
	case contextHookMarker:
		flags.context = false
	case syncHookMarker:
		flags.sync = false
	case gateHookMarker:
		flags.gate = false
	case briefHookMarker:
		flags.brief = false
	}
	if err := writePiExtension(agent, flags); err != nil {
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
	if !strings.Contains(text, piManagedHeader) {
		if strings.Contains(text, marker) {
			return hookStateLegacy
		}
		return hookStateAbsent
	}
	if strings.Contains(text, marker) {
		return hookStateConfigured
	}
	return hookStateAbsent
}

func piFamiliesFromSource(source string) piFamilyFlags {
	return piFamilyFlags{
		context: strings.Contains(source, contextHookMarker),
		sync:    strings.Contains(source, syncHookMarker),
		gate:    strings.Contains(source, gateHookMarker),
		brief:   strings.Contains(source, briefHookMarker),
	}
}

func reportPiStatus(agent agentConfig) {
	if contextFamilyInstalled(agent) {
		printSuccess("  Context hook: installed (%s)\n", piExtensionPath(agent))
	} else {
		printWarning("  Context hook: not installed\n")
	}
	state := syncFamilyState(agent)
	if !reportRetiredSyncFamily("pi", "  Sync hooks:", state) {
		if state == hookStateConfigured {
			printSuccess("  Sync hooks: installed (session_start · agent_settled · session_shutdown → detached --if-quiescent sync)\n")
		} else {
			printWarning("  Sync hooks: not installed\n")
		}
	}
	if gateFamilyInstalled(agent) {
		printSuccess("  Process gate: armed (tool_call bash → modernpath check --hook PreToolUse)\n")
	} else {
		printWarning("  Process gate: not armed\n")
	}
	if briefFamilyInstalled(agent) {
		printSuccess("  Session brief: installed (session_start → modernpath your-move --hook)\n")
	} else {
		printWarning("  Session brief: not installed\n")
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

func piExtensionSource(families piFamilyFlags) string {
	var b strings.Builder
	b.WriteString(piManagedHeader)
	b.WriteString(" — rewritten by `modernpath hooks install`. Do not edit.\n")
	b.WriteString(`import { spawn, spawnSync } from "node:child_process";

function runModernpath(args, input) {
  try {
    const result = spawnSync("modernpath", args, {
      encoding: "utf8",
      input: input ?? "",
      stdio: ["pipe", "pipe", "ignore"],
      timeout: 30000,
    });
    return result.stdout || "{}";
  } catch {
    return "{}";
  }
}

function additionalContext(raw) {
  try {
    return JSON.parse(raw)?.hookSpecificOutput?.additionalContext || "";
  } catch {
    return "";
  }
}

`)
	if families.sync {
		b.WriteString(`function modernpathSync(trigger) {
  try {
    const child = spawn(
      "modernpath",
      ["factory", "sync", "--if-quiescent", "--trigger", trigger],
      { detached: true, stdio: "ignore" },
    );
    child.unref();
  } catch {
    // a missing binary is a silent no-op, same as the other agents
  }
}

`)
	}
	if families.context {
		b.WriteString("// modernpath context --hook " + piContextEventName + "\n")
	}
	if families.sync {
		b.WriteString("// modernpath factory sync --if-quiescent\n")
	}
	if families.gate {
		b.WriteString("// modernpath check --hook PreToolUse\n")
	}
	if families.brief {
		b.WriteString("// modernpath your-move --hook\n")
	}
	b.WriteString("export default function (pi) {\n")
	if families.brief {
		b.WriteString("  let pendingBrief = \"\";\n")
	}
	if families.sync || families.brief {
		b.WriteString("  pi.on(\"session_start\", (event, ctx) => {\n")
		if families.sync {
			b.WriteString("    modernpathSync(\"SessionStart\");\n")
		}
		if families.brief {
			b.WriteString(`    const sessionId = ctx.sessionManager?.getSessionFile?.() || "pi";
    const source = event?.reason || "startup";
    pendingBrief = additionalContext(
      runModernpath(
        ["your-move", "--hook", "SessionStart"],
        JSON.stringify({
          session_id: sessionId,
          hook_event_name: "SessionStart",
          source,
        }),
      ),
    );
`)
		}
		b.WriteString("  });\n")
	}
	if families.sync {
		b.WriteString(`  pi.on("agent_settled", () => {
    modernpathSync("Stop");
  });
  pi.on("session_shutdown", () => {
    modernpathSync("SessionEnd");
  });
`)
	}
	if families.context || families.brief {
		b.WriteString("  pi.on(\"before_agent_start\", () => {\n")
		b.WriteString("    const parts = [];\n")
		if families.brief {
			b.WriteString("    if (pendingBrief) {\n")
			b.WriteString("      parts.push(pendingBrief);\n")
			b.WriteString("      pendingBrief = \"\";\n")
			b.WriteString("    }\n")
		}
		if families.context {
			b.WriteString("    const context = additionalContext(\n")
			b.WriteString("      runModernpath([\"context\", \"--hook\", \"UserPromptSubmit\"]),\n")
			b.WriteString("    );\n")
			b.WriteString("    if (context) parts.push(context);\n")
		}
		b.WriteString(`    if (!parts.length) return;
    return {
      message: {
        customType: "modernpath",
        content: parts.join("\n\n"),
        display: false,
      },
    };
`)
		b.WriteString("  });\n")
	}
	if families.gate {
		b.WriteString(`  pi.on("tool_call", (event) => {
    if (event.toolName !== "bash") return;
    const command = typeof event.input?.command === "string" ? event.input.command : "";
    const raw = runModernpath(
      ["check", "--hook", "PreToolUse"],
      JSON.stringify({ cwd: process.cwd(), tool_input: { command } }),
    );
    try {
      const decision = JSON.parse(raw)?.hookSpecificOutput;
      if (decision?.permissionDecision === "deny") {
        return { block: true, reason: decision.permissionDecisionReason || "process gate" };
      }
    } catch {
      // tooling failure is fail-open, same as the PreToolUse adapter
    }
  });
`)
	}
	b.WriteString("}\n")
	return b.String()
}
