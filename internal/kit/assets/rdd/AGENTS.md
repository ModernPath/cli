# Requirement-driven delivery — agent instructions

This repository defines a reusable requirement-driven delivery process. It
contains canonical process instructions, procedural skills, and flat-file
serialization shapes; it does not contain a project's live delivery state.

## Instruction ownership

- [`PROCESS.md`](PROCESS.md) defines the canonical process, including authority,
  items, traces, lifecycles, gates, planning, development, evidence,
  completion, records, and reconciliation.
- [`skills/`](skills/) contains focused procedures that apply the process.
- [`file-state/`](file-state/) contains the canonical serialization shapes for
  Epic, requirement, gate, work-selection, and backlog/gap records.
- Project instructions own only project-specific rules such as architecture,
  repository topology, commands, environments, and test gates. They reference
  rather than redefine the process. They name the project's sanctioned tool
  for the process store and its channel for surfacing tooling gaps; the rule
  that binds both is `PROCESS.md` "State records and reconciliation".
- `CLAUDE.md` files are compatibility pointers; they do not override these
  instructions.

If project instructions conflict with a process rule, stop the affected
transition and report the conflict rather than creating a silent local variant.

## Required reading

Before planning, changing, reviewing, or delivering product work — and
before answering what to work on next, where the loop stands, what is waiting
on a decision, or what is blocked:

1. read the project's root `AGENTS.md` for project-specific rules;
2. read this file and `PROCESS.md`;
3. enter the session with `skills/rdd-start/SKILL.md` — it verifies the store
   binding and active release before any selection; then use
   `skills/rdd-deliver/SKILL.md` for end-to-end delivery, or the applicable
   focused `skills/rdd-*/SKILL.md` when the request explicitly ends at one
   process pass; use `skills/rdd-verify/SKILL.md` for reverse-engineered
   `PENDING_VERIFICATION` rows, and `skills/rdd-reverse-engineer/SKILL.md` to
   adopt a codebase that has no requirement corpus yet;
   `skills/rdd-audit/SKILL.md` is a shared utility other passes invoke, not a
   phase;
4. read the relevant product sources, requirement records, optional epic, and
   active work-selection record.

Being asked what to do next is session entry, not a shortcut past it. The
answer is the store's pending human decisions and routed work, read through
`skills/rdd-start/SKILL.md`. Version control, change lists, and the working
tree describe the repository rather than the loop, and never answer that
question. A project's root `AGENTS.md` names the concrete read for its own
store; if it names none, that omission is the report.

## Binding process

Follow `PROCESS.md`. Skills apply it and `file-state/` serializes its records;
neither redefines its rules.
