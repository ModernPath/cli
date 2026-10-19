## Read this first (binding, all agents)

The shared delivery process is installed by the `modernpath` CLI from a
versioned `req-driven-dev` release snapshot. Read and follow these before
starting product work:

1. `.modernpath/rdd/AGENTS.md` — binding shared agent rules;
2. `.modernpath/rdd/PROCESS.md` — the canonical process: authority, items,
   traces, lifecycles, planning, development, evidence and human completion. It names phases and required exits and never
   names a verb;
3. the procedures under `.modernpath/rdd/skills/` — the path is not
   agent-specific: follow them as checklists whatever agent you are (Claude
   Code also discovers byte-identical copies under `.claude/skills/`).
   Enter a session with `rdd-start`; `rdd-deliver` runs the complete loop;
   use a focused one when the request explicitly ends at a single pass:
   - `rdd-start` — session entry: read the selected Epic, saved plan and human approvals,
     confirm scope and route to the needed phase;
   - `rdd-discover` — turn raw requirements into canonical design sources;
   - `rdd-plan` — save the Epic plan and exact proposed requirement content;
   - `rdd-entry-review` — obtain human implementation approval for the saved plan;
   - `rdd-build` — implement the approved behavior red-first;
   - `rdd-verify` — verify the implementation with actual test evidence;
   - `rdd-cold-review` — review a change without its authoring context;
   - `rdd-completion-review` — review evidence for human completion approval;
   - `rdd-triage` — route discoveries, blockers and deferrals;
   - `rdd-deliver` — deliver the approved Epic through verification and human completion;
   - `rdd-autopilot` — a sprint under a human's autopilot grant: product
     questions asked without waiting, process refusals cleared or logged,
     one review and one completion page at sprint end;
   - `rdd-reverse-engineer` — read existing behavior and record requirements
     with typed traces to source files;
   - `rdd-reverse-engineer-verify` — verify existing behavior with criterion-level execution evidence;
   - `rdd-reverse-engineer-accept` — obtain explicit human acceptance and publish verified existing behavior;
   - `rdd-audit` — shared utility the other passes invoke: resolve citations,
     diff inventories both directions, judge whether a measurement is real.

The CLI ships `mp-knowledge-search` for reading local analysis and live
knowledge documents. The delegated cold review runs in
the agent definition the CLI installs at `.claude/agents/rdd-cold-reviewer.md`
(Read, Grep, Glob; no shell); it returns findings and a verdict, and the
session records them. The reviewer is installed for Claude Code
(`.claude/agents/*.md`) and Codex (`.codex/agents/*.toml`), which Cursor also
reads; its role is written for any harness. The former workspace `rdd-audit` and `rdd-reverse-engineer` are
now package skills — the citation auditor installs at
`.modernpath/rdd/skills/rdd-audit/audit-citations.mjs`.

Project instructions may add stack, commands, architecture, domain, and safety
rules. On conflict the process wins (see "Instruction ownership" in
`.modernpath/rdd/AGENTS.md`); a genuine conflict is a defect to report — fix the
canonical `req-driven-dev` source and publish a new CLI rather than creating a
local variant.

## Codebase knowledge (binding, all agents)

This repository is analyzed into a **knowledge core** — per-subsystem
architecture, module docs, data model, patterns. Run `modernpath docs sync` explicitly when the local export needs refreshing.

Search locally first: search the repository for the requirement id, code and
tests; then search the docs export under
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
