---
name: rdd-cold-review
description: Review a saved Epic plan in a context separate from authoring, then hand the plan and findings to the human for a decision.
---

# Review the saved plan independently

Read `PROCESS.md`, the exact saved plan and proposed content, its linked Epic and
requirements, and the sources needed to assess its claims. Do not rely on the
author's session summary when source material can be inspected.

The agent handling the task asks the user to answer the declared questions and
records their answers before requesting the plan review. That agent then
fetches and retains the review bundle and supplies it with source access.
Fetch the plan review bundle with `--phase plan-ready`. Only with the phase
does it hold the earlier reviews of this planning cycle, their findings and
their recorded resolutions, and in `readiness` the gate's causes.
Use that exact bundle; do not run unavailable process tools or substitute a later
bundle. For a round of an Initiative's Epics, the agent supplies
`modernpath initiative review-bundle <initiative-id> --epics <ids> --phase <phase>`
and records one review of the round with `modernpath initiative review`; the
review covers every criterion and obligation of every Epic in the round. For an
Initiative round, one review covers the round: read the round review bundle of
the explicit list of Epics and record one review for all of them. Return coverage of every criterion and verification obligation, reviewer
context, reason, findings and limitations. Context separation is an attestation,
not proof of different human identities.

Before you hand the bundle to the reviewer, check that it holds the title of
each TestCase the plan links and the planning contexts of the Epic's Release
and Initiative. Supply a missing part from `modernpath test-cases get <uuid>`
or `modernpath epics get <epic-id> --fields planning_contexts`, and tell the
reviewer where it came from. Name a part you cannot supply to the reviewer
once, and report the tooling gap through the project's named channel. Before
you request a plan review, count the plan reviews of the planning cycle: the
records in the bundle's `reviews` whose `phase` is `plan`. At three or more,
request no review unless the human chose a revision with its review or asked
for a review, and follow the cap of `PROCESS.md` §Planning and readiness.

For a review of the change, also supply the output of
`modernpath epics plan history <epic-id> --limit 50 --json`, fetched after the
last change, and the next page with `--offset` while `has_more` is true. The
causes are in the bundle's `readiness`, in the blocker `plan_review_required`.
When the causes name `stored_trace`, also supply
`modernpath traces list --entity-type <kind> --entity-id <id> --status all --limit 200 --json`,
and the next page with `--cursor` while `next_cursor` is not null, for each
requirement, criterion and test of the plan and each requirement it retires
with its criteria, and tell the reviewer which lists could not be read. For a
round, supply the history of each Epic in it.

For a review of the change, also supply the changes that, after the previous
review was recorded, reached the branch of the checkout the reviewer reads,
with their content, in the files the plan's traces and its `Reconnaissance`
section name; a commit made before the review that reached the branch after it
counts. For a round, supply them for the files of each Epic in it. Say so when
nothing landed.

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
- the plan or change keeps to the Active Engineering Constraints the review
  bundle carries (`engineering_constraints`: ref, revision, title, statement,
  rationale and verification method), read beside the planning contexts; a
  finding that concerns one cites it as `EC-<number>` with the revision read
  and says what the change does against it; a finding whose only ground is a
  constraint is recorded with severity `note`: it is information, not a
  verdict, and blocks no gate; the PR-review read of constraints belongs to
  `rdd-pr-review` and is outside this skill;
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
check once. Record the reviewer's reason with its first words unchanged. A
review whose reason does not start with `Review of the change.` counts as a
full review. After recording a review of the change, the agent handling the
task fetches the bundle again; a reviewer still uses only the bundle it was
given. When the full review before approval is due (`PROCESS.md`), obtain it
and hand both reviews to `rdd-entry-review`. Hand off to `rdd-entry-review`
with the exact saved plan, review and recorded blockers, including a failed
review or blocked gate. Do not revise the plan, resolve findings on the human's
behalf, or request another review before the human decides, except the full
review before approval. Keep review results out of the saved plan body.

A human-requested revision gets one new independent review, then the full
review before approval when it is due, and another human decision. Reuse an
existing review on session resume. If reviewed inputs changed, disclose that
the review is stale and ask for a human decision before renewing it. Gate
checks and finding-resolution records do not themselves require another
review. A passing review grants no implementation approval. The reviewer does
not implement, change process state, or approve on the human's behalf.
