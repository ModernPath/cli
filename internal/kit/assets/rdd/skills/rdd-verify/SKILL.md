---
name: rdd-verify
description: Verify implementation under an approved Epic plan, including current lower-level and user-flow evidence and proportional regression checks.
---

# Verify approved work

Read `PROCESS.md`, the approved plan and its requirement, code and test Traces.
For each criterion, verify that every required TestCase has a current passing
`CriterionTestResult` linked to that criterion and a scoped source Trace. Record
actual direct outcomes with `modernpath evidence record`, or
`modernpath epics build record` after a delegated build, and read back the
current results and Traces. Revision differences and prior outcomes do not
determine proof applicability.

`ModernizationTestRun` and `ModernizationTestResult` are historical outcomes
captured with `modernpath modernization record` under a Modernization Release.
They do not satisfy, replace or invalidate current criterion proof or ordinary
readiness.

Run focused tests after cleanup, proportional boundary and regression checks,
and affected user scenarios. Report runtime observations and limits. A run whose outcome depends on a language model is a measurement; report its result
without presenting it as a deterministic test outcome. Do not reconstruct results or mark skipped tests
as passing. Keep commands and output in PR notes. Make the single rebase onto
main that `rdd-build` names before your final checks, rerun the affected checks
after it, and name in the PR notes the commit your checks ran at and the tests
they ran.

For existing behavior, verify the selected current facts without changing code or
fabricating failures. Route missing behavior or tests to normal development. A
material scope change requires an explicit return to planning and fresh approval.
Retest each published criterion whose approved meaning differed from its published
meaning after reapproval; its current proof was cleared on return to planning.
Finish evidence collection before independent implementation review. A material current criterion outcome change in review scope outdates the Epic review; an identical retry does not.
Changes to reviewed content, code or structural relationships require renewed review.

Do not claim completion here. Hand current results and limits to
`rdd-completion-review`, which reuses them; report a tooling gap if required evidence cannot be
stored or read back.

## Standalone small changes

For new standalone completion, retain one applicable same-System
`user_requirement_id`, `scenario_id` and `supporting_system_requirement_ids`,
or one nonblank `no_user_requirement_reason`. Verify the actual typed
relationships and current scenario and changed-SR proof. The no-UR reason
never waives criterion proof. Use focused RED/GREEN and proportional regressions.
Explicit human completion remains separate from implementation and evidence.

For published unowned content, use `requirements proposal save`, then discover
canonical IDs with selector-free `requirements proposal get`. After a stale
mutation, read the current proposal and reconcile before retrying. Select its
exact `proposal_id` and `proposal_generation` on each evidence operation and
approved DONE update. Retained accepted criteria reuse current proof read-only.
Ordinary reads show accepted content while the proposal is open. Cancellation
preserves it. Active Epic work stays on its approved Epic path.
After publication, use `requirements get <UUID> --kind user|system --version
<VERSION-UUID>` to inspect the immutable accepted snapshot. Missing historical
versions remain unavailable. Use `traces remove` only for a named outdated
implementation link with a valid replacement and an attributed reason.
