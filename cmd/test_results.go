package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// Epic 427: epics build record reads TRX, JUnit XML and go test -json files.

// testResult is one test outcome a result file reports.
type testResult struct {
	Format, File, Root string
	// Name is the test name as the result file reports it; Normalized is the
	// name the source rule compares (see normalizedResultName).
	Name, Normalized string
	// Method is a TRX result's TestMethod name, normalized like the testName;
	// empty when the result's test definition carries none (Epic 428).
	Method string
	// Unit is the result's own unit: the go package import path, the TRX
	// className or the JUnit classname. UnitFile is a JUnit file attribute.
	Unit, UnitFile string
	// TestCaseID is a test_case_id trait or property, if the file carries one.
	TestCaseID string
	Outcome    string
}

// packageFailure is a go test package that failed outside its tests' outcomes.
type packageFailure struct {
	Package string `json:"package"`
	Failure string `json:"failure"`
}

type parsedResults struct {
	Results []testResult
	// NotRecorded lists packages that failed without test events of their
	// own (a setup or build failure); no test takes an outcome from them.
	NotRecorded []packageFailure
	// FailedAfterPassing lists packages that failed although their tests
	// passed; those tests keep their outcomes.
	FailedAfterPassing []packageFailure
}

// parseTestResults reads one result file of format trx, junit or
// go-test-json. A file it cannot parse is an error naming the file.
func parseTestResults(format, path, root string) (parsedResults, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return parsedResults{}, err
	}
	var parsed parsedResults
	switch format {
	case "trx":
		parsed.Results, err = parseTRX(raw)
	case "junit":
		parsed.Results, err = parseJUnit(raw)
	case "go-test-json":
		parsed, err = parseGoTestJSON(raw)
	default:
		return parsedResults{}, fmt.Errorf("unknown result format %q (use trx, junit or go-test-json)", format)
	}
	if err != nil {
		return parsedResults{}, err
	}
	for i := range parsed.Results {
		parsed.Results[i].Format = format
		parsed.Results[i].File = path
		parsed.Results[i].Root = root
		parsed.Results[i].Normalized = normalizedResultName(format, parsed.Results[i].Name)
	}
	return parsed, nil
}

// normalizedResultName is the name the source rule compares: go test -json,
// the name before the first /; TRX, the text before the first parenthesis,
// then its part after the last dot; JUnit, the testcase name.
func normalizedResultName(format, name string) string {
	switch format {
	case "go-test-json":
		if i := strings.Index(name, "/"); i >= 0 {
			return name[:i]
		}
	case "trx":
		if i := strings.Index(name, "("); i >= 0 {
			name = name[:i]
		}
		if i := strings.LastIndex(name, "."); i >= 0 {
			return name[i+1:]
		}
	}
	return name
}

// ---------------------------------------------------------------- TRX

var trxOutcomes = map[string]string{
	"Passed": "pass", "Warning": "pass",
	"Failed": "fail",
	"Error":  "error", "Timeout": "error", "Aborted": "error", "PassedButRunAborted": "error", "Disconnected": "error", "InProgress": "error",
	"NotExecuted": "skip", "NotRunnable": "skip", "Pending": "skip",
	"Inconclusive": "inconclusive", "Completed": "inconclusive",
}

type trxRun struct {
	XMLName xml.Name `xml:"TestRun"`
	Results []struct {
		TestID   string `xml:"testId,attr"`
		TestName string `xml:"testName,attr"`
		Outcome  string `xml:"outcome,attr"`
	} `xml:"Results>UnitTestResult"`
	Definitions []struct {
		ID         string `xml:"id,attr"`
		Properties []struct {
			Key   string `xml:"Key"`
			Value string `xml:"Value"`
		} `xml:"Properties>Property"`
		Method struct {
			ClassName string `xml:"className,attr"`
			Name      string `xml:"name,attr"`
		} `xml:"TestMethod"`
	} `xml:"TestDefinitions>UnitTest"`
}

func parseTRX(raw []byte) ([]testResult, error) {
	var run trxRun
	if err := xml.Unmarshal(raw, &run); err != nil {
		return nil, err
	}
	type definition struct{ className, method, testCase string }
	definitions := map[string]definition{}
	for _, d := range run.Definitions {
		def := definition{className: d.Method.ClassName}
		if name := strings.TrimSpace(d.Method.Name); name != "" {
			def.method = normalizedResultName("trx", name)
		}
		for _, p := range d.Properties {
			if strings.TrimSpace(p.Key) == "test_case_id" {
				def.testCase = strings.TrimSpace(p.Value)
			}
		}
		definitions[d.ID] = def
	}
	results := []testResult{}
	for _, r := range run.Results {
		outcome, ok := trxOutcomes[r.Outcome]
		if !ok {
			return nil, fmt.Errorf("test %q has unknown TRX outcome %q", r.TestName, r.Outcome)
		}
		def := definitions[r.TestID]
		results = append(results, testResult{Name: r.TestName, Method: def.method, Unit: def.className, TestCaseID: def.testCase, Outcome: outcome})
	}
	return results, nil
}

// ---------------------------------------------------------------- JUnit

type junitCase struct {
	Name       string    `xml:"name,attr"`
	ClassName  string    `xml:"classname,attr"`
	File       string    `xml:"file,attr"`
	Failure    *struct{} `xml:"failure"`
	Error      *struct{} `xml:"error"`
	Skipped    *struct{} `xml:"skipped"`
	Properties []struct {
		Name  string `xml:"name,attr"`
		Value string `xml:"value,attr"`
	} `xml:"properties>property"`
}

type junitSuite struct {
	Suites []junitSuite `xml:"testsuite"`
	Cases  []junitCase  `xml:"testcase"`
}

func parseJUnit(raw []byte) ([]testResult, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var root junitSuite
	var start *xml.StartElement
	for start == nil {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if element, ok := token.(xml.StartElement); ok {
			start = &element
		}
	}
	if start.Name.Local != "testsuites" && start.Name.Local != "testsuite" {
		return nil, fmt.Errorf("root element is <%s>, not <testsuites> or <testsuite>", start.Name.Local)
	}
	if err := decoder.DecodeElement(&root, start); err != nil {
		return nil, err
	}
	results := []testResult{}
	var walk func(suite junitSuite)
	walk = func(suite junitSuite) {
		for _, c := range suite.Cases {
			result := testResult{Name: c.Name, Unit: c.ClassName, UnitFile: c.File, Outcome: "pass"}
			switch {
			case c.Failure != nil:
				result.Outcome = "fail"
			case c.Error != nil:
				result.Outcome = "error"
			case c.Skipped != nil:
				result.Outcome = "skip"
			}
			for _, p := range c.Properties {
				if strings.TrimSpace(p.Name) == "test_case_id" {
					result.TestCaseID = strings.TrimSpace(p.Value)
				}
			}
			results = append(results, result)
		}
		for _, nested := range suite.Suites {
			walk(nested)
		}
	}
	walk(root)
	return results, nil
}

// ---------------------------------------------------------------- go test -json

type goTestEvent struct {
	Action      string
	Package     string
	Test        string
	Output      string
	ImportPath  string
	FailedBuild string
}

// parseGoTestJSON reads a go test -json stream. A test's outcome is its last
// pass, fail or skip action; a test that ran without one is an error. A
// build failure is attributed by the package fail event's FailedBuild field,
// which Go 1.24 and later write.
func parseGoTestJSON(raw []byte) (parsedResults, error) {
	type testState struct {
		pkg, name, final string
	}
	var order []string
	tests := map[string]*testState{}
	packageTests := map[string]bool{}
	packageFailed := map[string]bool{}
	packageFailedBuild := map[string]string{}
	var packageOrder []string
	seenPackage := map[string]bool{}
	packageOutput := map[string][]string{}
	buildOutput := map[string][]string{}

	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 1<<20), 64<<20)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		var event goTestEvent
		decoder := json.NewDecoder(strings.NewReader(text))
		if err := decoder.Decode(&event); err != nil {
			return parsedResults{}, fmt.Errorf("line %d is not a go test -json event: %v", line, err)
		}
		if _, err := decoder.Token(); err != io.EOF {
			return parsedResults{}, fmt.Errorf("line %d is not a go test -json event", line)
		}
		if event.Action == "" {
			return parsedResults{}, fmt.Errorf("line %d has no Action", line)
		}
		if event.ImportPath != "" && event.Package == "" {
			if event.Action == "build-output" {
				buildOutput[event.ImportPath] = append(buildOutput[event.ImportPath], event.Output)
			}
			continue
		}
		if event.Package == "" {
			continue
		}
		if !seenPackage[event.Package] {
			seenPackage[event.Package] = true
			packageOrder = append(packageOrder, event.Package)
		}
		if event.Test == "" {
			switch event.Action {
			case "output":
				packageOutput[event.Package] = append(packageOutput[event.Package], event.Output)
			case "fail":
				packageFailed[event.Package] = true
				if event.FailedBuild != "" {
					packageFailedBuild[event.Package] = event.FailedBuild
				}
			}
			continue
		}
		packageTests[event.Package] = true
		key := event.Package + "\x00" + event.Test
		state, ok := tests[key]
		if !ok {
			state = &testState{pkg: event.Package, name: event.Test}
			tests[key] = state
			order = append(order, key)
		}
		switch event.Action {
		case "pass", "fail", "skip":
			state.final = event.Action
		}
	}
	if err := scanner.Err(); err != nil {
		return parsedResults{}, err
	}

	var parsed parsedResults
	parsed.Results = []testResult{}
	unhealthy := map[string]bool{}
	for _, key := range order {
		state := tests[key]
		outcome := state.final
		if outcome == "" {
			outcome = "error"
		}
		if outcome == "fail" || outcome == "error" {
			unhealthy[state.pkg] = true
		}
		parsed.Results = append(parsed.Results, testResult{Name: state.name, Unit: state.pkg, Outcome: outcome})
	}
	sort.Strings(packageOrder)
	for _, pkg := range packageOrder {
		if !packageFailed[pkg] {
			continue
		}
		failure := packageFailureText(packageOutput[pkg])
		if build := packageFailedBuild[pkg]; build != "" {
			failure = "build failed: " + packageFailureText(buildOutput[build])
		}
		switch {
		case !packageTests[pkg]:
			parsed.NotRecorded = append(parsed.NotRecorded, packageFailure{Package: pkg, Failure: failure})
		case !unhealthy[pkg]:
			parsed.FailedAfterPassing = append(parsed.FailedAfterPassing, packageFailure{Package: pkg, Failure: failure})
		}
	}
	return parsed, nil
}

// packageFailureText joins a package's own output lines, without the PASS,
// FAIL and ok summary lines go test adds.
func packageFailureText(lines []string) string {
	var kept []string
	for _, chunk := range lines {
		for _, line := range strings.Split(chunk, "\n") {
			line = strings.TrimRight(line, "\r")
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || trimmed == "PASS" || trimmed == "FAIL" || strings.HasPrefix(trimmed, "FAIL\t") || strings.HasPrefix(trimmed, "ok  \t") {
				continue
			}
			kept = append(kept, trimmed)
		}
	}
	if len(kept) == 0 {
		return "the package failed without output of its own"
	}
	return strings.Join(kept, "; ")
}
