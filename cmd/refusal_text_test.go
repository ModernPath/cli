package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// REQ-CROSS-372 (EPIC-CLI-016): every server refusal reaches the operator as the
// server wrote it. Four rendering sites lost or rewrote the text on main:
// authorPost printed a 422 `details` map in Go map syntax; factoryAnswer replaced
// every 409 with a first-wins sentence and printed the answer 422 as a map;
// factoryEnv.call discarded an undecodable body so a proxy error printed <nil>;
// processSupersede dropped the body entirely. RED first on all four.

func TestREQCROSS372AuthorRefusalDetailsRenderAsFieldLines(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
			"details": map[string]any{
				"exact_scope": []string{"entry gate names EPIC-X but omits members still PROPOSED: REQ-X-1"},
				"transition":  []string{"an entry gate names the FROM->TO transition it authorizes"},
			},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	err := authorCreate(env, "gate", "ENTRY-X", map[string]any{"title": "Entry"})
	if err == nil {
		t.Fatal("a 422 must surface as an error")
	}
	msg := err.Error()
	if strings.Contains(msg, "map[") {
		t.Fatalf("details must render as field lines, not Go map syntax: %s", msg)
	}
	for _, want := range []string{"exact_scope: entry gate names EPIC-X", "transition: an entry gate names"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("details must render as `field: message` (%q); got: %s", want, msg)
		}
	}
	if strings.Index(msg, "exact_scope:") > strings.Index(msg, "transition:") {
		t.Fatalf("field lines must be sorted by field for a stable read; got: %s", msg)
	}
}

func TestREQCROSS372AnswerRefusalReasonIsPrintedVerbatim(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/gates/ENTRY-X", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"gate": map[string]any{
			"external_id": "ENTRY-X", "purpose": "entry",
			"content_fingerprint": "c1", "evaluated_scope_fingerprint": "s1",
		}}})
	})
	mux.HandleFunc("/api/v1/sync/gates/ENTRY-X/answer", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
			"reason": "the gate is superseded, not ANSWERED — open its successor instead",
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	err := factoryAnswer(env, "ENTRY-X", "approve", "", "USER:2026-09-12:t")
	if err == nil || !strings.Contains(err.Error(), "superseded, not ANSWERED") {
		t.Fatalf("a 409 reason must be printed verbatim, not replaced by the first-wins sentence; got: %v", err)
	}
}

func TestREQCROSS372AnswerRefusalDetailsRenderAsFieldLines(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/gates/ENTRY-Y", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"gate": map[string]any{
			"external_id": "ENTRY-Y", "purpose": "entry",
		}}})
	})
	mux.HandleFunc("/api/v1/sync/gates/ENTRY-Y/answer", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
			"details": map[string]any{"review": []string{"The prerequisite checks must all pass at the current revision before approval: TRACE-1 is stale, not pass."}},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	err := factoryAnswer(env, "ENTRY-Y", "approve", "", "")
	if err == nil {
		t.Fatal("a 422 must surface as an error")
	}
	if strings.Contains(err.Error(), "map[") || !strings.Contains(err.Error(), "review: The prerequisite checks") {
		t.Fatalf("the answer 422 must render its details as `field: message`; got: %v", err)
	}
}

func TestREQCROSS372AnUndecodableRefusalBodyIsPrintedRaw(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(503)
		w.Write([]byte("upstream connect error or disconnect/reset before headers"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	err := authorCreate(env, "gate", "ENTRY-Z", map[string]any{"title": "Entry"})
	if err == nil {
		t.Fatal("a 503 must surface as an error")
	}
	if strings.Contains(err.Error(), "<nil>") || !strings.Contains(err.Error(), "upstream connect error") {
		t.Fatalf("an undecodable body must be printed raw, never as <nil>; got: %v", err)
	}
}

func TestREQCROSS372SupersedeRefusalIsPrinted(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
			"reason": "REQ-Q-1 is DONE — a delivered item is superseded through its successor, not in place",
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	err := processSupersede(env, "REQ-Q-1", "because", "", "", false)
	if err == nil || !strings.Contains(err.Error(), "superseded through its successor") {
		t.Fatalf("the supersede refusal must be printed, not reduced to an HTTP code; got: %v", err)
	}
}

// The review block rides on the server's own signal, not on the client's copy of
// the governance rule: a gate whose answer_readiness says requires_review gets it
// even without a governed purpose (REQ-CROSS-354 later changes the predicate in
// one place). RED: the client keys on purpose alone.
func TestREQCROSS372AnswerAttachesTheReviewWhenTheServerRequiresIt(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/gates/D-GOV", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"gate": map[string]any{
			"external_id": "D-GOV", "purpose": nil, "transition": "PROPOSED->TODO",
			"content_fingerprint": "c9", "evaluated_scope_fingerprint": "s9",
			"answer_readiness": map[string]any{"ready": true, "reason": nil, "requires_review": true},
		}}})
	})
	mux.HandleFunc("/api/v1/sync/gates/D-GOV/answer", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"gate": map[string]any{
			"external_id": "D-GOV", "answer": "approve", "source_tag": "USER:x",
		}}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	if err := factoryAnswer(env, "D-GOV", "approve", "", "USER:x"); err != nil {
		t.Fatalf("answer failed: %v", err)
	}
	review, _ := got["review"].(map[string]any)
	if review["content_fingerprint"] != "c9" || review["evaluated_scope_fingerprint"] != "s9" {
		t.Fatalf("the review block must ride when the server says requires_review; got payload %v", got)
	}
}

// PR445-07 (cold code review): a decodable non-store body on a failure — a
// gateway's {"message": …} with no `error` key — must print its text, not Go
// map syntax. RED: refusalText falls through to fmt.Sprint(body).
func TestREQCROSS372ATopLevelGatewayMessageIsPrinted(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		json.NewEncoder(w).Encode(map[string]any{"message": "upstream request timeout"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	err := authorCreate(env, "gate", "ENTRY-GW", map[string]any{"title": "Entry"})
	if err == nil {
		t.Fatal("a 502 must surface as an error")
	}
	if strings.Contains(err.Error(), "map[") || !strings.Contains(err.Error(), "upstream request timeout") {
		t.Fatalf("a top-level message must print as text; got: %v", err)
	}
}

// REQ-CROSS-380 (EPIC-CLI-018): the CLI never prints an empty server error —
// a bodyless fault still names the status and the request reference the
// server sent, so the person can find it in the logs.
func TestREQCROSS380AnEmptyServerErrorCitesTheRequestReference(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-request-id", "req-1")
		w.WriteHeader(500)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	err := authorCreate(env, "gate", "ENTRY-Z", map[string]any{"title": "Entry"})
	if err == nil {
		t.Fatal("a 500 must surface as an error")
	}
	if strings.Contains(err.Error(), "<nil>") || !strings.Contains(err.Error(), "req-1") || !strings.Contains(err.Error(), "500") {
		t.Fatalf("an empty server error must cite the status and the request reference, never <nil>; got: %v", err)
	}
}

func TestREQCROSS380AReadRefusalNeverPrintsNil(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/items", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-request-id", "req-2")
		w.WriteHeader(500)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	err := workingSetPull(env, []string{"EPIC-Z"}, wsNow)
	if err == nil {
		t.Fatal("a 500 on the read must surface as an error")
	}
	if strings.Contains(err.Error(), "<nil>") || !strings.Contains(err.Error(), "req-2") {
		t.Fatalf("a bodyless read refusal must cite the reference, never <nil>; got: %v", err)
	}
}

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
