package cmd

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
)

// REQ-CROSS-388 — `auth status` names the actor, the organization, the bound
// system and the token's expiry, and refuses by name with the remedy when the
// credential is missing, expired, or issued for a different server.

// jwtWithClaims builds an unsigned token from an arbitrary payload; only the
// payload is read locally.
func jwtWithClaims(t *testing.T, payload map[string]any) string {
	t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return enc(map[string]string{"alg": "RS256", "typ": "JWT"}) + "." + enc(payload) + ".not-a-real-signature"
}

var statusNow = time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

// (a) A stored credential with an actor and an expiry renders the actor, the
// organization, the bound system and the time left; --json carries the same.
func TestAuthStatusReportsActorSystemAndExpiry(t *testing.T) {
	expires := statusNow.Add(30 * time.Minute)
	auth := &config.Auth{
		Token:         jwtWithClaims(t, map[string]any{"sub": "u1", "email": "jane@example.com", "exp": expires.Unix()}),
		Actor:         "jane@example.com",
		ExpiresAt:     expires.Format(time.RFC3339),
		WorkspaceID:   "371594261646278691",
		WorkspaceName: "Acme",
		Issuer:        "https://id.example.test",
	}

	st := evaluateAuthStatusAt("https://api.example.test", 7, auth, true, statusNow)

	if !st.Authenticated {
		t.Fatalf("a credential 30 minutes from expiry must be authenticated offline, got %+v", st)
	}
	if st.Actor != "jane@example.com" {
		t.Errorf("Actor = %q, want the stored actor", st.Actor)
	}
	if st.SystemID != 7 {
		t.Errorf("SystemID = %d, want the bound system 7", st.SystemID)
	}
	if st.ExpiresAt != expires.Format(time.RFC3339) || st.ExpiresInSeconds != 1800 {
		t.Errorf("expiry = %q / %d s, want %s / 1800", st.ExpiresAt, st.ExpiresInSeconds, expires.Format(time.RFC3339))
	}

	var out []byte
	_ = captureStdout(t, func() error { renderAuthStatusHuman(st); return nil }, &out)
	human := string(out)
	for _, want := range []string{"Actor:", "jane@example.com", "Organization:", "Acme (371594261646278691)", "System:", "7", "Expires:", "in 30m"} {
		if !strings.Contains(human, want) {
			t.Errorf("human render lacks %q:\n%s", want, human)
		}
	}
}

// (b) An expired credential is refused offline, naming the expiry time and
// the sanctioned remedy — no network, no generic "present".
func TestAuthStatusOfflineRefusesExpiredCredential(t *testing.T) {
	expired := statusNow.Add(-time.Minute)
	auth := &config.Auth{
		Token:     jwtWithClaims(t, map[string]any{"sub": "u1", "exp": expired.Unix()}),
		ExpiresAt: expired.Format(time.RFC3339),
	}

	st := evaluateAuthStatusAt(config.DefaultAPIURL, 3, auth, true, statusNow)

	if st.Authenticated {
		t.Fatalf("an expired credential must not be authenticated, got %+v", st)
	}
	if st.Reason != "expired" {
		t.Errorf("Reason = %q, want expired", st.Reason)
	}
	if !strings.Contains(st.Detail, "expired at "+expired.Format(time.RFC3339)) {
		t.Errorf("detail must name the expiry time, got %q", st.Detail)
	}
	if remedy := authRepairCommand(config.DefaultAPIURL); !strings.Contains(st.Detail, remedy) {
		t.Errorf("detail must name the remedy %q, got %q", remedy, st.Detail)
	}
}

// A credential stored by an older build has no expires_at; the access token's
// own exp claim is the fallback.
func TestAuthStatusFallsBackToTheAccessTokenExp(t *testing.T) {
	expired := statusNow.Add(-time.Hour)
	auth := &config.Auth{Token: jwtWithClaims(t, map[string]any{"sub": "u1", "exp": expired.Unix()})}

	st := evaluateAuthStatusAt(config.DefaultAPIURL, 3, auth, true, statusNow)

	if st.Authenticated || st.Reason != "expired" {
		t.Fatalf("the exp claim must be read when expires_at is absent, got %+v", st)
	}
}

// (c) A token issued by the test plane's identity provider, stored under the
// production URL, is refused as issued for a different server, with the
// production remedy.
func TestAuthStatusRefusesTokenIssuedForAnotherServer(t *testing.T) {
	auth := &config.Auth{
		Token:  jwtWithClaims(t, map[string]any{"iss": zitadel.TestProfile.Issuer, "sub": "u1", "exp": statusNow.Add(time.Hour).Unix(), "aud": []string{zitadel.TestProfile.ProjectID}}),
		Issuer: zitadel.TestProfile.Issuer,
	}

	st := evaluateAuthStatusAt(zitadel.ProdProfile.APIURL, 3, auth, true, statusNow)

	if st.Authenticated {
		t.Fatalf("a test-plane token under the production URL must be refused, got %+v", st)
	}
	if st.Reason != "issued for a different server" {
		t.Errorf("Reason = %q, want issued for a different server", st.Reason)
	}
	if remedy := authRepairCommand(zitadel.ProdProfile.APIURL); !strings.Contains(st.Detail, remedy) {
		t.Errorf("detail must name the remedy %q, got %q", remedy, st.Detail)
	}
}

// A credential stored before the actor was recorded says so instead of
// printing a blank.
func TestAuthStatusNamesAnUnrecordedActor(t *testing.T) {
	auth := &config.Auth{Token: "opaque-but-present"}

	st := evaluateAuthStatusAt("https://api.example.test", 0, auth, true, statusNow)

	var out []byte
	_ = captureStdout(t, func() error { renderAuthStatusHuman(st); return nil }, &out)
	if !strings.Contains(string(out), "not recorded") {
		t.Errorf("human render must say the actor is not recorded:\n%s", out)
	}
	if !strings.Contains(string(out), "not bound") {
		t.Errorf("human render must say no system is bound:\n%s", out)
	}
}

// (e) A login result carrying an ID token and an expiry is stored with the
// actor and expires_at, so later reads need no claim parsing.
func TestSaveLoginRecordsActorAndExpiry(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	expires := statusNow.Add(12 * time.Hour)
	tok := &zitadel.Token{
		AccessToken:  platformJWT(t, testIssuer, "org-home", "org-home"),
		RefreshToken: "rt",
		IDToken:      jwtWithClaims(t, map[string]any{"sub": "u1", "email": "jane@example.com"}),
		Expiry:       expires,
		Flow:         zitadel.FlowLoopback,
	}

	if err := saveLogin(zitadel.TestProfile.APIURL, tok, workspaceChoice{Issuer: testIssuer}); err != nil {
		t.Fatalf("saveLogin: %v", err)
	}

	stored := readAuthFile(t, dir)
	if stored["actor"] != "jane@example.com" {
		t.Errorf("actor = %v, want the ID token's email", stored["actor"])
	}
	if stored["expires_at"] != expires.Format(time.RFC3339) {
		t.Errorf("expires_at = %v, want %s", stored["expires_at"], expires.Format(time.RFC3339))
	}
}

// A sign-in replaces the active credential and keeps the credentials stashed
// for the other identity providers, so `env --set` still switches back
// (F-CLI019-PR-03). The slot of the provider signed into is dropped.
func TestSaveLoginKeepsTheStashOfOtherProviders(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	prev := &config.Auth{
		Token: "old-test-token", Issuer: testIssuer,
		Stash: map[string]config.AuthMaterial{
			zitadel.ProdProfile.Issuer: {Token: "prod-token", Actor: "jane@example.com", Issuer: zitadel.ProdProfile.Issuer},
			testIssuer:                 {Token: "stale-test-token"},
		},
	}
	if err := config.WriteAuth(prev); err != nil {
		t.Fatalf("WriteAuth: %v", err)
	}
	tok := &zitadel.Token{
		AccessToken: platformJWT(t, testIssuer, "org-home", "org-home"),
		IDToken:     jwtWithClaims(t, map[string]any{"sub": "u1", "email": "jane@example.com"}),
		Expiry:      statusNow.Add(time.Hour),
		Flow:        zitadel.FlowLoopback,
	}
	if err := saveLogin(zitadel.TestProfile.APIURL, tok, workspaceChoice{Issuer: testIssuer}); err != nil {
		t.Fatalf("saveLogin: %v", err)
	}

	stored, err := config.ReadAuth()
	if err != nil {
		t.Fatalf("ReadAuth: %v", err)
	}
	if got := stored.Stash[zitadel.ProdProfile.Issuer].Token; got != "prod-token" {
		t.Errorf("the production credential must survive a test sign-in, got %q", got)
	}
	if _, still := stored.Stash[testIssuer]; still {
		t.Errorf("the slot of the provider signed into must be dropped, got %v", stored.Stash)
	}
}
