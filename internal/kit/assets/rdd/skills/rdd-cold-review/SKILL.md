---
name: rdd-cold-review
description: Independently audit a requirement planning packet before implementation entry. Use after technical reconnaissance and SR enrichment to review trace and scope alignment, affected code and data flow, contracts, failure behavior, feasibility, boundaries, and test strategy from a context separate from plan authoring. Produces technical findings and a trace-gate verdict; never grants human approval.
---

# Run a cold technical review

Start from a context independent of the planning-authoring conversation. Read
the project `AGENTS.md`, canonical `PROCESS.md` (`.modernpath/rdd/PROCESS.md`
in a consuming repository), versioned product sources, selected requirements,
optional epic/specifications, technical reconnaissance, and repository state at
the recorded revision.

## Procedure

1. Audit the authoritative graph and selected scope without relying on
   unstated author reasoning.
2. Verify the affected repositories, files, symbols, entry points, callers,
   writers, readers, and every changed control/data-flow hop.
3. Examine contracts, schemas, compatibility, persistence, integrations,
   failure propagation, retries, concurrency, security, and operational risks
   where applicable.
4. Assess feasibility, dependency order, SR boundaries, reuse of established
   patterns, testability, expected RED reasons, and proportional gates.
5. Identify any product, architecture, acceptance, or scope choice that lacks
   human authority.
6. Record each finding with severity, direct source, owner, and disposition as
   `OPEN`, `RESOLVED`, `DEFERRED`, or `REJECTED`.
7. Return the cold-review trace gate `PASS` only when the material-finding rule
   in `PROCESS.md` is satisfied. Otherwise return `FAIL` with exact blockers.

Use `skills/rdd-audit/SKILL.md` to resolve the packet's citations and diff its
inventories against the code — scoped to the packet's affected surface. Its
findings enter this review's finding list with the same dispositions.

Do not edit implementation, answer a human gate, or treat this technical
verdict as entry approval.

## Report

Lead with material findings, then state the reviewed fingerprints, finding
dispositions, trace-gate verdict, and the exact handoff: `rdd-plan` after a
failure or `rdd-entry-review` after a current pass.
