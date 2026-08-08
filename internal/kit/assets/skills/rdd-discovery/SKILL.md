---
name: rdd-discovery
description: Turn raw customer requirements, user stories and business rules into the canonical design docs a requirement ledger can derive from, and bootstrap the development harness. Use once per project or major feature, before any requirements exist.
---

# Discovery

<!-- TOOL-OWNED. Installed by `modernpath install`. -->

Getting the design docs right is most of the work — requirements derive from
them, and a vague doc produces a vague ledger. Read `.claude/rdd/PROCESS.md` §2
and §3 first.

---

## Customer requirements → design docs

**Inputs:** the customer's requirements, user stories and business rules; the
technology choices; the target domain.

Produce the canonical documentation set. Names below are the default convention —
follow the project's existing numbering if it has one.

1. **`docs/00-overview.md`** — one-paragraph system description, core
   capabilities, key constraints and non-negotiables, a glossary of domain terms
   in the customer's own words, and conventions (ids, timestamps, money).
2. **`docs/02-bounded-contexts.md`** — the context map: contexts with short
   codes (USR, ORD, PAY), what each owns (aggregates, events), integration
   points, and single-writer rules.
3. **`docs/10-<domain>.md`**, one per major domain — purpose and scope;
   aggregates and their state machines; value objects; **invariants**
   (`INV-<CTX>-NNN`) that must never be violated; **business rules**
   (`BR-<CTX>-NNN`); commands with inputs, outputs and emitted events; event
   payloads; policies (event → command); read models and queries; integration
   points; API endpoints; open questions.
4. **`docs/data/40-data-model.md`** — tables per context, column types and
   constraints, foreign keys, indexes, triggers and check constraints.
5. **`docs/data/41-event-catalog.md`** — event name and owning context, payload
   schema, producers and consumers, idempotency keys.
6. **`docs/07-api-contracts.md`** — URL structure, auth model, request/response
   formats, error conventions, pagination.
7. **`docs/82-tech-stack.md`** — language, runtime, frameworks, databases,
   messaging, key libraries, and an ADR for each significant choice.
8. **`process/08-open-questions.md`** and **`process/gap-register.md`** —
   initialize with the ambiguities and gaps you found.

**Rules:** extract every business rule — nothing implicit. Number all invariants
and business rules. Flag ambiguities as open questions rather than guessing. Use
the customer's terminology in the glossary. Keep domain docs independently
readable.

**Report:** contexts identified, invariant and rule counts per context, and every
open question raised.

---

## Bootstrap the harness (once)

Stand up the development skeleton only — no business features:

- repository layout per `AGENTS.md`;
- local development environment (services, database, processes);
- schema-first codegen wiring where applicable: schemas → types, migrations,
  handlers;
- the test harness at each level — unit, component, API/contract, integration —
  plus the focused gates the team will actually run;
- a CI pipeline that runs those gates and blocks merge on failure.

**Exit criteria:** a trivial end-to-end path passes through the whole stack, and
the gates run green.

**Report:** what you created, how to run it, and confirmation of the exit criteria.
