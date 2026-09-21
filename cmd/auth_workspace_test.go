package cmd

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
)

// REQ-CROSS-337 — the stored credential names the workspace the token was
// issued for, a token that landed somewhere else is refused rather than
// stored, and the credential file is always written whole.

// platformJWT builds an unsigned token shaped like a platform access token:
// the issuer, the home organization and the platform claim naming the
// organization the token acts in.
func platformJWT(t *testing.T, issuer, home, org string) string {
	t.Helper()
	payload := map[string]any{"iss": issuer, "sub": "u1"}
	if home != "" {
		payload["urn:zitadel:iam:user:resourceowner:id"] = home
	}
	if org != "" {
		payload["urn:modernpath:token:v2"] = map[string]any{"org_id": org, "roles": []string{"platform.user"}}
	}
	enc := base64.RawURLEncoding.EncodeToString
	header, _ := json.Marshal(map[string]string{"alg": "none"})
	body, _ := json.Marshal(payload)
	return enc(header) + "." + enc(body) + "." + enc([]byte("sig"))
}

func readAuthFile(t *testing.T, dir string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, ".modernpath", "auth.json"))
	if err != nil {
		t.Fatalf("read auth.json: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("auth.json is not JSON: %v", err)
	}
	return m
}

const testIssuer = "https://id.example.test"

func TestSaveTokensRefusesATokenThatLandedInAnotherWorkspace(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	chosen := workspaceChoice{ID: "org-b", Name: "Beta", Issuer: testIssuer, Chosen: true}

	err := saveTokens(zitadel.TestProfile.APIURL, platformJWT(t, testIssuer, "org-home", "org-home"), "rt", chosen)
	if err == nil {
		t.Fatal("a token whose platform claim names another workspace must be refused as a command error")
	}
	for _, want := range []string{"org-home", "org-b"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want it to name both %q", err, want)
		}
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".modernpath", "auth.json")); statErr == nil {
		t.Fatal("a refused token must write no credential file")
	}
}

func TestSaveTokensRecordsTheChosenWorkspaceWhenTheTokenNamesIt(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	chosen := workspaceChoice{ID: "org-b", Name: "Beta", Issuer: testIssuer, Chosen: true}

	if err := saveTokens(zitadel.TestProfile.APIURL, platformJWT(t, testIssuer, "org-home", "org-b"), "rt", chosen); err != nil {
		t.Fatalf("saveTokens: %v", err)
	}
	got := readAuthFile(t, dir)
	if got["workspace_id"] != "org-b" || got["workspace_name"] != "Beta" || got["issuer"] != testIssuer {
		t.Fatalf("auth.json = %v, want workspace org-b named Beta from %s", got, testIssuer)
	}
}

func TestSaveTokensRecordsTheTokensOwnWorkspaceWhenNoneWasChosen(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := saveTokens(zitadel.TestProfile.APIURL, platformJWT(t, testIssuer, "org-home", "org-home"), "rt", workspaceChoice{Name: "Home Org", Issuer: testIssuer}); err != nil {
		t.Fatalf("saveTokens: %v", err)
	}
	got := readAuthFile(t, dir)
	if got["workspace_id"] != "org-home" || got["workspace_name"] != "Home Org" || got["issuer"] != testIssuer {
		t.Fatalf("auth.json = %v, want the token's own workspace recorded", got)
	}
}

// The pasted-token path has no profile: a JWT carrying the claim records its
// organization and its own issuer, with no name (EPIC-CLI-009 D7, CR-12).
func TestSaveTokensRecordsAPastedJWTsOwnIssuerAndOrganization(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := saveTokens(config.LocalAPIURL, platformJWT(t, "https://local-issuer.test", "", "org-local"), "", workspaceChoice{}); err != nil {
		t.Fatalf("saveTokens: %v", err)
	}
	got := readAuthFile(t, dir)
	if got["workspace_id"] != "org-local" || got["issuer"] != "https://local-issuer.test" {
		t.Fatalf("auth.json = %v, want the claim's organization and the token's issuer", got)
	}
	if _, named := got["workspace_name"]; named {
		t.Fatalf("auth.json = %v, want no name for a pasted token", got)
	}
}

func TestSaveTokensStoresANonJWTWithNoWorkspaceAndClearsAnOldOne(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := config.WriteAuth(&config.Auth{Token: "old", WorkspaceID: "org-b", WorkspaceName: "Beta", Issuer: testIssuer}); err != nil {
		t.Fatal(err)
	}

	if err := saveTokens(config.LocalAPIURL, "opaque-token", "", workspaceChoice{}); err != nil {
		t.Fatalf("saveTokens: %v", err)
	}
	got := readAuthFile(t, dir)
	if got["token"] != "opaque-token" {
		t.Fatalf("auth.json = %v, want the new token", got)
	}
	for _, key := range []string{"workspace_id", "workspace_name", "issuer"} {
		if _, present := got[key]; present {
			t.Fatalf("auth.json = %v, want %q gone: the file is written whole", got, key)
		}
	}
}

// A chosen workspace whose token cannot be read at all is refused too: the
// choice cannot be confirmed, and storing it would be a guess.
func TestSaveTokensRefusesAChosenWorkspaceItCannotConfirm(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	err := saveTokens(zitadel.TestProfile.APIURL, "opaque-token", "rt", workspaceChoice{ID: "org-b", Issuer: testIssuer, Chosen: true})
	if err == nil {
		t.Fatal("a chosen workspace that the token cannot confirm must be refused")
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".modernpath", "auth.json")); statErr == nil {
		t.Fatal("a refused token must write no credential file")
	}
}

// A token whose platform claim exists but names no workspace — an identity
// service that predates workspace-scoped tokens — cannot confirm a chosen
// workspace either. The refusal says so instead of printing a blank
// workspace id and blaming access.
func TestSaveTokensRefusesAChosenWorkspaceWhenTheTokenNamesNone(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	chosen := workspaceChoice{ID: "org-b", Name: "Beta", Issuer: testIssuer, Chosen: true}

	err := saveTokens(zitadel.TestProfile.APIURL, platformJWT(t, testIssuer, "org-home", ""), "rt", chosen)
	if err == nil {
		t.Fatal("a chosen workspace that the token does not name must be refused")
	}
	if !strings.Contains(err.Error(), "org-b") || !strings.Contains(err.Error(), "names no workspace") {
		t.Fatalf("err = %v, want it to name the chosen workspace and say the token names none", err)
	}
	if strings.Contains(err.Error(), "rather than") || strings.Contains(err.Error(), "access") {
		t.Fatalf("err = %v, must not read as a wrong-workspace or access refusal", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".modernpath", "auth.json")); statErr == nil {
		t.Fatal("a refused token must write no credential file")
	}
}

// `modernpath env` shows the workspace the credential is for.
func TestWorkspaceStatusLineNamesTheStoredWorkspace(t *testing.T) {
	cases := []struct {
		auth *config.Auth
		want string
	}{
		{&config.Auth{Token: "t", WorkspaceID: "org-b", WorkspaceName: "Beta"}, "Beta (org-b)"},
		{&config.Auth{Token: "t", WorkspaceID: "org-b"}, "org-b"},
		{&config.Auth{Token: "t"}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := workspaceStatusLine(c.auth); got != c.want {
			t.Errorf("workspaceStatusLine(%+v) = %q, want %q", c.auth, got, c.want)
		}
	}
}
