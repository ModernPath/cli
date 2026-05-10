package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	token     string
	logout    bool
	authLocal bool
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authenticate with ModernPath platform",
	Long: `Authenticate with the ModernPath platform to access private systems.

By default, uses the environment set via 'modernpath env --set=<env>'.
Use --local or --api-url to override.

Examples:
  modernpath auth                    # Login using current environment
  modernpath auth --local            # Login to localhost:4000
  modernpath auth --api-url=<url>    # Login to specific URL
  modernpath auth --token=<token>    # Provide token directly
  modernpath auth --logout           # Remove stored credentials`,
	RunE: runAuth,
}

func init() {
	authCmd.Flags().StringVar(&token, "token", "", "API token")
	authCmd.Flags().BoolVar(&logout, "logout", false, "Remove stored credentials")
	authCmd.Flags().BoolVar(&authLocal, "local", false, "Use local server (localhost:4000) instead of beta.modernpath.ai")
}

func runAuth(cmd *cobra.Command, args []string) error {
	if logout {
		return doLogout()
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
	if err := openBrowser(deviceFlow.VerificationURL); err != nil {
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
		return saveTokens(baseURL, result.AccessToken, result.RefreshToken)
	}

	fmt.Println()
	printError("Authentication timed out. Please try again.\n")
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
	_, err := client.ListSystems()
	if err != nil {
		printError("Token validation failed: %v\n", err)
		return nil
	}

	return saveTokens(baseURL, authToken, "")
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
