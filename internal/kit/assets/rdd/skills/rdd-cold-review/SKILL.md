---
name: rdd-cold-review
description: Independently review a saved Epic plan before human implementation approval. Use a context separate from plan authoring.
---

# Review the saved plan independently

Read `PROCESS.md`, the exact saved plan and proposed content, its linked native Epic and
requirements, and the sources needed to assess its claims. Do not rely on the
author's session summary when source material can be inspected.

Check that:

- the user outcome, scope and exclusions are explicit and consistent with
  authoritative sources;
- acceptance and requirement links trace to the intended behavior using
  native identities;
- reconnaissance covers affected state, interfaces, integrations, failure
  modes, risks and project test gates;
- the change boundary says what changes and what must remain unchanged;
- each changed behavior has a meaningful expected RED and proportional
  verification strategy;
- material security, data-integrity, operational, migration and testability
  concerns are addressed.

A file citation resolving is not proof that it supports the claim. Open the
source and assess the connection. Report findings with severity, the precise
claim or plan section, inspected evidence and a concrete correction. The
orchestrator updates and resubmits a materially changed plan. Do not implement,
change process state, or approve on the human's behalf.
