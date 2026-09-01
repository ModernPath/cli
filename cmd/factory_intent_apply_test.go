package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type intentApplyKeyFixture struct {
	SystemID                  int    `json:"system_id"`
	IntentExternalID          string `json:"intent_external_id"`
	ProcessRevision           string `json:"process_revision"`
	ContentFingerprint        string `json:"content_fingerprint"`
	EvaluatedScopeFingerprint string `json:"evaluated_scope_fingerprint"`
	IdempotencyKey            string `json:"idempotency_key"`
}

func TestTaskCH8009FactoryPullApplySendsExactTypedRequestAndSharedKey(t *testing.T) {
	fixture := loadIntentApplyKeyFixture(t)
	root := workspaceWithRDDSource(t, "revision="+fixture.ProcessRevision+"\n")

	var gotPath string
	var gotBody map[string]any
	var legacyAck bool
	server := intentApplyServer(t, fixture, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/sync/jobs":
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}})
		case strings.HasSuffix(r.URL.Path, "/apply"):
			gotPath = r.URL.Path
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Errorf("decode apply body: %v", err)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"application": map[string]any{"application_state": "applied"},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/ack"):
			legacyAck = true
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"acked": true}})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})

	env := &factoryEnv{Root: root, APIURL: server.URL, SystemID: fixture.SystemID, token: "t"}
	if err := factoryPullRun(env, true); err != nil {
		t.Fatalf("factory pull --apply: %v", err)
	}

	wantPath := "/api/v1/sync/intents/" + fixture.IntentExternalID + "/apply"
	if gotPath != wantPath {
		t.Fatalf("apply path = %q, want %q", gotPath, wantPath)
	}
	wantBody := map[string]any{
		"system_id":                            float64(fixture.SystemID),
		"workspace_process_revision":           fixture.ProcessRevision,
		"idempotency_key":                      fixture.IdempotencyKey,
		"expected_content_fingerprint":         fixture.ContentFingerprint,
		"expected_evaluated_scope_fingerprint": fixture.EvaluatedScopeFingerprint,
	}
	if !reflect.DeepEqual(gotBody, wantBody) {
		t.Fatalf("apply body = %#v, want %#v", gotBody, wantBody)
	}
	if legacyAck {
		t.Fatal("factory pull --apply must not use the legacy ACK as application")
	}
}

func TestTaskCH8009FactoryPullApplyRefusesMissingMalformedOrAmbiguousWorkspaceRevision(t *testing.T) {
	fixture := loadIntentApplyKeyFixture(t)
	cases := map[string]*string{
		"missing source":     nil,
		"missing revision":   ptr("repository=https://example.test/rdd\n"),
		"short revision":     ptr("revision=abc123\n"),
		"uppercase revision": ptr("revision=" + strings.Repeat("A", 40) + "\n"),
		"ambiguous revision": ptr("revision=" + strings.Repeat("a", 40) + "\nrevision=" + strings.Repeat("b", 40) + "\n"),
	}

	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if source != nil {
				writeRDDSource(t, root, *source)
			}

			var mutationCalled bool
			server := intentApplyServer(t, fixture, func(w http.ResponseWriter, r *http.Request) {
				mutationCalled = true
				json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}})
			})

			env := &factoryEnv{Root: root, APIURL: server.URL, SystemID: fixture.SystemID, token: "t"}
			err := factoryPullRun(env, true)
			if err == nil || !strings.Contains(err.Error(), ".modernpath/rdd/.source") {
				t.Fatalf("error = %v, want exact workspace source repair", err)
			}
			if mutationCalled {
				t.Fatal("invalid workspace process revision must refuse before mutation")
			}
		})
	}
}

func intentApplyServer(t *testing.T, fixture intentApplyKeyFixture, mutation http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/intents", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"intents": []any{map[string]any{
			"kind":                        "rdd_pending_intent",
			"external_id":                 fixture.IntentExternalID,
			"content_fingerprint":         fixture.ContentFingerprint,
			"evaluated_scope_fingerprint": fixture.EvaluatedScopeFingerprint,
			"title":                       "Confirm candidate",
			"answer":                      "Confirm",
			"source_tag":                  "USER:2026-08-27",
		}}}})
	})
	mux.HandleFunc("/", mutation)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func workspaceWithRDDSource(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	writeRDDSource(t, root, body)
	return root
}

func writeRDDSource(t *testing.T, root, body string) {
	t.Helper()
	path := filepath.Join(root, ".modernpath", "rdd", ".source")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func loadIntentApplyKeyFixture(t *testing.T) intentApplyKeyFixture {
	t.Helper()
	path := filepath.Join("testdata", "intent-apply-key.json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture intentApplyKeyFixture
	if err := json.Unmarshal(body, &fixture); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return fixture
}

func ptr(value string) *string { return &value }
