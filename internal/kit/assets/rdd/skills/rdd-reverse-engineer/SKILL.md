---
name: rdd-reverse-engineer
description: Onboard an existing system from code and documentation. Ask whether to establish a usable as-built baseline or create DERIVED additions for review; inventory declared repositories, derive grounded URs/SRs with citations and relationships, publish through the sanctioned store workflow, and verify read-backs. Use for initial adoption or additional reverse-engineering, not implementation or verification.
---

# Reverse-engineer a usable requirement base

Read the project `AGENTS.md` and canonical `PROCESS.md`. This skill applies its
source-scoped baseline and DERIVED rules; it never changes product code or
turns code inspection into passing verification evidence.

## Connect, inspect, then ask once

1. Establish the target system and workspace binding through the sanctioned
   tool. Synchronize documentation locally. Read the authoritative requirement
   corpus and current fingerprint; directories and cached exports are not proof
   the system is empty. Do not change a different system's records.
2. Declare every repository with a stable key and local root. The parent may be
   non-Git; a Git worktree may have a `.git` file. Inventory tracked/unignored
   files or explicitly scoped non-Git files. Include configuration and legacy
   templates such as XML, JSP, XSL and XSLT, not just modern source extensions.
   Report generated/vendor/secret exclusions and unsupported classes explicitly.
3. Pin repository revision, dirty state, exact paths/sizes/digests and synchronized
   document identities/versions/digests. Exclude credentials and private workspace
   metadata. Show the source scope and existing corpus counts.
4. Ask which result the user wants, explaining both consequences:
   - **Baseline ready for use** (recommend for an empty corpus): publish grounded
     as-built URs/SRs and their graph to Base, visible in Ledger and System →
     Requirements as **Baselined — not verified**. The choice authorizes this
     source-scoped run, not compliance approval, delivery entry or DONE.
   - **DERIVED additions for approval** (recommend when requirements exist):
     leave the existing corpus untouched and put proposed requirements/links in
     System → Requirements, awaiting exact later approval and absent from Ledger.
     This does not require a delivery Epic.
5. Record the explicit attributable mode/source authorization through the
   supported operation. Do not silently choose, rebaseline existing rows or claim
   the user reviewed not-yet-generated statements. Resume an already authorized
   unchanged run without asking again; materially changed sources or corpus
   require fresh scope, not an expanded retry.

In ModernPath workspaces, read `mp-process-cli` for the concrete command sequence
and `.modernpath/cli-reference.md` for flags and payload contracts. Use the
source-scoped onboarding operations, not generic requirement birth with a forced
status. If necessary server/tool support is absent, report that exact gap.
Do not substitute raw API writes or a task-ledger sync.

## Recover behavior with explicit denominators

Work a coherent bounded context at a time, repeating these passes:

| Pass | Inspect | Produce |
|---|---|---|
| Domain | schemas, migrations, constraints, existing analysis | aggregates, invariants and ownership |
| Surfaces | views, actors, role gates, entry points | user journeys and actor/outcome URs |
| Behavior | implementation paths, normal and refusal cases, tests | independently meaningful SRs, UR scenarios and evidence |
| Recovered design | the complete context map and checked documents | system-level architecture/integration/data-flow notes |

Enumerate routes/RPCs, jobs/events/webhooks, CLI and agent-tool surfaces, data
models, access controls, client flows, integrations and tests. A genuinely absent
class is stated as inapplicable. Draw contexts by aggregate ownership, not route
layout. Read existing knowledge and human guides as orientation, then confirm
claims against code. Unconfirmed intent and contradictory behavior remain open
questions; a dead path is a finding, not shipped behavior.

A requirement states behavior a change could breach, not how functions happen
to be arranged. Derive both what an entry point does and what it refuses. Include
the implementation behind entry points. Do not invent intent from a constant or
write one row per function. URs need actor, outcome, inline acceptance scenarios
and sources; SRs need a boundary, behavior, source and meaningful criteria.
Criteria describe expected observations; they do not claim a test ran.

Join both sides: UR journeys to serving SRs, and SRs to implementing code and
existing test identities. Produce a join report naming views calling nothing
and endpoints no view reaches, alongside missing joins and optional relation rationales.
Do not infer relations merely from prose mentioning an ID. Do not create Epics
as discovery folders. Where the user explicitly wants a real grouping, persist
its intended UR/SR members and verify both membership counts.

## Capture, publish, read back

Capture authorized bytes before publishing references. New systems need not
have provider OAuth or FileAnalysis records. A source snapshot is not an AI
analysis request and must not replace a connected repository's latest selection.
Keep Git revision separate from dirty snapshot digest. A file citation names
repository key, revision, relative path, digest, optional locator and immutable
source identity. A document citation names the authorized document revision.
Ambiguous basenames and unresolved evidence are not valid authority.

Publish each coherent group atomically and read its receipt back:

- **Baseline:** create new grounded requirements as PENDING_VERIFICATION in Base
  with confirmed canonical UR–SR and SR–code/test relationships. Reuse only exact
  unchanged existing identities. No row/context approval round follows.
- **DERIVED:** create new distinct candidate IDs, proposed scenarios/criteria,
  sources, proposed relations, conflicts, consequences and a confirmation brief.
  Comparisons to existing requirements use exact typed identities and show
  current versus proposed content. Never overwrite existing approval/content.
- **Exceptions:** insufficient provenance remains explicitly DERIVED; do not
  invent edges to meet a count. Report partial baseline output and remainder.
  Candidate test stubs and edges remain review-only and cannot count as ordinary
  tests, coverage, execution assignments or release snapshot content.

Keep stable run/group keys and input fingerprints. After interruption, query
receipts before retrying identical input. A conflict requires reconciliation,
not generating a new key to defeat the check. Read captured historical evidence
by identity; do not silently replace an unavailable source with latest code.

In a store-backed workspace, the store is the sole authority. Do not recreate
retired `tasks/*-REQUIREMENTS.md`, `WORKLIST.md` or mirrored `file-state/` files
to make old synchronization work. In a file-backed workspace, use the canonical
`file-state/` shapes and preserve the same authorization/receipt semantics;
if tooling cannot enforce baseline authority, report the limitation rather than
silently assigning PENDING_VERIFICATION.

## Exact candidate review

Prepare the exact typed UR/SR selection and separately named proposed links.
Preview the current content, evidence, comparisons and transitions at one graph
fingerprint. One human action may approve all named requirements and links;
neither an Epic nor an open delivery release is required. Omitted links stay
candidate and may be accepted later without reopening accepted requirements.

Route explicit decisions only:

- accept as-built → Base/PENDING_VERIFICATION;
- accept desired intent → PROPOSED, eligible for normal delivery planning;
- reject → OBSOLETE, no relationship publication;
- defer → unchanged DERIVED.

Changed content/relationships invalidate the preview. Refresh it; never retry
an old human decision against new content. Record typed requirement/link
decisions and an idempotent receipt. Approval is additive, not replacement,
compliance approval, behavioral verification, entry approval or DONE.

## Relation serialization

Store-backed onboarding uses the sanctioned typed group contract: an SR's
`parent_external_ids` names exact UR parents; typed code/test citations create
the corresponding proposed or baseline source links. The server returns exact
trace IDs and authority. Candidate decisions select those trace IDs separately.
Do not put prose relation clauses into store fields and assume they create edges.

For an existing **file-backed legacy ledger only**, retain these importer shapes:

```
Proposed relations (CANDIDATE): requires SR-KERNEL-030, SR-KERNEL-031.
```

```
Proposed relations (CANDIDATE): serves UR-KERNEL-002.
```

```
- **UR:** UR-KERNEL-002
```

`requires` names rows that take this row as parent; `serves` names its parent.
A dedicated UR field takes precedence over a packet sentence. Mere mentions of
IDs create no relation. An unknown target is a reported defect, not a dropped
edge. An explicit empty typed relation set plus rationale is valid; never invent
a parent to satisfy a count. These legacy forms do not authorize promotion or
recreation of retired store-backed task ledgers.

## D1. Recovered system-document output contract

Read maintained `docs/guides/` first as a human lens, not evidence of implemented
behavior. Preserve guide provenance separately from derived findings. Reuse or
extend existing documents before creating a competing account. Produce the
following system-wide set once, with verified code/config citations:

| Document | Content |
|---|---|
| `docs/03-architecture.md` | Subsystems, interfaces, stores and representative request flow |
| `docs/20-deployment-topology.md` | Runtime locations and boundaries; explicitly fold into architecture for a single stack |
| `docs/21-integrations.md` | Direction, protocol and failure behavior, including unreachable dependencies, for each external system |
| `docs/22-cross-cutting.md` | Auth, tenancy, secrets, observability and resilience enforcement points |
| `docs/23-data-flow.md` | Origins, transformations, destinations and trust boundaries |

An existing `ARCHITECTURE.md` or `architecture.md` can satisfy the architecture
role; adopt it explicitly and verify the document collector recognizes its path.
Use the sanctioned document publication route and verify its store readback.
These are derived design documents, not a second requirement store.

## D2. Observed decision records — `docs/adr/NNNN-<slug>.md`

Key by slug and preserve existing ADRs. New recovered decisions carry status
`observed`, never fabricated ratification. A code/config citation establishes
what choice was implemented, not that a human accepted it.

## D3. NFRs (legacy file-backed path: `tasks/NFR-REQUIREMENTS.md`)

In a **store-backed** workspace, publish NFRs as ordinary typed requirements
through the authorized run. Never create the path in this heading there. It is
the compatibility contract for workspaces whose authoritative ledger already
uses that file-backed format; canonical file-state workspaces use their selected
`file-state/` serialization instead.

Use context IDs such as `REQ-NFR-NNN` and the categories performance, scalability,
availability, security, privacy, operability, maintainability and compatibility.
Check existing requirement ownership before deriving another row. A bare timeout,
pool size, retry count or threshold with unconfirmed intent stays a question or
DERIVED exception, not an invented target or measured result. Key repeated
findings by exact source/symbol. For numeric configuration, read the whole expression
and its units before interpreting it; a partial grep hit is not a value.
Before finishing, check the document set, every
integration's failure behavior, observed ADR provenance, NFR questions and actual
publication readbacks. A file existing locally does not prove it reached the UI.

## Audit and finish

Invoke `rdd-audit` over exact outputs and declared repositories. Measure both
directions: inventory units without requirements and requirement references
outside inventory. Use the authoritative store projection, not empty retired
ledgers. Distinguish **candidate extraction** from **governed coverage**, and both
from **verified behavior**. Zero recognized citations over nonempty input is a
failed measurement, not 100% coverage.

Keep re-runnable enumeration/matching scripts and their actual output. Report
N/N by denominator class and repository/context, exclusions and every miss.
The audit script exits non-zero below the floor; retain its verbatim output and
exit code, not just an agent's summary. Run the same measurement before and after;
rank uncovered directories and untraced test files to choose the next scope.
The extraction floor is 90% of each applicable behavior class and 60% of eligible
source files cited or explicitly dispositioned; target complete coverage.
No row budget replaces behavioral granularity. Below the floor is incomplete,
never silently truncated. A requested full sweep continues across remaining
contexts while authority and source scope stay unchanged, with no per-context
baseline reapproval.

Read back exact UR/SR IDs, lifecycle/release, citations, relationship authority,
test artifacts and optional Epic memberships. For baseline, verify Ledger and
System → Requirements agree with the receipt. For DERIVED, verify candidate
review contains them while governed views/counts remain unchanged. Report
created/reused/baselined/derived/rejected/unresolved counts separately.

Adopt existing design documents before creating competing ones. Recovered
architecture, deployment, integrations (including failure behavior), cross-cutting
concerns and data flow are system-wide, not per-context duplicates. Observed
ADRs are labelled observed, not ratified. NFRs use the same store and authority
as other requirements; bare thresholds with unknown intent remain questions.

Onboarding ends with a usable base or a clearly reviewable candidate set and an
honest remainder. Normal `rdd-plan`/entry review and `rdd-verify` own subsequent
verification; reverse-engineering itself changes no implementation, executes no
untrusted code and creates no passing test result or DONE state.
