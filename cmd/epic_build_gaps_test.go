package cmd

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Epic 428: build-record gaps.

// displayNameTests is the source of testdata/test_results/xunit_v3_displayname.trx,
// a real run: xunit.v3 3.0.0, xunit.runner.visualstudio 3.1.3,
// Microsoft.NET.Test.Sdk 17.14.1, .NET SDK 10.0.107, VSTest 18.0.2,
// `dotnet test --logger trx`. Machine name and paths in the file were
// replaced with neutral values; nothing else was edited.
const (
	tDisplayFact   = "11111111-1111-4111-8111-111111111111"
	tDisplayTheory = "22222222-2222-4222-8222-222222222222"
	tDisplayRefund = "33333333-3333-4333-8333-333333333333"
	tPlainFact     = "44444444-4444-4444-8444-444444444444"
	tPlainTheory   = "55555555-5555-4555-8555-555555555555"

	displayNameTests = `using Xunit;

namespace Sample.Tests;

public class InvoiceTests
{
    // ` + tDisplayFact + `
    [Fact(DisplayName = "Pays the invoice")]
    public void PaysTheInvoice()
    {
        Assert.True(true);
    }

    // ` + tDisplayTheory + `
    [Theory(DisplayName = "Adds two amounts")]
    [InlineData(1, 2, 3)]
    [InlineData(2, 3, 5)]
    public void AddsAmounts(int a, int b, int sum)
    {
        Assert.Equal(sum, a + b);
    }

    // ` + tDisplayRefund + `
    [Fact(DisplayName = "Refund after PaysTheInvoice")]
    public void RefundsTheInvoice()
    {
        Assert.True(true);
    }

    // ` + tPlainFact + `
    [Fact]
    public void Adds()
    {
        Assert.Equal(3, 1 + 2);
    }

    // ` + tPlainTheory + `
    [Theory]
    [InlineData(1, 2)]
    [InlineData(2, 3)]
    public void Increments(int a, int b)
    {
        Assert.Equal(b, a + 1);
    }
}
`
)

func runDisplayNameBuild(t *testing.T, trx string) (map[string]any, string) {
	t.Helper()
	testCases := []string{tDisplayFact, tDisplayTheory, tDisplayRefund, tPlainFact, tPlainTheory}
	var criteria [][2]string
	for i, tc := range testCases {
		criteria = append(criteria, [2]string{crit(i + 1), tc})
	}
	state, server := newBuildServer(t, buildPlan(nil, criteria))
	root, env, base, head := buildWorkspace(t, server, map[string]string{"README.md": "r\n"},
		map[string]string{"tests/Sample.Tests/InvoiceTests.cs": displayNameTests})
	buildWrite(t, root, "results/v3.trx", trx)
	handoff := map[string]any{"epic_id": 427, "head": head, "base": base,
		"results": []any{map[string]any{"format": "trx", "path": "results/v3.trx"}}}
	out, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff))
	if err != nil {
		t.Fatalf("build record: %v\n%s", err, out)
	}
	return onlyPost(t, state), out
}

func resultsByCriterion(t *testing.T, body map[string]any) map[string]string {
	t.Helper()
	got := map[string]string{}
	results, _ := body["results"].([]any)
	for _, raw := range results {
		item := raw.(map[string]any)
		tests := item["tests"].([]any)
		if len(tests) != 1 {
			t.Fatalf("criterion %v has %d tests", item["criterion_id"], len(tests))
		}
		got[item["criterion_id"].(string)] = tests[0].(map[string]any)["result"].(string)
	}
	return got
}

// TestCase cefc1da3-8111-4b03-988b-196235f9713b
// TestCase 1404fef1-90f9-4ff7-af61-4710cdf7ab10 (SR-4875 criterion 2: the TRX method name)
func TestBuildRecordMatchesTRXResultsByMethodName(t *testing.T) {
	real := fixtureText(t, "xunit_v3_displayname.trx")

	t.Run("a real xUnit v3 run with DisplayNames", func(t *testing.T) {
		body, _ := runDisplayNameBuild(t, real)
		want := map[string]string{crit(1): "pass", crit(2): "pass", crit(3): "pass", crit(4): "pass", crit(5): "pass"}
		if got := resultsByCriterion(t, body); !reflect.DeepEqual(got, want) {
			t.Fatalf("results sent = %v\nwant %v", got, want)
		}
	})

	t.Run("a DisplayName ending in another method's name matches only its own method", func(t *testing.T) {
		// "Refund after PaysTheInvoice" is the DisplayName of RefundsTheInvoice.
		// Failing it must fail RefundsTheInvoice's criterion and leave
		// PaysTheInvoice's passing.
		const refund = `testName="Refund after PaysTheInvoice"`
		at := strings.Index(real, refund)
		if at < 0 {
			t.Fatal("the fixture has no Refund result")
		}
		start := strings.LastIndex(real[:at], "<UnitTestResult")
		end := at + strings.Index(real[at:], ">")
		element := real[start:end]
		if !strings.Contains(element, `outcome="Passed"`) {
			t.Fatalf("unexpected Refund element: %s", element)
		}
		failed := real[:start] + strings.Replace(element, `outcome="Passed"`, `outcome="Failed"`, 1) + real[end:]
		body, _ := runDisplayNameBuild(t, failed)
		want := map[string]string{crit(1): "pass", crit(2): "pass", crit(3): "fail", crit(4): "pass", crit(5): "pass"}
		if got := resultsByCriterion(t, body); !reflect.DeepEqual(got, want) {
			t.Fatalf("results sent = %v\nwant %v", got, want)
		}
	})

	t.Run("a qualified or parameter-typed method name is normalized like the testName", func(t *testing.T) {
		const trx = `<TestRun xmlns="http://microsoft.com/schemas/VisualStudio/TeamTest/2010">
  <Results>
    <UnitTestResult testId="t1" testName="Shown name one" outcome="Passed" />
    <UnitTestResult testId="t2" testName="Shown name two" outcome="Passed" />
    <UnitTestResult testId="t3" testName="Ns.Calc.Plain" outcome="Passed" />
  </Results>
  <TestDefinitions>
    <UnitTest id="t1" name="Shown name one"><TestMethod className="Ns.Calc" name="Ns.Calc.Adds" /></UnitTest>
    <UnitTest id="t2" name="Shown name two"><TestMethod className="Ns.Calc" name="Divides(System.Int32, System.Int32)" /></UnitTest>
    <UnitTest id="t3" name="Ns.Calc.Plain"><TestMethod className="Ns.Calc" /></UnitTest>
  </TestDefinitions>
</TestRun>`
		dir := t.TempDir()
		buildWrite(t, dir, "r.trx", trx)
		parsed, err := parseTestResults("trx", dir+"/r.trx", "")
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, r := range parsed.Results {
			got[r.Name] = r.Method
		}
		want := map[string]string{"Shown name one": "Adds", "Shown name two": "Divides", "Ns.Calc.Plain": ""}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("method names = %v\nwant %v", got, want)
		}
	})
}

// linkedWorktree binds a temporary main checkout to server, commits files
// there and adds a linked git worktree outside it with no .modernpath of its
// own. It changes into the worktree and returns the main checkout's and the
// worktree's roots.
func linkedWorktree(t *testing.T, server *httptest.Server, files map[string]string) (string, string) {
	t.Helper()
	cobraWorkspace(t, server, 4)
	main, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	buildGit(t, main, "init", "-q")
	buildWrite(t, main, ".gitignore", ".modernpath/\nresults/\nhandoff.json\n")
	for rel, body := range files {
		buildWrite(t, main, rel, body)
	}
	buildGit(t, main, "add", "-A")
	buildGit(t, main, "commit", "-q", "-m", "base")
	worktree := filepath.Join(t.TempDir(), "wt")
	buildGit(t, main, "worktree", "add", "-q", "-b", "build", worktree)
	t.Chdir(worktree)
	return main, worktree
}

func samePath(t *testing.T, a, b string) bool {
	t.Helper()
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		t.Fatal(err)
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		t.Fatal(err)
	}
	return ra == rb
}

// TestCase 47ab81f4-5445-440c-a667-8429b56130c4
func TestStoreCommandsUseTheMainCheckoutBindingInALinkedWorktree(t *testing.T) {
	state, server := newBuildServer(t, buildPlan([][2]string{{"system_requirement", buildR1}}, [][2]string{{buildC1, buildT1}}))
	main, worktree := linkedWorktree(t, server, map[string]string{"README.md": "# shop\n"})

	env, err := cliBindingLoad()
	if err != nil {
		t.Fatalf("a store command in a linked worktree: %v", err)
	}
	if env.SystemID != 4 || env.APIURL != server.URL || !samePath(t, env.Root, main) {
		t.Fatalf("binding = system %d at %s, root %s; want system 4 at %s, root %s", env.SystemID, env.APIURL, env.Root, server.URL, main)
	}

	// docs sync, search, ask, read-doc and read-file keep their gate.
	if _, err := apiClientLoad(false); err == nil || !strings.Contains(err.Error(), "no system is bound here") {
		t.Fatalf("the api-client gate in a linked worktree: err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(worktree, ".modernpath")); !os.IsNotExist(err) {
		t.Fatalf("a .modernpath folder exists in the worktree: %v", err)
	}

	// epics build record compares the handoff's head with the worktree's HEAD.
	mainHead := buildGit(t, main, "rev-parse", "HEAD")
	buildWrite(t, worktree, "go.mod", "module example.com/shop\n\ngo 1.24\n")
	buildWrite(t, worktree, "cart/cart.go", "package cart\n")
	buildWrite(t, worktree, "cart/cart_test.go", minimalGoTest)
	buildGit(t, worktree, "add", "-A")
	buildGit(t, worktree, "commit", "-q", "-m", "build")
	head := buildGit(t, worktree, "rev-parse", "HEAD")
	buildWrite(t, worktree, "results/go.json", minimalGoResults)
	credentialed, err := cliCredentialLoad()
	if err != nil {
		t.Fatalf("credential load in a linked worktree: %v", err)
	}
	handoff := map[string]any{
		"epic_id": 427, "head": mainHead, "base": mainHead,
		"results":      []any{map[string]any{"format": "go-test-json", "path": "results/go.json"}},
		"requirements": []any{map[string]any{"requirement_id": buildR1, "paths": []any{"cart/cart.go"}}},
	}
	if _, err := runBuildRecord(t, credentialed, "--file", writeHandoff(t, worktree, handoff)); err == nil || !strings.Contains(err.Error(), "is not the checkout HEAD "+head) {
		t.Fatalf("a handoff with the main checkout's head: err = %v", err)
	}
	handoff["head"] = head
	if out, err := runBuildRecord(t, credentialed, "--file", writeHandoff(t, worktree, handoff)); err != nil {
		t.Fatalf("build record in the worktree: %v\n%s", err, out)
	}
	body := onlyPost(t, state)
	if got := resultsByCriterion(t, body); got[buildC1] != "pass" {
		t.Fatalf("results sent from the worktree = %v", got)
	}

	// Neither a .modernpath folder nor a linked worktree's main binding.
	t.Chdir(t.TempDir())
	if _, err := cliBindingLoad(); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("outside any binding: err = %v", err)
	}
}

const (
	gapT1 = "a2000000-0000-4000-8000-000000000001"
	gapT2 = "a2000000-0000-4000-8000-000000000002"
	gapT3 = "a2000000-0000-4000-8000-000000000003"
	gapR1 = "b2000000-0000-4000-8000-000000000001" // regression TestCases
	gapR2 = "b2000000-0000-4000-8000-000000000002"
	gapR3 = "b2000000-0000-4000-8000-000000000003"

	gapGoTests = `package cart

import "testing"

// TestCase ` + gapT1 + `
func TestAdd(t *testing.T) {}

// TestCase ` + gapT2 + `
func TestTotals(t *testing.T) {}
`
	gapRegressionTests = `package cart

import "testing"

// TestCase ` + gapR1 + `
func TestOld(t *testing.T) {}

// TestCase ` + gapR2 + `
func TestNeverRun(t *testing.T) {}
`
	gapGoResults = `{"Action":"pass","Package":"example.com/shop/cart","Test":"TestAdd"}
{"Action":"pass","Package":"example.com/shop/cart","Test":"TestTotals"}
{"Action":"fail","Package":"example.com/shop/cart","Test":"TestOld"}
{"Action":"fail","Package":"example.com/shop/cart"}
`
)

// regressionCriterion is a criterion of a published requirement outside the plan.
func regressionCriterion(n int) string { return fmt.Sprintf("d0000000-0000-4000-8000-%012d", n) }

// gapsBuild is a workspace whose plan has one requirement with criteria 1..3
// (1 and 2 verified by gapT1, 3 by gapT2) and whose head adds a Go package
// with those tests, two regression tests and results for them. lib/old.go is
// in the base commit, so the build did not change it.
func gapsBuild(t *testing.T) (*buildServer, string, *cliEnv, map[string]any) {
	t.Helper()
	plan := buildPlan([][2]string{{"system_requirement", buildR1}}, [][2]string{{crit(1), gapT1}, {crit(2), gapT1}, {crit(3), gapT2}})
	plan["requirements"].([]any)[0].(map[string]any)["criteria"] = []any{
		map[string]any{"id": crit(1)}, map[string]any{"id": crit(2)}, map[string]any{"id": crit(3)}, map[string]any{"id": crit(9)}}
	state, server := newBuildServer(t, plan)
	root, env, base, head := buildWorkspace(t, server,
		map[string]string{"README.md": "# shop\n", "lib/old.go": "package lib\n"},
		map[string]string{"go.mod": "module example.com/shop\n\ngo 1.24\n", "cart/cart.go": "package cart\n",
			"cart/cart_test.go": gapGoTests, "cart/regress_test.go": gapRegressionTests})
	buildWrite(t, root, "results/go.json", gapGoResults)
	handoff := map[string]any{
		"epic_id": 427, "head": head, "base": base,
		"results":      []any{map[string]any{"format": "go-test-json", "path": "results/go.json"}},
		"requirements": []any{map[string]any{"requirement_id": buildR1, "paths": []any{"cart/cart.go"}}},
	}
	return state, root, env, handoff
}

func tracePaths(body map[string]any, linkKind string) map[string][]string {
	out := map[string][]string{}
	traces, _ := body["traces"].([]any)
	for _, raw := range traces {
		trace := raw.(map[string]any)
		if trace["link_kind"] == linkKind {
			source := trace["source_id"].(string)
			out[source] = append(out[source], trace["path"].(string))
		}
	}
	return out
}

func gapsLocal(t *testing.T, out string) map[string]any {
	t.Helper()
	var printed map[string]any
	if err := json.Unmarshal([]byte(out), &printed); err != nil {
		t.Fatalf("--json output is not JSON: %v\n%s", err, out)
	}
	local, _ := printed["local"].(map[string]any)
	return local
}

// gapsRefused runs the handoff and wants a refusal that names every fragment
// and sends nothing.
func gapsRefused(t *testing.T, state *buildServer, root string, env *cliEnv, handoff map[string]any, fragments ...string) {
	t.Helper()
	out, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff))
	if err == nil {
		t.Fatalf("want a refusal naming %v, got:\n%s", fragments, out)
	}
	for _, fragment := range append(fragments, "nothing was sent") {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("refusal %q does not name %q", err.Error(), fragment)
		}
	}
	if len(state.posts()) != 0 {
		t.Fatal("a refused handoff sent the write")
	}
}

// TestCase 80b94cd9-e851-4743-988d-0930b144eeb1
// TestCase 1404fef1-90f9-4ff7-af61-4710cdf7ab10 (SR-4875 criterion 2: an excluded obligation is listed as excluded)
// TestCase f8be56d8-9a56-4a70-a8d9-622b7fdfc230 (SR-4875 criterion 3: no test file needed when every obligation is excluded)
func TestBuildRecordSendsExcludedCriteria(t *testing.T) {
	t.Run("an excluded obligation is not sent and is listed as excluded", func(t *testing.T) {
		state, root, env, handoff := gapsBuild(t)
		handoff["excluded"] = []any{
			map[string]any{"criterion_id": crit(2), "reason": "The refund path is half built."},
			map[string]any{"criterion_id": strings.ToUpper(crit(3)), "reason": "No parser yet."},
		}
		out, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff), "--json")
		if err != nil {
			t.Fatalf("build record: %v\n%s", err, out)
		}
		body := onlyPost(t, state)
		if got, want := resultsByCriterion(t, body), map[string]string{crit(1): "pass"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("results sent = %v, want %v", got, want)
		}
		wantExcluded := asJSON(t, []any{
			map[string]any{"criterion_id": crit(2), "test_case_id": gapT1, "reason": "The refund path is half built."},
			map[string]any{"criterion_id": crit(3), "test_case_id": gapT2, "reason": "No parser yet."},
		})
		if !reflect.DeepEqual(body["excluded"], wantExcluded) {
			t.Fatalf("excluded sent = %v\nwant %v", body["excluded"], wantExcluded)
		}
		// gapT2's only obligation is excluded; it has a result and a test
		// file, so its verifies trace is still sent.
		wantVerifies := map[string][]string{gapT1: {"cart/cart_test.go"}, gapT2: {"cart/cart_test.go"}}
		if got := tracePaths(body, "verifies"); !reflect.DeepEqual(got, wantVerifies) {
			t.Fatalf("verifies traces = %v, want %v", got, wantVerifies)
		}
		local := gapsLocal(t, out)
		if listed := fmt.Sprint(local["excluded"]); !strings.Contains(listed, crit(2)) || !strings.Contains(listed, crit(3)) {
			t.Fatalf("local excluded = %v", local["excluded"])
		}
		if notRecorded, _ := local["not_recorded"].([]any); len(notRecorded) != 0 {
			t.Fatalf("an excluded obligation is listed as not recorded: %v", notRecorded)
		}
	})

	t.Run("the text output lists the obligation as excluded", func(t *testing.T) {
		_, root, env, handoff := gapsBuild(t)
		handoff["excluded"] = []any{map[string]any{"criterion_id": crit(3), "reason": "No parser yet."}}
		out, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff))
		if err != nil {
			t.Fatalf("build record: %v\n%s", err, out)
		}
		if !strings.Contains(out, "Obligations excluded:") || !strings.Contains(out, crit(3)) || !strings.Contains(out, "No parser yet.") || strings.Contains(out, "not recorded") {
			t.Fatalf("text output:\n%s", out)
		}
	})

	t.Run("no excluded section without excluded entries", func(t *testing.T) {
		state, root, env, handoff := gapsBuild(t)
		handoff["excluded"] = []any{}
		if out, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff)); err != nil {
			t.Fatalf("build record: %v\n%s", err, out)
		}
		if _, present := onlyPost(t, state)["excluded"]; present {
			t.Fatal("the request carries an excluded section without excluded entries")
		}
	})

	t.Run("a TestCase whose obligations are all excluded needs no test file", func(t *testing.T) {
		// gapT3's result carries its id as a property and no file at HEAD
		// carries its UUID.
		const junit = `<testsuite><testcase classname="x" name="parses"><properties><property name="test_case_id" value="` + gapT3 + `"/></properties></testcase></testsuite>`
		for _, excluded := range []bool{false, true} {
			plan := buildPlan([][2]string{{"system_requirement", buildR1}}, [][2]string{{crit(1), gapT3}})
			state, server := newBuildServer(t, plan)
			root, env, base, head := buildWorkspace(t, server, map[string]string{"README.md": "r\n"}, map[string]string{"src/a.ts": "export {}\n"})
			buildWrite(t, root, "results/x.xml", junit)
			handoff := map[string]any{"epic_id": 427, "head": head, "base": base,
				"results": []any{map[string]any{"format": "junit", "path": "results/x.xml"}}}
			if !excluded {
				gapsRefused(t, state, root, env, handoff, gapT3, "has a result but no test file")
				continue
			}
			handoff["excluded"] = []any{map[string]any{"criterion_id": crit(1), "reason": "Half built."}}
			out, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff))
			if err != nil {
				t.Fatalf("build record: %v\n%s", err, out)
			}
			body := onlyPost(t, state)
			if traces, _ := body["traces"].([]any); len(traces) != 0 {
				t.Fatalf("traces sent = %v", traces)
			}
			if !strings.Contains(out, gapT3) || !strings.Contains(out, "no test file") {
				t.Fatalf("the output must name the untraced TestCase:\n%s", out)
			}
		}
	})

	t.Run("a UUID in several files is still refused when every obligation is excluded", func(t *testing.T) {
		const junit = `<testsuite><testcase classname="x" name="parses"><properties><property name="test_case_id" value="` + gapT3 + `"/></properties></testcase></testsuite>`
		state, server := newBuildServer(t, buildPlan([][2]string{{"system_requirement", buildR1}}, [][2]string{{crit(1), gapT3}}))
		root, env, base, head := buildWorkspace(t, server, map[string]string{"README.md": "r\n"},
			map[string]string{"notes/a.md": "TestCase " + gapT3 + "\n", "notes/b.md": "TestCase " + gapT3 + "\n"})
		buildWrite(t, root, "results/x.xml", junit)
		handoff := map[string]any{"epic_id": 427, "head": head, "base": base,
			"results":  []any{map[string]any{"format": "junit", "path": "results/x.xml"}},
			"excluded": []any{map[string]any{"criterion_id": crit(1), "reason": "Half built."}}}
		gapsRefused(t, state, root, env, handoff, gapT3, "more than one file", "notes/a.md", "notes/b.md")
	})

	t.Run("refusals", func(t *testing.T) {
		many := []any{}
		for i := 0; i < 51; i++ {
			many = append(many, map[string]any{"criterion_id": crit(100 + i), "reason": "r"})
		}
		for name, c := range map[string]struct {
			excluded  []any
			fragments []string
		}{
			"a criterion that is not an obligation": {[]any{map[string]any{"criterion_id": crit(9), "reason": "r"}}, []string{"excluded entry 1", crit(9), "not a verification obligation"}},
			"a criterion listed twice": {[]any{map[string]any{"criterion_id": crit(2), "reason": "r"}, map[string]any{"criterion_id": crit(2), "reason": "again"}},
				[]string{"excluded entry 2", crit(2), "twice"}},
			"an empty reason":      {[]any{map[string]any{"criterion_id": crit(2), "reason": "  "}}, []string{"excluded entry 1", crit(2), "reason"}},
			"more than 50 entries": {many, []string{"51 excluded entries", "50"}},
		} {
			t.Run(name, func(t *testing.T) {
				state, root, env, handoff := gapsBuild(t)
				handoff["excluded"] = c.excluded
				gapsRefused(t, state, root, env, handoff, c.fragments...)
			})
		}
	})
}

// TestCase 7aa4e546-a627-491e-a639-ba194238e646
// TestCase 1404fef1-90f9-4ff7-af61-4710cdf7ab10 (SR-4875 criterion 2: a regression entry's TestCase is matched)
// TestCase f8be56d8-9a56-4a70-a8d9-622b7fdfc230 (SR-4875 criterion 3: a regression TestCase gets no verifies trace)
func TestBuildRecordSendsRegressionResults(t *testing.T) {
	t.Run("a regression TestCase's outcome is sent apart, without a trace", func(t *testing.T) {
		state, root, env, handoff := gapsBuild(t)
		handoff["regression"] = []any{
			map[string]any{"criterion_id": regressionCriterion(1), "test_case_id": strings.ToUpper(gapR1)},
			map[string]any{"criterion_id": regressionCriterion(2), "test_case_id": gapR2},
		}
		out, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff), "--json")
		if err != nil {
			t.Fatalf("build record: %v\n%s", err, out)
		}
		body := onlyPost(t, state)
		want := asJSON(t, []any{map[string]any{"criterion_id": regressionCriterion(1), "tests": []any{map[string]any{"test_case_id": gapR1, "result": "fail"}}}})
		if !reflect.DeepEqual(body["regression_results"], want) {
			t.Fatalf("regression_results sent = %v\nwant %v", body["regression_results"], want)
		}
		if got, want := resultsByCriterion(t, body), map[string]string{crit(1): "pass", crit(2): "pass", crit(3): "pass"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("results sent = %v, want %v", got, want)
		}
		verifies := tracePaths(body, "verifies")
		if _, traced := verifies[gapR1]; traced || len(verifies) != 2 {
			t.Fatalf("verifies traces = %v; a regression TestCase gets none", verifies)
		}
		local := gapsLocal(t, out)
		if notRecorded := fmt.Sprint(local["not_recorded"]); !strings.Contains(notRecorded, regressionCriterion(2)) || !strings.Contains(notRecorded, gapR2) {
			t.Fatalf("not_recorded = %v must name the regression entry without a result", local["not_recorded"])
		}
		if ignored := fmt.Sprint(local["ignored_results"]); strings.Contains(ignored, gapR1) {
			t.Fatalf("a regression result is listed as ignored: %v", ignored)
		}
	})

	t.Run("without regression, a result outside the plan is still not sent", func(t *testing.T) {
		state, root, env, handoff := gapsBuild(t)
		out, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff), "--json")
		if err != nil {
			t.Fatalf("build record: %v\n%s", err, out)
		}
		if _, present := onlyPost(t, state)["regression_results"]; present {
			t.Fatal("the request carries regression_results without regression entries")
		}
	})

	t.Run("a regression TestCase with no file is named, not checked", func(t *testing.T) {
		state, root, env, handoff := gapsBuild(t)
		buildWrite(t, root, "results/named.xml", `<testsuite><testcase classname="x" name="old flow `+gapR3+`"/></testsuite>`)
		handoff["results"] = append(handoff["results"].([]any), map[string]any{"format": "junit", "path": "results/named.xml"})
		handoff["regression"] = []any{map[string]any{"criterion_id": regressionCriterion(3), "test_case_id": gapR3}}
		out, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff))
		if err != nil {
			t.Fatalf("build record: %v\n%s", err, out)
		}
		want := asJSON(t, []any{map[string]any{"criterion_id": regressionCriterion(3), "tests": []any{map[string]any{"test_case_id": gapR3, "result": "pass"}}}})
		if body := onlyPost(t, state); !reflect.DeepEqual(body["regression_results"], want) {
			t.Fatalf("regression_results sent = %v", body["regression_results"])
		}
		if !strings.Contains(out, gapR3) || !strings.Contains(out, "not checked for uncommitted changes") {
			t.Fatalf("the output must name the unchecked regression TestCase:\n%s", out)
		}
	})

	t.Run("a regression test file with uncommitted changes is refused", func(t *testing.T) {
		state, root, env, handoff := gapsBuild(t)
		handoff["regression"] = []any{map[string]any{"criterion_id": regressionCriterion(1), "test_case_id": gapR1}}
		buildWrite(t, root, "cart/regress_test.go", gapRegressionTests+"\n// edited locally\n")
		gapsRefused(t, state, root, env, handoff, "cart/regress_test.go", "uncommitted changes")
	})

	t.Run("refusals", func(t *testing.T) {
		many := []any{}
		for i := 0; i < 51; i++ {
			many = append(many, map[string]any{"criterion_id": regressionCriterion(100 + i), "test_case_id": gapR1})
		}
		for name, c := range map[string]struct {
			regression []any
			fragments  []string
		}{
			"a criterion of a plan requirement": {[]any{map[string]any{"criterion_id": crit(9), "test_case_id": gapR1}}, []string{"regression entry 1", crit(9), "requirement of the plan"}},
			"the same criterion and TestCase twice": {[]any{map[string]any{"criterion_id": regressionCriterion(1), "test_case_id": gapR1}, map[string]any{"criterion_id": regressionCriterion(1), "test_case_id": gapR1}},
				[]string{"regression entry 2", regressionCriterion(1), "twice"}},
			"an entry without a TestCase":      {[]any{map[string]any{"criterion_id": regressionCriterion(1), "test_case_id": ""}}, []string{"regression entry 1", "test_case_id"}},
			"more than 50 regression criteria": {many, []string{"51 regression criteria", "50"}},
		} {
			t.Run(name, func(t *testing.T) {
				state, root, env, handoff := gapsBuild(t)
				handoff["regression"] = c.regression
				gapsRefused(t, state, root, env, handoff, c.fragments...)
			})
		}
	})
}

// TestCase 4d8809df-3205-48d2-9da2-b96b8f7a8fcb
// TestCase f8be56d8-9a56-4a70-a8d9-622b7fdfc230 (SR-4875 criterion 3: only a path in paths is named outside the diff)
func TestBuildRecordTracesUnchangedPaths(t *testing.T) {
	withUnchanged := func(handoff map[string]any, paths, unchanged []any) {
		handoff["requirements"] = []any{map[string]any{"requirement_id": buildR1, "paths": paths, "unchanged_paths": unchanged}}
	}

	t.Run("an unchanged path is traced and not named among the paths outside the diff", func(t *testing.T) {
		state, root, env, handoff := gapsBuild(t)
		withUnchanged(handoff, []any{"cart/cart.go", "README.md"}, []any{"lib/old.go"})
		out, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff), "--json")
		if err != nil {
			t.Fatalf("build record: %v\n%s", err, out)
		}
		implements := tracePaths(onlyPost(t, state), "implements")
		if got, want := implements[buildR1], []string{"cart/cart.go", "README.md", "lib/old.go"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("implements traces = %v, want %v", got, want)
		}
		// README.md is in paths and outside the diff: it keeps its warning.
		if got, want := fmt.Sprint(gapsLocal(t, out)["paths_not_in_diff"]), "[README.md]"; got != want {
			t.Fatalf("paths_not_in_diff = %s, want %s", got, want)
		}
	})

	t.Run("an entry with only unchanged paths", func(t *testing.T) {
		state, root, env, handoff := gapsBuild(t)
		handoff["requirements"] = []any{map[string]any{"requirement_id": buildR1, "unchanged_paths": []any{"lib/old.go"}}}
		if out, err := runBuildRecord(t, env, "--file", writeHandoff(t, root, handoff)); err != nil || strings.Contains(out, "Warning") {
			t.Fatalf("build record: %v\n%s", err, out)
		}
		if got := tracePaths(onlyPost(t, state), "implements")[buildR1]; !reflect.DeepEqual(got, []string{"lib/old.go"}) {
			t.Fatalf("implements traces = %v", got)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		for name, c := range map[string]struct {
			paths, unchanged []any
			dirty            string
			fragments        []string
		}{
			"an unchanged path the build changed": {[]any{}, []any{"cart/cart.go"}, "", []string{"unchanged path cart/cart.go", "git diff"}},
			"a path listed in both":               {[]any{"lib/old.go"}, []any{"lib/old.go"}, "", []string{"unchanged path lib/old.go", "paths"}},
			"an untracked unchanged path":         {[]any{}, []any{"lib/missing.go"}, "", []string{"lib/missing.go", "not a tracked file"}},
			"an unchanged path with local edits":  {[]any{}, []any{"lib/old.go"}, "lib/old.go", []string{"lib/old.go", "uncommitted changes"}},
		} {
			t.Run(name, func(t *testing.T) {
				state, root, env, handoff := gapsBuild(t)
				withUnchanged(handoff, c.paths, c.unchanged)
				if c.dirty != "" {
					buildWrite(t, root, c.dirty, "package lib\n\n// edited\n")
				}
				gapsRefused(t, state, root, env, handoff, c.fragments...)
			})
		}
	})
}
