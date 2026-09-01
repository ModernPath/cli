// REQ-CROSS-208 T5 (RUN:2026-08-18): the WAF block answered
// {"error":"Request blocked by WAF"} and the CLI showed a bare "HTTP 403" —
// the server's reason must reach the user.
package cmd

import (
	"strings"
	"testing"
)

func TestScanFailureCarriesTheServersErrorField(t *testing.T) {
	reason := scanFailureReason(403, []byte(`{"error":"Request blocked by WAF"}`))
	if !strings.Contains(reason, "Request blocked by WAF") {
		t.Fatalf("the server's reason must reach the user, got: %q", reason)
	}
}

func TestScanFailureCarriesTheMessageField(t *testing.T) {
	reason := scanFailureReason(404, []byte(`{"message":"System not found"}`))
	if !strings.Contains(reason, "System not found") {
		t.Fatalf("message field lost: %q", reason)
	}
}

func TestScanFailureFallsBackToStatus(t *testing.T) {
	reason := scanFailureReason(500, []byte(`<html>gateway`))
	if !strings.Contains(reason, "500") {
		t.Fatalf("want the status as last resort, got: %q", reason)
	}
}
