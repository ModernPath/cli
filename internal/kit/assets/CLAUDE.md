<!-- TOOL-OWNED FILE — installed and upgraded by `modernpath install`.
     Do not add project-specific content here; it belongs in AGENTS.md and
     .claude/rules/. Edits to this file are overwritten on upgrade. -->

This repository is built with the ModernPath requirement-driven process. Read
the imports below before starting any task.

@.claude/rdd/PROCESS.md

@.claude/rdd/platform.md

@AGENTS.md

## Precedence

The process above governs how work is planned, built, evidenced and approved.
It is not advisory and it is not overridden by repository-local convention.

`AGENTS.md` and `.claude/rules/` are **project-owned** and authoritative for the
project's own subject matter — and only for it:

| Project-owned | Process-owned (do not override) |
|---|---|
| Tech stack, frameworks, versions | How a requirement enters build |
| Build, test and run commands; ports | What "done" means |
| CI/CD, deployment, environments | Red-before-green in both loops |
| Repository shape, module boundaries | Traceability: requirement ↔ test ↔ code |
| Code style and naming | Status hygiene across all three places |
| Architecture decisions and their records | Decision sourcing (`USER:` for product/scope calls) |
| Domain rules and invariants | Deferral and discovery capture |

Where the two appear to conflict, the process wins and the conflict is a defect
worth reporting. If the project genuinely needs different process behaviour, that
is a change to the process — raise it, don't work around it locally.

## Where to put things

- A rule about **this codebase** (stack, commands, boundaries, domain) → `AGENTS.md`.
- A rule that applies only to **certain files** (test conventions, CI config,
  a subsystem's patterns) → `.claude/rules/<topic>.md` with `paths:` frontmatter,
  so it loads only when those files are touched.
- A **repeatable procedure** rather than an always-true rule → a skill in
  `.claude/skills/`.
- A change to **how we build anything** → the process, not here.
