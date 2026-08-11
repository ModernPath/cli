## Read this first (binding, all agents)

The development process lives outside this file and **governs** it. Claude Code
loads it automatically through `CLAUDE.md`; every other agent — Codex, and any
tool that reads `AGENTS.md` — MUST read these before starting any task:

1. `.claude/rdd/PROCESS.md` — the requirement-driven process: non-negotiables,
   how work enters build (apply its spec-worthy test before choosing fast lane
   or epic), red-before-green in both loops, evidence, status hygiene, approval.
2. `.claude/rdd/platform.md` — ModernPath platform integration: sync, releases,
   gates.
3. `.claude/skills/rdd-build-loop/SKILL.md` and `.claude/skills/rdd-ledger/SKILL.md`
   — the procedures for running a slice and for ledger format and status
   hygiene. Not Claude-specific: follow them as checklists whatever agent you are.
4. `.claude/skills/rdd-reverse-engineer/SKILL.md` — for a repository that has code
   but no ledgers yet: derive the contexts, requirements and their real
   verification status from the analysis and the code. Also a checklist, whatever
   agent you are.

On conflict the process wins (see the precedence table in `CLAUDE.md`); a
genuine conflict is a defect to report, not to work around.

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
