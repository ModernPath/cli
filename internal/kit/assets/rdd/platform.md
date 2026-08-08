# ModernPath platform integration

<!-- TOOL-OWNED. Installed by `modernpath install`; replaced wholesale on upgrade.
     Remove this import from CLAUDE.md to run the process without the platform. -->

This layer connects the process in `PROCESS.md` to the ModernPath server, so the
ledgers, epics and gates in this repository become a live Mission Control view.
It is **separable**: the process works without it, and a repository that drops
this import keeps a fully functional requirement-driven loop.

## Sync

The workspace pushes its process state through the `modernpath` CLI:
`factory sync` sends the `tasks/` ledgers, `epics/` and `WORKLIST.md` as a typed
op batch. Binding lives in `.modernpath/config.json`, which is gitignored — it
holds auth material and machine-local state.

**Syncing is automatic.** Harness hooks (`modernpath hooks register`) trigger a
fire-and-forget sync at session start, session end, and the end of each agent
turn, debounced and skipped while the workspace is mid-change. You do not run it
by hand.

Two rules that come from real failures:

- **A green sync is not a landed change.** The hook reports success when the
  process exits cleanly, which it does even when the payload was built by a
  stale binary. After a change to the extractor or the ledger format, reconcile
  what was built against what the server serves rather than trusting the log.
- **The extractor is versioned with this repository.** If the sync runs a
  separately installed binary, changes to extraction silently do nothing.

## Releases

Work is grouped into **releases** recorded in `process/releases.md`, which holds
exactly one `active` row. `current_release` in `.modernpath/config.json` mirrors
it; drift between the two is a defect. Set it with
`modernpath factory release use <id>`.

- Membership is **workspace-level**: no ledger column, no per-row bookkeeping.
  Sync stamps every synced requirement and epic with the current release.
- **Stage** (MVP/Later) is a priority tier, not a release. Don't conflate them.
- **Base requirements** — rows in no release or in a `done` release — never mix
  with active-release requirements in one view.
- Membership in a done release is **sticky**: rotation never pulls finished work
  forward. A row leaves a closed release only when its status becomes active again.
- **Deferral is within-release, never descoping.** `DEFERRED` means "later in
  this release". At release close, every still-deferred row is explicitly moved
  to the next release or replanned — never silently left behind.
- Selecting or changing the current release is a product decision: `USER:`-sourced
  in the registry, never agent-assumed.

## Gates and decision briefs

An epic awaiting human approval, an unresolved decision request, and an epic
awaiting specification approval all sync as **gates** — the queue a human works
through in Mission Control.

**Every gate carries a plain-language brief.** When you author a decision or an
epic's approval section, add a `**Brief:**` block in product language, with no
process vocabulary:

```
**Brief:**
- What: <one sentence — the decision in product terms>
- Why now: <what raised it, what waits on it>
- Changes if approved: <the visible outcome>
- Risk if wrong: <honest downside + reversibility>
- Recommendation: <your call + one-line rationale>
```

`What` is mandatory — a block without it is ignored. Briefs ride the payload only
when present, so brief-less gates keep byte-identical hashes.

## Gate hygiene

Gate emission is driven by the wording of the epic record and its `WORKLIST.md`
row, and it fails silently in both directions:

- An epic can claim it is awaiting approval while **no gate op is built for it**,
  leaving it in review forever with nothing to approve. Causes: no `## Approval`
  section in the record, or a work-list phrasing that classifies it as in-progress.
- A phrasing can open a gate that **should not exist** — for example an approval
  cell beginning with "pending" while the epic is only at the specification stage.

Neither produces an error. When an epic reaches review, confirm its gate actually
exists rather than assuming the record's claim.

## Setting up a new workspace

`modernpath auth login` → `modernpath init` → `modernpath factory connect` →
`modernpath factory release use <id>`, then `modernpath hooks register` for
automatic syncing. The server side needs workspace membership, a system, and the
release rows to exist first.
