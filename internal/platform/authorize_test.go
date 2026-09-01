package platform

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/zitadel"
)

// REQ-CROSS-291 — one shared bearer-attach path, and the local pre-check that
// refuses a stored JWT lacking the platform project audience before it is sent
// to a host that would only 401 it.

const testProjectID = "111222333444555666"

// fakeJWT builds an unsigned token with the given audience. Authorize only
// reads the payload — the edge and core do the real validation — so a
// well-formed payload is all these tests need.
func fakeJWT(t *testing.T, aud any) string {
	t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	header := enc(map[string]string{"alg": "RS256", "typ": "JWT"})
	payload := enc(map[string]any{"sub": "cli-user", "aud": aud})
	return header + "." + payload + ".not-a-real-signature"
}

// withTestPlatformHost registers an arbitrary origin as a platform host for
// the duration of a test. platformOrigins is otherwise a fixed unexported set,
// so no test server could ever be one.
func withTestPlatformHost(t *testing.T, origin string, profile zitadel.Profile) {
	t.Helper()
	restore := SetPlatformOriginsForTest(map[string]zitadel.Profile{origin: profile})
	t.Cleanup(restore)
}

func newReq(t *testing.T, rawURL string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return req
}

func TestAuthorize_PlatformHost_JWTMissingProjectAudience_Refused(t *testing.T) {
	withTestPlatformHost(t, "https://api.example.test", zitadel.Profile{
		APIURL:    "https://api.example.test",
		ProjectID: testProjectID,
	})

	req := newReq(t, "https://api.example.test/api/systems")
	token := fakeJWT(t, []string{"some-cli-client-id"})

	err := Authorize(req, token)
	if err == nil {
		t.Fatal("Authorize returned nil, want a refusal for a token without the project audience")
	}
	if !errors.Is(err, ErrTokenMissingProjectAudience) {
		t.Errorf("error = %v, want ErrTokenMissingProjectAudience", err)
	}
	if !strings.Contains(err.Error(), "modernpath auth --sso") {
		t.Errorf("error = %q, want it to name the command that fixes it", err)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want unset — nothing may be sent", got)
	}
}

func TestAuthorize_PlatformHost_JWTWithProjectAudience_Attached(t *testing.T) {
	withTestPlatformHost(t, "https://api.example.test", zitadel.Profile{
		APIURL:    "https://api.example.test",
		ProjectID: testProjectID,
	})

	req := newReq(t, "https://api.example.test/api/systems")
	token := fakeJWT(t, []string{"some-cli-client-id", testProjectID})

	if err := Authorize(req, token); err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer "+token {
		t.Errorf("Authorization = %q, want the bearer attached", got)
	}
}

// A single-string aud is as valid as an array (RFC 7519 §4.1.3) and Zitadel
// emits both shapes.
func TestAuthorize_PlatformHost_StringAudience(t *testing.T) {
	withTestPlatformHost(t, "https://api.example.test", zitadel.Profile{
		APIURL:    "https://api.example.test",
		ProjectID: testProjectID,
	})

	req := newReq(t, "https://api.example.test/api/systems")
	if err := Authorize(req, fakeJWT(t, testProjectID)); err != nil {
		t.Fatalf("Authorize with a single-string aud: %v", err)
	}
	if req.Header.Get("Authorization") == "" {
		t.Error("Authorization unset, want the bearer attached")
	}
}

// AGENTS.md, "a guard flags, it does not delete": a token the check cannot
// read is not a token it may refuse. An opaque `--token` value and a legacy
// Guardian token both reach here.
func TestAuthorize_PlatformHost_NonJWTAttachedUnchanged(t *testing.T) {
	withTestPlatformHost(t, "https://api.example.test", zitadel.Profile{
		APIURL:    "https://api.example.test",
		ProjectID: testProjectID,
	})

	for _, token := range []string{"opaque-token-value", "not.a.jwt", ""} {
		req := newReq(t, "https://api.example.test/api/systems")
		if err := Authorize(req, token); err != nil {
			t.Errorf("Authorize(%q) = %v, want nil — an unreadable token is the edge's decision", token, err)
		}
		want := ""
		if token != "" {
			want = "Bearer " + token
		}
		if got := req.Header.Get("Authorization"); got != want {
			t.Errorf("Authorization for %q = %q, want %q", token, got, want)
		}
	}
}

// Everything that is not a platform host is untouched: beta, localhost, any
// --api-url. This is the no-regression clause.
func TestAuthorize_NonPlatformHost_AlwaysAttached(t *testing.T) {
	withTestPlatformHost(t, "https://api.example.test", zitadel.Profile{
		APIURL:    "https://api.example.test",
		ProjectID: testProjectID,
	})

	token := fakeJWT(t, []string{"some-cli-client-id"})
	for _, target := range []string{
		"https://beta.modernpath.ai/api/systems",
		"http://localhost:4000/api/systems",
		"http://127.0.0.1:4000/api/systems",
	} {
		req := newReq(t, target)
		if err := Authorize(req, token); err != nil {
			t.Errorf("Authorize(%s) = %v, want nil", target, err)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer "+token {
			t.Errorf("Authorization for %s = %q, want it attached unchanged", target, got)
		}
	}
}

// Until the project ids are known (EPIC-CLI-006 D2) a profile carries none,
// and a check with no expected value must not refuse anything.
func TestAuthorize_ProfileWithoutProjectID_DoesNotRefuse(t *testing.T) {
	withTestPlatformHost(t, "https://api.example.test", zitadel.Profile{
		APIURL: "https://api.example.test",
	})

	req := newReq(t, "https://api.example.test/api/systems")
	token := fakeJWT(t, []string{"some-cli-client-id"})

	if err := Authorize(req, token); err != nil {
		t.Fatalf("Authorize = %v, want nil when the profile declares no project id", err)
	}
	if req.Header.Get("Authorization") == "" {
		t.Error("Authorization unset, want the bearer attached")
	}
}

func TestProfileFor_ResolvesEachBakedInOrigin(t *testing.T) {
	for _, want := range []zitadel.Profile{zitadel.ProdProfile, zitadel.TestProfile} {
		req := newReq(t, want.APIURL+"/api/systems")
		got, ok := profileFor(req.URL)
		if !ok {
			t.Errorf("profileFor(%s) not found", want.APIURL)
			continue
		}
		if got.ClientID != want.ClientID {
			t.Errorf("profileFor(%s).ClientID = %q, want %q", want.APIURL, got.ClientID, want.ClientID)
		}
	}
}

func TestSetPlatformOriginsForTest_Restores(t *testing.T) {
	before := IsPlatformHost(zitadel.ProdProfile.APIURL)
	restore := SetPlatformOriginsForTest(map[string]zitadel.Profile{
		"https://api.example.test": {APIURL: "https://api.example.test"},
	})
	if IsPlatformHost(zitadel.ProdProfile.APIURL) {
		t.Error("the baked-in prod host is still a platform host while overridden")
	}
	if !IsPlatformHost("https://api.example.test") {
		t.Error("the override is not in effect")
	}
	restore()
	if IsPlatformHost(zitadel.ProdProfile.APIURL) != before {
		t.Error("restore did not put the baked-in origins back")
	}
}
