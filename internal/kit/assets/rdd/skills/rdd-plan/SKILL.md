---
name: rdd-plan
description: Prepare an epic-scoped or single-SR planning packet for controlled implementation entry. Use for a new or changed requirement, stale technical context, or work that needs sourced acceptance content, technical reconnaissance, SR enrichment, RED strategy, decisions, and an entry brief. Stops before cold review and human entry approval.
---

# Plan a requirement trace

Read the project `AGENTS.md`, canonical `PROCESS.md`
(`.modernpath/rdd/PROCESS.md` in a consuming repository), relevant product
sources, requirement records, code, tests, and optional epic. Apply the Work
scope, Item ownership, and Planning and readiness sections of `PROCESS.md`.

## Procedure

1. Confirm every requirement and relation the proposed scope depends on is
   authoritative. Take the scope from the stored relation graph — declared
   members, required SRs, gates — never from a keyword search over records.
   Stop at the confirmation gate for `DERIVED` requirements or candidate-only
   links. Search the records for requirements constraining affected behavior
   and verify each match against its content and declared relations. Carry
   applicable delivered acceptance into the packet. Record surfaces with no
   existing owner without inventing one. A change to existing acceptance
   is a scope decision for step 6. For a small change the search covers the
   files its boundary names.
2. Choose epic scope, single-SR scope, or the small-change lane using
   `PROCESS.md`. Do not invent epic membership or a UR link to make the graph
   appear complete. Take the small-change lane only when the change meets the
   lane's eligibility and a current lane authorization covers its class; its
   packet is the SR record's statement, boundary, RED plan and lane class, and
   nothing more.
3. Create or update the selected item content: sourced UR outcomes and inline
   scenarios when user behavior is in scope, and thin testable SRs for system
   behavior.
4. Perform technical reconnaissance at a named repository revision. Read the
   project's available system documentation for context and cite relevant
   sources as `DOC:`. Check claims about existing behavior against code;
   differences from intended behavior remain findings. Record the affected surface,
   control/data flow, contracts, reuse targets, dependencies, risks, test
   infrastructure, failure modes, and unknowns. Inventory the
   surface by what depends on the invariant the change alters, not by the
   callers of the module that owns it, and judge each break per call site
   against the post-change invariants — one file can hold call sites of both
   kinds. Verify every claim about existing code by reading it at that
   revision; a reconnaissance sentence is a citation, not a memory. When the
   change adds or touches persisted or shared state — a table, a column, a
   snapshot, a job, a cache, a file, a join row as much as an entity — write
   the state inventory: one row per piece of state with its writers today and
   after the change and each write shape it admits — a birth, an edit of an
   open row, an edit of a settled row, a row born before the change — the
   readers that branch on it, what a crash mid-write leaves, what makes it
   stale, and recovery. Record planned mitigations, their supporting criteria
   or decisions, and unresolved risks. Check the inventory for uncovered
   acceptance before handing it over. A value the packet
   states — a limit, a timeout, a constant — is read at the call site that
   applies it, not at its definition: a defined value may be unused, or one of
   several the code selects between. Keep the packet focused on facts the
   builder and gates need; do not split behavior to meet a document length limit.
5. Enrich every selected SR with its implementation context, explicit change
   boundary, and lower-RED strategy. Define a separate upper-RED strategy for
   each selected UR scenario requiring new evidence. Re-validating unchanged,
   previously proven behavior needs no new RED. A lower-RED strategy names
   the planned test file, the behavior it asserts, and the expected failure.
   Every planned RED case for new or changed behavior must fail against the
   code at the planning revision for the stated reason; a case that would
   already pass cannot serve as RED evidence. Check that reason against the
   current code during planning.
   Planned commands and test identities are proposals; actual commands and
   outcomes become evidence when executed at the RED revision. A migration or
   schema change that satisfies the test lands with GREEN. For entered
   verification of existing behavior, plan the safe temporary mutation or
   equivalent targeted failure described in `rdd-verify`. For a
   diagnosed, bounded defect the failing test may already exist on a branch
   as a `RUN:` source — the defect lane in `PROCESS.md` §Entry packet —
   cite it, and plan the SR's own lower RED to be re-established after entry.
6. Route product, scope, architecture, acceptance, priority, release, and
   workflow decisions through exact human gates. Open them only after their
   trace prerequisites pass. Before opening one, check whether a rule, a
   record, or an earlier answer already implies it — a change that preserves
   a delivered acceptance, or applies a stance the human has stated, is
   recorded and reported, not asked. Put what remains in one plain sentence
   about what changes for the product, the identifier a trailing breadcrumb.
   Record blockers, conflicts, gaps, and deferrals rather than guessing.
7. Assemble Entry-packet items 1–6 and the product-language brief. Reconcile
   planning records, then hand off to `rdd-cold-review` for item 7. A resolution
   edit re-enters step 4 for what it names: reread every symbol, path, and test
   it names at the recorded revision before marking the finding `RESOLVED`.
   Verify a fix proposed by the reviewer in the same way as a closure carried
   from an earlier round. A new mechanism needed to resolve a review finding
   is not added during the review cycle: route it as a scope
   question through step 6 and plan it as its own change with its own
   reconnaissance in step 4. Packet edits may clarify what the change already
   contained. Route other changes to approved scope or material decisions
   through step 6.

## Report

Report the selected scope, authoritative graph, reconnaissance revision,
planned evidence, unresolved decisions, blockers, and cold-review input.
