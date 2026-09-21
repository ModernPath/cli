package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
)

// REQ-CROSS-405 — `init` establishes the credential before it lists systems.
// A fresh checkout has no binding and often no credential; listing with none
// is answered 401, and a workspace left unbound answers 401 on every later
// verb. So the order is: reach the server, sign in (or name the sign-in as
// the next step), list, bind. An expired stored token counts as missing: the
// server would reject it exactly as it rejects none.

// signInForServer runs the interactive sign-in for baseURL: the baked-in
// profile's flow when the host has one, the manual token prompt otherwise.
var signInForServer = func(baseURL string) error {
	if profile, ok := zitadel.ProfileForAPIURL(baseURL); ok {
		return authenticateWithProfile(profile)
	}
	return authenticateWithManualToken(baseURL)
}

// initCredential returns the bearer init lists systems with. With none
// stored, or a stored one that has expired, it runs the sign-in when stdin is
// a terminal and otherwise refuses naming the sign-in command for this
// server. Nothing is sent to the server on the refusing path.
func initCredential(baseURL string, now time.Time) (string, error) {
	root, _ := os.Getwd()
	env := &factoryEnv{Root: root, APIURL: baseURL}
	repair := authRepairCommand(baseURL)

	token, err := storedFreshToken(env, now)
	if err == nil && token != "" {
		return token, nil
	}
	if !stdinIsTerminal() {
		if err != nil {
			return "", err
		}
		return "", credentialError{
			err:    fmt.Errorf("not signed in to %s — run '%s', then 'modernpath init' again", baseURL, repair),
			repair: repair,
		}
	}

	if err != nil {
		printWarning("%v\n", err)
	}
	printInfo("Signing in to %s before listing systems...\n", baseURL)
	if err := signInForServer(baseURL); err != nil {
		return "", err
	}
	token, err = storedFreshToken(env, now)
	if err != nil {
		return "", err
	}
	if token == "" {
		return "", credentialError{
			err:    fmt.Errorf("sign-in stored no credential — run '%s', then 'modernpath init' again", repair),
			repair: repair,
		}
	}
	return token, nil
}

// storedFreshToken reads the stored credential and applies the same freshness
// rule the factory verbs use: refresh when it can, refuse when it has expired.
// An absent credential is ("", nil); an expired one is the refusal itself.
func storedFreshToken(env *factoryEnv, now time.Time) (string, error) {
	auth, err := config.ReadAuth()
	if err != nil {
		repair := authRepairCommand(env.APIURL)
		return "", credentialError{
			err:    fmt.Errorf("auth.json is unreadable (%v) — fix it or run '%s'", err, repair),
			repair: repair,
		}
	}
	if auth == nil || strings.TrimSpace(auth.Token) == "" {
		return "", nil
	}
	auth, err = ensureFreshCredential(env, auth, now)
	if err != nil {
		return "", err
	}
	env.tokenExpiry = storedExpiry(auth)
	return auth.Token, nil
}

// initListingRefused turns a failed system listing into the step that failed
// and the verb that continues. A 401 is the credential statement the other
// verbs print; anything else names the listing and how to retry or bypass it.
func initListingRefused(baseURL string, err error) error {
	if errors.Is(err, api.ErrUnauthorized) {
		return (&factoryEnv{APIURL: baseURL}).credentialRejected()
	}
	return fmt.Errorf("could not list systems (%v) — run 'modernpath init' again, or bind one directly with 'modernpath factory connect --system <id>'", err)
}

// chooseSystemWithoutTerminal stands in for the picker when stdin is not a
// terminal: one system is bound as the only choice; several are listed and
// the flag that names one is the next step.
func chooseSystemWithoutTerminal(systems []api.System) (*api.System, error) {
	if len(systems) == 1 {
		selected := systems[0]
		printInfo("One system available — binding %s\n", selected.Name)
		return &selected, nil
	}
	if len(systems) == 0 {
		return nil, fmt.Errorf("no systems to bind — create one in the platform, then run 'modernpath init' again")
	}
	fmt.Println("Systems available:")
	for _, sys := range systems {
		fmt.Printf("  %d  %s\n", sys.ID, sys.Name)
	}
	return nil, fmt.Errorf("several systems and no terminal to choose in — run 'modernpath init --system-id <id>'")
}
