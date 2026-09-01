## Read this first (binding, all agents)

The shared delivery process is installed by the `modernpath` CLI from a
versioned `req-driven-dev` release snapshot. Read and follow these before
starting product work:

1. `.modernpath/rdd/AGENTS.md` — binding shared agent rules;
2. `.modernpath/rdd/PROCESS.md` — the canonical process: authority, items,
   traces, lifecycles, gates, planning, development, evidence, completion,
   records, and reconciliation;
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

Two `.claude/skills/` entries are ModernPath tooling rather than process, and
remain current: `mp-knowledge-search` (query the platform's analysis of this
repository before reading it by hand) and `rdd-ledger` (the project-local
compatibility adapter for the `tasks/` ledger format until the ledger→server
import). The former workspace `rdd-audit` and `rdd-reverse-engineer` are now
package skills — the citation auditor installs at
`.modernpath/rdd/skills/rdd-audit/audit-citations.mjs`.

Project instructions may add stack, commands, architecture, domain, and safety
rules. On conflict the process wins (see "Instruction ownership" in
`.modernpath/rdd/AGENTS.md`); a genuine conflict is a defect to report — fix the
canonical `req-driven-dev` source and publish a new CLI rather than creating a
local variant.

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
