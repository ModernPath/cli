package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

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

var systemDocsGetCmd = &cobra.Command{Use: "get <document-id>", Short: "Read one system document", Args: cobra.ExactArgs(1), RunE: runSystemDocsGet}

var (
	systemDocsPushType        string
	systemDocsPullOutput      string
	systemDocsPullContentOnly bool
	systemDocsJSON            bool
	systemDocsGetJSON         bool
	systemDocsListLimit       int
	systemDocsListCursor      string
	systemDocsListQuery       string
	systemDocsListType        string
	systemDocsListOrder       string
)

func init() {
	rootCmd.AddCommand(systemDocsCmd)
	systemDocsCmd.AddCommand(systemDocsPushCmd)
	systemDocsCmd.AddCommand(systemDocsPullCmd)
	systemDocsCmd.AddCommand(systemDocsListCmd)
	systemDocsCmd.AddCommand(systemDocsGetCmd)

	systemDocsPushCmd.Flags().StringVarP(&systemDocsPushType, "type", "t", "other",
		"Document type: requirements, architecture, api, design, process, compliance, other")

	systemDocsPullCmd.Flags().StringVarP(&systemDocsPullOutput, "output", "o", "",
		"Output directory (default: .modernpath/system-docs/)")
	systemDocsPullCmd.Flags().BoolVar(&systemDocsPullContentOnly, "content-only", false,
		"Download only extracted content (no original files)")
	for _, command := range []*cobra.Command{systemDocsPushCmd, systemDocsPullCmd, systemDocsListCmd} {
		command.Flags().BoolVar(&systemDocsJSON, "json", false, "print machine-readable output")
		command.SilenceErrors = true
	}
	systemDocsListCmd.Flags().IntVar(&systemDocsListLimit, "limit", 20, "Documents per page (1–200)")
	systemDocsListCmd.Flags().StringVar(&systemDocsListCursor, "cursor", "", "Continue from a previous page")
	systemDocsListCmd.Flags().StringVar(&systemDocsListQuery, "query", "", "Filter by document name or description")
	systemDocsListCmd.Flags().StringVar(&systemDocsListType, "type", "", "Filter by document type")
	systemDocsListCmd.Flags().StringVar(&systemDocsListOrder, "order", "", "Order: newest, name, name_desc, updated_asc")
	addRecordReadFlags(systemDocsListCmd)
	systemDocsGetCmd.Flags().BoolVar(&systemDocsGetJSON, "json", false, "Emit selected document fields as JSON")
	addRecordReadFlags(systemDocsGetCmd)
}

// SystemDocument represents a document from the API
type SystemDocument struct {
	ID               int                    `json:"id"`
	Name             string                 `json:"name"`
	Description      string                 `json:"description"`
	DocumentType     string                 `json:"document_type"`
	UIType           string                 `json:"ui_type"`
	Provenance       string                 `json:"provenance"`
	DocumentRole     string                 `json:"document_role"`
	SourceRevision   string                 `json:"source_revision"`
	ContentHash      string                 `json:"content_hash"`
	ProcessingState  string                 `json:"processing_state"`
	FailedPhase      string                 `json:"failed_phase"`
	ProcessingError  string                 `json:"processing_error"`
	ReadyAt          string                 `json:"ready_at"`
	FileType         string                 `json:"file_type"`
	FileSize         int                    `json:"file_size"`
	Content          string                 `json:"content"`
	Summary          string                 `json:"summary"`
	HasEncryptedFile bool                   `json:"has_encrypted_file"`
	Metadata         map[string]interface{} `json:"metadata"`
	PathSegments     []string               `json:"path_segments"`
	InsertedAt       string                 `json:"inserted_at"`
	UpdatedAt        string                 `json:"updated_at"`
	raw              json.RawMessage
}

func (doc *SystemDocument) UnmarshalJSON(data []byte) error {
	type documentAlias SystemDocument
	var value documentAlias
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*doc = SystemDocument(value)
	doc.raw = append(json.RawMessage(nil), data...)
	return nil
}

func (doc SystemDocument) MarshalJSON() ([]byte, error) {
	if len(doc.raw) > 0 {
		return doc.raw, nil
	}
	type documentAlias SystemDocument
	return json.Marshal(documentAlias(doc))
}

type systemDocItemResult struct {
	DocumentID int    `json:"document_id,omitempty"`
	Name       string `json:"name,omitempty"`
	Path       string `json:"path,omitempty"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

type systemDocBatchResult struct {
	Results   []systemDocItemResult `json:"results"`
	Succeeded int                   `json:"succeeded"`
	Failed    int                   `json:"failed"`
}

func summarizeSystemDocItems(results []systemDocItemResult) systemDocBatchResult {
	result := systemDocBatchResult{Results: results}
	if result.Results == nil {
		result.Results = []systemDocItemResult{}
	}
	for _, item := range results {
		if item.Status == "succeeded" {
			result.Succeeded++
		} else {
			result.Failed++
		}
	}
	return result
}

func runSystemDocsPush(cmd *cobra.Command, args []string) error {
	cfg, err := commandConfig()
	if err != nil {
		return err
	}
	if cfg.SystemID == 0 {
		return errNoBoundSystem
	}
	jsonOutput := systemDocsJSON
	progress := cmd.OutOrStdout()
	if jsonOutput {
		progress = cmd.ErrOrStderr()
	}

	var files []string
	var results []systemDocItemResult
	for _, pattern := range args {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			results = append(results, systemDocItemResult{Path: pattern, Status: "failed", Error: fmt.Sprintf("invalid pattern: %v", err)})
			continue
		}
		if len(matches) == 0 {
			if _, err := os.Stat(pattern); err == nil && isSupportedDocType(pattern) {
				files = append(files, pattern)
			} else if err == nil {
				results = append(results, systemDocItemResult{Path: pattern, Status: "failed", Error: "unsupported file type"})
			} else {
				results = append(results, systemDocItemResult{Path: pattern, Status: "failed", Error: "no files matched"})
			}
		} else {
			for _, match := range matches {
				info, err := os.Stat(match)
				if err != nil {
					results = append(results, systemDocItemResult{Path: match, Status: "failed", Error: err.Error()})
					continue
				}
				if info.IsDir() {
					err := filepath.Walk(match, func(path string, info os.FileInfo, err error) error {
						if err != nil {
							results = append(results, systemDocItemResult{Path: path, Status: "failed", Error: err.Error()})
							return nil
						}
						if info.IsDir() {
							return nil
						}
						if isSupportedDocType(path) {
							files = append(files, path)
						}
						return nil
					})
					if err != nil {
						results = append(results, systemDocItemResult{Path: match, Status: "failed", Error: err.Error()})
					}
				} else if isSupportedDocType(match) {
					files = append(files, match)
				} else {
					results = append(results, systemDocItemResult{Path: match, Status: "failed", Error: "unsupported file type"})
				}
			}
		}
	}
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
	if len(files) == 0 && len(results) == 0 {
		results = append(results, systemDocItemResult{Status: "failed", Error: "no supported files found (.pdf, .docx, .md, .txt, .json)"})
	}
	if !jsonOutput {
		fmt.Fprintf(progress, "Uploading %d document(s) to system %s...\n", len(files), cfg.SystemName)
	}

	client := newAuthenticatedClient(cfg)
	for _, filePath := range files {
		doc, err := uploadSystemDocument(client, cfg.SystemID, filePath, systemDocsPushType)
		item := systemDocItemResult{Path: filePath, Name: filepath.Base(filePath)}
		if err != nil {
			item.Status = "failed"
			item.Error = err.Error()
		} else {
			item.Status = "succeeded"
			if doc != nil {
				item.DocumentID = doc.ID
				item.Name = doc.Name
			}
		}
		results = append(results, item)
		if !jsonOutput {
			if err != nil {
				fmt.Fprintf(progress, "  %s: failed: %v\n", item.Name, err)
			} else {
				fmt.Fprintf(progress, "  %s: uploaded\n", item.Name)
			}
		}
	}
	batch := summarizeSystemDocItems(results)
	if jsonOutput {
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(batch); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(progress, "Uploaded %d, failed %d\n", batch.Succeeded, batch.Failed)
	}
	if batch.Failed > 0 {
		return fmt.Errorf("uploaded %d document(s), %d failed", batch.Succeeded, batch.Failed)
	}
	return nil
}

func uploadSystemDocument(client *authenticatedClient, systemID int, filePath, docType string) (*SystemDocument, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("cannot open file: %w", err)
	}
	defer file.Close()

	// Create multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add file
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, fmt.Errorf("cannot create form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("cannot copy file: %w", err)
	}

	// Add document type
	if err := writer.WriteField("document_type", docType); err != nil {
		return nil, fmt.Errorf("cannot write field: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("cannot close writer: %w", err)
	}

	// Send request
	url := fmt.Sprintf("%s/api/systems/%d/documents", client.baseURL, systemID)
	req, err := http.NewRequest("POST", url, &buf)
	if err != nil {
		return nil, fmt.Errorf("cannot create request: %w", err)
	}
	platform.Prepare(req)

	req.Header.Set("Content-Type", writer.FormDataContentType())
	if err := platform.Authorize(req, client.token); err != nil {
		return nil, err
	}

	resp, err := client.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}
	var envelope struct {
		Data SystemDocument `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("failed to parse upload response: %w", err)
	}
	return &envelope.Data, nil
}

func runSystemDocsPull(cmd *cobra.Command, args []string) error {
	cfg, err := commandConfig()
	if err != nil {
		return err
	}
	if cfg.SystemID == 0 {
		return errNoBoundSystem
	}
	jsonOutput := systemDocsJSON
	progress := cmd.OutOrStdout()
	if jsonOutput {
		progress = cmd.ErrOrStderr()
	}

	// Determine output directory
	outputDir := systemDocsPullOutput
	if outputDir == "" {
		outputDir = filepath.Join(".modernpath", "system-docs")
	}

	if !jsonOutput {
		fmt.Fprintf(progress, "Downloading documents from system %s...\n", cfg.SystemName)
	}

	// Get document list
	client := newAuthenticatedClient(cfg)
	docs, err := listSystemDocuments(client, cfg.SystemID)
	if err != nil {
		return err
	}

	if len(docs) == 0 {
		if jsonOutput {
			output := struct {
				OutputDir string                `json:"output_dir"`
				Results   []systemDocItemResult `json:"results"`
				Succeeded int                   `json:"succeeded"`
				Failed    int                   `json:"failed"`
			}{OutputDir: outputDir, Results: []systemDocItemResult{}, Succeeded: 0, Failed: 0}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(output)
		}
		fmt.Fprintln(progress, "No documents found for this system.")
		return nil
	}

	if !jsonOutput {
		fmt.Fprintf(progress, "Found %d document(s)\n", len(docs))
	}

	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}

	results := make([]systemDocItemResult, 0, len(docs)+1)
	claimedPaths := map[string]string{}
	manifestPath := filepath.Join(outputDir, "manifest.json")
	claimedPaths[systemDocPathKey(manifestPath)] = "manifest.json"

	for _, doc := range docs {
		docPath, pathErr := systemDocOutputPathForDocument(outputDir, doc)
		item := systemDocItemResult{DocumentID: doc.ID, Name: doc.Name, Path: docPath}
		if pathErr != nil {
			item.Status, item.Error = "failed", pathErr.Error()
			results = append(results, item)
			continue
		}
		plannedPaths := []string{docPath}
		if systemDocsPullContentOnly {
			plannedPaths = []string{systemDocContentPath(docPath)}
		} else if !doc.HasEncryptedFile {
			plannedPaths = append(plannedPaths, systemDocContentPath(docPath))
		}
		if err := claimSystemDocPaths(claimedPaths, plannedPaths, fmt.Sprintf("document %d", doc.ID)); err != nil {
			item.Status, item.Error = "failed", err.Error()
			results = append(results, item)
			continue
		}

		if systemDocsPullContentOnly {
			actualPath, saveErr := saveDocumentContent(outputDir, doc)
			if actualPath != "" {
				item.Path = actualPath
			}
			err = saveErr
			if err != nil {
				item.Status, item.Error = "failed", err.Error()
			} else {
				item.Status = "succeeded"
			}
		} else {
			actualPath, downloadErr := downloadSystemDocument(client, cfg.SystemID, doc, outputDir)
			if actualPath != "" {
				item.Path = actualPath
			}
			err = downloadErr
			if err != nil {
				item.Status, item.Error = "failed", err.Error()
			} else {
				item.Status = "succeeded"
			}
		}
		results = append(results, item)
		if !jsonOutput {
			if item.Error != "" {
				fmt.Fprintf(progress, "  %s: failed: %s\n", doc.Name, item.Error)
			} else {
				fmt.Fprintf(progress, "  %s: downloaded\n", doc.Name)
			}
		}
	}

	manifestData, marshalErr := json.MarshalIndent(docs, "", "  ")
	if marshalErr != nil {
		results = append(results, systemDocItemResult{Path: manifestPath, Status: "failed", Error: marshalErr.Error()})
	} else if symlinkErr := rejectOutputSymlinkComponents(outputDir, manifestPath); symlinkErr != nil {
		results = append(results, systemDocItemResult{Path: manifestPath, Status: "failed", Error: symlinkErr.Error()})
	} else if writeErr := os.WriteFile(manifestPath, manifestData, 0644); writeErr != nil {
		results = append(results, systemDocItemResult{Path: manifestPath, Status: "failed", Error: writeErr.Error()})
	} else {
		results = append(results, systemDocItemResult{Path: manifestPath, Status: "succeeded", Name: "manifest.json"})
	}
	batch := summarizeSystemDocItems(results)
	if jsonOutput {
		output := struct {
			OutputDir string                `json:"output_dir"`
			Results   []systemDocItemResult `json:"results"`
			Succeeded int                   `json:"succeeded"`
			Failed    int                   `json:"failed"`
		}{OutputDir: outputDir, Results: batch.Results, Succeeded: batch.Succeeded, Failed: batch.Failed}
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(output); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(progress, "Downloaded %d, failed %d to %s\n", batch.Succeeded, batch.Failed, outputDir)
	}
	if batch.Failed > 0 {
		return fmt.Errorf("downloaded %d item(s), %d failed", batch.Succeeded, batch.Failed)
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

	if result.Data == nil {
		result.Data = []SystemDocument{}
	}
	return result.Data, nil
}

func saveDocumentContent(outputDir string, doc SystemDocument) (string, error) {
	if doc.Content == "" {
		return "", fmt.Errorf("no content available")
	}

	docPath, err := systemDocOutputPathForDocument(outputDir, doc)
	if err != nil {
		return "", err
	}
	outputPath := systemDocContentPath(docPath)
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return "", err
	}

	content := fmt.Sprintf("# %s\n\nType: %s\n\n---\n\n%s", doc.Name, doc.DocumentType, doc.Content)
	if doc.Summary != "" {
		content = fmt.Sprintf("# %s\n\nType: %s\nSummary: %s\n\n---\n\n%s", doc.Name, doc.DocumentType, doc.Summary, doc.Content)
	}

	if err := os.WriteFile(outputPath, []byte(content), 0644); err != nil {
		return "", err
	}
	return outputPath, nil
}

func systemDocContentPath(docPath string) string {
	return strings.TrimSuffix(docPath, filepath.Ext(docPath)) + ".content.txt"
}

func systemDocOutputPathForDocument(outputDir string, doc SystemDocument) (string, error) {
	var outputPath string
	var err error
	if len(doc.PathSegments) == 0 {
		outputPath, err = systemDocOutputPath(outputDir, doc.Name)
	} else {
		for _, segment := range doc.PathSegments {
			if segment == "" || segment == "." || segment == ".." ||
				strings.ContainsAny(segment, "/\\\x00") ||
				(len(segment) >= 2 && isASCIIAlpha(segment[0]) && segment[1] == ':') {
				return "", fmt.Errorf("unsafe document path segment %q", segment)
			}
		}
		outputPath, err = systemDocOutputPath(outputDir, filepath.Join(doc.PathSegments...))
	}
	if err != nil {
		return "", err
	}
	if err := rejectOutputSymlinkComponents(outputDir, outputPath); err != nil {
		return "", err
	}
	return outputPath, nil
}

func isASCIIAlpha(b byte) bool {
	return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

func systemDocPathKey(filePath string) string {
	return systemDocPathKeyForOS(filePath, runtime.GOOS)
}

func systemDocPathKeyForOS(filePath, goos string) string {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filepath.Clean(filePath)
	} else {
		absPath = filepath.Clean(absPath)
	}
	if goos == "darwin" || goos == "windows" {
		absPath = strings.ToLower(absPath)
	}
	return absPath
}

func claimSystemDocPaths(claimed map[string]string, filePaths []string, owner string) error {
	return claimSystemDocPathsForOS(claimed, filePaths, owner, runtime.GOOS)
}

func claimSystemDocPathsForOS(claimed map[string]string, filePaths []string, owner, goos string) error {
	keys := make(map[string]string, len(filePaths))
	for _, filePath := range filePaths {
		key := systemDocPathKeyForOS(filePath, goos)
		if previous, exists := claimed[key]; exists {
			return fmt.Errorf("output path conflict: %s and %s both resolve to %s", previous, owner, filePath)
		}
		keys[key] = filePath
	}
	for key := range keys {
		claimed[key] = owner
	}
	return nil
}

func rejectOutputSymlinkComponents(outputDir, outputPath string) error {
	rootAbs, err := filepath.Abs(outputDir)
	if err != nil {
		return err
	}
	targetAbs, err := filepath.Abs(outputPath)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("unsafe document path: output escapes selected directory")
	}
	// The user may choose a symlink as the output root; resolve it once, then
	// reject symlinks below that root so document paths cannot redirect writes.
	current, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return fmt.Errorf("cannot resolve output directory: %w", err)
	}
	if rel == "." {
		return nil
	}
	for _, segment := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe document path: %s traverses a symlink inside the output directory", current)
		}
	}
	return nil
}

// systemDocOutputPath accepts server-provided relative names only, and keeps
// every download inside the selected output directory on all host platforms.
func systemDocOutputPath(outputDir, name string) (string, error) {
	normalized := strings.ReplaceAll(name, "\\", "/")
	clean := filepath.Clean(filepath.FromSlash(path.Clean(normalized)))
	if normalized == "" || path.IsAbs(normalized) || filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" ||
		(len(normalized) >= 2 && ((normalized[0] >= 'A' && normalized[0] <= 'Z') || (normalized[0] >= 'a' && normalized[0] <= 'z')) && normalized[1] == ':') ||
		clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe document name %q: expected a path inside the output directory", name)
	}
	outputPath := filepath.Join(outputDir, clean)
	rel, err := filepath.Rel(outputDir, outputPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe document name %q: expected a path inside the output directory", name)
	}
	return outputPath, nil
}

func downloadSystemDocument(client *authenticatedClient, systemID int, doc SystemDocument, outputDir string) (string, error) {
	url := fmt.Sprintf("%s/api/systems/%d/documents/%d/download", client.baseURL, systemID, doc.ID)
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		if doc.HasEncryptedFile {
			return "", fmt.Errorf("encrypted original is marked available but the download returned 404; re-upload the original, or use --content-only to export extracted text")
		}
		// No original file is available, save extracted content instead.
		return saveDocumentContent(outputDir, doc)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	// Save file
	filePath, err := systemDocOutputPathForDocument(outputDir, doc)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return "", err
	}
	file, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("cannot create file: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return "", fmt.Errorf("cannot write file: %w", err)
	}

	return filePath, nil
}

func runSystemDocsList(cmd *cobra.Command, args []string) error {
	allowedFields := []string{"id", "name", "description", "document_type", "ui_type", "provenance", "document_role", "source_revision", "content_hash", "processing_state", "failed_phase", "processing_error", "ready_at", "file_type", "file_size", "summary", "has_encrypted_file", "metadata", "path_segments", "inserted_at", "updated_at"}
	if err := validateRecordSelection(cmd, allowedFields); err != nil {
		return err
	}
	cfg, err := commandConfig()
	if err != nil {
		return err
	}

	if cfg.SystemID == 0 {
		return errNoBoundSystem
	}

	client := newAuthenticatedClient(cfg)
	if systemDocsListLimit < 1 || systemDocsListLimit > 200 {
		return fmt.Errorf("--limit must be between 1 and 200")
	}
	docs, nextCursor, err := discoverSystemDocuments(client, cfg.SystemID)
	if err != nil {
		return err
	}
	rows := make([]map[string]any, 0, len(docs))
	for _, doc := range docs {
		rows = append(rows, systemDocMap(doc))
	}
	selected, err := selectSearchItems(cmd, rows, []string{"id", "name", "document_type", "processing_state"}, allowedFields, []string{"id", "name"})
	if err != nil {
		return err
	}
	if systemDocsJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"items": selected, "next_cursor": nullableCursor(nextCursor)})
	}

	for _, doc := range selected {
		if cmd.Flags().Changed("fields") || flagIsTrue(cmd, "full") {
			if err := writeRecord(cmd, doc); err != nil {
				return err
			}
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "%v  %s\n", doc["id"], strings.Join(strings.Fields(fmt.Sprint(doc["name"])), " "))
		}
	}
	if nextCursor != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Next cursor: %s (repeat the same filters with --cursor)\n", nextCursor)
	}
	return nil
}

func discoverSystemDocuments(client *authenticatedClient, systemID int) ([]SystemDocument, string, error) {
	params := url.Values{"metadata": {"true"}, "limit": {fmt.Sprint(systemDocsListLimit)}}
	for key, value := range map[string]string{"cursor": systemDocsListCursor, "q": systemDocsListQuery, "type": systemDocsListType, "order": systemDocsListOrder} {
		if value != "" {
			params.Set(key, value)
		}
	}
	endpoint := fmt.Sprintf("%s/api/systems/%d/documents?%s", client.baseURL, systemID, params.Encode())
	resp, err := client.Get(endpoint)
	if err != nil {
		return nil, "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}
	var result struct {
		Data struct {
			Documents  []SystemDocument `json:"documents"`
			NextCursor string           `json:"next_cursor"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, "", fmt.Errorf("failed to parse response: %w", err)
	}
	if result.Data.Documents == nil {
		result.Data.Documents = []SystemDocument{}
	}
	return result.Data.Documents, result.Data.NextCursor, nil
}

func systemDocMap(doc SystemDocument) map[string]any {
	var row map[string]any
	if len(doc.raw) != 0 && json.Unmarshal(doc.raw, &row) == nil {
		return row
	}
	encoded, _ := json.Marshal(doc)
	_ = json.Unmarshal(encoded, &row)
	return row
}

func runSystemDocsGet(cmd *cobra.Command, args []string) error {
	allowedFields := []string{"id", "name", "description", "document_type", "ui_type", "provenance", "document_role", "source_revision", "content_hash", "processing_state", "failed_phase", "processing_error", "ready_at", "file_type", "file_size", "content", "summary", "has_encrypted_file", "metadata", "path_segments", "inserted_at", "updated_at"}
	if err := validateRecordSelection(cmd, allowedFields); err != nil {
		return err
	}
	cfg, err := commandConfig()
	if err != nil {
		return err
	}
	if cfg.SystemID == 0 {
		return errNoBoundSystem
	}
	client := newAuthenticatedClient(cfg)
	endpoint := fmt.Sprintf("%s/api/systems/%d/documents/%s", client.baseURL, cfg.SystemID, url.PathEscape(args[0]))
	resp, err := client.Get(endpoint)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	selected, err := selectRecordFields(cmd, envelope.Data, []string{"id", "name", "document_type", "summary"}, allowedFields, []string{"id", "name"})
	if err != nil {
		return err
	}
	if systemDocsGetJSON {
		return writeSearchJSON(cmd.OutOrStdout(), selected)
	}
	return writeRecord(cmd, selected)
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
