package cmd

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/manifoldco/promptui"
	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/platform"
	"github.com/modernpath/cli/internal/zitadel"
	"github.com/spf13/cobra"
)

var (
	envSet string
)

// knownEnvironment is a named ModernPath server `modernpath env` can switch to.
type knownEnvironment struct {
	Name       string
	URL        string
	Desc       string
	Deprecated bool
}

// knownEnvironments is the embedded set the CLI offers by name. The cloud URLs
// are the same baked-in Zitadel profile hosts auth (`--sso` / `--sso --test`)
// and the platform layer use, so there is one source of truth for them and the
// env names line up with the auth flags. Beta is retained as an explicitly
// deprecated target during the beta→cloud migration rather than dropped, so a
// workspace still pointed at it is named rather than shown as "custom".
func knownEnvironments() []knownEnvironment {
	return []knownEnvironment{
		{"production", zitadel.ProdProfile.APIURL, "ModernPath production (cloud)", false},
		{"test", zitadel.TestProfile.APIURL, "ModernPath test-plat (cloud)", false},
		{"local", config.LocalAPIURL, "Local development server", false},
		{"beta", config.BetaAPIURL, "Legacy beta — being decommissioned", true},
	}
}

// environmentName maps a URL back to its known environment name, or "custom".
// One mapping shared by `env` and `status` so the two never disagree.
func environmentName(url string) string {
	for _, e := range knownEnvironments() {
		if url == e.URL {
			return e.Name
		}
	}
	return "custom"
}

// environmentURL resolves an environment name (and its aliases) to a URL. ok is
// false for a name that is not a known environment, so the caller can fall back
// to treating the argument as a raw URL.
func environmentURL(name string) (string, bool) {
	switch name {
	case "production", "prod":
		return zitadel.ProdProfile.APIURL, true
	case "test", "test-plat":
		return zitadel.TestProfile.APIURL, true
	case "local", "localhost", "dev":
		return config.LocalAPIURL, true
	case "beta":
		return config.BetaAPIURL, true
	}
	return "", false
}

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage ModernPath environment settings",
	Long: `View and manage which ModernPath environment the CLI connects to.

Available environments:
  production  - https://api.modernpath.ai (cloud, default)
  test        - https://api.workload.test-plat.modernpath.ai (cloud test-plat)
  local       - http://localhost:4000
  beta        - https://beta.modernpath.ai (legacy, being decommissioned)

Examples:
  modernpath env                    # Show current environment
  modernpath env --set=production   # Switch to cloud production
  modernpath env --set=test         # Switch to cloud test-plat
  modernpath env --set=local        # Switch to local development
  modernpath env --set=custom       # Set a custom URL interactively`,
	RunE: runEnv,
}

var envListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available environments",
	RunE:  runEnvList,
}

var envTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Test connection to current environment",
	RunE:  runEnvTest,
}

func init() {
	envCmd.Flags().StringVar(&envSet, "set", "", "Set environment: production, test, local, beta, custom, or a URL")
	envCmd.AddCommand(envListCmd)
	envCmd.AddCommand(envTestCmd)
	rootCmd.AddCommand(envCmd)
}

func runEnv(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		cfg = &config.Config{APIURL: config.DefaultAPIURL}
	}

	// If --set flag provided, update the environment
	if envSet != "" {
		return setEnvironment(cfg, envSet)
	}

	// Show current environment status
	displayEnvironmentStatus(cfg)
	return nil
}

func runEnvList(cmd *cobra.Command, args []string) error {
	cfg, _ := config.ReadConfig()
	if cfg == nil {
		cfg = &config.Config{APIURL: config.DefaultAPIURL}
	}

	bold := color.New(color.Bold)
	green := color.New(color.FgGreen)
	cyan := color.New(color.FgCyan)

	fmt.Println()
	bold.Println("Available Environments")
	fmt.Println("─────────────────────────────────────────")

	// An empty binding falls back to the default (production), so mark that row
	// current too — the same rule ReadConfig applies.
	for _, env := range knownEnvironments() {
		current := cfg.APIURL == env.URL ||
			(env.URL == config.DefaultAPIURL && cfg.APIURL == "")
		name := env.Name
		if env.Deprecated {
			name += "  (deprecated)"
		}
		marker := "  "
		if current {
			marker = "→ "
			green.Printf("%s%s\n", marker, name)
		} else {
			fmt.Printf("%s%s\n", marker, name)
		}
		cyan.Printf("      URL: %s\n", env.URL)
		fmt.Printf("      %s\n", env.Desc)
	}

	// A URL that matches no known environment is a custom binding.
	if cfg.APIURL != "" && environmentName(cfg.APIURL) == "custom" {
		fmt.Println()
		green.Printf("→ custom\n")
		cyan.Printf("      URL: %s\n", cfg.APIURL)
		fmt.Printf("      Custom environment\n")
	}

	fmt.Println()
	printInfo("Use 'modernpath env --set=<name>' to switch environments.\n")
	return nil
}

func runEnvTest(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		cfg = &config.Config{APIURL: config.DefaultAPIURL}
	}

	apiURL := cfg.APIURL
	if apiURL == "" {
		apiURL = config.DefaultAPIURL
	}

	envName := environmentName(apiURL)

	fmt.Println()
	printInfo("Testing connection to %s (%s)...\n", envName, apiURL)

	client := &http.Client{Timeout: 10 * time.Second}
	healthReq, authorized, err := healthProbe(apiURL)
	if err != nil {
		return err
	}

	start := time.Now()
	resp, err := client.Do(healthReq)
	elapsed := time.Since(start)

	if err != nil {
		fmt.Println()
		printError("Connection failed: %v\n", err)
		fmt.Println()
		printInfo("Troubleshooting tips:\n")
		if apiURL == config.LocalAPIURL {
			fmt.Println("  • Make sure the local Phoenix server is running (mix phx.server)")
			fmt.Println("  • Check if port 4000 is available")
		} else {
			fmt.Println("  • Check your internet connection")
			fmt.Println("  • The production server might be temporarily unavailable")
		}
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Println()
		printSuccess("Connected successfully!\n")
		fmt.Printf("  Response time: %dms\n", elapsed.Milliseconds())
		fmt.Printf("  Status: %s\n", resp.Status)
	} else {
		fmt.Println()
		printWarning("Server responded but health check failed\n")
		fmt.Printf("  Status: %s\n", resp.Status)
		fmt.Print(unauthenticatedProbeHint(resp.StatusCode, authorized))
	}

	// Test authentication if we have a token
	auth, _ := config.ReadAuth()
	if auth != nil && auth.Token != "" {
		fmt.Println()
		printInfo("Testing authentication...\n")

		req, _ := http.NewRequest("GET", apiURL+"/api/systems", nil)
		platform.Prepare(req)
		if err := platform.Authorize(req, auth.Token); err != nil {
			// A local refusal is the answer to "is my authentication working" —
			// report it and stop, rather than reporting a connection error for a
			// request that was never sent.
			printWarning("%v\n", err)
			return nil
		}
		req.Header.Set("Accept", "application/json")

		authResp, err := client.Do(req)
		if err != nil {
			printWarning("Auth test failed: %v\n", err)
		} else {
			defer authResp.Body.Close()
			if authResp.StatusCode == http.StatusOK {
				printSuccess("Authentication valid!\n")
			} else if authResp.StatusCode == http.StatusUnauthorized {
				printWarning("Token expired or invalid. Run 'modernpath auth' to re-authenticate.\n")
			} else {
				printWarning("Auth check returned: %s\n", authResp.Status)
			}
		}
	} else {
		fmt.Println()
		printInfo("No authentication token found. Run 'modernpath auth' to login.\n")
	}

	fmt.Println()
	return nil
}

// healthProbe builds the request that answers "is this server reachable and
// healthy". Both `env` and `env test` ask that question, and both used to
// build it themselves — which is how each acquired a different wrong answer
// on a platform API host (REQ-CROSS-290): one addressed `/_health`, off the
// prefix the Gateway routes to core, and neither sent the stored bearer to an
// edge that authenticates every route it fronts.
//
// The returned bool reports whether a credential was attached, so a caller
// can tell "the server rejected my token" from "I had no token to send".
// A token the project-audience pre-check refuses is left off rather than
// failing the probe: reachability is a separate question from credential
// validity, and `env test` reports the refusal on its own line.
func healthProbe(apiURL string) (*http.Request, bool, error) {
	req, err := http.NewRequest("GET", apiURL+platform.HealthPath(apiURL), nil)
	if err != nil {
		return nil, false, err
	}
	platform.Prepare(req)

	auth, _ := config.ReadAuth()
	if auth == nil || auth.Token == "" {
		return req, false, nil
	}
	if err := platform.Authorize(req, auth.Token); err != nil {
		return req, false, nil
	}
	return req, true, nil
}

// unauthenticatedProbeHint explains a 401 that the probe itself caused, so the
// reader is not sent to debug a server that is answering correctly.
func unauthenticatedProbeHint(status int, authorized bool) string {
	if status != http.StatusUnauthorized || authorized {
		return ""
	}
	return "  This host authenticates its health check; no usable credential was sent.\n" +
		"  Run 'modernpath auth' and try again.\n"
}

func setEnvironment(cfg *config.Config, env string) error {
	var newURL string

	switch env {
	case "custom":
		// Interactive custom URL input
		prompt := promptui.Prompt{
			Label:   "Enter custom API URL",
			Default: cfg.APIURL,
		}
		result, err := prompt.Run()
		if err != nil {
			return err
		}
		newURL = result
	default:
		if url, ok := environmentURL(env); ok {
			newURL = url
		} else if strings.HasPrefix(env, "http://") || strings.HasPrefix(env, "https://") {
			// A full URL is accepted directly.
			newURL = env
		} else {
			printError("Unknown environment: %s\n", env)
			printInfo("Available environments: production, test, local, beta, custom\n")
			printInfo("Or provide a full URL: --set=https://your-server.com\n")
			return nil
		}
	}

	// Update config
	cfg.APIURL = newURL
	if err := config.WriteConfig(cfg); err != nil {
		printError("Failed to save config: %v\n", err)
		return err
	}

	envName := environmentName(newURL)
	printSuccess("Environment set to %s\n", envName)
	fmt.Printf("  URL: %s\n", newURL)
	fmt.Println()
	printInfo("Run 'modernpath env test' to verify the connection.\n")

	return nil
}

func displayEnvironmentStatus(cfg *config.Config) {
	apiURL := cfg.APIURL
	if apiURL == "" {
		apiURL = config.DefaultAPIURL
	}

	envName := environmentName(apiURL)

	bold := color.New(color.Bold)
	cyan := color.New(color.FgCyan)

	fmt.Println()
	bold.Println("Current Environment")
	fmt.Println("─────────────────────────────────────────")

	cyan.Printf("  Name: ")
	fmt.Printf("%s\n", envName)

	cyan.Printf("  URL:  ")
	fmt.Printf("%s\n", apiURL)

	// Check connection status
	client := &http.Client{Timeout: 5 * time.Second}
	req, authorized, err := healthProbe(apiURL)
	cyan.Printf("  Status: ")
	switch {
	case err != nil:
		color.Red("Unreachable")
	default:
		resp, err := client.Do(req)
		if err != nil {
			color.Red("Unreachable")
			break
		}
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusOK:
			color.Green("Connected")
		case resp.StatusCode == http.StatusUnauthorized && !authorized:
			color.Yellow("Unauthenticated (run 'modernpath auth')")
		default:
			color.Yellow("Degraded (%s)", resp.Status)
		}
	}
	fmt.Println()

	fmt.Println()
	printInfo("Commands:\n")
	fmt.Println("  modernpath env list      - Show all environments")
	fmt.Println("  modernpath env test      - Test current connection")
	fmt.Println("  modernpath env --set=X   - Switch environment")
	fmt.Println()
}
