---
name: rdd-reverse-engineer
description: Read existing code and documentation and record requirements with typed traces to source files.
---

# Record requirements from existing code

Read the project's `AGENTS.md`, `PROCESS.md` and current CLI reference. Establish
the user's requested repository and behavior scope. Use the sanctioned process
interface to read existing requirements and record new ones.

1. Read the relevant code, tests and documentation. Describe the behavior found,
   its actor, outcome, limits and side effects using the existing domain terms.
   Write meaningful acceptance criteria. A source link does not prove execution.
2. Reuse existing requirements by native identity and kind. Preserve their
   content. Avoid duplicate requirements; link serving system requirements to
   the user outcomes they implement.
3. Record requirements, source file references and typed Traces atomically.
   Use native IDs for trace endpoints. File references name repository and path;
   a known revision is optional context. New requirements start as PROPOSED.
4. Read back the recorded requirements, criteria, files and traces. Report
   created and reused records, unresolved questions and remaining requested work.

Do not hash files, inventories, documents or reports. Do not require source
capture, an authorization run, publication groups, clean Git state, matching
revisions, proof packets, as-built acceptance or mechanical quality/coverage
gates before recording. Source changes do not invalidate recorded requirements
or file traces. Authentication, tenant/System scope, native endpoint validation
and atomic writes remain normal store integrity rules.

Local working-set files may hold projections and development inputs. The server
remains authoritative. Recording requirements does not assert test PASS,
verification or completion. Missing tests do not block recording. When asked
to verify existing behavior, use
[rdd-reverse-engineer-verify](../rdd-reverse-engineer-verify/SKILL.md). After
complete applicable passing evidence, explicit human acceptance may use
[rdd-reverse-engineer-accept](../rdd-reverse-engineer-accept/SKILL.md) to
publish unowned PROPOSED requirements as DONE. If behavior or tests need
changes, use the ordinary Epic lifecycle instead.

For a requested coverage report, report the scope inspected and the remaining
files or behavior honestly; see [coverage reporting](references/coverage.md).
For requested design-document output, use
[recovered documents](references/recovered-documents.md).
