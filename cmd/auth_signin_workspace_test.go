package cmd

import (
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
)

// SR-MCP-001-015 (USER:2026-10-29:cli-and-mcp-use-workspace-picker): the
// workspace is chosen on the sign-in page of the forked login, so the browser
// sign-in is one sign-in. Without --workspace the CLI asks for no workspace
// and stores the one the token names; --workspace still sends the filter and
// refuses a token that landed elsewhere.

// A fresh `modernpath auth` lets the sign-in page choose: a workspace stored
// by an earlier sign-in is not sent as a filter.
func TestAuthWithoutTheFlagSendsNoWorkspaceFilterEvenWithAStoredOne(t *testing.T) {
	f := twoWorkspaces(t)
	dir, _ := arrange(t, f, true)
	if err := config.WriteAuth(&config.Auth{Token: "old", WorkspaceID: "org-b", Issuer: zitadel.TestProfile.Issuer}); err != nil {
		t.Fatal(err)
	}

	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if len(f.logins) != 1 || f.logins[0].OrganizationID != "" {
		t.Fatalf("logins = %+v, want one sign-in without a workspace filter", f.logins)
	}
	if got := readAuthFile(t, dir)["workspace_id"]; got != "org-home" {
		t.Fatalf("auth.json workspace_id = %v, want the workspace the sign-in landed in", got)
	}
}

// The workspace the sign-in page chose is read from the token and stored;
// nothing is asked of the person here, with or without a terminal.
func TestAuthStoresTheWorkspaceTheSignInChose(t *testing.T) {
	for _, terminal := range []bool{true, false} {
		f := twoWorkspaces(t)
		f.landed = "org-b"
		dir, said := arrange(t, f, terminal)

		if err := runAuth(authCmd, nil); err != nil {
			t.Fatalf("terminal=%v: runAuth: %v", terminal, err)
		}
		if len(f.logins) != 1 || f.logins[0].OrganizationID != "" {
			t.Fatalf("terminal=%v: logins = %+v, want one unfiltered sign-in", terminal, f.logins)
		}
		got := readAuthFile(t, dir)
		if got["workspace_id"] != "org-b" || got["issuer"] != zitadel.TestProfile.Issuer {
			t.Fatalf("terminal=%v: auth.json = %v, want org-b from the test issuer", terminal, got)
		}
		if !strings.Contains(said(), "org-b") {
			t.Fatalf("terminal=%v: output = %q, want the stored workspace named", terminal, said())
		}
	}
}

// --workspace keeps working: one sign-in carrying the filter.
func TestAuthWithWorkspaceFlagSignsInFilteredOnce(t *testing.T) {
	f := twoWorkspaces(t)
	dir, _ := arrange(t, f, false)
	withWorkspaceFlags(t, "org-b", false)

	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if len(f.logins) != 1 || f.logins[0].OrganizationID != "org-b" {
		t.Fatalf("logins = %+v, want exactly one carrying org-b", f.logins)
	}
	if got := readAuthFile(t, dir)["workspace_id"]; got != "org-b" {
		t.Fatalf("auth.json workspace_id = %v, want org-b", got)
	}
}

// The device flow has no workspace step: its authorization is created by the
// CLI's own back-channel request, which the forked login never sees, and
// ZITADEL completes it with a session or a deny only. So it reuses the
// workspace stored by an earlier sign-in at the same issuer
// (USER:2026-10-29:device-flow-reuses-stored-workspace), forced or fallen
// back to, and refuses a token that lands elsewhere.
func TestAuthDeviceFlowReusesTheStoredWorkspace(t *testing.T) {
	for _, c := range []struct {
		name   string
		forced bool
	}{{"forced", true}, {"fallen back to", false}} {
		t.Run(c.name, func(t *testing.T) {
			f := twoWorkspaces(t)
			f.forceDevice = !c.forced
			dir, said := arrange(t, f, true)
			withWorkspaceFlags(t, "", c.forced)
			if err := config.WriteAuth(&config.Auth{Token: "old", WorkspaceID: "org-b", Issuer: zitadel.TestProfile.Issuer}); err != nil {
				t.Fatal(err)
			}

			if err := runAuth(authCmd, nil); err != nil {
				t.Fatalf("runAuth: %v", err)
			}
			if len(f.logins) != 1 || f.logins[0].OrganizationID != "" || f.logins[0].DeviceFlowOrganizationID != "org-b" {
				t.Fatalf("logins = %+v, want one sign-in carrying the stored org-b for the device flow", f.logins)
			}
			if got := readAuthFile(t, dir)["workspace_id"]; got != "org-b" {
				t.Fatalf("auth.json workspace_id = %v, want the stored org-b", got)
			}
			if !strings.Contains(said(), "stored workspace org-b") || !strings.Contains(said(), "--workspace <id>") {
				t.Fatalf("output = %q, want the stored workspace and how to use another", said())
			}
		})
	}
}

// A workspace stored at another issuer names an organization this issuer
// does not know: it is not sent, and the person is told.
func TestAuthDeviceFlowIgnoresAWorkspaceStoredByAnotherIssuer(t *testing.T) {
	f := twoWorkspaces(t)
	dir, said := arrange(t, f, true)
	withWorkspaceFlags(t, "", true)
	if err := config.WriteAuth(&config.Auth{Token: "old", WorkspaceID: "prod-org", Issuer: zitadel.ProdProfile.Issuer}); err != nil {
		t.Fatal(err)
	}

	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if len(f.logins) != 1 || f.logins[0].OrganizationID != "" || f.logins[0].DeviceFlowOrganizationID != "" {
		t.Fatalf("logins = %+v, want one sign-in with no workspace filter", f.logins)
	}
	if got := readAuthFile(t, dir)["workspace_id"]; got != "org-home" {
		t.Fatalf("auth.json workspace_id = %v, want the landed org-home", got)
	}
	if !strings.Contains(said(), "belongs to another sign-in server") || !strings.Contains(said(), "--device-flow --workspace <id>") {
		t.Fatalf("output = %q, want the other-server note and the way to another workspace", said())
	}
}

// --workspace overrides the stored workspace in the device flow too.
func TestAuthDeviceFlowWorkspaceFlagOverridesTheStoredOne(t *testing.T) {
	f := twoWorkspaces(t)
	dir, _ := arrange(t, f, true)
	withWorkspaceFlags(t, "org-home", true)
	if err := config.WriteAuth(&config.Auth{Token: "old", WorkspaceID: "org-b", Issuer: zitadel.TestProfile.Issuer}); err != nil {
		t.Fatal(err)
	}

	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if len(f.logins) != 1 || f.logins[0].OrganizationID != "org-home" {
		t.Fatalf("logins = %+v, want one sign-in carrying the named org-home", f.logins)
	}
	if got := readAuthFile(t, dir)["workspace_id"]; got != "org-home" {
		t.Fatalf("auth.json workspace_id = %v, want org-home", got)
	}
}

// A stored workspace whose grant was revoked: ZITADEL degrades the filter to
// home, and the CLI refuses instead of landing there silently, naming the
// two ways out.
func TestAuthDeviceFlowRefusesWhenTheStoredWorkspaceIsNoLongerGranted(t *testing.T) {
	f := twoWorkspaces(t)
	dir, _ := arrange(t, f, true)
	withWorkspaceFlags(t, "", true)
	if err := config.WriteAuth(&config.Auth{Token: "previous", WorkspaceID: "org-gone", Issuer: zitadel.TestProfile.Issuer}); err != nil {
		t.Fatal(err)
	}

	err := runAuth(authCmd, nil)
	if err == nil {
		t.Fatal("a stored workspace the token does not confirm must be refused")
	}
	for _, want := range []string{"org-gone", "org-home", "modernpath auth --device-flow --workspace <id>", "modernpath auth --logout"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want %q", err, want)
		}
	}
	if got := readAuthFile(t, dir)["token"]; got != "previous" {
		t.Fatalf("auth.json token = %v, want the previous credential untouched", got)
	}
}
