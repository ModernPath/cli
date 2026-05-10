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
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

var (
	staged      bool
	diffBase    string
	showFiles   bool
	reviewLocal bool
)

var workReviewCmd = &cobra.Command{
	Use:   "review",
	Short: "AI-powered code review against epic specifications",
	Long: `Analyze git diff and provide AI-powered code review against your
epic's technical specifications.

The review evaluates your changes across multiple dimensions:
- Compliance: Do the changes meet the spec requirements?
- Code Quality: Best practices, maintainability, error handling
- Security: Potential vulnerabilities or concerns
- Performance: Efficiency and optimization opportunities
- Testing: Test coverage assessment
- Architecture: Alignment with system design

Examples:
  modernpath work review                  # Review uncommitted changes (requires epic)
  modernpath work review --staged         # Review staged changes only
  modernpath work review --base=main      # Review changes vs main branch
  modernpath work review --files          # Just show affected files
  modernpath work review --local          # Local review without server (basic)`,
	RunE: runReview,
}

func init() {
	workReviewCmd.Flags().BoolVar(&staged, "staged", false, "Only review staged changes")
	workReviewCmd.Flags().StringVar(&diffBase, "base", "", "Base branch/commit to compare against")
	workReviewCmd.Flags().BoolVar(&showFiles, "files", false, "Only show affected files")
	workReviewCmd.Flags().BoolVar(&reviewLocal, "local", false, "Use local review only (no server)")
}

func runReview(cmd *cobra.Command, args []string) error {
	if !config.IsInitialized() {
		printError("Not initialized. Run 'modernpath init' first.\n")
		return nil
	}

	// Check if we're in a git repo
	if !isGitRepo() {
		printError("Not a git repository\n")
		return nil
	}

	// Get the diff
	diff, err := getGitDiff()
	if err != nil {
		printError("Failed to get git diff: %v\n", err)
		return err
	}

	if diff == "" {
		printInfo("No changes to review\n")
		return nil
	}

	// Get changed files
	files, err := getChangedFiles()
	if err != nil {
		printWarning("Failed to get file list: %v\n", err)
	}

	bold := color.New(color.Bold)

	if showFiles {
		bold.Println("Changed Files")
		fmt.Println("─────────────────────────────────────────")
		for _, file := range files {
			fmt.Printf("  %s\n", file)
		}
		return nil
	}

	// Show summary header
	fmt.Println()
	bold.Println("📝 Code Review")
	fmt.Println("═══════════════════════════════════════════════════════════")

	// Files changed
	fmt.Println()
	bold.Printf("Changed Files (%d)\n", len(files))
	fmt.Println("───────────────────────────────────────────────────────────")
	for _, file := range files {
		statusColor := color.New(color.FgGreen)
		statusColor.Printf("  M ")
		fmt.Println(file)
	}

	// Diff stats
	additions, deletions := countDiffStats(diff)
	fmt.Println()
	fmt.Printf("Stats: ")
	color.Green("+%d ", additions)
	color.Red("-%d\n", deletions)

	// If local mode or no initiative, do basic local review
	cfg, err := config.ReadConfig()
	if err != nil || cfg.InitiativeID == 0 || reviewLocal {
		if !reviewLocal {
			printWarning("No initiative selected. Using local review only.\n")
			printInfo("Select an initiative with 'modernpath plan status' for AI-powered review.\n")
		}
		return runLocalReview(files, diff, bold)
	}

	// Run server-side AI review
	return runServerReview(cfg, files, diff, bold)
}

func runLocalReview(files []string, diff string, bold *color.Color) error {
	// Show the diff if verbose
	if verbose {
		fmt.Println()
		bold.Println("📄 Full Diff")
		fmt.Println("───────────────────────────────────────────────────────────")
		fmt.Println(colorDiff(diff))
	}

	fmt.Println()
	printInfo("Tip: Select an epic for AI-powered review against specifications\n")

	return nil
}

// ReviewResponse represents the server review response
type ReviewResponse struct {
	Success bool `json:"success"`
	Data    struct {
		InitiativeID   int    `json:"initiative_id"`
		InitiativeName string `json:"initiative_name"`
		Review         struct {
			Success bool `json:"success"`
			Cached  bool `json:"cached,omitempty"`
			Review  struct {
				ComplianceScore   int                    `json:"compliance_score"`
				Categories        map[string]interface{} `json:"categories"`
				BlockerIssues     []string               `json:"blocker_issues"`
				OverallAssessment string                 `json:"overall_assessment"`
				Structured        bool                   `json:"structured,omitempty"`
				RawReview         string                 `json:"raw_review,omitempty"`
			} `json:"review"`
			Metadata map[string]interface{} `json:"metadata"`
		} `json:"review"`
	} `json:"data"`
	Error string `json:"error,omitempty"`
}

func runServerReview(cfg *config.Config, files []string, diff string, bold *color.Color) error {
	fmt.Println()
	bold.Println("🤖 AI-Powered Review")
	fmt.Println("───────────────────────────────────────────────────────────")
	fmt.Printf("Initiative: %s (ID: %d)\n", cfg.InitiativeName, cfg.InitiativeID)
	fmt.Println()
	printInfo("Analyzing changes against initiative specifications...\n")

	// Prepare request
	requestBody := map[string]string{
		"changelog": diff,
	}
	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		printError("Failed to prepare request: %v\n", err)
		return err
	}

	// Send to server
	url := fmt.Sprintf("%s/api/work/initiatives/%d/review", cfg.APIURL, cfg.InitiativeID)
	
	client := &http.Client{Timeout: 120 * time.Second} // Long timeout for AI processing
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		printError("Failed to create request: %v\n", err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		printError("Failed to connect to server: %v\n", err)
		printInfo("Use --local flag for offline review\n")
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		printError("Failed to read response: %v\n", err)
		return err
	}

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			printError("Review failed: %s\n", errResp.Error)
		} else {
			printError("Review failed: HTTP %d - %s\n", resp.StatusCode, string(body))
		}
		return fmt.Errorf("review failed")
	}

	// Parse response
	var reviewResp ReviewResponse
	if err := json.Unmarshal(body, &reviewResp); err != nil {
		printError("Failed to parse review response: %v\n", err)
		return err
	}

	// Display review results
	displayReviewResults(reviewResp, bold)

	return nil
}

func displayReviewResults(resp ReviewResponse, bold *color.Color) {
	review := resp.Data.Review.Review

	// Check if we got a structured review
	if review.Structured == false && review.RawReview != "" {
		// Display raw review
		fmt.Println()
		bold.Println("📋 Review Summary")
		fmt.Println("───────────────────────────────────────────────────────────")
		fmt.Println(review.RawReview)
		return
	}

	// Display compliance score with color coding
	fmt.Println()
	scoreColor := color.New(color.FgGreen, color.Bold)
	if review.ComplianceScore < 70 {
		scoreColor = color.New(color.FgYellow, color.Bold)
	}
	if review.ComplianceScore < 50 {
		scoreColor = color.New(color.FgRed, color.Bold)
	}

	fmt.Printf("Compliance Score: ")
	scoreColor.Printf("%d/100\n", review.ComplianceScore)

	// Display overall assessment
	fmt.Println()
	bold.Println("📋 Overall Assessment")
	fmt.Println("───────────────────────────────────────────────────────────")
	fmt.Println(review.OverallAssessment)

	// Display blocker issues if any
	if len(review.BlockerIssues) > 0 {
		fmt.Println()
		bold.Println("🚫 Blocker Issues (Must Fix)")
		fmt.Println("───────────────────────────────────────────────────────────")
		for i, issue := range review.BlockerIssues {
			color.Red("  %d. %s\n", i+1, issue)
		}
	}

	// Display category summaries
	if len(review.Categories) > 0 {
		fmt.Println()
		bold.Println("📊 Category Breakdown")
		fmt.Println("───────────────────────────────────────────────────────────")

		categoryOrder := []string{"compliance", "quality", "security", "performance", "testing", "architecture"}
		categoryNames := map[string]string{
			"compliance":   "Compliance",
			"quality":      "Code Quality",
			"security":     "Security",
			"performance":  "Performance",
			"testing":      "Testing",
			"architecture": "Architecture",
		}

		for _, cat := range categoryOrder {
			if catData, ok := review.Categories[cat]; ok {
				if catMap, ok := catData.(map[string]interface{}); ok {
					score := 0
					if s, ok := catMap["score"].(float64); ok {
						score = int(s)
					}

					catColor := color.New(color.FgGreen)
					if score < 70 {
						catColor = color.New(color.FgYellow)
					}
					if score < 50 {
						catColor = color.New(color.FgRed)
					}

					fmt.Printf("  %-15s ", categoryNames[cat]+":")
					catColor.Printf("%d/100\n", score)

					// Show findings if verbose
					if verbose {
						if findings, ok := catMap["findings"].([]interface{}); ok && len(findings) > 0 {
							for _, f := range findings {
								fmt.Printf("    • %v\n", f)
							}
						}
					}
				}
			}
		}
	}

	// Show recommendations if verbose
	if verbose && len(review.Categories) > 0 {
		fmt.Println()
		bold.Println("💡 Recommendations")
		fmt.Println("───────────────────────────────────────────────────────────")

		for cat, catData := range review.Categories {
			if catMap, ok := catData.(map[string]interface{}); ok {
				if recs, ok := catMap["recommendations"].([]interface{}); ok && len(recs) > 0 {
					fmt.Printf("\n%s:\n", strings.Title(cat))
					for _, r := range recs {
						fmt.Printf("  → %v\n", r)
					}
				}
			}
		}
	}

	// Show cache status
	if resp.Data.Review.Cached {
		fmt.Println()
		printInfo("(Using cached review result)\n")
	}

	fmt.Println()
}

func isGitRepo() bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	return cmd.Run() == nil
}

func getGitDiff() (string, error) {
	var args []string

	if diffBase != "" {
		args = []string{"diff", diffBase}
	} else if staged {
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

func getChangedFiles() ([]string, error) {
	var args []string

	if diffBase != "" {
		args = []string{"diff", "--name-only", diffBase}
	} else if staged {
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

func countDiffStats(diff string) (additions, deletions int) {
	lines := strings.Split(diff, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			additions++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			deletions++
		}
	}
	return
}

func colorDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	var result []string

	green := color.New(color.FgGreen).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()

	for _, line := range lines {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			result = append(result, green(line))
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			result = append(result, red(line))
		} else if strings.HasPrefix(line, "@@") {
			result = append(result, cyan(line))
		} else {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

