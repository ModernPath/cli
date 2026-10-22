---
name: rdd-triage
description: Route discoveries, defects, blockers, conflicts and deferred work into the selected Epic plan or a separately authorized scope.
---

# Triage a discovery

Read `PROCESS.md`, the selected Epic and its current saved plan, plus relevant
source, code and test evidence. Preserve IDs.

1. Classify the discovery: behavior within approved scope, changed acceptance
   or scope, implementation defect, external blocker, contradictory source,
   or future work outside the Epic.
2. Continue red-first work for a defect within approved behavior.
3. For a material change in user outcome, acceptance, architecture or scope,
   revise the saved plan through `rdd-plan` only while the Epic is `PROPOSED`;
   implementation waits for human approval. Once implementation is approved,
   keep the active plan and owned requirements read-only, surface the change
   as a human decision and record new work separately for its own plan and
   human implementation approval. Continue unaffected work within the active
   approved scope. When a saved plan delivers a backlog, gap or tooling
   record, use the CLI to mark the record as routed to that Epic, with a
   reference to the plan save, and read it back. Listing the record in the
   plan body does not route it.
4. Ask the human to resolve intent that sources cannot establish. Record the
   decision through the CLI before relying on it.
5. Disclose blockers, assumptions and deferred work. Do not silently add
   another Epic, invent a local record, or declare a process-tool refusal
   cleared.

Report evidence, affected items, the earliest needed phase and what
remains incomplete.
