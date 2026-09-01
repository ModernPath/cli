// Package platform adapts an outbound CLI request to the API host it is
// addressed to.
//
// On the shared platform API host (`api.modernpath.ai` and its test-plat
// sibling) core does not own the whole `/api` namespace: every service there
// owns one sub-prefix, and core's is `/api/ex`, which the Gateway rewrites
// back to `/api` before core sees it. The CLI's route constants are written
// against core's own `/api/...` prefix, so a request to that host must be
// translated the same way the SPA translates its own.
//
// This mirrors `modernpath-frontend/src/config.ts` `coreApiUrl`/`boundaryInit`
// exactly, minus the parts that only exist in a browser: the CLI carries a
// bearer token rather than the cookie the SPA sends with `credentials:
// "include"`. Hosts that are not the platform host — `beta.modernpath.ai`,
// `localhost:4000`, any `--api-url` — are left byte-identical, so nothing
// changes for an existing deployment.
package platform

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/modernpath/cli/internal/zitadel"
)

const (
	// coreRoutePrefix is the prefix core's own routes are written against,
	// and what the legacy relay forwards verbatim.
	coreRoutePrefix = "/api"

	// corePlatformPrefix is core's public sub-prefix on the shared platform
	// API host. This is a fact of the platform's HTTPRoute, not a setting:
	// the `ex-api` route matches PathPrefix `/api/ex` and rewrites it to
	// `/api` before the request reaches core.
	corePlatformPrefix = "/api/ex"

	// TokenHandlerHeader is the static custom request header the oidc-bff
	// proxy requires on every request to the platform API host
	// (draft-ietf-oauth-browser-based-apps §6.1.3.3.2). Its value is not a
	// secret; it exists to force a CORS preflight for browser callers.
	TokenHandlerHeader = "X-MP-Token-Handler"
	TokenHandlerValue  = "1"
)

// HealthPath returns the health-check path to use against baseURL.
//
// Core answers the same check at two paths: `/_health` at the root, which
// Kamal and the local harness use, and `/api/_health`. Only the second is
// reachable on the shared platform host, where core is routed solely through
// its `/api/ex` sub-prefix — Prepare turns it into `/api/ex/_health` there.
// Everywhere else the root path is what deployed servers already answer, so
// it is what the CLI keeps asking for.
func HealthPath(baseURL string) string {
	if IsPlatformHost(baseURL) {
		return coreRoutePrefix + "/_health"
	}
	return "/_health"
}

// platformOrigins is the set of scheme://host values that are fronted by the
// platform Gateway and the oidc-bff token handler. It is derived from the
// baked-in Zitadel profiles rather than configured, for the same reason those
// profiles are baked in: the topology of a ModernPath platform host is not an
// operator choice.
// It maps each origin to the profile it came from: Authorize needs the
// profile's project id, and which instance an origin belongs to is not
// recoverable from the origin alone.
var platformOrigins = buildPlatformOrigins()

func buildPlatformOrigins() map[string]zitadel.Profile {
	origins := make(map[string]zitadel.Profile, 2)
	for _, p := range []zitadel.Profile{zitadel.ProdProfile, zitadel.TestProfile} {
		if u, err := url.Parse(p.APIURL); err == nil && u.Host != "" {
			origins[u.Scheme+"://"+u.Host] = p
		}
	}
	return origins
}

// IsPlatformHost reports whether rawURL addresses a shared platform API host.
func IsPlatformHost(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return isPlatformOrigin(u)
}

func isPlatformOrigin(u *url.URL) bool {
	if u == nil || u.Host == "" {
		return false
	}
	_, ok := platformOrigins[u.Scheme+"://"+u.Host]
	return ok
}

// Prepare adapts req in place for the host it is addressed to. On a platform
// API host it moves a core `/api/...` path onto core's platform prefix and
// sets the static token-handler header; anywhere else it does nothing.
//
// Prepare is idempotent: a path already under `/api/ex` is left alone, so
// applying it twice to the same request cannot produce `/api/ex/ex/...`.
func Prepare(req *http.Request) {
	if req == nil || req.URL == nil || !isPlatformOrigin(req.URL) {
		return
	}

	req.URL.Path = corePath(req.URL.Path)
	if req.URL.RawPath != "" {
		req.URL.RawPath = corePath(req.URL.RawPath)
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set(TokenHandlerHeader, TokenHandlerValue)
}

// corePath maps one of core's own `/api/...` paths onto its platform prefix.
// Paths outside `/api` — and paths already carrying the platform prefix — are
// returned unchanged.
func corePath(p string) string {
	if p == corePlatformPrefix || strings.HasPrefix(p, corePlatformPrefix+"/") {
		return p
	}
	if p == coreRoutePrefix || strings.HasPrefix(p, coreRoutePrefix+"/") {
		return corePlatformPrefix + p[len(coreRoutePrefix):]
	}
	return p
}
