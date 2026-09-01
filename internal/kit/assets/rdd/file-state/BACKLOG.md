# Backlog and gap flat-file state

> Canonical backlog and gap serialization for the authoritative process store.
> `PROCESS.md` defines the discovery routing table these records answer to. A
> store-backed repository materializes this file from the store; a file-backed
> repository versions it as the store. Never both.

- **Snapshot at:** «timestamp»
- **Source store/revision:** «database revision or repository SHA»

Neither record type is a requirement. Neither counts toward trace, release,
readiness, coverage, progress, or completion. Promotion out of this file always
goes through `PROCESS.md` routing — a directly sourced item to `PROPOSED`, an
inferred one to `DERIVED` plus its confirmation gate.

## Triage backlog

Discoveries with unclear ownership or a cross-cutting concern, held until a
human assigns them. An item that already has an owner and a source is a
requirement and does not belong here.

## BACKLOG-«NNN» — «Discovery in one line»

- **Raised by / at:** «USER:/CODE:/RUN:/TEST: source» / «timestamp»
- **Observed:** «what was seen, not what it implies»
- **Why unrouted:** «unclear owner, cross-cutting, or awaiting a decision»
- **Candidate route:** «PROPOSED UR/SR, DERIVED, gap, conflict, or decision gate»
- **Affected items:** «EPIC/UR/SR ids, or none known»
- **Disposition:** OPEN / ROUTED to «id» / REJECTED with «source»

## Gap records

A capability or specification deficiency linked to the traces it affects. A gap
is a disclosed absence, not a deferral: deferral postpones work that is already
a requirement, a gap names something no requirement yet covers.

## GAP-«AREA»-«NNN» — «Missing capability or specification»

- **Kind:** capability or specification
- **Source:** «USER:/DOC:/CODE:/TEST:/RUN: evidence the gap is real»
- **Affected traces:** «EPIC/UR/SR ids whose trace is incomplete because of it»
- **Consequence:** «what the affected traces cannot currently prove»
- **Disclosed in:** «gate ids where this gap was disclosed before an answer»
- **Disposition:** OPEN / CLOSED by «UR/SR id» / ACCEPTED with «USER: source»

Every open gap affecting an item in a completion gate's scope must be disclosed
in that gate before it is answered.
