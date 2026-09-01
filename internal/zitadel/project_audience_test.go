package zitadel

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// REQ-CROSS-291 — the device flow must ask for the platform project audience,
// or the issued token's `aud` is the CLI client id alone and neither the edge
// (oidc-bff `expected_audience`) nor core (`check_accepted_audience`) accepts
// it.

func TestProjectAudienceScope_Shape(t *testing.T) {
	got := projectAudienceScope("123456")
	want := "urn:zitadel:iam:org:project:id:123456:aud"
	if got != want {
		t.Errorf("projectAudienceScope = %q, want %q", got, want)
	}
}

func TestLoginScopes_IncludeProjectAudience(t *testing.T) {
	scopes := loginScopes(Profile{ProjectID: "123456"})

	for _, base := range []string{"openid", "profile", "email", "offline_access"} {
		if !contains(scopes, base) {
			t.Errorf("scopes %v missing the existing scope %q", scopes, base)
		}
	}
	if !contains(scopes, "urn:zitadel:iam:org:project:id:123456:aud") {
		t.Errorf("scopes %v missing the project-audience scope", scopes)
	}
}

// A profile with no project id yet (EPIC-CLI-006 D2 is unanswered) must ask
// for exactly what it asks for today — never a malformed
// `...:project:id::aud`, which Zitadel would reject and which would break the
// login that works now.
func TestLoginScopes_NoProjectID_UnchangedFromToday(t *testing.T) {
	scopes := loginScopes(Profile{})

	if len(scopes) != 4 {
		t.Errorf("scopes = %v, want exactly the four existing scopes", scopes)
	}
	for _, s := range scopes {
		if strings.Contains(s, "urn:zitadel:iam:org:project:id:") {
			t.Errorf("scopes %v contain a project-audience scope with no project id", scopes)
		}
	}
}

// Both baked-in profiles must name their project, or the CLI asks for the four
// scopes it always asked for and every authenticated call is refused at the
// edge — the failure this requirement exists to remove. Prod and test are
// separate ZITADEL instances, so a shared value would mean one of them is
// wrong.
func TestBakedInProfilesCarryDistinctProjectIDs(t *testing.T) {
	if ProdProfile.ProjectID == "" {
		t.Error("ProdProfile.ProjectID is empty")
	}
	if TestProfile.ProjectID == "" {
		t.Error("TestProfile.ProjectID is empty")
	}
	if ProdProfile.ProjectID == TestProfile.ProjectID {
		t.Errorf("both profiles carry project id %q — prod and test are different instances", ProdProfile.ProjectID)
	}
}

// The project id belongs in the scope the device flow sends, so a profile
// change that forgets to reach login is caught here rather than at a 401.
func TestBakedInProfilesRequestTheirOwnProjectAudience(t *testing.T) {
	for _, p := range []Profile{ProdProfile, TestProfile} {
		want := projectAudienceScope(p.ProjectID)
		if !contains(loginScopes(p), want) {
			t.Errorf("%s: login scopes %v missing %q", p.Issuer, loginScopes(p), want)
		}
	}
}

// The scope has to reach the wire, not just the scope list: `Scopes:` is one
// line in Login's oauth2 config and dropping it would leave every test above
// green while the issued token again carries the client id alone.
func TestLoginSendsProjectAudienceScopeOnTheWire(t *testing.T) {
	var sentScope string

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                        srv.URL,
			"authorization_endpoint":        srv.URL + "/authorize",
			"token_endpoint":                srv.URL + "/token",
			"device_authorization_endpoint": srv.URL + "/device_authorization",
		})
	})
	mux.HandleFunc("/device_authorization", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse device-authorization form: %v", err)
		}
		sentScope = r.Form.Get("scope")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "FAKE-DEVICE-CODE",
			"user_code":        "FAKE-USER-CODE",
			"verification_uri": srv.URL + "/device",
			"expires_in":       30,
			"interval":         1,
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "AT-1", "refresh_token": "RT-1",
			"token_type": "Bearer", "expires_in": 3600,
		})
	})

	var out bytes.Buffer
	profile := Profile{
		Issuer:    srv.URL,
		ClientID:  "test-client",
		APIURL:    "https://cloud.example.test",
		ProjectID: "424242",
	}
	if _, err := Login(context.Background(), profile, &out); err != nil {
		t.Fatalf("Login: %v", err)
	}

	want := "urn:zitadel:iam:org:project:id:424242:aud"
	if !strings.Contains(sentScope, want) {
		t.Errorf("device-authorization scope = %q, want it to contain %q", sentScope, want)
	}
	for _, base := range []string{"openid", "profile", "email", "offline_access"} {
		if !strings.Contains(sentScope, base) {
			t.Errorf("device-authorization scope = %q, missing the existing scope %q", sentScope, base)
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
