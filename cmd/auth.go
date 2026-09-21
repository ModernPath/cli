package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/manifoldco/promptui"
	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/browser"
	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	token          string
	logout         bool
	authLocal      bool
	ssoFlag        bool
	ssoTestFlag    bool
	deviceFlowFlag bool
	// workspaceFlag names the workspace to sign in to by id; chooseWorkspaceFlag
	// asks for the picker even when a choice is stored (REQ-CROSS-336).
	workspaceFlag       string
	chooseWorkspaceFlag bool
)

// The seams the workspace choice is tested through, beside zitadelLogin: the
// workspace list, the picker, and whether stdin is a terminal.
var (
	listOrganizationsFn = zitadel.ListOrganizations
	pickWorkspaceFn     = pickWorkspace
	stdinIsTerminal     = func() bool { return term.IsTerminal(int(syscall.Stdin)) }
)

// pickWorkspace asks the person to choose one of the listed workspaces: name
// and domain, the home one marked, the one the sign-in landed in preselected.
func pickWorkspace(orgs []zitadel.Organization, homeID, preselectedID string) (zitadel.Organization, error) {
	items := make([]string, 0, len(orgs))
	cursor := 0
	for i, org := range orgs {
		item := workspaceLabel(org)
		if org.ID == homeID {
			item += " — home"
		}
		if org.ID == preselectedID {
			cursor = i
		}
		items = append(items, item)
	}
	prompt := promptui.Select{
		Label:     "Choose a workspace",
		Items:     items,
		Size:      10,
		HideHelp:  true,
		CursorPos: cursor,
	}
	index, _, err := prompt.Run()
	if err != nil {
		return zitadel.Organization{}, err
	}
	return orgs[index], nil
}

// workspaceLabel is how a workspace is named to the person: its name, then
// its domain or id in parentheses.
func workspaceLabel(org zitadel.Organization) string {
	name := org.Name
	if name == "" {
		name = org.ID
	}
	detail := org.PrimaryDomain
	if detail == "" {
		detail = org.ID
	}
	return fmt.Sprintf("%s (%s)", name, detail)
}

// workspaceIDPattern bounds what --workspace may name: the id is embedded in
// a scope value, so anything that could carry a second scope is refused
// before it reaches the issuer (the auth Agent applies the same bound).
var workspaceIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// checkWorkspaceFlags refuses, before any request, what can never succeed:
// a value that is not a workspace id, both flags together, a picker with
// the device flow (which cannot list workspaces, USER:2026-09-09), and a
// picker with no terminal to pick in.
func checkWorkspaceFlags() error {
	switch {
	case workspaceFlag != "" && !workspaceIDPattern.MatchString(workspaceFlag):
		return fmt.Errorf("--workspace %q is not a valid workspace id. Allowed: letters, digits, '.', '_' and '-', up to 128 characters", workspaceFlag)
	case workspaceFlag != "" && chooseWorkspaceFlag:
		return fmt.Errorf("--workspace and --choose-workspace cannot be used together")
	case chooseWorkspaceFlag && deviceFlowFlag:
		return fmt.Errorf("--choose-workspace cannot be used with --device-flow: the device flow cannot list workspaces. Use --workspace <id> instead")
	case chooseWorkspaceFlag && !stdinIsTerminal():
		return fmt.Errorf("--choose-workspace needs an interactive terminal. Use --workspace <id> instead")
	}
	return nil
}

// refuseWorkspaceFlagsWithoutProfile keeps the workspace flags off the hosts
// that take a pasted token: there is no sign-in to choose at, and a pasted
// token names its own workspace.
func refuseWorkspaceFlagsWithoutProfile(baseURL string) error {
	if workspaceFlag == "" && !chooseWorkspaceFlag {
		return nil
	}
	return fmt.Errorf("--workspace and --choose-workspace only apply to the browser or device sign-in. %s takes a pasted token, which already contains its workspace", baseURL)
}

// zitadelLogin is the ZITADEL login entry point authenticateWithProfile
// calls — zitadel.Login chooses between the loopback authorization-code flow
// and the RFC 8628 device flow. It is a package var (rather than a direct
// call) so tests can stub it — the baked-in profile itself (SelectProfile) is
// never stubbed, only the network round trip.
var zitadelLogin = zitadel.Login

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Sign in to the ModernPath platform",
	// `auth logout` used to be silently ignored — and then STARTED A LOGIN
	// (REQ-CROSS-210 D22, RUN:2026-08-18). Positionals are errors that point
	// at the flag form.
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return nil
		}
		if strings.EqualFold(args[0], "logout") {
			return fmt.Errorf("auth takes no arguments. Use 'modernpath auth --logout'")
		}
		return fmt.Errorf("auth takes no arguments (got %q)", args[0])
	},
	Long: `Sign in to the ModernPath platform.

The credential is bound to the environment this checkout targets: --api-url,
then --local, then the binding set with 'modernpath env --set=<env>', then
production.

If you have access to more than one workspace, the browser sign-in lists
them and asks which one to use. The choice is stored and reused on the next
sign-in. --workspace <id> selects a workspace without asking (for scripts
and headless hosts). --choose-workspace asks again. The device flow cannot
list workspaces: without --workspace it signs you in to your own workspace.

By default the CLI opens your browser and waits for the sign-in to complete.
There is nothing to copy or approve. When no browser can reach this machine
(an SSH session, a container, a CI shell, a headless server), it prints a URL
to open on another device instead. --device-flow forces that path. Set
BROWSER to choose the program that opens the URL.

Browser sign-in works for production and the test environment. Any other
host (local, or a custom URL) takes an access token instead, from --token or
the prompt. The token must come from that host's identity provider.

Examples:
  modernpath auth                    # Sign in with your browser
  modernpath auth --device-flow      # Get a URL to open on another device
  modernpath auth --test             # Sign in to the test environment
  modernpath auth --workspace <id>   # Sign in to one workspace without asking
  modernpath auth --choose-workspace # Choose the workspace again
  modernpath auth --token=<token>    # Sign in with an access token
  modernpath auth --local            # Paste a token for localhost:4000
  modernpath auth --api-url=<url>    # Paste a token for a specific URL
  modernpath auth --logout           # Remove stored credentials`,
	RunE: runAuth,
}

func init() {
	authCmd.Flags().StringVar(&token, "token", "", "Access token issued by the target host's identity provider")
	authCmd.Flags().BoolVar(&logout, "logout", false, "Remove the stored credentials")
	authCmd.Flags().BoolVar(&authLocal, "local", false, "Use the local server (localhost:4000) instead of production")
	authCmd.Flags().BoolVar(&ssoTestFlag, "test", false, "Sign in to the test environment instead of production")
	authCmd.Flags().BoolVar(&deviceFlowFlag, "device-flow", false, "Print a sign-in URL to open on another device instead of opening a browser here")
	authCmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace id to sign in to. Without it, you are asked to choose when you have more than one")
	authCmd.Flags().BoolVar(&chooseWorkspaceFlag, "choose-workspace", false, "Choose the workspace again, even if one is already stored")
	// --sso is what the ZITADEL login was called while core still had a login
	// of its own; it is now the only login, so the flag is accepted and does
	// nothing. Kept so `modernpath auth --sso --test` in existing docs, hints
	// and scripts keeps working.
	authCmd.Flags().BoolVar(&ssoFlag, "sso", false, "Accepted for compatibility; this sign-in is the default")
	_ = authCmd.Flags().MarkHidden("sso")
}

func runAuth(cmd *cobra.Command, args []string) error {
	if logout {
		return doLogout()
	}
	if err := checkWorkspaceFlags(); err != nil {
		return err
	}

	baseURL := authTargetURL()

	if token != "" {
		if err := refuseWorkspaceFlagsWithoutProfile(baseURL); err != nil {
			return err
		}
		return authenticateWithToken(baseURL, token)
	}

	// The ZITADEL login needs an issuer, and the CLI only knows the two
	// baked-in ones. Every other host takes a pasted ZITADEL token instead.
	if profile, ok := zitadel.ProfileForAPIURL(baseURL); ok {
		return authenticateWithProfile(profile)
	}
	if err := refuseWorkspaceFlagsWithoutProfile(baseURL); err != nil {
		return err
	}

	fmt.Println("ModernPath Authentication")
	fmt.Println("─────────────────────────────────────────")
	fmt.Printf("API URL: %s\n", baseURL)
	fmt.Println("Browser sign-in is available for production and the test plane only.")
	fmt.Println("Paste an access token issued by this host's identity provider instead.")
	fmt.Println()
	return authenticateWithManualToken(baseURL)
}

// authTargetURL resolves the host this login binds the credential to, highest
// precedence first: --api-url, --local, --test, the workspace binding written
// by `modernpath env --set=<env>`, then production.
//
// Reading the binding is what keeps `env --set=<env>` followed by `auth` on
// one environment. Without it every login fell back to a baked-in profile and
// silently rebound the workspace to production (saveTokens writes the URL it
// authenticated against).
func authTargetURL() string {
	if apiURL != "" {
		return apiURL
	}
	if authLocal {
		return config.LocalAPIURL
	}
	// --test names a plane outright, so it wins over the stored binding.
	if ssoTestFlag {
		return zitadel.TestProfile.APIURL
	}
	if cfg, err := config.ReadConfig(); err == nil && cfg.APIURL != "" {
		return cfg.APIURL
	}
	return zitadel.ProdProfile.APIURL
}

// authenticateWithSSO runs the ZITADEL login against the baked-in profile
// selected by testProfile (REQ-CROSS-235).
func authenticateWithSSO(testProfile bool) error {
	return authenticateWithProfile(zitadel.SelectProfile(testProfile))
}

// loginOptions is what the real command hands zitadel.Login: the
// --device-flow choice, the browser launcher, and the pre-check that reads
// the process's surroundings to say whether a browser can reach a loopback
// listener here.
func loginOptions() zitadel.Options {
	host := browser.DetectHost()
	return zitadel.Options{
		DeviceFlow:          deviceFlowFlag,
		OpenBrowser:         browser.Open,
		LoopbackUnavailable: func() string { return browser.LoopbackUnavailable(host) },
	}
}

// authenticateWithProfile runs the ZITADEL login against one baked-in
// profile — the one that goes with the host being authenticated to — and
// decides the workspace (REQ-CROSS-336). A failed sign-in, and every refusal
// below, is a command error that writes no credential file; success
// persists through saveTokens, which refuses a token that landed in another
// workspace than the one chosen (REQ-CROSS-337).
func authenticateWithProfile(profile zitadel.Profile) error {
	label := "prod"
	if profile.APIURL == zitadel.TestProfile.APIURL {
		label = "test"
	}

	choice := initialWorkspaceChoice(profile)
	tok, err := signIn(profile, label, choice)
	if err != nil {
		return err
	}

	// A token that is not a readable JWT names no workspace and cannot list
	// any; saveTokens stores it as before (or refuses it when one was chosen).
	// A device-flow token cannot list them either (ZITADEL answers it 403,
	// USER:2026-09-09), so nothing is asked of the list on that path.
	if _, readable := zitadel.TokenClaims(tok.AccessToken); readable {
		if choice.Chosen {
			// The sign-in carried the filter; the list is read once for the name.
			if tok.Flow == zitadel.FlowDevice {
				// No name to look up; the id is stored on its own.
			} else if list, err := listOrganizationsFn(context.Background(), profile, tok.AccessToken); err == nil {
				if org, ok := findWorkspace(list.Organizations, choice.ID); ok {
					choice.Name = org.Name
				}
			}
		} else {
			tok, choice, err = chooseWorkspace(profile, label, tok)
			if err != nil {
				return err
			}
		}
	}

	if err := saveLogin(profile.APIURL, tok, choice); err != nil {
		return err
	}
	warnIfConfiguredSystemUnreachable(profile.APIURL)
	return nil
}

// initialWorkspaceChoice decides what the first sign-in carries: the
// workspace --workspace names, else a stored choice made at this issuer
// (a choice made on the other plane names an organization this issuer does
// not know, and is left alone), else nothing.
func initialWorkspaceChoice(profile zitadel.Profile) workspaceChoice {
	choice := workspaceChoice{Issuer: profile.Issuer}
	if workspaceFlag != "" {
		choice.ID, choice.Chosen = workspaceFlag, true
		return choice
	}
	if chooseWorkspaceFlag {
		return choice
	}
	stored, err := config.ReadAuth()
	if err != nil || stored == nil || stored.WorkspaceID == "" {
		return choice
	}
	if stored.Issuer != profile.Issuer {
		printInfo("Your stored workspace (%s) belongs to another sign-in server and is not applied here.\n", workspaceStatusLine(stored))
		return choice
	}
	choice.ID, choice.Name, choice.Chosen = stored.WorkspaceID, stored.WorkspaceName, true
	printInfo("Signing in to the stored workspace %s. Run 'modernpath auth --choose-workspace' to choose another.\n", workspaceStatusLine(stored))
	return choice
}

// signIn runs one login, carrying the organization filter when a workspace
// was chosen. A failure is printed and returned: a script must be able to
// stop on it, and the previous credential file is left as it was.
func signIn(profile zitadel.Profile, label string, choice workspaceChoice) (*zitadel.Token, error) {
	opts := loginOptions()
	opts.OrganizationID = choice.ID
	if choice.Chosen {
		printInfo("Signing in (%s) to workspace %s...\n", label, choice.ID)
	} else {
		printInfo("Signing in (%s)...\n", label)
	}
	tok, err := zitadelLogin(context.Background(), profile, os.Stdout, opts)
	if err != nil {
		printError("Sign-in failed: %v\n", err)
		return nil, fmt.Errorf("sign-in failed: %w", err)
	}
	return tok, nil
}

// chooseWorkspace decides the workspace after an unfiltered sign-in. One
// workspace, or none, needs nothing more. Several: the browser flow asks in
// a terminal and refuses without one. The device flow — asked for or fallen
// back to — keeps the workspace the unfiltered sign-in landed in (home, for
// most people) without listing any, since its token cannot list them
// (USER:2026-09-09), and says how to reach another. A choice other than the
// landed workspace runs a second sign-in carrying it.
func chooseWorkspace(profile zitadel.Profile, label string, tok *zitadel.Token) (*zitadel.Token, workspaceChoice, error) {
	claims, readable := zitadel.TokenClaims(tok.AccessToken)
	choice := workspaceChoice{ID: claims.OrganizationID, Issuer: profile.Issuer}
	if readable && claims.OrganizationID == "" {
		// No workspace to confirm a choice against (see saveTokens): the
		// picker would only lead to a refusal, so say why and keep the
		// credential as the identity service issued it.
		printWarning("The sign-in token names no workspace. No workspace is stored.\n")
		return tok, choice, nil
	}
	if tok.Flow == zitadel.FlowDevice {
		printInfo("Signed in to workspace %s. The device flow cannot list workspaces. To use another one, run 'modernpath auth --device-flow --workspace <id>'.\n", claims.OrganizationID)
		return tok, choice, nil
	}

	list, err := listOrganizationsFn(context.Background(), profile, tok.AccessToken)
	if err != nil {
		printWarning("Could not list your workspaces: %v. Signed in to workspace %s. To use another one, run 'modernpath auth --workspace <id>'.\n", err, claims.OrganizationID)
		return tok, choice, nil
	}
	orgs := zitadel.SortOrganizations(list.Organizations, claims.HomeOrganizationID)
	if list.Truncated {
		printWarning("You have access to %d workspaces. Only the first %d are listed.\n", list.Total, len(orgs))
	}
	if own, listed := findWorkspace(orgs, claims.OrganizationID); listed {
		choice.Name = own.Name
	}

	switch {
	case len(orgs) == 0:
		printInfo("No workspaces are listed for you. Signed in to workspace %s.\n", claims.OrganizationID)
		return tok, choice, nil
	case len(orgs) == 1 && !chooseWorkspaceFlag:
		return tok, choice, nil
	case !stdinIsTerminal():
		return nil, choice, fmt.Errorf("you have access to %d workspaces. Choose one with --workspace <id>:\n%s", len(orgs), workspaceListing(orgs, claims.HomeOrganizationID))
	}

	picked, err := pickWorkspaceFn(orgs, claims.HomeOrganizationID, claims.OrganizationID)
	if err != nil {
		return nil, choice, fmt.Errorf("no workspace chosen: %w", err)
	}
	choice = workspaceChoice{ID: picked.ID, Name: picked.Name, Issuer: profile.Issuer, Chosen: true}
	if picked.ID == claims.OrganizationID {
		return tok, choice, nil
	}
	second, err := signIn(profile, label, choice)
	if err != nil {
		return nil, choice, err
	}
	return second, choice, nil
}

// workspaceListing names every workspace for a person who must choose one
// with --workspace.
func workspaceListing(orgs []zitadel.Organization, homeID string) string {
	var b strings.Builder
	for _, org := range orgs {
		fmt.Fprintf(&b, "  %s  --workspace %s", workspaceLabel(org), org.ID)
		if org.ID == homeID {
			b.WriteString("  (home)")
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func findWorkspace(orgs []zitadel.Organization, id string) (zitadel.Organization, bool) {
	for _, org := range orgs {
		if org.ID == id {
			return org, true
		}
	}
	return zitadel.Organization{}, false
}

func authenticateWithManualToken(baseURL string) error {
	fmt.Print("Enter API token: ")

	var authToken string
	if term.IsTerminal(int(syscall.Stdin)) {
		tokenBytes, err := term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			return err
		}
		fmt.Println()
		authToken = string(tokenBytes)
	} else {
		reader := bufio.NewReader(os.Stdin)
		authToken, _ = reader.ReadString('\n')
		authToken = strings.TrimSpace(authToken)
	}

	if authToken == "" {
		printInfo("No token given. Continuing without authentication.\n")
		return nil
	}

	return authenticateWithToken(baseURL, authToken)
}

func authenticateWithToken(baseURL, authToken string) error {
	printInfo("Validating token...\n")

	client := api.NewClient(baseURL, authToken)
	systems, err := client.ListSystems()
	if err != nil {
		printError("Token validation failed: %v\n", err)
		return nil
	}

	// REQ-CROSS-282: reuses the validation call above — zero extra network
	// traffic. Only when a system_id is already configured; a fresh, unbound
	// workspace (SystemID == 0) has nothing to check yet.
	if cfg, cerr := config.ReadConfig(); cerr == nil && cfg.SystemID != 0 && !systemReachable(systems, cfg.SystemID) {
		printWarning("%s\n", systemMismatchMessage(cfg.SystemID, systems))
	}

	return saveTokens(baseURL, authToken, "", workspaceChoice{})
}

// warnIfConfiguredSystemUnreachable runs after a successful auth save for the
// SSO path (REQ-CROSS-282) — the one with no earlier ListSystems() call to
// reuse. Fails open: a check that itself errors prints nothing, since
// "couldn't check" is never "confirmed unreachable."
func warnIfConfiguredSystemUnreachable(apiURL string) {
	cfg, err := config.ReadConfig()
	if err != nil || cfg.SystemID == 0 {
		return
	}
	systems, err := listSystemsFn(apiURL, "")
	if err != nil {
		return
	}
	if !systemReachable(systems, cfg.SystemID) {
		printWarning("%s\n", systemMismatchMessage(cfg.SystemID, systems))
	}
}

// workspaceChoice is what a sign-in decided about the workspace: the id and
// name it was meant to land in (Chosen when a person or a stored choice named
// one) and the issuer of the sign-in.
type workspaceChoice struct {
	ID     string
	Name   string
	Issuer string
	Chosen bool
}

// saveTokens writes the credential file whole — a sign-in without a
// workspace, and a logout, clear a stored one — after deciding what it says
// about the workspace (REQ-CROSS-337). The token's platform claim is read
// without verification: ZITADEL degrades a filter naming a workspace the
// person is not granted to their home organization, so a sign-in that
// "chose" a workspace may have landed elsewhere, and storing that credential
// would run every following command against the wrong workspace silently.
// A chosen workspace the token does not confirm is refused and nothing is
// written; the previous credential file stays as it was.
// saveLogin stores a fresh login result: the tokens, and the identity and
// lifetime the response carried (REQ-CROSS-388).
func saveLogin(apiURL string, tok *zitadel.Token, ws workspaceChoice) error {
	var actor string
	if id, ok := zitadel.TokenClaims(tok.IDToken); ok {
		actor = id.Email
		if actor == "" {
			actor = id.Subject
		}
	}
	return saveTokensWith(apiURL, tok.AccessToken, tok.RefreshToken, actor, tok.Expiry, ws)
}

// saveTokens stores a credential with only what the access token itself
// says about its holder and lifetime — the `auth --token` path and older
// callers; a login goes through saveLogin, which knows more.
func saveTokens(apiURL, accessToken, refreshToken string, ws workspaceChoice) error {
	return saveTokensWith(apiURL, accessToken, refreshToken, "", time.Time{}, ws)
}

func saveTokensWith(apiURL, accessToken, refreshToken, actor string, expiry time.Time, ws workspaceChoice) error {
	auth := &config.Auth{
		Token:        accessToken,
		RefreshToken: refreshToken,
	}

	claims, readable := zitadel.TokenClaims(accessToken)
	// The actor and the expiry are recorded once here so every later read —
	// `auth status`, the pre-call expiry check — needs no claim parsing. The
	// ID token's identity wins; the access token's own sub/email/exp are the
	// fallback for a credential that arrived without one.
	if actor == "" && readable {
		actor = claims.Email
		if actor == "" {
			actor = claims.Subject
		}
	}
	if expiry.IsZero() && readable {
		expiry = claims.ExpiresAt
	}
	auth.Actor = actor
	if !expiry.IsZero() {
		auth.ExpiresAt = expiry.UTC().Format(time.RFC3339)
	}
	switch {
	case ws.Chosen && !readable:
		return fmt.Errorf("could not read a workspace from the sign-in token, so workspace %s cannot be confirmed. Nothing was stored", ws.ID)
	case ws.Chosen && claims.OrganizationID == "":
		// The platform claim is there (a JWT from the identity service) but
		// carries no org_id: the identity service predates workspace-scoped
		// tokens. Not an access problem, and not something another sign-in
		// fixes — say so instead of printing a blank id.
		return fmt.Errorf("the sign-in token names no workspace, so workspace %s cannot be confirmed. The identity service does not issue workspace-scoped tokens yet. Nothing was stored", ws.ID)
	case ws.Chosen && claims.OrganizationID != ws.ID:
		return fmt.Errorf("signed in to workspace %s rather than %s. You may not have access to it. Nothing was stored", claims.OrganizationID, ws.ID)
	case readable && claims.OrganizationID != "":
		auth.WorkspaceID = claims.OrganizationID
		if ws.Name != "" && (ws.ID == "" || ws.ID == claims.OrganizationID) {
			auth.WorkspaceName = ws.Name
		}
		auth.Issuer = ws.Issuer
		if auth.Issuer == "" {
			auth.Issuer = claims.Issuer
		}
	}

	// A sign-in replaces the active credential, not the credentials stashed
	// for the other identity providers `env --set` switches back to
	// (REQ-CROSS-391); only the slot this sign-in fills is dropped.
	if prev, err := config.ReadAuth(); err == nil && prev != nil && len(prev.Stash) > 0 {
		auth.Stash = prev.Stash
		delete(auth.Stash, auth.Issuer)
		delete(auth.Stash, apiURL)
		if issuer, ok := issuerFor(apiURL); ok {
			delete(auth.Stash, issuer)
		}
		if len(auth.Stash) == 0 {
			auth.Stash = nil
		}
	}

	if err := config.WriteAuth(auth); err != nil {
		printError("Could not save credentials: %v\n", err)
		return err
	}

	// Save the API URL to config so subsequent commands use it
	cfg, _ := config.ReadConfig()
	if cfg == nil {
		cfg = &config.Config{}
	}
	cfg.APIURL = apiURL
	if err := config.WriteConfig(cfg); err != nil {
		printWarning("Could not save the API URL to config: %v\n", err)
	}

	printSuccess("Sign-in successful.\n")
	printInfo("Credentials saved to .modernpath/auth.json\n")
	printInfo("API URL saved: %s\n", apiURL)
	if line := workspaceStatusLine(auth); line != "" {
		printInfo("Workspace: %s\n", line)
	}

	return nil
}

func doLogout() error {
	configDir, err := config.FindConfigDir()
	if err != nil || configDir == "" {
		printInfo("Not signed in.\n")
		return nil
	}

	// Remove auth file
	auth := &config.Auth{}
	if err := config.WriteAuth(auth); err != nil {
		printWarning("Could not remove credentials: %v\n", err)
	}

	printSuccess("Signed out.\n")
	return nil
}
