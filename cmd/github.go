package cmd

import (
	"fmt"

	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

const (
	// GitHub App installation URLs per environment
	githubAppURLProd = "https://github.com/apps/modernpath/installations/new"
	githubAppURLDev  = "https://github.com/apps/modernpath/installations/new"
)

var githubCmd = &cobra.Command{
	Use:   "github",
	Short: "Connect your GitHub organization to ModernPath",
	Long: `Opens the GitHub App installation page in your browser.

An organization admin selects the org and grants repository access.
A workspace is automatically created when the installation completes.

Examples:
  modernpath github          # Install production GitHub App
  modernpath github --local  # Install dev GitHub App (for local development)`,
	RunE: runGithub,
}

var githubLocal bool

func init() {
	githubCmd.Flags().BoolVar(&githubLocal, "local", false, "Use development GitHub App instead of production")
	rootCmd.AddCommand(githubCmd)
}

func runGithub(cmd *cobra.Command, args []string) error {
	installURL := githubAppURLProd

	if githubLocal {
		installURL = githubAppURLDev
	} else {
		// Check if current environment points to localhost
		cfg, err := config.ReadConfig()
		if err == nil && cfg.APIURL == config.LocalAPIURL {
			installURL = githubAppURLDev
		}
	}

	fmt.Println("ModernPath GitHub Setup")
	fmt.Println("─────────────────────────────────────────")
	fmt.Println()
	fmt.Println("This will open the GitHub App installation page.")
	fmt.Println("An organization admin should:")
	fmt.Println("  1. Select the organization to connect")
	fmt.Println("  2. Choose which repositories to grant access to")
	fmt.Println("  3. Approve the installation")
	fmt.Println()
	fmt.Println("A ModernPath workspace will be created automatically.")
	fmt.Println()

	printInfo("Opening browser...\n")
	if err := openBrowser(installURL); err != nil {
		printWarning("Could not open browser automatically.\n")
		fmt.Printf("Please open this URL manually:\n%s\n", installURL)
		return nil
	}

	printSuccess("Browser opened. Complete the installation on GitHub.\n")
	printInfo("After installation, authenticate with: modernpath auth\n")

	return nil
}
