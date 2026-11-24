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
| Session entry and scope | `rdd-start` | `status`, `auth status`; `epics get <id> --full --json`; `epics plan show <id> --full --json`; `epics plan history <id>` (what changed since the last session, from the record); `requirements get <uuid> --kind user\|system --full --json`; `evidence latest --target-type requirement --target-id <uuid>`; `traces list --entity-type <kind> --entity-id <uuid>`; the Roadmap: `roadmap show` (each Initiative with its context and the Epics it covers); the planning contexts of the Epic's Release and Initiative: `epics get <id> --fields planning_contexts`; at the same moment the System's Active Engineering Constraints the Epic is held to: `constraints list --state active`, and each one the change may touch with `constraints get <ref>` (the Active text is the current revision, not the row text when a change is pending) |
| Discover and classify | `rdd-discover` | `requirements search`, `requirements list`, `epics search`, `search`, `read-doc`, `ask`; a product discovery outside the Epic: `backlog create --file`; missing capabilities follow the System-name rule above |
| Plan | `rdd-plan` | before planning, `roadmap show` for the context of the Initiative the Epic is linked to, and `epics get <id> --fields planning_contexts` for the planning contexts of its Release and Initiative, and at the same moment `constraints list --state active` for the Active Engineering Constraints the plan is held to, with `constraints get <ref>` for the text of each one the change may touch (the current revision), named in the reconnaissance by its EC ref; on a person's word, `roadmap apply --file plan.json` (`--dry-run` first); new behavior: `requirements create --kind user\|system --file`; correct an unowned, never-published draft: `requirements update <uuid\|display-ref> --file` (content fields plus optional replacement criteria and traces arrays; omission preserves them, [] clears them); the Epic: `epics create --file`, `epics update <id> --file` (title, description, scope, non_goals in any state but IN_PROGRESS and IN_REVIEW; outcome_source, impact_assessment and shared_context while PROPOSED only; every edit audited); test definitions before review: `test-cases create --file` (or reuse existing IDs); the plan including `verification`: `epics plan save <id> --file plan.json --reason "<why>"`, then `epics plan show <id> --full --json`; a removal Epic lists each published requirement it retires in the plan's `retire` (`{kind, id, reason}`) and each of its implements traces in `remove_traces`; its domain owner confirms each entry with Confirm removal on Your move, or `epics gates confirm-removal <id> --file` with `{requirement_kind, requirement_id, source, reason?}`; each backlog, gap or tooling record the plan delivers, at plan save and again when a later save adds one: `backlog update <uuid> --file` with `{"disposition":"ROUTED to <epic id>","disposition_ref":"USER:<date>:plan-save"}`, read back with `backlog get <uuid> --json`; before independent review, inspect `epics gates review-bundle <id> --phase implementation --json` and correct structural blockers (a missing review is expected) |
| Cold review | `rdd-cold-review` | the agent handling the task fetches and retains `epics gates review-bundle <id> --phase plan-ready --json` (its `engineering_constraints` are the System's Active Engineering Constraints with ref, revision and text, read by the reviewer beside `planning_contexts`; a finding about one cites it as `EC-<number>` with the revision read and has severity `note` when the constraint is its only ground) plus source material from `epics plan show --full --json`, `requirements get`, `traces list`, `evidence latest`, and supplies these inputs to the read-only reviewer; the reviewer does not invoke shell commands. The agent handling the task records the exact independent review using `epics gates review <id> --file`, then runs `epics gates check <id> --phase plan-ready --json` once and presents the exact plan, review and blockers to the human, even when blocked; do not revise or repeat review before that decision, except the full review before approval; human-directed finding resolutions, including notes the human leaves to the build, use `epics gates resolve <id> --file`; a human-requested plan revision uses `epics plan save`, one new review, the full review before approval when it is due, and another human decision; a product finding outside the Epic becomes a `backlog create` record; missing capabilities follow the System-name rule above |
| Implementation approval | `rdd-entry-review` | run `epics gates check <id> --phase implementation --json` once; present the current plan, decisions, review findings and blockers; wait for the human to approve eligible content, request revision or further review, or reject; after eligible approval, `epics plan approve-implementation <id> --reason "USER:<date>: <answer>"`; read back IN_PROGRESS and requirement ownership. Upload no plan or presentation. |
| Implement | `rdd-build` | before the first change, `epics get <id> --fields planning_contexts` and `constraints list --state active`, with `constraints get <ref>` for each Active Engineering Constraint the change may touch (the current revision); before building, `epics gates presentation <id> --json` for `changes_since_approval` and `assumed_defaults`; run the focused assertion before implementation, confirm the expected failure, then implement and run focused checks. Record current criterion/TestCase outcomes with `evidence record --file`. After a delegated build, `epics build record <id> --file <build handoff>` (first with `--dry-run`) records the build's code and test-file traces, its outcomes read from the test result files, and its assumed defaults in one write. |
| Verify | `rdd-verify` | Run the Epic's minimal tests; record actual criterion outcomes and read back `evidence latest`. The full run is the sprint pull request's CI or the project's full-run command. Builds and regressions are development checks. |
| Review, publish and complete | `rdd-completion-review` | wait for a passing full run of the sprint pull request, then finish evidence before independent code review; `epics gates review-bundle <id> --json` supplies current facts; record the actual verdict with `epics gates review <id> --file` (phase implementation, scope_implemented true). Resolve findings with `epics gates resolve`; `epics gates check <id> --phase completion-review --json`; `epics plan submit-review <id> --reason`; `epics gates check <id> --phase completion --json`; `epics gates presentation <id> --json` presents current facts. The human separately decides: `epics plan approve-completion <id> --reason "USER:..."`, or, for "not yet", `epics plan return-to-implementation <id> --reason "USER:..."`. Both send only a decision, recheck current state and preserve ownership rules. A return can succeed when readiness fails. Read back DONE, versions and released ownership, or IN_PROGRESS with the same approval. Then, for each backlog, gap or tooling record routed to the Epic, use `backlog update <uuid> --file` to record {"disposition":"CLOSED by <epic id>","disposition_ref":"USER:<date>:completion"} and read it back. An assumed default is never closed by the Epic: it stays routed until a person accepts or rejects it with their own words as the `USER:` source (`ACCEPTED with USER:<their words>`, `REJECTED with USER:<their words>`). Changed scope requires an explicit `epics plan return-to-planning <id> --reason "USER:..."`, renewed planning/review/approval. Every plan write supports optional --agent attribution. |
| PR review | `rdd-pr-review` | read the pull request with `gh` (base and head, commits, changed files, checks; without `gh`, the branch against its base with `git`); for each Epic the pull request delivers, `epics get <id> --full --json`, `epics plan show <id> --full --json` and `epics gates review-bundle <id> --phase implementation --json` (its `engineering_constraints` are the Active constraints' current revisions), retained as files for the read-only reviewer `rdd-pr-reviewer`; with no Epic, `constraints list --state active --json` and `constraints get <ref> --json`, assessing the current revision, not the row text when a change is pending; the review is returned in chat with findings by severity, intent verdict, constraint assessment (each cited `EC-<number>` with its revision), checks and limits; no store command writes, no review is recorded, posting to GitHub only on the person's request |
| Discoveries and blockers | `rdd-triage` | `backlog create --file` (`kind`: `backlog` or `gap` for product discoveries; `tooling` for missing capabilities only under the System-name rule above; `assumption` for an assumed default, the default as `title` and its question as `observation`); a record a plan delivers is routed in the store, not by naming it in a plan body: `backlog update <uuid> --file` with `{"disposition":"ROUTED to <epic id>","disposition_ref":"USER:<date>:plan-save"}`, read back with `backlog get <uuid> --json`; use this template only for a backlog, gap or tooling record and for an assumed default's first routing, to the Epic being built; moving an assumed default that is already routed needs the person's own words as the `USER:` source in `disposition_ref`; `requirements obsolete <uuid> --kind … --reason`, or `requirements obsolete --file` for an all-or-nothing batch of up to 200; `requirements set-context`; changed active scope: explicit human `epics plan return-to-planning <id> --reason "USER:..."`, then revise, review and re-approve; DONE needs a follow-up Epic |
| Constraints | `rdd-constraint` | an Engineering Constraint is a rule of the System in a person's words, ref `EC-<number>`, states proposed, active and deactivated; the skill restates the rule with the person, drafts the file (`{"title","statement","rationale","verification_method"}`) and, on the person's word, runs `constraints create --file` (its only store write), then reads the ref and the proposed state back with `constraints get <ref>`; a correction before acceptance: `constraints update <ref> --file` (on an active constraint this is a pending change; `constraints discard <ref>` drops it); the person's own decisions, never an agent's: `constraints accept <ref> --reason "USER:<date>: ..."` (writes the revision every Epic is held to) and `constraints deactivate <ref> --reason "USER:<date>: ..."`; `constraints delete <ref>` removes a proposed or deactivated constraint; reads: `constraints list [--state proposed\|active\|deactivated]` (`--state active` is what every Epic is held to) and `constraints get <ref>` (row text, current revision, revisions and audit events); every write takes `--agent <slug>` |
| Record existing behavior | `rdd-reverse-engineer` | `requirements contexts` (the System's list of contexts; every recorded requirement takes a listed code and leaves the name out, or a new code and name no listed context has), `reverse-engineer schema`, then `reverse-engineer check --dir groups` (offline), then `reverse-engineer record --file group.json` or `reverse-engineer record --dir groups` for a folder; read back `requirements get --full`, `traces list`; regroup a recorded requirement with `requirements set-context` or `requirements update` |
| Verify existing behavior | `rdd-reverse-engineer-verify` | Read `reverse-engineer review`; reuse complete current proof or run required tests and record each criterion/TestCase outcome. |
| Accept existing behavior | `rdd-reverse-engineer-accept` | `reverse-engineer review --file selection.json --json`; present current facts; the human decides; `reverse-engineer accept --file acceptance.json` sends requirements (kind/id), source and reason only. The server checks current eligible facts under locks. |
| Modernize a legacy system | `rdd-modernize` | the Release's legacy system and reference revision: `roadmap show`; the kept and changed legacy requirements: `roadmap selection --initiative <id> --json`; from a folder bound to the legacy system: the capture test case with `test-cases create --file`, its `relates` traces to its capture script, the legacy files it exercises and the requirement with `reverse-engineer record --file`; from a folder bound to the new system (in place, the legacy system itself): the capture run with `modernization record --release-id <id> --file capture.json`, its entry with `criterion_ids` an empty list; run in the git checkout that holds the run's commit, the capture carries its commit chain and names the commit by its full id, elsewhere it carries none; read back with `modernization history --system-id <legacy system id> --commit <sha>` and `traces list`; the replay and its break test with `test-cases create --file`, their traces to their files with `reverse-engineer record --file`, then `roadmap recording --file recordings.json` (`--dry-run` first; `--release <id>` when several open Modernization Releases replace the legacy system; `--agent <slug>`), read back with `traces list --entity-type test_case --entity-id <replay uuid>`; the proof: `roadmap proof --initiative <id>`; on the person's explicit word only, with the person's own words as the USER: source and as the words, `roadmap accept-lower-level --file accept.json --agent <slug>` for a row proved below the agreed level |

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
is in the review bundle. A new or changed answer puts the plan review out of
date; the same answer recorded again does not. Present the change to the human, who directs renewed review before approval. Implementation approval
freezes answers; changed intent requires separately approved work.
Record a human grant with `autopilot create --file grant.json --json`.
Input requires `epic_ids`, `expires_at` (RFC3339), `exclusions` ([] if none),
and the human's `USER:` source. The response confirms the saved grant and its
server-assigned identity, grantor and recording time. A grant is separate from
implementation/completion approval and never authorizes merge, deployment or release.

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
Any authorized workspace member may record proof, submit review or return work to planning with an explicit human decision.

The session records independent review as phase, reviewer context, criterion and
obligation IDs, findings, scope implementation confirmation and reason. Store no
presentation. The server derives applicability from current state and existing
semantic changes. Context and reason attest separate review work.

To revise an IN_PROGRESS or IN_REVIEW Epic, obtain the human's explicit return
to planning decision. Any authorized workspace member runs:

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
decisions (`carry_decisions`). Published content, revisions and accepted requirement versions remain intact. Stale content, ownership conflicts and failed writes leave everything unchanged. DONE stays
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
epics gates confirm-removal -> removal_confirmation for one retire entry (PROPOSED only; a changed reason needs a new one)
plan approve-implementation -> Epic IN_PROGRESS; named requirements IN_PROGRESS, owned by the Epic with approval attributed to the person; outside-active-Release requirements require a reviewed --release-choice file
plan return-to-planning  -> active Epic and selected requirements PROPOSED; approval withdrawn; ownership released; listed decisions and unchanged RED carry
plan submit-review       -> Epic and owned requirements IN_REVIEW (any authorized workspace member runs it); outside-active-Release requirements require a reviewed --release-choice file
epics gates presentation -> the completion bundle the human answers (read-only, Epic IN_REVIEW)
plan return-to-implementation --reason "USER:..." -> Epic and owned requirements IN_PROGRESS (the human's "not yet"); plan, approval and ownership kept; outside-active-Release requirements require a reviewed --release-choice file

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
| `requirement_in_progress` / `requirement_owned` | server 409 | `update`, `set-context` or `obsolete` targets approved or Epic-owned work; existing-behavior acceptance also refuses owned work; a `reverse-engineer record` reference entry gives such a requirement another `what_it_has`, and nothing was written | finish through the owning Epic; any authorized user can record an explicit human decision to return an active Epic to planning, then apply the requested operation's lifecycle rules |
| `release_choice_required` | server 409 | a requirement entering IN_PROGRESS or IN_REVIEW is outside the active Release; no transition or membership write occurred | Base/unassigned rows are mandatory moves; `keep` applies only to other-Release rows. Review the placements and target, then retry with `--release-choice <file>` containing action, target Release ID, and every prompted requirement with its expected current Release ID or null. The server owns `move_required`; omit it. Stale choices prompt again |
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
| `invalid_planning_return` | server 422 | an unknown field or no explicit `USER:` decision in `--reason` | supply only the human's actual return decision |
| `planning_return_write_failed` | server 500 | the atomic return could not be written; approval and ownership are unchanged | retry after the write failure is resolved |
| `planning_return_outcome_unknown` / `return may already have completed` | server 500 | the connection was lost before the result could be confirmed; the return may have committed | read `epics plan show <id> --full --json` and `epics plan history <id> --full --json` before deciding whether to retry |
| `requirement not owned` / `no plan` | server | completion on an Epic whose requirements are not owned by it, or that has no saved plan | re-plan and re-approve |
| `invalid plan` / `invalid requirement proposal` / `invalid requirement content` / `invalid criterion` / `invalid trace` | server | a key outside the plan shape, an empty `body`, or a content field of the other requirement kind | fix the named part (see `epics plan save`) |
| `requirement not found` / `trace endpoint not found` | server | a UUID the System does not hold | `requirements create` first, or correct the id |
| `trace outside proposal` | server | a plan trace touching none of the plan's requirements or criteria | drop it, or add that requirement to the plan |
| `document trace must relate` | server | a requirement–document trace whose `link_kind` is not `relates` | use `relates` |
| `trace requirement outside plan` | server 422 | `epics plan save` (or approval) adds, changes or drops a trace whose other requirement is not in the plan | keep that trace unchanged, or add that requirement to the plan |
| `retire_trace_not_removed` | gate | a retired requirement or one of its criteria keeps an active implements trace the plan's `remove_traces` does not list | list the trace ID in `remove_traces` and save the plan again |
| `removal_confirmation_required` | gate | a retire entry has no confirmation in this planning cycle that matches its kind, id and reason | the domain owner confirms it: Confirm removal on Your move, or `epics gates confirm-removal <id> --file` |
| `retire_not_published` | server 422 | a retire entry names a requirement that is not published | use `requirements obsolete` for an unpublished requirement |
| `trace criterion has other epic` | server 422 | a trace to a criterion another Epic owns | drop it; that owner cannot join this plan |
| `unsupported constraint field` / `constraint file requires` / `constraint ref must be EC-<number> or a UUID` / `--reason must be the human's decision beginning USER:` / `--state must be` | cli | a `constraints` file with a key outside title, statement, rationale and verification_method or without a required one, a ref that is neither `EC-<n>` nor a UUID, `accept` or `deactivate` without a `USER:` reason, or `list --state` outside proposed, active and deactivated; nothing was sent | fix the file, the ref or the flag (see `constraints create`); the reason is the person's own decision |
| `human_decision_required` | server 422 | `constraints accept` or `constraints deactivate` whose reason does not begin `USER:`; nothing was written | pass the person's own decision: `--reason "USER:<date>: <decision>"` |
| `nothing_to_accept` | server 422 | `constraints accept` on an active constraint without a pending change; nothing changed | there is nothing to accept; edit the text with `constraints update` first when a change is wanted |
| `nothing_to_discard` | server 422 | `constraints discard` on a proposed constraint, or on one whose text equals its current revision; nothing changed | edit a proposed constraint with `constraints update` or remove it with `constraints delete`; an unchanged active one needs no discard |
| `not_active` | server 422 | `constraints deactivate` on a proposed or deactivated constraint; nothing changed | only an active constraint is deactivated; a proposed one is deleted with `constraints delete` |
| `active_must_be_deactivated` | server 422 | `constraints delete` on an active constraint; nothing changed | the person deactivates it first: `constraints deactivate <ref> --reason "USER:..."`, then `constraints delete <ref>` |
| `Give the human's answer as a non-empty reason, such as a USER: source.` | server 422, `reason_required` | `epics plan return-to-implementation` without a non-blank `--reason` | pass the human's answer: `--reason "USER:<date>: <answer>"` |
| `Name an agent with a non-empty agent slug, or send no actor so the signed-in person is the actor.` | server 422, `invalid_actor` | an `actor` whose kind is not `agent` or `human`, or an agent without a non-blank slug | set `--agent <slug>`, or omit it so the signed-in person is the actor |
| `Invalid proposed content` with `details` | server 422 | a content field failed the requirement changeset; the fields are listed | fix the named fields |
| `invalid_payload` / `invalid_requirement` / `invalid_requirement_fields` / `invalid_criterion_fields` / `invalid_trace_fields` / `invalid_trace_endpoint` / `invalid_source_file` | server | a `reverse-engineer record` file outside its shape; nothing was written | see `reverse-engineer record` |
| `context_required` | server 422 | `reverse-engineer record` creates a requirement without a context, null or blank, also one given by a client UUID the System does not hold; nothing was written | give it a code from `requirements contexts`, or a new code and name no listed context has; the refusal names the requirement's position in the file, counted from 0, and its title |
| `context_name_mismatch` | server 422 | `reverse-engineer record` creates a requirement in a listed context with another name than the list's, also one that differs only in letter case; nothing was written | leave `context_name` out, or send the list's name the refusal names; rename a context with `requirements update` |
| `context_code_conflict` | server 422 | `reverse-engineer record`, `requirements set-context` or `requirements update` uses a code that differs from a listed code only in letter case; nothing was written | use the listed code the refusal names |
| `context_name_conflict` | server 422 | `reverse-engineer record`, `requirements set-context` or `requirements update` gives a context a name that another listed context has, or that requirements of another context carry; nothing was written | use the listed context that has the name, or a name no listed context has |
| `invalid_context` | server 422 | `reverse-engineer record`, `requirements set-context` or `requirements update` sends a value with spaces before or after it, a blank name, the code `—` or Unclassified in any letter case, which stand for requirements without a context; also `roadmap apply` with a context over 255 bytes; nothing was written | send the value without the spaces, and a code and name of a real context |
| `invalid_what_it_has` | server 422 | a `reverse-engineer record` entry carries a `what_it_has` that is null, not a list, or holds an element other than `interface`, `stored_data` and `screen`; nothing was written | give a list of those values (an empty list for none of these), or leave the key out; `reverse-engineer check` finds it first (`what-it-has`) |
| `requirement_deleted` | server 422 | a `reverse-engineer record` reference entry gives `what_it_has` to a deleted requirement of the System; the message names the entry's position, counted from 0, its kind and id; nothing was written | drop the entry, or record the behaviour as a new requirement |
| `existing_requirement_conflict` / `existing_trace_conflict` | server | an entry whose id the System holds, with different content | drop the entry to reuse the stored record, or send identical content; a content change is Epic work |
| `capture_link_not_recordable` | server 422 | a `reverse-engineer record` file holds a trace from a test case to a test result, or a `relates` trace between two test cases either of which is a capture test case or a replay; nothing was written | drop the trace from the file; a replay's links to its capture and its break test's link to the replay are written only by `roadmap recording` |
| `native_test_chain_required` | server 422 | an executed TestCase has no active, same-System Trace to a source_file or file_analysis | establish the inspected test/code relationship with `reverse-engineer record` before execution; repair a missing relationship and finalize the retained run ID without rerunning the test |
| `acceptance_not_ready` | server 409 | selected current requirements that are not `eligible` | clear each `problems` entry (see `reverse-engineer review`) |
| `unsupported recording file field` / `unsupported field` / `invalid_recordings` | cli, server 422 | a `roadmap recording` file whose `recordings` list is missing, not a list or empty, or a recording without one of its four keys or with another key; nothing was sent or written | give each recording exactly `replay_test_case_id`, `break_test_case_id`, `capture_test_case_id` and `capture_result_id`; the refusal names the recording's position, counted from 0 |
| `is not an agent name` / `invalid_agent` | cli, server 422 | `roadmap recording --agent` with a name outside lowercase letters, digits, `.`, `_` and `-`, or the name `chat`; nothing was sent or written | name the agent within that rule, or leave `--agent` out so only the signed-in person is recorded |
| `needs --agent` / `unsupported accept-lower-level file field` | cli | `roadmap accept-lower-level` without `--agent`, or a file with a key outside `line_id`, `requirement_kind`, `requirement_id`, `reached_level`, `expected_level`, `words` and `source`; nothing was sent | name the agent with `--agent <slug>`, and give the file exactly those seven keys |
| `proof_changed` / `proof_not_below` / `release_closed` | server 422 | `roadmap accept-lower-level` for a row whose levels differ from the file's, a row proved as agreed or not proved yet, or an Initiative under a closed Release; nothing was written | read the row again with `roadmap proof --initiative <id> --json` and send the levels it gives; only a row below its agreed level, under an open Release or none, takes an acceptance |
| `release_closed` | server 422 | `roadmap apply`, with or without `--dry-run`, for a `set_selection` that names an Initiative under a released or archived Modernization Release; nothing was written | this Initiative's decisions can no longer change; an Initiative under an open Release can still take one of its legacy requirements that no approved Epic or written modernizes trace holds, with a row of `move: true` in that Initiative's `set_selection` |
| `release_not_open_modernization` | server 422 | `roadmap recording --release` names no open Modernization Release of the bound System, or no open one replaces the capture test case's System; nothing was written | run from the folder bound to the new system; name an open Modernization Release with `--release <id>` (`roadmap show` lists them), or link a capture test case of its legacy system |
| `release_required` | server 422 | `roadmap recording` without `--release` while several open Modernization Releases replace the capture test case's System, such as a follow-on beside its predecessor; nothing was written | choose one with `--release <id>` |
| `reference_revision_required` | server 422 | `roadmap recording` against a Modernization Release with no reference revision; nothing was written | the Release owner sets the reference revision on the Release page; then record the capture at it |
| `not_a_capture_test_case` | server 422 | `roadmap recording` names a capture test case that is not found, deleted, not of the Release's legacy system, has a `verifies` trace or has no `relates` trace to a legacy requirement; or a `modernization record` entry whose `criterion_ids` is an empty list names a test case that is not a capture test case of the Release's legacy System: one with a `verifies` trace, or without a `relates` trace to a legacy requirement in the Release's scope that the in-place rule keeps, or one of the Release's own System; nothing was written | name the capture test case from the legacy system; link it to its requirement with `relates`, and give its capture entry an empty `criterion_ids`; a test case that verifies criteria is recorded with them |
| `invalid_verification_endpoint` | server 422 | a `modernization record` file holds test cases of both the legacy System and the Release's own System, or of neither, or names a criterion of another System; nothing was written | send one file per System: the legacy System's tests in the Release's scope, or onto a new system the Release's own System's tests with the criteria their test cases verify |
| `invalid_commit_chain` | server 422 | the `commit_chain` a `modernization record` capture carries breaks a rule: more than 2,000 commits or 8,000,000 bytes of objects, a first commit other than the run's commit, an id that is not the hash of its object, an object that is not a commit, a commit that is not a parent of an earlier one, or a repeated commit; nothing was written | run the command again from the git checkout that holds the commit; the command builds the chain itself, so a refusal points at an edited file or at damaged objects (`git fsck`) |
| `invalid_commit_details` | server 422 | the `commit_details` a `modernization record` capture carries breaks a rule: a first message line over 200 characters (code points), an author over 255, a date that is not ISO 8601, or another key; nothing was written | run the command again; it reads the commit's message, author and date with git itself and cuts them to the limits, so a refusal points at an edited request |
| `is not a git checkout` | cli | `modernization record --legacy-checkout <path>` names a folder that is not a git checkout; nothing was sent | name a checkout of the legacy repository that holds the run's commit, or leave the flag out and record the run without its commit's message, author and date |
| `give the full commit id` | cli | the capture file's `sha` names a commit this checkout holds by a short id, a branch or a tag, while a capture that carries a chain names its commit by the full id; nothing was sent | put the full 40- or 64-character id in the file (`git rev-parse <ref>`); `modernization record` sends it in lower case |
| `commit or stash them` | cli | the capture file's commit is this checkout's HEAD and tracked files have uncommitted changes, so the tests did not run on that commit's tree; nothing was sent | commit or stash the changes and run the tests again, then run `modernization record` |
| `capture_not_selected` | server 422 | a legacy requirement the capture test case relates to has no keep or change row of the bound System; nothing was written | capture only kept or changed legacy requirements; the person decides the row (`roadmap apply` with `set_selection`) before the link |
| `capture_result_not_passing` | server 422 | the capture result is of another test case, did not pass, is no longer current, or its run is not complete; nothing was written | link the result of a capture that matched: capture again and record that run with `modernization record`; never link a capture that did not match, and tell the person |
| `capture_revision_not_allowed` | server 422 | the capture's commit is not the Release's reference revision and, onto a new system, not a later commit in the legacy repository's analysed history; the message names the commit | capture again at the reference revision, or onto a new system at a later commit that is not a merge commit; when the history does not hold the commit, have the legacy repository analysed again |
| `invalid_replay` | server 422 | the replay or the break test is not a test case of the bound System traced (`relates`) to a source file, is a capture test case, both are one test case, or the replay relates to another test case; nothing was written | create the replay and its break test as two test cases of the new system and trace each to its files with `relates` traces through `reverse-engineer record` |
| `replay_already_run` | server 409 | on a first link, the replay or its break test already has a recorded result; nothing was written | links come before either test runs: create a new replay and break test and link them first |
| `replay_linked_elsewhere` | server 409 | the replay is linked to another capture test case; nothing was written | link it to the capture test case it already has, or create a new replay for this capture |
| `capture_result_older` | server 409 | the replay is linked to a result of a later run of the same capture test case; nothing was written | name the newer result the replay is linked to; a link moves only to a newer passing result |
| `break_test_taken` | server 409 | the break test already breaks another replay; nothing was written | create a break test for this replay |
| `is not the command's Epic` / `is not the checkout HEAD` / `uncommitted changes` / `is not a tracked file at HEAD` / `does not exist; nothing was sent` / `cannot be read as` / `unknown field` | cli | `epics build record`: the build handoff names another Epic, a head that is not the checkout HEAD, a traced file with uncommitted changes, an untracked path, a missing or unreadable result file, or a field outside its shape; nothing was sent | commit the build, set `head` to the checkout HEAD, fix the named field, path or file and run it again |
| `matches more than one result unit or name` | cli | `epics build record`: the test declared below a TestCase UUID is named by results of more than one package, type or file, or by more than one result name; nothing was sent | name the result in the handoff's `tests` list, or put the TestCase UUID in the test name or a `test_case_id` trait |
| `has a result but no test file` / `is in more than one file at HEAD` | cli | `epics build record`: a matched TestCase of the plan has no test file to trace and not every criterion it proves is excluded, or its UUID is in several files and nothing chooses one; nothing was sent | add a `tests` entry naming its file, or put the UUID on the line above its test declaration |
| `distinct criteria exceed the 50` | cli | `epics build record`: the results cover more than 50 criteria; nothing was sent | split the result files across handoffs of at most 50 criteria |
| `excluded entry` / `excluded entries` | cli | `epics build record`: an `excluded` entry names a criterion that is not a verification obligation of the plan, lists one twice, has no reason, or there are more than 50; nothing was sent | fix or drop the named entry |
| `regression entry` / `regression criteria` / `regression test files have uncommitted changes` | cli | `epics build record`: a `regression` entry has no criterion or TestCase UUID, names a criterion of a plan requirement, is listed twice, there are more than 50 regression criteria, or the file its result came from has uncommitted changes; nothing was sent | fix or drop the named entry; commit the test file and update `head` |
| `unchanged path` | cli | `epics build record`: an `unchanged_paths` entry is not tracked at HEAD, is in git diff base...head, or is also listed under `paths`; nothing was sent | list a file the build changed under `paths` only |
| `regression_criterion_not_allowed` | server 422 | `epics build record`: a `regression` criterion is not active, is in this plan, belongs to an OBSOLETE requirement, is owned by active work or is not published; nothing was written | drop the entry and run it again; record an owned criterion in its own Epic |
| `invalid_excluded` / `excluded_criterion_has_result` / `too_many_excluded` / `too_many_regression_criteria` | server 422 | `epics build record`: an excluded item is malformed, its criterion also has a result or is listed twice, or a section is over its limit; nothing was written | fix the named item; send at most 50 excluded items and 50 regression criteria |
| `the write may have committed` | cli | `epics build record` timed out waiting for the server | run the same handoff again; items already written read back as kept |
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

### Starting a requirement without an Epic

An eligible unowned, never-published PROPOSED UR/SR can start with
`requirements update <uuid|display-ref> --file start.json`, where
`start.json` contains only `{"work_status":"IN_PROGRESS"}`. If the server
returns `release_choice_required`, review the target and placements and retry
with `--release-choice choice.json`. Base/unassigned rows are mandatory moves;
`keep` applies only to rows already in another delivery Release. The server
rechecks stale choices. The CLI accepts only a receipt confirming the requested
identity, System, kind, revision 0 and IN_PROGRESS state. Start does not publish
content or create Epic ownership. IN_REVIEW and mixed start/content requests
are refused. Ordinary draft editing, proof and verified DONE completion remain
available after start.
