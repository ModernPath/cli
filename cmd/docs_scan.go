package cmd

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

type ScanResult struct {
	TotalFiles      int
	TotalLines      int
	PrimaryLanguage string
	Languages       []string
}

func scanFolder(path string) (*ScanResult, error) {
	// Simple Go-based file scanner
	var totalFiles int
	var totalLines int
	languageMap := make(map[string]int)

	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Skip common directories
		if info.IsDir() {
			base := filepath.Base(filePath)
			if shouldSkipDir(base) {
				return filepath.SkipDir
			}
			return nil
		}

		// Only count source files
		ext := strings.ToLower(filepath.Ext(filePath))
		if !isSourceFile(ext) {
			return nil
		}

		totalFiles++

		// Count lines
		file, err := os.Open(filePath)
		if err != nil {
			return nil // Skip files we can't read
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		lineCount := 0
		for scanner.Scan() {
			lineCount++
		}
		totalLines += lineCount

		// Track language
		lang := extToLanguage(ext)
		if lang != "" {
			languageMap[lang]++
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Determine primary language
	primaryLang := ""
	maxCount := 0
	for lang, count := range languageMap {
		if count > maxCount {
			maxCount = count
			primaryLang = lang
		}
	}

	languages := make([]string, 0, len(languageMap))
	for lang := range languageMap {
		languages = append(languages, lang)
	}

	return &ScanResult{
		TotalFiles:      totalFiles,
		TotalLines:      totalLines,
		PrimaryLanguage: primaryLang,
		Languages:       languages,
	}, nil
}

func shouldSkipDir(name string) bool {
	skipDirs := []string{
		".git", ".svn", ".hg",
		"node_modules", "deps", "_build", "vendor", "packages",
		"dist", "build", "target", "out", ".next", ".nuxt",
		".vscode", ".idea", ".vs",
		".tmp", ".cache", ".pytest_cache", "__pycache__", ".coverage",
		".DS_Store", "Thumbs.db",
		".modernpath", // Skip our own config directory
	}
	for _, skip := range skipDirs {
		if name == skip {
			return true
		}
	}
	return false
}

func isSourceFile(ext string) bool {
	sourceExts := []string{
		".go", ".js", ".ts", ".jsx", ".tsx", ".py", ".java", ".rb", ".php",
		".ex", ".exs", ".rs", ".cpp", ".c", ".h", ".hpp", ".cs", ".swift",
		".kt", ".scala", ".clj", ".sh", ".bash", ".zsh", ".fish",
		".sql", ".html", ".css", ".scss", ".sass", ".less",
		".json", ".yaml", ".yml", ".toml", ".xml", ".md",
		".tf", ".hcl", ".dockerfile",
	}
	for _, sourceExt := range sourceExts {
		if ext == sourceExt {
			return true
		}
	}
	return false
}

func extToLanguage(ext string) string {
	langMap := map[string]string{
		".go": "go", ".js": "javascript", ".ts": "typescript", ".jsx": "javascript", ".tsx": "typescript",
		".py": "python", ".java": "java", ".rb": "ruby", ".php": "php",
		".ex": "elixir", ".exs": "elixir", ".rs": "rust", ".cpp": "cpp", ".c": "c", ".h": "c", ".hpp": "cpp",
		".cs": "csharp", ".swift": "swift", ".kt": "kotlin", ".scala": "scala", ".clj": "clojure",
		".sh": "shell", ".bash": "shell", ".zsh": "shell", ".fish": "shell",
		".sql": "sql", ".html": "html", ".css": "css", ".scss": "scss", ".sass": "sass", ".less": "less",
		".json": "json", ".yaml": "yaml", ".yml": "yaml", ".toml": "toml", ".xml": "xml", ".md": "markdown",
		".tf": "hcl", ".hcl": "hcl", ".dockerfile": "dockerfile",
	}
	return langMap[ext]
}
