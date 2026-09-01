package config

// REQ-CROSS-017 (EPIC-SYNC-005): current_release is the machine-readable
// release selector factory sync reads. ReadConfig hand-copies fields from the
// legacy struct — a field missing from that copy block silently drops on
// read, so the round-trip (including the legacy architecture_* path) is
// pinned here.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".modernpath"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCurrentReleaseRoundTrip(t *testing.T) {
	chdirTemp(t)

	cfg := &Config{APIURL: "http://localhost:4000", SystemID: 46543, CurrentRelease: "modernpath-v1-09"}
	if err := WriteConfig(cfg); err != nil {
		t.Fatal(err)
	}

	got, err := ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.CurrentRelease != "modernpath-v1-09" {
		t.Fatalf("current_release dropped on round-trip: got %q", got.CurrentRelease)
	}
	if got.SystemID != 46543 {
		t.Fatalf("system_id: got %d", got.SystemID)
	}
}

func TestCurrentReleaseUnsetStaysEmptyAndOmitted(t *testing.T) {
	dir := chdirTemp(t)

	if err := WriteConfig(&Config{APIURL: "http://localhost:4000", SystemID: 1}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".modernpath", ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) == "" || strings.Contains(string(raw), "current_release") {
		t.Fatalf("unset current_release must be omitted from the file, got: %s", raw)
	}

	got, err := ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.CurrentRelease != "" {
		t.Fatalf("unset current_release must read empty, got %q", got.CurrentRelease)
	}
}

func TestCurrentReleaseSurvivesLegacyArchitectureRead(t *testing.T) {
	dir := chdirTemp(t)

	legacy := `{"api_url":"http://localhost:4000","architecture_id":45,"architecture_slug":"modernpath","current_release":"modernpath-v1-09"}`
	if err := os.WriteFile(filepath.Join(dir, ".modernpath", ConfigFile), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.SystemID != 45 {
		t.Fatalf("legacy architecture_id migration broke: got %d", got.SystemID)
	}
	if got.CurrentRelease != "modernpath-v1-09" {
		t.Fatalf("current_release dropped on the legacy read path: got %q", got.CurrentRelease)
	}
}

func TestEpicConfigWritesCanonicalKeys(t *testing.T) {
	dir := chdirTemp(t)

	cfg := &Config{
		APIURL:       "http://localhost:4000",
		SystemID:     45,
		EpicID:       157,
		EpicName:     "Canonical planning model",
		EpicSpecsDir: "tasks/157-canonical-planning-model",
	}
	if err := WriteConfig(cfg); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, ".modernpath", ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"epic_id"`, `"epic_name"`, `"epic_specs_dir"`} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("canonical config key %s missing: %s", key, raw)
		}
	}
	if strings.Contains(string(raw), "initiative_") {
		t.Fatalf("new config must not write deleted initiative keys: %s", raw)
	}
}

func TestReadConfigAcceptsLegacyInitiativeKeys(t *testing.T) {
	dir := chdirTemp(t)

	legacy := `{"api_url":"http://localhost:4000","initiative_id":157,"initiative_name":"Canonical planning model","initiative_specs_dir":"tasks/157-canonical-planning-model"}`
	if err := os.WriteFile(filepath.Join(dir, ".modernpath", ConfigFile), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.EpicID != 157 {
		t.Fatalf("legacy initiative_id was not read as epic_id: got %d", got.EpicID)
	}
	if got.EpicName != "Canonical planning model" {
		t.Fatalf("legacy initiative_name was not read as epic_name: got %q", got.EpicName)
	}
	if got.EpicSpecsDir != "tasks/157-canonical-planning-model" {
		t.Fatalf("legacy initiative_specs_dir was not read as epic_specs_dir: got %q", got.EpicSpecsDir)
	}
}

func TestCanonicalEpicKeysTakePrecedenceOverLegacyInitiativeKeys(t *testing.T) {
	dir := chdirTemp(t)

	raw := `{"epic_id":157,"epic_name":"Canonical","epic_specs_dir":"epics/canonical","initiative_id":99,"initiative_name":"Legacy","initiative_specs_dir":"tasks/legacy"}`
	if err := os.WriteFile(filepath.Join(dir, ".modernpath", ConfigFile), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.EpicID != 157 || got.EpicName != "Canonical" || got.EpicSpecsDir != "epics/canonical" {
		t.Fatalf("canonical epic keys must win: %#v", got)
	}
}

// REQ-CROSS-116 — a config is written back to where it was read from.
//
// ReadConfig searches upward for .modernpath; WriteConfig always wrote to the
// CURRENT directory. Every read-modify-write command — connect, auth, docs sync,
// factory sync, env, work — therefore forks a nested binding when run from a
// subdirectory, and that binding then shadows the real one for every later
// command in that subtree: no auth.json, no last_sync_at, no
// release.
func TestWriteConfigUpdatesTheBindingItRead(t *testing.T) {
	root := chdirTemp(t)
	if err := WriteConfig(&Config{SystemID: 243, APIURL: "https://beta.modernpath.ai"}); err != nil {
		t.Fatal(err)
	}

	sub := filepath.Join(root, "sub", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}

	cfg, err := ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SystemID != 243 {
		t.Fatalf("the read walked up correctly? got system %d", cfg.SystemID)
	}
	cfg.SystemSlug = "modernpath-v1"
	if err := WriteConfig(cfg); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(sub, ".modernpath")); err == nil {
		t.Fatal("a nested .modernpath was created in the subdirectory, shadowing the workspace binding")
	}
	back, err := os.ReadFile(filepath.Join(root, ".modernpath", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(back), "modernpath-v1") {
		t.Fatalf("the update did not reach the config it was read from:\n%s", back)
	}
}

// Guard, expected green: with no binding anywhere above, a write still creates
// one where the user is standing. That is the first-connect path.
func TestWriteConfigCreatesABindingWhenThereIsNone(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := WriteConfig(&Config{SystemID: 7}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".modernpath", "config.json")); err != nil {
		t.Fatalf("a first binding must be created in the current directory: %v", err)
	}
}

// Guard: `modernpath init` means "initialize HERE". A deliberate nested
// workspace must still be possible — this fix removes the accidental one, not
// the intended one.
func TestInitConfigAlwaysBindsTheCurrentDirectory(t *testing.T) {
	root := chdirTemp(t)
	if err := WriteConfig(&Config{SystemID: 243}); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "packages", "widget")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}
	if err := InitConfig(&Config{SystemID: 99}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sub, ".modernpath", "config.json")); err != nil {
		t.Fatalf("init must bind the current directory: %v", err)
	}
}

// The same defect, one file over: `modernpath auth` from a subdirectory wrote
// the credential into a new nested .modernpath, which then shadowed the real
// binding for everything below it.
func TestWriteAuthTargetsTheBoundWorkspace(t *testing.T) {
	root := chdirTemp(t)
	if err := WriteConfig(&Config{SystemID: 243}); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "apps", "web")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}

	if err := WriteAuth(&Auth{Token: "t0ken"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sub, ".modernpath")); err == nil {
		t.Fatal("signing in from a subdirectory forked a nested binding")
	}
	if _, err := os.Stat(filepath.Join(root, ".modernpath", AuthFile)); err != nil {
		t.Fatalf("the credential did not reach the bound workspace: %v", err)
	}
}

// The credential file must never be the only thing standing between a token
// and `git add -A`. A workspace's .modernpath often exists WITHOUT the ignore
// file — created by an older tool, or tracked because the rdd/ snapshot lives
// under it — so the write that stores the secret is the write that must
// provision the protection, wherever the bound directory is.
func TestWriteAuthProvisionsTheIgnoreRuleWhereItWrites(t *testing.T) {
	root := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(root, ConfigDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteAuth(&Auth{Token: "t0ken"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ConfigDir, ".gitignore"))
	if err != nil {
		t.Fatalf("the directory holding auth.json has no ignore rule for it: %v", err)
	}
	if !strings.Contains(string(data), "auth.json") {
		t.Fatalf(".gitignore does not cover auth.json: %q", data)
	}
}

// Guard, expected green: an existing .gitignore is the user's; the credential
// write must not rewrite it.
func TestWriteAuthKeepsAnExistingIgnoreFile(t *testing.T) {
	root := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(root, ConfigDir), 0o755); err != nil {
		t.Fatal(err)
	}
	own := "auth.json\ncustom.secret\n"
	if err := os.WriteFile(filepath.Join(root, ConfigDir, ".gitignore"), []byte(own), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteAuth(&Auth{Token: "t0ken"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ConfigDir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != own {
		t.Fatalf("the user's ignore file was rewritten: %q", data)
	}
}

// The rule every .modernpath-relative path resolver depends on, stated once:
// specs, tasks and the docs export all live in the BOUND workspace, not
// wherever the command happened to be invoked.
func TestWorkspaceConfigDirResolvesUpwardThenCreates(t *testing.T) {
	// EvalSymlinks: on macOS t.TempDir() hands back /var/... while Getwd()
	// reports the resolved /private/var/..., which is a difference in the
	// harness rather than in what is being tested.
	root, err := filepath.EvalSymlinks(chdirTemp(t))
	if err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}
	got, err := WorkspaceConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, ConfigDir); got != want {
		t.Fatalf("want the bound workspace %s, got %s", want, got)
	}

	// nothing bound anywhere above: bind here
	fresh, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(fresh); err != nil {
		t.Fatal(err)
	}
	got, err = WorkspaceConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(fresh, ConfigDir); got != want {
		t.Fatalf("with nothing bound, want %s, got %s", want, got)
	}
}

// WriteAuth stores a credential, so the ignore rule that keeps it out of git
// is part of the same write: an existing .gitignore that does not cover
// auth.json gains that rule without losing its own content, and a rule that
// cannot be written means the token is not stored at all — the acting
// protection, not a best effort.
func TestWriteAuthAppendsTheMissingRuleToAnExistingGitignore(t *testing.T) {
	dir := chdirTemp(t)
	gitignore := filepath.Join(dir, ".modernpath", ".gitignore")
	if err := os.WriteFile(gitignore, []byte("# keep this\nexport-cache/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteAuth(&Auth{Token: "tok"}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(gitignore)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# keep this", "export-cache/", "auth.json"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf(".gitignore lost or never gained %q:\n%s", want, raw)
		}
	}
}

func TestWriteAuthRefusesToStoreATokenItCannotIgnore(t *testing.T) {
	dir := chdirTemp(t)
	// A directory at the .gitignore path makes both reading and writing the
	// rule fail — the shape of any filesystem refusal.
	if err := os.Mkdir(filepath.Join(dir, ".modernpath", ".gitignore"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := WriteAuth(&Auth{Token: "tok"}); err == nil {
		t.Fatal("WriteAuth stored a credential it could not protect")
	}
	if _, err := os.Stat(filepath.Join(dir, ".modernpath", AuthFile)); !os.IsNotExist(err) {
		t.Fatal("the token was written despite the failed ignore rule")
	}
}

// Guards, expected green: a fresh workspace still gets the rule provisioned,
// and a covering file is left exactly as it is.
func TestWriteAuthProvisionsAndDoesNotDuplicateTheIgnoreRules(t *testing.T) {
	dir := chdirTemp(t)
	if err := WriteAuth(&Auth{Token: "tok"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteAuth(&Auth{Token: "tok2"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".modernpath", ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(raw), "auth.json"); got != 1 {
		t.Fatalf("auth.json must appear exactly once, got %d in:\n%s", got, raw)
	}
}
