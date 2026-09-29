---
name: rdd-start
description: Enter a delivery session — verify the process-store binding and applicable release, reconcile answered gates, take or prompt for the work scope, and route to the phase the loop needs. Use at the start of product work and when asked to continue, choose work, report pending decisions, or identify blockers. Not a substitute for any phase skill.
---

# Start a delivery session

Read the project `AGENTS.md` and the canonical `PROCESS.md` — installed at
`.modernpath/rdd/PROCESS.md` in a consuming repository — before anything else.
`PROCESS.md` owns trace, status, gate, evidence, and completion meanings.

## Preflight — before any work selection

If the project provides an input-preparation step, run it once. Complete any
remaining checks according to the project's instructions.

Find and read relevant documents available locally. Checking when documents
were last updated does not refresh the local copies. When a refresh is needed,
follow the project's documented procedure. Consult the authoritative source
when local documents lack the information or you need an up-to-date answer.

1. Identify the authoritative process store (store-backed or file-backed) and
   confirm it is reachable. In a store-backed repository, confirm the binding
   identity from the store itself, never from a number quoted in instructions;
   report binding drift as a defect, not a variance.
2. For normal delivery selection, confirm exactly one active release with a
   `USER:` source. Candidate confirmation and source-scoped baseline work do
   not require an active delivery release. Report release problems without
   withholding the pending-decision projection from an orientation request.
3. Reconcile answered human gates and apply their consequences, then list the
   pending human decisions — only `OPEN` human gates with current passing
   prerequisites.
4. In a store-backed project, refresh working-set snapshots and check their
   headers against the store revision; never edit a stale snapshot. In a
   file-backed project, read the authoritative versioned records and regenerate
   any derived projections from them.
5. Count the suspended selections. More than one is a preflight fact: report
   each with its suspended status and reason, and name the mitigation —
   resume one, release one — before selecting new work.

The pending-decision projection may already have been delivered into the
session by the host — a session-start brief injected as context rather than
requested. That is the store's own answer arriving early, not ambient
background: date it against the store revision before relying on it, and
refresh it when it cannot be dated. A projection whose currency is unknown is
reported as unknown, never presented as current.

An unmet preflight fact is the report. Do not select work past it — unless a
current autopilot grant covers the scope: then clear or log each fact and
continue (`PROCESS.md` §Autopilot, `rdd-autopilot`).

## Take the scope

Accept the work scope as the argument: an Epic id, a single SR id, an exact
UR/SR set for candidate or baseline work, a source inventory for onboarding,
or a raw request.
Without one — including when the request is an orientation question
rather than a scope — answer from the store: read pending human decisions,
current and suspended selections, and routed work across all supported phases.
Include `PENDING_VERIFICATION` baselines awaiting proof or acceptance and
interrupted gate applications needing recovery. Present the projection and
ask the human to choose. Never pick a release commitment silently.

Version control, change lists, and the working tree describe the repository,
not the loop. They are never the source for what to do next; a session that
answers an orientation question from them has skipped this skill.

Rank what you present by what a single human answer releases: an `OPEN` human
gate holding built `IN_REVIEW` work outranks unstarted work, and a gate
holding many items outranks one holding few. State the distribution across
awaiting-decision, ready-to-build, and awaiting-acceptance. A queue whose
awaiting-acceptance bucket dwarfs its ready bucket is a finding about where
the loop is stalled — report it as one rather than leaving the reader to count
rows.

For normal delivery, freeze the Epic or single-SR selection in the existing
work-selection record. For candidate and baseline work, retain the exact scope
in the applicable authorization, run and decision records. For normal planning,
entry-packet depth is proportional to the frozen scope; no required item may
be omitted.

## Hold the session discipline

These rules bind every subsequent phase in the session:

- run the project's deterministic process checks before every commit, chained
  so a failure stops the commit — the expected RED of a red-first waypoint is
  the one failure that does not (see `rdd-build`);
- read the store's projection before any claim about readiness or state, and
  the repository and its hosting service before any claim about a branch, a
  pull request, a check run, or a deployment; a claim made from memory of an
  earlier read is not a fact;
- name every step that someone outside the loop performs — a merge, a
  promotion, a deployment — with who does it and when; never imply that it
  has happened or will;
- present the decision brief and linked list of relevant working-set files
  before asking for approval or a choice, following `PROCESS.md` §Gates; state
  each option's consequences, and treat the answer as a decision, not as an
  instruction to continue:
  apply it, report, and wait (`PROCESS.md` §Gates);
- timebox the diagnosis of a tooling failure; when the box closes, surface the
  gap through the project's channel and continue on a read-only path or stop.
  Never mutate a shared store to test a hypothesis;
- commit at waypoints — specification, expected RED, GREEN, cleanup,
  reconciliation — with RED evidence committed before the change that
  satisfies it, so red-first is auditable in history;
- work on a reviewable feature branch and preserve RED and passing
  fingerprints;
- route a discovery through `rdd-triage` to the earliest phase it
  invalidates; never silently widen the frozen scope.

## Route

Select the earliest unmet prerequisite for the frozen scope and hand off to
its skill; a change that meets the small-change lane's eligibility under a
current lane authorization is planned, reviewed and entered through that lane: `rdd-discover`, `rdd-plan`, `rdd-cold-review`, `rdd-entry-review`,
`rdd-build`, `rdd-verify`, `rdd-completion-review`, or `rdd-deliver` for the
complete loop. When a current autopilot grant covers the scope, or the human
asks for one, hand off to `rdd-autopilot` instead. Source-inventory
onboarding uses `rdd-reverse-engineer`.

For genuine source-scoped baseline requirements still in PENDING_VERIFICATION,
route to rdd-reverse-engineer-verify without development entry. When a dedicated
current exact-proof decision names the complete selected scope, route to
rdd-reverse-engineer-accept. Holds, suspension, blockers and DERIVED confirmation
retain precedence. Mixed scopes remain explicit; do not silently change the
selection or apply normal RED requirements to this dedicated path. Provenance
alone never reroutes an entered or defect-reopened item.

A pass ends with its report. Enter the next phase only when the human asks
for it, or when the request at session entry was the complete loop and the
boundary carried no human answer.

## Report

Report the store binding and how it was confirmed, the active release and its
source when applicable, pending human decisions, the frozen scope and fingerprint, and the
phase entered — or the exact preflight fact that stopped the session.

Repository and process work that carries no requirement record — tooling,
instructions, delivery infrastructure — is reported separately and labeled as
such. It is real work and may be urgent, but it is not what the queue is
asking for and never substitutes for the queue in the answer.
