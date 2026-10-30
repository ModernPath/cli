package zitadel

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// USER:2026-10-29:device-flow-reuses-stored-workspace — the device flow has no
// workspace step (its authorization is created by the CLI's back-channel
// request and completed with AuthorizeOrDenyDeviceAuthorization, which takes
// no scope), so the stored workspace is sent as the filter whenever the
// sign-in runs as the device flow: forced, ruled out up front, or fallen back
// to after the browser could not open. The browser flow sends none.

func TestDeviceFlowCarriesTheStoredWorkspace(t *testing.T) {
	ssh := func() string { return "this is an SSH session" }
	broken := func(string) error { return errors.New("no browser") }
	cases := []struct {
		name string
		opts Options
	}{
		{"forced", Options{DeviceFlow: true}},
		{"ruled out up front", Options{OpenBrowser: func(string) error { return nil }, LoopbackUnavailable: ssh}},
		{"the browser cannot open", Options{OpenBrowser: broken, LoopbackUnavailable: laptop}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			issuer := newFakeIssuer(t)
			opts := c.opts
			opts.DeviceFlowOrganizationID = "org-b"
			if _, err := Login(context.Background(), issuer.profile(), &bytes.Buffer{}, opts); err != nil {
				t.Fatalf("Login: %v", err)
			}
			assertDeviceScope(t, issuer, true)
		})
	}
}

func TestDeviceFlowNamedWorkspaceWinsOverTheStoredOne(t *testing.T) {
	issuer := newFakeIssuer(t)
	opts := Options{DeviceFlow: true, OrganizationID: "org-c", DeviceFlowOrganizationID: "org-b"}
	if _, err := Login(context.Background(), issuer.profile(), &bytes.Buffer{}, opts); err != nil {
		t.Fatalf("Login: %v", err)
	}
	issuer.mu.Lock()
	scope := issuer.deviceForm.Get("scope")
	issuer.mu.Unlock()
	if got := strings.Fields(scope); !sameScopes(got, append(signInScopes("proj-1"), organizationFilterScope("org-c"))) {
		t.Fatalf("device-authorization scope = %v, want only the named org-c filter", got)
	}
}

func TestBrowserFlowSendsNoStoredWorkspace(t *testing.T) {
	issuer := newFakeIssuer(t)
	browser := newBrowser()

	opts := Options{OpenBrowser: browser.open, LoopbackUnavailable: laptop, DeviceFlowOrganizationID: "org-b"}
	if _, err := Login(context.Background(), issuer.profile(), &bytes.Buffer{}, opts); err != nil {
		t.Fatalf("Login: %v", err)
	}
	<-browser.done
	if got := strings.Fields(issuer.authorizeQuery.Get("scope")); !sameScopes(got, signInScopes("proj-1")) {
		t.Fatalf("authorize scope = %v, want the sign-in set without a filter", got)
	}
}
