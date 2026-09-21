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
the flags:

| Phase (`PROCESS.md`) | Verbs | Section |
|---|---|---|
| any — where do I stand | `process next`, `process check --phase <p>`, `your-move`, `factory status` | Where the loop stands |
| source → plan | `working-set select … --phase plan [--lane defect]`, `working-set pull --scope`, `working-set push` | 1 |
| cold review | `working-set pull --scope --for-review`, `process findings add` / `disposition`, `author trace --purpose cold-review` | 1 |
| entry | `process enter <scope>`, then the human's `factory answer`, then `author advance … --to TODO` | 1 |
| build → verify | `factory evidence --fail --role RED`, `factory evidence --pass`, `process advance <SR>` | 2 |
| completion → apply | `process complete <scope>`, then the human's `factory answer`, then `author advance … --to DONE`, `working-set select --put-down` | 2 |
| triage | `author requirement` / `author epic` / `author backlog`, `author gate --gate-kind question`, `modernpath feedback "<line>"` for a tooling gap; `author demote` to reopen a delivered item | The write channels |
| session | `auth status`, `env --set`, `factory status` | Where the loop stands; the reference |

## Where the loop stands (both modes)

| Read | What it answers |
|---|---|
| `modernpath process next` | the derived phase, why, and the skill to run; `-v` prints the full packet aggregate and process revision |
| `modernpath process check --phase <p>` | one phase's decision-table checks for the current selection (a pure read) |
| `modernpath your-move` | the pending human decisions, ranked by what one answer releases |
| `modernpath working-set pull <id>…` | a record as a readable file with its `Fingerprint:` line |
| `modernpath factory gates <gate-id>` | one gate: state, answer, chosen options, applied state |
| `modernpath factory status` / `modernpath status` | the binding and whether the workspace is store-backed |

Phases are `source`, `plan`, `cold_review`, `entry`, `build`, `verify`,
`completion`, `triage` — the vocabulary `--phase` accepts.

## The write channels (store-backed)

Every write is single-record and actor-attributed; legality is enforced by the
server, and a stale fingerprint conflicts instead of overwriting. Retired files
are not recreated to record something — a file-derived batch is refused whole.

| Channel | Carries |
|---|---|
| `modernpath author requirement`, `author epic`, `author update`, `author member`, `author relate` | records, content edits, membership and UR–SR relations. A user requirement's scenarios are `author update --criteria <json or file>`: a JSON array whose every object carries an `external_id` (the replace-set keys on it) and `given`, `when`, `then`; the array replaces the stored set whole |
| `modernpath author trace` | an immutable PASS/FAIL/STALE trace gate at an exact fingerprint |
| `modernpath author gate` / `author gate-withdraw` | a human gate, born open; `--supersedes <old>` retires an open or answered decision in the same write; a mistakenly opened one is withdrawn with a reason |
| `modernpath author advance` | a lifecycle transition; a human-gated one names the `ANSWERED` gate and its fingerprint |
| `modernpath author demote` | the demotion gate that reopens a delivered item (`--to PROPOSED\|IN_PROGRESS\|OBSOLETE --basis reversed-decision\|defect\|superseded --reason USER:…`, no prerequisite trace); `--apply` after the answer advances the item on it and reports the user requirement and epic that followed |
| `modernpath working-set select` | the frozen scope, its phase, suspension, resumption, put-down |
| `modernpath working-set push` | packet sections whole and item files as atomic patches, from the scope directory `pull --scope` materialized |
| `modernpath process findings add` / `disposition` | cold-review findings and their dispositions |
| `modernpath process reconcile --apply` | the automatic transitions current trace proofs allow |
| `modernpath process enter`, `process advance`, `process complete` | the ceremony verbs (EPIC-CLI-017): the entry gate, the lower trace plus reconcile, and the delivered-revision evidence plus completion trace plus completion gate — each from the store facts, each refusing before any write on an unmet fact |
| `modernpath factory evidence` | a test or browser run as evidence, pinned to the repository HEAD; `--role RED` marks a red-first result |
| `modernpath factory answer` | an attributable human answer on an open gate (a governed gate's review is attached for you) |
| `modernpath install --store-backed --source USER:…` | the store-backed declaration of a bound workspace that never had file ledgers: the activation gate answered with the source, the server state `active` and the marker, in one step; a workspace with ledgers goes through `migrate flip` |
| `modernpath author backlog` / `author update --kind backlog` | one backlog, gap or tooling record (`BACKLOG-…`, `GAP-…`), born `OPEN` and attributed to you; a disposition change names its `--source`; `working-set pull <id>` renders it |

A triage discovery that is neither a requirement, an epic nor a gate is a
backlog record (`author backlog <id> --kind backlog`), a gap is `--kind gap`
with its `--gap-kind`, `--affected-trace` and `--consequence`, and a tooling
gap is `modernpath feedback "<line>"`. These are store records like every
other (`PROCESS.md` §State records and reconciliation): their state is read
from the store — `working-set pull <id>` — never from a plan file or a note,
which carry no disposition the store does not; no verb lists them yet, so an
id comes from `your-move`, the feed or the record that routed it. The release
registry file is retired;
the single active release and its `USER:` source are the newest answered,
approved `release_selection` gate naming `release:<slug>` on the system —
`GATE-RELEASE-<slug>`, or a successor `GATE-RELEASE-<slug>-<n>` — written by
`factory release activate` on the system it runs from.

## 1. Plan → cold review → entry (store-backed)

**Take the scope.**
`working-set select <EPIC|SR> --kind epic|single_sr [--members a,b] --phase plan [--lane defect]`
(`--lane defect` marks a customer-blocking defect: `process next`, `your-move`
and the session brief say so, and the defect-lane rules of `rdd-start` apply).
A take with no `--replaces` *adds* a holder — you may hold several pieces; name
the one you are displacing with `--replaces <piece> --outcome done|obsolete|returned=<phase>`.
Advance the phase later with `working-set select <scope> --phase <p>`.

**Author the packet.** `working-set pull --scope` materializes
`.modernpath/working-set/<scope>/` — item files and `packet/*.md` sections —
under an authoring context. Edit, then `working-set push` (`--dry-run` first).
Until the push order is fixed in the tool: a member-record patch and its packet
sections may go in one push: item patches post first, and every filled section
the server then reports as stale is re-put unchanged so it carries the current
stamp — the push says `re-stamped N section(s)`, never "nothing to do"
(`--restamp` forces every filled canonical section). The scaffold reads the
required keys the server serves (`facts.sections.required`), so the stubs and
`process check --phase plan` walk one key set; a FAIL on `canonical_sections`
names the missing or stale keys.

**Cold review** runs from an independent context and records from it.
Delegate the pass to the installed reviewer, `.claude/agents/rdd-cold-reviewer.md`
(Read, Grep, Glob; no shell): it returns findings and a verdict, and this
session records them.

1. `working-set pull --scope --for-review` — a read-only render that stamps a
   review context. Pushes from it are refused by design.
2. Read the full aggregate: `process next -v` → `full packet_fingerprint:`.
   The unadorned line is a 12-character display prefix that `--fingerprint`
   accepts and never matches.
3. Findings: `process findings add --scope <kind>:<id> --id <F-id> --category
   <c> --severity <s> --owner <o> --body … --aggregate <full aggregate>` —
   both `--category` and `--severity` are required (the CLI never defaults to
   the most blocking pair), and a `--severity note` never blocks whatever its
   category; `process findings list` prints severity, per-round counts and
   flags a round whose material findings are all `traceability` (audit the
   change, not the document); `process findings disposition --id <F-id>
   --disposition RESOLVED|DEFERRED|REJECTED --expected-fingerprint <from list>`.
   Material categories are `correctness`, `security`, `data_loss`, `contract`,
   `traceability`, `testability`; a material finding left `OPEN` **or
   `DEFERRED`** fails the check — an out-of-scope pre-existing finding is
   `REJECTED`, not deferred. Findings are scope-scoped, not aggregate-scoped.
4. Verdict: `author trace CR-TRACE-<scope>-R<n> --purpose cold-review
   --verdict PASS|FAIL --scope <epic> --scope <each member> --source …
   --title …`. The pin defaults to the scope's current packet aggregate and
   the transition to `plan->entry` (REQ-CROSS-376/377); a given
   `--fingerprint` must be the full 64-character hash of the right class (a
   display prefix or a content hash is refused by name; a full hash matching
   nothing is recorded with a warning), and a transition is `--from`/`--to`
   or one quoted `--transition FROM->TO` — a fragment is refused before any
   write. The trace inherits the review context the scope was pulled under; a
   verdict with no review context is refused, and one recorded from an
   authoring context reads as not independent.
5. `process check --phase cold_review` → `independent_verdict` and `findings`.

**Entry.** `modernpath process enter <scope>` does the ceremony from the store
facts: it refuses naming the fact when the packet sections are missing or
stale, when no independent passing cold-review trace exists at the current
aggregate, when the `entry_brief` packet section is absent or incomplete, or
when `ENTRY-<scope>` is open or answered (answer or apply it); otherwise it
opens the gate naming the epic plus every member still in FROM (a single SR
names itself), the cold-review trace as prerequisite and `PROPOSED->TODO`,
and moves the selection to phase `entry`. `--dry-run` prints the plan. The
brief is the `packet/entry_brief.md` section in the PROCESS.md brief shape
(`- What:`, `- Why now:`, `- Changes if approved:`, `- Risk if wrong:`,
`- Recommendation:`; every bullet required), or `--brief-file`.

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

**Answer, then apply.** The human answers in Mission Control or with
`factory answer ENTRY-<scope> --options approve` (a human decision; the CLI
attaches the review a governed gate needs). Answering records; it moves
nothing. Apply members first, then the epic:

```
modernpath author advance <member> --kind requirement --to TODO --expected PROPOSED \
  --gate ENTRY-<scope> --gate-answer approve --gate-fingerprint <gate Fingerprint>
modernpath author advance <epic> --kind epic --to TODO --expected PROPOSED --gate … 
```

`--gate-fingerprint` is the gate's content-shadow hash — the `Fingerprint:`
line of `working-set pull ENTRY-<scope>`. `--gate-answer` accepts the option
key, its label, or the stored answer text. The gate closes and reads
`applied` once every named transition has landed.

The entry pass ends here: report the applied transitions and stop. The answer
was a decision about the gate, not an instruction to build (`PROCESS.md`
§Gates); §2 begins in a session the human opens for the build.

## 2. Build → evidence → completion → apply (store-backed)

The operator records evidence; the tool does the ceremony (EPIC-CLI-017).

1. `working-set select <scope> --phase build`.
2. **RED**, recorded at the RED commit: `factory evidence --fail <SR> --role
   RED --kind local_test --log "<command>"`, or later with `--revision
   <red-commit>` (no checkout). Evidence currency is role-aware: a RED never
   shadows a passing result, and the server warns at record time about a pass
   with no RED before it or a RED recorded after a pass.
3. **GREEN**: `factory evidence --pass <SR> --kind local_test --log "<command>"`.
4. **Advance**: `modernpath process advance <SR> --log "<command>"` reads the
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
   --purpose upper --scope <UR> --fingerprint <its content hash> --from build
   --to verify --verdict PASS` — the transition is inferred only for
   cold-review, entry, lower and completion, so an upper trace names it) and
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
8. **Apply**, members first: `author advance <member> --kind requirement --to
   DONE --expected IN_REVIEW --gate COMPLETE-<scope> --gate-answer approve
   --gate-fingerprint <gate Fingerprint>` for every SR, then the UR (also
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
   `no route derived — entry_origin_unavailable` for an epic whose members are
   all `DONE`; it still prints the aggregate.
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
   standing; a reversed decision stales the plan too — retires the item's own
   entry inside the shared entry gate (siblings keep theirs), and moves the
   user requirement and the owning epic that were `IN_REVIEW`/`DONE` on the
   same gate (the epic to `PROPOSED` for a reversed decision, else
   `IN_PROGRESS`); the verb prints them as `followed:` lines.
4. Next: a defect is rebuilt red-first and `process complete` re-completes
   the epic on the siblings' current evidence (§2 step 6); a reversed
   decision is re-planned and cold-reviewed, and `process enter` opens the
   successor entry gate `ENTRY-<scope>-R2` (§1 Entry).

## 3. The work-selection model (store-backed)

- **One holder per piece; several pieces per person.** The constraint stops
  two people taking the same work and does nothing else. A take of a piece
  someone else holds is refused naming the holder; a suspended piece is
  claimable by anyone.
- **`--piece <scope>`** on `working-set` and `process` names which of your
  current pieces a read or write resolves. With several held and none named,
  every scoped read and write is refused by name — `process next`, `process
  check` and `process reconcile` print "you hold several current pieces (2):
  A, B — name one with --piece <id>", the server answers 409, and `factory
  status` lists
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
- Re-pulling `--scope` rotates the authoring context; the review context is
  stamped by `--for-review` and inherited by the cold-review trace.

## 5. Refusal glossary

`origin: cli` strings are printed by this binary; `origin: server` strings
arrive as `server <code>: <message>` or verbatim from a 422.

| Refusal (substring) | Origin | Cause | Clears it |
|---|---|---|---|
| `no current work selection — \`working-set select\` a scope first` | cli | a scope read or push with no selection | `working-set select <scope>` |
| `no current selection — nothing to route` | cli | no current piece is held (several held pieces are refused by name instead) | `working-set select <scope>` |
| `you hold several current pieces (…): … — name one with --piece <id>` | cli | a scoped read or write under several held pieces, none named | `--piece <scope>` on the command |
| `push: re-stamped %d section(s) whose scope context moved` | cli | unchanged sections the server reported stale were re-put for a fresh stamp (not an error) | nothing |
| `no current selection named %s is held by you` | cli | `working-set pull selection --piece X` for a piece you do not hold; the snapshot is left unchanged | name a held piece |
| `no route derived — <reason>` | cli | `process next` on a live selection with no phase to route — `entry_origin_unavailable` when every member is already `DONE`; not an error, and `-v` still prints the aggregate | nothing; read the aggregate from it or from `process check --phase completion -v` |
| `you hold several current selections (…) — name one with ?scope=<id>` | server | an unscoped scope read or write under several pieces | `--piece <scope>` on the command |
| `is already held by` | server | a take of a piece someone else holds | take a different piece, or wait for a put-down |
| `closing … requires stating how it ended` | server | a put-down or displacement with no outcome | add `--outcome done\|obsolete\|returned=<phase>` |
| `you already hold … \`replaces\` displaces a piece on a fresh take` | server | `--replaces` on a piece you hold | advance in place; drop `--replaces` |
| `suspending requires a reason` | server | `--suspend` without `--reason` | add `--reason` |
| `DERIVED requirement candidates cannot be selected as governed work` | server | selecting a candidate | confirm it first (`rdd-discover`) |
| `is a --for-review directory … review pulls are read-only and never push` | cli | a push from the review render | pull without `--for-review` to author |
| `a cold-review trace needs a review context, but none is stamped` | cli | `author trace --purpose cold-review` with no review pull | `working-set pull --scope --for-review`, then record |
| `could not determine the packet aggregate fingerprint` | cli | a finding without a resolvable aggregate | pass `--aggregate <full aggregate>` |
| `--expected-fingerprint is required — a finding disposition is fingerprint-guarded` | cli | disposition without the guard | read it from `process findings list` |
| `a … finding is immutable except its reference — a reopen or content edit is refused` | server | editing a settled finding | record a new finding |
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
| `a demotion carries the human's reason as a USER: source` | cli | `author demote` without `--reason USER:…` | pass the reason as `USER:<date>:<why>` |
| `only delivered work is demoted` | cli | `author demote` on an item that is not `IN_REVIEW` or `DONE` | nothing — the item is not delivered; use the ordinary loop |
| `is open — answer it first` | cli | `author demote --apply` before the human answered | answer the gate, then `--apply` |
| `cannot enter through a members-only gate; demote or reopen the epic first` | cli | `process enter` on a DONE or OBSOLETE epic with PENDING_VERIFICATION members | demote or reopen the epic, then enter its as-built members |
| `is already open — answer it` | cli | `process enter`/`complete` find the primary id (or one in its -R series) open; `process reenter` finds its id open | answer it in Mission Control or with `factory answer` |
| `is already answered — apply it` | cli | the same, answered | `author advance … --gate <id>` members first |
| `behind the delivered tip origin/` | cli | `process complete` on an ancestor of the merged tip (a session that never pulled) | pull the merged tip |
| `is not on the delivered branch origin/` | cli | `process complete` on a branch commit | merge, then check out the merged revision |
| `could not fetch origin/` | cli | `process complete` offline or without a reachable remote | restore the remote; `--no-fetch` is for offline fixtures only |
| `has members not yet IN_REVIEW` | cli | `process complete` before every member advanced | `process advance <SR>` each |
| `--fingerprint must be a full 64-character hash` | cli | a display prefix on `author trace` | omit `--fingerprint` (the store's value is read) or pass the full hash from `process next -v` / `working-set pull <SR>` |
| `but the value given is the …` (`--purpose lower pins to the content hash, but the value given is the packet aggregate`) | cli | the wrong pin class on `author trace` | omit `--fingerprint`, or pass the class the purpose takes |
| `--transition must be FROM->TO` | cli | a shell fragment of an unquoted arrow | `--from`/`--to`, or quote the arrow |
| `must be a full 64-character hash for a … trace` / `must be FROM->TO` | server | the same shapes reaching the authored path from an older CLI | upgrade the CLI, or pass the full value / the pair |
| `still a prerequisite of` | server | withdrawing a gate another gate names | withdraw the dependent first |
| `pin_required` | server | `factory release activate` (and the other release lifecycle transitions) without `--pin`, or with a wrong one | the release PIN of the signed-in person is required; it is set in Mission Control — pass it with `--pin`; `pin_locked` says repeated failures locked it for a while — retry later |
| `does not exist` (`gate <id> does not exist`) | cli | `factory gates <id>` on an id the store does not hold — a mistyped id, or a gate the store never wrote (the active release's `GATE-RELEASE-<slug>` when the release was activated from another system, or before the activation wrote it) | check the id in `your-move --queue` or the epic pull; for the release, `factory status` and `SELECTION.md` name the gate or say `no recorded selection gate on this system` — run `factory release activate <slug> --source USER:…` here to record one |
| `not served by … (unknown external id)` | cli | `working-set pull <id>` on an id the read surface does not hold | the same checks; an applied gate a pull cannot resolve is a known read gap, not a missing decision |
| `transition is required for purpose …: give --from and --to` | cli | `author trace --purpose upper` without `--from`/`--to` | add `--from build --to verify`; only cold-review, entry, lower and completion infer their transition |
| `criteria: every stated criterion needs an external_id` | server | `author update --criteria` with an object lacking `external_id` | give every object `external_id`, `given`, `when`, `then` |

## Reverse-engineering onboarding (store-backed)

Use this sequence with `rdd-reverse-engineer`; these operations are separate
from delivery entry/completion gates. Run them only in the main session.

1. `modernpath factory status` verifies the authenticated system binding.
   `modernpath docs sync` refreshes the local document projection.
2. `modernpath reverse-engineer inventory --repository key=/absolute/root`
   (repeat for each repository) emits frozen paths, hashes, sizes, revision,
   dirty state and exclusions. Non-Git directories and Git worktrees are supported.
3. `modernpath reverse-engineer preflight` returns existing-corpus fingerprint,
   recommended mode and document descriptors. Ask the user for **baseline** or
   **derived**, showing scope and consequences; recommendation is not authorization.
4. `modernpath reverse-engineer authorize --file authorization.json` takes
   `key`, explicit `mode`, attributable `authorization_source: "USER:…"`,
   `corpus_fingerprint`, `repositories` from inventory and `documents` selected
   from preflight. Document descriptors include `id`, `kind`, `version` and
   `fingerprint`. Actor, system, Base and process revision are server-owned.
5. For each repository, `modernpath reverse-engineer capture-source --run ID
   --repository key --root /absolute/root`. Poll `source-status --capture ID`
   until ready; use returned immutable source-file IDs. Interrupted captures
   resume without provider OAuth or FileAnalysis. `read-source --source ID`
   returns exact captured bytes; `read-document --run ID --document ID` returns
   the authorized immutable document snapshot.
6. `modernpath reverse-engineer publish --run ID --group stable-key --file group.json`
   publishes a coherent graph atomically. `status --run ID` reads durable group
   receipts. Repeat identical inputs and keys after interruption; conflict means
   reconcile, not silently pick another key.
7. `modernpath reverse-engineer coverage --run ID` measures the frozen inventory
   against stored traces. It separates governed/candidate linkage, captures,
   assessments and exclusions. It is not execution evidence or behavior-class
   enumeration. `modernpath coverage` does not measure retired local ledgers in
   a store-backed workspace. Read the actual Ledger and Requirements surfaces
   before claiming baseline-ready or candidate-ready.

### Group JSON contract

`requirements` is a list of `kind: "user" | "system"`, unique `external_id`,
`title`, `description`, `source_citations`, and `criteria`. UR criteria contain
`external_id`, `given`, `when`, `then`; SR criteria contain `external_id` and
`statement`. Supply the UR actor/outcome and SR boundary/verification method.
SR `parent_external_ids` names exact UR parents; a parentless SR needs a rationale.
Do not send authority, approval, release or work-status fields as intent.

File citations use `kind: "code" | "test" | "document"`, `source_file_id`,
`repository_key`, `revision`, `path`, `sha256`, and optional locator fields.
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

### Exact candidate decisions

`candidates` reads typed records and proposed trace IDs. `preview --file selection.json`
takes `requirements: [{kind, external_id, decision}]` and independent `links: [trace-id]`.
Decisions are `accept_as_built`, `accept_desired`, `reject`, `defer`. No Epic is needed.
After explicit approval of that preview, `decide --file decision.json` takes the
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
- **Several held pieces, none named.** Every scoped read and write refuses by
  name ("you hold several current pieces (…): … — name one with --piece <id>"; the server
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
