---
name: rdd-reverse-engineer-verify
description: Verify a published reverse-engineered baseline using existing captured sources, semantic assertions, genuine execution reports and repository integration proof. Use before human acceptance; missing tests or behavior require normal development, and this skill grants no approval.
---

# Verify an existing baseline

## Purpose

Build an exact eligibility packet for existing behavior. This pass records real
evidence and gaps; it changes no product/test code, requirement lifecycle or
compliance approval and fabricates no RED or PASS.

## Inputs and prerequisites

Read the project `AGENTS.md`, canonical `PROCESS.md` "As-built verification and
acceptance", and the consuming project's sanctioned CLI procedure. In ModernPath,
use the installed `mp-process-cli` skill and `.modernpath/cli-reference.md`.

Start from the publication packet: exact requested UR/SR identities, captured
source/run identities, persisted citations and publication receipts. Acceptance
targets must be PENDING_VERIFICATION with persisted source-scoped baseline provenance. Include
every required confirmed SR of each selected UR; already DONE SRs may remain
proof dependencies without being accepted again. A standalone SR needs no
invented parent or Epic. Individual DERIVED confirmation alone does not confer
eligibility for this path. Entered or defect-reopened work uses normal development.

## Resume checks

Read retained source, execution, integration and preview identities before
collecting again. Reuse genuine current evidence for the same captured snapshot;
rerun only what is missing, stale or invalid. Preserve stable evidence keys and
returned report/result IDs and digests. Resolve conflicts before retrying.

If a gate for this scope exists and its answer/application status is unknown,
hand its ID and retained inputs to `rdd-reverse-engineer-accept` for recovery
before collecting evidence. A known stale, unapplied gate does not repeat that
handoff: continue verification of its pending targets and retain the old gate
status for successor review. An applied receipt is historical evidence; do not
preview DONE targets as pending work. Established evidence invalidation routes
through `rdd-triage`, not back through baseline acceptance.

## Steps

1. **Define the denominator.** Read every active criterion for the selected
   requirements and required SR dependencies. Evaluate each UR's own upper
   scenarios independently. Record omissions explicitly.
2. **Inspect semantic proof.** Resolve captured code/test citations by exact ID,
   revision and hash through the supported source reader. Map each criterion to
   a meaningful assertion and the production subject it exercises. Bind the
   executed name to its registered TestCase or captured citation's `test_case_ref`.
   A name, CI badge, self-generated assertion, or unrelated passing test in the
   same file is insufficient. Add missing citation identities only through
   sanctioned fingerprinted authoring, then refresh the affected packet.
3. **Retain genuine execution.** Run existing tests at the exact captured revision
   or inspect a genuine retained current report. Record command, environment,
   named outcomes and exact source/test/report identities. CI reports retain
   provider, repository, run, job, attempt and tested commit. Record per-clause
   results through the evidence store: LOWER for SRs, UPPER for URs. Never reuse
   an SR pass as UR proof or label an unexecuted test as passing.
4. **Retain integration proof.** For each repository, use a genuine current
   integration observation or the supported collector, which fetches the remote
   default branch and checks the tested clean revision and captured snapshot at
   its tip. Preserve the returned observation and digest across identical
   retries. Passing branch tests alone are insufficient. Delivery here means
   observed repository integration, not deployment or live CI monitoring.
   Record the observed remote. ModernPath's collector reads the checkout's
   `origin`; it does not bind that setting to an independently declared expected
   repository. Disclose this limitation in the proof packet.
5. **Preview exact proof.** Submit the typed proof to the sanctioned eligibility
   evaluator. Missing, contradictory, stale, revoked or inaccessible evidence stays a gap. Adding
   tests or changing behavior requires an explicitly scoped handoff to normal
   planning/build/verify; do not repair product/test code in this pass.

## Outputs and handoff

Retain the exact proof input and returned digest/eligibility, criterion coverage,
all gaps, source/execution/CI/integration identities, stable retry inputs and
disclosed limits. An eligible packet goes to `rdd-reverse-engineer-accept`; an
incomplete packet names what evidence or development work remains. Preview alone
changes no lifecycle or compliance state.

If a required operation is missing, file the tooling gap through the sanctioned
channel. Do not call the API or edit the store directly as a workaround.
