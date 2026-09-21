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
3. Record the entry trace gate against the exact content fingerprint, before
   the human gate exists, so the gate names it as its prerequisite; a human
   gate opened without a named passing trace cannot be answered. Keep the
   human gate `DRAFT` when the trace does not pass.
4. After a current trace `PASS`, make only the exact scoped human gate `OPEN`
   and present its brief and recommendation in **plain product language** — see
   `PROCESS.md` §Gates: self-contained for a reader who has not seen the packet,
   with every referenced decision, correction, or finding stated by its
   substance and not its code. Before presenting, reread the brief as that
   reader and expand any bare identifier or jargon.
5. Do not answer the gate for the human. If the authorized human answers,
   record the real actor, role, scope, answer, and `USER:` source; apply only
   named transitions and reconcile all affected records. The answer and its
   application are two steps: apply member requirements before a named epic,
   and report an answer that is recorded but not yet applied as exactly that.
6. Move approved named `PROPOSED` or `PENDING_VERIFICATION` requirements and
   any named proposed epic to `TODO`. Otherwise retain the strongest honest
   state and route requested changes.
7. Report and stop. The entry pass ends with the applied transitions, not
   with the first build step: the answer was a decision about the gate, not
   an instruction to build (`PROCESS.md` §Gates). The build begins with a
   fresh session entry when the human asks for it.

## Report

Report the entry-trace verdict, exact human-gate state, applied transitions,
remaining blockers, and the exact handoff: `rdd-build`, `rdd-verify`, or the
earliest planning pass that must be repeated — named as the next pass for the
human to open, not entered by this one.
