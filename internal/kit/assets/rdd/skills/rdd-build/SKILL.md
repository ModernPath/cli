---
name: rdd-build
description: Execute the AI-owned TDD loop for one approved system requirement from TODO through current lower evidence and IN_REVIEW. Use for new or changed SR behavior after human entry approval. Repeat RED, GREEN, cleanup, lower verification, and separate affected-UR upper validation until the approved trace needs pass or an exact replanning condition is found. Use rdd-verify instead for confirmed as-built PENDING_VERIFICATION behavior.
---

# Build one SR slice

Read the project `AGENTS.md`, canonical `PROCESS.md`
(`.modernpath/rdd/PROCESS.md` in a consuming repository), selected SR,
applicable UR acceptance content, optional epic, technical reconnaissance,
code, tests, and current records.

## Procedure

1. Orient and select exactly one approved `TODO` or `IN_PROGRESS` SR with
   current planning and no active hold. Work on a reviewable feature branch and
   preserve unrelated changes.
2. Before its first implementation iteration, establish every selected affected
   UR's upper RED for the expected reason. Keep that evidence on the UR.
3. Select one unmet approved SR clause, establish its focused lower RED for the
   expected reason, and link the stable test identity to the clause.
4. Implement the smallest behavior that makes the focused evidence pass.
   Capture discoveries instead of silently expanding scope.
5. Perform requirement-scoped cleanup or record a no-op. Return to RED if the
   cleanup exposes a correctness change.
6. Run focused and proportional post-cleanup gates and record current SR lower
   evidence. Separately rerun affected UR scenarios and update their upper
   evidence, including live-browser and screenshot evidence for UI behavior.
7. Re-evaluate the selected SR clauses and affected UR scenarios. Repeat from
   step 3 while an unmet result is caused by approved behavior in this SR.
8. Move the SR to `IN_REVIEW` when its lower trace passes. An affected UR moves
   to `IN_REVIEW` only when its upper trace passes and every required SR is
   `IN_REVIEW` or `DONE`.
9. Reconcile the affected graph and derived views. Return remaining approved
   trace failures to `rdd-deliver` for another AI iteration. Hand fully eligible
   `IN_REVIEW` scope to `rdd-completion-review`; do not deliver or solicit
   completion here.

Do not ask for human input inside the loop. Return to planning only when drift
creates a new product, scope, architecture, acceptance, priority, release,
workflow, or material technical decision. Record an external blocker exactly.

## Report

Report the planning revision, RED and passing observations, code and test
references, cleanup, final gates, status changes, discoveries, gaps, and the
exact next skill or hold.
