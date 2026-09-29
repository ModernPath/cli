# Gate flat-file state

> Canonical gate serialization for the authoritative process store.
> `PROCESS.md` defines gate kinds, prerequisites, legal transitions, and
> application rules. A store-backed repository materializes this file from the
> store; a file-backed repository versions it as the store. Never both.

- **Snapshot at:** «timestamp»
- **Source store/revision:** «database revision or repository SHA»
- **Context / release:** «scope»

Human gates name every prerequisite required by their transition. A gate with
missing required prerequisites cannot open. An empty prerequisite set is valid
only where `PROCESS.md` explicitly permits it, such as an attributable demotion.

## GATE-«AREA»-«NNN» — «Transition or decision purpose»

- **Kind:** trace or human / «confirmation, entry, decision, demotion, cold-review, start-review, completion»
- **Transition / purpose:** «exact state transition, or the decision being asked»
- **Exact scope:** «named EPIC/UR/SR ids, or the immutable source inventory for baseline authorization before requirements exist»
- **Prerequisites:** «gate ids that must be PASS before this one may leave DRAFT, or none»
- **Fingerprint:** «approval-scope or evidence fingerprint and the exact inputs evaluated, per PROCESS.md»
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
