---
name: rdd-deliver
description: Deliver an authorized Epic through its saved plan, human implementation approval, engineering evidence and human completion approval.
---

# Deliver an Epic

Read `PROCESS.md` and run `rdd-start`. Small changes follow §Small changes,
including its RED/GREEN evidence and regression checks, without the Epic
steps below. For Epic delivery, use the project's CLI and confirm the
human-selected Epic and its latest saved plan before acting.

1. Run `rdd-start` and identify the earliest incomplete phase.
2. Establish sources and scope with `rdd-discover` when needed; save the plan
   with `rdd-plan`.
3. Obtain and record answers to declared decisions before the final plan review.
   Retain the authoritative bundle. Obtain an independent plan review with
   `rdd-cold-review`. Resolve and record resolutions for material
   findings and revise the draft plan before implementation approval.
4. Present the exact saved plan and proposed content for human implementation approval
   with `rdd-entry-review`. Do not implement before approval.
5. Implement red-first with `rdd-build`, then run `rdd-verify` for required
   focused, boundary and user-flow evidence.
6. Use `rdd-completion-review` for independent code review, trace audit and
   separate human completion approval. Only that approval publishes requirement
   content, raises revisions, records audit and releases ownership atomically.

Keep all work inside the approved Epic. A material scope or acceptance change
may revise its saved plan only while the Epic is `PROPOSED`. Once implementation
is approved, keep the active plan and owned requirements read-only, surface the
change as a human decision and record new work separately for its own plan and
human implementation approval. Continue unaffected work within the active
approved scope. Report blockers, assumptions, excluded work and evidence
honestly. A focused skill exit is a handoff, not completion. Never infer
completion from code, tests, review or publication alone.
