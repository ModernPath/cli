---
name: rdd-reverse-engineer
description: Derive and publish source-scoped requirements from existing code and documentation, as an authorized baseline or DERIVED proposals. Use for initial adoption or additional reverse-engineering; verification and acceptance belong to their dedicated skills.
---

# Reverse-engineer a usable requirement base

## Purpose

Establish grounded URs/SRs and their source relationships within the agreed
scope. Baseline publication creates PENDING_VERIFICATION requirements in Base;
DERIVED publication creates candidates for later review. Neither creates test
PASS, compliance approval, delivery entry or DONE. Change no product/test code
and execute no untrusted repository code during this pass.

## Inputs and prerequisites

Read the project `AGENTS.md` and canonical `PROCESS.md`
(`.modernpath/rdd/PROCESS.md` in a consuming workspace). In ModernPath, read
the installed `mp-process-cli` skill for the onboarding sequence and
`.modernpath/cli-reference.md` for the installed command contracts.

Establish the target system, requested behavior/context scope, source roots and
whether state is store-backed or file-backed. Prefer fresh synchronized local
documents: check freshness, search/read local files, and refresh only missing or
stale exports. Use live reads for missing material or a current authoritative
answer; the authoritative source wins a disagreement. Always read the authoritative
requirement corpus and fingerprint through the sanctioned tool before authorizing
a run. An empty directory or cache does not prove an empty system.

Before the first inventory of a Git repository, prepare it as the
`mp-process-cli` onboarding says. Commit the files `modernpath install`
created or changed, by the person or by the agent after the person agreed: an
inventory of a dirty repository marks the whole capture dirty, and a run
captured dirty cannot be accepted as built, whatever is committed later,
without a new authorized run with its own capture. As-built acceptance also
needs the tested commit to be the tip of the remote default branch — which may
lie past the captured commit, when the commits between changed none of the
files the run authorized (the `mp-process-cli` verification steps). Put
untracked files that are not source
into the repository's local exclude file: ignored files are left out and
disclosed, and tracked files are listed whatever the ignore rules say, except
symbolic links, private paths and files under `.claude`, which are left out and
disclosed. Keep every saved file in the sweep's working folder that the
`mp-process-cli` onboarding names, never in the repository root or a temp
folder. The repository stays clean and at one commit from the first inventory
until acceptance, and the kit is not updated during a sweep. Before the first
inventory of a run, check that the installed kit matches the CLI, using the
install check the `mp-process-cli` onboarding names. Install and commit any
drift then, before the capture, so it never surfaces mid-run. If the person
decides to update it anyway, it is installed between runs and committed, never
left uncommitted; a run captured before that commit whose authorized files the
update changed can then be accepted as built only through a new authorized run
with its own capture and with the citations of its requirements moved to it.

## Resume checks

If a run already exists, read its authorization, captured sources and group
receipts before collecting or publishing again. Reuse unchanged authorization,
keys and prepared inputs; reconcile acknowledged receipts with the unpublished
remainder. Retry identical input only when recovery requires it. A conflict is
not permission to invent a new key or overwrite an existing requirement. Read
the run mode from the run's authorization source and the sweep's run-mode
change file as the `mp-process-cli` onboarding says, and do not ask for it
again: the run is autonomous only when its source says so and the change file
holds no confirm entry for it; otherwise, and also when the change file is
unreadable, confirm after each publication group.

Resume against the run's captured source identities. Reconcile corpus changes
against its own receipts; external changes to the authorized source or
materially changed source scope require fresh scope authorization. Never
replace unavailable historical evidence with latest code or ask again for an
unchanged authorized run.

Of the corpus, a publication group is refused only for a collision with a
record it names, never because the rest of the corpus changed. Runs publish
one after another: authorize the next run only after the previous one has
published every group, its source assessments included, and meets the finish
condition, or after the person decided to leave it unfinished. Fold a
correction into a group before publishing it. A later run cites the source
file ids of its own capture; a file covered by a requirement from an earlier
run is assessed as reviewed, naming that requirement. Contexts on published
requirements are set before verification records execution proof; citation
changes and trace refreshes belong to verification. The `mp-process-cli`
onboarding gives the checks and the refusals.

## Steps

### 1. Inventory and authorize the source scope

Declare each repository with a stable key and local root. Git worktrees and
non-Git roots are valid. Inventory tracked/unignored files or explicitly scoped
non-Git files, including configuration and legacy XML/JSP/XSL/XSLT templates.
Record paths, sizes, digests, revision, dirty state and document identities,
versions and digests. Report generated/vendor/secret exclusions and unsupported
classes; exclude credentials and private workspace metadata.

Show the existing corpus counts, source scope, authorization brief and relevant
inventory files. Obtain an explicit attributable mode choice unless the session
already contains that exact authorization:

- **Baseline ready for use:** grounded as-built requirements and confirmed links
  enter the authoritative corpus in Base as PENDING_VERIFICATION.
  Recommend this for an empty corpus. It authorizes source-scoped publication,
  not acceptance of statements that have not yet been generated.
- **DERIVED additions for approval:** distinct proposed requirements and links
  await exact later review, separate from the confirmed corpus. Existing
  governed content remains unchanged. Recommend this when requirements already exist.

Preflight recommends derived whenever the system has a requirement, which in a
planned area-by-area baseline is every run after the first. Show the
recommendation with that explanation; the person still chooses baseline or
derived. Recommend baseline instead when the existing corpus is this sweep's
own earlier baseline, or when the person has said the requirements are to be
an as-built base to work from, such as a modernization baseline.

Ask once. The person's own words answer the question when they settle it: a
stated purpose of an as-built base to work from selects baseline, and a reply
that asks to proceed without naming a choice — "run it", "go", "do it now" —
accepts the recommendation that the one question showed. Record their words
as the authorization source and say in one line which choices they settled.
Never put the same question a second time. A request to redo or deepen an
earlier run over an unchanged inventory carries over that run's mode,
documents and run mode unless the person names others. Ask again only when a
refusal's rule requires it.

In the same question as the mode, the person chooses the run mode, written into
the run's authorization source: autonomous, where the agent continues through
capture, every group, coverage and the read-back and stops only for the stop
list of the `mp-process-cli` onboarding; or confirm, where it reports after
each publication group and waits. Call this choice the run mode, never just
the mode: the mode is baseline or derived.

Record the chosen mode/source authorization through the sanctioned operation.
Do not substitute generic requirement birth with a forced status, raw API writes
or retired task-ledger synchronization. Report missing tooling through the
project's sanctioned channel. Do not change another system's records.

### 2. Capture sources and recover behavior

Capture authorized bytes before publishing references. Capture needs neither
provider OAuth nor FileAnalysis and must not replace a connected repository's
latest selection. Keep Git revision separate from dirty snapshot digest. File
citations identify repository, revision, exact path, digest, optional locator
and immutable source ID; document citations identify the authorized revision.

Work one coherent bounded context at a time, inspecting these perspectives:

| Perspective | Inspect | Recover |
|---|---|---|
| Domain | Schemas, migrations, constraints, analysis | Ownership, aggregates and invariants |
| Surfaces | Actors, views, role gates, entry points | User journeys and actor/outcome URs |
| Behavior | Implementation, refusal paths, tests | SRs, criteria and existing test identities |
| Design | Relevant context map and documents | Source-backed design findings within scope |

Enumerate applicable routes/RPCs, jobs/events/webhooks, CLI/agent-tool surfaces,
data models, access controls, client flows, integrations and tests. Record absent
classes as inapplicable. Use aggregate ownership to draw contexts. Guides and
analysis orient the work; code establishes implemented behavior. Unconfirmed
intent, contradictory behavior and dead paths remain questions or findings.

State behavior a change could breach, including refusal cases and implementation
behind entry points. Do not write one requirement per function or invent intent
from a constant. URs need actor, outcome, inline scenarios and sources; SRs need
a boundary, behavior, meaningful criteria and sources. Criteria claim no test ran.

A UR is one actor reaching one outcome they would start on its own and notice
if it broke: add a payment card, remove a card, see why a payment failed,
start a charging session. A bounded context normally holds many. A UR whose
scenarios cover outcomes a user would start separately is several URs; split
it before publishing. One UR per context or subsystem is under-derivation, the
recorded failure of this pass, not a summary level to stop at. An SR states one
boundary's behavior serving a UR; a UR is served by every SR its outcome needs.

Derive from the surface inventory, not from a target count. Per repository,
enumerate the units an actor reaches: screens, views, routes and dialogs; the
actions that change state; user- and operator-facing API operations;
notifications, receipts and messages the actor receives; scheduled outcomes the
actor observes; and test cases that describe actor behavior. On a service, the
entry points are every controller, function, queue or message processor, timer
and background job, and every request filter that admits or refuses callers.
Report them per repository as covered out of total. Account for every
unit as covered by a named UR (the same outcome on two clients is one UR citing
both), technical with a reason, or unresolved. The UR count follows from this
inventory. A number the person names is a signal to check the inventory, never
a quota to pad and never a cap. A large codebase is worked context by context
to the end of the inventory, never trimmed to a summary.

Write every requirement from reading the code it cites. A script may enumerate
the inventory, group units and match citations; it never writes a title,
statement, actor, outcome or scenario. Text made by filling a sentence template
with words split from identifiers or test names is not a requirement, however
many rows it makes. In particular:

- **A UI control is a step, not an outcome.** Continue, close a dialog, open a
  menu or a URL, poll a status: these are steps inside the outcome they serve,
  which is where they belong as scenario steps, as are gesture mechanics and
  animation timing unless they serve a goal of their own. A private method, an event
  handler, a view or composable name and a test name are never a title.
- **One outcome across its layers.** A flow that runs through the client, the
  gateway and the backend is one UR. The layers are its SRs. A guard,
  validation or security rule is a scenario or criterion of the outcome it
  guards, never a UR of its own. The same mechanism behind several products is
  one SR serving each product's UR.
- **State the boundary of the scope.** When the authorized scope holds only
  some of a flow's layers, such as one service, the run describes what that
  scope does and states its boundary: who calls in through layers outside the
  scope, and which rules it relies on them for, such as authentication, payment
  or ownership. The end report lists the outside layers as not covered. A
  requirement never claims behavior the scope does not hold, and a rule found
  only outside the scope is an open question for the run that covers it.
- **Side effects are criteria.** List every outbound effect of a handler:
  messages and notifications sent, reports and events published, sessions
  finalized or invalidated, calls to providers, settlement. Each one appears in
  an acceptance criterion of the requirement it serves, with its trigger. A
  main path with its refusals but without its effects is incomplete.
- **As built, not as intended.** State what the code does, including where it
  differs from evident intent: a delete that keeps the record, a next link that
  returns the first page, a not-found answered with success. Mark each such
  difference as as-built behavior in the requirement and add it to the
  open-questions list. A rebuild must know it, and never discover it.
- **Numbers and platforms.** State the limits, intervals, timeouts, buffers and
  thresholds the code applies, with their units and where they are configured.
  Where Android and iOS, or two clients, behave differently, give each its own
  scenario, or its own requirement when the outcome differs.
- **Title and statement in the domain's words.** The title names what the actor
  achieves, as the product or its users would say it, never a split identifier
  ("update continue", "b2 b cards", "view transaction guid"). The statement says
  what the actor achieves and the rules that govern it. No two requirements
  share a statement.
- **Actor and outcome are set on every UR.** The actor is the real role, such
  as customer, cashier, operator or a named integrating system, never a catch-all
  such as "service or provider". Trace who calls the entry point: a customer
  call through a gateway is the customer's, a service-to-service call is that
  service's, and a call with no outside caller is an SR, not a UR. The verb is
  what the actor does: showing is not adding, filtering is not paying.
- **Live code only.** Code marked obsolete, sitting in a retired module, or
  reached by no entry point is not baselined as live behavior. Record it as an
  open question. When two source trees hold the same client, find out which one
  ships, or ask once and record the answer, before citing either.
- **Scenarios carry the code's rules.** The main path and each refusal or
  variant the code implements, with the preconditions, data, limits and
  observable results the code shows: which card types are accepted, what
  happens when the provider declines, what the actor sees on a timeout. A
  scenario that would read the same for any other outcome is a template;
  replace it.
- **Every UR is served by system requirements.** Each UR has as children the
  SRs that implement its outcome, published as their parents, and each SR
  states one boundary's behavior with criteria a test could check. A UR with no
  serving SR is unfinished. Thirty SRs cannot serve a thousand distinct
  outcomes.
- **Cite where the behavior is.** A citation is a file where the stated
  behavior is implemented or tested. Migrations, dependency wiring, telemetry,
  Swagger set-up and every file of a folder are not citations of a behavior the
  requirement does not state. A cited test asserts the stated rule, not a
  neighbor in the same folder, another product or another platform. A smoke
  test that only reaches a screen verifies no rule. A rule with no asserting
  test is reported as untested. When one repository holds a copy of another's
  code, cite the original and disclose the copy.

Depth takes time on a large codebase. Neither the person's urgency nor the
autonomous run mode allows templated text: work context by context, and report
the contexts not reached as unfinished.

For NFRs, check existing ownership and read complete configuration expressions
and units; bare thresholds without confirmed intent remain questions or DERIVED
exceptions, never invented targets or measurements.

Join UR journeys to serving SRs and SRs to code/test identities. Report missing
joins, views calling nothing, endpoints no view reaches and justified absent
relations. Do not infer edges from prose mentions or invent parents. Create an
Epic only when the user requested a real grouping, with exact UR/SR memberships.
Verify the persisted member identities and both UR and SR counts against that scope.

For a full-system adoption or requested design-document output, use
[recovered documents](references/recovered-documents.md). A narrow requirement
run only maintains documents relevant to that scope; it does not require a new
system-wide document set.

### 3. Prepare the complete batch, then publish

Prepare the full requested scope locally before the first requirement
publication: URs, SRs, scenarios, criteria, citations and relationships, with
stable run/group keys and input fingerprints. Check duplicates, exact existing
identity reuse, unresolved citations and cross-context joins across the batch.
Compare the staged requirements with the existing corpus by the outcome and
behavior they state, using the files they cite to find candidates, and reuse
or merge into an existing requirement instead of publishing the same behavior
under a new id. A shared file is not shared behavior: an existing broad
requirement covers only the outcomes it states, so a finer outcome it does not
state is a new requirement, published next to it. Where two staged outcomes
look alike, record a one-line reason for keeping them apart or merging them.
Identify the tests that cover each group's code and cite them, naming the test
case. When the code has tests, a requirement cites the ones that exercise its
behavior. Before publishing, count the requirements without a cited test per
context and show them as a gap. A cited test is not an executed one, so never
report it as verification. List the requirements published without a test in
the end report. These reusable inputs are staging
artifacts, not another requirement store.

Before a run's first publication, show the person the bounded contexts the run
derived that the sweep has not confirmed yet, a code and a name each, shown in
the question itself, each with the files and the requirements it covers, and
written as the same list to
`.modernpath/reverse-engineering.runs/<folder>/contexts-proposed.md` so the
person can read it before answering, drawn from the recovered behavior with
analysis subsystems as an input only; the person confirms or changes them, in
either run mode. This is a naming
confirmation of the list, asked once per run, not an approval of content; a
run whose contexts are all confirmed asks nothing. Record a confirmed context
on every requirement created, never an unconfirmed one; a reuse entry carries
none.

Before publishing each group, check its content with
[the content quality checks](references/coverage.md#content-quality), measured
and read. A group that fails is rewritten before it is published. A failure is
not a note for the end report.

Publish coherent atomic groups of related requirements. Split only for supported
limits or dependencies, sending referenced parents before dependent groups.
Read and retain each returned group receipt; perform a consolidated record
read-back and audit after the batch. Do not alternate deriving and publishing
one row at a time.
Atomicity is per group; report any unpublished groups explicitly.

Apply the authorized mode:

- **Baseline:** new grounded rows become PENDING_VERIFICATION in Base with
  confirmed UR–SR and SR–code/test relationships. Reuse only exact unchanged
  identities. No row/context approval round follows publication; the naming
  confirmation of new bounded contexts comes before it. Every baselined
  requirement cites the production code that implements its behavior. An
  outcome found only in a test is traced through the test's calls to that
  code; when it cannot be, the row stays DERIVED with that reason.
- **DERIVED:** publish distinct candidate IDs, proposed criteria/links, sources,
  conflicts, consequences and a confirmation brief. Compare exact typed existing
  identities and current/proposed content without overwriting approved content.
- **Insufficient provenance:** keep the row explicitly DERIVED with its reason
  and candidate packet. Do not invent edges to meet a count. Candidate test stubs
  and links count as neither ordinary tests nor execution/release coverage.

In store-backed workspaces, use typed `parent_external_ids` and code/test
citations; retain returned trace IDs and authority. Put each relation on the
field its record kind takes, as the `mp-process-cli` onboarding names it: an SR
names its UR parents. A field the kind does not take may be dropped without a
refusal, so check the fields against the kind before publishing. Afterwards,
compare every intended UR–SR link with the receipt and with the read-back of
the SR. A missing link fails the group, which is corrected and published
again. Never recreate retired
`tasks/*-REQUIREMENTS.md`, `WORKLIST.md` or mirrored file-state ledgers. For an
existing file-backed workspace only, use [file-backed formats](references/file-backed.md).

### 4. Audit the persisted result

Use [coverage and citation checks](references/coverage.md) for the distinct
measurements and failure conditions. Citation resolution, extraction coverage,
governed linkage and verified behavior are separate claims. Neither a passing
citation checker nor source-file coverage proves behavioral coverage.

Read back exact IDs, lifecycle/release, citations, relationship authority, test
artifacts and any requested Epic memberships from the authoritative store. In
a file-backed project, verify the versioned records and their receipt/revision;
in a store-backed project, use the sanctioned read-back operation and projections.
For ModernPath, check Ledger and System → Requirements against the receipts.
For DERIVED, verify candidate records contain the proposals while confirmed
records and governed counts remain unchanged. Report
created, reused, baselined, derived, rejected and unresolved counts separately.
Report the content quality checks next to the counts, measured on the persisted
records. A count, file linkage or reached target never makes a run complete on
its own, and reporting a run as complete while those checks fail is a false
report.

### 5. Confirm candidates only when requested

This step applies to DERIVED rows whose exact confirmation is part of the task,
not to ordinary baseline publication. Preview typed UR/SR identities and separately
selected proposed trace IDs at one current graph fingerprint. Present the brief,
current/proposed content, evidence, consequences and relevant preview files.
One human action can decide all named rows and links; no Epic or delivery release
is needed. Omitted links remain candidate and can be decided later.

Record the explicit attributable decision and idempotent receipt:

- accept as-built → Base/PENDING_VERIFICATION;
- accept desired intent → PROPOSED for normal delivery planning;
- reject → OBSOLETE, without publishing relationships;
- defer → unchanged DERIVED.

Changed content or relationships require a new preview and reviewed decision.
Confirmation is additive; it is not replacement, compliance approval, test PASS,
delivery entry or DONE.

## Outputs and handoff

Retain the authorized scope and source/run identities, prepared batch and group
keys, publication receipts, exact persisted IDs/links, coverage scripts/reports,
and all gaps or unpublished groups. Include design documents only when in scope.
Unresolved in-scope work and unchecked coverage remain incomplete. A run is
finished only when the finish condition of the coverage reference holds. Continue a requested full
sweep across remaining authorized contexts without per-context reapproval,
in the run mode the person chose; the end report is made from the open
questions kept in the sweep's working folder.

Finish with a usable baseline or a reviewable candidate set and an honest
remainder. Existing-proof verification of published baselines belongs to
`rdd-reverse-engineer-verify`; its eligible packet then goes to
`rdd-reverse-engineer-accept`. Missing tests or changed behavior require an
explicitly scoped normal-development handoff.
