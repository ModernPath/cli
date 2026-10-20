## Operating the delivery loop

`.modernpath/rdd/PROCESS.md` names phases and never a command, and the skills
under `.modernpath/rdd/skills/` say *what* a pass reads and records. This
section says *how* the installed `modernpath` binary does it. Nothing here
changes the process. The server is the only authority: every write is
attributed to the signed-in person, checked by the server, and read back
before it is relied on. Each command's section below gives its flags and the
exact JSON it accepts.

### Rules that apply to every command

- Run every command from the workspace root. A nested directory that carries its
  own `.modernpath/config.json` binds to another System or reports
  `no system_id`.
- One store command per shell call. Never chain writes and never pipe one
  write into another.
- Store writes run only from the orchestrating session. A delegated reviewer
  or verifier returns findings; the session records them.
- A read or write the CLI does not offer is a tooling gap. Read the bound
  System name with `modernpath status`. Only when that name contains
  `modernpath` (case-insensitive), such as TEST-ModernPath-v1 (105), record
  the missing capability with `backlog create --file` and kind `tooling`.
  For other Systems, create no missing-capability report or backlog item.
  If the sanctioned command cannot record the item, do not invent another
  write channel. Never call the API by hand, edit a store file or keep a
  local ledger. A refused permission is a decision: return it verbatim.
  Report task incompleteness honestly; this routing rule never makes an
  unavailable operation successful.
- Identity: Epics are integers (`epics list`); requirements and test cases
  are native UUIDs with a display reference such as `UR-6-0`, `SR-12-1` or
  `TC-8-0`, whose last number is the revision. Pass the UUID wherever a command
  takes `--kind user|system` with an id and in every JSON file; a display
  reference is for a human-facing read. Never invent, derive or reuse an
  identity the store has not issued.
- Input files are JSON objects; `--file -` reads standard input where offered.
  Follow each command's documented file shape. Unknown-field handling varies
  by command: some reject unknown keys, while `evidence record` can ignore
  them. Read back the stored fields and result count to confirm the input
  was retained.
  Pass `--json` for machine output. A refusal exits non-zero with one error
  line; a server refusal reads `<what> (server <status>): <reason>`.
- After each write, read the result back (`epics plan show`, `requirements
  get`, `traces list`, `evidence runs --full`) and confirm the expected state
  before reporting it.

### Phase to command

| Phase (`PROCESS.md`) | Skill | Commands |
|---|---|---|
| Session entry and scope | `rdd-start` | `status`, `auth status`; `epics get <id> --full --json`; `epics plan show <id> --full --json`; `requirements get <uuid> --kind user\|system --full --json`; `evidence latest --target-type requirement --target-id <uuid>`; `traces list --entity-type <kind> --entity-id <uuid>` |
| Discover and classify | `rdd-discover` | `requirements search`, `requirements list`, `epics search`, `search`, `read-doc`, `ask`; a product discovery outside the Epic: `backlog create --file`; missing capabilities follow the System-name rule above |
| Plan | `rdd-plan` | new behavior: `requirements create --kind user\|system --file`; the Epic: `epics create --file`, `epics update <id> --file` (PROPOSED only); the plan: `epics plan save <id> --file plan.json --reason "<why>"`, then `epics plan show <id> --full --json` |
| Cold review | `rdd-cold-review` | reads only: `epics plan show --full --json`, `requirements get`, `traces list`, `evidence latest`. The orchestrator folds the findings into the plan with `epics plan save`; a product finding outside the Epic becomes a `backlog create` record; missing capabilities follow the System-name rule above |
| Implementation approval | `rdd-entry-review` | the human decides; the session records the answer: `epics plan approve-implementation <id> --file reviewed-plan.json --reason "USER:<date>: <answer>"`, where the file is the `plan` object of `epics plan show <id> --fields plan --json`; read back `process_status: IN_PROGRESS`, `implementation_approval`, and each named requirement `IN_PROGRESS` with `change_epic_id` set |
| Implement | `rdd-build` | the RED run, then the passing run: `evidence record --file` (a result with `role: RED` and `result: fail`, later one with `result: pass`), each at its tested commit; a stable test identity when one is needed: `test-cases create --file` |
| Verify | `rdd-verify` | `evidence record --file` for focused, boundary and user-flow runs; read back `evidence latest`, `evidence runs --full --target-id <uuid>` |
| Review, publish and complete | `rdd-completion-review` | `epics plan submit-review <id> --reason` (to IN_REVIEW; the approver runs it); the human decides; `epics plan approve-completion <id> --reason "USER:<date>: <answer>"`; read back `epics plan show --full --json` (`completion_approval`, `versions`) and `requirements get` (`DONE`, revision +1) |
| Discoveries and blockers | `rdd-triage` | `backlog create --file` (`kind`: `backlog` or `gap` for product discoveries; `tooling` for missing capabilities only under the System-name rule above); `requirements obsolete <uuid> --kind … --reason`; `requirements set-context`; new scope after approval: a new Epic and plan, never an edit of the approved one |
| Record existing behavior | `rdd-reverse-engineer` | `reverse-engineer schema`, then `reverse-engineer record --file group.json`; read back `requirements get --full`, `traces list` |
| Verify existing behavior | `rdd-reverse-engineer-verify` | `evidence record --file` with `target_type: criterion` results (see `evidence record`); `reverse-engineer review --file selection.json --json > review.json` lists what still blocks acceptance under `problems` |
| Accept existing behavior | `rdd-reverse-engineer-accept` | `reverse-engineer review … --json > review.json`; the human decides; `reverse-engineer accept --file acceptance.json`, wrapping that exact bundle with `source` and `reason` |

A human answer is recorded as the `--reason` (`USER:<date>: …`) of the
approval or acceptance that applies it. No command records an autopilot grant, a
cold-review verdict or a human answer on its own; when a pass needs such a
record, handle the missing capability under the System-name rule above.

### Lifecycle, as the commands see it

```text
epics create             -> Epic PROPOSED
epics plan save          -> saved plan (PROPOSED only; repeat to revise)
plan approve-implementation -> Epic IN_PROGRESS; named requirements IN_PROGRESS, owned by the Epic and the approver
plan submit-review       -> Epic and owned requirements IN_REVIEW (the approver runs it)
plan approve-completion  -> approved content published: revision +1, immutable version; Epic DONE; ownership released
reverse-engineer record  -> requirements PROPOSED at revision 0, with their traces
reverse-engineer accept  -> unowned PROPOSED requirements DONE: revision +1, an existing_behavior version
```

A requirement owned by an Epic (`change_epic_id` set) is not accepted,
obsoleted, regrouped or planned elsewhere until that Epic completes. Epic
content and the plan are read-only in `IN_PROGRESS` and `IN_REVIEW`: a scope
change is a human decision and new work, never an edit of the approval.

### Refusals and what clears them

| Refusal (substring) | Origin | Cause | Clears it |
|---|---|---|---|
| `connect this workspace to a system` / `no system_id` | cli | no binding, or a nested `.modernpath/config.json` | run from the workspace root; `modernpath init` binds |
| `does not belong to the bound System` | cli | an Epic or review bundle of another System | check `modernpath status`; use an id of this System |
| `unknown input field` / `input must include` / `must be an array` | cli | a file key outside the documented shape; nothing was sent | correct the key (see the command's section) |
| `invalid lifecycle state` | server 409 | a plan write in the wrong Epic state: `save` and `approve-implementation` need PROPOSED, `submit-review` IN_PROGRESS, `approve-completion` IN_REVIEW | read `epics plan show`; run the command that state allows |
| `requirement owned` | server 409 | the approval names a requirement another Epic owns, or acceptance names an owned one | finish or release the owning Epic; plan without it |
| `not change owner` | server 409 | `submit-review` by someone other than the approver | the approver runs it |
| `requirement not owned` / `no plan` | server | completion on an Epic whose requirements are not owned by it, or that has no saved plan | re-plan and re-approve |
| `incomplete reviewed content` | server | `approve-implementation` with a partial plan: a requirement without every content field, a criterion without every field, or no `traces` key | send the `plan` object `epics plan show` returned, unchanged |
| `invalid plan` / `invalid requirement proposal` / `invalid requirement content` / `invalid criterion` / `invalid trace` | server | a key outside the plan shape, an empty `body`, or a content field of the other requirement kind | fix the named part (see `epics plan save`) |
| `requirement not found` / `trace endpoint not found` | server | a UUID the System does not hold | `requirements create` first, or correct the id |
| `trace outside proposal` | server | a plan trace touching none of the plan's requirements or criteria | drop it, or add that requirement to the plan |
| `document trace must relate` | server | a requirement–document trace whose `link_kind` is not `relates` | use `relates` |
| `Invalid proposed content` with `details` | server 422 | a content field failed the requirement changeset; the fields are listed | fix the named fields |
| `invalid_payload` / `invalid_requirement` / `invalid_requirement_fields` / `invalid_criterion_fields` / `invalid_trace_fields` / `invalid_trace_endpoint` / `invalid_source_file` | server | a `reverse-engineer record` file outside its shape; nothing was written | see `reverse-engineer record` |
| `existing_requirement_conflict` / `existing_trace_conflict` | server | an entry whose id the System holds, with different content | drop the entry to reuse the stored record, or send identical content; a content change is Epic work |
| `review_changed` | server 409 | `reverse-engineer accept` after a requirement, criterion, trace or evidence moved | `reverse-engineer review` again; the human decides on the new bundle |
| `acceptance_not_ready` | server 409 | a bundle that is not `eligible`, or that stopped being eligible | clear each `problems` entry (see `reverse-engineer review`) |
| `requirement_uuid_required` / `invalid_requirement_kind` | server | an evidence result with `target_type: requirement` and no `requirement_kind`, or a `target_id` that is not a UUID | add `requirement_kind` and the UUID |
| `execution_proof_key_conflict` / `delivery_proof_key_conflict` | server 409 | `evidence record` changes proof under the `id` of a run marked `raw_payload.as_built_proof: execution` or `delivery`; ordinary runs allow updates | omit `id`; a new observation is a new run |
| `server rejected the session token`, or any sign-in refusal | cli | the sign-in lapsed | a human runs `modernpath auth`; the agent stops |
