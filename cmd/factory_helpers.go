package cmd

import (
	"os"
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

func machineFingerprint(root string) string {
	host, _ := os.Hostname()
	return host + ":" + root
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
