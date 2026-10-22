# Requirement-driven delivery process

This document defines the delivery process. Skills apply it. The project's CLI is the only way to read or change delivery state; its server is authoritative. This process does not define local ledger formats or add a second record system.

## Canonical model

```text
EPIC -> required UR/SR requirements -> code and test references
UR -> acceptance scenario -> TEST_CASE -> TEST_RESULT
UR acceptance scenario -- may require --> SR
SR -> CODE -> TEST_CASE -> TEST_RESULT

EPIC: PROPOSED -> IN_PROGRESS -> IN_REVIEW -> DONE
UR/SR: PROPOSED -> IN_PROGRESS -> IN_REVIEW -> DONE
      PROPOSED -> DONE (verified existing behavior, explicit acceptance)
        BLOCKED | DEFERRED | OBSOLETE when applicable

Human decisions: implementation approval -> completion approval
                 or explicit acceptance of verified existing behavior
```

Every product Epic must include one or more UR/SR requirements that
cover its in-scope behavior, with explicit acceptance criteria and planned
code/test traces before implementation approval. Use URs for user outcomes
and SRs for system behavior as applicable; both kinds are not required for
every Epic. Acceptance checks written only in a saved plan do not substitute
for requirements. Small changes may use the exception in
[Small changes](#small-changes). During Epic delivery, connect those requirements to actual implementation and criterion-level test
evidence before completion review. Meta-work remains outside the product
loop under `AGENTS.md` §Product work and meta-work.

The Epic is the normal unit of authorized delivery. Requirements express
behavior and link to implementation and evidence; they do not replace the
Epic's saved plan or human approvals. Upper evidence exercises a user outcome;
lower evidence exercises a system behavior. Reverse engineering records
requirements and source traces directly. An explicit request to verify and
accept behavior already present may use the separate PROPOSED-to-DONE path
below; requested implementation or test changes follow [Work scope](#work-scope).

## Authority and sources

Humans decide product intent, scope, architecture, acceptance, priority, and
workflow. Agents establish facts, write and review plans, implement approved
work, and report evidence. They do not infer human approval from silence,
prior work, a passing test, a commit, or a repository state.

Use the project's CLI to read the Epic and its
requirements, save a plan, obtain recorded approvals, and read back results.
If it lacks a needed read or write, report the tooling gap through the
project's named channel. Do not read or edit process state through a local
ledger, private database connection, or direct API call.

Ground product claims in product documents, code, tests, or observed runs.
When a document supplies a claim, read the relevant passage and quote or
faithfully summarize it with the file and location. Code proves existing
behavior, not intended behavior. Repository status and a change list describe
the repository; they do not establish process approval or completion.

The CLI attributes human decisions and approvals to the person
who made them. In the saved plan, cite product sources in ordinary prose with
links or file locations and enough quoted or summarized context for another
reviewer to inspect the claim. A link audit checks whether a reference
resolves; it does not prove that the source supports the claim.

## Items and traces

| Item | Owns |
|---|---|
| `EPIC` | Human-readable outcome, explicit scope, saved plan and exact proposed content, decisions, lifecycle, implementation and completion approvals |
| `UR` | Actor, context, user outcome, sources, acceptance scenarios, optional Epic relation |
| `SR` | One independently implementable system behavior, source, boundary, technical context, code and test links |
| `CODE` | Implementing files, symbols, revision, branch and changes |
| `TEST_CASE` | Stable test identity, target clause and expected observation |
| `TEST_RESULT` | Outcome, command/report, environment when relevant, tested revision and validity |

Requirement and Epic identifiers come from the authoritative process store.
Use those IDs in plans, code/test links, review notes and evidence.
Never invent, derive, or substitute a path, slug, code, or external key for an
identity the store owns.

A trace records relationships, not execution order. An Epic plan names the
selected requirements and their expected code/test evidence. An UR trace is
its sourced outcome, scenarios, current upper evidence and each SR the
scenario requires. An SR trace is `SR -> CODE -> TEST_CASE -> TEST_RESULT`.
Do not invent parent relations or code links to make a trace look complete.
A code link identifies implementation; it does not prove behavior. A test
that covers several clauses identifies each clause and assertion it checks.

## Lifecycle

`PROPOSED` is a draft. Human implementation approval moves the Epic and its
named requirements directly to `IN_PROGRESS`, retaining the exact approved plan
and proposed requirement content separately from published content. It atomically
locks the whole requirement set to that Epic and one user. A conflicting owner
must leave the whole approval unchanged.

The approved Epic plan, its scope and the owned requirement content are read-only
through `IN_PROGRESS` and `IN_REVIEW`. Do not revise an active approval in place
or treat a new request as permission to edit it. Surface a requested scope or
acceptance change as a human decision and record the new work separately;
implementation continues only within the approved scope. New work requires
its own saved plan and human implementation approval; unaffected authorized
implementation may continue. The narrowly scoped legacy recovery operation below
is an explicit human-authorized exception that ends the active approval rather
than revising it in place.

`IN_REVIEW` means implementation is complete and awaits human completion
approval. The ownership lock, approved proposal and prior published requirement
content remain unchanged. `DONE` follows human completion approval: atomically
publish the approved content, increment each requirement revision once, retain
an immutable accepted version with criteria and trace meaning, record the
actor, time, reason and originating Epic, and release ownership. A failed write
must roll back all effects; repeating completion must not publish twice.

For an explicit request to verify and accept behavior that already exists,
unowned `PROPOSED` requirements may move directly to `DONE` after complete,
applicable passing evidence and explicit human acceptance of the exact
requirement content and evidence. The supported acceptance operation atomically
publishes the accepted versions, increments each revision once, retains the
immutable accepted versions with criteria and trace meaning, and retains the
evidence links. It records the acceptance actor, time and reason with no
originating Epic, and leaves the scope unchanged on failure. It is idempotent.
It must reject changed content or evidence, incomplete or failing evidence, and
any requirement owned by active implementation work. It does not create an
Epic, an implementation approval, or an ownership lock. Already-DONE
requirements are left unchanged. A missing human decision leaves requirements
PROPOSED. If the CLI does not provide this operation, report
the tooling gap and leave them PROPOSED.

This path describes and accepts existing behavior only. If behavior or tests
need to change, route that work through an Epic plan and implementation
approval; verification evidence alone never authorizes the change.

There is no READY/TODO entry step, requirement stage, evidence-conclusion status,
plan-revision token, fingerprint, generic gate or separate ownership framework.
Requirements keep their own UUIDs and types; Epic membership and criterion
ownership are direct containment, while links among requirements, code,
documents and tests are traces. Requirement-to-document traces use `relates`.
`BLOCKED`, `DEFERRED` and `OBSOLETE` retain their actual reason and history when
applicable; none authorizes publication or erases an accepted version.

### Reverse engineering

Reverse engineering records requirements and their typed Traces to source files
in the server store. Read the code and documents within the user's requested
scope and write statements that describe the behavior actually found. Use the
existing domain vocabulary and preserve existing requirement content.

Record requirements, file references and their traces atomically. A source file
reference has its identity, repository and path; a known revision is
optional descriptive context. Do not hash files, documents, inventories or
reports, capture source snapshots, or require an authorization run, publication
group, proof packet, clean checkout, matching revision, coverage gate or
as-built acceptance before recording. Source changes do not invalidate recorded
requirements or their traces.

New requirements start as PROPOSED. Recording is not implementation approval,
verification, compliance approval or completion. Existing requirements may be
referenced by identity and kind without replacing their content.
Authentication, tenant and System scope, valid endpoints and atomic
writes remain normal store integrity rules.

After recording, verification is a separate pass using
`rdd-reverse-engineer-verify`: it checks the current PROPOSED content against
the implementation, maps every in-scope criterion to meaningful assertions,
and records actual test outcomes and traces. Complete passing evidence is a
prerequisite for the separate `rdd-reverse-engineer-accept` pass, not a
prerequisite for recording. Acceptance presents exact targets and evidence to
the human and applies only an explicit attributable decision through the
supported atomic operation above. If behavior or tests need to change, return
to [Work scope](#work-scope) to choose small-change or Epic delivery.
Verification does not alter lifecycle state, and neither recording nor passing
evidence implies human acceptance.

Local working-set files may hold store projections and development inputs.
They are not a second authority and need no fingerprint or synchronization
ceremony. Read back the recorded requirements and traces through the CLI and report what was recorded and any remaining work.

## Human approvals

The human reviews a saved Epic plan and its exact proposed requirement
content, criteria and traces. Implementation approval retains that content and
the decision's actor, time and reason. Read back the approval and requirement
ownership through the CLI before implementation. A later
changed intent needs fresh human approval; never silently change what an
existing approval accepted.

Present the outcome, included requirements, exclusions, sources, test strategy,
decisions and risks in plain language. An approval refers to saved content that
the human can read, not a technical hash or plan-revision token.

Human completion approval is separate. Present the approved proposal beside the
still-published baseline, the implementation evidence, review findings, known
failures and remaining risks. Only that approval publishes the approved
requirement content, raises revisions, retains immutable accepted versions,
records audit history and releases ownership in one transaction. Implementation
approval, review, an autopilot grant, a commit, merge or silence is not a
completion answer.

For existing-behavior acceptance, present the exact PROPOSED content and its
criterion-level verification evidence, tested revisions, exclusions and
remaining risks. Obtain explicit acceptance attributable to the human for
those targets and that evidence. A passing test, prior approval to record or
verify, or acceptance of different content is not acceptance. The supported
operation and its read-back follow the lifecycle rules above; this decision
does not stand in for implementation or completion approval on Epic work.

Git and PR/CI systems own code revisions, commits, merges and conflicts. Retain
links and tested code revisions for traceability; their hashes do not define
requirement identity, versions or approval validity. Never describe a code
branch as merged or deployed without inspecting that external state.

## Work scope

For changed behavior or tests outside the small-change exception below, work
on the Epic scope the user selected. If scope is missing, ask which Epic to use
or whether to prepare a sourced
proposal. Read its ID, saved plan, approved proposal, human decisions
and affected requirements from the CLI. Choosing work
creates no separate selection record or Epic lock. Requirement ownership is
acquired by implementation approval, not session entry.

For an explicit request limited to recording, verifying or accepting existing
behavior, an Epic is not required. Read the System and requirement IDs,
current content, evidence and implementation ownership from the CLI, then route to the corresponding reverse-engineering pass. If the
request entails a behavior or test change, choose small-change or Epic delivery
under this section.

Keep work within the approved outcome and exclusions. Disclose changed intent,
acceptance or scope and obtain fresh human approval before implementing it.
Do not infer scope from a branch, latest activity, changed files or a search
result.

### Small changes

Small changes do not require an Epic. A small change has a clear requested
outcome, a narrow code and test boundary, and focused tests plus proportional
regression checks that can verify it. It may relate to a single system
requirement; that relationship is optional and does not by itself require an
Epic. When present, cite the requirement in the PR or handoff. Judge the
behavior and dependencies, not the number of changed lines. State the scope
and expected observation briefly in the session; the user's request authorizes work within that scope.
No separate Epic plan or implementation-approval ceremony is required.

Small changes follow the [AI TDD inner loop](#ai-tdd-inner-loop), including
RED/GREEN execution, regression checks and retained evidence.

Do not create Epic, requirement or execution-registration records solely to
use this exception. PR and test evidence suffice for this path; they do not
publish or complete governed requirements. Existing Epic-owned work stays
within its approved Epic and evidence obligations. If investigation expands
the change beyond a narrow, verifiable scope, return to the Epic path before
implementing the expanded scope. Security, tenant isolation, data integrity,
project checks and human merge, deployment and release authority still apply.

A small change has no Epic to route a backlog, gap or tooling record to. When
it delivers such a record, name the record in the PR. If the change cites a
requirement, use the CLI to mark the record as routed to that requirement and
read it back. Once the change merges, use the CLI to mark the record as closed
by that pull request and read it back.

## Autopilot

Autopilot is a human-granted way to continue implementation across a bounded
sprint; it is not a completion approval. The grant names the Epic or Epics,
end date and excluded areas and is recorded with its `USER:` source through
the CLI. Under a current grant, the agent may continue
within an approved saved plan and may record ordinary, reversible product
assumptions for later human confirmation. Unless already explicitly authorized, it must stop for decisions whose
wrong answer is hard to undo, including deletion or migration of existing
data, security or authorization, billing or legal terms, and external-party
contracts. It must not expand the approved Epic scope on its own.

Autopilot does not waive red-first testing, security, tenant isolation, data
integrity, project commit checks, independent completion review, accepted-content
read-back, or explicit human completion approval. The human remains in
control of merge, deployment and release. Report assumptions, exclusions,
failures and incomplete work at sprint closeout.

## Planning and readiness

Epic gates check the recorded conditions for planning readiness, implementation
approval, completion review and completion approval.

Planning produces a saved, reviewable Epic plan and exact proposed content. It is useful to a
builder and an independent reviewer without depending on session memory. It
contains:

- the Epic's ID, desired user outcome, included requirements and
  explicit exclusions;
- authoritative sources and the claims they support, unresolved decisions,
  assumptions, conflicts and risks;
- relevant architecture, state, data-flow, integration and failure-mode
  reconnaissance at identified source revisions;
- the change boundary, affected code and interfaces, dependencies, and what
  must remain unchanged;
- acceptance scenarios and the lower-level requirements they exercise;
- expected failing tests for changed behavior, plus focused and proportional
  regression checks;
- operational, migration, accessibility, security and tenant-isolation
  considerations that apply to the change;
- the proposed content, criteria and traces that completion would publish,
  and how implementation and accepted content will be reviewed.

Read actual source documents, code and tests before making claims. Search for
existing ownership and constraints, then verify each relevant match in its
source. A source inventory is not an implementation plan. For persisted or
shared state, inspect the actual writers, readers, existing data shapes,
partial-failure behavior and recovery; document the risks and mitigation in
the saved plan. Do not replace reconnaissance with a list of source links.

A plan with no requirements, uncovered in-scope behavior, missing
criteria or missing planned code/test traces is not ready for implementation
approval. Return it to planning rather than approving plan-only acceptance
checks.

An independent cold review checks the saved plan against its sources, scope,
acceptance, architecture, state boundaries, risks and test strategy. Record
material findings and correct the plan before asking for implementation
approval. Renew the review whenever its reviewed content changes, including
decision answers, membership, test definitions, candidate or relevant evidence.
An identical saved value does not itself require another review. Record explicit
resolutions for material findings, including findings from superseded reviews;
editing the plan alone does not record their disposition. The review audits the planned
change, not the amount of prose or the presence of links.

### Epic gates, verification and review

The four Epic gates and their CLI checks are:

| Epic gate | Required conditions | CLI check |
|---|---|---|
| Plan readiness (`plan-ready`) | Saved requirements, criteria, code/test traces, test definitions and verification obligations; answered decisions; current independent plan review and resolved material findings. | `modernpath epics gates check <epic-id> --phase plan-ready --json` |
| Implementation approval (`implementation`) | Plan readiness and requirements available for ownership. Approval also checks the exact reviewed plan and, for declared decisions, the retained presentation shown to the user. | `modernpath epics gates check <epic-id> --phase implementation --json` |
| Completion review (`completion-review`) | Current candidate, intact membership and ownership, passing results for every obligation, qualifying RED evidence for changed criteria, current implementation review confirming scope, and resolved material findings. | `modernpath epics gates check <epic-id> --phase completion-review --json` |
| Completion approval (`completion`) | Completion-review conditions while the Epic is IN_REVIEW. Approval also checks the exact retained completion presentation and requires the user's explicit decision. | `modernpath epics gates check <epic-id> --phase completion --json` |

Each explicit check records its readiness result and blockers in the Audit Trail,
with the actor, server time and Git SHA source, without changing Epic lifecycle.
The CLI uses checkout HEAD and known dirty status by default; `--git-sha` supplies
an explicit full SHA. When no SHA is available, the record says so. A supplied
SHA is trace information and does not prove that the repository was verified.
Both passing and blocked checks are recorded. Reading a preview or review bundle
does not record a check. Passing a check grants no approval. The transition commands recheck the
conditions: `modernpath epics plan approve-implementation`,
`modernpath epics plan submit-review`, and
`modernpath epics plan approve-completion`. Follow the CLI reference for their
required Epic ID, input files and approval arguments.

After recording the independent plan review, `rdd-cold-review` runs the
`plan-ready` check. Before asking for implementation approval,
`rdd-entry-review` runs `implementation`. `rdd-completion-review` runs
`completion-review` before submission and `completion` before asking for
human completion approval. Inspect each recorded result and resolve blockers
through the appropriate pass. None of these checks records a human approval.

Before the final plan review, create or reuse test definitions and declare
verification obligations for every active criterion and required project check.
Each obligation identifies its test, boundary and expected observation;
each criterion states whether behavior changes and therefore needs RED evidence.
Record applicable boundaries and reasons for exclusions, the plan author's
context, and required human decisions using identities issued by the CLI. Retain each decision question beside its effective human answer.
These are test definitions, not executable test changes before approval.
Obtain and record answers to declared decisions before the final independent
review. A later answer changes the reviewed content and requires a renewed review.

The agent handling the task fetches and retains the authoritative review bundle, supplies it
to a context independent of the author, and records the returned review against
that exact bundle. The reviewer covers every criterion and obligation, reports
material findings separately from notes, and states limitations. Distinct author
and reviewer context labels attest to separate contexts; they do not prove that
different people performed the work. Do not replace an independent review with
an author-assigned label. The agent handling the task records finding resolutions and reads
back the review; a changed bundle needs review again.

Retain the exact plan and decision answers presented for implementation approval
before asking the human. Apply their answer to that retained presentation; do
not refresh it silently to reuse an earlier answer. At completion, similarly
retain the presentation containing the approved proposal, published baseline,
candidate, evidence, reviews and readiness before asking. Changed presented
content requires a current presentation and a fresh human answer.

A legacy active Epic whose approved plan lacks verification obligations needs
explicit human-authorized recovery through a supported audited operation. Such
recovery returns it to PROPOSED, releases implementation ownership, preserves
published content and history, and requires a new saved plan, independent review
and implementation approval. It is not permission to edit an active approval.
If that operation is unavailable, report the gap and leave the work incomplete.

## Development loop

Small changes use the [AI TDD inner loop](#ai-tdd-inner-loop) directly.
The Epic delivery loop is:

```text
CHOOSE EPIC -> SAVE PLAN AND PROPOSED CONTENT -> ANSWER DECLARED DECISIONS
-> RETAIN REVIEW BUNDLE -> INDEPENDENT REVIEW + RECORDED RESOLUTIONS
-> HUMAN IMPLEMENTATION APPROVAL / IN_PROGRESS + REQUIREMENT OWNERSHIP
-> REGISTER / EXECUTE / FINALIZE RED -> CANDIDATE
-> REGISTER / EXECUTE / FINALIZE GREEN -> CLEANUP -> VERIFY
-> RECORDED INDEPENDENT IMPLEMENTATION REVIEW -> IN_REVIEW
-> HUMAN COMPLETION APPROVAL / ATOMIC PUBLICATION + REVISION + AUDIT + UNLOCK
```

Enter with `rdd-start`. Use `rdd-deliver` for an end-to-end request, and a
focused skill only when the user explicitly ends the task at that pass. Each
skill uses the project's CLI; it does not prescribe
or invent CLI commands. A missing CLI capability is reported as a tooling
gap.

| Phase | Skill | Exit |
|---|---|---|
| Session entry and scope | `rdd-start` | Small-change scope and RED/GREEN checks identified, Epic plan read, or System/requirements/evidence read for an explicit existing-behavior pass; next phase identified |
| Discover and classify | `rdd-discover` | Sources and proposed scope/decisions recorded for the selected Epic |
| Plan | `rdd-plan` | Saved plan and exact proposed content ready for independent review |
| Cold review | `rdd-cold-review` | Findings resolved or clearly disclosed; reviewed content identified |
| Implementation approval | `rdd-entry-review` | Human implementation approval read back for the exact saved plan and exact proposed content |
| Implement | `rdd-build` | Red-first implementation and focused checks complete |
| Verify | `rdd-verify` | Required lower and upper evidence passes at the candidate revision |
| Review, publish and complete | `rdd-completion-review` | Human-approved content atomically published/versioned/audited, ownership released, or incomplete work reported |
| Verify existing behavior | `rdd-reverse-engineer-verify` | Current PROPOSED content traced to existing behavior and complete applicable test outcomes recorded; no lifecycle transition |
| Accept existing behavior | `rdd-reverse-engineer-accept` | Exact verified targets explicitly accepted by a human and atomically published as DONE, or left PROPOSED with the tooling gap reported |
| Discoveries and blockers | `rdd-triage` | Discovery routed into a draft plan or reported as a separate-work decision/blocker; active approvals preserved |

A focused pass is a handoff, not whole-task completion. Stop when its exit is
reached and report what remains.

### AI TDD inner loop

Establish each changed clause's expected failure before implementing it.
For Epic work, begin after implementation approval. Small changes use the
same test sequence, omitting Epic candidate declaration, execution registration
and store finalization/read-back; retain the evidence in the PR or handoff
instead. For each clause:

1. Write or select a focused assertion that expresses the approved behavior.
2. Register the RED execution through the CLI before running
   it, identifying obligations, revision, command and environment. Run it
   against the current implementation and confirm the expected failure;
   a build error or unrelated failure is not RED evidence.
3. Finalize and read back the actual RED evidence before registering GREEN.
   Implement the smallest change that makes the assertion pass.
4. Declare the candidate revision before GREEN. Register each execution before
   running the focused test and proportional boundary and regression checks;
   finalize each returned execution identity with its actual evidence.
5. Clean up without changing behavior and rerun affected checks with fresh
   registrations. A changed candidate needs fresh passing evidence and review.
6. Re-run affected user scenarios and verify the complete changed trace.

Registration is required for evidence claimed against active declared obligations,
including CI and delegated verification. Use the CLI's authorized
registration account; a runner attribution does not grant write authority. A
candidate is required before GREEN, not before RED. If GREEN was registered too
early, finalize the valid original RED and register and execute a new GREEN;
do not manufacture a replacement RED. Every rerun needs a new registration.
Finish evidence collection before the implementation review; a change to reviewed
evidence requires renewed review before submission.

Do not reconstruct RED or passing evidence after the fact. Preserve the test
output, command, environment and revision for each result. Never skip or
weaken a failing assertion to pass a project commit gate. Deletion checks prove scoped machinery is absent and retained data is preserved.
When the human explicitly accepts broken affected paths, report their actual
failures; do not restore removed machinery, invent replacements, weaken tests
of retained behavior or manufacture RED/GREEN evidence.

If an upper/user-flow failure remains after lower-level requirements pass,
diagnose it. Repeat the loop when it is within approved behavior. If fixing it
requires changed scope, acceptance, architecture, priority or a material
technical decision, keep the active approved plan and owned requirements
read-only, surface the change as a human decision and record new work
separately for its own plan and human implementation approval. Continue
unaffected work within the active approved scope. Record an external impediment
as blocked. UI evidence requires the live stack, loaded assets and inspected
browser output; never mutate production-like data to verify rendering.

### Delegated passes

A cold review or verification pass may run in an independent context. It
returns findings, verdict and sources to the agent handling the task, who
records them through the CLI. Recording a reviewer's verdict does not make that
agent an independent reviewer. A delegated pass that encounters a permission, authentication or
process-tool refusal stops and returns it verbatim; it does not retry by
rewriting or splitting the refused action.

## Evidence and completion

A test result records its stable test identity, target requirement, outcome,
command/report, environment when relevant and exact code revision. Outcomes
are `PASS`, `FAIL` or `SKIP`. A rerun is a new observation. Only a passing
result that actually exercises the cited assertion at the identified revision
supports the trace. A broad suite proves only assertions it exercised. RED
and passing evidence are separate results; a RED demonstrates regression
sensitivity at its recorded revision, not execution of the passing
implementation.

Choose evidence by boundary: unit/property for domain rules, component and
browser for UI, endpoint or contract tests for APIs, integration for
persistence and external integrations, schema conformance for cross-service
contracts, and harness/smoke for operations. Run project-required checks and
proportional regression suites after cleanup. Keep test identities and
requirement links stable; file line numbers are navigation aids,
not identities.

Before completion review, run the required checks and record their actual
outcomes. Review from a context independent of the author. Resolve material
correctness, security, data-loss, contract, traceability and testability
findings; explain remaining limits. Keep code integration in version control
under the project's rules and the human's PR-specific merge approval.

Audit the approved Epic, exact proposed content, implementation and traces.
Identify what each result actually proves. Disclose exclusions, failures,
assumptions and deferred items. For explicitly authorized deletion, broken
features or checks remain reported cleanup outcomes; they never justify
restoring removed machinery or claiming a working runtime without proof.

Submit implementation for review while retaining published requirement content
and ownership. Present the review and evidence for the human's completion
answer. After explicit approval, read back the atomically published requirement
content and immutable versions, incremented revisions, actor/time/reason/Epic
audit and released ownership. Never infer that answer or claim `DONE` from
code, test, review or repository state. Merge and deployment remain separate
human decisions.

## Process state and reconciliation

The server holds the authoritative records for the Epic,
requirements, saved plan and exact proposed content, implementation,
completion and existing-behavior acceptance decisions, and their
relationships. This process defines no file-backed ledger,
local process snapshot, generic gate format, local plan authority, or
synchronization/reconciliation protocol. Ordinary product documents remain
in their project repositories and are read or refreshed by that project's
documented documentation workflow; they do not become delivery state.

After each process operation, read back the authoritative result and confirm
that the expected Epic, saved/approved content, approval, requirement links or
completion state is present. If a write outcome is uncertain, read the server
before repeating the operation. Never construct or edit a replacement record
locally. If an expected server read or transition is unavailable, surface the
tooling gap and leave the item explicitly incomplete.

## Discoveries and conflicts

| Discovery | Route |
|---|---|
| New behavior or changed user outcome | Revise the saved plan only while the Epic is PROPOSED and obtain human implementation approval; after approval, preserve the active plan and owned requirements and record new work separately for its own plan and human approval, continuing unaffected authorized work |
| Missing human decision or ambiguous intent | Ask the human and record the answer with its source before relying on it |
| Technical failure inside approved behavior | Continue red-first implementation and verification |
| External impediment | Report the blocker, its owner and the next action |
| Known future work outside the approved scope | Disclose it and leave it for a separately authorized Epic |
| Contradicted or removed behavior | Explain the conflict and ask the human how the approved outcome changes |

Backlog, gap and tooling records live on the server like every other delivery
record: they are created, routed and closed there, through the CLI. When a
saved plan delivers a record, mark the record as routed to that Epic. After
human completion approval, mark it as closed by that Epic. If the human wants
a record left open, leave it open and add a note saying why. Whether a record
is open, routed or closed (its disposition) is read from the server, not from
the plan text; a plan only lists the records it delivers. A discovery that
exists only in a note is not yet a record.

Preserve competing authoritative sources and ask for a human decision; never
resolve intent by timestamp, branch state, or a convenient test result.
