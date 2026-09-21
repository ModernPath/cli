package zitadel

import (
	"context"
	"net/http"
	"net/http/httptest"

	"golang.org/x/oauth2"
)

// Serving a fake issuer in memory, and why the device-flow tests need it.
//
// The device flow spends nearly all of its wall time asleep between polls: an
// `interval` of 1 second is the smallest RFC 8628 allows, and slow_down adds
// five more. Run on the real clock, the eight polling tests in this package
// cost ~24s of doing nothing.
//
// `testing/synctest` exists for exactly that — inside a bubble the clock jumps
// to the next timer as soon as every goroutine is durably blocked. But a
// goroutine blocked on network I/O is never durably blocked, so a bubble that
// talks to an `httptest.Server` over a real socket stops advancing and the
// test hangs until the whole run times out. That is not a theory: bubbling the
// polling tests against the live server hung 5 runs out of 5, and a listener's
// Accept loop inside a bubble hangs the same way.
//
// So the bubbled tests keep the handlers and drop the socket. muxTransport
// answers each request from a mux directly, on the calling goroutine — no
// listener, no connection, no background reader, nothing for the bubble to
// wait on. The handlers are the same ones the live servers use and still
// receive a fully encoded request, so assertions about what a login "sends on
// the wire" keep their meaning.
//
// What this deliberately does not cover is the socket itself. Tests that are
// about real I/O — the loopback listener, the browser redirect, a token
// endpoint that accepts a connection and never answers — keep their live
// server and their real clock; see login_test.go and authcode_test.go.
type muxTransport struct{ mux *http.ServeMux }

func (t muxTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// A real transport fails a request whose context is already done rather
	// than serving it; the polling loop relies on that once the device code
	// has expired.
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	rec := httptest.NewRecorder()
	t.mux.ServeHTTP(rec, r)
	resp := rec.Result()
	resp.Request = r
	return resp, nil
}

// withInMemoryIssuer routes every HTTP request this login makes — discovery,
// device authorization and each token poll — at mux, with no network in
// between.
func withInMemoryIssuer(ctx context.Context, mux *http.ServeMux) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, &http.Client{Transport: muxTransport{mux}})
}
