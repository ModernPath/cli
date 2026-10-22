---
name: rdd-autopilot
description: Run a bounded implementation sprint under an explicit human autopilot grant. The grant may authorize implementation within saved Epic plans but never completion, merge, deployment, or release.
---

# Run an autopilot sprint

Read `PROCESS.md`, confirm the grant through the CLI, and verify its Epic scope, end date and excluded areas. Stop if the
grant is missing, expired or ambiguous.

1. Confirm each selected Epic and its current saved plan. Do not expand scope.
2. Review sources and plan changes; record ordinary reversible assumptions
   for later confirmation. Unless already explicitly authorized, stop for
   hard-to-reverse decisions: existing-data
   deletion or migration, security or authorization, billing or legal terms,
   or an external-party contract.
3. Follow `rdd-build` and `rdd-verify` for every slice: register before execution,
   finalize RED before registering GREEN, declare the candidate before GREEN,
   finalize actual results and read back. Use the authorized registration account;
   the grant does not confer another account's authority. Work red-first in
   bounded slices. Each new behavior gets an expected
   failing assertion before implementation, then focused and proportional
   regression checks after cleanup. Keep security, tenant isolation and data
   integrity from the first functioning slice.
4. Use focused checks during the sprint and reserve required full verification
   and hardening for closeout. Do not skip project commit checks.
5. At closeout, finish evidence collection, then follow `rdd-completion-review`
   to retain and record the independent implementation review, resolve findings,
   submit and retain the human completion presentation. Audit its
   Epic/requirement/code/test traces. Record tested code revisions through
   version control and present assumptions, findings, evidence and unfinished
   scope. Keep published requirement content and ownership unchanged in review.
6. Obtain explicit human completion approval, then read back atomic requirement
   publication, revision increments, immutable versions, audit and released
   ownership. The grant is never that answer.
   Merge, deployment and release remain human acts.

Record skipped checks, unresolved findings, assumptions and impediments in the
CLI when it supports them. If it does not, report the
gap; never invent a local record format or bypass a refusal.
