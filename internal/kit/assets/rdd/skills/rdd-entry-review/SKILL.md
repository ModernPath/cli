---
name: rdd-entry-review
description: Evaluate and apply the strict implementation-entry gate for an epic, UR, or SR. Use after planning and cold review are complete to audit entry prerequisites, open the human gate only after the entry trace passes, present the decision brief, and apply an attributable human answer. Never substitutes technical review or agent judgment for human approval.
---

# Review implementation entry

Read the project `AGENTS.md`, canonical `PROCESS.md`
(`.modernpath/rdd/PROCESS.md` in a consuming repository), selected planning
packet, cold-review findings, current gate records, and relevant sources.

## Procedure

1. Audit every selected entity against the Entry packet, Strict human
   transitions, and gate-state rules in `PROCESS.md`.
2. Fail the entry trace for missing or conflicting sources, `DERIVED` items,
   candidate links counted as authoritative, ambiguous acceptance, broad SRs,
   stale reconnaissance, incomplete implementation context, inadequate RED
   strategy, or unresolved material cold-review findings.
3. Record the entry trace gate against the exact content fingerprint. Keep the
   human gate `DRAFT` when the trace does not pass.
4. After a current trace `PASS`, make only the exact scoped human gate `OPEN`
   and present its brief and recommendation in **plain product language** — see
   `PROCESS.md` §Gates: self-contained for a reader who has not seen the packet,
   with every referenced decision, correction, or finding stated by its
   substance and not its code. Before presenting, reread the brief as that
   reader and expand any bare identifier or jargon.
5. Do not answer the gate for the human. If the authorized human answers,
   record the real actor, role, scope, answer, and `USER:` source; apply only
   named transitions and reconcile all affected records.
6. Move approved named `PROPOSED` or `PENDING_VERIFICATION` requirements and
   any named proposed epic to `TODO`. Otherwise retain the strongest honest
   state and route requested changes.

## Report

Report the entry-trace verdict, exact human-gate state, applied transitions,
remaining blockers, and the exact handoff: `rdd-build`, `rdd-verify`, or the
earliest planning pass that must be repeated.
