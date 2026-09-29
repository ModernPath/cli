---
name: rdd-build
description: Implement one approved SR from TODO through current lower evidence and IN_REVIEW using RED, GREEN, cleanup and regression checks. Use after normal development entry for new or changed behavior. Use rdd-verify for entered verification work and rdd-reverse-engineer-verify for eligible source-scoped baselines with existing proof.
---

# Build one SR slice

Read the project `AGENTS.md`, canonical `PROCESS.md`
(`.modernpath/rdd/PROCESS.md` in a consuming repository), selected SR,
applicable UR acceptance content, optional epic, technical reconnaissance,
code, tests, and current records.

## Procedure

1. Check that current applied normal entry approval covers the whole selected
   delivery scope, including the Epic when selected, under `PROCESS.md`.
   Then select exactly one `TODO` or `IN_PROGRESS` SR with current planning
   and no active hold. A reopened baseline without that approval needs
   planning and entry first. Work on a
   reviewable feature branch and preserve unrelated changes.
2. Before its first implementation iteration, establish upper RED for selected
   UR scenarios requiring new evidence. Re-validation of previously proven
   unchanged scenarios needs no new RED. Keep upper evidence on the UR.
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

The project's commit gate binds the GREEN, cleanup, and reconciliation
commits. A RED waypoint may legitimately fail the very suite the gate runs —
its targeted failing test is its gate, and committing it before the change
that satisfies it is what makes red-first auditable in history. Record the
RED evidence while the repository stands at the RED commit, so the evidence
is pinned to the revision that produced it.

## Report

Report the planning revision, RED and passing observations, code and test
references, cleanup, final gates, status changes, discoveries, gaps, and the
exact next skill or hold.
