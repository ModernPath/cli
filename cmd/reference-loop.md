## Operating the delivery loop

`.modernpath/rdd/PROCESS.md` defines the four Epic gates and required exits. The skills
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
- Store writes run only from the agent handling the task. A delegated reviewer
  or verifier returns findings; the session records them.
- A read or write the CLI does not offer is a tooling gap. Read the bound
  System name with `modernpath status`. Only when that name contains
  `modernpath` (case-insensitive), such as TEST-ModernPath-v1 (105), record
  the missing capability with `backlog create --file` and kind `tooling`.
  For other Systems, create no missing-capability report or backlog item.
  If the CLI command cannot record the item, do not invent another
  write channel. Never call the API by hand, edit a store file or keep a
  local ledger. A refused permission is a decision: return it verbatim.
  Report task incompleteness honestly; this routing rule never makes an
  unavailable operation successful.
- Identity: Epics are integers (`epics list`); requirements and test cases
  are UUIDs with a display reference such as `UR-6-0`, `SR-12-1` or
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

### Pass to command

| Pass (`PROCESS.md`) | Skill | Commands |
|---|---|---|
| Session entry and scope | `rdd-start` | `status`, `auth status`; `epics get <id> --full --json`; `epics plan show <id> --full --json`; `epics plan history <id>` (what changed since the last session, from the record); `requirements get <uuid> --kind user\|system --full --json`; `evidence latest --target-type requirement --target-id <uuid>`; `traces list --entity-type <kind> --entity-id <uuid>` |
| Discover and classify | `rdd-discover` | `requirements search`, `requirements list`, `epics search`, `search`, `read-doc`, `ask`; a product discovery outside the Epic: `backlog create --file`; missing capabilities follow the System-name rule above |
| Plan | `rdd-plan` | new behavior: `requirements create --kind user\|system --file`; the Epic: `epics create --file`, `epics update <id> --file` (title, description, scope, non_goals in any state but IN_PROGRESS and IN_REVIEW; outcome_source, impact_assessment and shared_context while PROPOSED only; every edit audited); test definitions before review: `test-cases create --file` (or reuse existing IDs); the plan including `verification`: `epics plan save <id> --file plan.json --reason "<why>"`, then `epics plan show <id> --full --json`; each backlog, gap or tooling record the plan delivers, at plan save and again when a later save adds one: `backlog update <uuid> --file` with `{"disposition":"ROUTED to <epic id>","disposition_ref":"USER:<date>:plan-save"}`, read back with `backlog get <uuid> --json` |
| Cold review | `rdd-cold-review` | the agent handling the task fetches and retains `epics gates review-bundle <id> --json` plus source material from `epics plan show --full --json`, `requirements get`, `traces list`, `evidence latest`, and supplies these inputs to the read-only reviewer; the reviewer does not invoke shell commands. The agent handling the task records the exact independent review using `epics gates review <id> --file`, and finding resolutions using `epics gates resolve <id> --file`; after recording the plan review, run `epics gates check <id> --phase plan-ready --json`; planning changes use `epics plan save` and need a fresh review; a product finding outside the Epic becomes a `backlog create` record; missing capabilities follow the System-name rule above |
| Implementation approval | `rdd-entry-review` | run `epics gates check <id> --phase implementation --json` and resolve blockers before asking; the human decides; the session records the answer: `epics plan approve-implementation <id> --file reviewed-plan.json --presentation-file reviewed-bundle.json --reason "USER:<date>: <answer>"`, where the files are the exact `epics gates review-bundle <id> --json` shown to the human and its `plan` object; retain them before asking and never refresh them to reuse an earlier answer; `--presentation-file` is required when the plan declares decisions; read back `process_status: IN_PROGRESS`, `implementation_approval`, and each named requirement `IN_PROGRESS` with `change_epic_id` set |
| Implement | `rdd-build` | `epics gates start-execution <id> --file` before RED; run the declared command and finalize via `evidence record --file` with returned `run_id` as `id`, `status: complete`, actual assertion failure and `role: RED`; read back before registering GREEN. Declare the passing candidate using `epics gates candidate <id> --file`, register GREEN, run, finalize and read back; repeat registration for every rerun |
| Verify | `rdd-verify` | `epics gates start-execution <id> --file` before every focused, boundary, user-flow or verification rerun; execute the declared command, then `evidence record --file` using the returned `run_id` as `id` and `status: complete`; read back `evidence latest`, `evidence runs --full --target-id <uuid>` |
| Review, publish and complete | `rdd-completion-review` | finish and finalize all evidence, then obtain independent implementation code review of the retained `epics gates review-bundle <id> --json` supplied by the agent handling the task; the agent handling the task records it with `epics gates review <id> --file` (`phase: implementation`, `scope_implemented: true`) and records material-finding resolutions with `epics gates resolve <id> --file`; run `epics gates check <id> --phase completion-review --json`, then `epics plan submit-review <id> --reason` (to IN_REVIEW; the approver runs it); run `epics gates check <id> --phase completion --json` while IN_REVIEW and retain `epics gates presentation <id> --json` before presenting its exact content/evidence to the human; the human decides; `epics plan approve-completion <id> --file presentation.json --reason "USER:<date>: <answer>"`, or, for a "not yet", `epics plan return-to-implementation <id> --file <presentation.json> --reason "USER:<date>: <answer>"` with the same retained presentation (back to IN_PROGRESS under the same approval; build on, renew whatever changed, then pass the completion-review check and `submit-review` again); the server refuses that return, as printed, with `Give the human's answer as a non-empty reason, such as a USER: source.` without a reason, `Present the exact completion bundle the human answered.` without the bundle, and `The completion bundle changed after the human saw it. Present the current bundle and record the human's answer to it.` when the file is not the current presentation; read back `epics plan show --full --json` (`completion_approval`, `versions`) and `requirements get` (`DONE`, revision +1); then each record routed to the Epic (`epics get <id> --fields backlog --json`): `backlog update <uuid> --file` with `{"disposition":"CLOSED by <epic id>","disposition_ref":"USER:<date>:completion"}` after completion approval, read back with `backlog get <uuid> --json`; a record the human keeps open stays OPEN with a note. Every plan write takes `--agent <slug>`, the agent that ran it; the signed-in person is the human behind it, and a slug that is blank once trimmed is refused with `Name an agent with a non-empty agent slug, or send no actor so the signed-in person is the actor.` |
| Discoveries and blockers | `rdd-triage` | `backlog create --file` (`kind`: `backlog` or `gap` for product discoveries; `tooling` for missing capabilities only under the System-name rule above); a record a plan delivers is routed in the store, not by naming it in a plan body: `backlog update <uuid> --file` with `{"disposition":"ROUTED to <epic id>","disposition_ref":"USER:<date>:plan-save"}`, read back with `backlog get <uuid> --json`; `requirements obsolete <uuid> --kind … --reason`; `requirements set-context`; new scope after approval: a new Epic and plan, never an edit of the approved one |
| Record existing behavior | `rdd-reverse-engineer` | `reverse-engineer schema`, then `reverse-engineer record --file group.json`; read back `requirements get --full`, `traces list` |
| Verify existing behavior | `rdd-reverse-engineer-verify` | `evidence record --file` with `target_type: criterion` results (see `evidence record`); `reverse-engineer review --file selection.json --json > review.json` lists what still blocks acceptance under `problems` |
| Accept existing behavior | `rdd-reverse-engineer-accept` | `reverse-engineer review … --json > review.json`; the human decides; `reverse-engineer accept --file acceptance.json`, wrapping that exact bundle with `source` and `reason` |

Declare each required question with `epics gates declare-decision <id> --file`
using `{"question":"..."}` while PROPOSED. Read back the retained definition's
`content.id`, add that issued UUID to `plan.verification.decisions` with
`epics plan save`, then record the actual human answer. Never invent decision IDs.
The review bundle retains the question definition beside its effective answer.

A human answer is recorded as the `--reason` (`USER:<date>: …`) of the
approval, return or acceptance that applies it; the human's "not yet" at
completion review is the `--reason` of `epics plan return-to-implementation`,
with the declined completion bundle as its `--file`.
`epics gates review` records independent reviews; `epics gates decision` records
answers to declared decisions before the final independent plan review, using `decision_id`, `answer` and a `USER:` source,
only while PROPOSED. The latest attributable answer for each declared decision
is in the review bundle. A new answer invalidates the previous independent
review and human approval presentation. Refresh the bundle, renew the review,
and ask the human about that exact content again. Implementation approval
freezes answers; changed intent requires separately approved work.
No command records an autopilot sprint grant; handle that missing capability
under the System-name rule above.

The four named server checks use `epics gates check <id> --phase
plan-ready|implementation|completion-review|completion [--git-sha <full-sha>] [--json]`.
Each explicit check records its result and blockers in the Audit Trail with the
actor, server time, Git SHA source and known dirty status. The CLI defaults to
checkout HEAD; `--git-sha` supplies an explicit full 40- or 64-character SHA
with unknown dirty status. Outside Git, SHA is recorded as unavailable. The SHA
is caller-supplied trace information, not a server-verified repository revision.
Passing and blocked checks are recorded. Audit failure returns an error. Checks
return all specific blockers and responsible RDD skills, and exit nonzero when
blocked. Reading a preview or review bundle records no check. A check does not
change Epic lifecycle or grant human approval.
Declare `plan.verification` before approval; see `epics gates --help` for its
criteria, test cases, checks, boundaries and decisions shape. Registered
executions and immutable finalization order prevent replay or an older PASS
from hiding a later run. New candidates require fresh passing evidence and
independent review. Preview success grants no permission; transitions recheck
atomically and human completion approval remains separate. Legacy active plans
without obligations fail closed and need explicit human-authorized recovery below.

The implementation approver's signed-in account must declare candidates and
register executions. Another workspace using that account is allowed; a runner
attribution or CI `kind` does not authorize a different account. CI must arrange
registration through that authorized account before execution. Unregistered
results for any criterion or project check declared by an active Epic are refused,
including CI feeds and existing-behavior verification; unrelated evidence is not
subject to that registration rule. Active Epic-owned work stays on the Epic path.

RED must be finalized before GREEN registration. If GREEN was registered too
early, finalize the valid original RED and register and execute a new GREEN;
a new RED is not inherently necessary. Candidate declaration is required before
GREEN, not RED. A changed test definition, decision answer, membership, candidate
or selected evidence changes the review bundle and needs renewed review. An
identical save or unrelated evidence write does not itself invalidate a review.
Record material-finding resolutions explicitly, including findings from older
reviews; a corrected plan alone does not record a resolution.

The agent handling the task translates the independent review into the documented review
JSON: phase, unchanged presentation, reviewer context, all criterion and obligation
IDs, material/note findings, scope implementation confirmation and reason. Context
labels attest to separate review contexts, not different human reviewers. Never
replace the supplied presentation with a newer bundle under an old verdict.

For an active legacy Epic without verification obligations, obtain explicit human
authorization for recovery, retaining the exact currently approved plan. The
current implementation approver may run `epics gates recover-legacy <id> --file`
with `{"plan":<exact current approved plan>,"source":"USER:...","reason":"..."}`.
This audited operation returns the Epic and owned requirements to PROPOSED and
releases ownership, preserving published content and history. Read back the
result, save a complete verification plan, answer required decisions, renew the
independent review and obtain fresh implementation approval. Never run recovery
merely because a gate failed; it needs the human's actual recovery decision.

### Lifecycle, as the commands see it

```text
epics create             -> Epic PROPOSED
epics plan save          -> saved plan (PROPOSED only; repeat to revise; `epics plan history` lists every revision)
plan approve-implementation -> Epic IN_PROGRESS; named requirements IN_PROGRESS, owned by the Epic and the approver
plan submit-review       -> Epic and owned requirements IN_REVIEW (the approver runs it)
epics gates presentation -> the completion bundle the human answers (read-only, Epic IN_REVIEW)
plan return-to-implementation --file <presentation.json> -> Epic and owned requirements IN_PROGRESS again (the human's "not yet" with the declined bundle; plan, approval and ownership kept; --reason required)
                         refused, as printed: "Give the human's answer as a non-empty reason, such as a USER: source." (no --reason);
                         "Present the exact completion bundle the human answered." (no bundle);
                         "The completion bundle changed after the human saw it. Present the current bundle and record the human's answer to it." (not the current bundle)
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
| `below an already bound workspace` / `linked git worktree` | cli | `install`, `init` or `init workspace` run in a directory under a bound workspace root, or `init` at a linked worktree root | run it from the workspace root the refusal names (the main checkout for a worktree); `modernpath init --force` binds a nested workspace deliberately |
| `does not belong to the bound System` | cli | an Epic or review bundle of another System | check `modernpath status`; use an id of this System |
| `unknown input field` / `input must include` / `must be an array` | cli | a file key outside the documented shape; nothing was sent | correct the key (see the command's section) |
| `; the Epic is` | server 409, `invalid_lifecycle_state` | a plan write in the wrong Epic state, printed as `<operation> requires <state>; the Epic is <state>.`: `save` and `approve-implementation` need PROPOSED, `submit-review` IN_PROGRESS, `return-to-implementation` and `approve-completion` IN_REVIEW | read `epics plan show`; run the command that state allows |
| `Epic planning fields are editable only while the Epic is PROPOSED` | server 409 | `epics update` with outcome_source, impact_assessment or shared_context on an Epic that is not PROPOSED | a planning correction after approval is a human decision and new work |
| `unsupported Epic fields` | server 422 | `epics update` naming a server-assigned field (tenant_id, system_id, process_status, plan, metadata, …); nothing was written | send only title, description, scope, non_goals, outcome_source, impact_assessment, shared_context or the date, severity, category, profile, assignee and release fields |
| `references and accepted_context_revision are server-owned` | server 422 | `epics update` shared_context whose references or accepted_context_revision differ from the stored values | read `epics get <id> --fields shared_context --json`; send those values unchanged and edit decisions, assumptions, open_questions |
| `requirement owned` | server 409 | the approval names a requirement another Epic owns, or acceptance names an owned one | finish or release the owning Epic; plan without it |
| `not change owner` | server 409 | candidate declaration, execution registration or `submit-review` by someone other than the implementation approver | use the implementation approver's signed-in account; runner attribution does not confer permission |
| `requirement not owned` / `no plan` | server | completion on an Epic whose requirements are not owned by it, or that has no saved plan | re-plan and re-approve |
| `implementation_presentation_required` | gate blocker | declared decisions need the exact bundle shown to the human | capture `epics gates review-bundle`, show its plan and answers, then supply it with `--presentation-file` after approval |
| `implementation_presentation_changed` | gate blocker | content or decision answers differ from the approved presentation | obtain a current bundle, renewed independent review and new human approval; never reuse the old answer |
| `incomplete reviewed content` | server | `approve-implementation` with a partial plan: a requirement without every content field, a criterion without every field, or no `traces` key | send the `plan` object `epics plan show` returned, unchanged |
| `invalid plan` / `invalid requirement proposal` / `invalid requirement content` / `invalid criterion` / `invalid trace` | server | a key outside the plan shape, an empty `body`, or a content field of the other requirement kind | fix the named part (see `epics plan save`) |
| `requirement not found` / `trace endpoint not found` | server | a UUID the System does not hold | `requirements create` first, or correct the id |
| `trace outside proposal` | server | a plan trace touching none of the plan's requirements or criteria | drop it, or add that requirement to the plan |
| `document trace must relate` | server | a requirement–document trace whose `link_kind` is not `relates` | use `relates` |
| `trace requirement outside plan` | server 422 | `epics plan save` (or approval) adds, changes or drops a trace whose other requirement is not in the plan; a reactivated stale trace counts as a change | keep that trace unchanged, or add that requirement to the plan |
| `trace criterion has other epic` | server 422 | a trace to a criterion another Epic owns | drop it; that owner cannot join this plan |
| `Give the human's answer as a non-empty reason, such as a USER: source.` | server 422, `reason_required` | `epics plan return-to-implementation` without a non-blank `--reason` | pass the human's answer: `--reason "USER:<date>: <answer>"` |
| `Name an agent with a non-empty agent slug, or send no actor so the signed-in person is the actor.` | server 422, `invalid_actor` | an `actor` whose kind is not `agent` or `human`, or an agent without a non-blank slug | set `--agent <slug>`, or omit it so the signed-in person is the actor |
| `Present the exact completion bundle the human answered` | server 422, `return_presentation_required` | `epics plan return-to-implementation` without the completion bundle | pass the `epics gates presentation <id> --json` the human answered with `--file` |
| `The completion bundle changed after the human saw it` | server 409, `return_presentation_changed` | evidence, a review or content changed after the human saw the bundle passed with `--file` | show the human the current bundle and record their answer to it |
| `Invalid proposed content` with `details` | server 422 | a content field failed the requirement changeset; the fields are listed | fix the named fields |
| `invalid_payload` / `invalid_requirement` / `invalid_requirement_fields` / `invalid_criterion_fields` / `invalid_trace_fields` / `invalid_trace_endpoint` / `invalid_source_file` | server | a `reverse-engineer record` file outside its shape; nothing was written | see `reverse-engineer record` |
| `existing_requirement_conflict` / `existing_trace_conflict` | server | an entry whose id the System holds, with different content | drop the entry to reuse the stored record, or send identical content; a content change is Epic work |
| `review_changed` | server 409 | `reverse-engineer accept` after a requirement, criterion, trace or evidence moved | `reverse-engineer review` again; the human decides on the new bundle |
| `acceptance_not_ready` | server 409 | a bundle that is not `eligible`, or that stopped being eligible | clear each `problems` entry (see `reverse-engineer review`) |
| `requirement_uuid_required` / `invalid_requirement_kind` | server | an evidence result with `target_type: requirement` and no `requirement_kind`, or a `target_id` that is not a UUID | add `requirement_kind` and the UUID |
| `execution_proof_key_conflict` / `delivery_proof_key_conflict` | server 409 | `evidence record` changes proof under the `id` of a run marked `raw_payload.as_built_proof: execution` or `delivery`; ordinary runs allow updates | omit `id`; a new observation is a new run |
| `server rejected the session token`, or any sign-in refusal | cli | the sign-in lapsed | a human runs `modernpath auth`; the agent stops |

Named Epic gate refusals return HTTP 409 with `error.code: epic_gate_blocked`
and `error.gate`: phase, ready=false, skills and all blockers. The CLI preserves
this report. `completion_presentation_changed` requires presenting the current
bundle again; `epic_execution_registration_required` requires `epics gates start-execution` before running tests and recording the returned run ID; `epic_execution_immutable` requires a new registered run rather
than editing proof; `approved_membership_immutable` requires separately approved
work instead of changing active scope. A blocked explicit check still records its audit event; a preview records no event. Neither changes Epic lifecycle.

For an `implementation_review_required` blocker, use `rdd-completion-review` step 2
to obtain independent code review, then record that exact `review-bundle` with
`phase: "implementation"` before submission. `rdd-cold-review` is the planning
review pass. Verification reruns require a fresh execution registration; a prior
run ID cannot be reused for changed results. An unexecuted applicable boundary
needs a criterion or project-check obligation before plan approval.
