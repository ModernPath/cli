---
name: rdd-reverse-engineer-verify
description: Verify reverse-engineered requirements against existing behavior and real test executions before human acceptance. Use when implementation already exists; route missing behavior or tests to normal development.
---

# Verify existing behavior

Read the project's instructions, current process and CLI reference.
Start from an explicit System and user/system requirement IDs. Read their
current content, active acceptance criteria and implementation/test traces.
Acceptance targets are PROPOSED. Existing DONE requirements may support the
trace, but are not accepted again. Verification does not change lifecycle state.

1. Identify what each acceptance criterion requires and where the existing
   implementation supplies it. Preserve the actual requirement, code and test
   identities. A source link or test name alone does not establish correctness.
2. Map every in-scope criterion to meaningful assertions. For a user requirement,
   assess its user-facing scenarios independently of lower-level system tests.
   Include the supporting system requirements actually required by those
   scenarios; do not invent relationships or parents to fill a diagram.
3. Run the existing tests, or inspect retained execution results that genuinely
   apply to the inspected implementation. Record the command/report, environment,
   tested code revision, test identities, criterion targets and actual PASS,
   FAIL or SKIP outcomes through the CLI. A broad
   passing suite proves only the assertions it executed.
4. Report uncovered criteria, failures, skipped checks and uncertainty. If tests
   or behavior need changing, leave the requirements PROPOSED and hand the
   missing work to normal planning/build/verification. Do not change product or
   test code during this pass or fabricate a RED result for existing behavior.
5. Read back the stored evidence and traces. Recheck that the requirement
   content and relevant implementation still match what was evaluated. Present
   the exact requirement set, criterion-level results, exclusions and remaining
   risks. Complete evidence is ready for rdd-reverse-engineer-accept; it is not
   human acceptance and does not itself mark anything DONE.

Record grounded requirements even when tests are missing; verification is a
subsequent requested operation, not a prerequisite for recording. Do not restore
source captures, authorization runs, publication groups, fingerprints, generic
gates or the DERIVED/PENDING_VERIFICATION states for this path.

Use verified capabilities from the current CLI reference. If evidence cannot be
stored or read back, report the tooling gap and retain the actual results as
reviewable evidence without claiming they are persisted. Do not call the gateway
or edit process state as a workaround.
