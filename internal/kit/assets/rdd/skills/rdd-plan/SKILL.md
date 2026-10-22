---
name: rdd-plan
description: Prepare and save a reviewable plan and proposed content for a human-selected Epic before implementation approval. Use for new or materially changed scope, acceptance, or technical context.
---

# Prepare the Epic plan

Start the saved plan with a short, plain-language summary of what will change,
why it is needed, and its expected impact on users and affected parts of the
product. Put this before the detailed scope, requirements and implementation
approach so the human can quickly understand the proposed change.

Read `PROCESS.md`, the selected Epic and its requirement links. Inspect
relevant product documents, code and tests; do not treat repository state as
process authority.

1. Confirm the exact Epic, its lifecycle and intended outcome. Preserve
   IDs and identify included requirements and explicit exclusions.
   Create or revise its plan only while it is `PROPOSED`. Once implementation
   is approved, keep the active plan and owned requirements read-only; surface
   changed scope as a human decision and record new work separately for its
   own plan and human implementation approval. Continue unaffected work
   within the active approved scope.
   Include one or more UR/SR requirements covering all in-scope
   behavior, with explicit criteria and planned code/test traces. Use each
   kind as applicable; small changes have no exemption, and plan-only
   acceptance checks do not substitute for requirements.
2. Ground claims in source passages, code, tests or observed runs. Verify
   important source references by reading the source; a path alone is insufficient.
3. Inspect affected control/data flow, persistence, interfaces, integrations,
   failure modes, dependencies and project test gates. For shared or persisted
   state, identify actual writers, readers, existing shapes, partial-failure
   behavior and recovery.
4. Define behavior boundaries, acceptance scenarios, code/test targets,
   expected RED assertions, focused and proportional regression checks,
   operational and security risks, and the requirement content that completion would publish.
5. Create or reuse test definitions before readiness review. Declare
   every active criterion and required project check as a verification obligation,
   with test identity, boundary and expected observation. Identify changed
   criteria requiring RED, applicable boundaries and exclusion reasons, author
   context, and required human decisions using IDs issued through the CLI.
   Retain each declared question beside its effective human answer.
   This creates test definitions, not executable tests before approval.
6. Record open questions, conflicts, assumptions, deferred scope and risk in
   plain language. Do not invent a CLI command or local plan format.
7. Save the plan through the CLI and read back the exact
   saved content. Obtain and record answers to declared decisions, then retain the
   review bundle. Stop for independent review and human implementation
   approval; do not change tests or product code in this pass.

Report the Epic ID, saved plan and proposed content, material decisions, sources and the
next phase. If saving or reading back is unsupported, surface that tooling gap.
