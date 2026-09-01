// EPIC-CLI-001 T7 cosmetics (REQ-CROSS-210, RUN:2026-08-18): read-file
// rendered "   1 │     1 | defmodule…" — the server content arrives with its
// own line numbers and the CLI adds a second gutter on top.
package cmd

import "testing"

func TestServerNumberedContentIsNotDoubleNumbered(t *testing.T) {
	serverContent := "    1 | defmodule Core.Auth do\n    2 |   @moduledoc false\n    3 | end"
	got := stripServerLineNumbers(serverContent)
	want := "defmodule Core.Auth do\n  @moduledoc false\nend"
	if got != want {
		t.Fatalf("server numbering must be stripped before the CLI adds its gutter:\n got: %q\nwant: %q", got, want)
	}
}

func TestUnnumberedContentPassesThrough(t *testing.T) {
	content := "plain line\n  indented | with a pipe\nlast"
	if got := stripServerLineNumbers(content); got != content {
		t.Fatalf("content without uniform numbering must pass through untouched, got %q", got)
	}
}
