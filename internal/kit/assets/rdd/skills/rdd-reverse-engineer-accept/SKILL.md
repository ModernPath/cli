---
name: rdd-reverse-engineer-accept
description: Present verified existing behavior for explicit human acceptance and apply an atomic PROPOSED-to-DONE transition through a supported server operation. Use after reverse-engineering verification, without inventing implementation work.
---

# Accept verified existing behavior

Use the results of rdd-reverse-engineer-verify. Read the project's instructions,
current process and sanctioned CLI reference. This path accepts an accurate,
verified description of existing behavior; it does not approve a requested code
change or require an implementation Epic merely to manufacture a lifecycle path.

1. Read the exact native requirement set, current content and acceptance
   criteria, implementation/test traces and persisted execution evidence.
   Targets must remain PROPOSED and have complete applicable passing evidence.
   Leave already DONE requirements unchanged. Requirements owned by active
   implementation work continue through that work's completion process.
2. Present the requirements becoming DONE, what the assertions establish,
   actual results, tested code revisions, exclusions and remaining risks to the
   human. Obtain an explicit attributable acceptance for this exact scope unless
   that acceptance has already been supplied for the same reviewed content and
   evidence. Approval to record requirements or run tests is not acceptance.
   Rejection or missing proof leaves the targets PROPOSED.
3. Apply the human decision only through a documented, supported server
   operation for accepting verified existing requirements. The operation must
   recheck current scope, content, evidence, lifecycle, tenant/System access and
   ownership, then atomically publish the accepted requirement versions, move
   exactly the approved targets from PROPOSED to DONE and retain the evidence
   links and attributed acceptance reason. Failure must leave the scope
   unchanged. Repeating an applied acceptance must not publish another version.
4. Read back the actual accepted versions, DONE states, evidence links and
   actor/reason audit. If an operation's outcome is uncertain, read its stored
   result before retrying. Changed content or evidence needs verification and a
   human decision over the changed scope; do not reuse approval for other work.

Current capability must be verified, not assumed from this skill. If the CLI or
server lacks this acceptance operation, report that tooling gap and leave the
requirements PROPOSED. Never substitute a generic status update, call the gateway
directly, edit process state, or claim that the skill itself implements the
transition. Do not restore captures, fingerprints, generic gates or the
DERIVED/PENDING_VERIFICATION states. Code integration, merge, deployment and
release retain their separate authority.
