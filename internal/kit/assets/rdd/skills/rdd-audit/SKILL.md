---
name: rdd-audit
description: Inspect native Traces, their endpoints, requirement coverage and actual evidence. Use within reverse-engineering, planning review, completion review or an explicitly requested audit. Reports findings and limits without changing lifecycle or granting approval.
---

# Audit Traces and evidence

Read the project `AGENTS.md` and canonical `PROCESS.md`. This is a shared
utility, not a delivery phase. The invoking pass owns scope and routes findings
through `rdd-triage`. An audit alone authorizes no record or implementation changes.

## Establish scope

Identify the native System, Epic or requirements and the criteria being checked.
Read current records and Traces through the project's sanctioned interface.
Use native endpoint identities and kinds; never infer the corpus from local
files or recreate retired ledgers. If a required read is unavailable, report
the tooling gap and leave the check incomplete.

Separate intended behavior from implemented behavior. Code establishes the
latter; it does not replace approved intent. For each inventory, state its
unit, enumeration method and exclusions. Compare both directions: requirements
without support and relevant implementation or tests without requirement coverage.
Disclose unresolved items; counts alone do not prove semantic coverage.

## Inspect native Traces

1. Enumerate Traces touching each in-scope requirement and criterion. Follow
   pagination to completion. Inspect both endpoints, their native kinds,
   relationship and System scope. Report missing, invalid, cross-System or
   wrongly related endpoints.
2. Inspect the connected records and actual source material. Requirement-to-
   document Traces use `relates`. Verify that the document supports the claimed
   intent and that code implements the requirement's behavior. A valid Trace
   establishes a relationship, not the truth of a claim.
3. Map each UR acceptance scenario to meaningful user-outcome assertions and
   any required SRs. Map each SR criterion to implementation, test cases and
   actual results. During planning inspect proposed targets and expected RED
   assertions; do not require execution evidence for unimplemented behavior.
4. For verification and completion inspect actual results, commands/reports,
   tested revisions, environment where relevant and assertions exercised.
   Distinguish PASS, FAIL and SKIP. Test metadata or a source-file link does
   not prove execution; broad suite output proves only exercised assertions.
5. Report missing criterion coverage, unsupported relationships, contradictory
   sources and failing or inapplicable evidence. Do not manufacture links,
   results or coverage to complete a chain. Do not hash sources, capture
   snapshots, require a matching checkout revision or invalidate recorded
   requirements merely because sources changed. Assess the applicability of
   execution evidence to the claimed behavior at its recorded revision.

## Verify claims

Open the relevant declaration, callers, assertions and document context before
reporting a defect. Account for removals, renames, disabled code and boundaries.
Equal counts can describe different sets; inspect identities and meaning.
Agreement among documents does not establish authority. Surface conflicting
intent for a human decision instead of resolving it by timestamp or majority.

Use the project's supported source readers and ordinary repository files.
Unavailable historical material is a disclosed limit, not a reason to recreate
retired source-capture machinery. Keep uninspected findings separate from
confirmed defects. If checking published copies, use the sanctioned read-back
route and compare actual content; length alone does not prove equality.

## Report

Report native scope, inspected Trace identities and endpoints, criterion-level
coverage, sources and actual test observations. Include confirmed findings,
exclusions, unresolved checks and command outcomes. State what each observation
proves and its limits. Route findings through the invoking pass; an audit does
not change lifecycle, resolve a gap or authorize implementation or completion.

After an authorized correction, check related records, instructions and
published copies for the same defect within scope. Recheck affected Traces and
claims; disclose findings outside the authorized work.
