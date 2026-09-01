package zitadel

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// tokenStep is one canned response the fake token endpoint returns, in order.
// The last step repeats for any poll beyond len(steps).
type tokenStep struct {
	status int
	body   map[string]any
}

// fakeOIDCServer serves discovery, device-authorization, and token endpoints
// for RFC 8628 device-flow tests (REQ-CROSS-235 clause 3). deviceInterval and
// deviceExpiresIn seed the device-authorization response; steps drives the
// token endpoint across successive polls.
func fakeOIDCServer(t *testing.T, deviceInterval, deviceExpiresIn int, steps []tokenStep) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                        srv.URL,
			"authorization_endpoint":        srv.URL + "/authorize",
			"token_endpoint":                srv.URL + "/token",
			"device_authorization_endpoint": srv.URL + "/device_authorization",
		})
	})

	mux.HandleFunc("/device_authorization", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":               "FAKE-DEVICE-CODE",
			"user_code":                 "FAKE-USER-CODE",
			"verification_uri":          srv.URL + "/device",
			"verification_uri_complete": srv.URL + "/device?user_code=FAKE-USER-CODE",
			"expires_in":                deviceExpiresIn,
			"interval":                  deviceInterval,
		})
	})

	var poll int
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		step := steps[poll]
		if poll < len(steps)-1 {
			poll++
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(step.status)
		_ = json.NewEncoder(w).Encode(step.body)
	})

	return srv
}

func TestLoginSucceedsAfterAuthorizationPending(t *testing.T) {
	srv := fakeOIDCServer(t, 1, 30, []tokenStep{
		{http.StatusBadRequest, map[string]any{"error": "authorization_pending"}},
		{http.StatusOK, map[string]any{
			"access_token": "AT-1", "refresh_token": "RT-1",
			"token_type": "Bearer", "expires_in": 3600,
		}},
	})

	var out bytes.Buffer
	profile := Profile{Issuer: srv.URL, ClientID: "test-client", APIURL: "https://cloud.example.test"}
	token, err := Login(context.Background(), profile, &out)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if token.AccessToken != "AT-1" || token.RefreshToken != "RT-1" {
		t.Fatalf("token = %+v, want AccessToken=AT-1 RefreshToken=RT-1", token)
	}
	if !strings.Contains(out.String(), "FAKE-USER-CODE") && !strings.Contains(out.String(), "user_code=FAKE-USER-CODE") {
		t.Fatalf("verification URL/code must be printed for the operator, got: %q", out.String())
	}
}

func TestLoginHonorsSlowDown(t *testing.T) {
	srv := fakeOIDCServer(t, 1, 30, []tokenStep{
		{http.StatusBadRequest, map[string]any{"error": "slow_down"}},
		{http.StatusBadRequest, map[string]any{"error": "authorization_pending"}},
		{http.StatusOK, map[string]any{
			"access_token": "AT-2", "refresh_token": "RT-2",
			"token_type": "Bearer", "expires_in": 3600,
		}},
	})

	start := time.Now()
	profile := Profile{Issuer: srv.URL, ClientID: "test-client", APIURL: "https://cloud.example.test"}
	token, err := Login(context.Background(), profile, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if token.AccessToken != "AT-2" {
		t.Fatalf("token = %+v, want AccessToken=AT-2", token)
	}
	// RFC 8628 §3.5: slow_down must add at least 5s to the poll interval, so
	// the wait before the third poll grows from 1s to at least 6s.
	if elapsed := time.Since(start); elapsed < 6*time.Second {
		t.Fatalf("slow_down must widen the poll interval by >=5s, only waited %s", elapsed)
	}
}

func TestLoginFailsOnDeviceCodeExpiryAndReturnsNoToken(t *testing.T) {
	srv := fakeOIDCServer(t, 1, 1, []tokenStep{
		{http.StatusBadRequest, map[string]any{"error": "authorization_pending"}},
	})

	profile := Profile{Issuer: srv.URL, ClientID: "test-client", APIURL: "https://cloud.example.test"}
	token, err := Login(context.Background(), profile, &bytes.Buffer{})
	if err == nil {
		t.Fatal("an expired device code must return an error")
	}
	if token != nil {
		t.Fatalf("an expired device code must return no token, got %+v", token)
	}
}

func TestLoginFailsOnTokenEndpointError(t *testing.T) {
	srv := fakeOIDCServer(t, 1, 30, []tokenStep{
		{http.StatusBadRequest, map[string]any{"error": "access_denied"}},
	})

	profile := Profile{Issuer: srv.URL, ClientID: "test-client", APIURL: "https://cloud.example.test"}
	token, err := Login(context.Background(), profile, &bytes.Buffer{})
	if err == nil {
		t.Fatal("a terminal token-endpoint error must return an error")
	}
	if token != nil {
		t.Fatalf("a terminal token-endpoint error must return no token, got %+v", token)
	}
}

func TestLoginFailsOnDiscoveryError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	profile := Profile{Issuer: srv.URL, ClientID: "test-client", APIURL: "https://cloud.example.test"}
	if _, err := Login(context.Background(), profile, &bytes.Buffer{}); err == nil {
		t.Fatal("a broken discovery document must fail Login, not hang or panic")
	}
}
