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

On conflict the process wins (see the precedence table in `CLAUDE.md`); a
genuine conflict is a defect to report, not to work around.
