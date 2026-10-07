package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const writtenBodyKey = "- **Written body:** sha256:"

const sourceIdentityKey = "- **Source identity:** sha256:"

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

type workingSetSnapshot struct {
	sourceIdentity string
	writtenBody    string
	body           string
}

// parseWorkingSetSnapshot reads only the title and metadata blocks. The
// second blank-line delimiter closes metadata regardless of which fields it
// contains, so missing keys can never be recovered from body text.
func parseWorkingSetSnapshot(filename, content string) (workingSetSnapshot, error) {
	blocks := strings.SplitN(content, "\n\n", 3)
	if len(blocks) != 3 {
		return workingSetSnapshot{}, fmt.Errorf("missing snapshot header/body framing")
	}
	name := strings.TrimSuffix(filename, ".md")
	if blocks[0] != "# "+name+" — working-set snapshot" {
		return workingSetSnapshot{}, fmt.Errorf("snapshot title does not match %s", filename)
	}
	var source, written string
	var sourceCount, writtenCount int
	metadataLines := strings.Split(blocks[1], "\n")
	for _, line := range metadataLines {
		sourceLabel := strings.TrimSuffix(sourceIdentityKey, " sha256:")
		writtenLabel := strings.TrimSuffix(writtenBodyKey, " sha256:")
		if strings.HasPrefix(line, sourceLabel) {
			sourceCount++
			value := strings.TrimSpace(strings.TrimPrefix(line, sourceLabel))
			source = strings.TrimPrefix(value, "sha256:")
			if source == value {
				source = ""
			}
		}
		if strings.HasPrefix(line, writtenLabel) {
			writtenCount++
			value := strings.TrimSpace(strings.TrimPrefix(line, writtenLabel))
			written = strings.TrimPrefix(value, "sha256:")
			if written == value {
				written = ""
			}
		}
	}
	if sourceCount != 1 || writtenCount != 1 {
		return workingSetSnapshot{}, fmt.Errorf("snapshot metadata must contain one Source identity and one Written body")
	}
	if !strings.HasPrefix(metadataLines[len(metadataLines)-1], writtenBodyKey) {
		return workingSetSnapshot{}, fmt.Errorf("Written body must close the snapshot metadata block")
	}
	if !validSnapshotHash(source) || !validSnapshotHash(written) {
		return workingSetSnapshot{}, fmt.Errorf("snapshot metadata contains an invalid SHA256")
	}
	return workingSetSnapshot{sourceIdentity: source, writtenBody: written, body: blocks[2]}, nil
}

func validSnapshotHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func snapshotBodyMatches(snapshot workingSetSnapshot) bool {
	return strings.EqualFold(sha256Hex([]byte(snapshot.body)), snapshot.writtenBody)
}

// writeWorkingSetItem enforces REQ-CROSS-218: a file whose current body
// diverges from its recorded written-body hash was edited locally — preserve
// it, land the fresh pull beside it as <id>.md.pulled, and report the
// conflict. A file that cannot be parsed as a snapshot is treated the same
// way: when a guard cannot tell wrong from unverified it flags, it does not
// delete.
func writeWorkingSetItem(env *factoryEnv, item wsItem, gates []any, now time.Time) (conflict bool, err error) {
	fresh, err := renderWorkingSetFile(env, item, gates, now)
	if err != nil {
		return false, err
	}
	return writeWorkingSetSnapshot(env, item.id+".md", fresh)
}

// unsafeSnapshotName reports whether a working-set file name — derived from a
// served external id — would escape the working-set directory: filepath.Join
// collapses "../", so only a plain single-component name is writable.
func unsafeSnapshotName(filename string) bool {
	return filename != filepath.Base(filename) || strings.ContainsAny(filename, `/\`)
}

// writeWorkingSetSnapshot is the conflict-guarded writer every working-set
// file goes through: a file whose current body diverges from its recorded
// written-body hash was edited locally — preserve it, land the fresh pull
// beside it as .pulled, report the conflict.
func writeWorkingSetSnapshot(env *factoryEnv, filename, fresh string) (conflict bool, err error) {
	if unsafeSnapshotName(filename) {
		return false, fmt.Errorf("refusing %q: an external id must be a plain file name, not a path", strings.TrimSuffix(filename, ".md"))
	}
	path := filepath.Join(env.Root, workingSetDir, filename)
	existing, readErr := os.ReadFile(path)
	if readErr == nil {
		snapshot, parseErr := parseWorkingSetSnapshot(filename, string(existing))
		if parseErr != nil || !snapshotBodyMatches(snapshot) {
			if err := atomicWrite(path+".pulled", []byte(fresh)); err != nil {
				return true, err
			}
			return true, nil
		}
	} else if !os.IsNotExist(readErr) {
		return false, readErr
	}
	return false, atomicWrite(path, []byte(fresh))
}

// --- REQ-CROSS-217: staleness ---

func workingSetCheck(env *factoryEnv, refresh bool, now time.Time) error {
	entries, err := os.ReadDir(filepath.Join(env.Root, workingSetDir))
	if os.IsNotExist(err) {
		printSuccess("no working set materialized — nothing to check")
		return nil
	}
	if err != nil {
		return err
	}
	var itemIDs []string
	needsSelection := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		if entry.Name() == selectionFile {
			needsSelection = true
			continue
		}
		itemIDs = append(itemIDs, strings.TrimSuffix(entry.Name(), ".md"))
	}
	index, err := fetchDirectItems(env, itemIDs, workingSetIncludeCandidates, true)
	if err != nil {
		return err
	}
	var gates []any
	if needsSelection {
		gates, err = fetchList(env, fmt.Sprintf("/api/v1/sync/gates?system_id=%d&state=all", env.SystemID), "gates")
		if err != nil {
			return err
		}
	}
	staleSet := map[string]bool{}
	localSet := map[string]bool{}
	unverifiedSet := map[string]bool{}
	vanished := []string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") {
			continue
		}
		if name == selectionFile {
			raw, err := os.ReadFile(filepath.Join(env.Root, workingSetDir, name))
			if err != nil {
				return err
			}
			payload, err := fetchSelectionSnapshot(env)
			if err != nil {
				return err
			}
			src, err := selectionReleaseSource(env, payload, gates)
			if err != nil {
				return err
			}
			current, err := selectionIdentity(payload, src)
			if err != nil {
				return err
			}
			snapshot, parseErr := parseWorkingSetSnapshot(name, string(raw))
			if parseErr != nil {
				unverifiedSet[name] = true
				fmt.Printf("? %s — unverified (%v)\n", name, parseErr)
			} else {
				if !snapshotBodyMatches(snapshot) {
					localSet[name] = true
					fmt.Printf("✗ %s — local body edit (written-body hash does not match)\n", name)
				}
				if !strings.EqualFold(current, snapshot.sourceIdentity) {
					staleSet[name] = true
					fmt.Printf("✗ %s — stale (recorded %.12s…, current %.12s…)\n", name, snapshot.sourceIdentity, current)
				}
			}
			if parseErr == nil && !localSet[name] && !staleSet[name] {
				printSuccess("current: %s", selectionFile)
			}
			continue
		}
		id := strings.TrimSuffix(name, ".md")
		raw, err := os.ReadFile(filepath.Join(env.Root, workingSetDir, name))
		if err != nil {
			return err
		}
		item, ok := index[id]
		if !ok {
			vanished = append(vanished, id)
			fmt.Printf("? %s — no longer served by the store; file left in place (deletion is yours to decide)\n", id)
			continue
		}
		current, err := sourceIdentityFor(item, item.gates)
		if err != nil {
			return err
		}
		snapshot, parseErr := parseWorkingSetSnapshot(name, string(raw))
		if parseErr != nil {
			unverifiedSet[name] = true
			fmt.Printf("? %s — unverified (%v)\n", name, parseErr)
			continue
		}
		changed := false
		if !snapshotBodyMatches(snapshot) {
			localSet[name] = true
			changed = true
			fmt.Printf("✗ %s — local body edit (written-body hash does not match)\n", name)
		}
		if !strings.EqualFold(current, snapshot.sourceIdentity) {
			staleSet[name] = true
			changed = true
			fmt.Printf("✗ %s — stale (recorded %.12s…, current %.12s…)\n", id, snapshot.sourceIdentity, current)
		}
		if !changed {
			printSuccess("current: %s", id)
		}
	}

	if refresh {
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".md") || (!staleSet[name] && !localSet[name] && !unverifiedSet[name]) {
				continue
			}
			if name == selectionFile {
				_, conflict, err := pullSelection(env, now, gates)
				if err != nil {
					return err
				}
				if conflict {
					fmt.Printf("✗ CONFLICT %s — local snapshot preserved; fresh copy at %s.pulled\n", name, filepath.Join(workingSetDir, name))
				} else {
					delete(staleSet, name)
					printSuccess("refreshed %s", selectionFile)
				}
				continue
			}
			id := strings.TrimSuffix(name, ".md")
			item, ok := index[id]
			if !ok {
				continue
			}
			conflict, err := writeWorkingSetItem(env, item, item.gates, now)
			if err != nil {
				return err
			}
			if conflict {
				fmt.Printf("✗ CONFLICT %s — original preserved; fresh copy at %s.pulled\n", id, filepath.Join(workingSetDir, name))
				continue
			}
			delete(staleSet, name)
			printSuccess("refreshed %s", id)
		}
	}

	var problems []string
	stale := sortedSetKeys(staleSet)
	local := sortedSetKeys(localSet)
	unverified := sortedSetKeys(unverifiedSet)
	if len(stale) > 0 {
		problems = append(problems, "stale: "+strings.Join(stale, ", "))
	}
	if len(local) > 0 {
		problems = append(problems, "local edits: "+strings.Join(local, ", "))
	}
	if len(unverified) > 0 {
		problems = append(problems, "unverified: "+strings.Join(unverified, ", "))
	}
	if len(vanished) > 0 {
		problems = append(problems, "vanished from store: "+strings.Join(vanished, ", "))
	}
	if len(problems) > 0 {
		return fmt.Errorf("working set not current — %s", strings.Join(problems, "; "))
	}
	return nil
}

func sortedSetKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
