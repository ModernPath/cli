package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Slugify converts a human-readable name into a filesystem-safe slug.
func Slugify(name string) string {
	slug := strings.ToLower(strings.TrimSpace(name))
	slug = strings.ReplaceAll(slug, " ", "-")

	var result strings.Builder
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}
	slug = strings.Trim(result.String(), "-")
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	return slug
}

// InitiativeSpecsDirName returns the folder name for an epic workspace, e.g. "157-simplify-roles-and-navigation".
func InitiativeSpecsDirName(initiativeID int, title string) string {
	slug := Slugify(title)
	if slug == "" {
		return fmt.Sprintf("%d", initiativeID)
	}
	return fmt.Sprintf("%d-%s", initiativeID, slug)
}

// NormalizeEpicWorkspaceRelPath rewrites legacy specs/<slug> paths to tasks/<slug>.
func NormalizeEpicWorkspaceRelPath(rel string) string {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" {
		return rel
	}
	if strings.HasPrefix(rel, "specs/") {
		return "tasks/" + strings.TrimPrefix(rel, "specs/")
	}
	return rel
}

// InitiativeSpecsRelPath returns the epic workspace path relative to .modernpath,
// e.g. "tasks/157-simplify-roles-and-navigation". Spec category subfolders and
// task context .md files both live under this directory.
func InitiativeSpecsRelPath(initiativeID int, title string) string {
	return filepath.ToSlash(filepath.Join("tasks", InitiativeSpecsDirName(initiativeID, title)))
}

// LegacyInitiativeSpecsRelPath is the pre-unification layout under .modernpath/specs/.
func LegacyInitiativeSpecsRelPath(initiativeID int, title string) string {
	return filepath.ToSlash(filepath.Join("specs", InitiativeSpecsDirName(initiativeID, title)))
}

// EpicWorkspaceAbsDir returns the absolute path to an epic workspace folder.
func EpicWorkspaceAbsDir(initiativeID int, title string) (string, error) {
	configDir, err := GetConfigDir(true)
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, filepath.FromSlash(InitiativeSpecsRelPath(initiativeID, title))), nil
}

// ClearEpicSpecCategoryDirs removes category subfolders from an epic workspace
// without deleting task context .md files at the workspace root.
func ClearEpicSpecCategoryDirs(epicDir string) error {
	entries, err := os.ReadDir(epicDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			if err := os.RemoveAll(filepath.Join(epicDir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// ResolveInitiativeSpecsDir returns the absolute path to the active epic workspace directory.
func ResolveInitiativeSpecsDir(cfg *Config) (string, error) {
	configDir, err := GetConfigDir(false)
	if err != nil {
		return "", err
	}
	if configDir == "" {
		return "", fmt.Errorf("config directory not found")
	}

	if cfg.InitiativeID > 0 && cfg.InitiativeName != "" {
		canonicalRel := InitiativeSpecsRelPath(cfg.InitiativeID, cfg.InitiativeName)
		canonical := filepath.Join(configDir, filepath.FromSlash(canonicalRel))
		if _, err := os.Stat(canonical); err == nil {
			return canonical, nil
		}

		legacyRel := LegacyInitiativeSpecsRelPath(cfg.InitiativeID, cfg.InitiativeName)
		legacy := filepath.Join(configDir, filepath.FromSlash(legacyRel))
		if _, err := os.Stat(legacy); err == nil {
			return legacy, nil
		}

		if HasLegacyFlatSpecsLayout(configDir) {
			return filepath.Join(configDir, "specs"), nil
		}
		return canonical, nil
	}

	if cfg.InitiativeSpecsDir != "" {
		normalized := NormalizeEpicWorkspaceRelPath(cfg.InitiativeSpecsDir)
		candidate := filepath.Join(configDir, filepath.FromSlash(normalized))
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		if HasLegacyFlatSpecsLayout(configDir) {
			return filepath.Join(configDir, "specs"), nil
		}
		return candidate, nil
	}

	return filepath.Join(configDir, "tasks"), nil
}

// ResolveInitiativeSpecsRelPath returns the display path relative to .modernpath for the active epic.
func ResolveInitiativeSpecsRelPath(cfg *Config) string {
	if cfg.InitiativeID > 0 && cfg.InitiativeName != "" {
		return InitiativeSpecsRelPath(cfg.InitiativeID, cfg.InitiativeName)
	}
	if cfg.InitiativeSpecsDir != "" {
		return NormalizeEpicWorkspaceRelPath(cfg.InitiativeSpecsDir)
	}
	return "tasks"
}

// HasLegacyFlatSpecsLayout reports whether specs categories live directly under .modernpath/specs/.
func HasLegacyFlatSpecsLayout(configDir string) bool {
	specsRoot := filepath.Join(configDir, "specs")
	entries, err := os.ReadDir(specsRoot)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.Contains(name, "-") && len(name) > 0 && name[0] >= '0' && name[0] <= '9' {
			continue
		}
		return true
	}

	return false
}
