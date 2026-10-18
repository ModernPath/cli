package cmd

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

// readCaptureParallel bounds the source reads one read-source --capture sends at once.
const readCaptureParallel = 4

const reverseReadSourceHelp = `Read exact captured source. Give one of:

  --source <source_file id>  one file, printed as JSON with its content base64 encoded
  --capture <capture id> --out <folder>
                             the capture's files written to the folder as plain
                             files under their repository paths, so a group of
                             files is read with one command

With --capture, --prefix keeps the files under a path prefix (repeatable) and
--paths-from keeps the paths a file lists, one per line. Each file's sha256 is
checked against the capture before it is written; a file already in the folder
with the same sha256 is kept. It prints one line of counts, then any path that
failed or that --paths-from names but the capture does not hold. Capture ids
come from capture-source or source-status. It never falls back to latest.`

// reverseReadCapture writes the capture's selected files under out.
func reverseReadCapture(cmd *cobra.Command, load func() (*factoryEnv, error), capture string) error {
	out, _ := cmd.Flags().GetString("out")
	if out == "" {
		return fmt.Errorf("--capture writes files: give --out <folder>")
	}
	prefixes, _ := cmd.Flags().GetStringArray("prefix")
	wanted, err := readCapturePathList(cmd)
	if err != nil {
		return err
	}
	env, err := load()
	if err != nil {
		return err
	}
	body, err := reverseCall(env, "GET", "/reverse-engineering/sources/"+url.PathEscape(capture), nil)
	if err != nil {
		return err
	}
	data := body["data"].(map[string]any)
	rows, _ := data["files"].([]any)
	type captured struct{ id, path, sha string }
	var selected []captured
	held := map[string]bool{}
	for _, row := range rows {
		file, _ := row.(map[string]any)
		id, _ := file["id"].(string)
		path, _ := file["path"].(string)
		sha, _ := file["sha256"].(string)
		sha = strings.TrimPrefix(sha, "sha256:")
		held[path] = true
		if id == "" || path == "" || !readCaptureSelected(path, prefixes, wanted) {
			continue
		}
		selected = append(selected, captured{id, path, sha})
	}
	var failed []string
	for path := range wanted {
		if !held[path] {
			failed = append(failed, path+": not in this capture")
		}
	}
	var mu sync.Mutex
	written, kept := 0, 0
	jobs := make(chan captured)
	var wg sync.WaitGroup
	for i := 0; i < readCaptureParallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range jobs {
				result, err := readCaptureFile(env, out, file.id, file.path, file.sha)
				mu.Lock()
				switch {
				case err != nil:
					failed = append(failed, file.path+": "+err.Error())
				case result == "kept":
					kept++
				default:
					written++
				}
				mu.Unlock()
			}
		}()
	}
	for _, file := range selected {
		jobs <- file
	}
	close(jobs)
	wg.Wait()
	repository, _ := data["repository_key"].(string)
	fmt.Fprintf(cmd.OutOrStdout(), "capture %s (%s): %d of %d files selected; %d written, %d already present, %d failed, in %s\n",
		capture, repository, len(selected), len(rows), written, kept, len(failed), out)
	sort.Strings(failed)
	for _, line := range failed {
		fmt.Fprintln(cmd.OutOrStdout(), "  "+line)
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d captured files were not written", len(failed))
	}
	return nil
}

// readCaptureFile writes one captured file under out and says whether it was
// written or an identical copy was kept.
func readCaptureFile(env *factoryEnv, out, id, path, sha string) (string, error) {
	target, err := readCaptureTarget(out, path)
	if err != nil {
		return "", err
	}
	if existing, err := os.ReadFile(target); err == nil && sha != "" && readCaptureDigest(existing) == sha {
		return "kept", nil
	}
	body, err := reverseCall(env, "GET", "/reverse-engineering/source-files/"+url.PathEscape(id), nil)
	if err != nil {
		return "", err
	}
	data := body["data"].(map[string]any)
	if truncated, _ := data["truncated"].(bool); truncated {
		return "", fmt.Errorf("the server returned a truncated copy")
	}
	encoded, _ := data["content_base64"].(string)
	content, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("content is not base64: %v", err)
	}
	if sha != "" && readCaptureDigest(content) != sha {
		return "", fmt.Errorf("content does not match the captured sha256")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	return "written", os.WriteFile(target, content, 0o644)
}

// readCaptureTarget keeps a captured path inside the output folder.
func readCaptureTarget(out, path string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(path))
	if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path leaves the output folder")
	}
	return filepath.Join(out, clean), nil
}

// readCaptureDigest is content's sha256 in the capture's bare hex form.
func readCaptureDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func readCaptureSelected(path string, prefixes []string, wanted map[string]bool) bool {
	if len(prefixes) == 0 && wanted == nil {
		return true
	}
	if wanted[path] {
		return true
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// readCapturePathList reads --paths-from: one repository path per line, blank
// lines and lines starting with # skipped. Nil means no list was given.
func readCapturePathList(cmd *cobra.Command) (map[string]bool, error) {
	from, _ := cmd.Flags().GetString("paths-from")
	if from == "" {
		return nil, nil
	}
	file, err := os.Open(from)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	wanted := map[string]bool{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			wanted[line] = true
		}
	}
	return wanted, scanner.Err()
}
