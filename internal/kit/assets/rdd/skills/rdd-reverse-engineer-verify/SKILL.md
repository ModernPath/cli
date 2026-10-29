---
name: rdd-reverse-engineer-verify
description: Verify reverse-engineered requirements against existing behavior and real test executions before human acceptance. Use when implementation already exists; route missing behavior or tests to normal development.
---

# Verify existing behavior

Do not change product or test code during this pass or fabricate RED. If
behavior or coverage needs a change, route it to ordinary development.

Read the project's instructions, current process and CLI reference.
Start from an explicit System and user/system requirement IDs. Read their
current content, active acceptance criteria and implementation/test traces.
Acceptance targets are PROPOSED. Existing DONE requirements may support the
trace, but are not accepted again. Verification does not change lifecycle state.

1. Read current verification status and skip DONE requirements. Inspect each
   active criterion, its required TestCases and executable assertions. A file
   link or test name alone does not prove execution.
2. Confirm each of the required tests has a scoped Trace to its executable source
   in the same tenant/System.
   Record a missing relationship through the supported operation. A shared test
   can execute once, but each criterion needs its own linked result.
3. Reuse complete current passing proof when it already covers the criterion.
   Otherwise execute the required test and record the actual outcome of each executed TestCase for the
   criterion and TestCase. Distinguish assertion failures from setup errors.
   Do not upload commands, raw output or copied definitions.
4. Read back current proof. Every required TestCase must pass for its criterion.
   Independent updates combine; missing or nonpassing proof blocks the affected
   criterion. Revision numbers alone do not invalidate proof. Changing a
   criterion's meaning removes its current results, so verify it again.
5. Report requirement and criterion status, result identities, missing links,
   failures and uncertainty. Verification does not publish or accept content.
   Use `rdd-reverse-engineer-accept` for separate human acceptance. Route changed
   behavior or tests through normal development.

Record grounded requirements even when tests are missing; verification is a
subsequent requested operation, not a prerequisite for recording. Do not restore
source captures, authorization runs, publication groups, fingerprints, generic
gates or the DERIVED/PENDING_VERIFICATION states for this path.

Use verified capabilities from the current CLI reference. If evidence cannot be
stored or read back, report the tooling gap and retain the actual results as
reviewable evidence without claiming they are persisted. Do not call the gateway
or edit process state as a workaround.
