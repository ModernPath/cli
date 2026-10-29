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

1. Inspect each approved requirement and its actual evidence. Run the owning
   checks and record current criterion outcomes and limits before obtaining the
   implementation review.
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
   findings, risks and any unfinished scope to the human. Also present the
   changes since approval and the assumed defaults not yet confirmed, which
   block nothing: the presentation lists them as `changes_since_approval` and
   `assumed_defaults`. Obtain
   the separate explicit completion answer; never infer it from implementation
   approval, an autopilot grant, tests, code review or repository state.
   For a round, submit with `modernpath initiative submit-review`, fetch
   `modernpath initiative presentation <initiative-id> --epics <ids>`, present
   every Epic in the round, and apply the human's one answer with
   `modernpath initiative approve-completion`. A round submission needs the
   caller to be each Epic's implementation approver or its assignee; any one
   person may approve a round's completion, which writes each Epic's own completion records, all
   or nothing. A refusal changes no Epic.
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
