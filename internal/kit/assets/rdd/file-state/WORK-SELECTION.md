# Work-selection flat-file state

> Canonical work-selection serialization for the authoritative process store.
> `PROCESS.md` defines scope selection, phase order, and lifecycle meanings. A
> store-backed repository materializes this file from the store; a file-backed
> repository versions it as the store. Never both.

- **Snapshot at:** «timestamp»
- **Source store/revision:** «database revision or repository SHA»
- **Active release:** «release with its USER: source»

Work selection is authoritative state, not a derived queue. It records which
scope is frozen, at which fingerprint, and which phase it is waiting on.
Rendered queues, progress counts, and dashboards are regenerated from the
authoritative store and are not recorded here.

## Current selection

- **Selected scope:** «EPIC id, or the single SR id»
- **Members:** «UR/SR ids in the frozen scope; empty for single-SR scope»
- **Scope kind:** Epic or single SR
- **Frozen at fingerprint:** «content/code fingerprint the selection was frozen at»
- **Reconnaissance revision:** «named revision the packet was authored against»
- **Current phase:** «source / plan / cold review / entry / build / verify / completion / triage»
- **Waiting on:** «gate id, blocker, external prerequisite, or nothing»
- **Owner:** «who holds the selection»

## Suspended selections

One row per scope held at `BLOCKED` or `DEFERRED`, so the suspended state is
recoverable rather than inferred.

| Scope | Suspended status | Restored-to status | Reason | Owner | Target | Blocker/gate |
|---|---|---|---|---|---|---|
| «EPIC/SR id» | BLOCKED or DEFERRED | «strongest state supported when released» | «reason» | «owner» | «target» | «gate id or ref» |

## Selection history

Append-only. A selection leaves the current slot when its scope reaches `DONE`
or `OBSOLETE`, or when triage returns it to an earlier phase.

| Scope | Selected at | Left at | Outcome | Successor selection |
|---|---|---|---|---|
| «EPIC/SR id» | «timestamp» | «timestamp» | DONE / OBSOLETE / returned to «phase» | «scope id or none» |
