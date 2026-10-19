---
name: rdd-verify
description: Verify implementation under an approved Epic plan, including current lower-level and user-flow evidence and proportional regression checks.
---

# Verify approved work

Read `PROCESS.md`, the approved plan and proposed content and its native requirement,
code and test links. Confirm the tested content is the current candidate
revision and the work remains within approved scope.

1. Confirm each changed behavior has a recorded expected RED and passing
   result. A build error or unrelated failure does not count as RED.
2. Run focused tests after cleanup and proportional boundary/regression gates.
   Choose checks appropriate to the boundary: unit/property, component and
   browser, endpoint/contract, persistence/integration, schema conformance, or
   operations harness/smoke.
3. Rerun affected user acceptance scenarios and report runtime/browser
   observations. UI evidence requires the live stack, loaded assets and
   inspected output.
4. Link each result to its native requirement, test identity and exact code
   revision. Broad suite output proves only exercised assertions. Do not
   reconstruct evidence or mark a skipped test as passing.
5. Report failures as implementation work within approved behavior, or route
   material scope/acceptance changes back to `rdd-plan` for renewed approval.

Do not claim completion here. Hand the passing candidate and evidence to
`rdd-completion-review`; if the process interface cannot store or read back
required evidence, report the tooling gap and its effect.
