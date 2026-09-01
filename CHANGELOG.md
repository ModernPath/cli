# Changelog

## v0.6.0 — the process became twelve passes, and the loop got a place to stand

`modernpath install` now writes the full requirement-driven process rather than a
handful of skills: **twelve passes** under `.modernpath/rdd/skills/`, each one a
phase you can enter and exit, pinned to `req-driven-dev@e3f60dde`.

`rdd-start` · `rdd-discover` · `rdd-plan` · `rdd-cold-review` · `rdd-entry-review`
· `rdd-build` · `rdd-verify` · `rdd-completion-review` · `rdd-triage` ·
`rdd-deliver` · `rdd-audit` · `rdd-reverse-engineer`

The old flat set (`rdd-planning`, `rdd-discovery`, `rdd-build-loop`) is gone. The
replacement is not a rename: entry is now a gate with a packet behind it,
cold review runs from a context that did not author what it reviews, and
completion audits evidence at the delivered revision rather than at the revision
someone remembered. `PROCESS.md` ships alongside as the single authority, and
`file-state/` carries the record shapes it serializes.

### `modernpath focus` — say what you are on, without claiming it

Visibility only: no assignment, no lock, nobody's queue changes. Declare it,
clear it, list what everyone else declared. `--infer` reads the refs you are
actually touching and proposes the answer, with a hysteresis buffer so a single
stray file does not flip your focus. A transition emits `focus_changed` naming
the human who caused it.

### Your move, on arrival

A `SessionStart` hook writes a personal brief: what is waiting on **you**,
ordered by what it unblocks, not by when it was created. The hook family owns no
repository files — it reads and writes nothing you have to merge.

### `factory sync` stops falling over on a bad afternoon

Tunable chunk size, retry on 5xx instead of abandoning the batch, and `--no-docs`
when you want records without the document payload. Reachability warnings now
name the system they could not reach, which turns "connection failed" into
something you can act on.

### Fixes worth naming

- **The health probe never sent the bearer**, so a reachable system answered
  `401` and reported itself unreachable. Two of the three probes also skipped the
  Gateway prefix.
- **12 of 20 open-question gates were losing their brief** between authoring and
  the wire — the decision arrived without the reason for it.
- **Five approval reports were false.** A dormant test, switched on, found them.
- **The answered-gate log was written into a directory the CLI never creates.**
  `factory pull --apply` opened `mission-control/ANSWERS.md`, got `ENOENT`,
  swallowed it, and reported success — so in every installed workspace the log
  was silently empty. It now writes `ANSWERS.md` and `answers.jsonl` at the
  workspace root.
- **Hook migration was guarded for Claude and not for Cursor**, so Cursor users
  kept a stale hook after upgrading.

### Changed defaults

- `modernpath env` defaults to **cloud production**. `beta` still exists as a
  named environment, now marked deprecated.
- The `--legacy-extractor` flag is removed. It shelled out to a node op-builder
  that only ever existed in one repository; the bundled Go parsers have been the
  default for some time and are now the only implementation.

## v0.5.0 — a repository with no requirements gets a ledger

`modernpath install` now ships **`rdd-reverse-engineer`**: the method for turning
an existing codebase into a requirement ledger, so the four-command path —
`auth`, `init`, `install`, then ask — works on a repository that has never heard
of this process.

It is not a prompt. It is a procedure that was run against two real client
repositories and corrected eight times by what they did to it.

### The method, in the order that matters

**A — the domain first.** Read the schema directly: entities, owners, and the
invariants only constraints state. Draw context boundaries by **aggregate
ownership** — whoever writes a table owns it — not by route-file layout, which
follows the build system rather than the business.

**B — then the surfaces.** Every view, the actors its role gate admits, the
endpoints it calls. Group them into journeys and write one epic per journey,
each carrying a **user requirement**. A pass that emits only ledger rows produces
a system with no stated purpose.

**C — only then the requirements.** Enumerate entry points and work the list:
HTTP routes, **agent and LLM tool surfaces**, workers, webhooks, CLI commands.
Two passes over one context differed by an entire conversational surface because
one counted only HTTP.

### Honesty rules, each learned the hard way

- **`DONE` is not reachable by a derivation pass.** `PROCESS.md` §7.6 requires
  human sign-off. Test evidence lives in an `Evidence` column instead — *run,
  passing* / *mocked* / *no test* / *not run* — because the vocabulary has no
  value for "test-verified but unapproved".
- **Nothing derived is `PROPOSED`.** That means *not started*, and a payment flow
  running in production every day is not an unstarted proposal. The one exception
  is behaviour the system should have and does not, sourced `finding, this pass`.
- **A mocked-out service is not evidence.** A route test that patches the service
  it nominally tests verifies routing, not behaviour.
- **Ambiguity becomes a question, not an inference.** Code answers *what*, rarely
  *why*.
- **A coverage manifest is the only definition of "done enough"**, and it gates
  the sync. Five passes once produced an accurate ledger covering 10% of its
  system with nothing anywhere saying so.
- **Audit your own citations, and skip named absences.** A row saying *"there is
  no `test_draft_service.py`"* is naming a gap, not citing a file.

### What it produced

On a 156-endpoint platform: **967 requirements across 11 of 11 contexts**, 16
journey epics, 36 of 36 reachable views carrying a user requirement, **3,222 of
3,222 citations resolving**, and zero rows claiming verification they did not
have. Findings no endpoint walk reaches: 13 unreachable views, six tables written
by two owners, an agent tool returning contact details the REST route gates.


## v0.4.0 — the harness hooks stop lying, and start being fast

### Hooks own no repo files

Both hook families invoked scripts under `.claude/hooks/` by relative path, so
each depended on two things it did not control: the file surviving in the working
tree, and the harness running from the repository root. When either failed, the
shell died before reaching the `exit 0` that keeps a hook silent — every turn
ended in `No such file or directory` and no sync ran.

Both now invoke the installed CLI directly and write nothing into the repository:

```
command -v modernpath >/dev/null 2>&1 && ( modernpath factory sync --if-quiescent --trigger Stop >/dev/null 2>&1 & ) ; exit 0
command -v modernpath >/dev/null 2>&1 && modernpath context --hook UserPromptSubmit 2>/dev/null || echo '{}'
```

`command -v` makes a missing CLI a silent no-op. Install **migrates**: it deletes
the legacy scripts and strips settings entries that name them — previously a
reinstall no-oped on broken wiring, because the legacy entry counted as
"installed".

### `modernpath context --hook` — 11–28s becomes ~1s

The hook used to spend 0.8s deciding relevance and then 9–27s on a reasoning
model writing an essay, for a consumer that has grep and file reads.

- **Specific questions cost no synthesis at all** — ranked file paths with their
  purpose. Measured p50 **1.2s**.
- **Only high-level questions buy prose**, from a capped fast model, and only
  when something was actually retrieved. Measured **2.1s**. A model asked to
  summarize an empty result answers from the question alone.
- **Irrelevant prompts stay silent** in 0.8s.
- Decomposed queries are searched **separately and merged**; the old code joined
  them into `"Core.Sync Also: epic op Also: Core.Sync epic op"` and handed that to
  a model as if it were a question.
- The searches run **alongside** the relevance gate rather than after it.
- Every response carries the workspace's own state — release, requirement counts,
  open gates and how many are approvals, next READY rows — computed from stored
  state with no model call.

### The hook says what happened

`.modernpath/context-hook.log` gets one line per run, naming the **reason**:

```
2026-08-11T07:59:35Z · delivered · 1.8s · 2731 bytes
2026-08-11T08:43:47Z · failed · 1.4s · server error 400 {"error":"..."}
2026-08-11T07:59:36Z · empty · 0.7s · not relevant
```

Previously every failure — missing config, auth error, non-200, decode failure,
timeout — rendered as an empty result and read as "nothing relevant", so a broken
hook and a quiet one were indistinguishable. A client-side deadline (default 8s,
`--deadline`) returns empty rather than holding the turn.

### `modernpath hooks doctor`

The hooks call the CLI by bare name, so PATH decides which build runs. A second,
older copy earlier in PATH silently downgrades every hook run while still exiting
0 — which happened here, with a tested deadline and log that never executed.

`hooks doctor` reports which binary wins, which are shadowed, whether both hook
families are wired, whether any entry still points at a removed script, and
recent failures from the log.

### `modernpath install` tells agents where the knowledge is

The managed `AGENTS.md` block now carries a **Codebase knowledge** section: the
API finds (`modernpath search`, or the context hook), the local export reads
(`.modernpath/modernpath/…`), `modernpath docs sync` when the export is absent,
and the API wins when they disagree — the export is a cache. In the managed block
rather than a skill, because skills are Claude-only and Codex reads `AGENTS.md`
where it does not read `CLAUDE.md`.

### `scripts/install-local.sh`

Writes **every** copy of `modernpath` on PATH rather than only the usual homes —
it previously missed `~/.local/bin`, which is exactly where the shadowing copy
lived. Stamps commit and build time into the version string, and warns loudly
about copies it could not write instead of skipping them silently.

---

Earlier releases are catalogued in the GitHub releases for `ModernPath/cli`.
