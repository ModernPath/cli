# Requirement-driven delivery

Reusable agent instructions for delivering software through human gates,
user/system requirements, red-first TDD, and V-model traceability.

The process traces:

```text
EPIC -- optionally groups --> UR and/or SR
UR -> acceptance scenario -> TEST_CASE -> TEST_RESULT
UR acceptance scenario -- may require --> SR
SR -> CODE -> TEST_CASE -> TEST_RESULT

Each selected trace passes planning, technical review, human entry, red-first
evidence, implementation, cleanup, verification, delivery, reconciliation,
and human completion.
```

Product repositories hold authoritative process records in one selected store,
alongside specifications, code, tests, evidence, and attributable gate answers.
`file-state/` defines the canonical serialization of those records: a
store-backed repository materializes the shapes as uncommitted snapshots and
projections, a file-backed repository versions them as the store itself — one
or the other, never both at once.

## Read first

1. [`AGENTS.md`](AGENTS.md) — binding agent rules.
2. [`PROCESS.md`](PROCESS.md) — the complete canonical process.
3. [`skills/rdd-deliver/SKILL.md`](skills/rdd-deliver/SKILL.md) — complete-loop
   orchestration; use the other `skills/rdd-*/SKILL.md` files for explicitly
   bounded passes.

## Process authority

[`PROCESS.md`](PROCESS.md) is the single source for process semantics. Skills
apply that model; `file-state/` serializes its records without redefining it.

## Repository contents

| Path | Purpose |
|---|---|
| `AGENTS.md` | shared agent policy and canonical entry point |
| `PROCESS.md` | complete canonical process |
| `CLAUDE.md` | root compatibility entry required for Claude discovery |
| `skills/` | full-loop orchestration plus focused procedures for discovery, planning, review, building, triage, completion, and as-built verification; corpus adoption for codebases without requirement records (`rdd-reverse-engineer`); a shared document/citation auditing utility (`rdd-audit`) |
| `file-state/` | canonical serialization shapes for Epic, requirement, gate, work-selection, and backlog/gap records |

## Distribution

Consuming repositories do not vendor this repository. A distribution tool —
for ModernPath workspaces, the `modernpath` CLI — embeds a byte-identical
snapshot of these files and installs it under `.modernpath/rdd/`, so in a
consuming repository the canonical process resolves at
`.modernpath/rdd/PROCESS.md` with every `skills/rdd-*/SKILL.md` beside it.
The installer also registers the skills and hooks with each agentic platform
in use (for example `.claude/skills/`) and writes managed instruction blocks
pointing agents at the installed paths.

In a store-backed consuming repository the tooling additionally materializes
two uncommitted projections (for ModernPath workspaces,
`.modernpath/working-set/` and `.modernpath/your-move/`): the session working
set — shape files pulled on demand, each stamped with its source-store
revision — and the pending human-decision queue, which lists only `OPEN`
human gates with current passing prerequisites. Neither is an authority; see
`PROCESS.md` "State records and reconciliation".

## Process maintenance

Changes to lifecycle, status meanings, trace relationships, gate requirements,
evidence rules, or record ownership belong in `PROCESS.md`. Validate internal
links and search the skills and flat-file shapes for competing authority
statements whenever it changes.

License: MIT.
