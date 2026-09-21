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
   - `rdd-verify` — write the missing test for reverse-engineered
     `PENDING_VERIFICATION` rows and promote only what goes green;
   - `rdd-cold-review` — review a change without its authoring context;
   - `rdd-completion-review` — audit evidence before the completion gate;
   - `rdd-triage` — route discoveries, blockers and deferrals;
   - `rdd-deliver` — land, reconcile and close the loop;
   - `rdd-reverse-engineer` — adopt a codebase with no requirement corpus:
     derive `DERIVED` candidates against explicit denominators and hand
     confirmed scope to the loop;
   - `rdd-audit` — shared utility the other passes invoke: resolve citations,
     diff inventories both directions, judge whether a measurement is real;
4. `.modernpath/rdd/file-state/` — the canonical serialization shapes for
   Epic, requirement, gate, work-selection, and backlog/gap records: a
   store-backed workspace materializes them as uncommitted snapshots, a
   file-backed repository versions them as the store.

Three `.claude/skills/` entries are shipped by the `modernpath` CLI rather
than by the process package: `mp-process-cli` (how the CLI records each phase
of the loop — verb sequences, the work-selection and fingerprint models, the
refusal glossary), `mp-knowledge-search` (query the platform's analysis of
this repository before reading it by hand) and `rdd-ledger` (the compatibility
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
`modernpath author update --kind backlog … --source USER:…`. No verb lists
them yet; until one does, an id comes from the store's own reads —
`your-move`, the feed, the record that routed it — never from a plan file, a
handover or a note, which are projections and carry no disposition the store
does not.

**File-backed — no declaration.** The ledger files are the store and are
edited in place. `modernpath author` is not the write path there.

`git status`, branch and pull-request lists, and the working tree describe the
repository, not the loop, and are never the source for what to do next.
Repository and tooling work that carries no requirement record is reported
separately and labelled, never in place of the queue.

## Codebase knowledge (binding, all agents)

This repository is analyzed into a **knowledge core** — per-subsystem
architecture, module docs, data model, patterns. There are two ways to reach it
and they do different jobs:

- **To find** something: `modernpath search "<terms>"`, or let the context hook
  inject ranked pointers into your prompt (`modernpath hooks install`). Both ask
  the API, so both are current.
- **To read** what you found: open it from the local export under
  `.modernpath/modernpath/…` — free, instant, and the whole document rather than
  an excerpt. `modernpath read-doc --id=<id>` fetches anything not exported.

If `.modernpath/modernpath/` is missing, the export has not been run here:
`modernpath docs sync`. **If the export and the API disagree, the API is right** —
the export is a cache, and a stale cache answers confidently.

Cite what you actually used as a `DOC:` source. Generated docs describe modules
rather than lines, so verify a specific claim against the code before recording
it as fact.
