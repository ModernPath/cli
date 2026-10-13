package cmd

// REQ-CROSS-537 (EPIC-CLI-OUTAGE) — while the server is unavailable, the
// loop's reads answer from the last snapshot on this machine, under a line
// naming its time, store revision and age. Nothing here writes: a snapshot is
// never rewritten from an unavailable answer.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// staleSnapshotAfter is the age past which an answer from a snapshot warns.
const staleSnapshotAfter = 24 * time.Hour

const staleSnapshotWarning = "Warning: this snapshot is more than 24 hours old."

// readSnapshot is a snapshot file read for an outage answer: the time and
// store revision from its header, and the body after the header.
type readSnapshot struct {
	at   time.Time // zero when the header names no time
	rev  string
	body string
}

func loadSnapshot(path string) (readSnapshot, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return readSnapshot{}, err
	}
	content := string(raw)
	s := readSnapshot{rev: "unknown", body: content}
	blocks := strings.SplitN(content, "\n\n", 3)
	if len(blocks) == 3 {
		s.body = blocks[2]
	}
	if len(blocks) >= 2 {
		for _, line := range strings.Split(blocks[1], "\n") {
			if v, ok := strings.CutPrefix(line, "- **Snapshot at:** "); ok {
				s.at, _ = time.Parse(time.RFC3339, strings.TrimSpace(v))
			}
			if v, ok := strings.CutPrefix(line, "- **Source store/revision:** "); ok {
				s.rev = snapshotRevision(v)
			}
		}
	}
	return s, nil
}

// snapshotRevision takes the store revision from a `Source store/revision`
// value: `<url> · system <id> · store <rev>`.
func snapshotRevision(v string) string {
	for _, part := range strings.Split(v, " · ") {
		if rev, ok := strings.CutPrefix(strings.TrimSpace(part), "store "); ok && rev != "" {
			return rev
		}
	}
	return "unknown"
}

// snapshotAge is the age in whole minutes, hours or days.
func snapshotAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "less than a minute"
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d/time.Hour))
	}
	return fmt.Sprintf("%d days", int(d/(24*time.Hour)))
}

// describe is `<time> (<age> old, store <rev>)`, plus whether it is past
// staleSnapshotAfter.
func (s readSnapshot) describe(now time.Time) (string, bool) {
	if s.at.IsZero() {
		return fmt.Sprintf("an unknown time (age unknown, store %s)", s.rev), false
	}
	age := now.Sub(s.at)
	return fmt.Sprintf("%s (%s old, store %s)", s.at.Local().Format("2006-01-02 15:04"), snapshotAge(age), s.rev), age > staleSnapshotAfter
}

// ageLine is the line every answer from a snapshot starts with, and the
// warning past 24 hours.
func (s readSnapshot) ageLine(outage serverUnavailableError, now time.Time) string {
	desc, stale := s.describe(now)
	line := fmt.Sprintf("From the snapshot of %s — the ModernPath server is unavailable since %s.",
		desc, outage.since.Local().Format("15:04"))
	if stale {
		line += "\n" + staleSnapshotWarning
	}
	return line
}

// asOutage returns the outage err carries.
func asOutage(err error) serverUnavailableError {
	var outage serverUnavailableError
	_ = errors.As(err, &outage)
	return outage
}

// outageRoot is the workspace the outage answer reads: the env's, else the
// one found from the current directory.
func outageRoot(env *factoryEnv) string {
	if env != nil && env.Root != "" {
		return env.Root
	}
	return workspaceRoot()
}

// missingSnapshots is the error for a read with no snapshot to answer from:
// it names them and stays an outage, so the command exits 75.
func missingSnapshots(names []string, outage error) error {
	return fmt.Errorf("no snapshot on this machine for %s, so it cannot be answered offline: %w", strings.Join(names, ", "), outage)
}

func gatesSnapshotPath(root string) string { return filepath.Join(root, yourMoveDir, "GATES.md") }

func selectionSnapshotPath(root string) string {
	return filepath.Join(root, workingSetDir, selectionFile)
}

// gatesSnapshotAnswer is the GATES.md snapshot under its age line, or false
// when there is none.
func gatesSnapshotAnswer(root string, outage error, now time.Time) (string, bool) {
	s, err := loadSnapshot(gatesSnapshotPath(root))
	if err != nil {
		return "", false
	}
	return s.ageLine(asOutage(outage), now) + "\n\n" + strings.TrimRight(s.body, "\n"), true
}

// yourMoveFromSnapshot answers `your-move` from GATES.md.
func yourMoveFromSnapshot(root string, outage error, out io.Writer, now time.Time) error {
	answer, ok := gatesSnapshotAnswer(root, outage, now)
	if !ok {
		return missingSnapshots([]string{filepath.Join(yourMoveDir, "GATES.md")}, outage)
	}
	fmt.Fprintln(out, answer)
	return nil
}

// processNextFromSnapshot answers `process next` from WORK-SELECTION.md and
// the pending decisions in GATES.md.
func processNextFromSnapshot(root string, outage error, out io.Writer, now time.Time) error {
	var missing []string
	if s, err := loadSnapshot(selectionSnapshotPath(root)); err == nil {
		fmt.Fprintf(out, "%s\n\n%s\n", s.ageLine(asOutage(outage), now), strings.TrimRight(s.body, "\n"))
	} else {
		missing = append(missing, filepath.Join(workingSetDir, selectionFile))
	}
	if answer, ok := gatesSnapshotAnswer(root, outage, now); ok {
		fmt.Fprintf(out, "\n## Pending decisions\n\n%s\n", answer)
	} else {
		missing = append(missing, filepath.Join(yourMoveDir, "GATES.md"))
	}
	if len(missing) > 0 {
		return missingSnapshots(missing, outage)
	}
	return nil
}

// workingSetPullFromSnapshots answers a by-id `working-set pull`: each
// existing snapshot is left as it is and named with its age; a missing one is
// named and the command exits 75.
func workingSetPullFromSnapshots(root string, ids []string, outage error, out io.Writer, now time.Time) error {
	fmt.Fprintf(out, "The ModernPath server is unavailable since %s — nothing is pulled; the snapshots on this machine are left as they are.\n",
		asOutage(outage).since.Local().Format("15:04"))
	var missing []string
	for _, id := range ids {
		name := id + ".md"
		if id == selectionTarget || id == selectionFile || id == strings.TrimSuffix(selectionFile, ".md") {
			name = selectionFile
		}
		rel := filepath.Join(workingSetDir, name)
		s, err := readPullSnapshot(root, name)
		if err != nil {
			missing = append(missing, id)
			fmt.Fprintf(out, "✗ %s — no snapshot on this machine\n", id)
			continue
		}
		desc, stale := s.describe(now)
		fmt.Fprintf(out, "  %s — %s from the snapshot of %s\n", id, rel, desc)
		if stale {
			fmt.Fprintf(out, "    %s\n", staleSnapshotWarning)
		}
	}
	if len(missing) > 0 {
		return missingSnapshots(missing, outage)
	}
	return nil
}

func readPullSnapshot(root, name string) (readSnapshot, error) {
	if unsafeSnapshotName(name) {
		return readSnapshot{}, fmt.Errorf("not a plain file name")
	}
	return loadSnapshot(filepath.Join(root, workingSetDir, name))
}
