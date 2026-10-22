---
name: rdd-cold-review
description: Independently review a saved Epic plan before human implementation approval. Use a context separate from plan authoring.
---

# Review the saved plan independently

Read `PROCESS.md`, the exact saved plan and proposed content, its linked Epic and
requirements, and the sources needed to assess its claims. Do not rely on the
author's session summary when source material can be inspected.

The agent handling the task asks the user to answer the declared questions and
records their answers before requesting the final plan review. That agent then
fetches and retains the review bundle and supplies it with source access.
Use that exact bundle; do not run unavailable process tools or substitute a later
bundle. Return coverage of every criterion and verification obligation, reviewer
context, reason, findings and limitations. Context separation is an attestation,
not proof of different human identities.

Check that:

- the user outcome, scope and exclusions are explicit and consistent with
  authoritative sources;
- one or more UR/SR requirements cover all in-scope behavior, with
  explicit criteria and planned code/test traces; an empty requirement set
  or plan-only acceptance checks is a material finding;
- acceptance and requirement links trace to the intended behavior using
  identities;
- reconnaissance covers affected state, interfaces, integrations, failure
  modes, risks and project test gates;
- the change boundary says what changes and what must remain unchanged;
- each changed behavior has a meaningful expected RED and proportional
  verification strategy;
- material security, data-integrity, operational, migration and testability
  concerns are addressed.

A source link resolving is not proof that it supports the claim. Open the
source and assess the connection. Report findings with severity, the precise
claim or plan section, inspected evidence and a concrete correction. The agent handling the task
records the review against the retained bundle, records explicit
material-finding resolutions, then runs the `plan-ready` Epic gate check and
reports its recorded result and blockers. A passing review alone is not a passing
gate check or human approval. That agent resubmits whenever reviewed content changes,
including decision answers or test definitions. Do not implement,
change process state, or approve on the human's behalf.
