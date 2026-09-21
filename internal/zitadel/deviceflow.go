package zitadel

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	oidcclient "github.com/zitadel/oidc/v3/pkg/client"
	"golang.org/x/oauth2"
)

// Token is a successful login result — the two values saveTokens already
// persists (config.Auth{Token, RefreshToken}) and the flow that produced
// them. Login may hand the loopback flow over to the device flow after it
// has started, so the caller cannot know from its options which one ran;
// the command's workspace choice depends on it (REQ-CROSS-336 D9).
type Token struct {
	AccessToken  string
	RefreshToken string
	// IDToken and Expiry are what the token response also carries and the
	// CLI records at sign-in (REQ-CROSS-388): the identity claims live on
	// the ID token, the lifetime on the response.
	IDToken string
	Expiry  time.Time
	Flow    Flow
}

// tokenFrom keeps what the response carried beyond the two bearer strings:
// the ID token (identity claims; the access token has none by contract) and
// the lifetime, which the flows used to drop (REQ-CROSS-388).
func tokenFrom(tok *oauth2.Token, flow Flow) *Token {
	t := &Token{AccessToken: tok.AccessToken, RefreshToken: tok.RefreshToken, Expiry: tok.Expiry, Flow: flow}
	if id, ok := tok.Extra("id_token").(string); ok {
		t.IDToken = id
	}
	return t
}

// Flow names the login flow that produced a Token.
type Flow string

const (
	// FlowLoopback is authorization code with PKCE on a loopback listener.
	FlowLoopback Flow = "loopback"
	// FlowDevice is the RFC 8628 device flow.
	FlowDevice Flow = "device"
)

// discoveryTimeout bounds the OIDC discovery request. http.DefaultClient has
// no timeout, so an issuer that accepts the connection and never answers
// would otherwise hang the login before either flow starts.
const discoveryTimeout = 30 * time.Second

// httpClientFrom returns the HTTP client this login should use. oauth2 already
// takes one from the context for the device-authorization and token requests
// (oauth2.HTTPClient); discovery used to hold a client of its own, so a caller
// that configured a transport had it honoured for two of a login's three
// requests and ignored for the first. One client for all three keeps them on
// the same transport, and is the seam a test uses to serve the issuer in
// memory. No production caller sets it, so the default below is what ships.
func httpClientFrom(ctx context.Context) *http.Client {
	if c, ok := ctx.Value(oauth2.HTTPClient).(*http.Client); ok && c != nil {
		return c
	}
	return &http.Client{Timeout: discoveryTimeout}
}

// oauthConfig runs OIDC discovery against profile.Issuer and returns the
// public-client oauth2 configuration both login flows share. Discovery runs
// once per login even when the loopback flow hands over to the device flow.
func oauthConfig(ctx context.Context, profile Profile) (*oauth2.Config, error) {
	discovery, err := oidcclient.Discover(ctx, profile.Issuer, httpClientFrom(ctx))
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery for %s: %w", profile.Issuer, err)
	}

	return &oauth2.Config{
		ClientID: profile.ClientID,
		Endpoint: oauth2.Endpoint{
			AuthURL:       discovery.AuthorizationEndpoint,
			TokenURL:      discovery.TokenEndpoint,
			DeviceAuthURL: discovery.DeviceAuthorizationEndpoint,
			// A public client (no client_secret) sends client_id as a body
			// param, matching the device-authorization request. Left unset,
			// oauth2 probes both auth styles on every non-2xx poll response —
			// silently doubling requests and discarding the first attempt's
			// error code (e.g. losing "slow_down" behind a second attempt's
			// "authorization_pending").
			AuthStyle: oauth2.AuthStyleInParams,
		},
		Scopes: loginScopes(profile),
	}, nil
}

// DeviceLogin runs RFC 8628 device authorization against profile.Issuer: OIDC
// discovery, a device-authorization request, printing the verification
// URL/code to out, then polling the token endpoint until the operator
// approves, the device code expires, or the token endpoint returns a
// terminal error (REQ-CROSS-235 clause 3). authorization_pending and
// slow_down are handled by (*oauth2.Config).DeviceAccessToken internally
// (RFC 8628 §3.5).
//
// It is the flow for a machine whose browser cannot reach it — an SSH
// session, a container, a CI shell — and the one `modernpath auth
// --device-flow` forces; Login chooses it only then.
func DeviceLogin(ctx context.Context, profile Profile, out io.Writer, opts Options) (*Token, error) {
	conf, err := oauthConfig(ctx, profile)
	if err != nil {
		return nil, err
	}
	withOrganizationFilter(conf, opts)
	return deviceLogin(ctx, conf, out)
}

func deviceLogin(ctx context.Context, conf *oauth2.Config, out io.Writer) (*Token, error) {
	deviceResp, err := conf.DeviceAuth(ctx)
	if err != nil {
		return nil, fmt.Errorf("device authorization request: %w", err)
	}

	if deviceResp.VerificationURIComplete != "" {
		fmt.Fprintf(out, "\nOpen this URL to sign in:\n  %s\n\n", deviceResp.VerificationURIComplete)
	} else {
		fmt.Fprintf(out, "\nVisit:  %s\nCode:   %s\n\n", deviceResp.VerificationURI, deviceResp.UserCode)
	}

	token, err := conf.DeviceAccessToken(ctx, deviceResp)
	if err != nil {
		return nil, fmt.Errorf("device token exchange: %w", err)
	}

	return tokenFrom(token, FlowDevice), nil
}

// loginScopes are the scopes both login flows request for a profile.
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
//
// Two more reserved scopes join every login (REQ-CROSS-334): ZITADEL's own
// project audience, without which its authenticated-user API — the
// workspace list — refuses the token; and the resource-owner claims, without
// which core cannot tell a granted organization from a home one and applies
// its domain guard to a sign-in that ZITADEL already authorized.
func loginScopes(profile Profile) []string {
	scopes := []string{"openid", "profile", "email", "offline_access", zitadelProjectAudienceScope, resourceOwnerClaimsScope}
	if profile.ProjectID != "" {
		scopes = append(scopes, projectAudienceScope(profile.ProjectID))
	}
	return scopes
}

const (
	// zitadelProjectAudienceScope puts ZITADEL's own project id in the
	// token's audience, which its authenticated-user API requires.
	zitadelProjectAudienceScope = "urn:zitadel:iam:org:project:id:zitadel:aud"
	// resourceOwnerClaimsScope adds the home organization's id, name and
	// primary domain to the token.
	resourceOwnerClaimsScope = "urn:zitadel:iam:user:resourceowner"
)

// withOrganizationFilter returns conf's scopes plus the organization filter
// when opts names a workspace, so the token the issuer mints acts there.
func withOrganizationFilter(conf *oauth2.Config, opts Options) {
	if opts.OrganizationID != "" {
		conf.Scopes = append(conf.Scopes, organizationFilterScope(opts.OrganizationID))
	}
}

// projectAudienceScope builds Zitadel's reserved project-audience scope.
func projectAudienceScope(projectID string) string {
	return "urn:zitadel:iam:org:project:id:" + projectID + ":aud"
}

// organizationFilterScope builds the reserved scope that limits the granted
// organizations in the issued token to one (REQ-CROSS-334).
func organizationFilterScope(orgID string) string {
	return "urn:zitadel:iam:org:roles:id:" + orgID
}
