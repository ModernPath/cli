package cmd

import (
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/api"
)

// The reachability warning exists so an operator can pick a reachable system to
// bind to. A bare list of ids makes that a lookup; showing `id (name)` makes it
// a choice. Names come straight off the systems the credential can reach.
func TestSystemMismatchMessageShowsNamesWhenPresent(t *testing.T) {
	msg := systemMismatchMessage(243, []api.System{
		{ID: 49, Name: "modernpath-v1-jussi"},
		{ID: 52, Slug: "modernpath-v1-pasi"}, // name blank → slug is the label
		{ID: 7},                              // neither → a bare id
	})

	for _, want := range []string{
		"configured system 243",
		"49 (modernpath-v1-jussi)",
		"52 (modernpath-v1-pasi)",
		"factory connect --system",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message must contain %q, got %q", want, msg)
		}
	}
	// A system with neither name nor slug renders as a bare id, never "7 ()".
	if strings.Contains(msg, "7 (") {
		t.Errorf("a nameless system must render as a bare id, got %q", msg)
	}
}

func TestSystemMismatchMessageSaysNoneForAnEmptyList(t *testing.T) {
	msg := systemMismatchMessage(243, nil)
	if !strings.Contains(msg, "reachable: none") {
		t.Errorf("an empty reachable set must read 'reachable: none', got %q", msg)
	}
}
