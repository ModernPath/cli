---
name: rdd-reverse-engineer
description: Bootstrap a requirement corpus from an existing codebase that has none — inventory observable behavior by bounded context against explicit denominators, create every inferred requirement as DERIVED with candidate-only relations, build exact confirmation packets, and hand confirmed scope to the normal delivery loop. Use to adopt a repository that has code but no authoritative requirement records; never for a workspace that already has them (use rdd-plan there), and never as a substitute for any phase — it creates no acceptance content, tests, release commitments, or authoritative relations.
---

# Bootstrap a corpus from an existing codebase

An optional orchestrator over the standard process, not another lifecycle. A
repository arrives with a hundred thousand lines and no requirement records;
this pass gives it a corpus that says what the code observably does, marks
every inferred statement as awaiting human confirmation, and routes what a
human confirms into the same loop every other requirement travels.

Read the project `AGENTS.md` and the canonical `PROCESS.md`
(`.modernpath/rdd/PROCESS.md` in a consuming repository) first. `PROCESS.md`
owns the `DERIVED` hold, the confirmation gate, and every status this pass may
apply; nothing here redefines them.

## Preflight — when this pass applies

1. **Stop if an authoritative requirement corpus exists** — a store binding
   with requirement records, or populated `file-state/REQUIREMENTS.md`
   equivalents. Re-deriving over a real corpus overwrites decisions people
   made deliberately; use `rdd-plan` to extend it instead. A bare `tasks/` or
   `docs/` directory proves nothing — other conventions use those names, so
   check for the records, not the directory.
2. **Identify the process store** per `PROCESS.md` — store-backed or
   file-backed — and serialize every record this pass creates through the
   `file-state/` shapes for that store. This pass never invents a third
   representation.

   **A ledger-format workspace materializes the requirement corpus too.** Where
   a workspace's tooling reads `tasks/<CTX>-REQUIREMENTS.md` — the shape the
   `rdd-ledger` adapter and `modernpath factory sync` ingest — write the
   corpus there as well as to `file-state/REQUIREMENTS.md`, one file per
   context, in the dashboard/detail-block shape. **Keep the `UR-`/`SR-` id
   prefix** — it is how a ledger row says which kind it is, and the store
   routes user and system requirements to different tables. Writing every row
   as `REQ-` files a user requirement under a system requirement's evidence
   class, which is a silent loss, not a formatting choice. `REQ-<CTX>-NNN`
   remains valid and still means a system requirement.
   `file-state/` alone is not enough for those workspaces: in a store-backed
   repository it is a projection, so a corpus that exists only there never
   reaches the store: sync reads its `tasks/` glob, finds nothing, and coverage
   reports zero rows over the whole tree after a complete pass. This
   instruction already says so for Epics and for NFRs; saying it for the
   requirement corpus is the same rule, not a new one.
3. **Take what analysis exists as a lens.** A platform knowledge core, a
   maintained `ARCHITECTURE.md`, human-written guides — read them all before
   the code. Each proposes; the code decides. Every claim this pass records
   cites `CODE:`, `DOC:`, or `TEST:` sources it verified itself. A guide that
   cannot be confirmed in code becomes an open question naming the guide,
   never a silently adopted fact — and this pass never authors a guide, which
   would launder its assumptions into an input.

## Four phases, and the order is the method

```text
A  DOMAIN        schema and analysis        ->  entities, invariants, contexts
B  SURFACES      every view, its actors     ->  journeys carrying candidate URs
C  REQUIREMENTS  entry points, both halves  ->  DERIVED candidates, cross-linked
D  ARCHITECTURE  the shape around it all    ->  recovered design documents
```

A–C loop, one bounded context per pass, until the context map lists no
context without records; phase D runs **once per system**, after at least one
full A–C pass, because its documents need the context map and the data model.
A pass that starts at C produces a corpus of refusals with no statement of
what the product is for — that question is settled in the schema and the
views, which an endpoint walk never visits.

**Phase A** reads the schema directly — models, migrations, constraints —
beside whatever analysis exists. For each entity: what it is, who writes it,
and what the schema enforces; those constraints are invariants nobody wrote
down anywhere else. Contexts are drawn by **aggregate ownership** — who
writes which table — never by route-file layout.

**Phase B** walks every surface: which actors reach it (cite the **role
gate** — it is a fact in code), what each actor can do there, and which entry
points it calls. Group the views into user journeys; each journey is a
candidate epic carrying a candidate **user requirement** — an actor, an
outcome, and the views that serve it, every one cited. Candidate groupings
serialize to `file-state/EPICS.md` (ledger-format workspaces materialize an
`epics/` directory).

**Phase C** derives candidates from the entry-point inventory below.
**Phase D** writes the recovered design documents, further down.

## Inventory observable behavior — the denominators

Enumerate mechanically, by bounded context, before deriving anything. The
counts are denominators; coverage is measured against them, and no class may
be silently omitted — a genuinely inapplicable class (no client app, no jobs)
is a stated fact in the report, not a skipped row.

| Class | Enumerate |
|---|---|
| Entry points | every HTTP route, RPC procedure, worker, scheduled job, webhook, event handler, CLI command — and every agent/LLM tool surface, which carries its own authorization and is the class most often missed |
| Data models | every table/model, with the invariants the schema enforces — uniqueness, foreign keys, nullability, state machines |
| Access control | every guard, middleware, policy, role gate |
| User-visible flows | every route/view/flow in each client application, and which actors reach it (cite the gate — it is a fact in code) |
| Integrations | every external system, enumerated from injected credentials and configuration, not only from named modules — an adapter wired in config has no module to find |
| Tests | every test file, so existing evidence can be traced rather than rewritten |

Draw context boundaries by who writes which aggregate, not by route-file
layout or deployment units — a map drawn from those describes the build
system, not the business. A table written by two contexts is a single-writer
violation: report it, never smooth it over.

Work the entry-point list, not the error branches. For each entry point,
derive both halves: what it does and for whom, and what it refuses — the
rejection branches are where invariants live, and they are the requirements
most worth having.

## Derive — one observable behavior, one candidate

A requirement is something a change could **breach**. A sentence that merely
describes how the code is shaped cannot be breached — it is documentation, and
it belongs in the recovered design documents this pass writes alongside the
corpus, not in a requirement record.

| Sentence | Verdict |
|---|---|
| "An event's type is at most 64 characters" | requirement — a change can breach it |
| "Only the lead may open a draft" | requirement |
| "An event's detail is unstructured JSON with no schema" | documentation — adding a schema breaches nothing |
| "The model and the migration agree, column for column" | a test, not a requirement |
| "Append-only is a convention, not a constraint" | a finding — record it as one |

- One behavior, one candidate — not one function, one candidate. A
  three-function validation chain enforcing one rule is one requirement; an
  endpoint with five distinct observable behaviors is five.
- State behavior, never implementation: *"a refund reverses the VAT it
  charged"*, not *"RefundService calls VatCalculator.reverse"*.
- Declare the canonical kind on every candidate: `UR` for an actor-outcome
  behavior served by identified surfaces, `SR` for a system behavior at a
  boundary. Group views into user journeys and derive candidate URs from
  them — a UR carries the same evidence burden as an SR and needs it more,
  because a user story reads as true even when nobody checked. Cite the view,
  the route, and the gate, or leave it out.
- A hardcoded threshold is either a business rule nobody wrote down (a
  candidate) or an accident (an open question). Never present an inferred
  intent as a derived fact: code answers *what*, rarely *why* — a threshold
  with no comment, a branch nobody can date, a rule contradicting another is
  an open question for the confirmation packet, not a guess.

While establishing these facts, prefer the artefact over the description of
it; read the whole expression, never the grep hit; search the entity alone
rather than conjoining verb and noun on one line (deletion code almost never
names its table beside the verb); exclude comment lines from behavioral
evidence while still citing a comment as evidence of what the code *says*;
and support an absence claim only with a named search — *"nothing does X"*
requires stating which files were opened to reject it, and a clean absence
after a real search is a finding to state plainly, with the search shown.
Where a decision is recorded but not deployed, write both halves labelled
**Decided** and **Deployed** — they are different facts with different
evidence.

Link candidate URs and SRs both ways, then report the **join report**: a view
calling an entry point that does not exist is a broken or unfinished surface;
an entry point no view calls is dead surface or an undocumented integration.
Neither is visible from one side alone, and an empty join over a codebase of
any size is a claim that needs its own evidence.

## Everything lands DERIVED

Every inferred requirement is created `DERIVED`, exactly as `PROCESS.md`
§Derived requirement hold prescribes: candidate statement, inference sources,
proposed relations all labelled `CANDIDATE`, conflicts, consequences, and a
confirmation brief — the **Candidate packet** slot in the requirement shape.
`DERIVED` rows are excluded from authoritative trace, release, readiness,
coverage, progress, and completion, and this pass never creates or advances
anything related to them.

The legacy failure this replaces: assigning `PENDING_VERIFICATION` directly
to derived rows. That status is human-confirmed as-built behavior; inference
is not confirmation, however good the citations. Only a human answer moves a
row out of `DERIVED`.

Three routings that are not `DERIVED` candidates:

- a **pre-existing red test** is work someone started, not shipped behavior —
  record it as a discovery for `rdd-triage`, and say the suite was already
  red there before this pass arrived;
- a code path that **provably cannot run as written** is a finding, not a
  behavior;
- a discovery with unclear ownership or a cross-cutting concern goes to the
  triage backlog shape, not the requirement shape.

## The coverage contract — the only definition of "done enough"

**The floor is 90% of every denominator class, the target is 100%, and the
claim is a script's exit code — never a sentence.** Two recorded failures
share one cause: a 46-row "complete" corpus over an 80-endpoint,
two-application estate, and a "110/110 endpoints routed" claim that audited
to 65/110 the same day. Both stated coverage as prose, and prose drifts
toward optimism.

- For each denominator class, write the enumeration and matching as a small
  script kept in the repository (e.g. `tools/endpoint-coverage-audit.py`)
  that prints `N/N` per context, lists every miss, and
  **exits non-zero below the floor** — coverage stays re-checkable by anyone, forever. The
  report **embeds the scripts' verbatim output**; a coverage claim without
  embedded audit output is invalid, and an orchestrator receiving one
  re-runs the audit rather than relaying the claim.
- **No row budgets exist.** There is no such thing as a row budget: the
  grain is one candidate per observable behavior, however many that yields.
  A reference estate correctly derived carried **978 requirements** across
  13 contexts, at per-context densities of 50–130; a context reporting 6
  rows over 30 endpoints is under-derived, full stop. Grouping is legal only
  where the observable behavior is genuinely one, and the grouped units are
  always enumerated on the row.
- **Below the floor, the pass is not done.** Keep deriving, or hand off an
  honest partial that names the precise remainder — a partial is a handoff,
  never an endpoint, and a silent truncation reads as "covered everything".
- **No silent fallbacks.** A tool that fails — an analysis export missing, a
  test runner broken, a script erroring — is named in the report with what
  it blocked; work continues on every path that does not depend on it.
- Where the workspace ships a standing coverage instrument (for ModernPath
  workspaces, `modernpath coverage --json`), run it before deriving anything
  and pin its output as the pass's *before*; take targets from its
  ranked uncovered directories rather than from taste, and passing over the
  top-ranked gap needs a stated reason. At the end of the pass
  the same command is the after — the delta is the pass's receipt, and a pass whose
  delta on its declared target is zero did not happen, whatever its prose
  says. An instrument's untraced test files list is standing input for
  `rdd-verify`: an existing green test nobody traced is the cheapest
  verification available once its row is confirmed.

Before claiming anything, invoke `skills/rdd-audit/SKILL.md` over what this
pass produced: every citation resolves from the repository root
(§"Citations — does every reference resolve?"), every inventory is diffed in
both directions, and the coverage numbers carry their populations
(§"The audit").

### The full sweep — every context, one run, stated cost

One context per pass is the default because it protects derivation rigor.
When the human explicitly asks for the complete adoption, the sweep is a
different contract, not a shortcut, and it runs unattended:
**one invocation loops** measure → target → derive → audit, taking the next ranked gap each
iteration, until every denominator-class floor passes **and** the
**file-coverage floor** passes — coverage lands **well over 50%** of the
source-file inventory (operationally, keep looping below 60%
cited-or-dispositioned; files legitimately outside the behavioral surface,
such as type barrels, migrations, and generated code, count only when
explicitly dispositioned in the report — a disposition is written, never
assumed). There is **no human checkpoint inside the loop**: candidates still
land `DERIVED`, and the confirmation gates open in the terminating report,
which embeds the final audit output as proof — deferring confirmation to the
end of the sweep is not skipping it. On an honest-partial handoff the
orchestrating agent **relaunches for the remainder** automatically rather
than reporting the partial and waiting; asking the human to notice
under-coverage is the failure mode this contract exists to prevent.

## Phase D — the recovered design documents, once per system

Phase C says what the system does; none of it says what the system *is*.

Read `docs/guides/` — every human-written guide — before phase A begins. A
guide is a **lens**, never a source: it directs attention, every claim still
cites the code or config it came from, a guide that cannot be confirmed
becomes an open question naming the guide, and the pass never writes one.

## D1. The five documents

System-wide, written once — no per-context fan-out:

| Document | Answers |
|---|---|
| `docs/03-architecture.md` | key subsystems, external interfaces, datastores, the path one request takes — the document a reader opens first |
| `docs/20-deployment-topology.md` | what runs where and what a request crosses; fold into `03` for a single-stack estate |
| `docs/21-integrations.md` | each external system — direction, protocol, and **what happens when it is unreachable**, which no dependency list gives you |
| `docs/22-cross-cutting.md` | auth, tenancy, secrets, observability, resilience *as implemented*, each concern naming its enforcement point |
| `docs/23-data-flow.md` | where a value originates, what transforms it, where it lands, which trust boundaries it crosses |

Before writing any document, look for what the repository **already covers**
— a maintained architecture document, existing ADRs. Adopt it or extend it
in place rather than writing a competing file, and supersede only
deliberately, with a coverage diff proving nothing is lost.

## D2. Decision records — `docs/adr/NNNN-<slug>.md`

One record per decision the code has plainly already made — datastore,
transport, isolation, deployment shape — with status **`observed`**, a third
status beside accepted and superseded: the pass can prove a decision was
made, never that anyone ratified it, and an ADR claiming a ratification the
repository never performed is the same lie as a completion with no evidence.

## D3. Non-functional requirements — `tasks/NFR-REQUIREMENTS.md`

Ids follow the ledger convention, `REQ-NFR-NNN`, not `NFR-<CATEGORY>-NNN`: the
ledger row regex matches `REQ-<CTX>-NNN` and parses nothing otherwise, so rows
written any other way are silently ingested as none.

Quality attributes get their own requirement context, serialized like any
other — ledger-format workspaces materialize it at the path above — one
category per row from exactly eight:
`performance` · `scalability` · `availability` · `security` · `privacy` ·
`operability` · `maintainability` · `compatibility`; an open list becomes
forty overlapping labels within two passes. Sweep for latent thresholds
(timeouts, pool sizes, retry counts, rate limits, cache TTLs, payload caps),
but first check whether the behavior **already has a requirement** — an NFR
row for a threshold another row owns is a duplicate wearing a different id.
Every remaining bare constant becomes a candidate held as a question —
`BLOCKED` on the only thing that matters: is the value a target, a measured
limit, or the first number someone typed? A `timeout: 30_000` gives the
value, never the intent, and asserting intent from a constant is the same
failure the `DERIVED` hold exists to prevent.

**D5 — when phase D is done:** the five documents present or explicitly
folded, every integration carrying a failure entry or an open question,
every ADR at `observed`, the NFR sweep run with every bare-constant row held
as a question, and **citations resolve** across all of it — checked, not
trusted. Phase D is idempotent — ADRs key by slug, NFR rows by the
`file:symbol` the threshold lives at — and it measures nothing: configured
thresholds are read, latencies are never invented, and threat models are a
person's to write, seeded by `22`, never substituted by this pass.

## Relation serialization

A relation this pass declares must reach the graph. Write it in the Candidate
packet, in one of these exact shapes — the reader parses ids that follow the
verb, and nothing else:

```
Proposed relations (CANDIDATE): requires SR-KERNEL-030, SR-KERNEL-031.
```

```
Proposed relations (CANDIDATE): serves UR-KERNEL-002.
```

`requires` names the rows that take this one as their parent; `serves` names
this row's parent. A row may also carry its parent in a dedicated field — a
`UR` ledger column, a bare `UR-…` in the `Source` cell, or a detail bullet:

```
- **UR:** UR-KERNEL-002
```

A dedicated field always wins: a packet sentence never overwrites a parent an
author stated in a field of its own.

Prose that merely mentions a requirement is a citation, not a relation, and is
read as none — `Conflict — SR-KERNEL-033 records that …` declares nothing.
Naming an id no row carries is a corpus defect and is reported, never dropped.

**Do not invent a shape.** Three estates have been synced with every row
orphaned because a pass wrote parents in a form the reader did not parse
(REQ-CROSS-076, SR-SY-1402, REQ-CROSS-286). The forms above are the contract
between this skill and `internal/rdd`, and a test asserts that every example in
this section parses. A new shape needs a reader in the same change.

Declaring nothing is a real answer, and a common one: most derived rows have no
parent to propose. Say so by writing no relation clause — not by inventing a
plausible one. The pass reports how many rows declare a relation against how
many exist, and that ratio is a quality signal about the derivation, not a
number to inflate.

## Confirmation — the only exit for a candidate

Build exact confirmation gates per `PROCESS.md` §Strict human transitions:
the candidate packet and exact confirmation scope complete before the gate
opens, each gate carrying the standard brief. One human answer may cover
explicitly named candidates — batch confirmation over a context's candidates
is the expected shape; a gate per row is a denial-of-service on the person
this pass is meant to help. Never batch by wildcard or range: the gate names
every id it covers.

Apply answers exactly as `PROCESS.md` routes them:

```text
confirmed accurate as-built --> PENDING_VERIFICATION
confirmed/corrected intent ---> PROPOSED
rejected ---------------------> OBSOLETE
```

Confirmation proves the requirement exists. It does not approve entry, make
candidate links authoritative, prove behavior, or select a release — a
derived corpus describes what already ships and is never stamped into an
active release.

## Handoff

Confirmed scope enters the standard loop and this orchestrator's job ends:
`rdd-plan` → `rdd-cold-review` → `rdd-entry-review`, then `rdd-verify` for
`PENDING_VERIFICATION` rows or `rdd-build` for `PROPOSED` behavior — those
passes own the advance to `IN_REVIEW` — then `rdd-completion-review`.
`rdd-start`'s session discipline binds throughout: run the project's
deterministic process checks (for ModernPath workspaces, `modernpath check`)
before every commit of derived records, chained so a failure stops the
commit. Comprehensive is reached by repetition — one bounded
context per pass, confirmed and handed off, until the context map lists no
context without records — not by one enormous unreviewable pass.

## This pass never

- creates or advances acceptance content, tests, implementation,
  verification, or delivery while its candidates are `DERIVED`;
- assigns `PENDING_VERIFICATION`, `TODO`, or any status past `DERIVED`
  without an applied human answer;
- makes a candidate relation authoritative, or invents a parent to complete
  a trace;
- commits derived items to a release;
- edits product code — a pass that edits code can be reviewed as neither
  documentation nor a change;
- guesses a business rule to avoid recording an open question.

## Coverage floor — the pass is not finished at 59%

Before reporting, run the workspace's own instrument and read the number it
gives, not one of your own:

```
modernpath coverage
```

**A pass below 60% file coverage is incomplete, not merely modest.** It means
the corpus describes what the system exposes — routes, pages, tables — and not
what implements it. Entry points are the easy half: a route handler is named in
one place and reads like a requirement already. The modules behind it are where
behaviour actually lives, and a corpus that skips them cannot support a change.

Two rules follow:

- **Derive against the implementation layer too.** A `lib/`, `services/`,
  `domain/` or `internal/` module that holds a rule, a calculation, a state
  transition or an integration is behaviour a requirement must name and cite.
  Reaching a directory only through the handler that calls it does not cover it.
- **Read the per-app breakdown, not just the total.** One directory sitting far
  below the rest is the gap; a healthy total can hide it. Report each app's
  number, and treat a low one as a finding with a reason — "these 81 modules are
  presentation-only" is an answer, silence is not.

The instrument counts what git accounts for, so a vendored or gitignored tree
never inflates or deflates the result. If the number still looks wrong, say so
and show the breakdown rather than quietly adopting a denominator that flatters
the pass.

## Report

Report the contexts inventoried and the one derived; every denominator class
as `N/N` with misses and stated-inapplicable classes; candidates created by
kind; conflicts, open questions, and backlog discoveries routed; the audit
result over the pass's own output; the confirmation gates now open and the
exact ids each covers; and which contexts remain, with the one this pass
would take next.
