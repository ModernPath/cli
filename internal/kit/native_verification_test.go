package kit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerificationProcedureRequiresNativeProof(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, ".modernpath", "rdd", "skills", "rdd-reverse-engineer-verify", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, obligation := range []string{"TestCase", "executed TestCase", "required tests", "criterion", "actual outcome", "tenant/System"} {
		if !strings.Contains(string(body), obligation) {
			t.Errorf("verification procedure omits %s", obligation)
		}
	}
}
