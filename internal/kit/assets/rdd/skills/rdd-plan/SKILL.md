---
name: rdd-plan
description: Prepare and save a reviewable plan and proposed content for a human-selected Epic before implementation approval. Use for new or materially changed scope, acceptance, or technical context.
---

# Prepare the Epic plan

Read `PROCESS.md`, the selected Epic and its native requirement links. Inspect
relevant product documents, code and tests; do not treat repository state as
process authority.

1. Confirm the exact Epic and intended outcome. Preserve native IDs and
   identify included requirements and explicit exclusions.
2. Ground claims in source passages, code, tests or observed runs. Verify
   important citations by reading the source; a path alone is insufficient.
3. Inspect affected control/data flow, persistence, interfaces, integrations,
   failure modes, dependencies and project test gates. For shared or persisted
   state, identify actual writers, readers, existing shapes, partial-failure
   behavior and recovery.
4. Define behavior boundaries, acceptance scenarios, code/test targets,
   expected RED assertions, focused and proportional regression checks,
   operational and security risks, and the requirement content that completion would publish.
5. Record open questions, conflicts, assumptions, deferred scope and risk in
   plain language. Do not invent a CLI verb or local plan format.
6. Save the plan through the sanctioned process interface and read back the exact saved content. Stop for independent review and human implementation
   approval; do not change tests or product code in this pass.

Report the Epic ID, saved plan and proposed content, material decisions, sources and the
next phase. If saving or reading back is unsupported, surface that tooling gap.
