---
name: rdd-triage
description: Route discoveries, defects, blockers, conflicts and deferred work into the selected Epic plan or a separately authorized scope.
---

# Triage a discovery

Read `PROCESS.md`, the selected Epic and its current saved plan, plus relevant
source, code and test evidence. Preserve native IDs.

1. Classify the discovery: behavior within approved scope, changed acceptance
   or scope, implementation defect, external blocker, contradictory source,
   or future work outside the Epic.
2. Continue red-first work for a defect within approved behavior.
3. Route a material change in user outcome, acceptance, architecture or scope
   back to `rdd-plan`; affected implementation waits for a revised saved plan
   and renewed human approval.
4. Ask the human to resolve intent that sources cannot establish. Record the
   decision through the sanctioned interface before relying on it.
5. Disclose blockers, assumptions and deferred work. Do not silently add
   another Epic, invent a local record, or declare a process-tool refusal
   cleared.

Report evidence, affected native items, the earliest needed phase and what
remains incomplete.
