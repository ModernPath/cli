// Package zitadel runs RFC 8628 device-flow login against a baked-in ZITADEL
// profile for the modernpath CLI (REQ-CROSS-235). Every instance-specific
// value — issuer, OIDC client ID, API base URL — is compiled into the binary;
// SelectProfile is the only choice an operator makes (prod vs test), never an
// arbitrary issuer or client ID.
package zitadel

// Profile is a baked-in ZITADEL device-flow target: the OIDC issuer, the
// public client ID registered for the modernpath CLI, and the ModernPath API
// base URL that issuer's tokens are valid against.
type Profile struct {
	Issuer   string
	ClientID string
	APIURL   string

	// ProjectID is the Zitadel platform project this instance's API belongs
	// to. Login requests it as a reserved audience scope, so the issued
	// access token carries it in `aud` — which is what the oidc-bff proxy's
	// expected_audience and core's check_accepted_audience actually accept.
	// Without it the token's audience is the CLI client id alone and every
	// authenticated call is refused (REQ-CROSS-291).
	//
	// Empty means "not known here yet": login asks for exactly what it asks
	// for today, and platform.Authorize expects nothing and refuses nothing.
	ProjectID string
}

// ProdProfile is the baked-in prod ZITADEL target: the "modernpath-cli"
// native app registered under zitadel-project-platform (D1/D4,
// USER:2026-08-24).
//
// APIURL is the API host, not the SPA host. `cloud.modernpath.ai` serves the
// single-page app and answers every path with index.html, so a CLI pointed
// there parses HTML as JSON ("invalid character '<'") instead of reaching the
// API at all. The SPA's own runtime document names the host it calls:
// https://cloud.modernpath.ai/config.json -> "apiBaseUrl":
// "https://api.modernpath.ai" (RUN:2026-08-27).
var ProdProfile = Profile{
	Issuer:   "https://id.modernpath.ai",
	ClientID: "387656061974216719",
	APIURL:   "https://api.modernpath.ai",

	// zitadel-project-platform (prod). The same value core runs with as
	// ZITADEL_PROJECT_ID, which is what makes a token carrying it acceptable
	// to core's check_accepted_audience; the modernpath-cli app is registered
	// under this project, so the reserved audience scope is one it may ask
	// for (RUN:2026-08-27, EPIC-CLI-006 D2).
	ProjectID: "373481497824329999",
}

// TestProfile is the baked-in test-plat ZITADEL target (D4/D5,
// USER:2026-08-24) — the same test-plat tenant tmctl and cc-runner already
// target. APIURL is the API host for the same reason as ProdProfile:
// https://cloud.workload.test-plat.modernpath.ai/config.json -> "apiBaseUrl":
// "https://api.workload.test-plat.modernpath.ai" (RUN:2026-08-27).
var TestProfile = Profile{
	Issuer:   "https://id.test-plat.modernpath.ai",
	ClientID: "387656000301170778",
	APIURL:   "https://api.workload.test-plat.modernpath.ai",

	// zitadel-project-platform (test). Same reasoning as ProdProfile, and a
	// different project: prod and test are separate ZITADEL instances.
	ProjectID: "371589174458843143",
}

// SelectProfile returns TestProfile when test is true, otherwise ProdProfile.
// These two baked-in values are the only profiles SelectProfile ever returns.
func SelectProfile(test bool) Profile {
	if test {
		return TestProfile
	}
	return ProdProfile
}
