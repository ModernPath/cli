package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// docPushManifestEntry matches one row in docs_push_manifest.json (from Core.Export).
type docPushManifestEntry struct {
	Tier  string `json:"tier"`
	Angle string `json:"angle"`
}

type docPushManifest struct {
	Version              int                             `json:"version"`
	SystemDocFiles       map[string]docPushManifestEntry `json:"system_doc_files"`
	ArchitectureDocFiles map[string]docPushManifestEntry `json:"architecture_doc_files"` // backward compat
}

func modernpathReservedTopDir(name string) bool {
	switch strings.ToLower(name) {
	case "viewer", "memories", "specs", "artifacts", "organization", "patterns", "workflows", "scripts", "datamodel":
		return true
	default:
		return false
	}
}

func hasSystemRootMarkers(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "docs_push_manifest.json")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "blueprint.json")); err == nil {
		return true
	}
	return false
}

// resolveSystemRootDir returns the directory for {slug} under .modernpath (flat or legacy docs/{slug}/).
func resolveSystemRootDir(modernpathRoot, slug string) (string, bool) {
	flat := filepath.Join(modernpathRoot, slug)
	if hasSystemRootMarkers(flat) {
		return flat, true
	}
	legacy := filepath.Join(modernpathRoot, "docs", slug)
	if hasSystemRootMarkers(legacy) {
		return legacy, true
	}
	return flat, false
}

// listSystemExportSlugs finds system roots: .modernpath/{slug}/ or .modernpath/docs/{slug}/.
func listSystemExportSlugs(modernpathRoot string) []string {
	seen := map[string]bool{}
	var slugs []string
	add := func(slug string, dir string) {
		if slug == "" || seen[slug] || modernpathReservedTopDir(slug) {
			return
		}
		if !hasSystemRootMarkers(dir) {
			return
		}
		seen[slug] = true
		slugs = append(slugs, slug)
	}

	if entries, err := os.ReadDir(modernpathRoot); err == nil {
		for _, e := range entries {
			if !e.IsDir() || modernpathReservedTopDir(e.Name()) {
				continue
			}
			add(e.Name(), filepath.Join(modernpathRoot, e.Name()))
		}
	}
	legacyDocs := filepath.Join(modernpathRoot, "docs")
	if entries, err := os.ReadDir(legacyDocs); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			add(e.Name(), filepath.Join(legacyDocs, e.Name()))
		}
	}
	return slugs
}

// docPushLookupKey strips a leading docs/ segment from walk-relative paths so manifest keys match export layout.
func docPushLookupKey(rel string) string {
	p := filepath.ToSlash(strings.TrimSpace(rel))
	if strings.HasPrefix(p, "docs/") {
		return p[len("docs/"):]
	}
	return p
}

// exportPathForDocPush returns the path relative to the system root (inside .modernpath/<slug>/),
// e.g. "architecture/auth/flow.md", for API import alignment. Empty if slug is unknown or path is outside slug.
func exportPathForDocPush(lookupKey, systemSlug string) string {
	if systemSlug == "" {
		return ""
	}
	p := filepath.ToSlash(strings.TrimSpace(lookupKey))
	prefix := filepath.ToSlash(systemSlug) + "/"
	if len(p) >= len(prefix) && strings.EqualFold(p[:len(prefix)], prefix) {
		return p[len(prefix):]
	}
	return ""
}

func shouldSkipDocPushSubtree(rel string) bool {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" || rel == "." {
		return false
	}
	first, _, _ := strings.Cut(rel, "/")
	return modernpathReservedTopDir(first)
}

// loadMergedDocPushManifests reads every system root's docs_push_manifest.json and merges keys as "{slug}/{rel}".
func loadMergedDocPushManifests(modernpathRoot string) map[string]docPushManifestEntry {
	out := map[string]docPushManifestEntry{}
	for _, slug := range listSystemExportSlugs(modernpathRoot) {
		root, ok := resolveSystemRootDir(modernpathRoot, slug)
		if !ok {
			continue
		}
		manifestPath := filepath.Join(root, "docs_push_manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}
		var m docPushManifest
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		// Prefer new key, fall back to legacy key
		docFiles := m.SystemDocFiles
		if docFiles == nil {
			docFiles = m.ArchitectureDocFiles
		}
		if docFiles == nil {
			continue
		}
		for rel, te := range docFiles {
			key := filepath.ToSlash(filepath.Join(slug, rel))
			out[key] = te
		}
	}
	return out
}

func shouldSkipDocPush(rel string) bool {
	p := filepath.ToSlash(rel)
	lower := strings.ToLower(p)

	switch {
	case strings.HasSuffix(lower, "/blueprint.md"):
		return true
	case strings.Contains(lower, "/capabilities/"):
		return true
	case strings.Contains(lower, "/patterns/"):
		return true
	case strings.HasSuffix(lower, "/subsystems/index.md"):
		return true
	case strings.HasSuffix(lower, "/architecture/index.md"):
		return true
	}
	return false
}

func isLikelyArchitectureAngleDir(seg string) bool {
	switch strings.ToLower(strings.TrimSpace(seg)) {
	case "overview", "code_structure", "data_models", "data_model", "interfaces", "api",
		"security", "deployment", "use_cases", "general", "design_system", "tech_stack",
		"patterns", "navigation":
		return true
	default:
		return false
	}
}

func inferFromArchitectureTree(ar []string) (tier, angle string) {
	tier, angle = "architecture", ""
	if len(ar) == 0 {
		return tier, angle
	}
	if len(ar) == 1 && strings.EqualFold(ar[0], "INDEX.md") {
		return "architecture", "navigation"
	}

	if len(ar) >= 2 && ar[0] == "_legacy" && len(ar) >= 3 {
		return ar[1], ar[2]
	}

	if len(ar) >= 2 && ar[0] == "_unscoped" {
		switch ar[1] {
		case "subsystem":
			if len(ar) == 3 && strings.HasSuffix(strings.ToLower(ar[2]), ".md") {
				return "subsystem", ""
			}
			if len(ar) >= 4 {
				return "subsystem", ar[2]
			}
		case "module":
			if len(ar) == 3 && strings.HasSuffix(strings.ToLower(ar[2]), ".md") {
				return "module", ""
			}
			if len(ar) >= 4 {
				return "module", ar[2]
			}
		}
	}

	// Global / architecture-scoped code: architecture/code/<angle>/…
	if len(ar) >= 2 && ar[0] == "code" {
		return "code", ar[1]
	}

	if len(ar) >= 5 && ar[1] == "modules" {
		if len(ar) == 5 && strings.EqualFold(ar[4], "overview.md") {
			return "module", "overview"
		}
		return "module", ar[3]
	}

	if len(ar) == 4 && ar[1] == "modules" && strings.EqualFold(ar[3], "overview.md") {
		return "module", "overview"
	}

	// Legacy subsystem-scoped code: <ss>/code/<angle>/file
	if len(ar) >= 4 && ar[1] == "code" {
		return "code", ar[2]
	}

	if len(ar) == 2 && strings.EqualFold(ar[1], "overview.md") {
		return "subsystem", "overview"
	}

	if len(ar) == 2 {
		if isLikelyArchitectureAngleDir(ar[0]) {
			return "architecture", ar[0]
		}
		return "subsystem", ""
	}

	if len(ar) >= 3 {
		return "subsystem", ar[1]
	}

	return tier, angle
}

// inferTierAngleFromExportPath derives tier/angle from paths relative to a system slug (e.g. my-app/architecture/...),
// plus legacy layouts: docs/ prefix, architectures/ prefix, subsystems/, nested docs/.
func inferTierAngleFromExportPath(rel string) (tier, angle string) {
	tier, angle = "architecture", ""
	p := docPushLookupKey(filepath.ToSlash(strings.TrimSpace(rel)))
	if p == "" {
		return tier, angle
	}

	parts := strings.Split(p, "/")
	if len(parts) >= 2 && parts[0] == "architectures" {
		parts = parts[1:]
		if len(parts) == 0 {
			return tier, angle
		}
	}

	if len(parts) == 1 {
		switch strings.ToLower(parts[0]) {
		case "readme.md":
			return "architecture", "overview"
		case "design-system.md":
			return "architecture", "design_system"
		case "tech-stack.md":
			return "architecture", "tech_stack"
		}
		return tier, angle
	}

	rest := parts[1:]
	if len(rest) == 1 {
		switch strings.ToLower(rest[0]) {
		case "readme.md":
			return "architecture", "overview"
		case "design-system.md":
			return "architecture", "design_system"
		case "tech-stack.md":
			return "architecture", "tech_stack"
		}
	}

	if len(rest) >= 2 && strings.EqualFold(rest[0], "instructions") {
		return "instructions", rest[1]
	}

	if len(rest) >= 2 && rest[0] == "architecture" {
		return inferFromArchitectureTree(rest[1:])
	}

	if len(rest) >= 4 && rest[0] == "docs" {
		switch rest[1] {
		case "architecture":
			return "architecture", rest[2]
		case "code":
			return "code", rest[2]
		case "_legacy":
			if len(rest) >= 4 {
				return rest[2], rest[3]
			}
		case "_unscoped":
			if len(rest) >= 4 {
				switch rest[2] {
				case "subsystem":
					return "subsystem", rest[3]
				case "module":
					return "module", rest[3]
				}
			}
		}

		knownTier := map[string]bool{
			"architecture": true, "subsystem": true, "module": true, "code": true,
			"instructions": true, "component": true,
		}
		if knownTier[rest[1]] && len(rest) >= 4 {
			return rest[1], rest[2]
		}
	}

	if len(rest) >= 2 && rest[0] == "subsystems" {
		if len(rest) == 2 && strings.EqualFold(rest[1], "INDEX.md") {
			return "architecture", "navigation"
		}
		if len(rest) == 3 && strings.EqualFold(rest[2], "overview.md") {
			return "subsystem", "overview"
		}

		if len(rest) >= 5 && rest[2] == "modules" {
			if len(rest) == 5 && strings.EqualFold(rest[4], "overview.md") {
				return "module", "overview"
			}
			if len(rest) >= 8 && rest[4] == "docs" && rest[5] == "code" {
				return "code", rest[6]
			}
			if len(rest) >= 7 && rest[4] == "docs" {
				return "module", rest[5]
			}
		}

		if len(rest) >= 6 && rest[2] == "docs" && rest[3] == "code" {
			return "code", rest[4]
		}
		if len(rest) >= 5 && rest[2] == "docs" {
			return "subsystem", rest[3]
		}
	}

	return scanPartsForTierAngleKeywords(parts)
}

func scanPartsForTierAngleKeywords(parts []string) (tier, angle string) {
	tier, angle = "architecture", ""
	for _, part := range parts {
		switch part {
		case "architecture", "subsystem", "module", "component", "code", "instructions":
			tier = part
		case "overview", "code_structure", "data_model", "api", "security", "deployment", "use_cases", "general":
			if angle == "" {
				angle = part
			}
		}
	}
	return tier, angle
}
