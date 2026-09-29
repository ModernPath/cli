package cmd

import (
	"encoding/json"
	"testing"
)

// REQ-CROSS-447 (PR #694 review, minor): a --json page says whether records
// remain beyond it, so a script can tell a short list from a cut one.
func TestREQCROSS447JSONPageSaysWhetherMoreRemain(t *testing.T) {
	cobraWorkspace(t, listServer(t, 12, 12, 0))

	for _, args := range [][]string{
		{"process", "backlog", "list", "--json", "--limit", "10"},
		{"factory", "gates", "--json", "--limit", "10"},
	} {
		out, err := runRoot(t, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		var env map[string]any
		if err := json.Unmarshal([]byte(out), &env); err != nil {
			t.Fatalf("%v: not JSON: %v\n%s", args, err, out)
		}
		if env["has_more"] != true || env["total"] != float64(12) {
			t.Errorf("%v: a cut page carries has_more true and the total, got has_more=%v total=%v", args, env["has_more"], env["total"])
		}
	}

	out, err := runRoot(t, "process", "backlog", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if env["has_more"] != false {
		t.Errorf("the whole list carries has_more false, got %v", env["has_more"])
	}
	if rows, _ := env["backlog"].([]any); len(rows) != 12 {
		t.Errorf("the rows keep their key, got %v", env)
	}
}
