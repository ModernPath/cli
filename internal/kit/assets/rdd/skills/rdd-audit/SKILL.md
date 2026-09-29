---
name: rdd-audit
description: Check document claims, source citations, and inventory completeness against direct evidence. Use within reverse-engineering, planning review, or completion review, or for an explicitly requested document audit. Reports verified findings and measurement limits; does not assign lifecycle state or authorize changes.
---

# Audit claims and citations

Read the project `AGENTS.md` and canonical `PROCESS.md`. This is a shared
utility, not a delivery phase. The invoking pass owns scope and routes findings
through `rdd-triage`. An audit request alone does not authorize document or
implementation changes.

## Establish the scope

Identify the documents, source repositories, revisions, and claims being checked.
Use the project's actual document locations. Separate intended behavior from
claims about implemented behavior: code can establish the latter but cannot
silently replace approved intent.

In a store-backed project, use its sanctioned tool for authoritative records,
typed citations, inventory projections, and immutable source reads. Do not infer
the requirement corpus from local snapshots or recreate retired task ledgers.

For each inventory, state what one item is, how it was enumerated, and which
items are excluded with reasons. Compare both directions: entries lacking
support and relevant source items absent from the inventory. Account for each
item as covered, excluded with a reason, or unresolved. A disclosed unresolved
gap remains a gap. Counts and percentages describe that population; they do not
prove semantic coverage or authorize a lifecycle transition.

## Check citations

Use `audit-citations.mjs` beside this skill for local Markdown citations. Pass
the actual document roots and source repositories explicitly. Substitute the
paths and repository keys in this command:

```text
node <skill-directory>/audit-citations.mjs <document-root> --repository=<key>=<source-root>
```

Repeat `--repository` for multiple repositories. Git worktrees and explicitly
declared non-Git roots are supported. A qualified reference has the form
`CODE:repository@revision:exact/path` or `TEST:` with the same structure.
Qualified Git revisions must match the declared checkout's HEAD; files must
exist in that commit, regardless of working-tree changes.
Unqualified references and `@unversioned` roots use local files. Use the
sanctioned source reader for captured dirty snapshots or other historical revisions.

The checker resolves cited files for `CODE:`, `TEST:` and `DOC:`. The referenced
file carries the requirement or other item ID it supports. Optional symbol,
line, test-name and heading suffixes are ignored. The checker does not read
source contents or match strings within them. File resolution does not establish
that the source supports a claim or that a test executed.

Describe missing artifacts as gaps in prose. Do not prefix an absent path with
`CODE:` or `TEST:` as though it were supporting evidence. Nearby words such as
“no” or “missing” do not exempt a citation from checking.

Teaching examples may use the per-line `<!-- example-citation -->` marker or
an `example-citation` fenced block. Those references are reported as skipped,
never included in the checked denominator. Prompt directories are guarded by
the checker; use `--force-prompts` only for an intentional review of their
examples and real references.

The checker uses these exit statuses:

- `0`: the file-resolution check passed and the minimum citation count was met.
- `1`: broken, ambiguous, unsupported, or elided references, or fewer than
  `--min=N` citations checked.
- `2`: a requested document target is missing, the source inventory is invalid,
  or a guarded prompt target was refused.

Exit `2` means the audit could not run on the requested inputs. Retain the error,
correct the inputs or invocation, and rerun; it is not a pass or an automatic
skip. The default minimum is one; `--min` is a citation-count guard against an
empty or incomplete scan, not an extraction-coverage threshold. Establish an
expected citation count independently before using a higher minimum.

## Verify meaning and measurement

Open each suspected defect and check the full relevant expression, declaration,
caller, and document context before reporting it. Distinguish a documented
exclusion or intended future behavior from an incorrect claim of current behavior.

Check claims about complete sets against an independently enumerated population.
Account for removals, renames, disabled code, nested route prefixes, and other
language-specific constructs where they affect that population. Verify named
references in addition to counts; equal counts can describe different sets.

Validate a new or changed checker against both known-good and known-bad inputs.
Check exit status, selected inputs, and actual output. Neither a high failure
rate nor a clean first run establishes whether the checker is correct. Keep
uninspected detector hits separate from confirmed findings.

Compare related documents to locate disagreements, then resolve factual claims
against authoritative sources. Agreement among several documents does not make
their claim authoritative. Do not resolve conflicting intent by majority or
timestamp.

For published copies, use the sanctioned publication/read-back route and compare
content or digests at the relevant revision. Content length alone cannot establish
equality. Check applicable runbook commands, configuration, document links, and
indexes against their actual targets.

## Report

Report the inspected scope and revisions, inventory units and counts, checked
citations, confirmed findings with direct sources, exclusions, and unresolved
or unsupported checks. State what was checked and held as well as what failed.
Preserve command output and exit codes for measurements used by a gate.

Route findings through the invoking pass and the sanctioned process store when
they affect process records. A report does not itself change a requirement,
resolve a gap, approve a decision, or waive missing evidence.

After an authorized correction, search the audited documents and relevant
repositories for other occurrences of the corrected claim and its identifiers,
including examples, configuration, and templates. Verify each occurrence
against current sources before editing. Correct only within the authorized
scope and report findings outside it. Recheck affected claims after changes.
