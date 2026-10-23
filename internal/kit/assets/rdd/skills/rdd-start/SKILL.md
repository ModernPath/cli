---
name: rdd-start
description: Enter a product-delivery session, confirm Epic scope or an explicit existing-behavior pass, and route work to the next needed phase. Use when starting, resuming, or asking what delivery work is active.
---

# Enter the delivery session

Read the consuming project's root instructions and `.modernpath/rdd/PROCESS.md`
(or this repository's `PROCESS.md`). Use the project's CLI for all process reads.

Before starting delivery work, follow the project's session preparation
instructions. If they provide a preparation command, run it from the connected
repository.

1. Determine whether the user explicitly requests changed behavior/tests or a
   pass limited to existing behavior. Do not infer this from the branch or
   worktree. For a small change, follow `PROCESS.md` §Small changes: state
   its scope and expected observation, then execute and retain RED/GREEN
   evidence without requiring an Epic or its approval steps below. Other
   changed behavior or tests use the Epic path. Explicit
   recording, verification or acceptance of existing behavior needs no
   implementation Epic.
2. For the Epic path, confirm the user-selected Epic by its ID. If none
   is selected, ask which one to work on. Read the Epic, its latest saved plan, implementation approval,
   lifecycle, linked requirements, recent evidence and completion state.
   If a draft Epic has no requirements, or its requirements lack
   behavior coverage, criteria or planned code/test traces, route to
   `rdd-plan` before approval. If already approved with these gaps, surface
   the conflict for a human decision; do not silently edit approved content
   or treat plan-only checks as sufficient. If the human requests an active Epic
   return to planning, follow `PROCESS.md` §Returning to planning through the
   CLI with the exact approved plan and decision. Read back PROPOSED, withdrawn
   approval and released ownership before revising the draft. If unsupported,
   report the tooling gap; do not edit active records through another channel.
3. For an explicit existing-behavior pass, read the System and
   requirement IDs, current content, criteria, traces, evidence and ownership
   from the CLI. Route recording to `rdd-reverse-engineer`,
   verification to `rdd-reverse-engineer-verify`, or requested human
   acceptance to `rdd-reverse-engineer-accept`. Active Epic-owned requirements
   remain on the Epic path. If behavior or tests need to change, stop this
   route and classify the change under step 1.
4. If Epic implementation is not approved for the current saved content, route to
   `rdd-plan`, `rdd-cold-review` or `rdd-entry-review`, whichever is first
   incomplete. Do not start implementation or test changes before approval.
5. If Epic implementation is approved, route to `rdd-build` or `rdd-verify` based
   on current evidence. If ready for delivery, route to
   `rdd-completion-review`.
6. If scope or acceptance changes while the Epic is PROPOSED, route back to
   planning and obtain human approval before implementation. Once implementation
   is approved, keep the active plan and owned requirements read-only until
   an explicit human return to planning is applied. Then require fresh review,
   implementation approval and verification. DONE uses a follow-up Epic.

If the CLI cannot read the current state, report the missing capability
through the project's tooling-gap channel. Do not recreate state from notes,
branches, local files or memory. Report the selected Epic, saved plan,
proposed content, approval state and evidence, or the selected System,
requirements and evidence for an existing-behavior pass, then identify the
next phase.
