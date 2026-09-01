//go:build chat008_red

package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestTaskCH8010ApplyRDDIntentReturnsCanonicalApplication(t *testing.T) {
	fixture := loadIntentApplyKeyFixture(t)
	root := workspaceWithRDDSource(t, "revision="+fixture.ProcessRevision+"\n")
	applicationRevision := "db:00000000-0000-0000-0000-000000000010"

	server := intentApplyServer(t, fixture, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/apply") {
			http.NotFound(w, r)
			return
		}

		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"application": map[string]any{
					"external_id":          fixture.IntentExternalID,
					"application_state":    "applied",
					"application_revision": applicationRevision,
				},
			},
		})
	})

	env := &factoryEnv{Root: root, APIURL: server.URL, SystemID: fixture.SystemID, token: "t"}
	intent := map[string]any{
		"external_id":                 fixture.IntentExternalID,
		"content_fingerprint":         fixture.ContentFingerprint,
		"evaluated_scope_fingerprint": fixture.EvaluatedScopeFingerprint,
	}

	application, err := applyRDDIntent(env, intent, "")
	if err != nil {
		t.Fatalf("apply typed intent: %v", err)
	}
	if got := str(application, "application_revision"); got != applicationRevision {
		t.Fatalf("application_revision = %q, want %q", got, applicationRevision)
	}
}
