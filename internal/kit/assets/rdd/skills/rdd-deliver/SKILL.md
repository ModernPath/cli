---
name: rdd-deliver
description: Deliver an authorized Epic through its saved plan, human implementation approval, engineering evidence and human completion approval.
---

# Deliver an Epic

Read `PROCESS.md` and use the project's sanctioned process interface. Confirm
the human-selected Epic and its latest saved plan before acting.

1. Run `rdd-start` and identify the earliest incomplete phase.
2. Establish sources and scope with `rdd-discover` when needed; save the plan
   with `rdd-plan`.
3. Obtain an independent plan review with `rdd-cold-review`. Resolve material
   findings and save a revised plan when needed.
4. Present the exact saved plan and proposed content for human implementation approval
   with `rdd-entry-review`. Do not implement before approval.
5. Implement red-first with `rdd-build`, then run `rdd-verify` for required
   focused, boundary and user-flow evidence.
6. Use `rdd-completion-review` for independent code review, trace audit and
   separate human completion approval. Only that approval publishes requirement
   content, raises revisions, records audit and releases ownership atomically.

Keep all work inside the approved Epic. A material scope or acceptance change
returns to planning for a revised plan and renewed approval. Report blockers,
assumptions, excluded work and evidence honestly. A focused skill exit is a
handoff, not completion. Never infer completion from code, tests, review or
publication alone.
