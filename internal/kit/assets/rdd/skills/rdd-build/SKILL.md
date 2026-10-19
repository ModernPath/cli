---
name: rdd-build
description: Implement behavior within a human-approved Epic plan, using red-first development and proportional cleanup and regression checks.
---

# Build approved behavior

Read `PROCESS.md` and verify through the sanctioned interface that the current
saved plan and proposed content has human implementation approval. Confirm the Epic and
requirement IDs and remain inside approved scope. The active approved plan and
owned requirement content are read-only during implementation and completion
review. Surface a requested scope or acceptance change as a human decision and
record the new work separately; do not revise the active approval in place.

For each behavior:

1. Write or select a focused assertion for the approved outcome.
2. Run it against current code and confirm the expected failure. A compile
   error or unrelated failure is not RED evidence.
3. Implement the smallest passing change.
4. Run focused checks, then boundary-appropriate regression checks.
5. Clean up without changing behavior and rerun affected checks.
6. Preserve actual commands, outputs and tested revisions; keep RED separate
   from passing results.

Never disable or weaken tests to get past a project check. Keep security,
authorization, tenant isolation and atomic data integrity in the first
functioning slice. Do not claim a change is verified from code inspection
alone. Hand complete evidence to `rdd-verify` or, when explicitly requested,
report the bounded pass and remaining work.
