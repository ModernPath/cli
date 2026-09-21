package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/fatih/color"
	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/platform"
	"github.com/modernpath/cli/internal/zitadel"
	"github.com/spf13/cobra"
)

var (
	authStatusJSON    bool
	authStatusOffline bool
)

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether you are signed in, as which workspace, against which server",
	Long: `Report the current authentication state.

Answers, from the CLI itself, the questions a script or agent should not have
to answer by reading .modernpath/auth.json or calling the API by hand: are you
signed in, as whom (the actor recorded at sign-in), to which organization,
server and bound system, until when (the token's expiry), and is the stored
credential still accepted. A refusal names its reason — no credential,
expired, issued for a different server, missing project audience, rejected by
the server — and the sign-in command that repairs it.

By default it checks the credential against the server (GET /api/systems, the
same call 'modernpath auth' uses to validate a sign-in), through the same
platform adaptation and project-audience pre-check every other command uses.
--offline skips the network and reports only what the stored credential says
about itself; there, "authenticated" means a credential is present and locally
valid, not that the server still accepts it (checked_online is false) — require
checked_online too if you need a server-verified answer.

The exit code is 0 when authenticated and non-zero when not, so it can gate a
script without parsing output:

  modernpath auth status >/dev/null 2>&1 || modernpath auth

--json prints a single JSON object on stdout and nothing else, for tools:
actor, workspace_id, workspace_name, system_id, expires_at,
expires_in_seconds, and reason when not authenticated.

Examples:
  modernpath auth status            # Check the credential against the server
  modernpath auth status --offline  # Local check only, no network
  modernpath auth status --json     # Machine-readable status`,
	Args: cobra.NoArgs,
	RunE: runAuthStatus,
}

func init() {
	authStatusCmd.Flags().BoolVar(&authStatusJSON, "json", false, "Print a single JSON object on stdout and nothing else")
	authStatusCmd.Flags().BoolVar(&authStatusOffline, "offline", false, "Do not contact the server; report only what the stored credential says")
	authStatusCmd.SilenceUsage = true
	// Return an error for the exit code (below) without Cobra also printing its
	// own "Error: …" line: the Execute() wrapper (root.go) prints it once, and
	// the human/JSON report on stdout already carries the detail.
	authStatusCmd.SilenceErrors = true
	authCmd.AddCommand(authStatusCmd)
}

// authStatus is the machine-readable shape behind `auth status --json`. The
// field names are the contract that output feeds to tools — keep them stable.
type authStatus struct {
	Authenticated bool   `json:"authenticated"`
	Server        string `json:"server"`
	Environment   string `json:"environment"`
	WorkspaceID   string `json:"workspace_id,omitempty"`
	WorkspaceName string `json:"workspace_name,omitempty"`
	Issuer        string `json:"issuer,omitempty"`
	TokenPresent  bool   `json:"token_present"`
	CheckedOnline bool   `json:"checked_online"`
	Detail        string `json:"detail"`
	// REQ-CROSS-388: the identity and lifetime a session needs without
	// reading the credential file — the actor, the bound system, the expiry
	// and, when not authenticated, a stable reason key beside the detail.
	Actor            string `json:"actor,omitempty"`
	SystemID         int    `json:"system_id,omitempty"`
	ExpiresAt        string `json:"expires_at,omitempty"`
	ExpiresInSeconds int64  `json:"expires_in_seconds,omitempty"`
	Reason           string `json:"reason,omitempty"`
}

func runAuthStatus(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		cfg = &config.Config{APIURL: config.DefaultAPIURL}
	}
	apiURL := cfg.APIURL
	if apiURL == "" {
		apiURL = config.DefaultAPIURL
	}
	auth, _ := config.ReadAuth()

	st := evaluateAuthStatusAt(apiURL, cfg.SystemID, auth, authStatusOffline, time.Now())

	if authStatusJSON {
		renderAuthStatusJSON(st)
	} else {
		renderAuthStatusHuman(st)
	}

	// Unlike `env test` and `auth` — diagnostics that "ran fine" whatever they
	// found, and so return nil (auth_system_reachability_test.go) — `auth
	// status` is a predicate: its whole purpose is a yes/no a script or agent
	// branches on. A non-zero exit for "no" is that contract (as `gh auth
	// status` and friends do). SilenceUsage on the root (root.go) keeps the
	// exit clean: one stderr line, no usage dump. The full report is already on
	// stdout above; --json output stays pure because the error rides stderr.
	if !st.Authenticated {
		return fmt.Errorf("not authenticated: %s", st.Detail)
	}
	return nil
}

// evaluateAuthStatus decides the state without printing, so both renderers and
// the tests see the same result. The online probe is the authoritative answer;
// offline reports only what is knowable locally and says so.
func evaluateAuthStatus(apiURL string, auth *config.Auth, offline bool) authStatus {
	return evaluateAuthStatusAt(apiURL, 0, auth, offline, time.Now())
}

// evaluateAuthStatusAt is the evaluation with the bound system and the clock
// injected (REQ-CROSS-388): the identity and the expiry are read from the
// stored credential, falling back to the access token's own claims for a
// credential an older build stored, and a refusal names its reason and the
// sanctioned remedy for this server.
func evaluateAuthStatusAt(apiURL string, systemID int, auth *config.Auth, offline bool, now time.Time) authStatus {
	st := authStatus{
		Server:      apiURL,
		Environment: environmentName(apiURL),
		SystemID:    systemID,
	}
	var token string
	if auth != nil {
		st.WorkspaceID = auth.WorkspaceID
		st.WorkspaceName = auth.WorkspaceName
		st.Issuer = auth.Issuer
		st.Actor = auth.Actor
		token = auth.Token
	}
	st.TokenPresent = token != ""

	if !st.TokenPresent {
		st.Reason = "no credential"
		st.Detail = fmt.Sprintf("no stored credential (run '%s')", authRepairCommand(apiURL))
		return st
	}

	claims, readable := zitadel.TokenClaims(token)
	if st.Actor == "" && readable {
		st.Actor = claims.Email
		if st.Actor == "" {
			st.Actor = claims.Subject
		}
	}
	if st.Issuer == "" && readable {
		st.Issuer = claims.Issuer
	}
	if expiry := credentialExpiry(auth, claims, readable); !expiry.IsZero() {
		st.ExpiresAt = expiry.Format(time.RFC3339)
		st.ExpiresInSeconds = int64(expiry.Sub(now).Seconds())
		if !expiry.After(now) {
			st.Reason = "expired"
			st.Detail = fmt.Sprintf("stored credential expired at %s — run '%s'", st.ExpiresAt, authRepairCommand(apiURL))
			return st
		}
	}

	// A token from another plane's identity provider is refused before the
	// audience check names a missing project: the remedy is the same, the
	// reason is not.
	if profile, known := zitadel.ProfileForAPIURL(apiURL); known && st.Issuer != "" && st.Issuer != profile.Issuer {
		st.Reason = "issued for a different server"
		st.Detail = fmt.Sprintf("stored credential was issued by %s, not by this server's identity provider %s — run '%s'", st.Issuer, profile.Issuer, authRepairCommand(apiURL))
		return st
	}

	// The project-audience pre-check is a local read: a JWT issued before the
	// CLI began requesting the platform project audience (REQ-CROSS-291) is one
	// this host will reject, and saying so needs no round trip. A non-JWT token
	// or a non-platform host expects nothing and passes it through. Both paths
	// run it; only the default path then goes to the network.
	// A malformed api_url (from `env --set`, or a hand-edited config) is handled
	// here so the paths below never touch a nil request — a panic online, a
	// false "present" offline. The siblings guard it too (env test's
	// healthProbe, api.Client.doRequest). Keep platform.Prepare within the
	// wiring guard's window of the NewRequest above (platform_wiring_test.go).
	probe, err := http.NewRequest(http.MethodGet, apiURL+"/api/systems", nil)
	if err != nil {
		st.Detail = fmt.Sprintf("invalid API URL %q: %v", apiURL, err)
		return st
	}
	platform.Prepare(probe)
	if err := platform.Authorize(probe, token); err != nil {
		st.Reason = "missing project audience"
		st.Detail = err.Error()
		return st
	}

	if offline {
		st.Authenticated = true
		st.Detail = "stored credential present; not verified against the server (--offline)"
		return st
	}

	// Authoritative check: the same GET /api/systems `modernpath auth` uses to
	// validate a sign-in.
	st.CheckedOnline = true
	probe.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(probe)
	if err != nil {
		st.Detail = fmt.Sprintf("server unreachable: %v", err)
		return st
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		st.Authenticated = true
		st.Detail = "authenticated"
	case http.StatusUnauthorized:
		st.Reason = "rejected by the server"
		st.Detail = fmt.Sprintf("stored credential expired or invalid (run '%s')", authRepairCommand(apiURL))
	default:
		st.Detail = fmt.Sprintf("server returned %s", resp.Status)
	}
	return st
}

// credentialExpiry is the stored expires_at when the sign-in recorded one,
// else the access token's exp claim, else zero (no lifetime known).
func credentialExpiry(auth *config.Auth, claims zitadel.Claims, readable bool) time.Time {
	if auth != nil && auth.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, auth.ExpiresAt); err == nil {
			return t.UTC()
		}
	}
	if readable {
		return claims.ExpiresAt
	}
	return time.Time{}
}

// expiryLine renders the time left in the coarsest unit that still says
// something: "in 2h", "in 30m", "in 45s".
func expiryLine(st authStatus) string {
	if st.ExpiresAt == "" {
		return "not known"
	}
	if st.ExpiresInSeconds <= 0 {
		return "expired at " + st.ExpiresAt
	}
	d := time.Duration(st.ExpiresInSeconds) * time.Second
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("in %dh%02dm (%s)", int(d.Hours()), int(d.Minutes())%60, st.ExpiresAt)
	case d >= time.Minute:
		return fmt.Sprintf("in %dm (%s)", int(d.Minutes()), st.ExpiresAt)
	default:
		return fmt.Sprintf("in %ds (%s)", int(d.Seconds()), st.ExpiresAt)
	}
}

// renderAuthStatusJSON writes only the JSON object, only to stdout, so the
// output stays parseable (REQ-CROSS-121 / REQ-CROSS-210 D13: --json owns
// stdout, and it must not call the printInfo/printSuccess helpers, which write
// to color.Output = stdout).
func renderAuthStatusJSON(st authStatus) {
	blob, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stdout, "{\"authenticated\":%t}\n", st.Authenticated)
		return
	}
	fmt.Fprintln(os.Stdout, string(blob))
}

func renderAuthStatusHuman(st authStatus) {
	bold := color.New(color.Bold)
	cyan := color.New(color.FgCyan)

	fmt.Println()
	bold.Println("Authentication")
	fmt.Println("─────────────────────────────────────────")

	cyan.Printf("  Server:       ")
	fmt.Printf("%s (%s)\n", st.Environment, st.Server)

	if st.TokenPresent {
		cyan.Printf("  Actor:        ")
		if st.Actor != "" {
			fmt.Printf("%s\n", st.Actor)
		} else {
			fmt.Println("not recorded — sign in again to record it")
		}
	}
	if line := workspaceStatusLine(&config.Auth{WorkspaceID: st.WorkspaceID, WorkspaceName: st.WorkspaceName}); line != "" {
		cyan.Printf("  Organization: ")
		fmt.Printf("%s\n", line)
	}
	cyan.Printf("  System:       ")
	if st.SystemID != 0 {
		fmt.Printf("%d\n", st.SystemID)
	} else {
		fmt.Println("not bound (run 'modernpath init')")
	}
	if st.Issuer != "" {
		cyan.Printf("  Issuer:       ")
		fmt.Printf("%s\n", st.Issuer)
	}
	if st.TokenPresent {
		cyan.Printf("  Expires:      ")
		fmt.Printf("%s\n", expiryLine(st))
	}

	cyan.Printf("  Status:       ")
	switch {
	case st.Authenticated && st.CheckedOnline:
		color.Green("Authenticated")
	case st.Authenticated:
		// Offline: a credential is present but unverified — not a green light.
		fmt.Println(st.Detail)
	default:
		color.Yellow(st.Detail)
	}
	fmt.Println()
}
