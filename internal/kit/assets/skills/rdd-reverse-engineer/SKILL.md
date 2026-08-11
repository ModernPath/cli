---
name: rdd-reverse-engineer
description: Derive a requirement ledger from an existing codebase that has none — bounded contexts, requirements, and an honest verification status for each, from the platform's analysis plus the code itself. Use in a repository that has code but no requirement ledger — no tasks/<CTX>-REQUIREMENTS.md and no WORKLIST.md. A bare tasks/ or docs/ directory does not disqualify it; other conventions use those names. Not for a workspace that already has a ledger; use rdd-planning there.
---

# Reverse-engineer the ledger

<!-- TOOL-OWNED. Installed by `modernpath install`. -->

A repository arrives with a hundred thousand lines and no requirements. This is
the pass that gives it a ledger which says what the code does, which parts are
actually verified, and what nobody can answer.

Read `.claude/rdd/PROCESS.md` §1 and §3 first. This pass writes documentation and
ledger rows; it does not change behaviour.

---

## Before you start

```bash
ls tasks/*-REQUIREMENTS.md WORKLIST.md   # MUST be absent — this pass is for a repo with no ledger
modernpath status                        # bound to a system?
ls .modernpath/                          # the export dir is named after the SYSTEM, e.g.
                                         # .modernpath/<system-slug>/architecture/
```

**Stop if a ledger already exists** — a `tasks/<CTX>-REQUIREMENTS.md` or a
`WORKLIST.md`. Then this is not the skill you want: use `rdd-planning` to extend
it. Re-deriving over a real ledger overwrites decisions people made deliberately.

A bare `tasks/` directory means nothing: other conventions use that name for
numbered spec folders, and one real repository did (`RUN:2026-08-11`). Check for
the ledger files, not the directory. Read whatever is in there — existing specs,
`ARCHITECTURE.md`, a constitution, agent memories — as design input; they are the
closest thing to recovered intent you will get.

**If `modernpath status` says not configured:** the token is stored per
repository, so `modernpath auth` must run *here* and it is interactive — ask the
person to run it. Then `modernpath init` to select the system.

**If no system exists for this repository yet**, the analysis has never run: it
must be imported and analyzed before this pass has a map to start from. Say so and
stop rather than substituting your own reading of the code for it — unless the
person asks you to proceed without it, in which case say plainly in your report
that the contexts are your reading, not the platform's analysis.

## What one pass produces

```
docs/00-overview.md            what the system is, every claim cited
docs/40-data-model.md          the entities, their owners, the invariants the schema enforces
docs/02-bounded-contexts.md    the context map — drawn by aggregate ownership
docs/06-surfaces.md            every view × the actors it admits × the use case it serves
epics/<EPIC-ID>/EPIC.md        one per user journey, each carrying a user requirement
docs/10-<domain>.md            the ONE context this pass covers
tasks/<CTX>-REQUIREMENTS.md    its ledger — dashboard, detail blocks, Totals
process/08-open-questions.md   what the code could not answer
WORKLIST.md                    the rollup
BACKLOG.md                     discoveries belonging to no context yet
```

**Three phases, in this order** (`USER:2026-08-11`). The order is the method:

```
A  DOMAIN      the schema and the analysis  →  entities, owners, contexts
B  SURFACES    every view, its actors        →  epics carrying user requirements
C  REQUIREMENTS UR ↔ SR, cross-linked        →  the ledger, and the join report
```

An earlier version of this skill started at C. It produced 47 system requirements,
**zero** user requirements, and opened none of the repository's 42 view files —
a ledger of what the code refuses, with no statement of what the product is for.
Phases A and B exist because that question is settled in the schema and the views,
neither of which an endpoint walk visits.

One context. Not nine. `PROCESS.md` §4: finish a context before seeding the next.
A pass that emits four hundred rows across every context produces a ledger nobody
reviews, and an unreviewed ledger is worse than none because it looks
authoritative.

---

## A1. The data model, before anything else

Read the schema directly — ORM models, migrations, constraints — and the
platform's analysis. **Neither alone.** The analysis names intent; the schema
cannot lie about structure.

```bash
grep -c "__tablename__\|CREATE TABLE" <models or migrations>   # your entity count
```

For each entity: what it is, who writes it, and **what the schema enforces** —
unique constraints, foreign keys, nullability, defaults. Those are invariants
nobody wrote down anywhere else, and they are among the most valuable rows a pass
can produce.

Write `docs/40-data-model.md`, citing the table definition for every claim.

## A2. Contexts by aggregate ownership

**Draw context boundaries by who writes which table**, not by route-file layout or
deployment units. Whoever writes an aggregate owns it; that is a boundary the code
cannot misrepresent. Route files follow the build system, and a context map drawn
from them will describe the deployment, not the business.

Each context names the tables it owns. **A table written by two contexts is a
single-writer violation** — report it, do not smooth it over.

## B. Every surface, and who reaches it

The frontend answers the question the backend cannot: what is this *for*, and for
whom.

```bash
ls <views/pages dir> | wc -l          # your denominator
grep -c "path:\|<Route" <router>      # route declarations
grep -rn "role\|requireRole\|Guard" <router>   # who is admitted where
```

For each view: **which actors reach it** (cite the role gate — it is a fact in
code, not an inference), what the actor can do there, and which endpoints it
calls. Write `docs/06-surfaces.md`.

Then group the views into **user journeys**, and write one epic per journey,
each carrying a **user requirement**: an actor, an outcome, and the views that
serve it — every one cited.

A user requirement carries the same evidence burden as a system requirement, and
needs it more: a user story reads as true even when nobody checked. *"As a buyer I
can compare vendors side by side"* is a sentence anyone can write from a filename.
Cite the view, the route and the gate, or leave it out.

## C1. Take the map from the analysis, not from grep

The platform already analyzed this repository. Its subsystems are candidate
bounded contexts, derived from this code rather than from your reading of it.

```bash
modernpath search "<domain terms>"        # find the doc
# then read it whole from the export, under the system's own slug:
.modernpath/<system-slug>/architecture/   # domain-*.md is the one to start with
.modernpath/<system-slug>/capabilities/ · patterns/ · datamodel/
```

Start with `architecture/domain-*.md`. It names the entities, the lifecycle and —
most valuably — the **invariants**, which are the requirements most worth having
and the hardest to find by reading code (`RUN:2026-08-11`: it named two that
turned out to be enforced in code and two that turned out not to be).

The subsystem folders (`backend-api`, `frontend-app`, …) are **deployment units,
not domain contexts**. Take the contexts from the domain document and confirm them
against module layout; a context map drawn from deployment units will follow the
build system rather than the business.

Then **confirm each boundary in the code** — module layout, ownership of tables,
who writes what. The analysis proposes; the code decides. An agent that instead
greps a large repository blind invents boundaries and then defends them.

Write `docs/02-bounded-contexts.md` from what survives that confirmation: context
name, short code (`ORD`, `PAY`, `USR`), what it owns, its integration points.

## C2. Choose one context

Prefer the one with the clearest ownership and the most behaviour a user would
recognise. Say in your report why you chose it and what you left.

## C3. Derive requirements from behaviour

**Enumerate the entry points first, and work the list.** Not the error branches —
the entry points.

```bash
grep -cE '^@(router|app)\.(get|post|put|patch|delete)' <the context's route files>
```

That count is your denominator: a context with 25 endpoints does not have 17
requirements. The first real pass mined `raise` sites instead and produced a
ledger of guardrails with no product in it — it captured "who may not do this"
and missed listing, visibility, an entire questionnaire flow, and the proposal
that flow exists to produce (`RUN:2026-08-11`).

For **each** entry point, derive both halves:

1. **What it does, and for whom.** The affordance, plus who may see what — "a
   vendor sees only the drafts it is a candidate for" is behaviour a user
   notices, and it usually has a test.
2. **What it refuses.** The invariants, from the rejection branches.

Then **the entry points that are not HTTP**, and count them in the same
denominator:

- **agent and LLM tool surfaces** — a tool an assistant can call is an entry
  point with its own authorization, and often a *different* one: a real pass
  found an agent recording questionnaire answers without the membership check
  every REST route enforces (`RUN:2026-08-11`). Look for agent/tool registries
  and prompt directories, not just routers.
- scheduled jobs and workers — and note which tables they write, since a worker
  co-writing a context's table is a single-writer violation (A2);
- event handlers, webhooks, inbound integrations;
- CLI commands; migrations that carry rules.

A denominator that counts only HTTP routes will look complete and be wrong. Two
passes over the same context differed by an entire conversational surface for
exactly this reason (`RUN:2026-08-11`).

For each **behaviour a user or another system would notice**, write one
requirement.

- One behaviour, one requirement. Not one function, one requirement — a
  three-function validation chain enforcing one rule is one requirement.
- State it as behaviour, not implementation: *"a refund reverses the VAT it
  charged"*, not *"RefundService calls VatCalculator.reverse"*.
- Every row cites `CODE:<path>:<line>` for the code that implements it.
- Invariants and business rules the code enforces — thresholds, state machines,
  uniqueness — become requirements too. They are the ones most worth having.

Configuration values are a signal: a hardcoded threshold is either a business
rule nobody wrote down (a requirement) or an accident (a question — §5).

## C4. Give each row an honest status

This is the part that makes the ledger worth trusting.

**Every derived row is at least `IN_REVIEW`.** The behaviour ships; it is not
verified and nobody has accepted it. Never `PROPOSED` — that status means *not
started*, and a payment flow running in production every day is not an unstarted
proposal.

**One exception, and only one:** a row that describes behaviour the system
**should have and does not** — a gate missing from one surface that every other
surface enforces — is a genuine proposal, and `PROPOSED` is correct for it.
Source it as `finding, this pass` so it is never mistaken for recovered
behaviour. If the fix needs a product decision rather than a mechanical port,
it is `BLOCKED` with a question instead (`RUN:2026-08-11`: an agent tool
returning other organizations' contact details with no access check could not
simply copy the REST gate, because it runs before the entity the gate keys on
exists).

**`DONE` is not yours to give.** `PROCESS.md` §7.6 requires human sign-off, and a
derived context has had none — so a derivation pass leaves every row `IN_REVIEW`
and records test evidence in an **Evidence** column instead. The status
vocabulary has no value for "test-verified but unapproved"; do not invent one by
promoting a row.

**What the Evidence column must distinguish:**

1. Find the tests that cover the behaviour.
2. **Open them.** A filename resembling the behaviour is not evidence; assertions
   are. A test named `test_refund` that only asserts a 200 does not cover
   "a refund reverses VAT".

   **Check what the test mocked.** A route test that patches out the service it
   is nominally testing verifies routing, serialization and auth wiring — not
   behaviour. If the assertion is `status_code == 204` while the service call is
   an `AsyncMock`, the row is `IN_REVIEW`, not `DONE`, and the gap is "route
   tested, behaviour mocked out". This is the single easiest way to overclaim a
   ledger, and a real pass did it on 1 row and risked it on 11 of 28
   (`RUN:2026-08-11`).

   The exception is a rule the **route layer itself** enforces — a dependency
   like `RequirePublicSectorOrg`, or filtering done in the handler. There the
   route test is testing the real thing. Say which case you are in.
3. **Run them** and see green. A test file on disk is not a passing test.
4. Only then may the row read **test run**, citing test path and line beside the
   code reference.

Anything short of that names what is missing — no
covering test, thin assertions, or tests not run. That list is the coverage cliff,
and it is the most valuable thing this pass produces.

**`BLOCKED` for what the code cannot explain.** Code answers *what*, rarely *why*.
A threshold with no comment, a branch nobody can date, a rule that contradicts
another: record the question in `process/08-open-questions.md`, mark the row
`BLOCKED`, and move on. Never infer intent from an if-statement and present it as
a derived fact.

## C5. Write the docs the ledger derives from

Requirements are never invented — they trace to canonical design
(`PROCESS.md` §3). Here the design is being recovered, so write
`docs/00-overview.md` and `docs/10-<domain>.md` **as you derive**, citing the code
each claim comes from. If you cannot cite it, it is a question, not a doc.

## C6. Link the levels, and report the join

Every system requirement links **up** to the user requirement it serves; every
user requirement links **down** to the requirements that implement it.

Then report the join, which is the reason for doing both halves:

- **a view that calls an endpoint that does not exist** — a broken or unfinished
  surface;
- **an endpoint no view calls** — dead surface, or an undocumented integration.

Neither is visible from one side alone. If the join is empty, say how you checked
— in a codebase of any size, zero of both is a claim that needs its own evidence.

## C7. The coverage manifest — the only definition of "done enough"

**Maintain `process/coverage.md`, and recompute it every pass.** Without it,
"comprehensive" is an opinion and each pass reports only its own corner. A real
project ran five passes and produced an accurate ledger covering **10%** of its
system, with nothing anywhere saying so (`RUN:2026-08-11`).

Count the whole system mechanically — the commands differ per stack, so write
down the ones you used:

| Dimension | Denominator | Covered |
|---|---|---|
| HTTP endpoints | every route across all route files | rows citing them |
| Non-HTTP entry points | agent tools, workers, webhooks, CLI | rows citing them |
| Tables | every table in the schema | tables owned by a seeded context |
| Views | every view file | views cited by a UR |
| Contexts | every context in `02-bounded-contexts.md` | contexts with a ledger |

**The manifest gates the sync.** Do not push a ledger to the platform below the
bar the project set — someone will modernize from it, and a confident map of 10%
of a system is worse than no map, because nobody doubts it.

## C8. Audit your own citations before you claim anything

Every `file:line` you wrote must resolve. This is mechanical, it takes seconds,
and it is the cheapest guard against a ledger that reads well and points nowhere:

```
extract every path:line from the ledger → assert the file exists
                                        → assert the file has that many lines
                                        → assert test citations land on a test
```

**Skip named absences.** A row that says *"no test — there is no
`test_draft_service.py` in the repository"* is naming a gap, which is the most
useful thing a derivation pass produces. It is not a citation, and an audit that
counts it as broken teaches the next pass to stop naming what is missing
(`RUN:2026-08-12`: 8 of 8 "broken citations" in a 3,222-citation ledger were
absences stated correctly). Ignore any reference preceded by *no*, *missing*,
*there is no*, or *does not exist*.

**And resolve paths properly before reporting a failure.** A citation written
`db/models.py:1483` may live six directories deep; a bare `admin.py` may match two
files, only one of which is long enough. Match on path suffix, accept if **any**
candidate satisfies the line, and search the whole repository rather than one
subtree. Three separate audit scripts written in one session each reported the
ledger as broken when the resolver was at fault (`RUN:2026-08-12`). Prove your
checker on a citation you know is good before you trust its failures.

Report the result as a fraction (`140/140 resolve`). A pass that cannot say this
number has not checked.

## C9. Reconcile, verify, land

```bash
modernpath check          # status hygiene: Totals vs rows, the three places
modernpath factory sync   # land it on the platform
```

**Compute the `Totals:` line by counting the rows, never by hand** — count them
with a script and paste the result. The first real pass typed it and was wrong by
one on its largest bucket (`RUN:2026-08-11`), which `modernpath check` would have
caught but a reader would have believed. It is a bug when Totals and rows disagree
(`PROCESS.md` §1.8). Add the context's rows to `WORKLIST.md`.

The reviewer accepts the seeded context as a **batch**, the way fast-lane rows are
reviewed. Do not open an approval gate per derived requirement: sixty gates is a
denial-of-service on the person this pass is meant to help.

## C10. Report, and say what is left

State plainly:

- which context you seeded, and why that one;
- **entities, views and entry points covered / the totals for each** — the
  denominators from A1, B and C3. "17 rows" means nothing; "38 rows across 25 of 25 endpoints" does;
- how many rows, split by status;
- **the coverage cliff** — what ships with no test behind it;
- every question you raised;
- what you skipped and why. A pass that silently truncated is indistinguishable
  from one that found nothing more;
- **which contexts remain unseeded, and the next one you would take.**

## C11. This is one iteration of a loop

One pass seeds one context. A repository with nine contexts needs nine passes,
and the ledger is not comprehensive until they have all run.

So end every pass by naming the next context, and expect to be run again. The loop
terminates when `docs/02-bounded-contexts.md` lists no context without a ledger —
at which point say so plainly rather than seeding something twice.

If the person driving you wants it unattended, `/loop` (or any scheduler) can
re-invoke this skill; each iteration is independently reviewable, which is the
whole reason for one-context-at-a-time. Comprehensive is reached by repetition,
not by one enormous pass.

---

## This pass never

- writes the missing tests — that is ordinary build-loop work, red-first, with its
  own requirements;
- refactors anything — a pass that edits code cannot be reviewed as either
  documentation or a change;
- marks a row `DONE` on a test it did not open and run;
- guesses a business rule to avoid leaving a `BLOCKED` row.

**Exit criteria:** one context seeded, every row sourced to `CODE:` or `DOC:`,
`modernpath check` green, the coverage cliff and the open questions written down.
