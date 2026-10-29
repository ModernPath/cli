---
name: rdd-verify
description: Verify implementation under an approved Epic plan, including current lower-level and user-flow evidence and proportional regression checks.
---

# Verify approved work

Read `PROCESS.md`, the approved plan and its requirement, code and test Traces.
For each criterion, verify that every required TestCase has a current passing
`CriterionTestResult` linked to that criterion and a scoped source Trace. Record
actual direct outcomes with `modernpath evidence record` and read back the
current results and Traces. Revision differences and prior outcomes do not
determine proof applicability.

`ModernizationTestRun` and `ModernizationTestResult` are historical outcomes
captured with `modernpath modernization record` under a Modernization Release.
They do not satisfy, replace or invalidate current criterion proof or ordinary
readiness.

Run focused tests after cleanup, proportional boundary and regression checks,
and affected user scenarios. Report runtime observations and limits. A run whose outcome depends on a language model is a measurement; report its result
without presenting it as a deterministic test outcome. Do not reconstruct results or mark skipped tests
as passing. Keep commands and output in PR notes.

For existing behavior, verify the selected current facts without changing code or
fabricating failures. Route missing behavior or tests to normal development. A
material scope change requires an explicit return to planning and fresh approval.
Retest each published criterion whose approved meaning differed from its published
meaning after reapproval; its current proof was cleared on return to planning.
Finish evidence collection before independent implementation review. A material current criterion outcome change in review scope outdates the Epic review; an identical retry does not.
Changes to reviewed content, code or structural relationships require renewed review.

Do not claim completion here. Hand current results and limits to
`rdd-completion-review`; report a tooling gap if required evidence cannot be
stored or read back.
