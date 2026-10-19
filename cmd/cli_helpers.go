package cmd

import (
	"os/exec"
	"strings"
)

func printGapWarnings(warnings []string) {
	for _, w := range warnings {
		printWarning("%s", w)
	}
}

func dataOf(decoded map[string]any) map[string]any {
	if d, ok := decoded["data"].(map[string]any); ok {
		return d
	}
	return map[string]any{}
}

func str(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func gitOut(root string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func stringSlice(v any) []string {
	items, _ := v.([]any)
	out := []string{}
	for _, it := range items {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return "—"
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
