package zitadel

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
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

// A profile with no project id must never produce a malformed
// `...:project:id::aud`, which Zitadel would reject and which would break the
// login that works now. The exact scope count moved to REQ-CROSS-334's
// TestLoginScopesWithoutAProjectIDOmitOnlyTheProjectAudience.
func TestLoginScopes_NoProjectID_OmitsTheProjectAudience(t *testing.T) {
	scopes := loginScopes(Profile{})

	for _, s := range scopes {
		if strings.HasPrefix(s, "urn:zitadel:iam:org:project:id:") && s != zitadelAudienceScope {
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
	var mu sync.Mutex
	var sentScope string

	issuer := inMemoryIssuerURL
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                        issuer,
			"authorization_endpoint":        issuer + "/authorize",
			"token_endpoint":                issuer + "/token",
			"device_authorization_endpoint": issuer + "/device_authorization",
		})
	})
	mux.HandleFunc("/device_authorization", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse device-authorization form: %v", err)
		}
		mu.Lock()
		sentScope = r.Form.Get("scope")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "FAKE-DEVICE-CODE",
			"user_code":        "FAKE-USER-CODE",
			"verification_uri": issuer + "/device",
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

	profile := Profile{
		Issuer:    issuer,
		ClientID:  "test-client",
		APIURL:    "https://cloud.example.test",
		ProjectID: "424242",
	}
	synctest.Test(t, func(t *testing.T) {
		var out bytes.Buffer
		ctx := withInMemoryIssuer(context.Background(), mux)
		if _, err := DeviceLogin(ctx, profile, &out, Options{}); err != nil {
			t.Fatalf("Login: %v", err)
		}
	})

	mu.Lock()
	defer mu.Unlock()
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
