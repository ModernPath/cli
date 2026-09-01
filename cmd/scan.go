package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

var (
	scanMode     string
	scanStaged   bool
	scanBase     string
	scanVerbose  bool
	scanSeverity string
	scanAgents   []string
	scanListAgents bool
)

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Architecture analysis with selectable agents",
	Long: `Scan your codebase using specialized architect agents.

Available Agents:
  antivibe    - Detect vibe coding issues (security, complexity, duplication)
  clean_arch  - Clean Architecture analysis (dependencies, SOLID, layers)
  ddd         - Domain-Driven Design analysis (bounded contexts, aggregates)

By default, runs the AntiVibe scanner. Use --agents to select specific agents.

Modes:
- full: Scan the entire codebase (default, requires system)
- changes: Scan only current git changes
- staged: Scan only staged git changes

Examples:
  modernpath scan                           # AntiVibe scan (default)
  modernpath scan --agents=antivibe         # Explicitly run AntiVibe only
  modernpath scan --agents=clean_arch       # Clean Architecture analysis
  modernpath scan --agents=antivibe,ddd     # Run multiple agents
  modernpath scan --list-agents             # List available agents
  modernpath scan --mode=changes            # Scan uncommitted changes only
  modernpath scan --staged                  # Scan staged changes only
  modernpath scan --base=main               # Scan changes vs main branch
  modernpath scan --severity=high           # Show only high+ severity findings`,
	RunE: runScan,
}

func init() {
	scanCmd.Flags().StringSliceVar(&scanAgents, "agents", []string{"antivibe"}, "Agents to run (comma-separated): antivibe, clean_arch, ddd")
	scanCmd.Flags().BoolVar(&scanListAgents, "list-agents", false, "List available agents and exit")
	scanCmd.Flags().StringVar(&scanMode, "mode", "full", "Scan mode: full, changes, or staged")
	scanCmd.Flags().BoolVar(&scanStaged, "staged", false, "Shortcut for --mode=staged")
	scanCmd.Flags().StringVar(&scanBase, "base", "", "Base branch/commit to compare against (implies --mode=changes)")
	scanCmd.Flags().BoolVarP(&scanVerbose, "verbose", "v", false, "Show detailed findings")
	scanCmd.Flags().StringVar(&scanSeverity, "severity", "", "Filter by minimum severity: critical, high, medium, low")
	
	rootCmd.AddCommand(scanCmd)
}

// ScanResponse represents the server scan response
type ScanResponse struct {
	Success bool `json:"success"`
	Data    struct {
		SystemID   int      `json:"system_id"`
		SystemName string   `json:"system_name"`
		Mode             string   `json:"mode"`
		AgentsUsed       []string `json:"agents_used"`
		VibeDept         *struct {
			Score                float64 `json:"score"`
			Grade                string  `json:"grade"`
			SecurityScore        float64 `json:"security_score"`
			ReliabilityScore     float64 `json:"reliability_score"`
			MaintainabilityScore float64 `json:"maintainability_score"`
			DuplicationScore     float64 `json:"duplication_score"`
		} `json:"vibe_debt"`
		Summary struct {
			TotalFindings int            `json:"total_findings"`
			BySeverity    map[string]int `json:"by_severity"`
			ByCategory    map[string]int `json:"by_category"`
			ByAgent       map[string]int `json:"by_agent"`
		} `json:"summary"`
		Findings []ScanFinding `json:"findings"`
	} `json:"data"`
	Error string `json:"error,omitempty"`
}

type ScanFinding struct {
	FindingType     string                   `json:"finding_type"`
	Severity        string                   `json:"severity"`
	Category        string                   `json:"category"`
	Title           string                   `json:"title"`
	Description     string                   `json:"description"`
	ConfidenceScore float64                  `json:"confidence_score"`
	Recommendations []map[string]interface{} `json:"recommendations"`
	Evidence        []map[string]interface{} `json:"evidence"`
	Heuristics      []string                 `json:"heuristics_applied"`
	AgentType       string                   `json:"agent_type"`
}

// AgentInfo describes an available agent
type AgentInfo struct {
	Type        string   `json:"type"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Icon        string   `json:"icon"`
	Expertise   []string `json:"expertise"`
}

func runScan(cmd *cobra.Command, args []string) error {
	bold := color.New(color.Bold)

	// Handle --list-agents
	if scanListAgents {
		return listAvailableAgents(bold)
	}

	if !config.IsInitialized() {
		printError("Not initialized. Run 'modernpath init' first.\n")
		return nil
	}

	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.SystemID == 0 {
		printError("No system configured.\n")
		printInfo("Run 'modernpath init' in a project with an analyzed system.\n")
		return nil
	}

	// Determine mode
	mode := scanMode
	if scanStaged {
		mode = "staged"
	}
	if scanBase != "" {
		mode = "changes"
	}

	// Determine header based on agents
	headerIcon := "🔍"
	headerText := "Architecture Scan"
	if len(scanAgents) == 1 {
		switch scanAgents[0] {
		case "antivibe":
			headerIcon = "🔮"
			headerText = "AntiVibe Scanner"
		case "clean_arch":
			headerIcon = "🧹"
			headerText = "Clean Architecture Analysis"
		case "ddd":
			headerIcon = "🏛️"
			headerText = "DDD Analysis"
		}
	}

	// Print header
	fmt.Println()
	bold.Printf("%s %s\n", headerIcon, headerText)
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Printf("System: %s (ID: %d)\n", cfg.SystemName, cfg.SystemID)
	fmt.Printf("Mode: %s\n", mode)
	fmt.Printf("Agents: %s\n", strings.Join(scanAgents, ", "))

	// Prepare request
	requestBody := map[string]interface{}{
		"mode":   mode,
		"agents": scanAgents,
	}

	// For changes/staged mode, get git diff
	if mode == "changes" || mode == "staged" {
		if !isGitRepo() {
			printError("Not a git repository. Use --mode=full for architecture scan.\n")
			return nil
		}

		diff, err := getScanDiff(mode)
		if err != nil {
			printError("Failed to get git diff: %v\n", err)
			return err
		}

		if diff == "" {
			printInfo("No changes to scan.\n")
			return nil
		}

		files, _ := getScanChangedFiles(mode)
		
		fmt.Println()
		bold.Printf("Changed Files (%d)\n", len(files))
		fmt.Println("───────────────────────────────────────────────────────────")
		for _, file := range files {
			fmt.Printf("  %s\n", file)
		}

		requestBody["changes"] = diff
		requestBody["changed_files"] = files
	}

	fmt.Println()
	printInfo("Scanning for vibe coding issues...\n")

	// Send request
	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		printError("Failed to prepare request: %v\n", err)
		return err
	}

	url := fmt.Sprintf("%s/api/systems/%d/scan", cfg.APIURL, cfg.SystemID)
	resp, err := api.DoAuthenticatedPost(url, bytes.NewBuffer(jsonBody), 180*time.Second)
	if err != nil {
		printError("Failed to connect to server: %v\n", err)
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		printError("Failed to read response: %v\n", err)
		return err
	}

	if resp.StatusCode != http.StatusOK {
		printError("Scan failed: %s\n", scanFailureReason(resp.StatusCode, body))
		return fmt.Errorf("scan failed")
	}

	// Parse response
	var scanResp ScanResponse
	if err := json.Unmarshal(body, &scanResp); err != nil {
		printError("Failed to parse response: %v\n", err)
		return err
	}

	// Display results
	displayScanResults(scanResp, bold)

	return nil
}

func displayScanResults(resp ScanResponse, bold *color.Color) {
	data := resp.Data

	// Vibe Debt Score
	if data.VibeDept != nil {
		fmt.Println()
		bold.Println("📊 Vibe Debt Score")
		fmt.Println("───────────────────────────────────────────────────────────")

		gradeColor := color.New(color.FgGreen, color.Bold)
		if data.VibeDept.Grade == "C" || data.VibeDept.Grade == "D" {
			gradeColor = color.New(color.FgYellow, color.Bold)
		}
		if data.VibeDept.Grade == "F" {
			gradeColor = color.New(color.FgRed, color.Bold)
		}

		fmt.Printf("Overall: ")
		gradeColor.Printf("%.1f/100 (Grade: %s)\n", data.VibeDept.Score, data.VibeDept.Grade)
		fmt.Println()
		fmt.Printf("  🔐 Security:       %s\n", formatScore(data.VibeDept.SecurityScore))
		fmt.Printf("  ⚡ Reliability:    %s\n", formatScore(data.VibeDept.ReliabilityScore))
		fmt.Printf("  🔧 Maintainability: %s\n", formatScore(data.VibeDept.MaintainabilityScore))
		fmt.Printf("  📋 Duplication:    %s\n", formatScore(data.VibeDept.DuplicationScore))
	}

	// Summary
	fmt.Println()
	bold.Println("📋 Findings Summary")
	fmt.Println("───────────────────────────────────────────────────────────")
	fmt.Printf("Total: %d findings\n\n", data.Summary.TotalFindings)

	// By severity
	severityOrder := []string{"critical", "high", "medium", "low", "info"}
	severityEmoji := map[string]string{
		"critical": "🔴",
		"high":     "🟠",
		"medium":   "🟡",
		"low":      "🟢",
		"info":     "🔵",
	}

	for _, sev := range severityOrder {
		count := data.Summary.BySeverity[sev]
		if count > 0 {
			fmt.Printf("  %s %-10s %d\n", severityEmoji[sev], strings.Title(sev)+":", count)
		}
	}

	// By category
	fmt.Println()
	fmt.Println("By Category:")
	categoryEmoji := map[string]string{
		"security":        "🔐",
		"reliability":     "⚡",
		"maintainability": "🔧",
		"duplication":     "📋",
		"dead_code":       "💀",
		"readability":     "📖",
		"vibe_debt":       "📊",
		"dependencies":    "🔗",
		"layering":        "📐",
		"cohesion":        "🎯",
		"complexity":      "🧩",
	}

	for cat, count := range data.Summary.ByCategory {
		if cat != "vibe_debt" && count > 0 {
			emoji := categoryEmoji[cat]
			if emoji == "" {
				emoji = "📌"
			}
			fmt.Printf("  %s %-15s %d\n", emoji, strings.Title(cat)+":", count)
		}
	}

	// By agent (if multiple agents used)
	if len(data.Summary.ByAgent) > 1 {
		fmt.Println()
		fmt.Println("By Agent:")
		agentEmoji := map[string]string{
			"antivibe":   "🔮",
			"clean_arch": "🧹",
			"ddd":        "🏛️",
		}
		for agent, count := range data.Summary.ByAgent {
			emoji := agentEmoji[agent]
			if emoji == "" {
				emoji = "🔍"
			}
			fmt.Printf("  %s %-15s %d\n", emoji, agent+":", count)
		}
	}

	// Filter findings by severity if specified
	findings := data.Findings
	if scanSeverity != "" {
		findings = filterBySeverity(findings, scanSeverity)
	}

	// Exclude vibe_debt from findings list (already shown as score)
	findings = filterOutCategory(findings, "vibe_debt")

	// Show findings
	if len(findings) > 0 {
		fmt.Println()
		bold.Println("🔍 Findings")
		fmt.Println("───────────────────────────────────────────────────────────")

		// Group by severity for display
		for _, sev := range []string{"critical", "high", "medium", "low"} {
			sevFindings := filterBySeverity(findings, sev)
			if len(sevFindings) == 0 {
				continue
			}

			// Only show exact match for this severity
			sevFindings = filterBySeverityExact(findings, sev)
			if len(sevFindings) == 0 {
				continue
			}

			for _, f := range sevFindings {
				displayFinding(f, scanVerbose)
			}
		}
	}

	// Recommendations summary
	if scanVerbose && len(findings) > 0 {
		fmt.Println()
		bold.Println("💡 Top Recommendations")
		fmt.Println("───────────────────────────────────────────────────────────")

		shown := 0
		for _, f := range findings {
			if f.Severity == "critical" || f.Severity == "high" {
				for _, rec := range f.Recommendations {
					if title, ok := rec["title"].(string); ok {
						desc := ""
						if d, ok := rec["description"].(string); ok {
							desc = d
						}
						fmt.Printf("  → %s\n", title)
						if desc != "" && scanVerbose {
							fmt.Printf("    %s\n", color.New(color.FgHiBlack).Sprint(desc))
						}
						shown++
						if shown >= 5 {
							break
						}
					}
				}
			}
			if shown >= 5 {
				break
			}
		}
	}

	fmt.Println()
}

func displayFinding(f ScanFinding, verbose bool) {
	sevColor := color.New(color.FgGreen)
	sevEmoji := "🟢"

	switch f.Severity {
	case "critical":
		sevColor = color.New(color.FgRed, color.Bold)
		sevEmoji = "🔴"
	case "high":
		sevColor = color.New(color.FgHiRed)
		sevEmoji = "🟠"
	case "medium":
		sevColor = color.New(color.FgYellow)
		sevEmoji = "🟡"
	}

	fmt.Printf("\n%s ", sevEmoji)
	sevColor.Printf("[%s] ", strings.ToUpper(f.Severity))
	fmt.Printf("%s\n", f.Title)

	if verbose {
		// Truncate description for display
		desc := f.Description
		lines := strings.Split(desc, "\n")
		if len(lines) > 5 {
			desc = strings.Join(lines[:5], "\n") + "\n..."
		}
		fmt.Printf("   %s\n", color.New(color.FgHiBlack).Sprint(strings.ReplaceAll(desc, "\n", "\n   ")))

		// Show recommendations
		if len(f.Recommendations) > 0 {
			fmt.Println("   Recommendations:")
			for i, rec := range f.Recommendations {
				if i >= 2 {
					break
				}
				if title, ok := rec["title"].(string); ok {
					effort := rec["effort"]
					impact := rec["impact"]
					fmt.Printf("   • %s", title)
					if effort != nil && impact != nil {
						fmt.Printf(" (effort: %v, impact: %v)", effort, impact)
					}
					fmt.Println()
				}
			}
		}
	}
}

func formatScore(score float64) string {
	scoreColor := color.New(color.FgGreen)
	label := "✅ Low"
	if score >= 10 {
		scoreColor = color.New(color.FgGreen)
		label = "🟡 Moderate"
	}
	if score >= 25 {
		scoreColor = color.New(color.FgYellow)
		label = "🟠 Elevated"
	}
	if score >= 40 {
		scoreColor = color.New(color.FgRed)
		label = "🔴 High"
	}
	return scoreColor.Sprintf("%.1f %s", score, label)
}

func filterBySeverity(findings []ScanFinding, minSeverity string) []ScanFinding {
	severityRank := map[string]int{
		"critical": 4,
		"high":     3,
		"medium":   2,
		"low":      1,
		"info":     0,
	}

	minRank := severityRank[minSeverity]
	var filtered []ScanFinding
	for _, f := range findings {
		if severityRank[f.Severity] >= minRank {
			filtered = append(filtered, f)
		}
	}
	return filtered
}

func filterBySeverityExact(findings []ScanFinding, severity string) []ScanFinding {
	var filtered []ScanFinding
	for _, f := range findings {
		if f.Severity == severity {
			filtered = append(filtered, f)
		}
	}
	return filtered
}

func filterOutCategory(findings []ScanFinding, category string) []ScanFinding {
	var filtered []ScanFinding
	for _, f := range findings {
		if f.Category != category {
			filtered = append(filtered, f)
		}
	}
	return filtered
}

func getScanDiff(mode string) (string, error) {
	var args []string

	if scanBase != "" {
		args = []string{"diff", scanBase}
	} else if mode == "staged" {
		args = []string{"diff", "--staged"}
	} else {
		args = []string{"diff"}
	}

	cmd := exec.Command("git", args...)
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return "", err
	}

	return out.String(), nil
}

func getScanChangedFiles(mode string) ([]string, error) {
	var args []string

	if scanBase != "" {
		args = []string{"diff", "--name-only", scanBase}
	} else if mode == "staged" {
		args = []string{"diff", "--staged", "--name-only"}
	} else {
		args = []string{"diff", "--name-only"}
	}

	cmd := exec.Command("git", args...)
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return nil, err
	}

	files := strings.Split(strings.TrimSpace(out.String()), "\n")
	var result []string
	for _, f := range files {
		if f != "" {
			result = append(result, f)
		}
	}

	return result, nil
}

func listAvailableAgents(bold *color.Color) error {
	fmt.Println()
	bold.Println("🔍 Available Architect Agents")
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println()

	agents := []struct {
		Type        string
		Icon        string
		Name        string
		Description string
		Expertise   []string
	}{
		{
			Type:        "antivibe",
			Icon:        "🔮",
			Name:        "AntiVibe Scanner",
			Description: "Detect vibe coding issues from rapid AI-assisted development",
			Expertise:   []string{"Security vulnerabilities", "Missing error handling", "God modules", "Code duplication", "Vibe Debt scoring"},
		},
		{
			Type:        "clean_arch",
			Icon:        "🧹",
			Name:        "Clean Architecture Expert",
			Description: "Analyze codebase through Clean Architecture principles",
			Expertise:   []string{"Dependency Rule validation", "SOLID principles", "Layer separation", "Circular dependencies"},
		},
		{
			Type:        "ddd",
			Icon:        "🏛️",
			Name:        "DDD Architect",
			Description: "Domain-Driven Design analysis and recommendations",
			Expertise:   []string{"Bounded contexts", "Aggregates", "Domain events", "Ubiquitous language"},
		},
	}

	for _, agent := range agents {
		bold.Printf("%s %s\n", agent.Icon, agent.Name)
		fmt.Printf("   Type: %s\n", color.CyanString(agent.Type))
		fmt.Printf("   %s\n", agent.Description)
		fmt.Printf("   Expertise: %s\n", strings.Join(agent.Expertise, ", "))
		fmt.Println()
	}

	fmt.Println("Usage:")
	fmt.Printf("  modernpath scan --agents=%s           # Run single agent\n", color.CyanString("antivibe"))
	fmt.Printf("  modernpath scan --agents=%s  # Run multiple agents\n", color.CyanString("antivibe,clean_arch"))
	fmt.Println()

	return nil
}

// scanFailureReason names a non-200 scan answer for the user. The server's
// reason rides "message" or "error" ({"error":"Request blocked by WAF"} was
// shown as a bare "HTTP 403" — REQ-CROSS-208/T5, RUN:2026-08-18); a bare
// status code is the last resort, not the default.
func scanFailureReason(status int, body []byte) string {
	var errResp struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}

	if json.Unmarshal(body, &errResp) == nil {
		if errResp.Message != "" {
			return errResp.Message
		}

		if errResp.Error != "" {
			return errResp.Error
		}
	}

	return fmt.Sprintf("HTTP %d", status)
}
