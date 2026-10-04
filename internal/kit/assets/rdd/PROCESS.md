# Requirement-driven delivery process

This document is the sole process authority. Skills execute it; `file-state/`
is its flat-file serialization. Neither may redefine it.

Runtime sessions, user interfaces, queues, and tool transports are outside the
process model.

## Canonical model

```text
EPIC -- optionally groups --> UR and/or SR

UPPER (UR): UR -- contains --> acceptance scenario -> TEST_CASE -> TEST_RESULT
                                      |
                                      +-- may require --> SR

LOWER (SR): SR -> CODE -> TEST_CASE -> TEST_RESULT

EPIC: PROPOSED -[HUMAN]-> TODO -> IN_PROGRESS -> IN_REVIEW -[HUMAN]-> DONE

UR/SR: DERIVED -[HUMAN]-> PROPOSED -[HUMAN]-> TODO -> IN_PROGRESS -> IN_REVIEW -[HUMAN]-> DONE
UR/SR: DERIVED -[HUMAN]-> PENDING_VERIFICATION -[HUMAN]-> TODO -> IN_PROGRESS -> IN_REVIEW -[HUMAN]-> DONE
UR/SR: source-scoped baseline authorization -[publish as-built]-> PENDING_VERIFICATION
UR/SR with source-scoped baseline authority: PENDING_VERIFICATION -[complete current verified/delivered proof + one HUMAN acceptance]-> DONE

TRACE: PENDING -> PASS | FAIL; PASS | FAIL -> STALE -> PASS | FAIL

HUMAN: DRAFT -> OPEN -> ANSWERED -> CLOSED
       DRAFT | OPEN | ANSWERED -> SUPERSEDED
```

UR evidence is upper evidence, normally exercised through an acceptance/E2E
path. SR evidence is lower evidence, exercised at the appropriate unit, API,
component, contract, or integration boundary. Upper and lower evidence belong
to different requirement types. Normal development is red-first; the
existing-baseline path uses the proof specified under As-built verification
and acceptance.

## Authority

Humans decide product intent, scope, architecture, acceptance, priority,
release, and workflow. Agents establish facts and propose options; they do not
make those decisions by assumption. Ask humans only for decisions that cannot
be established from authoritative records, code, tests, or runtime evidence.
Under an autopilot grant an agent may take a reversible product decision as a
recorded assumption pending confirmation (§Autopilot).

| Tag | Source |
|---|---|
| `USER:<date>:<summary>` | Attributable human fact, decision, or approval |
| `DOC:<path>` | Product, domain, architecture, or contract source |
| `CODE:<path>` | Implementation source file |
| `TEST:<path>` | Verification source file |
| `RUN:<command-or-report>` | Observed test or runtime result |
| `EPIC:<path>#<section>` | Existing Epic record |

File citations name the source file. The cited file carries the requirement or
other item ID it supports. Optional line, symbol, test-name or section suffixes
are navigation hints; the citation auditor checks only that the file resolves.
Execution evidence separately identifies the tests that ran and their results.

Missing support is an open question. Conflicting support remains a conflict
until a human resolves it. Code proves existing behavior, not intended
behavior. Repository state — branches, diffs, change lists, and version
control's own review queues — proves what the repository contains, not what
the loop holds; it is never a source for selection, status, or priority.
Here, **material** means capable of changing correctness, security, data
integrity, a public contract, trace completeness, acceptance, or testability.

Session working notes — review write-ups, test plans, scratch alignment
records — are not authoritative sources and may be discarded at any time. A
durable record restates their content rather than pointing at them, and
identifiers internal to one (finding numbers, plan step ids, review round
labels) are never citable from records, code, or instructions. Provenance
for an applied change belongs to the change itself and its gate records.

## Item ownership

| Item | Owns |
|---|---|
| `EPIC` | Optional requirement grouping, human-readable outcome, shared scope and decisions, lifecycle, gates, aggregate views, completion record |
| `UR` | Actor, context, user outcome, source, inline acceptance scenarios, lifecycle, upper evidence, optional Epic membership |
| `SR` | Smallest independently implementable system behavior, source, boundary, scope, technical context, lifecycle, lower evidence, code/test links, optional UR and Epic relations |
| `CODE` | Implementing files, symbols, revisions, branches, and changes |
| `TEST_CASE` | Stable identity, targeted UR scenario or SR clause, expected observation |
| `TEST_RESULT` | Outcome, RED/passing role, validity, command/report, environment, and tested fingerprint |

Acceptance scenarios are UR content, not separate lifecycle records. Split an
SR that contains independently implementable behaviors. Projects may retain
`REQ-*` IDs if each record declares its canonical kind.

## Trace completeness

The graph contains relationships, not execution order. Epic membership and
UR-to-SR links are optional. Never invent a parent to complete a trace.

| Selected item | Complete trace |
|---|---|
| Epic | Every in-scope member requirement satisfies its applicable trace; every declared Epic gate passes |
| UR | Sourced outcome, scenarios, current upper evidence, and every SR explicitly required by those scenarios |
| SR | Lower trace: `SR -> CODE -> TEST_CASE -> TEST_RESULT` |

Every declared relation must be authoritative, reciprocal where stored twice,
and covered by the gates that depend on it. A missing optional relation is not
a gap. A code link identifies implementation; it does not prove correctness.
A test covering multiple clauses must identify every target and assertion. A
UR-to-SR relation makes the SR part of the UR trace; it does not make UR upper
evidence part of the SR lower trace.

## Lifecycle states

UR and SR use the same status vocabulary.

| Status | Meaning |
|---|---|
| `DERIVED` | Inferred requirement awaiting human confirmation; all relations are candidate-only |
| `PENDING_VERIFICATION` | As-built behavior established by exact confirmation or source-scoped baseline authorization; awaiting current direct proof and acceptance; normal development entry applies when tests or behavior need development |
| `PROPOSED` | Confirmed or directly sourced requirement being prepared for entry |
| `TODO` | Entry trace passed and human entry approval was applied |
| `IN_PROGRESS` | Applicable red-first evidence work is underway |
| `IN_REVIEW` | Required evidence is complete; delivery, reconciliation, or completion acceptance remains |
| `DONE` | Delivered trace passed the applicable normal completion or existing-baseline acceptance gate, and human acceptance was applied |
| `BLOCKED` | Work cannot proceed; blocker and suspended status are recorded |
| `DEFERRED` | Work is postponed with reason, owner, target, and suspended status |
| `OBSOLETE` | Terminal rejection or supersession with decision/replacement linked |

A directly sourced requirement may start `PROPOSED`. An Epic uses
`PROPOSED -> TODO -> IN_PROGRESS -> IN_REVIEW -> DONE` and the same side states;
it has no `DERIVED` state.

On release from `BLOCKED` or `DEFERRED`, restore only the strongest state
supported by current gates and evidence.

Acceptance scenarios, code, test cases, and planning artifacts have no work
lifecycle. Evidence conclusions are not completion states:

- `LOWER_VERIFIED`: current lower evidence for an SR.
- `UPPER_VALIDATED`: current upper evidence for UR acceptance content.

### Derived requirement hold

While a requirement is `DERIVED`:

- record its candidate statement, inference sources, proposed relations,
  conflicts, consequences, and confirmation brief;
- label every proposed relation `CANDIDATE`;
- exclude it from authoritative trace, release, readiness, coverage, progress,
  and completion;
- candidate statements may include proposed UR scenarios, related candidates,
  source links and references to existing tests so they are reviewable together;
  all such content and artifacts remain candidate-only, excluded from ordinary
  test lists, coverage, release snapshots and execution assignment;
- do not use a candidate to authorize implementation, test execution,
  verification, entry, release commitments or delivery.

```text
DERIVED -> confirmed/corrected ----------> PROPOSED
        -> confirmed accurate as-built --> PENDING_VERIFICATION
        -> rejected ---------------------> OBSOLETE
```

Confirmation proves the requirement exists. Requirement decisions and link
decisions are separate: one attributable action may cover an exact typed set
of URs/SRs and explicitly named proposed relationships at a checked packet and
graph fingerprint. Unselected links remain candidate-only and may be approved
later in a relationship-only decision. No Epic is required. Changed inputs
require a refreshed preview. Rejection publishes no links; deferral leaves the
candidate unchanged. Approval is additive and never overwrites a referenced
existing requirement or its compliance approval.

Accepting as-built scope places it in Base/PENDING_VERIFICATION; accepting new
desired intent makes it PROPOSED and eligible for normal delivery planning.
Base is the store's destination for established as-built requirements, separate
from a delivery release; the project's store interface identifies that destination.
Neither action approves entry, verifies behavior, records a passing test result
or marks anything DONE. Candidate confirmation is not delivery release selection.

### Source-scoped baseline onboarding

Reverse-engineering can establish a usable requirement base or propose additions
to an existing corpus. Before publication, present both modes and require an
explicit choice. Recommend baseline when the corpus is empty, DERIVED additions
when it is populated. Existing content and approvals are preserved in both modes.

A baseline authorization names the authenticated actor, system/store, immutable
repository/file and document revision inventory, current corpus fingerprint,
Base destination, permitted relationship publication and process revision.
It authorizes derivation and publication of grounded as-built URs/SRs with
inline scenarios and canonical source/UR–SR/code/test relationships. It does
**not** assert the human reviewed statements that had not yet been generated.
No per-row or per-context confirmation is required within this approved scope.

Publish coherent groups atomically with durable input fingerprints and receipts.
Identical retries return the same receipt; changed input, out-of-scope sources
or conflicting existing content cannot silently expand the authorization.
Unchanged existing requirements may be reused only by exact identity/fingerprint.
Essential unresolved evidence remains an explicit DERIVED exception, never an
authoritative guess. Report partial publication and the exact remainder.

Baseline requirements are PENDING_VERIFICATION, not compliance-approved or
verified. They and their grounded confirmed graph appear in authoritative
requirement views; DERIVED additions appear only in candidate review. A test
reference or stub is not an execution result. Optional Epics need real intended
members; do not manufacture discovery-container Epics or membership.

Source capture preserves immutable repository/revision/path/digest identity,
including local changes. A historical read never substitutes latest code.
Unavailability or explicit source revocation is shown honestly while retaining
audit identity. Source/store read-backs and explicit coverage denominators are
part of onboarding completion, not evidence of product verification.

## Gates

A trace gate evaluates non-human facts at an exact fingerprint. A human gate
records a decision by an authorized human. A human gate may become `OPEN` only
after every prerequisite trace gate is `PASS`.

An approval-scope fingerprint identifies the requirement content, declared
relations, scope, and decisions being approved. An evidence fingerprint identifies
the tested code/content, tests, inputs, and revision. Each gate names its inputs.
Implementation within approved scope updates evidence; it does not by itself
invalidate entry approval. Changed approved scope or material review assumptions
require the affected gates to be re-evaluated.

```text
authoritative trace -> TRACE PASS -> HUMAN OPEN -> attributable answer
-> answer applied -> records reconciled -> HUMAN CLOSED

application: NOT_APPLICABLE -> PENDING -> APPLIED | FAILED
```

`PASS` and `FAIL` become `STALE` when inputs change. `STALE` never counts as
pass. The first answer to an exact `OPEN` gate is immutable; changed scope or
decision creates a successor and marks the old gate `SUPERSEDED`. An application
failure leaves the gate `ANSWERED` and preserves its holds.

Feedback not attached to an exact `OPEN` gate is a source or proposed decision,
not a gate answer.

### Strict human transitions

| Transition | Required trace `PASS` before human input |
|---|---|
| Requirement `DERIVED -> PROPOSED/PENDING_VERIFICATION/OBSOLETE` | Candidate packet and exact confirmation scope complete |
| Source-scoped baseline authorization | Bound system/store, current corpus and exact source/document inventory reviewed; allowed baseline publication made explicit |
| Source-scoped baseline requirement `PENDING_VERIFICATION -> DONE` | Complete exact current as-built verification and separate repository integration proof; one dedicated reviewed human acceptance |
| Requirement `PROPOSED/PENDING_VERIFICATION -> TODO` | Its Entry packet is complete at the exact fingerprint |
| Lane authorization for a system | The classes, excluded areas, who may apply it, expiry and daily cap are explicit |
| Small change `PROPOSED -> TODO` | Its narrow independent pass is `PASS`, it is eligible, and a current lane authorization covers its class and cap; the entry is recorded as an application of that authorization, attributed to the human who answered it |
| Epic `PROPOSED -> TODO` | Its Entry packet and every selected member's entry trace are complete |
| Requirement `IN_REVIEW -> DONE` | Its completion predicate is satisfied at the delivered fingerprint |
| Epic `IN_REVIEW -> DONE` | Every member is already `DONE` or named and completion-eligible in the same gate; the Epic completion predicate is satisfied |

One human answer may cover an exact Epic and named requirements. Apply member
requirement transitions before the Epic and record a `USER:` source for each.

Every human gate carries:

```markdown
**Brief:**
- What: <decision>
- Why now: <trigger and blocked work>
- Changes if approved: <visible outcome>
- Risk if wrong: <downside and reversibility>
- Recommendation: <option and rationale>
- Image: <optional evidence>
```

The brief is self-contained and in plain product language: an authorized human
who has not read the packet decides from it alone. State the substance of every
decision, correction, finding, option, or requirement it rests on — an internal
identifier (a decision, correction, or finding code) never substitutes for its
meaning and may appear only as a trailing parenthetical breadcrumb. Prefer
concrete user-visible outcomes to process, code, or architecture shorthand, and
name any agent choices the answer will also ratify in those same plain terms.

Before requesting any human approval or decision, present its self-contained
brief and a listing of the current working-set files the human needs to review.
Give each file a clickable link with its full absolute filesystem path as both
the visible label and the link target, followed by a short description of what
it contributes to the decision. These file links let the human open the material
in their chosen editor. Cover the gate brief, scoped requirement and
acceptance content, proposed relationships or changes, review findings,
evidence conclusions, risks, exceptions, and unresolved questions wherever
those are recorded. List the files relevant to the exact approval scope.

Read the files before presenting the listing. Validate materialized store
snapshots against authoritative store revisions, and source files against their
applicable pinned revisions or digests. In a file-backed repository, read the
current authoritative records. When a gate exists, confirm the listed material
matches its exact decision inputs; otherwise confirm it matches the current
choice's source inventory and scope. Refresh stale snapshots through the
project's sanctioned tool. Resolve missing or stale decision files
before asking for approval; a ready assertion or record id is not a file listing.
The listing supports the brief rather than replacing it. Do not print the full
file contents unless the human asks for them.

For a decision without an existing packet, list the available source or
authorization files that define the choice. Source-scoped authorization names
the exact source inventory and permitted publication; do not imply that
ungenerated requirements have been reviewed.

The same rule binds every question an agent puts to a human inside the loop,
not only a gate brief: each option states what it changes for the product, the
records, and the work ahead, in the same plain terms, and an identifier is at
most a trailing breadcrumb. A question is put only when no rule, record, or
earlier answer already implies its answer: preserving a delivered acceptance,
applying a stance the human has stated, or bookkeeping that follows from a
decision already given is done and reported, never asked. Each question
stands alone in one plain sentence about what changes for the product, and
bookkeeping is never bundled with a decision. A human's answer — to a gate
or to a question — is a decision about that gate or question, never an
instruction to enter the next phase: the agent applies it, reports what
moved, and waits for the human's word before any further phase, in the
complete loop as in a focused pass. Under an autopilot grant the agent
continues instead (§Autopilot).

### Automatic transitions

An agent or deterministic check may apply these only from a current trace-gate
`PASS`:

| Transition | Required proof |
|---|---|
| SR `TODO -> IN_PROGRESS` | Approved entry fingerprint and expected lower RED |
| UR `TODO -> IN_PROGRESS` | Expected upper RED or a required SR is `IN_PROGRESS` |
| Epic `TODO -> IN_PROGRESS` | An in-scope member is `IN_PROGRESS` |
| SR `IN_PROGRESS -> IN_REVIEW` | Its lower trace is current and passes |
| UR `IN_PROGRESS -> IN_REVIEW` | Required SRs are `IN_REVIEW/DONE`; current upper evidence passes |
| Epic `IN_PROGRESS -> IN_REVIEW` | Members are `IN_REVIEW/DONE`; applicable trace gates pass |

Agents may also apply evidence-invalidation demotions, and may apply `BLOCKED`
from an established impediment and release it when the impediment is gone.
Applying `DEFERRED` records a postponement decision and requires an
attributable human source. No automated transition creates or substitutes for
a human answer.

### Attributable demotions

An item that reached `IN_REVIEW` or `DONE` re-enters the loop under the same
identity, never through a duplicate requirement, a hand-edited status, or a
synthetic failure. Record the transition, actor, basis, and supporting evidence
or decision. Observed evidence invalidation follows the automatic rules under
Evidence and completion: attribute it to the evaluating agent or check with
its factual sources, without inventing a human answer or `USER:` source.
Demotions based on a human decision use the human gate described below.

| Demotion | Basis | What it requires and what follows |
|---|---|---|
| Requirement or Epic `IN_REVIEW/DONE -> PROPOSED` | Reversed decision: the approved scope, acceptance, or a decision it rests on no longer holds | The reversing decision linked; entry approval is stale and a new entry packet, cold review, and human entry gate precede `TODO` again |
| Requirement or Epic `IN_REVIEW/DONE -> IN_PROGRESS` | Defect: the delivered behavior is wrong against the approved requirement | The invalidated evidence named; red-first evidence is re-established for the defect; `IN_REVIEW` returns through the lower or upper trace and `DONE` only through a successor human completion gate |
| Requirement or Epic `-> OBSOLETE` | Superseded or retired | The replacement or the retiring decision linked (the supersession rule above) |

A human-directed demotion is recorded as a gate of purpose `demotion`. Its
transition names the destination, its exact scope names the demoted items, and
its sources carry the human decision and `USER:` attribution. This gate requires
no prerequisite trace; retain the decision and supporting facts. Applying it
reconciles the graph like any other answer. Evidence invalidation needs no
human demotion gate.
Demoting a member reopens its Epic to the weakest member state.
Siblings the demotion does not touch keep their state: their evidence stays
`CURRENT` when it is current at the present revision, and the Epic's next
completion re-validates it there rather than requiring it to be posted again.
Red-first binds the evidence that first proves a clause; a re-validation of
evidence that already passed — a sibling at the Epic's next completion, a UR's
upper validation at delivery — is not a new build and needs no new expected
failure.

## Work scope

| Scope | Use when | Required relations |
|---|---|---|
| Epic | Multiple requirements form one human-readable change, or shared product/architecture/contract/data decisions are required | Exact member UR/SR set; membership is grouping, not ancestry |
| Single SR | Exactly one independently verifiable system behavior changes | Authoritative SR source; Epic and UR relations optional |
| Small change | One SR changes one bounded behavior, the change meets the lane's eligibility, and a current lane authorization covers its class | Authoritative SR source; the lane authorization it applies; UR relation optional |
| Named UR/SR set | Candidate confirmation or source-scoped baseline verification or acceptance | Exact named requirements and applicable declared dependencies; no Epic required |
| Source inventory | Reverse-engineering before requirement IDs exist | Exact repository/file and document revisions, corpus fingerprint, derivation mode, and source authorization; publication receipts identify the resulting requirements |

Record Epic and single-SR delivery selections in the existing work-selection
format. Candidate and baseline scopes belong to their authorization, run and
decision records; they do not add work-selection kinds or phase values.

While preparing source authorization, record the mode or authorization as
pending where undecided. Publication requires the applied authorization.

Expand single-SR work to Epic scope when it changes user outcome or acceptance,
requires another SR, or introduces a cross-cutting decision. Related approved
items repeat entry approval only when their approved scope changes. A named
UR/SR set does not authorize normal development: any work needing implementation
or test changes is explicitly selected for normal planning and entry.

Normal delivery selection requires exactly one active release with a `USER:`
source. Candidate confirmation and source-scoped baseline work do not select a
delivery release and do not require one to be active.

### Small-change lane

The small-change lane scales review and gates to the size of the change; it
keeps every invariant of the loop. A change is eligible only when all of these
hold, checked by the store and the CLI rather than asserted by an agent:

- exactly one SR changes and no other requirement changes status;
- its change boundary names at most five non-test source files in one
  repository (tests are not counted — red-first adds them);
- it touches none of: security, authentication, authorization, or the
  process's own guards and permissions; migrations, persisted schema or tenant
  data access; public API routes, response shapes, or CLI verbs and flags;
  model prompts or anything that calls a model or runs a background job;
  billing; and any area the lane authorization excludes;
- its class is one the current lane authorization covers. The classes are: a
  defect with a diagnosed failing test; wording or copy; a presentation-only
  change inside one view or one library component; a patch-version dependency
  update with the suite passing. New features are never small changes.

A lane authorization is a strict human gate answered once for a system by a
workspace admin or a platform superuser. It names the classes it covers, any
further excluded areas, who may apply it, its expiry (at most 30 days; renewed
by a successor gate), and a daily cap. It can be withdrawn at any time; work
already entered finishes, nothing new enters. Every application of it is
visible in the authorizing human's feed.

A change that stops being eligible — a finding, a wider boundary, an excluded
area — leaves the lane for single-SR scope with its full packet and review. It
is never forced through.

### Autopilot

Autopilot is for building larger scopes — greenfield systems, initiatives,
many Epics — when stopping at every process check costs more than the check
protects. It keeps the records and the tests. It moves the human checkpoint to
the end of a sprint, and turns process refusals into logged work instead of
stops.

**The unit of work is a connected journey.** A connected journey is a set of
URs, their acceptance scenarios, and the SRs those scenarios require, taken
together because a user moves through them in one sequence. Its entry points
are where a user starts it — a view, route, or command — and its primary
actions are the actions that carry the user to the next step of its
scenarios. Autopilot selects a coherent user journey and the requirements it
needs, existing or newly written with sources — not one package per
table, route, screen, or test boundary. A journey may span Epics; requirement
ownership, sources, and requirement-to-test links do not change, and the exact
scope is held as the Epic and single-SR selections and entry records the
store supports: a journey that needs more than one SR from an Epic is held as
that Epic's selection. A journey is coordination, not a new requirement kind or
selection kind. Within the scope, a missing view or primary action outranks
refining one that already works. This changes the order of work, never what a
requirement means or what completion requires.

The sprint keeps a usable-coverage matrix: for each journey, whether its
entry point is reachable, its primary action is connected to real state, the
state persists, the next step works, and end-to-end evidence exists — each
marked built, partial, missing, or externally held. Externally held means the
cell waits on something the sprint cannot supply — a human answer, access, or
an outside party. It is a matrix mark, not a lifecycle state: when the hold
stops work on a requirement, that requirement is `BLOCKED` with the blocker
recorded; otherwise the requirement keeps its state and its matrix cell names
the hold. A navigable prototype, a
mocked response, or an isolated backend test does not make a journey built.
Every unfinished requirement clause stays visible in it. The matrix is a
working view of the sprint, reported at checkpoints and on the sprint-end
page; it links requirement records and never stands in for them. Between
sessions it is kept in the sprint's working record with the checkpoint
reports, not in the store unless the project says so; where it and a record
disagree, the record holds.

A journey is built in three stages:

1. connect its entry points and primary actions to real, authorized local
   state, and mark what is unfinished, simulated, or externally held;
2. complete representative end-to-end runs, including persistence, reload,
   and the handoff to the next step — in disposable fixtures where the grant
   excludes customer writes or external actions;
3. harden the remaining acceptance clauses, edge cases, scale, and
   presentation.

Stages 1 and 2 run per journey during the sprint; stage 3 runs at the sprint's
closeout. A stage proceeds in batches: a batch is the adjacent primary
actions built together under one RED commit and the commit that turns it
green, and the working code a batch lands is a slice.

Security, authorization, tenant isolation, and atomic data integrity belong to
the first functioning slice. They are never deferred to the hardening stage.

**Build-time and completion-time verification are separate.** During the
sprint, focused checks are the feedback gate: a focused-green slice is a
development waypoint, not a completion event, and the next step is the next
missing capability. A run of the full verifier before closeout needs a
concrete cross-system risk that focused checks cannot bound, or a human's
request, and the reason is stated. The full-verifier rule governs the agent's
own feedback runs; the project's commit gate runs on every commit as
configured, even when it runs the full suite. Full regression and hardening
run in the sprint's closeout, with time reserved for repairs. Closeout adds
verification and hardening, not RED for clauses already built. Missing or
failing required evidence keeps an item from `IN_REVIEW`: it stops
acceptance, not the building of the next authorized action.

**The grant.** A human who may answer entry and completion gates for the scope
grants autopilot once. The grant names:

- the scope (Epics, an initiative, or a system);
- the end of the sprint (a date);
- any areas that stay out of it.

It is recorded with its `USER:` source where the scope's decisions are
recorded: in each Epic's decisions as it is selected, in a store-backed
repository, and as `process/autopilot.md` in a file-backed one. The grant is
the human's answer, given in advance, to every entry gate in its scope. The
agent applies it by answering each such gate with the grant's `USER:` source.
It is never the answer to a completion gate. A grant ends at its date, when the
human withdraws it, or when its scope is done.

**Under a current grant these rules change:**

| Rule | Normal | Autopilot |
|---|---|---|
| Entry gate | The human answers | The agent answers, citing the grant |
| Cold review | Up to two rounds, blocking | One pass, then at most one narrow confirmation. Material findings are folded into the packet as tests or boundary lines, and the rest are logged. If the confirmation still fails, the item is built ahead of its record and the failure is logged. |
| Packet completeness | Items 1–6 and the brief complete before review | Write what the builder needs; a gap the review finds is folded in or logged, never a stop |
| Preflight facts (suspended selections, stale snapshots, drift, open holds) | Stop selection | Clear each with its documented verb, or log it and continue |
| A refusal at a phase boundary | Stop and report | Try the documented clearing verb once. If it still refuses, log it, move to the next item, and let the record catch up at sprint end. |
| A product question | Asked; the loop waits | Asked, but the loop does not wait: see below |
| After a gate answer or a pass | Wait for the human's word | Continue |
| Work selection | An Epic or a single SR | A connected journey with the requirements it needs, existing or newly written with sources; more than one SR from an Epic is held as that Epic's selection |
| Planning and cold review | Per selected scope | Once per journey. An unchanged reviewed packet goes straight to entry, or to RED when its entry is already applied and current; any change in a blocking cold-review category — correctness, security, data integrity or data loss, contract, traceability, testability — and any changed scope or acceptance is reviewed again, and only that change |
| Readiness to build | The complete packet and an applied entry | Code starts once the source, outcome, owning requirements, state and safety boundaries, and representative RED assertions are clear; remaining inventory and record updates run alongside the build. Code written before the entry is applied is reversible local build-ahead (below); the entry is applied before any RED evidence is recorded or any requirement advances |
| Cold review timing | Before any code | May run alongside reversible local build-ahead. A material safety finding is repaired before the affected code is committed past its RED commit |
| AI TDD inner loop | One SR clause at a time, rerunning affected URs each iteration | Batch: representative journey and state-boundary REDs cover adjacent primary actions, which are implemented together and checked with one focused integrated GREEN run. The batch's RED commit carries a failing assertion for every clause the batch implements; representative REDs only reduce the number of commits, never the assertions |
| Commits | At each waypoint: RED, GREEN, cleanup, reconciliation | Per connected batch: one RED commit carrying the batch's failing tests, its RED evidence recorded there, then the working batch. A RED commit carries only failing tests that its next commit turns green; that commit lands the whole batch. To land a batch SR by SR, use one RED/GREEN pair per SR. A journey assertion is written in the batch that can make it pass. A test is never skipped, disabled or left failing to get a commit past the project's gate, other than the declared RED tests of a RED commit as the gate admits them; the gate runs on every commit as configured |
| Records | Reconciled after each pass | Reconciled at each integration checkpoint; record grooming never holds the build |
| Full verification | At each phase's required exit | The agent's full-verifier feedback runs wait for sprint closeout, unless the exception above applies; the project's commit gate is unchanged |

**Reversible local build-ahead** is uncommitted work on the feature branch,
written before the entry is applied. It is set aside so that the RED commit
and its observed failure are made at a revision without it, and it returns
only after that RED is recorded. It authorizes no RED evidence before entry
and no status change. While it stays uncommitted and is set aside before RED,
it is not a gap record; a build-ahead that reaches a commit before its RED is
a build ahead of its record, filed as a gap record under the log below.

**Checkpoints.** An integration checkpoint follows each batch that lands. At
each integration checkpoint, compare usable coverage with
the previous checkpoint. When the batch added no user capability and repaired
no blocking defect, the next action is the next missing journey step; any
exception is explained in user-outcome terms. Immediate further work on a
green slice is justified only by a safety or data-integrity defect, a
regression, or a failure that blocks the selected journey; every other finding
is queued with an owner and a next action. This restriction governs the
build during the sprint; it does not apply to the hardening stage at
closeout. Report the observed split between
implementation and preparation or checking; when preparation and checking
dominate, stop discretionary process work and build the next missing action.
Never discretionary: recording RED and GREEN evidence at its commit, applying
entry before a requirement advances, gap records for skipped checks and
refusals, and recording and applying the cold-review verdict.
Timings are observed, never reconstructed.

**Product questions.** The agent still asks the questions only a human can
answer: behavior, scope, user experience, meaning of data, priority. It asks
each one when it arises, in plain words. Then it takes the most reasonable
default and records it in the Epic's decisions as *assumed under autopilot,
pending confirmation*, with the grant's `USER:` source. Then it continues.

It waits for an answer only when a wrong guess is hard to undo:

- deleting or migrating data that already exists;
- security, authentication, or authorization;
- money, billing, or legal terms;
- a contract with an outside party.

**What does not change:**

- Requirements are written with sources, and assumptions are marked as such.
- Red-first tests, with evidence recorded. A RED is observed before the code
  that satisfies it, and no evidence is reconstructed after the fact. Every
  clause a batch implements has its failing assertion in the batch's RED
  commit.
- Security, authorization, tenant isolation, and atomic data integrity hold
  from the first functioning slice.
- Every acceptance clause is met before acceptance. Focused-green slices,
  partial evidence, and the sprint's end date are not acceptance.
- The store's server-side refusals are cleared through the sanctioned tool or
  logged. They are never bypassed with hand-made writes.
- Merging, deploying, and releasing stay with humans.
- Destructive operations, secrets, and the grant's excluded areas stay out.

**The log.** Each skipped check, uncleared refusal, advisory finding left
open, and build ahead of its record is filed as a gap record naming the
sprint. Nothing is dropped silently.

**Sprint end.** Closeout runs the full required verification and the
hardening stage, and repairs what fails; it adds verification and hardening,
not RED for clauses already built. An item is presented
as eligible for completion only when its required evidence passes. Then one
independent review reads everything the sprint built — the change, not the
packets — against its requirements and tests. The human then gets one page:

- the journeys and primary actions a user can now complete, with evidence;
- the coverage matrix: remaining clauses and what is still unavailable;
- the assumed decisions to confirm or change;
- the log;
- the review's findings.

The human answers completion item by item, as in a lane batch. A rejected
item stays `IN_REVIEW`. A changed assumption is triaged into the next sprint.
Only then does normal gate discipline apply again to anything the grant did
not cover.

## Planning and readiness

Planning consists of packet authoring, independent cold review, and entry
review, in that order. Changed review inputs or a failed result return work to
the earliest affected pass; a downstream pass cannot repair an upstream gap.
Packet depth is proportional to the selected scope — a single-SR packet may
satisfy an item in a sentence where an Epic needs pages — but no packet item
may be omitted. Include the information a builder or gate needs to assess the
change; document length does not determine requirement scope.
A small change's packet is its SR record: statement and source, change
boundary (files, and what must not change), RED plan (the failing test and why
it fails today), and lane class; together they satisfy the packet items. It
carries no state inventory — the lane's excluded areas keep persisted and
shared state out of it — and its ledger search covers the files its boundary
names.

### Entry packet

The fingerprinted packet must contain:

1. authoritative item content, declared relations, scope, owner, and release;
2. UR scenarios and thin SRs where applicable;
3. reconnaissance at a named revision covering the affected surface,
   control/data flow, contracts, persistence, integrations, reuse targets,
   dependencies, failure modes, operational risks, test infrastructure, and
   project gates, and — when the change adds or touches persisted or shared
   state — a state inventory: one row per piece of state, with its writers
   and each write shape it admits (a birth, an edit of an open row, an edit
   of a settled row, a row born before the change), the readers that branch
   on it, what a crash mid-write leaves, what makes it stale, and how it
   recovers, with the planned mitigation, supporting criterion or decision,
   and any unresolved risk;
4. each SR's owned flow segment, change boundary, dependencies, risks, and test
   path;
5. an upper-RED strategy for selected UR scenarios requiring new evidence, a
   lower-RED strategy for selected SR clauses requiring new evidence, and
   proportional regression gates; unchanged proven scenarios need revalidation,
   not a new RED;
6. sourced decisions, conflicts, gaps, deferrals, blockers, and unknowns;
7. cold-review findings and verdict; and
8. the human entry brief.

Reconnaissance cites `DOC:`, `CODE:`, and `TEST:` sources. Search the requirement
records for existing ownership and constraints on affected behavior; verify
the matches against their content and declared relations. Preserve applicable
delivered acceptance unless an authorized scope decision changes it. If no
existing requirement owns a surface, record that fact and its relation to the
proposed work; do not invent a delivered owner. Read available system
documentation for context. Resolve claims about existing behavior against code
at the named revision; differences from intended behavior remain findings.
Material revision drift makes the packet and its dependent reviews stale.

Items 1–6 and the human brief must be complete before cold review starts;
entry review also requires the cold-review verdict. Missing required state
analysis or an unresolved risk to an acceptance criterion is a finding. Read a
stated limit, timeout, or constant at the call site that applies it, including the
conditions that select its value.
A small change's SR record is its complete packet.

A RED strategy names, for each case requiring new evidence, the planned test
file, the behavior it asserts, and why it is expected to fail at the revision.
For new or changed behavior, every planned RED case must fail against the code
at the planning revision for the stated reason; a case that would already pass
cannot serve as RED evidence. For entered verification of existing behavior,
the strategy names the safe temporary mutation or equivalent targeted failure
that makes the case fail, as described in `rdd-verify`. Cold review checks these
failure reasons against the code at the recorded revision.
Planned commands and test identities are proposals, not execution evidence.
Record the actual command and expected failure at the RED revision. A migration
or schema change that satisfies the test lands with GREEN, not with RED.

Cold review runs from a context independent of packet authoring and audits the
trace, scope, technical surface, changed flow, contracts, data, compatibility,
failure behavior, feasibility, dependency order, SR boundaries, RED strategy,
gates, and unauthorized decisions. Each finding records severity, source,
owner, and `OPEN`, `RESOLVED`, `DEFERRED`, or `REJECTED` disposition. Open or
in-scope deferred correctness, security, data-loss, contract, traceability, or
testability findings fail the cold-review trace gate. Technical review cannot
grant entry approval.

The review audits the change, not the document. A finding about the packet's
own wording, counts, or citations that would alter none of the code, the
tests, the interfaces, or the risks is a note and never blocks; a traceability
finding is material only when a builder or a gate would act on the wrong
citation. Record the reviewer and independent review context with the verdict;
an author's self-review is not a cold review. A closure carried
from an earlier round is a claim to re-verify, not a fact. A finding that
would change a human decision returns to that human as a question; it is never
resolved by editing the packet.

A `RESOLVED` disposition names how it resolved: a packet edit that clarifies
what the change already contained, a scope action with its record, or a
decision with its `USER:` source. No new mechanism enters a packet during a
review cycle. A finding whose fix needs a new acceptance criterion, a wider
boundary, or a new flow hop is a scope question — split, defer, or decide —
and the mechanism is planned with its own reconnaissance as its own change.
A resolution edit re-enters reconnaissance for what it names: reread every
symbol, path, and test it names at the recorded revision before marking the
finding `RESOLVED`. Verify a fix proposed by the reviewer in the same way as
a closure carried from an earlier round.
Changes to approved behavior, scope, architecture, acceptance, or a material
technical decision return to the affected planning pass. A review finding does
not itself authorize implementation.

At most two cold-review rounds run on the selected planning scope. A later
round reviews what changed since the previous round's recorded trace — the
records and sections whose fingerprints moved, the findings still open, and
what those touch — and carries the unchanged remainder at that round's verdict;
its trace still pins the full current aggregate. A current
pass proceeds to entry review. After a second failed round, report the remaining
material findings to the human; do not start a third round automatically. Under an autopilot grant, one pass
and at most one narrow confirmation replace the rounds (§Autopilot).
A small change gets one narrow pass from an independent context — the
boundary against the code, the RED plan, and the eligibility — and no second
round: a blocking finding sends it out of the lane.

Completion review requires an independent code review of the built change for
Epic scope. For single-SR scope, obtain one when project gates or the user
require it. The review runs from a context independent of implementation;
record the reviewer, reviewed revision, findings, and verdict separately from
planning review. Missing required review or open material findings stop
delivery and completion. Resolved implementation findings cite the correcting
commit and verification.

Entry review evaluates the complete packet at its exact fingerprint. Only a
current entry trace `PASS` may open the human entry gate. Do not create or
change tests or implementation until current applied entry approval covers the
whole selected delivery scope, including the Epic when selected. Newly entered
items must be `TODO`; already-entered items retain their current states.
Unchanged `DONE` dependencies are not re-entered. Partial entry approval does
not permit development to start on the approved subset. There are two
exceptions. The first is the defect lane: when a defect is already diagnosed
and the change is bounded, the failing test may be written first, on a branch
and before entry, and cited in the packet as a `RUN:` source: it is the reconnaissance,
and it gives the review something executable instead of prose about whether a
planned test would fail. The red test does not replace the SR's own lower
RED, which is re-established after entry, and it authorizes no implementation.
The second is reversible local build-ahead under a current autopilot grant
(§Autopilot): uncommitted code, set aside before the RED commit, that
authorizes no RED evidence and no status change before entry.

## Development loop

```text
SOURCE -> PLAN -> COLD REVIEW -> HUMAN ENTRY -> AI TDD LOOP -> COMPLETE -> DONE
            ^                                          |
            +--------------- TRIAGE / REPLAN <----------+
```

Enter a session with `rdd-start`. Use `rdd-deliver` for end-to-end work. Use a
focused skill alone only when the requested scope explicitly ends at that pass.

| Phase | Skill | Required exit |
|---|---|---|
| Enter session | `rdd-start` | Store binding verified; single active release verified when selecting normal delivery; answered gates reconciled; frozen scope routed to its earliest unmet phase, or an orientation request answered from the current pending-decision projection |
| Source/classify | `rdd-discover` | Authoritative input or an exact confirmation gate; no unconfirmed requirement proceeds |
| Plan/reconnaissance | `rdd-plan` | Entry-packet items 1–6 and the human brief at a named revision |
| Cold review | `rdd-cold-review` | Current cold-review trace verdict and finding dispositions |
| Entry | `rdd-entry-review` | Applied human approval and selected items in `TODO`, or an explicit non-entry result |
| Execute changed SR | `rdd-build` | Current lower evidence; eligible SR in `IN_REVIEW`; selected UR evidence updated independently |
| Verify entered work requiring tests | `rdd-verify` | Current UR upper or SR lower evidence; eligible requirement in `IN_REVIEW` |
| Verify existing baseline | `rdd-reverse-engineer-verify` | Complete applicable current assertion/execution proof and separate repository integration observations; exact eligibility packet |
| Accept existing baseline | `rdd-reverse-engineer-accept` | One reviewed human decision atomically applied from `PENDING_VERIFICATION` to `DONE`, with durable receipt; compliance unchanged |
| Deliver/complete | `rdd-completion-review` | Delivered revision, reconciled records, completion trace, and applied human result |
| Route change | `rdd-triage` | Discovery assigned to the earliest phase it invalidates |

Before each phase, reconcile answered gates and state, then select the earliest
unmet prerequisite. A focused skill's exit is a handoff, not completion of the
full loop.

### As-built verification and acceptance

A published source-scoped baseline starts in `PENDING_VERIFICATION`. This path
requires that persisted baseline authority; the status alone, including status
from individual DERIVED confirmation, does not confer eligibility. Publication
establishes requirement authority, not test PASS, delivery or compliance approval.
A dedicated existing-behavior path may move exactly reviewed URs/SRs directly to
`DONE` with **one human acceptance decision** after all of the following hold:

- Every active applicable criterion has inspected semantic assertion coverage,
  confirmed code/test connections and genuine current named execution proof.
  UR upper evidence is evaluated independently and includes its required SRs;
  a standalone SR needs no invented parent or Epic.
- Exact requirement content, graph, source/test bytes, revisions, report/result
  identities and CI provider/repository/run/job/attempt/tested commit are pinned.
  Missing, unexecuted, contradictory, stale, inaccessible or revoked proof is a
  gap, not a partial PASS or an accepted undisclosed coverage limit.
- Every repository has a distinct retained integration observation of the clean
  tested revision at the fetched remote default-branch tip, matching the captured
  snapshot. Passing unmerged branch tests do not prove delivery. This is an
  integration observation, not a deployment claim or a live provider connector.
  Retain the observed remote identity. If the integration check does not validate
  it against an independently declared repository identity, disclose that
  limitation; a local origin setting alone does not establish repository ownership.
- A current eligibility trace evaluated through the sanctioned tool supports
  the exact human decision.
  Its attributed reviewed answer and guarded application recheck the complete
  proof. Application is atomic across the named scope and uses normal lifecycle
  events. A stable key and identical input recover the same receipt; changed
  input conflicts. Historical receipts and their exact reviewed evidence remain
  readable after drift, while stale proof cannot be applied or replayed as current.

`rdd-reverse-engineer` remains publication-only. The dedicated
`rdd-reverse-engineer-verify` gathers and evaluates existing evidence;
`rdd-reverse-engineer-accept` reviews the exact packet, records the one human
answer and applies/readbacks its receipt. No skill fabricates RED or rebuilds an
already proven product. Test additions and behavior changes remain gaps until
an explicitly scoped handoff to normal development. No general-purpose gate
label, client flag or generic advance authorizes this transition.

This path changes work lifecycle only: compliance draft/approved fields are
unchanged. Normal `rdd-build`, `rdd-verify` and `rdd-completion-review` retain their
RED/GREEN, entry and completion contracts. Requirements reopened for a defect
return to normal development; baseline provenance alone does not reroute them.

### AI TDD inner loop

After human entry places the selected scope in `TODO`, the AI owns the automatic
`TODO -> IN_PROGRESS -> IN_REVIEW` transitions. It does not request human input
while the approved scope remains unchanged.

```text
establish required selected UR upper RED
  -> select an unmet approved SR clause
  -> SR lower RED -> GREEN -> CLEAN -> lower verify
  -> rerun affected UR upper evidence
  -> all applicable trace gates PASS?
       no  -> repeat
       yes -> IN_REVIEW
```

Run the loop as follows:

1. Establish the expected upper RED for every selected UR requiring new
   evidence. A standalone SR has no upper step.
2. If an SR trace is unmet, select one approved clause, establish its focused
   lower RED, implement the smallest passing behavior, and perform scoped
   behavior-preserving cleanup.
3. Run the SR's focused and boundary-appropriate regression gates on the
   cleaned content, then rerun each affected UR scenario.
4. Re-evaluate every selected SR lower trace and UR upper trace independently.
   A trace `FAIL` caused by unmet approved behavior starts another iteration;
   it does not request human input.
5. Exit to `IN_REVIEW` only when every selected SR lower trace is current and
   `PASS`, and every selected UR upper trace is current and `PASS` with all of
   its required SRs in `IN_REVIEW` or `DONE`.

Under a current autopilot grant, §Autopilot's batch cadence replaces the
per-clause rhythm of steps 2–4, at closeout as during the sprint: a clause
first built at closeout gets its own RED like any other. Step 1 changes in
one respect: a UR scenario's upper RED is established in the batch that can
make it pass, and it still fails at that batch's RED revision. The exit in
step 5 does not change.

Use a reviewable feature branch and preserve RED and passing fingerprints. For
as-built behavior entered into normal verification, demonstrate regression
sensitivity with a safe temporary local mutation or equivalent targeted failure,
then restore it. The restored implementation may require no product-code change.

If an upper failure remains after all planned SR lower traces pass, diagnose it.
Repeat the inner loop when the failure is within approved behavior. Return to
the earliest planning pass when satisfying it requires a new or changed
requirement, relation, scope, architecture, acceptance rule, priority, release,
workflow, or material technical decision. Record an external impediment as a
blocker. These are the only exits before the trace gates pass.

Move an eligible item to `IN_REVIEW`, then use completion review to audit,
deliver, re-evaluate evidence at the delivered revision, reconcile records,
open the human completion gate, and apply the answer. UI evidence requires the
live stack, loaded assets, and an inspected screenshot; do not mutate real
production-like data to verify rendering.

The full loop terminates only when the selected scope is `DONE` or `OBSOLETE`.
An unanswered human gate, `BLOCKED`, `DEFERRED`, `TODO`, or `IN_REVIEW` state is
an explicit incomplete handoff, not completion.

### Delegated passes

A pass may be delegated to another context — a cold review, a verification
sweep, one reconnaissance surface. A delegated pass establishes facts and
returns them: findings, a verdict, citations. It writes nothing to the process
store; the orchestrating session records what the pass returned, under its own
actor attribution while retaining the reviewer's identity and context. Recording
an independent review's returned verdict does not turn it into self-review.

A delegated pass that is refused by its environment — a permission denial, an
authentication failure, a store refusal — stops and returns the refusal
verbatim as its report. A refusal is a decision by the environment's owner,
not an obstacle: the pass never reformulates, splits, or re-issues the refused
call, and an instruction to finish the task does not override this.

## Evidence and completion

A test result is immutable. Rerunning creates a new result.

Outcome is `PASS`, `FAIL`, or `SKIP`. Validity is:

| Validity | Meaning |
|---|---|
| `CURRENT` | Matches the exact clause, content/code fingerprint, and revision |
| `STALE` | A traced input changed after the result |
| `INVALID` | The tested content is unreachable, reverted, or abandoned |
| `INHERITED_UNVERIFIED` | Carried from another revision or change without a confirming run |

Only `CURRENT` evidence linked to the exact clause, test case, code/content
fingerprint, and revision counts. Broad suites prove only exercised assertions.
Line numbers are navigation hints, not test identities. Evidence may be current
on a feature branch before integration; delivery is a separate completion
prerequisite. Earlier RED results demonstrate regression sensitivity at their
recorded revision; they are not relabelled as executions of the passing
implementation. Current passing proof is required separately.

Normal development requires the following evidence. The existing-baseline path
uses the requirements under As-built verification and acceptance instead.

| Requirement/evidence | Required evidence |
|---|---|
| SR lower — `LOWER_VERIFIED` | Sourced SR clause; expected focused failure; named regression-sensitive assertions; linked code; passing focused and proportional post-cleanup gates at the cited revision |
| UR upper — `UPPER_VALIDATED` | Sourced UR scenario; expected user-flow failure; passing result; required runtime/browser observation; linked revision |

Choose evidence by boundary: unit/property for domain rules, component plus
browser for UI, endpoint/contract for APIs, integration for persistence and
integrations, schema conformance for cross-service contracts, and harness/smoke
for operations.

Invalidating required evidence atomically:

1. changes result validity;
2. makes dependent trace gates `STALE` and supersedes dependent unclosed human
   gates;
3. removes affected evidence conclusions;
4. demotes dependent `IN_REVIEW`/`DONE` Epic, UR, and SR items to `IN_PROGRESS`;
5. propagates only through declared relations.

Supplemental evidence causes no demotion. Re-verification may restore
`IN_REVIEW`; restoring `DONE` at a new fingerprint requires a successor human
completion gate. Material approved-scope changes stale entry approval and send
work back to planning.

One completion human gate may name many small changes — a lane batch — each
meeting its own predicate below; the human may reject single items, which stay
`IN_REVIEW`.

A normal completion human gate may open only when named items are `IN_REVIEW`,
code is delivered, evidence is current at the delivered revision, state is reconciled,
candidate relations are excluded, and gaps/deferrals/decisions are disclosed.
The delivered revision is the one the authorized integration path produced,
not the branch head that fed it. A member-scoped trace from an earlier round
that is `STALE` still counts against its Epic — an Epic-scoped pass does not
stand in for it — until it is re-evaluated at the current fingerprint.

| Item | `DONE` predicate after human acceptance |
|---|---|
| SR | Its lower trace is delivered, current, and reconciled |
| UR | All scenarios have current upper evidence; every required SR has a complete lower trace; result is delivered and reconciled |
| Epic | Every member is `DONE`; applicable member and declared Epic gates pass; Epic scope is delivered and reconciled |

Completing one item never advances an optional related item unless that item
independently satisfies its predicate and is named in the human gate.

## State records and reconciliation

Exactly one process store is authoritative per repository, and the
`file-state/` shapes are the canonical serialization of its records in either
case:

- **Store-backed.** A database or platform owns the records. Tooling
  materializes shape files locally as working-set snapshots and projections;
  a materialized file records the store revision it came from, is never
  committed, is never an authority, and never overwrites newer store state.
  Conflicting syncs are surfaced for a decision, never resolved silently.
- **File-backed.** The versioned `file-state/` records are the store.

A repository is one or the other, never both at once. Every serialized file
carries its snapshot header — `Snapshot at` and `Source store/revision` — so
currency is checkable per file.

A project names one sanctioned tool as its interface to the store — its write
channels, its reads, its projections — and a channel for surfacing what that
tool lacks. When a task needs something the tool does not expose — a session
or authentication fact, an untruncated value, any read — that is a tooling
gap to surface through that channel, never a variance to absorb. Reaching past
the tool — reading its credential or configuration files, calling its
transport by hand, editing store files — is the anti-pattern, with the same
standing as every other rule here. A read-only workaround that unblocks the
session is acceptable when the gap is surfaced in the same session. A
workaround that writes to the store by hand is a stop: it bypasses server-side
legality and actor attribution, which are safety properties, not conveniences.
An agent's persistent notes never carry such a workaround as knowledge: the
gap is filed, and the note is retired when the surface lands.

```text
file-state/
  EPICS.md
  REQUIREMENTS.md
  GATES.md
  WORK-SELECTION.md
  BACKLOG.md
```

`EPICS.md` stores optional grouping records. `REQUIREMENTS.md` stores URs, SRs,
declared relations, and trace references. `GATES.md` stores every trace and
human gate record. `WORK-SELECTION.md` stores the frozen delivery scope, suspended
selections, and selection history. `BACKLOG.md` stores unrouted triage items
and gap records. Derived queues and progress views — including the pending
human-decision projection — are regenerated, not backed up separately.

Backlog, gap, and tooling-gap records are records of the store like every
other: they are created, routed, and closed there, and their state is read
from there. A plan, a handover, or an agent's notes may summarize them and is
a projection at best — it never carries a disposition the store does not, and
a disagreement between the two is resolved by reading the store, not the
note. A discovery that lives only in a note is not yet a record.

That projection is never lifecycle authority, and it is the session's answer
to what to work on next: it is read from the store, dated against the store
revision, and presented — ranked by what a single human answer releases. A
projection delivered into a session ahead of the request is that same answer
arriving early, not background context.

| Concern | Authority |
|---|---|
| Product/domain/architecture/contracts | Product documents and schemas |
| Epic, requirement, relation, gate, decision, release, and work-selection state | Authoritative process store |
| Backlog, gap, and tooling-gap records and their dispositions | Authoritative process store |
| Code, test cases, and results | Implementation repository plus exact evidence references |
| Aggregate progress and human queues | Generated projections; never lifecycle authority |

Every gate record stores id, kind, transition/purpose, exact scope, prerequisites,
fingerprint, state/verdict/answer, actor/evaluator, sources, timestamps,
application state/revision, and predecessor/successor.

Every evidence record stores targeted clause, stable test case, outcome, role,
validity, command/report, environment when relevant, fingerprint, revision, and
code link.

Apply a human answer only when its `ANSWERED` gate fingerprint is current:

1. update every named item and consequence;
2. record actor, scope, source, transitions, and application revision;
3. run deterministic checks and reconcile projections;
4. mark application `APPLIED` and gate `CLOSED` only after records agree.

After every transition, update the complete affected graph and run checks for:

- valid identities/statuses and reciprocal declared relations;
- stable test identities, revision-pinned validity, and invalidation cascades;
- exact gate fingerprints and legal gate/state transitions;
- no `TODO` without applied entry approval;
- no `DONE` without delivered evidence, reconciliation, and applied human
  acceptance through normal completion or the eligible existing-baseline path;
- isolation of `DERIVED` items and candidate links from authoritative scope;
- agreement between authoritative state and generated projections.

Only `OPEN` human gates with current passing prerequisites appear as pending
human decisions.

## Discoveries, releases, and conflicts

| Discovery | Route |
|---|---|
| Inferred possible requirement outside an authorized baseline | `DERIVED` plus exact confirmation; links remain candidate-only |
| Grounded as-built requirement within authorized baseline scope | `PENDING_VERIFICATION` in Base with validated relationships; not verified or delivered |
| Directly sourced requirement | `PROPOSED` UR or SR |
| Missing human decision or ambiguity | Decision gate; `BLOCKED` only when work cannot proceed |
| Known future work | `DEFERRED` with reason, owner, and target |
| Capability/specification gap | Gap linked to affected traces |
| Unclear ownership/cross-cutting concern | Triage backlog |
| Contradicted or removed behavior | Conflict or `OBSOLETE` with replacement |
| Delivered item found defective, or its decision reversed | Attributable demotion of the existing item (§Attributable demotions); never a duplicate requirement |

Normal delivery selects the single active release from the project's registry
with a `USER:` source. Drift between repository records and the
store binding is a defect to report, not a variance to work around. `DERIVED`
items are not release commitments. Preserve competing authoritative sources
and request a human decision; never resolve intent by timestamp or weaken a
trace to make records agree.
