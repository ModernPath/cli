---
name: rdd-cold-review
description: Review a saved Epic plan once in a context separate from authoring, then hand the plan and findings to the human for a decision.
---

# Review the saved plan independently

Read `PROCESS.md`, the exact saved plan and proposed content, its linked Epic and
requirements, and the sources needed to assess its claims. Do not rely on the
author's session summary when source material can be inspected.

The agent handling the task asks the user to answer the declared questions and
records their answers before requesting the plan review. That agent then
fetches and retains the review bundle and supplies it with source access.
Use that exact bundle; do not run unavailable process tools or substitute a later
bundle. For a round of an Initiative's Epics, the agent supplies
`modernpath initiative review-bundle <initiative-id> --epics <ids> --phase <phase>`
and records one review of the round with `modernpath initiative review`; the
review covers every criterion and obligation of every Epic in the round. For an
Initiative round, one review covers the round: read the round review bundle of
the explicit list of Epics and record one review for all of them. Return coverage of every criterion and verification obligation, reviewer
context, reason, findings and limitations. Context separation is an attestation,
not proof of different human identities.

Check that:

- the user outcome, scope and exclusions are explicit and consistent with
  authoritative sources;
- one or more UR/SR requirements cover all in-scope behavior, with
  explicit criteria and planned code/test traces; an empty requirement set
  or plan-only acceptance checks is a material finding;
- acceptance and requirement links trace to the intended behavior using
  identities;
- reconnaissance covers affected state, interfaces, integrations, failure
  modes, risks and project test gates;
- the change boundary says what changes and what must remain unchanged;
- the plan stays within the planning contexts of the Epic's Release and
  Initiative that the review bundle carries (`planning_contexts`) and names the
  decisions and assumptions it relies on; a plan that contradicts one of them
  is a material finding;
- every reader and screen of changed data, behavior at the largest expected
  input, and the privileges and bypasses of any database or security
  mechanism the plan relies on are covered;
- each changed behavior has a meaningful expected RED that fails at the first
  commit for the stated reason, and a proportional verification strategy;
- each changed statement agrees wherever it appears: plan body, requirement
  fields, criteria and test titles;
- material security, data-integrity, operational, migration and testability
  concerns are addressed.

A source link resolving is not proof that it supports the claim. Open the
source and assess the connection. Report findings with severity, the precise
claim or plan section, inspected evidence and a concrete correction. Check a
correction you suggest in the code before you suggest it, including how a
changed record shape behaves after approval; the planner treats it as a claim
to verify. Mark each
note as requirement text, when the plan would publish false requirement content
or change promised behavior, or as build, for test detail, plan wording and
disclosures. Return one review with all findings and limitations. Notes are
nonblocking.

The agent handling the task records the actual verdict, coverage, reason and
findings without uploading a presentation, then runs the `plan-ready` Epic gate
check once. Hand off to `rdd-entry-review` with the exact saved plan, review and
recorded blockers, including a failed review or blocked gate. Do not revise the
plan, resolve findings on the human's behalf, or request another review before
the human decides. Keep review results out of the saved plan body.

A human-requested revision gets one new independent review and another human
decision. Reuse an existing review on session resume. If reviewed inputs changed,
disclose that the review is stale and ask for a human decision before renewing
it. Gate checks and finding-resolution records do not themselves require another
review. A passing review grants no implementation approval. The reviewer does
not implement, change process state, or approve on the human's behalf.
