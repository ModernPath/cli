package cmd

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/zitadel"
)

// REQ-CROSS-336 — `modernpath auth --workspace <id>` signs in to one
// workspace; without it the sign-in page of the forked login chooses
// (SR-MCP-001-015, auth_signin_workspace_test.go).

// fakeZitadel plays the issuer's part in the flow: a sign-in with no filter
// lands in the workspace the sign-in page chose (landed), else home; a filter
// naming a granted workspace lands there; a filter naming anything else
// degrades to home, as ZITADEL does (zitadel-actions/docs/token-format.md
// §3.3). Every login is recorded.
type fakeZitadel struct {
	t       *testing.T
	home    string
	granted []string
	logins  []zitadel.Options
	fail    error
	// noWorkspaceClaim issues tokens whose platform claim carries no org_id:
	// an identity service that predates workspace-scoped tokens.
	noWorkspaceClaim bool
	// forceDevice answers every login as a device-flow one, the way Login
	// does when the loopback flow is ruled out on the host.
	forceDevice bool
	// landed is the workspace an unfiltered sign-in lands in: the one the
	// forked login's workspace step chose (SR-MCP-001-015). Empty means home.
	landed string
}

func (f *fakeZitadel) login(_ context.Context, p zitadel.Profile, _ io.Writer, opts zitadel.Options) (*zitadel.Token, error) {
	f.logins = append(f.logins, opts)
	if f.fail != nil {
		return nil, f.fail
	}
	device := opts.DeviceFlow || f.forceDevice
	// Login sends the device-only filter when the sign-in runs as the
	// device flow and no workspace was named.
	filter := opts.OrganizationID
	if filter == "" && device {
		filter = opts.DeviceFlowOrganizationID
	}
	org := f.home
	if filter == "" && f.landed != "" && !device {
		org = f.landed
	}
	if filter != "" {
		for _, g := range f.granted {
			if g == filter {
				org = g
			}
		}
	}
	flow := zitadel.FlowLoopback
	if device {
		flow = zitadel.FlowDevice
	}
	if f.noWorkspaceClaim {
		org = ""
	}
	return &zitadel.Token{AccessToken: platformJWT(f.t, p.Issuer, f.home, org), RefreshToken: "rt", Flow: flow}, nil
}

// twoWorkspaces is the person of SCN-CLI-009-001: home plus one granted.
func twoWorkspaces(t *testing.T) *fakeZitadel {
	return &fakeZitadel{t: t, home: "org-home", granted: []string{"org-b", "org-home"}}
}

// arrange wires the fake into the login seam, resets the flags and puts the
// test in a fresh checkout bound to the test plane.
func arrange(t *testing.T, f *fakeZitadel, terminal bool) (dir string, said func() string) {
	t.Helper()
	restoreLogin := stubZitadelLogin(f.login)
	t.Cleanup(restoreLogin)
	origTTY := stdinIsTerminal
	stdinIsTerminal = func() bool { return terminal }
	t.Cleanup(func() { stdinIsTerminal = origTTY })
	restoreSystems := stubListSystemsFn(func(string, string) ([]api.System, error) { return nil, nil })
	t.Cleanup(restoreSystems)
	withAuthFlags(t, false, true, "", "", false)
	withWorkspaceFlags(t, "", false)
	dir = t.TempDir()
	t.Chdir(dir)
	return dir, captureCLIOutput(t)
}

// withWorkspaceFlags sets --workspace and --device-flow for one test.
func withWorkspaceFlags(t *testing.T, workspace string, device bool) {
	t.Helper()
	origW, origD := workspaceFlag, deviceFlowFlag
	workspaceFlag, deviceFlowFlag = workspace, device
	t.Cleanup(func() { workspaceFlag, deviceFlowFlag = origW, origD })
}

func authFileExists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".modernpath", "auth.json"))
	return err == nil
}

// --workspace naming a workspace the person is not granted: ZITADEL degrades
// the filter to home; the CLI refuses and stores nothing (D5).
func TestAuthWithAnUngrantedWorkspaceRefusesAndStoresNothing(t *testing.T) {
	f := twoWorkspaces(t)
	dir, _ := arrange(t, f, false)
	withWorkspaceFlags(t, "org-x", false)

	err := runAuth(authCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "org-x") || !strings.Contains(err.Error(), "org-home") {
		t.Fatalf("err = %v, want a refusal naming org-x and org-home", err)
	}
	if authFileExists(dir) {
		t.Fatal("a refused sign-in must write no credential file")
	}
}

// Refused before any request: a value that is not an identifier, and
// --workspace on a host without a profile.
func TestAuthWorkspaceFlagsAreRefusedBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		name      string
		workspace string
		local     bool
	}{
		{"not an identifier", "org b;evil", false},
		{"--workspace on a host without a profile", "org-b", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := twoWorkspaces(t)
			dir, _ := arrange(t, f, true)
			withWorkspaceFlags(t, c.workspace, false)
			if c.local {
				withAuthFlags(t, false, false, "", "", true)
				withEmptyStdin(t)
			}

			err := runAuth(authCmd, nil)
			if err == nil {
				t.Fatal("must be refused as a command error")
			}
			if len(f.logins) != 0 {
				t.Fatalf("logins = %+v: nothing may be requested", f.logins)
			}
			if authFileExists(dir) {
				t.Fatal("nothing may be stored")
			}
		})
	}
}

// A failed sign-in itself is a command error and writes nothing (CR-11).
func TestAuthFailedSignInIsACommandErrorAndWritesNothing(t *testing.T) {
	f := twoWorkspaces(t)
	f.fail = errors.New("device code expired")
	dir, _ := arrange(t, f, true)

	err := runAuth(authCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "device code expired") {
		t.Fatalf("err = %v, want the sign-in failure returned", err)
	}
	if authFileExists(dir) {
		t.Fatal("a failed sign-in must write no credential file")
	}
}

// The device flow with a named workspace: one sign-in carrying it.
func TestAuthDeviceFlowWithANamedWorkspaceSignsInFiltered(t *testing.T) {
	f := twoWorkspaces(t)
	dir, _ := arrange(t, f, true)
	withWorkspaceFlags(t, "org-b", true)

	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if len(f.logins) != 1 || f.logins[0].OrganizationID != "org-b" || !f.logins[0].DeviceFlow {
		t.Fatalf("logins = %+v, want one device-flow sign-in carrying org-b", f.logins)
	}
	if readAuthFile(t, dir)["workspace_id"] != "org-b" {
		t.Fatal("the named workspace must be stored")
	}
}

// When the CLI falls back to the device flow on its own (no browser here)
// with no workspace named, it keeps the workspace the sign-in landed in and
// says how to reach another.
func TestAuthDeviceFlowFallbackKeepsTheLandedWorkspace(t *testing.T) {
	f := twoWorkspaces(t)
	f.forceDevice = true
	dir, said := arrange(t, f, true)

	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if len(f.logins) != 1 {
		t.Fatalf("logins = %+v, want one sign-in", f.logins)
	}
	if readAuthFile(t, dir)["workspace_id"] != "org-home" {
		t.Fatal("the landed workspace must be stored")
	}
	if !strings.Contains(said(), "org-home") || !strings.Contains(said(), "--device-flow --workspace <id>") {
		t.Fatalf("output = %q, want the landed workspace and the way to another", said())
	}
}

// A token whose platform claim names no workspace stores none, with a
// warning naming the cause; an explicit --workspace is refused for the same
// reason, nothing stored.
func TestAuthStoresNoWorkspaceWhenTheTokenNamesNone(t *testing.T) {
	f := twoWorkspaces(t)
	f.noWorkspaceClaim = true
	dir, said := arrange(t, f, true)

	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if len(f.logins) != 1 {
		t.Fatalf("logins = %+v, want one sign-in", f.logins)
	}
	got := readAuthFile(t, dir)
	if _, has := got["workspace_id"]; has {
		t.Fatalf("auth.json = %v, must store no workspace the token does not name", got)
	}
	if !strings.Contains(said(), "names no workspace") {
		t.Fatalf("output = %q, want the warning that the token names no workspace", said())
	}
}

func TestAuthWorkspaceFlagIsRefusedWhenTheTokenNamesNoWorkspace(t *testing.T) {
	f := twoWorkspaces(t)
	f.noWorkspaceClaim = true
	dir, _ := arrange(t, f, true)
	withWorkspaceFlags(t, "org-b", false)

	err := runAuth(authCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "names no workspace") {
		t.Fatalf("err = %v, want a refusal saying the token names no workspace", err)
	}
	if authFileExists(dir) {
		t.Fatal("a refused token must write no credential file")
	}
}
