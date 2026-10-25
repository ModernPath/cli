package cmd

import (
	"encoding/json"
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

// Epic 322: a batch refusal names the failing item so the operator can fix the input.
func TestRefusalTextBatchItem(t *testing.T) {
	t.Run("a batch refusal prints the failing index and item", func(t *testing.T) {
		const id = "11111111-1111-4111-8111-111111111111"
		var body map[string]any
		fixture := `{"error":{"code":"requirement_owned","message":"the requirement is owned by active Epic work"},"index":2,"item":{"kind":"system","id":"` + id + `"}}`
		if err := json.Unmarshal([]byte(fixture), &body); err != nil {
			t.Fatal(err)
		}
		out := refusalText(body)
		if !strings.Contains(out, "requirement_owned — item 2 (system "+id+")") {
			t.Fatalf("refusalText = %q; want the code, index and item", out)
		}
		if !strings.Contains(out, "the requirement is owned by active Epic work") {
			t.Fatalf("refusalText = %q; want the server message", out)
		}
		err := serverRefusal("requirement obsoletion refused", 409, body)
		want := "requirement obsoletion refused (server 409): requirement_owned — item 2 (system " + id + ")"
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("serverRefusal = %q; want %q", err.Error(), want)
		}
	})
}
