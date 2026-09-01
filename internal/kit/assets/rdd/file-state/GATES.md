# Gate flat-file state

> Canonical gate serialization for the authoritative process store.
> `PROCESS.md` defines gate kinds, prerequisites, legal transitions, and
> application rules. A store-backed repository materializes this file from the
> store; a file-backed repository versions it as the store. Never both.

- **Snapshot at:** «timestamp»
- **Source store/revision:** «database revision or repository SHA»
- **Context / release:** «scope»

Trace gates and human gates are recorded in the same file because a human gate
is addressable only through the trace gates that gate it. A human gate with no
recorded prerequisite trace gate is unreadable, not implicitly open.

## GATE-«AREA»-«NNN» — «Transition or decision purpose»

- **Kind:** trace or human / «confirmation, entry, decision, cold-review, start-review, completion»
- **Transition / purpose:** «exact state transition, or the decision being asked»
- **Exact scope:** «named EPIC/UR/SR ids this gate covers; one answer may cover an Epic and named members»
- **Prerequisites:** «gate ids that must be PASS before this one may leave DRAFT, or none»
- **Fingerprint:** «content/code fingerprint the gate was evaluated at»
- **State:** «trace: PENDING / PASS / FAIL / STALE — human: DRAFT / OPEN / ANSWERED / CLOSED / SUPERSEDED»
- **Verdict / answer:** «trace verdict with exact blockers, or the human answer as given»
- **Actor / evaluator:** «real human actor and role for a human gate; evaluating agent or check for a trace gate»
- **Sources:** «USER:/DOC:/CODE:/TEST:/RUN:/EPIC: support for the verdict or answer»
- **Timestamps:** «evaluated/opened at; answered at; closed at»
- **Application:** «NOT_APPLICABLE / PENDING / APPLIED / FAILED» at «revision»
- **Predecessor / successor:** «superseded gate id and successor gate id, or none»

### Brief

Human gates only. Omit for trace gates.

```markdown
**Brief:**
- What: «decision»
- Why now: «trigger and blocked work»
- Changes if approved: «visible outcome»
- Risk if wrong: «downside and reversibility»
- Recommendation: «option and rationale»
- Image: «optional evidence»
```

### Holds

- **Held items:** «EPIC/UR/SR ids blocked until this gate closes, or none»
- **Applied transitions:** «item id -> from -> to, one per line; empty until APPLIED»
