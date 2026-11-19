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
   revise the saved plan through `rdd-plan` only while the Epic is `PROPOSED`.
   If it already has a plan review, present the discovery with that review and
   wait for the human to direct a revision; do not restart review automatically.
   Implementation waits for human approval. Once implementation is approved,
   keep the active plan and owned requirements read-only until an explicit
   human return to planning under `PROCESS.md` is applied. Then route the
   PROPOSED revision through planning, fresh review and implementation approval.
   DONE stays closed; changed work uses a follow-up Epic. When a saved plan
   delivers a backlog, gap or tooling record, use the CLI to mark the record as
   routed to that Epic, with a reference to the plan save, and read it back.
   Listing the record in the plan body does not route it. A backlog record of
   kind `assumption` holds an assumed default. An assumed default is routed
   that way only for its first routing, to the Epic being built. Moving an
   assumed default that is already routed needs the person's own words as its
   `USER:` source.
4. Ask the human to resolve intent that sources cannot establish. Record the
   decision through the CLI before relying on it.
5. Disclose blockers, assumptions and deferred work. Do not silently add
   another Epic, invent a local record, or declare a process-tool refusal
   cleared.
6. For a full-run failure, keep the link to its output. If the project
   instructions list it as a known flake, rerun it alone three times; when all
   three pass, list it in the sprint report and rerun the failed part of the
   full run, or the whole run where it cannot be rerun in parts. The full run
   has not passed until that rerun passes. Otherwise the failure is a defect.
   Fix one that a sprint Epic caused red-first under that Epic, unless that
   Epic is DONE. For any other defect, check which approved Epic owns the
   failing behavior before you fix it. When an approved Epic that is not DONE
   owns it, the fix belongs to that Epic: report it as a blocker with that Epic
   as its owner, and do not fix it as a small change. When a DONE Epic caused
   it, or it also fails on the tip of the release branch and no approved Epic
   owns it, fix it red-first as a small change (`PROCESS.md` §Small changes) on
   the same pull request, and list it in the sprint report. Do not wait for the
   human, unless the fix needs a stop of `PROCESS.md` §Autopilot or, under a
   grant, is in an area the grant excludes. A fix that changes what a published
   requirement or its criteria state, or that is not narrow, is its own Epic.
   An autopilot grant does not widen an Epic's scope to cover it.

Report evidence, affected items, the earliest needed phase and what
remains incomplete.
