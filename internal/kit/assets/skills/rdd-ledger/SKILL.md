---
name: rdd-ledger
description: The requirements ledger format — dashboard table, detail blocks, status vocabulary, the three-place status hygiene rule and how to verify it. Use when creating or editing a tasks/<CTX>-REQUIREMENTS.md file, changing a requirement's status, or checking whether the ledgers are internally consistent.
---

# The requirements ledger

<!-- TOOL-OWNED. Installed by `modernpath install`. -->

One ledger per bounded context, at `tasks/<CTX>-REQUIREMENTS.md`. It opens with
a dashboard table — the at-a-glance completeness view — and continues with detail
blocks carrying acceptance criteria and test links.

**Requirement id:** `REQ-<CTX>-NNN`, e.g. `REQ-USR-014`. Stable, never reused.

## Status vocabulary

| Status | Meaning |
|---|---|
| `PROPOSED` | Identified from the design docs; acceptance criteria not yet written |
| `READY` | Criteria written and reviewed; ready to build |
| `IN_PROGRESS` | Tests written (red) and/or implementation underway |
| `IN_REVIEW` | Green and traced; awaiting sign-off |
| `DONE` | Merged; all criteria pass; traced; logged |
| `DEFERRED` | Consciously not now — **must** carry a reason and a tracking link |
| `BLOCKED` | Cannot proceed — **must** carry the blocking question id |
| `OBSOLETE` | Superseded by a design change — **must** carry the superseding ref |

## Dashboard

```markdown
## Dashboard — USR (User Management)
Totals: 14 DONE · 3 IN_PROGRESS · 2 READY · 4 PROPOSED · 1 DEFERRED · 0 BLOCKED

| ID | Title | Stage | Status | Source | Tests | Code |
|----|-------|-------|--------|--------|-------|------|
| REQ-USR-001 | User registration validates email | MVP | DONE | INV-USR-001 | tests/user.register.spec.ts | domain/user.ts |
```

## Detail block

```markdown
### REQ-USR-001 — User registration validates email
- **Status:** DONE · **Stage:** MVP · **Priority:** must · **Owner:** —
- **Raised-by:** seeded from the domain doc
- **Source:** INV-USR-001 (`docs/10` §4)
- **Statement:** A user may only be registered if their email is syntactically
  valid and not already taken.
- **Acceptance criteria:**
  - GIVEN a valid, unique email WHEN RegisterUser THEN the user is created and a
    confirmation email is sent.
  - GIVEN an invalid email WHEN RegisterUser THEN it is rejected with `INV-USR-001`.
  - GIVEN an already-registered email WHEN RegisterUser THEN rejected with `EMAIL_TAKEN`.
- **Tests:** `tests/user.register.spec.ts` (unit), `tests/user.register.property.ts`
- **Code:** `domain/user.ts` (enforcement annotated `// INV-USR-001`)
- **Log:** EPIC-USR-001
- **Deferred / notes:** —
```

## Criteria-first rule

Acceptance criteria and tests are first-class citizens of the ledger, not
deferred paperwork:

1. **No requirement enters `IN_PROGRESS` without a detail block** carrying
   GIVEN/WHEN/THEN criteria, sharpened at SPECIFY.
2. **A `DONE` requirement's detail block links tests that exist**, and the
   criteria describe what those tests verify.

The `Statement:` line matters beyond documentation: tooling reads it as the
requirement's one-line description, falling back to the first criterion's THEN
clause. A row with neither reads as a bare title everywhere it appears.

## Status hygiene — the three places

A status change touches **three** locations and is a bug unless all three move
together:

1. the dashboard table row,
2. the detail block's `- **Status:**` line,
3. the dashboard `Totals:` line — decrement the old status, increment the new.

Verify after any batch of changes:

```bash
grep -oE "\| (DONE|IN_REVIEW|IN_PROGRESS|READY|PROPOSED|DEFERRED|BLOCKED) \|" \
  tasks/<CTX>-REQUIREMENTS.md | sort | uniq -c
grep "^Totals:" tasks/<CTX>-REQUIREMENTS.md
```

The counts must match. When recomputing totals across a large edit, count
programmatically rather than by hand — hand arithmetic across dozens of rows
drifts silently, and the `Totals:` line is what people read.

Then refresh `PROGRESS.md`.

## Deviation tracking

**Every deferral creates a requirement.** When you defer work you must create a
PROPOSED or DEFERRED requirement in the owning context's ledger stating what is
missing, what depends on it, and what must exist first.

Never use vague pointers — each deferred capability gets its own requirement.
Code comments are not tracking: a `TODO` or `DEVIATION` comment must be
duplicated as a ledger row.
