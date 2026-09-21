---
name: rdd-completion-review
description: Audit, deliver, reconcile, and complete an IN_REVIEW requirement or epic scope. Use after implementation evidence is complete to verify the applicable delivered trace, integrate through the project's authorized path, open the human completion gate only after non-human prerequisites pass, and apply an attributable acceptance answer.
---

# Review and complete delivered work

Read the project `AGENTS.md`, canonical `PROCESS.md`
(`.modernpath/rdd/PROCESS.md` in a consuming repository), every entity
explicitly in scope, current code/tests/change, gate records, authoritative
records, and derived views.

## Procedure

1. Treat completion as unproven. Audit every scoped requirement clause,
   acceptance scenario, declared relation, gate, evidence result, and
   completion condition against direct current sources.
2. Stop for any `DERIVED` dependency, candidate link counted as authoritative,
   stale or inherited-unverified evidence, missing RED observation, material
   cold-review finding, undisclosed gap, or incomplete reconciliation.
3. If the pre-delivery audit passes, deliver through the project's authorized
   integration path while keeping awaiting entities `IN_REVIEW`.
4. Re-run or confirm evidence against the delivered revision — the revision
   the authorized integration path produced, not the branch head that fed
   it — and reconcile all authoritative records and derived views. Use
   `skills/rdd-audit/SKILL.md` to verify that delivered records, citations,
   and documents still describe the code; a finding it surfaces is a stop
   condition or routes through `rdd-triage`, never a silent correction.
5. Record completion trace `PASS` only for the exact eligible fingerprint, and
   before the human gate exists, so the gate names it as its prerequisite. A
   `STALE` member-scoped cold-review trace from an earlier round blocks an
   epic's completion until it is re-evaluated at the current fingerprint; an
   epic-scoped pass does not stand in for it. Only then make the scoped human
   completion gate `OPEN` and present its brief.
6. Do not answer the gate for the human. If the authorized human answers,
   record the real actor, role, exact scope, answer, and `USER:` source.
7. Apply `DONE` only to accepted named entities that independently satisfy the
   complete applicable predicate. The answer and its application are separate
   steps: apply member requirements before a named epic, and report an answer
   that is recorded but not yet applied as exactly that, never as completion.
   Route rejection or requested changes through `rdd-triage` to the earliest
   invalidated phase.

## Report

Report proven, contradicted, incomplete, indirect, and missing completion
facts; delivered revision; reconciliation result; gate states; applied
transitions; and remaining work.
