---
name: rdd-entry-review
description: Present the exact saved Epic plan and proposed content for human implementation approval. Stops before implementation.
---

# Obtain implementation approval

Read `PROCESS.md`, the Epic's exact saved plan and proposed content, independent
review findings and their resolution. Confirm that the human is reviewing that
content and its stated scope.

Present the user outcome, included requirements and exclusions, material
sources, risks, test strategy, unresolved decisions and recommendation in
plain language. Ask the human to approve or revise the plan for
implementation. Do not imply that a passing review or autopilot grant is
completion approval.

If approved, record the human's actual answer and `USER:` attribution through
the sanctioned interface and read back the resulting Epic state and plan
content. Confirm the named requirements atomically acquired the human/Epic
ownership and moved directly to IN_PROGRESS, with published content unchanged.
Do not answer for the human or start test/code changes before the
approval is confirmed. If rejected or revised, report that result and route
back to `rdd-plan`. If the interface cannot record/read back the approval,
report the tooling gap and stop before implementation.
