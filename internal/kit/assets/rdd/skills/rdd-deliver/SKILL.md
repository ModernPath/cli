---
name: rdd-deliver
description: Drive selected requirements through the applicable delivery path. Use for end-to-end work including normal planning, entry, red-first development and completion, or existing-proof verification and acceptance of an eligible source-scoped baseline. Stops at an unmet human or external prerequisite.
---

# Deliver requirement scope end to end

Read the project `AGENTS.md`, canonical `PROCESS.md`
(`.modernpath/rdd/PROCESS.md` in a consuming repository), selected records and
sources, and each focused skill before executing its phase. `PROCESS.md` owns
all semantics; this skill owns phase order and continuation.

## Run the loop

For a source-scoped published baseline still in PENDING_VERIFICATION, take the
separate rdd-reverse-engineer-verify → rdd-reverse-engineer-accept path. Its
single reviewed human decision applies verified and delivered proof directly
to DONE. Normal planning, entry, RED/GREEN and completion below apply only after
an explicitly scoped handoff for missing tests or changed behavior. Reopened
requirements remain normal development work.

1. Reconcile authoritative state, answered gates, evidence validity, and
   projections. Fix state disagreement before selecting work.
2. Freeze the selected scope under `PROCESS.md` and find its earliest unmet
   prerequisite. Never start from the most convenient phase.
3. If input is not authoritative or is `DERIVED`, apply `rdd-discover` and its
   confirmation gate. Continue only with confirmed requirements and relations.
4. For active work lacking current entry approval, apply `rdd-plan`, then
   `rdd-cold-review`, then `rdd-entry-review` until entry is applied to those
   items as `TODO`. Resume already-entered work at its current phase; unchanged
   DONE dependencies do not repeat entry. Entry approval must cover the whole
   selected delivery scope, including the Epic when selected, before step 5.
5. Run the AI TDD inner loop below. Apply `rdd-build` to changed SRs and
   `rdd-verify` to entered verification work establishing regression-sensitive
   evidence for existing behavior. Continue until every selected requirement
   has a current passing trace and is `IN_REVIEW` or already `DONE`. Keep
   unchanged DONE members as proof dependencies, without reaccepting them.
6. Apply `rdd-completion-review` to audit, deliver, recheck the delivered
   revision, reconcile records, run the completion trace gate, and apply the
   human completion answer.
7. Apply `rdd-triage` whenever a discovery, contradiction, requested change,
   or invalidation alters the selected trace. Resume at the earliest phase it
   invalidates.

At an exact `OPEN` human gate, present its brief and linked list of relevant
working-set files before asking and wait, following `PROCESS.md` §Gates. If an
attributable answer is already available, apply it, report, and wait for the
human to resume. Never infer or supply the answer.

## Run the AI TDD inner loop

When the human resumes after applied entry approval, iterate without further
human input while the approved scope remains unchanged:

1. Evaluate every selected UR upper trace and SR lower trace. Establish any
   required initial RED observations.
2. Select the next unmet approved SR clause. Apply `rdd-build` or `rdd-verify`
   until its lower trace is current and passing.
3. Rerun affected UR scenarios and update their separate upper evidence.
4. Repeat for any failing or stale approved trace. Do not stop after the first
   GREEN result or completed SR while another selected trace remains unmet.
5. Move eligible active requirements to `IN_REVIEW` only after all applicable
   trace gates pass. Previously DONE members retain their state when their
   proof is current; invalidated proof returns through `rdd-triage`.

If all planned SR lower traces pass while a UR upper trace still fails, diagnose
the mismatch. Continue the loop for an implementation defect within approved
scope. Apply `rdd-triage` and return to planning for a missing or changed
requirement, relation, acceptance rule, or material decision. Record an exact
external blocker when progress cannot continue.

## Continue honestly

- Treat a focused skill's exit as a handoff, not completion of this skill.
- A human gate answer inside the loop is applied and reported; the loop
  resumes on the human's word, not on the answer itself.
- Do not bypass a failed trace gate, a `DERIVED` hold, candidate relation,
  material finding, stale evidence, missing delivery, or failed reconciliation.
- Keep unchanged approved items at their strongest supported state; re-enter
  only the scope invalidated by changed inputs.
- Report `DONE` or `OBSOLETE` as terminal outcomes. Report an open human gate,
  `BLOCKED`, `DEFERRED`, `TODO`, or `IN_REVIEW` item as incomplete with its
  exact resume condition.

## Report

Report the selected scope and fingerprint, completed phases, current lifecycle
states, trace and human gates, evidence and delivered revision, discoveries,
and either the terminal result or the exact next phase and prerequisite.
