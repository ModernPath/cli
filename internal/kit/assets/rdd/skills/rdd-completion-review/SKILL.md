---
name: rdd-completion-review
description: Review implemented Epic work and present its exact approved proposal for human completion approval and atomic requirement publication.
---

# Review and complete the Epic

For a small change, use `PROCESS.md` §Small changes: review exact requirement
content, criteria, source/test Traces and current criterion outcomes;
obtain explicit human requirement completion and apply only a supported
requirement-level completion operation. Read back accepted versions and approval
attribution. Missing capability leaves completion blocked. The Epic gates and
transitions below apply only to Epic work.

Read `PROCESS.md`, the Epic's approved plan and exact proposed content, the
still-published requirements, ownership, implementation, traces and evidence.

1. Inspect each approved requirement and its actual evidence. Wait for a
   passing full run of the sprint pull request that contains the Epic, unless
   the Epic changes no code or tests. Keep a current passing result, without
   recording it again, when that full run ran its owning tests and they passed.
   Record an owning test's failure in a full run as the criterion's current
   outcome, and its pass after the fix or the rerun. Run the owning checks for
   every other criterion, and record those outcomes and limits before obtaining
   the implementation review. Request the implementation review after the full
   run has passed, where there is one.
   Explicitly authorized deletion
   can leave affected paths broken; disclose those failures without restoring
   deleted machinery or claiming unproven behavior works.
2. Fetch and retain the authoritative review bundle after current results are recorded.
   Supply its current facts to a review context independent of the author. Record the
   implementation review with coverage of every criterion and obligation and
   confirmation whether all approved scope is implemented. Context labels attest
   separation; they do not prove different people reviewed the work.
   Resolve and explicitly record resolutions for material correctness,
   security, data-loss, contract, traceability and testability findings. Keep
   source, test, code-revision and PR links as evidence, not approval tokens.
   If reviewed content, code or structural relationships change, renew the
   independent review before submission. A material current criterion outcome
   change in review scope outdates that review. Identical retries and modernization
   history do not affect review applicability. Keep the earlier review findings; do not attach a saved presentation.
3. Run the `completion-review` Epic gate check and inspect its recorded result
   and blockers. Resolve blockers before submitting completed implementation
   for review. Read back
   IN_REVIEW while the
   approved proposal, ownership and prior published requirements stay intact.
4. Run the `completion` Epic gate check while the Epic is IN_REVIEW; resolve
   blockers before asking for completion approval. Fetch and retain the
   authoritative completion presentation before asking.
   Present its exact approved proposal, current published content, evidence,
   findings, risks and any unfinished scope to the human.
   Show the full run's result and link, and each fix made after a full-run
   failure, beside the Epic's proof, and say so when the full run has not
   passed. The human's approval waits for a passing full run that contains the
   Epic, unless the Epic changes no code or tests; no gate checks it. Also present the
   changes since approval and the assumed defaults not yet confirmed, which
   block nothing: the presentation lists them as `changes_since_approval` and
   `assumed_defaults`. For an Epic of a Modernization Release, also present
   the three completion reads, which inform the answer and never block it:
   the presentation lists them as `completion_reads`.
   - `revisions` names the Release's reference revision, the totals of the
     baseline (the legacy tests' results captured at that revision) and each
     captured run the reads use, with its commit and when it was captured.
     It says that no run will count until the merge commit of the last pull
     request that carried the change is recorded on the Epic. It also says
     that, once that commit is recorded, only a run at the merge commit, or at
     a commit that contains it, will count for the requirements this Epic
     takes or reaches. An Epic reaches a requirement when its saved plan holds a
     verifies trace to the requirement or one of its criteria. It gives the
     Proof tab's reasons in that tab's words.
   - In place, `kept_touched` lists the kept requirements (legacy
     requirements an Initiative keeps as they are) whose files the change
     touched, each with the newest captured result of each of its replays.
   - `suite_comparison` compares, in place, each legacy test's newest
     captured result since the baseline with its result at the reference
     revision; onto a new system it compares each replay's newest captured
     result with the one captured before it.

   Obtain the separate explicit completion answer; never infer it from
   implementation approval, an autopilot grant, tests, code review or
   repository state.
   For a round, submit with `modernpath initiative submit-review`, fetch
   `modernpath initiative presentation <initiative-id> --epics <ids>`, present
   every Epic in the round, and apply the human's one answer with
   `modernpath initiative approve-completion`. Any authorized workspace member may submit a round; any one
   person may approve a round's completion, which writes each Epic's own completion records, all
   or nothing. A refusal changes no Epic.
   Epics that are not a round of one Initiative are approved one by one.
   After the merge, once the Epic's merge commit is recorded, also capture
   every test of the criteria of each kept requirement in the Release's scope
   that the completion presentation listed as touched by the change, its
   replay and break test among them. Put all the tests of one requirement in
   one run, with the runs that prove the Epic's requirements, or in runs of
   their own when the capture's limits of 500 tests and 50 criteria in one run
   require it.
5. Apply only the human's explicit current-state approval and reason through the
   CLI. Upload no plan or presentation. The server rechecks current eligible
   state under locks. Read back atomic publication, one revision increment per
   requirement, immutable accepted content/criteria/trace meaning, attributable
   approval and released ownership. A failed action leaves all effects unchanged;
   a repeated terminal completion cannot republish. Resolve an uncertain
   nonterminal response by reading current state and audit.
   If the human answers not yet, record the reason as a return to implementation.
   This needs the current authorization and IN_REVIEW lifecycle, even when
   completion readiness fails. Read back IN_PROGRESS with the same plan,
   implementation approval and ownership, continue the approved build, and renew
   changed evidence and review before submitting again.
6. After that publication is read back, use the CLI to mark each backlog, gap
   or tooling record routed to the Epic as closed by the Epic, and read each
   record back. An assumed default is never closed by the Epic: it stays
   routed until a person accepts or rejects it in their own words. If the
   human wants a record left open, leave it open and add a note saying why.

Git and PR/CI own code integration. Never merge a PR without the user's explicit
approval for that PR. Deployment is a separate human act. If a required process
capability is missing or the result cannot be read back, report the incomplete
work and tooling gap instead of claiming DONE.
