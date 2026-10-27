package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Epic 329 (A6), test case e449463f, criterion 706a4d59: `backlog create`
// accepts a record of the kind assumption, an assumed default, whose title is
// the default the agent chose and whose observation is the question it
// answers. Without a question it is refused before any request, with a
// message that names the question. The help of `backlog create` names the
// kind.

const (
	a6AssumedDefaultID    = "33333333-3333-4333-8333-333333333333"
	a6AssumedDefaultTitle = "Show the points balance on the receipt"
	a6AssumedDefaultAsked = "Should the receipt show the points balance after the move to Winter?"
)

func TestA6E449463FBacklogCreateAcceptsAnAssumedDefaultWithItsQuestion(t *testing.T) {
	calls := 0
	var sent map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/api/v1/backlog" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&sent)
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"backlog": map[string]any{
			"id": a6AssumedDefaultID, "system_id": 4, "kind": "assumption",
			"title": a6AssumedDefaultTitle, "observation": a6AssumedDefaultAsked,
			"disposition": "OPEN", "display_number": 1, "display_ref": "Assumed default 1",
		}}})
	}))
	defer server.Close()
	input, err := json.Marshal(map[string]any{"kind": "assumption", "title": a6AssumedDefaultTitle, "observation": a6AssumedDefaultAsked})
	if err != nil {
		t.Fatal(err)
	}
	cmd := newBacklogCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	cmd.SetArgs([]string{"create", "--file", "-", "--json"})
	cmd.SetIn(bytes.NewReader(input))
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("backlog create must accept the kind assumption with its question in observation, got: %v", err)
	}
	if calls != 1 {
		t.Fatalf("backlog create sent %d requests, want 1", calls)
	}
	if sent["kind"] != "assumption" || sent["title"] != a6AssumedDefaultTitle || sent["observation"] != a6AssumedDefaultAsked || sent["system_id"] != float64(4) {
		t.Errorf("the request must carry the kind assumption, the default as title, the question as observation and the bound System, got %#v", sent)
	}
	if !strings.Contains(out.String(), a6AssumedDefaultID) || !strings.Contains(out.String(), "Assumed default 1") || !strings.Contains(out.String(), "assumption") {
		t.Errorf("the receipt must name the record, its reference and its kind:\n%s", out.String())
	}
}

func TestA6E449463FBacklogCreateRefusesAnAssumedDefaultWithoutItsQuestion(t *testing.T) {
	for _, input := range []string{
		`{"kind":"assumption","title":"Show the points balance on the receipt"}`,
		`{"kind":"assumption","title":"Show the points balance on the receipt","observation":"  "}`,
	} {
		cmd := newBacklogCommand(func() (*cliEnv, error) {
			t.Fatalf("an assumed default without its question loaded the workspace: %s", input)
			return nil, nil
		})
		cmd.SetArgs([]string{"create", "--file", "-"})
		cmd.SetIn(strings.NewReader(input))
		err := cmd.Execute()
		if err == nil {
			t.Errorf("accepted an assumed default without its question: %s", input)
			continue
		}
		if !strings.Contains(strings.ToLower(err.Error()), "question") {
			t.Errorf("the refusal of an assumed default without its question must name the question, got %q for %s", err.Error(), input)
		}
	}
}

func TestA6E449463FBacklogCreateHelpNamesTheKindAssumption(t *testing.T) {
	cmd := newBacklogCommand(func() (*cliEnv, error) { t.Fatal("help loaded the workspace"); return nil, nil })
	cmd.SetArgs([]string{"create", "--help"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	help := strings.Join(strings.Fields(out.String()), " ")
	if !strings.Contains(help, "Create a discovery from a JSON object") {
		t.Fatalf("backlog create --help printed no create help: %q", help)
	}
	if !strings.Contains(help, "assumption") {
		t.Errorf("the help of backlog create must name the kind assumption: %q", help)
	}
}
