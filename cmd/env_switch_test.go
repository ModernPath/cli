package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
)

// REQ-CROSS-391 — `env --set` switches the URL, the system binding and the
// credential together, restoring what was stashed for the target or refusing
// with the remedy verb, and `status` and `factory status` print the same
// server and system lines in every state.

func readConfigFile(t *testing.T, dir string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, ".modernpath", "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("config.json is not JSON: %v", err)
	}
	return m
}

func writeJSON(t *testing.T, root, rel string, v any) {
	t.Helper()
	blob, _ := json.Marshal(v)
	writeFactoryTestFile(t, root, rel, string(blob))
}

func testPlaneToken(t *testing.T) string {
	return jwtWithClaims(t, map[string]any{"iss": zitadel.TestProfile.Issuer, "sub": "u1", "aud": []string{zitadel.TestProfile.ProjectID}})
}

func prodPlaneToken(t *testing.T) string {
	return jwtWithClaims(t, map[string]any{"iss": zitadel.ProdProfile.Issuer, "sub": "u1", "aud": []string{zitadel.ProdProfile.ProjectID}})
}

// (a) Bound to test with a production stash: the switch restores the
// production system and credential and stashes the test ones.
func TestEnvSetRestoresTheStashedBindingAndCredential(t *testing.T) {
	root := enterFactoryTestWorkspace(t)
	writeJSON(t, root, ".modernpath/config.json", map[string]any{
		"api_url": zitadel.TestProfile.APIURL, "system_id": 49, "system_name": "sandbox", "system_slug": "sandbox",
		"environments": map[string]any{"production": map[string]any{"system_id": 3, "system_name": "ModernPath", "system_slug": "modernpath"}},
	})
	prod := prodPlaneToken(t)
	test := testPlaneToken(t)
	writeJSON(t, root, ".modernpath/auth.json", map[string]any{
		"token": test, "issuer": zitadel.TestProfile.Issuer, "actor": "jane@example.com", "workspace_id": "org-test",
		"stash": map[string]any{zitadel.ProdProfile.Issuer: map[string]any{"token": prod, "actor": "jane@example.com", "workspace_id": "org-prod", "workspace_name": "Acme"}},
	})
	cfg, _ := config.ReadConfig()

	var out []byte
	if err := captureStdout(t, func() error { return setEnvironment(cfg, "production") }, &out); err != nil {
		t.Fatalf("setEnvironment: %v", err)
	}

	c := readConfigFile(t, root)
	if c["api_url"] != zitadel.ProdProfile.APIURL || c["system_id"] != float64(3) || c["system_slug"] != "modernpath" {
		t.Errorf("config after the switch = %v, want the production URL and system 3", c)
	}
	if envs, _ := c["environments"].(map[string]any); envs == nil || envs["test"] == nil || envs["test"].(map[string]any)["system_id"] != float64(49) {
		t.Errorf("the test binding must be stashed under environments.test, got %v", c["environments"])
	}
	a := readAuthFile(t, root)
	if a["token"] != prod || a["issuer"] != zitadel.ProdProfile.Issuer || a["workspace_id"] != "org-prod" {
		t.Errorf("the production credential must be active, got token=%v issuer=%v workspace=%v", a["token"] == prod, a["issuer"], a["workspace_id"])
	}
	if stash, _ := a["stash"].(map[string]any); stash == nil || stash[zitadel.TestProfile.Issuer] == nil || stash[zitadel.TestProfile.Issuer].(map[string]any)["token"] != test {
		t.Errorf("the test credential must be stashed under its issuer, got %v", a["stash"])
	}
	text := string(out)
	for _, want := range []string{"ModernPath (ID: 3)", "jane@example.com"} {
		if !strings.Contains(text, want) {
			t.Errorf("the switch output must name %q:\n%s", want, text)
		}
	}
}

// (b) No stash for the target: the binding is cleared with the init remedy,
// the credential is stashed and not left active, and the auth remedy is named.
func TestEnvSetWithoutAStashClearsAndNamesTheRemedies(t *testing.T) {
	root := enterFactoryTestWorkspace(t)
	writeJSON(t, root, ".modernpath/config.json", map[string]any{"api_url": zitadel.TestProfile.APIURL, "system_id": 49, "system_name": "sandbox"})
	test := testPlaneToken(t)
	writeJSON(t, root, ".modernpath/auth.json", map[string]any{"token": test, "issuer": zitadel.TestProfile.Issuer})
	cfg, _ := config.ReadConfig()

	var out []byte
	if err := captureStdout(t, func() error { return setEnvironment(cfg, "production") }, &out); err != nil {
		t.Fatalf("setEnvironment: %v", err)
	}

	c := readConfigFile(t, root)
	if _, bound := c["system_id"]; bound {
		t.Errorf("no production binding is stashed, so system_id must be cleared, got %v", c["system_id"])
	}
	if envs, _ := c["environments"].(map[string]any); envs == nil || envs["test"] == nil {
		t.Errorf("the test binding must be stashed, got %v", c["environments"])
	}
	a := readAuthFile(t, root)
	if tok, _ := a["token"].(string); tok != "" {
		t.Errorf("the test credential must not stay active under the production URL")
	}
	if stash, _ := a["stash"].(map[string]any); stash == nil || stash[zitadel.TestProfile.Issuer] == nil {
		t.Errorf("the test credential must be stashed, got %v", a["stash"])
	}
	text := string(out)
	for _, want := range []string{"modernpath init", authRepairCommand(zitadel.ProdProfile.APIURL)} {
		if !strings.Contains(text, want) {
			t.Errorf("the switch output must name the remedy %q:\n%s", want, text)
		}
	}
}

// (d) A restored credential this server does not accept is reported at
// switch time, not at the next write.
func TestEnvSetReportsACredentialTheServerWillNotAccept(t *testing.T) {
	root := enterFactoryTestWorkspace(t)
	writeJSON(t, root, ".modernpath/config.json", map[string]any{"api_url": zitadel.TestProfile.APIURL, "system_id": 49})
	wrongAudience := jwtWithClaims(t, map[string]any{"iss": zitadel.ProdProfile.Issuer, "sub": "u1", "aud": []string{zitadel.TestProfile.ProjectID}})
	writeJSON(t, root, ".modernpath/auth.json", map[string]any{
		"token": testPlaneToken(t), "issuer": zitadel.TestProfile.Issuer,
		"stash": map[string]any{zitadel.ProdProfile.Issuer: map[string]any{"token": wrongAudience}},
	})
	cfg, _ := config.ReadConfig()

	var out, errOut []byte
	var err error
	captureStderr(t, func() {
		err = captureStdout(t, func() error { return setEnvironment(cfg, "production") }, &out)
	}, &errOut)
	if err != nil {
		t.Fatalf("setEnvironment: %v", err)
	}
	if !strings.Contains(string(out)+string(errOut), "not one this server accepts") {
		t.Errorf("the switch must report the unacceptable credential:\n%s%s", out, errOut)
	}
}

// bindingLinesOf keeps only the Server: and System: lines, trimmed, so the two
// status commands can be compared on exactly the binding they print.
func bindingLinesOf(out []byte) []string {
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "Server:") || strings.HasPrefix(l, "System:") {
			lines = append(lines, l)
		}
	}
	return lines
}

// (c) status and factory status print byte-identical Server: and System:
// lines in the bound, unbound and unsigned states; factory status exits
// non-zero after them when unbound.
func TestStatusAndFactoryStatusPrintTheSameBinding(t *testing.T) {
	saved := listSystemsFn
	listSystemsFn = func(apiURL, token string) ([]api.System, error) { return []api.System{{ID: 7}}, nil }
	t.Cleanup(func() { listSystemsFn = saved })
	const apiURL = "http://127.0.0.1:1"

	cases := map[string]struct {
		cfg, auth map[string]any
		unbound   bool
	}{
		"bound and signed in":  {cfg: map[string]any{"api_url": apiURL, "system_id": 7, "system_name": "Seven", "system_slug": "seven"}, auth: map[string]any{"token": "t", "actor": "jane@example.com"}},
		"unbound":              {cfg: map[string]any{"api_url": apiURL}, auth: map[string]any{"token": "t"}, unbound: true},
		"bound, not signed in": {cfg: map[string]any{"api_url": apiURL, "system_id": 7, "system_name": "Seven"}, auth: map[string]any{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := enterFactoryTestWorkspace(t)
			writeJSON(t, root, ".modernpath/config.json", tc.cfg)
			writeJSON(t, root, ".modernpath/auth.json", tc.auth)

			var statusOut, factoryOut []byte
			if err := captureStdout(t, func() error { return runStatus(statusCmd, nil) }, &statusOut); err != nil {
				t.Fatalf("status: %v", err)
			}
			ferr := captureStdout(t, func() error { return factoryStatusCmd.RunE(factoryStatusCmd, nil) }, &factoryOut)
			if tc.unbound && ferr == nil {
				t.Error("factory status must still refuse when unbound, after printing the binding")
			}
			if !tc.unbound && ferr != nil {
				t.Fatalf("factory status: %v", ferr)
			}

			got, want := bindingLinesOf(factoryOut), bindingLinesOf(statusOut)
			if len(want) != 2 {
				t.Fatalf("status must print a Server: and a System: line, got %v", want)
			}
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("binding lines differ:\n status:         %v\n factory status: %v", want, got)
			}
		})
	}
}
