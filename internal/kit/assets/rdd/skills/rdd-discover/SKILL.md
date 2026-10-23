---
name: rdd-discover
description: Establish sourced product intent and identify draft planning or separately authorized work. Use before planning when the request contains ambiguity, conflicting sources, or newly discovered scope.
---

# Discover and classify

Read `PROCESS.md`, the selected Epic and relevant product sources. Use the
CLI for process state and preserve IDs.

1. Separate explicit user intent from facts established by documents, code,
   tests and runtime evidence.
2. Inspect the exact source passage or behavior supporting each material
   claim. Distinguish a source reference from proof that the source supports
   the claim.
3. Identify existing requirements, affected behavior, acceptance, conflicts,
   dependencies, risks and unknowns. Do not create a requirement merely to
   mirror a file, route or test.
4. Ask the human to resolve intent or scope that sources cannot establish.
   Do not treat a proposed plan or assumption as approval.
5. Route sourced scope into the selected Epic's saved plan through
   `rdd-plan` only while it is `PROPOSED`; implementation requires human
   approval. Once implementation is approved, keep the active plan and owned
   requirements read-only until an explicit human return to planning under
   `PROCESS.md` is applied. Revised scope then needs fresh review and human
   implementation approval. DONE stays closed; use a follow-up Epic.

Do not implement in this pass. Report sources inspected, confirmed intent,
open questions and the next needed phase. The dedicated reverse-engineering
skill records requirements and file traces directly.
