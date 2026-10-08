# Changelog

## Unreleased

## v0.18.0 — refusals name what to correct, proofs are visible, imported names hold

### Upgrading from v0.17.0

- Run `modernpath install` after upgrading so the embedded process kit, the
  reverse-engineering skill and the tooling skill match this release.
- The collision refusal details of `reverse-engineer publish` and the actor,
  intended use and release of a user requirement's read-back come from the
  server; against an older server the refusal prints its bare word and the
  read-back shows those slots as not served. Verify supported server
  deployments before publishing this release.

### A sweep's agent can act on every refusal

- `reverse-engineer publish` prints the details of a collision refusal as it
  prints a citation refusal: the requirement, the rule it broke (`id_exists`,
  `reuse_missing`, `reuse_deleted`, `reuse_fingerprint_moved`,
  `reuse_not_governed`, `missing`, `deleted`) and, for a parent rule, the
  parent and its work status. The glossary row names the remedy: correct that
  entry and publish the same group in the same run.
- `reverse-engineer execution-proof` checks the report field by field. A
  refusal names the result position, the field, the value as written and the
  values accepted (`results[3].result "ok": expected
  pass|fail|error|skip|inconclusive`); `result` is lower-cased and `role`
  upper-cased before the check and sent normalized; `inconclusive` is
  accepted; the raw report's `executed_tests[].result` is checked against the
  same lowercase vocabulary and the raw report is still sent as written.

### Every proof is visible

- `reverse-engineer delivery-proof` prints one line on stderr from the
  retained report — `delivery proof <result> at <tip> · captured <revision> ·
  ancestor: <true|false|unknown> · <n> files measured`, the later parts only
  when the report carries them — and its JSON output carries the collected
  report under `report` beside the server's receipt under `data`. The posted
  report is unchanged.
- `working-set pull` renders a user requirement's `Actor / outcome` line and
  its served release; a citation that names its test case shows it after the
  file (`test: repo@rev:path › test_case_ref`) in the item file, the review
  bundle and the per-file review copy. The editable citation list and push are
  unchanged. A system requirement whose release is served as null now reads
  "—" where it read the not-served marker.

### Imported systems keep their folder

- `modernpath import` marks the given name as person-set on the server, so
  analysis keeps it and the export folder and the binding stay the folder's
  name. `docs sync` reads the server's export slug before any download and,
  when it differs from the bound one, refuses naming both folders and
  `modernpath factory connect`; it does not rewrite the binding.
- The onboarding text shows the derived contexts in the confirmation question
  itself, each with the files and requirements it covers, and writes the same
  list to `contexts-proposed.md`; it states the execution-proof result
  vocabulary; and it says the remote tip may lie past the captured commit when
  the commits between changed none of the run's files.

## v0.17.0 — drafts survive a refresh, reviews keep their evidence, and onboarding works area by area

### Upgrading from v0.16.0

- Run `modernpath install` after upgrading so the embedded process kit,
  reviewer instructions, CLI guide and tooling skill match this release.
- `author context --file` needs a server advertising `author.set_context`;
  an older server refuses it before any requirement reads or writes.
  System-wide reverse-engineering coverage and run listing also need the
  corresponding server reads. Verify supported server deployments before
  publishing this release.

### Draft protection and review evidence

- Scoped authoring pulls stage required reads and protect edited, deleted and
  unbaselined managed files with a local hash baseline. Packet 404 preserves
  unserved and untracked drafts. Push retries reconcile already accepted
  content without rewriting authored bodies; recovery diagnostics preserve
  drafts and describe manual archive, fresh pull and reapplication.
  Accepted scaffold creates can recover missing CAS metadata on retry, and
  item-only pushes work when the optional packet endpoint returns 404.
  Pull saves baseline progress after each managed write or deletion, so a later
  file-write failure does not make completed changes look like local edits.
  Packet cleanup retains CAS pins until all deletions succeed; changed sections
  without a pull-time CAS pin require manual recovery before any store write.
  Withdrawals of absent targets are reported as no-ops when the record has not
  changed since pull; other files can still push. Ambiguous retries name their
  targets in recovery guidance. Required-section fallback uses HTTP status.
  Packet writes, retry reconciliation and restamps refresh metadata for the
  original staged filename, including aliases such as `reconnaissance.md`.
- Review pulls create isolated read-only snapshots with a manifest binding the
  store, system, scope, context, file hashes and aggregate. Findings and
  cold-review traces select them with `--review-context`, validate snapshot
  integrity, and retain the selected digest in their existing stored bodies.
  Cold-review traces no longer inherit an authoring directory's `.context`.
  The delegated reviewer uses explicit scoped snapshots while retaining the
  small-change lane's by-ID review bundle and context stamp.
  Invalid trace snapshot flags refuse before server reads. Review recording
  rechecks the originally selected digest after finding/disposition writes
  and before the trace, including replacement manifests with updated hashes.
  Explicit finding aggregate mismatches refuse the entire review before writes.
  Disposition batches validate the original snapshot before every update and
  stop remaining writes on drift; retries skip already accepted dispositions.
  Finding batches also recheck the original snapshot before each write and
  stop if its files or manifest change, including a replacement with new hashes.
- `reverse-engineer publish` prints one warning line naming the requirements
  created without a bounded context or without a test citation, and the
  working-set read-only render shows every stored citation by its typed
  identity. A new `inventory` leaves files under `.claude` out and refuses a
  named path under it, `delivery-proof` without `--run` points to `--run` for
  an older capture that holds such files, and the size refusal states the
  total counted and the largest top-level folders.


### Baseline a repository area by area

- `reverse-engineer inventory --like-run <run>` inventories again the area
  an earlier run covered and reports which files differ. It cannot be
  combined with `--path`.
- Tracked file identities use committed Git content, keeping the capture
  consistent with Git attributes and local working-tree changes.
- `reverse-engineer coverage --system` reads coverage across the system's
  runs; `--files` includes the file detail. `reverse-engineer runs` lists
  runs with pagination, and `--all` follows every page.
- At a moved tip, `reverse-engineer delivery-proof --run <run>` names the
  captured revision and whether it is an ancestor of the tested tip. It
  refuses proof when the tested commit lacks an authorized file.
- The onboarding skills describe how a multi-area sweep runs, where to keep
  verification inputs, and the publication and coverage checks.

### Assign bounded contexts in bulk

- `author context --file <entries.json>` sets the bounded context code and
  name of up to 500 requirements in one server write. Entries can give their
  own context or use `--context` and `--context-name`. If any entry is refused,
  nothing is written; pull the requirements again after an accepted write.
- `author update --context-name` sets a requirement's bounded context name.

### Delivery guidance and setup

- Updated process and tooling instructions cover parked work, blockers across
  suspension and resumption, active obligations, obsolete historical owners,
  exact reconciliation blockers and fresh entry audits.
- The canonical CLI guide is embedded in the installed kit, with installation
  channels, upgrade commands and clearer production sign-in guidance.

## v0.16.0 — the upload path fits a legacy estate

### Upgrading from v0.15.0: `import --git` is removed

- `modernpath import` no longer offers an import from a git URL, and its
  `--git` flag is gone: the server has refused that import (410) since the
  push channel shipped. A script that passes `--git` now fails with
  `unknown flag: --git`; use `modernpath import --yes` to upload the local
  files (REQ-CROSS-502).

### Import runs unattended

- `import` takes `--yes` (`-y`) to skip its confirmation. Without `--yes` and
  without a terminal it exits non-zero with `--yes is required for unattended
  runs` instead of printing "Import cancelled." and exiting 0. The method
  menu is gone; `--local` is accepted and changes nothing.
- The help labels `--exclude`, `--keep`, `--no-gitignore` and `--max-size` as
  local-upload flags, and the git remote line names `origin`.
- After an upload the next steps say the analysis is queued and name
  `modernpath analysis status`, `source push` and the other analysis verbs
  instead of `docs generate`. The View in UI link of `import`, `new` and
  `docs push` opens the app host the server names; a server that names none
  gets a link on the API host, marked as such. Every `import` error prints
  once.

### Uploads are checked against the server's limit before sending

- `import --local` and `source push` read the server's upload limit
  (`GET /api/import/capabilities`) and compare the request body with it
  before uploading. A body that does not fit is not sent: the command prints
  the zip size, the limit, the ten largest top-level directories by
  compressed size and an `--exclude` example, and exits non-zero. A server
  without the read accepts 50,000,000 bytes; when the read fails the CLI
  assumes 90,000,000. The output says which limit was used (REQ-CROSS-500).
  The read closes its connection, so a server that drops the connection
  after answering the unknown route cannot take the upload down with it.
- `--max-size` now bounds the compressed zip (MiB), not the files before
  compression.
- A `413` from the server prints one message with the same guidance instead
  of the raw JSON twice. Every `source push` error and every error of the
  `import` upload now prints once; other `import` upload refusals print as
  `code: message`, as `source push` does.

### Every upload filter is reported

- The scan report of `import --local` and `source push` now lists each
  built-in directory name and extension that dropped files (`packages/`,
  `bin/`, `*.svg`, …) with the file count and bytes, beside the `.gitignore`
  and `--exclude` drops. `source push` prints the whole report, including the
  `.gitignore` count (REQ-CROSS-501).
- New `--keep <name>` (repeatable) on both commands uploads a directory the
  built-in list skips, for example `--keep packages` in a JavaScript
  monorepo; the report names it. A name that is not on the list, the
  `.modernpath` directory and the version control directories are refused.
- `--exclude` matches the way its help says: a pattern without `/` matches a
  file or directory name at any depth (`fixtures` now also leaves out
  `src/a/fixtures/x.cs`, not only `fixtures/` at the top); a pattern with `/`
  matches the path from the top, where `*` stays within one directory and
  `**` spans any depth.
- Report sizes are exact bytes, with megabytes from one megabyte up.

### Run and follow the analysis from the CLI

- New `modernpath analysis start | reanalyze <repository-id> | reset |
  status` drive the analysis of the bound system through the lifecycle the UI
  uses. `start` and `reset` take `--mode independent_repos|unified_workspace`.
  `reset` deletes the analysis results, asks first and needs `--yes` without a
  terminal. `status` prints the repository's analysis, documentation and
  embedding status, its source revision and refresh mark, and the current run
  with its phase; with no recorded run (often the case right after an import)
  it prints `no analysis run recorded` and exits 0. A server refusal prints
  once by its error code and exits non-zero (REQ-CROSS-503).
- `docs generate` starts that same lifecycle (`--mode`, default
  `independent_repos`) instead of posting to a route the server never had, and
  no longer creates a repository.
- `docs generate` and `docs refresh` take `--yes`. Without it and without a
  terminal they exit non-zero with `--yes is required for unattended runs`
  instead of printing "cancelled" and exiting 0.
- `docs refresh`, `preview` and `repair` find an import-created repository by
  the `repository_id` that `import --local` saves, else the system's single
  upload repository, else a local-path match, and exit non-zero when none
  resolves. The config reader now loads `repository_id`, and every command
  that rewrites `config.json`, including `env --set`, keeps it; a `factory
  connect` to another system or API host clears it.

## v0.15.0 — autopilot builds connected journeys in batches, and cold review checks terms

### Upgrading from v0.14.0

- Run `modernpath install` after upgrading so the embedded process kit matches
  this release. It is synced from req-driven-dev `a4e20ec`
  (ModernPath/req-driven-dev#34 and #35):
  - `rdd-autopilot` builds connected user journeys in batches. Red-first holds
    per batch: a batch's RED commit carries its failing tests, and an
    end-to-end failing test is established in the batch that can make it
    pass.
  - Closeout adds full verification and hardening, not RED for clauses
    already built; a clause first built at closeout gets its own RED.
  - `rdd-build` and `rdd-start` point to the batch cadence under an autopilot
    grant.
  - `rdd-cold-review` checks proposed domain terms against the project's
    established vocabulary. Wording alone is a note; a term that changes a
    model, contract, user-visible concept or scope is material.
- The installed `rdd-cold-reviewer` agent states the same vocabulary check,
  so a delegated review applies it whatever the project's own instructions
  say.
- These changes need no server change and work against the production
  server of v0.14.0 (modernpath-v1@6f69ff5ec).

### Internal

- The command code is split into smaller files by workflow (working set,
  factory, process ceremony, lane, RDD sync and extract, documentation and
  work commands). Commands, flags and help text are unchanged from v0.14.0.

## v0.14.0 — baseline a large repository path by path, and authorize without a JSON file

### Upgrading from v0.13.0

- Run `modernpath install` after upgrading so the `mp-process-cli` skill and
  `.modernpath/cli-reference.md` match this release. The skill's
  reverse-engineering onboarding steps changed: an agent follows the new
  steps only after the install.
- The reverse-engineering changes need no server change and work against
  the production server of v0.13.0 (modernpath-v1@6f69ff5ec).
- The release-planning lines below, and the help text of `factory release
  activate --close-current`, describe a server that production does not
  serve yet. Against the current production server the command behaves as
  in v0.13.0.

### Baseline a large repository path by path

- `reverse-engineer inventory --path key=relative/path` (repeatable) limits a
  Git repository to the named paths. The inventory keeps the repository's commit and
  clean or dirty state, the size and file limits count the included files, and
  every file left out is listed as an exclusion. A path is a literal name; a
  path that matches no file, is not relative or ends with a slash refuses.
  Non-Git roots cannot be scoped (SR-RDD-ONBOARD-010).
- A tracked symbolic link in a Git repository no longer stops the inventory.
  It is left out, listed as an exclusion and never followed. `capture-source`
  still refuses an authorized path that has become a link, and a non-Git root
  still refuses a link (SR-RDD-ONBOARD-011).
- `reverse-engineer delivery-proof --run <run>` takes its snapshot over the
  files that run authorized, so the proof of a path-scoped run matches its
  capture. The input's `snapshot_digest` must be the one the run captured;
  any other digest refuses and records nothing. The report adds
  `authorization_run_id` and `measured_files`. Without `--run` the command
  and its report are unchanged (SR-RDD-AS-BUILT-CLI-002).
- No server change is needed: the server already accepts an authorization for
  part of a repository.

### Authorize a run without writing a JSON file

- `reverse-engineer authorize --inventory inventory.json --preflight
  preflight.json --mode … --source "USER:…" --key … --documents all|none`
  builds the authorization from the saved output of `inventory` and
  `preflight`. Every flag is required and none has a default. The command
  checks the inventory file against its digest and refuses incomplete input
  before it calls the server (SR-RDD-ONBOARD-012). `--source` and `--key`
  are at most 255 bytes each.
- When the server refuses an authorization given this way with `stale_corpus`,
  `document_not_authorized` or `document_snapshot_too_large`, the error says
  what to do next and that nothing was recorded.
- `authorize --help` describes both forms and names the fields of the file
  form. `--file` works as before; the two forms cannot be mixed. With no
  flags, `authorize` now names both forms instead of reporting a missing
  `--file`.
- The `mp-process-cli` skill leads with the new form and tells the agent what
  to show the person before authorizing. The agent saves `inventory` and
  `preflight` once and authorizes from those files, proposes the run name,
  and asks one question. After a server refusal it asks again when what the
  person saw has changed, and always before it drops the documents. It
  compares two saved preflight files, saves the authorize response to a file
  and reads the run id from it, and stops on a repeated or any other refusal.
  In PowerShell it saves the two files through `cmd /c`, because PowerShell's
  own redirect re-encodes the output (not yet verified on Windows).

### Release planning

- `factory release activate --close-current` is refused while the active
  release still holds unfinished epics or system requirements
  (`incumbent_has_unfinished_work`); the refusal names the release and points
  to Close release in the product, where each item gets a destination first.
- `factory sync` prints one `kept <item> in <release> (workspace names
  <release>)` line for each epic or system requirement that was moved on a
  release page and stays there while that release is open (SR-ROADMAP-012).
  Older servers send no such rows.

## v0.13.0 — ask waits for slow answers, and trace refresh for existing baselines

### Upgrading from v0.12.0

- Run `modernpath install` after upgrading so the `mp-process-cli` skill and
  `.modernpath/cli-reference.md` match this release.
- Both changes need a server that has them; production serves them from
  modernpath-v1@e291eda5c.

### ask waits for a slow answer instead of timing out

Needs a server with the `agentic_search_result` tool.

- The server runs each `ask` in a background job. An answer ready within 45
  seconds comes back as before. Otherwise the server returns an ask id and
  `ask` polls it, showing `Still working (m:ss)…` on stderr in pretty format,
  until the answer arrives, the ask fails, or 10 minutes pass
  (`MODERNPATH_ASK_WAIT_LIMIT`, a Go duration, overrides the limit). The
  answer prints exactly as before in every format. A failed ask, or one still
  running at the limit, prints its reason and ask id and exits non-zero
  (REQ-CROSS-490).
- Server: the job has 5 minutes and always ends with an answer, partial when
  time runs out. The tool calls of one model turn run concurrently (up to 4),
  every model call, tool call and embedding is bounded by the time left, a
  search for the question's own text reuses its embedding, and a cold source
  cache is restored at the start of the job. Only the signed-in user who asked
  can read the answer; answers are kept 7 days (REQ-CROSS-490, REQ-CROSS-468 as
  amended).
- An older CLI prints "Still working on this question (ask N)." for an ask
  that takes longer than 45 seconds, instead of the answer.

### reverse-engineer refresh-traces links existing baselines to captured code and tests

Needs a server with the `captured_trace_refresh` capability.

- New `modernpath reverse-engineer refresh-traces --run <capture-run> --group
  <retry-key> --file refresh.json`. After `author update --citations-file`
  changes the captured citations of existing pending SR baselines, it creates
  or reuses confirmed `implements` links to the captured code and `verifies`
  links to source-bound test cases. Before, those baselines stayed ineligible
  for acceptance, because `publish` reuse creates no links.
- The input is `corpus_fingerprint` from `reverse-engineer preflight` and each
  requirement's current content fingerprint; a stale fingerprint returns a
  conflict. The capture run must belong to the signed-in person and be
  baseline-authorized; a DERIVED run is refused. Rejected, stale, deleted or
  differently governed links are refused, not revived.
- Requirement content, criteria, lifecycle, baseline provenance and existing
  links stay unchanged. A refresh records no test pass and no acceptance.
- The same input and group key return the same receipt, also after
  acceptance; `reverse-engineer status --run <capture-run>` lists
  `trace_refreshes` for recovery.

## v0.12.0 — find by meaning, a small-change lane, and fewer calls per phase

### Upgrading from v0.11.1

- Run `modernpath install` after upgrading so the embedded process kit
  (now with the `rdd-autopilot` skill), the managed `AGENTS.md` block and
  `.modernpath/cli-reference.md` match this release.
- Release repository history: v0.11.0 and v0.11.1 were made directly in
  `ModernPath/cli`; their migration fixes are listed first below.
- The search commands need a server that has the new search routes.

### Migration fixes first shipped in v0.11.0 and v0.11.1

These changes were made directly in `ModernPath/cli` and are imported here so
this release keeps them.

- `migrate report` and `migrate run` compare gate state before writing and
  stop if the gates read from the store changed between reads.
- A dismissed store gate that the corpus still holds open can be preserved with
  `--residue-manifest` and `--residue-gate`. The gate must be an answered human
  `migration_residue` decision that carries the exact manifest, its SHA-256 and
  a `USER:` source. Only the listed gate ops are skipped.
- The retired-file check also covers the requirement ledger paths the workspace
  declared, and matches nested globs.
- The store-backed marker no longer names `scripts/check-store-backed.sh`.

### Find requirements and epics by name or meaning

Run `modernpath install` after upgrading: `.modernpath/cli-reference.md`
changes. Both commands need a server with the new search routes.

- `requirements search <query>` finds user requirements, system requirements
  and test cases of the bound system by id, display reference (`UR-5`,
  `SR-12-0`, `TC-8`), title, description or meaning, in one ranked list.
  Each hit shows its external id (or display reference), kind, work status,
  name and how it matched; `DERIVED` requirements and unconfirmed test cases
  are marked `candidate`, and `OBSOLETE` rows are never returned. It runs
  the same server search as MCP's `search_requirements` and chat
  (REQ-PLN-192, REQ-CROSS-472).
- `epics search <query>` finds epics the same way, on the engine behind
  MCP's `search_epics`, and shows each epic's code, process status, name and
  how it matched (REQ-PLN-192).
- Both take `--limit` (10 by default, at most 25) and `--json`. A 404 says
  whether the system is unknown to the server or the server has no search
  route. The subagent guard lets a delegated agent run both, as reads.
- Server: MCP offers `search_requirements` next to `search_epics`
  (REQ-CROSS-473); chat's `search_requirements` uses the same engine
  (REQ-PLN-191); a daily job embeds eligible requirements and confirmed test
  cases whose embedding failed (REQ-CROSS-474).

### ask covers patterns and capabilities; the review packet shows what a chat found

- `ask`'s help says that the agentic search also covers the curated patterns
  and capabilities the system uses. The server names each source's kind and
  id; `--format=json` sources keep the keys title, type and path
  (REQ-CROSS-469, REQ-CROSS-470, server change).
- An epic created from a chat carries what the chat's agentic search found.
  `working-set pull --scope` shows these in a read-only `found_in_chat` block
  in the epic file, and `--for-review` lists them under "Found in chat" in
  `REVIEW.md`, each as its kind and name (REQ-CROSS-471).

### search finds code files; ask names its sources and answers within 90 s

- `search --files-only` returns the code files that match again: the
  server kept a code result's path and classified it as code, and a files
  search no longer spends its limit on source documents or data-model rows
  (REQ-CROSS-466).
- `ask` prints a file source's path when it has no title, in every format,
  and lists each source once. `--format=json` sources keep the keys title,
  type and path (REQ-CROSS-467).
- The server's agentic search behind `ask` runs on the fast model, starts
  from code files as well as documents, and answers within 90 s. When the
  model cannot answer in time, the answer is what the search gathered, and
  it says it is partial (REQ-CROSS-468, server change).

### Reopen several requirements at once; re-review only what changed

Run `modernpath install` after upgrading: the `mp-process-cli` skill, the
`rdd-cold-reviewer` agent and `.modernpath/cli-reference.md` change. The
reviewed fingerprints need a server that serves them on the gate read
(REQ-CROSS-463).

- `author demote --ids A,B,C` (or `--file`, one id per line) opens one
  demotion gate over every item, with one basis and one USER: reason. All
  items must be IN_REVIEW, or all DONE: a mixed batch is refused before any
  write, naming both groups, the group holding user requirements first. The
  gate's title names the items (the first ids and "and N more" within 255
  characters) and its brief lists every item and the user requirements and
  epics that follow. The gate id is `DEMOTE-<first id>`, or the next free
  `-R<n>` in its series when the earlier gates are closed or withdrawn; an
  open or answered gate in the series is refused and named (REQ-CROSS-462).
- `author demote --gate-id <gate> --apply` takes no id and advances every
  item of the answered gate, user requirements first, re-reading the gate
  before each item. An item with its own applied entry is skipped. An item a
  follow on another gate already moved is reported with that gate and passed
  over, and the gate that cannot then close names `author gate-withdraw`. Any
  other refusal stops the run, lists what was applied and what remains, and
  exits non-zero; a re-run resumes. `author demote <id> --apply` without
  `--gate-id` applies the newest gate in the `DEMOTE-<id>` series, every item
  on it. The printed apply hint names `--gate-id` for a batch and for a gate
  that is not at the default id (REQ-CROSS-462).
- `working-set pull --scope --for-review` stamps each record's and packet
  section's fingerprint in `.context` (`reviewed: <key> <fingerprint>`), and
  `process review record` sends them on the cold-review trace as
  `reviewed_fingerprints`. The trace still pins the full packet aggregate
  (REQ-CROSS-464).
- `working-set pull --scope --for-review --since <trace>` writes `REVIEW.md`
  as a later round: the previous trace's code revision beside HEAD (with a
  note to re-verify the citations of unchanged items when they differ), what
  changed since that review in full with the old and new fingerprints, the
  open findings, the previous verdict, and the unchanged ids with their
  fingerprints only. It refuses a trace that is not a cold review, belongs to
  another scope, or carries no reviewed fingerprints (a trace recorded before
  this release: run a full review once), and `--since` with the by-id narrow
  review (REQ-CROSS-464).
- Server side (REQ-CROSS-463): the gate read serves `reviewed_fingerprints`
  for a cold-review trace, from what was recorded at its birth, and null
  otherwise.
### Findings name how they resolved (EPIC-CLI-027)

- The batch files take the resolution fields (EPIC-CLI-027 on EPIC-CLI-TURNS):
  a `process findings add --file` entry takes `introduced_by`, and a
  `process findings disposition --file` or `process review record` /
  small-change lane review disposition takes `resolution`
  (`packet-edit|scope|decision`) and `widens`, with the rules of the flags —
  a RESOLVED entry without its kind, `scope` without `ref`, `decision`
  without a `USER:` ref, `widens` outside a packet edit, and a kind or link
  a server without `finding_resolution` would drop are refused before any
  write: per entry in the batch verbs, for the whole file in a review.
  `findings list` keeps its paging across the per-scope groups of `--all`:
  `--limit`/`--offset` page the rows, the round summary counts every row,
  and `--json` adds `total` and `has_more`.

- `process findings list --json` and a per-scope `--all` (SR-CLI-027-004,
  EPIC-CLI-027, BACKLOG-TOOL-203, BACKLOG-TOOL-225): `--json` writes one
  object to stdout and nothing else — `findings` (the served rows, each
  with `material`, `independent` and its `round`) and `rounds` (one object
  per scope and round with its counts and flags, `document` and
  `earlier_resolutions`, computed per round); an empty result is the same
  object with empty arrays and `-v` adds nothing. `--all` groups the rows
  and the round summary by scope with a heading per group, so round
  numbering and the flags never cross scopes; a single-scope listing
  renders as before with no heading. The output goes through the command
  writer.

- A finding names the earlier resolution it falls on (SR-CLI-027-003,
  EPIC-CLI-027): `process findings add --introduced-by <F-id>` records the
  earlier finding of the same scope whose resolution introduced the
  mechanism the new finding faults — the server refuses an id it does not
  hold on the scope or the finding itself, and a server that does not
  advertise `finding_resolution` is refused before any request. Each round
  line of `findings list` gains `widened` and `on earlier resolutions`, and
  a round whose material findings all fall on earlier resolutions is
  flagged: the packet was reviewed incomplete (PROCESS.md §Entry packet).

- A packet edit does not change a member (SR-CLI-027-002, EPIC-CLI-027):
  when a finding is raised the server stores the served content hash of
  each scoped member — the epic's stored membership, the SR itself for a
  `single_sr` — and a `packet-edit` resolution is refused naming every
  member whose content moved since, unless `process findings disposition
  --widens USER:<date>:<why>` states the widening on the human's word; the
  server stores that source and `findings list -v` prints it. The snapshot
  is the server's (a supplied one is refused on a create and on any
  update), a widening source rides a packet-edit resolution into RESOLVED
  only, and a finding raised before the change, or already RESOLVED, is
  not compared.

- A RESOLVED finding names how it resolved (SR-CLI-027-001, EPIC-CLI-027,
  BACKLOG-TOOL-218): `process findings disposition --disposition RESOLVED`
  requires `--resolution packet-edit|scope|decision` — `scope` names its
  record in `--ref`, `decision` its `USER:` source — refused before any
  request, as is a kind with any other disposition; the server stores the
  kind, refuses a transition into RESOLVED without it (an update of an
  OPEN/DEFERRED row or a create born RESOLVED) and a kind on any other final
  disposition, and `findings list` prints `RESOLVED/<kind>`. A finding
  already RESOLVED changes only its reference: `--ref` with
  `--expected-fingerprint` and no `--disposition`, so a row resolved before
  the change keeps its null kind. The server advertises
  `finding_resolution` under `author.finding`: a build before this change is
  refused on `findings add` too (USER:2026-09-27), and a new build refuses a
  server that does not advertise it (a 404 contract read reads the same)
  before any request, while a failed contract read names its status. The
  server deploys first.

### The state inventory and entry drift (EPIC-CLI-028)

- `process enter`'s reconnaissance drift check reads a citation written with
  a space after the prefix (`CODE: <path>`, `TEST: <path>`) as it already read
  `CODE:<path>` (SR-CLI-028-002). Such a citation used to be skipped, so a
  packet that cited that way entered past a changed cited file while the
  check printed "no cited path changed".

- `process enter` refuses on reconnaissance drift (SR-CLI-028-002,
  EPIC-CLI-028, BACKLOG-TOOL-219): it reads the selection's reconnaissance
  revision (refusing before the facts read when none is recorded), fetches
  the remote default branch (`--no-fetch` for offline fixtures), and when
  the tip moved past that revision and a path the packet cites as `CODE:`
  or `TEST:` changed from the merge-base to the tip, refuses naming the tip
  and the paths. A tip equal to or an ancestor of the revision is current.
  `--allow-drift USER:<date>:<why>` accepts the drift and the gate body
  records the source, the tip and the paths; both calls of a two-call entry
  run the check.

- The state inventory is a canonical packet section (SR-CLI-028-001,
  EPIC-CLI-028): `process check --phase plan` requires `state_inventory`
  beside reconnaissance, red strategy and decisions, `working-set pull
  --scope` scaffolds `15-state-inventory.md` as one marker line naming the
  table's columns and the no-state declaration, an untouched stub is never
  pushed, and a present section counts in the packet aggregate. Scopes still
  in planning read incomplete until the section is pushed. The server
  deploys first: an older server serves the three-key list and the CLI
  fallback names the fourth only when no list is served.

### Fixes from the PR #694 review

Run `modernpath install` and `modernpath hooks install` after upgrading: the
`mp-process-cli` skill and `.modernpath/cli-reference.md` change.

- `author apply` is now an input format for `working-set push`'s write
  engine. The whole plan is checked before the first write, and one invalid
  record stops the call with nothing written. A missing record is created,
  then patched. Each record's fields, criteria, relations and membership go
  in one atomic patch that carries the authoring context, so a review
  recorded from the same context is not counted as independent.
  **Pull first, or pin fingerprints:** an existing record the plan would
  change is guarded by its `expected_fingerprint` or else by its
  `working-set pull`. Without either it is refused, and a record that moved
  since is refused rather than overwritten. The fingerprint is never read at
  run time. `--from-pull` is deprecated and has no effect. Each updated
  record's line shows `<replaced> -> <new>` (REQ-CROSS-442).
- `author apply` reports a partial run truthfully. A fingerprint conflict is
  reported for that record and the rest continue; any other refusal stops the
  run and names the records written, those not written, and each record
  created whose patch never ran — its line reads `created, not patched` and
  the error names the `working-set pull <id>` that recovers it. A create the
  store answers 409 reads `already exists`, with no empty fingerprint. The
  call exits non-zero when any record did not apply (REQ-CROSS-442).
- After `author apply` updates a record, its by-id `working-set pull`
  snapshot carries the fingerprint the write returned, so a second apply from
  the same pull is no longer refused as changed since the pull. Only the
  fingerprint line and the written-body hash change; `working-set check`
  still reports the snapshot stale. A scope pull's file is left alone — push
  diffs it, and a moved fingerprint would let the next push revert the
  write — and the line says to re-pull the scope (REQ-CROSS-442).
- `working-set push` never creates a record; `author apply` is the only
  create path. An item file whose id the store does not know is refused
  before any write, and the refusal names `author apply`. A `members/` file
  whose record the store holds outside the frozen selection is named as
  skipped rather than dropped without a word; the line says it is not in the
  frozen selection (and, when the scope's served membership holds it, that it
  is a member) and to re-select to push it (REQ-CROSS-442).
- The line of a record whose by-id pull snapshot apply moved to the new
  fingerprint says the snapshot's content is from before the write: re-pull
  it before copying from it (REQ-CROSS-442).
- A pulled SR shows its `lane_class` and push can change it. The CLI's
  mutable-field set is checked against the server's own definition, so a new
  server field fails a test instead of drifting (REQ-CROSS-442).
- The subagent guard no longer reads a here-document body as commands unless
  the here-document feeds a shell (`bash`, `sh`, `zsh`, `eval`), and a
  variable whose value ends in `/modernpath` counts as the binary only when it
  is run as a command (REQ-CROSS-451).
- `process findings disposition --scope` needs `--from`, the disposition you
  saw, and writes only while the finding is still in it; a fingerprint read
  just before the write guarded nothing (REQ-CROSS-443). The findings,
  dispositions, review and lane authorization files refuse a key they do not
  know and name it, instead of dropping it (REQ-CROSS-443, REQ-CROSS-450).
- `author advance --gate` no longer advances the epic past a member it
  skipped. A member already past the transition is done; any other member
  that did not advance keeps the epic where it is, and the call exits
  non-zero naming it (REQ-CROSS-444).
- The subagent guard is default-deny: a delegated agent may run only the
  verbs listed as reads, and any other subcommand is denied, so a new write
  verb is denied until it is listed as a read. It reads the command as the
  shell runs it, so `process lane "approve"` is denied, and denies a
  subcommand it cannot read literally (`$VAR`, `$(…)`, `eval`, `xargs`
  input, an indirect path to the binary) (REQ-CROSS-451).
- The lane's help texts use plain words and no internal names
  (REQ-CROSS-458). `process lane complete` tells the approver that the
  `--log` run is the run the agent reported, not a verified one
  (REQ-CROSS-456).
- `--json` listings of `process backlog list` and `factory gates` carry
  `total` and `has_more` (REQ-CROSS-447).
- Server side (REQ-CROSS-453, 455, 456): a lane authorization's answerer is
  judged by the roles in the sign-in token, with the workspace role table only
  as the fallback for a caller without claims; the approver reads the terms as
  the server wrote them from what it enforces; `process lane check` refuses a
  DONE or OBSOLETE SR and a narrower re-report of the same commit, and its
  verdict says the file list was reported by the CLI; the excluded areas match
  without regard to case and also cover `*auth*`, `*token*`,
  `config/runtime.exs`, `.github/workflows/**` and the bulk sync path; an
  answered lane batch is pinned to its approved members only, so fixing a
  rejected change never blocks them.

### The small-change lane

Run `modernpath install` after upgrading: the embedded process kit, the
`mp-process-cli` skill and `.modernpath/cli-reference.md` change, and
`install --check` reports them as drifted until you do. The lane needs a
server that serves it (REQ-CROSS-453..457).

A small change is one SR in no epic, of a class a current lane authorization
covers, with at most five non-test source files and no excluded area. It
takes one narrow independent review, enters by the standing authorization
instead of a per-change human gate, and completes in a batch. A one-line
defect goes from its record to IN_REVIEW in five writing calls and the review
pull.

- `process lane authorize --classes … --appliers … --expires … --cap …
  [--exclude <glob>]…` (or `--file lane.json`) opens a lane authorization for
  the System and prints its id, and names the next step. A workspace admin
  answers it in the web app or with `process lane approve <gate>`. `process
  lane` shows the current authorization.
- `process lane approve <gate> [--text <decision>]` answers a lane
  authorization with approve in one call, through the server's `lane_approve`
  action, as the signed-in admin; `--text` is the USER: decision. It refuses
  before any write when the gate is not a `lane_authorization`. `factory
  answer` on a lane authorization is refused by the server, and so is any
  client other than the web app and the CLI.
- `working-set pull <SR> --for-review` renders one system requirement
  read-only for the narrow review. It stamps a review context and the SR's
  content in `.context`, and writes `REVIEW.md`.
- `process lane review <SR> --file review.json` records the narrow review in
  one call: a cold-review trace on PROPOSED->TODO at the SR's single-SR
  aggregate, with a `LANE:narrow` source. It refuses without the review stamp,
  without a lane class, or when the SR changed since the pull.
- `process lane enter <SR>` posts one advance PROPOSED->TODO with the current
  authorization as `lane_ref`. On an entered SR that changed afterwards, it
  re-applies the authorization.
- `process lane check <SR> --commit <sha> [--base <sha>]` posts the delivered
  file list over `<sha>^1..<sha>`, or `<base>..<sha>` for a rebase delivery,
  and prints the server's eligibility verdict. A FAIL names each offending
  file and exits non-zero.
- `process lane complete --log <run>` opens one lane-batch gate over the
  IN_REVIEW small changes with a passing eligibility, listing each change's
  files. `--apply` advances the members the answer approved; a rejected one
  stays IN_REVIEW.
- `author update --lane-class <class>` sets an SR's lane class. `author apply`
  takes `lane_class` and `sources` on a requirement.
- The verbs that read the single-SR aggregate take the SR as a `single_sr`
  work selection when you do not hold it, because the server serves the
  aggregate only for a held piece.
- The subagent guard denies `process lane authorize`, `approve`, `review`,
  `enter`, `check` and `complete` to delegated agents. The kit's permission
  rules allow them as routine writes, except `authorize` and `approve`, which
  ask; no allow rule covers `approve`. Run `modernpath hooks install` to add
  the new ask rule.
- The embedded process kit is synced from req-driven-dev `d027476` on the
  `feat/small-change-lane` branch (ModernPath/req-driven-dev#31). Re-sync the
  pin to req-driven-dev `main` with `scripts/sync-rdd-assets.sh` once that
  pull request merges.

### Three `process advance` and review-pull edge fixes

- `process advance` judges a TODO or READY member by its own entry, as
  reconcile does: a member entered through its own members-only gate at the
  current packet aggregate advances even when the epic's entry gate is pinned
  at an older aggregate. A member with no live entry of its own, under an
  epic entry gate that is current, is refused before any write; the refusal
  names the member and points to `process reconcile --piece <piece>`. This
  needs a server that serves the per-member `entry_current` fact; with an
  older server, the check and its message are unchanged (REQ-CROSS-459).
- `process advance <SR>` and `process reenter <SR>` no longer need `--piece`
  when you hold several selections and the SR is itself one of them. An item
  that is not one of the held pieces is still refused, naming the pieces and
  `--piece` (REQ-CROSS-460).
- `REVIEW.md` shows a `Sources` line for the scope requirement and for every
  member requirement: the served citations, `—` when there are none, and the
  not-served marker when the read does not serve them. The epic block is
  unchanged (REQ-CROSS-461).

### Fewer calls per delivery phase

Run `modernpath install` after upgrading. The `mp-process-cli` skill and
`.modernpath/cli-reference.md` now give each phase with the batch verbs below,
and `install --check` reports them as drifted until you do. A four-SR epic
from planning to IN_REVIEW takes 11 calls this way, instead of 46 one record
at a time. The single-record verbs still work and remain the fallback.

- `author apply --file <plan.yaml|json>` records an epic, its user and system
  requirements with their prose and criteria, the relations and the membership
  in one call. It writes only fields that differ and reports each record. A
  rerun writes nothing; `--dry-run` prints the plan.
- `working-set pull --scope --for-review` also writes `REVIEW.md`: the whole
  packet in one file, each record under an `id · fingerprint` heading. It
  stamps the packet aggregate in `.context`.
- `process review record --file <review.json>` records a delegated cold
  review in one call: its findings, its dispositions and the cold-review
  trace at the stamped aggregate. It refuses before any write when the pull
  has no review stamp, when the aggregate has moved, or when a PASS would
  leave a material finding open.
- `process findings add --file` records a list of findings.
  `process findings disposition --file` sets several dispositions, each only
  while the finding is still in its `from` disposition.
  `process findings disposition --scope <kind>:<id>` reads the finding's
  fingerprint itself, so `--expected-fingerprint` is no longer required.
- `author advance --gate <GATE>` reads the gate's fingerprint, transition and
  `approve` answer when they are omitted. Without a record id, it advances
  every record the gate names, members first, then the epic.
- `factory evidence --file <runs.json>` records several runs in one call. Each
  run ID is derived from its entry, so rerunning the file updates the same
  runs.
- `process advance --all --piece <EPIC> --log <run>` advances every system
  requirement of the piece and says why any one was not moved.
- `process next` with several held pieces prints one block per piece (scope,
  phase, why, the skill to run, the gates waiting on it) and exits 0, instead
  of refusing.
- `process enter` re-stamps sections whose content is unchanged but whose
  context moved, instead of refusing them. `--brief-file` also takes the
  markdown brief bullets.
- `process backlog list`, `factory gates` and `process findings list` print at
  most 50 records in text output. Use `--limit` and `--offset` to page;
  `process backlog list` gains `--json`.
- The kit's permission rules allow `author apply`, `process review record` and
  `process advance` as routine writes. `modernpath hooks install` adds the
  rules where the workspace has not placed them itself. The subagent guard
  still denies them, `process enter`, `process complete` and
  `process review record` to delegated agents.

- The managed `AGENTS.md` block now states what the loop covers: product
  work goes through the loop, meta-work (repository layout, CI/CD, deployment
  infrastructure, developer tooling, agent instructions, the process material)
  goes through the project's normal review path with the reason stated. The
  rule used to live only in the ModernPath workspace's own instructions, so
  every other installed repository was pushed to run meta-work through the
  loop. Run `modernpath install` to refresh the agent instructions.

- `author update --citations-file` refuses non-string `ref` values before
  posting, even when another valid source identity is supplied.

- Plain `factory gates <id>` details and pulled gate snapshots show the stored
  decision brief: What, Why now, Changes if approved, Risk if wrong and
  Recommendation. Partial briefs omit empty fields and preserve multiline text;
  JSON and compact gate listings keep their existing output.

- `author update --citations-file` replaces an existing requirement's complete
  typed citation set while preserving supplied provenance metadata and legacy
  process-source identities. An empty array clears citations; omission preserves
  them. Existing fingerprint checks and lifecycle restrictions still apply.

- `feedback` writes tooling issues only to the verified ModernPath
  tenant and bound workspace. Customer credentials and customer workspace
  bindings are refused rather than rerouted; local tooling-gap fallback files are
  no longer created. Run `modernpath install` to refresh the agent instructions.
- `process prepare-inputs` checks delivery context with one request and reports
  held-piece count, local documentation last synced, and server documentation
  last updated. It leaves documents unchanged; use `docs sync` explicitly to
  refresh them. The server must provide the new preparation endpoint.
- Agent instructions prefer local `rg` and file reads, with live search/read-doc
  for missing material or a current server answer.
- Human approval instructions require a decision brief and a listing of the
  relevant working-set files, with full absolute paths as clickable links.
  File contents are shown on request. Run `modernpath install` to refresh the
  process snapshot and approval procedure skills.

### Export downloads go through the export link

- `init` and `docs sync` ask the export's link for the zip. A signed storage
  URL is fetched straight from the bucket, without your credential; the API's
  own file route is fetched with it. This needs a server with the export link
  (REQ-OBAN-018); older servers answer `invalid start export response`.
- An export whose zip has expired says so and asks you to run the export again.
- The export wait reports the job's status and the elapsed-time heartbeat; the
  server's progress text is gone.

### Keep an uploaded system current from your own network

- `modernpath source push` (REQ-SYS-211, EPIC-ANALYSIS-009) re-packs the
  working directory with exactly the `import --local` filters, computes the
  content revision the server uses and, only when it changed, uploads the
  archive to the bound system's upload repository; the server supersedes the
  previous source and queues an incremental refresh of the knowledge core.
  The same tree is a no-op after a completed refresh and a re-queue after a
  stopped one; the checkout's git head, branch and dirty flag travel as
  metadata. The verb never prompts and exits non-zero on a server refusal
  with its code, message and, when the repository is busy, the time since.
- `import --local` records the created repository id in the working
  directory's `.modernpath/config.json`, preserving the file's other fields;
  the git-URL import option is no longer labelled as recommended.

## v0.10.0 — exact reads and safer system activation

### Upgrading from v0.9.0

- Run `modernpath install` after upgrading so the embedded process kit,
  managed `AGENTS.md` block, tooling skills and `.modernpath/cli-reference.md`
  match this release.
- This release requires a server with the bounded named-item sync endpoint and
  system-scoped release activation. The production server was verified before
  publication.

### Read and update only the requested work

- `working-set pull <id>...` reads only the named records instead of fetching
  every Epic, requirement, gate and backlog record first. Batches are bounded
  by item count and encoded request size.
- Work-selection pulls, local snapshot checks and trace fingerprint lookup use
  exact reads. Successful single-record edits refresh from the canonical
  server response.
- `read-doc --list` now applies `--tier` and `--angle` filters through the
  server-filtered read path.

### Activate releases for the whole system

- UI and CLI activation use the same system-scoped server operation, so the
  active release and its approval are shared across users. The server records
  the signed-in actor instead of trusting a client-provided source.
- The CLI validates closure consent and preserves the existing approval when
  retrying activation.

## v0.9.0 — what the loop records, it reads back

### Upgrading from v0.8.0

- Run `modernpath install` after upgrading. The skills, the managed
  `AGENTS.md` block and `.modernpath/cli-reference.md` are compiled into the
  binary, and `install --check` reports them as drifted until you do.
- `working-set check` reports snapshots pulled with v0.8.0 as stale once:
  acceptance lines now carry their id and kind, and a Gates section can grow.
  Pull again.

### The records the loop writes read back in full

- `factory gates <trace>` and the gate block in a pull show the verdict, the
  evaluator and the time, the recording revision, the pin and its class, the
  scope, purpose, transition and prerequisites (`none` when there are none).
  `-v` prints any gate's body.
- Every acceptance line reads `<id> (<kind>) — <line>`, in the flat pull, the
  review render and the authoring pull alike.
- `working-set pull WORK-SELECTION` pulls the current work selection. `--piece`
  on a pull by id is refused before any request, and an unknown id names the
  places that were searched.
- `process findings list` lists the findings of the piece you hold — `--piece`
  picks one when you hold several, `--all` lists the whole system — and `-v`
  prints each finding's body, source, owner, scope, aggregate and disposition
  reference. A scope without a kind (`EPIC-X` rather than `epic:EPIC-X`) is
  refused by the server instead of widening to every finding in the system.
  `process findings add` refuses an unlisted severity or category before it
  sends anything.
- `your-move --domain` filters first, then `--queue` and `--more` page through
  what is left. The header names the domain, and an empty result says so.
- A question gate's brief says `names …` for the items its scope names. A
  question holds nothing, so it no longer reads as `holds` or `unblocks`.

### Backlog records have one vocabulary, a list verb and an editable body

- `process backlog list [--kind backlog|gap|tooling] [--disposition <prefix>]`
  lists backlog, gap and tooling records.
- A disposition is `OPEN`, `DEFERRED`, `ROUTED to <id>`,
  `REJECTED with <source>`, `CLOSED by <id>` or `ACCEPTED with <source>`, and a
  gap kind is `capability` or `specification`. The file-ledger extractor sends
  these words. A record stored with the older `routed`, `deferred` or `ledger`
  is rewritten on its next write, and the `--disposition` filter already
  matches it in the new form.
- `author update --kind backlog` takes the body fields `author backlog` files
  with: `--notes`, `--observed`, `--why-unrouted`, `--candidate-route`,
  `--affected`, and for a gap `--gap-kind`, `--consequence` and
  `--affected-trace`. A filed record could only be corrected by filing a
  second one. A flag left unset keeps the stored value.
- `--detail`, and every other requirement or epic field, is refused on a
  backlog record before any request; `--detail` points at `--notes`. It used
  to be dropped under a success line.
- The gap-only fields are refused on a backlog or tooling record, where
  nothing read them.

### Parked work names its holder and can be claimed

- `working-set select <scope> --resume` resumes a parked piece that nobody
  holds. Another person's parked piece is refused naming them;
  `--resume --claim` takes it over, and the hand-over is recorded on their
  row. The suspended table shows each holder, or `claimable`.
- `working-set select` prints the row the store recorded, not the request it
  sent.
- `working-set select --recon-revision <rev>` records the revision the
  reconnaissance was taken at, which `working-set status` reads back.

### A member added to an entered epic can enter

`process enter <epic>` opens a members-only entry gate for a PROPOSED member
added to an epic that is already entered (TODO, READY, IN_PROGRESS, IN_REVIEW
or BLOCKED). The gate is pinned at the epic's aggregate, so the epic's own
cold review applies to it. It used to be refused as pinned elsewhere. The plan
block, and so `--dry-run`, states the pin the verb will send.

### `author trace --purpose upper` finds its own pin

Left out, `--fingerprint` resolves to the user requirement's content hash, and
the transition defaults to `build->verify`, as they do for a lower trace. The
server now refuses a shortened display pin on an upper trace as it does on a
lower one: that pin matched nothing.

### `context` and `feedback` report failures instead of hiding them

- `modernpath context` reports a rejected credential as a credential problem,
  with the command that repairs it, and every failure exits non-zero. "Not
  relevant" is an answer and exits 0.
- The context hook still stays silent on a failure, and it no longer refreshes
  the credential: a hook that hit its deadline mid-refresh could leave a spent
  refresh token on disk and force a new sign-in.
- `feedback` writes the line to the interim file when the workspace is not
  bound, when the credential is rejected, or when the bound system cannot be
  reached, instead of exiting and losing it.
- The interactive system picker shows each system's id, so two systems with
  the same name can be told apart.

### Help and reference corrections

- `author advance --help` names `fingerprint` as the gate value to pass, not
  `content_fingerprint`.
- `author update --criteria` documents its shape. Set `position` on every criterion
  or on none; with none, the order you send is the order stored.
- `.modernpath/cli-reference.md` lists the flags each verb inherits from its
  command group.
- The subagent guard lets a delegated agent read a write verb's `--help`.

### A defect in a legacy-entered epic returns to review on its test proof

- A system requirement reopened by an applied defect demotion, in an epic
  entered through a legacy approval gate (no entry gate names it), returns to
  IN_REVIEW through `process advance` on its RED and passing lower trace. It
  used to be refused with `process reenter`: a cold review of the whole epic
  and a new approval that PROCESS.md does not ask of a defect. `process next`
  routes such a member to build, and `process check` no longer names
  `process reenter` for it. The CLI reads the new `defect_reopen_without_entry`
  member fact from the server. Any entry gate that names the item, whether
  current, stale or not yet applied, keeps its guard and its remedy
  (REQ-CROSS-435).

### The messages name the member, the remedy and the right recipe

Six operator-facing corrections, all in what the tool says:

- `process complete` prints two apply recipes, because `author advance --kind`
  defaults to `requirement`: one for the members (every SR and the UR) and one
  for the epic with `--kind epic`. Pasting the single recipe for the epic was
  refused 422.
- `process complete` refuses a DONE epic with members still IN_REVIEW **before**
  it writes anything, and names the sanctioned reopen. The server refuses a
  members-only completion gate for a done owner, and that refusal used to arrive
  after the delivered run and the completion trace were already recorded.
- `process check --phase build|verify` names each member whose evidence is not
  current — status, evidence state, whether a RED is recorded, whether the lower
  trace passes — and `--phase entry` names the members with no live entry.
  Both checks used to fail with no id in sight.
- `process next` prints the remedy for `entry_origin_unavailable` and names the
  members it applies to: the entry origin is a member's own fact, so the verb is
  `process reenter <member-id>`. Other reasons still print the reason alone.
- `process reconcile --help` states the predicates per kind: a system
  requirement is entry-gated (an applied entry gate at the current packet
  aggregate **and** a recorded RED), a user requirement has no entry gate of its
  own and enters on its RED or a required SR in progress.
- The context hook — the CLI's and the three shell hooks — labels the injected
  block as retrieved reference data, not instructions, before the content.

### A reused completion trace no longer skips a member's evidence

`process complete` reuses a completion trace that already passes at the
unchanged packet aggregate. It used to skip the whole evidence run with it, so
a member the completion added after that trace was recorded got no evidence at
the delivered revision and the completion gate refused "not yet" for it. The
run is now posted for the targets the reused trace's own run did not name —
none when it named them all, and the whole run when that trace's body carries
no readable target list, because unread coverage is not taken for coverage. A
trace is immutable, so the remainder is not recorded on it: a repeat at the
same aggregate posts the remainder again, and the plan lines say so.

## v0.8.0 — onboarding reads the code you authorized, and its evidence cannot move

### `modernpath reverse-engineer` establishes an as-built baseline

A codebase with no requirement corpus had no sanctioned way into the loop: the
skill described the pass, and every step of it was manual. `reverse-engineer`
now runs it — bind the workspace, sync documentation, inventory the declared
repositories, derive candidates against explicit denominators, and hand
confirmed scope to the loop. It takes either road: an authorized as-built
baseline, or review-only `DERIVED` proposals for an existing corpus.

### `reverse-engineer inventory` reads repositories the tooling used to skip

The inventory is read-only and local, and it no longer assumes Git. Repeat
`--repository key=directory` for each one; a non-Git root and a legacy source
format are both inventoried rather than passed over, so the denominators a
derivation is measured against cover the estate that is actually there.

### `reverse-engineer capture-source` refuses anything but the authorized bytes

Capture takes exactly the files the authorization names. A file whose bytes
changed since it was authorized, and a symlink pointing outside the captured
tree, are both refused rather than captured quietly — the baseline states what
the code was at a named revision, and that claim is only worth the isolation
behind it.

### Document evidence is immutable across the API, the CLI and the UI

A document cited as evidence can now be read back exactly as it was when it
was cited. The snapshot is required on every surface that serves it, so a
citation cannot come to mean something else because the document moved on
underneath it.

### Onboarding writes are refused to a delegated agent

`reverse-engineer`'s mutations join the verbs the subagent guard covers. A
delegated pass returns findings; the session that owns the work records them.
The guard is what makes that a property of the tool rather than of everyone
remembering.

### `rdd-audit` ships as a package skill, with its citation auditor under test

The citation auditor moves into the process package at
`.modernpath/rdd/skills/rdd-audit/audit-citations.mjs` and gains a test suite.
A foreign-history citation no longer reads as a false positive, and the skill
states how an instrument fails as well as how to run it.

### `rdd-reverse-engineer` and `mp-process-cli` describe the pass the CLI runs

Both skills are rewritten against the verbs that now exist: the
reverse-engineering procedure follows the command rather than describing the
work by hand, and the tooling skill carries the onboarding sequence and the
refusals it meets.

## v0.7.0 — the CLI ships its own instructions, and a shipped item can be reopened

### `process reenter --gate-id` opens a successor when the default id is reserved

`process reenter <scope>` hardcoded the re-entry gate id to `REENTRY-<scope>`.
If a prior `REENTRY-<scope>` was withdrawn — which permanently reserves the id —
open refused (`a gate is opened once`) and the shared `gateIDFree` message
suggested a `--gate-id …-R2` successor that `reenter` did not accept (`unknown
flag: --gate-id`), leaving no CLI path to re-open a re-entry gate after a
withdrawal. `reenter` now accepts `--gate-id`, mirroring `process enter` and
`process complete`, so a successor id can be opened. BACKLOG-TOOL-52.

### `process reenter` re-establishes entry after a material reversal

`process reenter <scope>` opens a human re-entry approval gate at the current
packet aggregate; `process reenter <scope> --apply` (after the gate is answered
`approve`) re-pins the stranded applied entry gate to the current aggregate so
the reopened item can build again. Unlike `process reapply-entry` — an
immaterial-only attestation — re-entry requires a fresh **independent cold-review
PASS at the current aggregate** and the answered human gate, because the content
materially changed (a reversed decision). The item stays IN_PROGRESS (no
lifecycle transition); `reapply-entry`'s immaterial-only contract is untouched.
This closes the follow-on gap to `process supersede --apply`: a reversed-decision
reopen lands at IN_PROGRESS but previously had no honest re-entry path
(`process enter` needs PROPOSED; `reapply-entry` forbids a material change).
BACKLOG-TOOL-44.

### `process supersede --apply` reopens one shipped item without a global enforce flip

`process supersede <id> --apply --source USER:… [--supersedes USER:…]` applies
the invalidation cascade for that one item — demoting a shipped `DONE`/`IN_REVIEW`
item back to `IN_PROGRESS` and staling its evidence — while the system's cascade
mode stays `report`. This is the scoped, attributed way to reopen a shipped
requirement when an approved decision is reversed, instead of flipping the whole
system into `enforce` (which then cascades on every later edit). Without `--apply`
it stays a dry run. `--apply` requires an attributable `USER:` source (server- and
client-enforced); `--supersedes` records the prior decision the reversal overrides,
and both ride the item's durable drift event so the reversal is captured old→new
(BACKLOG-TOOL-39).

### The kit ships the reviewer agent definition

`modernpath install` writes `.claude/agents/rdd-cold-reviewer.md` — the
delegated cold review's read-only context (Read, Grep, Glob; no shell; the
review rules) — in both modes, and `install --check` reports an edited or
missing copy as drift. The managed block and the `mp-process-cli` skill's
cold-review step name it (REQ-CROSS-409).

### `feedback --last` shows what it attaches; `--ref <n>` picks an earlier command

With `--last`, `feedback` prints the command it is about to attach — the
command line, its exit status and the first output line — before the record
is written. `--ref <n>` attaches the n-th most recent recorded command
(`--ref 1` is `--last`) and implies `--last`; a value beyond the recorded
count is refused naming how many entries exist (REQ-CROSS-410).

### Failing checks and the Drift label say why

`process check --phase cold_review` prints, under a failing
`independent_verdict`, the trace it chose and why it does not count — the
verdict, or which arm of the independence rule the review context failed (no
review context on the trace, it authored a section of this scope, it authored
a patch in this scope); under `UNAVAILABLE`, that no trace exists at the
current aggregate and the scope's traces pinned at other aggregates; under a
failing `findings`, the open or deferred material finding ids. `--phase
completion` names the settled gate that is missing, or the members not yet
`IN_REVIEW`/`DONE` with current evidence. The facts serve every reason
(`independence_reason`, `open_finding_ids`, `stale_traces`,
`settled_gate_external_id`, `members_not_reviewed`); the CLI computes nothing.
A `[Drift]` line in `your-move` carries its basis — the run's revision and the
head it no longer matches, or the run's time with its validity lapsed — and the
re-record verb; `factory drift`'s help defines the label (REQ-CROSS-408).

### Release activation records the selection gate; `pin_required` says whose PIN

`factory release activate` now writes the release-selection gate on the
system it runs from, in the release's transaction: `GATE-RELEASE-<slug>`,
answered by the activating person with the `USER:` source and stamped to the
release. The reads (`factory status`, `SELECTION.md`, the preflight) resolve
the newest approved gate naming the release rather than a fixed id, so a
successor `GATE-RELEASE-<slug>-<n>` — written when an open or answered gate of
that purpose and scope is superseded, or beside a dismissed one — is found,
and a release active with no gate on this system reads `no recorded selection
gate on this system` with the activation as the remedy; a gate written before
the scope token existed still binds under its bare id, and an approved gate
without a `USER:` source is superseded like an unaccepted one, so the remedy
cannot loop. The `pin_required`
refusal names the signed-in person's release PIN, Mission Control and
`--pin`; `pin_locked` names the lockout without a duration. A gate is now
pullable by its own id — `working-set pull GATE-RELEASE-<slug>` (or an entry
or completion gate) writes its block with the `Fingerprint:` line a gated
advance echoes (REQ-CROSS-407).

### `install --store-backed --source USER:…` declares a ledgerless workspace in one step

A bound workspace that never had file ledgers is declared store-backed by
the tool: one authoring action records the store-backed activation gate,
answered by the signed-in person with the `USER:` source, and sets the
system's process-store state active in one transaction; the CLI then writes
`process/store-backed.md` through the writer `migrate flip` uses, with no
retired files, and the install that follows withholds the ledger skill. The
server decides by its own state — a seeded or cleared system is refused by
name and goes through `migrate flip`, an already-declared one is reported, a
declaration gate that already exists in a state that cannot carry the
declaration is refused naming that state — and a workspace with ledgers under
`tasks/` is refused here. The state the flip's HTTP declaration records is
written by the same server function, under a row lock (REQ-CROSS-406).

### `init` signs in before it lists systems; the api-client verbs state the credential

On a checkout with no bound system, `modernpath init` establishes the
credential before it lists systems: with none stored, or a stored token whose
expiry has passed, it runs the sign-in for the target server when there is a
terminal and otherwise stops naming the sign-in command as the next step. The
listing is never attempted with a missing or expired credential. Without a
terminal, one system is bound as the only choice and several are listed with
`--system-id` named as the way to choose. A refused listing names the step
and the verb that continues, and leaves the workspace unbound. `docs sync`,
`search`, `ask`, `read-doc` and `read-file` now carry the three credential
statements the factory verbs already print: no binding, no or expired
credential (before any request), and a served 401 rendered as a statement
about the credential with the repair command — never `HTTP 401` alone
(REQ-CROSS-405).

### `mp-process-cli` glossary covers the refusals a fresh system meets

Four refusals the section G run met with no glossary row now have one:
`pin_required`, `does not exist` / `unknown external id`, the upper trace's
required transition, and a criterion without an `external_id`. The upper-trace
example carries `--from build --to verify`, and the write-channel table states
the `--criteria` object shape.

### `mp-process-cli` opens with the phase-to-verb map

The skill now starts with a table from each `PROCESS.md` phase to the verbs
that drive it and the section that gives their order, so a reader arriving
from the phase table finds the entry point without reading the skill end to
end. The interim gaps file is retired as the channel: `modernpath feedback`
files the record, and the file receives a line only when the store cannot be
written.

### The managed block points at the process skill from the reading order

The installed `AGENTS.md` block now says, on the `PROCESS.md` line of its
reading list, that in a store-backed workspace every phase is driven through
the `modernpath` CLI and that `mp-process-cli` is the verb sequence for each
phase, not background reading. A session that read the block in order used to
describe the loop with no verb in it, because the skill was filed under
"tooling rather than process" after the twelve-skill list. The block stays a
pointer; no verb detail moved back into it.

### Help and skill text follow the named length refusal

An over-length bounded authoring field is now refused by the server as a
422 naming the field (`should be at most 255 character(s)`), so `author
requirement|update`, `process findings add`, the `mp-process-cli` glossary
and its traps say that instead of describing an empty server fault.

### Help text names the caps, the UR in a completion gate and the retire-and-reopen recipe

`--help` (and the installed `.modernpath/cli-reference.md`) now states what six
sessions met as an unexplained refusal: `process findings add` lists the nine
categories the server accepts and keeps the body a pointer; `author gate`
says a completion gate names the epic's UR beside the SRs and how to retire a
stuck governed gate under a new id with `--supersedes`; `factory evidence` says
the UR needs a run of its own; `author requirement|epic|update` name the
255-character scalar cap and where prose goes; `working-set push` explains the
stale-section stamp after a record patch; `process next` explains the
several-pieces "no current selection" and the process-repin aggregate move.
Help text only — no behavior changes.

### Delegated agents cannot write the process store; permission rules ship with the kit

`modernpath check --hook PreToolUse` — the adapter `hooks install` wires into
Claude Code and Codex — now denies a store-write verb (`author …`, `factory
answer|evidence|sync|release|connect|pin|pull`, `working-set select|push`,
`process reconcile|findings add|findings disposition|supersede|cascade-mode`,
`migrate`, `env --set`, `docs push`, `import`, `new`) when the hook payload
carries a subagent identity, with the process rule as the reason: a delegated
pass returns findings and a verdict, the orchestrating session records them.
Reads, `auth status` and every main-session call pass as before.

`hooks install` also merges the kit's Claude Code permission rules for the
CLI verbs into `.claude/settings.json`: reads, routine writes and the in-loop
decision verbs are allowed, the system-wide verbs ask, both invocation forms
ruled. A rule is added only where the workspace holds it in no list — a rule
you moved, to `ask` or to `deny`, stays where you put it through every
install; `hooks uninstall` removes the kit's rules from allow and ask and
leaves a denied one alone; `hooks status` reports how many are placed.

The guard reads a command the way the commit gate does — through chains,
env-assignment prefixes, `command`/`env` wrappers, paths, root flags and
shell `-c` bodies — and quoted prose is not a command. Only an identity field
(`agent_id`, `subagent_id`) marks a delegated agent; `agent_type` alone never
does.

### `hooks install` keeps the project's settings bytes

Installing or reinstalling a hook family into `.claude/settings.json` (and the
Codex, Cursor and Pi settings) used to rewrite the whole file through the
default JSON encoder: `>` and `&` in every hook command came out as `\u003e`
and `\u0026`, top-level keys were re-sorted, and an owned entry was replaced
whole — a second, project-owned command placed beside the tool's in the same
matcher group was dropped. The writer now keeps the file's top-level key
order, never HTML-escapes, and salvages foreign commands from a replaced entry
into the replacement, so a reinstall on an unchanged file is byte-stable.

### `modernpath install` writes a command reference rendered from the binary

`.modernpath/cli-reference.md` now lists every verb, flag and default of the
binary that wrote it, rendered from its own command tree, so it cannot drift
from the build; `install --check` reports an edited or stale copy as drift,
and the file is versioned beside the process snapshot. The loop verbs' `--help`
(`author gate`, `author trace`, `author advance`, `working-set select`,
`factory evidence`, `process findings add`, `process reconcile`) now state
their prerequisites and order — which fingerprint each trace purpose takes and
where to read it, trace before gate, members before the epic, RED at the RED
commit, which finding categories block — and the reference carries the same
text. The managed `AGENTS.md` block and the `mp-process-cli` skill point at it.

### `mp-process-cli`: the loop's CLI recipes ship as a skill

`modernpath install` now writes `.claude/skills/mp-process-cli/SKILL.md` in
both file-backed and store-backed workspaces: the exact verb sequence for
plan → cold review → entry and build → evidence → completion → apply, the
work-selection model, the three fingerprints and what moves them, a refusal
glossary with the verb that clears each, and the traps six sessions paid for.
The managed `AGENTS.md` block shrinks to the entry reads and a pointer at the
skill; the write-channel table and the retired-file prose moved into it.

### Embedded process snapshot: req-driven-dev `55e4912`

`modernpath install` now writes the canonical rules merged in req-driven-dev
PR #20: cold review audits the change rather than the packet and stops after
two rounds; a single-requirement packet is bounded to one page; a diagnosed,
bounded defect may cite its failing test as a source before entry; a
delegated pass writes nothing to the store and returns a refusal verbatim;
the delivered revision is the integrated one; and the project's sanctioned
tool is the only path to the store — a missing surface is a gap to surface,
a hand-written store change is a stop.

### `modernpath install` with CLAUDE.md symlinked to AGENTS.md

When `CLAUDE.md` and `AGENTS.md` are the same file (a symlink in either
direction), install used to merge both managed blocks into it under the same
markers, and whichever it wrote last replaced the other — sometimes leaving
`AGENTS.md` with only the Claude `@`-imports, which Codex does not follow.
`install --check` then reported drift on every run. Install now merges only
the `AGENTS.md` block into the shared file, reports `CLAUDE.md` as carrying
it, keeps the symlink, and `--check` and `--dry-run` treat the link the same
way.

### `modernpath auth` chooses a workspace

If you have access to more than one workspace, the browser sign-in now lists
them by name after you sign in and asks which one to use, then signs you in
to it; every command after that answers from that workspace. The choice is
kept in `.modernpath/auth.json` beside the credential and applied on the
next bare `modernpath auth` on the same plane; `--choose-workspace` asks
again. `--workspace <id>` names the workspace outright, for scripts and
headless hosts, and is refused before any request when the value is not a
workspace id, when combined with `--choose-workspace`, or on a host that
takes a pasted token. The device flow cannot list your workspaces (ZITADEL
refuses the list to its token), so it never asks: a bare `--device-flow`
keeps the workspace the sign-in lands in — your own, for most people — and
says how to reach another, `--workspace <id>` names one, and
`--choose-workspace` is refused with it. A sign-in whose token names no workspace at all — an identity service that
predates workspace-scoped tokens — is stored without one, and a chosen
workspace is then refused with that reason rather than a blank id. The CLI
never stores a credential for a workspace
other than the one you named: if the issuer lands the sign-in elsewhere,
the command fails and the previous credential is left as it was. A failed
browser or device sign-in now exits non-zero, so a script can stop on it.
`modernpath env` shows the workspace the credential is for. Sign-in asks for
two more scopes — ZITADEL's own audience, which the workspace list needs,
and the resource-owner claims, which the platform needs to recognise a
workspace you were granted into — and, when a workspace is chosen, the
organization filter that narrows the token to it. (EPIC-CLI-009,
REQ-CROSS-334..336)

### `modernpath auth` opens your browser

`modernpath auth` against production or the test plane now runs OAuth
authorization code with PKCE: it opens your browser at ZITADEL, listens on a
loopback port for the sign-in to come back, and exchanges the code — no URL to
copy, no code to approve. The RFC 8628 device flow is still there for where
that cannot work, and the CLI picks it on its own when it sees an SSH session
(tmux on a remote box included), a CI shell, a container, a Linux host with no
display or no `xdg-open`, or a browser that fails to launch — printing why.
`--device-flow` forces it. Once the browser is open there is no fallback: a
refused or timed-out login fails as such rather than asking you to approve
twice. The five-minute login timeout covers the whole sign-in — the wait for
the browser and the token exchange — and OIDC discovery has a timeout of its
own, so a stalled issuer fails the login instead of hanging it.

The browser launcher honors `BROWSER` when set, so WSL, editor remote sessions
and unusual desktops can name their own — read the way `xdg-open` reads it: a
list of commands separated by the path-list separator, tried in order, where
`%s` stands for the URL; arguments split shell-style, so a program path with
spaces can be quoted. Setting `BROWSER` also says the browser is reachable:
it takes precedence over the SSH, CI and container checks, which is what a
VS Code remote session or dev container that forwards the loopback port
needs. `CI=false` no longer counts as a CI shell. A launcher that starts and
then exits with an error is treated as no browser, so the fallback fires at
once rather than after the timeout. On Windows the URL goes straight to the
registered protocol handler instead of through `cmd.exe`.

### `factory gates` reads a gate's state, not just the open queue

`modernpath factory gates <external_id>` shows one gate by id — its state and,
when answered, the answer, chosen options, `USER:` source, answerer and whether
the answer has been applied — so "did my approval land?" is answerable from the
CLI without reading the event stream. An applied answer is stored `closed` and
is read by id or under `--state all`.
`--state open|answered|dismissed|superseded|all` lists gate history (an unknown
value is refused before any request; a server `422` is surfaced verbatim), and
`--json` prints the server's gate envelope on stdout and nothing else —
`{"gates": […]}` for a listing, `{"gate": {…}}` by id. The bare `factory gates`
is unchanged: the open decision queue. (REQ-CROSS-109)

### `working-set` guards the planning packet against blank and unfilled sections

`working-set pull` now scaffolds the packet skeleton — a stub for each canonical
section the phase table requires that the store does not serve (reconnaissance,
red strategy, decisions, and one enrichment per system-requirement member) — so
an agent that pulls a scope sees what the loop expects, without ever overwriting
a local draft. `working-set push` treats a blank file or an unfilled stub as not
authored: it is skipped and named, never sent as a whole-blob put and never used
to empty a served section; a filled stub is sent with its marker line removed.
The store refuses a blank packet section outright, so a canonical section can
never register as present with nothing behind it. (REQ-CROSS-330/331/332)

### The tooling skill carries the completion-gate UR rule, the retire-and-reopen recipe and the caps

`mp-process-cli` now says what five stuck completion gates on a production store
taught: the epic's user requirement is a named item of the completion gate — it
needs its own passing evidence, its own `--scope`, and its own `author advance`
— while `working-set pull <epic>` lists only the SRs, so the UR is pulled by
id. A gate that can never be approved is retired by superseding it at the
current aggregate (`--supersedes`), with the suspend/resume piece-holding that
keeps other held pieces unambiguous. The refusal glossary gains the findings
category vocabulary, `no route derived`, the several-pieces refusal and the
nested-config binding trap; the traps name the 255-character field cap,
the stale packet-section stamp and the process-revision fold of the aggregate.

## v0.6.0 — the process became twelve passes, and the loop got a place to stand

`modernpath install` now writes the full requirement-driven process rather than a
handful of skills: **twelve passes** under `.modernpath/rdd/skills/`, each one a
phase you can enter and exit, pinned to `req-driven-dev@e3f60dde`.

`rdd-start` · `rdd-discover` · `rdd-plan` · `rdd-cold-review` · `rdd-entry-review`
· `rdd-build` · `rdd-verify` · `rdd-completion-review` · `rdd-triage` ·
`rdd-deliver` · `rdd-audit` · `rdd-reverse-engineer`

The old flat set (`rdd-planning`, `rdd-discovery`, `rdd-build-loop`) is gone. The
replacement is not a rename: entry is now a gate with a packet behind it,
cold review runs from a context that did not author what it reviews, and
completion audits evidence at the delivered revision rather than at the revision
someone remembered. `PROCESS.md` ships alongside as the single authority, and
`file-state/` carries the record shapes it serializes.

### `modernpath focus` — say what you are on, without claiming it

Visibility only: no assignment, no lock, nobody's queue changes. Declare it,
clear it, list what everyone else declared. `--infer` reads the refs you are
actually touching and proposes the answer, with a hysteresis buffer so a single
stray file does not flip your focus. A transition emits `focus_changed` naming
the human who caused it.

### Your move, on arrival

A `SessionStart` hook writes a personal brief: what is waiting on **you**,
ordered by what it unblocks, not by when it was created. The hook family owns no
repository files — it reads and writes nothing you have to merge.

### `factory sync` stops falling over on a bad afternoon

Tunable chunk size, retry on 5xx instead of abandoning the batch, and `--no-docs`
when you want records without the document payload. Reachability warnings now
name the system they could not reach, which turns "connection failed" into
something you can act on.

### Fixes worth naming

- **The health probe never sent the bearer**, so a reachable system answered
  `401` and reported itself unreachable. Two of the three probes also skipped the
  Gateway prefix.
- **12 of 20 open-question gates were losing their brief** between authoring and
  the wire — the decision arrived without the reason for it.
- **Five approval reports were false.** A dormant test, switched on, found them.
- **The answered-gate log was written into a directory the CLI never creates.**
  `factory pull --apply` opened `mission-control/ANSWERS.md`, got `ENOENT`,
  swallowed it, and reported success — so in every installed workspace the log
  was silently empty. It now writes `ANSWERS.md` and `answers.jsonl` at the
  workspace root.
- **Hook migration was guarded for Claude and not for Cursor**, so Cursor users
  kept a stale hook after upgrading.

### Changed defaults

- `modernpath env` defaults to **cloud production**. `beta` still exists as a
  named environment, now marked deprecated.
- The `--legacy-extractor` flag is removed. It shelled out to a node op-builder
  that only ever existed in one repository; the bundled Go parsers have been the
  default for some time and are now the only implementation.

## v0.5.0 — a repository with no requirements gets a ledger

`modernpath install` now ships **`rdd-reverse-engineer`**: the method for turning
an existing codebase into a requirement ledger, so the four-command path —
`auth`, `init`, `install`, then ask — works on a repository that has never heard
of this process.

It is not a prompt. It is a procedure that was run against two real client
repositories and corrected eight times by what they did to it.

### The method, in the order that matters

**A — the domain first.** Read the schema directly: entities, owners, and the
invariants only constraints state. Draw context boundaries by **aggregate
ownership** — whoever writes a table owns it — not by route-file layout, which
follows the build system rather than the business.

**B — then the surfaces.** Every view, the actors its role gate admits, the
endpoints it calls. Group them into journeys and write one epic per journey,
each carrying a **user requirement**. A pass that emits only ledger rows produces
a system with no stated purpose.

**C — only then the requirements.** Enumerate entry points and work the list:
HTTP routes, **agent and LLM tool surfaces**, workers, webhooks, CLI commands.
Two passes over one context differed by an entire conversational surface because
one counted only HTTP.

### Honesty rules, each learned the hard way

- **`DONE` is not reachable by a derivation pass.** `PROCESS.md` §7.6 requires
  human sign-off. Test evidence lives in an `Evidence` column instead — *run,
  passing* / *mocked* / *no test* / *not run* — because the vocabulary has no
  value for "test-verified but unapproved".
- **Nothing derived is `PROPOSED`.** That means *not started*, and a payment flow
  running in production every day is not an unstarted proposal. The one exception
  is behaviour the system should have and does not, sourced `finding, this pass`.
- **A mocked-out service is not evidence.** A route test that patches the service
  it nominally tests verifies routing, not behaviour.
- **Ambiguity becomes a question, not an inference.** Code answers *what*, rarely
  *why*.
- **A coverage manifest is the only definition of "done enough"**, and it gates
  the sync. Five passes once produced an accurate ledger covering 10% of its
  system with nothing anywhere saying so.
- **Audit your own citations, and skip named absences.** A row saying *"there is
  no `test_draft_service.py`"* is naming a gap, not citing a file.

### What it produced

On a 156-endpoint platform: **967 requirements across 11 of 11 contexts**, 16
journey epics, 36 of 36 reachable views carrying a user requirement, **3,222 of
3,222 citations resolving**, and zero rows claiming verification they did not
have. Findings no endpoint walk reaches: 13 unreachable views, six tables written
by two owners, an agent tool returning contact details the REST route gates.


## v0.4.0 — the harness hooks stop lying, and start being fast

### Hooks own no repo files

Both hook families invoked scripts under `.claude/hooks/` by relative path, so
each depended on two things it did not control: the file surviving in the working
tree, and the harness running from the repository root. When either failed, the
shell died before reaching the `exit 0` that keeps a hook silent — every turn
ended in `No such file or directory` and no sync ran.

Both now invoke the installed CLI directly and write nothing into the repository:

```
command -v modernpath >/dev/null 2>&1 && ( modernpath factory sync --if-quiescent --trigger Stop >/dev/null 2>&1 & ) ; exit 0
command -v modernpath >/dev/null 2>&1 && modernpath context --hook UserPromptSubmit 2>/dev/null || echo '{}'
```

`command -v` makes a missing CLI a silent no-op. Install **migrates**: it deletes
the legacy scripts and strips settings entries that name them — previously a
reinstall no-oped on broken wiring, because the legacy entry counted as
"installed".

### `modernpath context --hook` — 11–28s becomes ~1s

The hook used to spend 0.8s deciding relevance and then 9–27s on a reasoning
model writing an essay, for a consumer that has grep and file reads.

- **Specific questions cost no synthesis at all** — ranked file paths with their
  purpose. Measured p50 **1.2s**.
- **Only high-level questions buy prose**, from a capped fast model, and only
  when something was actually retrieved. Measured **2.1s**. A model asked to
  summarize an empty result answers from the question alone.
- **Irrelevant prompts stay silent** in 0.8s.
- Decomposed queries are searched **separately and merged**; the old code joined
  them into `"Core.Sync Also: epic op Also: Core.Sync epic op"` and handed that to
  a model as if it were a question.
- The searches run **alongside** the relevance gate rather than after it.
- Every response carries the workspace's own state — release, requirement counts,
  open gates and how many are approvals, next READY rows — computed from stored
  state with no model call.

### The hook says what happened

`.modernpath/context-hook.log` gets one line per run, naming the **reason**:

```
2026-08-11T07:59:35Z · delivered · 1.8s · 2731 bytes
2026-08-11T08:43:47Z · failed · 1.4s · server error 400 {"error":"..."}
2026-08-11T07:59:36Z · empty · 0.7s · not relevant
```

Previously every failure — missing config, auth error, non-200, decode failure,
timeout — rendered as an empty result and read as "nothing relevant", so a broken
hook and a quiet one were indistinguishable. A client-side deadline (default 8s,
`--deadline`) returns empty rather than holding the turn.

### `modernpath hooks doctor`

The hooks call the CLI by bare name, so PATH decides which build runs. A second,
older copy earlier in PATH silently downgrades every hook run while still exiting
0 — which happened here, with a tested deadline and log that never executed.

`hooks doctor` reports which binary wins, which are shadowed, whether both hook
families are wired, whether any entry still points at a removed script, and
recent failures from the log.

### `modernpath install` tells agents where the knowledge is

The managed `AGENTS.md` block now carries a **Codebase knowledge** section: the
API finds (`modernpath search`, or the context hook), the local export reads
(`.modernpath/modernpath/…`), `modernpath docs sync` when the export is absent,
and the API wins when they disagree — the export is a cache. In the managed block
rather than a skill, because skills are Claude-only and Codex reads `AGENTS.md`
where it does not read `CLAUDE.md`.

### `scripts/install-local.sh`

Writes **every** copy of `modernpath` on PATH rather than only the usual homes —
it previously missed `~/.local/bin`, which is exactly where the shadowing copy
lived. Stamps commit and build time into the version string, and warns loudly
about copies it could not write instead of skipping them silently.

---

Earlier releases are catalogued in the GitHub releases for `ModernPath/cli`.
