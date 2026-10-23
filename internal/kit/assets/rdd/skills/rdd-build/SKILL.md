---
name: rdd-build
description: Implement behavior within a human-approved Epic plan, using red-first development and proportional cleanup and regression checks.
---

# Build approved behavior

For a small change selected by `rdd-start`, follow `PROCESS.md` §Small changes
and its RED/GREEN loop; the Epic registration steps below do not apply.

For Epic work, read `PROCESS.md` and verify through the CLI that the current
saved plan and proposed content has human implementation approval. Confirm the Epic and
requirement IDs and remain inside approved scope. The active approved plan and
owned requirement content are read-only during implementation and completion
review. For a requested scope or acceptance change, follow `PROCESS.md`
§Returning to planning with the human's explicit decision before revising the
draft. Fresh review and implementation approval are required before resuming;
returning to planning waives no RED or GREEN requirement.

For each behavior:

1. Write or select a focused assertion for the approved outcome.
2. Register the RED execution with obligations, revision, command and environment
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
6. Preserve actual commands, outputs and tested revisions; keep RED separate
   from passing results.

Never disable or weaken tests to get past a project check. Keep security,
authorization, tenant isolation and atomic data integrity in the first
functioning slice. Do not claim a change is verified from code inspection
alone. Hand complete evidence to `rdd-verify` or, when explicitly requested,
report the bounded pass and remaining work.
