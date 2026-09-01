# CON — Consortiums · requirements ledger

## Dashboard — CON
Totals: 0 DONE · 0 IN_PROGRESS · 2 IN_REVIEW · 0 READY · 0 PROPOSED · 0 DEFERRED · 1 BLOCKED · 0 OBSOLETE

| ID | Title | Stage | Status | UR | Source | Evidence | Code |
|----|-------|-------|--------|----|--------|----------|------|
| REQ-CON-001 | Only public-sector organizations may lead | MVP | IN_REVIEW | UR-CON-001 | §Who may lead | no test | `consortium_service.py:99` |
| REQ-CON-002 | Forming a consortium locks the shared need | MVP | IN_REVIEW | UR-CON-001 | §Formation | run, passing | `consortium_service.py:84` |
| REQ-CON-003 | A consortium can be closed | MVP | BLOCKED | | Q-001 | — | *(nothing sets it)* |

---

## Detail

### REQ-CON-001 — Only public-sector organizations may lead
- **Status:** IN_REVIEW · **Stage:** MVP · **UR:** `UR-CON-001`
- **Source:** §Who may lead
- **Statement:** A consortium's lead must be a public-sector organization; the
  check rejects any other organization type before the group is created.
- **Evidence:** **no test** — `create_consortium` is never called with a
  non-public lead in `tests/test_consortium_service.py`, so the rejection branch
  has never run.
- **Code:** `services/consortium_service.py:99-104`,
  `services/api/routes/consortiums.py:41`

### REQ-CON-002 — Forming a consortium locks the shared need
- **Status:** IN_REVIEW · **Stage:** MVP · **UR:** `UR-CON-001`
- **Statement:** Forming a consortium locks its shared need, so later AI
  synthesis cannot re-map or delete it — the boundary between machine clustering
  and human commitment.
- **Evidence:** run, passing — `tests/test_consortium_service.py:158-171`
  asserts the `NeedLocked` guard fires; the `SynthesisRunner` path is covered by
  `tests/test_synthesis.py:88`
- **Code:** `services/consortium_service.py:84-91`
