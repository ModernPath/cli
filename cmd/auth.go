package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	token       string
	logout      bool
	authLocal   bool
	ssoFlag     bool
	ssoTestFlag bool
)

// zitadelLogin is the RFC 8628 device-flow entry point authenticateWithSSO
// calls. It is a package var (rather than a direct call to zitadel.Login) so
// tests can stub it — the baked-in profile itself (SelectProfile) is never
// stubbed, only the network round trip.
var zitadelLogin = zitadel.Login

// openBrowserFn is openBrowser as a package var (REQ-CROSS-282), mirroring
// zitadelLogin above — so a test can drive authenticateWithBrowser's success
// path without spawning a real OS browser process.
var openBrowserFn = openBrowser

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authenticate with ModernPath platform",
	// `auth logout` used to be silently ignored — and then STARTED A LOGIN
	// (REQ-CROSS-210 D22, RUN:2026-08-18). Positionals are errors that point
	// at the flag form.
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return nil
		}
		if strings.EqualFold(args[0], "logout") {
			return fmt.Errorf("auth takes no arguments — use 'modernpath auth --logout'")
		}
		return fmt.Errorf("auth takes no arguments (got %q)", args[0])
	},
	Long: `Authenticate with the ModernPath platform to access private systems.

By default, uses the environment set via 'modernpath env --set=<env>'.
Use --local or --api-url to override.

Examples:
  modernpath auth                    # Login using current environment
  modernpath auth --local            # Login to localhost:4000
  modernpath auth --api-url=<url>    # Login to specific URL
  modernpath auth --token=<token>    # Provide token directly
  modernpath auth --logout           # Remove stored credentials
  modernpath auth --sso              # Zitadel device-flow login (prod)
  modernpath auth --sso --test       # Zitadel device-flow login (test)`,
	RunE: runAuth,
}

func init() {
	authCmd.Flags().StringVar(&token, "token", "", "API token")
	authCmd.Flags().BoolVar(&logout, "logout", false, "Remove stored credentials")
	authCmd.Flags().BoolVar(&authLocal, "local", false, "Use local server (localhost:4000) instead of cloud production")
	authCmd.Flags().BoolVar(&ssoFlag, "sso", false, "Authenticate via your organization's Zitadel identity (device flow)")
	authCmd.Flags().BoolVar(&ssoTestFlag, "test", false, "With --sso, target the test Zitadel profile instead of prod")
}

func runAuth(cmd *cobra.Command, args []string) error {
	if ssoTestFlag && !ssoFlag {
		return fmt.Errorf("--test only modifies --sso — use 'modernpath auth --sso --test'")
	}

	if logout {
		return doLogout()
	}

	// A dedicated Zitadel device-flow path, gated behind --sso: it never
	// touches the interactive/manual-token flow below (REQ-CROSS-235).
	if ssoFlag {
		return authenticateWithSSO(ssoTestFlag)
	}

	// Get API URL - respects environment setting from 'modernpath env'
	// Priority: --api-url flag > --local flag > config > default (production)
	baseURL := apiURL
	if baseURL == "" {
		if authLocal {
			baseURL = config.LocalAPIURL
		} else {
			// Read from config to respect 'modernpath env' setting
			cfg, err := config.ReadConfig()
			if err == nil && cfg.APIURL != "" {
				baseURL = cfg.APIURL
			} else {
				baseURL = config.DefaultAPIURL
			}
		}
	}

	// If token provided directly, use it
	if token != "" {
		return authenticateWithToken(baseURL, token)
	}

	// Interactive mode - offer choice between browser OAuth and manual token
	fmt.Println("ModernPath Authentication")
	fmt.Println("─────────────────────────────────────────")
	fmt.Printf("API URL: %s\n\n", baseURL)

	fmt.Println("Choose authentication method:")
	fmt.Println("  1. Browser login (recommended)")
	fmt.Println("  2. Enter token manually")
	fmt.Println("  3. Cancel")
	fmt.Print("\nChoice [1]: ")

	reader := bufio.NewReader(os.Stdin)
	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	switch choice {
	case "", "1":
		return authenticateWithBrowser(baseURL)
	case "2":
		return authenticateWithManualToken(baseURL)
	default:
		printInfo("Authentication cancelled.\n")
		return nil
	}
}

func authenticateWithBrowser(baseURL string) error {
	client := api.NewClient(baseURL, "")

	// Initiate device flow
	printInfo("Initiating authentication...\n")
	deviceFlow, err := client.InitiateDeviceFlow()
	if err != nil {
		printError("Failed to initiate authentication: %v\n", err)
		return nil
	}

	// Display user code and open browser
	fmt.Println()
	fmt.Println("─────────────────────────────────────────")
	fmt.Printf("Your code: %s\n", deviceFlow.UserCode)
	fmt.Println("─────────────────────────────────────────")
	fmt.Println()

	// Try to open browser
	printInfo("Opening browser...\n")
	if err := openBrowserFn(deviceFlow.VerificationURL); err != nil {
		printWarning("Could not open browser automatically.\n")
		fmt.Printf("Please open this URL in your browser:\n%s\n\n", deviceFlow.VerificationURL)
	}

	printInfo("Waiting for authentication in browser...\n")
	fmt.Println("(Press Ctrl+C to cancel)")
	fmt.Println()

	// Poll for completion
	maxAttempts := deviceFlow.ExpiresIn / 2 // Poll every 2 seconds
	for i := 0; i < maxAttempts; i++ {
		time.Sleep(2 * time.Second)

		result, err := client.PollDeviceFlow(deviceFlow.DeviceCode)
		if err != nil {
			printError("Error checking authentication: %v\n", err)
			continue
		}

		if result.Error == "authorization_pending" {
			// Still waiting
			fmt.Print(".")
			continue
		}

		if result.Error == "expired" {
			fmt.Println()
			printError("Authentication expired. Please try again.\n")
			return nil
		}

		if result.Error != "" {
			fmt.Println()
			printError("Authentication failed: %s\n", result.Error)
			return nil
		}

		// Success!
		fmt.Println()
		if err := saveTokens(baseURL, result.AccessToken, result.RefreshToken); err != nil {
			return err
		}
		warnIfConfiguredSystemUnreachable(baseURL)
		return nil
	}

	fmt.Println()
	printError("Authentication timed out. Please try again.\n")
	return nil
}

// authenticateWithSSO runs RFC 8628 device-flow login against the baked-in
// Zitadel profile selected by testProfile (REQ-CROSS-235). A failure is
// reported like the legacy flow's own failures (printed, not returned as a
// command error) and writes no credential file; success persists through the
// same saveTokens the legacy flow uses.
func authenticateWithSSO(testProfile bool) error {
	profile := zitadel.SelectProfile(testProfile)
	label := "prod"
	if testProfile {
		label = "test"
	}

	printInfo("Initiating SSO authentication (%s)...\n", label)
	tok, err := zitadelLogin(context.Background(), profile, os.Stdout)
	if err != nil {
		printError("SSO authentication failed: %v\n", err)
		return nil
	}

	if err := saveTokens(profile.APIURL, tok.AccessToken, tok.RefreshToken); err != nil {
		return err
	}
	warnIfConfiguredSystemUnreachable(profile.APIURL)
	return nil
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
		printInfo("No token provided. Continuing without authentication.\n")
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

	return saveTokens(baseURL, authToken, "")
}

// warnIfConfiguredSystemUnreachable runs after a successful auth save for the
// SSO and browser paths (REQ-CROSS-282) — the two with no earlier
// ListSystems() call to reuse. Fails open: a check that itself errors prints
// nothing, since "couldn't check" is never "confirmed unreachable."
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

func saveTokens(apiURL, accessToken, refreshToken string) error {
	auth := &config.Auth{
		Token:        accessToken,
		RefreshToken: refreshToken,
	}

	if err := config.WriteAuth(auth); err != nil {
		printError("Failed to save credentials: %v\n", err)
		return err
	}

	// Save the API URL to config so subsequent commands use it
	cfg, _ := config.ReadConfig()
	if cfg == nil {
		cfg = &config.Config{}
	}
	cfg.APIURL = apiURL
	if err := config.WriteConfig(cfg); err != nil {
		printWarning("Failed to save API URL to config: %v\n", err)
	}

	printSuccess("Authentication successful!\n")
	printInfo("Credentials saved to .modernpath/auth.json\n")
	printInfo("API URL saved: %s\n", apiURL)

	return nil
}

func openBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("unsupported platform")
	}

	return cmd.Start()
}

func doLogout() error {
	configDir, err := config.FindConfigDir()
	if err != nil || configDir == "" {
		printInfo("Not logged in\n")
		return nil
	}

	// Remove auth file
	auth := &config.Auth{}
	if err := config.WriteAuth(auth); err != nil {
		printWarning("Failed to clear credentials: %v\n", err)
	}

	printSuccess("Logged out successfully\n")
	return nil
}
