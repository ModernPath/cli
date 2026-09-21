package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// B9 (REQ-CROSS-318): `process cascade-mode <report|enforce> --source USER:...`
// posts the cascade_mode author action, top-level mode+source. RED:
// processCascadeMode does not exist, so the package fails to build.
func TestProcessCascadeModePostsTheAction(t *testing.T) {
	var got map[string]any
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sync/author" {
			gotMethod = r.Method
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &got)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"cascade_mode":{"mode":"enforce"}}}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 5, token: "t"}
	if err := processCascadeMode(env, "enforce", "USER:2026-09-05:x"); err != nil {
		t.Fatalf("processCascadeMode: %v", err)
	}
	if gotMethod != "POST" {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if got["action"] != "cascade_mode" || got["mode"] != "enforce" || got["source"] != "USER:2026-09-05:x" {
		t.Fatalf("payload = %v; want action=cascade_mode mode=enforce source=USER:2026-09-05:x", got)
	}
}

// The client refuses a bad mode and a source that is not a USER: attribution,
// before any network call (mirroring the server's own refusal).
func TestProcessCascadeModeRejectsBadModeAndMissingSource(t *testing.T) {
	saved := processCascadeSource
	defer func() { processCascadeSource = saved }()

	processCascadeSource = "USER:2026-09-05:x"
	if err := processCascadeModeCmd.RunE(processCascadeModeCmd, []string{"bogus"}); err == nil {
		t.Fatal("a mode outside {report,enforce} must be refused")
	}
	processCascadeSource = "not-a-user-source"
	if err := processCascadeModeCmd.RunE(processCascadeModeCmd, []string{"enforce"}); err == nil {
		t.Fatal("a --source without a USER: prefix must be refused")
	}
}

// REQ-CROSS-342: `process cascade-mode` with no argument READS the current mode
// (defaulted, purpose-built) without changing it — GET /sync/cascade-mode, no
// --source, and never an author POST. RED: processCascadeModeShow does not exist,
// so the package fails to build.
func TestProcessCascadeModeReadsWithoutSetting(t *testing.T) {
	var gotMethod, gotPath string
	posted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/sync/cascade-mode" && r.Method == "GET":
			gotMethod, gotPath = r.Method, r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"cascade_mode":{"mode":"report","governs":["supersede","content_drift"],"not_governed":["result_recording"]}}}`))
		case r.URL.Path == "/api/v1/sync/author":
			posted = true
			w.WriteHeader(500)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 5, token: "t"}
	if err := processCascadeModeShow(env); err != nil {
		t.Fatalf("processCascadeModeShow: %v", err)
	}
	if gotMethod != "GET" || gotPath != "/api/v1/sync/cascade-mode" {
		t.Fatalf("read called %s %q; want GET /api/v1/sync/cascade-mode", gotMethod, gotPath)
	}
	if posted {
		t.Fatal("a read must not POST a cascade_mode set")
	}
}

// REQ-CROSS-342: the command accepts zero args (read) or one (set); a read must
// not demand the write's --source. RED: Args is ExactArgs(1), so zero args is
// refused today.
func TestProcessCascadeModeAcceptsZeroOrOneArg(t *testing.T) {
	if err := processCascadeModeCmd.Args(processCascadeModeCmd, []string{}); err != nil {
		t.Fatalf("zero args (read) must be accepted: %v", err)
	}
	if err := processCascadeModeCmd.Args(processCascadeModeCmd, []string{"report"}); err != nil {
		t.Fatalf("one arg (set) must be accepted: %v", err)
	}
	if err := processCascadeModeCmd.Args(processCascadeModeCmd, []string{"a", "b"}); err == nil {
		t.Fatal("two args must be refused")
	}
}

// REQ-CROSS-342 review nit: the no-argument read refuses a --source (the write's
// attribution) rather than silently reading, so a caller who meant to SET but
// dropped the mode argument gets an error, not the current mode. The guard fires
// before any network call.
func TestProcessCascadeModeReadRejectsSource(t *testing.T) {
	saved := processCascadeSource
	defer func() { processCascadeSource = saved }()

	processCascadeSource = "USER:2026-09-11:oops"
	err := processCascadeModeCmd.RunE(processCascadeModeCmd, []string{})
	if err == nil {
		t.Fatal("a --source with no mode argument must be refused (the no-arg form is a read)")
	}
	if !strings.Contains(err.Error(), "--source") {
		t.Fatalf("error should explain --source is for setting; got %v", err)
	}
}

// REQ-CROSS-342 review nit: the read surfaces a non-200 from the server as an
// error rather than printing a bogus mode.
func TestProcessCascadeModeShowErrorsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 5, token: "t"}
	if err := processCascadeModeShow(env); err == nil {
		t.Fatal("a non-200 read must return an error")
	}
}
