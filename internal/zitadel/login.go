package zitadel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// Options steer Login's choice between the two flows and supply what the
// loopback flow needs from its caller.
type Options struct {
	// DeviceFlow forces RFC 8628 device authorization (`--device-flow`).
	DeviceFlow bool
	// OpenBrowser launches url in the operator's browser. Nil means no
	// browser can be opened, which selects the device flow.
	OpenBrowser func(url string) error
	// LoopbackUnavailable says why a browser on this machine cannot reach a
	// loopback listener here, or returns "" when nothing rules it out
	// (browser.LoopbackUnavailable over the real host). Nil means no
	// pre-check: the listener and the launcher get their own chance to fail.
	LoopbackUnavailable func() string
	// Timeout bounds the wait for the browser; zero means DefaultLoginTimeout.
	Timeout time.Duration
	// OrganizationID, when set, narrows the sign-in to one organization: the
	// reserved `urn:zitadel:iam:org:roles:id:<org>` scope joins the request
	// (REQ-CROSS-334), so the issued token acts in that workspace.
	OrganizationID string
}

// Login signs in against a baked-in profile. Authorization code with PKCE on
// a loopback listener is the default; the device flow runs only when
// --device-flow asks for it, when the surroundings rule the loopback flow out
// (Options.LoopbackUnavailable), or when the listener or the browser cannot
// be started. Once the browser is open there is no fallback: a refused or
// timed-out login is reported as such, so the operator is never asked to
// approve twice.
func Login(ctx context.Context, profile Profile, out io.Writer, opts Options) (*Token, error) {
	if opts.DeviceFlow {
		return DeviceLogin(ctx, profile, out, opts)
	}
	if opts.LoopbackUnavailable != nil {
		if reason := opts.LoopbackUnavailable(); reason != "" {
			fmt.Fprintf(out, "Using the device flow: %s.\n", reason)
			return DeviceLogin(ctx, profile, out, opts)
		}
	}

	conf, err := oauthConfig(ctx, profile)
	if err != nil {
		return nil, err
	}
	withOrganizationFilter(conf, opts)
	tok, err := authCodeLogin(ctx, conf, out, opts)
	var unavailable *LoopbackUnavailableError
	if errors.As(err, &unavailable) {
		fmt.Fprintf(out, "Using the device flow: %s.\n", unavailable.Error())
		return deviceLogin(ctx, conf, out)
	}
	return tok, err
}
