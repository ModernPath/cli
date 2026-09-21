package main

import (
	_ "embed"
	"strings"

	"github.com/modernpath/cli/cmd"
)

// VERSION is the one place the CLI's version lives: the NEXT release, as the
// tag `v$(cat VERSION)` will name it in ModernPath/cli. A release build gets
// exactly that string through -ldflags (release.yml reads the tag); a local
// build gets `<VERSION>-dev+<commit> (<date>)` from install-local.sh; a bare
// `go build` gets `<VERSION>-dev` from here. Semver then orders every local
// build after the last release and before the next one, which is what a
// freshness check compares (BACKLOG-TOOL-71). install-local.sh used to
// hard-code 0.5.0 while the release repo was already at v0.6.0, so a fresh
// local build read as older than a month-old release.
//
//go:embed VERSION
var nextRelease string

// defaultVersion is what the binary reports when no -ldflags stamp was given.
func defaultVersion(embedded string) string {
	return strings.TrimSpace(embedded) + "-dev"
}

func init() {
	if cmd.Version == "dev" {
		cmd.Version = defaultVersion(nextRelease)
	}
}
