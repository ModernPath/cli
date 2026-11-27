// Package envflag reads flag-shaped environment variables, such as CI and
// MODERNPATH_NO_UPDATE_CHECK, with one rule for the whole CLI.
package envflag

import "strings"

// On reads a flag-shaped variable the way ci-info and the tools built on it
// do: set to anything but an explicit negation means on, ignoring letter case
// and surrounding spaces. A shell profile that exports CI=false to switch
// other tools out of CI mode is not a CI shell.
func On(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}
