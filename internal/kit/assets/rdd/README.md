# The RDD kit

<!-- TOOL-OWNED. Installed by `modernpath install`. -->

Requirement-driven development, installable into any repository. This directory
and the root `CLAUDE.md` are owned by the ModernPath CLI and replaced wholesale
on upgrade. Your project's own instructions live in `AGENTS.md` and
`.claude/rules/`, which the installer never touches.

## What is here

| File | Layer | Always in context |
|---|---|---|
| `PROCESS.md` | The method: non-negotiables, build loop, V-model epics, DoD | yes |
| `platform.md` | ModernPath server integration: sync, releases, gates | yes |
| `../skills/rdd-*/SKILL.md` | Procedures: build loop, planning, discovery, ledger | on demand |

The method works standalone. Removing the `@.claude/rdd/platform.md` import from
`CLAUDE.md` leaves a fully functional process with no server dependency.

## Ownership

```
CLAUDE.md              tool-owned    precedence + imports. Never edit.
.claude/rdd/**         tool-owned    the process. Never edit.
.claude/skills/rdd-*   tool-owned    the procedures. Never edit.

AGENTS.md              project-owned stack, commands, boundaries, domain rules
.claude/rules/**       project-owned path-scoped conventions (tests, CI, deploy)
.claude/skills/<own>   project-owned your team's procedures
```

Editing a tool-owned file loses the edit at the next upgrade **and** forks you
from the process everyone else runs. If something in the process is wrong for
your project, that is worth fixing centrally — raise it.

## Why it loads this way

Three mechanics drive the layout, and each one was a design constraint rather
than a preference:

1. **Only the root `CLAUDE.md` is loaded at session start** and re-injected after
   context compaction. Files in subdirectories load lazily — when an agent happens
   to read something nearby — and do not survive compaction. So the process is
   `@`-imported from the root rather than left in a directory to be discovered.
2. **Claude Code reads `CLAUDE.md`, not `AGENTS.md`.** A repository whose rules
   live only in `AGENTS.md` is running with those rules out of context unless an
   agent opens the file. The root import fixes that, and keeps one instruction
   set for every agent that reads either name.
3. **Imports do not reduce context cost** — everything imported is present in
   every session. That is why the always-on layer is deliberately small and
   procedures are skills, which load only when invoked or relevant.

## Adding project rules

`AGENTS.md` is yours. For rules that only matter for some files, prefer a
path-scoped rule so it costs nothing until it applies:

```markdown
---
paths:
  - "src/api/**/*.ts"
---
# API conventions
- All endpoints validate input at the boundary.
```

## Upgrading

`modernpath install` rewrites `CLAUDE.md`, `.claude/rdd/**` and the `rdd-*`
skills, and leaves everything else alone. To see what would change before
committing, run it on a clean tree and read the diff.
