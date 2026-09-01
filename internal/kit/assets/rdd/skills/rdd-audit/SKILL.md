---
name: rdd-audit
description: Audit a document corpus, a ledger, or a check you wrote — resolve citations, diff an inventory both directions, judge a count, and recognise how an instrument fails. Use when verifying that documents still describe the code, when a measurement disagrees with a previous one, when writing or widening a matcher, or when deciding whether a finding is real before acting on it. A shared utility invoked by rdd-reverse-engineer, rdd-cold-review, and rdd-completion-review — it establishes facts and findings, routes them through rdd-triage, and never assigns lifecycle state.
---

# Auditing what a document claims

A shared utility, not a phase. `rdd-reverse-engineer` invokes it before
claiming coverage, `rdd-cold-review` and `rdd-completion-review` invoke it to
verify citations and inventories against the code, and any pass correcting a
stale claim may load it alone. Its contract:

- it establishes facts about documents, records, and the instruments that
  check them; it never assigns or advances a lifecycle state;
- its findings are discoveries — route them through `rdd-triage`, which owns
  where they land;
- invoked inside the loop it is scoped to the affected surface; the
  full-corpus sweep is a deliberate act — at adoption, before a release, or
  when `rdd-start` reports drift — never an every-iteration cost.

Read the project `AGENTS.md` and the canonical `PROCESS.md`
(`.modernpath/rdd/PROCESS.md` in a consuming repository) for the authority
and reconciliation rules these checks serve.

Two sections, and they answer different questions. **Citations** asks *does this
reference resolve* — mechanical, and the place a pass first writes a checker.
**The audit** asks *is this document still true, and is my check trustworthy* —
which is mostly about not believing your own instrument.

---

## Citations — does every reference resolve?

Every `file:line` you wrote must resolve. This is mechanical, it takes seconds,
and it is the cheapest guard against a ledger that reads well and points nowhere.

**Run the script; do not re-derive it.**

```bash
# beside this skill; installed at .modernpath/rdd/skills/rdd-audit/ in a consuming repository
node .modernpath/rdd/skills/rdd-audit/audit-citations.mjs        # docs/ + ARCHITECTURE.md
node .modernpath/rdd/skills/rdd-audit/audit-citations.mjs tasks  # or any root
```

It sweeps both citation shapes over every markdown file under the given roots,
resolves by path suffix against `git ls-files`, flags elided paths, excuses named
gaps, and exits non-zero on failure. The rest of this section explains *why* each
of those rules exists — read it when the output surprises you, and when you are
tempted to write your own.

**That temptation is the point.** This procedure was re-derived by hand five times
in one session and was wrong three of those. Each rewrite looked correct and
produced a confident number. The rules below are the scar tissue:

**Skip named absences.** A row that says *"no test — there is no
`test_draft_service.py` in the repository"* is naming a gap, which is the most
useful thing a derivation pass produces. It is not a citation, and an audit that
counts it as broken teaches the next pass to stop naming what is missing
(8 of 8 "broken citations" in a 3,222-citation ledger were
absences stated correctly). Ignore any reference preceded by *no*, *missing*,
*there is no*, or *does not exist*.

**And resolve paths properly before reporting a failure.** A citation written
`db/models.py:1483` may live six directories deep; a bare `admin.py` may match two
files, only one of which is long enough. Match on path suffix, accept if **any**
candidate satisfies the line, and search the whole repository rather than one
subtree. Three separate audit scripts written in one session each reported the
ledger as broken when the resolver was at fault. Prove your
checker on a citation you know is good before you trust its failures.

### How an instrument fails, and in which direction

#### Reading what the instrument told you

**A near-total failure rate is a broken instrument, not a discovery.** Before
believing a result, ask what fraction of the population it condemns. A check that
indicts 100% of anything has found a convention it does not understand. Near 10%
the balance tips and a finding becomes likelier than a bug. The converse holds: a
check that passes *everything* on its first run has usually matched nothing —
which is why a new check earns trust by being made to fail on purpose.

**A module path is not a table name.** A `compliance/` directory prefix is not
part of `schema "change_plan_messages"`. **Read the declaration, never the
filename** — the `schema "…"` line for Ecto, `__tablename__`/`@Table` for an ORM,
the DDL for SQL.

**A shell utility that fails on your data reports an empty result, not an error.**
`sort` exits on non-UTF-8 bytes and swallows its input; the pipeline prints a
clean, wrong, empty answer. Run text sweeps under `LC_ALL=C`, and treat *"none
found"* with the same suspicion as a 100% failure rate.

**An overstated gap makes the wrong decision for you.** A gap measured at 52
files across four subtrees was filed as too big to fix; re-measured with the
population defined it was 12, and closed the same day. **Audit the numbers that
license inaction hardest** — an overstated gap buys a permanent deferral, while
an understated one is corrected the moment someone starts work.

**A count with an undefined population is not a measurement.** One question — *how
many cited tests name their requirement?* — gave 12, 35, 45 and 123 across four
bug-free runs, differing only in what counted as a cited test. **State the
population next to the criteria**, and when a re-measurement disagrees with a
recorded one, **suspect the population before the matcher**: a factor-of-ten
spread is what a definition disagreement looks like.

**Report the instrument's count and the verified count separately.** "35
mismatches" implies each was inspected. Say *upper bound*, and name the ones that
were.

#### Changing a matcher

**Widening a matcher to fix a false negative is the moment to write the
false-positive test** — not after. Each widening below let something through that
must not pass, and every one was caught by adding the negative case, none by
re-reading the regex:

| Widened to accept | Also accepted, wrongly |
|---|---|
| `APPROVED` anywhere on a line | `SPEC-APPROVED` — a different gate |
| any heading containing "approval" | `## Pre-approval audit` |
| every line of a section | rows belonging to the other gate |

A widened pattern reads as *"now it accepts X"*; the question that matters is
what **else** it accepts. Write the shape you must still reject, and watch it fail.

**Two implementations with *different* fixtures beat either suite alone.** A
parity mirror or a port is justified as a compatibility requirement; its quieter
value is that its tests were written by someone solving the same problem from
another angle, so it holds fixtures the original never thought to write. When a
widening left one suite green and failed the mirror on a case the first lacked,
the disagreement was the only detector. **Run the mirror's tests before your own
conclusions**, and copy back the fixture that surprised you.

**A widening that clears more than you expected is a warning, not a result.** If a
change fixes more cases than the one you were chasing, find out which extra ones
moved before booking them.

**Be most suspicious when the new behaviour is a pass.** A false negative annoys
someone; a false positive silently asserts that a thing was checked.

#### When the tool says nothing, or says it sideways

**Do not let display truncation become evidence.** A check that printed cited
lines through `cut -c1-46` made six correct citations look wrong. When a check
disagrees with a document, **widen the view before you edit** — print the whole
line, and confirm with a second method sharing no code with the first. Agreement
between `grep -n` and `awk` means something; agreement between a script and its
own truncated output means nothing.

**Read a checker's first run as a test of the checker, not of the corpus.** Four
checks each found a bug in *themselves* on first contact with real data: an
exemption applied before resolving (skipped 116 resolvable citations), an elision
gate whose hits were prose, an escape fix that collapsed counts 2220 → 483, an
anchor matcher that mangled its own character class. None was found by re-reading
the code. Budget the first run for debugging the instrument.

**Silence is not agreement.** A command producing *no output* has not been shown
to have run — a fixture using an unscanned extension reports `0/0`, a gate run
from the wrong directory reports *"checks pass"*, a command inside a broken `&&`
chain never executes. Verify the **effect**, not the exit code, and never pipe to
`tail`: the last two lines of no output are no output.

A gate proves what it checks, never that it was reached.

#### Which direction to distrust

**Both directions of failure happen; only one is self-correcting.**

| | What it does | How it ends |
|---|---|---|
| **False alarm** — a regex matching `.ex` inside `.exs`; a truncated display | sends you *to* the evidence | caught within minutes — you open the file to fix it and the code disagrees |
| **False pass** — a sweep over a subset reporting `56/56`; an exemption applied too early | sends you *away* from the evidence | survives until something unrelated exposes it; several persisted for months |

Budget suspicion asymmetrically. A false alarm costs one round trip and pays for
itself. A false pass costs nothing today and everything later, and **nothing in
your own workflow will surface it** — the only reliable detectors are a second
implementation and a number that does not match the change.

**Stop when the evidence explains *why*, not when it establishes *that*.** A
finding whose meaning changes as you look closer has not finished changing. One
sequence ran: *93 rows stranded on the server* → *but the ids are in `tasks/`* →
*so the builder is dropping rows* → *they were retired deliberately, and the data
model says so in its own headings*. Each reading pointed at a different action;
the one that held explained why the state exists. **The most alarming reading was
third of four, and it was wrong** — alarm is not evidence of depth.

**Splitting findings into "certain" and "ambiguous" puts the danger in the wrong
bucket.** The ambiguous pile gets read carefully because you cannot act on it; the
certain pile gets *acted on*, so a detector error inside it goes straight into an
edit. → **Before acting on the confident half, re-derive it once with a stricter
matcher.** If the count drops, your certainty came from the tool.

#### What the pattern never saw

**Count what your pattern did *not* match — a loose pattern does not over-report,
it stops looking.** Citations use a grammar: `file:N`, `file:N-M`, `file:N,M`,
`file:N-M,P`. A pattern matching `file:(\d+)` matches all of them and reads only
the first number, so `466,480` scores as one citation and 480 is never examined.

1. **Write down the grammar before the regex.** If the data has lists, ranges or
   optional parts, enumerate them.
2. **Measure coverage, not just hits.** If a line holds four numbers and your
   matcher reports one, that gap is the finding.

**Report the result as a fraction (`140/140 resolve`), and make the denominator
the whole corpus.** A pass that greps only its own `CODE:` prefix measures the
citations it thought to prefix — one corpus reported `56/56` while carrying 11
bare `` `adapter.ex:48` `` references no audit had ever seen. Sweep both shapes,
resolving bare ones by path suffix against the tracked file list.
### What the sweep must cover, and what it still cannot tell you

**And sweep every document, not the ones this pass wrote.** The subset error
recurs one level up, and it is easy to miss because each fraction looks complete.
The same corpus reported `56/56` over the derived set, then `67/67` once bare
references were counted, then **`119/123`** once the sweep covered `docs/**` — and
the four failures were in a document no earlier audit had opened, because it was
not part of the derived set. They were **elided paths**: `CODE:.../ai/proxy.ex`,
written by an author who knew where the file was and left the reader a citation
that resolves to nothing.

```bash
grep -rn 'CODE:\.\.\.' docs/     # elided paths — expect no output
```

Elision is worth a check of its own because it is invisible to a reader and to a
naive resolver alike: the line *looks* like a citation, and a suffix-matching
audit can even accept it if it strips the dots. Write the path from the repository
root, every time.

**Resolving is not being right, and the gap between them is where rot lives.**
Every check above is structural: the file exists, the file is long enough. A
citation can pass all of them and point at a line that no longer says what the
claim says. a corpus reported `50/50 resolve` while one
document quoted a per-file cap of 15,000 characters that applied only when the LLM
proxy was **off** — the code selects between two budgets, and the smaller one
(8,000) governs any deployment using the proxy. The citation resolved perfectly.

**So cite the line that carries the claim, not the definition that contains it.**
That document cited a function's `defp` head while quoting an expression three
lines inside it. Pointing at the head is what let the quote go unchecked — nobody,
human or script, could compare the claim to the cited line, because the cited line
was a signature. When you quote or paraphrase a specific expression, cite *its*
line; reserve the head for claims about the function as a whole.

Then one more check becomes possible and worth running: **for every citation where
the prose quotes code, assert the quoted text appears in the cited range.** That is
still mechanical, and it catches the drift the existence check cannot.

**A token-overlap heuristic is a triage list, not a gate.** Comparing symbols named
in the prose against tokens on the cited line flagged 14 of 52 citations in that
same corpus; 13 were false — the surrounding text was a table, and the "prose" was
a neighbouring cell. One was real, and it was the one above. Run it to decide what
to read, never to decide what to report; a checker with a 93% false-positive rate
teaches the next pass to ignore it.

---

## The audit — is the document still true, and is your check trustworthy?

Everything below applies to any document this process produces or inherits — a
recovered design corpus, an adopted `ARCHITECTURE.md`, per-context documents,
and the serialized process records. It is written as one section because these checks are a single
discipline, not per-document advice.

**Adopting is not accepting. Audit what you adopt.** A maintained document is
maintained *as of some date*, and the code moved after it. Before you adopt one,
run at least one **countable** check — a set the document enumerates against the
same set in the code:

- tables it lists versus `__tablename__` declarations
- services it names versus what the compose file and CI actually build
- endpoints it documents versus the routers on disk

On one real corpus that check took two commands and found a `Database Schema`
section that was **6 tables short of the code and named 2 that had been dropped**
— the document was edited 2026-06-10 while the services changed through
2026-08-03. A reader trusting it would have looked for a table the migrations had
deleted.

Then **extend it in place** with what you found, and say in the document that you
did, with the date and what you reconciled against. A silent correction leaves
the next reader unable to tell which parts have been checked.

**Two commands that do this well:**

```bash
grep -oE '__tablename__ = "[a-z_]+"' <models> | sed 's/.*"\(.*\)"/\1/' | sort -u > /tmp/real
# extract the same set from the document, then:
comm -23 /tmp/real /tmp/doc   # in the code, undocumented
comm -13 /tmp/real /tmp/doc   # documented, no longer real — the sharper finding
```

### Groundedness first, then correctness

**Measure groundedness before you measure correctness — `0 broken` on `0
citations` is not a pass.** A citation audit reports what it found wrong among
the claims that can be checked. A document that cites nothing cannot be wrong by
that measure, and will score perfectly.

Run this first, and read it as the headline result:

```bash
docs=$(ls docs/*.md docs/**/*.md 2>/dev/null | wc -l)
cited=$(grep -rl 'CODE:' docs/ 2>/dev/null | wc -l)
echo "$cited of $docs documents carry at least one citation"
```

Measured across three real corpora, which is the range to
expect:

| Corpus | Documents | With ≥1 citation | Citations |
|---|---:|---:|---:|
| derived by this process | 26 | 22 (84%) | 1,003 |
| a workspace's own design docs | 48 | 9 (18%) | 48 |
| a repository with 21,410 code files and no derivation pass | 36 | **0 (0%)** | **0** |

The third is the case to recognise. Thirty-six documents describing a substantial
system, none of them making a single checkable claim about it — and the citation
audit returned "0 broken", which looks like health.

**Audit a document's own status tags — they are a promise, and they are
machine-checkable.** A design document that marks each item **EXISTS** or
**TODO**, **built** or **planned**, is making a per-claim assertion far sharper
than its prose. That tagging is usually the most useful thing in the document and
the least maintained.

One audited API contract carried 69 endpoints tagged EXISTS: **five
were not in the router at all**, their only matches being LiveView modules rather
than JSON routes. A team building a frontend against that contract would have
discovered the gap at runtime. The other 64 EXISTS tags and all 65 TODO tags
held — so the document was 96% right, and the 4% was concentrated exactly where a
reader would act on it.

Note which question actually paid. The first comparison asked *"is this endpoint
in the router?"* and returned 40 of 138 missing — mostly the document's own TODOs
and scope-prefix noise. The useful question was **"is this document's claim about
itself true?"**, which is answerable, small, and directly actionable.

**Design documents and derived documents are different genera, and the audit only
speaks to the second.** A document written *before* the code says what should be
true; a derived document says what is. Do not "fix" the first kind by hanging
citations on it — check instead whether the system it describes was ever built
that way, and record the answer as findings. An uncited design corpus over a
large codebase is a **drift question**, not a formatting defect.

**Expect the ratio to be alarming and mostly fine.** A "documents with zero
citations" query over one workspace returned **48 of 57**, and
nearly all of them were right to have none — API contracts, a ubiquitous-language
glossary, feature specs, product positioning. Sort by genus before you react, or
the number will push you into hanging citations on documents that precede the
code, which destroys what they are for.

**Among the derived ones, though, uncited predicts wrong.** In the same corpus the
grounded derived documents had almost no defects; the one derived document with
**zero** citations had three, all in the direction that misleads someone doing
real work — two endpoint paths listed without the mount prefix they are registered
under, and two endpoints missing entirely. Nobody had checked, because there was
nothing to check against. Ground a derived document and you are not tidying it;
you are running its first test.

**And watch the citation form itself.** Two citations in that corpus were written
`CODE: path` with a space, which every checker's regex missed — so they were never
verified by anything, in a document that declares itself *generated from code*.
One of the two resolved only by suffix; the path as written did not exist. A
convention followed 130 times and broken twice is broken invisibly.

### Citations: whole, rooted, and re-checked

**A citation must resolve, which means it must be whole.** Never elide a path.
`CODE:.../job_processor.py:265` reads tidily and is worthless: a reader cannot
open it and a checker cannot verify it. Write the repository-relative path in
full, every time, however long.

Run the shipped audit over the corpus before you call a pass done — it reads
every citation in every document in seconds:

```bash
node .modernpath/rdd/skills/rdd-audit/audit-citations.mjs           # docs/
node .modernpath/rdd/skills/rdd-audit/audit-citations.mjs tasks epics process
```

**This section used to carry an inline Python re-implementation, and removing it
is the point.** That snippet swept `docs/**`
only, matched the `CODE:` prefix and not bare `` `file.ex:12` `` citations, held
two line-parts so everything after the second in `file:N,M,P` was invisible, and
closed by claiming *"a failure is always real"* — which five separate false alarms
in one session disproved. Every one of those flaws was fixed in the script and
would have had to be fixed again in the snippet.

A skill that tells a reader to hand-roll the check it also ships is not offering
a choice; it is offering the version nobody maintains.

### The runbook, the published copy, and being findable

**Audit the runbook — the lines a reader will type.** Ports, URLs, commands and
env var names are the most checkable claims in any document and the most
expensive to get wrong: a wrong architecture paragraph misleads, a wrong port
wastes an afternoon and gives no clue whether the reader broke the setup or the
document is lying.

Take every URL and port the document names, and confirm each against what the
compose file or deploy config actually **publishes** — not what a service listens
on internally. The two are different, and documents routinely conflate them.

One *Access points* table listed `http://localhost`,
`http://localhost:8082` and `http://localhost:8083/health`. The compose file
published exactly two ports and the strings `8082` and `8083` appeared **nowhere
in it**; both application services declared no `ports:` at all. Every URL but the
database was fiction, and the service sections above repeated it.

A one-line check catches this class:

```bash
grep -oE 'localhost:?[0-9]*' <doc> | sort -u      # what the document promises
grep -oE '"[^"]*:[0-9]+"' docker-compose.yml       # what is actually published
```

**"Listens on" is not "reachable".** A service with an internal port and no
published mapping is reachable only from inside the network. Say which it is —
the reader's next command depends on it.

**Audit the published copy, not just the file.** A document that syncs to a
platform now exists twice, and the copy a reader opens is the remote one. Sync is
usually hook-driven and fire-and-forget, so a failed push leaves the repository
right and the platform stale with nothing raising a hand.

Compare the far side after a pass. Content length is enough to catch drift
without pulling every document back:

```sql
-- the shape of the check, against whatever table holds the synced copies
select external_id, length(content) from system_documents
where external_id is not null order by external_id;
```

then diff those against the files on disk. In one checked sync, 21 documents
across two systems matched exactly — which is the result to expect and worth
recording, because the interesting version of this check is the day it does not.

**The failure this guards against is silent by construction.** Earlier the same
day, a server rejecting *every* batch produced no error anywhere a person would
see it; the workspace looked fine and the platform was hours behind. A length
comparison would have shown it in one query.

**A document nobody can find has not been written.** After producing or
superseding anything, update the corpus index — usually `docs/00-overview.md` or
a README table — and then check the index against the filesystem:

```bash
ls docs/*.md | xargs -n1 basename | while read f; do
  grep -q "$f" docs/00-overview.md || echo "unlisted: $f"
done
```

One pass had written five documents, five ADRs and a guide,
synced them, and audited them — and listed **none of them** in the index, which
still routed readers to a document the same pass had marked superseded. The
corpus was correct and unreachable.

**An index row carries a status, not just a name.** *Exists* and *is in force*
are different facts, and a filename alone conveys the first while implying the
second. One index gained nineteen rows; reading each document
to write its row revealed that one was a **tombstone** — its content had moved
months earlier and the file remained as a pointer — and another was an explicit
**proposal that was never ratified**. A reader meeting either as a bare filename
would have taken it for current guidance.

Use a status vocabulary that distinguishes them — `derived`, `decided`,
`proposed`, `superseded`, `moved`, `living`, `operational`, `report` — and take
each row's purpose from that document's own heading and opening line. That keeps
the job mechanical enough to finish, and stops the index asserting things the
documents do not.

Two cautions from running it. **Wildcard rows are invisible to exact matching**:
a `10–17 | 10-<domain>.md …` row legitimately covers eight files, and the naive
check called all eight missing — inspect the hits. And **listing is not
describing**: adding a filename with no one-line purpose makes the index longer
without making the corpus navigable, so where a backlog of unlisted documents
exists, record it as editorial work rather than padding the table.

### Staleness travels in groups

#### Enumerate, then diff

**Audit an inventory against the thing it inventories.** A document that lists
*what exists* — a structure table, a context register, an endpoint table — makes
a claim no other check can reach. Citations resolve, counts match, prose stays
coherent, and the list is still missing things it purports to enumerate. Nothing
contradicts an absence.

Three instances in one session, each found this way and by
nothing else:

| Inventory | Checked against | Found |
|---|---|---|
| `00`'s repository structure table | the filesystem, and `AGENTS.md` | **4 of 9 subtrees missing**, including the decided auth boundary |
| an overview's *"the pass seeded one context"* | `ls tasks/*-REQUIREMENTS.md` | **eleven** ledgers existed |
| an architecture note's endpoint table | the router's mount block | 2 paths wrong, **2 endpoints absent** |

**Enumerate the real thing, then diff the document against it — not the reverse.**
Reading the document and checking each entry exists finds *wrong* entries and
never *missing* ones, and missing is the half that rots.

**"The real thing" is itself a derivation, and the first one is usually wrong.**
Enumerating one estate's tables gave **171** from `create table(`; the
real figure was **112** once 59 drops and 7 renames were applied — a 53%
overstatement that would have manufactured a finding. And 112 was still the wrong
denominator: the document claimed to map *Ecto schemas*, of which there were
**148**. **Enumerate the population the document claims to cover, not the one
that greps most easily** — and for anything with a history (migrations, changelogs,
event logs) the current set is creations *minus removals*, never creations.

**A missing entry has three degrees, and only one is a defect.** Reporting all 49
omissions as "undocumented" would have been false and would have buried the part
that mattered. Separate them before writing a word:

1. **In a sibling document, absent here** — the two disagree; say which is more
   current (29 of them; `02` was ahead of `40`).
2. **Specified elsewhere, but in no index or map** — the spec exists and the map
   never caught up; point at its real home rather than restating it.
3. **Nowhere at all** — the only genuine gap. Six of the forty-nine, and the
   only ones worth a requirement.

Collapsing these into one number is how an audit becomes noise: a reader who
checks two entries, finds them documented elsewhere, and stops will discount the
whole finding — including the six that were real.

#### Scoping an audit honestly

**Before writing "not checked in this pass", price it.** Once a
banner was written saying the reverse comparison had not been run — and running
it took one command and found two real errors. The known-unknown note exists for
audits that would be **expensive or noisy**, not for ones a minute would settle;
used as a default it turns honest scoping into a licence to skip. The test is
cheap to apply: if you can state precisely what the check would be, you are
usually already most of the way through doing it.

**Sometimes the right call is not to run the diff — then say what that leaves
unchecked.** A realtime catalog scoped itself to *cross-context* events; the code
carried 59 message atoms across 111 broadcast sites, most of them intra-context
progress. Diffing those would have produced dozens of false gaps and taught
everyone to ignore the next audit. Declining was correct. **Silence about it was
not** — the document now names the narrower claim nobody has verified: that the
catalog is *closed*. An audit you chose not to run is a known unknown, and
writing it down is the difference between scoping and quietly implying coverage.

**An audit banner suppresses the next audit, so record which direction it ran.**
One API contract carried a prominent *"Tag audit:
69 endpoints tagged EXISTS, of which 5 are not in the router"*
— specific, honest, recent, and one-directional. It checked each documented entry
against the router and never the router against the document. Enumerating the
router found **264 routes across 42 prefixes** and a *"read first"* paragraph
claiming six whole contexts had **no JSON API**; all six had one, and the
controllers predated the audit by a month. The banner is why nobody looked: a
document that says it was verified reads as verified. **Write what was compared
against what** — *"every EXISTS tag checked against the router; the router was
not checked against this document"* — so the next pass knows which half is
unexamined instead of inferring both.

**And weigh the two directions differently.** A documented-but-absent entry fails
loudly the first time someone calls it. A real-but-undocumented one fails as
silent duplicated work — six contexts' worth of backend scheduled that already
existed. The cheap direction is the one that gets audited; the expensive one is
the one that needs you to enumerate.

For an HTTP inventory specifically, three things make the enumeration wrong if
you skip them: parse **scopes** so nested prefixes compose into full paths, drop
**comment lines** (a commented-out route reads as live), and remember that a
framework's non-verb route macros — Phoenix's `live`, mounts, forwards — are not
matched by a verb regex and are not JSON either. Getting any of those wrong
changes the count by enough to invent or hide a finding.

#### After you correct something

**A stale claim is rarely alone — grep for it after you correct it.** A document
states the same fact in more than one place: once in prose and once in a "key
patterns" list, once in an overview and once in a table. One
wrong migration mechanism was corrected in a schema section while an identical
claim sat forty lines below in a patterns list, and shipped uncorrected. After
every fix, search the corpus for the distinctive phrase and the thing it names.

**A correction banner is not a correction, and it is worse than none.** A
runbook was once corrected by adding a *"superseded in part"* note
to the section that described a removed credential — and two later paragraphs in
the same file went on describing it as in use, one of them in the Notes section a
reader actually lands on. The banner made the document look maintained, which
made the surviving stale claims look reviewed. Either correct every instance or
don't signal that you did.

**And the fact escapes the document.** The same retired credential was still
being handed to operators by `.kamal/secrets-common.example`, which told them to
mint it and set it. A fact lives in prose, in the example config that
operationalizes it, in templates, and in the code that reads the variable — and
correcting the prose is the easiest of the four. After correcting a fact, grep
the **repository** for the identifier, not the document for the sentence.

That sweep is also where the real finding usually is. Chasing which of two
contradictory paragraphs was true is what surfaced that the credential was gone
from the *deployment* and still live in the *code* — a difference neither
paragraph stated, and a better result than picking a winner between them. **Treat
a document contradicting itself as evidence about the system, not as a typo.**

**Audit the whole document, not the section you came for.** The same pass that
found the schema section six tables short stopped there — and the service
sections, unexamined, said "four job types" where the enum had five. A document
drifts uniformly; a section that is stale is evidence about its neighbours, not
an isolated defect.

**And be as ready to be wrong as the document is.** In the same audit the pass
suspected `google-genai SDK directly` was stale, because the dependency manifest
listed `spaik-sdk`. Both were true: the backend goes through `spaik-sdk`, the
worker imports `google.genai` directly, and the *pass's own* document was the one
with the incomplete claim. Check the code before correcting a document — the
existing text may be recording something you have not found yet.

#### Claims that age without being edited

**A claim with a shelf life carries the moment it was true.** Counts, hashes,
versions, "currently N" — these are measurements, not facts, and a document that
states them bare will be wrong without ever being edited.

Two ways to keep them honest, in order of preference:

1. **Write the invariant, and let the instance illustrate it.** *"A credential
   ping and a generation call carry different timeouts by design"* survives a new
   vendor; *"six of the eleven adapters use 10 seconds"* does not.
2. **Where the number is the point, date it** — `RUN:<date>` in the same
   sentence, and say which direction it moves. *"88 of 132 paths resolved when
   measured, a figure that falls as checkouts are pruned"* tells a reader both
   what was true and how to think about it later.

A byte-hash asserting that three files are identical is the sharp case: the
durable claim is *"a lockstep test fails if they drift"*; the hash is evidence for
a moment and needs its date beside it.

**Traceability is bidirectional, and only one direction is ever checked.** A
ledger row citing a test is audited to death — the path resolves, the line exists,
the extension is right. Whether the **test** names the requirement is checked by
nothing, and that is the direction that survives the ledger being reorganised: a
test carrying `REQ-PLN-061` in its `describe` can be traced back from the code
even if every ledger row is rewritten.

Measure it, because the number is not what you expect. On one measured workspace,
**129 of 181** cited test files named a requirement that cites them — **52 did
not**, in a workspace whose ledger citations were 100% resolving.

Two cautions, both learned by getting it wrong. **Recognise a test by its path or
suffix**, never by the word *test* appearing in a filename: the first run counted
58 because it matched `specification_pipeline.ex` and `test_execution.ex`, which
are implementation. And **naming any one covering requirement is enough** — a test
covering three requirements does not need three ids, and demanding that turns
traceability into bookkeeping nobody maintains.

**Abbreviation is how a correct citation becomes a broken one.** Sweeping 1,359
file paths across a workspace's ledgers found **two**
failures, and neither was stale — both were *shortened*. One dropped a directory
segment (`controllers/app_token_controller.ex` for
`controllers/auth/app_token_controller.ex`); the other dropped a filename prefix
(`bridges_test.exs` for `work_events_bridges_test.exs`), in a sentence that had
just named `bridges.ex`, so the shorthand read naturally to whoever wrote it.

That is the failure mode to expect in a mature corpus. A citation is rarely wrong
because the file moved — a move breaks a build. It is wrong because a writer
mid-sentence wrote the part a human reader would need and dropped the part a tool
needs. **Paste paths; never retype them**, and be most suspicious of the second
mention of a file, where the writer already has the context and the reader of the
tool does not.

**Every citation resolves from the repository root**, not from wherever the code
felt close. In a monorepo with vendored subtrees this is not pedantry: in one measured workspace
**19 of 40** citations in one workspace's documents were written
relative to a subtree — `apps/core/lib/...` — which exists only under the
vendored subtree's own root. They read correctly beside the code and
cannot be opened by a reader at the root, which is where the document lives.

Set the checker's working directory to the repository root and let it fail
loudly; a citation that only resolves after a human guesses the prefix is not a
citation.

### Your own workspace, and documents against each other

**But not on the prompts — and the tool now refuses.** Teaching material quotes
citations that must not resolve: an elided path shown as the thing *not* to
write, a template row citing `domain/user.ts`, a finding cited from the
repository it was found in. Pointed at `.claude/` or the installed `.modernpath/rdd/skills/` the audit reports broken and
elided paths that are all correct prose, and the obvious next move deletes the
lesson. It exits 2 with an explanation instead; `--force-prompts` overrides it for
a reader who will inspect every hit.

That refusal was priced before it was written, not assumed: **132 backticked
paths across the prompts, 21 unresolved, all 21 legitimate** — templates, other
repositories, and paths the skill *instructs a pass to create*. Four looked like
real errors until opened. Nothing was being missed.

**Run this audit on your own workspace, not only on the one you are analysing.**
The same pass that fixed elided citations in a client repository had left three
of them, and nineteen subtree-relative paths, in its own — because it had never
pointed the check at itself.

**Line numbers rot faster than paths.** A citation surviving this check proves
the file exists and the line is in range, not that the line still says what you
claimed. Spot-check a sample by reading them back; cite a range when the exact
line is likely to move.

**Cross-check the documents against each other, not only against the code.**
Where two documents in one repository describe the same set, the disagreement
tells you which one is being maintained.

`ARCHITECTURE.md` was six tables short and named two that had
been dropped — while `docs/02-bounded-contexts.md`, sitting beside it, listed
**all 34 correctly**, every one owned by exactly one context. The pass had
adopted the stale document as `03` without noticing the accurate one next door.

So when you adopt, compare the candidate against the corpus first. The document
that agrees with the code is the one to adopt or to extend from; the one that
disagrees is a finding, and often tells you *when* maintenance stopped.

**Read the siblings before you enumerate the code — it is the cheaper audit, and
often the same finding.** One realtime map listed three PubSub
topics that do not exist. Finding it by enumeration meant extracting 111
broadcast call sites across 1,075 files, and the first two extractors were wrong
(one matched progress payloads, not topics). **Two sibling documents already said
all three were net-new** — one called that context "mostly new projections",
another called the feature net-new over a flat column, and the third put the data
in a table column rather than behind a topic. Minutes, not an extractor.

**And weigh agreement by count.** Three documents agreeing against a fourth is
stronger evidence than any single pairwise comparison, and it points at the
outlier without needing the code at all. Use the code to *confirm* the outlier,
not to discover it.

**A check is useful when you can afford to inspect every hit.** That is the
threshold to iterate towards, and it usually takes two or three attempts at the
instrument rather than one.

On one pass, verifying a data model's column claims: the first attempt
checked every backticked token in each row and returned **39 suspects out of
69** — it was reading the writer-service column as if those were column names.
Narrowing to the constraints cell and stripping enum braces gave **33 checked, 3
hits**, and all three turned out to be values rather than columns. Three is
reviewable by hand; thirty-nine is a pile nobody reads, and a check nobody reads
is worse than none because it looks like diligence.

Tighten the extraction until the hit list is short enough to read, then read all
of it. **Never report the raw hit list as findings** — every hit needs a human
look before it becomes a claim.

**Exclude what the language treats as inert, and read the document's own caveats
before reporting a gap.** An extractor that cannot tell live code from commented
code manufactures findings.

One route audit reported three surfaces missing from a document
titled *"every view, its actors, its use case"*. All three were in a commented-out
legacy block — and the document already listed them, with ten others, under a
**Dead surface** heading, citing the exact line range and raising a question about
the two admin routes still live above them. The document was more thorough than
the check.

So before a gap becomes a finding: strip comments and disabled blocks from what
you extract, then search the document for the thing you think is missing —
including its "dead", "deprecated", "not covered" and "open questions" sections.
Documents written by a careful pass usually record their own exclusions, and a
gap that is already named is not a gap.

### Report what held, not only what broke

> How a *matcher* fails — false alarms against false passes, the danger in the
> "certain" bucket, counting what a pattern did not match — is in **Citations**
> above, where a pass first writes a checker. This section is what you then say.

#### Reading a result before acting on it

**Re-read the evidence at the moment you decide to act, not when you measure.** A
count is reported once and reused — in a row, a summary, the next decision — each
reuse further from the instrument. When a number is about to become a change,
**open two of the things it counts.** It costs a minute and is the last point at
which a measurement error is cheap. (A finding of *"3 rows with no question"*
survived measurement, a requirement, a commit and publication; acting on it opened
all three and every one had its question, in a field the matcher could not see.)

**Watch for rules that grew around a bad number.** That finding had already
sprouted an acceptance criterion derived from a population that did not exist. A
wrong count does not merely misreport — it becomes policy, and the policy outlives
the correction unless you go looking for it.

**A cross-reference is a claim, and its qualifier is the load-bearing part.** A
decision listing *"**Resolves:** OQ-101, OQ-102, …"* looks like authorisation to
close them; the same line ends *"(theme T-B **partial**)"*. **Batch-closing on a
cross-reference asserts a completeness its own author declined to claim** — and it
is tempting because it makes a number go down. When a reference resolves N things
at once, read what it says about *itself* first.

**Read the false positives before dismissing them — sometimes they are the
finding.** A sweep scored 146 cited, 144 resolved; the two failures were library
modules the enumeration had not scanned. Dismissing them was correct about the
sweep and would have lost the result: one was `Ecto.Enum`, and a canonical
document stated flatly that `Ecto.Enum` is not used here. Ten schemas use it. Ask
what made a false positive *look* plausible before deleting it.

#### When nothing was wrong

**A clean audit is a result, and needs a banner as much as a dirty one.** Record
what was compared and what was left unchecked — an unbannered document invites the
next pass to redo the work, and *"we checked and it held"* is what stops that.

**A clean audit is the cheapest moment to mechanise the rule it just confirmed.**
The sweep has done the expensive half: it established the corpus is clean, so the
new check goes green on the first run and you never mix enforcement with cleanup.
Add the same check after it breaks and you must fix N violations and land the
guard in one change — which is when guards get watered down to fit the mess.

**And the exemptions come out of the sweep.** When a rule held across 948 rows
with four exceptions, all one status, that status became the carve-out. Had they
been a status the rule should cover, the answer would differ — and no amount of
thinking in advance would have said so.

→ Every time an audit comes back clean, ask what one-line check would keep it that
way, and whether anything enforces it today.

#### Writing the report

**Say what held, not only what broke.** A derived document that passes its audit
is a result, and a report listing only corrections implies the rest was unread.

**A finding needs its counterweight when the counterweight changes how it reads.**
*"Four optional credentials behave four different ways"* reads as carelessness
until you add that the two which would compromise the system fail loudly at boot.
Same facts, opposite conclusion.

**Record what you checked and found correct, not only what you fixed.** Otherwise
the next pass re-derives it, and a corpus accumulates repeated audits of the same
clean thing while the unexamined half stays unexamined.

**A confirmation you wrote yourself is not evidence.** Prove the instrument can
report the other answer before believing this one. The shapes that lie:

- a check reporting **perfection** — feed it something known-broken;
- a check reporting **catastrophe** — a real document is rarely 80% wrong;
- a script printing **"done"** unconditionally;
- a **runtime probe** answering from the wrong state (`function_exported?` before
  the module is loaded);
- an **exit status from the wrong command** — a pipeline returns the last stage's
  status, so `… | tail` reports the success of `tail`.

#### What actually rots

**It is not documents that rot, it is *claims about sets*.** A corpus wrong about
which enums exist scored 146 of 146 on the module names it cited. Named references
hold — someone wrote the name while looking at it, and a rename breaks a build
long before it breaks a document. Quantified claims do not:

| Shape | Example | Ages |
|---|---|---|
| Named reference | *"`Aiengine.Repo` injects the tenant filter"* | slowly — checkable and load-bearing |
| Coverage claim | *"every endpoint named here is in `07`"* | fast — new ones arrive without touching the document |
| Convention claim | *"enums are never `Ecto.Enum`"* | fast and silently — one exception falsifies it |
| Bare count | *"the ~80 Ecto schemas"* | fastest — wrong without anyone editing anything |

**Aim an audit at the quantifiers** — *every, all, none, never, only, the N* — not
at the nouns. Grepping for those finds more rot per minute than reading, and each
hit settles with one command.

**The quantifier is not what makes a claim fragile — writing it without opening
the code is.** The same sweep against a *derived* corpus found every absolute
holding. A derived pass states an absolute only after enumerating, so its
absolutes survive; a design document states one as an intention, and intentions
acquire exceptions silently. That is where to spend an audit first.

Two cautions when running it. Most quantifier hits in a UX or product document are
**prescriptive** — *"never display a secret after creation"* is a requirement, not
a claim about code, and checking it against code is a category error. And scope
the check to the **declaration** you are testing: counting constraints in a window
around a class catches its neighbours.

### Ids, capture, and siblings

**Every id a document cites must be defined somewhere — check both directions.**
An id cited but never defined is a dangling reference; one defined but never cited
is either dead, or a gap in the document that should point at it.

**Scope the check to the ids this workspace owns.** A platform that analyses other
repositories will carry ids belonging to them; a sweep that does not scope reports
those as dangling forever.

**Register by reference, not by copy.** The full statement belongs in one place;
everywhere else cites its id. Two copies of a question diverge, and the reader
cannot tell which is current.

**A finding recorded only in prose is not captured.** An audit ending with a note
inside the document it audited has told the document about itself. A discovery
becomes a ledger row or a backlog line, with provenance, or it is carried rather
than captured.

**Cross-check siblings.** Where two documents describe the same set, the
disagreement tells you which one is being maintained.

### Audit this document too

**A prompt's own counts rot like any other document's.** An earlier version of this material said *"Three phases, in this order"* directly above a block listing
**four** — the sentence was written when there were three and never revisited
when D was added. It had shipped that way to two workspaces.

Sweep your own stated counts against the thing they count, the same way you would
sweep a document you inherited:

```bash
# the leading [^-[:alnum:]] matters: \b alone matches "six" inside "Thirty-six"
grep -onE '(^|[^-[:alnum:]])(two|three|four|five|six|seven|eight|nine|ten)[[:space:]]+(phases|documents|rules|steps|files|ways|copies)' SKILL.md
```

That refinement came from running this very command on this very
file: of 14 hits, one was *"six documents"* matched inside
*"Thirty-six documents"*. It is the same substring trap that has now produced a
false finding three times in one session — `NFR` inside `INFRA`, `Q-AGT-001`
inside `REQ-AGT-001`, and this. **A word boundary is not a word boundary when the
neighbouring character is a hyphen.**

Then check each against its list. Here *"five documents"* matched five rows and
*"three files"* matched the contract's three copies — one defect in four claims,
which is roughly the hit rate to expect from any document nobody has counted.

**Audit this document too — a prompt is a document.** Everything above applies to
the skill itself, and it fails the same ways.

Once a rule was inserted by anchoring on a phrase, and the anchor
sat **inside a numbered checklist**: the new rule became "step 6", its code block
landed between steps, and the two-line step it displaced vanished. Nothing
errored. The skill still rendered. It was found by counting the steps and reading
them back — the same check the sequence itself prescribes.

So after editing any structured document programmatically, verify the
**structure**, not just the text: item counts, balanced code fences, headings
still where they were. An anchored replacement is a blind edit, and prose anchors
are unstable exactly where documents are most structured.

### The audit as a sequence

The rules above are the reasoning; this is the order to run them in. Each step is
minutes, and each one has caught a real defect.

1. **Groundedness.** `grep -rl 'CODE:' docs/ | wc -l` against the document count.
   Zero citations over a real codebase is the headline finding, not a pass.
2. **Citations resolve**, from the repository root, no elided paths. The
   shipped script does the whole corpus in seconds.
3. **Read a sample back.** Five citations, opened, compared to the sentence that
   cites them. Resolution is not correctness.
4. **One countable set per document** — tables against `__tablename__`, routes
   against the router, ports against what compose publishes, commands against
   `package.json`. Probe the check with a value you know is absent.
5. **Status tags.** If the document marks claims EXISTS/TODO or built/planned,
   verify those — they are the sharpest promise it makes.
6. **Ids resolve, and siblings agree.** Every `Q-`/`REQ-`/`ADR-` id cited is
   defined somewhere; where two documents describe the same set, the
   disagreement names the stale one.
7. **The runbook** — every URL, port and command a reader will type.
8. **The index.** Every document listed, with a status; every listed document
   present.
9. **The published copy**, if the corpus syncs anywhere.
10. **Record what held**, with the date, inside the documents themselves.

**Inspect every hit before writing any of it down.** Across one session this
sequence produced four false findings — an ASCII tree read as paths, commented-out
routes read as live, a writer-service column read as column names, a
module-plus-function read as a missing module — and each looked exactly like a
real defect until it was opened.
