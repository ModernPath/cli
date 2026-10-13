package cmd

// REQ-CROSS-538 (EPIC-CLI-OUTAGE) — while the server is unavailable, process
// writes are kept on this machine with their ids fixed. A keeping verb arms
// its env; a write the outage stops is collected instead of failing, and the
// verb writes one journal entry under .modernpath/outage-journal/ and exits 0.
// REQ-CROSS-539 records the entries when the server is back.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
)

// The journal's subdirectories for entries replay could not record.
var keptAsideDirs = []string{"conflicted", "refused"}

// keptRequest is one write as it would have been sent.
type keptRequest struct {
	Method string         `json:"method"`
	Path   string         `json:"path"`
	Body   map[string]any `json:"body,omitempty"`
}

// keptWrite is armed on the env of a keeping verb: writes the outage stops
// are collected here rather than failing.
type keptWrite struct {
	verb     string
	requests []keptRequest
}

// errKept is what a collected write returns to its caller: the write is not
// lost, the verb writes it to the journal.
var errKept = errors.New("kept on this machine until the ModernPath server is back")

// keptBy names the person who kept the write, from the credential.
type keptBy struct {
	Subject string `json:"subject,omitempty"`
	Email   string `json:"email,omitempty"`
}

// keptEntry is one journal file. Reason is set only by a replay that moved the
// entry aside.
type keptEntry struct {
	Seq     int            `json:"seq"`
	Verb    string         `json:"verb"`
	Record  string         `json:"record"`
	Inputs  map[string]any `json:"inputs"`
	KeptAt  string         `json:"kept_at"`
	Keeper  keptBy         `json:"keeper"`
	Fixed   map[string]any `json:"fixed,omitempty"`
	Basis   map[string]any `json:"basis,omitempty"`
	WaitsOn string         `json:"waits_on,omitempty"`
	Reason  string         `json:"reason,omitempty"`
	// Attempts counts replays whose request failed without an outage, a 5xx
	// or a 429; the third sets the entry aside as refused (REQ-CROSS-539 AC7).
	Attempts int `json:"attempts,omitempty"`

	// chained asks the keep to set WaitsOn, under its sequence reservation,
	// to the latest kept entry of the same verb and record.
	chained bool
}

// keeping arms the loaded env for verb: keeping(verb)(factoryEnvLoad()). A
// load that failed only because the server is unavailable still has its env:
// the verb runs and its write is kept.
func keeping(verb string) func(*factoryEnv, error) (*factoryEnv, error) {
	return func(env *factoryEnv, err error) (*factoryEnv, error) {
		if err != nil && (env == nil || !isUnavailable(err)) {
			return nil, err
		}
		env.keep = &keptWrite{verb: verb}
		return env, nil
	}
}

// collect keeps a write the outage stopped.
func (e *factoryEnv) collect(method, apiPath string, payload any) error {
	body, _ := payload.(map[string]any)
	e.keep.requests = append(e.keep.requests, keptRequest{Method: method, Path: apiPath, Body: body})
	return errKept
}

// keptBasis is the named fields of the first collected write's record: the
// guard the write was made on.
func (e *factoryEnv) keptBasis(fields ...string) map[string]any {
	basis := map[string]any{}
	if !e.kept() {
		return basis
	}
	record, _ := e.keep.requests[0].Body["record"].(map[string]any)
	for _, f := range fields {
		if v, ok := record[f]; ok {
			basis[f] = v
		}
	}
	return basis
}

// kept reports whether a write was collected.
func (e *factoryEnv) kept() bool { return e.keep != nil && len(e.keep.requests) > 0 }

// keepCollected writes the collected requests as one entry, or returns err
// when nothing was collected.
func (e *factoryEnv) keepCollected(err error, entry keptEntry) error {
	if !e.kept() {
		return err
	}
	entry.Verb = e.keep.verb
	entry.Inputs = map[string]any{"requests": e.keep.requests}
	if kerr := keepEntry(e, entry); kerr != nil {
		return kerr
	}
	if errors.Is(err, errKept) {
		return nil
	}
	return err
}

func journalDir(root string) string { return filepath.Join(root, config.ConfigDir, outageJournalDir) }

// keepEntry writes the entry and says it is kept.
func keepEntry(env *factoryEnv, entry keptEntry) error {
	entry.KeptAt = time.Now().UTC().Format(time.RFC3339)
	entry.Keeper = keeperOf(env)
	if _, err := writeJournalEntry(env.Root, &entry); err != nil {
		return fmt.Errorf("could not keep %s %s on this machine: %w", entry.Verb, entry.Record, err)
	}
	printKeptLine(env.Root, entry.Verb, entry.Record)
	return nil
}

func printKeptLine(root, verb, record string) {
	fmt.Printf("Kept on this machine (%d waiting): %s %s. It is recorded when the server is back.\n",
		keptWriteCount(root), verb, record)
}

func keeperOf(env *factoryEnv) keptBy {
	claims, _ := zitadel.TokenClaims(env.token)
	by := keptBy{Subject: claims.Subject, Email: claims.Email}
	if by.Email == "" && env.Auth != nil {
		by.Email = env.Auth.Actor
	}
	return by
}

// writeJournalEntry creates the entry whole under the next sequence number.
// The number is reserved first (reserveSeq); the content goes to a temporary
// file that is then linked under its name, and the reservation is removed
// once the entry is there. It never waits on a replay.
func writeJournalEntry(root string, e *keptEntry) (string, error) {
	dir := journalDir(root)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	seq, err := reserveSeq(dir, e)
	if err != nil {
		return "", err
	}
	defer os.Remove(filepath.Join(dir, seqMarkerName(seq)))
	e.Seq = seq
	raw, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return "", err
	}
	name := entryName(seq, e.Verb) + ".json"
	created, err := createWhole(dir, name, append(raw, '\n'))
	if err != nil {
		return "", err
	}
	if !created {
		return "", fmt.Errorf("%s already exists", name)
	}
	return name, nil
}

// reserveSeq reserves the next sequence number for e: under the journal's
// sequence lock, one past every number held by an entry or a reservation, it
// creates the <seq>.seq marker naming the verb and record exclusively. An
// entry that chains on its record waits on the latest entry or reservation of
// the same verb and record before it, resolved under the same lock.
func reserveSeq(dir string, e *keptEntry) (int, error) {
	unlock, err := lockSeq(dir)
	if err != nil {
		return 0, err
	}
	defer unlock()
	reservation, err := json.Marshal(seqReservation{Verb: e.Verb, Record: e.Record})
	if err != nil {
		return 0, err
	}
	for seq := nextJournalSeq(dir); ; seq++ {
		reserved, err := createWhole(dir, seqMarkerName(seq), reservation)
		if err != nil {
			return 0, err
		}
		if !reserved {
			continue
		}
		if e.chained {
			e.WaitsOn = latestBefore(dir, seq, e.Verb, e.Record)
		}
		return seq, nil
	}
}

// seqLockStaleAfter is how old the sequence lock is when its holder is taken
// to have crashed; a keep holds it for a few file operations.
const seqLockStaleAfter = 10 * time.Second

// lockSeq takes the journal's sequence lock, waiting for a keep that holds it.
func lockSeq(dir string) (func(), error) {
	path := filepath.Join(dir, "seq.lock")
	deadline := time.Now().Add(5 * time.Second)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		if info, serr := os.Stat(path); serr == nil && time.Since(info.ModTime()) > seqLockStaleAfter {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("another keep holds the journal's sequence lock")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// seqReservation is the content of a <seq>.seq marker.
type seqReservation struct {
	Verb   string `json:"verb"`
	Record string `json:"record"`
}

func seqMarkerName(seq int) string { return fmt.Sprintf("%06d.seq", seq) }

// entryName is an entry's file name without .json.
func entryName(seq int, verb string) string {
	return fmt.Sprintf("%06d-%s", seq, strings.ReplaceAll(verb, " ", "-"))
}

// markerSeq is the sequence number a <seq>.seq marker reserves.
func markerSeq(name string) (int, bool) {
	head, found := strings.CutSuffix(name, ".seq")
	if !found {
		return 0, false
	}
	n, err := strconv.Atoi(head)
	return n, err == nil
}

// latestBefore names the latest entry kept, or reserved, before seq at the
// top of the journal with the same verb and record; "" when there is none.
func latestBefore(dir string, seq int, verb, record string) string {
	best, name := 0, ""
	consider := func(n int, v, r string) {
		if n < seq && n > best && v == verb && r == record {
			best, name = n, entryName(n, v)
		}
	}
	for _, f := range readJournal(dir) {
		consider(f.entry.Seq, f.entry.Verb, f.entry.Record)
	}
	dirents, _ := os.ReadDir(dir)
	for _, d := range dirents {
		n, ok := markerSeq(d.Name())
		if !ok {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, d.Name()))
		var r seqReservation
		if err != nil || json.Unmarshal(raw, &r) != nil {
			continue
		}
		consider(n, r.Verb, r.Record)
	}
	return name
}

func createWhole(dir, name string, raw []byte) (bool, error) {
	tmp, err := os.CreateTemp(dir, ".keeping-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Link(tmp.Name(), filepath.Join(dir, name)); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// journalSeq is the sequence number an entry file name starts with.
func journalSeq(name string) (int, bool) {
	head, _, found := strings.Cut(name, "-")
	if !found || !strings.HasSuffix(name, ".json") {
		return 0, false
	}
	n, err := strconv.Atoi(head)
	return n, err == nil
}

// nextJournalSeq is one past the highest sequence number in the journal,
// counting the reservations and the entries moved aside, so a number is not
// reused while they stay.
func nextJournalSeq(dir string) int {
	highest := 0
	for _, d := range append([]string{dir}, asideDirs(dir)...) {
		entries, _ := os.ReadDir(d)
		for _, e := range entries {
			n, ok := journalSeq(e.Name())
			if !ok {
				n, ok = markerSeq(e.Name())
			}
			if ok && n > highest {
				highest = n
			}
		}
	}
	return highest + 1
}

func asideDirs(dir string) []string {
	out := make([]string, len(keptAsideDirs))
	for i, sub := range keptAsideDirs {
		out[i] = filepath.Join(dir, sub)
	}
	return out
}

type journalFile struct {
	name  string
	entry keptEntry
}

// readJournal reads the entries in dir in sequence order; unreadable files are
// skipped.
func readJournal(dir string) []journalFile {
	dirents, _ := os.ReadDir(dir)
	var out []journalFile
	for _, d := range dirents {
		if !d.Type().IsRegular() {
			continue
		}
		if _, ok := journalSeq(d.Name()); !ok {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, d.Name()))
		if err != nil {
			continue
		}
		var e keptEntry
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		out = append(out, journalFile{d.Name(), e})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].entry.Seq < out[j].entry.Seq })
	return out
}

// lastKept is the name, without .json, of the latest kept entry of verb on
// record; "" when there is none.
func lastKept(root, verb, record string) string {
	last := ""
	for _, f := range readJournal(journalDir(root)) {
		if f.entry.Verb == verb && f.entry.Record == record {
			last = strings.TrimSuffix(f.name, ".json")
		}
	}
	return last
}

// gatePins are the two review pins of a gate's working-set snapshot.
type gatePins struct {
	content, scope string
	at             time.Time
}

// snapshotGatePins reads the review pins from the newest snapshot under
// .modernpath/working-set/ that holds the gate's block; ok is false when no
// snapshot holds it.
func snapshotGatePins(root, gateID string) (gatePins, bool) {
	var best gatePins
	found := false
	heading := "## GATE " + gateID + " — "
	_ = filepath.WalkDir(filepath.Join(root, workingSetDir), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(raw)
		i := strings.Index(content, heading)
		if i < 0 || (i > 0 && content[i-1] != '\n') {
			return nil
		}
		block := content[i+len(heading):]
		if j := strings.Index(block, "\n## "); j >= 0 {
			block = block[:j]
		}
		pins := gatePins{}
		for _, line := range strings.Split(block, "\n") {
			if v, ok := strings.CutPrefix(line, "- **Content fingerprint:** "); ok {
				pins.content = pinValue(v)
			}
			if v, ok := strings.CutPrefix(line, "- **Scope fingerprint:** "); ok {
				pins.scope = pinValue(v)
			}
		}
		if s, err := loadSnapshot(path); err == nil && !s.at.IsZero() {
			pins.at = s.at
		} else if info, err := d.Info(); err == nil {
			pins.at = info.ModTime()
		}
		if !found || pins.at.After(best.at) {
			best, found = pins, true
		}
		return nil
	})
	return best, found
}

// pinValue is a rendered pin, or "" for an absent one.
func pinValue(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || v == "—" || v == notServed || strings.ContainsAny(v, " «") {
		return ""
	}
	return v
}

// selectedPiece is the held piece the local WORK-SELECTION.md names; "" when
// there is none.
func selectedPiece(root string) string {
	raw, err := os.ReadFile(selectionSnapshotPath(root))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(line, "- **Selected scope:** "); ok {
			if v = strings.TrimSpace(v); v != "" && v != "—" {
				return v
			}
		}
	}
	return ""
}

// keepPush keeps an offline `working-set push`: the files stay as edited and
// one kept push names the piece, given with --piece or held in the local
// WORK-SELECTION.md. With neither, or for a dry run, it waits.
func keepPush(env *factoryEnv, outage error, piece string, dryRun bool) error {
	if dryRun {
		return outage
	}
	from := "--piece"
	if piece == "" {
		piece, from = selectedPiece(env.Root), "WORK-SELECTION.md"
	}
	if piece == "" {
		return outage
	}
	const verb = "working-set push"
	if lastKept(env.Root, verb, piece) != "" {
		printKeptLine(env.Root, verb, piece)
		return nil
	}
	return keepEntry(env, keptEntry{
		Verb: verb, Record: piece,
		Inputs: map[string]any{"piece": piece},
		Basis:  map[string]any{"piece_from": from},
	})
}

// keptEvidenceFacts are the record and the fixed values of kept runs: each
// run's id, ran_at and branch.
func keptEvidenceFacts(requests []keptRequest) (string, map[string]any) {
	var targets []string
	seen := map[string]bool{}
	runs := []any{}
	for _, r := range requests {
		runs = append(runs, map[string]any{
			"run_id": str(r.Body, "external_id"), "ran_at": str(r.Body, "ran_at"), "branch": str(r.Body, "branch"),
		})
		results, _ := r.Body["results"].([]map[string]any)
		for _, res := range results {
			if id := str(res, "target_external_id"); id != "" && !seen[id] {
				seen[id] = true
				targets = append(targets, id)
			}
		}
	}
	return strings.Join(targets, ", "), map[string]any{"runs": runs}
}

// waitLine is what a write that cannot be kept says during an outage.
func waitLine(outage serverUnavailableError) string {
	return fmt.Sprintf("Waits for the ModernPath server (unavailable since %s); nothing was kept.", outage.since.Local().Format("15:04"))
}
