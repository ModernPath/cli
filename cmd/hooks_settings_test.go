package cmd

import (
	"os"
	"strings"
	"testing"
)

func TestWriteSettingsFileFallsBackToSortedKeysForANewFile(t *testing.T) {
	path := t.TempDir() + "/settings.json"
	if err := writeSettingsFile(path, map[string]interface{}{"zeta": 1, "alpha": "x > y"}); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	text := string(out)
	if strings.Index(text, `"alpha"`) > strings.Index(text, `"zeta"`) {
		t.Fatalf("new keys are not sorted:\n%s", text)
	}
	if !strings.Contains(text, "x > y") {
		t.Fatalf("value was escaped:\n%s", text)
	}
	if !strings.HasSuffix(text, "}\n") {
		t.Fatalf("file does not end with a newline:\n%q", text)
	}
}
