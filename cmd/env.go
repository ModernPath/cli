package cmd

import (
	"fmt"
	"net/http"
	"time"

	"github.com/fatih/color"
	"github.com/manifoldco/promptui"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

var (
	envSet string
)

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage ModernPath environment settings",
	Long: `View and manage which ModernPath environment (local or production) the CLI connects to.

Available environments:
  production  - https://beta.modernpath.ai (default)
  local       - http://localhost:4000

Examples:
  modernpath env                    # Show current environment
  modernpath env --set=production   # Switch to production
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
	envCmd.Flags().StringVar(&envSet, "set", "", "Set environment: production, local, or custom")
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

	envs := []struct {
		name    string
		url     string
		desc    string
		current bool
	}{
		{
			name:    "production",
			url:     config.DefaultAPIURL,
			desc:    "ModernPath production server",
			current: cfg.APIURL == config.DefaultAPIURL || cfg.APIURL == "",
		},
		{
			name:    "local",
			url:     config.LocalAPIURL,
			desc:    "Local development server",
			current: cfg.APIURL == config.LocalAPIURL,
		},
	}

	for _, env := range envs {
		marker := "  "
		if env.current {
			marker = "→ "
			green.Printf("%s%s\n", marker, env.name)
		} else {
			fmt.Printf("%s%s\n", marker, env.name)
		}
		cyan.Printf("      URL: %s\n", env.url)
		fmt.Printf("      %s\n", env.desc)
	}

	// Check if using a custom URL
	isCustom := cfg.APIURL != "" &&
		cfg.APIURL != config.DefaultAPIURL &&
		cfg.APIURL != config.LocalAPIURL

	if isCustom {
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

	envName := getEnvironmentName(apiURL)

	fmt.Println()
	printInfo("Testing connection to %s (%s)...\n", envName, apiURL)

	client := &http.Client{Timeout: 10 * time.Second}
	start := time.Now()
	resp, err := client.Get(apiURL + "/_health")
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
	}

	// Test authentication if we have a token
	auth, _ := config.ReadAuth()
	if auth != nil && auth.Token != "" {
		fmt.Println()
		printInfo("Testing authentication...\n")

		req, _ := http.NewRequest("GET", apiURL+"/api/systems", nil)
		req.Header.Set("Authorization", "Bearer "+auth.Token)
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

func setEnvironment(cfg *config.Config, env string) error {
	var newURL string

	switch env {
	case "production", "prod":
		newURL = config.DefaultAPIURL
	case "local", "localhost", "dev":
		newURL = config.LocalAPIURL
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
		// Check if it's a URL directly
		if len(env) > 4 && (env[:4] == "http" || env[:5] == "https") {
			newURL = env
		} else {
			printError("Unknown environment: %s\n", env)
			printInfo("Available environments: production, local, custom\n")
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

	envName := getEnvironmentName(newURL)
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

	envName := getEnvironmentName(apiURL)

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
	resp, err := client.Get(apiURL + "/_health")
	cyan.Printf("  Status: ")
	if err != nil {
		color.Red("Unreachable")
	} else {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			color.Green("Connected")
		} else {
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

func getEnvironmentName(url string) string {
	switch url {
	case config.DefaultAPIURL:
		return "production"
	case config.LocalAPIURL:
		return "local"
	default:
		return "custom"
	}
}
