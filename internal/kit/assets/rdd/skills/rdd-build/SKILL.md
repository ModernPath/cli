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
know; only the approver returns the Epic to planning, and only if the change
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
   proof with `modernpath evidence record`, then read back CriterionTestResults
   and Traces. A later outcome replaces the previous outcome for that pair.
4. Clean up, rerun affected checks and record current outcomes. Keep commands
   and output in PR notes; do not upload procedures or raw reports.

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
