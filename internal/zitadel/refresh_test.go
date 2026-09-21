package zitadel

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// REQ-CROSS-389 — a stored refresh token is exchanged at the issuer's token
// endpoint for a new pair, through the oauth2 client and never through the
// API-host wiring.
func TestRefreshExchangesTheRefreshTokenAtTheIssuer(t *testing.T) {
	const issuer = "https://id.example.test"
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 issuer,
			"authorization_endpoint": issuer + "/oauth/v2/authorize",
			"token_endpoint":         issuer + "/oauth/v2/token",
		})
	})
	var grant, presented, clientID string
	mux.HandleFunc("/oauth/v2/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		grant, presented, clientID = r.Form.Get("grant_type"), r.Form.Get("refresh_token"), r.Form.Get("client_id")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "at-2",
			"refresh_token": "rt-2",
			"token_type":    "Bearer",
			"expires_in":    3600,
			"id_token":      unsignedJWT(t, map[string]any{"sub": "u1", "email": "jane@example.com"}),
		})
	})

	tok, err := Refresh(withInMemoryIssuer(context.Background(), mux), Profile{Issuer: issuer, ClientID: "cli-client"}, "rt-1")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if grant != "refresh_token" || presented != "rt-1" || clientID != "cli-client" {
		t.Fatalf("token request = grant %q refresh %q client %q, want refresh_token / rt-1 / cli-client", grant, presented, clientID)
	}
	if tok.AccessToken != "at-2" || tok.RefreshToken != "rt-2" || tok.Flow != FlowRefresh {
		t.Fatalf("token = %+v, want at-2 / rt-2 / refresh", tok)
	}
	if tok.Expiry.IsZero() || tok.IDToken == "" {
		t.Fatalf("token must carry the expiry and the ID token, got %+v", tok)
	}
}

func TestProfileForIssuerFindsTheBakedInPlanes(t *testing.T) {
	for _, p := range []Profile{ProdProfile, TestProfile} {
		got, ok := ProfileForIssuer(p.Issuer)
		if !ok || got.APIURL != p.APIURL {
			t.Errorf("ProfileForIssuer(%q) = %+v ok=%v, want the profile for %s", p.Issuer, got, ok, p.APIURL)
		}
	}
	if _, ok := ProfileForIssuer("https://nobody.example.test"); ok {
		t.Error("an unknown issuer must not resolve")
	}
}
