package cmd

import (
	"fmt"
	"net/http"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/platform"
	"github.com/modernpath/cli/internal/zitadel"
)

// bindingLines is the one renderer of the server, system and sign-in lines
// both `status` and `factory status` print, so the two never disagree about
// the binding (REQ-CROSS-391). auth may be nil (unreadable credential file).
func bindingLines(cfg *config.Config, auth *config.Auth) []string {
	apiURL := cfg.APIURL
	if apiURL == "" {
		apiURL = config.DefaultAPIURL
	}
	lines := []string{fmt.Sprintf("Server:  %s (%s)", environmentName(apiURL), apiURL)}
	if cfg.SystemID > 0 {
		name := cfg.SystemName
		if name == "" {
			name = "system"
		}
		lines = append(lines, fmt.Sprintf("System:  %s (ID: %d)", name, cfg.SystemID))
	} else {
		lines = append(lines, "System:  not bound (run 'modernpath init')")
	}
	switch {
	case auth == nil || auth.Token == "":
		lines = append(lines, fmt.Sprintf("Signed in: no — run '%s'", authRepairCommand(apiURL)))
	case auth.Actor != "":
		lines = append(lines, "Signed in: "+auth.Actor)
	default:
		lines = append(lines, "Signed in: yes (actor not recorded — sign in again to record it)")
	}
	return lines
}

// bindingKey names the slot a binding is stashed under: the environment name
// for a known host, the URL itself for a custom one.
func bindingKey(apiURL string) string {
	if name := environmentName(apiURL); name != "custom" {
		return name
	}
	return apiURL
}

// credentialRefusal reports why the active credential is not one this server
// accepts — the same local audience check every call runs — or "" when it
// passes or nothing can be said (REQ-CROSS-391: reported at switch time,
// not at the next write).
func credentialRefusal(apiURL, token string) string {
	if token == "" {
		return ""
	}
	probe, err := http.NewRequest(http.MethodGet, apiURL+"/api/systems", nil)
	if err != nil {
		return ""
	}
	platform.Prepare(probe)
	if err := platform.Authorize(probe, token); err != nil {
		return err.Error()
	}
	if profile, known := zitadel.ProfileForAPIURL(apiURL); known {
		if claims, ok := zitadel.TokenClaims(token); ok && claims.Issuer != "" && claims.Issuer != profile.Issuer {
			return fmt.Sprintf("issued by %s, not by this server's identity provider %s", claims.Issuer, profile.Issuer)
		}
	}
	return ""
}
