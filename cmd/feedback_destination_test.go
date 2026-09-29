package cmd

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
)

func feedbackAuth(t *testing.T, organization, issuer string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"iss":                     issuer,
		"urn:modernpath:token:v2": map[string]any{"org_id": organization},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := config.WriteAuth(&config.Auth{
		Token:       "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature",
		WorkspaceID: "371734807656268047", WorkspaceName: "ModernPath",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFeedbackWritesOnlyFromModernPathWorkspace(t *testing.T) {
	var got map[string]any
	var readSystem string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/systems":
			_ = json.NewEncoder(w).Encode([]api.System{{ID: 1, Slug: "customer"}, {ID: 7, Slug: "modernpath"}})
		case "/api/v1/sync/backlog":
			readSystem = r.URL.Query().Get("system_id")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"backlog": toolRows("BACKLOG-TOOL-9")}})
		case "/api/v1/sync/author":
			_ = json.NewDecoder(r.Body).Decode(&got)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"backlog": map[string]any{"external_id": "BACKLOG-TOOL-10"}}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	feedbackWorkspace(t, srv)
	if _, err := runRoot(t, "feedback", "a tooling defect"); err != nil {
		t.Fatal(err)
	}
	if readSystem != "7" || got["system_id"] != float64(7) {
		t.Fatalf("feedback must read and write only ModernPath: read=%q posted=%v", readSystem, got)
	}
	cfg, err := config.ReadConfig()
	if err != nil || cfg.SystemID != 7 {
		t.Fatalf("feedback must preserve the checkout binding: cfg=%v err=%v", cfg, err)
	}
}

func TestFeedbackRefusesCustomerWorkspaceDespiteModernPathAccess(t *testing.T) {
	var requests, writes int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/api/systems" {
			writes++
		}
		_ = json.NewEncoder(w).Encode([]api.System{{ID: 1, Slug: "customer"}, {ID: 7, Slug: "modernpath"}})
	}))
	t.Cleanup(srv.Close)
	cobraWorkspace(t, srv) // Bound to customer system 1, with access to ModernPath.
	feedbackAuth(t, "371734807656268047", zitadel.ProdProfile.Issuer)
	// A cached slug alone must not authorize the destination.
	cfg, err := config.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.SystemSlug = "modernpath"
	if err := config.WriteConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := runRoot(t, "feedback", "a tooling defect"); err == nil {
		t.Fatal("customer workspace must be refused even with access to ModernPath")
	}
	if requests != 1 || writes != 0 {
		t.Fatalf("only the identity read is allowed: requests=%d other requests=%d", requests, writes)
	}
	cfg, err = config.ReadConfig()
	if err != nil || cfg.SystemID != 1 {
		t.Fatalf("feedback must preserve the checkout binding: cfg=%v err=%v", cfg, err)
	}
	if _, err := os.Stat(filepath.Join("process", "tooling-gaps.md")); !os.IsNotExist(err) {
		t.Fatalf("feedback must not create a customer-local fallback: %v", err)
	}
}

func TestFeedbackRefusesUnverifiedDestinations(t *testing.T) {
	for _, c := range []struct {
		name, organization, issuer string
		systems                    []api.System
		status                     int
	}{
		{"customer credential despite cached ModernPath identity", "customer-org", zitadel.ProdProfile.Issuer, []api.System{{ID: 1, Slug: "modernpath"}}, 200},
		{"missing organization claim", "", zitadel.ProdProfile.Issuer, []api.System{{ID: 7, Slug: "modernpath"}}, 200},
		{"different issuer", "371734807656268047", zitadel.TestProfile.Issuer, []api.System{{ID: 7, Slug: "modernpath"}}, 200},
		{"customer system named ModernPath", "371734807656268047", zitadel.ProdProfile.Issuer, []api.System{{ID: 7, Name: "ModernPath", Slug: "customer"}}, 200},
		{"ModernPath exists but bound system is not accessible", "371734807656268047", zitadel.ProdProfile.Issuer, []api.System{{ID: 8, Slug: "modernpath"}}, 200},
		{"invalid system id", "371734807656268047", zitadel.ProdProfile.Issuer, []api.System{{Slug: "modernpath"}}, 200},
		{"failed identity lookup", "371734807656268047", zitadel.ProdProfile.Issuer, nil, 500},
		{"rejected credential on identity lookup", "371734807656268047", zitadel.ProdProfile.Issuer, nil, 401},
	} {
		t.Run(c.name, func(t *testing.T) {
			var writes int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					writes++
				}
				if r.URL.Path == "/api/systems" {
					w.WriteHeader(c.status)
					_ = json.NewEncoder(w).Encode(c.systems)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"backlog": []any{}}})
			}))
			t.Cleanup(srv.Close)
			feedbackWorkspace(t, srv)
			feedbackAuth(t, c.organization, c.issuer)
			if _, err := runRoot(t, "feedback", "a tooling defect"); err == nil {
				t.Fatal("unverified feedback destination must be refused")
			}
			if writes != 0 {
				t.Fatalf("unverified destination received %d writes", writes)
			}
			if _, err := os.Stat(filepath.Join("process", "tooling-gaps.md")); !os.IsNotExist(err) {
				t.Fatalf("feedback must not create a customer-local fallback: %v", err)
			}
		})
	}
}

func TestFeedbackUnavailableServerWritesNoFallback(t *testing.T) {
	srv, _ := feedbackServer(t, nil, 200)
	feedbackWorkspace(t, srv)
	srv.Close()
	if _, err := runRoot(t, "feedback", "a tooling defect"); err == nil {
		t.Fatal("unreachable destination must be refused")
	}
	if _, err := os.Stat(filepath.Join("process", "tooling-gaps.md")); !os.IsNotExist(err) {
		t.Fatalf("no local fallback may be written: %v", err)
	}
}
