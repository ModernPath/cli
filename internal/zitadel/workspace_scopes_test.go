package zitadel

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// REQ-CROSS-334 — every profile login asks for the resource-owner claims,
// and carries the organization filter when a workspace is set.
// `urn:zitadel:iam:user:resourceowner` adds the home organization's id, name
// and domain, which core's domain guard needs to recognise a granted
// organization (EPIC-CLI-009 R4). ZITADEL's own project audience is not
// asked for: the CLI calls no ZITADEL API with the access token
// (USER:2026-10-29:drop-zitadel-audience-scope).

const resourceOwnerScope = "urn:zitadel:iam:user:resourceowner"

// signInScopes is the whole scope set a login asks for, without the filter.
func signInScopes(projectID string) []string {
	scopes := []string{"openid", "profile", "email", "offline_access", resourceOwnerScope}
	if projectID != "" {
		scopes = append(scopes, "urn:zitadel:iam:org:project:id:"+projectID+":aud")
	}
	return scopes
}

// sameScopes compares two scope lists as sets.
func sameScopes(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]int{}
	for _, s := range got {
		seen[s]++
	}
	for _, s := range want {
		if seen[s] == 0 {
			return false
		}
		seen[s]--
	}
	return true
}

func TestLoginScopesAreTheSignInSet(t *testing.T) {
	for _, projectID := range []string{"123456", ""} {
		if got, want := loginScopes(Profile{ProjectID: projectID}), signInScopes(projectID); !sameScopes(got, want) {
			t.Errorf("ProjectID=%q: scopes = %v, want %v", projectID, got, want)
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
	if got, want := strings.Fields(scope), append(signInScopes("proj-1"), organizationFilterScope("org-b")); !sameScopes(got, want) {
		t.Fatalf("authorize scope = %v, want %v", got, want)
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
	want := signInScopes("proj-1")
	if filtered {
		want = append(want, organizationFilterScope("org-b"))
	}
	if got := strings.Fields(scope); !sameScopes(got, want) {
		t.Fatalf("device-authorization scope = %v, want %v", got, want)
	}
}
