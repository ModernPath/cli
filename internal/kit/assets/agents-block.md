## Read this first (binding, all agents)

The shared delivery process is installed by the `modernpath` CLI from a
versioned `req-driven-dev` release snapshot. Read and follow these before
starting product work:

1. `.modernpath/rdd/AGENTS.md` — binding shared agent rules;
2. `.modernpath/rdd/PROCESS.md` — the canonical process: authority, items,
   traces, lifecycles, gates, planning, development, evidence, completion,
   records, and reconciliation. It names phases and required exits and never
   names a verb. In a store-backed workspace every phase it describes is
   driven through the `modernpath` CLI, and the `mp-process-cli` skill
   (`.claude/skills/mp-process-cli/SKILL.md`) is the verb sequence for each
   phase — read it with this list, not as background. In a file-backed
   workspace the `file-state/` ledgers are edited directly instead;
3. the procedures under `.modernpath/rdd/skills/` — the path is not
   agent-specific: follow them as checklists whatever agent you are (Claude
   Code also discovers byte-identical copies under `.claude/skills/`).
   Enter a session with `rdd-start`; `rdd-deliver` runs the complete loop;
   use a focused one when the request explicitly ends at a single pass:
   - `rdd-start` — session entry: verify the store binding and active
     release, take or prompt for the scope, route to the needed phase;
   - `rdd-discover` — turn raw requirements into canonical design sources;
   - `rdd-plan` — derive requirements and select the next trace;
   - `rdd-entry-review` — the human entry gate before red-first evidence;
   - `rdd-build` — run a trace red-first through implementation;
   - `rdd-verify` — verify entered work requiring tests through the normal
     RED/GREEN contract and promote only what goes green;
   - `rdd-cold-review` — review a change without its authoring context;
   - `rdd-completion-review` — audit evidence before the completion gate;
   - `rdd-triage` — route discoveries, blockers and deferrals;
   - `rdd-deliver` — land, reconcile and close the loop;
   - `rdd-autopilot` — a sprint under a human's autopilot grant: product
     questions asked without waiting, process refusals cleared or logged,
     one review and one completion page at sprint end;
   - `rdd-reverse-engineer` — establish a source-scoped as-built baseline or
     propose `DERIVED` additions; ask for the mode, preserve existing requirements,
     and verify the persisted graph against explicit denominators;
   - `rdd-reverse-engineer-verify` — inspect complete existing assertion,
     execution and repository integration proof for pending baselines;
   - `rdd-reverse-engineer-accept` — one exact human acceptance and guarded
     DONE application with receipt, keeping compliance approval unchanged;
   - `rdd-audit` — shared utility the other passes invoke: resolve citations,
     diff inventories both directions, judge whether a measurement is real;
4. `.modernpath/rdd/file-state/` — the canonical serialization shapes for
   Epic, requirement, gate, work-selection, and backlog/gap records: a
   store-backed workspace materializes them as uncommitted snapshots, a
   file-backed repository versions them as the store.

Three `.claude/skills/` entries are shipped by the `modernpath` CLI rather
than by the process package: `mp-process-cli` (how the CLI records each phase
of the loop — verb sequences, the work-selection and fingerprint models, the
refusal glossary), `mp-knowledge-search` (find and read the local
analysis, with live API reads when needed) and `rdd-ledger` (the compatibility
adapter for the file `tasks/` ledger format). The ledger skill is installed only while the workspace
is file-backed; under `process/store-backed.md` its subject is retired, so
`modernpath install` withholds it and `install --check` reports a copy found
there as drift. The delegated cold review runs in the agent definition the
CLI installs at `.claude/agents/rdd-cold-reviewer.md` (Read, Grep, Glob; no
shell); it returns findings and a verdict, and the session records them. The former workspace `rdd-audit` and `rdd-reverse-engineer` are
now package skills — the citation auditor installs at
`.modernpath/rdd/skills/rdd-audit/audit-citations.mjs`.

Project instructions may add stack, commands, architecture, domain, and safety
rules. On conflict the process wins (see "Instruction ownership" in
`.modernpath/rdd/AGENTS.md`); a genuine conflict is a defect to report — fix the
canonical `req-driven-dev` source and publish a new CLI rather than creating a
local variant.

## What the loop covers (binding, all agents)

**The loop is for product development.** The dividing line is who the change
is for. If it changes what a customer can do or observe, it is product work
and the loop applies — including infrastructure work whose *purpose* is
customer-visible behavior. If it only changes how the repository is built,
run or reviewed, or how its agents are instructed, it is meta-work.

**Meta-work stays out of the loop and its records.** Repository layout and
ownership, CI/CD lanes, deployment identity and infrastructure, developer
tooling and scripts, agent instructions, and the installed process material
(`AGENTS.md`, `CLAUDE.md`, `.claude/`, the process snapshot and skills under
`.modernpath/rdd/`) are meta-work unless the test above makes them product
work; they are not requirements in themselves. Do that work as a regular
change — a pull request where the project uses them — and state in its
description why the loop does not apply. Two things are not this
repository's meta-work: the `file-state/` ledgers where they are the store
(a file-backed workspace), and friction with the `modernpath` CLI itself,
which is a tooling gap filed through the channel named below.

## Where the loop stands (binding, all agents)

Session entry owns what to do next — `.modernpath/rdd/AGENTS.md` and
`rdd-start` carry that routing, including its preflight. Being asked what to
work on next, where the loop stands, or what is waiting on a decision is
session entry too: enter with `rdd-start` and answer from the reads below.
This section only names the reads that routing needs:

- `modernpath process next` — the entry read: where the delivery loop stands
  and the skill to run;
- `modernpath your-move` — the pending human decisions with what each one
  holds (`--more` for the next page, `--queue` for everything in scope,
  `--domain <name>` for one domain);
- `modernpath working-set pull <id>…` — materialize named items as readable
  files; `--scope` (a flag, not a value) pulls the current work selection's
  scope instead, and needs a selection to already exist.

Whether this workspace's process state lives in the store or in the file
ledgers is declared by `process/store-backed.md`. `modernpath process next`
and `modernpath status` disclose it in a line of their own, and so does the
SessionStart brief; `modernpath your-move` does not, so do not read its silence
as an answer.

**Store-backed — the declaration is present.** The files it names are retired
and are not recreated to record something. Every write is single-record and
actor-attributed, legality is enforced server-side, and a stale fingerprint
conflicts instead of overwriting. The write channels, the exact verb sequence
for each phase, the work-selection and fingerprint models and the refusal
glossary are the `mp-process-cli` skill
(`.claude/skills/mp-process-cli/SKILL.md`); every verb, flag and default of
the installed binary is `.modernpath/cli-reference.md`, rendered from the
binary by `modernpath install` (`--help` prints the same text). The retired `process/releases.md` is replaced by the answered
`release_selection` gate `GATE-RELEASE-<slug>`, which carries the active
release and its `USER:` source and which the `rdd-start` release preflight
reads.

Backlog, gap and tooling-gap records (`BACKLOG-…`, `GAP-…`, `BACKLOG-TOOL-<n>`)
are store records like every other (`PROCESS.md` §State records and
reconciliation): read one with `modernpath working-set pull <id>`, file a
tooling gap with `modernpath feedback "<line>"`, and change a disposition with
`modernpath author update --kind backlog … --source USER:…`. List them with
`modernpath process backlog list [--kind backlog|gap|tooling] [--disposition
<word>]`; an id comes from that list or the store's other reads —
`your-move`, the feed, the record that routed it — never from a plan file, a
handover or a note, which are projections and carry no disposition the store
does not.

ModernPath tooling feedback belongs only to the ModernPath workspace,
never to a customer project's backlog or local tooling-gap file. `modernpath
feedback` requires a ModernPath tenant credential and verifies that the current
checkout is bound to the ModernPath system. It refuses customer workspaces
without changing the binding. If unavailable, report the gap in the session;
do not substitute `author backlog --kind tooling` in the customer store.

**File-backed — no declaration.** The ledger files are the store and are
edited in place. `modernpath author` is not the write path there.

`git status`, branch and pull-request lists, and the working tree describe the
repository, not the loop, and are never the source for what to do next.
Repository and tooling work that carries no requirement record is reported
separately and labelled, never in place of the queue.

## Codebase knowledge (binding, all agents)

This repository is analyzed into a **knowledge core** — per-subsystem
architecture, module docs, data model, patterns. In a bound workspace, run
`modernpath process prepare-inputs` before sourced work; it checks the delivery
context and reports when local documents were last synced and when server
documents were last updated. It does not refresh the export. Run `modernpath
docs sync` explicitly when a refresh is needed.

Search locally first, in this order: the repository for the requirement id,
its code and tests (`rg -n "<requirement id>" .`); `modernpath working-set pull
<id>` for the requirement records; the docs export under
`.modernpath/<system-slug>/` — `rg -n "authentication" .modernpath/<system-slug>/`
followed by `cat <matching-document>` needs no further ModernPath API call; then
`modernpath ask "<question>"` for why and how questions the local search cannot
answer.

Use `modernpath search "<terms>"` or `modernpath read-doc --id=<id>` when the
export lacks the material or a live answer is needed. The context hook also
provides live pointers. **If the export and the API disagree, the API is right**
— the export is a cache, and a stale cache answers confidently. If only the
export is missing, `modernpath docs sync` can refresh it.

Cite what you actually used as a `DOC:` source. Generated docs describe modules
rather than lines, so verify a specific claim against the code before recording
it as fact.
