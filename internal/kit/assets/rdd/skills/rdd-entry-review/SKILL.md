---
name: rdd-entry-review
description: Present the saved Epic plan and its independent review for the human to approve, request revision, or reject. Stops before implementation.
---

# Obtain implementation approval

Read `PROCESS.md`, the Epic's exact saved plan and proposed content, independent
review findings and any recorded resolutions. Present that content and review
to the human even when the review fails or gate blockers remain. This is the
decision point after the plan's review, or after a review of the change and the
full review before approval, not another review or repair pass. If the full
review before approval is due and not recorded, return to `rdd-cold-review`
before you present the plan.

Before requesting approval, confirm the Epic includes UR/SR
requirements covering all in-scope behavior, with explicit criteria and
planned code/test traces. An empty requirement set or plan-only acceptance
checks is not ready; disclose the gap for the human's decision. Run the
`implementation` Epic gate check once after the plan review and retain its
result. Recheck only after a relevant condition changes; do not repeat a check
to confirm an unchanged result. A blocked gate prevents implementation approval,
not presentation of the plan and review to the human.

For a round of an Initiative's Epics, run
`modernpath initiative check <initiative-id> --epics <ids> --phase implementation`,
present every Epic's plan in the round with the round's review and blockers, and
apply the human's one answer with
`modernpath initiative approve-implementation <initiative-id> --epics <ids> --reason "USER:..."`.
A refusal names the Epic by index and id, and nothing was approved. An
Initiative round is the explicit list of the Initiative's Epics given to the
command; one round approval writes each Epic's own approval records, all or
nothing.

Resolve and record declared human decisions before the independent review.
Read the authoritative current plan, decisions, traces and separate review
before asking for a decision. If an answer or reviewed input changed, disclose
the stale review and let the human direct the revision or renewed review.
Do not silently edit the plan or commission another review, except the full
review before approval.

Present the user outcome, included requirements and exclusions, material
sources, risks, test strategy, recorded decision answers, review verdict,
material findings, notes, limitations and gate blockers in plain language.
Group the notes as the reviewer marked them, requirement text and build, so the
human can choose which to fix before approval. For a failed review, give each
material finding with the correction you propose, and ask separately the
questions only the human can answer. Check the claims of each correction and
each option in the code before you offer them, so the human decides on facts.
Give each requirement-text note with the wording you propose, checked in the
code as you check a correction. Before you offer to leave a note to the build,
check whether acting on it would change behavior, requirement content the plan
would publish, or a file that other work reads or generates. If it would, do
not offer it for the build. Look for the answer in a recorded human decision,
then in a published requirement, then in the legacy system, and give the human
the plan correction with that source. Ask the human the question only when no
source answers it.
A decision of the planning context counts as a recorded human decision
(`rdd-plan` step 3).
Do not propose an approval rule that needs a review with no
notes.

Before you ask for approval, read
`modernpath epics plan history <epic-id> --limit 50 --json` and list for the
human each item recorded after the current plan review: for a plan save its
`changed` parts and its `comment`, for an edit of the Epic its `fields`; read
the next page with `--offset` while `has_more` is true. When the review is
current, these changes left it current, so no reviewer has read them. Say so
when there are none.

List also the changes that, after the current plan review was recorded, reached
the branch of the checkout that review read, in the files the plan's traces and
its `Reconnaissance` section name; a commit made before the review that reached
the branch after it counts. No reviewer has read the plan against those
changes. Say so when nothing landed.

When the planning cycle has three or more plan reviews, say so and present the
choices `PROCESS.md` §Planning and readiness gives for the cap, instead of
offering a further review.

Ask the human to approve eligible content, request a revision or further review,
or reject the plan. Wait for that decision. Notes do not require plan edits.
Do not imply that a passing review or autopilot grant is completion approval.

After applying the human's disposition, record actual resolutions and reasons
through the CLI, including findings from older reviews. Do not mark a promised
correction resolved. Record the disposition of each note the human leaves to
the build, so the build finds it with the review. Route a finding about
existing behavior that the Epic leaves unchanged to `rdd-triage` as a Backlog
record, unless the human widens the Epic. A requested revision returns
to `rdd-plan` for the specified changes, one new review and, when it is due,
the full review before approval, before presenting it again. Rejection stops
the plan. Do not manufacture resolutions or waive gate conditions to turn a
blocked plan into an approval.

If eligible and approved, apply the current-state approval with the human's
actual answer and `USER:` attribution through the CLI. Reuse that explicit
answer after recording resolutions if the reviewed content is unchanged;
do not ask for the same approval again. Read back the resulting Epic state and plan
content. Confirm the named requirements atomically acquired the human/Epic
ownership and moved directly to IN_PROGRESS, with published content unchanged.
Do not answer for the human or start test/code changes before the
approval is confirmed. If the CLI cannot record/read back the approval,
report the tooling gap and stop before implementation.
