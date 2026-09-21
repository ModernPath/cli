package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/modernpath/cli/internal/config"
)

// cli-notices.json records which once-per-session notices — an expiry
// warning for a given credential, a contract warning for a given version —
// have already been printed, so a long run is told once, not on every call
// (REQ-CROSS-389 D9). It is local state, never a store write; an unreadable
// file reads as empty.
func noticesPath(root string) string {
	return filepath.Join(root, config.ConfigDir, "cli-notices.json")
}

func readNotices(root string) map[string]string {
	seen := map[string]string{}
	raw, err := os.ReadFile(noticesPath(root))
	if err != nil {
		return seen
	}
	_ = json.Unmarshal(raw, &seen)
	return seen
}

// noticeOnce reports whether key has not been noticed yet, recording it when
// so. Two processes racing on the file can both print once; the loser of
// the write never loses another key, because the file is rewritten from what
// was read moments before.
func noticeOnce(root, key string) bool {
	seen := readNotices(root)
	if _, done := seen[key]; done {
		return false
	}
	seen[key] = time.Now().UTC().Format(time.RFC3339)
	if blob, err := json.MarshalIndent(seen, "", "  "); err == nil {
		_ = os.WriteFile(noticesPath(root), blob, 0o644)
	}
	return true
}
