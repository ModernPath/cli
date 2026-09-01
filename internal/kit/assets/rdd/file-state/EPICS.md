# Epic flat-file state

> Canonical Epic serialization for the authoritative process store. A
> store-backed repository materializes this file from the store; a file-backed
> repository versions it as the store. Never both.

- **Snapshot at:** «timestamp»
- **Source store/revision:** «database revision or repository SHA»

## EPIC-«AREA»-«NNN» — «Human-readable change»

- **Status:** «status from PROCESS.md»
- **Outcome / source:** «unit of change» / USER:/DOC:
- **Scope / non-goals:** «included and excluded change»
- **Owner / release:** «owner and release»
- **Members:** «UR/SR ids with CONFIRMED membership; candidate ids labelled CANDIDATE»
- **Shared context:** «cross-cutting decision, specification, reconnaissance, and cold-review refs»
- **Entry gates:** «GATES.md gate ids»
- **Completion gates:** «GATES.md gate ids»
- **Delivered revision:** «repository + revision or not delivered»
- **Gaps / deferrals / blockers:** «refs or none»
- **Completion facts:** «Done; Decisions; Deferred; Discovered; Follow-ups; Gate result»
