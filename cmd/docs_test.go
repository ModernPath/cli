// REQ-CROSS-503 (EPIC-CLI-029): the docs verbs reach an import-created
// system. refresh, preview and repair resolve the repository by the id
// `import --local` recorded (else the system's single upload repository,
// else a local-path match) and exit non-zero when none resolves; generate
// starts the lifecycle the UI uses and creates no repository; generate and
// refresh take --yes and, without it and without a terminal, refuse instead
// of exiting 0 with "cancelled" (USER:2026-09-30, D11).
//
// The fake core is lifecycleStub (analysis_test.go).
package cmd

import (
	"net/http"
	"strings"
	"testing"
)

// The system read the stub serves: the import-created upload repository,
// whose local path is a sealed handle the server does not project, and a
// git-linked one. Neither name is the checkout's directory name.
func importedSystem(repositories ...map[string]any) map[string]any {
	list := make([]any, 0, len(repositories))
	for _, r := range repositories {
		list = append(list, r)
	}
	return map[string]any{"id": 42, "name": "Legacy Estate", "repositories": list}
}

func uploadRepo(id int, localPath any) map[string]any {
	return map[string]any{"id": id, "name": "Legacy Estate", "provider": "local", "repository_url": nil, "local_path": localPath}
}

func gitRepo(id int) map[string]any {
	return map[string]any{"id": id, "name": "app", "provider": "github", "repository_url": "https://github.com/acme/app", "local_path": nil}
}

func noChangesPreview() map[string]any {
	return map[string]any{"success": true, "data": map[string]any{"repository_id": 7, "has_changes": false}}
}

func changesPreview() map[string]any {
	return map[string]any{"success": true, "data": map[string]any{
		"repository_id": 7, "has_changes": true,
		"file_changes": map[string]any{"modified": 2, "total": 10},
	}}
}

// AC1: with repository_id in config.json, refresh, preview and repair act on
// that repository although no repository's local path is the checkout.
func TestDocsVerbsResolveTheBoundRepositoryByID(t *testing.T) {
	for _, tc := range []struct {
		args []string
		read string
		body map[string]any
	}{
		{[]string{"docs", "refresh"}, "/api/systems/42/repositories/7/incremental-preview", noChangesPreview()},
		{[]string{"docs", "preview"}, "/api/systems/42/repositories/7/incremental-preview", noChangesPreview()},
		{[]string{"docs", "repair", "--dry-run"}, "/api/systems/42/repositories/7/incomplete-files",
			map[string]any{"success": true, "data": map[string]any{"incomplete_count": 0, "total_files": 10}}},
	} {
		t.Run(tc.args[1], func(t *testing.T) {
			s := newLifecycleStub(t)
			s.answer(http.MethodGet, "/api/systems/42", http.StatusOK, importedSystem(uploadRepo(7, "source://sealed/7"), uploadRepo(8, nil), gitRepo(9)))
			s.answer(http.MethodGet, tc.read, http.StatusOK, tc.body)
			bindLifecycleWorkspace(t, s, 7)

			out, err := runVerb(t, tc.args...)
			if err != nil {
				t.Fatalf("%v: %v\n%s", tc.args, err, out)
			}
			if s.requested(http.MethodGet, tc.read) == nil {
				t.Fatalf("%v must read %s for repository 7:\n%s", tc.args, tc.read, out)
			}
		})
	}
}

// AC1: a config that predates repository_id resolves the system's single
// upload repository, as `source push` does.
func TestDocsVerbsFallBackToTheSingleUploadRepository(t *testing.T) {
	s := newLifecycleStub(t)
	s.answer(http.MethodGet, "/api/systems/42", http.StatusOK, importedSystem(uploadRepo(7, nil), gitRepo(9)))
	s.answer(http.MethodGet, "/api/systems/42/repositories/7/incremental-preview", http.StatusOK, noChangesPreview())
	bindLifecycleWorkspace(t, s, 0)

	out, err := runVerb(t, "docs", "preview")
	if err != nil {
		t.Fatalf("docs preview: %v\n%s", err, out)
	}
	if s.requested(http.MethodGet, "/api/systems/42/repositories/7/incremental-preview") == nil {
		t.Fatalf("preview must resolve the single upload repository 7:\n%s", out)
	}
}

// The record: refresh and preview exit non-zero when no repository resolves
// (today they print the error and exit 0), and print the reason once.
func TestDocsRefreshAndPreviewExitNonZeroWhenNoRepositoryResolves(t *testing.T) {
	for _, verb := range []string{"refresh", "preview"} {
		t.Run(verb, func(t *testing.T) {
			s := newLifecycleStub(t)
			s.answer(http.MethodGet, "/api/systems/42", http.StatusOK, importedSystem(uploadRepo(7, nil), uploadRepo(8, nil)))
			bindLifecycleWorkspace(t, s, 0)

			out, err := runVerb(t, "docs", verb)
			if err == nil {
				t.Fatalf("docs %s must exit non-zero when no repository resolves:\n%s", verb, out)
			}
			if !strings.Contains(out, "repository_id") {
				t.Errorf("the refusal must name the repository_id remedy:\n%s", out)
			}
			if n := strings.Count(out, "repository_id"); n != 1 {
				t.Errorf("the refusal must be printed once, printed %d times:\n%s", n, out)
			}
		})
	}
}

// AC2: generate starts the lifecycle the UI uses — POST git-sources/analyze
// with the analysis mode — prints the run, and creates no repository, even
// though no repository is named after the checkout's directory.
func TestDocsGenerateStartsTheLifecycleWithoutCreatingARepository(t *testing.T) {
	for _, tc := range []struct {
		args []string
		mode string
	}{
		{[]string{"docs", "generate", "--yes"}, "independent_repos"},
		{[]string{"docs", "generate", "--yes", "--mode", "unified_workspace"}, "unified_workspace"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			s := newLifecycleStub(t)
			s.answer(http.MethodGet, "/api/systems/42", http.StatusOK, importedSystem(uploadRepo(7, nil)))
			s.answer(http.MethodPost, "/api/systems/42/repositories", http.StatusCreated, map[string]any{"data": map[string]any{"id": 99, "name": "stray"}})
			s.answer(http.MethodPost, analyzePath, http.StatusAccepted, lifecycleAccepted("15", "queued", tc.mode))
			bindLifecycleWorkspace(t, s, 7)

			out, err := runVerb(t, tc.args...)
			if err != nil {
				t.Fatalf("%v: %v\n%s", tc.args, err, out)
			}
			if posts := s.posts(); len(posts) != 1 || posts[0] != analyzePath {
				t.Fatalf("generate must POST only %s, posted %v\n%s", analyzePath, posts, out)
			}
			if got := s.requested(http.MethodPost, analyzePath).body["analysis_mode"]; got != tc.mode {
				t.Errorf("analysis_mode = %v, want %s", got, tc.mode)
			}
			if !strings.Contains(out, "run 15") {
				t.Errorf("generate must print the run id:\n%s", out)
			}
		})
	}
}

// AC2: a 409 from the Start is printed once by name and exits non-zero.
func TestDocsGeneratePrintsTheConflictByName(t *testing.T) {
	s := newLifecycleStub(t)
	s.answer(http.MethodPost, analyzePath, http.StatusConflict, refusal("analysis_already_running", "analysis is already running"))
	bindLifecycleWorkspace(t, s, 7)

	out, err := runVerb(t, "docs", "generate", "--yes")
	if err == nil {
		t.Fatalf("a 409 must exit non-zero:\n%s", out)
	}
	if n := strings.Count(out, "analysis_already_running"); n != 1 {
		t.Errorf("the 409 must be printed once by its code, printed %d times:\n%s", n, out)
	}
}

// AC5: generate and refresh with --yes reach their POST without a prompt —
// the test has no terminal, so a prompt would refuse or cancel.
func TestDocsGenerateAndRefreshWithYesSkipThePrompt(t *testing.T) {
	t.Run("refresh", func(t *testing.T) {
		s := newLifecycleStub(t)
		s.answer(http.MethodGet, "/api/systems/42/repositories/7/incremental-preview", http.StatusOK, changesPreview())
		s.answer(http.MethodPost, "/api/systems/42/repositories/7/incremental-update", http.StatusOK,
			map[string]any{"success": true, "data": map[string]any{"status": "completed", "summary": map[string]any{"files_modified": 2}}})
		bindLifecycleWorkspace(t, s, 7)
		withoutTerminal(t)

		out, err := runVerb(t, "docs", "refresh", "--yes")
		if err != nil {
			t.Fatalf("docs refresh --yes: %v\n%s", err, out)
		}
		if s.requested(http.MethodPost, "/api/systems/42/repositories/7/incremental-update") == nil {
			t.Fatalf("refresh --yes must run the update, posted %v\n%s", s.posts(), out)
		}
	})
	t.Run("generate", func(t *testing.T) {
		s := newLifecycleStub(t)
		s.answer(http.MethodPost, analyzePath, http.StatusAccepted, lifecycleAccepted("15", "queued", "independent_repos"))
		bindLifecycleWorkspace(t, s, 7)
		withoutTerminal(t)

		out, err := runVerb(t, "docs", "generate", "-y")
		if err != nil {
			t.Fatalf("docs generate -y: %v\n%s", err, out)
		}
		if s.requested(http.MethodPost, analyzePath) == nil {
			t.Fatalf("generate -y must start the analysis, posted %v\n%s", s.posts(), out)
		}
	})
}

// AC5: without --yes and without a terminal, generate and refresh exit
// non-zero, say --yes is required, and send nothing. The repository's local
// path is the checkout, so even today's path match resolves it and the
// difference is the confirmation alone.
func TestDocsGenerateAndRefreshRefuseWithoutATerminalOrYes(t *testing.T) {
	for _, verb := range []string{"generate", "refresh"} {
		t.Run(verb, func(t *testing.T) {
			s := newLifecycleStub(t)
			s.answer(http.MethodPost, analyzePath, http.StatusAccepted, lifecycleAccepted("15", "queued", "independent_repos"))
			s.answer(http.MethodGet, "/api/systems/42/repositories/7/incremental-preview", http.StatusOK, changesPreview())
			s.answer(http.MethodPost, "/api/systems/42/repositories/7/incremental-update", http.StatusOK,
				map[string]any{"success": true, "data": map[string]any{"status": "completed"}})
			cwd := bindLifecycleWorkspace(t, s, 7)
			s.answer(http.MethodGet, "/api/systems/42", http.StatusOK, importedSystem(uploadRepo(7, cwd)))
			withoutTerminal(t)

			out, err := runVerb(t, "docs", verb)
			if err == nil {
				t.Fatalf("docs %s without --yes and without a terminal must exit non-zero:\n%s", verb, out)
			}
			if !strings.Contains(out, "--yes is required for unattended runs") {
				t.Errorf("the refusal must say --yes is required for unattended runs:\n%s", out)
			}
			if posts := s.posts(); len(posts) != 0 {
				t.Errorf("a refused %s must send nothing, posted %v", verb, posts)
			}
		})
	}
}
