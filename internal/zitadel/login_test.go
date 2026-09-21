package zitadel

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
)

// REQ-CROSS-323: Login prefers the loopback flow and falls back to the device
// flow only for the stated reasons.

// laptop is a host nothing rules out.
func laptop() string { return "" }

func TestLoginUsesTheLoopbackFlowByDefault(t *testing.T) {
	issuer := newFakeIssuer(t)
	browser := newBrowser()
	var out bytes.Buffer

	tok, err := Login(context.Background(), issuer.profile(), &out, Options{OpenBrowser: browser.open, LoopbackUnavailable: laptop})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if tok.AccessToken != "AT-CODE" {
		t.Fatalf("token = %+v, want the authorization-code token", tok)
	}
	if issuer.deviceHits != 0 {
		t.Fatal("the device flow must not run when the loopback flow can")
	}
	if strings.Contains(out.String(), "device flow") {
		t.Fatalf("no fallback must be announced, got: %q", out.String())
	}
}

func TestLoginDeviceFlowFlagForcesTheDeviceFlow(t *testing.T) {
	issuer := newFakeIssuer(t)
	opened := false
	browser := func(string) error { opened = true; return nil }

	// No loopback listener is opened on this path, so the device flow's poll
	// interval can be skipped on a synctest clock.
	synctest.Test(t, func(t *testing.T) {
		ctx := withInMemoryIssuer(context.Background(), issuer.mux)
		tok, err := Login(ctx, issuer.profile(), &bytes.Buffer{}, Options{DeviceFlow: true, OpenBrowser: browser, LoopbackUnavailable: laptop})
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		if tok.AccessToken != "AT-DEVICE" {
			t.Fatalf("token = %+v, want the device-flow token", tok)
		}
		if issuer.authorizeHits != 0 || opened {
			t.Fatal("--device-flow must neither open a browser nor touch the authorization endpoint")
		}
	})
}

func TestLoginFallsBackToTheDeviceFlowWhenTheHostRulesLoopbackOut(t *testing.T) {
	issuer := newFakeIssuer(t)
	ssh := func() string { return "this is an SSH session" }
	opened := false
	browser := func(string) error { opened = true; return nil }
	var out bytes.Buffer

	// The handover happens before authCodeLogin runs, so no listener exists
	// to hold the bubble's clock still.
	synctest.Test(t, func(t *testing.T) {
		ctx := withInMemoryIssuer(context.Background(), issuer.mux)
		tok, err := Login(ctx, issuer.profile(), &out, Options{OpenBrowser: browser, LoopbackUnavailable: ssh})
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		if tok.AccessToken != "AT-DEVICE" {
			t.Fatalf("token = %+v, want the device-flow token", tok)
		}
		if opened || issuer.authorizeHits != 0 {
			t.Fatal("a host that rules the loopback flow out must not open a browser at all")
		}
		if !strings.Contains(out.String(), "Using the device flow") || !strings.Contains(out.String(), "SSH session") {
			t.Fatalf("the fallback and its reason must be told to the operator, got: %q", out.String())
		}
	})
}

// Not bubbled: this path opens the loopback listener before the browser
// launcher fails, and a listener's Accept loop never counts as durably
// blocked, so a synctest clock would stop instead of skipping the poll
// interval. The real socket is the point of the test.
func TestLoginFallsBackToTheDeviceFlowWhenTheBrowserCannotOpen(t *testing.T) {
	issuer := newFakeIssuer(t)
	broken := func(string) error { return errors.New("no browser") }
	var out bytes.Buffer

	tok, err := Login(context.Background(), issuer.profile(), &out, Options{OpenBrowser: broken, LoopbackUnavailable: laptop})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if tok.AccessToken != "AT-DEVICE" {
		t.Fatalf("token = %+v, want the device-flow token", tok)
	}
	if !strings.Contains(out.String(), "Using the device flow") || !strings.Contains(out.String(), "no browser") {
		t.Fatalf("the fallback and its reason must be told to the operator, got: %q", out.String())
	}
	if issuer.discoveryHits != 1 {
		t.Fatalf("discovery must run once per login even across the fallback, ran %d times", issuer.discoveryHits)
	}
	// The loopback listener is closed by the time the fallback is announced,
	// so an authorization URL left on screen would send the operator to a
	// redirect that can no longer be received.
	if strings.Contains(out.String(), issuer.srv.URL+"/authorize") {
		t.Fatalf("no authorization URL may be printed when the browser never opened, got: %q", out.String())
	}
}

func TestLoginDoesNotFallBackAfterTheOperatorRefused(t *testing.T) {
	issuer := newFakeIssuer(t)
	issuer.deny = true
	browser := newBrowser()

	tok, err := Login(context.Background(), issuer.profile(), &bytes.Buffer{}, Options{OpenBrowser: browser.open, LoopbackUnavailable: laptop})
	if err == nil || tok != nil {
		t.Fatalf("a refused login must fail, got tok=%+v err=%v", tok, err)
	}
	if issuer.deviceHits != 0 {
		t.Fatal("a refusal must not be retried through the device flow — the operator would be asked twice")
	}
}

// REQ-CROSS-336: the token names the flow that produced it, on the loopback
// path, the forced device path and the fallback path alike — the command
// pins the device flow to the workspace the sign-in lands in (D9) and can
// only know which flow ran from the token.
func TestLoginTokenNamesItsFlow(t *testing.T) {
	t.Run("loopback", func(t *testing.T) {
		issuer := newFakeIssuer(t)
		browser := newBrowser()
		tok, err := Login(context.Background(), issuer.profile(), &bytes.Buffer{}, Options{OpenBrowser: browser.open, LoopbackUnavailable: laptop})
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		if tok.Flow != FlowLoopback {
			t.Fatalf("Flow = %q, want %q", tok.Flow, FlowLoopback)
		}
	})
	t.Run("forced device", func(t *testing.T) {
		issuer := newFakeIssuer(t)
		tok, err := Login(context.Background(), issuer.profile(), &bytes.Buffer{}, Options{DeviceFlow: true})
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		if tok.Flow != FlowDevice {
			t.Fatalf("Flow = %q, want %q", tok.Flow, FlowDevice)
		}
	})
	t.Run("fallback", func(t *testing.T) {
		issuer := newFakeIssuer(t)
		broken := func(string) error { return errors.New("no browser") }
		tok, err := Login(context.Background(), issuer.profile(), &bytes.Buffer{}, Options{OpenBrowser: broken, LoopbackUnavailable: laptop})
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		if tok.Flow != FlowDevice {
			t.Fatalf("Flow = %q, want %q after the hand-over", tok.Flow, FlowDevice)
		}
	})
}
