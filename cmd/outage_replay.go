package cmd

// REQ-CROSS-539 (EPIC-CLI-OUTAGE) — when the server is back, the kept writes
// are recorded in sequence order and a conflict is reported, never forced.
// Replay runs in a write command from the main session, after its env loaded
// and before its own request, and in `modernpath outage replay`; never in a
// hook or a read verb (D8). Each entry is sent as it was kept: its requests,
// its fixed values and its kept pins. The server's legality checks decide; a
// 409 or a refusal moves the entry aside with the server's reason.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/spf13/cobra"
)

// replayOnLoad is armed for one invocation of a write command outside a hook:
// the first successful factoryEnvLoad replays the kept writes.
var (
	replayOnLoad     bool
	replayInvocation []string
)

// armReplay is the root's PersistentPreRunE: it arms replay for a command
// outside the read allowlist that is not a hook. `outage replay` replays on
// its own.
func armReplay(cmd *cobra.Command, _ []string) error {
	replayOnLoad = storeWriteVerb(cmd) && !hookMode(replayInvocation) && cmd != outageReplayCmd && !dryRunInvocation(cmd)
	return nil
}

// dryRunInvocation reports a command run with --dry-run: a preview never
// sends kept writes (REQ-CROSS-539 AC2, CODE-OUTAGE-3-5).
func dryRunInvocation(cmd *cobra.Command) bool {
	f := cmd.Flags().Lookup("dry-run")
	return f != nil && f.Value.String() == "true"
}

// replayAfterLoad runs the armed replay once, on the env a write command just
// loaded. Its outcome never fails the command: the summary says what happened
// and the command's own request proceeds.
func replayAfterLoad(env *factoryEnv) {
	if !replayOnLoad {
		return
	}
	replayOnLoad = false
	replayKept(env, os.Stderr)
}

// replayReport is what one replay did.
type replayReport struct {
	recorded int
	already  int
	aside    []asideNote
	foreign  string // the person who kept the writes, when it is not the signed-in user
	busy     bool
	stopped  error
	retrying []asideNote // kept for another try after a request that failed without an outage
}

type asideNote struct{ sub, name, label, reason string }

// entryOutcome is how one entry ended.
type entryOutcome struct {
	kind        string // recorded, already, conflicted, refused, stop
	reason      string
	fingerprint string // returned for an entry that waits on this one
	err         error
}

func recorded(fp string) entryOutcome { return entryOutcome{kind: "recorded", fingerprint: fp} }
func alreadyThere(fp string) entryOutcome {
	return entryOutcome{kind: "already", fingerprint: fp}
}
func conflicted(reason string) entryOutcome { return entryOutcome{kind: "conflicted", reason: reason} }
func refused(reason string) entryOutcome    { return entryOutcome{kind: "refused", reason: reason} }
func stopped(err error) entryOutcome        { return entryOutcome{kind: "stop", err: err} }

// retryLater is a request that failed without an outage, a 5xx or a 429: the
// entry stays kept for another replay and the rest go on (REQ-CROSS-539 AC7).
func retryLater(err error) entryOutcome { return entryOutcome{kind: "retry", err: err} }

// maxReplayAttempts is how many failed replays an entry gets before it is set
// aside as refused.
const maxReplayAttempts = 3

// callFailed is the outcome of a request that got no answer.
func callFailed(err error) entryOutcome {
	if stopsReplay(err) {
		return stopped(err)
	}
	return retryLater(err)
}

// serverFaultError is a 5xx from core during replay: not a refusal of the
// write, so the entry and the rest stay kept (REQ-CROSS-539 AC6, CODE-OUTAGE-3-2).
type serverFaultError struct {
	status int
	text   string
}

func (e serverFaultError) Error() string {
	return fmt.Sprintf("the server answered %d (%s)", e.status, e.text)
}

// answered is the outcome of a non-200 answer that is not otherwise handled:
// a 5xx stops the replay, anything else refuses the entry.
func answered(status int, body map[string]any) entryOutcome {
	if status >= 500 {
		return stopped(serverFaultError{status: status, text: refusalText(body)})
	}
	return refused(refusalText(body))
}

const gateChanged = "the gate changed while the server was unavailable"

// replayKept sends the kept writes in sequence order and reports on out.
func replayKept(env *factoryEnv, out io.Writer) replayReport {
	var res replayReport
	if len(readJournal(journalDir(env.Root))) == 0 {
		return res
	}
	release := acquireAgeLock(filepath.Join(journalDir(env.Root), "replay.lock"))
	if release == nil {
		res.busy = true
		fmt.Fprintln(out, "Another replay is recording the kept writes; this one is skipped.")
		return res
	}
	defer release()

	files := readJournal(journalDir(env.Root))
	me := keeperOf(env)
	fingerprints := map[string]string{}
	aside := map[string]string{} // entry name → the list it was moved to
	held := map[string]bool{}    // entries kept for another try in this replay
	for i := range files {
		f := files[i]
		name := strings.TrimSuffix(f.name, ".json")
		if !sameKeeper(f.entry.Keeper, me) {
			res.foreign = firstNonEmpty(f.entry.Keeper.Email, f.entry.Keeper.Subject, "someone else")
			break
		}
		var o entryOutcome
		w := f.entry.WaitsOn
		sub := aside[w]
		if sub == "" && w != "" {
			sub = asideList(env.Root, w)
		}
		if w != "" && held[w] {
			// It waits on a write kept for another try: it stays kept too.
			held[name] = true
			continue
		}
		if w != "" && sub != "" {
			// It waits on a write that was not recorded: it moves with it.
			o = entryOutcome{kind: sub, reason: fmt.Sprintf("waits on %s, which was %s", w, sub)}
		} else {
			// A predecessor no longer in the journal returned no fingerprint
			// here: the entry is sent on its own kept one (CODE-OUTAGE-3-1).
			if fp := fingerprints[w]; w != "" && fp != "" {
				f.entry = withExpectedFingerprint(f.entry, fp)
			}
			o = replayEntry(env, f)
		}
		switch o.kind {
		case "recorded", "already":
			if o.kind == "recorded" {
				res.recorded++
			} else {
				res.already++
			}
			if o.fingerprint != "" {
				fingerprints[name] = o.fingerprint
				// Kept on disk before the entry is removed: a replay that
				// stops before its dependents still sends them on it.
				if err := passFingerprint(env.Root, files[i+1:], name, o.fingerprint); err != nil {
					res.stopped = fmt.Errorf("could not keep the fingerprint %s returned: %w", name, err)
					break
				}
			}
			_ = os.Remove(filepath.Join(journalDir(env.Root), f.name))
		case "conflicted", "refused":
			aside[name] = o.kind
			if err := moveAside(env.Root, f, o.kind, o.reason); err != nil {
				res.stopped = fmt.Errorf("could not move %s to %s/: %w", name, o.kind, err)
			} else {
				res.aside = append(res.aside, asideNote{o.kind, name, entryLabel(f.entry), o.reason})
			}
		case "retry":
			f.entry.Attempts++
			if f.entry.Attempts >= maxReplayAttempts {
				reason := fmt.Sprintf("failed %d replays: %v", f.entry.Attempts, o.err)
				aside[name] = "refused"
				if err := moveAside(env.Root, f, "refused", reason); err != nil {
					res.stopped = fmt.Errorf("could not move %s to refused/: %w", name, err)
				} else {
					res.aside = append(res.aside, asideNote{"refused", name, entryLabel(f.entry), reason})
				}
				break
			}
			held[name] = true
			raw, err := json.MarshalIndent(f.entry, "", "  ")
			if err == nil {
				err = atomicWrite(filepath.Join(journalDir(env.Root), f.name), append(raw, '\n'))
			}
			if err != nil {
				res.stopped = fmt.Errorf("could not keep %s for another try: %w", name, err)
				break
			}
			res.retrying = append(res.retrying, asideNote{"retry", name, entryLabel(f.entry),
				fmt.Sprintf("try %d of %d failed: %v", f.entry.Attempts, maxReplayAttempts, o.err)})
		case "stop":
			res.stopped = o.err
		}
		if res.stopped != nil {
			break
		}
	}
	printReplay(out, env.Root, res)
	return res
}

// passFingerprint rewrites each later entry that waits on name to be sent on
// the fingerprint name's write returned, and to wait on nothing more. The
// rewrite replaces the entry file whole.
func passFingerprint(root string, later []journalFile, name, fp string) error {
	for j := range later {
		if later[j].entry.WaitsOn != name {
			continue
		}
		later[j].entry = withExpectedFingerprint(later[j].entry, fp)
		later[j].entry.WaitsOn = ""
		raw, err := json.MarshalIndent(later[j].entry, "", "  ")
		if err != nil {
			return err
		}
		if err := atomicWrite(filepath.Join(journalDir(root), later[j].name), append(raw, '\n')); err != nil {
			return err
		}
	}
	return nil
}

// sameKeeper compares the person who kept an entry with the signed-in user:
// by subject when both carry one, else by email.
func sameKeeper(kept, me keptBy) bool {
	if kept.Subject != "" && me.Subject != "" {
		return kept.Subject == me.Subject
	}
	return kept.Email != "" && strings.EqualFold(kept.Email, me.Email)
}

// asideList names the list, conflicted or refused, the named entry was moved
// to; "" when it is in neither.
func asideList(root, name string) string {
	if name == "" {
		return ""
	}
	for _, sub := range keptAsideDirs {
		if _, err := os.Stat(filepath.Join(journalDir(root), sub, name+".json")); err == nil {
			return sub
		}
	}
	return ""
}

// withExpectedFingerprint sends an entry that waits on another with the
// fingerprint that entry's write returned. It edits the entry's own maps.
func withExpectedFingerprint(e keptEntry, fp string) keptEntry {
	for _, r := range keptRequests(e) {
		if rec, ok := r.Body["record"].(map[string]any); ok {
			if _, has := rec["expected_fingerprint"]; has {
				rec["expected_fingerprint"] = fp
			}
		}
	}
	return e
}

// keptRequests are the entry's requests as kept. The returned bodies are the
// entry's own maps, so an edit to one is sent.
func keptRequests(e keptEntry) []keptRequest {
	raw, _ := e.Inputs["requests"].([]any)
	out := make([]keptRequest, 0, len(raw))
	for _, r := range raw {
		m, _ := r.(map[string]any)
		body, _ := m["body"].(map[string]any)
		out = append(out, keptRequest{Method: str(m, "method"), Path: str(m, "path"), Body: body})
	}
	return out
}

func entryLabel(e keptEntry) string {
	if e.Verb == "working-set push" {
		return "kept push of " + e.Record
	}
	return e.Verb + " " + e.Record
}

// moveAside writes the entry with its reason under sub/ and removes it from
// the journal.
func moveAside(root string, f journalFile, sub, reason string) error {
	e := f.entry
	e.Reason = reason
	raw, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Join(journalDir(root), sub)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(dir, f.name), append(raw, '\n')); err != nil {
		return err
	}
	return os.Remove(filepath.Join(journalDir(root), f.name))
}

func replayEntry(env *factoryEnv, f journalFile) entryOutcome {
	switch f.entry.Verb {
	case "factory evidence":
		return replayEvidence(env, f.entry)
	case "factory answer":
		return replayAnswer(env, f.entry)
	case "working-set push":
		return replayPush(env, f.entry)
	default:
		return replayAuthor(env, f.entry)
	}
}

// stopsReplay reports an error that leaves the entry and the rest kept: the
// server unavailable or limiting requests, a rejected credential, or a
// request that never got an answer.
func stopsReplay(err error) bool {
	var limiting serverLimitingError
	var cred credentialError
	var fault serverFaultError
	var refusal serverRefusalError
	return isUnavailable(err) || errors.As(err, &limiting) || errors.As(err, &cred) || errors.As(err, &fault) ||
		(errors.As(err, &refusal) && refusal.status >= 500)
}

// replayEvidence resends each kept run with its run id; the server upserts by
// run id, so a run that already landed is answered as an update.
func replayEvidence(env *factoryEnv, e keptEntry) entryOutcome {
	landed := true
	for _, r := range keptRequests(e) {
		status, body, err := env.call(r.Method, r.Path, r.Body)
		if err != nil {
			return callFailed(err)
		}
		if status != 200 {
			return answered(status, body)
		}
		if str(dataOf(body), "result") == "created" {
			landed = false
		}
	}
	if landed {
		return alreadyThere("")
	}
	return recorded("")
}

// answerRequiresReview is the server's signal that a gate is governed —
// answer_readiness.requires_review — with the purpose as the fallback for a
// server that predates it.
func answerRequiresReview(g map[string]any) bool {
	readiness, _ := g["answer_readiness"].(map[string]any)
	requires, served := readiness["requires_review"].(bool)
	purpose := str(g, "purpose")
	return (served && requires) || (!served && (purpose == "entry" || purpose == "completion"))
}

// replayAnswer sends a kept answer. A governed gate's answer carries the kept
// review pins, never freshly read ones; a gate that is not governed is
// answered only while its content is the one the answer was given on.
func replayAnswer(env *factoryEnv, e keptEntry) entryOutcome {
	reqs := keptRequests(e)
	if len(reqs) != 1 {
		return refused("the kept answer holds no request")
	}
	req := reqs[0]
	status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/gates/%s?system_id=%d", url.PathEscape(e.Record), env.SystemID), nil)
	if err != nil {
		return callFailed(err)
	}
	if status != 200 {
		return answered(status, body)
	}
	gate, _ := dataOf(body)["gate"].(map[string]any)
	keptContent, keptScope := str(e.Basis, "content_fingerprint"), str(e.Basis, "evaluated_scope_fingerprint")
	governed := answerRequiresReview(gate)
	open := str(gate, "state") == "" || str(gate, "state") == "open"
	switch {
	case governed && keptScope == "":
		return conflicted("kept without the gate's scope fingerprint, which an answer to a governed gate needs; answer it again")
	case governed:
		req.Body["review"] = map[string]any{"content_fingerprint": keptContent, "evaluated_scope_fingerprint": keptScope}
	case open && str(gate, "content_fingerprint") != keptContent:
		return conflicted(gateChanged)
	default:
		delete(req.Body, "review")
	}

	status, body, err = env.call(req.Method, req.Path, req.Body)
	if err != nil {
		return callFailed(err)
	}
	errBody, _ := body["error"].(map[string]any)
	switch {
	case status == 200:
		return recorded("")
	case status == 409:
		winner, _ := errBody["winner"].(map[string]any)
		if winner != nil && sameAnswer(winner, req.Body, firstNonEmpty(str(e.Fixed, "source_tag"), str(req.Body, "source_tag"))) {
			return alreadyThere("")
		}
		if winner != nil {
			return conflicted(fmt.Sprintf("the gate was answered first: %q (%s)", str(winner, "answer"), str(winner, "source_tag")))
		}
		return conflicted(refusalText(body))
	case status == 422 && governed && hasDetail(errBody, "review"):
		return conflicted(gateChanged + ": " + refusalText(body))
	}
	return answered(status, body)
}

// sameAnswer compares a first-wins winner with the kept answer: the answer,
// the chosen options and the kept USER: source. The answerer's ids are not
// compared (D10).
func sameAnswer(winner, sent map[string]any, source string) bool {
	return str(winner, "answer") == str(sent, "answer") &&
		sameStrings(winner["chosen_option_keys"], sent["chosen_option_keys"]) &&
		str(winner, "source_tag") == source
}

func sameStrings(a, b any) bool {
	sa, sb := stringSlice(a), stringSlice(b)
	if len(sa) != len(sb) {
		return false
	}
	for i := range sa {
		if sa[i] != sb[i] {
			return false
		}
	}
	return true
}

func hasDetail(errBody map[string]any, field string) bool {
	details, _ := errBody["details"].(map[string]any)
	_, ok := details[field]
	return ok
}

// replayPush runs `working-set push --piece <kept piece>` on the files as they
// are now. A piece that is no longer held is a conflict.
func replayPush(env *factoryEnv, e keptEntry) entryOutcome {
	piece := firstNonEmpty(str(e.Inputs, "piece"), e.Record)
	err := workingSetPushPiece(env, piece, false)
	var pushConflict pushConflictError
	switch {
	case err == nil:
		return recorded("")
	case stopsReplay(err):
		return stopped(err)
	case errors.Is(err, errNoCurrentSelection):
		return conflicted("the kept piece is no longer held: " + piece)
	case errors.As(err, &pushConflict):
		return conflicted(err.Error())
	case isNetworkFailure(err):
		// No answer: kept for another try (REQ-CROSS-539 AC7, CODE-OUTAGE-4-1).
		return retryLater(err)
	}
	return refused(err.Error())
}

// replayAuthor sends a kept authoring write: an edit, a create, a finding or
// a disposition. A 409 reads the record back: when it already holds the kept
// values the write landed before.
func replayAuthor(env *factoryEnv, e keptEntry) entryOutcome {
	reqs := keptRequests(e)
	if len(reqs) == 0 {
		return refused("the kept write holds no request")
	}
	out := recorded("")
	for _, r := range reqs {
		rec, _ := r.Body["record"].(map[string]any)
		// A disposition with a resolution kind, or a finding naming the
		// finding it was introduced by, needs the server's capability; it was
		// kept without reading it, so it is checked now (CODE-OUTAGE-3-4).
		// A contract read that failed is not an answer about the capability:
		// it stops or is retried (CODE-OUTAGE-4-1).
		if str(rec, "kind") == "finding" && (str(rec, "resolution_kind") != "" || str(rec, "introduced_by") != "") {
			c := env.ensureContract()
			switch {
			case c.readErr != nil:
				return callFailed(c.readErr)
			case c.status >= 500:
				return stopped(serverFaultError{status: c.status, text: "the sync contract read failed"})
			}
			if err := env.requireServerCapability("author.finding", "finding_resolution",
				"the server cannot record a finding's resolution kind or the finding it was introduced by"); err != nil {
				return refused(err.Error())
			}
		}
		status, body, err := env.call(r.Method, r.Path, r.Body)
		if err != nil {
			return callFailed(err)
		}
		switch {
		case status == 200:
			row, _ := dataOf(body)[str(rec, "kind")].(map[string]any)
			out.fingerprint = guardFingerprint(str(rec, "kind"), row)
		case status == 409:
			holds, fp, rerr := readBackHolds(env, rec)
			// A read-back that met a 5xx or got no answer proves nothing
			// either way: stop or retry, never conflict (CODE-OUTAGE-4-1).
			var refusal serverRefusalError
			if rerr != nil && (!errors.As(rerr, &refusal) || refusal.status >= 500) {
				return callFailed(rerr)
			}
			if !holds {
				return conflicted(refusalText(body))
			}
			out = alreadyThere(fp)
		default:
			return answered(status, body)
		}
	}
	return out
}

// readBackSkip are the request fields that guard or route a write rather than
// say what the record holds.
var readBackSkip = map[string]bool{
	"kind": true, "external_id": true, "expected_fingerprint": true, "aggregate_fingerprint": true,
	"authoring_context_id": true, "review_context_id": true, "current_release": true,
}

// readBackHolds reads the record a kept write names and reports whether it
// holds every kept value, with its fingerprint.
func readBackHolds(env *factoryEnv, rec map[string]any) (bool, string, error) {
	id := str(rec, "external_id")
	var payload map[string]any
	if str(rec, "kind") == "finding" {
		scopeKind, scopeExt := str(rec, "scope_kind"), str(rec, "scope_external_id")
		if scopeKind == "" || scopeExt == "" {
			return false, "", nil
		}
		rows, err := readScopeFindings(env, scopeKind+":"+scopeExt)
		if err != nil {
			return false, "", err
		}
		payload = rows[id]
	} else {
		items, err := fetchDirectItems(env, []string{id}, false, false)
		if err != nil {
			return false, "", err
		}
		payload = items[id].payload
	}
	if payload == nil {
		return false, "", nil
	}
	for k, v := range rec {
		if readBackSkip[k] {
			continue
		}
		if !sameJSON(payload[servedName(rec, k)], v) {
			return false, "", nil
		}
	}
	return true, guardFingerprint(str(rec, "kind"), payload), nil
}

// guardFingerprint is the fingerprint an edit of the row is guarded on: a
// backlog or finding row serves it as content_fingerprint, a requirement or
// an epic as fingerprint. "" when the row serves none.
func guardFingerprint(kind string, row map[string]any) string {
	if kind == "backlog" || kind == "finding" {
		// Never firstNonEmpty: its "—" for nothing would be sent as a guard
		// (CODE-OUTAGE-3-1).
		if fp := str(row, "content_fingerprint"); fp != "" {
			return fp
		}
		return str(row, "fingerprint")
	}
	return str(row, "fingerprint")
}

// servedName is the field a read serves for a request field. `author backlog`
// sends a backlog record's kind as backlog_kind; the store serves it as kind.
func servedName(rec map[string]any, field string) string {
	if field == "backlog_kind" && str(rec, "kind") == "backlog" {
		return "kind"
	}
	return field
}

func sameJSON(a, b any) bool {
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)
	var na, nb any
	_ = json.Unmarshal(ra, &na)
	_ = json.Unmarshal(rb, &nb)
	return reflect.DeepEqual(na, nb)
}

// printReplay is the summary on stderr.
func printReplay(out io.Writer, root string, res replayReport) {
	if res.foreign != "" {
		fmt.Fprintf(out, "Kept writes wait for the person who kept these writes: sign in as %s to record them. Nothing more was sent.\n", res.foreign)
	}
	conflicts, refusals := 0, 0
	for _, a := range res.aside {
		if a.sub == "conflicted" {
			conflicts++
		} else {
			refusals++
		}
	}
	if res.recorded+res.already+len(res.aside) > 0 {
		fmt.Fprintf(out, "Kept writes replayed: recorded %d, already there %d, conflicted %d, refused %d.\n",
			res.recorded, res.already, conflicts, refusals)
	}
	for _, a := range res.aside {
		fmt.Fprintf(out, "  %s: %s %s — %s\n", a.sub, a.name, a.label, a.reason)
	}
	for _, a := range res.retrying {
		fmt.Fprintf(out, "  kept for another try: %s %s — %s\n", a.name, a.label, a.reason)
	}
	if res.stopped != nil {
		fmt.Fprintf(out, "Replay stopped: %v. %d kept write(s) wait; they are recorded on the next write or with `modernpath outage replay`.\n",
			res.stopped, keptWriteCount(root))
	}
	if len(res.aside) > 0 {
		fmt.Fprintln(out, "Conflicted and refused writes are never retried: see `modernpath outage status`, then redo or delete each.")
	}
}

// outageReplay is `modernpath outage replay`.
func outageReplay(cmd *cobra.Command, _ []string) error {
	env, err := factoryEnvLoad()
	if err != nil {
		return err
	}
	if len(readJournal(journalDir(env.Root))) == 0 {
		fmt.Println("No process writes are kept on this machine.")
		return nil
	}
	res := replayKept(env, os.Stderr)
	switch {
	case res.stopped != nil:
		return reportedError{res.stopped}
	case res.foreign != "":
		return reportedError{fmt.Errorf("kept writes wait for %s", res.foreign)}
	case res.busy:
		return reportedError{errors.New("another replay holds the replay lock")}
	}
	return nil
}
