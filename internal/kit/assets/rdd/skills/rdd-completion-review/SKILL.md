---
name: rdd-completion-review
description: Review implemented Epic work and present its exact approved proposal for human completion approval and atomic requirement publication.
---

# Review and complete the Epic

Read `PROCESS.md`, the Epic's approved plan and exact proposed content, the
still-published requirements, ownership, implementation, traces and evidence.

1. Inspect each approved requirement and its actual evidence. Run the owning
   checks with registration before execution and finalized evidence afterward;
   record outcomes and limits before obtaining the implementation review.
   Explicitly authorized deletion
   can leave affected paths broken; disclose those failures without restoring
   deleted machinery or claiming unproven behavior works.
2. Fetch and retain the authoritative review bundle after evidence is finalized.
   Supply it to a review context independent of the author. Record that exact
   implementation review with coverage of every criterion and obligation and
   confirmation whether all approved scope is implemented. Context labels attest
   separation; they do not prove different people reviewed the work.
   Resolve and explicitly record resolutions for material correctness,
   security, data-loss, contract, traceability and testability findings. Keep
   source, test, code-revision and PR links as evidence, not approval tokens.
   If reviewed content or evidence changes, renew the independent review before
   submission. Do not silently replace the bundle attached to an earlier review.
3. Run the `completion-review` Epic gate check and inspect its recorded result
   and blockers. Resolve blockers before submitting completed implementation
   for review. Read back
   IN_REVIEW while the
   approved proposal, ownership and prior published requirements stay intact.
4. Run the `completion` Epic gate check while the Epic is IN_REVIEW; resolve
   blockers before asking for completion approval. Fetch and retain the
   authoritative completion presentation before asking.
   Present its exact approved proposal, current published content, evidence,
   findings, risks, assumptions and any unfinished scope to the human. Obtain
   the separate explicit completion answer; never infer it from implementation
   approval, an autopilot grant, tests, code review or repository state.
5. Apply only the human's actual approval with the unchanged retained
   presentation through the CLI. If it changed, present current
   content and obtain a new answer; never refresh it silently to reuse consent.
   Read back the atomic publication of approved content, one revision increment
   per requirement, immutable accepted versions with criteria and trace meaning,
   attributable Epic/requirement audit and released ownership. A failed action
   must leave all effects unchanged; a repeated completion must not republish.
   If the human answers not yet, record that answer with the unchanged
   retained presentation as a return to implementation through the CLI. Read
   back the Epic and its owned requirements IN_PROGRESS with the plan, the
   implementation approval and the ownership unchanged, continue the build,
   and renew whatever changed before submitting for review again.
6. After that publication is read back, use the CLI to mark each backlog, gap
   or tooling record routed to the Epic as closed by the Epic, and read each
   record back. If the human wants a record left open, leave it open and add
   a note saying why.

Git and PR/CI own code integration. Never merge a PR without the user's explicit
approval for that PR. Deployment is a separate human act. If a required process
capability is missing or the result cannot be read back, report the incomplete
work and tooling gap instead of claiming DONE.
