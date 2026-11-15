package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// Epic 427: epics build record turns a delegated build's handoff into one
// build-record write.

const (
	buildR1 = "11111111-1111-4111-8111-111111111111"
	buildR2 = "22222222-2222-4222-8222-222222222222"
	buildT1 = "a1000000-0000-4000-8000-000000000001"
	buildC1 = "c0000000-0000-4000-8000-000000000001"
)

// buildServer answers the plan read with plan and the build-record write
// with status and reply, recording every request.
type buildServer struct {
	mu     sync.Mutex
	calls  []batchedCall
	plan   map[string]any
	status int
	reply  string
	delay  time.Duration
}

func (s *buildServer) posts() []batchedCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	var posts []batchedCall
	for _, call := range s.calls {
		if call.method == http.MethodPost {
			posts = append(posts, call)
		}
	}
	return posts
}

func (s *buildServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func newBuildServer(t *testing.T, plan map[string]any) (*buildServer, *httptest.Server) {
	t.Helper()
	state := &buildServer{plan: plan, status: 200, reply: `{"data":{"dry_run":false,"traces":[],"results":[],"assumed_defaults":[]}}`}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		_ = decoder.Decode(&body)
		state.mu.Lock()
		state.calls = append(state.calls, batchedCall{r.Method, r.URL.Path + "?" + r.URL.RawQuery, body})
		status, reply, delay := state.status, state.reply, state.delay
		state.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/plan") {
			raw, _ := json.Marshal(map[string]any{"data": map[string]any{"id": 427, "plan": state.plan}})
			_, _ = w.Write(raw)
			return
		}
		if delay > 0 {
			time.Sleep(delay)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(server.Close)
	return state, server
}

// buildPlan is a saved plan with the given requirements (kind, id) and
// verification criteria (criterion id, TestCase id).
func buildPlan(requirements [][2]string, criteria [][2]string) map[string]any {
	reqs := []any{}
	for _, r := range requirements {
		reqs = append(reqs, map[string]any{"kind": r[0], "id": r[1], "content": map[string]any{}})
	}
	crits := []any{}
	for _, c := range criteria {
		crits = append(crits, map[string]any{"id": c[0], "test_case_id": c[1], "boundary": "unit", "changed": true})
	}
	return map[string]any{"body": "plan", "requirements": reqs, "verification": map[string]any{"criteria": crits}}
}

func buildGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	all := append([]string{"-C", root, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", all...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func buildWrite(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// buildWorkspace binds a temporary workspace to server, makes it a git
// repository and commits base files, then head files. It returns the
// workspace root, its env and the base and head commits.
func buildWorkspace(t *testing.T, server *httptest.Server, base, head map[string]string) (string, *cliEnv, string, string) {
	t.Helper()
	env := nativeCLIEnv(t, server, 4)
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	buildGit(t, root, "init", "-q")
	buildWrite(t, root, ".gitignore", ".modernpath/\nresults/\nhandoff.json\n")
	for rel, body := range base {
		buildWrite(t, root, rel, body)
	}
	buildGit(t, root, "add", "-A")
	buildGit(t, root, "commit", "-q", "-m", "base")
	baseSHA := buildGit(t, root, "rev-parse", "HEAD")
	for rel, body := range head {
		buildWrite(t, root, rel, body)
	}
	buildGit(t, root, "add", "-A")
	buildGit(t, root, "commit", "-q", "--allow-empty", "-m", "head")
	return root, env, baseSHA, buildGit(t, root, "rev-parse", "HEAD")
}

func writeHandoff(t *testing.T, root string, handoff map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(handoff)
	if err != nil {
		t.Fatal(err)
	}
	buildWrite(t, root, "handoff.json", string(raw))
	return filepath.Join(root, "handoff.json")
}

func runBuildRecord(t *testing.T, env *cliEnv, args ...string) (string, error) {
	t.Helper()
	command := newEpicBuildCommand(func() (*cliEnv, error) { return env, nil })
	command.SetArgs(append([]string{"record", "427"}, args...))
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&out)
	command.SilenceUsage = true
	err := command.Execute()
	return out.String(), err
}

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", "test_results", name))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func fixtureText(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(fixturePath(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

const minimalGoTest = `package cart

import "testing"

// TestCase ` + buildT1 + `
func TestAdd(t *testing.T) {}
`

const minimalGoResults = `{"Action":"start","Package":"example.com/shop/cart"}
{"Action":"run","Package":"example.com/shop/cart","Test":"TestAdd"}
{"Action":"pass","Package":"example.com/shop/cart","Test":"TestAdd","Elapsed":0}
{"Action":"pass","Package":"example.com/shop/cart","Elapsed":0.1}
`

// minimalBuild is a workspace with one requirement, one criterion and one Go
// test that passes, and a handoff that records them.
func minimalBuild(t *testing.T) (*buildServer, string, *cliEnv, map[string]any) {
	t.Helper()
	state, server := newBuildServer(t, buildPlan([][2]string{{"system_requirement", buildR1}}, [][2]string{{buildC1, buildT1}}))
	root, env, base, head := buildWorkspace(t, server,
		map[string]string{"README.md": "# shop\n"},
		map[string]string{"go.mod": "module example.com/shop\n\ngo 1.24\n", "cart/cart.go": "package cart\n", "cart/cart_test.go": minimalGoTest})
	buildWrite(t, root, "results/go.json", minimalGoResults)
	handoff := map[string]any{
		"epic_id": 427, "head": head, "base": base,
		"results":          []any{map[string]any{"format": "go-test-json", "path": "results/go.json"}},
		"requirements":     []any{map[string]any{"requirement_id": buildR1, "paths": []any{"cart/cart.go"}}},
		"assumed_defaults": []any{map[string]any{"title": "Cart limit", "observation": "No limit was specified; the build assumed 100 items."}},
	}
	return state, root, env, handoff
}

func onlyPost(t *testing.T, state *buildServer) map[string]any {
	t.Helper()
	posts := state.posts()
	if len(posts) != 1 {
		t.Fatalf("want exactly one build-record write, got %d: %#v", len(posts), posts)
	}
	if !strings.HasPrefix(posts[0].path, "/api/work/epics/427/build-record?") || !strings.Contains(posts[0].path, "system_id=4") {
		t.Fatalf("write went to %q", posts[0].path)
	}
	return posts[0].body
}

func asJSON(t *testing.T, value any) any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var out any
	if err := decoder.Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// ---------------------------------------------------------------- C2

func outcomesByName(results []testResult) map[string]string {
	out := map[string]string{}
	for _, r := range results {
		out[r.Name] = r.Outcome
	}
	return out
}

// TestCase 0448476b-6633-4906-afd3-70ef096f14fc
func TestBuildRecordReadsResultOutcomes(t *testing.T) {
	t.Run("trx", func(t *testing.T) {
		parsed, err := parseTestResults("trx", fixturePath(t, "xunit.trx"), "")
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{
			"Calc.Tests.CalculatorTests.Adds(a: 1, b: 2, expected: 3)": "pass",
			"Calc.Tests.CalculatorTests.Adds(a: 2, b: 2, expected: 5)": "fail",
			"Calc.Tests.CalculatorTests+Division.Divides":              "pass",
			"Calc.Tests.CalculatorTests.Subtracts":                     "skip",
			"Calc.Tests.CalculatorTests.Multiplies":                    "error",
		}
		if got := outcomesByName(parsed.Results); !reflect.DeepEqual(got, want) {
			t.Fatalf("trx outcomes = %v, want %v", got, want)
		}
		for _, r := range parsed.Results {
			switch r.Name {
			case "Calc.Tests.CalculatorTests+Division.Divides":
				if r.Normalized != "Divides" || r.Unit != "Calc.Tests.CalculatorTests+Division" {
					t.Errorf("nested test: %+v", r)
				}
			case "Calc.Tests.CalculatorTests.Adds(a: 1, b: 2, expected: 3)":
				if r.Normalized != "Adds" || r.Unit != "Calc.Tests.CalculatorTests" {
					t.Errorf("theory: %+v", r)
				}
			case "Calc.Tests.CalculatorTests.Subtracts":
				if r.TestCaseID != "9a7c1d2e-3f40-4b51-8c62-7d8e9f001122" {
					t.Errorf("trait test_case_id not read: %+v", r)
				}
			}
		}
	})
	t.Run("every trx outcome", func(t *testing.T) {
		mapping := map[string]string{
			"Passed": "pass", "Warning": "pass", "Failed": "fail",
			"Error": "error", "Timeout": "error", "Aborted": "error", "PassedButRunAborted": "error", "Disconnected": "error", "InProgress": "error",
			"NotExecuted": "skip", "NotRunnable": "skip", "Pending": "skip",
			"Inconclusive": "inconclusive", "Completed": "inconclusive",
		}
		var trx strings.Builder
		trx.WriteString(`<?xml version="1.0" encoding="utf-8"?><TestRun xmlns="http://microsoft.com/schemas/VisualStudio/TeamTest/2010"><Results>`)
		for outcome := range mapping {
			fmt.Fprintf(&trx, `<UnitTestResult testId="%s" testName="Ns.T.M_%s" outcome="%s"/>`, outcome, outcome, outcome)
		}
		trx.WriteString(`</Results></TestRun>`)
		path := filepath.Join(t.TempDir(), "all.trx")
		if err := os.WriteFile(path, []byte(trx.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		parsed, err := parseTestResults("trx", path, "")
		if err != nil {
			t.Fatal(err)
		}
		got := outcomesByName(parsed.Results)
		for outcome, want := range mapping {
			if got["Ns.T.M_"+outcome] != want {
				t.Errorf("TRX %s = %q, want %q", outcome, got["Ns.T.M_"+outcome], want)
			}
		}
	})
	t.Run("vitest junit", func(t *testing.T) {
		parsed, err := parseTestResults("junit", fixturePath(t, "vitest.xml"), "")
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"cart > adds an item": "pass", "cart > removes an item": "fail", "cart > totals": "skip", "cart > checkout": "error"}
		if got := outcomesByName(parsed.Results); !reflect.DeepEqual(got, want) {
			t.Fatalf("junit outcomes = %v, want %v", got, want)
		}
		if parsed.Results[0].Unit != "src/cart.test.ts" || parsed.Results[0].Normalized != "cart > adds an item" {
			t.Fatalf("junit unit and name: %+v", parsed.Results[0])
		}
	})
	t.Run("exunit junit", func(t *testing.T) {
		parsed, err := parseTestResults("junit", fixturePath(t, "exunit.xml"), "")
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"test adds an item": "pass", "test totals with discount": "fail", "test legacy pricing": "skip"}
		if got := outcomesByName(parsed.Results); !reflect.DeepEqual(got, want) {
			t.Fatalf("exunit outcomes = %v, want %v", got, want)
		}
		if parsed.Results[0].Unit != "Elixir.Shop.CartTest" {
			t.Fatalf("exunit unit: %+v", parsed.Results[0])
		}
	})
	t.Run("junit property and single testsuite root", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "one.xml")
		body := `<testsuite name="s"><testcase classname="c" name="n"><properties><property name="test_case_id" value="` + buildT1 + `"/></properties></testcase></testsuite>`
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		parsed, err := parseTestResults("junit", path, "")
		if err != nil || len(parsed.Results) != 1 || parsed.Results[0].TestCaseID != buildT1 || parsed.Results[0].Outcome != "pass" {
			t.Fatalf("junit property: %v %+v", err, parsed.Results)
		}
	})
	// The fixture is go test -json output of Go 1.25 (Go 1.24 or later reports
	// a build failure as a package fail event with FailedBuild).
	t.Run("go test json", func(t *testing.T) {
		parsed, err := parseTestResults("go-test-json", fixturePath(t, "go_test.json"), "")
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, r := range parsed.Results {
			got[r.Unit+" "+r.Name] = r.Outcome
			if r.Name == "TestTotals/discount" && r.Normalized != "TestTotals" {
				t.Errorf("subtest normalized to %q", r.Normalized)
			}
		}
		want := map[string]string{
			"example.com/shop/after TestFine":           "pass",
			"example.com/shop/cart TestAdd":             "pass",
			"example.com/shop/cart TestRemove":          "fail",
			"example.com/shop/cart TestTotals":          "pass",
			"example.com/shop/cart TestTotals/discount": "pass",
			"example.com/shop/cart TestTotals/tax":      "pass",
			"example.com/shop/cart TestLegacy":          "skip",
			"example.com/shop/cart TestExits":           "error",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("go outcomes = %v, want %v", got, want)
		}
		if len(parsed.NotRecorded) != 2 ||
			parsed.NotRecorded[0].Package != "example.com/shop/broken" || !strings.Contains(parsed.NotRecorded[0].Failure, "undefined: undefinedThing") ||
			parsed.NotRecorded[1].Package != "example.com/shop/setup" || !strings.Contains(parsed.NotRecorded[1].Failure, "setup: database unavailable") {
			t.Fatalf("packages not recorded = %+v", parsed.NotRecorded)
		}
		if len(parsed.FailedAfterPassing) != 1 || parsed.FailedAfterPassing[0].Package != "example.com/shop/after" ||
			!strings.Contains(parsed.FailedAfterPassing[0].Failure, "teardown: leaked goroutines") {
			t.Fatalf("packages failed after passing = %+v", parsed.FailedAfterPassing)
		}
	})
}

// The fixture is the real JUnit file `mix test` wrote for the core app with
// MP_JUNIT_DIR set (Epic 427, C13): its file attribute is relative to the app,
// so the result entry's root is the app's directory.
// TestCase e99da73e-b887-4912-b6dd-9e2267743246
func TestBuildRecordReadsTheCoreAppJUnitReport(t *testing.T) {
	const root = "modernpath-core/apps/core"
	parsed, err := parseTestResults("junit", fixturePath(t, "exunit_core.xml"), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Results) != 2 {
		t.Fatalf("results = %+v, want the two tests of the report", parsed.Results)
	}
	file := root + "/test/core/junit_report_test.exs"
	source := strings.Split(strings.Join([]string{
		"defmodule Core.Test.JUnitReportTest do",
		"  # TestCase e99da73e-b887-4912-b6dd-9e2267743246",
		`  test "without MP_JUNIT_DIR the run is unchanged; with it a JUnit report per app is written" do`,
		"  end",
		"  # TestCase e99da73e-b887-4912-b6dd-9e2267743246",
		`  test "this app's test helper configures ExUnit from MP_JUNIT_DIR" do`,
		"  end",
		"end",
	}, "\n"), "\n")
	repo := &headRepo{root: t.TempDir(), tracked: map[string]bool{file: true}, contents: map[string][]string{file: source}}
	declared := []string{}
	for _, at := range []int{1, 4} {
		name, ok := declarationBelow(source, at)
		if !ok {
			t.Fatalf("no declaration below line %d", at)
		}
		declared = append(declared, name)
	}
	for _, r := range parsed.Results {
		if r.Outcome != "pass" {
			t.Errorf("%q outcome = %q, want pass", r.Name, r.Outcome)
		}
		if !repo.resultFromFile(r, file) {
			t.Errorf("%q (file %q, unit %q) does not come from %s", r.Name, r.UnitFile, r.Unit, file)
		}
		named := false
		for _, d := range declared {
			named = named || resultNamesDeclaration(r.Normalized, d)
		}
		if !named {
			t.Errorf("%q names none of %q", r.Normalized, declared)
		}
	}
}

// MP_JUNIT_REPORTS names a folder that `mix test` filled with MP_JUNIT_DIR set;
// scripts/check-junit-reports.sh runs both apps' report tests into it and then
// this test, and fails unless it passes. Without the folder the test skips.
// TestCase e99da73e-b887-4912-b6dd-9e2267743246
func TestBuildRecordReadsBothAppsJUnitReports(t *testing.T) {
	dir := os.Getenv("MP_JUNIT_REPORTS")
	if dir == "" {
		t.Skip("MP_JUNIT_REPORTS is unset; run scripts/check-junit-reports.sh")
	}
	for _, app := range []string{"core", "core_http_api"} {
		path := filepath.Join(dir, app+".xml")
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 {
			t.Fatalf("%s: no JUnit report written (%v)", path, err)
		}
		parsed, err := parseTestResults("junit", path, "modernpath-core/apps/"+app)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if len(parsed.Results) == 0 {
			t.Fatalf("%s: the report has no test results", path)
		}
		for _, r := range parsed.Results {
			if r.Outcome != "pass" {
				t.Errorf("%s: %q outcome = %q, want pass", path, r.Name, r.Outcome)
			}
			if !strings.HasPrefix(r.UnitFile, "test/") {
				t.Errorf("%s: %q file attribute = %q, want a path relative to the app", path, r.Name, r.UnitFile)
			}
		}
	}
}

// TestCase 0448476b-6633-4906-afd3-70ef096f14fc
func TestBuildRecordRefusesUnparseableResultFile(t *testing.T) {
	for _, tc := range []struct{ format, body string }{
		{"junit", "<testsuites><testcase name=\"x\">"},
		{"trx", "not xml at all"},
		{"go-test-json", "{\"Action\":\"pass\"}\nok  \texample.com/shop\n"},
	} {
		t.Run(tc.format, func(t *testing.T) {
			state, root, env, handoff := minimalBuild(t)
			buildWrite(t, root, "results/bad.out", tc.body)
			handoff["results"] = []any{map[string]any{"format": tc.format, "path": "results/bad.out"}}
			_, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff))
			if err == nil || !strings.Contains(err.Error(), "results/bad.out") {
				t.Fatalf("unparseable result file: err = %v", err)
			}
			if state.count() != 0 {
				t.Fatalf("an unparseable result file sent %d requests", state.count())
			}
		})
	}
}

// ---------------------------------------------------------------- C3

const (
	tGoAdd     = "a1000000-0000-4000-8000-000000000001"
	tGoTotals  = "a1000000-0000-4000-8000-000000000002"
	tGoExits   = "a1000000-0000-4000-8000-000000000003"
	tGoAfter   = "a1000000-0000-4000-8000-000000000004"
	tToolsAdd  = "a1000000-0000-4000-8000-000000000005"
	tTrxAdds   = "a1000000-0000-4000-8000-000000000006"
	tTrxDiv    = "a1000000-0000-4000-8000-000000000007"
	tTrxTrait  = "9a7c1d2e-3f40-4b51-8c62-7d8e9f001122"
	tTrxMult   = "a1000000-0000-4000-8000-000000000008"
	tVitest    = "a1000000-0000-4000-8000-000000000009"
	tExUnit    = "a1000000-0000-4000-8000-00000000000a"
	tDescribe  = "a1000000-0000-4000-8000-00000000000b"
	tNamed     = "a1000000-0000-4000-8000-00000000000c"
	tNotInPlan = "99999999-9999-4999-8999-999999999999"
)

func crit(n int) string { return fmt.Sprintf("c0000000-0000-4000-8000-%012d", n) }

var matchFiles = map[string]string{
	"go.mod":       "module example.com/shop\n\ngo 1.24\n",
	"cart/cart.go": "package cart\n",
	"cart/cart_test.go": `package cart

import (
	"os"
	"testing"
)

// TestCase ` + tGoAdd + `
func TestAdd(t *testing.T) {}

func TestRemove(t *testing.T) { t.Fatal("expected 0 items, got 1") }

// TestCase ` + tGoTotals + `
// totals run as subtests
func TestTotals(t *testing.T) {
	t.Run("discount", func(t *testing.T) {})
	t.Run("tax", func(t *testing.T) {})
}

func TestLegacy(t *testing.T) { t.Skip("legacy path") }

// TestCase ` + tGoExits + `
func TestExits(t *testing.T) { os.Exit(3) }
`,
	"after/after_test.go": `package after

import "testing"

// TestCase ` + tGoAfter + `
func TestFine(t *testing.T) {}
`,
	"tools/go.mod": "module example.com/tools\n\ngo 1.24\n",
	"tools/cart/cart_test.go": `package cart

import "testing"

// TestCase ` + tToolsAdd + `
func TestAdd(t *testing.T) {}
`,
	"tests/Calc.Tests/CalculatorTests.cs": `using Xunit;

namespace Calc.Tests;

public class CalculatorTests
{
    // TestCase ` + tTrxAdds + `
    [Theory]
    [InlineData(1, 2, 3)]
    [InlineData(2, 2, 5)]
    public void Adds(int a, int b, int expected) => Assert.Equal(expected, a + b);

    // TestCase ` + tTrxTrait + `
    [Fact(Skip = "rounding rules pending")]
    [Trait("test_case_id", "` + tTrxTrait + `")]
    public void Subtracts() { }

    [Fact]
    public async Task Multiplies() { await Task.Delay(5000); }

    public class Division
    {
        /* TestCase ` + tTrxDiv + `
         * divides evenly */
        [Fact]
        public void Divides() => Assert.Equal(2, 4 / 2);
    }
}
`,
	"src/cart.ts": "export const cart = {};\n",
	"src/cart.test.ts": `import { describe, it, expect } from "vitest";

// TestCase ` + tDescribe + `
describe("cart", () => {
  // TestCase ` + tVitest + `
  it("adds an item", () => {
    expect(1).toBe(1);
  });
});
`,
	"src/misc.test.ts": `it("checkout ` + tNamed + ` works", () => {});
`,
	"test/shop/cart_test.exs": `defmodule Shop.CartTest do
  use ExUnit.Case, async: true

  # TestCase ` + tExUnit + `
  @tag :pricing
  test "adds an item" do
    assert true
  end
end
`,
}

const namedJUnit = `<testsuites><testsuite name="misc">
<testcase classname="misc" name="checkout ` + tNamed + ` works"/>
<testcase classname="misc" name="refund ` + tNotInPlan + `"/>
<testcase classname="misc" name="partial a1000000-0000 prefix"/>
</testsuite></testsuites>`

// TestCase 1404fef1-90f9-4ff7-af61-4710cdf7ab10
func TestBuildRecordMatchesResultsToPlanTestCases(t *testing.T) {
	testCases := []string{tGoAdd, tGoTotals, tGoExits, tGoAfter, tToolsAdd, tTrxAdds, tTrxDiv, tTrxTrait, tTrxMult, tVitest, tExUnit, tDescribe, tNamed}
	var criteria [][2]string
	for i, tc := range testCases {
		criteria = append(criteria, [2]string{crit(i + 1), tc})
	}
	state, server := newBuildServer(t, buildPlan([][2]string{{"system_requirement", buildR1}, {"user_requirement", buildR2}}, criteria))
	fixtures := map[string]string{}
	for _, name := range []string{"go_test.json", "xunit.trx", "vitest.xml", "exunit.xml"} {
		fixtures[name] = fixtureText(t, name)
	}
	root, env, base, head := buildWorkspace(t, server, map[string]string{"README.md": "# shop\n"}, matchFiles)
	buildWrite(t, root, "results/go.json", fixtures["go_test.json"])
	buildWrite(t, root, "results/xunit.trx", fixtures["xunit.trx"])
	buildWrite(t, root, "results/vitest.xml", fixtures["vitest.xml"])
	buildWrite(t, root, "results/exunit.xml", fixtures["exunit.xml"])
	buildWrite(t, root, "results/named.xml", namedJUnit)
	handoff := map[string]any{
		"epic_id": 427, "head": head, "base": base, "repository_key": "monorepo",
		"results": []any{
			map[string]any{"format": "go-test-json", "path": "results/go.json"},
			map[string]any{"format": "trx", "path": "results/xunit.trx"},
			map[string]any{"format": "junit", "path": "results/vitest.xml"},
			map[string]any{"format": "junit", "path": "results/exunit.xml"},
			map[string]any{"format": "junit", "path": "results/named.xml"},
		},
		"requirements": []any{
			map[string]any{"requirement_id": buildR1, "paths": []any{"cart/cart.go"}},
			map[string]any{"requirement_id": buildR2, "paths": []any{"src/cart.ts"}},
		},
		"tests": []any{map[string]any{"test_case_id": tTrxMult, "file": "tests/Calc.Tests/CalculatorTests.cs", "names": []any{"Calc.Tests.CalculatorTests.Multiplies"}}},
	}
	out, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff), "--json")
	if err != nil {
		t.Fatalf("build record: %v\n%s", err, out)
	}
	body := onlyPost(t, state)

	wantResults := []any{}
	for i, outcome := range []string{"pass", "pass", "error", "pass", "", "fail", "pass", "skip", "error", "pass", "pass", "", "pass"} {
		if outcome == "" {
			continue
		}
		wantResults = append(wantResults, map[string]any{"criterion_id": crit(i + 1), "tests": []any{map[string]any{"test_case_id": testCases[i], "result": outcome}}})
	}
	if !reflect.DeepEqual(body["results"], asJSON(t, wantResults)) {
		t.Fatalf("results sent = %v\nwant %v", body["results"], wantResults)
	}

	verifies := map[string]string{}
	for _, raw := range body["traces"].([]any) {
		trace := raw.(map[string]any)
		if trace["link_kind"] == "verifies" {
			verifies[trace["source_id"].(string)] = trace["path"].(string)
		}
	}
	wantVerifies := map[string]string{
		tGoAdd: "cart/cart_test.go", tGoTotals: "cart/cart_test.go", tGoExits: "cart/cart_test.go", tGoAfter: "after/after_test.go",
		tTrxAdds: "tests/Calc.Tests/CalculatorTests.cs", tTrxDiv: "tests/Calc.Tests/CalculatorTests.cs",
		tTrxTrait: "tests/Calc.Tests/CalculatorTests.cs", tTrxMult: "tests/Calc.Tests/CalculatorTests.cs",
		tVitest: "src/cart.test.ts", tExUnit: "test/shop/cart_test.exs", tNamed: "src/misc.test.ts",
	}
	if !reflect.DeepEqual(verifies, wantVerifies) {
		t.Fatalf("verifies traces = %v\nwant %v", verifies, wantVerifies)
	}

	var printed map[string]any
	if err := json.Unmarshal([]byte(out), &printed); err != nil {
		t.Fatalf("--json output is not JSON: %v\n%s", err, out)
	}
	local, _ := printed["local"].(map[string]any)
	notRecorded := fmt.Sprint(local["not_recorded"])
	for _, want := range []string{tToolsAdd, tDescribe, crit(5), crit(12)} {
		if !strings.Contains(notRecorded, want) {
			t.Errorf("not_recorded %s does not name %s", notRecorded, want)
		}
	}
	ignored := fmt.Sprint(local["ignored_results"])
	if !strings.Contains(ignored, tNotInPlan) || !strings.Contains(ignored, "partial a1000000-0000 prefix") {
		t.Errorf("ignored_results %s must name the not-in-plan TestCase and the prefix-only result", ignored)
	}
	if !strings.Contains(fmt.Sprint(local["packages_not_recorded"]), "example.com/shop/broken") ||
		!strings.Contains(fmt.Sprint(local["packages_failed_after_passing"]), "example.com/shop/after") {
		t.Errorf("package lists: %v", local)
	}
}

// TestCase 1404fef1-90f9-4ff7-af61-4710cdf7ab10
func TestBuildRecordRefusesAmbiguousDeclarationMatch(t *testing.T) {
	const tc = "a1000000-0000-4000-8000-0000000000aa"
	files := map[string]string{
		"src/x.test.ts": "// TestCase " + tc + "\nit(\"adds\", () => {});\n",
	}
	for name, junit := range map[string]string{
		"two names in one unit": `<testsuite><testcase classname="src/x.test.ts" name="a &gt; adds"/><testcase classname="src/x.test.ts" name="b &gt; adds"/></testsuite>`,
		"two units":             `<testsuite><testcase classname="src/x.test.ts" name="a &gt; adds"/><testcase classname="other" file="src/x.test.ts" name="a &gt; adds"/></testsuite>`,
	} {
		t.Run(name, func(t *testing.T) {
			state, server := newBuildServer(t, buildPlan(nil, [][2]string{{buildC1, tc}}))
			root, env, base, head := buildWorkspace(t, server, map[string]string{"README.md": "r\n"}, files)
			buildWrite(t, root, "results/x.xml", junit)
			handoff := map[string]any{"epic_id": 427, "head": head, "base": base,
				"results": []any{map[string]any{"format": "junit", "path": "results/x.xml"}}}
			_, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff))
			if err == nil || !strings.Contains(err.Error(), tc) || !strings.Contains(err.Error(), "a > adds") {
				t.Fatalf("ambiguous declaration: err = %v", err)
			}
			if name == "two names in one unit" && !strings.Contains(err.Error(), "b > adds") {
				t.Fatalf("refusal must name both results: %v", err)
			}
			if len(state.posts()) != 0 {
				t.Fatal("an ambiguous match sent the write")
			}
		})
	}
}

// TestCase 1404fef1-90f9-4ff7-af61-4710cdf7ab10
func TestBuildRecordSameNamedTestInAnotherModuleDoesNotMatch(t *testing.T) {
	state, root, env, handoff := minimalBuild(t)
	// The plan's TestCase is declared in a second module; the result comes
	// from example.com/shop/cart, so it must not match.
	buildWrite(t, root, "cart/cart_test.go", "package cart\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {}\n")
	buildWrite(t, root, "tools/go.mod", "module example.com/tools\n")
	buildWrite(t, root, "tools/cart/cart_test.go", minimalGoTest)
	buildGit(t, root, "add", "-A")
	buildGit(t, root, "commit", "-q", "-m", "move")
	handoff["head"] = buildGit(t, root, "rev-parse", "HEAD")
	out, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff))
	if err != nil {
		t.Fatalf("build record: %v\n%s", err, out)
	}
	body := onlyPost(t, state)
	if results, _ := body["results"].([]any); len(results) != 0 {
		t.Fatalf("a same-named test of another module was recorded: %v", body["results"])
	}
	if !strings.Contains(out, "not recorded") || !strings.Contains(out, buildT1) {
		t.Fatalf("output must list the obligation as not recorded:\n%s", out)
	}
}

// ---------------------------------------------------------------- C4

func traceKeys(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	keys := map[string]any{}
	for _, raw := range body["traces"].([]any) {
		trace := raw.(map[string]any)
		key := trace["path"].(string)
		switch {
		case trace["repository_key"] != nil:
			keys[key] = trace["repository_key"]
		case trace["repository_id"] != nil:
			keys[key] = trace["repository_id"]
		default:
			keys[key] = nil
		}
	}
	return keys
}

// TestCase f8be56d8-9a56-4a70-a8d9-622b7fdfc230
func TestBuildRecordTracesRequirementsAndTestFiles(t *testing.T) {
	state, root, env, handoff := minimalBuild(t)
	handoff["requirements"] = []any{map[string]any{"requirement_id": buildR1, "paths": []any{"cart/cart.go", "README.md"}}}
	out, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff))
	if err != nil {
		t.Fatalf("build record: %v\n%s", err, out)
	}
	body := onlyPost(t, state)
	head := handoff["head"].(string)
	want := []any{
		map[string]any{"source_type": "system_requirement", "source_id": buildR1, "target_type": "source_file", "path": "cart/cart.go", "revision": head, "link_kind": "implements"},
		map[string]any{"source_type": "system_requirement", "source_id": buildR1, "target_type": "source_file", "path": "README.md", "revision": head, "link_kind": "implements"},
		map[string]any{"source_type": "test_case", "source_id": buildT1, "target_type": "source_file", "path": "cart/cart_test.go", "revision": head, "link_kind": "verifies"},
	}
	if !reflect.DeepEqual(body["traces"], asJSON(t, want)) {
		t.Fatalf("traces = %v\nwant %v", body["traces"], want)
	}
	if !reflect.DeepEqual(body["assumed_defaults"], asJSON(t, handoff["assumed_defaults"])) || body["dry_run"] != false {
		t.Fatalf("assumed_defaults/dry_run = %v %v", body["assumed_defaults"], body["dry_run"])
	}
	if !strings.Contains(out, "README.md") || !strings.Contains(out, "not in git diff") {
		t.Fatalf("a path outside the diff must be named as a warning:\n%s", out)
	}
}

// TestCase f8be56d8-9a56-4a70-a8d9-622b7fdfc230
func TestBuildRecordRepositoryKeyOrder(t *testing.T) {
	t.Run("entry key, then top-level key", func(t *testing.T) {
		state, root, env, handoff := minimalBuild(t)
		env.Config.RepositoryID = 9
		buildWrite(t, root, "web/app.test.ts", "it(\"loads\", () => {});\n")
		buildGit(t, root, "add", "-A")
		buildGit(t, root, "commit", "-q", "-m", "web")
		handoff["head"] = buildGit(t, root, "rev-parse", "HEAD")
		const tc2 = "a1000000-0000-4000-8000-0000000000bb"
		state.plan = buildPlan([][2]string{{"system_requirement", buildR1}, {"user_requirement", buildR2}}, [][2]string{{buildC1, buildT1}, {crit(2), tc2}})
		buildWrite(t, root, "results/web.xml", `<testsuite><testcase classname="web" name="loads"/></testsuite>`)
		handoff["results"] = []any{map[string]any{"format": "go-test-json", "path": "results/go.json"}, map[string]any{"format": "junit", "path": "results/web.xml"}}
		handoff["repository_key"] = "mono"
		handoff["requirements"] = []any{
			map[string]any{"requirement_id": buildR1, "repository_key": "svc", "paths": []any{"cart/cart.go"}},
			map[string]any{"requirement_id": buildR2, "paths": []any{"README.md"}},
		}
		handoff["tests"] = []any{map[string]any{"test_case_id": tc2, "repository_key": "web", "file": "web/app.test.ts", "names": []any{"loads"}}}
		if _, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff)); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"cart/cart.go": "svc", "README.md": "mono", "cart/cart_test.go": "mono", "web/app.test.ts": "web"}
		if got := traceKeys(t, onlyPost(t, state)); !reflect.DeepEqual(got, want) {
			t.Fatalf("keys = %v, want %v", got, want)
		}
	})
	t.Run("binding repository_id without a key", func(t *testing.T) {
		state, root, env, handoff := minimalBuild(t)
		env.Config.RepositoryID = 9
		if _, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff)); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"cart/cart.go": json.Number("9"), "cart/cart_test.go": json.Number("9")}
		if got := traceKeys(t, onlyPost(t, state)); !reflect.DeepEqual(got, want) {
			t.Fatalf("keys = %v, want %v", got, want)
		}
	})
	t.Run("neither", func(t *testing.T) {
		state, root, env, handoff := minimalBuild(t)
		if _, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff)); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"cart/cart.go": nil, "cart/cart_test.go": nil}
		if got := traceKeys(t, onlyPost(t, state)); !reflect.DeepEqual(got, want) {
			t.Fatalf("keys = %v, want %v", got, want)
		}
	})
}

// TestCase f8be56d8-9a56-4a70-a8d9-622b7fdfc230
func TestBuildRecordTraceRefusals(t *testing.T) {
	t.Run("path not tracked at HEAD", func(t *testing.T) {
		state, root, env, handoff := minimalBuild(t)
		handoff["requirements"] = []any{map[string]any{"requirement_id": buildR1, "paths": []any{"cart/missing.go"}}}
		_, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff))
		if err == nil || !strings.Contains(err.Error(), "cart/missing.go") || len(state.posts()) != 0 {
			t.Fatalf("untracked path: %v", err)
		}
	})
	t.Run("UUID in several files without a declaration", func(t *testing.T) {
		state, root, env, handoff := minimalBuild(t)
		buildWrite(t, root, "cart/cart_test.go", "package cart\n\n// "+buildT1+"\nvar _ = 1\n")
		buildWrite(t, root, "docs/notes.md", "covers "+buildT1+"\n")
		buildGit(t, root, "add", "-A")
		buildGit(t, root, "commit", "-q", "-m", "spread")
		handoff["head"] = buildGit(t, root, "rev-parse", "HEAD")
		buildWrite(t, root, "results/named.xml", `<testsuite><testcase classname="x" name="adds `+buildT1+`"/></testsuite>`)
		handoff["results"] = []any{map[string]any{"format": "junit", "path": "results/named.xml"}}
		_, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff))
		if err == nil || !strings.Contains(err.Error(), "cart/cart_test.go") || !strings.Contains(err.Error(), "docs/notes.md") || len(state.posts()) != 0 {
			t.Fatalf("UUID in two files: %v", err)
		}
	})
	t.Run("matched TestCase without a test file", func(t *testing.T) {
		state, root, env, handoff := minimalBuild(t)
		buildWrite(t, root, "cart/cart_test.go", "package cart\n")
		buildGit(t, root, "add", "-A")
		buildGit(t, root, "commit", "-q", "-m", "drop")
		handoff["head"] = buildGit(t, root, "rev-parse", "HEAD")
		buildWrite(t, root, "results/named.xml", `<testsuite><testcase classname="x" name="adds `+buildT1+`"/></testsuite>`)
		handoff["results"] = []any{map[string]any{"format": "junit", "path": "results/named.xml"}}
		_, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff))
		if err == nil || !strings.Contains(err.Error(), buildT1) || !strings.Contains(err.Error(), "no test file") || len(state.posts()) != 0 {
			t.Fatalf("no test file: %v", err)
		}
	})
	t.Run("more than 50 criteria", func(t *testing.T) {
		state, root, env, handoff := minimalBuild(t)
		var criteria [][2]string
		for i := 1; i <= 51; i++ {
			criteria = append(criteria, [2]string{crit(i), buildT1})
		}
		state.plan = buildPlan([][2]string{{"system_requirement", buildR1}}, criteria)
		_, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff))
		if err == nil || !strings.Contains(err.Error(), "51") || len(state.posts()) != 0 {
			t.Fatalf("51 criteria: %v", err)
		}
	})
}

// ---------------------------------------------------------------- C5

// TestCase 638d531d-26b3-4fdd-99cb-863c5bd9ee32
func TestBuildRecordRefusesInconsistentHandoff(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, root string, handoff map[string]any)
		want  []string
	}{
		{"epic_id mismatch", func(t *testing.T, root string, h map[string]any) { h["epic_id"] = 428 }, []string{"epic_id", "428", "427"}},
		{"stale head", func(t *testing.T, root string, h map[string]any) {
			h["head"] = h["base"]
		}, []string{"head"}},
		{"unknown field", func(t *testing.T, root string, h map[string]any) { h["revision"] = "x" }, []string{`"revision"`}},
		{"unknown nested field", func(t *testing.T, root string, h map[string]any) {
			h["results"] = []any{map[string]any{"format": "go-test-json", "path": "results/go.json", "kind": "x"}}
		}, []string{`"kind"`}},
		{"missing result file", func(t *testing.T, root string, h map[string]any) {
			h["results"] = []any{map[string]any{"format": "junit", "path": "results/absent.xml"}}
		}, []string{"results/absent.xml"}},
		{"dirty traced file", func(t *testing.T, root string, h map[string]any) {
			buildWrite(t, root, "cart/cart.go", "package cart\n\n// edited\n")
		}, []string{"cart/cart.go", "uncommitted"}},
		{"dirty test file", func(t *testing.T, root string, h map[string]any) {
			buildWrite(t, root, "cart/cart_test.go", minimalGoTest+"\n// edited\n")
		}, []string{"cart/cart_test.go", "uncommitted"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, root, env, handoff := minimalBuild(t)
			tc.setup(t, root, handoff)
			if tc.name == "stale head" {
				tc.want = append(tc.want, handoff["base"].(string))
			}
			_, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff))
			if err == nil {
				t.Fatal("handoff was not refused")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal %q does not name %q", err, want)
				}
			}
			if len(state.posts()) != 0 {
				t.Fatal("a refused handoff sent the write")
			}
		})
	}
}

// ---------------------------------------------------------------- C6

const buildReply = `{"data":{"dry_run":true,
 "traces":[{"id":"t1","source_type":"system_requirement","source_id":"` + buildR1 + `","path":"cart/cart.go","link_kind":"implements","status":"written"},
           {"id":"t2","source_type":"test_case","source_id":"` + buildT1 + `","path":"cart/cart_test.go","link_kind":"verifies","status":"kept"}],
 "results":[{"criterion_id":"` + buildC1 + `","test_case_id":"` + buildT1 + `","result":"pass","status":"written"}],
 "assumed_defaults":[{"id":"b1","display_ref":"BACKLOG-7","title":"Cart limit","status":"written"}]}}`

// TestCase 7e1c42a1-fd3c-4e74-8516-30d27c9c8993
func TestBuildRecordDryRunJSONAndAgent(t *testing.T) {
	t.Run("dry run prints the answer", func(t *testing.T) {
		state, root, env, handoff := minimalBuild(t)
		state.reply = buildReply
		out, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff), "--dry-run")
		if err != nil {
			t.Fatal(err)
		}
		body := onlyPost(t, state)
		if body["dry_run"] != true || body["actor"] != nil {
			t.Fatalf("dry run body: %v", body)
		}
		for _, want := range []string{"Dry run", "written", "kept", "cart/cart.go", "BACKLOG-7", buildC1} {
			if !strings.Contains(out, want) {
				t.Errorf("dry-run output does not show %q:\n%s", want, out)
			}
		}
	})
	t.Run("json and agent", func(t *testing.T) {
		state, root, env, handoff := minimalBuild(t)
		state.reply = buildReply
		out, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff), "--json", "--agent", "rdd-build")
		if err != nil {
			t.Fatal(err)
		}
		body := onlyPost(t, state)
		if !reflect.DeepEqual(body["actor"], map[string]any{"kind": "agent", "agent_slug": "rdd-build"}) || body["dry_run"] != false {
			t.Fatalf("actor/dry_run: %v", body)
		}
		var printed map[string]any
		if err := json.Unmarshal([]byte(out), &printed); err != nil {
			t.Fatalf("--json output: %v\n%s", err, out)
		}
		if traces, _ := printed["traces"].([]any); len(traces) != 2 || printed["local"] == nil {
			t.Fatalf("--json must print the answer and the local lists: %s", out)
		}
	})
	t.Run("server refusal", func(t *testing.T) {
		state, root, env, handoff := minimalBuild(t)
		state.status = 422
		state.reply = `{"error":{"code":"native_test_chain_required","message":"TestCase has no test file trace."}}`
		_, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff))
		if err == nil || !strings.Contains(err.Error(), "native_test_chain_required") || !strings.Contains(err.Error(), "TestCase has no test file trace.") {
			t.Fatalf("refusal: %v", err)
		}
	})
	t.Run("timeout says re-running is safe", func(t *testing.T) {
		state, root, env, handoff := minimalBuild(t)
		state.delay = 300 * time.Millisecond
		env.callTimeout = 50 * time.Millisecond
		_, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff))
		if err == nil || !strings.Contains(err.Error(), "may have committed") || !strings.Contains(err.Error(), "same handoff") {
			t.Fatalf("timeout: %v", err)
		}
	})
}
