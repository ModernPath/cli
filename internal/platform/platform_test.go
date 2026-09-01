// Covers REQ-CROSS-290, REQ-CROSS-291

package platform

import (
	"net/http"
	"testing"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
)

func prepared(t *testing.T, method, rawURL string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		t.Fatalf("NewRequest(%q): %v", rawURL, err)
	}
	Prepare(req)
	return req
}

// The platform Gateway's `ex-api` HTTPRoute matches PathPrefix /api/ex and
// rewrites it to /api. The CLI writes its routes against core's own /api
// prefix, so every core path must be moved onto /api/ex for that host — the
// same mapping modernpath-frontend's coreApiUrl performs.
func TestCorePathsMoveOntoThePlatformPrefixOnAPlatformHost(t *testing.T) {
	base := zitadel.ProdProfile.APIURL
	cases := map[string]string{
		"/api":                     "/api/ex",
		"/api/systems":             "/api/ex/systems",
		"/api/tech-profiles":       "/api/ex/tech-profiles",
		"/api/work/epics":          "/api/ex/work/epics",
		"/api/v1/sync/requirement": "/api/ex/v1/sync/requirement",
		"/api/v1/knw/documents":    "/api/ex/v1/knw/documents",
	}
	for path, want := range cases {
		got := prepared(t, http.MethodGet, base+path).URL.Path
		if got != want {
			t.Errorf("%s%s -> path %q, want %q", base, path, got, want)
		}
	}
}

func TestQueryStringSurvivesTheRewrite(t *testing.T) {
	req := prepared(t, http.MethodGet, zitadel.TestProfile.APIURL+"/api/v1/sync/gates?system_id=243&state=all")
	if got, want := req.URL.Path, "/api/ex/v1/sync/gates"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	if got, want := req.URL.RawQuery, "system_id=243&state=all"; got != want {
		t.Errorf("query = %q, want %q", got, want)
	}
}

// The oidc-bff proxy rejects any request to the platform host that does not
// carry the static header (proxy.go handleRequestHeaders step 0), so the CLI
// must send it for the same reason the SPA does.
func TestPlatformHostRequestsCarryTheStaticTokenHandlerHeader(t *testing.T) {
	for _, base := range []string{zitadel.ProdProfile.APIURL, zitadel.TestProfile.APIURL} {
		req := prepared(t, http.MethodGet, base+"/api/systems")
		if got := req.Header.Get(TokenHandlerHeader); got != TokenHandlerValue {
			t.Errorf("%s: %s = %q, want %q", base, TokenHandlerHeader, got, TokenHandlerValue)
		}
	}
}

// Every deployment that exists today — beta, localhost, an operator's
// --api-url — must produce byte-identical requests. Nothing about the
// platform host may leak onto them.
func TestNonPlatformHostsAreUntouched(t *testing.T) {
	for _, base := range []string{
		config.BetaAPIURL,
		config.LocalAPIURL,
		"https://legacy.example.test",
		"https://cloud.modernpath.ai", // the SPA host is not the API host
	} {
		req := prepared(t, http.MethodGet, base+"/api/systems")
		if got, want := req.URL.Path, "/api/systems"; got != want {
			t.Errorf("%s: path = %q, want %q untouched", base, got, want)
		}
		if got := req.Header.Get(TokenHandlerHeader); got != "" {
			t.Errorf("%s: must not carry %s, got %q", base, TokenHandlerHeader, got)
		}
	}
}

// Non-core paths on the platform host belong to other services (the agent's
// /oauth-agent/*, tenant-management's own prefix) and are never core's to
// rewrite.
func TestPathsOutsideCoreAreNotRewritten(t *testing.T) {
	base := zitadel.ProdProfile.APIURL
	for _, path := range []string{"/oauth-agent/claims", "/_health", "/apiary", "/"} {
		if got := prepared(t, http.MethodGet, base+path).URL.Path; got != path {
			t.Errorf("%s -> %q, want %q untouched", path, got, path)
		}
	}
}

// Prepare runs at the request-issuing boundary, and more than one helper may
// wrap the same request. Applying it twice must not yield /api/ex/ex/....
func TestPrepareIsIdempotent(t *testing.T) {
	req := prepared(t, http.MethodGet, zitadel.ProdProfile.APIURL+"/api/systems")
	Prepare(req)
	Prepare(req)
	if got, want := req.URL.Path, "/api/ex/systems"; got != want {
		t.Errorf("path after three Prepares = %q, want %q", got, want)
	}
}

// Core answers the health check at both `/_health` and `/api/_health`. Only
// the second survives the Gateway on the platform host, and only the first is
// what already-deployed servers answer, so the choice is per host.
func TestHealthPathIsChosenPerHost(t *testing.T) {
	for _, base := range []string{zitadel.ProdProfile.APIURL, zitadel.TestProfile.APIURL} {
		if got, want := HealthPath(base), "/api/_health"; got != want {
			t.Errorf("HealthPath(%q) = %q, want %q", base, got, want)
		}
		// ...and Prepare must carry it through to core's platform prefix.
		req := prepared(t, http.MethodGet, base+HealthPath(base))
		if got, want := req.URL.Path, "/api/ex/_health"; got != want {
			t.Errorf("%s: prepared health path = %q, want %q", base, got, want)
		}
	}

	for _, base := range []string{config.BetaAPIURL, config.LocalAPIURL, "https://legacy.example.test"} {
		if got, want := HealthPath(base), "/_health"; got != want {
			t.Errorf("HealthPath(%q) = %q, want %q", base, got, want)
		}
		req := prepared(t, http.MethodGet, base+HealthPath(base))
		if got, want := req.URL.Path, "/_health"; got != want {
			t.Errorf("%s: prepared health path = %q, want %q untouched", base, got, want)
		}
	}
}

func TestIsPlatformHost(t *testing.T) {
	yes := []string{zitadel.ProdProfile.APIURL, zitadel.TestProfile.APIURL, zitadel.ProdProfile.APIURL + "/api/systems"}
	no := []string{config.BetaAPIURL, config.LocalAPIURL, "https://cloud.modernpath.ai", "", "::not a url"}
	for _, u := range yes {
		if !IsPlatformHost(u) {
			t.Errorf("IsPlatformHost(%q) = false, want true", u)
		}
	}
	for _, u := range no {
		if IsPlatformHost(u) {
			t.Errorf("IsPlatformHost(%q) = true, want false", u)
		}
	}
}
