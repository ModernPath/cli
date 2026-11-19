package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCase 5e0d3130-9214-4132-8ad2-3ccad15a8575
func TestEpicGatesCheckTextPrintsTheCausesOfABlockerOnTheNextLine(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"ready":false,"skills":["rdd-plan"],"blockers":[
			{"code":"plan_review_required","message":"The plan review is out of date","reason":"review_outdated","review_id":"r1","causes":["stored_trace","verification"],"trace_ids":["t1","t2"]},
			{"code":"material_review_finding","message":"Material finding Y is unresolved"}]}}`))
	}))
	defer server.Close()
	run := func(args ...string) string {
		t.Helper()
		command := newEpicGatesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 105), nil })
		command.SetArgs(append([]string{"check", "287", "--phase", "plan-ready", "--git-sha", strings.Repeat("a", 40)}, args...))
		var out bytes.Buffer
		command.SetOut(&out)
		command.SetErr(&bytes.Buffer{})
		command.SilenceUsage = true
		if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "unresolved plan-ready conditions") {
			t.Fatalf("an unready gate must fail with its phase: %v", err)
		}
		return out.String()
	}

	text := run()
	want := "- plan_review_required: The plan review is out of date\n  causes: stored_trace, verification\n- material_review_finding: Material finding Y is unresolved\nRDD skills:"
	if !strings.Contains(text, want) {
		t.Fatalf("the text has no causes line after the blocker that has causes, or changed a blocker without them:\n%s", text)
	}
	if strings.Count(text, "causes:") != 1 {
		t.Fatalf("a blocker without causes must print as before:\n%s", text)
	}
	if strings.Contains(text, "trace_ids") || strings.Contains(text, "t1") {
		t.Fatalf("the text output must not print trace_ids:\n%s", text)
	}

	var report struct {
		Blockers []map[string]any `json:"blockers"`
	}
	machine := run("--json")
	if err := json.Unmarshal([]byte(machine), &report); err != nil || len(report.Blockers) != 2 {
		t.Fatalf("json output: %s %v", machine, err)
	}
	causes, _ := json.Marshal(report.Blockers[0]["causes"])
	traces, _ := json.Marshal(report.Blockers[0]["trace_ids"])
	if string(causes) != `["stored_trace","verification"]` || string(traces) != `["t1","t2"]` {
		t.Fatalf("--json must pass causes and trace_ids through as served: %s", machine)
	}
}
