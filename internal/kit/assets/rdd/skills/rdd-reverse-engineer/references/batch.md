# Batch sweep

A sweep of a system with several bounded contexts runs as a batch: one
coordinating session asks once, writes a brief per context, starts short-lived
workers, and makes every store write. It can run unattended for hours.

## The batch question

Ask once, for the whole batch: the repositories; the contexts, each with a
code, a name, and the directories and shared mechanisms it owns; an id prefix
per context for titles and file names; exclusion globs, each with one reason
(generated, vendor, fixtures); the stored requirements to keep, by id; and
unattended: yes or no. Write the answer to
`batch-plan.md`. It is never asked again: a resumed session reads the batch
folder instead. The context code and name fill every requirement's `context`
and `context_name`.

## Batch folder

`.modernpath/reverse-engineering/<batch>/` holds `batch-plan.md` and
`batch-summary.md` (coordinator) and, per context, `<context>/brief.md`
(coordinator), `<context>/groups/*.json` and `<context>/handoff-<slice>.md`
(worker, one per slice), `<context>/open-questions.md` (worker, merged by the
coordinator), and `<context>/groups/recorded/` (written by `record --dir`: the
copy and receipt of each recorded file, the note of each refused file).

## Brief and handoff

The brief is the worker's whole prompt, at most 60 lines: the slice's file
list, the rules of [requirement quality](requirement-quality.md) in the
context's terms, the link kinds, the UUID rule and reference entries, a
`requirements search` step for reuse, the handoff template, and the exact
check command. A slice is a group of files that implement related behavior,
small enough to read whole. A worker never writes to the store. It stages
`groups/*.json` in the record input shape and writes `handoff-<slice>.md`:
first line `ready` or `partial:` with the remaining files, then surface units
covered out of total, near-matches found in the store and whether each was
reused or replaced (one `replaces: <stored id>` line per replacement), writes
it needs, open questions. A partial handoff starts a new worker on the
remainder. A worker that returns without its handoff is restarted once with
the same slice, then the slice is logged open. Each staged file refers only to
itself, earlier files in lexical order, or the store through reference
entries.

## Check and record

Run `modernpath reverse-engineer check --dir <context>/groups` and rewrite
what fails from the code. The store does not enforce this. Then record each
context with one call: `modernpath reverse-engineer record --dir
<context>/groups`. Never loop over `record` in the shell: one store command per
shell call. The folder is not atomic, only each file's request. Classify the
result:

- Sign-in, permission, server-request or binding failure: stop the batch and
  tell the person. Long runs outlive tokens; sign in again and rerun.
- `local-write` or `local-read`: the batch folder could not be written, or a
  staged file could not be read; stop and tell the person.
- Content refusal: the file wrote nothing. Fix it in place from the code and
  rerun, or note it in `open-questions.md` and move on. When the refusal names
  no content problem, rerun with `--retry-refused`.
- Check failure: rewrite from the code, check again, and record with an open
  question when it still fails. A file no handoff names is still staged work:
  ground it the same way and never delete it. Never hold a staged file back or
  move it out of `groups/`: the server refuses what it cannot accept, and
  `record --dir` notes the refusal.
- Server, unknown-outcome, rate-limit or connection failure: wait and rerun,
  which resumes, at most 3 times for the same file and reason; then log the
  context unfinished and go on.
- Changed after record: never edit a recorded file. Make a new file with new
  UUIDs, list it with the old id under `Replacements:` and make the old record obsolete in the end order below.

Receipts only help resume; the store is the authority. Read back with
`requirements get` and `traces list`.

## Stop list

The batch stops only for a sign-in, permission, server-request, binding,
`local-write` or `local-read` failure, a refusal named in `PROCESS.md` §Delegated passes, or a
question only the person can answer that blocks a whole context. Every other
gap goes to `open-questions.md` and the batch continues.

## Sample read and merge

Give the installed reviewer (`.claude/agents/rdd-cold-reviewer.md` or
`.codex/agents/rdd-cold-reviewer.toml`) this prompt with a sample of at least
20 records of each context, or all of them when it has fewer, and the code
they cite: "Rate each record against the code. There is no Epic and no PASS or
FAIL. Use this rubric: good states what the code does with its refusals, saved
effects and limits; weak is true but omits a refusal, side effect, unit or
limit the code has, or cites a file that does not implement it; not a
requirement is a step, a guard, an option set or a restated function; wrong
contradicts the code or names a caller no code shows. Return one line per
record: id, verdict, reason." For a weak, wrong or not-a-requirement record
the coordinator fixes it from the code and the reviewer's reasons, and the
fixed record gets a new read by a fresh reviewer. The summary reports the
first read's counts beside the final ones. Run `modernpath reverse-engineer
check --dir <context>/groups --repo <key>=<dir>` for each context and copy its
linked, excluded and unresolved counts into the summary. `<dir>` is the
context's own directory, never the repository root (repeat `--repo` for each
directory the context owns), with one `--exclude` per exclusion glob of
`batch-plan.md`. The end order is fixed: record every context, run the sample
read and record its fixes, make replaced stored records obsolete with each
result under `Replacements:`, all before writing `batch-summary.md`. On a
refused obsolete call the coordinator stops further obsolete calls, writes
`batch-summary.md` with the refusal verbatim, then ends the batch; every other
field is still filled. Then merge: link shared system requirements to the user
requirements they serve, list duplicates by outcome, combine the open
questions, and write `batch-summary.md` with, per context, the check result,
linked, excluded and unresolved file counts, system requirements without a
verifies trace and user requirements without a serving system requirement,
each by id, the sample verdicts, refused files, `Replacements:` (new id, the
stored id it replaces, and whether the stored record was made obsolete or the
refusal verbatim), open questions, and finished or unfinished
([coverage](coverage.md)): finished only when the sample has no
not-a-requirement or wrong verdict and at most one weak in ten, and the other
conditions there hold. The batch is not done until `batch-summary.md` has
every field for every context; a session that resumes a batch whose summary
lacks one finishes it.
