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
  the missing capability with `backlog create --file <report.json> --focus` and
  kind `tooling`. Automatically reported issues in ModernPath Systems must enter
  the authenticated reporter's focus. Creation and focus are atomic and append
  a declared lane without clearing other focus; a focus failure creates no report.
  For other Systems, create no missing-capability report or backlog item.
  If the CLI command cannot record the item, do not invent another
  write channel. Never call the API by hand, edit a store file or keep a
  local ledger. A refused permission is a decision: return it verbatim.
  Report task incompleteness honestly; this routing rule never makes an
  unavailable operation successful.
- Identity: Epics are integers (`epics list`); requirements and test cases
  are UUIDs. Requirements also have display references such as `UR-6-0` or
  `SR-12-1`, whose last number is the revision. TestCases use UUID/title. Pass the UUID wherever a command
  takes `--kind user|system` with an id and in every JSON file; a display
  reference is for a human-facing read. Never invent, derive or reuse an
  identity the store has not issued.
- Input files are JSON objects; `--file -` reads standard input where offered.
  Follow each command's documented file shape. Unknown-field handling varies
  by command: `evidence record` rejects removed fields. Read back identities,
  outcomes and links to confirm the input was retained.
  Pass `--json` for machine output. A refusal exits non-zero with one error
  line; a server refusal reads `<what> (server <status>): <reason>`.
- After each write, read the result back (`epics plan show`, `requirements
  get`, `traces list`, `evidence latest`) and confirm the expected state
  before reporting it.

### Pass to command

| Pass (`PROCESS.md`) | Skill | Commands |
|---|---|---|
| Session entry and scope | `rdd-start` | `status`, `auth status`; `epics get <id> --full --json`; `epics plan show <id> --full --json`; `epics plan history <id>` (what changed since the last session, from the record); `requirements get <uuid> --kind user\|system --full --json`; `evidence latest --target-type requirement --target-id <uuid>`; `traces list --entity-type <kind> --entity-id <uuid>`; the Roadmap: `roadmap show` (each Initiative with its context and the Epics it covers); the planning contexts of the Epic's Release and Initiative: `epics get <id> --fields planning_contexts` |
| Discover and classify | `rdd-discover` | `requirements search`, `requirements list`, `epics search`, `search`, `read-doc`, `ask`; a product discovery outside the Epic: `backlog create --file`; missing capabilities follow the System-name rule above |
| Plan | `rdd-plan` | before planning, `roadmap show` for the context of the Initiative the Epic is linked to, and `epics get <id> --fields planning_contexts` for the planning contexts of its Release and Initiative; on a person's word, `roadmap apply --file plan.json` (`--dry-run` first); new behavior: `requirements create --kind user\|system --file`; correct an unowned, never-published draft: `requirements update <uuid\|display-ref> --file` (same content fields as create; criteria and traces preserved); the Epic: `epics create --file`, `epics update <id> --file` (title, description, scope, non_goals in any state but IN_PROGRESS and IN_REVIEW; outcome_source, impact_assessment and shared_context while PROPOSED only; every edit audited); test definitions before review: `test-cases create --file` (or reuse existing IDs); the plan including `verification`: `epics plan save <id> --file plan.json --reason "<why>"`, then `epics plan show <id> --full --json`; each backlog, gap or tooling record the plan delivers, at plan save and again when a later save adds one: `backlog update <uuid> --file` with `{"disposition":"ROUTED to <epic id>","disposition_ref":"USER:<date>:plan-save"}`, read back with `backlog get <uuid> --json`; before independent review, inspect `epics gates review-bundle <id> --phase implementation --json` and correct structural blockers (a missing review is expected) |
| Cold review | `rdd-cold-review` | the agent handling the task fetches and retains `epics gates review-bundle <id> --json` plus source material from `epics plan show --full --json`, `requirements get`, `traces list`, `evidence latest`, and supplies these inputs to the read-only reviewer; the reviewer does not invoke shell commands. The agent handling the task records the exact independent review using `epics gates review <id> --file`, then runs `epics gates check <id> --phase plan-ready --json` once and presents the exact plan, review and blockers to the human, even when blocked; do not revise or repeat review before that decision; human-directed finding resolutions, including notes the human leaves to the build, use `epics gates resolve <id> --file`; a human-requested plan revision uses `epics plan save`, one new review and another human decision; a product finding outside the Epic becomes a `backlog create` record; missing capabilities follow the System-name rule above |
| Implementation approval | `rdd-entry-review` | run `epics gates check <id> --phase implementation --json` once; present the current plan, decisions, review findings and blockers; wait for the human to approve eligible content, request revision or further review, or reject; after eligible approval, `epics plan approve-implementation <id> --reason "USER:<date>: <answer>"`; read back IN_PROGRESS and requirement ownership. Upload no plan or presentation. |
| Implement | `rdd-build` | before the first change, `epics get <id> --fields planning_contexts`; before building, `epics gates presentation <id> --json` for `changes_since_approval` and `assumed_defaults`; run the focused assertion before implementation, confirm the expected failure, then implement and run focused checks. Record current criterion/TestCase outcomes with `evidence record --file`. |
| Verify | `rdd-verify` | Run proportional checks and user scenarios; record actual criterion outcomes and read back `evidence latest`. Builds and regressions are development checks. |
| Review, publish and complete | `rdd-completion-review` | finish evidence before independent code review; `epics gates review-bundle <id> --json` supplies current facts; record the actual verdict with `epics gates review <id> --file` (phase implementation, scope_implemented true). Resolve findings with `epics gates resolve`; `epics gates check <id> --phase completion-review --json`; `epics plan submit-review <id> --reason`; `epics gates check <id> --phase completion --json`; `epics gates presentation <id> --json` presents current facts. The human separately decides: `epics plan approve-completion <id> --reason "USER:..."`, or, for "not yet", `epics plan return-to-implementation <id> --reason "USER:..."`. Both send only a decision, recheck current state and preserve ownership rules. A return can succeed when readiness fails. Read back DONE, versions and released ownership, or IN_PROGRESS with the same approval. Then, for each backlog, gap or tooling record routed to the Epic, use `backlog update <uuid> --file` to record {"disposition":"CLOSED by <epic id>","disposition_ref":"USER:<date>:completion"} and read it back. An assumed default is never closed by the Epic: it stays routed until a person accepts or rejects it with their own words as the `USER:` source (`ACCEPTED with USER:<their words>`, `REJECTED with USER:<their words>`). Changed scope requires an explicit `epics plan return-to-planning <id> --reason "USER:..."`, renewed planning/review/approval. Every plan write supports optional --agent attribution. |
| Discoveries and blockers | `rdd-triage` | `backlog create --file` (`kind`: `backlog` or `gap` for product discoveries; `tooling` for missing capabilities only under the System-name rule above; `assumption` for an assumed default, the default as `title` and its question as `observation`); a record a plan delivers is routed in the store, not by naming it in a plan body: `backlog update <uuid> --file` with `{"disposition":"ROUTED to <epic id>","disposition_ref":"USER:<date>:plan-save"}`, read back with `backlog get <uuid> --json`; use this template only for a backlog, gap or tooling record and for an assumed default's first routing, to the Epic being built; moving an assumed default that is already routed needs the person's own words as the `USER:` source in `disposition_ref`; `requirements obsolete <uuid> --kind … --reason`, or `requirements obsolete --file` for an all-or-nothing batch of up to 200; `requirements set-context`; changed active scope: explicit human `epics plan return-to-planning <id> --reason "USER:..."`, then revise, review and re-approve; DONE needs a follow-up Epic |
| Record existing behavior | `rdd-reverse-engineer` | `requirements contexts` (the System's list of contexts; every recorded requirement takes a listed code and leaves the name out, or a new code and name no listed context has), `reverse-engineer schema`, then `reverse-engineer check --dir groups` (offline), then `reverse-engineer record --file group.json` or `reverse-engineer record --dir groups` for a folder; read back `requirements get --full`, `traces list`; regroup a recorded requirement with `requirements set-context` or `requirements update` |
| Verify existing behavior | `rdd-reverse-engineer-verify` | Read `reverse-engineer review`; reuse complete current proof or run required tests and record each criterion/TestCase outcome. |
| Accept existing behavior | `rdd-reverse-engineer-accept` | `reverse-engineer review --file selection.json --json`; present current facts; the human decides; `reverse-engineer accept --file acceptance.json` sends requirements (kind/id), source and reason only. The server checks current eligible facts under locks. |

Declare each required question with `epics gates declare-decision <id> --file`
using `{"question":"..."}` while PROPOSED. Read back the retained definition's
`content.id`, add that issued UUID to `plan.verification.decisions` with
`epics plan save`, then record the actual human answer. Never invent decision IDs.
Several questions or answers go in one call as `{"items":[...]}` of the same
objects (at most 200, no duplicates); the batch writes all or nothing, returns
the ids in input order, and a refusal names `item <index>`.
The review bundle retains the question definition beside its effective answer.

A human answer is recorded as the `--reason` (`USER:<date>: …`) of the
approval, return or acceptance that applies it; the human's "not yet" at
completion review is the `--reason` of `epics plan return-to-implementation`,
without a presentation upload.
`epics gates review` records independent reviews; `epics gates decision` records
answers to declared decisions before the independent plan review, using `decision_id`, `answer` and a `USER:` source,
only while PROPOSED. The latest attributable answer for each declared decision
is in the review bundle. A new answer invalidates the previous independent
review. Present the change to the human, who directs renewed review before approval. Implementation approval
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
change Epic lifecycle or grant human approval. Repeat a planning check only after
a relevant condition changes. Gate checks and finding resolutions do not themselves
require another independent review; resuming a session reuses the recorded review.
Declare `plan.verification` before approval; see `epics gates --help` for its
criteria, test cases, boundaries and decisions shape. Completion reads current
criterion proof and requires an independent implementation review. Human
implementation and completion approvals remain separate.

An existing-behavior Epic declares its mode and reason. Check current passing
results and scoped requirement, criterion, TestCase and source Traces. Product
or test changes use ordinary implementation.
Use epics retire <id> to retire an unnecessary Epic while preserving requirements
and history; the server refuses an Epic that owns approved requirement changes.

Record actual outcomes with `evidence record --file` for a criterion and its
TestCases. A batch may update up to 50 criteria with up to 500 tests each, in
one atomic request. The latest committed outcome for each pair is current.
Identical retries retain identities. Scope and active Epic ownership are checked.
Material current criterion outcome changes in review scope outdate the Epic
review. Identical retries change nothing. Modernization history leaves the
review current. Reviewed content, code or structural relationship changes also
require renewed review. Record finding resolutions.
The Epic’s implementation approver or assignee may record its proof and submit
it for review; the approver alone can return it to planning.

The session records independent review as phase, reviewer context, criterion and
obligation IDs, findings, scope implementation confirmation and reason. Store no
presentation. The server derives applicability from current state and existing
semantic changes. Context and reason attest separate review work.

To revise an IN_PROGRESS or IN_REVIEW Epic, obtain the human's explicit return
to planning decision. The current implementation approver runs:

```sh
modernpath epics plan return-to-planning <id> --reason "USER:<actual decision and reason>" [--carry-decision <decision-id> ...]
```

The optional `--agent <slug>` names the agent on the Epic and requirement audit
events; without it the signed-in person is recorded as the actor. The payload
actor is separate from the approved plan and return decision.

The operation atomically
returns the Epic and its selected requirements to PROPOSED, withdraws approval
and releases ownership. The Audit Trail retains the actor, time, reason, prior
state, withdrawn approval attribution, the criteria snapshot and the carried
decisions (`carry_decisions`). Published content, revisions and accepted requirement versions remain intact. Stale content, another approver,
ownership conflicts and failed writes leave everything unchanged. DONE stays
closed; create a follow-up Epic.

Return clears current proof for each published criterion whose approved meaning
differs from its published meaning; unchanged criterion proof remains current.
Removed unpublished criteria lose their proof. Read back the return, revise the
draft, answer newly declared decisions, record a new plan review and obtain human
implementation approval. Retest each changed published criterion after reapproval. A new
independent implementation review and separate completion approval are required.

`epics gates recover-legacy` remains a compatibility command limited to active
plans without verification obligations. It takes
`{"source":"USER:...","reason":"..."}`;
normal replanning uses `epics plan return-to-planning`.

### Lifecycle, as the commands see it

```text
epics create             -> Epic PROPOSED
epics plan save          -> saved plan (PROPOSED only; repeat to revise; `epics plan history` lists every revision)
plan approve-implementation -> Epic IN_PROGRESS; named requirements IN_PROGRESS, owned by the Epic and the approver
plan return-to-planning  -> active Epic and selected requirements PROPOSED; approval withdrawn; ownership released; listed decisions and unchanged RED carry
plan submit-review       -> Epic and owned requirements IN_REVIEW (the approver or the assignee runs it)
epics gates presentation -> the completion bundle the human answers (read-only, Epic IN_REVIEW)
plan return-to-implementation --reason "USER:..." -> Epic and owned requirements IN_PROGRESS (the human's "not yet"); plan, approval and ownership kept

plan approve-completion  -> approved content published: revision +1, immutable version; Epic DONE; ownership released
reverse-engineer record  -> requirements PROPOSED at revision 0, with their traces
reverse-engineer accept  -> unowned PROPOSED requirements DONE: revision +1, an existing_behavior version
```

A requirement owned by an Epic (`change_epic_id` set) is not accepted,
obsoleted, regrouped or planned elsewhere until that Epic completes or explicitly
returns to planning. Epic content and the plan are read-only in `IN_PROGRESS`
and `IN_REVIEW`; the human-authorized return ends approval before edits.

### Refusals and what clears them

| Refusal (substring) | Origin | Cause | Clears it |
|---|---|---|---|
| `connect this workspace to a system` / `no system_id` | cli | no binding, or a nested `.modernpath/config.json` | run from the workspace root; `modernpath init` binds |
| `below an already bound workspace` / `linked git worktree` | cli | `install`, `init` or `init workspace` run in a directory under a bound workspace root, or `init` at a linked worktree root | run it from the workspace root the refusal names (the main checkout for a worktree); `modernpath init --force` binds a nested workspace deliberately |
| `requirement_in_progress` / `requirement_owned` | server 409 | `update`, `set-context` or `obsolete` targets approved or Epic-owned work; existing-behavior acceptance also refuses owned work | finish through the owning Epic, or ask its implementation approver to return it to planning; then apply the requested operation's lifecycle rules |
| `requirement_already_published` | server 409 | a content `update` or `obsolete` targets DONE, a nonzero revision or an accepted version | use a follow-up Epic for published content; grouping-only updates may still be allowed |
| `invalid_requirement_status` | server 409 | a content edit or obsoletion targets an ineligible state | content edits and new obsoletions require unowned, unpublished PROPOSED, BLOCKED or DEFERRED requirements; repeated obsoletion is unchanged |
| `items_required` / `too_many_items` / `duplicate_items` / `invalid_item` | server 422 | a `requirements obsolete --file` batch is empty, holds more than 200 items, repeats an item or names an item without kind `user`/`system` and a UUID; nothing was written | fix the file; a refusal naming `item <index> (<kind> <id>)` points at the failing entry, and the whole batch is retried after the fix |
| `stale_reference` | server 409 | an exact requirement reference names an older or incorrect revision | read the current requirement and retry with its exact reference |
| `does not belong to the bound System` | cli | an Epic or review bundle of another System | check `modernpath status`; use an id of this System |
| `unknown input field` / `input must include` / `must be an array` | cli | a file key outside the documented shape; nothing was sent | correct the key (see the command's section) |
| `; the Epic is` | server 409, `invalid_lifecycle_state` | a plan write in the wrong Epic state, printed as `<operation> requires <state>; the Epic is <state>.`: `save` and `approve-implementation` need PROPOSED, `submit-review` IN_PROGRESS, `return-to-implementation` and `approve-completion` IN_REVIEW, `return-to-planning` IN_PROGRESS or IN_REVIEW | read `epics plan show`; run the command that state allows |
| `Epic planning fields are editable only while the Epic is PROPOSED` | server 409 | `epics update` with outcome_source, impact_assessment or shared_context on an Epic that is not PROPOSED | return the active Epic to planning with the human's explicit decision before editing; DONE uses a follow-up Epic |
| `unsupported Epic fields` | server 422 | `epics update` naming a server-assigned field (tenant_id, system_id, process_status, plan, metadata, …); nothing was written | send only title, description, scope, non_goals, outcome_source, impact_assessment, shared_context or the date, severity, category, profile, assignee and release fields |
| `references and accepted_context_revision are server-owned` | server 422 | `epics update` shared_context whose references or accepted_context_revision differ from the stored values | read `epics get <id> --fields shared_context --json`; send those values unchanged and edit decisions, assumptions, open_questions |
| `requirement owned` | server 409 | the approval names a requirement another Epic owns, or acceptance names an owned one | finish or release the owning Epic; plan without it |
| `not change owner` | server 403 or 409 | criterion proof update or `submit-review` by someone other than the implementation approver or the Epic's assignee | use the signed-in account of the implementation approver or the Epic's assignee |
| `not change owner` | server 409 | `return-to-planning` or `recover-legacy` by someone other than the implementation approver | use the implementation approver's signed-in account; the assignee cannot return the Epic to planning |
| `invalid_planning_return` | server 422 | an unknown field or no explicit `USER:` decision in `--reason` | supply only the human's actual return decision |
| `planning_return_write_failed` | server 500 | the atomic return could not be written; approval and ownership are unchanged | retry after the write failure is resolved |
| `planning_return_outcome_unknown` / `return may already have completed` | server 500 | the connection was lost before the result could be confirmed; the return may have committed | read `epics plan show <id> --full --json` and `epics plan history <id> --full --json` before deciding whether to retry |
| `requirement not owned` / `no plan` | server | completion on an Epic whose requirements are not owned by it, or that has no saved plan | re-plan and re-approve |
| `invalid plan` / `invalid requirement proposal` / `invalid requirement content` / `invalid criterion` / `invalid trace` | server | a key outside the plan shape, an empty `body`, or a content field of the other requirement kind | fix the named part (see `epics plan save`) |
| `requirement not found` / `trace endpoint not found` | server | a UUID the System does not hold | `requirements create` first, or correct the id |
| `trace outside proposal` | server | a plan trace touching none of the plan's requirements or criteria | drop it, or add that requirement to the plan |
| `document trace must relate` | server | a requirement–document trace whose `link_kind` is not `relates` | use `relates` |
| `trace requirement outside plan` | server 422 | `epics plan save` (or approval) adds, changes or drops a trace whose other requirement is not in the plan | keep that trace unchanged, or add that requirement to the plan |
| `trace criterion has other epic` | server 422 | a trace to a criterion another Epic owns | drop it; that owner cannot join this plan |
| `Give the human's answer as a non-empty reason, such as a USER: source.` | server 422, `reason_required` | `epics plan return-to-implementation` without a non-blank `--reason` | pass the human's answer: `--reason "USER:<date>: <answer>"` |
| `Name an agent with a non-empty agent slug, or send no actor so the signed-in person is the actor.` | server 422, `invalid_actor` | an `actor` whose kind is not `agent` or `human`, or an agent without a non-blank slug | set `--agent <slug>`, or omit it so the signed-in person is the actor |
| `Invalid proposed content` with `details` | server 422 | a content field failed the requirement changeset; the fields are listed | fix the named fields |
| `invalid_payload` / `invalid_requirement` / `invalid_requirement_fields` / `invalid_criterion_fields` / `invalid_trace_fields` / `invalid_trace_endpoint` / `invalid_source_file` | server | a `reverse-engineer record` file outside its shape; nothing was written | see `reverse-engineer record` |
| `context_required` | server 422 | `reverse-engineer record` creates a requirement without a context, null or blank, also one given by a client UUID the System does not hold; nothing was written | give it a code from `requirements contexts`, or a new code and name no listed context has; the refusal names the requirement's position in the file, counted from 0, and its title |
| `context_name_mismatch` | server 422 | `reverse-engineer record` creates a requirement in a listed context with another name than the list's, also one that differs only in letter case; nothing was written | leave `context_name` out, or send the list's name the refusal names; rename a context with `requirements update` |
| `context_code_conflict` | server 422 | `reverse-engineer record`, `requirements set-context` or `requirements update` uses a code that differs from a listed code only in letter case; nothing was written | use the listed code the refusal names |
| `context_name_conflict` | server 422 | `reverse-engineer record`, `requirements set-context` or `requirements update` gives a context a name that another listed context has, or that requirements of another context carry; nothing was written | use the listed context that has the name, or a name no listed context has |
| `invalid_context` | server 422 | `reverse-engineer record`, `requirements set-context` or `requirements update` sends a value with spaces before or after it, a blank name, the code `—` or Unclassified in any letter case, which stand for requirements without a context; also `roadmap apply` with a context over 255 bytes; nothing was written | send the value without the spaces, and a code and name of a real context |
| `existing_requirement_conflict` / `existing_trace_conflict` | server | an entry whose id the System holds, with different content | drop the entry to reuse the stored record, or send identical content; a content change is Epic work |
| `native_test_chain_required` | server 422 | an executed TestCase has no active, same-System Trace to a source_file or file_analysis | establish the inspected test/code relationship with `reverse-engineer record` before execution; repair a missing relationship and record the actual outcome |
| `acceptance_not_ready` | server 409 | selected current requirements that are not `eligible` | clear each `problems` entry (see `reverse-engineer review`) |
| `server rejected the session token`, or any sign-in refusal | cli | the sign-in lapsed | a human runs `modernpath auth`; the agent stops |

Named Epic gate refusals return HTTP 409 with `error.code: epic_gate_blocked`
and `error.gate`: phase, ready=false, skills and all blockers. The CLI preserves
this report. `approved_membership_immutable` requires an explicit human return
to planning before scope edits, or separately approved work. A blocked explicit
check records its audit event; a preview records no event. Neither changes Epic
lifecycle.

For an `implementation_review_required` blocker, use `rdd-completion-review`
to obtain independent code review and record its verdict and coverage. Record
current outcomes per criterion and TestCase with `evidence record`; use
`evidence latest` to read them back. The latest committed pair outcome wins,
including failures. No raw output or historical proof presentation is stored.

### Requirement completion without an Epic

After reading complete current criterion proof and scoped source/test Traces,
present that current content and evidence for separate human completion approval, then
use `requirements update <uuid|display-ref> --file completion.json --reason
"<human completion decision>"` with
`{"work_status":"DONE","source":"USER:<decision>"}`. A blocked update reports
requirement and criterion blockers without writes. Read back DONE, revision,
version and approval attribution. Clients do not supply the classification; the
server records `requirement_completion` in the existing NOT NULL `acceptance_kind`
column, with no Epic.
Active Epic-owned requirements use their Epic's completion gate. UUID retries
of unowned DONE requirements are unchanged; exact stale display references fail.
