package kit

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The kit ships inside the binary rather than being fetched at install time.
// That is deliberate: on 2026-08-07 a sync ran for a full day against a CLI
// whose extractor was a day older than the workspace, reporting success while
// writing nothing. Embedding makes the process and the code that reads it one
// artifact — you cannot have a new CLI with an old process, or the reverse.
//
//go:embed assets
var assets embed.FS

// installTargets maps each embedded asset onto its path in the client's
// repository. These paths are kit-namespaced — nothing but the kit ever lives
// there — so they are replaced wholesale on upgrade.
var installTargets = map[string]string{
	"assets/rdd/PROCESS.md":                 ".claude/rdd/PROCESS.md",
	"assets/rdd/platform.md":                ".claude/rdd/platform.md",
	"assets/rdd/README.md":                  ".claude/rdd/README.md",
	"assets/skills/rdd-build-loop/SKILL.md": ".claude/skills/rdd-build-loop/SKILL.md",
	"assets/skills/rdd-planning/SKILL.md":   ".claude/skills/rdd-planning/SKILL.md",
	"assets/skills/rdd-ledger/SKILL.md":     ".claude/skills/rdd-ledger/SKILL.md",
	"assets/skills/rdd-discovery/SKILL.md":  ".claude/skills/rdd-discovery/SKILL.md",
	"assets/hooks/rdd-gate.sh":              ".claude/hooks/rdd-gate.sh",
}

// mergeTargets are files a client may already own. A real repository turned up
// with a 403-line CLAUDE.md of its own — its process manual, carrying
// project-specific non-negotiables this kit does not contain — and the
// installer would have deleted it. These are merged into a marked block
// instead: the kit owns what is between its markers and nothing else.
var mergeTargets = map[string]string{
	"assets/CLAUDE.md":               "CLAUDE.md",
	"assets/agents-block.md":         "AGENTS.md",
	"assets/copilot-instructions.md": ".github/copilot-instructions.md",
}

// titleFor gives a created file its heading, so a fresh repository still gets a
// readable document rather than a bare marker block.
var titleFor = map[string]string{
	"CLAUDE.md":                       "# Agent instructions\n",
	"AGENTS.md":                       "# AGENTS.md — this project\n",
	".github/copilot-instructions.md": "# Copilot instructions\n",
}

// Result describes what an install did, so the command can report honestly
// rather than claiming more than it changed.
type Result struct {
	Written       []string // tool-owned files written or refreshed
	Merged        []string // files the client may own, whose managed block was refreshed
	AgentsCreated bool     // true when AGENTS.md did not exist and was created
	AgentsMerged  bool     // true when an existing AGENTS.md had its block refreshed
}

// Install writes the kit into root and merges the managed block into the
// client's AGENTS.md.
//
// The AGENTS.md merge is attempted FIRST and aborts the whole install on
// failure. A half-installed repository whose AGENTS.md was mangled is far worse
// than one that was never touched, and damaged markers are exactly the case
// where the installer cannot tell which bytes belong to the client.
func Install(root string) (Result, error) {
	var res Result

	// Plan every merge BEFORE writing anything. A half-installed repository
	// whose instructions were mangled is far worse than one never touched, and
	// damaged markers are exactly the case where the installer cannot tell
	// which bytes belong to the client.
	planned := map[string][]byte{}
	for asset, target := range mergeTargets {
		block, err := assets.ReadFile(asset)
		if err != nil {
			return res, fmt.Errorf("read embedded %s: %w", asset, err)
		}
		path := filepath.Join(root, target)
		existing, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return res, fmt.Errorf("read %s: %w", target, err)
		}
		created := os.IsNotExist(err)
		doc := string(existing)
		if created {
			doc = titleFor[target]
		}
		merged, err := MergeManagedBlock(doc, string(block))
		if err != nil {
			return res, fmt.Errorf("%s: %w", target, err)
		}
		planned[target] = []byte(merged)
		if target == "AGENTS.md" {
			res.AgentsCreated, res.AgentsMerged = created, !created
		}
	}

	for asset, target := range installTargets {
		body, err := assets.ReadFile(asset)
		if err != nil {
			return res, fmt.Errorf("read embedded %s: %w", asset, err)
		}
		dest := filepath.Join(root, target)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return res, fmt.Errorf("create %s: %w", filepath.Dir(target), err)
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(target, ".sh") {
			mode = 0o755
		}
		if err := os.WriteFile(dest, body, mode); err != nil {
			return res, fmt.Errorf("write %s: %w", target, err)
		}
		res.Written = append(res.Written, target)
	}

	for target, body := range planned {
		dest := filepath.Join(root, target)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return res, fmt.Errorf("create %s: %w", filepath.Dir(target), err)
		}
		if err := os.WriteFile(dest, body, 0o644); err != nil {
			return res, fmt.Errorf("write %s: %w", target, err)
		}
		res.Merged = append(res.Merged, target)
	}

	return res, nil
}

// Assets exposes the embedded tree so callers can diff what is installed
// against what would be installed, without re-running a write.
func Assets() fs.FS { return assets }

// TargetForAsset returns the repository-relative destination of an embedded
// asset, and whether it is one the kit owns outright.
func TargetForAsset(asset string) (string, bool) {
	target, ok := installTargets[strings.TrimPrefix(asset, "./")]
	return target, ok
}

// MergeTargets lists the files a client may co-own, which are merged rather
// than replaced. Sorted, repository-relative.
func MergeTargets() []string {
	out := make([]string, 0, len(mergeTargets))
	for _, t := range mergeTargets {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// ListAssets returns every embedded asset path, so callers can enumerate what
// an install touches without duplicating the target table.
func ListAssets() ([]string, error) {
	var out []string
	err := fs.WalkDir(assets, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	return out, err
}

// Check reports which kit-namespaced files differ from what this binary would
// install — edited, or missing entirely. It exists because the generated copy
// is the readable one, so it is also the one people edit, and an install would
// revert that edit without a word.
//
// Check compares only the kit-namespaced files. Files a client co-owns are
// deliberately excluded: their content is supposed to differ.
func Check(root string) ([]string, error) {
	var drift []string
	for asset, target := range installTargets {
		want, err := assets.ReadFile(asset)
		if err != nil {
			return nil, fmt.Errorf("read embedded %s: %w", asset, err)
		}
		got, err := os.ReadFile(filepath.Join(root, target))
		if os.IsNotExist(err) || (err == nil && string(got) != string(want)) {
			drift = append(drift, target)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", target, err)
		}
	}
	sort.Strings(drift)
	return drift, nil
}
