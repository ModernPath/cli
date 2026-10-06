// REQ-CROSS-502 (EPIC-CLI-029, USER:2026-09-29 D7): `import` runs unattended
// with --yes and, without --yes and without a terminal, exits non-zero saying
// --yes is required instead of printing "Import cancelled." and exiting 0. It
// offers only the local upload — the git-URL path the server answers 410 is
// gone (D9) — its upload flags are labelled as such, and after an upload the
// next steps say the analysis is queued, name the lifecycle verbs of
// REQ-CROSS-503 and link to the app host the capabilities read names.
//
// The fake core is lifecycleStub (analysis_test.go).
package cmd

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

const appURL = "https://cloud.example.test"

// importCommandWorkspace is a checkout to import: a small tree, a sign-in, the
// stub as the server, and no bound system yet.
func importCommandWorkspace(t *testing.T, s *lifecycleStub) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	writeTree(t, dir, smallTree())
	writeJSON(t, dir, ".modernpath/config.json", map[string]any{"api_url": s.server.URL})
	writeFactoryTestFile(t, dir, ".modernpath/auth.json", `{"token":"t"}`)
	t.Chdir(dir)
	s.answer(http.MethodGet, capabilitiesPath, http.StatusOK, map[string]any{"upload_limit_bytes": 90000000, "app_url": appURL})
	s.answer(http.MethodPost, importPath, http.StatusCreated, map[string]any{"success": true, "data": map[string]any{
		"id": 42, "name": "Legacy Estate", "slug": "legacy-estate", "repository_id": 7,
	}})
}

// AC1: --yes (or -y) skips the confirmation and the upload proceeds, with or
// without --local, which is the only method and the default.
func TestImportWithYesUploadsWithoutAPrompt(t *testing.T) {
	for _, args := range [][]string{{"import", "--local", "--yes"}, {"import", "--yes"}, {"import", "-y"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			s := newLifecycleStub(t)
			importCommandWorkspace(t, s)
			withoutTerminal(t)

			out, err := runVerb(t, args...)
			if err != nil {
				t.Fatalf("%v: %v\n%s", args, err, out)
			}
			if s.requested(http.MethodPost, importPath) == nil {
				t.Errorf("%v must upload, posted %v:\n%s", args, s.posts(), out)
			}
			if strings.Contains(out, "Import cancelled") {
				t.Errorf("%v must not stop at a prompt:\n%s", args, out)
			}
			if !strings.Contains(out, "remote origin") {
				t.Errorf("the git remote line must name origin:\n%s", out)
			}
		})
	}
}

// AC2: without --yes and without a terminal, import exits non-zero, says
// --yes is required for unattended runs, once, and uploads nothing.
func TestImportWithoutATerminalOrYesRefuses(t *testing.T) {
	for _, args := range [][]string{{"import", "--local"}, {"import"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			s := newLifecycleStub(t)
			importCommandWorkspace(t, s)
			withoutTerminal(t)

			out, err := runVerb(t, args...)
			if err == nil {
				t.Fatalf("%v without a terminal and without --yes must exit non-zero:\n%s", args, out)
			}
			var reported reportedError
			if !errors.As(err, &reported) {
				t.Errorf("the refusal must be printed once and returned as reportedError, got %T %v", err, err)
			}
			if n := strings.Count(out, "--yes is required for unattended runs"); n != 1 {
				t.Errorf("the refusal must say once that --yes is required for unattended runs, said %d times:\n%s", n, out)
			}
			if posts := s.posts(); len(posts) != 0 {
				t.Errorf("a refused import must upload nothing, posted %v", posts)
			}
		})
	}
}

// AC3: the help offers no git-URL import and no --git flag, and labels
// --exclude, --no-gitignore and --max-size as local-upload flags.
func TestImportOffersOnlyTheLocalUpload(t *testing.T) {
	out, err := runVerb(t, "import", "--help")
	if err != nil {
		t.Fatalf("import --help: %v", err)
	}
	for _, gone := range []string{"Git URL", "git URL", "--git "} {
		if strings.Contains(out, gone) {
			t.Errorf("import --help must not offer a git-URL import (%q):\n%s", gone, out)
		}
	}
	if importCmd.Flags().Lookup("git") != nil {
		t.Error("the --git flag must be removed")
	}
	if !strings.Contains(out, "--yes") {
		t.Errorf("import --help must document --yes:\n%s", out)
	}
	for _, name := range []string{"exclude", "no-gitignore", "max-size"} {
		flag := importCmd.Flags().Lookup(name)
		if flag == nil {
			t.Errorf("--%s must stay", name)
			continue
		}
		if !strings.Contains(strings.ToLower(flag.Usage), "local upload") {
			t.Errorf("--%s must be labelled as a local-upload flag: %q", name, flag.Usage)
		}
	}
}

// AC4: after an upload the next steps say the analysis is queued, name the
// lifecycle verbs and source push — not docs generate — and the UI link opens
// the app host the capabilities read names; without one, the API host, said.
func TestImportNextStepsNameTheLifecycleAndTheAppLink(t *testing.T) {
	for _, tc := range []struct {
		name   string
		appURL any
	}{
		{"the server names the app", appURL},
		{"the server names none", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newLifecycleStub(t)
			dir := importWorkspace(t, s, smallTree())
			s.answer(http.MethodGet, capabilitiesPath, http.StatusOK, map[string]any{"upload_limit_bytes": 90000000, "app_url": tc.appURL})

			out, err := runImportUpload(t, s, dir, 100)
			if err != nil {
				t.Fatalf("import: %v\n%s", err, out)
			}
			for _, want := range []string{"queued", "modernpath analysis status", "modernpath source push"} {
				if !strings.Contains(out, want) {
					t.Errorf("the next steps must say %q:\n%s", want, out)
				}
			}
			if strings.Contains(out, "docs generate") {
				t.Errorf("the next steps must not send the customer to docs generate:\n%s", out)
			}
			if tc.appURL != nil {
				if !strings.Contains(out, appURL+"/systems/42/overview") {
					t.Errorf("the UI link must open the app host %s:\n%s", appURL, out)
				}
				if strings.Contains(out, s.server.URL+"/systems/42") {
					t.Errorf("the UI link must not use the API host:\n%s", out)
				}
				return
			}
			if !strings.Contains(out, s.server.URL+"/systems/42/overview") || !strings.Contains(out, "API host") {
				t.Errorf("without an app URL the link uses the API host and says so:\n%s", out)
			}
		})
	}
}

// AC4: the app address comes from the server and is printed as a link, so
// only an absolute http or https URL with a host and no control characters
// is used; anything else is treated as no address at all: the API-host link,
// said, and nothing of the value printed.
func TestImportAppLinkUsesOnlyAWebAddress(t *testing.T) {
	for _, tc := range []struct {
		name, appURL, never string
	}{
		{"a javascript: URL", "javascript:alert(1)", "javascript:"},
		{"an embedded newline", appURL + "/\nRun: modernpath auth --token stolen", "stolen"},
		{"an escape sequence", appURL + "/\x1b]8;;https://evil.test\x07", "evil.test"},
		{"a C1 control character", appURL + "/\u009b31m", "\u009b"},
		{"a relative path", "/app", ""},
		{"no host", "https://", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newLifecycleStub(t)
			dir := importWorkspace(t, s, smallTree())
			s.answer(http.MethodGet, capabilitiesPath, http.StatusOK, map[string]any{"upload_limit_bytes": 90000000, "app_url": tc.appURL})

			out, err := runImportUpload(t, s, dir, 100)
			if err != nil {
				t.Fatalf("import: %v\n%s", err, out)
			}
			if !strings.Contains(out, s.server.URL+"/systems/42/overview") || !strings.Contains(out, "API host") {
				t.Errorf("app_url %q must fall back to the API-host link, said:\n%s", tc.appURL, out)
			}
			if tc.never != "" && strings.Contains(out, tc.never) {
				t.Errorf("app_url %q must not be printed:\n%q", tc.appURL, out)
			}
		})
	}
}
