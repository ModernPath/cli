package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

const unifiedWorkspaceAnalysisMode = "unified_workspace"

var initWorkspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Initialize a multi-repo workspace from the workspace root folder",
	Long: `Initialize ModernPath for a unified multi-repository workspace.

Run this command from the parent directory that contains your Git repositories
as sibling folders (not from inside a single repo). The command downloads
unified documentation for the selected ModernPath workspace system and writes
.modernpath/ at the workspace root.

Example:
  cd ~/projects/my-platform
  modernpath init workspace
  modernpath init workspace --system-id=42
  modernpath init workspace --local --force`,
	RunE: runInitWorkspace,
}

func init() {
	initCmd.AddCommand(initWorkspaceCmd)
}

type localGitRepo struct {
	Name string
	Path string
}

func runInitWorkspace(cmd *cobra.Command, args []string) error {
	if config.IsInitialized() && !force {
		cfg, _ := config.ReadConfig()
		printWarning("Already initialized with system: %s\n", cfg.SystemName)
		printInfo("Use --force to reinitialize or 'modernpath docs sync' to update\n")
		return nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	localRepos, err := discoverGitRepos(cwd)
	if err != nil {
		return err
	}

	if len(localRepos) == 0 {
		printError("No Git repositories found in immediate subfolders of %s\n", cwd)
		printInfo("Create a workspace root with one folder per repo, then run init workspace from that parent.\n")
		printInfo("Example:\n  my-workspace/\n    api-service/\n    web-app/\n    .modernpath/  (created by this command)\n")
		return fmt.Errorf("no git repositories found in workspace root")
	}

	printInfo("Found %d local Git repositories:\n", len(localRepos))
	for _, repo := range localRepos {
		fmt.Printf("  • %s/\n", repo.Name)
	}
	fmt.Println()

	client, baseURL, err := connectInitClient()
	if err != nil {
		return err
	}

	systems, err := client.ListSystems()
	if err != nil {
		printError("Failed to list systems: %v\n", err)
		return err
	}

	workspaceSystems := filterUnifiedWorkspaceSystems(systems)
	if len(workspaceSystems) == 0 {
		printError("No unified workspace systems found on the platform.\n")
		printInfo("Create a system with multiple GitHub repos in unified workspace mode, run analysis, then retry.\n")
		return fmt.Errorf("no unified workspace systems available")
	}

	selectedSystem, err := resolveWorkspaceSystemSelection(client, workspaceSystems)
	if err != nil {
		return err
	}

	if selectedSystem.AnalysisMode != "" && selectedSystem.AnalysisMode != unifiedWorkspaceAnalysisMode {
		printWarning("Selected system is not in unified workspace mode (%s).\n", selectedSystem.AnalysisMode)
	}

	if len(selectedSystem.WorkspaceMembers) == 0 {
		full, err := client.GetSystem(selectedSystem.ID)
		if err != nil {
			return err
		}
		selectedSystem = full
	}

	printWorkspaceMemberMapping(localRepos, selectedSystem.WorkspaceMembers)

	return finalizeSystemInit(client, baseURL, selectedSystem, initFinalizeOptions{
		force:          force,
		initMode:       "workspace",
		localRepos:     localRepos,
		platformMembers: selectedSystem.WorkspaceMembers,
	})
}

func filterUnifiedWorkspaceSystems(systems []api.System) []api.System {
	var out []api.System
	for _, sys := range systems {
		if sys.AnalysisMode == unifiedWorkspaceAnalysisMode || len(sys.WorkspaceMembers) > 0 {
			out = append(out, sys)
		}
	}
	return out
}

func resolveWorkspaceSystemSelection(client *api.Client, systems []api.System) (*api.System, error) {
	if systemIDFlag > 0 {
		for i := range systems {
			if systems[i].ID == systemIDFlag {
				sys := systems[i]
				if len(sys.WorkspaceMembers) == 0 {
					return client.GetSystem(sys.ID)
				}
				return &sys, nil
			}
		}
		return nil, fmt.Errorf("unified workspace system with ID %d not found", systemIDFlag)
	}

	if systemNameFlag != "" {
		for i := range systems {
			if strings.Contains(strings.ToLower(systems[i].Name), strings.ToLower(systemNameFlag)) {
				sys := systems[i]
				if len(sys.WorkspaceMembers) == 0 {
					return client.GetSystem(sys.ID)
				}
				return &sys, nil
			}
		}
		return nil, fmt.Errorf("unified workspace system matching '%s' not found", systemNameFlag)
	}

	return selectWorkspaceSystem(client, systems)
}

func selectWorkspaceSystem(client *api.Client, systems []api.System) (*api.System, error) {
	items := make([]string, 0, len(systems))
	for _, sys := range systems {
		desc := sys.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		if desc == "" {
			desc = fmt.Sprintf("%d members", len(sys.WorkspaceMembers))
		}
		items = append(items, fmt.Sprintf("%s - %s", sys.Name, desc))
	}

	prompt := promptui.Select{
		Label:    "Select a unified workspace",
		Items:    items,
		Size:     10,
		HideHelp: true,
	}

	index, _, err := prompt.Run()
	if err != nil {
		return nil, err
	}

	sys := systems[index]
	if len(sys.WorkspaceMembers) == 0 {
		return client.GetSystem(sys.ID)
	}
	return &sys, nil
}

func discoverGitRepos(parentDir string) ([]localGitRepo, error) {
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return nil, err
	}

	var repos []localGitRepo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == config.ConfigDir || strings.HasPrefix(name, ".") {
			continue
		}

		gitDir := filepath.Join(parentDir, name, ".git")
		info, err := os.Stat(gitDir)
		if err != nil || !info.IsDir() {
			continue
		}

		repos = append(repos, localGitRepo{Name: name, Path: name})
	}

	return repos, nil
}

func printWorkspaceMemberMapping(localRepos []localGitRepo, members []api.WorkspaceMember) {
	if len(members) == 0 {
		printWarning("Platform workspace has no member metadata yet.\n")
		return
	}

	fmt.Println("Platform ↔ local folder mapping:")
	for _, member := range members {
		local := matchLocalRepoFolder(localRepos, member)
		if local == "" {
			fmt.Printf("  • %s (%s) — no matching local folder\n", member.Slug, member.FullName)
			continue
		}
		fmt.Printf("  • %s (%s) → ./%s/\n", member.Slug, member.FullName, local)
	}
	fmt.Println()
}

func matchLocalRepoFolder(localRepos []localGitRepo, member api.WorkspaceMember) string {
	slug := strings.TrimSpace(member.Slug)
	if slug == "" {
		return ""
	}

	for _, repo := range localRepos {
		if repo.Name == slug {
			return repo.Name
		}
	}

	slugLower := strings.ToLower(slug)
	for _, repo := range localRepos {
		nameLower := strings.ToLower(repo.Name)
		if strings.Contains(nameLower, slugLower) || strings.Contains(slugLower, nameLower) {
			return repo.Name
		}
	}

	if member.FullName != "" {
		parts := strings.Split(member.FullName, "/")
		repoName := parts[len(parts)-1]
		for _, repo := range localRepos {
			if repo.Name == repoName {
				return repo.Name
			}
		}
	}

	return ""
}

func buildWorkspaceConfigMembers(localRepos []localGitRepo, members []api.WorkspaceMember) []config.WorkspaceMember {
	out := make([]config.WorkspaceMember, 0, len(members))
	for _, member := range members {
		out = append(out, config.WorkspaceMember{
			Slug:        member.Slug,
			FullName:    member.FullName,
			DisplayName: member.DisplayName,
			LocalPath:   matchLocalRepoFolder(localRepos, member),
		})
	}
	return out
}
