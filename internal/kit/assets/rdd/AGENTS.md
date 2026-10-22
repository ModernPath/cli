# Requirement-driven delivery — agent instructions

This repository defines a reusable product-delivery process and its focused
agent procedures. The project's CLI is the command-line tool named in its
instructions for reading and updating delivery records. In modernpath-v1, it
is `modernpath`. The server holds the authoritative Epics, requirements,
saved plans, exact proposed content and human approvals. This repository
defines no local delivery ledger.

## Instruction ownership

- [`PROCESS.md`](PROCESS.md) defines product authority, item identity,
  traces, Epic scope, implementation approval, engineering, evidence, human
  completion and existing-behavior acceptance.
- [`skills/`](skills/) contains focused procedures that apply that process.
  They use the consuming project's CLI and do not prescribe or invent its
  commands.
- Project instructions own project-specific architecture, repository layout,
  commands, environments and test gates. They name the CLI and the channel
  for reporting a tooling gap.
- `skills/rdd-reverse-engineer-verify/SKILL.md` verifies PROPOSED requirements
  against existing behavior and actual test evidence; it does not change their
  lifecycle.
- `skills/rdd-reverse-engineer-accept/SKILL.md` obtains explicit human
  acceptance and uses the supported atomic PROPOSED-to-DONE operation for
  verified existing behavior.

If the CLI lacks a required read or write, report that gap. Do
not create a local ledger, direct transport, or another authority as a
workaround.

## Required reading

Before planning, changing, reviewing or delivering product work — and before
answering what to work on next — read the project's root `AGENTS.md`, this
file and `PROCESS.md`. Enter with `skills/rdd-start/SKILL.md`; use
`skills/rdd-deliver/SKILL.md` for end-to-end work or the focused skill when the
request ends at one pass. Use `skills/rdd-autopilot/SKILL.md` only under an
explicit human grant. Use `skills/rdd-reverse-engineer/SKILL.md` for
recording requirements and their traces to source files.
Use `skills/rdd-reverse-engineer-verify/SKILL.md` and
`skills/rdd-reverse-engineer-accept/SKILL.md` to verify and explicitly accept
existing behavior without an implementation Epic when no behavior or test
change is requested. Route any required code or test change through the normal
Epic delivery path.
`skills/rdd-audit/SKILL.md` is a shared utility invoked by other passes, not a phase.

Read relevant product documents and inspect their cited code and tests. A
valid Trace establishes a relationship; inspect its endpoints and source
material to establish support for a claim. Keep source text, code/test links and
process identity distinct.

## Product work and meta-work

The loop covers changes to what a customer can do or observe. Repository
layout, build and review tooling, CI, deployment identity and agent
instructions are meta-work unless their purpose changes customer-visible
behavior. Do that work as a regular repository change and explain why the
product loop does not apply. Do not create process records for meta-work.

## Authority and tooling gaps

For changed behavior or tests, the human selects an Epic and approves its exact
saved plan and proposed content for implementation, atomically locking its
requirements to one user and that Epic, and separately approves completion.
For an explicit existing-behavior acceptance pass, the human accepts the exact
PROPOSED requirements and complete passing evidence through the supported
atomic operation; active Epic-owned requirements stay on the Epic path. Agents
establish facts, plan, review, implement approved work and report results. A human retains
merge, deployment and release authority. Never infer approval from silence,
code, test results, commits, a passing review or a repository status.

Use only the project's CLI for process reads and
writes. If it lacks a needed read or transition, report the gap through the
project's named channel. Never read credentials or private configuration,
call its transport directly, or write process state by hand. Ordinary product
documents remain in their project repository and use its documented
documentation workflow.

## Communication and code style

Be terse and factual. Ground technical claims in sources, code, tests or
observed runs. Prefer simple, robust changes. Preserve red-first test evidence,
run the focused and proportional regression checks, and report commands and
results. Read back accepted requirement versions after completion approval or
existing-behavior acceptance;
verify Git, PR and deployment state separately before making those claims.

## Repository checks

Process edits require review of internal links and competing instruction
statements, and `git diff --check`. Inspect Traces and their supporting
records through the consuming project's CLI; this repository
has no process store or local evidence checker.
