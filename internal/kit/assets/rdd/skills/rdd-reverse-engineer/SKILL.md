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

## Resume checks

If a run already exists, read its authorization, captured sources and group
receipts before collecting or publishing again. Reuse unchanged authorization,
keys and prepared inputs; reconcile acknowledged receipts with the unpublished
remainder. Retry identical input only when recovery requires it. A conflict is
not permission to invent a new key or overwrite an existing requirement.

Resume against the run's captured source identities. Reconcile corpus changes
against its own receipts; external changes or materially changed source scope
require fresh scope authorization. Never replace unavailable historical evidence
with latest code or ask again for an unchanged authorized run.

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
These reusable inputs are staging artifacts, not another requirement store.

Publish coherent atomic groups of related requirements. Split only for supported
limits or dependencies, sending referenced parents before dependent groups.
Read and retain each returned group receipt; perform a consolidated record
read-back and audit after the batch. Do not alternate deriving and publishing
one row at a time.
Atomicity is per group; report any unpublished groups explicitly.

Apply the authorized mode:

- **Baseline:** new grounded rows become PENDING_VERIFICATION in Base with
  confirmed UR–SR and SR–code/test relationships. Reuse only exact unchanged
  identities. No row/context approval round follows publication.
- **DERIVED:** publish distinct candidate IDs, proposed criteria/links, sources,
  conflicts, consequences and a confirmation brief. Compare exact typed existing
  identities and current/proposed content without overwriting approved content.
- **Insufficient provenance:** keep the row explicitly DERIVED with its reason
  and candidate packet. Do not invent edges to meet a count. Candidate test stubs
  and links count as neither ordinary tests nor execution/release coverage.

In store-backed workspaces, use typed `parent_external_ids` and code/test
citations; retain returned trace IDs and authority. Never recreate retired
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
Unresolved in-scope work and unchecked coverage remain incomplete. Continue a requested full
sweep across remaining authorized contexts without per-context reapproval.

Finish with a usable baseline or a reviewable candidate set and an honest
remainder. Existing-proof verification of published baselines belongs to
`rdd-reverse-engineer-verify`; its eligible packet then goes to
`rdd-reverse-engineer-accept`. Missing tests or changed behavior require an
explicitly scoped normal-development handoff.
