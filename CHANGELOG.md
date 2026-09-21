# Changelog

## v0.7.0 — the CLI ships its own instructions, and a shipped item can be reopened

### `process reenter --gate-id` opens a successor when the default id is reserved

`process reenter <scope>` hardcoded the re-entry gate id to `REENTRY-<scope>`.
If a prior `REENTRY-<scope>` was withdrawn — which permanently reserves the id —
open refused (`a gate is opened once`) and the shared `gateIDFree` message
suggested a `--gate-id …-R2` successor that `reenter` did not accept (`unknown
flag: --gate-id`), leaving no CLI path to re-open a re-entry gate after a
withdrawal. `reenter` now accepts `--gate-id`, mirroring `process enter` and
`process complete`, so a successor id can be opened. BACKLOG-TOOL-52.

### `process reenter` re-establishes entry after a material reversal

`process reenter <scope>` opens a human re-entry approval gate at the current
packet aggregate; `process reenter <scope> --apply` (after the gate is answered
`approve`) re-pins the stranded applied entry gate to the current aggregate so
the reopened item can build again. Unlike `process reapply-entry` — an
immaterial-only attestation — re-entry requires a fresh **independent cold-review
PASS at the current aggregate** and the answered human gate, because the content
materially changed (a reversed decision). The item stays IN_PROGRESS (no
lifecycle transition); `reapply-entry`'s immaterial-only contract is untouched.
This closes the follow-on gap to `process supersede --apply`: a reversed-decision
reopen lands at IN_PROGRESS but previously had no honest re-entry path
(`process enter` needs PROPOSED; `reapply-entry` forbids a material change).
BACKLOG-TOOL-44.

### `process supersede --apply` reopens one shipped item without a global enforce flip

`process supersede <id> --apply --source USER:… [--supersedes USER:…]` applies
the invalidation cascade for that one item — demoting a shipped `DONE`/`IN_REVIEW`
item back to `IN_PROGRESS` and staling its evidence — while the system's cascade
mode stays `report`. This is the scoped, attributed way to reopen a shipped
requirement when an approved decision is reversed, instead of flipping the whole
system into `enforce` (which then cascades on every later edit). Without `--apply`
it stays a dry run. `--apply` requires an attributable `USER:` source (server- and
client-enforced); `--supersedes` records the prior decision the reversal overrides,
and both ride the item's durable drift event so the reversal is captured old→new
(BACKLOG-TOOL-39).

### The kit ships the reviewer agent definition

`modernpath install` writes `.claude/agents/rdd-cold-reviewer.md` — the
delegated cold review's read-only context (Read, Grep, Glob; no shell; the
review rules) — in both modes, and `install --check` reports an edited or
missing copy as drift. The managed block and the `mp-process-cli` skill's
cold-review step name it (REQ-CROSS-409).

### `feedback --last` shows what it attaches; `--ref <n>` picks an earlier command

With `--last`, `feedback` prints the command it is about to attach — the
command line, its exit status and the first output line — before the record
is written. `--ref <n>` attaches the n-th most recent recorded command
(`--ref 1` is `--last`) and implies `--last`; a value beyond the recorded
count is refused naming how many entries exist (REQ-CROSS-410).

### Failing checks and the Drift label say why

`process check --phase cold_review` prints, under a failing
`independent_verdict`, the trace it chose and why it does not count — the
verdict, or which arm of the independence rule the review context failed (no
review context on the trace, it authored a section of this scope, it authored
a patch in this scope); under `UNAVAILABLE`, that no trace exists at the
current aggregate and the scope's traces pinned at other aggregates; under a
failing `findings`, the open or deferred material finding ids. `--phase
completion` names the settled gate that is missing, or the members not yet
`IN_REVIEW`/`DONE` with current evidence. The facts serve every reason
(`independence_reason`, `open_finding_ids`, `stale_traces`,
`settled_gate_external_id`, `members_not_reviewed`); the CLI computes nothing.
A `[Drift]` line in `your-move` carries its basis — the run's revision and the
head it no longer matches, or the run's time with its validity lapsed — and the
re-record verb; `factory drift`'s help defines the label (REQ-CROSS-408).

### Release activation records the selection gate; `pin_required` says whose PIN

`factory release activate` now writes the release-selection gate on the
system it runs from, in the release's transaction: `GATE-RELEASE-<slug>`,
answered by the activating person with the `USER:` source and stamped to the
release. The reads (`factory status`, `SELECTION.md`, the preflight) resolve
the newest approved gate naming the release rather than a fixed id, so a
successor `GATE-RELEASE-<slug>-<n>` — written when an open or answered gate of
that purpose and scope is superseded, or beside a dismissed one — is found,
and a release active with no gate on this system reads `no recorded selection
gate on this system` with the activation as the remedy; a gate written before
the scope token existed still binds under its bare id, and an approved gate
without a `USER:` source is superseded like an unaccepted one, so the remedy
cannot loop. The `pin_required`
refusal names the signed-in person's release PIN, Mission Control and
`--pin`; `pin_locked` names the lockout without a duration. A gate is now
pullable by its own id — `working-set pull GATE-RELEASE-<slug>` (or an entry
or completion gate) writes its block with the `Fingerprint:` line a gated
advance echoes (REQ-CROSS-407).

### `install --store-backed --source USER:…` declares a ledgerless workspace in one step

A bound workspace that never had file ledgers is declared store-backed by
the tool: one authoring action records the store-backed activation gate,
answered by the signed-in person with the `USER:` source, and sets the
system's process-store state active in one transaction; the CLI then writes
`process/store-backed.md` through the writer `migrate flip` uses, with no
retired files, and the install that follows withholds the ledger skill. The
server decides by its own state — a seeded or cleared system is refused by
name and goes through `migrate flip`, an already-declared one is reported, a
declaration gate that already exists in a state that cannot carry the
declaration is refused naming that state — and a workspace with ledgers under
`tasks/` is refused here. The state the flip's HTTP declaration records is
written by the same server function, under a row lock (REQ-CROSS-406).

### `init` signs in before it lists systems; the api-client verbs state the credential

On a checkout with no bound system, `modernpath init` establishes the
credential before it lists systems: with none stored, or a stored token whose
expiry has passed, it runs the sign-in for the target server when there is a
terminal and otherwise stops naming the sign-in command as the next step. The
listing is never attempted with a missing or expired credential. Without a
terminal, one system is bound as the only choice and several are listed with
`--system-id` named as the way to choose. A refused listing names the step
and the verb that continues, and leaves the workspace unbound. `docs sync`,
`search`, `ask`, `read-doc` and `read-file` now carry the three credential
statements the factory verbs already print: no binding, no or expired
credential (before any request), and a served 401 rendered as a statement
about the credential with the repair command — never `HTTP 401` alone
(REQ-CROSS-405).

### `mp-process-cli` glossary covers the refusals a fresh system meets

Four refusals the section G run met with no glossary row now have one:
`pin_required`, `does not exist` / `unknown external id`, the upper trace's
required transition, and a criterion without an `external_id`. The upper-trace
example carries `--from build --to verify`, and the write-channel table states
the `--criteria` object shape.

### `mp-process-cli` opens with the phase-to-verb map

The skill now starts with a table from each `PROCESS.md` phase to the verbs
that drive it and the section that gives their order, so a reader arriving
from the phase table finds the entry point without reading the skill end to
end. The interim gaps file is retired as the channel: `modernpath feedback`
files the record, and the file receives a line only when the store cannot be
written.

### The managed block points at the process skill from the reading order

The installed `AGENTS.md` block now says, on the `PROCESS.md` line of its
reading list, that in a store-backed workspace every phase is driven through
the `modernpath` CLI and that `mp-process-cli` is the verb sequence for each
phase, not background reading. A session that read the block in order used to
describe the loop with no verb in it, because the skill was filed under
"tooling rather than process" after the twelve-skill list. The block stays a
pointer; no verb detail moved back into it.

### Help and skill text follow the named length refusal

An over-length bounded authoring field is now refused by the server as a
422 naming the field (`should be at most 255 character(s)`), so `author
requirement|update`, `process findings add`, the `mp-process-cli` glossary
and its traps say that instead of describing an empty server fault.

### Help text names the caps, the UR in a completion gate and the retire-and-reopen recipe

`--help` (and the installed `.modernpath/cli-reference.md`) now states what six
sessions met as an unexplained refusal: `process findings add` lists the nine
categories the server accepts and keeps the body a pointer; `author gate`
says a completion gate names the epic's UR beside the SRs and how to retire a
stuck governed gate under a new id with `--supersedes`; `factory evidence` says
the UR needs a run of its own; `author requirement|epic|update` name the
255-character scalar cap and where prose goes; `working-set push` explains the
stale-section stamp after a record patch; `process next` explains the
several-pieces "no current selection" and the process-repin aggregate move.
Help text only — no behavior changes.

### Delegated agents cannot write the process store; permission rules ship with the kit

`modernpath check --hook PreToolUse` — the adapter `hooks install` wires into
Claude Code and Codex — now denies a store-write verb (`author …`, `factory
answer|evidence|sync|release|connect|pin|pull`, `working-set select|push`,
`process reconcile|findings add|findings disposition|supersede|cascade-mode`,
`migrate`, `env --set`, `docs push`, `import`, `new`) when the hook payload
carries a subagent identity, with the process rule as the reason: a delegated
pass returns findings and a verdict, the orchestrating session records them.
Reads, `auth status` and every main-session call pass as before.

`hooks install` also merges the kit's Claude Code permission rules for the
CLI verbs into `.claude/settings.json`: reads, routine writes and the in-loop
decision verbs are allowed, the system-wide verbs ask, both invocation forms
ruled. A rule is added only where the workspace holds it in no list — a rule
you moved, to `ask` or to `deny`, stays where you put it through every
install; `hooks uninstall` removes the kit's rules from allow and ask and
leaves a denied one alone; `hooks status` reports how many are placed.

The guard reads a command the way the commit gate does — through chains,
env-assignment prefixes, `command`/`env` wrappers, paths, root flags and
shell `-c` bodies — and quoted prose is not a command. Only an identity field
(`agent_id`, `subagent_id`) marks a delegated agent; `agent_type` alone never
does.

### `hooks install` keeps the project's settings bytes

Installing or reinstalling a hook family into `.claude/settings.json` (and the
Codex, Cursor and Pi settings) used to rewrite the whole file through the
default JSON encoder: `>` and `&` in every hook command came out as `\u003e`
and `\u0026`, top-level keys were re-sorted, and an owned entry was replaced
whole — a second, project-owned command placed beside the tool's in the same
matcher group was dropped. The writer now keeps the file's top-level key
order, never HTML-escapes, and salvages foreign commands from a replaced entry
into the replacement, so a reinstall on an unchanged file is byte-stable.

### `modernpath install` writes a command reference rendered from the binary

`.modernpath/cli-reference.md` now lists every verb, flag and default of the
binary that wrote it, rendered from its own command tree, so it cannot drift
from the build; `install --check` reports an edited or stale copy as drift,
and the file is versioned beside the process snapshot. The loop verbs' `--help`
(`author gate`, `author trace`, `author advance`, `working-set select`,
`factory evidence`, `process findings add`, `process reconcile`) now state
their prerequisites and order — which fingerprint each trace purpose takes and
where to read it, trace before gate, members before the epic, RED at the RED
commit, which finding categories block — and the reference carries the same
text. The managed `AGENTS.md` block and the `mp-process-cli` skill point at it.

### `mp-process-cli`: the loop's CLI recipes ship as a skill

`modernpath install` now writes `.claude/skills/mp-process-cli/SKILL.md` in
both file-backed and store-backed workspaces: the exact verb sequence for
plan → cold review → entry and build → evidence → completion → apply, the
work-selection model, the three fingerprints and what moves them, a refusal
glossary with the verb that clears each, and the traps six sessions paid for.
The managed `AGENTS.md` block shrinks to the entry reads and a pointer at the
skill; the write-channel table and the retired-file prose moved into it.

### Embedded process snapshot: req-driven-dev `55e4912`

`modernpath install` now writes the canonical rules merged in req-driven-dev
PR #20: cold review audits the change rather than the packet and stops after
two rounds; a single-requirement packet is bounded to one page; a diagnosed,
bounded defect may cite its failing test as a source before entry; a
delegated pass writes nothing to the store and returns a refusal verbatim;
the delivered revision is the integrated one; and the project's sanctioned
tool is the only path to the store — a missing surface is a gap to surface,
a hand-written store change is a stop.

### `modernpath install` with CLAUDE.md symlinked to AGENTS.md

When `CLAUDE.md` and `AGENTS.md` are the same file (a symlink in either
direction), install used to merge both managed blocks into it under the same
markers, and whichever it wrote last replaced the other — sometimes leaving
`AGENTS.md` with only the Claude `@`-imports, which Codex does not follow.
`install --check` then reported drift on every run. Install now merges only
the `AGENTS.md` block into the shared file, reports `CLAUDE.md` as carrying
it, keeps the symlink, and `--check` and `--dry-run` treat the link the same
way.

### `modernpath auth` chooses a workspace

If you have access to more than one workspace, the browser sign-in now lists
them by name after you sign in and asks which one to use, then signs you in
to it; every command after that answers from that workspace. The choice is
kept in `.modernpath/auth.json` beside the credential and applied on the
next bare `modernpath auth` on the same plane; `--choose-workspace` asks
again. `--workspace <id>` names the workspace outright, for scripts and
headless hosts, and is refused before any request when the value is not a
workspace id, when combined with `--choose-workspace`, or on a host that
takes a pasted token. The device flow cannot list your workspaces (ZITADEL
refuses the list to its token), so it never asks: a bare `--device-flow`
keeps the workspace the sign-in lands in — your own, for most people — and
says how to reach another, `--workspace <id>` names one, and
`--choose-workspace` is refused with it. A sign-in whose token names no workspace at all — an identity service that
predates workspace-scoped tokens — is stored without one, and a chosen
workspace is then refused with that reason rather than a blank id. The CLI
never stores a credential for a workspace
other than the one you named: if the issuer lands the sign-in elsewhere,
the command fails and the previous credential is left as it was. A failed
browser or device sign-in now exits non-zero, so a script can stop on it.
`modernpath env` shows the workspace the credential is for. Sign-in asks for
two more scopes — ZITADEL's own audience, which the workspace list needs,
and the resource-owner claims, which the platform needs to recognise a
workspace you were granted into — and, when a workspace is chosen, the
organization filter that narrows the token to it. (EPIC-CLI-009,
REQ-CROSS-334..336)

### `modernpath auth` opens your browser

`modernpath auth` against production or the test plane now runs OAuth
authorization code with PKCE: it opens your browser at ZITADEL, listens on a
loopback port for the sign-in to come back, and exchanges the code — no URL to
copy, no code to approve. The RFC 8628 device flow is still there for where
that cannot work, and the CLI picks it on its own when it sees an SSH session
(tmux on a remote box included), a CI shell, a container, a Linux host with no
display or no `xdg-open`, or a browser that fails to launch — printing why.
`--device-flow` forces it. Once the browser is open there is no fallback: a
refused or timed-out login fails as such rather than asking you to approve
twice. The five-minute login timeout covers the whole sign-in — the wait for
the browser and the token exchange — and OIDC discovery has a timeout of its
own, so a stalled issuer fails the login instead of hanging it.

The browser launcher honors `BROWSER` when set, so WSL, editor remote sessions
and unusual desktops can name their own — read the way `xdg-open` reads it: a
list of commands separated by the path-list separator, tried in order, where
`%s` stands for the URL; arguments split shell-style, so a program path with
spaces can be quoted. Setting `BROWSER` also says the browser is reachable:
it takes precedence over the SSH, CI and container checks, which is what a
VS Code remote session or dev container that forwards the loopback port
needs. `CI=false` no longer counts as a CI shell. A launcher that starts and
then exits with an error is treated as no browser, so the fallback fires at
once rather than after the timeout. On Windows the URL goes straight to the
registered protocol handler instead of through `cmd.exe`.

### `factory gates` reads a gate's state, not just the open queue

`modernpath factory gates <external_id>` shows one gate by id — its state and,
when answered, the answer, chosen options, `USER:` source, answerer and whether
the answer has been applied — so "did my approval land?" is answerable from the
CLI without reading the event stream. An applied answer is stored `closed` and
is read by id or under `--state all`.
`--state open|answered|dismissed|superseded|all` lists gate history (an unknown
value is refused before any request; a server `422` is surfaced verbatim), and
`--json` prints the server's gate envelope on stdout and nothing else —
`{"gates": […]}` for a listing, `{"gate": {…}}` by id. The bare `factory gates`
is unchanged: the open decision queue. (REQ-CROSS-109)

### `working-set` guards the planning packet against blank and unfilled sections

`working-set pull` now scaffolds the packet skeleton — a stub for each canonical
section the phase table requires that the store does not serve (reconnaissance,
red strategy, decisions, and one enrichment per system-requirement member) — so
an agent that pulls a scope sees what the loop expects, without ever overwriting
a local draft. `working-set push` treats a blank file or an unfilled stub as not
authored: it is skipped and named, never sent as a whole-blob put and never used
to empty a served section; a filled stub is sent with its marker line removed.
The store refuses a blank packet section outright, so a canonical section can
never register as present with nothing behind it. (REQ-CROSS-330/331/332)

### The tooling skill carries the completion-gate UR rule, the retire-and-reopen recipe and the caps

`mp-process-cli` now says what five stuck completion gates on a production store
taught: the epic's user requirement is a named item of the completion gate — it
needs its own passing evidence, its own `--scope`, and its own `author advance`
— while `working-set pull <epic>` lists only the SRs, so the UR is pulled by
id. A gate that can never be approved is retired by superseding it at the
current aggregate (`--supersedes`), with the suspend/resume piece-holding that
keeps other held pieces unambiguous. The refusal glossary gains the findings
category vocabulary, `no route derived`, the several-pieces refusal and the
nested-config binding trap; the traps name the 255-character field cap,
the stale packet-section stamp and the process-revision fold of the aggregate.

## v0.6.0 — the process became twelve passes, and the loop got a place to stand

`modernpath install` now writes the full requirement-driven process rather than a
handful of skills: **twelve passes** under `.modernpath/rdd/skills/`, each one a
phase you can enter and exit, pinned to `req-driven-dev@e3f60dde`.

`rdd-start` · `rdd-discover` · `rdd-plan` · `rdd-cold-review` · `rdd-entry-review`
· `rdd-build` · `rdd-verify` · `rdd-completion-review` · `rdd-triage` ·
`rdd-deliver` · `rdd-audit` · `rdd-reverse-engineer`

The old flat set (`rdd-planning`, `rdd-discovery`, `rdd-build-loop`) is gone. The
replacement is not a rename: entry is now a gate with a packet behind it,
cold review runs from a context that did not author what it reviews, and
completion audits evidence at the delivered revision rather than at the revision
someone remembered. `PROCESS.md` ships alongside as the single authority, and
`file-state/` carries the record shapes it serializes.

### `modernpath focus` — say what you are on, without claiming it

Visibility only: no assignment, no lock, nobody's queue changes. Declare it,
clear it, list what everyone else declared. `--infer` reads the refs you are
actually touching and proposes the answer, with a hysteresis buffer so a single
stray file does not flip your focus. A transition emits `focus_changed` naming
the human who caused it.

### Your move, on arrival

A `SessionStart` hook writes a personal brief: what is waiting on **you**,
ordered by what it unblocks, not by when it was created. The hook family owns no
repository files — it reads and writes nothing you have to merge.

### `factory sync` stops falling over on a bad afternoon

Tunable chunk size, retry on 5xx instead of abandoning the batch, and `--no-docs`
when you want records without the document payload. Reachability warnings now
name the system they could not reach, which turns "connection failed" into
something you can act on.

### Fixes worth naming

- **The health probe never sent the bearer**, so a reachable system answered
  `401` and reported itself unreachable. Two of the three probes also skipped the
  Gateway prefix.
- **12 of 20 open-question gates were losing their brief** between authoring and
  the wire — the decision arrived without the reason for it.
- **Five approval reports were false.** A dormant test, switched on, found them.
- **The answered-gate log was written into a directory the CLI never creates.**
  `factory pull --apply` opened `mission-control/ANSWERS.md`, got `ENOENT`,
  swallowed it, and reported success — so in every installed workspace the log
  was silently empty. It now writes `ANSWERS.md` and `answers.jsonl` at the
  workspace root.
- **Hook migration was guarded for Claude and not for Cursor**, so Cursor users
  kept a stale hook after upgrading.

### Changed defaults

- `modernpath env` defaults to **cloud production**. `beta` still exists as a
  named environment, now marked deprecated.
- The `--legacy-extractor` flag is removed. It shelled out to a node op-builder
  that only ever existed in one repository; the bundled Go parsers have been the
  default for some time and are now the only implementation.

## v0.5.0 — a repository with no requirements gets a ledger

`modernpath install` now ships **`rdd-reverse-engineer`**: the method for turning
an existing codebase into a requirement ledger, so the four-command path —
`auth`, `init`, `install`, then ask — works on a repository that has never heard
of this process.

It is not a prompt. It is a procedure that was run against two real client
repositories and corrected eight times by what they did to it.

### The method, in the order that matters

**A — the domain first.** Read the schema directly: entities, owners, and the
invariants only constraints state. Draw context boundaries by **aggregate
ownership** — whoever writes a table owns it — not by route-file layout, which
follows the build system rather than the business.

**B — then the surfaces.** Every view, the actors its role gate admits, the
endpoints it calls. Group them into journeys and write one epic per journey,
each carrying a **user requirement**. A pass that emits only ledger rows produces
a system with no stated purpose.

**C — only then the requirements.** Enumerate entry points and work the list:
HTTP routes, **agent and LLM tool surfaces**, workers, webhooks, CLI commands.
Two passes over one context differed by an entire conversational surface because
one counted only HTTP.

### Honesty rules, each learned the hard way

- **`DONE` is not reachable by a derivation pass.** `PROCESS.md` §7.6 requires
  human sign-off. Test evidence lives in an `Evidence` column instead — *run,
  passing* / *mocked* / *no test* / *not run* — because the vocabulary has no
  value for "test-verified but unapproved".
- **Nothing derived is `PROPOSED`.** That means *not started*, and a payment flow
  running in production every day is not an unstarted proposal. The one exception
  is behaviour the system should have and does not, sourced `finding, this pass`.
- **A mocked-out service is not evidence.** A route test that patches the service
  it nominally tests verifies routing, not behaviour.
- **Ambiguity becomes a question, not an inference.** Code answers *what*, rarely
  *why*.
- **A coverage manifest is the only definition of "done enough"**, and it gates
  the sync. Five passes once produced an accurate ledger covering 10% of its
  system with nothing anywhere saying so.
- **Audit your own citations, and skip named absences.** A row saying *"there is
  no `test_draft_service.py`"* is naming a gap, not citing a file.

### What it produced

On a 156-endpoint platform: **967 requirements across 11 of 11 contexts**, 16
journey epics, 36 of 36 reachable views carrying a user requirement, **3,222 of
3,222 citations resolving**, and zero rows claiming verification they did not
have. Findings no endpoint walk reaches: 13 unreachable views, six tables written
by two owners, an agent tool returning contact details the REST route gates.


## v0.4.0 — the harness hooks stop lying, and start being fast

### Hooks own no repo files

Both hook families invoked scripts under `.claude/hooks/` by relative path, so
each depended on two things it did not control: the file surviving in the working
tree, and the harness running from the repository root. When either failed, the
shell died before reaching the `exit 0` that keeps a hook silent — every turn
ended in `No such file or directory` and no sync ran.

Both now invoke the installed CLI directly and write nothing into the repository:

```
command -v modernpath >/dev/null 2>&1 && ( modernpath factory sync --if-quiescent --trigger Stop >/dev/null 2>&1 & ) ; exit 0
command -v modernpath >/dev/null 2>&1 && modernpath context --hook UserPromptSubmit 2>/dev/null || echo '{}'
```

`command -v` makes a missing CLI a silent no-op. Install **migrates**: it deletes
the legacy scripts and strips settings entries that name them — previously a
reinstall no-oped on broken wiring, because the legacy entry counted as
"installed".

### `modernpath context --hook` — 11–28s becomes ~1s

The hook used to spend 0.8s deciding relevance and then 9–27s on a reasoning
model writing an essay, for a consumer that has grep and file reads.

- **Specific questions cost no synthesis at all** — ranked file paths with their
  purpose. Measured p50 **1.2s**.
- **Only high-level questions buy prose**, from a capped fast model, and only
  when something was actually retrieved. Measured **2.1s**. A model asked to
  summarize an empty result answers from the question alone.
- **Irrelevant prompts stay silent** in 0.8s.
- Decomposed queries are searched **separately and merged**; the old code joined
  them into `"Core.Sync Also: epic op Also: Core.Sync epic op"` and handed that to
  a model as if it were a question.
- The searches run **alongside** the relevance gate rather than after it.
- Every response carries the workspace's own state — release, requirement counts,
  open gates and how many are approvals, next READY rows — computed from stored
  state with no model call.

### The hook says what happened

`.modernpath/context-hook.log` gets one line per run, naming the **reason**:

```
2026-08-11T07:59:35Z · delivered · 1.8s · 2731 bytes
2026-08-11T08:43:47Z · failed · 1.4s · server error 400 {"error":"..."}
2026-08-11T07:59:36Z · empty · 0.7s · not relevant
```

Previously every failure — missing config, auth error, non-200, decode failure,
timeout — rendered as an empty result and read as "nothing relevant", so a broken
hook and a quiet one were indistinguishable. A client-side deadline (default 8s,
`--deadline`) returns empty rather than holding the turn.

### `modernpath hooks doctor`

The hooks call the CLI by bare name, so PATH decides which build runs. A second,
older copy earlier in PATH silently downgrades every hook run while still exiting
0 — which happened here, with a tested deadline and log that never executed.

`hooks doctor` reports which binary wins, which are shadowed, whether both hook
families are wired, whether any entry still points at a removed script, and
recent failures from the log.

### `modernpath install` tells agents where the knowledge is

The managed `AGENTS.md` block now carries a **Codebase knowledge** section: the
API finds (`modernpath search`, or the context hook), the local export reads
(`.modernpath/modernpath/…`), `modernpath docs sync` when the export is absent,
and the API wins when they disagree — the export is a cache. In the managed block
rather than a skill, because skills are Claude-only and Codex reads `AGENTS.md`
where it does not read `CLAUDE.md`.

### `scripts/install-local.sh`

Writes **every** copy of `modernpath` on PATH rather than only the usual homes —
it previously missed `~/.local/bin`, which is exactly where the shadowing copy
lived. Stamps commit and build time into the version string, and warns loudly
about copies it could not write instead of skipping them silently.

---

Earlier releases are catalogued in the GitHub releases for `ModernPath/cli`.
