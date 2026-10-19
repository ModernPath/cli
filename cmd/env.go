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
	envSet      string
	envJSON     bool
	envListJSON bool
	envTestJSON bool
)

// knownEnvironment is a named ModernPath server `modernpath env` can switch to.
type knownEnvironment struct {
	Name string
	URL  string
	Desc string
}

// knownEnvironments is the embedded set the CLI offers by name. The cloud URLs
// are the same baked-in Zitadel profile hosts auth (`--sso` / `--sso --test`)
// and the platform layer use, so there is one source of truth for them and the
// env names line up with the auth flags. Any other URL shows as "custom".
func knownEnvironments() []knownEnvironment {
	return []knownEnvironment{
		{"production", zitadel.ProdProfile.APIURL, "ModernPath production (cloud)"},
		{"test", zitadel.TestProfile.APIURL, "ModernPath test-plat (cloud)"},
		{"local", config.LocalAPIURL, "Local development server"},
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

--set switches the whole binding: the current system binding is kept under
its environment in config.json and the target's restored; the current
credential is kept under its identity provider in auth.json and the one for
the target's provider restored. A target with nothing kept is left unbound
(run 'modernpath init') or signed out (run the sign-in command it names) —
never an interactive prompt. A restored credential this server will not
accept is reported at the switch, not at the next write.

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
	envCmd.Flags().StringVar(&envSet, "set", "", "Set environment: production, test, local, custom, or a URL")
	envCmd.Flags().BoolVar(&envJSON, "json", false, "Emit the environment result as JSON")
	envListCmd.Flags().BoolVar(&envListJSON, "json", false, "Emit available environments as JSON")
	envTestCmd.Flags().BoolVar(&envTestJSON, "json", false, "Emit the connection check as JSON")
	addRecordReadFlags(envCmd)
	addRecordReadFlags(envTestCmd)
	envCmd.AddCommand(envListCmd)
	envCmd.AddCommand(envTestCmd)
	rootCmd.AddCommand(envCmd)
}

func runEnv(cmd *cobra.Command, args []string) error {
	allowed := []string{"environment", "api_url", "workspace", "status", "http_status", "error"}
	if err := validateRecordSelection(cmd, allowed); err != nil {
		return err
	}
	cfg, err := config.ReadConfig()
	if err != nil {
		cfg = &config.Config{APIURL: config.DefaultAPIURL}
	}

	// If --set flag provided, update the environment
	if envSet != "" {
		if envJSON && envSet == "custom" {
			return fmt.Errorf("--set=custom requires a URL with --json; provide --set=https://your-server.example")
		}
		if err := setEnvironment(cfg, envSet); err != nil {
			return err
		}
		if envJSON {
			auth, _ := config.ReadAuth()
			return writeCommandJSON(cmd, map[string]any{
				"api_url": cfg.APIURL, "environment": environmentName(cfg.APIURL), "system_id": cfg.SystemID,
				"system_name": cfg.SystemName, "system_slug": cfg.SystemSlug,
				"repository_id": cfg.RepositoryID, "authenticated": auth != nil && auth.Token != "",
			})
		}
		return nil
	}

	if envJSON {
		result := environmentStatus(cfg)
		selected, err := selectRecordFields(cmd, result, []string{"environment", "api_url", "status"}, allowed, []string{"api_url"})
		if err != nil {
			return err
		}
		return writeCommandJSON(cmd, selected)
	}
	full, _ := cmd.Flags().GetBool("full")
	if cmd.Flags().Changed("fields") || full {
		selected, err := selectRecordFields(cmd, environmentStatus(cfg), nil, allowed, nil)
		if err != nil {
			return err
		}
		return writeRecord(cmd, selected)
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
	if envListJSON {
		rows := make([]map[string]any, 0, len(knownEnvironments())+1)
		for _, env := range knownEnvironments() {
			current := cfg.APIURL == env.URL || (env.URL == config.DefaultAPIURL && cfg.APIURL == "")
			rows = append(rows, map[string]any{"name": env.Name, "api_url": env.URL, "description": env.Desc, "current": current})
		}
		if cfg.APIURL != "" && environmentName(cfg.APIURL) == "custom" {
			rows = append(rows, map[string]any{"name": "custom", "api_url": cfg.APIURL, "description": "Custom environment", "current": true})
		}
		return writeCommandJSON(cmd, rows)
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
	allowed := []string{"environment", "api_url", "connected", "http_status", "response_time_ms", "error", "authentication_checked", "authentication_status", "authentication_error", "authentication_http_status"}
	if err := validateRecordSelection(cmd, allowed); err != nil {
		return err
	}
	cfg, err := commandConfig()
	if err != nil {
		cfg = &config.Config{APIURL: config.DefaultAPIURL}
		if apiURL != "" {
			cfg.APIURL = apiURL
		}
	}

	apiURL := cfg.APIURL
	if apiURL == "" {
		apiURL = config.DefaultAPIURL
	}
	result := testEnvironment(apiURL)
	selected, err := selectRecordFields(cmd, result, []string{"environment", "api_url", "connected", "http_status", "response_time_ms", "authentication_checked", "authentication_status"}, allowed, []string{"connected"})
	if err != nil {
		return err
	}
	if err := writeRecord(cmd, selected); err != nil {
		return err
	}
	if result["connected"] != true {
		if reason, ok := result["error"].(string); ok {
			return fmt.Errorf("connection to %s failed: %s", apiURL, reason)
		}
		return fmt.Errorf("health check at %s failed: %v", apiURL, result["http_status"])
	}
	if result["authentication_checked"] == true && result["authentication_status"] != "valid" {
		return fmt.Errorf("authentication check failed (%v); run 'modernpath auth': %v", result["authentication_status"], result["authentication_error"])
	}
	return nil
}

func testEnvironment(apiURL string) map[string]any {
	result := map[string]any{"environment": environmentName(apiURL), "api_url": apiURL, "connected": false, "authentication_checked": false}
	client := &http.Client{Timeout: 10 * time.Second}
	healthReq, authorized, err := healthProbe(apiURL)
	if err != nil {
		result["error"] = err.Error()
		return result
	}
	start := time.Now()
	resp, err := client.Do(healthReq)
	result["response_time_ms"] = time.Since(start).Milliseconds()
	if err != nil {
		result["error"] = err.Error()
		return result
	}
	resp.Body.Close()
	result["http_status"] = resp.Status
	result["connected"] = resp.StatusCode == http.StatusOK

	auth, _ := config.ReadAuth()
	if auth == nil || auth.Token == "" {
		result["authentication_status"] = "not_configured"
		return result
	}
	result["authentication_checked"] = true
	req, err := http.NewRequest("GET", apiURL+"/api/systems", nil)
	if err != nil {
		result["authentication_status"] = "invalid_url"
		result["authentication_error"] = err.Error()
		return result
	}
	platform.Prepare(req)
	if err := platform.Authorize(req, auth.Token); err != nil {
		result["authentication_status"] = "refused_locally"
		result["authentication_error"] = err.Error()
		return result
	}
	req.Header.Set("Accept", "application/json")
	authResp, err := client.Do(req)
	if err != nil {
		result["authentication_status"] = "unreachable"
		result["authentication_error"] = err.Error()
		return result
	}
	defer authResp.Body.Close()
	switch authResp.StatusCode {
	case http.StatusOK:
		result["authentication_status"] = "valid"
	case http.StatusUnauthorized:
		result["authentication_status"] = "invalid"
	default:
		result["authentication_status"] = "degraded"
		result["authentication_http_status"] = authResp.Status
	}
	_ = authorized
	return result
}

func environmentStatus(cfg *config.Config) map[string]any {
	apiURL := cfg.APIURL
	if apiURL == "" {
		apiURL = config.DefaultAPIURL
	}
	result := map[string]any{"environment": environmentName(apiURL), "api_url": apiURL}
	if auth, err := config.ReadAuth(); err == nil {
		if line := workspaceStatusLine(auth); line != "" {
			result["workspace"] = line
		}
	}
	req, authorized, err := healthProbe(apiURL)
	if err != nil {
		result["status"] = "unreachable"
		result["error"] = err.Error()
		return result
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		result["status"] = "unreachable"
		result["error"] = err.Error()
		return result
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
		result["status"] = "connected"
	case resp.StatusCode == http.StatusUnauthorized && !authorized:
		result["status"] = "unauthenticated"
	default:
		result["status"] = "degraded"
		result["http_status"] = resp.Status
	}
	return result
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
			return fmt.Errorf("unknown environment %q; use production, test, local, custom, or a full URL", env)
		}
	}

	return switchEnvironment(cfg, newURL)
}

// switchEnvironment moves the workspace to newURL whole (REQ-CROSS-391): the
// current system binding is stashed under its environment and the target's
// restored, the current credential is stashed under its issuer and the one
// for the target's identity provider restored, so a switch never leaves the
// test system and a test-plane token under the production URL. A target
// with nothing stashed is left unbound or signed out and the remedy verb is
// named — never an interactive prompt, which an agent cannot answer (D6).
func switchEnvironment(cfg *config.Config, newURL string) error {
	auth, err := config.ReadAuth()
	if err != nil {
		// An unreadable auth.json is not "signed out": writing an empty
		// credential back would replace it. Refuse and name the file.
		return fmt.Errorf("cannot read .modernpath/auth.json (%v); fix or remove it before switching", err)
	}
	if auth == nil {
		auth = &config.Auth{}
	}
	oldURL := cfg.APIURL
	if oldURL == "" {
		oldURL = config.DefaultAPIURL
	}

	if cfg.SystemID > 0 {
		if cfg.Environments == nil {
			cfg.Environments = map[string]config.EnvBinding{}
		}
		cfg.Environments[bindingKey(oldURL)] = config.EnvBinding{
			SystemID: cfg.SystemID, SystemName: cfg.SystemName, SystemSlug: cfg.SystemSlug,
			RepositoryID: cfg.RepositoryID, CurrentRelease: cfg.CurrentRelease,
		}
	}
	if auth.Token != "" {
		if auth.Stash == nil {
			auth.Stash = map[string]config.AuthMaterial{}
		}
		auth.Stash[credentialKeyFor(auth, oldURL)] = config.AuthMaterial{
			Token: auth.Token, RefreshToken: auth.RefreshToken, ExpiresAt: auth.ExpiresAt,
			Actor: auth.Actor, WorkspaceID: auth.WorkspaceID, WorkspaceName: auth.WorkspaceName,
			Issuer: auth.Issuer,
		}
	}

	// The upload repository travels with its system (REQ-CROSS-503), so the
	// docs verbs and `source push` never aim at another environment's.
	cfg.APIURL = newURL
	if b, ok := cfg.Environments[bindingKey(newURL)]; ok {
		cfg.SystemID, cfg.SystemName, cfg.SystemSlug, cfg.RepositoryID, cfg.CurrentRelease = b.SystemID, b.SystemName, b.SystemSlug, b.RepositoryID, b.CurrentRelease
	} else {
		cfg.SystemID, cfg.SystemName, cfg.SystemSlug, cfg.RepositoryID, cfg.CurrentRelease = 0, "", "", 0, ""
	}

	envName := environmentName(newURL)
	var note string
	targetIssuer, known := issuerFor(newURL)
	if m, ok := auth.Stash[targetIssuer]; ok && known {
		activateCredential(auth, m, targetIssuer)
		delete(auth.Stash, targetIssuer)
	} else if m, ok := auth.Stash[newURL]; ok && !known {
		activateCredential(auth, m, "")
		delete(auth.Stash, newURL)
	} else if known {
		activateCredential(auth, config.AuthMaterial{}, "")
		note = fmt.Sprintf("not signed in for %s — run '%s'", envName, authRepairCommand(newURL))
	} else if auth.Token != "" {
		note = fmt.Sprintf("cannot tell which identity provider serves %s; the stored credential stays active", newURL)
	}

	if err := config.WriteConfig(cfg); err != nil {
		printError("Failed to save config: %v\n", err)
		return err
	}
	if err := config.WriteAuth(auth); err != nil {
		printError("Failed to save credentials: %v\n", err)
		return err
	}
	if envJSON {
		return nil
	}

	printSuccess("Environment set to %s\n", envName)
	for _, line := range bindingLines(cfg, auth) {
		fmt.Printf("  %s\n", line)
	}
	if note != "" {
		printWarning("%s\n", note)
	}
	if why := credentialRefusal(newURL, auth.Token); why != "" {
		printWarning("Credential: the stored credential is not one this server accepts — %s; run '%s'\n", why, authRepairCommand(newURL))
	}
	fmt.Println()
	printInfo("Run 'modernpath env test' to verify the connection.\n")
	return nil
}

// issuerFor names the identity provider of a known host; a custom host has
// none the CLI can name.
func issuerFor(apiURL string) (string, bool) {
	if p, ok := zitadel.ProfileForAPIURL(apiURL); ok {
		return p.Issuer, true
	}
	return "", false
}

// credentialKeyFor is the stash slot of the active credential: its recorded
// issuer, else the issuer of the host it was used against, else that URL.
func credentialKeyFor(auth *config.Auth, apiURL string) string {
	if auth.Issuer != "" {
		return auth.Issuer
	}
	if issuer, ok := issuerFor(apiURL); ok {
		return issuer
	}
	return apiURL
}

func activateCredential(auth *config.Auth, m config.AuthMaterial, issuer string) {
	auth.Token, auth.RefreshToken, auth.ExpiresAt = m.Token, m.RefreshToken, m.ExpiresAt
	auth.Actor, auth.WorkspaceID, auth.WorkspaceName = m.Actor, m.WorkspaceID, m.WorkspaceName
	if issuer == "" {
		issuer = m.Issuer
	}
	auth.Issuer = issuer
}

// workspaceStatusLine says which workspace the stored credential is for:
// name and id, the id alone when no name is stored, nothing when none is.
func workspaceStatusLine(auth *config.Auth) string {
	switch {
	case auth == nil || auth.WorkspaceID == "":
		return ""
	case auth.WorkspaceName == "":
		return auth.WorkspaceID
	default:
		return fmt.Sprintf("%s (%s)", auth.WorkspaceName, auth.WorkspaceID)
	}
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

	if auth, err := config.ReadAuth(); err == nil {
		if line := workspaceStatusLine(auth); line != "" {
			cyan.Printf("  Workspace: ")
			fmt.Printf("%s\n", line)
		}
	}

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
