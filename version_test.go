package main

import (
	"regexp"
	"strings"
	"testing"
)

// VERSION is read by the build scripts, embedded here, and named by the
// release tag; a value that is not MAJOR.MINOR.PATCH breaks the semver order
// every freshness comparison rests on.
func TestVersionFileIsAReleaseSemver(t *testing.T) {
	v := strings.TrimSpace(nextRelease)
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(v) {
		t.Fatalf("VERSION must hold the next release as MAJOR.MINOR.PATCH, got %q", v)
	}
	if got := defaultVersion(nextRelease); got != v+"-dev" {
		t.Fatalf("an unstamped build reports %q, want %q", got, v+"-dev")
	}
}
