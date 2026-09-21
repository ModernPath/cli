package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
)

// REQ-CROSS-336 — `modernpath auth` chooses a workspace: a picker in the
// browser flow when several are listed and stdin is a terminal,
// `--workspace <id>` anywhere, `--choose-workspace` to pick again, and the
// device flow keeping the workspace it lands in, since it cannot list them.

// fakeZitadel plays the issuer's part in the flow: a sign-in with no filter
// lands in home; a filter naming a granted workspace lands there; a filter
// naming anything else degrades to home, as ZITADEL does
// (zitadel-actions/docs/token-format.md §3.3). Every login is recorded.
type fakeZitadel struct {
	t        *testing.T
	issuer   string
	home     string
	granted  []zitadel.Organization
	logins   []zitadel.Options
	fail     error
	listErr  error
	listHits int
	// noWorkspaceClaim issues tokens whose platform claim carries no org_id:
	// an identity service that predates workspace-scoped tokens.
	noWorkspaceClaim bool
	// forceDevice answers every login as a device-flow one, the way Login
	// does when the loopback flow is ruled out on the host.
	forceDevice bool
}

func (f *fakeZitadel) login(_ context.Context, p zitadel.Profile, _ io.Writer, opts zitadel.Options) (*zitadel.Token, error) {
	f.logins = append(f.logins, opts)
	if f.fail != nil {
		return nil, f.fail
	}
	org := f.home
	if opts.OrganizationID != "" {
		for _, g := range f.granted {
			if g.ID == opts.OrganizationID {
				org = g.ID
			}
		}
	}
	flow := zitadel.FlowLoopback
	if opts.DeviceFlow || f.forceDevice {
		flow = zitadel.FlowDevice
	}
	if f.noWorkspaceClaim {
		org = ""
	}
	return &zitadel.Token{AccessToken: platformJWT(f.t, p.Issuer, f.home, org), RefreshToken: "rt", Flow: flow}, nil
}

func (f *fakeZitadel) list(_ context.Context, _ zitadel.Profile, _ string) (zitadel.OrganizationList, error) {
	f.listHits++
	if f.listErr != nil {
		return zitadel.OrganizationList{}, f.listErr
	}
	return zitadel.OrganizationList{Organizations: append([]zitadel.Organization(nil), f.granted...)}, nil
}

// twoWorkspaces is the person of SCN-CLI-009-001: home plus one granted.
func twoWorkspaces(t *testing.T) *fakeZitadel {
	return &fakeZitadel{t: t, issuer: zitadel.TestProfile.Issuer, home: "org-home", granted: []zitadel.Organization{
		{ID: "org-b", Name: "Beta", PrimaryDomain: "beta.example"},
		{ID: "org-home", Name: "Home Org", PrimaryDomain: "home.example"},
	}}
}

// arrange wires the fake into every seam, resets the flags and puts the test
// in a fresh checkout bound to the test plane.
func arrange(t *testing.T, f *fakeZitadel, terminal bool) (dir string, said func() string) {
	t.Helper()
	restoreLogin := stubZitadelLogin(f.login)
	t.Cleanup(restoreLogin)
	origList, origPick, origTTY := listOrganizationsFn, pickWorkspaceFn, stdinIsTerminal
	listOrganizationsFn = f.list
	pickWorkspaceFn = func([]zitadel.Organization, string, string) (zitadel.Organization, error) {
		t.Fatal("the picker must not run in this case")
		return zitadel.Organization{}, nil
	}
	stdinIsTerminal = func() bool { return terminal }
	t.Cleanup(func() { listOrganizationsFn, pickWorkspaceFn, stdinIsTerminal = origList, origPick, origTTY })
	restoreSystems := stubListSystemsFn(func(string, string) ([]api.System, error) { return nil, nil })
	t.Cleanup(restoreSystems)
	withAuthFlags(t, false, true, "", "", false)
	withWorkspaceFlags(t, "", false, false)
	dir = t.TempDir()
	t.Chdir(dir)
	return dir, captureCLIOutput(t)
}

// withWorkspaceFlags sets this epic's flags for one test.
func withWorkspaceFlags(t *testing.T, workspace string, choose, device bool) {
	t.Helper()
	origW, origC, origD := workspaceFlag, chooseWorkspaceFlag, deviceFlowFlag
	workspaceFlag, chooseWorkspaceFlag, deviceFlowFlag = workspace, choose, device
	t.Cleanup(func() { workspaceFlag, chooseWorkspaceFlag, deviceFlowFlag = origW, origC, origD })
}

func pickerChoosing(t *testing.T, id string, ran *bool) func([]zitadel.Organization, string, string) (zitadel.Organization, error) {
	return func(orgs []zitadel.Organization, home, preselected string) (zitadel.Organization, error) {
		*ran = true
		for _, o := range orgs {
			if o.ID == id {
				return o, nil
			}
		}
		t.Fatalf("picker asked to choose %q, not among %v", id, orgs)
		return zitadel.Organization{}, nil
	}
}

func authFileExists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".modernpath", "auth.json"))
	return err == nil
}

// (a) several workspaces, browser flow, no terminal, no flag → an error that
// names --workspace and the workspaces; the previous credential stays.
func TestAuthWithSeveralWorkspacesAndNoTerminalRefusesNamingTheFlag(t *testing.T) {
	f := twoWorkspaces(t)
	dir, said := arrange(t, f, false)
	if err := config.WriteAuth(&config.Auth{Token: "previous"}); err != nil {
		t.Fatal(err)
	}

	err := runAuth(authCmd, nil)
	if err == nil {
		t.Fatal("several workspaces without a terminal must be a command error")
	}
	for _, want := range []string{"--workspace", "org-b", "org-home", "Beta"} {
		if !strings.Contains(err.Error()+said(), want) {
			t.Fatalf("err = %v / output = %q, want %q", err, said(), want)
		}
	}
	if got := readAuthFile(t, dir)["token"]; got != "previous" {
		t.Fatalf("auth.json token = %v, want the previous credential untouched", got)
	}
	_ = dir
}

// (b) --workspace B → one filtered sign-in, the list read once for the name.
func TestAuthWithWorkspaceFlagSignsInFilteredOnce(t *testing.T) {
	f := twoWorkspaces(t)
	dir, _ := arrange(t, f, false)
	withWorkspaceFlags(t, "org-b", false, false)

	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if len(f.logins) != 1 || f.logins[0].OrganizationID != "org-b" {
		t.Fatalf("logins = %+v, want exactly one carrying org-b", f.logins)
	}
	if f.listHits != 1 {
		t.Fatalf("list read %d times, want once for the name", f.listHits)
	}
	got := readAuthFile(t, dir)
	if got["workspace_id"] != "org-b" || got["workspace_name"] != "Beta" || got["issuer"] != zitadel.TestProfile.Issuer {
		t.Fatalf("auth.json = %v, want org-b named Beta from the test issuer", got)
	}
}

// --workspace naming a workspace the person is not granted: ZITADEL degrades
// the filter to home; the CLI refuses and stores nothing (D5).
func TestAuthWithAnUngrantedWorkspaceRefusesAndStoresNothing(t *testing.T) {
	f := twoWorkspaces(t)
	dir, _ := arrange(t, f, false)
	withWorkspaceFlags(t, "org-x", false, false)

	err := runAuth(authCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "org-x") || !strings.Contains(err.Error(), "org-home") {
		t.Fatalf("err = %v, want a refusal naming org-x and org-home", err)
	}
	if authFileExists(dir) {
		t.Fatal("a refused sign-in must write no credential file")
	}
}

// (c) refused before any request: a value that is not an identifier, both
// flags together, --choose-workspace without a terminal, either flag on a
// host without a profile.
func TestAuthWorkspaceFlagsAreRefusedBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		name      string
		workspace string
		choose    bool
		local     bool
		terminal  bool
	}{
		{"not an identifier", "org b;evil", false, false, true},
		{"both flags", "org-b", true, false, true},
		{"choose without a terminal", "", true, false, false},
		{"--workspace on a host without a profile", "org-b", false, true, true},
		{"--choose-workspace on a host without a profile", "", true, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := twoWorkspaces(t)
			dir, _ := arrange(t, f, c.terminal)
			withWorkspaceFlags(t, c.workspace, c.choose, false)
			if c.local {
				withAuthFlags(t, false, false, "", "", true)
				withEmptyStdin(t)
			}

			err := runAuth(authCmd, nil)
			if err == nil {
				t.Fatal("must be refused as a command error")
			}
			if len(f.logins) != 0 || f.listHits != 0 {
				t.Fatalf("logins = %+v, list hits = %d: nothing may be requested", f.logins, f.listHits)
			}
			if authFileExists(dir) {
				t.Fatal("nothing may be stored")
			}
		})
	}
}

// (c2) a failed sign-in itself is a command error and writes nothing (CR-11).
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

// (d) a stored choice from the same issuer → one filtered sign-in, no picker.
func TestAuthAppliesAStoredWorkspaceFromTheSameIssuer(t *testing.T) {
	f := twoWorkspaces(t)
	dir, said := arrange(t, f, true)
	if err := config.WriteAuth(&config.Auth{Token: "old", WorkspaceID: "org-b", WorkspaceName: "Beta", Issuer: zitadel.TestProfile.Issuer}); err != nil {
		t.Fatal(err)
	}

	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if len(f.logins) != 1 || f.logins[0].OrganizationID != "org-b" {
		t.Fatalf("logins = %+v, want one carrying the stored org-b", f.logins)
	}
	if !strings.Contains(said(), "Beta") || !strings.Contains(said(), "--choose-workspace") {
		t.Fatalf("output = %q, want the chosen workspace and how to choose another", said())
	}
	if readAuthFile(t, dir)["workspace_id"] != "org-b" {
		t.Fatal("the stored workspace must be kept")
	}
}

// (d2) a stored choice from another issuer is not applied: one unfiltered
// sign-in and a note (CR-2).
func TestAuthIgnoresAStoredWorkspaceFromAnotherIssuer(t *testing.T) {
	f := twoWorkspaces(t)
	_, said := arrange(t, f, true)
	if err := config.WriteAuth(&config.Auth{Token: "old", WorkspaceID: "prod-org", WorkspaceName: "Prod", Issuer: zitadel.ProdProfile.Issuer}); err != nil {
		t.Fatal(err)
	}
	ran := false
	pickWorkspaceFn = pickerChoosing(t, "org-home", &ran)

	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if len(f.logins) < 1 || f.logins[0].OrganizationID != "" {
		t.Fatalf("logins = %+v, want the first sign-in unfiltered", f.logins)
	}
	if !strings.Contains(said(), "not applied") {
		t.Fatalf("output = %q, want a note that the stored choice was not applied", said())
	}
}

// (e) --choose-workspace with a stored choice → the picker runs.
func TestAuthChooseWorkspaceRunsThePickerDespiteAStoredChoice(t *testing.T) {
	f := twoWorkspaces(t)
	dir, _ := arrange(t, f, true)
	withWorkspaceFlags(t, "", true, false)
	if err := config.WriteAuth(&config.Auth{Token: "old", WorkspaceID: "org-b", WorkspaceName: "Beta", Issuer: zitadel.TestProfile.Issuer}); err != nil {
		t.Fatal(err)
	}
	ran := false
	pickWorkspaceFn = pickerChoosing(t, "org-home", &ran)

	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if !ran {
		t.Fatal("--choose-workspace must run the picker")
	}
	if f.logins[0].OrganizationID != "" {
		t.Fatalf("logins = %+v, want the first sign-in unfiltered", f.logins)
	}
	if readAuthFile(t, dir)["workspace_id"] != "org-home" {
		t.Fatal("the picked workspace must be stored")
	}
}

// (f) one workspace → one sign-in, no picker, nothing new said.
func TestAuthWithOneWorkspaceIsUnchanged(t *testing.T) {
	f := &fakeZitadel{t: t, home: "org-home", granted: []zitadel.Organization{{ID: "org-home", Name: "Home Org", PrimaryDomain: "home.example"}}}
	dir, _ := arrange(t, f, true)

	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if len(f.logins) != 1 {
		t.Fatalf("logins = %+v, want exactly one", f.logins)
	}
	got := readAuthFile(t, dir)
	if got["workspace_id"] != "org-home" || got["workspace_name"] != "Home Org" {
		t.Fatalf("auth.json = %v, want the one workspace recorded", got)
	}
}

// (g) the device flow cannot list workspaces (ZITADEL answers its token
// with 403 on the list; USER:2026-09-09), so it never tries: a bare
// `--device-flow` keeps the workspace the unfiltered sign-in lands in — home,
// for most people — and says how to reach another; `--choose-workspace` is
// refused with it; a named workspace is stored without a listing.
func TestAuthBareDeviceFlowKeepsTheLandedWorkspaceWithoutListing(t *testing.T) {
	f := twoWorkspaces(t)
	dir, said := arrange(t, f, true)
	withWorkspaceFlags(t, "", false, true)

	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if len(f.logins) != 1 || f.logins[0].OrganizationID != "" || !f.logins[0].DeviceFlow || f.listHits != 0 {
		t.Fatalf("logins = %+v, list hits = %d; want one unfiltered device-flow sign-in and no listing", f.logins, f.listHits)
	}
	if readAuthFile(t, dir)["workspace_id"] != "org-home" {
		t.Fatal("the landed workspace must be stored")
	}
	if !strings.Contains(said(), "org-home") || !strings.Contains(said(), "--device-flow --workspace <id>") {
		t.Fatalf("output = %q, want the landed workspace and the way to another", said())
	}
}

func TestAuthDeviceFlowRefusesThePicker(t *testing.T) {
	f := twoWorkspaces(t)
	dir, _ := arrange(t, f, true)
	withWorkspaceFlags(t, "", true, true)

	err := runAuth(authCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--choose-workspace") || !strings.Contains(err.Error(), "--workspace <id>") {
		t.Fatalf("err = %v, want a refusal of --choose-workspace that names --workspace <id>", err)
	}
	if len(f.logins) != 0 || f.listHits != 0 || authFileExists(dir) {
		t.Fatal("nothing may be requested or stored")
	}
}

func TestAuthDeviceFlowWithANamedWorkspaceNeverListsWorkspaces(t *testing.T) {
	f := twoWorkspaces(t)
	dir, _ := arrange(t, f, true)
	withWorkspaceFlags(t, "org-b", false, true)

	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if len(f.logins) != 1 || f.logins[0].OrganizationID != "org-b" || !f.logins[0].DeviceFlow {
		t.Fatalf("logins = %+v, want one device-flow sign-in carrying org-b", f.logins)
	}
	if f.listHits != 0 {
		t.Fatalf("list hits = %d, the device flow must not list workspaces", f.listHits)
	}
	if readAuthFile(t, dir)["workspace_id"] != "org-b" {
		t.Fatal("the named workspace must be stored")
	}
}

// A stored choice serves the device flow the way --workspace does.
func TestAuthDeviceFlowReusesAStoredChoice(t *testing.T) {
	f := twoWorkspaces(t)
	dir, _ := arrange(t, f, true)
	withWorkspaceFlags(t, "", false, true)
	if err := config.WriteAuth(&config.Auth{Token: "previous", WorkspaceID: "org-b", WorkspaceName: "Beta", Issuer: zitadel.TestProfile.Issuer}); err != nil {
		t.Fatal(err)
	}

	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if len(f.logins) != 1 || f.logins[0].OrganizationID != "org-b" || f.listHits != 0 {
		t.Fatalf("logins = %+v, list hits = %d; want one sign-in carrying the stored org-b and no listing", f.logins, f.listHits)
	}
	if readAuthFile(t, dir)["workspace_id"] != "org-b" {
		t.Fatal("the stored workspace must be kept")
	}
}

// When the CLI falls back to the device flow on its own (no browser here)
// with no workspace named, it keeps the workspace the sign-in landed in,
// lists nothing, and says how to reach another.
func TestAuthDeviceFlowFallbackKeepsTheLandedWorkspaceWithoutListing(t *testing.T) {
	f := twoWorkspaces(t)
	f.forceDevice = true
	dir, said := arrange(t, f, true)

	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if len(f.logins) != 1 || f.listHits != 0 {
		t.Fatalf("logins = %+v, list hits = %d; want one sign-in and no listing", f.logins, f.listHits)
	}
	if readAuthFile(t, dir)["workspace_id"] != "org-home" {
		t.Fatal("the landed workspace must be stored")
	}
	if !strings.Contains(said(), "org-home") || !strings.Contains(said(), "--device-flow --workspace <id>") {
		t.Fatalf("output = %q, want the landed workspace and the way to another", said())
	}
}

// (h) the picker: choosing the token's own workspace needs no second
// sign-in; choosing another runs one carrying it.
func TestAuthPickerChoosingTheOwnWorkspaceSignsInOnce(t *testing.T) {
	f := twoWorkspaces(t)
	dir, _ := arrange(t, f, true)
	ran := false
	pickWorkspaceFn = pickerChoosing(t, "org-home", &ran)

	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if !ran || len(f.logins) != 1 {
		t.Fatalf("picker ran = %v, logins = %+v; want the picker and one sign-in", ran, f.logins)
	}
	if readAuthFile(t, dir)["workspace_id"] != "org-home" {
		t.Fatal("the own workspace must be stored")
	}
}

func TestAuthPickerChoosingAnotherWorkspaceSignsInAgainWithTheFilter(t *testing.T) {
	f := twoWorkspaces(t)
	dir, _ := arrange(t, f, true)
	ran := false
	pickWorkspaceFn = pickerChoosing(t, "org-b", &ran)

	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if len(f.logins) != 2 || f.logins[0].OrganizationID != "" || f.logins[1].OrganizationID != "org-b" {
		t.Fatalf("logins = %+v, want an unfiltered sign-in then one carrying org-b", f.logins)
	}
	got := readAuthFile(t, dir)
	if got["workspace_id"] != "org-b" || got["workspace_name"] != "Beta" {
		t.Fatalf("auth.json = %v, want org-b named Beta", got)
	}
}

// The list failing after an unfiltered sign-in does not lose the sign-in:
// the token's own workspace is stored and the way to choose is printed.
func TestAuthKeepsTheSignInWhenTheListCannotBeRead(t *testing.T) {
	f := twoWorkspaces(t)
	f.listErr = fmt.Errorf("the identity provider answered 502 to the workspace list")
	dir, said := arrange(t, f, true)

	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if readAuthFile(t, dir)["workspace_id"] != "org-home" {
		t.Fatal("the token's own workspace must still be stored")
	}
	if !strings.Contains(said(), "502") || !strings.Contains(said(), "--workspace") {
		t.Fatalf("output = %q, want the list failure and the way to choose", said())
	}
}

// (i) a token whose platform claim names no workspace cannot confirm any
// choice, so the picker is skipped and the credential is stored without a
// workspace, with a warning naming the cause; an explicit --workspace is
// refused for the same reason, nothing stored.
func TestAuthSkipsThePickerWhenTheTokenNamesNoWorkspace(t *testing.T) {
	f := twoWorkspaces(t)
	f.noWorkspaceClaim = true
	dir, said := arrange(t, f, true)

	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if len(f.logins) != 1 {
		t.Fatalf("logins = %+v, want one sign-in and no second attempt", f.logins)
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
	withWorkspaceFlags(t, "org-b", false, false)

	err := runAuth(authCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "names no workspace") {
		t.Fatalf("err = %v, want a refusal saying the token names no workspace", err)
	}
	if authFileExists(dir) {
		t.Fatal("a refused token must write no credential file")
	}
}
