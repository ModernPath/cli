package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

type reverseFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
type reverseExclusion struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
	Kind   string `json:"kind"`
}
type reverseRepository struct {
	Key            string             `json:"key"`
	Revision       string             `json:"revision"`
	Dirty          bool               `json:"dirty"`
	SnapshotDigest string             `json:"snapshot_digest"`
	Files          []reverseFile      `json:"files"`
	Exclusions     []reverseExclusion `json:"exclusions,omitempty"`
}
type reverseInventory struct {
	Repositories []reverseRepository `json:"repositories"`
	Files        int                 `json:"file_count"`
	Bytes        int64               `json:"byte_count"`
}

func reverseDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
func reverseSnapshot(files []reverseFile) string {
	tuples := make([][]any, len(files))
	for i, file := range files {
		tuples[i] = []any{file.Path, file.SHA256, file.Size}
	}
	sort.Slice(tuples, func(i, j int) bool { return tuples[i][0].(string) < tuples[j][0].(string) })
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(tuples)
	return reverseDigest(bytes.TrimSuffix(out.Bytes(), []byte("\n")))
}

func reverseSafePath(path string) bool {
	if len(path) == 0 || len(path) > 1024 || strings.ContainsAny(path, "\\\x00:") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		switch part {
		case "", ".", "..", ".git", ".modernpath", ".codex", ".agents", ".ssh", ".aws", "id_rsa", "id_ed25519":
			return false
		}
		if strings.HasPrefix(part, ".env") || strings.HasSuffix(part, ".pem") || strings.HasSuffix(part, ".key") {
			return false
		}
	}
	return true
}

func reverseRead(root, path string) ([]byte, error) {
	if !reverseSafePath(path) {
		return nil, fmt.Errorf("unsafe source path %q", path)
	}
	boundary, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer boundary.Close()
	current := ""
	for _, part := range strings.Split(path, "/") {
		current = filepath.Join(current, part)
		info, err := boundary.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symlink source refused: %s", path)
		}
	}
	// Root resolves through directory descriptors, so a concurrent parent swap
	// cannot escape the authorized root. Nonblocking open makes FIFO refusal bounded.
	file, err := boundary.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Size() > 32_000_000 {
		return nil, fmt.Errorf("non-regular or oversized source: %s", path)
	}
	content, err := io.ReadAll(io.LimitReader(file, 32_000_001))
	if err != nil {
		return nil, err
	}
	after, err := boundary.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, after) || before.Size() != int64(len(content)) || !before.ModTime().Equal(after.ModTime()) {
		return nil, fmt.Errorf("source changed while reading: %s", path)
	}
	return content, nil
}

func reverseRoot(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repository root is not a directory")
	}
	return root, nil
}

func buildReverseInventory(declarations []string) (reverseInventory, error) {
	result := reverseInventory{Repositories: []reverseRepository{}}
	if len(declarations) == 0 || len(declarations) > 100 {
		return result, fmt.Errorf("declare 1–100 repositories explicitly")
	}
	keys, roots := map[string]bool{}, map[string]bool{}
	for _, declaration := range declarations {
		key, path, ok := strings.Cut(declaration, "=")
		if !ok || strings.TrimSpace(key) == "" || len(key) > 255 || keys[key] {
			return result, fmt.Errorf("repository must be a unique key=directory: %q", declaration)
		}
		root, err := reverseRoot(path)
		if err != nil {
			return result, err
		}
		if roots[root] {
			return result, fmt.Errorf("repository directory declared twice")
		}
		keys[key], roots[root] = true, true
		repo := reverseRepository{Key: key, Revision: "unversioned", Dirty: true, Files: []reverseFile{}}
		paths := []string{}
		var initialStatus []byte
		git := func(args ...string) ([]byte, error) {
			return exec.Command("git", append([]string{"-C", root}, args...)...).Output()
		}
		if _, err := os.Lstat(filepath.Join(root, ".git")); err == nil {
			revision, err := git("rev-parse", "HEAD")
			if err != nil {
				return result, fmt.Errorf("repository %s has unreadable Git revision: %w", key, err)
			}
			repo.Revision = strings.TrimSpace(string(revision))
			status, err := git("status", "--porcelain", "-z")
			if err != nil {
				return result, err
			}
			repo.Dirty = len(status) > 0
			initialStatus = status
			ignored, err := git("ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--directory")
			if err != nil {
				return result, err
			}
			for _, path := range strings.Split(strings.TrimSuffix(string(ignored), "\x00"), "\x00") {
				if path == "" {
					continue
				}
				kind := "file"
				if strings.HasSuffix(path, "/") {
					kind = "subtree"
				}
				repo.Exclusions = append(repo.Exclusions, reverseExclusion{strings.TrimSuffix(path, "/"), "Git ignore policy", kind})
			}
			listed, err := git("ls-files", "-z", "--cached", "--others", "--exclude-standard")
			if err != nil {
				return result, fmt.Errorf("repository %s inventory failed: %w", key, err)
			}
			paths = strings.Split(strings.TrimSuffix(string(listed), "\x00"), "\x00")
		} else if !os.IsNotExist(err) {
			return result, err
		} else {
			err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if path == root {
					return nil
				}
				relative, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				relative = filepath.ToSlash(relative)
				if !reverseSafePath(relative) {
					kind := "file"
					if entry.IsDir() {
						kind = "subtree"
					}
					repo.Exclusions = append(repo.Exclusions, reverseExclusion{relative, "private or workspace metadata", kind})
					if entry.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				if entry.IsDir() {
					switch entry.Name() {
					case "node_modules", "_build", "deps", "vendor", "dist", "build", ".cache":
						repo.Exclusions = append(repo.Exclusions, reverseExclusion{relative, "generated or dependency directory", "subtree"})
						return filepath.SkipDir
					}
					if _, err := os.Lstat(filepath.Join(path, ".git")); err == nil {
						return fmt.Errorf("nested repository %s must be declared separately", relative)
					}
					return nil
				}
				paths = append(paths, relative)
				if len(paths) > 50_000 {
					return fmt.Errorf("source inventory exceeds 50000 files")
				}
				return nil
			})
			if err != nil {
				return result, err
			}
		}
		sort.Strings(paths)
		seen := map[string]bool{}
		var size int64
		for _, path := range paths {
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			if !reverseSafePath(path) {
				repo.Exclusions = append(repo.Exclusions, reverseExclusion{path, "private or workspace metadata", "file"})
				continue
			}
			content, err := reverseRead(root, path)
			if err != nil {
				return result, err
			}
			size += int64(len(content))
			if size > 32_000_000 {
				return result, fmt.Errorf("repository %s exceeds the 32000000-byte source limit; narrow and disclose the scope", key)
			}
			repo.Files = append(repo.Files, reverseFile{path, reverseDigest(content), int64(len(content))})
		}
		if len(repo.Files) == 0 {
			return result, fmt.Errorf("repository %s has no eligible source files", key)
		}
		repo.SnapshotDigest = reverseSnapshot(repo.Files)
		if repo.Revision != "unversioned" {
			revision, err := git("rev-parse", "HEAD")
			if err != nil {
				return result, err
			}
			status, err := git("status", "--porcelain", "-z")
			if err != nil {
				return result, err
			}
			if strings.TrimSpace(string(revision)) != repo.Revision || !bytes.Equal(initialStatus, status) {
				return result, fmt.Errorf("repository %s changed during inventory; retry before authorization", key)
			}
		}
		result.Files += len(repo.Files)
		result.Bytes += size
		if result.Files > 50_000 || result.Bytes > 250_000_000 {
			return result, fmt.Errorf("run source inventory exceeds its bounded file/byte limits")
		}
		result.Repositories = append(result.Repositories, repo)
	}
	return result, nil
}

func reverseSourceBundle(repository reverseRepository, path string) ([]map[string]string, error) {
	root, err := reverseRoot(path)
	if err != nil {
		return nil, err
	}
	if len(repository.Files) == 0 || len(repository.Files) > 50_000 || reverseSnapshot(repository.Files) != repository.SnapshotDigest {
		return nil, fmt.Errorf("invalid authorized source inventory")
	}
	files := make([]map[string]string, 0, len(repository.Files))
	seen := map[string]bool{}
	var size int64
	for _, file := range repository.Files {
		if seen[file.Path] {
			return nil, fmt.Errorf("duplicate authorized source path")
		}
		seen[file.Path] = true
		size += file.Size
		if file.Size < 0 || size > 32_000_000 {
			return nil, fmt.Errorf("authorized source inventory exceeds repository byte limit")
		}
		content, err := reverseRead(root, file.Path)
		if err != nil {
			return nil, err
		}
		if int64(len(content)) != file.Size || reverseDigest(content) != file.SHA256 {
			return nil, fmt.Errorf("authorized source changed: %s; re-inventory and obtain new authorization", file.Path)
		}
		files = append(files, map[string]string{"path": file.Path, "content_base64": base64.StdEncoding.EncodeToString(content)})
	}
	return files, nil
}
