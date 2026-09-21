package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// BACKLOG-TOOL-33 / -43: a hook shell whose PATH lacks the install directory
// ran nothing and said nothing. The commands now try the workspace link first
// and PATH second, and stay a silent, well-formed no-op when neither has a
// binary. Run the way a harness runs them, not by reading the string.
func runHook(t *testing.T, command, projectDir, pathDir string) string {
	t.Helper()
	cmd := exec.Command("sh", "-c", command)
	cmd.Env = append(os.Environ(), "PATH="+pathDir, "CLAUDE_PROJECT_DIR="+projectDir)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("hook command failed the harness: %v (%s)", err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeStub(t *testing.T, path, says string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho "+says+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestHookCommandsPreferTheWorkspaceLinkThenPath(t *testing.T) {
	project := t.TempDir()
	pathDir := t.TempDir()
	command := hookCommand("context --hook UserPromptSubmit", "printf '{}'")

	if got := runHook(t, command, project, pathDir); got != "{}" {
		t.Fatalf("with no binary anywhere the hook must answer '{}', got %q", got)
	}
	writeStub(t, filepath.Join(pathDir, "modernpath"), "PATH")
	if got := runHook(t, command, project, pathDir); got != "PATH" {
		t.Fatalf("without the link PATH resolves the binary, got %q", got)
	}
	writeStub(t, filepath.Join(project, hooksBinaryRel), "LINK")
	if got := runHook(t, command, project, pathDir); got != "LINK" {
		t.Fatalf("the workspace link must win over PATH, got %q", got)
	}
	// The install directory missing from the hook shell's PATH is the reported
	// failure; the link makes the hook independent of it.
	if got := runHook(t, command, project, t.TempDir()); got != "LINK" {
		t.Fatalf("the link must run with nothing on PATH, got %q", got)
	}
	for _, c := range []string{gateHookCommand, briefHookCommand(), contextHookCommand("UserPromptSubmit"), codexSyncHookCommand("Stop")} {
		if !strings.HasPrefix(c, hookPrelude) {
			t.Errorf("every hook family goes through the prelude: %s", c)
		}
	}
}

func TestLinkHooksBinaryPointsAtTheRunningExecutable(t *testing.T) {
	root := t.TempDir()
	target, note, err := linkHooksBinary(root)
	if err != nil || note != "" {
		t.Fatalf("first link: err=%v note=%q", err, note)
	}
	exe, _ := runningExecutable()
	if target != exe {
		t.Fatalf("link target %q, running executable %q", target, exe)
	}
	got, ok := hooksBinaryTarget(root)
	if !ok || got != exe {
		t.Fatalf("hooksBinaryTarget = %q, %v; want %q", got, ok, exe)
	}

	// A stale link is re-pointed, not kept.
	link := filepath.Join(root, hooksBinaryRel)
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "gone"), link); err != nil {
		t.Fatal(err)
	}
	if _, ok := hooksBinaryTarget(root); ok {
		t.Fatal("a dangling link is not a usable binary")
	}
	if _, _, err := linkHooksBinary(root); err != nil {
		t.Fatal(err)
	}
	if got, ok := hooksBinaryTarget(root); !ok || got != exe {
		t.Fatalf("a stale link must be re-pointed, got %q %v", got, ok)
	}

	// A regular file there is somebody's deliberate binary: left alone, said so.
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	writeStub(t, link, "LOCAL")
	if _, note, err := linkHooksBinary(root); err != nil || !strings.Contains(note, "regular file") {
		t.Fatalf("a regular file must be left alone and reported: err=%v note=%q", err, note)
	}
	want, _ := filepath.EvalSymlinks(link) // the temp dir itself may sit behind a symlink
	if got, ok := hooksBinaryTarget(root); !ok || got != want {
		t.Fatalf("the regular file is what the hooks run, got %q %v, want %q", got, ok, want)
	}
}

// A reinstall must carry the current command into a settings file that already
// holds the context entry: the previous writer appended only when no entry
// carried the marker, so the bare-name command outlived the launcher change.
func TestClaudeContextEntryIsUpgradedOnReinstall(t *testing.T) {
	agent := gateTestAgent(t)
	agent.eventName = "UserPromptSubmit"
	agent.scriptName = "modernpath-context.sh"
	stale := `{"hooks":{"UserPromptSubmit":[` +
		`{"matcher":"*","hooks":[{"type":"command","command":"command -v modernpath >/dev/null 2>&1 && modernpath context --hook UserPromptSubmit 2>/dev/null || echo '{}'","timeout":60}]},` +
		`{"hooks":[{"type":"command","command":"project-own.sh"}]}]}}`
	if err := os.WriteFile(agent.configPath, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeClaudeConfig(agent); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(agent.configPath)
	text := string(raw)
	if !strings.Contains(text, hooksBinaryRel) || strings.Contains(text, "command -v modernpath >/dev/null") {
		t.Fatalf("the stale context command survived the reinstall: %s", text)
	}
	if strings.Count(text, contextHookMarker) != 1 {
		t.Fatalf("exactly one context entry after reinstall, got %d: %s", strings.Count(text, contextHookMarker), text)
	}
	if !strings.Contains(text, "project-own.sh") {
		t.Fatalf("the project's own UserPromptSubmit hook was lost: %s", text)
	}
}
