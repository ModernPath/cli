# Requirement-driven delivery

Reusable agent instructions for delivering software through explicit human
implementation and completion approvals, sourced requirements, red-first
engineering and traceable evidence.

The process traces:

```text
EPIC -> required UR/SR requirements
UR -> acceptance scenario -> TEST_CASE -> TEST_RESULT
UR acceptance scenario -- may require --> SR
SR -> CODE -> TEST_CASE -> TEST_RESULT
```

Normal delivery uses the human-selected Epic, saves and independently reviews
its plan and proposed content, and obtains implementation approval. That approval
locks requirements to one user and Epic and starts implementation. Review keeps
the published content and lock unchanged. Human completion approval atomically
publishes approved content, raises revisions, retains immutable accepted versions,
records audit and releases ownership. Reverse engineering records requirements
and file traces directly; it has no hash, source capture or as-built proof
prerequisite.

## Read first

1. [`AGENTS.md`](AGENTS.md) — shared agent rules and session entry.
2. [`PROCESS.md`](PROCESS.md) — canonical product-delivery process.
3. [`skills/rdd-start/SKILL.md`](skills/rdd-start/SKILL.md) — session entry and
   scope routing.
4. [`skills/rdd-deliver/SKILL.md`](skills/rdd-deliver/SKILL.md) — complete-loop
   orchestration; use a focused skill when a request ends at one pass.

## Process authority

[`PROCESS.md`](PROCESS.md) is the process authority. Skills apply it using the
consuming project's sanctioned process interface; they do not define CLI
verbs or a second record format. The server is authoritative for Epics,
requirements, saved plans, exact approved content and human decisions. This
repository has no file-backed process ledger.

## Repository contents

| Path | Purpose |
|---|---|
| `AGENTS.md` | Shared agent policy and canonical entry point |
| `PROCESS.md` | Canonical process authority and engineering workflow |
| `skills/` | End-to-end orchestration, autopilot, focused delivery passes, and shared Trace and evidence audit |

## Distribution

Consuming repositories do not vendor this repository. A distribution tool
embeds a versioned snapshot of these instructions and installs it into the
workspace. Platform-specific metadata, compatibility files, registration and
hooks belong to that distribution tool; this repository supplies shared
instructions and skills.

Product documents remain ordinary repository sources and use the consuming
project's documented documentation workflow. They are not process state.

## Process maintenance

Changes to item identity, traces, lifecycle, human approvals, evidence or
completion belong in `PROCESS.md`. Check internal links and search skills for
competing authority statements. Keep reverse-engineering skills and their
shared audit utility aligned with the process sections they consume.

License: MIT.
