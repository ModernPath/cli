// REQ-CROSS-501 (EPIC-CLI-029): every upload filter is reported. The scan
// report of `import --local` and `source push` lists each built-in directory
// name and extension that dropped files, with the file count and bytes,
// beside the .gitignore and --exclude drops (push prints the .gitignore count
// too); --keep keeps a built-in directory drop for one run and the report
// says so; --exclude matches the way its help says (TestMatchesExclude).
//
// The fake core is lifecycleStub (analysis_test.go); the two verbs and their
// workspaces are uploadVerbs (import_test.go).
package cmd

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// builtInDropsTree holds a JavaScript monorepo's packages/, .NET build output
// under bin/ at two depths, and .svg images; only src/App/Program.cs and the
// .gitignore survive the built-in filters, and logs/ is gitignored.
func builtInDropsTree() map[string]string {
	return map[string]string{
		"packages/web/index.js":     strings.Repeat("w", 100),
		"packages/api/index.js":     strings.Repeat("a", 200),
		"src/App/bin/Debug/App.pdb": strings.Repeat("p", 50),
		"bin/tool.json":             strings.Repeat("t", 70),
		"assets/logo.svg":           strings.Repeat("s", 40),
		"assets/icons/x.svg":        strings.Repeat("x", 60),
		"logs/app.log":              strings.Repeat("l", 30),
		"src/App/Program.cs":        "class Program {}\n",
		".gitignore":                "logs/\n",
	}
}

// runUploadFlags runs verb with flags: push on its command line; import's
// upload half through import's flag set, since it has no command line.
func runUploadFlags(t *testing.T, verb uploadVerb, s *lifecycleStub, flags ...string) (string, error) {
	t.Helper()
	if verb.name == "source push" {
		return runVerb(t, append([]string{"source", "push"}, flags...)...)
	}
	resetTreeFlags(rootCmd)
	t.Cleanup(func() { resetTreeFlags(rootCmd) })
	for i := 0; i+1 < len(flags); i += 2 {
		if err := importCmd.Flags().Set(strings.TrimPrefix(flags[i], "--"), flags[i+1]); err != nil {
			return "", err
		}
	}
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	return captureStreams(t, func() error { return importViaUpload(s.server.URL, "t", "Legacy Estate", dir) })
}

// reportLine returns the first output line holding every one of parts.
func reportLine(out string, parts ...string) string {
	for _, line := range strings.Split(out, "\n") {
		found := true
		for _, part := range parts {
			if !strings.Contains(line, part) {
				found = false
				break
			}
		}
		if found {
			return line
		}
	}
	return ""
}

// AC1: the scan report of import and of push lists each built-in drop — the
// directory names packages/ and bin/ (at any depth) and the extension .svg —
// with its file count and bytes, and push reports the .gitignore drop as
// import does.
func TestUploadReportsEveryBuiltInDrop(t *testing.T) {
	for _, verb := range uploadVerbs {
		t.Run(verb.name, func(t *testing.T) {
			s := newLifecycleStub(t)
			dir := verb.setup(t, s, builtInDropsTree())
			gitInit(t, dir)
			s.answer(http.MethodGet, capabilitiesPath, http.StatusOK, capabilities(90000000))

			out, err := runUploadFlags(t, verb, s)
			if err != nil {
				t.Fatalf("%s: %v\n%s", verb.name, err, out)
			}
			for _, want := range [][]string{
				{"packages/", "2 files", "300 bytes"},
				{"bin/", "2 files", "120 bytes"},
				{"*.svg", "2 files", "100 bytes"},
				{".gitignore", "1 file"},
			} {
				if reportLine(out, want...) == "" {
					t.Errorf("the scan report must have a line with %q:\n%s", want, out)
				}
			}
		})
	}
}

// AC3: --keep packages keeps the built-in packages/ drop for the run — its
// files are uploaded — and the report says it was kept. A name that is not a
// built-in directory drop, or the binding's own .modernpath, is refused.
func TestKeepKeepsABuiltInDirectory(t *testing.T) {
	for _, verb := range uploadVerbs {
		t.Run(verb.name+"/packages", func(t *testing.T) {
			s := newLifecycleStub(t)
			gitInit(t, verb.setup(t, s, builtInDropsTree()))
			s.answer(http.MethodGet, capabilitiesPath, http.StatusOK, capabilities(90000000))

			out, err := runUploadFlags(t, verb, s, "--keep", "packages")
			if err != nil {
				t.Fatalf("%s --keep packages: %v\n%s", verb.name, err, out)
			}
			if s.requested(http.MethodPost, verb.postPath) == nil {
				t.Errorf("the upload must be sent:\n%s", out)
			}
			if !strings.Contains(out, "Files: 4") {
				t.Errorf("the two packages/ files must be uploaded beside Program.cs and .gitignore (Files: 4):\n%s", out)
			}
			if reportLine(out, "--keep", "packages/") == "" {
				t.Errorf("the report must say packages/ was kept by --keep:\n%s", out)
			}
			if line := reportLine(out, "packages/", "2 files"); line != "" {
				t.Errorf("a kept directory must not be reported as skipped: %q\n%s", line, out)
			}
		})
		for _, name := range []string{"packges", ".modernpath"} {
			t.Run(verb.name+"/"+name+" is refused", func(t *testing.T) {
				s := newLifecycleStub(t)
				verb.setup(t, s, builtInDropsTree())
				s.answer(http.MethodGet, capabilitiesPath, http.StatusOK, capabilities(90000000))

				out, err := runUploadFlags(t, verb, s, "--keep", name)
				if err == nil {
					t.Fatalf("--keep %s must be refused:\n%s", name, out)
				}
				if s.requested(http.MethodPost, verb.postPath) != nil {
					t.Errorf("a refused --keep must upload nothing:\n%s", out)
				}
				if !strings.Contains(out+err.Error(), "--keep "+name+" is not") {
					t.Errorf("the refusal must say --keep %s is not a directory it can keep:\n%s\n%v", name, out, err)
				}
			})
		}
	}
}

// AC2: --exclude's help says how a pattern matches, for both verbs.
func TestExcludeHelpSaysHowPatternsMatch(t *testing.T) {
	for _, args := range [][]string{{"import", "--help"}, {"source", "push", "--help"}} {
		out, err := runVerb(t, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		for _, want := range []string{"any depth", "**", "--keep"} {
			if !strings.Contains(out, want) {
				t.Errorf("%v must say %q:\n%s", args, want, out)
			}
		}
	}
}

// AC2: a malformed --exclude pattern matches nothing, so the operator would
// believe files were left out that are uploaded. Every pattern is checked
// before the scan; a malformed one refuses the command, naming it once, and
// nothing is uploaded. Both verbs run through the command tree.
func TestMalformedExcludeRefusesTheUpload(t *testing.T) {
	for _, verb := range []struct {
		name     string
		args     []string
		postPath string
		setup    func(t *testing.T, s *lifecycleStub)
	}{
		{"import", []string{"import", "--yes"}, importPath, func(t *testing.T, s *lifecycleStub) {
			importCommandWorkspace(t, s)
			withoutTerminal(t)
		}},
		{"source push", []string{"source", "push"}, pushArchivePath, func(t *testing.T, s *lifecycleStub) {
			pushWorkspace(t, s, smallTree())
			s.answer(http.MethodGet, capabilitiesPath, http.StatusOK, capabilities(90000000))
		}},
	} {
		for _, pattern := range []string{"[", "src/[a-"} {
			t.Run(verb.name+"/"+pattern, func(t *testing.T) {
				s := newLifecycleStub(t)
				verb.setup(t, s)

				out, err := runVerb(t, append(verb.args, "--exclude", "*.min.js", "--exclude", pattern)...)
				refusedOnce(t, err, out)
				if s.requested(http.MethodPost, verb.postPath) != nil {
					t.Errorf("a malformed --exclude must upload nothing:\n%s", out)
				}
				if named := fmt.Sprintf("--exclude %q", pattern); strings.Count(out, named) != 1 {
					t.Errorf("the refusal must name %s once:\n%s", named, out)
				}
			})
		}
	}
}
