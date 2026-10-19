# ModernPath CLI

This is the canonical guide for the `modernpath` command-line interface. The
Getting Started section is written for users and may be reused by product help
surfaces. The Technical Description records how the CLI actually works and
links every behavioral claim to the implementation.

Other READMEs and UI surfaces should link here instead of maintaining a second
command guide. When this document and `modernpath --help` disagree, the current
binary and the source files cited below win.

## Getting Started

### Install the CLI

#### macOS and Linux

Homebrew (recommended):

```sh
brew install modernpath/tap/modernpath
```

##### Install script alternative

```sh
curl -fsSL https://raw.githubusercontent.com/ModernPath/cli/main/scripts/install.sh | bash
```

#### Windows

Scoop (recommended):

```powershell
scoop bucket add modernpath https://github.com/ModernPath/scoop-bucket
scoop install modernpath
```

##### PowerShell alternative

```powershell
irm https://raw.githubusercontent.com/ModernPath/cli/main/scripts/install.ps1 | iex
```

#### Verify the installation

Confirm that the command is available:

```sh
modernpath --version
```

### Connect a repository

Run setup from the root of the repository you want to connect:

```sh
cd /path/to/repository
modernpath auth
modernpath init
modernpath status
```

A fresh repository connects to production (`https://api.modernpath.ai`) by
default; no environment selection is needed.
`modernpath auth` opens your browser to sign in; no API token needs to be copied.
For a machine without a browser, run `modernpath auth --device-flow`.
`modernpath init` lets you choose the system represented by this repository,
and `modernpath status` confirms the connection.

If `modernpath auth` shows `http://localhost:4000` and asks for a token, check
`modernpath env`: this repository may have a saved local binding. To connect to
production instead, cancel the token prompt and run:

```sh
modernpath env --set=production
modernpath auth
```

For the cloud test environment, use `modernpath auth --test`. Local development
is explicit: `modernpath env --set=local` or `modernpath auth --local`. Local and
custom servers require an access token issued by their configured identity
provider.
If the command or flags differ from this guide, check `modernpath --version`
and update using the installation method above.

### Install the process and agent hooks

From the connected repository root:

```sh
modernpath install
modernpath hooks install
```

`modernpath install` installs the CLI's pinned process snapshot and agent
instructions. `modernpath hooks install` configures the ModernPath integration
for supported coding agents. Use `modernpath install --check` to inspect drift
and `modernpath hooks status` to inspect hook configuration.

Hook installation and removal clean up obsolete ModernPath entries while
preserving project hooks. `modernpath hooks doctor` reports any remaining
obsolete entries; JSON output includes `retired_hooks` for each agent.

### Upgrade

#### macOS and Linux

With Homebrew:

```sh
brew update && brew upgrade modernpath
```

#### Windows

With Scoop:

```powershell
scoop update modernpath
```

#### After upgrading

If you used an install script, run it again to get the current release.
Re-run `modernpath install` in each connected repository after any CLI upgrade.
Use `modernpath install --check` to check for drift, or
`modernpath hooks doctor` if a different CLI version appears on your PATH.

Signed archives for manual installation are available from the
[ModernPath CLI releases](https://github.com/ModernPath/cli/releases).

### Authentication and advanced behavior

The release installers choose the correct archive for the operating system and
CPU. Set `MODERNPATH_VERSION` to pin a release and
`MODERNPATH_INSTALL_DIR` to override the Unix installation directory.

`modernpath init` shows the systems available in your workspace. It downloads
the selected system's Knowledge Core export and writes the repository binding
to `.modernpath/config.json`.

`modernpath auth` opens your browser at ZITADEL and listens on a loopback port
for the sign-in to come back (OAuth authorization code with PKCE). Sign in;
the browser tab reports "Signed in" and the CLI stores the resulting
credential in the repository's gitignored `.modernpath/auth.json`. Where no
browser can reach this machine — an SSH session, a container, a CI shell, a
headless Linux box — the CLI falls back to the RFC 8628 device flow on its own
and prints a URL to open on any device; `modernpath auth --device-flow` forces
that path. `BROWSER` names the program that opens the URL and, when set, also
tells the CLI the browser is reachable.

If you have access to more than one workspace, the browser sign-in lists them
by name once you have signed in and asks which one to use, then signs you in
to it; the choice is kept beside the credential and applied on the next bare
`modernpath auth` on the same plane. `modernpath auth --workspace <id>` names
the workspace outright — for scripts and headless hosts — and
`--choose-workspace` asks again. The device flow cannot list your workspaces
(ZITADEL refuses the list to its token), so it never asks: a bare
`--device-flow` keeps the workspace the sign-in lands in — your own, for most
people — and says how to reach another, `--workspace <id>` names one, and
`--choose-workspace` is refused with it. The CLI never stores
a credential for a workspace other than the one you named: if the sign-in
lands elsewhere the command fails and the previous credential stays. A
failed sign-in exits non-zero. `modernpath env` shows the workspace the
credential is for.

`modernpath auth status` reports that state without signing in: whether a
credential is stored, who it was issued to (the actor recorded at sign-in),
the workspace, server and bound system it is for, when it expires, and — by
default — whether the server still accepts it (the same `GET /api/systems`
check `auth` runs to validate a sign-in). A refusal names its reason and the
sign-in command that repairs it. It exits non-zero when not authenticated, so a
script can gate on it (`modernpath auth status || modernpath auth`); `--offline`
skips the network and reports only what the stored credential says about itself
— there `authenticated` means present and locally valid, not server-verified
(`checked_online` is false); `--json` prints a single status object on stdout
and nothing else. Read it to
check authentication state rather than parsing `.modernpath/auth.json` or
calling the API by hand.

Every store command checks the credential's expiry before anything leaves the
process. Inside the last ten minutes it renews the token at the identity
provider when a refresh token is stored (the renewed pair replaces the stored
one; concurrent commands renew once, under `.modernpath/auth.lock`); when it
cannot, it warns once per credential with the time left and the sign-in
command; once expired it refuses before any request, naming the expiry time
and the remedy. The once-per-credential notices live in
`.modernpath/cli-notices.json`, local state the CLI keeps for itself.

### Local development and environment switching

For a local development server, use the local option for both steps:

```sh
modernpath auth --local
modernpath init --local
```

`modernpath env --set` switches the whole binding: it keeps the current
system binding under its environment name in `config.json` and restores the
target's, and keeps the current credential under its identity provider in
`auth.json` and restores the one for the target's provider. When nothing is
kept for the target, the checkout is left unbound (run `modernpath init`) or
signed out (run the sign-in command the switch names); the CLI never prompts.
A restored credential the server will not accept is reported at the switch.

`modernpath auth` (add `--test` to target the test profile instead of prod)
authenticates through your organization's ZITADEL identity against a
compiled-in issuer and client ID; `--sso` is accepted for compatibility and
does nothing, since this is now the only login. The prod profile targets the
cloud production API at `https://api.modernpath.ai`; the test profile targets
`https://api.workload.test-plat.modernpath.ai`. Both are the API hosts, not
the `cloud.modernpath.ai` single-page-app host (which answers every path with
`index.html`); a successful login writes the profile's API URL into the
binding.

### Import a codebase

`modernpath import` creates a new system from the codebase in the current
directory, for a repository on a network the platform cannot reach:

```bash
modernpath import                    # summary, confirmation, upload
modernpath import --yes              # unattended, for CI
modernpath import --name "My App" --exclude fixtures --keep packages
```

The CLI packs the directory into a zip with the upload filters (below), shows a
summary and asks for a confirmation, then uploads the zip. The server creates
the system and its repository, stores the source and queues the analysis. The
binding, with the repository id, is saved to `.modernpath/config.json`.
`--yes` (`-y`) skips the confirmation; without a terminal `--yes` is required,
and the command exits non-zero with `--yes is required for unattended runs`
instead of exiting 0 without importing. Declining the confirmation exits 0.

The CLI imports the local directory as an upload.

After the upload the next steps say that the analysis is queued and name the
verbs that follow it (`modernpath analysis status`) and keep the system
current (`modernpath source push`). The View in UI link opens the system on
the app host the server names in its capabilities read; a server that names
none gets a link on the API host, marked as such.

### Keep an uploaded system current

A system created with `modernpath import --local` holds a copy of the tree the
CLI packed — the path for a repository on a network the platform cannot reach.
New commits reach the knowledge core with `modernpath source push`, run from
the same checkout by hand or from your own CI:

```bash
modernpath source push
modernpath source push --exclude fixtures --max-size 200
```

The verb packs the directory with the import filters (skip lists,
`.gitignore`, `--exclude`, `--keep`, `--max-size`; see below) and always sends
the selected archive. The server ingests it, computes the source revision and
reports whether the source changed. The checkout's git head, branch and dirty
flag travel as metadata and show on the system's Define page beside the
revision and time it was received. The CLI does not hash the local tree or
compare a local digest with the server before uploading.

The verb never prompts. A server refusal — the system is being analysed or
its documentation updated, or the repository is linked to a git provider
the platform refreshes itself — exits non-zero with the server's error code
and message, so a CI job fails loudly. A busy refusal names the run and the
time it started. A changed tree keeps the system busy from the push until
the run it started has made the tree current and refreshed it, so a second
push in that window is refused too. Cancelling the analysis or update in the
UI ends the run at once; a pushed tree the run had not made current yet is
dropped, and the previous source stays current.
`import --local` records the repository the push targets in the working
directory's `.modernpath/config.json`; a workspace whose config predates that
resolves the system's single upload repository through the API.

#### What the upload leaves out

`import --local` and `source push` pack the tree with the same filters and
print the same scan report: the files kept and their size, then what each
filter left out with its file count and bytes. The filters are:

- the built-in skip lists: directories such as `.git`, `node_modules`,
  `packages`, `vendor`, `bin`, `obj`, `build` and `dist` at any depth, and
  binary, image, media, font, archive and office files by extension (`*.svg`,
  `*.dll`, …), lock files and `.env`. The report lists each directory name and
  extension that dropped files;
- the files `.gitignore` excludes (`--no-gitignore` keeps them);
- `--exclude <pattern>` (repeatable). A pattern without `/` matches a file or
  directory name at any depth: `--exclude fixtures` leaves out
  `src/a/fixtures/x.cs` but not `fixtures2/`, and `--exclude '*.min.js'`
  matches by file name. A pattern with `/` matches the path from the top of
  the tree: `*` stays within one directory and `**` spans any depth, so
  `'*/fixtures/*'` matches `a/fixtures/x` but not `a/b/fixtures/x`, and
  `'**/fixtures/**'` matches at every depth. A directory that matches is left
  out with everything under it.

`--keep <name>` (repeatable) uploads a directory the built-in list skips for
this run, for example `--keep packages` in a JavaScript monorepo whose code
lives under `packages/`; the report names it under `Kept (--keep)`. A name
that is not on the list is refused, and so are `.modernpath` (it holds the
sign-in credential) and the version control directories. Pass the same
`--exclude` and `--keep` to `source push` as to the import, or the pushed tree
differs from the imported one.

#### Upload size

`import --local` and `source push` upload the tree as one zip. `--max-size`
(MiB, default 100) bounds the compressed zip, not the files before
compression. Before sending, both commands read the server's upload limit
(`GET /api/import/capabilities`, 90,000,000 bytes by default) and compare the
whole request body with it. A body that does not fit is not sent: the command
prints the zip size, the limit, the ten largest top-level directories by
compressed size and an `--exclude` example, and exits non-zero. A server that
answers 404 to the read predates it and accepts 50,000,000 bytes; when the
read fails the CLI assumes 90,000,000 bytes. Either way it prints the limit it
used. A `413` from the server prints the same guidance once and exits
non-zero.

### Run and follow the analysis

`modernpath analysis` drives the analysis of the bound system through the
same lifecycle the UI uses:

```bash
modernpath analysis start                      # or --mode unified_workspace
modernpath analysis status
modernpath analysis reanalyze 7                # one repository, by id
modernpath analysis reset --yes                # delete the results and start again
```

`start` starts the analysis of the system's repositories. `--mode` is
`independent_repos` (each repository on its own, the default) or
`unified_workspace` (the repositories as one workspace). When an analysis is
already running, its run is printed and no second one starts. `reanalyze`
re-runs one repository; `analysis status` prints its id. `reset` cancels a
running analysis and deletes the system's analysis results (file analyses and
dependencies, subsystems, capabilities, patterns, findings, libraries and
generated documentation), keeps the repositories, their uploaded source and
planning data, and starts a new analysis. It asks first; `--yes` skips the
question, and without a terminal `--yes` is required. Each verb prints the run
the server answers with, or the server's refusal once by its error code and
message (`analysis_already_complete`, `sources_required`,
`source_materials_not_ready`, …) and exits non-zero.

`analysis status` prints the bound repository's analysis, documentation and
embedding status, its current source revision and how the last refresh of that
source ended, then the current analysis run: its status, mode, phase and
steps counted by stage. Right after an import there is often no recorded run;
status then prints `no analysis run recorded` and exits 0.

`modernpath docs generate` sends the same start request as `analysis start`
(with `--mode`) after showing estimates, and creates no repository.
`docs generate` and `docs refresh` ask for a confirmation; `--yes` skips it,
and without a terminal they exit non-zero with `--yes is required for
unattended runs` instead of printing "cancelled" and exiting 0. `docs refresh`
queues the update and prints its job id; the update runs in the background, so
follow it with `analysis status` and run `docs sync` once documentation reads
completed.

`docs refresh`, `docs preview` and `analysis status` act on the
repository `.modernpath/config.json` records (`repository_id`, written by
`import --local`), else the system's single upload repository, else the
repository whose local path is the current directory. When none resolves they
exit non-zero and name the `repository_id` remedy.

### Explore the Knowledge Core

When the local document export is missing or stale, refresh it with
`modernpath docs sync`. Then search and read the exported files locally:

```sh
modernpath docs sync
rg -n "authentication" .modernpath/<system-slug>
cat .modernpath/<system-slug>/architecture/overview.md
```

For API-backed discovery or a synthesized answer, use `modernpath search` or
`modernpath ask`. `modernpath read-doc --list` accepts `--tier` and `--angle`
to narrow an API-backed document list. `search`, `read-file`, `read-doc`,
`status`, `source`, `analysis`, and `system-docs` support JSON output with
`--json`. For `ask`, `--json` is an alias for `--format=json`.

`modernpath ask` runs the question in a background job on the server. An
answer ready within 45 seconds comes back at once; otherwise `ask` waits for
it, showing `Still working (m:ss)…` on stderr in pretty format, for up to 10
minutes (`MODERNPATH_ASK_WAIT_LIMIT`, a Go duration such as `20m`, changes the
limit). A failed ask, or one still running at the limit, prints its reason and
its ask id and exits non-zero. The job answers within 5 minutes. It starts
from matching code files as well as documents, and can also search curated
patterns and capabilities. Sources are listed once by title or file path; the
server names each source's kind and id. `--format=json` (or `--json`) emits
the answer and source references for scripts.

### CLI output and query contract

CLI commands serve people and coding agents with the same
selected data. Discovery is bounded and terse; complete records are explicit.

The common rule for resource commands is: `list` shows only the resource's
usable ID or display reference and title/name, one item per line, in a bounded
page. Use that identity with `get` for a short summary, `get --fields
title,description` to fetch selected fields, or `get --full` to fetch all public
fields. Large content is returned only when explicitly selected or exported.
`--json` preserves the same selection and includes list pagination metadata.
The command-specific details below define identity fields and any exceptions.

- `epics list` prints the native numeric ID and title, one row per Epic.
  `requirements list` prints the display reference and title, one row per
  requirement. Titles are kept on one terminal line.
- Both lists default to 20 items (maximum 200). Filtering, ordering and
  continuation run on the server before the page is returned. `--order id`
  is the stable default; `--order title` orders by title with native identity
  breaking ties. `--cursor` continues the same System, filters and order.
  A mismatched or malformed cursor is an error; pages are live reads, not
  snapshots.
- Epic filters include `--status` and `--query` (literal, case-insensitive
  text in title, description or scope). Requirement filters include
  `--status`, `--kind`, `--context` and `--query`. Requirement statuses are
  `PROPOSED`, `IN_PROGRESS`, `IN_REVIEW`, `DONE`, `BLOCKED`, `DEFERRED` and
  `OBSOLETE`.
- `get` defaults to a short identity/title/status summary. `--fields` selects
  comma-separated public fields, including larger content such as description
  or scope. `--full` requests all public detail. Unknown or empty field names
  and combining `--fields` with `--full` are errors.
- `--json` emits the same selected data as a JSON object. Lists emit
  `{"items":[...],"next_cursor":null}`; a continuation replaces null with an
  opaque cursor. Epic list items contain `id` and `title`. Requirement list
  items contain `id` (native UUID), `requirement_kind`, `display_ref` and `title`.
  Requirement detail always retains those identity fields (`id`,
  `requirement_kind`, `display_ref`), including when `--fields` selects content.
- Text lists print a continuation hint to stderr when another page exists.
  Progress and warnings also go to stderr. Errors are reported once and exit
  nonzero. Read commands never prompt. Write commands return a short receipt
  identifying the affected resource and result; explicit content reads and
  specification exports may return complete content.

#### Requirement references and native identity

A requirement display reference is `UR-<display_number>-<revision>` for a
user requirement or `SR-<display_number>-<revision>` for a system requirement.
For example, `UR-6-0` identifies user requirement number 6 at revision 0.
Its native UUID is immutable; the display reference changes with the revision.
All lookups are scoped to the authenticated workspace and bound System.

`requirements get UR-6-0` infers the kind from the prefix. A conflicting
`--kind` is an error. A revision-qualified reference must match the current
revision exactly: a stale reference returns an actionable error rather than
silently returning newer content. This command does not retrieve historical
accepted content. UUID lookup remains available and requires `--kind user`
or `--kind system`. The native `user_requirement` and `system_requirement`
values printed by JSON lists are also accepted.

```sh
modernpath epics list
modernpath epics list --status IN_PROGRESS --query cart --limit 20
modernpath epics list --order title --json
modernpath epics get 7 --fields title,scope
modernpath epics get 7 --fields title,scope --json
modernpath epics get 7 --full --json
modernpath requirements list --kind user --query checkout
modernpath requirements get UR-6-0 --fields title,description
modernpath requirements get <uuid> --kind user --fields title,description
```

### Create native requirements

Create a requirement for new behavior in the bound System:

```sh
modernpath requirements create --kind user --file user-requirement.json --json
modernpath requirements create --kind system --file - --json < system-requirement.json
```

The JSON object requires `title`. Optional common content fields are
`description`, `context`, `context_name`, `priority`, `release_note` and
`detail_md`. User requirements also accept `actor` and `intended_use`; system
requirements accept `boundary`, `rationale` and `verification_method`.

For example, `system-requirement.json` can contain:

```json
{"title":"Rotate signing keys","description":"Keys can be rotated without interrupting requests","verification_method":"integration test"}
```

The CLI supplies the bound `system_id`. The server assigns the native UUID,
display reference, revision `0`, `PROPOSED` status and authenticated author,
and records the creation audit atomically. Client identity, lifecycle,
release assignment and approval fields are rejected.

The receipt includes `id`, `requirement_kind`, `display_ref`, `title`,
`system_id`, `work_status` and `revision`. Use its native kind and UUID in
`epics plan save`; propose acceptance criteria and further content there.
Creating a requirement does not approve implementation or completion.

### Find synced requirements

Use `requirements list` to read compact requirement summaries from the
bound system:

```sh
modernpath requirements list
modernpath requirements list --status IN_REVIEW --kind user --context AUTH --query "two factor" --limit 25
modernpath requirements list --status IN_REVIEW --kind user --context AUTH --query "two factor" --limit 25 --cursor "<next_cursor>" --json
```

The command returns one page, 20 rows by default and at most 200. Status means
the exact `work_status`; kind is `system` or `user`; context is the exact
context code. These filters combine, while `--query` is a case-insensitive
literal substring search across the native UUID, display reference, title and
description.
Null status and context values remain eligible when those filters are absent.

Each result contains the display reference and title; JSON additionally retains
the native UUID and requirement kind. Pages are live reads, so concurrent
changes may affect a later page. Continue with the returned cursor
and the same filters; the cursor does not provide a snapshot. `--json` prints
`items` and `next_cursor` for scripts. The command returns compact summaries
only. A server that does not support discovery returns an explicit error; the
CLI does not fall back to downloading the full corpus.

### Find requirements and epics by name or meaning

Use `requirements search` and `epics search` when you know what you are looking
for but not its id (REQ-PLN-192):

```sh
modernpath requirements search "signing keys rotate"
modernpath requirements search UR-5
modernpath requirements search "audit trail" --limit 25 --json
modernpath epics search "two-way chat integration" --json
```

Both run the same server search as MCP's `search_requirements` and
`search_epics`: an exact match on id, title and description, and a match by
meaning on the stored embedding, in one ranked list for the bound system. Hits
found both ways come first, then exact hits by recency, then meaning hits by
similarity. Default hits show their identity and title on one line. Use `--fields` to select
content or `--full` to request the complete hit.

`requirements search` covers user requirements, system requirements and test
cases. It matches display references (`UR-5`, `SR-12-0`, `TC-8`) and stored content;
results retain native identities alongside display references.
`OBSOLETE` requirement rows are never returned. Unconfirmed test cases are
found by exact match only and are marked `candidate`. `epics search`
shows the native Epic ID as `#<id>` and says `keyword`,
`semantic` or both. The complete Epic hit includes its app link; select `url` or use `--full`
to include it. Links use the server-advertised app address, falling back to the
API host when none is available.

`--limit` is 10 by default and at most 25. These are bounded ranked searches,
not paged inventories; use list commands for continuation. `--json` prints
`query` and the selected `items`. A 404 names its cause: an unknown system (check the binding with
`modernpath status`) or a server without the search routes (update the server).
Use `requirements list` for exact status filters and paging.

### Read Epics and exchange specifications

Epics use native IDs. Read, create and update them under the bound System, and exchange
specifications with the dedicated `epics specs` commands:

```sh
modernpath epics list
modernpath epics get <epic-id>
modernpath epics create --file epic.json
modernpath epics update <epic-id> --file changes.json
modernpath epics specs list <epic-id> --full --json
modernpath epics specs status <epic-id>
modernpath epics specs generate <epic-id>
modernpath epics specs pull <epic-id> --file bundle.json
modernpath epics specs push <epic-id> --file bundle.json
```

`epics create` reads an Epic JSON object from `--file` or `--file -`; it
returns a short identity receipt without selecting the Epic as active work. `epics update`
accepts a partial JSON object with `title`, `description`, `scope` or `non_goals`,
using `--file` or `--file -`; only supplied fields change. Epic content is
read-only in `IN_PROGRESS` and `IN_REVIEW`; the server refuses these edits.
`specs status` reports the artifact count and counts by stored status;
`--fields artifacts` selects the artifact metadata.
`specs list` summarizes the specification count; `--fields specs` or `--full`
includes content. `specs pull` writes the server's specification bundle as JSON. `specs push` accepts that
bundle or an object shaped as `{"specs":[{"category":"architecture",
"filename":"overview.md","content":"# Overview"}]}`. Partial import
failures return a nonzero status.

Specification generation captures the Epic’s Knowledge Core snapshot when none
exists and preserves an existing snapshot. It schedules work only after that
preparation succeeds.

### Read and approve a native Epic plan

Plans and their decisions live in the server process store:

```sh
modernpath epics plan show <epic-id> --full --json
modernpath epics plan save <epic-id> --reason "<reason>" < plan.json
modernpath epics plan approve-implementation <epic-id> --reason "<reason>" < reviewed-plan.json
modernpath epics plan submit-review <epic-id> --reason "<reason>"
modernpath epics plan approve-completion <epic-id> --reason "<reason>"
```

`show` summarizes the current plan state; `show --full --json` reads the saved
plan, human decisions and accepted requirement versions in full.
`save` and `approve-implementation` read one JSON plan from standard input
or `--file plan.json`; implementation approval applies to that saved plan.
Writes accept an optional `--reason` comment. `submit-review` submits the implementation for human completion
review. `approve-completion` records the human completion decision and
publishes the approved requirement content. The approved plan and owned
requirement content remain read-only through `IN_PROGRESS` and `IN_REVIEW`.
Capture a requested scope change separately; it does not revise the active
approval. The server remains authoritative.

### Read requirements, test cases, traces and evidence

Read a requirement by display reference, or by native UUID and kind. Use
`--fields` or `--full` to include stored criteria and trace detail:

```sh
modernpath requirements get UR-6-0 --fields title,description
modernpath requirements get <uuid> --kind user --full
modernpath requirements get <uuid> --kind system --fields title,criteria
modernpath traces list --entity-type user_requirement --entity-id <uuid>
modernpath test-cases list
modernpath test-cases get <uuid>
```

Trace entity types use the stored names: `user_requirement`,
`system_requirement`, `test_case`, `system_document`, `source_file`,
`file_analysis`, `subsystem`, `epic`, or `criterion`. Trace reads are filtered
to the System bound to the checkout. Requirement and test-case detail reads
also verify the native entity belongs to that System.

Test cases are procedure records with retained title, description,
preconditions and ordered steps. Create or update that metadata with JSON:

```sh
modernpath test-cases create --file test-case.json
modernpath test-cases update <uuid> --file test-case-update.json
```

The CLI supplies the bound `system_id`; JSON may contain `title`,
`description`, `preconditions`, and `steps`. Each step needs `number` and
`description`; `expected_result` can describe the expected observation. The
CLI does not execute a TestCase. Record actual test or other verification
observations as evidence:

```sh
modernpath evidence record --file evidence.json
modernpath evidence latest
modernpath evidence runs --kind local_test
```

Evidence input includes a run `kind`, `ran_at`, a `runner` attribution, and
per-target `results`; each result uses the native target type and ID, outcome,
and optional clause, role, revision and detail. The bound System ID is added
by the CLI. `latest` is the derived current evidence view; `runs` reads the
recorded run summaries. Result rows are fetched only when explicitly selected
with `--full`. Use `--json` for the same selected data
as JSON; text is the default.

### Other lists, content reads and operational output

The same terse output contract applies across the command surface:

| Commands | Discovery and detail |
|---|---|
| `test-cases list`, `get` | Lists show `TC-<number>-<revision>` and title. JSON also retains native UUID. `get` and `update` accept either exact display reference or UUID; stale revisions fail. Lists filter with `--query`, `--status draft|approved`, and `--order id|title`. |
| `traces list` | Requires an entity type and ID; supports relation filtering and server continuation. Each row identifies the trace and its endpoints. |
| `evidence latest`, `runs` | Bounded server queries support target, outcome and time filters. `runs` also supports kind. `latest` filters the newest observation per target, so filtering cannot resurrect an older pass. |
| `read-doc` without `--id` | Paged metadata discovery with query, tier, angle and ordering. Select content through a direct ID read. |
| `system-docs list`, `get` | Metadata lists support query, type, order and continuation. `get` summarizes by default; select content with `--fields` or `--full`. |
| `search`, `requirements search`, `epics search` | Bounded ranked hits, compact by default, with `--fields`, `--full` and `--json`. |

Paged inventories default to 20 items, maximum 200, with `--limit` and
`--cursor`. JSON uses `{"items":[...],"next_cursor":null}`. Cursors are opaque
and bound to the query scope; continue with the same filters and order.
`--help` lists each command's supported filters and selectable fields.

`status`, `auth status`, `env` and `env test` return concise summaries and
support `--fields`, `--full` and `--json`. `auth status` inspects local session
state without starting login. `env test` uses an explicit `--api-url` when
provided. A missing workspace or failed connection returns nonzero.

Docs operations, import, scan, hook setup, install, focus, initialization and
project creation return structured results. Their size varies: for example, `scan
--json` includes findings, `docs preview --json` returns the preview data, and
`hooks doctor --json` includes diagnostic detail. Progress and warnings remain
on stderr; stdout contains the result. Receipt `url` fields contain only URLs; notes about
a missing advertised app address go to stderr. JSON commands do not enter interactive
prompts: provide required selectors and explicit `--yes` for operations that
require confirmation. `init --json` needs an explicit System selector unless
already bound; `new --json` needs explicit creation inputs and `--yes`.

Full file and document reads, `ask` answers and explicit exports such as
`system-docs pull` or specification pull intentionally return content.
`dev setup codex` currently requires manual MCP configuration and returns an
actionable error; it does not claim to have written a config.
`dev run` forwards child-process output and rejects `--json`; its child owns
the output format. Read commands never ask interactive questions.

`system-docs pull` preserves the server's document folder paths, so source
documents with the same basename remain separate. Receipts name the actual
written file, including `.content.txt` when only extracted content is available.
Encrypted-original failures remain errors.

A completed data-model scan can legitimately report zero domains when the
repository has no schema entities. That empty model is persisted and exportable.
If no scan artifact exists yet, use `analysis start --wait`, or
`analysis reanalyze <repository-id> --wait` for an already analyzed repository.
`analysis status` identifies the bound repository.

### Diagnose a connection

Start with the active environment, binding and authentication status:

```sh
modernpath env
modernpath status
modernpath auth status
modernpath hooks doctor
```

Common repairs:

| Symptom | Repair |
|---|---|
| Authentication is absent or the server rejects the session | Run `modernpath auth` against the intended server. |
| Knowledge Core content is old | Run `modernpath docs sync`. |
| The repository is not bound to the intended system | Run `modernpath init` and select the system. |
| Hooks use an unexpected or stale binary | Run `modernpath hooks doctor`, update the binary it identifies, and reinstall hooks if directed. |

Use `modernpath <command> --help` for current flags and subcommands.

## Technical Description

### Source authority

Paths in this section are relative to `modernpath-core`.

| Concern | Authoritative source |
|---|---|
| Command registration and global flags | `CODE:tools/modernpath/cmd/root.go:rootCmd` and each command file's `init` function |
| Authentication and credential writes | `CODE:tools/modernpath/cmd/auth.go:runAuth`, `CODE:tools/modernpath/internal/config/config.go:WriteAuth` |
| ZITADEL login and device flow | `CODE:tools/modernpath/internal/zitadel/login.go:Login`, `CODE:tools/modernpath/internal/zitadel/authcode.go:AuthCodeLogin`, `CODE:tools/modernpath/internal/zitadel/deviceflow.go:DeviceLogin` |
| Repository initialization and Knowledge Core export | `CODE:tools/modernpath/cmd/init.go:runInit`, `CODE:tools/modernpath/cmd/init.go:finalizeSystemInit` |
| Binding discovery and persistence | `CODE:tools/modernpath/internal/config/config.go:FindConfigDir`, `CODE:tools/modernpath/internal/config/config.go:WriteConfig` |
| Knowledge Core synchronization | `CODE:tools/modernpath/cmd/docs.go:runDocsSync` |
| Source upload | `CODE:tools/modernpath/cmd/source.go` |
| Requirements and native procedure/evidence commands | `CODE:tools/modernpath/cmd/requirements_list.go`, `CODE:tools/modernpath/cmd/requirements_search.go`, `CODE:tools/modernpath/cmd/compliance_native.go` |
| Epic records, specifications and saved plan commands | `CODE:tools/modernpath/cmd/epics.go`, `CODE:tools/modernpath/cmd/epic_records.go`, `CODE:tools/modernpath/cmd/epic_plan.go` |
| Agent hooks | `CODE:tools/modernpath/cmd/hooks.go:runHooksInstall`, `CODE:tools/modernpath/cmd/hooks_doctor.go:runHooksDoctor` |
| Development review | `CODE:tools/modernpath/cmd/review.go` |
| Installers and release archives | `CODE:tools/modernpath/scripts/install.sh`, `CODE:tools/modernpath/scripts/install.ps1`, `CODE:tools/modernpath/scripts/build-all.sh` |

Generated `.modernpath` documents are caches, not evidence for CLI behavior
when they disagree with the sources above.

### Connection model

The CLI and browser UI are separate clients of the same server. A repository
binding records the API URL and system ID; authentication supplies the
workspace identity. The server verifies that the selected system belongs to
the authenticated workspace.

### Authentication and credentials

`modernpath auth` resolves the server in this order: global `--api-url`,
`auth --local`, `auth --test`, the current repository binding, then production
(`https://api.modernpath.ai`). Browser sign-in uses OAuth authorization code
with PKCE. When a browser cannot reach the machine, the CLI uses the device
flow; `modernpath auth --device-flow` forces it.

`modernpath auth status` reports the stored credential, workspace, server and
bound system, expiry, and (unless `--offline` is used) whether the server still
accepts it. `auth.json` is stored under `.modernpath/` with mode `0600`; the CLI
protects it through the repository's `.modernpath/.gitignore`. Use this command
to check authentication rather than reading the credential file directly.

### Repository-local state

The nearest `.modernpath/` directory at or above the working directory is the
active binding. It contains the server and system binding, workspace
credential, and downloaded Knowledge Core export. The export is a cache that
can be refreshed with `modernpath docs sync`. The generated
`.modernpath/cli-reference.md` is the command reference for the installed
binary and can be refreshed with `modernpath install`.

### Environment behavior

The named environments are URL aliases:

| Name | URL |
|---|---|
| `production` | `https://api.modernpath.ai` (cloud, default) |
| `test` | `https://api.workload.test-plat.modernpath.ai` (cloud test-plat) |
| `local` | `http://localhost:4000` |
| `custom` | User-supplied URL |

`modernpath env --set` switches the API binding and restores a credential
associated with the target identity provider when one is available. A custom
URL without a known provider keeps the active credential and reports that fact.

### Initialization and binding

`modernpath init` checks server health, lists systems visible to the signed-in
workspace, resolves a selected system, downloads its Knowledge Core export and
writes the local binding and ignore rules. Re-running `init` deliberately
selects and binds the current directory to a system.

### Knowledge Core synchronization

`modernpath docs sync` downloads and validates the latest export for the bound
system, replaces the local export and updates its server-generated sync time.
`modernpath docs push` uploads edited documentation. Both commands operate on
document exports.

The export normally lives under `.modernpath/<system-slug>/`; reserved names
use `.modernpath/docs/<system-slug>/`. After syncing, use `rg` and `cat` to
read exported documents locally. `modernpath search` and `modernpath ask` remain
available for API-backed discovery.

### Coding-agent and MCP setup

`modernpath dev setup <tool>` configures ModernPath's MCP endpoint for OpenCode,
Cursor, Claude Code, or Codex. The registered setup path is implemented by
`CODE:tools/modernpath/cmd/dev.go:runDevSetup`; tool-specific writers live in
the same file under `setupOpenCodeMCP`, `setupCursorMCP`, `setupClaudeMCP`, and
`setupGenericMCP`.

The current writers put `<api_url>/api/mcp` into the client configuration.
Phoenix separately exposes REST tool execution under `/api/mcp/tools*` and MCP
transport under `/mcp/sse` plus `/mcp/message`. Treat this as a known contract
area and verify an actual client connection before changing or documenting a
different endpoint. The endpoint inventory is grounded in the monorepo source
`modernpath-core/docs/mcp-and-slack.md`, section “CLI integration”.

### Command surface

Run each command's `--help` for the current flags and registered children.

| Area | Commands |
|---|---|
| Connection | `auth`, `init`, `env`, `status` |
| Current work visibility | `focus` |
| Knowledge | `search`, `ask`, `context`, `read-doc`, `read-file`, `docs`, `system-docs`, `datamodel` |
| Source and analysis | `import`, `source`, `analysis`, `scan`, `github` |
| Native requirements | `requirements create`, `requirements list`, `requirements search`, `requirements get` |
| Epic records and specifications | `epics list`, `epics get`, `epics create`, `epics update`, `epics search`, `epics specs list/status/generate/pull/push` |
| Requirement traces and test procedure records | `traces list`, `test-cases list/get/create/update` |
| Evidence observations | `evidence record/latest/runs` |
| Reverse engineering | `reverse-engineer record/review/accept` |
| Agent integration | `install`, `hooks` |
| Development | `dev run`, `dev setup`, `dev list`, `dev review` |

`dev review` inspects the current Git diff.

`modernpath reverse-engineer record --file <JSON|->` sends native requirements,
and their traces to the bound system in one request.
The JSON object contains `requirements` and `traces` arrays. A trace with a
`source_file` endpoint supplies `repository_key`, `path`, and optional `revision`
instead of a file endpoint ID. The server resolves the file internally. New
requirements are recorded as `PROPOSED`. The command reads the supplied JSON and
posts it directly; it does not inspect a repository checkout or require a run,
content digest or proof report. Implementation: `CODE:tools/modernpath/cmd/reverse_engineer.go`.

Select recorded requirements by native kind and UUID to review their current
content, traces, criteria and evidence:

```sh
modernpath reverse-engineer review --file selection.json --json > review.json
modernpath reverse-engineer review --file - --json < selection.json
```

The selection object contains only a `requirements` array, with each item
`{"kind":"user_requirement|system_requirement","id":"<native-uuid>"}`.
Review sends a read-only preview request to the bound System and prints the full
review bundle. An ineligible review still returns its problems for inspection.
Evidence is read from the existing `evidence` record, latest and runs commands;
`evidence runs --full` returns criterion results and run provenance, including
test-case reference, role, outcome, validity, tested revision, detail,
environment, raw evidence, run source revision and report reference. Acceptance
requires current complete-run criterion results, each PASS and valid, not RED,
with a test-case reference and detail that identifies the command or report and
environment. Use `evidence record` to preserve those observations and
`evidence runs --full` to read them back.

To accept, wrap the exact JSON review bundle from `review.json` with the
human's source and reason. Do not construct or edit the review object; the
server rechecks that exact bundle:

```sh
jq '{review: ., source: "USER:2026-10-19", reason: "Observed behavior and evidence are accepted"}' review.json > acceptance.json
```

```sh
modernpath reverse-engineer accept --file acceptance.json --json
```

The command requires non-empty `source` and `reason`, then sends the original
review bundle to the server for a complete recheck and atomic publication as
`DONE`. It does not prompt for approval. The receipt includes each accepted
requirement's resulting revision, accepted version, actor, time, reason, and
whether the request accepted it now or returned an existing acceptance.

### Build, test, and release

Contributor commands:

| Task | Command | Source |
|---|---|---|
| Build local binary | `cd tools/modernpath && go build -o modernpath .` | `CODE:tools/modernpath/go.mod` |
| Run the CLI suite | `cd tools/modernpath && go test ./...` | `CODE:Dockerfile` (`go-builder`) |
| Build release archives | `cd tools/modernpath && bash scripts/build-all.sh [version]` | `CODE:tools/modernpath/scripts/build-all.sh` |
| Install the working tree for local testing | `cd tools/modernpath && bash scripts/install-local.sh` | `CODE:tools/modernpath/scripts/install-local.sh` |
| Refresh embedded process assets | `tools/modernpath/scripts/sync-rdd-assets.sh /path/to/req-driven-dev` | `CODE:tools/modernpath/scripts/sync-rdd-assets.sh` |

The release build targets Darwin amd64/arm64, Linux amd64/arm64, and Windows
amd64 when the required archive tools are available.
