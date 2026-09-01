package zitadel

import (
	"context"
	"fmt"
	"io"
	"net/http"

	oidcclient "github.com/zitadel/oidc/v3/pkg/client"
	"golang.org/x/oauth2"
)

// Token is a successful device-flow result — the two values saveTokens
// already persists for the legacy flow (config.Auth{Token, RefreshToken}).
type Token struct {
	AccessToken  string
	RefreshToken string
}

// Login runs RFC 8628 device authorization against profile.Issuer: OIDC
// discovery, a device-authorization request, printing the verification
// URL/code to out, then polling the token endpoint until the operator
// approves, the device code expires, or the token endpoint returns a
// terminal error (REQ-CROSS-235 clause 3). authorization_pending and
// slow_down are handled by (*oauth2.Config).DeviceAccessToken internally
// (RFC 8628 §3.5).
func Login(ctx context.Context, profile Profile, out io.Writer) (*Token, error) {
	discovery, err := oidcclient.Discover(ctx, profile.Issuer, http.DefaultClient)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery for %s: %w", profile.Issuer, err)
	}

	conf := &oauth2.Config{
		ClientID: profile.ClientID,
		Endpoint: oauth2.Endpoint{
			AuthURL:       discovery.AuthorizationEndpoint,
			TokenURL:      discovery.TokenEndpoint,
			DeviceAuthURL: discovery.DeviceAuthorizationEndpoint,
			// A public client (no client_secret) sends client_id as a body
			// param, matching the device-authorization request above. Left
			// unset, oauth2 probes both auth styles on every non-2xx poll
			// response — silently doubling requests and discarding the
			// first attempt's error code (e.g. losing "slow_down" behind a
			// second attempt's "authorization_pending").
			AuthStyle: oauth2.AuthStyleInParams,
		},
		Scopes: loginScopes(profile),
	}

	deviceResp, err := conf.DeviceAuth(ctx)
	if err != nil {
		return nil, fmt.Errorf("device authorization request: %w", err)
	}

	if deviceResp.VerificationURIComplete != "" {
		fmt.Fprintf(out, "\nOpen this URL to authenticate:\n  %s\n\n", deviceResp.VerificationURIComplete)
	} else {
		fmt.Fprintf(out, "\nVisit:  %s\nCode:   %s\n\n", deviceResp.VerificationURI, deviceResp.UserCode)
	}

	token, err := conf.DeviceAccessToken(ctx, deviceResp)
	if err != nil {
		return nil, fmt.Errorf("device token exchange: %w", err)
	}

	return &Token{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken}, nil
}

// loginScopes are the scopes the device flow requests for a profile.
//
// The reserved `urn:zitadel:iam:org:project:id:<id>:aud` scope is what puts
// the platform project id into the issued token's `aud` claim. Without it the
// audience is the CLI client id alone, which neither the oidc-bff proxy's
// expected_audience nor core's check_accepted_audience contains — the login
// succeeds and every call after it is refused. Core's own service tokens
// request the same scope.
//
// A profile with no project id asks for exactly the four scopes it asked for
// before this existed: an empty id would produce `...:project:id::aud`, which
// Zitadel rejects, breaking a login that works today.
func loginScopes(profile Profile) []string {
	scopes := []string{"openid", "profile", "email", "offline_access"}
	if profile.ProjectID != "" {
		scopes = append(scopes, projectAudienceScope(profile.ProjectID))
	}
	return scopes
}

// projectAudienceScope builds Zitadel's reserved project-audience scope.
func projectAudienceScope(projectID string) string {
	return "urn:zitadel:iam:org:project:id:" + projectID + ":aud"
}
