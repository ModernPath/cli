# Requirement-driven delivery process

This document defines the delivery process. Skills apply it. The project's CLI is the only way to read or change delivery state; its server is authoritative. This process does not define local ledger formats or add a second record system.

## Canonical model

```text
EPIC -> required UR/SR requirements -> code and test references
UR -> acceptance scenario -> TEST_CASE -> TEST_RESULT
UR acceptance scenario -- may require --> SR
SR -> CODE -> TEST_CASE -> TEST_RESULT

EPIC: PROPOSED -> IN_PROGRESS -> IN_REVIEW -> DONE
      IN_REVIEW -> IN_PROGRESS (a human's not-yet at completion review)
UR/SR: PROPOSED -> IN_PROGRESS -> IN_REVIEW -> DONE
      IN_REVIEW -> IN_PROGRESS (returned with their Epic)
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
| `TEST_CASE` | ID, title, existing revision and approval attribution; typed links to requirements, criteria and code |
| `TEST_RESULT` | Current actual outcome for one criterion and TestCase, with scoped result-to-criterion and result-to-TestCase Traces |
| `EC` | Engineering Constraint record, ref `EC-<number>`: a human-owned rule of the System, Proposed, Active or Deactivated, effective from a person's accept to a person's deactivate, authored through `rdd-constraint`; its own item, never a requirement kind |

Requirement and Epic identifiers come from the authoritative process store.
Use those IDs in plans, code/test links, review notes and evidence.
When changing requirement-driven behavior, put the requirement ID in a short
comment beside the code that implements it and beside the test assertion or
group of assertions that verifies it. Include the criterion ID when it helps
distinguish which clause the assertions cover. Stored Traces remain the
authoritative record of these relationships.
Never invent, derive, or substitute a path, slug, code, or external key for an
identity the store owns.

A trace records relationships, not execution order. An Epic plan names the
selected requirements and their expected code/test evidence. An UR trace is
its sourced outcome, scenarios, current upper evidence and each SR the
scenario requires. An SR trace is `SR -> CODE -> TEST_CASE -> TEST_RESULT`.
Do not invent parent relations or code links to make a trace look complete.
A code link identifies implementation; it does not prove behavior. A test
that covers several clauses identifies each clause and assertion it checks.

## Releases, Initiatives and the Roadmap

A Release is a delivery grouping of a System: the Epics delivered together and
the Initiatives shown under it, with a target date when it has one. Each Epic
is in a Release. An Initiative is the business and UX planning container for
one clear, scoped feature or change to one bounded context, which its Epics'
requirements share, placed in a horizon (Now, Next or Later) and delivered by
its Epics. The Roadmap is the System's Initiatives by Release and
horizon, with the Now plan its business owner agrees.

The chain runs Initiative → Epic → requirement → trace: Epics deliver an
Initiative, requirements state an Epic's behavior, and traces connect a
requirement to code and evidence. An Initiative stays in its stored Release.
When a Modernization Release closes, a close continues a partly done
Initiative in a linked Initiative of the next Modernization Release: a partly
done Initiative has a done Epic in the closing Release or continues another,
and the linked Initiative takes the Epics carried to that Release and the
decisions that are not yet proved. A close is refused when no Release can take
them.

Read the Roadmap with `modernpath roadmap show` before planning an Epic: it
lists each Initiative with its context and the Epics it covers. Change the
Roadmap only on a person's word: `modernpath roadmap apply --file <plan.json>`
applies a plan of Initiatives in one call, `--dry-run` first lists what it
would do, and `--agent` names the agent beside the signed-in person. Applying a
Roadmap plan approves no Epic and starts no build.

An Initiative that replaces part of the legacy system has a selection: keep,
change and drop rows, a reason on each drop, and one row per legacy requirement
for each System. An agent decides a selection at volume: it reads a context
with `roadmap selection`, suggests each row's outcome with its reason, sums the
file up in plain words for the person, checks it with `roadmap apply --dry-run`,
and applies it only on the person's word, naming itself with `--agent`.

An Epic drafted from an Initiative that replaces part of the legacy system
links each new requirement to the legacy requirement it modernizes with a
`modernizes` trace in its saved plan. The platform admits that trace only for
a keep or change decision of the Epic's own Initiative; a later Epic carries it
unchanged or retires it by holding its source, and reverse engineering never
records it. Such an Epic's implementation approval waits until the business
owner's agreement holds its Initiative unchanged; the agent reports the gate's
blocker and asks the business owner rather than working around it. In place,
the plan takes the requirements it changes with their revised statements and
links tests to kept requirements under their keep decisions, and the agent
presents the kept requirements at risk with a fallback for each before asking
for approval.

A Release and an Initiative each carry a planning context: the decisions,
assumptions, open questions and references that hold for all the work under
them, the Release's for the whole Release and the Initiative's for its part.
An Epic is planned and built within both. The plan review reads them from the
review bundle, and while the Epic is PROPOSED a change to either makes its plan
review outdated. The agent reads them when it enters a session on an Epic and
again before it plans and before it builds, with
`epics get <id> --fields planning_contexts` or `roadmap show`; the context hook
does not carry them.

A System's Active Engineering Constraints are read at the same moments: at
session entry and before the agent plans and before it builds, with
`constraints list --state active`, and each one the change may touch with
`constraints get <ref>`; the Active text is the current revision, not the row
text when a change is pending. The review bundle carries them as
`engineering_constraints` (ref, revision, title, statement, rationale and
verification method), and the cold review reads them beside the planning
contexts. Unlike a planning context, a constraint accepted or changed after a
plan review does not outdate that review. A finding that cites a constraint
names it as `EC-<number>` with the revision read and is information, not a
verdict; a finding whose only ground is a constraint is recorded with severity
`note` and blocks no gate.

A planning context locks when the business owner agrees it and at the first
approval of an Epic that reads it. When the business owner agrees the Now plan
on the Roadmap, the planning context of each Initiative in it locks, and so
does the planning context of the Release each one is shown under. Approving an
Epic locks the planning contexts of its Release and of its Initiative, in any
horizon, if they have never been locked. A planning context with nothing in it
stays unlocked. Nobody can edit a locked planning context: its page, Chat's
plan card and `roadmap apply` all refuse the change. A locked planning context
changes only through Open for change, by a team member with a reason, on the
Release page or the Initiative's page; the command line has no command for it,
so the agent asks a person. Opening it reopens the agreement of the Now plan,
and the business owner sees the opening and its reason on the Roadmap. Approving
another Epic does not lock an opened planning context: the next agreement locks
it again.

## Lifecycle

`PROPOSED` is a draft. Human implementation approval moves the Epic and its
named requirements directly to `IN_PROGRESS`, retaining the exact approved plan
and proposed requirement content separately from published content. It atomically
locks the whole requirement set to that Epic. A conflicting Epic owner
must leave the whole approval unchanged. Human implementation and completion
approval also add the Epic to the approver’s declared focus atomically. Focus
preserves other lanes and grants no exclusive permission to work on the Epic.

The approved Epic plan, its scope and the owned requirement content are read-only
through `IN_PROGRESS` and `IN_REVIEW`. Do not revise an active approval in place
or treat a new request as permission to edit it. A human may explicitly return
either active state to planning when the scope, acceptance or plan needs to
change. This ends the current approval before editing; it does not revise that
approval in place. Unaffected work may continue within the approved scope until
the return is applied. Separately authorized work may use another Epic.

`IN_REVIEW` means implementation is complete and awaits human completion
approval. The ownership lock, approved proposal and prior published requirement
content remain unchanged. A human's not-yet at completion review returns the
Epic and its owned requirements to `IN_PROGRESS` with the plan, the
implementation approval and the ownership kept; record the explicit human
reason against current server state, continue the build and submit for review
again. `DONE` follows human completion approval: atomically
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
It must reject ineligible current content or evidence, incomplete or failing evidence, and
any requirement owned by active implementation work. It does not create an
Epic, an implementation approval, or an ownership lock. Already-DONE
requirements are left unchanged. A missing human decision leaves requirements
PROPOSED. If the CLI does not provide this operation, report
the tooling gap and leave them PROPOSED.

This path describes and accepts existing behavior only. If behavior or tests
need to change, classify that work under [Work scope](#work-scope): use the
small-change path or an Epic plan and implementation approval as applicable.
Verification evidence alone never authorizes the change.

There is no READY/TODO entry step, requirement stage, evidence-conclusion status,
plan-revision token, fingerprint, generic gate or separate ownership framework.
Requirements keep their own UUIDs and types; Epic membership and criterion
ownership are direct containment, while links among requirements, code,
documents and tests are traces. Requirement-to-document traces use `relates`.
`BLOCKED`, `DEFERRED` and `OBSOLETE` retain their actual reason and history when
applicable; none authorizes publication or erases an accepted version.

### Returning to planning

An `IN_PROGRESS` or `IN_REVIEW` Epic can return to `PROPOSED` through the
supported CLI operation with the human's explicit decision and reason. Any authorized workspace member records the decision.
If the CLI cannot perform the return, report the tooling gap; do not change
the process store through another channel.
This applies whether or not verification declarations already exist. `DONE`
stays closed; changed work uses a follow-up Epic with its own plan and approvals.

The operation atomically withdraws implementation approval, releases requirement
ownership and returns the Epic and selected requirements to `PROPOSED`. Retain
the draft proposal, published requirement content, revisions, accepted versions,
and all prior approvals and reviews. Record the actor, time,
reason, prior state, withdrawn approval, criteria snapshot and carried decisions (`carry_decisions`) in the Audit Trail. The current plan
stays on the Epic. An unauthorized actor, ownership conflict or failed write
leaves all effects unchanged.

Return to planning clears current proof for each published criterion whose
approved meaning differs from its published meaning; unchanged criterion proof
remains current. Removed unpublished criteria lose their proof. Revise the draft,
record a new plan review and obtain human implementation approval. Retest each
changed published criterion after reapproval. A carried decision keeps its question and
answer. Record a new independent implementation review and obtain separate
human completion approval.

A change to a planning context returns no Epic by itself. Each approved Epic
that reads a changed planning context lists the change on its page and in its
completion presentation (`epics gates presentation`): the opening with who
opened it and why, each edit, and the lock that followed. Any authorized workspace member returns it to planning, and only if the change needs a new plan; otherwise
the build continues within the approved plan.

Build an Epic from the tip of the sprint branch (§Minimal tests and the full
run). When the Epic lands, push its commits to the sprint branch under the
project's rules for pushing, and start the next Epic from the branch's tip.
Until it lands, its commits are not pushed to the sprint branch. A fix is built
from the tip and pushed, under the same rules, when its red-first test and the
quick checks pass. An Epic whose commits are not yet on the sprint branch
brings the new tip in before they are pushed and reruns the affected checks.
Bringing the release branch into the sprint branch with no conflict in the
Epic's files is not a return to planning. Rerun the affected deterministic
checks after bringing it in. A conflict that changes the Epic's behavior or
scope requires a return to planning.

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

Give every requirement reverse engineering records a context from the System's
one list of contexts: each context code its requirements carry, with one name.
Read the list before recording, take a listed code and leave its name to the
list, and add a new context only when no listed one fits, with a code and a
name that no listed context has. A requirement sent with no context, or a blank
one, and no name is recorded without a context and counted as Unclassified
until it is regrouped. The store refuses a context name sent without a
context, or a code or name that disagrees with the list, and writes nothing of
that request. A recorded requirement moves to another context with a
requirement update, which applies the same list and keeps the code that
Initiatives and Modernization Release scopes name.
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
Verification records actual TestCase outcomes for each requirement criterion.
TestCase-to-criterion Traces define required coverage. Each current result has
one Trace to its criterion and one to its TestCase. The latest committed outcome
for each pair is current, including failures. An identical retry retains the
result and Trace identities; an update to one criterion leaves other criteria
unchanged. Required tests must also have scoped source Traces. Read back the
results and Traces before reporting verification. Test or requirement revisions,
source hashes and historical outcomes do not determine applicability.

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
failures and remaining risks. Within an Epic, only that approval publishes the
approved requirement content, raises revisions, retains immutable accepted
versions, records audit history and releases ownership in one transaction.
Implementation approval, review, an autopilot grant, a commit, merge or silence
is not a completion answer.

Outside an Epic, one more human act ends published behavior: the close of an
active in-place Modernization Release, by the person who closes it, sets to
`OBSOLETE` each published requirement that an Initiative of that Release, done
apart from proof when the close plan is read, dropped, unless active work owns
it, a small change is open for it or it is unfinished work of that Release.
The close page lists each one before the close, and the close is refused unless
its request names each one. The close raises each one's revision once and keeps
an immutable version of kind `release_close` with that person, the time and the
drop's reason.

An Initiative delivered by several Epics may be reviewed, approved, submitted
and completed in rounds. A round is the explicit list of the Initiative's Epics
that one command names. A round command writes the same records as the
single-Epic commands for every Epic in the round, in one transaction: every
Epic passes or nothing is written, and a refusal names the Epic. Present every
Epic of the round; the human's one answer names the round. A round review
covers every criterion and obligation of every Epic in it. A round review
records the Initiative and the Epic list it covered as ordinary review content.
Any authorized workspace member may submit a round; any one
person may approve a round's completion. Each Epic keeps its own approvals, reviews, gate checks and
completion, and can still be returned alone.

For existing-behavior acceptance, present the exact PROPOSED content and its
criterion-level verification evidence, tested revisions, exclusions and
remaining risks. Obtain explicit acceptance attributable to the human for
those targets and that evidence. A passing test, prior approval to record or
verify, or acceptance of different content is not acceptance. The supported
operation and its read-back follow the lifecycle rules above; this decision
does not stand in for implementation or completion approval on Epic work.

The business owner's agreement of the Now plan, on the Roadmap, is the human
stop for a selection, which no agent and no command records.

The domain expert's acceptance of an Initiative and a person's acceptance of a
difference are human stops: no agent records either on its own. An agent
records the domain expert's results and sign-off only on their word, with a
`USER:` reference to their decision as the source and their words as the note.

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
regression checks that can verify it. State the scope and expected observation
briefly in the session; the user's request authorizes work within that scope.
No separate Epic plan or implementation-approval ceremony is required.
The exemption removes the Epic, not requirements, acceptance criteria,
recorded evidence or the human's completion decision.

1. Before creating or revising a requirement or changing tests or behavior,
   check what the small change affects. Use the CLI to search the System's URs
   and SRs for the behavior and related terms. Read the matching requirements'
   current content, criteria and ownership. Follow their Traces to check
   related requirements, shared implementation and tests for overlap and
   downstream effects. Reuse accurate requirements and criteria. Update
   eligible unowned, unpublished drafts in place. If the intent of a
   published unowned requirement still applies, use
   `requirements proposal save` to revise it. Create a requirement only for
   behavior the existing requirements do not cover. Active Epic-owned work
   stays within its approved Epic and evidence obligations.
   Read back changed records and retain every changed clause's source,
   boundary and expected observation. Apply the findings to the requirement
   changes and focused regression checks without a separate saved assessment.
2. Establish scoped implementation and test Traces and link each criterion
   to its required TestCases before execution. Use actual source and test
   identities, with TestCase revisions; a PR description is no substitute
   for these records. Read the CLI reference for supported shapes and commands.
3. Follow the [AI TDD inner loop](#ai-tdd-inner-loop). Run the focused assertion
   before implementation and confirm its expected failure; setup or build errors
   are not assertion failures. Implement the change, rerun focused and proportional
   checks, and record actual current outcomes for each criterion and TestCase.
   Keep commands and output in PR notes.
4. Verify current requirement-to-criterion-to-TestCase-to-result chains and
   scoped source Traces. Present the proposed content, current outcomes,
   exclusions and remaining risks for the separate human completion decision.

5. After explicit human completion approval, use a supported requirement-level
   completion operation that checks current content, complete applicable evidence,
   scope and ownership and atomically publishes the accepted version, criteria
   and trace meaning with actor, time and reason. Read back publication and the
   revision increment; an identical retry must not publish twice. This is the
   lightweight completion path without an Epic. Do not use existing-behavior
   acceptance to disguise changed behavior or manufacture an Epic approval.

For each new standalone completion, declare exactly one user outcome mode:
`user_requirement_id`, `scenario_id` and `supporting_system_requirement_ids`,
or a nonblank `no_user_requirement_reason`. Use an applicable same-System UR
scenario and actual typed supporting SR relationships. Verify current scenario
and changed-SR proof. A no-UR reason never waives the changed requirement's
criterion proof. Do not invent a UR relationship to pass the check. The server
retains the declaration with the authenticated completion decision.
Already-DONE replay, Epic completion and explicit existing-behavior acceptance
retain their existing rules.

For a published unowned requirement, save a standalone proposal with the
current `expected_revision` and `expected_generation` zero. The accepted
content stays visible. Read the canonical proposal using `requirements proposal
get <UUID> --kind user|system`. Save edits and cancellation with its
`proposal_id` and `expected_generation`. After `proposal_stale`, discover the
current open proposal again and reconcile the intended edit before retrying.
Use the exact `proposal_id` and `proposal_generation` on each evidence operation
and on the separately approved DONE update. New criteria receive server-issued
IDs. Retained accepted criteria reuse current proof read-only in the proposal.
Record focused RED/GREEN and proportional regression evidence, without copying
old proof into changed criteria. A cancelled proposal changes no accepted content.

Read the exact accepted snapshot with `requirements get <UUID> --kind user|system
--version <VERSION-UUID>` after publication. Earlier accepted versions stay
unchanged. Missing historical versions remain unavailable. Retire an outdated
implementation link explicitly with `traces remove <TRACE-UUID> --requirement
<UUID> --kind user|system --reason <REASON>` only after a valid replacement
exists. Removal preserves accepted history and refuses active Epic or open
proposal work. These operations require no delivery Epic.

Check capabilities before relying on them. If the CLI cannot write criteria,
retain applicable test evidence, propose a published requirement change or
complete a small change, report the specific tooling gap through the project's
named channel. Keep the requirement and evidence work already recorded, leave
unpublished requirements PROPOSED, and report completion as blocked. Do not
invent a command, bypass the CLI, silently fall back to PR-only evidence or claim
that these instructions implement missing server behavior. This process change
does not retroactively create RED evidence or complete earlier small changes.

If investigation expands beyond a narrow, verifiable scope, return to the Epic
path before implementing the expanded scope. Security, tenant isolation, data
integrity, project checks and human merge, deployment and release authority
still apply. Repository/process meta-work remains outside the product loop
under `AGENTS.md` §Product work and meta-work.

A small change has no Epic to route a backlog, gap or tooling record to. When
it delivers such a record, name the record in the PR, use the CLI to mark the
record as routed to the relevant requirement, and read it back. Once the change
merges, use the CLI to mark the record as closed by that pull request and read it back.
A difference closed by a merged pull request is written `CLOSED by PR #<number>`
or with the pull request's address.
This closes the delivered discovery, not the governed requirement; requirement
publication still needs its explicit human completion decision. If completion
or delivered scope remains blocked, leave the discovery open and explain why.

## Autopilot

Autopilot is a human-granted way to continue implementation across a bounded
sprint; it is not a completion approval. The grant names the Epic or Epics,
end date and excluded areas and is recorded with its `USER:` source through
the CLI. Under a current grant, the agent may continue
within an approved saved plan and may choose an ordinary, reversible default
for a product question nobody has answered, for a person to confirm later. Unless already explicitly authorized, it must stop for decisions whose
wrong answer is hard to undo, including deletion or migration of existing
data, security or authorization, billing or legal terms, and external-party
contracts. It must not expand the approved Epic scope on its own.

An assumed default is recorded as a backlog record of kind `assumption` with
its question: the default the agent chose is its title, and the question it
answers is its observation. It is routed once to the Epic being built and is
never written into the approved plan, which stays read-only. The server
refuses any later change of its disposition without a `USER:` source: a person
confirms it with `ACCEPTED with USER:` and their words, rejects it with
`REJECTED with USER:` and their words, and moving, deferring, reopening or
closing it needs their words in `disposition_ref`. The server can only check
that a `USER:` source is there, so the agent writes that source only from the
person's own words, never from a template such as `USER:<date>:completion`.

An autopilot sprint is one pull request. Each Epic lands on the sprint branch
when its minimal tests pass, the full run runs during the sprint, and the pull
request description has one section for each Epic: its record, criteria,
minimal-test evidence and changed files. An instruction to verify at the end
names the full run. The sprint report lists each full run with its result and
link, and each fix.

Autopilot does not waive red-first testing, security, tenant isolation, data
integrity, project commit checks, independent completion review, accepted-content
read-back, or explicit human completion approval. The human remains in
control of merge, deployment and release. Report the assumed defaults not yet
confirmed, exclusions, failures and incomplete work at sprint closeout.

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

Before the first draft, read the code in each reconnaissance area the change
touches:

- contracts: API specifications, generated schemas, shared error and value
  types, and the rules of any contract check;
- stored data: the schema, grants and migrations, and how an existing database
  is upgraded;
- shared mechanisms the change passes through, such as authorization,
  idempotency, paging and deletion;
- concurrency: the existing locks and transactions and their order;
- composition and startup: host programs, configuration, route inventories and
  startup checks;
- existing behavior: the guards and tests that already exist, so that a
  regression check is not planned as an expected RED;
- process records: the existing traces of the files the change touches, and the
  content of the review bundle.

The project's instructions name the files of each area. When the project names
none for an area, find the files yourself. The saved plan has a section
`Reconnaissance` with one line for each area: what was read, with file and
line, or why the area does not apply.

Before drafting, and after a first reconnaissance, list the questions only the
human can answer and ask them together: conflicts with stored requirements,
documented behavior or the kit's own rules, which entry points and callers are
in scope, names for new user-visible terms, behavior at the largest expected
input, and how earlier answers apply after later changes. `rdd-plan` holds the
full list of question topics. Declare each question as a
decision; its answer is recorded once the saved plan lists it. A question left
for a reviewer to find costs a review round and a plan revision.

A criterion states observable behavior: what the system does, refuses, saves
or shows. Text that an agent follows (kit, prompt or instruction wording) is
checked by phrase tests under the criterion it serves; a wording change that
keeps the behavior stays under the same criterion and needs no return to planning.

Gating evidence comes from deterministic tests: unit, integration, phrase and
scripted user-flow tests with fixed inputs. A run whose outcome depends on a
language model, such as a live reverse-engineering sweep, an agent-driven user
flow or a quality sample read, is a measurement, never a RED or GREEN obligation.
Run it once near the end and report its result in the completion review or PR.
Prompt and model tuning is research outside an Epic's gates; an Epic delivers
the chosen text with deterministic tests.

A plan with no requirements, uncovered in-scope behavior, missing
criteria or missing planned code/test traces is not ready for implementation
approval. Correct these gaps before its first review. If already reviewed,
present the gaps with that review and let the human direct any revision.

Each plan submission gets one independent cold review against its sources,
scope, acceptance, architecture, state boundaries, risks and test strategy.
Then present the exact saved plan, review verdict, material findings, notes,
limitations and gate blockers to the human, even when the review fails. Wait
for the human to approve eligible content, request a revision or further review,
or reject the plan. Do not revise the plan or repeat the review in response to
findings before that decision. Notes are nonblocking; do not polish the plan
or insert review results into its body after review.

The reviewer marks each note as requirement text or build. A requirement-text
note says the plan would publish false requirement content or change promised
behavior; a build note concerns test detail, plan wording or disclosures.
Present the notes in these two groups so the human can choose which to fix
before approval. Do not offer to leave to the build a note that would change
behavior, requirement content the plan would publish, or a file that other work
reads or generates (`rdd-entry-review`). For a failed review, present each
material finding with the
correction the agent proposes, and ask separately any question only the human
can answer. A finding about existing behavior that the Epic leaves unchanged
goes to `rdd-triage` as a Backlog record unless the human widens the Epic.

The first plan review of a planning cycle is a full review: it reads the whole
plan. A later review in the same cycle is a review of the change, unless it is
the full review before approval, the gate names no cause, or, in a round, it is
the first plan review of any Epic in it. A review of the change reads each
earlier finding with its recorded resolution, what the gate's `causes` name,
and each criterion, verification obligation, requirement field, plan section
and test title that uses a changed term, rule or record, or that describes the
same behavior and should now say what changed.

A review of the change also reads the changes that, after the previous review
was recorded, reached the branch of the checkout the reviewer reads, in the
files the plan's traces and its `Reconnaissance` section name; a commit made
before the review that reached the branch after it counts. A plan statement
that such a change makes false is a finding, although the plan text did not
change.

For `plan_content`, `verification`, `decision`, `trace_removal` and
`modernizes_trace` the reviewer reads the `changed` parts and ids and the
`comment` of each plan save the plan history lists since the previous review;
for a changed `body` it reads the sections whose headings the `comment` names
word for word, and the whole body when the `comment` names no heading of the
plan. The previous review is the plan review the blocker's `review_id` names: a
plan save counts when its `at` is at or after that review's `inserted_at`, and
a decision record when its `inserted_at` is at or after it. For
`requirement_edit` the gate names no ids: read
every requirement field and criterion of the plan again, against the stored
content in the bundle's `review.baseline`. For `decision` also read the answers
recorded since the previous review, for `planning_context`, `release` and
`initiative` the planning contexts in the bundle, and for `stored_trace` it
finds each id of `trace_ids` in the traces of the bundle's `review.baseline`.
An id that is not there is looked up by `id` in the supplied trace lists; an id
in neither is a removed trace. When no plan save explains a cause, read in full
the part of the plan the cause names.

A review passes when it has no material finding. The full review before
approval is one more full review of a revised plan. It is due when the latest
plan review is a review of the change, has no material finding and is current
(the `readiness` of a bundle fetched after that review was recorded has no
`plan_review_required` blocker), the planning cycle has only one full review,
and the reason of a review of the change in this cycle names `plan_content` or
`requirement_edit`. Obtain it before you present the plan, without a human
decision between the two reviews, and present both. A review is a full review
unless its reason starts with `Review of the change.` Count the plan reviews in
the `reviews` of the same bundle; a return to planning starts a new planning
cycle. In a round it is due for the round when it is due for any Epic in it,
and a round review is a full review when it is the first plan review of any
Epic in it.

A planning cycle has at most three plan reviews, and the full review before
approval is not due when the cycle already has three or more. After the third,
obtain no further plan review unless the human chooses a revision with its
review or asks for a review in their own words. Present the plan with its
reviews and its open findings. Say when the full review before approval was not
obtained because of the cap. The human decides: approve the plan as it stands,
with the human's resolution recorded for each open material finding; request
one more revision with its review; split the Epic; or reject the plan. When the
latest plan review is out of date, say that approval needs a renewed review,
which the human may ask for. In a round the cap is reached when any Epic in it
has three or more.

A human-requested revision gets one new independent review, then returns to
the human. The one exception to one review for each plan submission and each
revision is the full review before approval, defined above. A fix is a design
change: make the smallest change that removes each
finding, and check every claim, option and suggested correction it relies on in
the code as for the first draft, including how a changed record shape behaves
through approval, evidence recording and completion. Check each change for side
effects: search the plan body, every requirement field, the criteria and the
test titles for the old wording, and inspect what else reads the changed
behavior, including code that reads any path or name the change introduces and
the actual output of any tool it names. Before the new review, a read-only agent
that did not write the fixes checks them the same way; that check records no
review.
Reuse recorded reviews when resuming a session. If the gate reports a review
out of date, disclose the stale review, with the causes the gate names for a
plan review, and obtain a human decision before renewing it. A current
review remains required for implementation approval. Gate checks, finding
resolutions and an identical saved value do not themselves require another
review. After applying the human's disposition, record actual resolutions and
reasons for material findings, including findings from superseded reviews;
editing the plan alone does not record their disposition. Record the
disposition of each note the human leaves to the build, so the build finds it
in the review bundle. Do not mark a
promised correction resolved, manufacture a resolution or
waive server gate conditions. The review audits the planned change, not the
amount of prose or the presence of links.

### Verifying an Epic whose behavior already exists

An Epic may explicitly declare that it verifies already-implemented behavior.
Its saved verification declaration uses `mode: existing_behavior` and a nonblank
`existing_behavior_reason`. Review establishes that behavior and tests exist;
product or executable test changes use ordinary implementation. Each required
TestCase needs current passing criterion proof and scoped source Traces. Reviews,
ownership and separate human completion still apply. Publication retains
requirement and structural Trace identity and is atomic and idempotent.
This Epic mode does not replace the separate unowned-requirement acceptance path.

### Epic gates, verification and review

The four Epic gates and their CLI checks are:

| Epic gate | Required conditions | CLI check |
|---|---|---|
| Plan readiness (`plan-ready`) | Saved requirements, criteria, code/test traces, test definitions and verification obligations; answered decisions; current independent plan review and resolved material findings. | `modernpath epics gates check <epic-id> --phase plan-ready --json` |
| Implementation approval (`implementation`) | Plan readiness and requirements available for ownership. Approval reads the current saved plan, decisions and applicable separate review under server locks. | `modernpath epics gates check <epic-id> --phase implementation --json` |
| Completion review (`completion-review`) | Intact membership and ownership, passing current proof for every criterion, current implementation review confirming scope, and resolved material findings. | `modernpath epics gates check <epic-id> --phase completion-review --json` |
| Completion approval (`completion`) | Completion-review conditions while the Epic is IN_REVIEW. Approval rechecks eligible current database state under locks and requires the user's explicit decision. | `modernpath epics gates check <epic-id> --phase completion --json` |

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
For a round, `modernpath initiative check <initiative-id> --epics <ids> --phase <phase>`
records each Epic's own gate check, and `modernpath initiative approve-implementation`,
`submit-review` and `approve-completion` recheck every Epic's gate in one
transaction.

After recording the independent plan review, `rdd-cold-review` runs the
`plan-ready` check once. `rdd-entry-review` runs `implementation` once and
presents the plan and review with either passing or blocked results. Recheck
a planning gate only after a relevant condition changes; do not poll unchanged
state or treat each gate as a request for another independent review.
`rdd-completion-review` runs
`completion-review` before submission and `completion` before asking for
human completion approval. Resolve planning blockers as directed by the human;
completion blockers follow the implementation and verification passes.
None of these checks records a human approval.

Before the plan review, create or reuse test definitions and declare
verification obligations for every active criterion. Run project checks as development practice.
Each obligation identifies its test, boundary and expected observation;
each criterion states whether its content differs from the accepted baseline.
Ordinary implementation requires RED for changed criteria as a development practice;
keep the assertion, command and observed failure in PR notes. The server does not
store or gate on a separate RED result. An explicitly reviewed
existing-behavior Epic uses that declaration instead of manufacturing failures.
Record applicable boundaries and reasons for exclusions, the plan author's
context, and required human decisions using identities issued by the CLI. Retain each decision question beside its effective human answer.
Give each decision a one-line title that names the choice, and its alternatives
as `options`, with the recommended one marked; the background stays in the
question text.
These are test definitions, not executable test changes before approval.
Obtain and record answers to declared decisions before the independent review.
Inspect read-only phase readiness and correct structural blockers before review;
a missing independent review is expected at this point. A later answer that
differs from the recorded one changes the reviewed content; disclose it for the human to direct renewed review before
implementation approval.

The agent supplies current authoritative requirements, plan, traces, outcomes and
findings to a review context independent of the author. The reviewer covers every
criterion and obligation, separates material findings from notes, and states
limitations. Record reviewer context, actor, coverage, findings and resolutions.
A context string attests to a separately performed review; comparing strings
cannot prove independence.

The server derives review applicability from scoped content, current criterion
proof, structural Traces, ownership and server ordering. Relevant content,
code, structural relationship or material current outcome changes require renewed
review before approval; planning follows the human decision above. Identical
outcome retries and modernization history do not invalidate an Epic review.
Current failures or missing required proof block readiness for the affected
criteria. A later passing outcome restores proof readiness but leaves a review
outdated by the earlier change. Ambiguous applicability blocks approval.
Writers share the existing serialization boundary and assign ordering
times after acquiring it. A no-op save or JSON formatting change is not a semantic
change. Add no fingerprints, ETags, tokens, counters or reviewed-state fields.

For a plan review the rule is narrower. A plan review goes out of date when the
plan's content changes, when something the plan relies on changes, such as which
test proves a criterion, when the plan's list of traces to remove
(`remove_traces`) changes, when the plan's modernizes traces change, or when a
stored trace changes that belongs to the plan's requirements or criteria, to a
requirement it retires, or that links a test of the plan and code. The gate
names what changed in `causes`: `plan_content`, `requirement_edit`,
`verification`, `decision`, `planning_context`, `release`, `initiative`,
`stored_trace`, `trace_removal` or `modernizes_trace`, and lists the changed
stored traces in `trace_ids`. These leave a plan review current: a trace added,
changed or left out of the plan's `traces` that is neither a test's verifies
link nor a modernizes trace, a renamed test, a requirement's grouping, the same
answer recorded again and a selection change.

Present the current saved plan and effective human decisions before implementation
approval, and the approved proposal, published baseline, outcomes, findings and
risks before completion approval. Apply the explicit human action with its reason
and attribution. Actions approve eligible current database state; they do not
certify that a previously rendered screen stayed unchanged. They upload no plan
or presentation. The server reads and checks current state under its existing
locks, then commits publication, ownership and audit atomically. Normal responses
include each record once by its ID and review summaries without nested
historical presentations. Explicit history reads expose surviving facts and
accepted content. No archive or exact historical bundle reconstruction is kept.

For a human-requested revision of an active plan, use
[Returning to planning](#returning-to-planning) before saving changes. Incomplete
verification declarations are a reason to repair the plan, not to waive evidence.

## Development loop

Small changes use the [AI TDD inner loop](#ai-tdd-inner-loop) directly.
The Epic delivery loop is:

```text
CHOOSE EPIC -> SAVE PLAN AND PROPOSED CONTENT -> ANSWER DECLARED DECISIONS
-> RETAIN REVIEW BUNDLE
-> INDEPENDENT PLAN REVIEW, AND THE FULL REVIEW BEFORE APPROVAL WHEN DUE
-> HUMAN DECISION ON PLAN AND FINDINGS -> RECORD FINDING RESOLUTIONS
-> HUMAN IMPLEMENTATION APPROVAL / IN_PROGRESS + REQUIREMENT OWNERSHIP
-> RUN EXPECTED FAILING ASSERTIONS -> IMPLEMENT -> RUN FOCUSED AND QUICK CHECKS
-> RECORD CURRENT CRITERION RESULTS -> CLEANUP -> VERIFY WITH THE EPIC'S MINIMAL TESTS
-> LAND ON THE SPRINT BRANCH -> FULL RUN ON THE SPRINT PULL REQUEST
-> FIXES ON THE SAME PULL REQUEST -> PASSING FULL RUN
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
| Cold review | `rdd-cold-review` | One review recorded, and the full review before approval when it is due; exact plan, findings and gate blockers handed to the human |
| Implementation approval | `rdd-entry-review` | Human decision on the plan and review; eligible approval read back, requested revision routed, or rejection reported |
| Implement | `rdd-build` | Red-first implementation and focused checks complete |
| Verify | `rdd-verify` | Required lower and upper criterion proof passes |
| Review, publish and complete | `rdd-completion-review` | Human-approved content atomically published/versioned/audited after a passing full run, ownership released, or incomplete work reported |
| Verify existing behavior | `rdd-reverse-engineer-verify` | Current PROPOSED content traced to existing behavior and complete applicable test outcomes recorded; no lifecycle transition |
| Accept existing behavior | `rdd-reverse-engineer-accept` | Exact verified targets explicitly accepted by a human and atomically published as DONE, or left PROPOSED with the tooling gap reported |
| Discoveries and blockers | `rdd-triage` | Discovery routed into a draft plan or reported as a separate-work decision/blocker; active approvals preserved |
| Record an Engineering Constraint | `rdd-constraint` | Proposed constraint created on the person's word and read back by ref; acceptance left to the person |
| Review a pull request | `rdd-pr-review` | Review returned in chat: findings by severity, intent verdict per Epic and for the branch, constraint assessment, checks and limits; nothing recorded, nothing posted unless asked |

A focused pass is a handoff, not whole-task completion. Stop when its exit is
reached and report what remains.

### Minimal tests and the full run

A sprint is one Epic or several built together on one pull request, with or
without an autopilot grant. The sprint branch is that pull request's branch.
The release branch is the branch the sprint branch starts from and merges into;
it is usually the main branch and is not a Release record. The quick checks are
the project's fast checks, such as build, static and architecture checks, and
unit and domain tests; the project instructions name them. An Epic lands when
`rdd-verify`'s run of its minimal tests passes; its commits are then pushed to
the sprint branch (§Returning to planning). The sprint report is the agent's
report to the human at the end of the sprint, before it asks for completion
approval.

An Epic's minimal tests are the TestCases of its changed criteria, at unit
level first and at integration level where the criterion's boundary needs it;
mutation testing of the code the Epic changed, where the project instructions
name a mutation tool, with its results kept in the PR notes; the project's
quick checks; and any check the plan names that the full run does not run.
`rdd-verify` runs these and no more.

The full run is the project's full test suite, with mutation testing in full
where the project uses it. It runs on the sprint pull request, as its CI or as
the project's full-run command, during the sprint as well as at its end. Coding
never waits for a full run. The full run's result and a link to it go in the
pull request description and the sprint report. The platform records criterion
outcomes only: a full-run result with no TestCase traced to a criterion is not
recorded and blocks no gate.

A full-run failure goes to `rdd-triage`. A failure the project instructions
list as a known flake is rerun alone three times. It is a flake only when all
three pass, and it is listed in the sprint report. A confirmed flake does not
make the full run pass: rerun the failed part of the full run until it passes,
or the whole run where it cannot be rerun in parts. The full run has passed
when every part of it has passed on the same commit. Every other failure is a
defect. A defect that a sprint Epic caused is fixed red-first under that Epic,
on the same pull request, while that Epic is not DONE. A defect that was
already on the release branch also fails on the tip of the release branch and
belongs to no approved Epic. The agent fixes a defect that was already on the
release branch, and a defect that a DONE Epic caused, red-first as a small
change (§Small changes) on the same pull request, without waiting for the
human, and lists it in the sprint report. This rule authorizes that small
change in place of a user's request and of the fresh approval §Work scope asks
for. With or without a grant, the stops of §Autopilot apply to it; under a
grant, so do the grant's excluded areas. The human still decides the small
change's completion (§Small changes). A fix that changes what a published
requirement or its criteria state, or that is not narrow, is its own Epic, with
its own plan and approval; for a DONE Epic that is its follow-up Epic.

Request the independent implementation review after the full run passes,
because a criterion outcome that changes after the review outdates it. The
human approves an Epic's completion after a full run that contains the Epic has
passed. This is a rule for the agent and the approver; no gate checks it. The
human merges the pull request after its full run has passed; how it is merged
is the project's choice. An Epic that changes no code or tests has no full run
to wait for.

The project instructions name the quick checks, the full-run command and where
it runs, how many long runs one machine may run at a time, the sprint branch
and the release branch, the mutation tool if the project uses one, and the
known flakes.

### AI TDD inner loop

Establish each changed clause's expected failure before implementing it. Begin
Epic work after implementation approval. Keep requirement, criterion, source and
TestCase Traces in the platform. For each clause:

1. Write or select a focused assertion and run it before implementation.
   Confirm the expected assertion failure. A build error or unrelated failure
   is not RED evidence.
2. Implement the change and run the focused checks. Epic work also runs the
   project's quick checks; a small change runs its proportional regression
   checks.
3. Record each actual outcome against its criterion and TestCase. A later
   outcome replaces the earlier one for that pair. Read back the current results
   and Traces; a pass for one required test does not cover another.
4. Clean up, rerun affected checks and record current outcomes. Complete evidence
   collection before independent implementation review. A material change to a
   current criterion outcome in review scope outdates that review. An identical
   retry changes nothing. Modernization history does not outdate the review.
   Changes to reviewed content, code or structural relationships also require renewed review.

Record only observed outcomes: pass, fail, error, skip or inconclusive. Keep
commands and output in PR notes. Do not manufacture results, skip failing
assertions or weaken tests to pass a gate. Builds and regression suites are
development checks; they are not separate stored proof subjects.

If an upper/user-flow failure remains after lower-level requirements pass,
diagnose it. Repeat the loop when it is within approved behavior. If fixing it
requires changed scope, acceptance, architecture, priority or a material
technical decision, keep the active approved plan and owned requirements
read-only until a human-authorized return to planning is applied. Revise the
PROPOSED draft and obtain fresh review and implementation approval before
resuming changed work. A separate Epic remains available for separate work. Record an external impediment
as blocked. UI evidence requires the live stack, loaded assets and inspected
browser output; never mutate production-like data to verify rendering.

### Delegated passes

A cold review or verification pass may run in an independent context. A build
pass may also run in an independent context. It never writes to the store: it
ends by writing the build handoff, an input file for one command and not a
record, and returns it to the agent handling the task, who records the build
with `modernpath epics build record`. A cold review or verification pass
returns findings, verdict and sources to the agent handling the task, who
records them through the CLI. A PR review (`rdd-pr-review`) runs the same way:
the reviewer returns findings, verdict and limits to the agent handling the
task, who hands them to the person in chat and records nothing. Recording a
reviewer's verdict does not make that agent an independent reviewer. A delegated pass that encounters a permission, authentication or
process-tool refusal stops and returns it verbatim; it does not retry by
rewriting or splitting the refused action.
A delegated pass that returns an empty or missing result has failed. Record
nothing from it, say that it failed, and run it again once the cause is
removed. Before a build runs again, check what it left in the working tree. A
failed run is not a review, and a refusal is not a failed run.

## Evidence and completion

A result records the current actual outcome for one criterion and TestCase,
linked through scoped Traces. Outcomes are `PASS`, `FAIL`, `ERROR`, `SKIP` or
`INCONCLUSIVE`. Only an executed assertion failure is FAIL; setup and build
failures are ERROR. The latest committed outcome replaces the earlier outcome
for that pair. A broad suite proves only assertions it exercised. Revision
numbers do not determine proof applicability. Changing a criterion's meaning
removes that criterion's current results and proof Traces; a TestCase title or
unrelated requirement metadata edit does not remove current outcomes.

Current direct proof is a `CriterionTestResult`: one current outcome per
criterion/TestCase pair, linked by `verifies` and `relates` Traces. A later
direct outcome replaces that pair's result. Modernization history is separate:
an authorized open Modernization Release captures a `ModernizationTestRun`, its
commit and immutable `ModernizationTestResult` outcomes atomically after
execution. Each historical result has a TestCase Trace and the criterion
Traces its entry names; a capture test case's result names none. Runs have no
pre-registration or finalization lifecycle. Their reads and reference
selection do not satisfy, replace or invalidate current criterion proof or
ordinary readiness. Capture with `modernpath modernization record`; use
`modernpath evidence record`, or `modernpath epics build record` after a
delegated build, for current direct proof.

A difference is a place where the new system behaves differently from the
legacy system it replaces. Record a difference with `modernpath backlog create`
as a gap of kind difference that names the kept or changed legacy requirement
it concerns, what changed and the evidence. A person on the team accepts a
difference by name on the Initiative's Proof tab, with their own words, and
the agent never does. A difference that changes stored values or fails a write
is fixed as a defect, never accepted. Close a difference only by the done Epic
that fixed it, with `CLOSED by` and the Epic's number, or by the merged pull
request that fixed it, with `CLOSED by PR #<number>` or the pull request's
address. A mask added to a replay after its first failing run cites an
accepted difference by its reference. `modernpath roadmap proof` lists the
Initiative's differences with their references, its acceptance and what it
waits for.

Choose evidence by boundary: unit/property for domain rules, component and
browser for UI, endpoint or contract tests for APIs, integration for
persistence and external integrations, schema conformance for cross-service
contracts, and harness/smoke for operations. Run the Epic's minimal tests after cleanup; the full run covers the regression suites, including regression checks a plan names. A check a plan names that the full run does not run is one of the Epic's minimal tests. Keep test identities and
requirement links stable; file line numbers are navigation aids,
not identities.

Before completion review, record the actual outcomes of the Epic's criteria and, unless the Epic changes no code or tests, wait for a passing full run of the sprint pull request. Review from a context independent of the author. Resolve material
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
answer, with the changes since approval to the planning contexts the Epic
reads and the assumed defaults not yet confirmed; neither blocks completion.
After explicit approval, read back the atomically published requirement
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
| New behavior or changed user outcome | Revise a PROPOSED plan before its first review; after review, present the change for the human to direct any revision. Obtain human implementation approval. For an active Epic, obtain an explicit human return to planning first, then fresh review and approval. DONE uses a follow-up Epic |
| Missing human decision or ambiguous intent | Ask the human and record the answer with its source before relying on it |
| Technical failure inside approved behavior | Continue red-first implementation and verification |
| External impediment | Report the blocker, its owner and the next action |
| A full-run failure | §Minimal tests and the full run |
| Known future work outside the approved scope | Disclose it and leave it for a separately authorized Epic |
| Contradicted or removed behavior | Explain the conflict and ask the human how the approved outcome changes |

Backlog, gap and tooling records live on the server like every other delivery
record: they are created, routed and closed there, through the CLI. When a
saved plan delivers a record, mark the record as routed to that Epic. After
human completion approval, mark it as closed by that Epic. If the human wants
a record left open, leave it open and add a note saying why. An assumed
default is never closed by the Epic: it stays routed to the Epic until a
person accepts or rejects it in their own words. Whether a record
is open, routed or closed (its disposition) is read from the server, not from
the plan text; a plan only lists the records it delivers. A discovery that
exists only in a note is not yet a record.

Preserve competing authoritative sources and ask for a human decision; never
resolve intent by timestamp, branch state, or a convenient test result.
