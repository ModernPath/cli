package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func assertTraceFlagRefusalBeforeRequests(t *testing.T, want string, flags ...string) {
	t.Helper()
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "invalid trace flags must not contact the server", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	cobraWorkspace(t, srv)
	args := []string{"author", "trace", "TRACE-FLAGS", "--scope", "EPIC-F", "--verdict", "PASS"}
	_, err := runRoot(t, append(args, flags...)...)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("want flag refusal containing %q, got %v", want, err)
	}
	if got := requests.Load(); got != 0 {
		t.Errorf("invalid trace flags made %d server requests", got)
	}
}

func TestAuthorTraceRejectsReviewContextForOtherPurposesBeforeRequests(t *testing.T) {
	for _, purpose := range []string{"entry", "lower", "upper", "completion", "custom", ""} {
		t.Run(purpose, func(t *testing.T) {
			assertTraceFlagRefusalBeforeRequests(t, "--review-context is only valid with --purpose cold-review",
				"--purpose", purpose, "--from", "build", "--to", "verify", "--review-context", "review-selected")
		})
	}
	t.Run("explicit_empty_selector", func(t *testing.T) {
		assertTraceFlagRefusalBeforeRequests(t, "--review-context is only valid with --purpose cold-review",
			"--purpose", "entry", "--review-context=")
	})
}

func TestAuthorTraceRequiresReviewContextBeforeRequests(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags []string
	}{
		{"missing", nil},
		{"empty", []string{"--review-context="}},
		{"whitespace", []string{"--review-context", " \t "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flags := append([]string{"--purpose", "cold-review"}, tc.flags...)
			assertTraceFlagRefusalBeforeRequests(t, "--purpose cold-review requires --review-context", flags...)
		})
	}
}
