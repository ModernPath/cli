package cmd

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

// The 13 November end-to-end walkthrough (findings F-1-7 and F-1-8) bound a
// checkout to a System made in the web app, which has no repository. source
// push said its repositories were linked to a git provider, and analysis
// status advised a hand edit of the CLI's private .modernpath/config.json.
// Each refusal says what the System has and names a command instead.

func requireAdvice(t *testing.T, err error, out string, want, forbid []string) {
	t.Helper()
	if err == nil {
		t.Fatalf("the command must exit non-zero\n%s", out)
	}
	text := err.Error() + "\n" + out
	for _, phrase := range want {
		if !strings.Contains(text, phrase) {
			t.Errorf("the refusal must say %q, got: %s", phrase, text)
		}
	}
	for _, phrase := range forbid {
		if strings.Contains(text, phrase) {
			t.Errorf("the refusal must not say %q, got: %s", phrase, text)
		}
	}
}

var noRepositoryAdvice = []string{
	"system 42 has no repository",
	"run 'modernpath import' in this directory",
	"creates a new System with this directory as its repository",
}

func TestSourcePushOnASystemWithNoRepositoryNamesImport(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, parityFixture)
	t.Chdir(dir)
	stub := newPushStub(t)
	stub.system = map[string]interface{}{"id": 42, "repositories": []map[string]interface{}{}}

	var out bytes.Buffer
	err := sourcePush(stub.config(0), sourcePushOptions{MaxSizeMB: 100}, &out)
	requireAdvice(t, err, out.String(), noRepositoryAdvice, []string{"git provider", "config.json"})
	if len(stub.posts) != 0 {
		t.Fatal("nothing may be posted")
	}
}

var severalUploadsAdvice = []string{
	"in the checkout where 'modernpath import' created the repository",
}

func TestSourcePushWithSeveralUploadRepositoriesNamesNoConfigEdit(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, parityFixture)
	t.Chdir(dir)
	stub := newPushStub(t)
	stub.system = map[string]interface{}{
		"id": 42,
		"repositories": []map[string]interface{}{
			{"id": 7, "name": "one", "provider": "local", "repository_url": nil},
			{"id": 8, "name": "two", "provider": "local", "repository_url": nil},
		},
	}

	var out bytes.Buffer
	err := sourcePush(stub.config(0), sourcePushOptions{MaxSizeMB: 100}, &out)
	requireAdvice(t, err, out.String(), append([]string{"system 42 has 2 upload repositories"}, severalUploadsAdvice...), []string{"config.json"})
}

func TestAnalysisStatusOnASystemWithNoRepositoryNamesImport(t *testing.T) {
	s := newLifecycleStub(t)
	s.answer(http.MethodGet, "/api/systems/42", http.StatusOK, map[string]any{"id": 42, "repositories": []any{}})
	bindLifecycleWorkspace(t, s, 0)

	out, err := runVerb(t, "analysis", "status")
	requireAdvice(t, err, out, noRepositoryAdvice, []string{"config.json"})
}

func TestAnalysisStatusWithSeveralUploadRepositoriesNamesNoConfigEdit(t *testing.T) {
	s := newLifecycleStub(t)
	s.answer(http.MethodGet, "/api/systems/42", http.StatusOK, map[string]any{"id": 42, "repositories": []any{
		map[string]any{"id": 7, "repository_url": nil, "local_path": "/elsewhere/one"},
		map[string]any{"id": 8, "repository_url": nil, "local_path": "/elsewhere/two"},
	}})
	bindLifecycleWorkspace(t, s, 0)

	out, err := runVerb(t, "analysis", "status")
	requireAdvice(t, err, out, severalUploadsAdvice, []string{"config.json"})
}

func TestAnalysisStatusWithAStaleRepositoryNamesInit(t *testing.T) {
	s := newLifecycleStub(t)
	s.answer(http.MethodGet, repoReadPath, http.StatusNotFound, refusal("not_found", "Repository not found"))
	bindLifecycleWorkspace(t, s, 7)

	out, err := runVerb(t, "analysis", "status")
	requireAdvice(t, err, out, []string{
		"system 42 has no repository 7",
		"run 'modernpath init --force --system-id 42'",
	}, []string{"config.json"})
}
