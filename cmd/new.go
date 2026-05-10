package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/manifoldco/promptui"
	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

var (
	projectName     string
	projectDesc     string
	descFile        string
	techProfile     string
	projectType     string
	skipInit        bool
	inPlace         bool
	yesFlag         bool
)

// TechProfile represents a technology standards profile
type TechProfile struct {
	ID          string                 `json:"id"` // UUID or "ai-generated-XXX"
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Icon        string                 `json:"icon"`
	IsDefault   bool                   `json:"is_default"`
	Tags        []string               `json:"tags"`
	TechStack   map[string]interface{} `json:"tech_stack,omitempty"`
	Generated   bool                   `json:"generated,omitempty"` // true for AI-generated profiles
}

var newCmd = &cobra.Command{
	Use:   "new [project-name]",
	Short: "Create a new project with AI-generated system",
	Long: `Create a new project from scratch using ModernPath's Builder.

This command creates a project folder (like 'git clone') containing:
  my-project/
  └── .modernpath/
      └── config.json

Steps:
1. Select a technology standards profile (customer-specific)
2. Name your project
3. Provide a description (text or from a file)
4. Generate the system and initial initiative

Examples:
  modernpath new                                     # Interactive - creates folder
  modernpath new "My Project"                        # Creates my-project/ folder
  modernpath new "API" --profile="Python"            # Match profile by name
  modernpath new --desc-file=./PROJECT.md            # From markdown file
  modernpath new --in-place                          # Init in current directory`,
	RunE: runNew,
}

func init() {
	newCmd.Flags().StringVarP(&projectName, "name", "n", "", "Project name")
	newCmd.Flags().StringVarP(&projectDesc, "desc", "d", "", "Project description")
	newCmd.Flags().StringVarP(&descFile, "desc-file", "f", "", "Read description from file (markdown or text)")
	newCmd.Flags().StringVarP(&techProfile, "profile", "p", "", "Tech profile name (partial match) or ID")
	newCmd.Flags().StringVarP(&projectType, "type", "t", "", "Project type: web, api, cli, library")
	newCmd.Flags().BoolVar(&skipInit, "skip-init", false, "Don't initialize .modernpath after creation")
	newCmd.Flags().BoolVar(&inPlace, "in-place", false, "Initialize in current directory (don't create project folder)")
	newCmd.Flags().BoolVarP(&yesFlag, "yes", "y", false, "Skip confirmation prompt")

	rootCmd.AddCommand(newCmd)
}

func runNew(cmd *cobra.Command, args []string) error {
	// Get API URL
	baseURL := apiURL
	if baseURL == "" {
		cfg, _ := config.ReadConfig()
		if cfg != nil && cfg.APIURL != "" {
			baseURL = cfg.APIURL
		} else {
			baseURL = config.DefaultAPIURL
		}
	}

	bold := color.New(color.Bold)

	fmt.Println()
	bold.Println("🏗️  ModernPath Builder")
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println()

	// Check API connection
	client := api.NewClient(baseURL, "")
	if err := client.HealthCheck(); err != nil {
		printError("Cannot connect to ModernPath at %s\n", baseURL)
		printInfo("Make sure the server is running\n")
		return err
	}

	// Get project name
	name := projectName
	if len(args) > 0 && name == "" {
		name = args[0]
	}
	if name == "" {
		prompt := promptui.Prompt{
			Label:   "Project Name",
			Default: "My Awesome Project",
		}
		var err error
		name, err = prompt.Run()
		if err != nil {
			return err
		}
	}

	// Get description FIRST (needed for AI profile selection)
	description := projectDesc
	if descFile != "" {
		content, err := os.ReadFile(descFile)
		if err != nil {
			printError("Failed to read description file: %v\n", err)
			return err
		}
		description = string(content)
	}
	if description == "" {
		// Check if there's a PROJECT.md or similar in current directory
		for _, f := range []string{"PROJECT.md", "project.md", "DESCRIPTION.md", "description.md", "SPEC.md", "spec.md"} {
			if content, err := os.ReadFile(f); err == nil {
				printInfo("Found %s, using as project description\n", f)
				description = string(content)
				break
			}
		}
	}
	if description == "" {
		// Interactive prompt with validation
		fmt.Println()
		bold.Println("📝 Project Description (required)")
		fmt.Println("Describe the product, its purpose, target users, and key features.")
		fmt.Println("The more detail you provide, the better the generated system.")
		fmt.Println()
		
		prompt := promptui.Prompt{
			Label: "What are you building?",
			Validate: func(input string) error {
				if len(strings.TrimSpace(input)) < 20 {
					return fmt.Errorf("please provide at least 20 characters describing your project")
				}
				return nil
			},
		}
		var promptErr error
		description, promptErr = prompt.Run()
		if promptErr != nil {
			return promptErr
		}
	}
	
	// Final validation
	if len(strings.TrimSpace(description)) < 20 {
		printError("Description is too short. Please provide a meaningful description.\n")
		printInfo("Use --desc=\"...\" or --desc-file=PROJECT.md\n")
		return fmt.Errorf("description required")
	}

	// Get tech profile (after description so AI can use it)
	selection, err := selectTechProfile(baseURL, description)
	if err != nil {
		return err
	}
	profile := selection.Profile

	// Get project type - skip if AI already determined it
	pType := projectType
	if pType == "" && selection.AISelected && selection.ProjectType != "" {
		// AI determined the project type
		pType = selection.ProjectType
	} else if pType == "" {
		// Manual selection needed
		selectPrompt := promptui.Select{
			Label:    "Project Type",
			Items:    []string{"Web Application", "Mobile App", "API Service", "CLI Tool", "Game", "Library/Package", "Full Stack", "Desktop App", "ML/AI Project"},
			HideHelp: true,
		}
		_, pType, err = selectPrompt.Run()
		if err != nil {
			return err
		}
	}

	// Confirm
	fmt.Println()
	bold.Println("📋 Project Summary")
	fmt.Println("───────────────────────────────────────────────────────────")
	fmt.Printf("Name:     %s\n", name)
	fmt.Printf("Profile:  %s\n", profile.Name)
	fmt.Printf("Type:     %s\n", pType)
	if selection.AISelected {
		gray := color.New(color.FgHiBlack)
		gray.Printf("          (AI recommended)\n")
	}
	fmt.Printf("Description:\n")
	descPreview := description
	if len(descPreview) > 200 {
		descPreview = descPreview[:200] + "..."
	}
	gray := color.New(color.FgHiBlack)
	gray.Printf("  %s\n", strings.ReplaceAll(descPreview, "\n", "\n  "))

	fmt.Println()
	if !yesFlag {
		confirmPrompt := promptui.Prompt{
			Label:     "Create this project",
			IsConfirm: true,
		}
		_, err = confirmPrompt.Run()
		if err != nil {
			printInfo("Cancelled\n")
			return nil
		}
	}

	// Create project via API (includes AI generation of product definition)
	fmt.Println()
	printInfo("Creating system and generating product definition...\n")

	archResult, err := createSystem(baseURL, name, description, profile, pType)
	if err != nil {
		printError("Failed to create system: %v\n", err)
		return err
	}

	printSuccess("System created (ID: %d)\n", archResult.ID)
	
	// Show generated product definition
	if archResult.ProductDefinition != nil {
		if vision, ok := archResult.ProductDefinition["vision"].(string); ok && vision != "" {
			cyan := color.New(color.FgCyan)
			fmt.Println()
			cyan.Println("📋 Generated Product Definition:")
			fmt.Printf("   Vision: %s\n", vision)
			if mission, ok := archResult.ProductDefinition["mission"].(string); ok {
				fmt.Printf("   Mission: %s\n", mission)
			}
		}
	}

	// Create initial initiative with product definition as project overview
	printInfo("Creating initial initiative...\n")

	initID, err := createInitiative(baseURL, archResult)
	if err != nil {
		printWarning("Failed to create initiative: %v (system was created)\n", err)
	} else {
		printSuccess("Initiative created (ID: %d)\n", initID)
	}

	// Create project folder (unless --in-place)
	projectDir := "."
	slug := slugify(name)
	
	if !inPlace {
		projectDir = slug
		
		// Check if folder already exists
		if _, err := os.Stat(projectDir); err == nil {
			printError("Directory '%s' already exists\n", projectDir)
			printInfo("Use --in-place to initialize in current directory\n")
			return fmt.Errorf("directory exists")
		}
		
		fmt.Println()
		printInfo("Creating project folder: %s/\n", projectDir)
		
		if err := os.MkdirAll(projectDir, 0755); err != nil {
			printError("Failed to create directory: %v\n", err)
			return err
		}
	}

	// Initialize .modernpath if not skipped
	if !skipInit {
		printInfo("Initializing .modernpath/...\n")

		// Create .modernpath directory
		modernpathDir := filepath.Join(projectDir, ".modernpath")
		if err := os.MkdirAll(modernpathDir, 0755); err != nil {
			printWarning("Failed to create .modernpath: %v\n", err)
		} else {
			// Save config (including initiative ID)
			cfg := &config.Config{
				APIURL:           baseURL,
				SystemID:   archResult.ID,
				SystemName: name,
				SystemSlug: slug,
				InitiativeID:     initID,
				InitiativeName:   name + " - Initial Development",
			}

			configPath := filepath.Join(modernpathDir, "config.json")
			configData, _ := json.MarshalIndent(cfg, "", "  ")
			if err := os.WriteFile(configPath, configData, 0644); err != nil {
				printWarning("Failed to save config: %v\n", err)
			} else {
				printSuccess("Config saved to %s/.modernpath/config.json\n", projectDir)
			}
			
			// Create .gitignore for auth.json inside .modernpath
			innerGitignorePath := filepath.Join(modernpathDir, ".gitignore")
			innerGitignoreContent := "# ModernPath - ignore sensitive files\nauth.json\n*.log\n"
			os.WriteFile(innerGitignorePath, []byte(innerGitignoreContent), 0644)
			
			// Add .modernpath/ to project's .gitignore
			if err := addToGitignore(projectDir); err != nil {
				printWarning("Could not update .gitignore: %v\n", err)
			} else {
				printInfo("Added .modernpath/ to .gitignore\n")
			}
		}
		
		// Generate README.md with product definition
		generateProjectReadme(projectDir, archResult)
	}

	// Print next steps
	fmt.Println()
	bold.Println("✨ Project Created!")
	fmt.Println()
	fmt.Println("Next steps:")
	if !inPlace {
		fmt.Printf("  1. cd %s\n", projectDir)
		fmt.Printf("  2. View in UI:    %s/systems/%d\n", baseURL, archResult.ID)
	} else {
		fmt.Printf("  1. View in UI:    %s/systems/%d\n", baseURL, archResult.ID)
	}
	fmt.Printf("  %d. Sync docs:     modernpath sync\n", ifThen(!inPlace, 3, 2))
	fmt.Printf("  %d. Search:        modernpath search \"...\"\n", ifThen(!inPlace, 4, 3))
	fmt.Printf("  %d. Ask questions: modernpath ask \"...\"\n", ifThen(!inPlace, 5, 4))
	fmt.Println()

	return nil
}

func ifThen(cond bool, a, b int) int {
	if cond {
		return a
	}
	return b
}

// SelectionResult holds the selected profile and optionally AI-determined project type
type SelectionResult struct {
	Profile     *TechProfile
	ProjectType string // Empty if manually selected (user will choose)
	AISelected  bool
}

func selectTechProfile(baseURL string, projectDescription string) (*SelectionResult, error) {
	// Fetch profiles from API
	profiles, err := fetchTechProfiles(baseURL)
	if err != nil {
		printWarning("Failed to fetch tech profiles: %v\n", err)
		profiles = []TechProfile{}
	}

	// If profile specified via flag
	if techProfile != "" {
		// Special case: AI decides
		if strings.ToLower(techProfile) == "ai" || strings.ToLower(techProfile) == "auto" {
			aiRec, err := generateTechProfileWithAI(baseURL, projectDescription)
			if err != nil {
				return nil, err
			}
			return &SelectionResult{
				Profile:     aiRec.Profile,
				ProjectType: aiRec.ProjectType,
				AISelected:  true,
			}, nil
		}
		
		// Find matching profile
		lowerProfile := strings.ToLower(techProfile)
		for i := range profiles {
			if strings.Contains(strings.ToLower(profiles[i].Name), lowerProfile) ||
				profiles[i].ID == techProfile {
				return &SelectionResult{Profile: &profiles[i]}, nil
			}
		}
		printWarning("Profile '%s' not found, showing selection...\n", techProfile)
	}

	// Build selection items - AI option first
	items := []string{"🤖 Let AI decide (tech stack + project type)"}
	
	for _, p := range profiles {
		icon := p.Icon
		if icon == "" {
			icon = "🎯"
		}
		tags := strings.Join(p.Tags, ", ")
		
		defaultMarker := ""
		if p.IsDefault {
			defaultMarker = " ⭐"
		}
		
		if p.Description != "" {
			items = append(items, fmt.Sprintf("%s %s%s - %s [%s]", icon, p.Name, defaultMarker, p.Description, tags))
		} else {
			items = append(items, fmt.Sprintf("%s %s%s [%s]", icon, p.Name, defaultMarker, tags))
		}
	}

	// Use simpler select without the navigation hint spam
	prompt := promptui.Select{
		Label:        "Select Tech Standards Profile",
		Items:        items,
		Size:         10,
		HideHelp:     true,
		HideSelected: false,
	}

	index, _, err := prompt.Run()
	if err != nil {
		return nil, err
	}

	// Index 0 = AI decides
	if index == 0 {
		aiRec, err := generateTechProfileWithAI(baseURL, projectDescription)
		if err != nil {
			return nil, err
		}
		return &SelectionResult{
			Profile:     aiRec.Profile,
			ProjectType: aiRec.ProjectType,
			AISelected:  true,
		}, nil
	}

	// Adjust index for actual profiles (subtract 1 for AI option)
	return &SelectionResult{Profile: &profiles[index-1]}, nil
}

// AIRecommendation holds both profile and project type from AI
type AIRecommendation struct {
	Profile     *TechProfile
	ProjectType string
	Reasoning   string
}

func generateTechProfileWithAI(baseURL, projectDescription string) (*AIRecommendation, error) {
	printInfo("Asking AI to recommend tech stack and project type...\n")
	
	payload := map[string]interface{}{
		"description": projectDescription,
	}
	
	jsonPayload, _ := json.Marshal(payload)
	
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Post(baseURL+"/api/tech-profiles/generate", "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		printWarning("AI generation failed: %v\n", err)
		return nil, fmt.Errorf("AI generation unavailable")
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		printWarning("AI generation failed: %s\n", string(body))
		return nil, fmt.Errorf("AI generation failed")
	}
	
	var result struct {
		Profile     TechProfile `json:"profile"`
		ProjectType string      `json:"project_type"`
		Reasoning   string      `json:"reasoning"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	
	// Show AI's reasoning
	fmt.Println()
	bold := color.New(color.Bold)
	cyan := color.New(color.FgCyan)
	bold.Printf("🤖 AI Recommendation:\n")
	fmt.Printf("   Tech Stack: ")
	cyan.Printf("%s\n", result.Profile.Name)
	fmt.Printf("   Project Type: ")
	cyan.Printf("%s\n", result.ProjectType)
	if result.Reasoning != "" {
		gray := color.New(color.FgHiBlack)
		gray.Printf("   %s\n", result.Reasoning)
	}
	fmt.Println()
	
	return &AIRecommendation{
		Profile:     &result.Profile,
		ProjectType: result.ProjectType,
		Reasoning:   result.Reasoning,
	}, nil
}

func fetchTechProfiles(baseURL string) ([]TechProfile, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(baseURL + "/api/tech-profiles")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %d", resp.StatusCode)
	}

	var profiles []TechProfile
	if err := json.NewDecoder(resp.Body).Decode(&profiles); err != nil {
		return nil, err
	}

	return profiles, nil
}

// SystemResult holds the full response from system creation
type SystemResult struct {
	ID                int                    `json:"id"`
	Name              string                 `json:"name"`
	Slug              string                 `json:"slug"`
	Description       string                 `json:"description"`
	SystemType  string                 `json:"system_type"`
	AISummary         string                 `json:"ai_summary"`
	ProductDefinition map[string]interface{} `json:"product_definition"`
	TechStack         map[string]interface{} `json:"tech_stack"`
}

func createSystem(baseURL, name, description string, profile *TechProfile, projectType string) (*SystemResult, error) {
	payload := map[string]interface{}{
		"name":               name,
		"description":        description,
		"system_type":  projectType,
		"status":             "active",
	}
	
	// Check if this is an AI-generated profile (ID starts with "ai-generated")
	if strings.HasPrefix(profile.ID, "ai-generated") {
		// Pass tech_stack directly for AI-generated profiles
		payload["tech_stack"] = profile.TechStack
		payload["profile_name"] = profile.Name
	} else {
		// Use profile ID for real profiles
		payload["target_profile_id"] = profile.ID
	}

	jsonPayload, _ := json.Marshal(payload)

	// Longer timeout for AI generation
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Post(baseURL+"/api/systems", "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	var result SystemResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

func createInitiative(baseURL string, arch *SystemResult) (int, error) {
	// Build project overview from product definition
	projectOverview := buildProjectOverview(arch)
	
	payload := map[string]interface{}{
		"name":                    arch.Name + " - Initial Development",
		"description":             arch.Description,
		"system_id":  arch.ID,
		"status":                  "planning",
		"priority":                "high",
		"project_type":            "greenfield",
		"project_overview":        projectOverview,
		"auto_generated":          true,
		"workflow_phase":          "discovery",
		"metadata": map[string]interface{}{
			"source": "cli",
			"created_by": "modernpath new",
		},
	}

	jsonPayload, _ := json.Marshal(payload)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(baseURL+"/api/work/initiatives", "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	var result struct {
		Data struct {
			ID int `json:"id"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	return result.Data.ID, nil
}

// Build project overview markdown from system result
func buildProjectOverview(arch *SystemResult) string {
	var overview strings.Builder
	
	overview.WriteString(fmt.Sprintf("# Product Specification: %s\n\n", arch.Name))
	overview.WriteString("**Version:** 1.0\n")
	overview.WriteString("**Status:** Draft\n")
	overview.WriteString(fmt.Sprintf("**Date:** %s\n\n", time.Now().Format("January 2, 2006")))
	
	overview.WriteString("## 1. Executive Summary\n\n")
	overview.WriteString("### Product Overview\n")
	overview.WriteString(fmt.Sprintf("%s\n\n", arch.Description))
	
	if arch.ProductDefinition != nil {
		// Vision
		if vision, ok := arch.ProductDefinition["vision"].(string); ok && vision != "" {
			overview.WriteString("### Vision\n")
			overview.WriteString(fmt.Sprintf("%s\n\n", vision))
		}
		
		// Value Proposition
		if value, ok := arch.ProductDefinition["value_proposition"].(string); ok && value != "" {
			overview.WriteString("### Key Value Proposition\n")
			overview.WriteString(fmt.Sprintf("%s\n\n", value))
		}
		
		// Target Users
		if targetUsers, ok := arch.ProductDefinition["target_users"].(string); ok && targetUsers != "" {
			overview.WriteString("### Target Market/Users\n")
			overview.WriteString(fmt.Sprintf("%s\n\n", targetUsers))
		}
		
		// Mission
		if mission, ok := arch.ProductDefinition["mission"].(string); ok && mission != "" {
			overview.WriteString("## 2. Mission Statement\n\n")
			overview.WriteString(fmt.Sprintf("%s\n\n", mission))
		}
		
		// Key Features
		if features, ok := arch.ProductDefinition["key_features"].([]interface{}); ok && len(features) > 0 {
			overview.WriteString("## 3. Core Features & Capabilities\n\n")
			for i, f := range features {
				if feature, ok := f.(string); ok {
					overview.WriteString(fmt.Sprintf("### Feature %d: %s\n", i+1, feature))
					overview.WriteString("- **Priority:** Must Have\n")
					overview.WriteString("- **User Benefit:** Enables core functionality\n\n")
				}
			}
		}
		
		// Success Metrics
		if metrics, ok := arch.ProductDefinition["success_metrics"].([]interface{}); ok && len(metrics) > 0 {
			overview.WriteString("## 4. Success Metrics\n\n")
			for _, m := range metrics {
				if metric, ok := m.(string); ok {
					overview.WriteString(fmt.Sprintf("- %s\n", metric))
				}
			}
			overview.WriteString("\n")
		}
	}
	
	// Tech Stack
	if arch.TechStack != nil {
		overview.WriteString("## 5. Technical Foundation\n\n")
		
		if langs, ok := arch.TechStack["languages"].([]interface{}); ok && len(langs) > 0 {
			langStrs := make([]string, 0)
			for _, l := range langs {
				if lang, ok := l.(string); ok {
					langStrs = append(langStrs, lang)
				}
			}
			overview.WriteString(fmt.Sprintf("**Languages:** %s\n", strings.Join(langStrs, ", ")))
		}
		
		if fws, ok := arch.TechStack["frameworks"].([]interface{}); ok && len(fws) > 0 {
			fwStrs := make([]string, 0)
			for _, f := range fws {
				if fw, ok := f.(string); ok {
					fwStrs = append(fwStrs, fw)
				}
			}
			overview.WriteString(fmt.Sprintf("**Frameworks:** %s\n", strings.Join(fwStrs, ", ")))
		}
		
		overview.WriteString("\n")
	}
	
	overview.WriteString("---\n")
	overview.WriteString("*Generated by ModernPath CLI*\n")
	
	return overview.String()
}

func slugify(name string) string {
	slug := strings.ToLower(name)
	slug = strings.ReplaceAll(slug, " ", "-")
	// Remove non-alphanumeric except hyphens
	var result strings.Builder
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}
	return strings.Trim(result.String(), "-")
}

// Helper to get current directory name
func getCurrentDirName() string {
	dir, err := os.Getwd()
	if err != nil {
		return "my-project"
	}
	return filepath.Base(dir)
}

// Generate README.md with product definition
func generateProjectReadme(projectDir string, arch *SystemResult) {
	if arch == nil {
		return
	}
	
	var readme strings.Builder
	
	// Header
	readme.WriteString(fmt.Sprintf("# %s\n\n", arch.Name))
	readme.WriteString(fmt.Sprintf("> %s\n\n", arch.Description))
	
	// Product Definition
	if arch.ProductDefinition != nil {
		readme.WriteString("## Product Overview\n\n")
		
		if vision, ok := arch.ProductDefinition["vision"].(string); ok && vision != "" {
			readme.WriteString(fmt.Sprintf("**Vision:** %s\n\n", vision))
		}
		
		if mission, ok := arch.ProductDefinition["mission"].(string); ok && mission != "" {
			readme.WriteString(fmt.Sprintf("**Mission:** %s\n\n", mission))
		}
		
		if targetUsers, ok := arch.ProductDefinition["target_users"].(string); ok && targetUsers != "" {
			readme.WriteString(fmt.Sprintf("**Target Users:** %s\n\n", targetUsers))
		}
		
		if value, ok := arch.ProductDefinition["value_proposition"].(string); ok && value != "" {
			readme.WriteString(fmt.Sprintf("**Value Proposition:** %s\n\n", value))
		}
		
		// Key Features
		if features, ok := arch.ProductDefinition["key_features"].([]interface{}); ok && len(features) > 0 {
			readme.WriteString("## Key Features\n\n")
			for _, f := range features {
				if feature, ok := f.(string); ok {
					readme.WriteString(fmt.Sprintf("- %s\n", feature))
				}
			}
			readme.WriteString("\n")
		}
		
		// Success Metrics
		if metrics, ok := arch.ProductDefinition["success_metrics"].([]interface{}); ok && len(metrics) > 0 {
			readme.WriteString("## Success Metrics\n\n")
			for _, m := range metrics {
				if metric, ok := m.(string); ok {
					readme.WriteString(fmt.Sprintf("- %s\n", metric))
				}
			}
			readme.WriteString("\n")
		}
	}
	
	// Tech Stack
	if arch.TechStack != nil {
		readme.WriteString("## Tech Stack\n\n")
		
		if langs, ok := arch.TechStack["languages"].([]interface{}); ok && len(langs) > 0 {
			readme.WriteString("**Languages:** ")
			langStrs := make([]string, 0)
			for _, l := range langs {
				if lang, ok := l.(string); ok {
					langStrs = append(langStrs, lang)
				}
			}
			readme.WriteString(strings.Join(langStrs, ", "))
			readme.WriteString("\n\n")
		}
		
		if fws, ok := arch.TechStack["frameworks"].([]interface{}); ok && len(fws) > 0 {
			readme.WriteString("**Frameworks:** ")
			fwStrs := make([]string, 0)
			for _, f := range fws {
				if fw, ok := f.(string); ok {
					fwStrs = append(fwStrs, fw)
				}
			}
			readme.WriteString(strings.Join(fwStrs, ", "))
			readme.WriteString("\n\n")
		}
		
		if dbs, ok := arch.TechStack["databases"].([]interface{}); ok && len(dbs) > 0 {
			readme.WriteString("**Databases:** ")
			dbStrs := make([]string, 0)
			for _, d := range dbs {
				if db, ok := d.(string); ok {
					dbStrs = append(dbStrs, db)
				}
			}
			readme.WriteString(strings.Join(dbStrs, ", "))
			readme.WriteString("\n\n")
		}
	}
	
	// Getting Started placeholder
	readme.WriteString("## Getting Started\n\n")
	readme.WriteString("```bash\n")
	readme.WriteString("# Initialize the project\n")
	readme.WriteString("npm install  # or your package manager\n")
	readme.WriteString("\n")
	readme.WriteString("# Start development\n")
	readme.WriteString("npm run dev\n")
	readme.WriteString("```\n\n")
	
	// ModernPath section
	readme.WriteString("## ModernPath\n\n")
	readme.WriteString("This project was created with [ModernPath](https://modernpath.dev).\n\n")
	readme.WriteString("```bash\n")
	readme.WriteString("# Search documentation\n")
	readme.WriteString("modernpath search \"...\"\n")
	readme.WriteString("\n")
	readme.WriteString("# Ask questions about the codebase\n")
	readme.WriteString("modernpath ask \"How does X work?\"\n")
	readme.WriteString("\n")
	readme.WriteString("# Sync latest docs\n")
	readme.WriteString("modernpath sync\n")
	readme.WriteString("```\n")
	
	// Write README.md
	readmePath := filepath.Join(projectDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme.String()), 0644); err != nil {
		printWarning("Failed to create README.md: %v\n", err)
	} else {
		printSuccess("Generated README.md with product definition\n")
	}
}
