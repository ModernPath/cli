---
name: rdd-build
description: Implement behavior within a human-approved Epic plan, using red-first development and proportional cleanup and regression checks.
---

# Build approved behavior

For a small change selected by `rdd-start`, follow `PROCESS.md` §Small changes.
Read or create the requirements, criteria and scoped source/test Traces. Run the
expected failing assertion before implementation, then record current outcomes.

TestCase title and revision changes do not invalidate existing criterion proof.

For Epic work, read `PROCESS.md` and verify through the CLI that the current
saved plan and proposed content has human implementation approval. Confirm the Epic and
requirement IDs and remain inside approved scope. Before the first change,
read the planning contexts of the Epic's Release and Initiative again
(`epics get <id> --fields planning_contexts`) and build within them; a question
they leave open goes to the human. Before building, read the changes since
approval with `epics gates presentation <id> --json` (`changes_since_approval`):
a team member may have opened one of those planning contexts for change and
edited it since the approval. Tell the approver about a change the plan did not
know; any authorized workspace member returns the Epic to planning, and only if the change
needs a new plan.
Read the plan review notes the
human left to the build, recorded as finding resolutions, and apply them.
The active approved plan and
owned requirement content are read-only during implementation and completion
review. For a requested scope or acceptance change, follow `PROCESS.md`
§Returning to planning with the human's explicit decision before revising the
draft. Fresh review and implementation approval are required before resuming;
red-first development remains required.

For an approved existing-behavior Epic, confirm its explicit verification mode,
reason and current implementation links, then follow `rdd-verify` without changing
product or test code or fabricating RED. New behavior or test changes require
ordinary implementation and an explicit return to planning for active work.

For each changed behavior in ordinary implementation:

1. Write or select a focused assertion and run it against current code. Confirm
   the expected failure; a compile error or unrelated failure is not RED evidence.
2. Implement the smallest passing change. Run focused checks and proportional
   boundary and regression checks.
3. Record each actual outcome for the criterion and TestCase as current direct
   proof. After a delegated build, run
   `modernpath epics build record <id> --file <handoff>` with `--dry-run`
   first, then without it. It records the build's code and test-file traces,
   its outcomes, read from the test result files, and its assumed defaults in
   one write. Otherwise use `modernpath evidence record`. Then read back
   CriterionTestResults and Traces. A later outcome replaces the previous
   outcome for that pair.
4. Clean up, rerun affected checks and record current outcomes. Keep commands
   and output in PR notes; do not upload procedures or raw reports.

A delegated build agent puts each TestCase's full UUID in its test's name or in
a comment just above the test (for a Go test function, in a comment above it),
or lists the test names per TestCase in the build handoff's `tests`. It ends by
writing the build handoff: the result files it produced, the paths that
implement each requirement and its assumed defaults. A criterion it built only
in part goes in the build handoff's `excluded` with the reason, a TestCase it
reran for a criterion of a published requirement that no Epic owns goes in
`regression`, and a file a requirement uses but the build did not change goes
in `unchanged_paths`. It never writes to the store.

Build on the main revision the Epic started from and rebase once, before the
implementation review (`PROCESS.md` §Returning to planning). Never disable or
weaken tests to get past a project check. Keep security,
authorization, tenant isolation and atomic data integrity in the first
functioning slice. Do not claim a change is verified from code inspection
alone. Hand complete evidence to `rdd-verify` or, when explicitly requested,
report the bounded pass and remaining work.

Modernization runs are separate historical captures made after execution with
`modernpath modernization record` under an authorized Modernization Release.
Their ModernizationTestResults do not replace, satisfy or invalidate current
criterion proof or ordinary readiness.

## Standalone small changes

For new standalone completion, retain one applicable same-System
`user_requirement_id`, `scenario_id` and `supporting_system_requirement_ids`,
or one nonblank `no_user_requirement_reason`. Verify the actual typed
relationships and current scenario and changed-SR proof. The no-UR reason
never waives criterion proof. Use focused RED/GREEN and proportional regressions.
Explicit human completion remains separate from implementation and evidence.

For published unowned content, use `requirements proposal save`, then discover
canonical IDs with selector-free `requirements proposal get`. After a stale
mutation, read the current proposal and reconcile before retrying. Select its
exact `proposal_id` and `proposal_generation` on each evidence operation and
approved DONE update. Retained accepted criteria reuse current proof read-only.
Ordinary reads show accepted content while the proposal is open. Cancellation
preserves it. Active Epic work stays on its approved Epic path.
After publication, use `requirements get <UUID> --kind user|system --version
<VERSION-UUID>` to inspect the immutable accepted snapshot. Missing historical
versions remain unavailable. Use `traces remove` only for a named outdated
implementation link with a valid replacement and an attributed reason.
