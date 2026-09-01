package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/platform"
	"github.com/spf13/cobra"
)

var systemDocsCmd = &cobra.Command{
	Use:   "system-docs",
	Short: "Manage system documents (requirements, architecture docs, etc.)",
	Long: `Upload and download system documents from the ModernPath platform.

System documents include requirements, architecture docs, API specs,
design documents, and other files that describe your system.

Examples:
  modernpath system-docs push ./docs/*.pdf ./requirements/**/*.md
  modernpath system-docs push ./PRD.pdf --type requirements
  modernpath system-docs pull
  modernpath system-docs pull --output ./local-docs/
  modernpath system-docs list`,
}

var systemDocsPushCmd = &cobra.Command{
	Use:   "push [files...]",
	Short: "Upload documents to ModernPath",
	Long: `Upload documents from your local filesystem to ModernPath.

Supports glob patterns for batch uploads:
  modernpath system-docs push ./docs/*.pdf
  modernpath system-docs push ./requirements/**/*.md ./specs/*.docx
  modernpath system-docs push ./PRD.pdf --type requirements

Files are encrypted on the server and indexed for AI search.
Existing documents with the same filename are updated.

Supported file types: .pdf, .docx, .md, .txt, .json`,
	Args: cobra.MinimumNArgs(1),
	RunE: runSystemDocsPush,
}

var systemDocsPullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Download documents from ModernPath",
	Long: `Download system documents to your local filesystem.

Documents are downloaded to .modernpath/system-docs/ by default,
or to the path specified with --output.

Examples:
  modernpath system-docs pull
  modernpath system-docs pull --output ./local-docs/
  modernpath system-docs pull --content-only`,
	RunE: runSystemDocsPull,
}

var systemDocsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List system documents",
	Long:  `List all documents attached to the current system.`,
	RunE:  runSystemDocsList,
}

var (
	systemDocsPushType        string
	systemDocsPullOutput      string
	systemDocsPullContentOnly bool
)

func init() {
	rootCmd.AddCommand(systemDocsCmd)
	systemDocsCmd.AddCommand(systemDocsPushCmd)
	systemDocsCmd.AddCommand(systemDocsPullCmd)
	systemDocsCmd.AddCommand(systemDocsListCmd)

	systemDocsPushCmd.Flags().StringVarP(&systemDocsPushType, "type", "t", "other",
		"Document type: requirements, architecture, api, design, process, compliance, other")

	systemDocsPullCmd.Flags().StringVarP(&systemDocsPullOutput, "output", "o", "",
		"Output directory (default: .modernpath/system-docs/)")
	systemDocsPullCmd.Flags().BoolVar(&systemDocsPullContentOnly, "content-only", false,
		"Download only extracted content (no original files)")
}

// SystemDocument represents a document from the API
type SystemDocument struct {
	ID               int                    `json:"id"`
	Name             string                 `json:"name"`
	Description      string                 `json:"description"`
	DocumentType     string                 `json:"document_type"`
	FileType         string                 `json:"file_type"`
	FileSize         int                    `json:"file_size"`
	Content          string                 `json:"content"`
	Summary          string                 `json:"summary"`
	HasEncryptedFile bool                   `json:"has_encrypted_file"`
	Metadata         map[string]interface{} `json:"metadata"`
	InsertedAt       string                 `json:"inserted_at"`
	UpdatedAt        string                 `json:"updated_at"`
}

func runSystemDocsPush(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.SystemID == 0 {
		printError("No system configured. Run 'modernpath init' first.\n")
		return nil
	}

	// Expand glob patterns and collect files
	var files []string
	for _, pattern := range args {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			printWarning("Invalid pattern %s: %v\n", pattern, err)
			continue
		}
		if len(matches) == 0 {
			// Try as literal path
			if _, err := os.Stat(pattern); err == nil {
				files = append(files, pattern)
			} else {
				printWarning("No files matched: %s\n", pattern)
			}
		} else {
			for _, match := range matches {
				info, err := os.Stat(match)
				if err != nil {
					continue
				}
				if info.IsDir() {
					// Walk directory for supported files
					err := filepath.Walk(match, func(path string, info os.FileInfo, err error) error {
						if err != nil || info.IsDir() {
							return nil
						}
						if isSupportedDocType(path) {
							files = append(files, path)
						}
						return nil
					})
					if err != nil {
						printWarning("Error walking %s: %v\n", match, err)
					}
				} else if isSupportedDocType(match) {
					files = append(files, match)
				}
			}
		}
	}

	if len(files) == 0 {
		printError("No supported files found.\n")
		printInfo("Supported types: .pdf, .docx, .md, .txt, .json\n")
		return nil
	}

	// Remove duplicates
	seen := make(map[string]bool)
	var uniqueFiles []string
	for _, f := range files {
		absPath, _ := filepath.Abs(f)
		if !seen[absPath] {
			seen[absPath] = true
			uniqueFiles = append(uniqueFiles, f)
		}
	}
	files = uniqueFiles

	fmt.Println()
	printInfo("📤 Uploading %d document(s) to system %s...\n", len(files), cfg.SystemName)
	fmt.Println()

	client := newAuthenticatedClient(cfg)
	successCount := 0
	failCount := 0

	for _, filePath := range files {
		filename := filepath.Base(filePath)
		fmt.Printf("  • %s ... ", filename)

		err := uploadSystemDocument(client, cfg.SystemID, filePath, systemDocsPushType)
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			failCount++
		} else {
			fmt.Printf("✅\n")
			successCount++
		}
	}

	fmt.Println()
	if failCount == 0 {
		printSuccess("Successfully uploaded %d document(s)!\n", successCount)
	} else {
		printWarning("Uploaded %d, failed %d\n", successCount, failCount)
	}

	return nil
}

func uploadSystemDocument(client *authenticatedClient, systemID int, filePath, docType string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("cannot open file: %w", err)
	}
	defer file.Close()

	// Create multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add file
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return fmt.Errorf("cannot create form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return fmt.Errorf("cannot copy file: %w", err)
	}

	// Add document type
	if err := writer.WriteField("document_type", docType); err != nil {
		return fmt.Errorf("cannot write field: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("cannot close writer: %w", err)
	}

	// Send request
	url := fmt.Sprintf("%s/api/systems/%d/documents", client.baseURL, systemID)
	req, err := http.NewRequest("POST", url, &buf)
	if err != nil {
		return fmt.Errorf("cannot create request: %w", err)
	}
	platform.Prepare(req)

	req.Header.Set("Content-Type", writer.FormDataContentType())
	if err := platform.Authorize(req, client.token); err != nil {
		return err
	}

	resp, err := client.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func runSystemDocsPull(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.SystemID == 0 {
		printError("No system configured. Run 'modernpath init' first.\n")
		return nil
	}

	// Determine output directory
	outputDir := systemDocsPullOutput
	if outputDir == "" {
		outputDir = filepath.Join(".modernpath", "system-docs")
	}

	fmt.Println()
	printInfo("📥 Downloading documents from system %s...\n", cfg.SystemName)

	// Get document list
	client := newAuthenticatedClient(cfg)
	docs, err := listSystemDocuments(client, cfg.SystemID)
	if err != nil {
		printError("Failed to list documents: %v\n", err)
		return err
	}

	if len(docs) == 0 {
		printInfo("No documents found for this system.\n")
		return nil
	}

	fmt.Printf("Found %d document(s)\n\n", len(docs))

	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		printError("Failed to create output directory: %v\n", err)
		return err
	}

	successCount := 0
	failCount := 0

	for _, doc := range docs {
		fmt.Printf("  • %s ... ", doc.Name)

		if systemDocsPullContentOnly {
			// Just save extracted content
			err := saveDocumentContent(outputDir, doc)
			if err != nil {
				fmt.Printf("❌ %v\n", err)
				failCount++
			} else {
				fmt.Printf("✅ (content)\n")
				successCount++
			}
		} else {
			// Download actual file
			err := downloadSystemDocument(client, cfg.SystemID, doc, outputDir)
			if err != nil {
				fmt.Printf("❌ %v\n", err)
				failCount++
			} else {
				fmt.Printf("✅\n")
				successCount++
			}
		}
	}

	// Save manifest
	manifestPath := filepath.Join(outputDir, "manifest.json")
	manifestData, _ := json.MarshalIndent(docs, "", "  ")
	os.WriteFile(manifestPath, manifestData, 0644)

	fmt.Println()
	if failCount == 0 {
		printSuccess("Downloaded %d document(s) to %s\n", successCount, outputDir)
	} else {
		printWarning("Downloaded %d, failed %d\n", successCount, failCount)
	}

	return nil
}

func listSystemDocuments(client *authenticatedClient, systemID int) ([]SystemDocument, error) {
	url := fmt.Sprintf("%s/api/systems/%d/documents", client.baseURL, systemID)
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []SystemDocument `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result.Data, nil
}

func saveDocumentContent(outputDir string, doc SystemDocument) error {
	if doc.Content == "" {
		return fmt.Errorf("no content available")
	}

	// Save content as .txt file
	filename := strings.TrimSuffix(doc.Name, filepath.Ext(doc.Name)) + ".content.txt"
	path := filepath.Join(outputDir, filename)

	content := fmt.Sprintf("# %s\n\nType: %s\n\n---\n\n%s", doc.Name, doc.DocumentType, doc.Content)
	if doc.Summary != "" {
		content = fmt.Sprintf("# %s\n\nType: %s\nSummary: %s\n\n---\n\n%s", doc.Name, doc.DocumentType, doc.Summary, doc.Content)
	}

	return os.WriteFile(path, []byte(content), 0644)
}

func downloadSystemDocument(client *authenticatedClient, systemID int, doc SystemDocument, outputDir string) error {
	url := fmt.Sprintf("%s/api/systems/%d/documents/%d/download", client.baseURL, systemID, doc.ID)
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// No file available, save content instead
		return saveDocumentContent(outputDir, doc)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	// Save file
	path := filepath.Join(outputDir, doc.Name)
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("cannot create file: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("cannot write file: %w", err)
	}

	return nil
}

func runSystemDocsList(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.SystemID == 0 {
		printError("No system configured. Run 'modernpath init' first.\n")
		return nil
	}

	client := newAuthenticatedClient(cfg)
	docs, err := listSystemDocuments(client, cfg.SystemID)
	if err != nil {
		printError("Failed to list documents: %v\n", err)
		return err
	}

	fmt.Println()
	fmt.Printf("📄 System Documents for %s\n", cfg.SystemName)
	fmt.Println("═══════════════════════════════════════════════════════════")

	if len(docs) == 0 {
		fmt.Println("No documents found.")
		fmt.Println()
		printInfo("Upload documents with: modernpath system-docs push <files...>\n")
		return nil
	}

	for _, doc := range docs {
		sizeStr := formatFileSize(doc.FileSize)
		typeIcon := getDocTypeIcon(doc.DocumentType)
		fmt.Printf("  %s %s (%s, %s)\n", typeIcon, doc.Name, doc.DocumentType, sizeStr)
		if doc.Summary != "" {
			// Truncate summary
			summary := doc.Summary
			if len(summary) > 80 {
				summary = summary[:77] + "..."
			}
			fmt.Printf("     └─ %s\n", summary)
		}
	}

	fmt.Println()
	fmt.Printf("Total: %d document(s)\n", len(docs))
	return nil
}

func isSupportedDocType(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	supported := []string{".pdf", ".docx", ".md", ".txt", ".json"}
	for _, s := range supported {
		if ext == s {
			return true
		}
	}
	return false
}

func formatFileSize(bytes int) string {
	if bytes == 0 {
		return "unknown size"
	}
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
}

func getDocTypeIcon(docType string) string {
	icons := map[string]string{
		"requirements": "📋",
		"architecture": "🏗️",
		"api":          "🔌",
		"design":       "🎨",
		"process":      "⚙️",
		"compliance":   "🔒",
		"other":        "📄",
	}
	if icon, ok := icons[docType]; ok {
		return icon
	}
	return "📄"
}
