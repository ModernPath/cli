package cmd

// REQ-CROSS-097: a bound workspace records which system it is bound to — by
// name and slug, not only by id. The lookup is injected, so no network.

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modernpath/cli/internal/config"
)

func TestConnectRecordsTheSystemsNameAndSlug(t *testing.T) {
	cfg := &config.Config{}
	if err := resolveBinding(cfg, 243, "https://beta.modernpath.ai"); err != nil {
		t.Fatal(err)
	}

	lookup := func(id int) (string, string, error) {
		if id != 243 {
			t.Errorf("looked up system %d, want the one just bound (243)", id)
		}
		return "Modernpath", "modernpath", nil
	}
	if err := recordSystemIdentity(cfg, lookup); err != nil {
		t.Fatalf("the lookup succeeded, so identity must be recorded: %v", err)
	}

	if cfg.SystemName != "Modernpath" {
		t.Errorf("system name = %q, want %q", cfg.SystemName, "Modernpath")
	}
	if cfg.SystemSlug != "modernpath" {
		t.Errorf("system slug = %q, want %q", cfg.SystemSlug, "modernpath")
	}
}

// The repair path: an existing config predating this change is fixed by
// re-running connect, which must not demand the id the config already holds.
func TestConnectRepairsAnExistingBindingWithoutASystemFlag(t *testing.T) {
	cfg := &config.Config{SystemID: 243, APIURL: "https://beta.modernpath.ai"}

	if err := resolveBinding(cfg, 0, ""); err != nil {
		t.Fatalf("re-running connect on a bound workspace must repair it, not require a re-init: %v", err)
	}
	if cfg.SystemID != 243 {
		t.Errorf("system id = %d, want the bound 243 kept", cfg.SystemID)
	}
	if cfg.APIURL != "https://beta.modernpath.ai" {
		t.Errorf("api url = %q, want the bound one kept", cfg.APIURL)
	}
}

func TestConnectRebindsWhenGivenADifferentSystem(t *testing.T) {
	cfg := &config.Config{SystemID: 243, SystemName: "Modernpath", SystemSlug: "modernpath"}

	if err := resolveBinding(cfg, 45, ""); err != nil {
		t.Fatal(err)
	}
	if cfg.SystemID != 45 {
		t.Errorf("system id = %d, want 45", cfg.SystemID)
	}

	if err := recordSystemIdentity(cfg, func(int) (string, string, error) {
		return "Other", "other", nil
	}); err != nil {
		t.Fatal(err)
	}
	if cfg.SystemName != "Other" || cfg.SystemSlug != "other" {
		t.Errorf("identity = %q/%q, want it to follow the new binding", cfg.SystemName, cfg.SystemSlug)
	}
}

// Rebinding to a system whose lookup then fails must leave no identity at all.
// A blank name is honest; system 45 wearing system 243's name is a lie, and the
// config is what every other command reads.
func TestConnectDoesNotCarryTheOldSystemsIdentityToANewOne(t *testing.T) {
	cfg := &config.Config{SystemID: 243, SystemName: "Modernpath", SystemSlug: "modernpath"}

	if err := resolveBinding(cfg, 45, ""); err != nil {
		t.Fatal(err)
	}
	_ = recordSystemIdentity(cfg, func(int) (string, string, error) {
		return "", "", errors.New("offline")
	})

	if cfg.SystemName != "" || cfg.SystemSlug != "" {
		t.Errorf("identity = %q/%q, want it cleared when the bound system changed", cfg.SystemName, cfg.SystemSlug)
	}
}

// The same lie one axis over: the identity was read from a SERVER, so pointing
// the binding at a different server invalidates it even when the numeric id is
// unchanged — system 243 on another host is another system's 243.
func TestConnectDoesNotCarryTheOldServersIdentityToANewOne(t *testing.T) {
	cfg := &config.Config{SystemID: 243, SystemName: "Modernpath", SystemSlug: "modernpath",
		APIURL: "http://localhost:4000"}

	if err := resolveBinding(cfg, 243, "https://beta.modernpath.ai"); err != nil {
		t.Fatal(err)
	}
	_ = recordSystemIdentity(cfg, func(int) (string, string, error) {
		return "", "", errors.New("offline")
	})

	if cfg.SystemName != "" || cfg.SystemSlug != "" {
		t.Errorf("identity = %q/%q, want it cleared when the server changed", cfg.SystemName, cfg.SystemSlug)
	}
}

// Guard: a repair re-run against the SAME server keeps the recorded identity
// when the lookup fails — that is what the advisory error exists to allow.
func TestConnectKeepsIdentityWhenRebindingTheSameServerOffline(t *testing.T) {
	cfg := &config.Config{SystemID: 243, SystemName: "Modernpath", SystemSlug: "modernpath",
		APIURL: "https://beta.modernpath.ai"}

	if err := resolveBinding(cfg, 243, "https://beta.modernpath.ai"); err != nil {
		t.Fatal(err)
	}
	_ = recordSystemIdentity(cfg, func(int) (string, string, error) {
		return "", "", errors.New("offline")
	})

	if cfg.SystemName != "Modernpath" || cfg.SystemSlug != "modernpath" {
		t.Errorf("identity = %q/%q, want the same server's identity kept", cfg.SystemName, cfg.SystemSlug)
	}
}

// The warning after a failed identity lookup names the step that fixes it.
// "Re-run once the server is reachable" is only true for reachability: a
// credential failure re-fails identically on every re-run, and the loop hides
// the real repair.
func TestConnectHintMatchesTheFailureItExplains(t *testing.T) {
	env := &factoryEnv{APIURL: "https://api.workload.test-plat.modernpath.ai"}
	if hint := identityRepairHint(env.credentialRejected()); !strings.Contains(hint, "modernpath auth --sso --test") {
		t.Errorf("a credential failure must use the exact target-aware repair, got %q", hint)
	}
	if hint := identityRepairHint(errors.New("dial tcp: connection refused")); !strings.Contains(hint, "reachable") {
		t.Errorf("a reachability failure must suggest the re-run, got %q", hint)
	}
}

// A 401 does not say why the bearer was rejected, so the message names the
// possible causes and the exact target-aware repair.
func TestCredentialRejectedReportsWhatIsEstablished(t *testing.T) {
	bearer := (&factoryEnv{APIURL: "https://beta.modernpath.ai"}).credentialRejected().Error()
	if !strings.Contains(bearer, "expired, revoked, or issued for a different server") {
		t.Errorf("the bearer message asserts one cause: %q", bearer)
	}
	if !strings.Contains(bearer, "modernpath auth --api-url=https://beta.modernpath.ai") {
		t.Errorf("the beta repair must preserve beta exactly: %q", bearer)
	}
}

// The lookup finds its credentials by reading the workspace's own config back
// off disk, so a first connect can only succeed if the binding is saved first.
func TestConnectPersistsTheBindingBeforeLookingUpIdentity(t *testing.T) {
	saved := false
	save := func(*config.Config) error { saved = true; return nil }
	lookup := func(int) (string, string, error) {
		if !saved {
			return "", "", errors.New("not connected — credentials come from a config that is not written yet")
		}
		return "Modernpath", "modernpath", nil
	}

	cfg := &config.Config{}
	res, err := connectWorkspace(cfg, 243, "https://beta.modernpath.ai", save, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if res.IdentityErr != nil {
		t.Fatalf("a first connect must still record identity: %v", res.IdentityErr)
	}
	if cfg.SystemName != "Modernpath" || cfg.SystemSlug != "modernpath" {
		t.Errorf("identity = %q/%q, want it recorded on a first connect", cfg.SystemName, cfg.SystemSlug)
	}
}

// The guard: a failed lookup must not cost the binding.
func TestConnectSavesTheBindingWhenTheLookupFails(t *testing.T) {
	writes := 0
	save := func(*config.Config) error { writes++; return nil }

	cfg := &config.Config{}
	res, err := connectWorkspace(cfg, 243, "", save, func(int) (string, string, error) {
		return "", "", errors.New("no credentials")
	})
	if err != nil {
		t.Fatalf("a failed lookup must not fail the connect: %v", err)
	}
	if res.IdentityErr == nil {
		t.Error("the lookup failure must be reported so connect can warn")
	}
	if writes == 0 {
		t.Error("the binding must be saved even when identity cannot be read")
	}
	if cfg.SystemID != 243 {
		t.Errorf("system id = %d, want 243 bound regardless", cfg.SystemID)
	}
}

// The guard: with nothing bound and no flag there is no system to talk about.
func TestConnectStillNeedsASystemWhenNothingIsBound(t *testing.T) {
	cfg := &config.Config{}

	if err := resolveBinding(cfg, 0, ""); err == nil {
		t.Error("with no binding and no --system, connect must say so rather than write an empty binding")
	}
}

// The guard: binding a workspace must not become network-dependent.
func TestConnectWritesTheBindingEvenWhenTheLookupFails(t *testing.T) {
	cfg := &config.Config{}
	if err := resolveBinding(cfg, 243, "https://beta.modernpath.ai"); err != nil {
		t.Fatal(err)
	}

	err := recordSystemIdentity(cfg, func(int) (string, string, error) {
		return "", "", errors.New("no credentials")
	})
	if err == nil {
		t.Error("a failed lookup must be reported so connect can warn, not swallowed")
	}
	if cfg.SystemID != 243 || cfg.APIURL == "" {
		t.Error("the binding must survive a failed identity lookup")
	}
}

// The guard: an offline re-run degrades to what it already knew.
func TestConnectKeepsRecordedIdentityWhenALaterLookupFails(t *testing.T) {
	cfg := &config.Config{SystemID: 243, SystemName: "Modernpath", SystemSlug: "modernpath"}

	_ = recordSystemIdentity(cfg, func(int) (string, string, error) {
		return "", "", errors.New("offline")
	})

	if cfg.SystemName != "Modernpath" || cfg.SystemSlug != "modernpath" {
		t.Errorf("identity = %q/%q, want an offline re-run to leave what was already recorded", cfg.SystemName, cfg.SystemSlug)
	}
}

// The archival document ingest makes a first `migrate run` legitimately take
// minutes server-side (183 records × a real embedding batch attempt), and the
// default 120s ceiling abandoned a batch the server then finished without the
// client (two timed-out runs). The timeout must be per-env:
// migrate run raises it; everything else keeps the default.
func TestFactoryCallHonorsConfiguredTimeout(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer slow.Close()

	tight := &factoryEnv{APIURL: slow.URL, token: "t", callTimeout: 50 * time.Millisecond}
	if _, _, err := tight.call("GET", "/x", nil); err == nil {
		t.Fatalf("a 50ms budget must not survive a 300ms server")
	}

	roomy := &factoryEnv{APIURL: slow.URL, token: "t", callTimeout: 2 * time.Second}
	if status, _, err := roomy.call("GET", "/x", nil); err != nil || status != http.StatusOK {
		t.Fatalf("a 2s budget must survive a 300ms server: status=%d err=%v", status, err)
	}
}
