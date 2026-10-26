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

1. Read current verification status through the supported requirement-selection
   check before reserving or executing anything. Skip DONE requirements. Reuse
   complete applicable recorded results for unchanged requirements and tests.
   Inspect current requirement content and executable assertions through their
   existing identities; a file link or test name alone does not prove execution.
2. Resolve required tests through TestCase-to-criterion Traces. Before executing,
   confirm each TestCase has an active, same-System Trace to its executable source
   file, as required by PROCESS.md's verification contract. Record missing code
   relationships through the supported recording operation. Select the
   existing TestCase UUIDs and revisions; a shared test executes once for all
   selected criteria it covers. Requirements and executable code define expected
   behavior. Store no procedure, runner, selector, assertion array or copied
   definition. For user requirements, check their scenarios independently of
   lower-level system tests and retain only real supporting relationships.
3. If the selected requirements already have complete applicable passing proof,
   report those result identities and proceed to separate human acceptance. Do
   not rerun tests merely to refresh a read, produce another presentation or
   repeat this pass. Missing tests, failed execution or changed behavior leave
   verification incomplete and route the needed work to normal development.
4. For requirements that have no recorded execution, reserve one running run
   through the CLI with the tested code revision. Retain its returned UUID before
   executing. Run each distinct required test once and finalize one actual result
   per test. Record the actual outcome, executed TestCase revision and criterion
   relationships. A failed
   assertion is FAIL; setup/build failures are ERROR. Missing, skipped, stale,
   inconclusive and incomplete evidence cannot establish pass. Upload no command,
   report, environment, raw output or definition snapshot. Every request uses
   current tenant/System permissions and scoped relationships.
5. Recover a lost finalization response by current readback or exact replay under
   the same run UUID. Never reserve another run to retry submission. Conflicting
   outcomes or mappings are refused rather than overwriting an observation.
   An empty reservation has no stored test selection: do not infer ownership or
   reuse it from a matching SHA. If its identity is lost, report the uncertainty
   instead of claiming a completed or exactly-once execution.
6. Read the current verification response again. All criteria of each unresolved
   requirement must be supported by one current run with complete passing test
   coverage. A newer partial or nonpassing execution cannot borrow older passes.
   Independent requirements may reuse their own already-recorded current runs;
   shared run/result records appear once across the selection. No accumulated
   execution history, accepted-version bodies or presentation bundle is needed.
7. Report the exact requirement set, criterion status, current run/result IDs,
   missing links, failures and uncertainty. Do not change product or test code
   during this pass or fabricate RED evidence for existing behavior. Complete
   verification is ready for rdd-reverse-engineer-accept; it does not itself
   accept the requirements or set DONE. Epic-owned work retains its separate
   registration and approval rules. An explicitly approved existing-behavior Epic
   follows `rdd-verify` using selected passing results, with no fabricated RED; ordinary
   implementation retains RED/GREEN.

Record grounded requirements even when tests are missing; verification is a
subsequent requested operation, not a prerequisite for recording. Do not restore
source captures, authorization runs, publication groups, fingerprints, generic
gates or the DERIVED/PENDING_VERIFICATION states for this path.

Use verified capabilities from the current CLI reference. If evidence cannot be
stored or read back, report the tooling gap and retain the actual results as
reviewable evidence without claiming they are persisted. Do not call the gateway
or edit process state as a workaround.
