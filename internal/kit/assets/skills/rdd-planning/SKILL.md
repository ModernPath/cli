---
name: rdd-planning
description: Planning passes for requirement-driven development — seed a context's requirements ledger from its design doc, run a backlog triage and replan, or route customer feedback into the right place. Use between build slices, when the backlog fills up, when a design doc changes, or when asked to plan, triage, groom or seed requirements.
---

# Planning

<!-- TOOL-OWNED. Installed by `modernpath install`. -->

Planning passes never write implementation code. Read `.claude/rdd/PROCESS.md`
§3 and §5 first.

---

## Seed a ledger (once per bounded context)

Read the context's design doc in full, plus the context map and the data/event
contracts.

For every invariant, business rule, command, event and read model in the doc,
create a `REQ-<CTX>-NNN` requirement with:

- a one-line statement,
- GIVEN/WHEN/THEN acceptance criteria (or a link to the spec that holds them),
- source rule ids and doc sections,
- stage (MVP/Later) and priority (must/should/could),
- status READY if the criteria are unambiguous, else PROPOSED.

Where the doc is ambiguous, mark the requirement BLOCKED and add the question to
`process/08-open-questions.md`. Where something is consciously out of scope, mark
it DEFERRED with a reason.

Write `tasks/<CTX>-REQUIREMENTS.md` in the format the `rdd-ledger` skill defines,
and add the context row to `tasks/README.md` and `PROGRESS.md`.

Do not write implementation code. Do not resolve open design decisions yourself —
record them as open questions; a decision needs a `USER:` source.

**Report:** status counts, the requirement list, and any open questions raised.

---

## Triage and replan (between slices)

Run at natural boundaries: session start, when `BACKLOG.md` passes about five
items, at the end of a slice, and whenever a design doc changes.

1. **Sweep `BACKLOG.md`.** Route each item, then remove it from the inbox:
   - clear owning context → a PROPOSED requirement with `Raised-by:` provenance;
   - design ambiguity → `process/08-open-questions.md`, plus a BLOCKED
     placeholder if it blocks work;
   - market or scope gap → `process/gap-register.md`, plus a DEFERRED
     requirement if it is a known future need;
   - not worth doing → drop it with a one-line reason in the routing log.
2. **Reconcile docs ↔ ledgers ↔ epics.** For any design doc changed since the
   last pass: derive PROPOSED requirements for new rules, flag affected
   requirements for re-review, mark removed ones OBSOLETE. **Verify** that
   completed and active epics are reflected in the ledgers — the reconciliation
   rule should have kept them in sync, so any drift you find is a process defect
   worth noting, not just fixing.
3. **Re-prioritize and promote.** Set stage and priority; promote the next
   slice's requirements PROPOSED → READY by sharpening acceptance criteria.
   Don't guess on ambiguity — raise an open question and leave it BLOCKED.
4. **Record.** Append a dated routing-log entry to `BACKLOG.md` and refresh
   `PROGRESS.md`.

**Report:** items routed and where, requirements added/re-prioritized/retired,
the new READY queue per context, any drift found, new questions or gaps.

---

## Route customer feedback

For a bug report, feature request or change request:

1. **Bug in existing functionality** — identify the owning context and related
   requirements; create a PROPOSED requirement for the fix with
   `Raised-by: customer-feedback-YYYY-MM-DD`; link the requirement it fixes.
2. **New feature request** — decide whether it fits an existing context or needs
   a new one; check for conflicts with existing docs and invariants. Clear scope
   → PROPOSED requirements with criteria. Ambiguous → an open question plus a
   BLOCKED placeholder.
3. **Design change request** — identify the affected docs, draft the changes
   without applying them, list every requirement needing re-review, and flag it
   for a human decision.

**Report:** how it was routed, requirements created, questions raised, and the
recommended next step.
