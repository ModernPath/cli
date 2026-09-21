package zitadel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// REQ-CROSS-323: the loopback authorization-code flow, tested against a fake
// issuer.
//
// fakeIssuer serves discovery, authorization, device-authorization and token
// endpoints so one fake can stand behind either flow. The authorization
// endpoint answers like a user who approved (or, with deny set, refused) by
// redirecting straight back to the CLI's loopback redirect_uri; the token
// endpoint checks the PKCE verifier against the challenge it saw. Tokens name
// the flow that produced them so a test can tell which one ran.
type fakeIssuer struct {
	srv *httptest.Server
	// mux is the same handler set srv serves. A test that must run on a
	// synctest clock hands this to withInMemoryIssuer instead of dialling
	// srv, because a bubble stops advancing the moment a goroutine blocks on
	// the network (fakehttp_test.go).
	mux  *http.ServeMux
	deny bool
	// stallToken makes the token endpoint accept the request and never
	// answer, the way a wedged issuer does.
	stallToken bool
	// closing releases a stalled handler when the server is being torn down,
	// so httptest.Server.Close() does not block on a request whose client has
	// given up (the server-side context cancel is not reliably delivered once
	// the client abandons the request).
	closing chan struct{}

	mu             sync.Mutex
	discoveryHits  int
	authorizeHits  int
	deviceHits     int
	authorizeQuery url.Values
	deviceForm     url.Values
	tokenForm      url.Values
	pkceChecked    bool
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	f := &fakeIssuer{closing: make(chan struct{})}
	mux := http.NewServeMux()
	f.mux = mux
	f.srv = httptest.NewServer(mux)
	// t.Cleanup is LIFO: the close below runs before srv.Close, releasing a
	// stalled handler so Close() is not left waiting on its active connection.
	t.Cleanup(f.srv.Close)
	t.Cleanup(func() { close(f.closing) })

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.discoveryHits++
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                        f.srv.URL,
			"authorization_endpoint":        f.srv.URL + "/authorize",
			"token_endpoint":                f.srv.URL + "/token",
			"device_authorization_endpoint": f.srv.URL + "/device_authorization",
		})
	})

	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f.mu.Lock()
		f.authorizeHits++
		f.authorizeQuery = q
		f.mu.Unlock()
		back, err := url.Parse(q.Get("redirect_uri"))
		if err != nil {
			http.Error(w, "bad redirect_uri", http.StatusBadRequest)
			return
		}
		v := url.Values{"state": {q.Get("state")}}
		if f.deny {
			v.Set("error", "access_denied")
			v.Set("error_description", "the user refused")
		} else {
			v.Set("code", "FAKE-CODE")
		}
		back.RawQuery = v.Encode()
		http.Redirect(w, r, back.String(), http.StatusFound)
	})

	mux.HandleFunc("/device_authorization", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.deviceHits++
		f.deviceForm = r.PostForm
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":               "FAKE-DEVICE-CODE",
			"user_code":                 "FAKE-USER-CODE",
			"verification_uri":          f.srv.URL + "/device",
			"verification_uri_complete": f.srv.URL + "/device?user_code=FAKE-USER-CODE",
			"expires_in":                30,
			"interval":                  1,
		})
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if f.stallToken {
			// Never answer, the way a wedged issuer does — but release on
			// teardown so the test's Close() is not blocked on the request
			// the client already abandoned.
			select {
			case <-r.Context().Done():
			case <-f.closing:
			}
			return
		}
		_ = r.ParseForm()
		f.mu.Lock()
		f.tokenForm = r.PostForm
		challenge := f.authorizeQuery.Get("code_challenge")
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")

		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
			if r.PostForm.Get("code") != "FAKE-CODE" || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant"})
				return
			}
			f.mu.Lock()
			f.pkceChecked = true
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "AT-CODE", "refresh_token": "RT-CODE",
				"token_type": "Bearer", "expires_in": 3600,
			})
		case "urn:ietf:params:oauth:grant-type:device_code":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "AT-DEVICE", "refresh_token": "RT-DEVICE",
				"token_type": "Bearer", "expires_in": 3600,
			})
		default:
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "unsupported_grant_type"})
		}
	})
	return f
}

func (f *fakeIssuer) profile() Profile {
	return Profile{Issuer: f.srv.URL, ClientID: "test-client", APIURL: "https://cloud.example.test", ProjectID: "proj-1"}
}

// browserThatFollows plays the operator's browser: it fetches the
// authorization URL and follows the issuer's redirect back to the CLI's
// loopback listener, recording what the listener answered.
type browserThatFollows struct {
	mu     sync.Mutex
	opened string
	status int
	body   string
	done   chan struct{}
}

func newBrowser() *browserThatFollows { return &browserThatFollows{done: make(chan struct{})} }

func (b *browserThatFollows) open(u string) error {
	b.mu.Lock()
	b.opened = u
	b.mu.Unlock()
	go func() {
		defer close(b.done)
		resp, err := http.Get(u)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		b.mu.Lock()
		b.status, b.body = resp.StatusCode, buf.String()
		b.mu.Unlock()
	}()
	return nil
}

func TestAuthCodeLoginRoundTripsPKCEThroughTheLoopbackListener(t *testing.T) {
	issuer := newFakeIssuer(t)
	browser := newBrowser()
	var out bytes.Buffer

	tok, err := AuthCodeLogin(context.Background(), issuer.profile(), &out, Options{OpenBrowser: browser.open})
	if err != nil {
		t.Fatalf("AuthCodeLogin: %v", err)
	}
	if tok.AccessToken != "AT-CODE" || tok.RefreshToken != "RT-CODE" {
		t.Fatalf("token = %+v, want the authorization-code tokens", tok)
	}
	<-browser.done

	q := issuer.authorizeQuery
	if q.Get("response_type") != "code" || q.Get("client_id") != "test-client" {
		t.Fatalf("authorization request must be a code request for the profile's client, got %v", q)
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Fatalf("authorization request must carry an S256 PKCE challenge, got %v", q)
	}
	if !issuer.pkceChecked {
		t.Fatal("the token endpoint must have verified the code_verifier against the challenge")
	}
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || redirect.Scheme != "http" || redirect.Hostname() != "127.0.0.1" || redirect.Path != CallbackPath {
		t.Fatalf("redirect_uri must be http://127.0.0.1:<port>%s, got %q", CallbackPath, q.Get("redirect_uri"))
	}
	if issuer.tokenForm.Get("redirect_uri") != q.Get("redirect_uri") {
		t.Fatalf("the exchange must repeat the redirect_uri it authorized with, got %q", issuer.tokenForm.Get("redirect_uri"))
	}
	scopes := q.Get("scope")
	for _, want := range []string{"openid", "offline_access", projectAudienceScope("proj-1")} {
		if !strings.Contains(scopes, want) {
			t.Fatalf("scope %q must include %q (same scopes as the device flow)", scopes, want)
		}
	}
	if browser.opened == "" || !strings.HasPrefix(browser.opened, issuer.srv.URL+"/authorize?") {
		t.Fatalf("the browser must be opened at the issuer's authorization endpoint, got %q", browser.opened)
	}
	if !strings.Contains(out.String(), browser.opened) {
		t.Fatalf("the authorization URL must also be printed for an operator whose browser did not open, got: %q", out.String())
	}
	if browser.status != http.StatusOK || !strings.Contains(browser.body, "close this tab") {
		t.Fatalf("the listener must tell the operator to return to the terminal, got %d %q", browser.status, browser.body)
	}
}

func TestAuthCodeLoginIgnoresACallbackWithTheWrongState(t *testing.T) {
	issuer := newFakeIssuer(t)
	browser := newBrowser()
	var strayStatus int
	stray := func(u string) error {
		// Something else reaches the listener first with a code of its own and
		// a state that is not this login's. It must be refused and must not
		// end the login; the real redirect still completes it.
		auth, _ := url.Parse(u)
		back, _ := url.Parse(auth.Query().Get("redirect_uri"))
		back.RawQuery = url.Values{"code": {"EVIL"}, "state": {"not-this-login"}}.Encode()
		resp, err := http.Get(back.String())
		if err != nil {
			return err
		}
		resp.Body.Close()
		strayStatus = resp.StatusCode
		return browser.open(u)
	}

	tok, err := AuthCodeLogin(context.Background(), issuer.profile(), &bytes.Buffer{}, Options{OpenBrowser: stray})
	if err != nil {
		t.Fatalf("AuthCodeLogin: %v", err)
	}
	if strayStatus != http.StatusBadRequest {
		t.Fatalf("a callback with a foreign state must be answered 400, got %d", strayStatus)
	}
	if tok.AccessToken != "AT-CODE" || issuer.tokenForm.Get("code") != "FAKE-CODE" {
		t.Fatalf("only the code that arrived with this login's state may be exchanged, exchanged %q", issuer.tokenForm.Get("code"))
	}
}

func TestAuthCodeLoginFailsWhenTheOperatorRefuses(t *testing.T) {
	issuer := newFakeIssuer(t)
	issuer.deny = true
	browser := newBrowser()

	tok, err := AuthCodeLogin(context.Background(), issuer.profile(), &bytes.Buffer{}, Options{OpenBrowser: browser.open})
	if err == nil || !strings.Contains(err.Error(), "access_denied") || !strings.Contains(err.Error(), "the user refused") {
		t.Fatalf("a refused authorization must fail with the issuer's error, got err=%v", err)
	}
	if tok != nil {
		t.Fatalf("a refused authorization must return no token, got %+v", tok)
	}
	var unavailable *LoopbackUnavailableError
	if errors.As(err, &unavailable) {
		t.Fatal("a refusal after the browser opened is a failed login, not a reason to fall back")
	}
	if issuer.tokenForm != nil {
		t.Fatal("no exchange may be attempted without a code")
	}
	<-browser.done
	if browser.status != http.StatusBadRequest || !strings.Contains(browser.body, "Sign-in failed") {
		t.Fatalf("the browser must be told the sign-in failed before the listener is shut down, got %d %q", browser.status, browser.body)
	}
}

func TestAuthCodeLoginTimesOutWhenTheBrowserNeverComesBack(t *testing.T) {
	issuer := newFakeIssuer(t)
	silent := func(string) error { return nil }

	tok, err := AuthCodeLogin(context.Background(), issuer.profile(), &bytes.Buffer{}, Options{OpenBrowser: silent, Timeout: 150 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "--device-flow") {
		t.Fatalf("a timed-out login must fail and point at --device-flow, got err=%v", err)
	}
	if tok != nil {
		t.Fatalf("a timed-out login must return no token, got %+v", tok)
	}
}

// Review round `RUN:2026-09-08`: the timeout bounds the exchange too. The
// oauth2 client runs the exchange on the login's context and http.DefaultClient
// has no timeout of its own, so without the deadline a token endpoint that
// accepts the connection and never answers held the CLI forever after the
// browser had already shown "Signed in".
func TestAuthCodeLoginTimesOutWhenTheTokenEndpointNeverAnswers(t *testing.T) {
	issuer := newFakeIssuer(t)
	issuer.stallToken = true
	browser := newBrowser()

	done := make(chan struct{})
	var tok *Token
	var err error
	go func() {
		defer close(done)
		tok, err = AuthCodeLogin(context.Background(), issuer.profile(), &bytes.Buffer{}, Options{OpenBrowser: browser.open, Timeout: 300 * time.Millisecond})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the login must give up when the token endpoint stalls; it is still waiting after 5s")
	}
	if err == nil || !strings.Contains(err.Error(), "--device-flow") {
		t.Fatalf("a stalled exchange must fail as a timed-out login pointing at --device-flow, got err=%v", err)
	}
	if tok != nil {
		t.Fatalf("a timed-out login must return no token, got %+v", tok)
	}
}

func TestAuthCodeLoginReportsLoopbackUnavailableWhenNoBrowserOpens(t *testing.T) {
	issuer := newFakeIssuer(t)
	broken := func(string) error { return errors.New("exec: \"xdg-open\": executable file not found") }

	_, err := AuthCodeLogin(context.Background(), issuer.profile(), &bytes.Buffer{}, Options{OpenBrowser: broken})
	var unavailable *LoopbackUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("a browser that cannot open must be reported as the loopback path being unavailable, got %v", err)
	}
	if !strings.Contains(unavailable.Error(), "xdg-open") {
		t.Fatalf("the reason must carry the launcher's error, got %q", unavailable.Error())
	}

	_, err = AuthCodeLogin(context.Background(), issuer.profile(), &bytes.Buffer{}, Options{})
	if !errors.As(err, &unavailable) {
		t.Fatalf("no browser launcher at all must be reported the same way, got %v", err)
	}
	if issuer.authorizeHits != 0 {
		t.Fatal("nothing may reach the authorization endpoint when no browser can open")
	}
}
