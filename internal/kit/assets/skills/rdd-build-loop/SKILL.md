---
name: rdd-build-loop
description: Run a requirement-driven build slice end to end — enter via the work-list, create or extend an epic, run both TDD loops red-first, trace, get approval, and reconcile the ledger. Use when implementing requirements, continuing an epic, or when asked to "run the build loop" or "do the next slice".
---

# Build loop

<!-- TOOL-OWNED. Installed by `modernpath install`. -->

The workhorse. Run it repeatedly with small batches — two or three requirements —
so every run ends at a clean stopping point.

Read `.claude/rdd/PROCESS.md` §1, §4, §5 and §7 first, plus `AGENTS.md` for the
project's stack and commands. Open `WORKLIST.md`, the target ledger
`tasks/<CTX>-REQUIREMENTS.md`, and the active epic record if one exists.

## A. Enter via the work-list — never start implementation outside it

- If `WORKLIST.md` has a READY work row, continue that epic and go to B.
- Otherwise pick the next READY requirement from the ledger (lowest stage, then
  highest priority; promote a PROPOSED one by sharpening its criteria first).
  Then apply the **spec-worthy test** in `PROCESS.md` §5:
  - **Fast lane** — the ledger row is the task record. It carries sharpened
    GIVEN/WHEN/THEN criteria, RED→GREEN evidence and code refs, and a WORKLIST
    work row referencing the REQ id.
  - **Epic** — create or extend one that passes the Epic Specification Gate:
    sourced UR and user outcome, bounded-context ownership, BDD scenarios (SCN)
    derived from the requirement's criteria, system requirements (SR), tasks
    (TASK), failing-test strategy. Every normative claim cites a
    `DOC:`/`CODE:`/`USER:` source. **Record the human spec approval before
    writing any RED test.** Add the rollup row plus work rows to `WORKLIST.md`.

Set the ledger rows to IN_PROGRESS in all three places.

## B. Run both loops, red-first

1. **Lower loop** (per TASK/SR): write the failing unit/component/API/contract
   test tagged with the TASK and REQ ids. Make the smallest change that passes.
   Annotate invariant enforcement in code with the rule id. Run the suite until
   green. Record failing→passing evidence and code refs in the epic; mark the
   row LOWER_VERIFIED; update `WORKLIST.md`.
2. **Upper loop** (per SCN): write the failing BDD/E2E/user-flow check first and
   drive it green through lower-loop slices. Record failing→passing evidence;
   mark the SCN UPPER_VALIDATED. For UI slices this includes a real-browser run
   and an inspected screenshot (`PROCESS.md` §5).
3. **Trace**: link code and tests in the ledger row; status → IN_REVIEW in all
   three places once SCNs are UPPER_VALIDATED and SRs LOWER_VERIFIED.
4. **Approval**: request human approval; record it with a `USER:<date>` source.
   Only then does the epic go DONE and the ledger rows follow. PR titles carry
   the REQ or TASK id.

## C. Capture and reconcile — in the same change, not later

5. Record every discovery as a PROPOSED row in the owning context's ledger with
   `Raised-by:` provenance, or a `BACKLOG.md` line if the owner is unclear.
   Decisions go in the epic's Decisions table with `USER:` sources; unsourced
   claims become open questions.
6. Update the epic record — evidence map, decisions, deferred, discovered,
   follow-ups, gate result — and the `WORKLIST.md` rollup, **and** reconcile the
   mapped ledger rows and `PROGRESS.md`.
7. **Deviation audit**: grep the touched code for `TODO`, `FIXME`, `DEVIATION`.
   For each, confirm a PROPOSED requirement exists. If not, create one now.
8. **Status hygiene check**: verify the dashboard rows match the `Totals:` line.

   ```bash
   grep -oE "\| (DONE|IN_REVIEW|IN_PROGRESS|READY|PROPOSED|DEFERRED|BLOCKED) \|" \
     tasks/<CTX>-REQUIREMENTS.md | sort | uniq -c
   grep "^Totals:" tasks/<CTX>-REQUIREMENTS.md
   ```

   If they disagree — or WORKLIST, epic and ledger disagree — fix it before
   reporting.

## Hard rules

Red before green in both loops. Nothing is DONE without passing tagged tests,
epic evidence, human approval and a reconciled ledger. Deferral is always an
explicit DEFERRED requirement; discovery is always a PROPOSED row or a
`BACKLOG.md` line — never silent. Never resolve a product, scope or architecture
decision by assumption. Don't touch another context's code or tables.

Verify by reading back what you changed, not by trusting that a command
succeeded — a green run that produced no effect looks identical to a good one.

## Report

Requirements completed, deferred or blocked (ledger and epic status);
discoveries captured and where they landed; the gate result; updated PROGRESS
counts; and the next READY work-list row.
