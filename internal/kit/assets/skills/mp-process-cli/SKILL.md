---
name: mp-process-cli
description: Operate the requirement-driven delivery loop through the modernpath CLI — the exact verb sequences for plan, cold review, entry, build, evidence, completion and apply; the work-selection model; the three fingerprints and what moves them; every refusal the loop meets and the verb that clears it. Use whenever an rdd skill says to record, select, pull, push, trace, gate, answer or advance and the workspace is store-backed; flags and defaults are in the installed `.modernpath/cli-reference.md`.
---

# Operating the loop through the CLI

<!-- TOOL-OWNED. Installed by `modernpath install`. -->

The rdd skills say *what* a pass records; this skill says *how* the `modernpath`
CLI records it. Nothing here changes the process — `.modernpath/rdd/PROCESS.md`
owns the meanings. Every verb, flag and default of the installed binary is in
`.modernpath/cli-reference.md`, rendered from the binary itself by
`modernpath install`; look a flag up there rather than guessing it. Every command below is the sanctioned path: if a step needs
something the CLI does not expose, that is a tooling gap to surface through the
project's gap channel, never a reason to call the API or edit a store file by
hand (`PROCESS.md` §State records and reconciliation).

Sections marked **store-backed** apply only when `process/store-backed.md`
declares the store; in a file-backed workspace the ledger files are the store
and `modernpath author` is a rehearsal tool, not a write path.

Two rules from the project's agent instructions apply throughout: one store
command per shell call, and store writes only from the orchestrating session —
a delegated pass returns findings and a verdict.

`PROCESS.md` names phases and never a verb. In a store-backed workspace each
phase is driven by these entry points; the sections below give the order and
the flags. Each phase runs with the batch verbs first — one call per step,
whatever the number of records — and the single-record verbs stay as the
fallback, for one record or on a CLI that lacks the batch verb:

| Phase (`PROCESS.md`) | Batch verbs | Single-record fallback | Section |
|---|---|---|---|
| session preparation | `process prepare-inputs` | — | Where the loop stands |
| any — where do I stand | one `process next`, even with several held pieces; `process check --phase <p>`, `your-move`, `factory status` | `process next --piece <id>` | Where the loop stands |
| source → plan | `author apply --file <plan>`, `working-set select … --phase plan [--lane defect]`, `working-set pull --scope`, `working-set push` | `author epic` / `requirement` / `update` / `relate` / `member` | 1 |
| cold review | `working-set pull --scope --for-review` (one `REVIEW.md`), then `process review record --file <review.json>` | `process findings add` / `disposition`, `author trace --purpose cold-review` | 1 |
| entry | `process enter <scope>`, the human's `factory answer`, then `author advance --gate ENTRY-<scope>` | `author advance <id> … --to TODO` per record | 1 |
| build → verify | `factory evidence --file <runs.json>`, then `process advance --all --piece <EPIC>` | `factory evidence --fail --role RED`, `factory evidence --pass`, `process advance <SR>` per SR | 2 |
| completion → apply | `process complete <scope>`, the human's `factory answer`, then `author advance --gate COMPLETE-<scope>`, `working-set select --put-down` | `author advance <id> … --to DONE` per record | 2 |
| triage | `author requirement` / `author epic` / `author backlog`, `author gate --gate-kind question`, `modernpath feedback "<line>"` for a tooling gap; `author demote` to reopen a delivered item | — | The write channels |
| session | `auth status`, `env --set`, `factory status` | — | Where the loop stands; the reference |

## Where the loop stands (both modes)

In a bound workspace, run `modernpath process prepare-inputs` before sourced
work. It checks the binding, CLI/kit and server contract with one context read,
and reports the held-piece count, active release, pending decisions, local docs
last synced and server docs last updated. It does not refresh documents or
change process records. For scripts, `--json` reports `ready` and the separate
documentation timestamps; readiness does not mean the local docs are current.
Run `modernpath docs sync` explicitly when a refresh is needed. Search locally
first, in this order: the repository for the requirement id, its code and
tests; `modernpath working-set pull <id>`; the docs export
(`rg -n "<terms>" .modernpath/<system-slug>/`, then read the matching files);
then `modernpath ask` for why and how questions the local search cannot answer.
Use `modernpath search` or `modernpath read-doc` when the export lacks the
material or a live answer is needed.

After running `modernpath process prepare-inputs`, always show its complete
human-readable output verbatim in a fenced `text` block in the user-facing
response. Include every section, warning, and remedy, including a `Not ready`
result. A summary does not replace the output. Put timing and any explanation
outside the block.

| Read | What it answers |
|---|---|
| `modernpath process next` | the derived phase, why, and the skill to run; `-v` prints the full packet aggregate and process revision. With several held pieces and no `--piece` it prints one block per piece — scope, phase, why, the skill, the gates waiting on it — and exits 0: session entry is one call |
| `modernpath process check --phase <p>` | one phase's decision-table checks for the current selection (a pure read) |
| `modernpath your-move` | the pending human decisions, ranked by what one answer releases |
| `modernpath working-set pull <id>…` | a record as a readable file with its `Fingerprint:` line |
| `modernpath factory gates <gate-id>` | one gate: state, answer, chosen options, applied state |
| `modernpath factory status` / `modernpath status` | the binding and whether the workspace is store-backed |

Phases are `source`, `plan`, `cold_review`, `entry`, `build`, `verify`,
`completion`, `triage` — the vocabulary `--phase` accepts.

## The write channels (store-backed)

Every write is single-record and actor-attributed; legality is enforced by the
server, and a stale fingerprint conflicts instead of overwriting. The batch
verbs — `author apply`, `process review record`, the `--file` forms,
`author advance --gate` and `process advance --all` — run those same
single-record writes in one call, chain each fingerprint, and report every
record's result. Retired files
are not recreated to record something — a file-derived batch is refused whole.

| Channel | Carries |
|---|---|
| `modernpath author requirement`, `author epic`, `author update`, `author member`, `author relate` | records, content edits, membership and UR–SR relations. A user requirement's scenarios are `author update --criteria <json or file>`: a JSON array whose every object carries an `external_id` (the replace-set keys on it) and `given`, `when`, `then`; the array replaces the stored set whole |
| `modernpath author apply --file <plan.yaml\|json>` | a whole plan in one call: the epic, its URs and SRs with their prose and criteria, the UR–SR relations and the membership, through `working-set push`'s write engine. The whole plan is checked before the first write; a missing record is created, then each record is one atomic patch carrying the authoring context. An existing record it would change is guarded by its `working-set pull` or a pinned `expected_fingerprint`; a re-run writes nothing; `--dry-run` prints the plan |
| `modernpath author trace` | an immutable PASS/FAIL/STALE trace gate at an exact fingerprint |
| `modernpath author gate` / `author gate-withdraw` | a human gate, born open; `--supersedes <old>` retires an open or answered decision in the same write; a mistakenly opened one is withdrawn with a reason |
| `modernpath author advance` | a lifecycle transition; a human-gated one names the `ANSWERED` gate and its fingerprint. `author advance --gate <GATE>` with no id reads the gate — its fingerprint, the FROM and TO of its transition, an `approve` answer — and advances every record its scope names that is still in FROM, members first, then the epic |
| `modernpath author demote` | the demotion gate that reopens delivered items — one id, or `--ids A,B,C` / `--file` for one gate over several items of one state (`--to PROPOSED\|IN_PROGRESS\|OBSOLETE --basis reversed-decision\|defect\|superseded --reason USER:…`, no prerequisite trace); `--apply` (with `--gate-id`, or an id for the newest gate in its series) after the answer advances every item on it and reports the user requirements and epics that followed |
| `modernpath working-set select` | the frozen scope, its phase, suspension, resumption, put-down |
| `modernpath working-set push` | packet sections whole and item files as atomic patches, from the scope directory `pull --scope` materialized; push never creates a record: an item file whose id the store does not know is refused before any write — create it with `author apply`, the only create path; a `members/` file whose record the store holds outside the frozen selection is skipped and named — re-select to push it |
| `modernpath process review record --file <review.json>` | a delegated cold review in one call: the reviewer's findings, its guarded dispositions and the cold-review trace at the aggregate the review pull stamped |
| `modernpath process findings add` / `disposition` | cold-review findings and their dispositions, one per call or a whole list with `process findings add --file` and `process findings disposition --file` |
| `modernpath process reconcile --apply` | the automatic transitions current trace proofs allow |
| `modernpath process enter`, `process advance`, `process complete` | the ceremony verbs (EPIC-CLI-017): the entry gate, the lower trace plus reconcile, and the delivered-revision evidence plus completion trace plus completion gate — each from the store facts, each refusing before any write on an unmet fact; `process advance --all --piece <EPIC>` advances every SR of the piece |
| `modernpath factory evidence` | a test or browser run as evidence, pinned to the repository HEAD; `--role RED` marks a red-first result; `factory evidence --file <runs.json>` records several runs in one call |
| `modernpath factory answer` | an attributable human answer on an open gate (a governed gate's review is attached for you) |
| `modernpath install --store-backed --source USER:…` | the store-backed declaration of a bound workspace that never had file ledgers: the activation gate answered with the source, the server state `active` and the marker, in one step; a workspace with ledgers goes through `migrate flip` |
| `modernpath author backlog` / `author update --kind backlog` | one backlog, gap or tooling record (`BACKLOG-…`, `GAP-…`), born `OPEN` and attributed to you; a disposition change names its `--source`; `working-set pull <id>` renders it |

A triage discovery that is neither a requirement, an epic nor a gate is a
backlog record (`author backlog <id> --kind backlog`), a gap is `--kind gap`
with its `--gap-kind`, `--affected-trace` and `--consequence`, and a tooling
gap is `modernpath feedback "<line>"`. These are store records like every
other (`PROCESS.md` §State records and reconciliation): their state is read
from the store — `process backlog list [--kind backlog|gap|tooling]
[--disposition <word>]` lists them newest first and `working-set pull <id>`
reads one — never from a plan file or a note, which carry no disposition the
store does not. Tooling feedback requires a ModernPath tenant credential and
a current checkout bound to the server-verified `modernpath` system. It refuses
customer workspaces rather than switching destinations. It never
files tooling records or local fallback files in customer projects. When that
destination is unavailable, report the gap in the session; do not substitute
`author backlog --kind tooling` in the customer store. Read feedback records
from a checkout bound to ModernPath. The release
registry file is retired;
the single active release and its `USER:` source are the newest answered,
approved `release_selection` gate naming `release:<slug>` on the system —
`GATE-RELEASE-<slug>`, or a successor `GATE-RELEASE-<slug>-<n>` — written by
`factory release activate` on the system it runs from.

## 1. Plan → cold review → entry (store-backed)

### Plan

**Author the records.** One call records the whole plan:
`author apply --file plan.yaml` (YAML or JSON):

```
epic: {id, title, description, expected_fingerprint}
requirements:
  - {id, kind: ur|sr, context, title, description, rationale, boundary,
     verification_method, criteria: [...], parents: [UR ids],
     expected_fingerprint}
members: [requirement ids]
```

It runs through `working-set push`'s write engine. Every record is checked
before the first write — one invalid record (a new one with no title or
context, a lane class on a UR, a criterion without `external_id`) stops the
call and nothing is written. A record the store does not know is created,
then patched — apply is the only create path, and push never creates; each
record's changes — the fields that differ (an empty field
is left alone), its criteria, relations and membership — are one atomic patch
carrying the authoring context. Each record's line shows its result and
fingerprint; an updated one shows `<replaced> -> <new>`. A re-run writes
nothing and each record reads `unchanged`. `--dry-run` prints the plan.
A fingerprint conflict is reported for that record and the rest continue;
any other refusal stops the run and names what was written. A record created
whose patch never ran reads `created, not patched`: `working-set pull <id>`
records its fingerprint, then apply the plan again. The call exits non-zero
when any record did not apply.

**Pull first, or pin fingerprints.** A record that already exists and would
change is guarded by the version you wrote the plan against: its
`expected_fingerprint` in the plan, or else the fingerprint of its
`working-set pull` (`working-set pull <id>`, or the scope pull's member
file). With neither, apply refuses it before any write; it never reads a
fingerprint at run time. A record whose fingerprint moved since then stops
the whole call — another session edited it; pull it again and keep that edit
in the plan. New records need neither, so a plan of new records is one call.
After apply updates a record, its by-id pull snapshot moves to the
fingerprint the write returned, so the next apply from the same pull goes
through. Only the fingerprint moves: the snapshot's content is from before
the write, and the record's line says so — re-pull it before copying from
it. A scope pull's file is left as pulled — push diffs it, and a moved
fingerprint would let the next push revert the write — and the record's line
says to re-pull the scope.

Fallback, one record per call, each carrying the fingerprint the previous
write printed: `author epic <id> --title … --description …`, `author
requirement <id> --kind ur|sr --context … --title …`, `author update <id>
--expected-fingerprint … --description … --rationale … --boundary …
--verification-method …` (a UR's scenarios with `--criteria`), `author relate
<SR> --expected-fingerprint … --parent <UR>`, `author member <epic>
--expected-fingerprint … --member <id>…`.

### Take the scope

**Take the scope.**
`working-set select <EPIC|SR> --kind epic|single_sr [--members a,b] --phase plan [--lane defect]`
(`--lane defect` marks a customer-blocking defect: `process next`, `your-move`
and the session brief say so, and the defect-lane rules of `rdd-start` apply).
A take with no `--replaces` *adds* a holder — you may hold several pieces; name
the one you are displacing with `--replaces <piece> --outcome done|obsolete|returned=<phase>`.
Advance the phase later with `working-set select <scope> --phase <p>`.

### Author the packet

**Author the packet.** `working-set pull --scope` materializes
`.modernpath/working-set/<scope>/` — item files and `packet/*.md` sections —
under an authoring context. Edit, then `working-set push` (`--dry-run` first).
Pull stages required reads and checks local hashes before replacing or deleting
managed files. Edited, deleted or unbaselined targets refuse the refresh;
unserved drafts stay in place. Packet 404 preserves untracked files and edited
never-served scaffolds. Do not delete the baseline to force a pull.
After a push error following accepted writes, preserve files and retry: matching
canonical content reconciles metadata while retaining authored bodies. A local
edit during reconciliation refuses it. A known scaffold has no pull-time packet
pin: matching canonical content can establish its accepted pin on retry.
When missing legacy pins or accepted operation markers require manual recovery,
rename the whole scope directory
to a dated recovery path under `.modernpath/working-set/`, pull a fresh scope,
and compare/reapply wanted edits. Preserve that recovery copy.
If a file only withdraws already-absent targets and its store fingerprint has
not changed since pull, push reports a no-op and continues with other files.
Remove the named withdrawal markers locally; push preserves that file and its
baseline. A changed or missing fingerprint still requires retry recovery.
Until the push order is fixed in the tool: a member-record patch and its packet
sections may go in one push: item patches post first, and every filled section
the server then reports as stale is re-put unchanged so it carries the current
stamp — the push says `re-stamped N section(s)`, never "nothing to do"
(`--restamp` forces every filled canonical section). The scaffold reads the
required keys the server serves (`facts.sections.required`), so the stubs and
`process check --phase plan` walk one key set; a FAIL on `canonical_sections`
names the missing or stale keys. The fixed sections are `10-recon.md`
(reconnaissance), `15-state-inventory.md` (the state inventory — one row per
piece of state the change touches, or the one-line declaration that it
touches none; the stub names the columns), `30-red-strategy.md` and
`40-decisions.md`, plus `20-enrichment-<SR>.md` per selected system
requirement (SR-CLI-028-001).

### Approval file listing

**Approval file listing.** Before asking for human approval, present the brief
and list the related working-set files (`PROCESS.md` §Gates). Materialize a
selected scope with `working-set pull --scope`; pull named gates or items with
`working-set pull <id>…` as needed. Read the files and apply the currency checks
for the applicable file type and decision in `PROCESS.md` §Gates. Include the relevant scoped item
files, acceptance content, packet brief, decisions, findings, and evidence.
Resolve each listed file to its actual full absolute filesystem path and use
that path as both the visible Markdown link label and the target, followed by
a short description. Keep the links outside code formatting; use filesystem
paths rather than `file://` or editor-specific URIs. Wrap a target containing
spaces in angle brackets. Resolve missing or stale files before asking; never
invent paths. Full file contents are shown only when the human requests them.

### Cold review

**Cold review** runs from an independent context and records from it.
Delegate the pass to the installed reviewer, `.claude/agents/rdd-cold-reviewer.md`
(Read, Grep, Glob; no shell): it returns findings and a verdict as JSON, and
this session records them in one call.

1. `working-set pull --scope --for-review` creates an immutable snapshot under
   `.modernpath/working-set-reviews/<scope>/<context-id>/`, preserving authoring
   files. `MANIFEST.json` binds store, system, scope, aggregate and file hashes.
   Pass the printed context ID and exact snapshot directory to the delegated
   reviewer. It reads that directory's `MANIFEST.json` and `REVIEW.md`; a missing
   snapshot stops the review rather than selecting another bundle.
   The snapshot is a read-only render that stamps a
   review context and the packet aggregate in `.context`, and writes the
   whole packet to one `REVIEW.md`: the aggregate header, the epic, the packet
   sections, the URs with their scenarios and the SRs with statement,
   rationale, boundary and verification method, each under an
   `id · fingerprint` heading, the members taken from the epic's stored
   membership. The reviewer reads that one file. Pushes from the review
   render are refused by design. The stamp also carries each record's and
   section's fingerprint (`reviewed: <key> <fingerprint>` lines).
   A later round adds `--since <previous CR-TRACE-…>`: `REVIEW.md` then holds
   the previous trace's code revision beside HEAD, what changed since that
   review in full, the open findings, the previous verdict, and the unchanged
   ids with their fingerprints only; the stamp and the trace still pin the
   full aggregate. It needs a previous trace recorded with reviewed
   fingerprints (one full round by this CLI first), is refused for a trace
   that is not this scope's cold review, and never takes the by-id narrow
   form.
2. The reviewer returns `{verdict, body, source, findings[], dispositions[]}`:
   a finding is `{id, category, severity, owner, source, body,
   introduced_by}`, a disposition `{id, from, disposition, ref, resolution,
   widens}` — a `RESOLVED` one names its `resolution`. Save it as
   `review.json`.
3. `process review record --file review.json --review-context <context-id>` (`--scope`, or the held piece)
   records the new findings (ids already recorded are skipped), applies each
   disposition only while its finding is still in `from`, then records the
   cold-review trace `CR-TRACE-<scope>-<review context>` (`plan->entry`)
   naming the scope and each member, pinned to the aggregate the review pull
   stamped, with the stamped per-item fingerprints as
   `reviewed_fingerprints`. It refuses before any write without a validated snapshot, when the
   aggregate moved since the pull (re-pull `--for-review` and review again),
   or on a PASS that would leave a material finding `OPEN` or `DEFERRED`. A
   failure stops before the trace; a re-run records only what is missing.
4. `process check --phase cold_review` → `independent_verdict` and `findings`
   shows the checks when you need them.

Findings rules, whichever verb records them: both `category` and `severity`
are required (the CLI never defaults to the most blocking pair), and a
`note` severity never blocks whatever its category. Material categories are
`correctness`, `security`, `data_loss`, `contract`, `traceability`,
`testability`; a material finding left `OPEN` **or `DEFERRED`** fails the
check — an out-of-scope pre-existing finding is `REJECTED`, not deferred.
Findings are scope-scoped, not aggregate-scoped. `process findings list`
prints severity, per-round counts — including `widened` and `on earlier
resolutions` — and flags a round whose material findings are all
`traceability` (audit the change, not the document) or all on earlier
resolutions (the packet was reviewed incomplete: return it to `rdd-plan`);
`--all` groups rows and rounds per scope with a heading each, and `--json`
writes the rows and the per-scope rounds as one object (SR-CLI-027-004: a
convergence survey is one call). A `RESOLVED` disposition names its kind
(below), in a file as on the flags; a review file that breaks a resolution
rule is refused before any write.

By hand — one record per call, or on a CLI that lacks `review record`:

1. Read the full aggregate: `process next -v` → `full packet_fingerprint:`.
   The unadorned line is a 12-character display prefix that `--fingerprint`
   accepts and never matches.
2. Findings: `process findings add --scope <kind>:<id> --id <F-id> --category
   <c> --severity <s> --owner <o> --body … --review-context <context-id> --aggregate <full aggregate>`, or a
   whole list with `process findings add --file findings.json` (a JSON array
   of `{id, scope, category, severity, owner, source, body, introduced_by}`,
   each pinned to its own scope's aggregate). With `--review-context <context-id>`,
   every entry must name that snapshot's scope and any explicit aggregate must
   match it; the whole batch is checked before its first write. `--introduced-by <F-id>` (the
   file's `introduced_by`) names the earlier finding of the same scope whose
   resolution introduced the mechanism this one faults (SR-CLI-027-003).
   Dispositions: `process findings disposition --id <F-id> --disposition
   RESOLVED|DEFERRED|REJECTED --scope <kind>:<id> --from <what you saw>` —
   the CLI reads the finding's fingerprint for the scope, and
   `--expected-fingerprint` from `process findings list` still works — or
   several with `process findings disposition --file dispositions.json`
   (`{id, scope, from, disposition, ref, resolution, widens}`, each written
   only while its finding is still in `from`). No flag resolves every open
   finding: a disposition names its finding.
   A `RESOLVED` names how it resolved (SR-CLI-027-001): `--resolution
   packet-edit` (the packet was clarified), `scope` (a scope action; `--ref`
   names its record) or `decision` (`--ref` is its `USER:` source) — the
   file's `resolution` takes the same values and a review file's
   dispositions carry it too; the list prints `RESOLVED/<kind>`. A packet
   edit does not change a member: the server refuses a `packet-edit` whose
   scoped member changed since the finding was raised, unless `--widens
   USER:<date>:<why>` (the file's `widens`) states the widening on the
   human's word (SR-CLI-027-002; `-v` prints it). A finding already
   `RESOLVED` changes only its reference: `--ref --expected-fingerprint …`
   with no `--disposition`.
3. Verdict: `author trace CR-TRACE-<scope>-R<n> --purpose cold-review
   --verdict PASS|FAIL --scope <epic> --scope <each member> --source …
   --title … --review-context <context-id>`. The pin defaults to the selected snapshot aggregate and
   the transition to `plan->entry` (REQ-CROSS-376/377); a given
   `--fingerprint` must be the full 64-character hash of the right class (a
   display prefix or conflicting snapshot pin is refused), and a transition is `--from`/`--to`
   or one quoted `--transition FROM->TO` — a fragment is refused before any
   write. The trace requires an explicit snapshot selector, validates file
   integrity, and stores its context, aggregate and digest in the verdict body.
4. `process check --phase cold_review` → `independent_verdict` and `findings`.

### Entry

**Entry.** `modernpath process enter <scope>` does the ceremony from the store
facts: it refuses naming the fact when no independent passing cold-review
trace exists at the current aggregate, when a packet section is missing,
when the `entry_brief` packet section is absent or incomplete, or when
`ENTRY-<scope>` is open or answered (answer or apply it); otherwise it opens
the gate naming the epic plus every member still in FROM (a single SR names
itself), the cold-review trace as prerequisite and `PROPOSED->TODO`, and
moves the selection to phase `entry`. A section whose content is unchanged
but whose scope context moved — a record edit moves it — is not refused:
once the cold review passes, the verb re-puts it with its served content
under its served fingerprint (a concurrent edit conflicts), re-reads, and
refuses only the sections still missing, by name. `--dry-run` prints the
plan and names the sections it would re-stamp, writing nothing. It
also refuses on reconnaissance drift (SR-CLI-028-002): the selection must
carry the revision the packet was reconnoitred at (`working-set select
<scope> --recon-revision <sha>`; absent, the verb refuses before the facts
read); it fetches the remote default branch (`--no-fetch` for offline
fixtures only) and, when the tip moved past that revision and a path the
packet cites as `CODE:`/`TEST:` changed from the merge-base to the tip,
refuses naming the tip and the paths — a packet reconnoitred on a branch
ahead of main is current, not drift. Re-reconnoitre and re-select with the
new revision, or, on the human's word, `--allow-drift USER:<date>:<why>`
records the source, the tip and the paths on the gate body; in a two-call
entry both calls run the check and take the flag. The brief is
the `packet/entry_brief.md` section in the PROCESS.md brief shape (`- What:`,
`- Why now:`, `- Changes if approved:`, `- Risk if wrong:`,
`- Recommendation:`; every bullet required), or `--brief-file` with a JSON
brief object or the same markdown bullets.

*As-built members first.* An epic whose members split between PROPOSED and
PENDING_VERIFICATION enters in two calls: the first opens
`ENTRY-<scope>-VERIFY` (`PENDING_VERIFICATION->TODO`) naming the as-built
members only, pinned at the epic's packet aggregate so the epic's cold review
is its prerequisite — the store admits it for a still-PROPOSED epic on that
passing review — and says which members and the epic enter next; once those
members are `TODO`, the second call opens `ENTRY-<scope>` as usual. An epic
already entered (`TODO`, `READY`, `IN_PROGRESS`, `IN_REVIEW`) with as-built
members gets the verification gate alone, answerable in Mission Control or
with `factory answer`; a `DONE` or `OBSOLETE` epic is refused (demote or
reopen it first). A member entered through its own members-only gate is
re-pinned after a packet move with `process reapply-entry <member>` and
re-entered with `process reenter <member>`, which reads the piece that holds
it.

*Successor ids.* When `ENTRY-<scope>` (or `-VERIFY`) is already closed or
withdrawn — a scope demoted to PROPOSED and re-planned — the verb derives the
next free `ENTRY-<scope>-R2`, `-R3`… and names the closed predecessor in the
gate body; `--gate-id` overrides the id only — an open or answered id is
never rotated past.

By hand — when the verb refuses for a fact you must record first, or on a
CLI or server that lacks it — record the entry trace (`author trace
ENTRY-TRACE-<scope> --purpose entry --verdict PASS --scope … --prerequisite
CR-TRACE-<scope>-R<n>`; the pin defaults to the packet aggregate), then open
the gate **naming the cold-review trace**:

```
modernpath author gate ENTRY-<scope> --purpose entry --gate-kind approval_request \
  --transition "PROPOSED->TODO" --scope <epic> --scope <each member> \
  --prerequisite CR-TRACE-<scope>-R<n> --prerequisite ENTRY-TRACE-<scope> \
  --option approve="Approve entry" --recommended-option approve --brief-file brief.json
```

Trace **before** gate: the gate is refused unless a named prerequisite is a
passing cold-review trace pinned to the current aggregate, and a gate cannot be
edited after birth. The gate scope is the epic plus **every member not yet
entered** — the server refuses a gate that names the epic but omits one — or
the single requirement; several items with no epic cannot be pinned. Members
of an epic that is already `TODO` are recovered by a members-only entry gate
naming them (its prerequisite is the epic's cold-review trace). Move the
selection: `working-set select <scope> --phase entry`.

### Answer, then apply

**Answer, then apply.** The human answers in Mission Control or with
`factory answer ENTRY-<scope> --options approve` (a human decision; the CLI
attaches the review a governed gate needs). Answering records; it moves
nothing. Apply in one call:

```
modernpath author advance --gate ENTRY-<scope>
```

It reads the gate — its fingerprint, the FROM and TO of its transition, and
an `approve` answer when the chosen options are exactly `approve` (any other
answer is echoed with `--gate-answer`) — then advances every record the
gate's exact scope names that is still in FROM, members first, then the
epic; it prints each result, skips a record already past, and stops before
the epic when a member is refused. `--expected` is always the gate's FROM
state, never a fresh read. The gate closes and reads `applied` once every
named transition has landed.

Fallback, one record per call, members first, then the epic:

```
modernpath author advance <member> --kind requirement --to TODO --expected PROPOSED \
  --gate ENTRY-<scope> --gate-answer approve --gate-fingerprint <gate Fingerprint>
modernpath author advance <epic> --kind epic --to TODO --expected PROPOSED --gate … 
```

`--gate-fingerprint` is the gate's content-shadow hash — the `Fingerprint:`
line of `working-set pull ENTRY-<scope>`, or `fingerprint` in `factory gates
ENTRY-<scope> --json`; `author advance <id> --gate <GATE>` also reads it, and
the FROM, TO and kind, when they are omitted. `--gate-answer` accepts the
option key, its label, or the stored answer text.

The entry pass ends here: report the applied transitions and stop. The answer
was a decision about the gate, not an instruction to build (`PROCESS.md`
§Gates); §2 begins in a session the human opens for the build.

## 2. Build → evidence → completion → apply (store-backed)

The operator records evidence; the tool does the ceremony (EPIC-CLI-017).

1. `working-set select <scope> --phase build`.
2. **Evidence**, in one call: `factory evidence --file runs.json`, a JSON
   array with one run per entry, `{pass, fail, skip, role, revision, kind,
   log, totals}` — each SR's RED entry `{"fail": ["<SR>"], "role": "RED",
   "revision": "<red-commit>", "log": "<command>"}` and its passing entry
   `{"pass": ["<SR>"], "log": "<command>"}`. Each run is posted on its own
   with an id derived from the whole entry, so running the same file again
   updates the same runs and records any that failed; a refused run is
   reported and the rest continue.

   Single-record fallback — **RED**, recorded at the RED commit: `factory
   evidence --fail <SR> --role RED --kind local_test --log "<command>"`, or
   later with `--revision <red-commit>` (no checkout); **GREEN**: `factory
   evidence --pass <SR> --kind local_test --log "<command>"`. Evidence
   currency is role-aware: a RED never shadows a passing result, and the
   server warns at record time about a pass with no RED before it or a RED
   recorded after a pass.
3. **Advance**, in one call: `process advance --all --piece <EPIC> --log "<command>"`
   runs the per-SR advance below for every system requirement of the piece,
   prints each SR it moved and, for each one it did not, the refusal that
   names why; it exits non-zero when any SR was not advanced, and the ones
   that were stay moved.
4. **Advance one SR**: `modernpath process advance <SR> --log "<command>"` reads the
   facts of the piece that holds the SR (`--piece` when you hold several),
   refuses naming the fact (no RED recorded, evidence failing/claimed/stale,
   the SR not a member, no delivery facts served), records
   `TRACE-LOWER-<SR>` at the SR's content hash and reconciles until nothing
   remains — `TODO->IN_PROGRESS->IN_REVIEW` for the SR and the epic's own
   step. A second call reports nothing to do. Sibling transitions and FAILs
   are information; a FAIL naming the SR fails the verb.

   By hand: `author trace TRACE-LOWER-<SR> --purpose lower --scope <SR>
   --verdict PASS` (the pin defaults to the SR's content hash — the
   `Fingerprint:` line of `working-set pull <SR>`, never the aggregate or the
   git revision; the transition defaults to `build->verify`), then `process
   reconcile --apply` twice. An epic is held at `IN_PROGRESS` while any member
   carries a `STALE` member-scoped cold-review trace with no newer pass at
   member scope: re-record the member-scoped trace at the aggregate the pass
   reviewed, saying in its body that it reflects that review.
5. **Deliver** through the project's integration path, then check out the
   merged revision: completion runs at the **delivered revision**, the tip
   of the remote default branch.
6. **Complete**: `modernpath process complete <scope> --log "<ci run>"
   [--body "<audit and disclosures>"]` fetches the default branch and refuses
   unless HEAD is its tip (behind, or not on the branch, is named), refuses a
   member not `IN_REVIEW` (`process advance` it) or an epic not `IN_REVIEW`
   that was never completed before (`process reconcile --apply` folds it),
   then records one `ci` run at HEAD naming the epic, its user requirement
   and every member this completion moves (a `DONE` sibling an earlier
   completion accepted keeps its CURRENT evidence and is never re-posted),
   records `COMPLETE-TRACE-<scope>` PASS at the packet aggregate
   naming every `IN_REVIEW`/`DONE` member, opens `COMPLETE-<scope>` naming
   that trace and the epic plus every member still `IN_REVIEW` (`OBSOLETE`
   and `DEFERRED` members are excluded and disclosed), with the brief from
   `packet/completion_brief.md` (or `--brief-file`), and moves the selection
   to phase `completion`. `--dry-run` prints the plan. A reopened epic —
   `IN_PROGRESS` after a defect demotion, every member back in `IN_REVIEW` or
   `DONE`, its earlier `COMPLETE-<scope>` closed — completes in the same
   call: the successor trace `COMPLETE-TRACE-<scope>-R2` is recorded,
   reconcile folds the epic to `IN_REVIEW`, and `COMPLETE-<scope>-R2` opens
   naming the predecessor; a rebuild that moved the packet aggregate under
   the applied entry approval is refused naming `process reapply-entry`.
   The epic's user requirement is a member the facts serve, so the run, the
   trace and the gate name the epic and its user requirement `UR-<epic>`
   (`UR-<suffix>` for `EPIC-<suffix>`) while it is `IN_REVIEW`; it reaches
   `IN_REVIEW` through its upper trace by hand (`author trace TRACE-UPPER-<UR>
   --purpose upper --scope <UR> --verdict PASS` — like `lower`, an upper trace
   reads its pin from the store, the UR's own content hash, and infers
   `build->verify`; name `--fingerprint`/`--from`/`--to` only to override) and
   `process reconcile --apply` — `process advance` takes an SR. `working-set pull <epic>` lists only the SRs under
    **Members** — pull the UR by id to read its state.

   By hand, in this order: `factory evidence --pass <epic>,<UR>,<member>,…
   --kind ci --log "<run>"` (an epic or UR with no posted passing run of its
   own reads `:claimed` and the gate refuses with "not yet"); `author trace
   COMPLETE-TRACE-<scope> --purpose completion --verdict PASS --scope <epic>
   --scope <each member> --body …` (pin defaults to the aggregate, transition
   to `IN_REVIEW->DONE`); then the gate naming it, scoped to the epic, every
   SR member not yet `DONE` and the UR while it is `IN_REVIEW` — a gate that
   omits one is refused with `exact_scope: completion gate names <epic> but omits
    members not yet DONE: <ids>`:

```
modernpath author gate COMPLETE-<scope> --purpose completion --gate-kind approval_request \
  --scope <epic> --scope <each member> \
  --prerequisite COMPLETE-TRACE-<scope> --option approve="Approve completion" \
  --recommended-option approve --brief-file brief.json
```

7. The human answers (Mission Control, or `factory answer COMPLETE-<scope>
   --options approve --text "USER:<date>: …"`).
8. **Apply** in one call: `author advance --gate COMPLETE-<scope>` — every SR,
   the UR, then the epic. Fallback, members first: `author advance <member>
   --kind requirement --to DONE --expected IN_REVIEW --gate COMPLETE-<scope>
   --gate-answer approve --gate-fingerprint <gate Fingerprint>` for every SR, then the UR (also
    `--kind requirement`), then the epic with `--kind epic`.
9. `working-set select <scope> --put-down --outcome done`; `process next` now
   reads complete or "no current selection".

### Retiring a gate that cannot be approved

A governed gate born with no prerequisite trace, or pinned under an aggregate
that has since moved, is unapprovable, and a gate id is opened once. Retire it
by superseding it with one that can pass:

1. Hold the piece: `working-set select <epic> --kind epic --phase completion`.
2. Read the **current** aggregate: `process check --phase completion -v --piece
   <epic>` prints `full packet_fingerprint:`. `process next -v` may answer
   `no route derived — entry_origin_unavailable` — a member whose evidence is
   not current has no live entry origin — and it still prints the aggregate.
3. Record the completion trace at that aggregate (step 9), then open the
   successor naming both: `author gate COMPLETE-<scope>-2 … --prerequisite
   COMPLETE-TRACE-<scope> --supersedes COMPLETE-<scope>`. The old gate reads
   `superseded` in `factory gates <id>`; the new one carries the link.
4. Park the piece while the human answers, so your other held pieces stay
   unambiguous: `working-set select <epic> --suspend --reason "awaiting the
   completion answer" --waiting-on COMPLETE-<scope>-2`.
5. On the answer: `working-set select <epic> --resume`, `author advance` the
   SRs, the UR, then the epic (step 12), and `--put-down --outcome done`.

### Reopening a delivered item

A shipped requirement that turns out defective, or whose decision is
reversed, is sent back with one attributable decision — never a duplicate
requirement, a hand-edited status or a synthetic failure (`PROCESS.md`
§Attributable demotions):

1. `author demote <id> --to IN_PROGRESS --basis defect --reason "USER:<date>:<why>"`
   (or `--to PROPOSED --basis reversed-decision`; `--to OBSOLETE --basis
   superseded --superseded-by <new id>`). It reads the item's status (only
   `IN_REVIEW` or `DONE` is demoted), refuses locally without a `USER:`
   reason or with a basis that does not match the destination, and opens
   `DEMOTE-<id>`: purpose `demotion`, no prerequisite trace — an
   attributable decision, not a fingerprint check, so it stays answerable
   after the packet moves. A delegated agent is denied the verb.
2. The human answers (Mission Control, or `factory answer DEMOTE-<id>
   --options approve --text "USER:<date>: …"`).
3. `author demote <id> --apply` advances the item on the gate with the triple
   read from the store. The server invalidates by basis — a defect stales
   the item's own evidence, lower trace and the completion traces and leaves
   the epic's cold review and the user requirement's upper validation
   standing; a reversed decision stales the plan too. Only a reversed
   decision retires the item's own entry inside the shared entry gate
   (siblings keep theirs). Whatever the basis, the apply moves the user
   requirement and the owning epic that were `IN_REVIEW`/`DONE` on the same
   gate (the epic to `PROPOSED` for a reversed decision, else
   `IN_PROGRESS`); the verb prints them as `followed:` lines.

Several items reopened by one decision take one gate:
`author demote --ids A,B,C …` (or `--file`, one id per line). All items must
be `IN_REVIEW`, or all `DONE` — a mixed batch is refused before any write,
naming both groups; run the group holding user requirements first. The gate
is `DEMOTE-<first id>`, or the next free `-R<n>` when the earlier gates in
the series are closed or withdrawn (an open or answered one is refused and
named); the printed hint names it. After the answer,
`author demote --gate-id <gate> --apply` advances every item, user
requirements first, skipping an item already applied; an item another gate's
follow already moved is reported with that gate and passed over, and the
output names `author gate-withdraw <gate>` for the gate that then cannot
close. Any other refusal stops the run, lists what was applied and what
remains, and exits non-zero — re-run the same command to resume.
`author demote <id> --apply` without `--gate-id` applies the newest gate in
the `DEMOTE-<id>` series, every item on it.
4. Next: a defect is rebuilt red-first, `process advance` returns it to
   `IN_REVIEW` on its lower trace — with no re-entry, even when no entry
   gate names it (an epic entered through a legacy approval gate) — and
   `process complete` re-completes the epic on the siblings' current
   evidence (§2 step 6); a reversed
   decision is re-planned and cold-reviewed, and `process enter` opens the
   successor entry gate `ENTRY-<scope>-R2` (§1 Entry).

### The small-change lane (store-backed)

One small change — one SR in no epic, a class a current lane authorization
covers, at most five non-test source files and no excluded area — goes
through the lane (`PROCESS.md` §Small-change lane) instead of epic or
single-SR scope. The authorization is the human decision; the server checks
every stand-in for the per-change answer and refuses by name.

- **Authorize, once per System:** `process lane authorize --classes <c,…>
  --appliers <user id,…> --expires <date> --cap <n> [--exclude <glob>]…`
  (or `--file lane.json`) opens the gate and prints its id. **A workspace
  admin answers it once: in the web app (Mission Control), or with `process
  lane approve <gate> [--text "<decision>"]`** — one call to the server's
  `lane_approve` action, answered as the signed-in user (never an actor named
  in the request), refused before any write when the gate is not a
  `lane_authorization`. `factory answer` on it is refused, and so is any
  other client (an API, integration or MCP token). `lane approve` asks in the
  kit and the subagent guard denies it: the decision is the human's, made in
  the conversation. `process lane` (no subcommand) shows the current
  authorization.

The verb sequence for one small change, five writing calls and one read:

1. `author apply --file sr.yaml` — the SR with its statement, boundary,
   verification method, `lane_class` (`defect_with_failing_test`, `wording`,
   `presentation`, `dependency_patch`) and `sources: [USER:…]`; the lane
   enters only a sourced SR, and the class is set **before** the review,
   which pins it (`author update <SR> --lane-class <class>` to set it alone).
2. `working-set pull <SR> --for-review` — a read: renders the SR to
   `.modernpath/working-set/<SR>/REVIEW.md` under a fresh review context,
   stamping the SR's content fingerprint and packet sections. Explicitly request
   a narrow lane review and pass the SR, printed context ID and exact bundle path
   to the delegated reviewer. It reads that file and its sibling `.context`,
   checks their identity and returns the usual review JSON. This by-ID bundle
   has no snapshot manifest; it is never a fallback for a scoped review.
3. `process lane review <SR> --file review.json` — one narrow pass, no
   rounds. It refuses before any write without the stamp, without a
   `lane_class`, or when the SR changed since the pull; it holds the SR as a
   `single_sr` piece (the single-SR aggregate is served only for a held
   piece), then records `LANE-REVIEW-<SR>-<context>`: a cold-review trace on
   `PROPOSED->TODO` at the SR's single-SR aggregate, with the review context
   and a `LANE:narrow` source. A FAIL is recorded and the change leaves the
   lane for single-SR scope.
4. `process lane enter <SR>` — reads the narrow review and the newest
   answered authorization, then posts one advance `PROPOSED->TODO` with
   `lane_ref` and no `gate_ref`; the server checks currency, class, the
   signed-in applier, the daily cap and the review's independence. On an
   entered SR that changed afterwards (TODO through IN_REVIEW) it posts
   `lane_reapply` instead — review the changed SR again first.
5. Build red-first, then `factory evidence --file runs.json` (RED, then the
   passing run).
6. `process advance --all --piece <SR> --log "<command>"` — to IN_REVIEW.

After delivery to the default branch:

- `process lane check <SR> --commit <sha> [--base <sha>]` posts `git diff
  --name-only` over `<sha>^1..<sha>` (a merge or squash commit) or
  `<base>..<sha>` (a rebase delivery). It refuses a commit the default
  branch does not reach and a base that is not the commit's ancestor. The
  server has no repository access: it records the SR's delivered revision
  and the lane-eligibility trace as its own verdict over the file list the
  CLI reported, and says so. The first full report for a commit counts — a
  later one for the same commit that leaves a file out or uses another base
  is refused; a new commit may be reported. A DONE or OBSOLETE SR is refused.
  A FAIL names each offending file, exits non-zero, and the change leaves
  the lane.
- `process lane complete --log "<ci run>"` takes every IN_REVIEW small change
  with a passing eligibility at its delivered revision, records the run you
  name as the run the agent reported (nothing verifies it, and the brief
  says so) and `LANE-COMPLETE-TRACE-<SR>` at its single-SR aggregate,
  and opens one `LANE-BATCH-<date>` gate: options `approve` and
  `reject:<SR>` per member, each change's files in the brief. It lists the
  small changes left out and why. `--dry-run` prints the batch.
- The human answers it (web app, or `factory answer <id> --options
  approve,reject:<SR>`): the answer approves every member not rejected.
- `process lane complete --apply [--gate <id>]` advances each approved member
  `IN_REVIEW->DONE`; a rejected one stays IN_REVIEW, to be fixed and batched
  again. Once answered, the batch is pinned to its approved members only, so
  fixing a rejected change never blocks the approved ones.

A lane SR never completes through an ordinary completion gate unless it
re-entered with a full packet and review.

## 3. The work-selection model (store-backed)

- **One holder per piece; several pieces per person.** The constraint stops
  two people taking the same work and does nothing else. A take of a piece
  someone else holds is refused naming the holder; a suspended piece is
  claimable by anyone.
- **`--piece <scope>`** on `working-set` and `process` names which of your
  current pieces a read or write resolves. With several held and none named,
  `process next` prints one block per held piece — its scope, phase, why,
  the skill to run and the gates waiting on it — names the `--piece` remedy
  and exits 0, so session entry is one call; every other scoped read and
  write is refused by name — `process check`, `process reconcile` and
  `process advance` print "you hold several current pieces (2): A, B — name
  one with --piece <id>", the server answers 409, and `factory status` lists
  what you hold; "no current selection" is printed only when you hold none.
  Name the piece.
- **Suspend / resume / claim / put down:** `working-set select <scope>
  --suspend --reason … [--target … --waiting-on …]`; `--resume <scope>`;
  a fresh take of a suspended scope claims it; `--put-down --outcome
  done|obsolete|returned=<phase>` closes it. Only a piece you currently hold
  can be suspended: to change a parked piece's reason, resume it first; the
  earlier reason stays on the closed row.
- **Re-read the selection immediately before mutating it.** A stale snapshot
  has displaced a colleague's live selection and closed it with the default
  outcome. `working-set check` reports stale files; `--refresh` re-pulls.
- The selection is caller-scoped: reads and writes resolve against the
  authenticated person's pieces, never a colleague's.

## 4. The fingerprint model (store-backed)

Three pins, one per trace class, named the same way everywhere: the
**packet aggregate**, the **content hash**, the **process revision**.
`author trace` reads the right one from the store when `--fingerprint` is
omitted and refuses a display prefix or the wrong class by name
(REQ-CROSS-376); the ceremony verbs never take one. Passing a wrong one by
hand records a trace that never matches and can never be removed.

| Trace | Pin to | Read it from |
|---|---|---|
| lower (an SR's `LOWER-<SR>`) | the SR's content fingerprint (its sync shadow) | `Fingerprint:` in `working-set pull <SR>` |
| cold-review, entry, completion | the packet **aggregate** of the scope | `full packet_fingerprint:` under `process next -v` / `process check … -v` |
| evidence (`factory evidence`) | the git revision | pinned to `HEAD` at record time |

A **human gate** has its own content-shadow hash (`Fingerprint:` in
`working-set pull <gate>`) — that is what `author advance --gate-fingerprint`
and `author update --expected-fingerprint` echo, and what a stale copy makes
conflict instead of overwrite.

What moves what:

- The **aggregate** folds the scope's member records and packet sections. It
  does not move on lifecycle changes or on git HEAD. Editing a **member
  record** moves the scope context and thereby the stamp on every sibling
  packet section; each section is re-stamped only by pushing changed content.
  Prefer editing a packet section over a member record when a fix can live in
  either.
- A packet section is stamped for its **own** scope's context, so a section on
  a member is judged against that member, not the ambient selection.
- A decision gate is pinned to the subject it **names**: an epic plus items
  pins to the epic; a single item to that item; several items with no epic is
  refused.
- Re-pulling `--scope` rotates the authoring context. `--for-review` creates an
  isolated snapshot; findings and cold-review traces select its printed context
  ID explicitly. Repeated reads detect observable drift, not an endpoint-wide
  transaction or ABA changes. Findings and process projections are informational.

## 5. Refusal glossary

`origin: cli` strings are printed by this binary; `origin: server` strings
arrive as `server <code>: <message>` or verbatim from a 422.

| Refusal (substring) | Origin | Cause | Clears it |
|---|---|---|---|
| `no current work selection — \`working-set select\` a scope first` | cli | a scope read or push with no selection | `working-set select <scope>` |
| `no current selection — nothing to route` | cli | no current piece is held (with several held, `process next` prints one block per piece instead) | `working-set select <scope>` |
| `you hold several current pieces (…): … — name one with --piece <id>` | cli | a scoped read or write other than `process next` — `process check`, `process reconcile`, `process advance` — under several held pieces, none named | `--piece <scope>` on the command |
| `push: re-stamped %d section(s) whose scope context moved` | cli | unchanged sections the server reported stale were re-put for a fresh stamp (not an error) | nothing |
| `no current selection named %s is held by you` | cli | `working-set pull selection --piece X` for a piece you do not hold; the snapshot is left unchanged | name a held piece |
| `no route derived — <reason>` | cli | `process next` on a live selection with no phase to route — `entry_origin_unavailable` when a member whose evidence is not current has no live entry origin, retired by a demotion or never entered (a scope whose evidence is all current routes to `complete` instead); not an error, and `-v` still prints the aggregate | `process reenter <member-id>` re-establishes that MEMBER's entry (fresh cold review + human approval) — the entry origin is the member's fact, so naming the scope resolves a different gate; `process check --phase build` names the members. For any other reason nothing — read the aggregate from it or from `process check --phase completion -v` |
| `you hold several current selections (…) — name one with ?scope=<id>` | server | an unscoped scope read or write under several pieces | `--piece <scope>` on the command; `process advance` and `process reenter` on an SR that is itself one of the held pieces resolve it without one |
| `is already held by` | server | a take of a piece someone else holds | take a different piece, or wait for a put-down |
| `closing … requires stating how it ended` | server | a put-down or displacement with no outcome | add `--outcome done\|obsolete\|returned=<phase>` |
| `you already hold … \`replaces\` displaces a piece on a fresh take` | server | `--replaces` on a piece you hold | advance in place; drop `--replaces` |
| `suspending requires a reason` | server | `--suspend` without `--reason` | add `--reason` |
| `DERIVED requirement candidates cannot be selected as governed work` | server | selecting a candidate | confirm it first (`rdd-discover`) |
| `is a --for-review directory … review pulls are read-only and never push` | cli | a push from a legacy review-stamped authoring directory | preserve the directory by manual rename, then pull fresh authoring files |
| `a cold-review trace needs an explicit review context selector` | cli | `author trace --purpose cold-review` without explicit snapshot selection | `working-set pull --scope --for-review`, then record with the printed `--review-context <id>` |
| `could not determine the packet aggregate fingerprint` | cli | a finding without a resolvable aggregate | pass `--aggregate <full aggregate>` |
| `--expected-fingerprint is required — a finding disposition is fingerprint-guarded` | cli | a disposition with neither the guard nor the finding's scope | pass `--scope <kind>:<external-id> --from <disposition you saw>` and the CLI reads the fingerprint, or `--expected-fingerprint` from `process findings list` |
| `--from is required with --scope` | cli | `process findings disposition --scope` without the disposition you saw; nothing was written | add `--from OPEN` (or the disposition you read) |
| `someone changed it after you read it` | cli | `process findings disposition --scope --from` on a finding no longer in `--from`; nothing was written | read `process findings list` again and decide |
| `has unknown key(s)` | cli | a findings, dispositions, review or lane authorization file with a key the shape does not know (a misspelling); every one is named and nothing was written | correct the named keys |
| `problem(s) in the plan; nothing was written` | cli | `author apply` where a record is invalid, would change with neither a pinned `expected_fingerprint` nor a `working-set pull`, or moved since that fingerprint; each is named | fix the named record; for a guard, `working-set pull <id>` (keep any other change in the plan) or pin its `expected_fingerprint`, and run it again |
| `conflicted — the store moved since the fingerprint the plan guarded on` | cli | `author apply` whose patch the store refused as stale between the check and the write | `working-set pull <id>`, keep the other change in the plan, run it again |
| `its current disposition is …, the file expects …` | cli | a `process findings disposition --file` entry whose finding is no longer in its `from` disposition; the rest still run | re-read `process findings list` and correct `from` |
| `carries no review-mode stamp — pull it with` | cli | `process review record` on a scope not pulled `--for-review`; nothing was written | `working-set pull --scope --for-review`, review, then record from that pull |
| `moved since the review pull (stamped …, now …)` | cli | `process review record` after the packet aggregate changed; nothing was written | re-pull `--for-review` and review again |
| `the verdict is PASS, but material finding(s) would stay OPEN or DEFERRED on …` | cli | a PASS review file that leaves a material finding open; nothing was written | resolve or reject it in the file's `dispositions`, or record the verdict as FAIL |
| `the run stopped there.` | cli | `author apply` where the store refused a write (not a 409) after the plan checked; the refusal, the records written, any created but not patched, and those not written are named | `working-set pull <id>` for each record created but not patched, fix the refused record, then apply the plan again — written records read `unchanged` |
| `already exist — created since the plan was read` | cli | `author apply` whose create the store answered 409; the rest continued | `working-set pull <id>`, then apply the plan again |
| `which the store does not know — push never creates a record` | cli | `working-set push` with an item file for an id the store does not know; nothing was written | create it with `author apply --file <plan>`, then pull and push |
| `runs failed; the others were recorded` | cli | `factory evidence --file` with a refused run | run the same file again; each run id derives from its entry |
| `system requirements not advanced (…)` | cli | `process advance --all` where an SR was refused; the reason for each is printed | the fact each reason names (a RED, a passing run), then run it again |
| `--all needs --piece <EPIC>` | cli | `process advance --all` without the piece | add `--piece <EPIC>` |
| `carries no FROM->TO transition — pass --to and --expected with the record id` | cli | `author advance --gate` on a gate that holds no transition | name the record with `--to` and `--expected` |
| `did not advance — ` | cli | `author advance --gate` where a member the gate names is neither in the FROM state nor already past it (or is missing); the members that could move did, the epic did not | move the named members to the FROM state, then run the same command again |
| `packet sections missing or stale for …` | cli | `process enter` on sections still missing after it re-stamped the unchanged ones | author them and `working-set push` |
| `a … finding is immutable except its reference — a reopen or content edit is refused` | server | editing a settled finding | record a new finding |
| `--resolution is required on RESOLVED` / `resolution_kind: is required on a RESOLVED disposition` | cli / server | a `RESOLVED` without its kind | `--resolution packet-edit\|scope\|decision`; `scope` needs `--ref <record>`, `decision` needs `--ref USER:…` |
| `--disposition is required … or --ref alone to change the reference of a finding already RESOLVED` | cli | neither a disposition nor a reference | name the disposition, or `--ref` alone on a settled finding |
| `changed since … was raised — a packet edit does not change a member` | server | a `packet_edit` resolution after a scoped member's content moved | resolve it as `scope` or `decision`, or `--widens USER:<date>:<why>` |
| `--widens names the human who accepted the widening …` | cli | a `--widens` without `USER:` (a `--widens` with a resolution other than `packet-edit` is refused the same way) | `--widens USER:<date>:<why>` with `--resolution packet-edit` |
| `introduced_by: names no finding on …` / `names the finding itself` | server | `--introduced-by` naming a finding of another scope, an unrecorded one, or the finding itself | name an earlier finding recorded on the same scope |
| `the server does not record resolution kinds …` | cli | a kind posted to a server that does not advertise `finding_resolution` (a 404 contract read reads the same) | deploy the server; a failed contract read names its status instead |
| `category: is invalid` / `map[category:[is invalid]]` | server | `process findings add --category` outside the server vocabulary; the refusal does not list it | one of `correctness`, `security`, `data_loss`, `contract`, `traceability`, `testability`, `feasibility`, `scope`, `other` — the first six are material |
| `prerequisite_gate_external_ids is empty; record the cold-review trace at the packet aggregate` | server | an entry gate born with no prerequisite | record the trace, then `author gate --prerequisite <trace>` |
| `names prerequisite_gate_external_ids that do not exist` | server | a misspelled or unrecorded prerequisite | check the trace id |
| `not a trace, not pass, or pinned elsewhere` | server | a prerequisite that is not a passing trace at this aggregate | re-record the trace at the full aggregate |
| `one of its named passing prerequisites is a cold-review trace … name none` | server | an entry gate naming only non-cold-review traces | name the cold-review trace too |
| `a completion gate may open only when every named item has current passing evidence and is IN_REVIEW or DONE — not yet` | server | a completion gate while a named item lacks a posted passing run or is not yet `IN_REVIEW`; the epic and the UR are named items | `factory evidence --pass` the item (the epic and `UR-<epic>` too), reconcile |
| `a completion gate may open only after a passing completion trace at the packet aggregate` | server | a completion gate whose named prerequisites do not pass there; the message names each failing id and why | `author trace --purpose completion --fingerprint <full aggregate>`, then name it |
| `a governed decision may open only when every named prerequisite is a trace gate passing at the packet aggregate` | server | a named prerequisite does not exist, is not a trace, is not pass, or is pinned elsewhere; each is listed | re-record the trace at the full aggregate |
| `gate names … but omits members not yet entered` | server | an epic entry gate that leaves a member behind | add `--scope <member>` for every member not yet `TODO` |
| `exact_scope: completion gate names … but omits members not yet DONE` | server | an epic completion gate that leaves a member behind; the UR counts as a member and `working-set pull <epic>` does not list it | add `--scope <member>` for every SR still `IN_REVIEW` and `--scope UR-<…>` while the UR is |
| `belong to …, which is … — name the epic so the … gate moves it too` | server | a members-only entry gate while the epic itself is still `PROPOSED` | name the epic in `--scope` |
| `a members-only … gate recovers members of an epic already entered` | server | a members-only entry gate on a retired, postponed, done or working epic | recover through the epic's own lifecycle instead |
| `no such gate … to supersede` / `only a human decision can be superseded` / `only an open or answered gate can be superseded` | server | `author gate --supersedes` naming a missing gate, a trace, or a closed gate | check the predecessor id and state with `factory gates <id>` |
| `answer has already entered application … can no longer be superseded` | server | superseding a decision whose answer is being applied | open a new decision instead |
| `approval must name a single piece of work` | server | a gate scope of several items and no epic | scope to the epic plus members, or one item |
| `the gate's scope does not name` | server | an advance on an item the gate did not name | re-open the gate with the item in `--scope` |
| `a … gate is the decision itself — it must be born with the author's brief` | server | a governed gate without a brief | add `--brief-file` or the `--brief-*` flags |
| `gate_prerequisites_stale` / `Planning content or approval prerequisites changed` | server | the packet moved after the gate opened | re-review at the new aggregate; a successor gate |
| `The prerequisite checks must all pass at the current revision before approval` | server | answering a gate whose trace is missing or stale | fix the prerequisite; the gate stays open |
| `requires an attributable USER: decision reference` | server | `DEFERRED` or a governed transition without a source | `--decision USER:<date>:<why>` |
| `a decision reference must be at most 255 characters` | server | an over-long source tag | shorten it |
| `should be at most 255 character(s)` | server | a bounded authoring field — title, context code, source tag, decision ref, a finding or gate id, an option key — over 255 characters; the refusal names the field and nothing is written | shorten the named field; long prose goes in `--body` or `--detail` |
| `expected key=label — the option key is what an approving answer carries` | cli | a malformed `--option` | `--option approve="Approve entry"` |
| `already answered (first-wins)` | cli | a second answer on an answered gate | read it with `factory gates <id>`; open a successor if the decision changed |
| `phase … is invalid` / `state … is invalid` | server | a `--phase` outside the eight names, or a selection state the server does not know | use `plan`, `cold_review`, `entry`, `build`, `verify`, `completion` |
| `no system_id in … config.json` | cli | the command ran from a directory carrying its own `.modernpath/config.json` (`modernpath-core/` has one), resolving that binding instead of the workspace's — and a nested config *with* a system_id resolves silently to the wrong store | run every verb from the workspace root |
| `not connected — run 'modernpath factory connect --system <id>'` | cli | no binding | `factory connect --system <id>` (a human decision) |
| `server rejected the session token — expired, revoked, or issued for a different server` | cli | the session token lapsed | a human runs `modernpath auth`; the agent stops |
| `is not satisfied — see the FAIL checks above` | cli | `process check` found an unmet check | the named check says what to record |
| `reconcile reported … unmet transition(s)` | cli | proofs do not yet allow a transition | read the FAIL line; usually a missing trace or evidence |
| `a gate is opened once` | server | re-creating a gate id, including a withdrawn one | a new id; withdrawn ids stay reserved (`process enter/complete --gate-id <id>-R2`) |
| `this server serves no delivery facts` | cli | a ceremony verb against a server that predates the `facts` read | deploy the server; nothing was written |
| `no RED is recorded for` | cli | `process advance` before the red-first result | `factory evidence --fail <SR> --role RED` (at the RED commit, or `--revision`) |
| `the evidence for … reads failing, not passing` (or claimed, stale) | cli | `process advance` before the passing run, or after a drift | `factory evidence --pass <SR>` at the current revision |
| `has no live entry of its own at the current packet aggregate` | cli | `process advance` on a TODO or READY member the server serves `entry_current: false` while the epic's entry gate is current — reconcile would not move it; nothing was written | read `process reconcile --piece <piece>` (dry run) for what the store holds about that member's entry, then act on what it states |
| `a demotion carries the human's reason as a USER: source` | cli | `author demote` without `--reason USER:…` | pass the reason as `USER:<date>:<why>` |
| `only delivered work is demoted` | cli | `author demote` on an item that is not `IN_REVIEW` or `DONE` | nothing — the item is not delivered; use the ordinary loop |
| `is open — answer it first` | cli | `author demote --apply` before the human answered | answer the gate, then `--apply` |
| `a batch reopen holds one starting state` | cli | `author demote --ids` over IN_REVIEW and DONE items; nothing was opened | run the named groups as separate batches, the group holding user requirements first |
| `was refused: … — stopped; applied: …; remaining: …` | cli | `author demote --gate-id <gate> --apply` met a refusal on one item | fix the cause, then re-run the same command; it resumes with what remains |
| `cannot close: … was moved by another gate` | cli | an item of the gate was already moved by another gate's follow and was passed over | `author gate-withdraw <gate> --reason "USER:<date>: …"` |
| `--since is refused with a by-id pull` | cli | `working-set pull <SR> --for-review --since …` | the narrow review has no later rounds; pull it without `--since` |
| `carries no reviewed fingerprints` | cli | `working-set pull --scope --for-review --since` on a trace recorded without them | pull without `--since` for one full round; later rounds can then use it |
| `is not a cold review (purpose` | cli | `--since` names a trace of another purpose | name this scope's previous cold-review trace |
| `--since takes a previous cold review of this scope` | cli | `--since` names another scope's cold review | name this scope's previous cold-review trace |
| `cannot enter through a members-only gate; demote or reopen the epic first` | cli | `process enter` on a DONE or OBSOLETE epic with PENDING_VERIFICATION members | demote or reopen the epic, then enter its as-built members |
| `is already open — answer it` | cli | `process enter`/`complete` find the primary id (or one in its -R series) open; `process reenter` finds its id open | answer it in Mission Control or with `factory answer` |
| `is already answered — apply it` | cli | the same, answered | `author advance … --gate <id>` members first |
| `behind the delivered tip origin/` | cli | `process complete` on an ancestor of the merged tip (a session that never pulled) | pull the merged tip |
| `is not on the delivered branch origin/` | cli | `process complete` on a branch commit | merge, then check out the merged revision |
| `could not fetch origin/` | cli | `process complete` or `process enter` offline or without a reachable remote | restore the remote; `--no-fetch` is for offline fixtures only |
| `the selection records no reconnaissance revision` | cli | `process enter` on a selection taken without `--recon-revision` | `working-set select <scope> --recon-revision <sha>` (the revision the packet was reconnoitred at), then enter again |
| `is stale: origin/` | cli | `process enter` when the default branch moved past the reconnaissance revision and a cited path changed | re-reconnoitre and re-select with the new revision; or, on the human's word, `--allow-drift USER:<date>:<why>` |
| `is not present in the repository` | cli | `process enter` with a reconnaissance revision the local repository does not hold | fetch it, or re-select with a revision the repository holds |
| `--allow-drift accepts reconnaissance drift on the human's word` | cli | `process enter --allow-drift` with a value that is not a `USER:` source | pass `USER:<date>:<why>` |
| `has members not yet IN_REVIEW` | cli | `process complete` before every member advanced | `process advance <SR>` each |
| `--fingerprint must be a full 64-character hash` | cli | a display prefix on `author trace` | omit `--fingerprint` (the store's value is read) or pass the full hash from `process next -v` / `working-set pull <SR>` |
| `but the value given is the …` (`--purpose lower pins to the content hash, but the value given is the packet aggregate`) | cli | the wrong pin class on `author trace` | omit `--fingerprint`, or pass the class the purpose takes |
| `--transition must be FROM->TO` | cli | a shell fragment of an unquoted arrow | `--from`/`--to`, or quote the arrow |
| `must be a full 64-character hash for a … trace` / `must be FROM->TO` | server | the same shapes reaching the authored path from an older CLI | upgrade the CLI, or pass the full value / the pair |
| `still a prerequisite of` | server | withdrawing a gate another gate names | withdraw the dependent first |
| `pin_required` | server | `factory release activate` (and the other release lifecycle transitions) without `--pin`, or with a wrong one | the release PIN of the signed-in person is required; it is set in Mission Control — pass it with `--pin`; `pin_locked` says repeated failures locked it for a while — retry later |
| `does not exist` (`gate <id> does not exist`) | cli | `factory gates <id>` on an id the store does not hold — a mistyped id, or a gate the store never wrote (the active release's `GATE-RELEASE-<slug>` when the release was activated from another system, or before the activation wrote it) | check the id in `your-move --queue` or the epic pull; for the release, `factory status` and `SELECTION.md` name the gate or say `no recorded selection gate on this system` — run `factory release activate <slug> --source USER:…` here to record one |
| `not served by … (unknown external id)` | cli | `working-set pull <id>` on an id the read surface does not hold | the same checks; an applied gate a pull cannot resolve is a known read gap, not a missing decision |
| `transition is required for purpose …: give --from and --to` | cli | `author trace` with a purpose outside the inferring set | add `--from <state> --to <state>`; only cold-review, entry, lower, upper and completion infer their transition |
| `criteria: every stated criterion needs an external_id` | server | `author update --criteria` with an object lacking `external_id` | give every object `external_id`, `given`, `when`, `then` |
| `a by-id review pull takes a system requirement` | cli | `working-set pull <id> --for-review` on an epic or a user requirement | review an epic with `working-set pull --scope --for-review` |
| `has no lane_class — set it with` | cli | `process lane review` or `enter` on an SR without a lane class | `author update <SR> --lane-class <class>`, then pull and review again (the review pins the class) |
| `changed since the review pull` | cli | `process lane review` after the SR or its sections moved | `working-set pull <SR> --for-review` and review again |
| `has no passing narrow review` | cli | `process lane enter` without a `LANE:narrow` PASS at the SR's current aggregate | `working-set pull <SR> --for-review`, then `process lane review <SR> --file review.json` |
| `there is no answered lane authorization on this System` | cli | `process lane enter` before a workspace admin answered an authorization | `process lane authorize`; the admin answers it in the web app or with `process lane approve <gate>` |
| `process lane approve answers only a lane authorization` | cli | `process lane approve` on a gate of another purpose | answer that gate with `factory answer` |
| `is not reachable from the default branch` | cli | `process lane check --commit` on a commit not yet delivered | merge it and fetch, then name the delivering commit |
| `is not an ancestor of … — a rebase delivery names the commit its range starts from` | cli | `process lane check --base` naming a commit after, or beside, the tip | the base the rebased range starts from |
| `is not eligible for the lane` | cli | the server's eligibility verdict is FAIL (the verdict is recorded) | nothing in the lane: the change leaves it for single-SR scope with its full packet and review |
| `there is no answered lane-batch gate to apply` | cli | `process lane complete --apply` before the answer | answer the lane-batch gate in the web app or with `factory answer <gate> --options approve[,reject:<SR>…]` |
| `is not the System's current lane authorization` | server | a stale `lane_ref` | the refusal names the current one; rerun `process lane enter` |
| `the signed-in caller is not an applier of` | server | the person signed in is not named by the authorization | an applier runs it, or an admin answers a new authorization |
| `the daily cap of` | server | the System's lane applications for the UTC day reached the cap | wait for the next UTC day, or use single-SR scope |
| `` a lane authorization is answered from the developer CLI only with `process lane approve` `` | server | `factory answer` (the generic gate answer route) on a lane authorization with a CLI sign-in | `process lane approve <gate>` as a workspace admin, or the web app |
| `` a lane authorization is answered only from the web app or with the developer CLI's `process lane approve` `` | server | an answer with a token of any other client (API, integration, MCP) | sign in with the CLI (`modernpath auth`) or use the web app |
| `a lane authorization is answered only by a workspace admin or a platform superuser` | server | `process lane approve` (or the web answer) by a member | a workspace admin answers it |
| `a lane-batch gate may open only when every member can complete in the lane` | server | a member not lane-entered, not IN_REVIEW, without evidence or an eligibility PASS at its delivered revision | the causes are listed; fix each, then `process lane complete` again |
| `was rejected in` | server | advancing a member the lane-batch answer rejected | fix it and include it in a new batch |
| `entered through the small-change lane — it completes through a lane-batch gate` | server | `process complete` on a lane SR | `process lane complete` |
| `source_not_authorized` (from `reverse-engineer publish`) | server | a source citation was refused; the refusal names its `cause`, the `requirement` and the `citation_index` (its zero-based position in that requirement's `source_citations`), or the `source_file_id` for a deleted test record. The first failing citation refuses the whole group and nothing of the group is recorded | the check for that cause under "Refusals in a sweep" in the onboarding section; fix the citation and publish the same group key again. The same refusal a second time is a stop |
| `source_not_authorized` (from `reverse-engineer refresh-traces`) | server | the word comes bare: a stored code or test citation of a named requirement has no valid source file id, cites a document, or fails the publish checks against the capture of the run given to `--run`; nothing was recorded | "Refusals in a sweep" in the onboarding section; correct the citation, then refresh again |
| `stale_corpus` (from `reverse-engineer refresh-traces`) | server | the input's `corpus_fingerprint` is not the current one: requirements, links or lifecycles changed after the preflight it came from; nothing was recorded | `reverse-engineer preflight` again and its `data.corpus_fingerprint` in the input, with the same run and the same group key |
| `stale_corpus` (from `reverse-engineer authorize`) | server | the requirements changed after the saved preflight; nothing was recorded. `reverse-engineer publish` never answers `stale_corpus` | onboarding step 4 |
| `existing_requirement_conflict` / `parent_not_found` / `parent_not_governed` (from `reverse-engineer publish`) | server | a record the group names collides: an `external_id` the group creates already exists in the system; a reuse entry names a requirement whose fingerprint moved from its `reuse_fingerprint`, or one that is missing, deleted, DERIVED or OBSOLETE; a parent is missing or deleted, or is not governed (DERIVED or OBSOLETE under a confirmed system requirement). Publish checks nothing of the corpus outside the records the group names, and nothing of the group is recorded | when the record is the run's own — a parent in a group not yet published, a reuse entry of a requirement you corrected — publish the parent first or rebuild the entry from the current fingerprint. Otherwise the record was created or changed outside the run and the run is no longer current (stop list item 5): stop and show the person the refusal. Sending the same group again does not help, and another run does not clear it; do not authorize one without the person |
| `invalid_trace_refresh` | server | the `reverse-engineer refresh-traces` input was refused before anything was read; the refusal names the `rule` that failed and, for an entry, its `entry_index` (zero-based). Rules in order: `group_key`, `top_level_keys`, `corpus_fingerprint`, `requirement_count`, `entry_keys`, `kind`, `external_id`, `expected_fingerprint`, `duplicate_external_id` | send the accepted input: a group key of 1 to 255 bytes; top-level keys exactly `corpus_fingerprint` and `requirements`; 1 to 500 entries, each exactly `kind: "system"`, `external_id` and a nonempty `expected_fingerprint`; no `external_id` twice |

## Reverse-engineering onboarding (store-backed)

Use this sequence with `rdd-reverse-engineer`; these operations are separate
from delivery entry/completion gates. Run them only in the main session.

Before resuming, use `modernpath reverse-engineer status --run ID` to recover
stored authorization, source identities and group receipts. Reuse unchanged
inputs/keys and reconcile acknowledged groups with the unpublished remainder;
do not repeat mode approval or recapture completed sources. Resume incomplete
captures as needed. A conflict requires reconciliation, not another key.
`modernpath reverse-engineer runs --all` lists the system's runs with their
authorization sources when a run id is not at hand. Read the run mode as
"Run mode" under "Running a sweep" says, and continue the confirmed contexts
and the open questions from the sweep's working folder; do not ask for any of
them again. New runs follow the sequence below.

1. `modernpath factory status` verifies the authenticated system binding.
   `modernpath process prepare-inputs` reports local/server document freshness
   without refreshing. Search/read fresh local documents first; use
   `modernpath docs sync` only for missing or stale exports. Use live search/read
   for material missing locally or when a current authoritative answer is needed.

   Before the first inventory of a Git repository, prepare the repository:
   - Commit the files that `modernpath install` created or changed; `git
     status` lists them (`.claude/`, `.modernpath/rdd/`, `.gitignore`,
     `AGENTS.md`, `CLAUDE.md` and others). The person commits them, or you do
     after the person agreed to a commit in their repository. The reason: an
     inventory of a dirty repository marks the whole capture dirty, and a run
     captured dirty cannot be accepted as built, whatever is committed later,
     without a new authorized run with its own capture. As-built acceptance
     also needs the tested commit to be the tip of the remote default branch
     (verification step 3 below).
   - Put untracked files that are not source, such as build output and editor
     files, into the repository's local exclude file (`git rev-parse
     --git-path info/exclude` names it). Ignored files are left out of the
     inventory and disclosed as exclusions. Tracked files are listed whatever
     the ignore rules say, except symbolic links, private paths and files under
     `.claude`, which are left out and disclosed.
   - Create the working folder and the run's sub-folder (below). From the
     first inventory until acceptance the repository stays clean and at one
     commit: commit nothing during a sweep ("Working rules" under "Running a
     sweep").

   The working folder holds every file a sweep saves:
   `.modernpath/reverse-engineering.runs/` in the workspace root, where the
   `modernpath` commands run. Its name has a dot, which no system slug can
   take, so the document export never replaces it. Directly in it are the
   files of the whole sweep: `confirmed-contexts.json`, `open-questions.md`
   and `run-mode-changes.json`. Each run has its own sub-folder,
   `.modernpath/reverse-engineering.runs/<folder>/`, for its inventory,
   preflight, run, capture, group, receipt, refresh and coverage files. The
   input files of its verification and of candidate decisions go there too
   (execution, delivery, proof, acceptance, apply, selection and decision
   files), because `delivery-proof` refuses a dirty repository. Choose
   `<folder>` before the inventory, from letters, digits and hyphens, and
   propose the same text as the run name in step 4; if the person changes the
   run name, the folder keeps its name. A redirect does not create a folder,
   so create it first: `mkdir -p .modernpath/reverse-engineering.runs/<folder>`,
   or in PowerShell `New-Item -ItemType Directory -Force
   .modernpath\reverse-engineering.runs\<folder>`. Do not save these files in
   the repository root or in a temp folder: in the root, a file that is not
   ignored makes the repository dirty and is listed by the inventory that
   writes it; a temp folder can be emptied while it holds the only copy of an
   unpublished group. The folder is ignored by the rule `/.modernpath/*` that
   `modernpath install` writes into `.gitignore`. In a repository without that
   rule, for example one connected but not installed, add
   `/.modernpath/reverse-engineering.runs/` to the local exclude file first;
   otherwise the saved files make the repository dirty.
2. `modernpath reverse-engineer inventory --repository key=/absolute/root`
   (repeat the `--repository` flag in one command for each repository;
   one run has one inventory file) emits frozen paths, hashes, sizes, revision,
   dirty state and exclusions. Non-Git directories and Git worktrees are supported.
   A tracked symbolic link in a Git repository is left out and listed as an
   exclusion; it is never followed.
   For part of a large Git repository, add `--path key=relative/path`
   (repeatable) for the directories or files to include. The inventory then
   holds only those files; the revision and dirty state stay the whole
   repository's, the 32,000,000-byte and file limits count the included files,
   and every file left out is an exclusion with the reason `outside the
   authorized scope`; a directory left out whole is one `subtree` entry, and
   Git-ignored files under it are not listed again. A path is a literal name —
   no patterns, no trailing slash — and a non-Git root cannot be scoped. Name
   the tests with the code they cover: one proof takes one delivery report per
   repository, and every cited code and test file must come from the same
   capture. A path is only a set of files; which domain or capability it holds
   is for the person naming it to decide. A subfolder given as
   the repository root is not a substitute: it inventories as unversioned and
   dirty, so its requirements cannot be accepted as built.
   To inventory again the area an earlier run covered, give `--like-run <run
   id>` instead of `--path`: the error stream names every file that changed,
   is missing or is new since that run.
   Save the output unchanged when you run it: add
   `> .modernpath/reverse-engineering.runs/<folder>/inventory.json` to the
   command. A large inventory does not belong in your context; read from the
   file only the totals (`file_count`, `byte_count`) and, for each entry of
   `repositories`, its `key`, `revision`, `dirty` and how many `files` and
   `exclusions` it has.
   The saved file must hold the bytes the command printed. In bash, zsh and
   `cmd` the redirect does that. In PowerShell it does not: PowerShell's own
   redirect re-encodes the output. In PowerShell, let `cmd` save
   `inventory.json` and `preflight.json`, for example
   `cmd /c "modernpath reverse-engineer preflight > .modernpath\reverse-engineering.runs\<folder>\preflight.json"`.
   If step 4 refuses a saved file as invalid JSON, or because its snapshot
   digest does not match, the file was re-encoded when it was saved: save it
   again this way and never edit it. `run.json` in step 4 is not read back by
   the CLI and needs no such care.
3. `modernpath reverse-engineer preflight >
   .modernpath/reverse-engineering.runs/<folder>/preflight.json` saves, under
   `data`, the existing-corpus fingerprint, the requirement count, the
   recommended mode and the document descriptors. The recommendation is not
   authorization: the person chooses **baseline** or **derived** in the one
   question of step 4. Preflight recommends derived whenever the system has a
   requirement, so in a planned area-by-area baseline it recommends derived
   for every run after the first. Show the recommendation with that
   explanation; the person still chooses baseline or derived.
4. Authorize from the two saved files. Do not write a JSON file, and
   do not run `inventory` or `preflight` again, except after a refusal as
   described below: the saved files are what the person approves. After the
   person has answered:
   `modernpath reverse-engineer authorize --inventory
   .modernpath/reverse-engineering.runs/<folder>/inventory.json --preflight
   .modernpath/reverse-engineering.runs/<folder>/preflight.json --mode baseline
   --source "USER:<date>:<name> approved <mode>, documents <all|none>, run mode <autonomous|confirm>"
   --key <run name> --documents all
   > .modernpath/reverse-engineering.runs/<folder>/run.json`.
   Every flag is required and none has a default. The mode, the document
   choice and the run mode are the person's answers. You propose the run name
   for `--key` — short and stable, for example `billing-baseline`, the same
   text as the run's sub-folder — and you write `--source` from the answer, in
   at most 255 bytes, ending with one of the two fixed wordings of the run
   mode: `run mode autonomous` or `run mode confirm`. `<name>` is the person
   who answered; ask for it in the same question when you do not know it. The
   response is the whole run, with every file and every attached document, so
   it goes to `run.json`: read only `data.id` from that file for step 5.
   Actor, system, Base and process revision are server-owned.

   **Before authorizing, show the person** what they approve, read from the
   two files, in plain words and in one message:
   - the source: each repository with its commit and whether it is clean
     (`repositories[].revision`, `repositories[].dirty`), how many files and
     megabytes are included (`file_count`, `byte_count`), and how many
     entries are left out (`repositories[].exclusions`; the field is absent
     when nothing is left out);
   - the mode and what it means: baseline publishes as-built requirements as
     "Baselined — not verified"; derived proposes candidates for later review.
     State how many requirements the system already has
     (`data.requirement_count`) and `data.recommended_mode` as a
     recommendation, never as the answer, with the explanation of step 3;
   - the analysis documents: how many the preflight lists (`data.documents`)
     and what attaching them does — the run's requirements can then cite
     them; without them requirements cite code and tests only. Recommend
     `--documents all` when `data.documents_truncated` is false. When it is
     true, the system has more documents than one run can attach and `all`
     cannot be authorized from the saved files: say so and offer `none` only.
     The person still answers;
   - the run mode and what each choice means: autonomous — you continue
     through capture, every publication group, coverage and the read-back
     without asking, and stop only for the stop list under "Running a sweep";
     confirm — you report after each publication group and wait for the
     person;
   - the run name. You propose the run name; the person can change it.
   Ask once: one question that covers the mode, the documents, the run name
   and the run mode. Then run the command with what the person said.

   **If the authorization is refused**, nothing was recorded, and the same run
   name can be used again. A refusal never lets you change the mode, the
   document choice, the run mode or the run name yourself. The two rules below
   are for these three server reasons only:
   - `stale_corpus` (the requirements changed) or `document_not_authorized`
     (a document changed): run `preflight` again and save it as
     `.modernpath/reverse-engineering.runs/<folder>/preflight-new.json`. Then
     compare the four fields of the two files:
     `data.requirement_count`, `data.recommended_mode`, the number of
     `data.documents` and `data.documents_truncated`. If nothing differs,
     authorize again with the same answers and `--preflight
     .modernpath/reverse-engineering.runs/<folder>/preflight-new.json`, and
     tell the person that you did. If something differs, show the difference
     and ask again before you authorize.
   - `document_snapshot_too_large` (too much document text for one run): stop
     and tell the person. The error and the help name `--documents none` as
     the next step; that is the mechanism, not permission. `--documents none`
     changes what they approved, so authorize with it only after they agree,
     and write the new choice into `--source`.
   If the same refusal comes a second time, or the refusal is any other
   reason — for example `idempotency_conflict`, which means the run name
   already belongs to a run with other content — stop and show the person the
   error. The same holds when the command itself refuses your flags.
   The source list in `inventory.json` is not part of these refusals and
   needs no new review.

   A file built another way can still be given with `authorize --file
   .modernpath/reverse-engineering.runs/<folder>/authorization.json`. Its
   fields are `key`, `mode`, `authorization_source`,
   `corpus_fingerprint`, `repositories` (from inventory) and `documents` (from
   preflight; each has `kind`, `id`, `version`, `fingerprint`). The two forms
   cannot be mixed, and the file form's errors carry no next step.
5. For each repository, `modernpath reverse-engineer capture-source --run ID
   --repository key --root /absolute/root`. Poll `source-status --capture ID`
   until ready, then save it once with
   `> .modernpath/reverse-engineering.runs/<folder>/capture-<key>.json` and
   take the immutable source-file IDs from that file. A requirement cites the
   source file ids of the capture of the run it is published to, never those
   of an earlier run's capture of the same files. Interrupted captures
   resume without provider OAuth or FileAnalysis. `read-source --source ID`
   returns exact captured bytes; `read-document --run ID --document ID` returns
   the authorized immutable document snapshot.
6. Prepare the complete requested requirement batch locally: exact UR/SR IDs,
   criteria/scenarios, citations, relationships, stable keys and fingerprints.
   Check duplicates and cross-context joins before the first publication.
   Read "Running a sweep" before the first publish. Before the run's first
   publish, confirm the bounded contexts new to the sweep ("Bounded
   contexts"). Find and cite the tests that cover each group's code, naming
   the executed test in `test_case_ref`. Compare the staged requirements with
   the existing ones by the files they cite: read the existing requirements
   that cite the same files (earlier receipts name their ids, and
   `working-set pull <id>` shows each one's citations), and reuse such a
   requirement or merge the staged one into it instead of publishing the same
   behavior under a new id.
   `modernpath reverse-engineer publish --run ID --group stable-key --file
   .modernpath/reverse-engineering.runs/<folder>/group-<stable-key>.json
   > .modernpath/reverse-engineering.runs/<folder>/receipt-<stable-key>.json`
   publishes a coherent graph atomically. Split only for supported limits or
   dependencies; publish referenced parents first. Retain each returned receipt,
   then perform one consolidated read-back/audit. `status --run ID` recovers
   durable group receipts after interruption; retry identical inputs/keys only
   when needed. Do not derive/publish/read back each row separately.
   A publish prints one warning line naming the requirements it created
   without a context and without a test citation; the receipt lists them under
   `data.result` as `requirements_without_context` and
   `requirements_without_test_citation`.
   List the requirements published without a test in the end report. A
   refused publish: the glossary rows for `source_not_authorized` and the
   collision refusals, and "Refusals in a sweep" below.
7. `modernpath reverse-engineer coverage --run ID >
   .modernpath/reverse-engineering.runs/<folder>/coverage.json` measures the
   frozen inventory against stored traces. It separates governed/candidate
   linkage, captures, assessments and exclusions. It is not execution
   evidence or behavior-class enumeration. Follow the publication skill's
   linked coverage reference for a separate behavior-denominator checker;
   `audit-citations.mjs --min=N` checks citation counts/validity, not
   percentage coverage. `modernpath coverage` does not measure retired local
   ledgers in a store-backed workspace. Read the actual Ledger and
   Requirements surfaces before claiming baseline-ready or candidate-ready.

   A run is finished when `data.assessments.unresolved_files` is 0 and every
   entry of `data.files` has `governed` or `candidate` true, or an
   `assessment` of `reviewed` or `unsupported`. No single total answers it: a
   linked file without an assessment counts as `unassessed`, and
   `data.coverage.unlinked_files` counts reviewed files too. Read the totals
   and the files that fail the condition from the saved file, not the whole
   list. An `unresolved` assessment says that work remains; it is never used
   to complete a count. Linked means a governed or candidate link from a
   system requirement: a file cited only by a user requirement is not linked.
   The report is for one run: a link counts for it only when it was made
   through a capture of the same repository key and file list, whichever run
   made it. A file with `governed` and `candidate` both false, although a
   requirement from an earlier run covers it, is assessed as `reviewed` with
   that requirement named in the reason. The finish condition is the
   completeness of the sweep over this run's files, not verification, and
   counts and percentages define no passing gate. `coverage --system` reads
   every run of the system at once, each file counted once; a run's finish
   condition is read from `coverage --run`.

### Running a sweep

A sweep is the whole effort over one or more runs, for example one run per
area of a large repository. A run is finished when the condition of step 7
holds.

#### Run mode

The person chooses the run mode of each run in the authorization question of
step 4, and you write it into `--source` as `run mode autonomous` or `run mode
confirm`.

- Autonomous run mode: continue through capture, every publication group,
  coverage and the read-back without asking, and stop only for the stop list
  below. The one planned question is the confirmation of bounded contexts new
  to the sweep, asked once per run before that run's first publish and not at
  all when the run adds none ("Bounded contexts"). Everything else goes on the
  open-questions list, which you report once, at the end.
- Confirm run mode: report after each publication group and wait for the
  person.

A later session reads the run mode from
`data.authorization.authorization_source` of `status --run ID` and does not
ask for it again. The stored source cannot be changed: the same run name with
another source is refused as `idempotency_conflict`. So when the person
changes the run mode, write the change to
`.modernpath/reverse-engineering.runs/run-mode-changes.json`: one entry per
run, keyed by the run id, holding the run mode the person gave. Write the
whole file under a new name and rename it over the old one, so that a crash
leaves it missing or unreadable, never short. A later session reads that file
before the stored source. A run is autonomous only when its stored source says
`run mode autonomous` and the change file is missing, or is readable and holds
no entry for that run that says `confirm`. In every other case work in the
confirm run mode: a source that says `run mode confirm`; a source without the
run-mode wording, such as one written through the file form or by hand; an
unreadable change file; or an entry for the run that says `confirm`. A change
to autonomous on a run authorized as confirm therefore holds for the current
session only.

#### Stop list

In the autonomous run mode, stop and ask the person only when:

1. a new run needs authorization;
2. a refusal has no next step in this skill, or the same refusal comes a
   second time;
3. the authorized source changed (`capture-source` refuses with `authorized
   source changed`);
4. a publish would change an existing requirement;
5. the run is no longer current: a publish is refused for a collision with a
   record created or changed outside the run (an `external_id` the group
   creates already exists, a reuse entry names a requirement whose
   fingerprint moved, or a parent is missing or not governed; the collision
   row of the refusal glossary);
6. a sign-in or a permission is refused.

Show the refusal as printed, with its cause from this skill. Everything else —
a question about intent, a file you cannot classify, a requirement that fits
no confirmed context — goes on the open-questions list,
`.modernpath/reverse-engineering.runs/open-questions.md`. Append each question
when it arises and mark it answered when the person answers. A resumed session
continues the list, and the end report is made from it: for each run, whether
it is finished, with the coverage counts; the groups held back; the
requirements published without a test or without a context; and the open
questions.

#### Bounded contexts

Each requirement a run creates carries a bounded context: a code in `context`
and a name in `context_name`. Heartbeat groups requirements by the code and
shows a requirement without one as Unclassified.

In each run, after the source is captured and the behavior derived and before
anything is published, show the person the contexts the run derived that are
not yet in `.modernpath/reverse-engineering.runs/confirmed-contexts.json`, a
code and a name each. Draw them from the behavior you derived; the analysis
subsystems are an input only, since a subsystem is not a bounded context. The
person confirms or changes them, in either run mode. This is a naming
confirmation of the list, asked once per run, not an approval of each
context's content. A run whose derived contexts are all on the list asks
nothing. Add the confirmed contexts to the file, a list of `{"context":
"<code>", "context_name": "<name>"}` entries. It sits above the run folders,
so every run of the sweep reads it before its first publish and asks only
about the contexts it adds. When the file is missing or unreadable, ask the
person to confirm the whole list again; never guess.

Set a confirmed context on every requirement you create, never one the person
has not confirmed. A reuse entry carries no context. For a requirement that
fits no confirmed context after the run's planned question: in the confirm run
mode, ask. In the autonomous run mode, do not publish the group that holds it,
nor any group whose requirements name a parent in a held group (publish would
refuse it as `parent_not_found`); put the question on the open-questions list,
continue with the other groups, report the run as unfinished, and publish the
held groups after the person has confirmed the context. A held group leaves
its files unassessed, so the next run waits for that answer.

#### What a publish checks

A publish compares only the records its group names. It is refused, and
nothing of the group is recorded, when an `external_id` the group creates
already exists in the system; when a reuse entry names a requirement that is
missing, deleted, DERIVED or OBSOLETE, or whose fingerprint is not its
`reuse_fingerprint`; or when a parent is missing or deleted, or is DERIVED or
OBSOLETE under a confirmed system requirement. Publish does not compare the
system's corpus with what the run last saw: another run's publish, an author
edit, a context code, a trace refresh, an applied gate answer, a candidate
decision or an acceptance does not stop a run. A group that holds only source
assessments is accepted at any time, as long as each assessed file is in the
run's inventory. `corpus_after` on a receipt and `data.corpus_fingerprint` of
preflight are records, not a check that publish makes: comparing them before
a publish says nothing about whether it will be accepted.

The corpus fingerprint is what `authorize` and `refresh-traces` compare. It
covers the system's requirements, criteria, trace links, test cases, epics
and memberships, so it changes with every group that creates a requirement,
a link or an epic (not a group of only source assessments or reuse entries),
an author edit, a context code, a `refresh-traces` call that creates a link
(also one made with the same run), a lifecycle change such as applying a gate
answer, a candidate decision and an acceptance. Answering a gate without
applying it does not change it. A preflight saved before such a change is
refused as `stale_corpus` by `authorize` (step 4) and by `refresh-traces`
(refusal glossary).

#### Working rules

- Runs publish one after another. Authorize the next run only after the
  previous one has published every group, its source assessments included,
  so that it meets the finish condition of step 7, or after the person
  decided to leave it unfinished. The next run reuses the earlier run's
  confirmed contexts and assesses the files its requirements cover, and every
  group that creates a requirement, a link or an epic changes the corpus
  fingerprint that the next authorization is checked against.
- Fold a correction to a requirement into its group before the group is
  published. A requirement already published is corrected with an author
  edit; that does not stop the run, but it changes the requirement's
  fingerprint, so rebuild any staged reuse entry or refresh input that names
  it.
- A requirement cites the source file ids of the capture of the run it is
  published to, never those of an earlier run's capture of the same files:
  publish refuses them as `capture_of_another_run`.
- A file of this run that the coverage output shows with `governed` and
  `candidate` both false, although a requirement from an earlier run covers
  it, is assessed as `reviewed` with that requirement named in the reason.
- Changes to published citations and `refresh-traces` belong to
  verification, after the run has published its last group: a refresh needs
  the corpus fingerprint of a current preflight, and every group that creates
  a requirement, a link or an epic changes it.
  Set contexts on requirements already published, with `author context
  --file`, after the run's last group and before verification records
  execution proof: it refuses a requirement with current execution proof,
  because a new context would void that proof.
- The repository stays clean and at one commit from the first inventory until
  acceptance: commit nothing in it during a sweep. A run captured dirty cannot
  be accepted as built without a new authorized run with its own capture. A
  later commit that reaches the default branch leaves a run captured before it
  acceptable as built only at that newer tip, through `delivery-proof --run`
  (verification step 3), and only when the commit changed none of the files
  the run authorized.
- The kit is not updated during a sweep. If the person decides to update it
  anyway, install it between runs and commit the result before the next
  inventory; never leave it uncommitted, because the next inventory would
  then mark its run dirty. A run captured before that commit is accepted as
  built only at the new tip as above, which fails when the update changed a
  file the run authorized, for example `AGENTS.md` in a run over the whole
  repository. Such a run can then be accepted as built only through a new
  authorized run with its own capture (`inventory --like-run <run id>`
  inventories the same area again), with the citations of its requirements
  moved to the new capture's file ids and a trace refresh.

### Refusals in a sweep

The refusal glossary has rows for `source_not_authorized`, `stale_corpus`,
the collision refusals and `invalid_trace_refresh`; this section gives the
causes of `source_not_authorized` and `document_not_authorized`, and step 4
the refusals of `authorize`. Any other refusal is stop-list item 2. A
refused publish or trace refresh records nothing, so the same group key can
be sent again once the cause is fixed; the same refusal a second time is a
stop (stop list).

`source_not_authorized` from `publish` names the `cause`, the `requirement`
and the `citation_index`, the zero-based position of the citation in that
requirement's `source_citations`. The first failing citation refuses the
whole group. `source-status --capture <id>` for this run's capture shows
`data.state`, `data.run_id` and `data.files[]` with each file's id,
repository key, revision, path and sha256. Find the entry of `data.files[]`
by the cited `source_file_id`, never by path: an earlier run's capture of the
same file has the same path, revision and hash under another id.

| `cause` | Meaning | Read that shows it |
|---|---|---|
| `no_source_citations` | the requirement has no `source_citations`; the refusal names no position | the requirement in the group file |
| `unresolved_not_authorized` | a `kind: "unresolved"` citation on a row that is not a candidate (a baseline row without `exception_reason`), or one whose repository key and path are not in the run's authorization, or whose reason is blank | the row's `exception_reason`; the path under `data.authorization.repositories[].files` in `run.json` |
| `invalid_citation` | the citation is not an object, or its `source_file_id` is missing or not a UUID | the citation in the group file |
| `not_from_ready_capture` | the id names no captured file of this system, or its source is no longer available: the repository was unlinked from the system, the capture is not ready, or its manifest is missing or revoked (a superseded manifest is still valid) | `read-source --source <id>` answers `not_found` or `source_unavailable`; `source-status --capture <id>` shows `data.state` |
| `capture_of_another_run` | the id belongs to a ready capture of another run, for example an earlier run's capture of the same file | the id is not among `data.files[].id` of this run's capture; cite the id of this run's entry for the same path |
| `unsupported_kind` | `kind` is not `code`, `test` or `document` | the citation in the group file |
| `source_identity_differs` | the repository key, revision, path or sha256 differs from the captured file with that id | the entry of `data.files[]` that has the cited id |
| `test_record_deleted` | the test record of a cited test file was deleted; the refusal names the `source_file_id` instead of a position | no read shows the deleted record: stop and show the person |

A citation with `system_doc_id` refuses as `document_not_authorized` instead,
when the run did not attach that document, its version or fingerprint
differ, or the citation's `kind` is not `document`; `run.json` lists the
run's documents under `data.authorization.document_snapshots`.

### Existing-proof verification and acceptance

Run `rdd-reverse-engineer-verify`, then `rdd-reverse-engineer-accept`. These are
separate from normal entry, RED/GREEN and completion. Publication alone supplies
no execution proof. A standalone SR needs no invented Epic or parent. A UR needs
its own UPPER proof and complete current proof for every required SR. Already
DONE dependencies remain proof inputs and are not transitioned again.

#### Resume before collecting proof or opening a decision

Reuse retained current source, execution, delivery and preview identities for
the exact snapshot. Recollect only missing, stale or invalid evidence. If an
acceptance gate exists or answering/applying was interrupted, read
`modernpath reverse-engineer acceptance-status --gate ASBUILT-…` first:

- Current applied receipt: report its lifecycle/compliance states and finish.
- Current approved gate without a receipt: apply the retained exact input.
- Current open gate: reuse its brief/scope and any exact human answer already
  provided in the session; do not ask for that decision again.
- Rejected gate: report rejection without promotion.
- Stale proof/pins: report historical receipts as historical and return to
  verification before fresh review; an old answer cannot approve changed proof.

If the opening response was lost before its gate ID was retained, retry the
identical `acceptance-open` input/key to recover the existing gate. Missing status
is a tooling gap, not proof that there is no gate. Do not preview already DONE
scope as fresh pending work. New verification/acceptance follows these steps.

#### Fresh verification and acceptance

1. Inspect the exact captured code and tests with `read-source`. Bind each
   actual test name to a registered TestCase or a captured test citation's
   `test_case_ref`; inspect the behavior asserted, not only names or counts.
   After changing captured citations on existing pending SR baselines, run
   `modernpath reverse-engineer preflight >
   .modernpath/reverse-engineering.runs/<folder>/preflight-refresh.json`, then
   `modernpath reverse-engineer refresh-traces --run CAPTURE-RUN --group stable-key
   --file .modernpath/reverse-engineering.runs/<folder>/refresh-<stable-key>.json`,
   in the capture run's sub-folder. A refusal: the glossary rows for
   `stale_corpus`, `invalid_trace_refresh` and `source_not_authorized` from
   `refresh-traces`. Input is exactly `corpus_fingerprint` from preflight and
   `requirements: [{kind: "system", external_id, expected_fingerprint}]`, using
   each requirement's current content fingerprint. The capture run must belong
   to the signed-in actor, be baseline-authorized and contain every persisted
   code/test citation selected. DERIVED grants refuse confirmed refresh links.
   This atomically creates/reuses confirmed immutable source/TestCase links;
   requirement content, criteria, lifecycle and baseline provenance stay intact.
   Existing historical links remain. Rejected, stale, deleted or differently
   governed pairs refuse rather than being revived. UR upper proof uses captured
   citations directly and needs no invented SR links. Retain the returned receipt;
   `status --run CAPTURE-RUN` includes `trace_refreshes` for recovery. Identical
   inputs/key recover the receipt even after acceptance; changed input conflicts.
   A new refresh needs current graph/content fingerprints. Refresh grants neither
   test PASS nor acceptance. `publish` reuse does not refresh existing links.
2. Run the existing tests or inspect genuine retained execution reports.
   Put any output the test run writes into the repository and Git does not
   ignore, such as reports or coverage files, into the local exclude file
   before step 3: `delivery-proof` refuses a dirty repository. Save the input
   files of this step and the next ones in the capture run's sub-folder.
   `modernpath reverse-engineer execution-proof --file
   .modernpath/reverse-engineering.runs/<folder>/execution.json` records
   through the existing evidence channel and returns durable run/result IDs
   and the exact report digest. It does not execute tests. Input is `key`,
   `kind`, full tested `sha`, actual `ran_at`, exact JSON-string `raw_evidence`
   and `results`. `kind` is `local_test`, `ci`, `browser_verification` or
   `compliance_test_run`. Reusing a key replays the retained execution; changed
   reports or results require a new key. Each result includes `target_external_id`,
   `target_type: "requirement"`, `target_clause`, exact `test_case_ref`,
   `role: "LOWER" | "UPPER"`, `result`, `content_fingerprint`, full `revision`,
   and `detail: {assertion, production_subject}`. The report enumerates
   `executed_tests: [{test_case_ref, result}]`, `command`, `environment` and,
   for CI, `ci: {provider, repository, run, job, attempt, tested_commit}`.
3. For every repository, `modernpath reverse-engineer delivery-proof --file
   .modernpath/reverse-engineering.runs/<folder>/delivery.json` takes `key`,
   `repository_key`, local `root`, exact `tested_revision` and captured
   `snapshot_digest`. It checks the clean
   snapshot before/after fetching the advertised remote default branch and
   retains a distinct integration report in the evidence store. The tested
   revision must be the fetched tip; a failed fetch or non-tip revision refuses.
   The observed `origin` comes from the checkout's Git configuration, including
   local-file remotes. The server does not bind it to a registered provider
   repository. Verify and disclose that origin against the intended repository
   before using the observation as delivery proof; changing origin can change
   what this check proves. Local root is not sent. Retain the returned report/digest. An identical
   retry preserves the original observation and digest; changed intent under
   the same key conflicts. Reuse current retained observations when available.
   This makes no deployment claim.
   Add `--run CAPTURE-RUN` for a run inventoried with `--path`, for a run
   whose capture holds files under `.claude`, which a new inventory leaves
   out, and for acceptance at a default-branch tip past the commit the run
   captured: the command reads that run's authorization first and takes its
   snapshot over exactly the files authorized for the repository, so the
   digest equals the captured one. The input JSON is unchanged and the
   repository must still be clean as a whole. `snapshot_digest` must be the
   digest that run captured: any other digest, even that of the current
   files, refuses and records nothing. With `--run` the tip may be past the
   commit the run captured when the authorized files at the tip are the
   captured ones; the tested revision is still the tip. The report then also
   carries `authorization_run_id` and `measured_files`, and at a newer tip
   the captured revision and whether it is an ancestor of the tip; that is
   shown, not required, since a squash merge never keeps the captured commit.
   A run that cannot be read, or that does not authorize the repository key,
   refuses before any observation. Without `--run` a scoped run's digest
   never matches the whole repository.
4. `modernpath reverse-engineer proof-preview --file
   .modernpath/reverse-engineering.runs/<folder>/proof.json` evaluates
   without record or lifecycle writes. Input has `version: 1`, `requirements`
   and `delivery`. Each requirement has `kind: "SR" | "UR"`, `external_id`,
   exact `content_fingerprint` and `clauses`. Each clause has `clause`,
   `assertion`, `production_subject`, `code_source_id`, `test_source_id`,
   `run_id`, `result_id` and `report_digest`. Delivery entries are
   `{repository_key, run_id, report_digest}`. `clause` and execution
   `target_clause` name active criterion `external_id` values; the supplied set
   must exactly match every active criterion, with no omissions or duplicates.
   Read eligibility, denominator,
   gaps, exact pending scope and `proof_digest`. Unknown fields refuse.
5. `modernpath reverse-engineer acceptance-open --file
   .modernpath/reverse-engineering.runs/<folder>/acceptance.json` takes
   `key`, exact `proof`, returned `proof_digest` and the standard five-field
   human `brief`: exactly `what`, `why_now`, `changes_if_approved`,
   `risk_if_wrong` and `recommendation`, all nonempty strings. It rechecks proof,
   retains reviewed facts, creates an
   eligibility trace and opens one human decision. Use the retained eligible
   preview for this packet; no extra preview is needed solely to open it.
   Present the exact proof, relevant working-set files and limitations before
   obtaining its answer.
6. After the actual human answer (including an exact answer already provided
   in this session), use the existing
   `modernpath factory answer ASBUILT-… --options approve --text "…" --source
   USER:…`. Its gate read attaches both review pins. The answer boundary
   rechecks current evidence; permission to implement tooling is not acceptance.
7. `modernpath reverse-engineer acceptance-apply --gate ASBUILT-… --file
   .modernpath/reverse-engineering.runs/<folder>/apply.json` takes `key`,
   exact `proof_digest` and `gate_fingerprint`.
   Application rechecks all proof and atomically moves exactly the pending
   named scope to DONE, keeps compliance status unchanged and stores a receipt.
   Any currently authorized signed-in member may apply that exact reviewed
   decision; the receipt retains both answerer and applying actor. Application
   does not supply or replace the human answer.
   Report the receipt, proof currency and lifecycle/compliance states returned
   by this successful apply. Do not add status/list/preview calls or replay the
   write solely to confirm the same result. Use `acceptance-status --gate
   ASBUILT-…` if the response is missing, incomplete or contradictory, or after
   interruption. It separates recorded/applied state, current proof, retained
   reviewed facts and receipt. Identical input recovers the same receipt,
   changed input conflicts, and stale proof cannot replay as current acceptance.

Missing assertions, unexecuted tests, contradictory results, revoked sources or
unmerged delivery stay gaps. Test additions and behavior changes require an
explicit normal-development handoff. Do not fabricate RED, edit a snapshot to
approve it, pass client authority flags, or advance this edge through generic
author/sync operations. These write commands run only from the main session.

### Group JSON contract

`requirements` is a list of `kind: "user" | "system"`, unique `external_id`,
`title`, `description`, `source_citations`, and `criteria`. UR criteria contain
`external_id`, `given`, `when`, `then`; SR criteria contain `external_id` and
`statement`. Supply the UR actor/outcome and SR boundary/verification method.
SR `parent_external_ids` names exact UR parents; a parentless SR needs a rationale.
Do not send authority, approval, release or work-status fields as intent.

Each requirement also carries `context`, the bounded-context code, and
`context_name`, its name, each at most 255 characters. Heartbeat groups
requirements by the code. Use only contexts the person confirmed ("Bounded
contexts" under "Running a sweep"); publish accepts a requirement without one
and the receipt names it.

File citations use `kind: "code" | "test" | "document"`, `source_file_id`,
`repository_key`, `revision`, `path`, `sha256`, and optional locator fields.
When a test is cited and the executed test's name is known, name it in
`test_case_ref`: the as-built proof binds a criterion to the executed test by
that name. The readable `ref` remains the normalized source address.
A user requirement needs a code citation and a test citation of its own for
its upper proof, which uses its captured citations directly.
Document citations use `system_doc_id`, `version`, `fingerprint`; the server
validates them against the run and supplies their readable reference.
Typed citations publish SR→source/test edges and parents publish UR→SR edges.
Citing a test creates a source reference, never a passing test execution.

DERIVED rows require `candidate_packet` with `confirmation_brief` and nonempty
`consequences`; optional `conflicts` and `open_questions` explain ambiguity.
`compares_to: [{"kind":"SR","external_id":"SR-EXISTING"}]` asks the server
to capture an existing governed record. Review shows current versus proposed
content and warns about intervening edits. Never supply fabricated current text.

In baseline mode, explicit nonempty `exception_reason` keeps that individual row
DERIVED and requires a candidate packet. Only candidates may cite an authorized
but unresolved path using `kind: "unresolved"`, `repository_key`, `path`, `reason`.
They cannot be accepted as-built until evidence is resolved. Their links remain
candidate. Baseline receipts separate baselined and DERIVED counts.

To reuse an unchanged existing row, send only `kind`, `external_id` and exact
`reuse_fingerprint`. No incidental overwrite is permitted. Optional `epics`
contain `external_id`, `title`, and nonempty exact `members`; baseline members
must be governed. Do not create Epics to hold DERIVED discovery groups.

`source_assessments` may accompany a group or an empty `requirements` list:
each names `repository_key`, `snapshot_digest`, `path`, `sha256`, `outcome`
(`reviewed`, `unresolved`, `unsupported`) and a nonempty `reason`. Latest durable
assessment wins; an assessment is not a code trace or verified coverage.

One group holds at most 500 requirements, 100 criteria per requirement and
5000 source assessments.

### Exact candidate decisions

`candidates` reads typed records and proposed trace IDs. Save both input files
below in the sub-folder of the run that published the candidates.
`preview --file .modernpath/reverse-engineering.runs/<folder>/selection.json`
takes `requirements: [{kind, external_id, decision}]` and independent `links: [trace-id]`.
Decisions are `accept_as_built`, `accept_desired`, `reject`, `defer`. No Epic is needed.
After explicit approval of that preview, `decide --file
.modernpath/reverse-engineering.runs/<folder>/decision.json` takes the
same exact selection plus returned `fingerprint`, stable `key` and `source: "USER:…"`.
As-built becomes Base/PENDING_VERIFICATION; desired intent becomes PROPOSED;
rejected becomes OBSOLETE; deferred remains DERIVED. Unselected links remain
candidate and can be accepted later by a links-only selection. Stale fingerprints
require a new preview and decision. None of this approves compliance or DONE.

## 6. Traps

- **255 characters** per bounded authoring field: title, context code, source
  tags, decision refs, ids, option keys. Over it the server refuses the whole
  write with a 422 naming the field (`should be at most 255 character(s)`);
  a length fault the schema does not catch is still a 422 with a reason.
  Long prose goes in `--body` / `--detail` where the verb has it.
- **A push that patches a member record moves the scope context.** The push
  re-stamps every filled section the server then reports stale and says so;
  if `process next` still drops to `plan` with `canonical_sections_incomplete`,
  `process check --phase plan` names the missing or stale keys — author those
  (an unfilled stub is never pushed), or `working-set push --restamp`.
- **A process repin moves no aggregate.** The packet aggregate folds the
  scope's content, membership and canonical sections — not the server's
  compiled process revision (REQ-CROSS-412) — so a process-package repin deploy
  stales no packet and voids no open gate. `process next -v` still prints
  `full process_revision:` as information: which process text a packet was
  reviewed under stays readable, it just no longer pins a decision.
- **Several held pieces, none named.** `process next` prints one block per
  piece; every other scoped read and write refuses by name ("you hold
  several current pieces (…): … — name one with --piece <id>"; the server
  answers 409); `factory status` lists what you hold. Pass `--piece <scope>`.
- **The snapshot header names the store revision the server sent**
  (`store <revision>`, from `x-modernpath-store-revision`), or says the store
  serves none; the CLI's own build is on its own `CLI build:` line. A store
  that serves no revision has not been given one at deploy — not a sign the
  content is stale.
- **The requirements read truncates at its limit** silently; a limit below
  the corpus size gives a wrong "highest id". Ids collide anyway when
  colleagues author concurrently — re-check before minting.
- **Prefer `--from`/`--to` to `--transition`**; a quoted `--transition
  "build->verify"` is still accepted, and an unquoted arrow now fails fast (the
  fragment is refused before any write) instead of landing on an immutable
  trace. Traces recorded with a fragment before REQ-CROSS-377 are inert and
  stay.
- **A withdrawn gate id stays reserved.** Re-scoping means a new id; the
  withdrawn gate may still shadow a finder until the sibling fix lands.
- **A `your-move` label is not a gate id.** Cross-check `external_id` and
  title before reporting a gate as ready or answered.
- **Verify the whole guard, not the first firing arm.** Entry and completion
  refusals are multi-arm; the first message is true but not the whole cause.
- **Record RED evidence before GREEN**, at the RED commit — or afterwards with
  `--revision <red-commit>`; a RED never shadows a pass, and the server warns
  at record time about either ordering mistake. Run `mix` or `go` evidence
  commands from the project's documented root — and every `modernpath` verb
  from the workspace root, never from a subdirectory that carries its own `.modernpath/config.json`.
- **The on-PATH binary can lag `main`.** A merged CLI change is not usable
  until the binary is rebuilt and `modernpath install` has run; a server
  change is not live until deployed. Check `modernpath --version` against
  the merge before relying on a new verb.
- **Session tokens expire mid-session.** The remedy is interactive; report it
  and stop rather than retrying.
