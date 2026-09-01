package cmd

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/fatih/color"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

var (
	staged    bool
	diffBase  string
	showFiles bool
)

var workReviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Inspect the current git diff",
	Long: `Inspect the current git diff and summarize its changed files and line counts.

Examples:
  modernpath work review                  # Inspect uncommitted changes
  modernpath work review --staged         # Inspect staged changes only
  modernpath work review --base=main      # Inspect changes vs main branch
  modernpath work review --files          # Just show affected files`,
	RunE: runReview,
}

func init() {
	workReviewCmd.Flags().BoolVar(&staged, "staged", false, "Only review staged changes")
	workReviewCmd.Flags().StringVar(&diffBase, "base", "", "Base branch/commit to compare against")
	workReviewCmd.Flags().BoolVar(&showFiles, "files", false, "Only show affected files")
}

func runReview(cmd *cobra.Command, args []string) error {
	if !config.IsInitialized() {
		printError("Not initialized. Run 'modernpath init' first.\n")
		return nil
	}

	if !isGitRepo() {
		printError("Not a git repository\n")
		return nil
	}

	diff, err := getGitDiff()
	if err != nil {
		printError("Failed to get git diff: %v\n", err)
		return err
	}

	if diff == "" {
		printInfo("No changes to review\n")
		return nil
	}

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

	fmt.Println()
	bold.Println("📝 Code Review")
	fmt.Println("═══════════════════════════════════════════════════════════")

	fmt.Println()
	bold.Printf("Changed Files (%d)\n", len(files))
	fmt.Println("───────────────────────────────────────────────────────────")
	for _, file := range files {
		statusColor := color.New(color.FgGreen)
		statusColor.Printf("  M ")
		fmt.Println(file)
	}

	additions, deletions := countDiffStats(diff)
	fmt.Println()
	fmt.Printf("Stats: ")
	color.Green("+%d ", additions)
	color.Red("-%d\n", deletions)

	return runLocalReview(diff, bold)
}

func runLocalReview(diff string, bold *color.Color) error {
	if verbose {
		fmt.Println()
		bold.Println("📄 Full Diff")
		fmt.Println("───────────────────────────────────────────────────────────")
		fmt.Println(colorDiff(diff))
	}

	fmt.Println()
	return nil
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
	for _, file := range files {
		if file != "" {
			result = append(result, file)
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
		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			result = append(result, green(line))
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			result = append(result, red(line))
		case strings.HasPrefix(line, "@@"):
			result = append(result, cyan(line))
		default:
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}
