---
name: rdd-completion-review
description: Review implemented Epic work and present its exact approved proposal for human completion approval and atomic requirement publication.
---

# Review and complete the Epic

Read `PROCESS.md`, the Epic's approved plan and exact proposed content, the
still-published requirements, ownership, implementation, traces and evidence.

1. Inspect each approved requirement and its actual evidence. Run the owning
   checks and record their outcomes and limits. Explicitly authorized deletion
   can leave affected paths broken; disclose those failures without restoring
   deleted machinery or claiming unproven behavior works.
2. Obtain code review independent of the author. Resolve material correctness,
   security, data-loss, contract, traceability and testability findings. Keep
   source, test, code-revision and PR links as evidence, not approval tokens.
3. Submit completed implementation for review. Read back IN_REVIEW while the
   approved proposal, ownership and prior published requirements stay intact.
4. Present the exact approved proposal, current published content, evidence,
   findings, risks, assumptions and any unfinished scope to the human. Obtain
   the separate explicit completion answer; never infer it from implementation
   approval, an autopilot grant, tests, code review or repository state.
5. Apply only the human's actual approval through the sanctioned interface.
   Read back the atomic publication of approved content, one revision increment
   per requirement, immutable accepted versions with criteria and trace meaning,
   attributable Epic/requirement audit and released ownership. A failed action
   must leave all effects unchanged; a repeated completion must not republish.

Git and PR/CI own code integration. Never merge a PR without the user's explicit
approval for that PR. Deployment is a separate human act. If a required process
capability is missing or the result cannot be read back, report the incomplete
work and tooling gap instead of claiming DONE.
