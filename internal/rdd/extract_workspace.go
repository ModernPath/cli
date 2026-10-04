package rdd

import (
	"os"
	"os/exec"
	"path/filepath"

	"strings"
)

// ---------------------------------------------------------------- git activity

// ParseGitLog returns the last 14 days of commits (newest first, capped 400 —
// the extractor's data.commits shape the burst op folds).
func ParseGitLog(root string) []Commit {
	cmd := exec.Command("git", "log", "--since=14 days ago", "--date=iso-strict", "--pretty=format:%h|%ad|%s")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var commits []Commit
	for _, l := range strings.Split(string(out), "\n") {
		if l == "" {
			continue
		}
		parts := strings.SplitN(l, "|", 3)
		if len(parts) < 3 {
			continue
		}
		subject := capRunes(parts[2], 160)
		commits = append(commits, Commit{
			Hash:    parts[0],
			Date:    parts[1],
			Subject: subject,
			IDs:     idMentions(subject, "REQ", "EPIC", "RQ", "OQ"),
		})
		if len(commits) == 400 {
			break
		}
	}
	return commits
}

// ---------------------------------------------------------------- epic records

// ReadEpicRecord reads an epic's record file relative to root ("" if absent).
func ReadEpicRecord(root, rel string) string {
	if rel == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return ""
	}
	return string(raw)
}
