package cmd

// REQ-CROSS-282 — one shared implementation of "is the configured system_id
// reachable by the authenticated credential", used identically by every
// surface that needs it (auth's three variants, factoryEnvLoad, status).
// A single copy, not one per surface: four near-identical implementations
// are four independent chances for one of them to drift from the others —
// a shared function can't.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/modernpath/cli/internal/api"
)

// listSystemsFn fetches the authenticated credential's reachable systems. A
// package var, not a direct call: authenticateWithSSO's target URL comes
// from a baked-in Zitadel profile (zitadel.SelectProfile), unlike every
// other caller here, so tests cannot point it at a fake server without this
// seam — mirrors the existing var zitadelLogin / var openBrowserFn pattern.
var listSystemsFn = func(apiURL, token string) ([]api.System, error) {
	return api.NewClient(apiURL, token).ListSystems()
}

// systemReachable reports whether id appears in the authenticated
// credential's own system list.
func systemReachable(systems []api.System, id int) bool {
	for _, s := range systems {
		if s.ID == id {
			return true
		}
	}
	return false
}

// systemMismatchMessage names the configured system, the systems the
// credential can actually reach, and the exact repair command. Each reachable
// system is shown as `id (name)` so the operator can pick the right id without
// looking one up — the whole point of the list is choosing a replacement, and a
// bare column of numbers makes that a guess.
func systemMismatchMessage(systemID int, systems []api.System) string {
	items := make([]string, len(systems))
	for i, s := range systems {
		if label := systemLabel(s); label != "" {
			items[i] = fmt.Sprintf("%d (%s)", s.ID, label)
		} else {
			items[i] = strconv.Itoa(s.ID)
		}
	}
	available := "none"
	if len(items) > 0 {
		available = strings.Join(items, ", ")
	}
	return fmt.Sprintf(
		"configured system %d is not reachable by the authenticated credential (reachable: %s) — run 'modernpath factory connect --system <id>' with a reachable id",
		systemID, available)
}

// systemLabel is the human name to show beside a system id — its name, or its
// slug when the name is blank. Empty when the server sent neither, so the id
// still renders bare rather than as "<id> ()".
func systemLabel(s api.System) string {
	if n := strings.TrimSpace(s.Name); n != "" {
		return n
	}
	return strings.TrimSpace(s.Slug)
}
