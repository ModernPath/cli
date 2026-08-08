<!-- TOOL-OWNED. Installed by `modernpath install`.
     Secondary channel: GitHub Copilot reads this path; Claude Code and Codex do
     not, so nothing here affects them. -->

This repository is built with the ModernPath requirement-driven process, and it
governs. Read these before starting any task, and follow them:

- `.claude/rdd/PROCESS.md` — the process: non-negotiables, how work enters build
  (fast lane vs epic), red-before-green in both loops, evidence, status hygiene,
  approval gates.
- `.claude/rdd/platform.md` — ModernPath platform integration (sync, releases, gates).
- `AGENTS.md` — this project's stack, commands, repository shape and domain rules.

The short version, which is not optional:

1. Work starts from a requirement with GIVEN/WHEN/THEN acceptance criteria, entered
   through `WORKLIST.md`. Never start implementation outside the work-list.
2. Write the failing test first, tagged with its `REQ-<CTX>-NNN` id, at both the
   unit and the scenario level. Red before green.
3. Nothing is done without requirement, tests and code cross-linked, plus a
   reconciled ledger row and recorded human approval.
4. A requirement's status lives in three places — dashboard row, detail block and
   the `Totals:` line. Changing one is a bug.
5. Never resolve a product, scope or architecture decision by assumption; those
   need a `USER:<date>` source.

Where this file and `.claude/rdd/PROCESS.md` differ, PROCESS.md wins.
