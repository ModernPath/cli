# Requirement-Driven Development — the process

<!-- TOOL-OWNED. Installed by `modernpath install`; replaced wholesale on upgrade.
     Do not edit here — project-specific rules belong in AGENTS.md. -->

Every contributor — human or agent — follows this. It defines **how** we build.
The *what* (domain specs, data models, business rules) lives in the project's
design docs. Project-specific rules live in `AGENTS.md`.

## 1. Non-negotiables

A change that violates one of these is wrong even if its tests pass.

1. **Nothing is done without all three: requirement, tests, code — cross-linked.**
   A passing test with no requirement, a requirement with no test, or code no
   test exercises are all defects.
2. **Deferral is explicit, never silent.** Work not done becomes a `DEFERRED`
   requirement with a reason and a tracking link. "We'll get to it" is not a state.
3. **Contracts are canonical.** Schema definitions (Zod, OpenAPI, Protobuf,
   JSON Schema) are the source of truth. Hand-written boundary types are banned;
   derive from the schema.
4. **Configuration values are never literals.** Business rules, thresholds and
   rates come from versioned config, not inline constants.
5. **Thin vertical slices, not horizontal layers.** Every unit of work is a
   working path from API/event → domain → persistence → back.
6. **The record tells the truth.** Deferrals, decisions and discoveries go in the
   active epic record the same session. Decisions resolving product, scope or
   architecture carry a `USER:<date>:<summary>` source.
7. **Discoveries are captured, not carried.** A requirement found mid-build is
   written down immediately — a `PROPOSED` ledger row or a `BACKLOG.md` line,
   with provenance — never held in your head, never silently merged into the
   work in hand.
8. **Status is updated in ALL places, atomically.** A requirement's status lives
   in three places: the dashboard row, the detail block's `Status:` line, and the
   `Totals:` line. Changing one is a bug.
9. **Reuse, don't re-derive.** If the system already computes something — a
   count, a tree, a label, a status — the new surface calls that code and
   serializes it. Re-implementing the logic elsewhere is a defect even when the
   tests pass. Read the existing implementation before writing a new one.
10. **Verify by reading back, not by reporting success.** "The command
    succeeded" and "the effect happened" are different claims. Check the
    observable outcome — the row is queryable, the page renders, the value
    returns — not just that the mechanism ran without error.

## 2. Lifecycle

```
DISCOVERY   customer requirements + stack  →  design docs        (skill: rdd-discovery)
PLANNING    design docs                    →  requirement ledgers (skill: rdd-planning)
BUILD       ledger requirements            →  epics → tests → code (skill: rdd-build-loop)
```

BUILD executes through the **V-model epic loop** (§5). PLANNING replenishes the
backlog from discoveries and doc changes; the two run concurrently.

## 3. Where state lives

| Path | Holds |
|---|---|
| `docs/` | Canonical design. Requirements derive from these. |
| `tasks/<CTX>-REQUIREMENTS.md` | The full backlog per bounded context — what you query |
| `epics/` | Active build detail: decisions, evidence, approvals |
| `WORKLIST.md` | Top-level rollup of active work |
| `BACKLOG.md` | Triage inbox for discoveries without a home yet |
| `process/` | Work state: open questions, gap register, release registry, plans |
| `PROGRESS.md` | Cross-context rollup, regenerated from the ledgers |

**Spec plane vs process plane:** decisions about the *product* go in `docs/`;
decisions and state about the *work* go in `process/`, `tasks/`, `epics/`,
`WORKLIST.md`, `BACKLOG.md`.

Requirements are never invented. Each traces to canonical design: invariants and
business rules → enforcement requirements; commands and events → behavioral
requirements; read models and APIs → query requirements; the data model →
persistence requirements. When a doc is ambiguous, do not guess: raise it in
`process/08-open-questions.md`, mark the requirement `BLOCKED`, move on.

## 4. The build loop

```
0. ORIENT   read the ledger dashboard + WORKLIST; pick the next READY requirement
1. SPECIFY  sharpen acceptance criteria against the design doc. Ambiguous? → log
            an open question, mark BLOCKED, pick another. Status → IN_PROGRESS
2. RED      write acceptance + unit + property tests encoding the criteria,
            each tagged with its REQ id. They must FAIL
3. GREEN    implement the smallest change that passes; annotate invariant
            enforcement in code with the rule id
4. GATE     run the suite; fix until green
5. TRACE    link code + tests in the ledger; status → IN_REVIEW (all three places)
6. REVIEW   human sign-off
7. COMMIT   PR titled with the REQ id
8. CAPTURE  record every discovery; update the epic record + WORKLIST; → DONE
```

Red before green, always — the failing test proves the requirement is real. One
requirement at a time; a slice may span several, but finish end-to-end before
starting the next. Discoveries are captured, not chased.

### Working rhythm

The loop is designed for continuous execution — run it repeatedly rather than
planning a large batch and executing it once.

- **Keep slices thin.** Two or three requirements across one or two bounded
  contexts is the right size. Larger slices increase the risk of drift between
  the ledger, the epic and the code, and drift is expensive to repair.
- **Finish a context before seeding the next.** Seed one ledger, build it out,
  then move on. Seeding everything up front produces a backlog that ages before
  it is built.
- **End every run at a clean stopping point** — tests green, ledger reconciled,
  record updated. Any run may be the last one before an interruption, and the
  next person (or session) starts from what the record says, not from what you
  remember.
- **Get the design docs right first.** Requirements derive from them; a vague
  doc produces a vague ledger, and the cost surfaces much later as rework.

## 5. The execution engine — V-model epics

**A requirement enters build by becoming or joining an epic, or via the fast lane.**

An epic is REQUIRED if any of these hold; otherwise use the fast lane:
1. The work needs design decisions or spec content beyond its acceptance criteria.
2. It touches a contract surface (API, DB schema, message/op schema).
3. It spans bounded contexts or repositories.
4. It is multi-slice or estimated over one working day.
5. It changes product-visible behavior.

If a criterion becomes true mid-build, **stop and promote** the work to an epic.

**Fast lane (REQ-row-as-task):** the ledger row is the task record. It must carry
sharpened GIVEN/WHEN/THEN criteria, RED→GREEN evidence and code refs in the row,
and a WORKLIST work row referencing the REQ id. Implementation never starts
outside the work-list.

**Epic path:** create an epic record that passes the Epic Specification Gate —
sourced user outcome and UR, users/actors, bounded-context ownership, BDD
acceptance scenarios (SCN), system requirements (SR), tasks (TASK), and a
failing-test strategy. Every normative spec claim cites a `DOC:`/`CODE:`/`USER:`
source; ungrounded content fails the gate. **The human spec approval must be
recorded before any RED test is written.**

Then run both loops: the **lower loop** is §4's RED/GREEN/GATE (failing
unit/component/API/contract test first); the **upper loop** is the epic's SCN
plus E2E evidence (failing BDD/E2E first). Review is upper validation plus the
epic's human approval gate.

### Status mapping

| Ledger | Epic |
|---|---|
| `REQ-<CTX>-NNN` | realized by SR/TASK rows; the epic's UR states the outcome |
| Acceptance criteria | the epic's BDD scenarios (SCN) — same statements |
| `IN_PROGRESS` | either loop underway |
| `IN_REVIEW` | SCN `UPPER_VALIDATED` + SR/TASK `LOWER_VERIFIED`, awaiting approval |
| `DONE` | epic `DONE` — both loops verified **and** approval recorded |

`LOWER_VERIFIED` / `UPPER_VALIDATED` are epic-internal waypoints; the ledger
stays `IN_PROGRESS` until `IN_REVIEW`.

**Reconciliation rule:** when an epic's status changes, update the mapped ledger
rows in the same change — dashboard row, detail block, `Totals:` line. This is
part of the Definition of Done, not a periodic repair job.

### Decision sourcing

Do **not** resolve product, scope, architecture, acceptance or workflow
decisions by assumption. Propose options, but a selected decision carries a
`USER:<date>:<summary>` source before it is recorded. Everything recorded as
fact needs a source (`USER:`/`DOC:`/`CODE:`/`TEST:`/`RUN:`/`EPIC:`); unsourced
claims become open questions.

### Real-browser verification for UI slices

A UI slice is not `IN_REVIEW` until it has run in a real browser and the agent
has looked at it. Component tests in jsdom are structurally blind to position,
styling, asset loading and layout. Each UI slice needs:
1. a browser test against the live stack, with assets asserted as *loaded*
   (`naturalWidth > 0`), not merely present;
2. a screenshot the agent opens and inspects before claiming the slice done;
3. no clicking of mutating controls on real data during verification — a test
   click that records a real decision is a fake decision.

## 6. Traceability

- **Tests → requirements:** every test names its requirement id, e.g.
  `describe('REQ-USR-001: email validation', …)`.
- **Code → rules:** the lines enforcing an invariant carry its id in a comment.
- **Ledger → code & tests:** each requirement row links implementing files and
  covering tests.
- **Commits/PRs → requirements:** PR title = `REQ-USR-014: password reset expiry`.

## 7. Definition of Done

1. Every acceptance criterion maps to a passing, REQ-tagged test.
2. Every invariant it covers is enforced in code and annotated.
3. Architecture and lint checks pass.
4. Contract tests pass — no schema drift.
5. Epic path: the specification approval was recorded **before** implementation
   started. Fast-lane rows are exempt; they get batch review instead.
6. Human sign-off, plus domain-expert sign-off for business logic.
7. Epic record + `WORKLIST.md` updated **and** the mapped ledger rows reconciled
   in the same change.
8. Status updated in all three places.

## 8. Session ritual

**Start:** read §1, §4 and §5 here, plus `AGENTS.md` for project rules. Open
`WORKLIST.md` and the target ledger; read the active epic record. If `BACKLOG.md`
has items or a design doc changed, run a planning pass first. Then take the next
READY work-list row — never start implementation outside the work-list.

**End:** tests green; epic record + `WORKLIST.md` updated and the ledger
reconciled in the same change; deferrals recorded as `DEFERRED` requirements and
discoveries as `PROPOSED` rows or `BACKLOG.md` lines; decisions carry `USER:`
sources. Leave WORKLIST ↔ epic ↔ ledger in agreement.

## 9. Skills

Detailed procedures load on demand rather than occupying every session:

| Skill | Use for |
|---|---|
| `rdd-build-loop` | running a build slice end to end (the workhorse) |
| `rdd-planning` | seeding a ledger, triage/replan passes, routing customer feedback |
| `rdd-discovery` | turning customer requirements into design docs; bootstrapping a harness |
| `rdd-ledger` | ledger format, status hygiene checks, templates |

**In one sentence:** every change starts as a requirement with acceptance
criteria, enters a sourced epic, becomes a failing test at both loops, then code
that passes, then a traced ledger row, an evidenced record and a human approval —
and anything we choose not to do is written down as a deferral, not forgotten.
