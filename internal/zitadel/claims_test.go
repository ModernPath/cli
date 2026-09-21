package zitadel

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// REQ-CROSS-335 — a local, unverified read of the claims the CLI needs to
// decide about a workspace: the issuer, the home organization and the
// organization the platform claim names. Verification is core's job; this
// read only ever refuses to store what would be wrong.

// unsignedJWT builds a three-part token whose signature is not checked by
// anything in this package.
func unsignedJWT(t *testing.T, payload map[string]any) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	return enc(header) + "." + enc(body) + "." + enc([]byte("sig"))
}

func TestTokenClaimsReadsTheIssuerHomeAndActingOrganization(t *testing.T) {
	token := unsignedJWT(t, map[string]any{
		"iss":                                   "https://id.example.test",
		"urn:zitadel:iam:user:resourceowner:id": "org-home",
		"urn:modernpath:token:v2":               map[string]any{"org_id": "org-b", "roles": []string{"platform.user"}},
	})

	claims, ok := TokenClaims(token)
	if !ok {
		t.Fatal("a readable JWT must be reported as one")
	}
	if claims.Issuer != "https://id.example.test" || claims.HomeOrganizationID != "org-home" || claims.OrganizationID != "org-b" {
		t.Fatalf("claims = %+v, want issuer, home org-home and acting org-b", claims)
	}
}

func TestTokenClaimsWithoutThePlatformClaimNamesNoOrganization(t *testing.T) {
	token := unsignedJWT(t, map[string]any{"iss": "https://id.example.test", "sub": "u1"})

	claims, ok := TokenClaims(token)
	if !ok {
		t.Fatal("a readable JWT must be reported as one")
	}
	if claims.OrganizationID != "" || claims.HomeOrganizationID != "" {
		t.Fatalf("claims = %+v, want no organization", claims)
	}
	if claims.Issuer != "https://id.example.test" {
		t.Fatalf("claims = %+v, want the issuer still read", claims)
	}
}

func TestTokenClaimsRefusesWhatIsNotAJWT(t *testing.T) {
	for _, token := range []string{"", "opaque-token", "a.b", "a.b.c"} {
		if _, ok := TokenClaims(token); ok {
			t.Errorf("%q must not read as a JWT", token)
		}
	}
}

// REQ-CROSS-388 — the same local read also yields who the token was issued
// to and when it expires, so `auth status` can name the actor and the expiry
// without a round trip.
func TestTokenClaimsReadsSubjectEmailAndExpiry(t *testing.T) {
	token := unsignedJWT(t, map[string]any{
		"iss":   "https://id.example.test",
		"sub":   "user-42",
		"email": "jane@example.com",
		"exp":   1_800_000_000,
	})

	claims, ok := TokenClaims(token)
	if !ok {
		t.Fatal("a readable JWT must be reported as one")
	}
	if claims.Subject != "user-42" || claims.Email != "jane@example.com" {
		t.Fatalf("claims = %+v, want subject user-42 and email jane@example.com", claims)
	}
	if claims.ExpiresAt.Unix() != 1_800_000_000 {
		t.Fatalf("ExpiresAt = %v, want the exp claim as a time", claims.ExpiresAt)
	}
}

// A token without exp reads as a zero expiry, never as "expired now".
func TestTokenClaimsWithoutExpIsZero(t *testing.T) {
	claims, ok := TokenClaims(unsignedJWT(t, map[string]any{"iss": "https://id.example.test"}))
	if !ok || !claims.ExpiresAt.IsZero() {
		t.Fatalf("claims = %+v ok=%v, want a zero ExpiresAt", claims, ok)
	}
}
