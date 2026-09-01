# Gate flat-file state

## GATE-ACC-000 — Candidate packet complete

- **Kind:** trace / confirmation prerequisite
- **State:** PASS

## GATE-ACC-001 — Confirm the derived ACC corpus

- **Kind:** human / confirmation
- **Transition / purpose:** Requirement DERIVED -> PROPOSED for the named ids
- **Exact scope:** 3 ids — UR-ACC-001 SR-ACC-010 SR-ACC-011
- **Prerequisites:** GATE-ACC-000 (trace: candidate packet complete)
- **Fingerprint:** accessservice @ 9026a05
- **State:** DRAFT — opens when a human is available to answer
- **Verdict / answer:** unanswered
- **Sources:** CODE citations on every row

### Brief

```markdown
**Brief:**
- What: confirm whether each derived ACC requirement describes real behaviour.
```

## GATE-WEB-000 — Candidate packet complete

- **Kind:** trace / confirmation prerequisite
- **State:** FAIL

## GATE-WEB-001 — Confirm the derived WEB corpus

- **Kind:** human / confirmation
- **Exact scope:** SR-WEB-001
- **Prerequisites:** GATE-WEB-000
- **State:** DRAFT — opens when a human is available to answer

## GATE-OLD-001 — Already decided

- **Kind:** human / confirmation
- **Exact scope:** SR-OLD-001
- **Prerequisites:** none
- **State:** ANSWERED
- **Verdict / answer:** confirmed as-built (USER:2026-08-20)

## CRF-ACC-007 — Contract mismatch on the shared payload

- **Classification:** ORIGINAL
- **Exact scope:** SR-BLEED-999
- **Current disposition:** OPEN
- **Owner:** platform

## GATE-LAST-001 — Confirm the last corpus

- **Kind:** human / confirmation
- **Exact scope:** SR-LAST-001
- **Prerequisites:** none
- **State:** OPEN
