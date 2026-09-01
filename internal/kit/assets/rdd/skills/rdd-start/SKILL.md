---
name: rdd-start
description: Enter a delivery session — verify the process-store binding and active release, reconcile answered gates, take or prompt for the work scope, and route to the phase the loop actually needs, holding commit and check discipline for the whole session. Use at the start of any product-work session, or when asked to start, continue, or pick up requirement-driven work. Not a substitute for any phase skill.
---

# Start a delivery session

Read the project `AGENTS.md` and the canonical `PROCESS.md` — installed at
`.modernpath/rdd/PROCESS.md` in a consuming repository — before anything else.
`PROCESS.md` owns trace, status, gate, evidence, and completion meanings.

## Preflight — before any work selection

1. Identify the authoritative process store (store-backed or file-backed) and
   confirm it is reachable. In a store-backed repository, confirm the binding
   identity from the store itself, never from a number quoted in instructions;
   report binding drift as a defect, not a variance.
2. Confirm the release registry holds exactly one active release with a
   `USER:` source. No active release, or more than one, stops selection until
   a human answers.
3. Reconcile answered human gates and apply their consequences, then list the
   pending human decisions — only `OPEN` human gates with current passing
   prerequisites.
4. Refresh the session's working-set snapshots and check each file's snapshot
   header against the store revision. A stale snapshot is refreshed, never
   edited.

An unmet preflight fact is the report. Do not select work past it.

## Take the scope

Accept the work scope as the argument: an Epic id, a single SR id, or a raw
request. Without one, present the current work selection and the routed
`PROPOSED`/`TODO` queue and ask the human to choose; never pick a release
commitment silently.

Freeze the selection per `PROCESS.md` work scope and record it in the
work-selection record. Packet depth is proportional to the frozen scope; no
packet item may be omitted.

## Hold the session discipline

These rules bind every subsequent phase in the session:

- run the project's deterministic process checks before every commit, chained
  so a failure stops the commit;
- commit at waypoints — specification, expected RED, GREEN, cleanup,
  reconciliation — with RED evidence committed before the change that
  satisfies it, so red-first is auditable in history;
- work on a reviewable feature branch and preserve RED and passing
  fingerprints;
- route a discovery through `rdd-triage` to the earliest phase it
  invalidates; never silently widen the frozen scope.

## Route

Select the earliest unmet prerequisite for the frozen scope and hand off to
its skill: `rdd-discover`, `rdd-plan`, `rdd-cold-review`, `rdd-entry-review`,
`rdd-build`, `rdd-verify`, `rdd-completion-review`, or `rdd-deliver` for the
complete loop.

## Report

Report the store binding and how it was confirmed, the active release and its
source, pending human decisions, the frozen scope and fingerprint, and the
phase entered — or the exact preflight fact that stopped the session.
