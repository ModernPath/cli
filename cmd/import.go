package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/platform"
	"github.com/spf13/cobra"
)

var (
	importLocal     bool
	importYes       bool
	importName      string
	importMaxSizeMB int
	importExclude   []string
	importKeep      []string
	importNoIgnore  bool
)

const (
	defaultMaxSizeMB = 100 // 100 MiB default max zip size
	// REQ-CROSS-500 AC3: --max-size bounds the zip as uploaded.
	maxSizeUsage = "Maximum size of the compressed zip in MiB (the server's upload limit applies as well)"
	// REQ-CROSS-501 AC2/AC3: the matching matchesExclude does, and --keep.
	excludeUsage = "Leave out matching paths (repeatable). Without /, a name or glob matches a file or directory " +
		"at any depth (fixtures, *.min.js); with /, a path glob matches from the top, * within one directory " +
		"and ** across any depth (src/*/fixtures, **/generated/**)"
	keepUsage     = "Keep a directory the built-in filter skips, by name, such as packages (repeatable)"
	noIgnoreUsage = "Upload files that .gitignore excludes (off by default)"
	// REQ-CROSS-502 AC3: import's upload flags say so in import's help.
	localUploadLabel = "Local upload: "
)

// REQ-CROSS-502 (EPIC-CLI-029, USER:2026-09-29 D7): import uploads the local
// files and nothing else — the git-URL import the server answers 410 is
// removed (D9) — runs unattended with --yes, refuses without a terminal and
// without --yes instead of exiting 0, and its next steps name the lifecycle
// verbs and link to the app host.
var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import the codebase in this directory into ModernPath",
	Long: `Import the codebase in the current directory as a new ModernPath system.

The CLI packs the directory into a zip with the upload filters, shows a
summary, asks for a confirmation and uploads the zip. The server creates the
system and its repository, stores the source and queues the analysis. The
binding is saved to .modernpath/config.json; keep the system current with
'modernpath source push' and follow the analysis with
'modernpath analysis status'.

--yes skips the confirmation. Without a terminal --yes is required: the
command exits non-zero instead of waiting for an answer, so a CI job fails
loudly rather than succeeding without importing.

Local upload flags:
  --exclude <pattern>   leave out matching paths (repeatable): a name or glob
                        without / at any depth, a path glob with / from the
                        top, where * stays within one directory and ** spans
                        any depth
  --keep <name>         upload a directory the built-in filter skips
  --no-gitignore        upload files that .gitignore excludes
  --max-size <MiB>      the largest compressed zip to upload

The scan report lists what each filter left out. Before sending, the request
is compared with the server's upload limit; one that does not fit is refused
with the zip size, the limit, the largest top-level directories and an
--exclude example.

Examples:
  modernpath import                          # summary, confirmation, upload
  modernpath import --yes                    # unattended, for CI
  modernpath import --name="My App"          # system name (default: folder name)
  modernpath import --exclude fixtures --keep packages`,
	// Each error is printed once where it happens (REQ-CROSS-500, D4).
	SilenceErrors: true,
	RunE:          runImport,
}

func init() {
	importCmd.Flags().BoolVar(&importLocal, "local", false, "Upload the local files (the default and the only import method)")
	importCmd.Flags().BoolVarP(&importYes, "yes", "y", false, "Import without asking (required without a terminal)")
	importCmd.Flags().StringVar(&importName, "name", "", "System name (default: folder name)")
	importCmd.Flags().IntVar(&importMaxSizeMB, "max-size", defaultMaxSizeMB, localUploadLabel+maxSizeUsage)
	importCmd.Flags().StringArrayVar(&importExclude, "exclude", nil, localUploadLabel+excludeUsage)
	importCmd.Flags().StringArrayVar(&importKeep, "keep", nil, localUploadLabel+keepUsage)
	importCmd.Flags().BoolVar(&importNoIgnore, "no-gitignore", false, localUploadLabel+noIgnoreUsage)

	rootCmd.AddCommand(importCmd)
}

func runImport(cmd *cobra.Command, args []string) error {
	bold := color.New(color.Bold)
	cyan := color.New(color.FgCyan)

	fmt.Println()
	bold.Println("📦 ModernPath Import")
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println()

	// Get current directory info
	cwd, err := os.Getwd()
	if err != nil {
		return reportFailure(fmt.Errorf("failed to get current directory: %w", err))
	}
	folderName := filepath.Base(cwd)

	fmt.Printf("📁 Directory: %s\n", cwd)
	if origin := detectGitRemote(); origin != "" {
		cyan.Printf("🔗 Git remote origin: %s\n", origin)
	} else {
		fmt.Println("🔗 Git remote origin: none")
	}
	fmt.Println()

	// Get API URL and check connection
	baseURL := getAPIURL()
	client := &http.Client{Timeout: 10 * time.Second}

	// Check auth
	auth, _ := config.ReadAuth()
	if auth == nil || auth.Token == "" {
		return reportFailure(fmt.Errorf("not authenticated; run 'modernpath auth' first"))
	}

	// Health check
	healthReq, _, err := healthProbe(baseURL)
	if err != nil {
		return reportFailure(err)
	}
	resp, err := client.Do(healthReq)
	if err != nil {
		return reportFailure(fmt.Errorf("cannot connect to ModernPath at %s: %w", baseURL, err))
	}
	resp.Body.Close()

	// Get system name
	archName := importName
	if archName == "" {
		archName = folderName
	}

	// Confirm before proceeding
	bold.Println("📋 Import Summary")
	fmt.Println("───────────────────────────────────────────────────────────")
	fmt.Printf("Name:     %s\n", archName)
	fmt.Printf("Source:   %s (packed and uploaded as a zip)\n", cwd)
	fmt.Printf("API:      %s\n", baseURL)
	fmt.Println()

	ok, err := confirmAction("Proceed with import", importYes, false)
	if err != nil {
		return reportFailure(err)
	}
	if !ok {
		printInfo("Import cancelled.\n")
		return nil
	}

	fmt.Println()
	return importViaUpload(baseURL, auth.Token, archName, cwd)
}

// detectGitRemote is the URL of the checkout's origin remote, shown for
// reference; "" when there is none.
func detectGitRemote() string {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func getAPIURL() string {
	if apiURL != "" {
		return apiURL
	}
	cfg, _ := config.ReadConfig()
	if cfg != nil && cfg.APIURL != "" {
		return cfg.APIURL
	}
	return config.DefaultAPIURL
}

// importFilterOptions are the filters `import --local` and `source push`
// apply alike (REQ-SYS-211 AC1): the skip lists less the directories --keep
// names, .gitignore through `git check-ignore`, --exclude and --max-size.
type importFilterOptions struct {
	Exclude     []string
	Keep        []string
	MaxSizeMB   int
	NoGitignore bool
}

// importFile is one file the archive will hold: its path on disk and its
// slash-separated path inside the archive.
type importFile struct {
	abs  string
	rel  string
	size int64
}

// importReport is what the filters dropped, for the operator's eyes
// (REQ-CROSS-172): a filter that silently removes most of a codebase is
// indistinguishable from one that found a small codebase.
type importReport struct {
	Files        int
	TotalSize    int64
	Ignored      int
	SizeIgnored  int64
	Excluded     int
	SizeExcluded int64
	// BuiltIn is what each built-in rule dropped, largest first
	// (REQ-CROSS-501 AC1).
	BuiltIn []filterDrop
	// Kept is the built-in directory drops --keep turned off (AC3).
	Kept []string
}

// filterDrop is what one built-in rule dropped: a directory name ("bin/"), an
// extension ("*.svg") or a file name (".env").
type filterDrop struct {
	Rule  string
	Files int
	Bytes int64
}

// collectImportFiles walks sourceDir with the import filters and returns the
// files an archive of it holds, in walk order, with the filter report.
func collectImportFiles(sourceDir string, opts importFilterOptions) ([]importFile, importReport, error) {
	var cands []importFile
	var report importReport

	keep, err := keptDirectories(opts.Keep)
	if err != nil {
		return nil, report, err
	}
	if err := checkExcludePatterns(opts.Exclude); err != nil {
		return nil, report, err
	}
	builtIn := map[string]*filterDrop{}
	drop := func(rule string, files int, size int64) {
		if builtIn[rule] == nil {
			builtIn[rule] = &filterDrop{Rule: rule}
		}
		builtIn[rule].Files += files
		builtIn[rule].Bytes += size
	}

	err = filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Skip directories we don't want, counting what they held.
		if info.IsDir() {
			base := filepath.Base(path)
			if shouldSkipImportDir(base) && !keep[base] {
				files, size := treeSize(path)
				drop(base+"/", files, size)
				return filepath.SkipDir
			}
			return nil
		}

		// Skip files we don't want
		if rule := builtInFileRule(info.Name()); rule != "" {
			drop(rule, 1, info.Size())
			return nil
		}

		rel, relErr := filepath.Rel(sourceDir, path)
		if relErr != nil {
			return nil
		}
		cands = append(cands, importFile{path, filepath.ToSlash(rel), info.Size()})
		return nil
	})
	if err != nil {
		return nil, report, fmt.Errorf("failed to scan directory: %w", err)
	}

	// REQ-CROSS-172: drop what the repository already calls junk, and what the
	// operator named. Both are REPORTED.
	ignored := make(map[string]bool)
	if !opts.NoGitignore {
		rels := make([]string, 0, len(cands))
		for _, c := range cands {
			rels = append(rels, c.rel)
		}
		if ignored, err = gitIgnoredSet(sourceDir, rels); err != nil {
			return nil, report, fmt.Errorf("failed to consult .gitignore: %w", err)
		}
	}

	var files []importFile
	for _, c := range cands {
		switch {
		case ignored[c.rel]:
			report.SizeIgnored += c.size
			report.Ignored++
		case matchesExclude(c.rel, opts.Exclude):
			report.SizeExcluded += c.size
			report.Excluded++
		default:
			files = append(files, c)
			report.TotalSize += c.size
		}
	}
	report.Files = len(files)
	for _, d := range builtIn {
		report.BuiltIn = append(report.BuiltIn, *d)
	}
	sort.Slice(report.BuiltIn, func(i, j int) bool {
		if report.BuiltIn[i].Bytes != report.BuiltIn[j].Bytes {
			return report.BuiltIn[i].Bytes > report.BuiltIn[j].Bytes
		}
		return report.BuiltIn[i].Rule < report.BuiltIn[j].Rule
	})
	for name := range keep {
		report.Kept = append(report.Kept, name+"/")
	}
	sort.Strings(report.Kept)
	return files, report, nil
}

// treeSize counts the files under dir and their bytes.
func treeSize(dir string) (files int, size int64) {
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			files++
			size += info.Size()
		}
		return nil
	})
	return files, size
}

// printImportReport prints what the scan found and what every filter left
// out (REQ-CROSS-172, REQ-CROSS-501): .gitignore, --exclude, each built-in
// rule, and the built-in directories --keep kept.
func printImportReport(w io.Writer, report importReport) {
	fmt.Fprintf(w, "  Files: %d\n", report.Files)
	fmt.Fprintf(w, "  Size:  %s\n", byteCount(report.TotalSize))
	if report.Ignored > 0 {
		fmt.Fprintf(w, "  Skipped (.gitignore): %s, %s\n", fileCount(report.Ignored), byteCount(report.SizeIgnored))
	}
	if report.Excluded > 0 {
		fmt.Fprintf(w, "  Skipped (--exclude):  %s, %s\n", fileCount(report.Excluded), byteCount(report.SizeExcluded))
	}
	if len(report.BuiltIn) > 0 {
		fmt.Fprintln(w, "  Skipped (built-in):")
		width := 0
		for _, d := range report.BuiltIn {
			width = max(width, len(d.Rule))
		}
		directories := false
		for _, d := range report.BuiltIn {
			fmt.Fprintf(w, "    %-*s  %s, %s\n", width, d.Rule, fileCount(d.Files), byteCount(d.Bytes))
			directories = directories || strings.HasSuffix(d.Rule, "/")
		}
		if directories {
			fmt.Fprintln(w, "  Upload a skipped directory with --keep <name>.")
		}
	}
	if len(report.Kept) > 0 {
		fmt.Fprintf(w, "  Kept (--keep): %s\n", strings.Join(report.Kept, ", "))
	}
}

func fileCount(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

// zipFiles packs exactly the given files, in memory. The set was decided by
// collectImportFiles; zipping walks that list rather than the tree again, so
// the archive cannot disagree with the size that was checked or the digest
// that was computed.
func zipFiles(files []importFile) (*bytes.Buffer, error) {
	var zipBuffer bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuffer)
	for _, c := range files {
		writer, err := zipWriter.Create(c.rel)
		if err != nil {
			return nil, fmt.Errorf("failed to create zip: %w", err)
		}
		file, err := os.Open(c.abs)
		if err != nil {
			continue // Skip files we can't read
		}
		_, err = io.Copy(writer, file)
		file.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to create zip: %w", err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		return nil, err
	}
	return &zipBuffer, nil
}

// importViaUpload packs sourceDir, checks the zip against --max-size and the
// request body against the server's upload limit (REQ-CROSS-500), and posts
// it. Every failure is printed once and returned as a reportedError.
func importViaUpload(baseURL, token, name, sourceDir string) error {
	printInfo("Scanning directory...\n")

	files, report, err := collectImportFiles(sourceDir, importFilterOptions{
		Exclude:     importExclude,
		Keep:        importKeep,
		MaxSizeMB:   importMaxSizeMB,
		NoGitignore: importNoIgnore,
	})
	if err != nil {
		return reportFailure(err)
	}
	printImportReport(os.Stdout, report)

	printInfo("Creating zip archive...\n")

	zipBuffer, err := zipFiles(files)
	if err != nil {
		return reportFailure(err)
	}
	fmt.Printf("  Zip size: %s\n", byteCount(int64(zipBuffer.Len())))
	sizes := uploadSizes{command: "modernpath import --local", zip: zipBuffer.Bytes()}
	if int64(zipBuffer.Len()) > int64(importMaxSizeMB)*1024*1024 {
		return reportFailure(sizes.overMaxSize(importMaxSizeMB))
	}

	// Create multipart request
	var requestBody bytes.Buffer
	mpWriter := multipart.NewWriter(&requestBody)

	// Add name field
	mpWriter.WriteField("name", name)
	mpWriter.WriteField("import_type", "upload")

	// Add zip file
	part, err := mpWriter.CreateFormFile("file", name+".zip")
	if err != nil {
		return reportFailure(fmt.Errorf("failed to create form: %w", err))
	}
	part.Write(zipBuffer.Bytes())
	mpWriter.Close()
	sizes.body = int64(requestBody.Len())

	// One read gives the upload limit and the app address the next steps
	// link to (D2).
	capabilities, err := readUploadCapabilities(&authenticatedClient{client: &http.Client{Timeout: 30 * time.Second}, token: token, baseURL: baseURL})
	if err != nil {
		return reportFailure(err)
	}
	limit := capabilities.limit
	if limit.note != "" {
		printWarning("%s\n", limit.note)
	}
	if sizes.body > limit.bytes {
		return reportFailure(sizes.overLimit(limit.bytes))
	}

	printInfo("Uploading to ModernPath...\n")

	req, _ := http.NewRequest("POST", baseURL+"/api/systems/import", &requestBody)
	platform.Prepare(req)
	req.Header.Set("Content-Type", mpWriter.FormDataContentType())
	if err := platform.Authorize(req, token); err != nil {
		return reportFailure(err)
	}

	// Longer timeout for upload
	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return reportFailure(fmt.Errorf("upload failed: %w", err))
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusRequestEntityTooLarge {
		return reportFailure(sizes.refused(bodyTooLarge(body).limitOr(limit.bytes)))
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return reportFailure(pushRefusal(resp, body))
	}

	var result struct {
		Success bool `json:"success"`
		Data    struct {
			ID           int    `json:"id"`
			Name         string `json:"name"`
			Slug         string `json:"slug"`
			RepositoryID int    `json:"repository_id"`
		} `json:"data"`
		Message string `json:"message"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return reportFailure(fmt.Errorf("failed to parse response: %w", err))
	}

	if err := handleImportSuccess(baseURL, result.Data.ID, result.Data.Name, result.Data.Slug, result.Data.RepositoryID); err != nil {
		return err
	}
	printImportNextSteps(systemAppLink(capabilities.appURL, baseURL, result.Data.ID, "overview"), result.Data.RepositoryID)
	return nil
}

// handleImportSuccess records the binding in <cwd>/.modernpath/config.json —
// the init shape, never the parent-binding writer, which would rebind an
// enclosing workspace — merging into the file's other fields (REQ-SYS-211
// AC6). repositoryID is the upload repository `source push` targets; 0 when
// the import created none.
func handleImportSuccess(baseURL string, archID int, archName, archSlug string, repositoryID int) error {
	fmt.Println()
	printSuccess("System created successfully!\n")
	fmt.Printf("  ID:   %d\n", archID)
	fmt.Printf("  Name: %s\n", archName)
	fmt.Printf("  Slug: %s\n", archSlug)
	if repositoryID != 0 {
		fmt.Printf("  Repository: %d\n", repositoryID)
	}

	// Save config
	cwd, _ := os.Getwd()
	modernpathDir := filepath.Join(cwd, ".modernpath")

	if err := os.MkdirAll(modernpathDir, 0755); err == nil {
		configPath := filepath.Join(modernpathDir, "config.json")
		cfg := &config.Config{}
		if existing, err := os.ReadFile(configPath); err == nil {
			_ = json.Unmarshal(existing, cfg)
		}
		cfg.APIURL = baseURL
		cfg.SystemID = archID
		cfg.SystemName = archName
		cfg.SystemSlug = archSlug
		cfg.RepositoryID = repositoryID

		configData, _ := json.MarshalIndent(cfg, "", "  ")
		if err := os.WriteFile(configPath, configData, 0644); err == nil {
			printSuccess("Config saved to .modernpath/config.json\n")
		}

		// Create .gitignore
		gitignorePath := filepath.Join(modernpathDir, ".gitignore")
		gitignoreContent := "# ModernPath - ignore sensitive files\nauth.json\n*.log\n"
		os.WriteFile(gitignorePath, []byte(gitignoreContent), 0644)
	}

	return nil
}

// printImportNextSteps tells what happens after an upload (REQ-CROSS-502
// AC4): the server has queued the analysis, which the lifecycle verbs of
// REQ-CROSS-503 follow, start, re-run and reset; source push sends new
// commits. link opens the system in the app.
func printImportNextSteps(link string, repositoryID int) {
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  The analysis of the uploaded source is queued.")
	fmt.Println("  1. Follow the analysis:  modernpath analysis status")
	fmt.Printf("  2. View in UI:           %s\n", link)
	step := 3
	if repositoryID != 0 {
		fmt.Printf("  %d. Push new commits:     modernpath source push\n", step)
		step++
	}
	fmt.Printf("  %d. Sync documentation:   modernpath docs sync, once the analysis is done\n", step)
	fmt.Println("  To start, re-run or reset the analysis: modernpath analysis start | reanalyze | reset")
	fmt.Println()
}

// The built-in skip lists (REQ-CROSS-501): directory names dropped at any
// depth, and file extensions and names. Every drop is counted in the report.
var (
	builtInSkipDirs = []string{
		".git", ".svn", ".hg", ".modernpath",
		"node_modules", "deps", "_build", "vendor", "packages",
		"dist", "build", "target", "out", ".next", ".nuxt",
		"bin", "obj", "TestResults",
		".vscode", ".idea", ".vs",
		".tmp", ".cache", ".pytest_cache", "__pycache__", ".coverage",
		".DS_Store", "Thumbs.db",
	}
	// Large binary files and common non-code files.
	builtInSkipExtensions = []string{
		".exe", ".dll", ".so", ".dylib", ".a", ".o",
		".zip", ".tar", ".gz", ".rar", ".7z",
		".pdf", ".doc", ".docx", ".xls", ".xlsx",
		".png", ".jpg", ".jpeg", ".gif", ".bmp", ".ico", ".svg",
		".mp3", ".mp4", ".avi", ".mov", ".wav",
		".ttf", ".otf", ".woff", ".woff2", ".eot",
		".sqlite", ".db",
	}
	builtInSkipFiles = []string{
		".DS_Store", "Thumbs.db", ".env", ".env.local",
		"package-lock.json", "yarn.lock", "pnpm-lock.yaml",
	}
	// Built-in directory drops --keep never turns off, and why.
	unkeepableDirs = map[string]string{
		".modernpath": "it holds the workspace binding and the sign-in credential",
		".git":        "it holds version control data, not source",
		".svn":        "it holds version control data, not source",
		".hg":         "it holds version control data, not source",
	}
)

func shouldSkipImportDir(name string) bool {
	return slices.Contains(builtInSkipDirs, name)
}

// builtInFileRule is the built-in rule that drops a file by its name — its
// extension ("*.svg") or the name itself (".env") — or "" when none does.
func builtInFileRule(name string) string {
	if ext := strings.ToLower(filepath.Ext(name)); slices.Contains(builtInSkipExtensions, ext) {
		return "*" + ext
	}
	if slices.Contains(builtInSkipFiles, name) {
		return name
	}
	return ""
}

// keptDirectories checks the --keep names (REQ-CROSS-501 AC3, D6): each must
// be a built-in directory drop that may be uploaded.
func keptDirectories(names []string) (map[string]bool, error) {
	keep := map[string]bool{}
	for _, name := range names {
		name = strings.Trim(strings.TrimSpace(name), "/")
		if reason, ok := unkeepableDirs[name]; ok {
			return nil, fmt.Errorf("--keep %s is not allowed: %s", name, reason)
		}
		if !shouldSkipImportDir(name) {
			var keepable []string
			for _, dir := range builtInSkipDirs {
				if _, ok := unkeepableDirs[dir]; !ok {
					keepable = append(keepable, dir)
				}
			}
			return nil, fmt.Errorf("--keep %s is not a directory the built-in filter skips; --keep takes one of: %s",
				name, strings.Join(keepable, ", "))
		}
		keep[name] = true
	}
	return keep, nil
}
