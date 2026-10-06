package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type packetFilenameTransport func(*http.Request) (*http.Response, error)

func (f packetFilenameTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type packetFilenameStore struct {
	exists    bool
	content   string
	fp        string
	posts     []map[string]any
	afterPost func()
}

// REQ-CROSS-332: exercise the HTTP call path without binding a local listener.
func packetFilenameFixture(t *testing.T, create, canonical bool) (*factoryEnv, string, *packetFilenameStore) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "EPIC-ALIAS")
	origin, initial := "served-packet", "old reconnaissance\n"
	if create {
		origin, initial = "stub", packetStubMarker("reconnaissance", "epic", "EPIC-ALIAS")+"\n"
	}
	plan := scopedPullPlan{files: map[string]scopedPullFile{}}
	plan.add("packet/reconnaissance.md", []byte(initial), origin)
	if canonical {
		plan.add("packet/10-recon.md", []byte(packetStubMarker("reconnaissance", "epic", "EPIC-ALIAS")+"\n"), "stub")
	}
	if err := applyScopedPullPlan(dir, plan); err != nil {
		t.Fatal(err)
	}
	store := &packetFilenameStore{exists: !create, content: initial, fp: "original-fp"}
	pins := map[string]string{}
	if !create {
		pins["reconnaissance"] = store.fp
	}
	if err := writePacketFingerprints(dir, pins); err != nil {
		t.Fatal(err)
	}
	priorTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = priorTransport })
	http.DefaultTransport = packetFilenameTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "packet-filename.test" {
			t.Fatalf("unexpected HTTP destination: %s", r.URL)
		}
		w := httptest.NewRecorder()
		var response any
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/sync/contract":
			w.WriteHeader(http.StatusNotFound)
		case "GET /api/v1/sync/delivery-context":
			response = map[string]any{"data": staleFacts("reconnaissance")}
		case "GET /api/v1/sync/packet-sections":
			sections := []any{}
			if store.exists {
				sections = append(sections, map[string]any{"section_key": "reconnaissance", "content": store.content, "content_fingerprint": store.fp})
			}
			response = map[string]any{"data": map[string]any{"packet_sections": sections}}
		case "POST /api/v1/sync/author":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			record, _ := body["record"].(map[string]any)
			if str(record, "section_key") != "reconnaissance" || (store.exists && str(record, "expected_fingerprint") != store.fp) {
				t.Fatalf("wrong section identity or CAS: %v", record)
			}
			store.posts = append(store.posts, body)
			store.exists, store.content, store.fp = true, str(record, "content"), "accepted-fp"
			if store.afterPost != nil {
				store.afterPost()
			}
			response = map[string]any{"data": map[string]any{"packet_section": map[string]any{"fingerprint": store.fp}}}
		default:
			t.Fatalf("unexpected HTTP request: %s %s", r.Method, r.URL)
		}
		if response != nil {
			if err := json.NewEncoder(w).Encode(response); err != nil {
				t.Fatal(err)
			}
		}
		return w.Result(), nil
	})
	return &factoryEnv{Root: dir, APIURL: "https://packet-filename.test", SystemID: 1, token: "t"}, dir, store
}

func planPacketFilenameWrite(t *testing.T, env *factoryEnv, dir string) ([]plannedSection, []plannedSection) {
	t.Helper()
	var plan, skipped []string
	writes, reconcile, err := planPacketSections(env, dir, "epic", "EPIC-ALIAS", &plan, &skipped)
	if err != nil {
		t.Fatal(err)
	}
	return writes, reconcile
}

// REQ-CROSS-332#AC12: accepted writes refresh the path read during planning,
// even when its name is an alias for a numbered canonical packet filename.
func TestPacketFilenameWriteRefreshesOriginalPath(t *testing.T) {
	for _, create := range []bool{false, true} {
		for _, canonical := range []bool{false, true} {
			name := "update"
			if create {
				name = "create"
			}
			if canonical {
				name += "_with_canonical_stub"
			}
			t.Run(name, func(t *testing.T) {
				env, dir, store := packetFilenameFixture(t, create, canonical)
				before := scopeTreeBytes(t, dir)
				writePacketFile(t, dir, "reconnaissance.md", "authored reconnaissance\n")
				writes, reconcile := planPacketFilenameWrite(t, env, dir)
				if len(writes) != 1 || len(reconcile) != 0 {
					t.Fatalf("want one write, got writes=%v reconcile=%v", writes, reconcile)
				}
				if conflicts, err := applyPacketSections(env, dir, "ctx", "epic", "EPIC-ALIAS", writes); err != nil || len(conflicts) != 0 {
					t.Fatalf("accepted alias write failed its metadata refresh: conflicts=%v err=%v", conflicts, err)
				}
				if len(store.posts) != 1 || store.content != "authored reconnaissance\n" || readPacketFingerprints(dir)["reconnaissance"] != store.fp {
					t.Fatalf("write did not refresh section CAS: posts=%v pins=%v", store.posts, readPacketFingerprints(dir))
				}
				assertScopedBaselineMatchesFile(t, dir, "packet/reconnaissance.md")
				if got := scopedBaselineEntryForTest(t, dir, "packet/reconnaissance.md"); got.Origin != "served-packet" {
					t.Fatalf("accepted alias baseline origin = %q", got.Origin)
				}
				after := scopeTreeBytes(t, dir)
				if !reflect.DeepEqual(before["packet/10-recon.md"], after["packet/10-recon.md"]) {
					t.Fatal("alias write touched the canonical path")
				}
				writes, reconcile = planPacketFilenameWrite(t, env, dir)
				if len(writes)+len(reconcile) != 0 {
					t.Fatal("successful alias write needs another write or reconciliation")
				}
			})
		}
	}
}

// REQ-CROSS-332#AC12: metadata failure after acceptance is retryable without
// another POST or changes to either packet file's authored bytes.
func TestPacketFilenameRetryReconcilesOriginalPath(t *testing.T) {
	env, dir, store := packetFilenameFixture(t, false, true)
	writePacketFile(t, dir, "reconnaissance.md", "accepted reconnaissance\n")
	writes, _ := planPacketFilenameWrite(t, env, dir)
	metadata := packetFingerprintManifest(dir)
	prior, err := os.ReadFile(metadata)
	if err != nil {
		t.Fatal(err)
	}
	store.afterPost = func() {
		if err := os.Remove(metadata); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(metadata, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := applyPacketSections(env, dir, "ctx", "epic", "EPIC-ALIAS", writes); err == nil || !strings.Contains(err.Error(), "CAS metadata could not be refreshed") {
		t.Fatalf("expected accepted write followed by metadata failure, got %v", err)
	}
	if err := os.Remove(metadata); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadata, prior, 0o644); err != nil {
		t.Fatal(err)
	}
	before := scopeTreeBytes(t, dir)
	writes, reconcile := planPacketFilenameWrite(t, env, dir)
	if len(writes) != 0 || len(reconcile) != 1 {
		t.Fatalf("want metadata-only retry, got writes=%v reconcile=%v", writes, reconcile)
	}
	if err := reconcilePacketSections(dir, "epic", "EPIC-ALIAS", reconcile); err != nil {
		t.Fatal(err)
	}
	if len(store.posts) != 1 || readPacketFingerprints(dir)["reconnaissance"] != store.fp {
		t.Fatal("retry repeated the accepted write or failed to refresh CAS")
	}
	assertScopedBaselineMatchesFile(t, dir, "packet/reconnaissance.md")
	after := scopeTreeBytes(t, dir)
	for _, name := range []string{"packet/reconnaissance.md", "packet/10-recon.md"} {
		if !reflect.DeepEqual(before[name], after[name]) {
			t.Fatalf("retry changed authored bytes at %s", name)
		}
	}
}

// REQ-CROSS-384 and REQ-CROSS-332: both restamp modes retain the path and
// still protect edits made to that file after staging.
func TestPacketFilenameRestampRetainsPathAndChecksStagedBytes(t *testing.T) {
	for _, all := range []bool{false, true} {
		for _, editAfterPlan := range []bool{false, true} {
			t.Run(fmt.Sprintf("all=%t/edited=%t", all, editAfterPlan), func(t *testing.T) {
				env, dir, store := packetFilenameFixture(t, false, true)
				staged, err := staleUnchangedSections(env, dir, "epic", "EPIC-ALIAS", nil, all)
				if err != nil || len(staged) != 1 {
					t.Fatalf("want one restamp, got %v err=%v", staged, err)
				}
				if editAfterPlan {
					writePacketFile(t, dir, "reconnaissance.md", "concurrent edit\n")
				}
				before := scopeTreeBytes(t, dir)
				_, err = applyPacketSections(env, dir, "ctx", "epic", "EPIC-ALIAS", staged)
				if editAfterPlan {
					if err == nil || !strings.Contains(err.Error(), "packet/reconnaissance.md changed") || !reflect.DeepEqual(before, scopeTreeBytes(t, dir)) {
						t.Fatalf("concurrent edit must preserve local metadata and files: %v", err)
					}
				} else {
					if err != nil || readPacketFingerprints(dir)["reconnaissance"] != store.fp {
						t.Fatalf("alias restamp did not refresh CAS: %v", err)
					}
					assertScopedBaselineMatchesFile(t, dir, "packet/reconnaissance.md")
				}
			})
		}
	}
}
