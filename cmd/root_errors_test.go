package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/fatih/color"
)

func captureCommandError(t *testing.T, args []string, fail func() error) (int, string) {
	t.Helper()
	oldWriter := color.Error
	var output bytes.Buffer
	color.Error = &output
	t.Cleanup(func() { color.Error = oldWriter })

	beginCommandRun(args)
	code := finishCommandRun(fail())
	return code, output.String()
}

func TestReturnedErrorUsesJSONEnvelopeWhenRequested(t *testing.T) {
	for _, args := range [][]string{{"search", "term", "--json"}, {"ask", "term", "--format=json"}, {"ask", "term", "--format", "json"}} {
		code, output := captureCommandError(t, args, func() error { return errors.New("server unavailable") })
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		var envelope struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(output), &envelope); err != nil {
			t.Fatalf("error output is not JSON: %q (%v)", output, err)
		}
		if envelope.Error != "server unavailable" {
			t.Fatalf("error = %q, want server unavailable", envelope.Error)
		}
	}
}

func TestSummaryCommandsUsePlainErrorsUnlessJSONRequested(t *testing.T) {
	for _, args := range [][]string{{"epics", "get", "17"}, {"epics", "specs", "list", "17"}, {"requirements", "get", "UR-6-0"}, {"test-cases", "get", "TC-8-0"}, {"traces", "list"}, {"evidence", "latest"}} {
		code, output := captureCommandError(t, args, func() error { return errors.New("server unavailable") })
		if code != 1 || output != "server unavailable\n" {
			t.Fatalf("plain error for %v: code=%d output=%q", args, code, output)
		}
		jsonArgs := append(append([]string{}, args...), "--json")
		code, output = captureCommandError(t, jsonArgs, func() error { return errors.New("server unavailable") })
		if code != 1 || !json.Valid([]byte(output)) {
			t.Fatalf("JSON error for %v: code=%d output=%q", jsonArgs, code, output)
		}
	}
}

func TestPrintedAndReturnedErrorEmitsOnce(t *testing.T) {
	code, output := captureCommandError(t, []string{"search", "term", "--json"}, func() error {
		printError("request failed: %s\n", "offline")
		return errors.New("request failed: offline")
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if strings.Count(output, "\n") != 1 {
		t.Fatalf("want one error line, got %q", output)
	}
	var envelope struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil || envelope.Error != "request failed: offline" {
		t.Fatalf("error envelope = %q, err=%v", output, err)
	}
}

func TestPrintedAndReturnedPlainErrorEmitsOnce(t *testing.T) {
	code, output := captureCommandError(t, []string{"search", "term"}, func() error {
		printError("request failed: %s\n", "offline")
		return errors.New("request failed: offline")
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if strings.Count(output, "request failed: offline") != 1 {
		t.Fatalf("want one plain error, got %q", output)
	}
}

func TestPrintOnlyErrorStillFailsAndPreservesLimitingExit(t *testing.T) {
	code, output := captureCommandError(t, []string{"search"}, func() error {
		printError("invalid request\n")
		return nil
	})
	if code != 1 || !strings.Contains(output, "invalid request") {
		t.Fatalf("print-only failure: code=%d output=%q", code, output)
	}

	code, output = captureCommandError(t, []string{"search", "--json"}, func() error {
		return serverLimitingError{retryAfter: 3}
	})
	if code != 75 {
		t.Fatalf("limiting exit code = %d, want 75", code)
	}
	if !json.Valid([]byte(output)) {
		t.Fatalf("limiting error is not a JSON envelope: %q", output)
	}
}

func TestRequestsJSONOutput(t *testing.T) {
	for _, args := range [][]string{{"--json"}, {"--json=true"}, {"--format=json"}, {"--format", "json"}} {
		if !requestsJSONOutput(args) {
			t.Errorf("requestsJSONOutput(%v) = false", args)
		}
	}
	for _, args := range [][]string{{"--json=false"}, {"--format=text"}, {"--jsonish"}} {
		if requestsJSONOutput(args) {
			t.Errorf("requestsJSONOutput(%v) = true", args)
		}
	}
}

func TestProgressAndJSONSuccessKeepStdoutClean(t *testing.T) {
	oldOut, oldErr := color.Output, color.Error
	defer func() { color.Output, color.Error = oldOut, oldErr; commandErrors = commandErrorState{} }()
	for _, machine := range []bool{false, true} {
		var out, diagnostic bytes.Buffer
		color.Output, color.Error = &out, &diagnostic
		commandErrors = commandErrorState{active: true, jsonOutput: machine}
		printInfo("Connecting")
		if out.Len() != 0 || !strings.Contains(diagnostic.String(), "Connecting") {
			t.Fatalf("progress leaked to stdout: %q; stderr=%q", out.String(), diagnostic.String())
		}
		printSuccess("Connected")
		if machine && out.Len() != 0 {
			t.Fatalf("JSON success progress leaked to stdout: %q", out.String())
		}
		if !machine && !strings.Contains(out.String(), "Connected") {
			t.Fatalf("missing human success result: %q", out.String())
		}
	}
}

func TestCobraFailureEmitsOneJSONError(t *testing.T) {
	args := []string{"requirements", "get", "SR-1-0", "--fields", "unknown", "--json"}
	resetTreeFlags(rootCmd)
	rootCmd.SetArgs(args)
	defer rootCmd.SetArgs(nil)
	code, output := captureCommandError(t, args, func() error {
		rootCmd.SetErr(color.Error)
		defer rootCmd.SetErr(nil)
		return rootCmd.Execute()
	})
	if code != 1 || !json.Valid([]byte(output)) || strings.Count(output, "\n") != 1 {
		t.Fatalf("expected one JSON error, code=%d output=%q", code, output)
	}
}
