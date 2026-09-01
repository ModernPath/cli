---
name: rdd-verify
description: Verify human-confirmed PENDING_VERIFICATION URs and SRs against real behavior, add missing tests, and advance only evidence-backed state. Use after an as-built requirement has been confirmed and needs current UR upper or SR lower evidence. Repeat verification inside the AI TDD loop without bypassing approval or delivery. Never use for DERIVED requirements or new behavior; run confirmation or the normal red-first build loop instead.
---

# Verify confirmed as-built requirements

Turn behavior described from shipped code into current direct UR upper or SR
lower evidence.

Read the repository `AGENTS.md` and canonical `PROCESS.md`
(`.modernpath/rdd/PROCESS.md` in a consuming repository) before changing a
project repository. `PROCESS.md` owns trace, status, gate, evidence, and
completion meanings.

## Keep the transition honest

This skill verifies confirmed as-built behavior. It does not grant human
approval or prove delivery.

- Never run this skill for a `DERIVED` requirement. `DERIVED` means no human has
  confirmed that the requirement exists; its proposed links are candidate
  context and all downstream work is held. Apply its confirmation gate first.
- `PENDING_VERIFICATION` means the requirement and as-built description were
  already confirmed, but current direct test evidence is missing. It still
  needs human entry approval and must become `TODO` before tests change.
- For an SR, record `LOWER_VERIFIED` and move it to `IN_REVIEW` only when its
  lower trace is current. For a UR, record `UPPER_VALIDATED` and move it to
  `IN_REVIEW` only when its upper trace and required-SR conditions are current.
- Never move a requirement to `DONE` from test evidence alone.
  `DONE` also requires the applicable approval, authoritative-source
  delivery, and reconciliation conditions.
- If the implementation contradicts the row, record the discovery and route a
  separate red-first change. Do not silently change behavior during a
  verification pass.

## Enter through the same gate as any other change

A `PENDING_VERIFICATION` row is a description someone accepted. It is not an
entry permit, and this skill adds tests — which is implementation.

If the row is `DERIVED`, if a requirement it actually depends on is `DERIVED`,
or if an applicable relation is candidate-only, stop. Do not inspect tests as
though that trace were authoritative, and do not use existing SR evidence to
imply that an inferred user outcome is valid.

Before changing any test, require an authoritative requirement source and its
applicable trace:

```text
UR -> acceptance scenario -> TEST_CASE -> TEST_RESULT
SR -> CODE -> TEST_CASE -> TEST_RESULT
```

Epic membership and UR links are optional. A standalone SR is valid and must
not be sent back to planning merely because it has neither relation. When the
SR is linked to UR acceptance content, record the declared relation; the UR's
upper evidence remains part of the UR trace, not the SR lower trace. When an
epic groups the SR or linked UR, verify that membership only if the selected
scope depends on it.

Use single-SR scope for one independently verifiable SR, whether standalone or
linked to UR acceptance content. Use Epic scope when a UR or multiple
requirements are selected. If establishing the evidence changes a user outcome
or acceptance content, requires another independently implementable SR, or
introduces a cross-cutting decision, return it to Epic-scoped planning. In
either scope, fulfill the current planning, reconnaissance, cold-review,
test-strategy, work-selection, and entry-brief facts.

Then obtain strict human entry approval for every selected requirement and
Epic. An already approved related entity does not return to `TODO` merely
because another requirement starts. Move the selected requirement to `TODO`
before changing a test. If the Entry packet is incomplete or the entry answer
is absent, stop and route the entry first.
Verification outside the authoritative work selection is invisible to planning,
and a test written before the gate cannot be traced to approved intent.

## Establish the evidence bar

For each row, require all of the following:

1. Resolve its source, code, and test citations.
2. Split the statement and acceptance criteria into behavioral clauses.
3. Name an assertion that would fail if each clause regressed.
4. Confirm the test exercises the real subject rather than a mock that returns
   the expected answer.
5. Confirm the material inputs can **reach** that subject in production — that
   the caller, parser, contract or serializer actually produces the shape the
   test passes in. A real subject invoked with a shape no production path can
   construct verifies a function, not a behavior.
6. Open and read the exact test and its assertions.
7. Run the exact test and confirm from verbose output or runner enumeration
   that it executed.
8. Observe the expected failure before the passing result. For already-shipped
   behavior, use a safe local mutation or equivalent targeted failure, restore
   it immediately, and inspect the diff before continuing.
9. Run proportional regression gates and record evidence against the current
   revision.
10. Cite evidence by stable test path and name, for example
    `TEST:path/to/file:TestName/Subtest`, plus the observed `RUN:` command and
    result. Treat line numbers as optional, unstable navigation hints.

A green suite is not evidence for every clause in a sentence. Underline every
verb and every "and"; a clause without a corresponding failure-producing
assertion remains unverified.

## Work one row at a time

1. Read the row, its acceptance criteria, and every cited source.
2. Search for an existing test by behavior and assertion, not just filename.
3. Use an acceptance/E2E test for a UR scenario and the appropriate focused
   boundary test for an SR clause. If a sufficient test exists, run it and
   demonstrate its relevant failure mode. Otherwise, add the smallest test.
4. Restore any temporary mutation, run the focused test green, then run the
   required regression gates.
5. Update the authoritative requirement, optional related epic, evidence, and
   work-selection records atomically; in a store-backed repository, refresh
   the materialized snapshots afterwards.
6. Advance only the evidence conclusion justified by the run. Move the selected
   requirement to `IN_REVIEW` only if its applicable trace gates pass; otherwise
   leave it at the strongest supported non-final state.
7. Repeat for every approved scenario or clause lacking current evidence. Do
   not request human input for an evidence failure within the approved
   fingerprint. Route changed intent or scope through `rdd-triage`; record an
   external impediment as a blocker.
8. Run the project's deterministic process checks. Deliver and reconcile before
   soliciting completion acceptance; only the subsequent human gate may move a
   requirement to `DONE`.

## Detect verification traps

- **A filter matched nothing.** A runner can print success after executing zero
  tests. Use verbose output or list/count the selected test cases.
- **The source window hid the answer.** Before reporting a contradiction,
  inspect the complete function and its callers rather than a short excerpt.
- **The new test request is invalid.** Prove the production path was entered
  and inspect what collaborators received before blaming the implementation.
- **The test mocks the subject.** A stubbed service can verify the fixture while
  bypassing the behavior named by the row. Leave the row unverified.
- **The subject is real and the input is not.** A hand-built struct, a
  pre-decoded payload, or a field combination the parser cannot emit tests a
  function the production path never calls that way. Trace one material input
  back to its real producer; if nothing constructs it, the clause is unverified.
- **An assertion is satisfied by surrounding UI or fixture data.** Ask what
  production regression would make the assertion fail. If none would, replace
  it.
- **A citation moved.** Prefer stable test names over `path:line`; re-check any
  retained line number after editing.
- **The endpoint was contacted with the wrong payload.** Assert the material
  values and protocol fields named by the requirement, not only a path, status,
  or non-empty response.

## Leave honest gaps

Do not weaken a test to promote a row.

- If the row is only an inference that no human confirmed as a requirement,
  move it to `DERIVED`, record candidate links, and emit its confirmation gate.
- If the implementation cannot satisfy the row, record the contradiction and
  route the required new or changed SR through planning.
- If verification needs unavailable infrastructure, keep the row
  `PENDING_VERIFICATION` before entry; after entry, use `BLOCKED` and record the
  suspended `TODO` or `IN_PROGRESS` state.
- If only a mocked path is available, record `test run, subject mocked` and
  leave the row unverified.

## Report the pass

Report:

- rows that gained direct evidence and their new evidence/work state;
- rows left unchanged, grouped by reason;
- tests added or reused and confirmation that each relevant failure mode was
  observed before green;
- focused and regression commands run;
- contradictions, discoveries, pending approvals, delivery work, and record
  reconciliation gaps; and
- the exact next handoff: `rdd-completion-review`, `rdd-triage`, or the unmet
  prerequisite.

Exit only when every touched row has current direct evidence or an explicit
reason it remains unverified, all temporary mutations are gone, and repository
state passes its deterministic checks.
