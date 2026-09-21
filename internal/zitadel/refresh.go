package zitadel

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"
)

// FlowRefresh marks a Token minted by a refresh, not a login.
const FlowRefresh Flow = "refresh"

// Refresh exchanges a stored refresh token for a new token pair at the
// profile's issuer (REQ-CROSS-389). It is a call to the identity provider,
// not to the API host, so it goes through the oauth2 client — never through
// the API-host wiring (platform.Prepare/Authorize). ZITADEL rotates the
// refresh token on every renewal, so the returned pair replaces the stored
// one whole.
func Refresh(ctx context.Context, profile Profile, refreshToken string) (*Token, error) {
	conf, err := oauthConfig(ctx, profile)
	if err != nil {
		return nil, err
	}
	// A token with no access token is never valid, so the source refreshes
	// on its first read instead of handing back what it was given.
	tok, err := conf.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken}).Token()
	if err != nil {
		return nil, fmt.Errorf("token refresh at %s: %w", profile.Issuer, err)
	}
	return tokenFrom(tok, FlowRefresh), nil
}

// ProfileForIssuer returns the baked-in profile whose issuer is issuer, so a
// credential stored with its issuer can be refreshed even when the API URL
// is not one of the baked-in hosts.
func ProfileForIssuer(issuer string) (Profile, bool) {
	for _, p := range []Profile{ProdProfile, TestProfile} {
		if p.Issuer == issuer {
			return p, true
		}
	}
	return Profile{}, false
}
