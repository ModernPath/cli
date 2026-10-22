---
name: rdd-verify
description: Verify implementation under an approved Epic plan, including current lower-level and user-flow evidence and proportional regression checks.
---

# Verify approved work

Read `PROCESS.md`, the approved plan and proposed content and its requirement,
code and test links. Confirm the tested content is the current candidate
revision and the work remains within approved scope.

1. Confirm each changed behavior has a recorded expected RED and passing
   result, with RED finalized before GREEN registration. If GREEN started too
   early, finalize valid original RED and register and execute a new GREEN.
   A build error or unrelated failure does not count as RED.
2. Confirm the declared candidate and authorized registration account. Register
   each focused, boundary, regression and user-flow execution before running it,
   including CI; finalize its returned identity with actual evidence and read back.
   Run focused tests after cleanup and proportional boundary/regression gates.
   Choose checks appropriate to the boundary: unit/property, component and
   browser, endpoint/contract, persistence/integration, schema conformance, or
   operations harness/smoke.
3. Rerun affected user acceptance scenarios and report runtime/browser
   observations. UI evidence requires the live stack, loaded assets and
   inspected output.
4. Link each result to its requirement, test identity and exact code
   revision. Broad suite output proves only exercised assertions. Do not
   reconstruct evidence or mark a skipped test as passing.
5. Report failures within approved behavior as implementation work. For a
   material scope or acceptance change, keep the active approved plan and
   owned requirements read-only, surface the change as a human decision and
   record new work separately for its own plan and human implementation
   approval. Continue unaffected implementation and verification within the
   active approved scope.

Finish evidence collection before independent implementation review. A rerun
that changes reviewed evidence requires renewing that review.

Do not claim completion here. Hand the passing candidate and evidence to
`rdd-completion-review`; if the CLI cannot store or read back
required evidence, report the tooling gap and its effect.
