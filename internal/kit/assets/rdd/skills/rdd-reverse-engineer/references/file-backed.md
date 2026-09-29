# File-backed compatibility

Read this only when the workspace declares file-backed state. Store-backed
workspaces must not recreate retired task ledgers or mirrors of store records.
Canonical file-state workspaces use the selected `file-state/` serialization.
Preserve source-scoped authorization and receipt semantics; if the tooling cannot
enforce baseline authority, report the limitation rather than silently assigning
PENDING_VERIFICATION.

For an existing legacy file ledger, retain these importer shapes:

```
Proposed relations (CANDIDATE): requires SR-KERNEL-030, SR-KERNEL-031.
```

```
Proposed relations (CANDIDATE): serves UR-KERNEL-002.
```

```
- **UR:** UR-KERNEL-002
```

`requires` names rows that take this row as parent; `serves` names its parent.
A dedicated UR field takes precedence over a packet sentence. Mere ID mentions
create no relation. Report unknown targets rather than dropping edges. An
explicit empty relation set with rationale is valid; never invent a parent.
These formats do not authorize promotion.

Use `tasks/NFR-REQUIREMENTS.md` only where it is already the authoritative legacy
NFR ledger. Canonical file-state workspaces keep NFRs in their selected shapes;
store-backed workspaces publish NFRs as ordinary typed requirements. Preserve
existing identity conventions, such as `REQ-NFR-NNN`, and check ownership before
adding rows.
