# Recovered design documents

Use this reference for a full-system adoption or when design-document recovery
is included in the requested scope. For a narrow behavior/context request, only
update relevant existing documents and report out-of-scope areas; the full set
below is not an exit requirement for that run. Do not expand source authorization
to complete a template.

Read maintained project guides, where available, as a human lens and preserve
their provenance separately from code-derived findings. Adopt or extend existing documents before
creating competing accounts. Verify code/config citations and distinguish
implemented behavior from unconfirmed intent.

## Full-system output roles

Produce one system-wide account per applicable role, not per-context duplicates.
These paths are defaults; adopt established equivalents explicitly and verify
the sanctioned document collector recognizes them.

| Default path | Content |
|---|---|
| `docs/03-architecture.md` | Subsystems, interfaces, stores and representative request flow; an existing `ARCHITECTURE.md` or `architecture.md` can satisfy this role |
| `docs/20-deployment-topology.md` | Runtime locations and boundaries; explicitly fold into architecture for a single stack |
| `docs/21-integrations.md` | Direction, protocol and failure behavior for external systems, including unreachable dependencies |
| `docs/22-cross-cutting.md` | Auth, tenancy, secrets, observability and resilience enforcement points |
| `docs/23-data-flow.md` | Origins, transformations, destinations and trust boundaries |

Use the project's document publication route and verify the published revision
and content. For a file-backed project, read the versioned published documents;
for a store-backed project, use the sanctioned read-back operation. A local
draft does not prove publication. Recovered documents are derived design
sources, not a second requirement store.

## Observed decisions and NFRs

Preserve existing ADRs; key recovered records by slug under the established ADR
location, defaulting to `docs/adr/NNNN-<slug>.md`. New recovered decisions are
`observed`, never fabricated ratification. A code citation proves an implemented
choice, not human acceptance.

Derive in-scope NFRs through the same authorized requirement route. Consider
performance, scalability, availability, security, privacy, operability,
maintainability and compatibility without duplicating existing ownership. Bare
configuration values with unknown intent remain questions or DERIVED exceptions.
Check the full expression and units before interpreting a numeric value.

Before reporting, account for the requested document roles, relevant integration
failure behavior, observed ADR provenance, NFR questions and publication receipts.
