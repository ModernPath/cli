package zitadel

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// REQ-CROSS-334 — every profile login asks for the two scopes that make a
// workspace choice possible, and carries the organization filter when a
// workspace is set.
//
// `urn:zitadel:iam:org:project:id:zitadel:aud` puts ZITADEL's own project in
// the token's audience, which its authenticated-user API (the organization
// list, REQ-CROSS-335) requires. `urn:zitadel:iam:user:resourceowner` adds
// the home organization's id, name and domain, which core's domain guard
// needs to recognise a granted organization (EPIC-CLI-009 R4).

const (
	zitadelAudienceScope = "urn:zitadel:iam:org:project:id:zitadel:aud"
	resourceOwnerScope   = "urn:zitadel:iam:user:resourceowner"
)

func TestLoginScopesIncludeTheWorkspaceListingAndResourceOwnerScopes(t *testing.T) {
	for _, p := range []Profile{{ProjectID: "123456"}, {}} {
		scopes := loginScopes(p)
		for _, want := range []string{zitadelAudienceScope, resourceOwnerScope} {
			if !contains(scopes, want) {
				t.Errorf("ProjectID=%q: scopes %v missing %q", p.ProjectID, scopes, want)
			}
		}
	}
}

// REQ-CROSS-291's rule stands beside the new scopes: no project id, no
// project-audience scope — a malformed `...:project:id::aud` would be
// rejected by ZITADEL.
func TestLoginScopesWithoutAProjectIDOmitOnlyTheProjectAudience(t *testing.T) {
	scopes := loginScopes(Profile{})
	if len(scopes) != 6 {
		t.Errorf("scopes = %v, want the four existing scopes plus the two new ones", scopes)
	}
	for _, s := range scopes {
		if strings.HasPrefix(s, "urn:zitadel:iam:org:project:id:") && s != zitadelAudienceScope {
			t.Errorf("scopes %v contain a project-audience scope with no project id", scopes)
		}
	}
}

func TestOrganizationFilterScopeShape(t *testing.T) {
	if got, want := organizationFilterScope("371594261646278691"), "urn:zitadel:iam:org:roles:id:371594261646278691"; got != want {
		t.Errorf("organizationFilterScope = %q, want %q", got, want)
	}
}

// The filter has to reach the wire on the authorization request, and only
// when a workspace is set: an unfiltered login lists every workspace the
// person may choose from, a filtered one lands in the chosen workspace.
func TestLoginCarriesTheOrganizationFilterOnTheAuthorizeRequest(t *testing.T) {
	issuer := newFakeIssuer(t)
	browser := newBrowser()

	if _, err := Login(context.Background(), issuer.profile(), &bytes.Buffer{}, Options{OpenBrowser: browser.open, LoopbackUnavailable: laptop, OrganizationID: "org-b"}); err != nil {
		t.Fatalf("Login: %v", err)
	}
	<-browser.done
	scope := issuer.authorizeQuery.Get("scope")
	if !strings.Contains(scope, organizationFilterScope("org-b")) {
		t.Fatalf("authorize scope = %q, want the filter for org-b", scope)
	}
	for _, want := range []string{zitadelAudienceScope, resourceOwnerScope, projectAudienceScope("proj-1"), "openid"} {
		if !strings.Contains(scope, want) {
			t.Fatalf("authorize scope = %q, missing %q", scope, want)
		}
	}
}

func TestLoginSendsNoOrganizationFilterWhenNoWorkspaceIsSet(t *testing.T) {
	issuer := newFakeIssuer(t)
	browser := newBrowser()

	if _, err := Login(context.Background(), issuer.profile(), &bytes.Buffer{}, Options{OpenBrowser: browser.open, LoopbackUnavailable: laptop}); err != nil {
		t.Fatalf("Login: %v", err)
	}
	<-browser.done
	if scope := issuer.authorizeQuery.Get("scope"); strings.Contains(scope, "urn:zitadel:iam:org:roles:id:") {
		t.Fatalf("authorize scope = %q, must carry no organization filter", scope)
	}
}

// The device flow carries the same filter on its device-authorization
// request — on the forced path and on the fallback path alike, since Login
// may hand the loopback flow over after it has started.
func TestDeviceFlowCarriesTheOrganizationFilter(t *testing.T) {
	t.Run("forced", func(t *testing.T) {
		issuer := newFakeIssuer(t)
		if _, err := Login(context.Background(), issuer.profile(), &bytes.Buffer{}, Options{DeviceFlow: true, OrganizationID: "org-b"}); err != nil {
			t.Fatalf("Login: %v", err)
		}
		assertDeviceScope(t, issuer, true)
	})
	t.Run("fallback", func(t *testing.T) {
		issuer := newFakeIssuer(t)
		ssh := func() string { return "this is an SSH session" }
		if _, err := Login(context.Background(), issuer.profile(), &bytes.Buffer{}, Options{OpenBrowser: func(string) error { return nil }, LoopbackUnavailable: ssh, OrganizationID: "org-b"}); err != nil {
			t.Fatalf("Login: %v", err)
		}
		assertDeviceScope(t, issuer, true)
	})
	t.Run("no workspace set", func(t *testing.T) {
		issuer := newFakeIssuer(t)
		if _, err := Login(context.Background(), issuer.profile(), &bytes.Buffer{}, Options{DeviceFlow: true}); err != nil {
			t.Fatalf("Login: %v", err)
		}
		assertDeviceScope(t, issuer, false)
	})
}

func assertDeviceScope(t *testing.T, issuer *fakeIssuer, filtered bool) {
	t.Helper()
	issuer.mu.Lock()
	scope := issuer.deviceForm.Get("scope")
	issuer.mu.Unlock()
	if has := strings.Contains(scope, organizationFilterScope("org-b")); has != filtered {
		t.Fatalf("device-authorization scope = %q, filter present = %v, want %v", scope, has, filtered)
	}
	for _, want := range []string{zitadelAudienceScope, resourceOwnerScope} {
		if !strings.Contains(scope, want) {
			t.Fatalf("device-authorization scope = %q, missing %q", scope, want)
		}
	}
}
