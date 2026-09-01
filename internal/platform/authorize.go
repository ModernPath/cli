package platform

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modernpath/cli/internal/zitadel"
)

// ErrTokenMissingProjectAudience is returned when the stored credential is a
// JWT whose audience does not contain the platform project id the target host
// requires. Callers surface it; they do not send the request.
var ErrTokenMissingProjectAudience = errors.New("stored token lacks the platform project audience")

// Authorize attaches the bearer token to req, and is the only place in the CLI
// that sets an Authorization header — `cmd/platform_wiring_test.go` fails on
// any other site.
//
// One place, because on a platform host the token is checked locally first: a
// JWT issued before the CLI began requesting the project-audience scope
// (REQ-CROSS-291) is refused by the edge and by core, and a refusal the CLI
// can predict is better spent on an instruction to re-authenticate than on a
// round trip that returns an unexplained 401. Fifteen call sites attached the
// bearer themselves before this helper existed, so a check on any one path
// would have covered none of the others.
//
// The check is deliberately narrow. It refuses only what it can read and knows
// to be wrong: a token that does not parse as a JWT — an opaque `--token`
// value, a legacy Guardian token — is attached unchanged and the edge decides,
// and a profile that declares no project id expects nothing, so it refuses
// nothing. An empty token attaches no header, matching what the call sites did
// before. It is a convenience check, not authorization: the edge and core
// still validate every token in full.
func Authorize(req *http.Request, token string) error {
	if req == nil {
		return nil
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	if token == "" {
		return nil
	}

	if profile, ok := profileFor(req.URL); ok && profile.ProjectID != "" {
		aud, isJWT := jwtAudience(token)
		if isJWT && !containsString(aud, profile.ProjectID) {
			return fmt.Errorf("%w for %s: run `modernpath auth --sso` again to get a token this host accepts",
				ErrTokenMissingProjectAudience, req.URL.Host)
		}
	}

	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

// profileFor returns the baked-in profile whose API host u addresses.
// buildPlatformOrigins keeps the profile rather than only the origin, because
// the expected project id is a per-profile value — prod and test are different
// Zitadel instances with different projects.
func profileFor(u *url.URL) (zitadel.Profile, bool) {
	if u == nil || u.Host == "" {
		return zitadel.Profile{}, false
	}
	p, ok := platformOrigins[u.Scheme+"://"+u.Host]
	return p, ok
}

// SetPlatformOriginsForTest replaces the platform-origin set for the duration
// of a test and returns a function that restores it.
//
// platformOrigins is an unexported fixed set derived from the baked-in
// profiles — correct for production, and the reason no `httptest` server can
// ever be a platform host. A command-level test of the refusal needs one that
// is, so this seam exists. Use it only from _test.go files.
func SetPlatformOriginsForTest(origins map[string]zitadel.Profile) func() {
	previous := platformOrigins
	replacement := make(map[string]zitadel.Profile, len(origins))
	for origin, profile := range origins {
		replacement[origin] = profile
	}
	platformOrigins = replacement
	return func() { platformOrigins = previous }
}

// jwtAudience reads the `aud` claim out of a JWT payload without verifying
// anything. It reports false when the token is not a readable JWT, which is
// the signal to attach it unchanged.
//
// No signature check happens here on purpose: this is a local convenience
// read, and the parties that must not trust an unverified token — the edge and
// core — both validate it in full.
func jwtAudience(token string) ([]string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}

	var claims struct {
		Aud json.RawMessage `json:"aud"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, false
	}
	if len(claims.Aud) == 0 {
		return nil, true
	}

	// RFC 7519 §4.1.3: aud is an array, or a single string in the common case.
	var many []string
	if err := json.Unmarshal(claims.Aud, &many); err == nil {
		return many, true
	}
	var one string
	if err := json.Unmarshal(claims.Aud, &one); err == nil {
		return []string{one}, true
	}
	return nil, true
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
