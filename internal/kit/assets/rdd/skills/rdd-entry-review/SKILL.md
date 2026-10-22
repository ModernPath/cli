---
name: rdd-entry-review
description: Present the exact saved Epic plan and proposed content for human implementation approval. Stops before implementation.
---

# Obtain implementation approval

Read `PROCESS.md`, the Epic's exact saved plan and proposed content, independent
review findings and their resolution. Confirm that the human is reviewing that
content and its stated scope.

Before requesting approval, confirm the Epic includes UR/SR
requirements covering all in-scope behavior, with explicit criteria and
planned code/test traces. An empty requirement set or plan-only acceptance
checks is not ready; return it to `rdd-plan`. Run the `implementation` Epic
gate check after the plan review and record its result before asking for approval.
If it reports blockers, resolve them through the appropriate pass and check
again. A passing check does not record the human's approval.

Resolve and record declared human decisions before the final independent review.
Retain the authoritative review bundle and its exact saved plan before asking
for implementation approval. A changed answer or reviewed content requires
renewed review. Present only this retained content.

Present the user outcome, included requirements and exclusions, material
sources, risks, test strategy, recorded decision answers and recommendation in
plain language. Ask the human to approve or revise the plan for
implementation. Do not imply that a passing review or autopilot grant is
completion approval.

If approved, submit the unchanged retained presentation with the human's actual
answer and `USER:` attribution through
the CLI and read back the resulting Epic state and plan
content. Confirm the named requirements atomically acquired the human/Epic
ownership and moved directly to IN_PROGRESS, with published content unchanged.
Do not answer for the human or start test/code changes before the
approval is confirmed. If rejected or revised, report that result and route
back to `rdd-plan`. If the CLI cannot record/read back the approval,
report the tooling gap and stop before implementation.
