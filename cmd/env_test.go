package cmd

import (
	"testing"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
)

// The "production" environment, the fallback default, and the Zitadel prod
// profile must name the same host: `env --set=production`, a fresh workspace's
// default, and `auth --sso` all have to land on the same API. They are declared
// in two packages (config.DefaultAPIURL and zitadel.ProdProfile.APIURL), so
// lock them together — a drift here is a silently mis-pointed CLI.
func TestDefaultAPIURLIsCloudProd(t *testing.T) {
	if config.DefaultAPIURL != zitadel.ProdProfile.APIURL {
		t.Fatalf("config.DefaultAPIURL (%q) must equal zitadel.ProdProfile.APIURL (%q)",
			config.DefaultAPIURL, zitadel.ProdProfile.APIURL)
	}
	if config.DefaultAPIURL == config.BetaAPIURL {
		t.Fatal("the default must no longer be beta — beta is being decommissioned")
	}
}

func TestEnvironmentNameMapsKnownHosts(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{zitadel.ProdProfile.APIURL, "production"},
		{zitadel.TestProfile.APIURL, "test"},
		{config.LocalAPIURL, "local"},
		{config.BetaAPIURL, "beta"},
		{"https://some-operator-host.example", "custom"},
		{"", "custom"},
	}
	for _, tc := range cases {
		if got := environmentName(tc.url); got != tc.want {
			t.Errorf("environmentName(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

func TestEnvironmentURLResolvesNamesAndAliases(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"production", zitadel.ProdProfile.APIURL},
		{"prod", zitadel.ProdProfile.APIURL},
		{"test", zitadel.TestProfile.APIURL},
		{"test-plat", zitadel.TestProfile.APIURL},
		{"local", config.LocalAPIURL},
		{"localhost", config.LocalAPIURL},
		{"dev", config.LocalAPIURL},
		{"beta", config.BetaAPIURL},
	}
	for _, tc := range cases {
		got, ok := environmentURL(tc.name)
		if !ok || got != tc.want {
			t.Errorf("environmentURL(%q) = (%q, %v), want (%q, true)", tc.name, got, ok, tc.want)
		}
	}

	// An unknown name is not an environment — the caller treats it as a raw URL.
	if _, ok := environmentURL("staging"); ok {
		t.Error("environmentURL(\"staging\") reported a known environment; want ok=false")
	}
}

// Every canonical name round-trips: its URL maps back to the same name. Guards
// against a name and its URL drifting between the two helpers.
func TestEnvironmentRoundTrip(t *testing.T) {
	for _, e := range knownEnvironments() {
		url, ok := environmentURL(e.Name)
		if !ok {
			t.Errorf("%q is listed by knownEnvironments but environmentURL does not resolve it", e.Name)
			continue
		}
		if url != e.URL {
			t.Errorf("environmentURL(%q) = %q, but knownEnvironments lists %q", e.Name, url, e.URL)
		}
		if got := environmentName(e.URL); got != e.Name {
			t.Errorf("environmentName(%q) = %q, want %q", e.URL, got, e.Name)
		}
	}
}
