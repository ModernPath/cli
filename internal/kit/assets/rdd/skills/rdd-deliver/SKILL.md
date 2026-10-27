---
name: rdd-deliver
description: Deliver an authorized Epic through its saved plan, human implementation approval, engineering evidence and human completion approval.
---

# Deliver an Epic

Read `PROCESS.md` and run `rdd-start`. Small changes follow §Small changes,
including governed requirements and criteria, source/test Traces,
recorded RED/GREEN evidence and regression checks, without the Epic steps below.
Continue through the separate human requirement completion decision and a
supported completion operation; report missing capability without claiming DONE. For Epic delivery, use the project's CLI and confirm the
human-selected Epic and its latest saved plan before acting.

1. Run `rdd-start` and identify the earliest incomplete phase.
2. Establish sources and scope with `rdd-discover` when needed; save the plan
   with `rdd-plan`.
3. Obtain and record answers to declared decisions before the plan review.
   Retain the authoritative bundle. Obtain one independent plan review with
   `rdd-cold-review`; reuse it if already recorded. Do not revise the plan or
   repeat the review in response to its findings before the human decides.
4. Present the exact saved plan, proposed content, review and gate blockers
   with `rdd-entry-review`, even when the review fails. Wait for the human's
   decision. Only a human-requested revision or further review starts another
   pass, which returns to the human. Do not implement before eligible approval.
5. Implement red-first with `rdd-build`, then run `rdd-verify` for required
   focused, boundary and user-flow evidence.
6. Use `rdd-completion-review` for independent code review, trace audit and
   separate human completion approval. Only that approval publishes requirement
   content, raises revisions, records audit and releases ownership atomically.
   After completion approval is read back, use the CLI to mark each backlog,
   gap or tooling record routed to the Epic as closed by the Epic, and read
   each record back. If the human wants a record left open, leave it open and
   add a note saying why.

Keep all work inside the approved Epic. A material scope or acceptance change
may revise its saved plan only while the Epic is `PROPOSED`; after plan review,
wait for the human to direct that revision. Once implementation
is approved, keep the active plan and owned requirements read-only until an
explicit human return to planning under `PROCESS.md` is applied. Resume the
loop with the revised draft, fresh independent review, human implementation
approval and registered verification. DONE stays closed; use a follow-up Epic. Report blockers, assumptions, excluded work and evidence
honestly. A focused skill exit is a handoff, not completion. Never infer
completion from code, tests, review or publication alone.
