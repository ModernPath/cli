package zitadel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/oauth2"
)

// tokenStep is one canned response the fake token endpoint returns, in order.
// The last step repeats for any poll beyond len(steps).
type tokenStep struct {
	status int
	body   map[string]any
}

// inMemoryIssuerURL is the issuer a bubbled test discovers. Nothing dials it —
// muxTransport answers before the address is ever used — but it has to be a
// well-formed https issuer, because OIDC discovery checks that the document it
// gets back names the issuer that was asked for.
const inMemoryIssuerURL = "https://issuer.test"

// deviceIssuer serves discovery, device-authorization and token endpoints for
// RFC 8628 device-flow tests (REQ-CROSS-235 clause 3). deviceInterval and
// deviceExpiresIn seed the device-authorization response; steps drives the
// token endpoint across successive polls.
//
// It is a mux, not a server: serve() runs it in memory for the polling tests
// (see fakehttp_test.go for why they cannot use a socket) and serveLive()
// puts the same handlers behind a real httptest.Server for the one test that
// exercises the whole stack over HTTP.
type deviceIssuer struct {
	mux  *http.ServeMux
	base string

	mu        sync.Mutex
	polls     int
	pollForms []url.Values
}

func newDeviceIssuer(deviceInterval, deviceExpiresIn int, steps []tokenStep) *deviceIssuer {
	i := &deviceIssuer{mux: http.NewServeMux(), base: inMemoryIssuerURL}

	i.mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                        i.base,
			"authorization_endpoint":        i.base + "/authorize",
			"token_endpoint":                i.base + "/token",
			"device_authorization_endpoint": i.base + "/device_authorization",
		})
	})

	i.mux.HandleFunc("/device_authorization", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":               "FAKE-DEVICE-CODE",
			"user_code":                 "FAKE-USER-CODE",
			"verification_uri":          i.base + "/device",
			"verification_uri_complete": i.base + "/device?user_code=FAKE-USER-CODE",
			"expires_in":                deviceExpiresIn,
			"interval":                  deviceInterval,
		})
	})

	i.mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		i.mu.Lock()
		step := steps[min(i.polls, len(steps)-1)]
		i.polls++
		i.pollForms = append(i.pollForms, r.PostForm)
		i.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(step.status)
		_ = json.NewEncoder(w).Encode(step.body)
	})

	return i
}

// serveLive puts the same handlers behind a real HTTP server and points the
// issuer at it.
func (i *deviceIssuer) serveLive(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(i.mux)
	t.Cleanup(srv.Close)
	i.base = srv.URL
}

func (i *deviceIssuer) profile() Profile {
	return Profile{Issuer: i.base, ClientID: "test-client", APIURL: "https://cloud.example.test"}
}

func (i *deviceIssuer) pollCount() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.polls
}

func (i *deviceIssuer) poll(n int) url.Values {
	i.mu.Lock()
	defer i.mu.Unlock()
	if n >= len(i.pollForms) {
		return nil
	}
	return i.pollForms[n]
}

func TestLoginSucceedsAfterAuthorizationPending(t *testing.T) {
	issuer := newDeviceIssuer(1, 30, []tokenStep{
		{http.StatusBadRequest, map[string]any{"error": "authorization_pending"}},
		{http.StatusOK, map[string]any{
			"access_token": "AT-1", "refresh_token": "RT-1",
			"token_type": "Bearer", "expires_in": 3600,
		}},
	})

	synctest.Test(t, func(t *testing.T) {
		var out bytes.Buffer
		start := time.Now()
		ctx := withInMemoryIssuer(context.Background(), issuer.mux)
		token, err := DeviceLogin(ctx, issuer.profile(), &out, Options{})
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		if token.AccessToken != "AT-1" || token.RefreshToken != "RT-1" {
			t.Fatalf("token = %+v, want AccessToken=AT-1 RefreshToken=RT-1", token)
		}
		if !strings.Contains(out.String(), "FAKE-USER-CODE") && !strings.Contains(out.String(), "user_code=FAKE-USER-CODE") {
			t.Fatalf("verification URL/code must be printed for the operator, got: %q", out.String())
		}
		// A pending poll must be retried, not treated as a failure, and the
		// retry must wait the issuer's interval rather than spinning: one
		// interval before each of the two polls.
		if got, want := issuer.pollCount(), 2; got != want {
			t.Fatalf("token endpoint polled %d times, want %d", got, want)
		}
		if got, want := time.Since(start), 2*time.Second; got != want {
			t.Fatalf("polling took %s, want exactly %s (1s interval before each poll)", got, want)
		}
	})
}

// Each poll must be a device-code grant carrying the code the authorization
// step issued and the client asking for it. Nothing checked the body of a poll
// before: the fake token endpoint answered any request that reached it, so a
// poll that sent the wrong grant or dropped the device code would have been
// answered with a token here and rejected only by a real issuer.
//
// This does not pin oauthConfig's AuthStyleInParams — the device grant carries
// client_id in the body under either auth style. What that choice protects is
// the poll *sequence*, and TestLoginHonorsSlowDown is what measures it.
func TestDeviceFlowPollsCarryTheDeviceCodeGrant(t *testing.T) {
	issuer := newDeviceIssuer(1, 30, []tokenStep{
		{http.StatusOK, map[string]any{
			"access_token": "AT-1", "token_type": "Bearer", "expires_in": 3600,
		}},
	})

	synctest.Test(t, func(t *testing.T) {
		ctx := withInMemoryIssuer(context.Background(), issuer.mux)
		if _, err := DeviceLogin(ctx, issuer.profile(), &bytes.Buffer{}, Options{}); err != nil {
			t.Fatalf("Login: %v", err)
		}
		form := issuer.poll(0)
		if got, want := form.Get("grant_type"), "urn:ietf:params:oauth:grant-type:device_code"; got != want {
			t.Errorf("grant_type = %q, want %q", got, want)
		}
		if got, want := form.Get("device_code"), "FAKE-DEVICE-CODE"; got != want {
			t.Errorf("device_code = %q, want the code the authorization step issued (%q)", got, want)
		}
		if got, want := form.Get("client_id"), "test-client"; got != want {
			t.Errorf("client_id = %q, want %q in the body (AuthStyleInParams)", got, want)
		}
	})
}

func TestLoginHonorsSlowDown(t *testing.T) {
	issuer := newDeviceIssuer(1, 30, []tokenStep{
		{http.StatusBadRequest, map[string]any{"error": "slow_down"}},
		{http.StatusBadRequest, map[string]any{"error": "authorization_pending"}},
		{http.StatusOK, map[string]any{
			"access_token": "AT-2", "refresh_token": "RT-2",
			"token_type": "Bearer", "expires_in": 3600,
		}},
	})

	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		ctx := withInMemoryIssuer(context.Background(), issuer.mux)
		token, err := DeviceLogin(ctx, issuer.profile(), &bytes.Buffer{}, Options{})
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		if token.AccessToken != "AT-2" {
			t.Fatalf("token = %+v, want AccessToken=AT-2", token)
		}
		if got, want := issuer.pollCount(), 3; got != want {
			t.Fatalf("token endpoint polled %d times, want %d", got, want)
		}
		// RFC 8628 §3.5: slow_down widens the interval by 5s "for this and all
		// subsequent requests". With a 1s interval the polls therefore land at
		// 1s, 1+6=7s and 7+6=13s. On the real clock this could only be
		// asserted as a lower bound, which a single 6s sleep anywhere would
		// also satisfy; on the bubble's clock the total is exact, so a widening
		// that is applied once instead of to every subsequent poll (11s), or
		// not at all (3s), fails here.
		if got, want := time.Since(start), 13*time.Second; got != want {
			t.Fatalf("polling took %s, want exactly %s (1s, then 6s before each later poll)", got, want)
		}
	})
}

func TestLoginFailsOnDeviceCodeExpiryAndReturnsNoToken(t *testing.T) {
	// expires_in 3 with a 2s interval: the second poll would be due at 4s, so
	// the device code expires first, at 3s, with no ambiguity about which
	// timer fires.
	issuer := newDeviceIssuer(2, 3, []tokenStep{
		{http.StatusBadRequest, map[string]any{"error": "authorization_pending"}},
	})

	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		ctx := withInMemoryIssuer(context.Background(), issuer.mux)
		token, err := DeviceLogin(ctx, issuer.profile(), &bytes.Buffer{}, Options{})
		if err == nil {
			t.Fatal("an expired device code must return an error")
		}
		if token != nil {
			t.Fatalf("an expired device code must return no token, got %+v", token)
		}
		// It has to fail *because the code expired*, at the moment it expired.
		// Checking only that some error came back would pass just as happily
		// on a misdirected request or a malformed response.
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expiry must surface as a deadline, got %v", err)
		}
		if got, want := time.Since(start), 3*time.Second; got != want {
			t.Fatalf("gave up after %s, want the issuer's %s expires_in", got, want)
		}
		if got, want := issuer.pollCount(), 1; got != want {
			t.Fatalf("polled %d times, want %d before the code expired", got, want)
		}
	})
}

func TestLoginFailsOnTokenEndpointError(t *testing.T) {
	issuer := newDeviceIssuer(1, 30, []tokenStep{
		{http.StatusBadRequest, map[string]any{"error": "access_denied"}},
	})

	synctest.Test(t, func(t *testing.T) {
		ctx := withInMemoryIssuer(context.Background(), issuer.mux)
		token, err := DeviceLogin(ctx, issuer.profile(), &bytes.Buffer{}, Options{})
		if err == nil {
			t.Fatal("a terminal token-endpoint error must return an error")
		}
		if token != nil {
			t.Fatalf("a terminal token-endpoint error must return no token, got %+v", token)
		}
		// The issuer's own error code has to survive to the caller: a refusal
		// and an expiry are both "polling stopped", and only this tells the
		// operator which happened.
		var retrieve *oauth2.RetrieveError
		if !errors.As(err, &retrieve) {
			t.Fatalf("want the issuer's OAuth error, got %v", err)
		}
		if retrieve.ErrorCode != "access_denied" {
			t.Fatalf("ErrorCode = %q, want access_denied", retrieve.ErrorCode)
		}
		// access_denied is terminal: polling must stop on it, not retry.
		if got, want := issuer.pollCount(), 1; got != want {
			t.Fatalf("polled %d times, want %d — a refusal is terminal", got, want)
		}
	})
}

// The one device-flow test that goes over a real socket, so that the parts the
// in-memory transport stands in for — a real connection, a real response body,
// discovery and both grants against a live server — are exercised end to end
// through the exported entry point at least once.
func TestDeviceLoginWorksOverRealHTTP(t *testing.T) {
	issuer := newDeviceIssuer(1, 30, []tokenStep{
		{http.StatusOK, map[string]any{
			"access_token": "AT-1", "refresh_token": "RT-1",
			"token_type": "Bearer", "expires_in": 3600,
		}},
	})
	issuer.serveLive(t)

	var out bytes.Buffer
	token, err := DeviceLogin(context.Background(), issuer.profile(), &out, Options{})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if token.AccessToken != "AT-1" || token.RefreshToken != "RT-1" {
		t.Fatalf("token = %+v, want AccessToken=AT-1 RefreshToken=RT-1", token)
	}
	if !strings.Contains(out.String(), "FAKE-USER-CODE") {
		t.Fatalf("verification URL/code must be printed for the operator, got: %q", out.String())
	}
}

func TestLoginFailsOnDiscoveryError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	profile := Profile{Issuer: srv.URL, ClientID: "test-client", APIURL: "https://cloud.example.test"}
	if _, err := DeviceLogin(context.Background(), profile, &bytes.Buffer{}, Options{}); err == nil {
		t.Fatal("a broken discovery document must fail Login, not hang or panic")
	}
}
