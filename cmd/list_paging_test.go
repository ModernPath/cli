package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// REQ-CROSS-447 (EPIC-CLI-TURNS): the lists that can return a whole system's
// records print at most --limit (default 50) in text, with --offset to page
// and a footer that says how to see more; --json prints every record unless
// --limit is given, in the shape it has today.

func listServer(t *testing.T, backlog, gates, findings int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/backlog", func(w http.ResponseWriter, r *http.Request) {
		rows := []any{}
		for i := 0; i < backlog; i++ {
			rows = append(rows, map[string]any{"external_id": fmt.Sprintf("BACKLOG-T-%03d", i), "kind": "tooling",
				"disposition": "OPEN", "raised_at": fmt.Sprintf("2026-09-%02dT00:00:00Z", 1+i%28), "title": "t"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"backlog": rows}})
	})
	mux.HandleFunc("/api/v1/sync/gates", func(w http.ResponseWriter, r *http.Request) {
		rows := []any{}
		for i := 0; i < gates; i++ {
			rows = append(rows, map[string]any{"external_id": fmt.Sprintf("G-%03d", i), "kind": "question", "title": "t", "state": "open"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"gates": rows}})
	})
	mux.HandleFunc("/api/v1/sync/findings", func(w http.ResponseWriter, r *http.Request) {
		rows := []any{}
		for i := 0; i < findings; i++ {
			rows = append(rows, map[string]any{"external_id": fmt.Sprintf("F-%03d", i), "disposition": "OPEN",
				"category": "scope", "severity": "minor", "inserted_at": fmt.Sprintf("2026-09-01T00:00:%02dZ", i%60)})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"findings": rows}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func countLines(out, prefix string) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			n++
		}
	}
	return n
}

func TestREQCROSS447BacklogListPagesItsText(t *testing.T) {
	cobraWorkspace(t, listServer(t, 120, 0, 0))

	out, err := runRoot(t, "process", "backlog", "list")
	if err != nil {
		t.Fatalf("backlog list: %v", err)
	}
	if n := countLines(out, "BACKLOG-T-"); n != 50 {
		t.Errorf("text defaults to 50 rows, printed %d", n)
	}
	if !strings.Contains(out, "showing 50 of 120 · --offset 50 for more") {
		t.Errorf("the footer names the total and the next offset:\n%s", out)
	}

	out, _ = runRoot(t, "process", "backlog", "list", "--limit", "10", "--offset", "110")
	if n := countLines(out, "BACKLOG-T-"); n != 10 {
		t.Errorf("--limit 10 --offset 110 prints the last 10, printed %d", n)
	}
	if !strings.Contains(out, "showing 10 of 120") || strings.Contains(out, "for more") {
		t.Errorf("the last page names the total and no further offset:\n%s", out)
	}

	out, _ = runRoot(t, "process", "backlog", "list", "--json")
	var env struct {
		Backlog []any `json:"backlog"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil || len(env.Backlog) != 120 {
		t.Errorf("--json prints every record under backlog, got %d (%v)", len(env.Backlog), err)
	}
}

func TestREQCROSS447ShortListsPrintNoPagingHint(t *testing.T) {
	cobraWorkspace(t, listServer(t, 3, 3, 3))
	for _, args := range [][]string{
		{"process", "backlog", "list"},
		{"factory", "gates"},
		{"process", "findings", "list", "--all"},
	} {
		out, err := runRoot(t, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if strings.Contains(out, "showing") || strings.Contains(out, "--offset") {
			t.Errorf("%v: fewer records than the limit print no paging hint:\n%s", args, out)
		}
	}
}

func TestREQCROSS447FactoryGatesPagesTextNotJSON(t *testing.T) {
	cobraWorkspace(t, listServer(t, 0, 60, 0))

	out, err := runRoot(t, "factory", "gates")
	if err != nil {
		t.Fatalf("factory gates: %v", err)
	}
	if n := countLines(out, "G-"); n != 50 {
		t.Errorf("the open queue text defaults to 50 gates, printed %d", n)
	}
	if !strings.Contains(out, "showing 50 of 60 · --offset 50 for more") {
		t.Errorf("the footer names the total and the next offset:\n%s", out)
	}

	out, _ = runRoot(t, "factory", "gates", "--state", "all", "--limit", "5", "--offset", "5")
	if !strings.Contains(out, "showing 5 of 60 · --offset 10 for more") {
		t.Errorf("--limit and --offset page the history too:\n%s", out)
	}

	out, _ = runRoot(t, "factory", "gates", "--state", "all", "--json")
	var env struct {
		Gates []any `json:"gates"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil || len(env.Gates) != 60 {
		t.Errorf("--json keeps every gate in its shape, got %d (%v)", len(env.Gates), err)
	}
	out, _ = runRoot(t, "factory", "gates", "--json", "--limit", "7")
	if err := json.Unmarshal([]byte(out), &env); err != nil || len(env.Gates) != 7 {
		t.Errorf("--json honours an explicit --limit, got %d (%v)", len(env.Gates), err)
	}
}

func TestREQCROSS447FindingsListPagesItsText(t *testing.T) {
	cobraWorkspace(t, listServer(t, 0, 0, 70))

	out, err := runRoot(t, "process", "findings", "list", "--all")
	if err != nil {
		t.Fatalf("findings list: %v", err)
	}
	if n := countLines(out, "OPEN"); n != 50 {
		t.Errorf("text defaults to 50 findings, printed %d", n)
	}
	if !strings.Contains(out, "showing 50 of 70 · --offset 50 for more") {
		t.Errorf("the footer names the total and the next offset:\n%s", out)
	}
	out, _ = runRoot(t, "process", "findings", "list", "--all", "--offset", "50")
	if n := countLines(out, "OPEN"); n != 20 {
		t.Errorf("--offset 50 prints the remaining 20, printed %d", n)
	}
}
