package cmd

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// Every server refusal reaches the operator through one formatter. A raw
// `%v` of body["error"] prints <nil> on an empty body and a Go map dump on a
// structured one; none may remain in the command package.
func TestREQCROSS380NoRawErrorFormatterRemains(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var offenders []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if strings.Contains(line, `["error"]`) && strings.Contains(line, "%v") {
				offenders = append(offenders, fmt.Sprintf("%s:%d", name, i+1))
			}
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("raw %%v of a server error body remains (route it through serverRefusal / env.refusal):\n%s", strings.Join(offenders, "\n"))
	}
}
