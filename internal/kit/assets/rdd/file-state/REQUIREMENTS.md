# Requirement flat-file state

> Canonical requirement serialization for the authoritative process store.
> `PROCESS.md` defines item content, lifecycles, gates, and traces. A
> store-backed repository materializes this file from the store; a file-backed
> repository versions it as the store. Never both.

- **Snapshot at:** «timestamp»
- **Source store/revision:** «database revision or repository SHA»
- **Context / release:** «scope»

## UR/SR-«AREA»-«NNN» — «Title»

- **Kind / status:** UR or SR / «status from PROCESS.md»
- **Statement / source:** «normative content» / USER:/DOC:/CODE:
- **Owner / release:** «owner and delivery scope»
- **Epic membership:** «EPIC id + authority, or none»
- **Declared relations:** «UR scenarios requiring SRs, SR links to UR scenarios, or none»
- **UR content:** «actor, context, intended outcome, and inline scenarios; N/A for SR»
- **SR content:** «boundary, behavior, scope/non-goals, and technical context; N/A for UR»
- **Candidate packet:** «inference sources, proposed relations, consequences, and confirmation brief; DERIVED only»

### Trace references

| Evidence class | Target | Code | Test case | RED result | Passing result | Outcome | Environment | Fingerprint | Validity/revision |
|---|---|---|---|---|---|---|---|---|---|
| UR upper | «UR scenario or N/A» | «code refs» | TEST: | RUN: | RUN: | PASS / FAIL / SKIP | «when relevant» | «content/code fingerprint» | CURRENT / STALE / INVALID / INHERITED_UNVERIFIED + revision |
| SR lower | «SR clause or N/A» | CODE: | TEST: | RUN: | RUN: | PASS / FAIL / SKIP | «when relevant» | «content/code fingerprint» | CURRENT / STALE / INVALID / INHERITED_UNVERIFIED + revision |

The RED/Passing split carries each result's role; `Outcome`, `Environment`,
and `Fingerprint` carry the remaining mandated evidence-record fields.

### Gates and delivery

- **Confirmation gates:** «GATES.md gate ids, or N/A»
- **Entry gates:** «GATES.md gate ids»
- **Start/review gates:** «GATES.md gate ids»
- **Completion gates:** «GATES.md gate ids»
- **Delivered revision:** «repository + revision or not delivered»
- **Gaps / deferrals / blockers / notes:** «refs or none»
