package kit

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// The kit ships a released req-driven-dev snapshot and the agent-channel
// adapters that load it. Installation never fetches instructions at runtime.
//
//go:embed assets
var assets embed.FS

// installTargets maps non-process assets onto their paths in the client's
// repository. The process snapshot under assets/rdd is mapped recursively by
// TargetForAsset so adding a source document cannot be omitted from install.
var installTargets = map[string]string{
	"assets/rdd-source.txt": ".modernpath/rdd/.source",
	// The consolidated package's skills replaced the workspace-authored
	// rdd-build-loop/rdd-planning/rdd-ledger/rdd-discovery/rdd-reverse-engineer/
	// rdd-verify set; those live under assets/rdd/skills and reach
	// .claude/skills via ClaudeSkillTargetForAsset. rdd-reverse-engineer and
	// rdd-audit were promoted into the package (its skills/rdd-audit ships
	// audit-citations.mjs at the canonical .modernpath/rdd path), so the
	// package claims their .claude/skills paths too. The two ModernPath
	// tooling skills below are not process and remain current.
	// rdd-ledger stays current as the project-local compatibility adapter for
	// the tasks/ ledger format until the F6 ledger->server import; 13 ledger
	// headers root themselves in it.
	"assets/skills/rdd-ledger/SKILL.md":          ".claude/skills/rdd-ledger/SKILL.md",
	"assets/skills/mp-knowledge-search/SKILL.md": ".claude/skills/mp-knowledge-search/SKILL.md",
	"assets/hooks/rdd-gate.sh":                   ".claude/hooks/rdd-gate.sh",
}

// legacyInstallTargets records the old Claude-specific location of assets
// that are not part of the recursive process tree. Install removes only files
// it previously owned; client-created files under .claude/rdd are preserved.
var legacyInstallTargets = map[string]string{
	"assets/rdd-source.txt": ".claude/rdd/.source",
}

// retiredInstallTargets are tool-owned files from released package layouts
// that cannot be derived from the current embedded tree. Keep this list
// explicit so an upgrade removes stale manuals without deleting unknown client
// files from either namespace.
var retiredInstallTargets = []string{
	// The consolidated package replaced the process/ manuals and the work
	// templates with PROCESS.md, file-state/ and skills/. These paths are no
	// longer derivable from the embedded tree, so without listing them an
	// upgrade leaves the superseded manuals installed beside the new ones and
	// every agent reads two contradictory process definitions.
	".modernpath/rdd/process/V-model-loop.md",
	".modernpath/rdd/process/state-tracking.md",
	".modernpath/rdd/process/prompts.md",
	".modernpath/rdd/templates/work/BACKLOG.md",
	".modernpath/rdd/templates/work/EPIC.md",
	".modernpath/rdd/templates/work/PROGRESS.md",
	".modernpath/rdd/templates/work/REQUIREMENTS.md",
	".modernpath/rdd/templates/work/TASK.md",
	".modernpath/rdd/templates/work/TECHNICAL-RECONNAISSANCE.md",
	".modernpath/rdd/templates/work/WORKLIST.md",
	// The workspace-authored skills the consolidated package supersedes. Their
	// SKILL.md files are retired; rdd-verify's, rdd-audit's, and
	// rdd-reverse-engineer's paths are re-claimed by the package skills of the
	// same names — the live-target subtraction below keeps those. The legacy
	// audit-citations.mjs location is retired outright: the package ships the
	// script beside skills/rdd-audit, so its canonical installed path is
	// .modernpath/rdd/skills/rdd-audit/audit-citations.mjs and the old copy
	// would otherwise sit stale beside the new skill forever.
	".claude/skills/rdd-build-loop/SKILL.md",
	".claude/skills/rdd-planning/SKILL.md",
	".claude/skills/rdd-discovery/SKILL.md",
	".claude/skills/rdd-verify/SKILL.md",
	".claude/skills/rdd-reverse-engineer/audit-citations.mjs",
	".modernpath/rdd/compat/PROCESS.md",
	".modernpath/rdd/compat/platform.md",
	".modernpath/rdd/process/interview-flows.md",
	".modernpath/rdd/templates/agents/CLAUDE.md",
	".modernpath/rdd/templates/agents/CONTEXT_AGENTS.md",
	".modernpath/rdd/templates/agents/PROJECT_AGENTS.md",
	".modernpath/rdd/templates/docs/00-overview.md",
	".modernpath/rdd/templates/docs/01-bounded-contexts.md",
	".modernpath/rdd/templates/docs/DOMAIN_DOC.md",
	".modernpath/rdd/templates/docs/data/40-data-model.md",
	".modernpath/rdd/templates/docs/data/41-event-catalog.md",
	".modernpath/rdd/templates/docs/gap-register.md",
	".modernpath/rdd/templates/docs/open-questions.md",
	".modernpath/rdd/PROCESS.md",
	".modernpath/rdd/V-model-loop.md",
	".modernpath/rdd/interview-flows.md",
	".modernpath/rdd/platform.md",
	".modernpath/rdd/prompts.md",
	".modernpath/rdd/state-tracking.md",
	".modernpath/rdd/templates/BACKLOG.md",
	".modernpath/rdd/templates/CLAUDE.md",
	".modernpath/rdd/templates/CONTEXT_AGENTS.md",
	".modernpath/rdd/templates/DOMAIN_DOC.md",
	".modernpath/rdd/templates/EPIC.md",
	".modernpath/rdd/templates/PROGRESS.md",
	".modernpath/rdd/templates/PROJECT_AGENTS.md",
	".modernpath/rdd/templates/REQUIREMENTS.md",
	".modernpath/rdd/templates/TASK.md",
	".modernpath/rdd/templates/WORKLIST.md",
	".claude/rdd/PROCESS.md",
	".claude/rdd/V-model-loop.md",
	".claude/rdd/interview-flows.md",
	".claude/rdd/platform.md",
	".claude/rdd/prompts.md",
	".claude/rdd/state-tracking.md",
	".claude/rdd/templates/BACKLOG.md",
	".claude/rdd/templates/CLAUDE.md",
	".claude/rdd/templates/CONTEXT_AGENTS.md",
	".claude/rdd/templates/DOMAIN_DOC.md",
	".claude/rdd/templates/EPIC.md",
	".claude/rdd/templates/PROGRESS.md",
	".claude/rdd/templates/PROJECT_AGENTS.md",
	".claude/rdd/templates/REQUIREMENTS.md",
	".claude/rdd/templates/TASK.md",
	".claude/rdd/templates/WORKLIST.md",
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
	Merged        []string // pre-existing client files whose managed block was refreshed
	Created       []string // client-ownable files that did not exist and were created
	Removed       []string // retired tool-owned files removed from legacy locations
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
	createdTargets := map[string]bool{}
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
		createdTargets[target] = created
		if target == "AGENTS.md" {
			res.AgentsCreated, res.AgentsMerged = created, !created
		}
	}

	assetPaths, err := ListAssets()
	if err != nil {
		return res, fmt.Errorf("list embedded assets: %w", err)
	}
	for _, asset := range assetPaths {
		targets := make([]string, 0, 2)
		if target, owned := TargetForAsset(asset); owned {
			targets = append(targets, target)
		}
		if target, owned := ClaudeSkillTargetForAsset(asset); owned {
			targets = append(targets, target)
		}
		if len(targets) == 0 {
			continue
		}
		body, err := assets.ReadFile(asset)
		if err != nil {
			return res, fmt.Errorf("read embedded %s: %w", asset, err)
		}
		for _, target := range targets {
			dest := filepath.Join(root, target)
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return res, fmt.Errorf("create %s: %w", filepath.Dir(target), err)
			}
			mode := os.FileMode(0o644)
			if strings.HasSuffix(target, ".sh") || strings.HasSuffix(target, ".mjs") {
				mode = 0o755
			}
			if err := os.WriteFile(dest, body, mode); err != nil {
				return res, fmt.Errorf("write %s: %w", target, err)
			}
			// WriteFile applies mode only on creation; converge pre-existing
			// files (a 0644 copy from an older layout) to the intended mode.
			if err := os.Chmod(dest, mode); err != nil {
				return res, fmt.Errorf("chmod %s: %w", target, err)
			}
			res.Written = append(res.Written, target)
		}
	}

	for target, body := range planned {
		dest := filepath.Join(root, target)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return res, fmt.Errorf("create %s: %w", filepath.Dir(target), err)
		}
		if err := os.WriteFile(dest, body, 0o644); err != nil {
			return res, fmt.Errorf("write %s: %w", target, err)
		}
		if createdTargets[target] {
			res.Created = append(res.Created, target)
		} else {
			res.Merged = append(res.Merged, target)
		}
	}

	// Retire only known tool-owned files after their replacements and all
	// managed entry points have been written. Unknown client files are left
	// alone in both .claude/rdd and .modernpath/rdd.
	retired, err := RetiredTargets()
	if err != nil {
		return res, fmt.Errorf("list retired process files: %w", err)
	}
	for _, target := range retired {
		err := os.Remove(filepath.Join(root, target))
		switch {
		case err == nil:
			res.Removed = append(res.Removed, target)
		case os.IsNotExist(err):
		default:
			return res, fmt.Errorf("remove retired %s: %w", target, err)
		}
		if err := pruneEmptyParents(root, target); err != nil {
			return res, fmt.Errorf("remove empty directories for retired %s: %w", target, err)
		}
	}

	return res, nil
}

func pruneEmptyParents(root, target string) error {
	stop := filepath.Clean(root)
	for dir := filepath.Dir(filepath.Join(stop, target)); dir != stop; dir = filepath.Dir(dir) {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if len(entries) != 0 {
			return nil
		}
		if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// Assets exposes the embedded tree so callers can diff what is installed
// against what would be installed, without re-running a write.
func Assets() fs.FS { return assets }

// TargetForAsset returns the repository-relative destination of an embedded
// asset, and whether it is one the kit owns outright.
func TargetForAsset(asset string) (string, bool) {
	clean := strings.TrimPrefix(asset, "./")
	if relative, ok := strings.CutPrefix(clean, "assets/rdd/"); ok && relative != "" {
		return path.Join(".modernpath/rdd", relative), true
	}
	target, ok := installTargets[clean]
	return target, ok
}

// ClaudeSkillTargetForAsset returns the .claude/skills destination for a
// process-package skill, and whether the asset is one. Package skills install
// twice on purpose: the canonical copy under .modernpath/rdd/skills (the
// tool-neutral path every harness reads) and a byte-identical copy under
// .claude/skills, which is the only location Claude Code discovers skills
// from. Check compares both, so the copies cannot drift silently.
func ClaudeSkillTargetForAsset(asset string) (string, bool) {
	clean := strings.TrimPrefix(asset, "./")
	relative, ok := strings.CutPrefix(clean, "assets/rdd/skills/")
	if !ok {
		return "", false
	}
	name, file, ok := strings.Cut(relative, "/")
	if !ok || name == "" || file != "SKILL.md" {
		return "", false
	}
	return path.Join(".claude/skills", name, "SKILL.md"), true
}

// LegacyTargetForAsset returns the old Claude-specific destination for a
// process asset. It exists only to migrate installations made before the
// shared package moved to the tool-neutral .modernpath/rdd path.
func LegacyTargetForAsset(asset string) (string, bool) {
	clean := strings.TrimPrefix(asset, "./")
	if relative, ok := strings.CutPrefix(clean, "assets/rdd/"); ok && relative != "" {
		return path.Join(".claude/rdd", relative), true
	}
	target, ok := legacyInstallTargets[clean]
	return target, ok
}

// RetiredTargets lists process files an upgrade may remove. Sorted and
// repository-relative. Unknown files are never included.
func RetiredTargets() ([]string, error) {
	assetPaths, err := ListAssets()
	if err != nil {
		return nil, err
	}
	targets := make(map[string]struct{})
	for _, asset := range assetPaths {
		if target, ok := LegacyTargetForAsset(asset); ok {
			targets[target] = struct{}{}
		}
	}
	for _, target := range retiredInstallTargets {
		targets[target] = struct{}{}
	}
	// A path retired by one layout can be claimed again by a later one. Since
	// retirement runs after the write pass, a name collision would delete the
	// file this very install just produced — silently, and reported as success.
	// A live target always wins over a retired one.
	for _, asset := range assetPaths {
		if target, ok := TargetForAsset(asset); ok {
			delete(targets, target)
		}
		if target, ok := ClaudeSkillTargetForAsset(asset); ok {
			delete(targets, target)
		}
	}
	for _, target := range mergeTargets {
		delete(targets, target)
	}
	out := make([]string, 0, len(targets))
	for target := range targets {
		out = append(out, target)
	}
	sort.Strings(out)
	return out, nil
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

// Check reports which kit-owned files differ from what this binary would
// install — edited, or missing entirely. The installed process is a released
// snapshot, so process changes belong in req-driven-dev and arrive through a
// new CLI build rather than edits to generated files.
//
// Check compares only the kit-namespaced files. Files a client co-owns are
// deliberately excluded: their content is supposed to differ.
func Check(root string) ([]string, error) {
	var drift []string
	assetPaths, err := ListAssets()
	if err != nil {
		return nil, fmt.Errorf("list embedded assets: %w", err)
	}
	for _, asset := range assetPaths {
		targets := make([]string, 0, 2)
		if target, owned := TargetForAsset(asset); owned {
			targets = append(targets, target)
		}
		if target, owned := ClaudeSkillTargetForAsset(asset); owned {
			targets = append(targets, target)
		}
		if len(targets) == 0 {
			continue
		}
		want, err := assets.ReadFile(asset)
		if err != nil {
			return nil, fmt.Errorf("read embedded %s: %w", asset, err)
		}
		for _, target := range targets {
			got, err := os.ReadFile(filepath.Join(root, target))
			if os.IsNotExist(err) || (err == nil && string(got) != string(want)) {
				drift = append(drift, target)
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", target, err)
			}
		}
	}
	// Managed blocks are generated content too: an edit inside one is silently
	// reverted by the next install, and a CLI release that changes only the
	// adapter blocks would otherwise produce no drift signal at all. Only the
	// marker-delimited region is compared — the rest of these files belongs to
	// the client and is supposed to differ.
	for asset, target := range mergeTargets {
		block, err := assets.ReadFile(asset)
		if err != nil {
			return nil, fmt.Errorf("read embedded %s: %w", asset, err)
		}
		got, err := os.ReadFile(filepath.Join(root, target))
		if os.IsNotExist(err) {
			drift = append(drift, target)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", target, err)
		}
		doc := string(got)
		if MarkersDamaged(doc) {
			drift = append(drift, target+" (managed block markers damaged)")
			continue
		}
		begin := strings.Index(doc, BeginMarker)
		end := strings.Index(doc, EndMarker)
		want := BeginMarker + "\n" + strings.Trim(string(block), "\n") + "\n" + EndMarker
		if begin < 0 || doc[begin:end+len(EndMarker)] != want {
			drift = append(drift, target+" (managed block)")
		}
	}
	sort.Strings(drift)
	return drift, nil
}
