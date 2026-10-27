---
name: rdd-build
description: Implement behavior within a human-approved Epic plan, using red-first development and proportional cleanup and regression checks.
---

# Build approved behavior

For a small change selected by `rdd-start`, follow `PROCESS.md` §Small changes
and its RED/GREEN loop. First read or create the requirements, criteria and
scoped source/test Traces. Reserve a test run before each execution, finalize
actual outcomes and criterion/TestCase mappings, and read back RED before
reserving GREEN. The Epic candidate/obligation registration steps below do not
apply; test evidence recording and read-back still do.

For Epic work, read `PROCESS.md` and verify through the CLI that the current
saved plan and proposed content has human implementation approval. Confirm the Epic and
requirement IDs and remain inside approved scope. Read the plan review notes the
human left to the build, recorded as finding resolutions, and apply them.
Change a test title before that test's first recorded result: a changed title
advances the test's revision and invalidates evidence already recorded for it.
The active approved plan and
owned requirement content are read-only during implementation and completion
review. For a requested scope or acceptance change, follow `PROCESS.md`
§Returning to planning with the human's explicit decision before revising the
draft. Fresh review and implementation approval are required before resuming;
returning to planning waives no RED or GREEN requirement.

For an approved existing-behavior Epic, confirm its explicit verification mode,
reason and current implementation links, then follow `rdd-verify` without changing
product or test code or fabricating RED. New behavior or test changes require
ordinary implementation and an explicit return to planning for active work.

For each changed behavior in ordinary implementation:

1. Write or select a focused assertion for the approved outcome.
2. Register the RED execution with obligations and tested code revision
   through the authorized account, then run it against current code and confirm
   the expected failure. A compile
   error or unrelated failure is not RED evidence.
3. Finalize and read back actual RED evidence before registering GREEN.
   Implement the smallest passing change.
4. Declare the candidate before GREEN. Register each execution before running
   focused checks and boundary-appropriate regression checks, then finalize its
   returned identity with actual evidence and read it back.
5. Clean up without changing behavior and rerun affected checks with fresh
   registrations. Update a changed candidate and obtain fresh passing evidence.
6. Inspect actual execution and record test/run identities, executed test
   revision, outcomes and links. Commands and output are local diagnostics and
   PR check notes; upload no procedure, role, runner, environment or raw report.
   Keep actual failures separate from passing executions.

Build on the main revision the Epic started from and rebase once, before the
implementation review (`PROCESS.md` §Returning to planning). Never disable or
weaken tests to get past a project check. Keep security,
authorization, tenant isolation and atomic data integrity in the first
functioning slice. Do not claim a change is verified from code inspection
alone. Hand complete evidence to `rdd-verify` or, when explicitly requested,
report the bounded pass and remaining work.
