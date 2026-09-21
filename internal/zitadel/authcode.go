package zitadel

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/oauth2"
)

// CallbackPath is the path of the loopback redirect URI. It must equal the
// path registered on the modernpath-cli native app in ZITADEL
// (live-infrastructure, zitadel-project-platform, both planes register
// http://localhost:8766/api/identity/callback and its 127.0.0.1 twin). For a
// native client ZITADEL accepts an unregistered loopback redirect whose path
// matches a registered loopback URI and ignores host and port
// (zitadel/oidc pkg/op validateAuthReqRedirectURINative, RFC 8252 §7.3) —
// which is what lets the listener take any free port.
const CallbackPath = "/api/identity/callback"

// DefaultLoginTimeout bounds the loopback flow end to end: the wait for the
// browser to come back with an authorization code and the exchange of that
// code at the token endpoint.
const DefaultLoginTimeout = 5 * time.Minute

// LoopbackUnavailableError reports that the authorization-code flow could not
// start here: no loopback listener could be bound, or no browser could be
// opened. Nothing has been shown to the operator yet when it is returned, so
// Login treats it as the signal to hand over to the device flow — which is
// why the authorization URL is printed only after the browser opens. Every
// other error from AuthCodeLogin is a failed login.
type LoopbackUnavailableError struct {
	Reason string
	Err    error
}

func (e *LoopbackUnavailableError) Error() string {
	if e.Err == nil {
		return e.Reason
	}
	return e.Reason + ": " + e.Err.Error()
}

func (e *LoopbackUnavailableError) Unwrap() error { return e.Err }

// AuthCodeLogin runs OAuth 2.0 authorization code with PKCE (RFC 7636) and a
// loopback redirect (RFC 8252 §7.3): OIDC discovery, a listener on a free
// 127.0.0.1 port, the authorization URL opened in the operator's browser, and
// the code the browser brings back exchanged at the token endpoint. The
// verifier never leaves the process and the state parameter must round-trip,
// so a code delivered to the listener by anything other than this login is
// ignored.
func AuthCodeLogin(ctx context.Context, profile Profile, out io.Writer, opts Options) (*Token, error) {
	conf, err := oauthConfig(ctx, profile)
	if err != nil {
		return nil, err
	}
	withOrganizationFilter(conf, opts)
	return authCodeLogin(ctx, conf, out, opts)
}

func authCodeLogin(ctx context.Context, conf *oauth2.Config, out io.Writer, opts Options) (*Token, error) {
	if opts.OpenBrowser == nil {
		return nil, &LoopbackUnavailableError{Reason: "no browser launcher is configured"}
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, &LoopbackUnavailableError{Reason: "cannot open a loopback listener", Err: err}
	}
	conf.RedirectURL = fmt.Sprintf("http://127.0.0.1:%d%s", ln.Addr().(*net.TCPAddr).Port, CallbackPath)

	verifier := oauth2.GenerateVerifier()
	state, err := randomState()
	if err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("generate login state: %w", err)
	}
	authURL := conf.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))

	results := make(chan callbackResult, 1)
	srv := &http.Server{Handler: callbackHandler(state, results), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	// Nothing about this login is printed until the browser is open: a
	// launcher that fails hands over to the device flow, and an authorization
	// URL left on screen above the device URL would invite the operator to a
	// listener this function is about to close.
	if err := opts.OpenBrowser(authURL); err != nil {
		return nil, &LoopbackUnavailableError{Reason: "cannot open a browser", Err: err}
	}
	fmt.Fprintf(out, "\nOpening your browser to sign in. If it does not open, open this URL:\n  %s\n\n", authURL)

	// One deadline covers the wait and the exchange. The exchange runs on
	// this context through the oauth2 client, so a token endpoint that
	// accepts the connection and never answers is bounded too — without it
	// the CLI hung after the browser had already shown "Signed in".
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultLoginTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	timedOut := func() error {
		return fmt.Errorf("no sign-in completed within %s. Run 'modernpath auth' again, or 'modernpath auth --device-flow' if no browser on this machine can reach the CLI", timeout)
	}

	var res callbackResult
	select {
	case res = <-results:
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, timedOut()
		}
		return nil, ctx.Err()
	}
	if res.err != nil {
		return nil, res.err
	}

	tok, err := conf.Exchange(ctx, res.code, oauth2.VerifierOption(verifier))
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, timedOut()
		}
		return nil, fmt.Errorf("authorization code exchange: %w", err)
	}
	return tokenFrom(tok, FlowLoopback), nil
}

type callbackResult struct {
	code string
	err  error
}

// callbackHandler answers the browser's redirect. A request whose state does
// not match this login is answered 400 and otherwise ignored — it cannot
// complete or cancel the login, so a stray request to the port is harmless.
// The first matching callback decides; a reload after it is answered but not
// acted on.
func callbackHandler(state string, results chan<- callbackResult) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(CallbackPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
			http.Error(w, "Sign-in failed: this response does not belong to the current login. Return to the terminal.", http.StatusBadRequest)
			return
		}

		var res callbackResult
		switch {
		case q.Get("error") != "":
			msg := q.Get("error")
			if d := q.Get("error_description"); d != "" {
				msg += ": " + d
			}
			res.err = fmt.Errorf("sign-in refused by the identity provider: %s", msg)
		case q.Get("code") == "":
			res.err = errors.New("the browser returned without an authorization code")
		default:
			res.code = q.Get("code")
		}

		// Answer the browser, and get the answer onto the wire, before
		// publishing the result: taking the result releases authCodeLogin,
		// whose deferred Close cuts a connection that is still being written
		// to — which would reach the operator as a reset connection instead
		// of the outcome of their sign-in.
		if res.err != nil {
			writeCallbackPage(w, http.StatusBadRequest, "text/plain; charset=utf-8",
				"Sign-in failed: "+res.err.Error()+". Return to the terminal.\n")
		} else {
			writeCallbackPage(w, http.StatusOK, "text/html; charset=utf-8", successPage)
		}

		select {
		case results <- res:
		default:
		}
	})
	return mux
}

// writeCallbackPage writes one complete response and pushes it onto the
// connection. The length is declared so that the flush leaves nothing to be
// written after the handler returns; without it the response would be chunked
// and its terminating chunk could be lost to the server's close.
func writeCallbackPage(w http.ResponseWriter, status int, contentType, body string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

const successPage = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>ModernPath CLI</title>
<style>body{font-family:system-ui,sans-serif;margin:4rem auto;max-width:32rem;text-align:center;color:#222}</style></head>
<body><h1>Signed in</h1><p>The ModernPath CLI has received your login. You can close this tab and return to the terminal.</p></body></html>`

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
