---
name: rdd-discover
description: Source and classify product intent before requirement planning. Use when examining interviews, customer feedback, regulations, incidents, product goals, existing behavior, or other inputs that may establish or change a UR or SR. Produces sourced facts, conflicts, open questions, and DERIVED candidates without planning implementation.
---

# Discover product intent

Read the project `AGENTS.md` and canonical `PROCESS.md`
(`.modernpath/rdd/PROCESS.md` in a consuming repository), especially Authority,
Derived requirement hold, Gates, and Discoveries/releases/conflicts.
`PROCESS.md` controls when this skill and its records disagree.

## Procedure

1. Identify the input sources and the product or bounded context being examined.
2. Inspect relevant product documents, code, tests, and current requirement
   records. Separate observed behavior, normative intent, attributable human
   decisions, conflicts, and unanswered questions.
3. Update sourced product/domain documentation, vocabulary, rules, boundaries,
   contracts, and open questions without resolving ambiguity by assumption.
4. Create a directly sourced requirement as `PROPOSED`. Route a merely inferred
   possible requirement to `DERIVED` and record its candidate statement,
   sources, proposed relations, consequences, and confirmation brief.
5. Treat feedback as a source, not an automatic implementation order. Classify
   it as a fact, decision, defect report, new outcome, change request, or
   ambiguity and route it to planning or triage.
6. Evaluate the candidate-packet trace. Only after it passes, open the exact
   human confirmation gate and present its brief. Apply an attributable answer
   as `PROPOSED`, `PENDING_VERIFICATION`, or `OBSOLETE`; otherwise stop at the
   open gate. Proposed relations remain candidate-only until separately
   authorized.
7. Reconcile the source and candidate records. Do not create tests,
   implementation records, or release commitments in this pass.

## Report

Report changed sources, confirmed facts, `DERIVED` candidates, conflicts,
confirmation-gate results, questions requiring human authority, and either the
next eligible planning input or exact hold.
