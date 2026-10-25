# ModernPath CLI

This guide covers installation, connection and use of the `modernpath`
command-line interface. Run `modernpath <command> --help` for the options
supported by your installed version. `modernpath install` refreshes the local
command reference at `.modernpath/cli-reference.md`.

## Delivery release activation

From a workspace bound to the intended System:

```sh
modernpath releases list
modernpath releases pin set
modernpath releases activate delivery-1
```

PIN setup prompts twice without echo (4–6 digits). Input is hidden before
either prompt appears; Ctrl-C cancels and restores terminal settings. Activation prompts once;
`--pin-stdin` accepts a PIN from a secret store or protected file for either
command. Do not put a PIN on the command line. `releases pin status` reports
whether a PIN exists.

If another delivery release is active, activation refuses unless
`--close-current` explicitly consents to closing it. Consent is bound to the
incumbent read by the command; a concurrent change requires a new attempt.
An incumbent with unfinished work must first be closed using the release close
flow. `--reason` adds an optional audit reason; `--json` emits the scoped
activation receipt. IDs or exact slugs select the target. If a numeric slug
matches another release’s ID, selection refuses before reading the PIN; use
`--by id` or `--by slug` to disambiguate explicitly. Base cannot be
activated, creation remains planned, and repeating activation is idempotent
but still requires the PIN.

`factory release activate` and `factory pin set/status` are compatibility
entry points for these native operations. A compatible server must expose
`POST /api/compliance/releases/:id/activate`; UI and CLI share this endpoint.

## Move requirements between releases

Move an exact selection of user and system requirements through the supported
CLI. Sources may have no release, be in Base or be in an open delivery release.
The destination must be a planned or active non-Base release in the bound System. Closed,
release-exempt, deleted or foreign requirements refuse the entire request.

Create `selection.json` with native kinds and UUIDs:

```json
{"requirements":[
  {"kind":"system_requirement","id":"0951fc74-235d-4041-98ea-0dfa2a870e7d"},
  {"kind":"user_requirement","id":"d04e4a91-9585-43b0-811a-f90de731ae8d"}
]}
```

Preview, review the exact target/selection/source memberships, then apply the
saved preview without editing it:

```sh
modernpath releases move-requirements delivery-1 --file selection.json --json > preview.json
modernpath releases move-requirements delivery-1 --file preview.json --apply --json
modernpath releases members delivery-1 --all --json
```

The default preview is read-only. Each requirement row includes its title and
`work_status`, so review these alongside its UUID and source before applying.
New requirements recorded through reverse engineering start in their System's
Base release; moving them to a delivery release includes them in its normal
release notes and close planning, subject to their status. Preview JSON includes a reusable `apply` object
with the System, target ID and expected source per item, explicitly null for
records with no release. `--apply` requires the full preview output; a raw selection
cannot apply. `--file -` reads stdin. Use `--by id` or `--by slug` when a numeric
slug is ambiguous. Each request accepts 1–2,000 distinct typed requirements
in one atomic transaction with audit.

A refused requirement is identified by kind and UUID, with a reason, without
revealing content from another System or workspace. No selected rows move.
A changed source returns `requirement_source_changed`: preview again and review
the new sources. A now-closed target refuses. Already-target eligible rows are
unchanged. Only selected membership changes; Epics, linked requirements and
Roadmap records are not moved. Content, lifecycle, accepted versions and evidence
freshness stay intact; audit owns the movement time. This preservation applies
to `releases move-requirements`. Older Epic/member moves, release-close carry-forward
and older maintenance operations still update content timestamps and
can make reverse-engineering evidence require verification again.

Mutation requests are never retried or split automatically. After a network
failure or invalid receipt, inspect membership before retrying the reviewed
request. An unchanged retry creates no mutation event; a later real move records
a fresh event. Compatible server membership endpoints are required.

`releases members` reads up to 100 requirements; `--all` reads every page. Output
includes typed UUIDs, `total_count`, `complete` and `next_offset`. Pages are
separate snapshots: quiesce concurrent changes for an exact inventory, compare
complete typed-ID sets rather than totals alone, and retry if totals change or
a page is incomplete. This reads requirement membership, not Epic membership
or deployment status.

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

`modernpath prepare` reports the signed-in actor, bound System access, CLI
version, process-kit drift, local documentation export, live document edits and
analysis status, then lists useful next commands. `--json` emits the same
bounded report. The
command exits non-zero when authentication, System access, kit compatibility
or the document check needs attention; JSON still contains the report.
It checks the stored credential locally and verifies it with one System read.
It does not automatically refresh an expiring credential. On a released build,
it checks the latest published CLI release through a public GitHub GET bounded
at one second. An unavailable lookup leaves the latest version unknown; local
builds do not compare themselves with releases. A live document edit newer than the
export generation time means `modernpath docs sync` is useful. The check follows
all metadata pages until a newer edit or the last page, with a two-second timeout
per request. A failed request or repeated cursor leaves the check unavailable
and export currency unknown. No observed
newer edit cannot prove that the export is current: documents may have been
deleted or other exported data may have changed. `prepare` does not sign in,
change the System binding, sync, install, upgrade or start analysis. It does
not select an Epic or grant delivery approval.

If a newer release is confirmed, `prepare` lists the platform's upgrade
command before `modernpath install` and `modernpath install --check`, even when
the installed kit matches the running binary. When kit drift exists but the
release lookup is unavailable, it asks you to check the release channel first.
A local build points to the source build script when the checkout is present.
The advice follows the operating system, not the selected server environment;
it does not guess whether Homebrew, Scoop or an install script was used.

Hook installation and removal clean up obsolete ModernPath entries while
preserving project hooks. `modernpath hooks doctor` reports any remaining
obsolete entries; JSON output includes `retired_hooks` for each agent.

Pi uses a generated `.pi/extensions/modernpath.ts` extension. It forwards each
user prompt to the context hook and adds returned context as a hidden message
only when the hook succeeds. Pi loads project extensions after project trust
approval; use `pi --approve` for one run or accept Pi’s trust prompt. Hook
status and doctor report configuration, which alone does not prove execution.

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
Upgrade the CLI before running `modernpath install` in each connected
repository; install writes the running build's embedded kit.
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
commands that follow it (`modernpath analysis status`) and keep the system
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

The command packs the directory with the import filters (skip lists,
`.gitignore`, `--exclude`, `--keep`, `--max-size`; see below) and always sends
the selected archive. The server ingests it, computes the source revision and
reports whether the source changed. The checkout's git head, branch and dirty
flag travel as metadata and show on the system's Define page beside the
revision and time it was received. The CLI does not hash the local tree or
compare a local digest with the server before uploading.

The command never prompts. A server refusal — the system is being analysed or
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
question, and without a terminal `--yes` is required. Each command prints the run
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
`description`, `context`, `context_name`, `release_note` and
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

### Update requirement content or grouping

Any workspace member can edit every content field supported by
`requirements create`, including `context` and `context_name`:

```sh
modernpath requirements update SR-12-0 --file correction.json --json
modernpath requirements update <uuid> --kind user --file correction.json --reason "Clarify the agreed behavior"
```

For example, `{"description":"Clarified behavior"}` changes only the
description. Omitted fields are preserved; optional content fields can be cleared
with `null`. A supplied title must not be blank. Criteria, traces, ownership,
release membership and lifecycle fields cannot be edited with this command.
The server resolves an exact display reference under the mutation locks in one
request; UUIDs require `--kind user` or `--kind system`.

Content edits require unowned `PROPOSED`, `BLOCKED` or `DEFERRED` requirements
with revision 0 and no accepted version. Drafts in planning Epics can be edited;
the changed content makes their plan reviews outdated, requiring a fresh review
before implementation approval. Content edits also make previous acceptance
evidence stale, including edits within the same second.

Approved requirements return a refusal asking the Epic's approver to return it
to planning. Published or `DONE` content requires a follow-up Epic; `OBSOLETE`
content stays closed. The server checks these conditions under the planning,
System and requirement locks.

For grouping-only changes, send `{"context":"Billing"}` or include
`context_name`. Both use the same rules as `set-context`: context is required,
both values must be nonblank strings of at most 255 bytes, and omitted
context_name defaults to context. These edits preserve content timestamps and
evidence, even on unowned `DONE` or `OBSOLETE` requirements. Active Epic-owned
work remains read-only. A request mixing grouping and content fields must meet
the content-edit rules above and commits atomically. If the supplied content
values are unchanged and only grouping values change, content timestamps and
evidence remain current. Explicitly blank context names are rejected by both
commands; only an omitted context name defaults to context.

The Audit Trail records the authenticated actor and changed fields. Optional
`--reason` records why; if supplied it must contain nonblank text of at most
2,000 bytes. Sending unchanged values returns `result: "unchanged"` without
writing an audit entry, updating timestamps or notifying subscribers. Changed
values return `result: "updated"`, with notifications sent after commit.
Use `requirements get` to read back the changed content. This requires a
server serving `PATCH /api/v1/systems/:system_id/requirements/:kind/:id`.

### Obsolete an unpublished requirement

Use the explicit lifecycle operation when an unowned requirement is no longer
needed. The server accepts `PROPOSED`, `BLOCKED` and `DEFERRED` requirements,
records the authenticated actor and required reason in the audit trail, and
rejects requirements owned by active Epic work or requirements with a published
accepted version. Obsoletion preserves the requirement content, criteria and
traces.

```sh
modernpath requirements obsolete <uuid> --kind system --reason "The interface was removed"
```

To retire many requirements at once, for example the PROPOSED records a
reverse-engineering pass replaced, pass a batch file with `--file <path|->`.
The request is all-or-nothing: either every listed requirement is obsoleted
(or reported `already_obsolete`), or nothing changes and the refusal names the
failing item's zero-based index and identity. A batch holds at most 200 items,
each a native UUID listed once. An item's own `reason` overrides the batch
`reason`; one of them is required. `--file` cannot be combined with a
positional id, `--kind` or `--reason`. This requires a server serving
`POST /api/v1/systems/:system_id/requirements/obsolete`.

```sh
cat > obsolete.json <<'JSON'
{"reason": "Replaced by the reverse-engineered set",
 "items": [{"kind": "system", "id": "<uuid>"},
           {"kind": "user", "id": "<uuid>", "reason": "Merged into another requirement"}]}
JSON
modernpath requirements obsolete --file obsolete.json
```

The output prints one line per item in input order with its index, kind, id
and result; `--json` prints the same items as a JSON array. A refusal reads
`requirement obsoletion refused (server 409): requirement_owned — item 1 (system <uuid>) — <message>`.

### Set a requirement's grouping context

`set-context` is a compatibility shortcut to the same update service used by
`requirements update`. `context` and `context_name` are current grouping metadata. They can change on
`DONE` requirements without changing status, revision, criteria, traces or
immutable accepted versions. Existing accepted versions retain the context
values they recorded; newly accepted versions omit grouping metadata. The
current requirement row can therefore have a newer grouping value than an
older accepted snapshot. Active implementation work rejects regrouping.

```sh
modernpath requirements set-context <uuid> --kind system --context snake-game --context-name "Snake game"
modernpath requirements set-context <uuid> --kind system --context shifts
```

When omitted, `--context-name` defaults to the `--context` value. The server
records the actor and old/new grouping values in the requirement audit trail.

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
accepts a partial JSON object using `--file` or `--file -` and forwards its
fields after a JSON type check: the strings `title`, `description`, `scope`,
`non_goals`, `outcome_source`, `start_date`, `target_date`, `severity`,
`category` and `target_profile_id`, the native record IDs `assignee_id` and
`release_id`, the objects `metadata`, `shared_context`, `impact_assessment`
and `knowledge_config`, and `evidence`, an array of objects. The server
decides what it writes: the client-writable fields are `title`,
`description`, `scope`, `non_goals`, `outcome_source`, `start_date`,
`target_date`, `severity`, `category`, `target_profile_id`, `assignee_id`,
`release_id`, `impact_assessment` (object or null) and `shared_context`
(object; the server refuses null); only supplied fields change, and every
accepted edit is audited with the previous and new values (`epics plan
history`). Epic content is read-only in `IN_PROGRESS` and `IN_REVIEW`; the
server refuses these edits. The three planning fields are accepted only while
the Epic is `PROPOSED` (409 otherwise). `impact_assessment` is written whole
and stored with finding ids assigned where missing. Findings contain only
target references, description and proposed action. An empty `items` list or
`null` clears it; unsupported fields or invalid findings make the server refuse
the edit. `shared_context` is the whole
version-1 object, the only accepted shape (the stored default `{}` cannot be
written back); its `references` and `accepted_context_revision` must equal
the stored values (a CLI-created Epic stores `[]` and `0`), so `decisions`,
`assumptions` and `open_questions` are what an edit changes. Each decision and
assumption is an object with a non-empty `statement`, and each open question
an object with a non-empty `question`, for example
`{"decisions":[{"statement":"…"}]}`; other keys in an entry are kept as sent,
and a plain string is refused (`each decision needs a non-empty statement`). A
server-assigned field (`tenant_id`, `system_id`, `process_status`, `plan`,
`metadata`, `evidence`, `knowledge_config`, …) is refused with 422
`unsupported Epic fields`, a body whose `epic` is not a JSON object with 422
`Epic must be a JSON object`, and nothing is written.
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

### Epic gates and human approvals

Plans and their decisions live in the server process store. The four Epic gate
checks are:

```sh
modernpath epics gates check <epic-id> --phase plan-ready --json
modernpath epics gates check <epic-id> --phase implementation --json
modernpath epics gates check <epic-id> --phase completion-review --json
modernpath epics gates check <epic-id> --phase completion --json
```

Run `plan-ready` after recording the independent plan review, `implementation`
before asking for implementation approval, `completion-review` before submitting
implemented work, and `completion` while IN_REVIEW before asking for completion
approval. Each explicit check records its result and blockers in the Audit Trail,
including actor, server time, Git SHA source and known dirty status. The CLI uses
checkout HEAD by default; `--git-sha <full-sha>` supplies an explicit 40- or
64-character SHA with unknown dirty status. Outside Git, SHA is unavailable.
The SHA is caller-supplied trace information; the server does not verify it
against a repository. Both passing and blocked checks are recorded; blocked
checks exit nonzero, and audit failure returns an error. Reading a preview or
review bundle records no check. A passing check grants no human approval.

Read current server facts for a separate review with
`modernpath epics gates review-bundle <epic-id> --phase <phase> --json`.
The response contains Epic context, current `review`, review findings and
resolutions, `readiness`, and source-file identities. Within `review`, `test_cases`
is keyed by TestCase UUID and `evidence` by run UUID. Each execution contains its
registration, run, actual results and finalization once. Registrations name the
criterion/check obligations. Results reference native TestCases; typed Traces
supply criterion mappings. No full presentation is retained inside a review.

```sh
modernpath epics plan show <epic-id> --full --json
modernpath epics plan save <epic-id> --file plan.json --reason "<reason>"
modernpath epics gates review-bundle <epic-id> --json
modernpath epics plan approve-implementation <epic-id> --reason "USER:<source>: approve"
modernpath epics gates candidate <epic-id> --file candidate.json
modernpath epics gates start-execution <epic-id> --file execution.json
modernpath evidence record --file result.json
modernpath epics plan submit-review <epic-id> --reason "<reason>"
modernpath epics gates presentation <epic-id> --json
modernpath epics plan approve-completion <epic-id> --reason "USER:<source>: accept"
modernpath epics plan return-to-implementation <epic-id> --reason "USER:<source>: continue work"
modernpath epics plan return-to-planning <epic-id> --reason "USER:<source>: replan"
modernpath epics plan history <epic-id> --full --json
```

The saved plan has `body`, `requirements`, `traces` and `verification`.
Requirement proposals retain `kind`, native `id`, exact proposed `content` and
ordered `criteria`. Traces retain typed native endpoints and relationship.
`verification` declares the plan `author_context`, decision UUIDs, criterion
obligations `{id, changed, test_case_id, boundary}`, project checks
`{id, name, test_case_id, boundary}`, and boundaries `{name, applicable, reason}`.
A new check ID is assigned on save. A nonapplicable boundary needs a reason.
Source files that do not exist yet use the existing plan-only `planned` trace;
completion requires real scoped code endpoints.

Candidate input is `{"revision":"<tested code revision>"}`. Execution registration
is `{"revision":"<tested code revision>","obligation_ids":["<UUID>"]}`. Register
before running tests. Actual failed assertions establish RED; errors, skips and
inconclusive outcomes do not. Finalize the failed run before registering the
passing run. With several obligations, register the batch, run the tests,
finalize the batch: both `start-execution` and `evidence record` accept
`{"items":[...]}` (at most 50 items) and post to their batch routes. Each batch is
all-or-nothing and a refusal prints `<code> — item <index> (<identity>)`. Items
of one register batch cannot share an obligation or a test case, so never register
RED and GREEN for the same obligation in one batch. Passing evidence must bind to the current candidate identity and
revision; same-SHA redeclaration requires fresh passing evidence.

A separate review records `phase`, `reviewer_context`, all criterion and obligation
IDs, `reason`, `findings` and `scope_implemented`. Implementation review confirms
scope with `scope_implemented: true`. Findings have `severity` and `description`;
the server assigns IDs. Resolutions reference `review_id` and `finding_id` with a
reason. A decision definition stores its question once; answers reference its
existing decision ID and retain answer/source. No review uploads a presentation.
`epics gates declare-decision` and `epics gates decision` also take a batch,
`{"items":[...]}` of the single objects: 1 to 200 distinct items, written in
input order in one call, all or nothing. A batch declare returns
`{"items":[{"index","id","question"}]}`. A refused batch writes nothing and
names the item: `<code> — item <index> (<identity>) — <message>`.

Approval and return requests contain reason and optional actor attribution.
The server checks eligible current database state under its existing locks.
They upload no plan, presentation, hash or token. A preview does not certify that
a previously rendered screen stayed unchanged. Independent review applicability
comes from native changes, existing revisions and server ordering; formatting
alone is not a semantic change. Unknown applicability blocks.

Implementation approval owns the selected requirements and freezes the active
plan. Submission requires current completion-review readiness. Completion
publishes accepted requirement content, criteria and trace meaning atomically,
releases ownership and records approval attribution. Repeating terminal completion
publishes once. Return to implementation uses current authorization/lifecycle and
works when completion readiness fails. Return to planning withdraws approval and
starts a new planning cycle; earlier candidate/execution/review records cannot
satisfy it. Human implementation and completion decisions remain separate.
No command merges or deploys. Read state and audit after an uncertain response.

History exposes surviving decisions and accepted facts. Copied plans, raw output,
executed procedure snapshots and nested historical presentations are removed.
There is no archive or reconstruction from current TestCase content. See the
[model and cutover contract](../../docs/data/minimal-rdd-evidence.md).

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

For a Trace audit, read each in-scope requirement with criteria, then enumerate
its Traces and those touching its criteria. Use `--json` and follow every
returned cursor with `--cursor`; an initial page is not the complete set.
Inspect each source/relation/target tuple and read its connected records with
the supported requirement, document, test-case and evidence commands. Open the
referenced code and assertions. Check endpoint kind and System, relationship
meaning, criterion coverage and the actual test observation. A valid Trace is
not proof that a document supports intent, code implements it or a test passed.
Handle unsupported endpoint reads under the System-name rule in the delivery
loop reference: tooling backlog items only when the bound System name contains
`modernpath` (case-insensitive). Create these reports with
`modernpath backlog create --file <report.json> --focus` so they enter the
signed-in reporter's focus atomically. Preserve other lanes; do not claim a
report was created if its focus failed. Create no missing-capability reports or backlog items
for other Systems. Never query private transport. During planning assess intended targets; during verification and
completion assess executed results and their applicability to the tested revision.

TestCases retain native UUID, tenant/System, title, existing revision, approval
attribution, deletion and server timestamps. Create with `{"title":"Access check"}`;
update with the same title-only shape. The CLI supplies the bound System. Reads
use native UUIDs and title; lists search UUID/title and order by UUID or title.
Procedure, preconditions, runner/selector, authority, embeddings, descriptive body,
status and TC-number references are removed. A title change advances the existing
revision and invalidates earlier applicable proof.

```sh
modernpath test-cases create --file test-case.json
modernpath test-cases update <uuid> --file test-case-update.json
modernpath test-cases get <uuid> --full --json
modernpath evidence runs --target-type criterion --target-id <criterion-uuid> --full --json
```

Native results, executed revisions and typed relationships remain inspectable
through these existing identities. Historical definitions and raw output are not
retained or reconstructed. See [Native verification evidence](#native-verification-evidence).

### Other lists, content reads and operational output

The same terse output contract applies across the command surface:

| Commands | Discovery and detail |
|---|---|
| `test-cases list`, `get` | Native UUID, title and revision. `get` and `update` use UUIDs. Lists search UUID/title with `--query` and sort with `--order id|title`. |
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
writes the local binding and ignore rules. Re-running `init` in a bound
workspace reports the binding `unchanged` and sends nothing, unless
`--system-id` or `--system` names a different system: then it rebinds the
current directory, keeps the previous export folder and reports `rebound` with
`previous_system_id` (and `previous_export` when that folder exists). The
same system named by id, or by name case-insensitively, is `unchanged`.
`--force` replaces the selected system's export folder and the binding in the
current directory.

`init`, `init workspace` and `install` refuse to run in a directory below an
already bound workspace (one under a root that holds `.modernpath/config.json`)
and name that root, so a nested workspace is never created by accident.
`init --force` is the deliberate nested binding. `install` is exempt in a
directory that holds a `.git` file (a linked worktree or submodule root),
whose process files are its own while a linked worktree's binding and
credential are the main checkout's; `init` there is refused without `--force`
as well, and a subdirectory of a linked worktree is refused for both.
`install --check` and `install --dry-run` write nothing and are never
refused.

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

`modernpath dev setup <tool>` configures the connected server's MCP endpoint
for OpenCode, Cursor, Claude Code, or Codex. Run the command's `--help` for
supported options, then verify the connection in your coding agent before
using its tools.

### Command surface

Run each command's `--help` for the current flags and registered children.
`modernpath install` renders the same help, every input file's shape, the
phase-to-command map of the delivery loop and the refusal glossary into
`.modernpath/cli-reference.md`, the reference the installed rdd skills name.

| Area | Commands |
|---|---|
| Connection | `auth`, `init`, `env`, `status` |
| Current work visibility | `focus` |
| Knowledge | `search`, `ask`, `context`, `read-doc`, `read-file`, `docs`, `system-docs`, `datamodel` |
| Source and analysis | `import`, `source`, `analysis`, `scan`, `github` |
| Native requirements | `requirements create`, `requirements update`, `requirements list`, `requirements search`, `requirements get`, `requirements obsolete`, `requirements set-context` |
| Epic records and specifications | `epics list`, `epics get`, `epics create`, `epics update`, `epics search`, `epics specs list/status/generate/pull/push` |
| Epic plans and their history | `epics plan show/save/approve-implementation/submit-review/approve-completion/return-to-implementation/history` |
| Requirement traces and native test records | `traces list`, `test-cases list/get/create/update` |
| Evidence observations | `evidence record/latest/runs` |
| Reverse engineering | `reverse-engineer schema/check/record/review/accept` |
| Release acceptance (not RDD evidence) | `acceptance specs list/create/update/retire`, `acceptance report open/get/record/sign-off/reject`, `acceptance release` |
| Agent integration | `install`, `hooks` |
| Development | `dev run`, `dev setup`, `dev list`, `dev review` |

`dev review` inspects the current Git diff.

`modernpath reverse-engineer record --file <JSON|->` sends native requirements,
and their traces to the bound system in one request.
Before constructing input, query the server's current JSON Schema:

```sh
modernpath reverse-engineer schema > recording-schema.json
```

Use that schema for requirement content, criteria and trace endpoints. The
command reads the authenticated bound System and prints the schema directly;
failed reads report an error rather than falling back to a copied model. This
requires a server version that exposes recording schema discovery.
New requirements are recorded as `PROPOSED` in the bound System's Base release.
The server creates Base if needed within the recording transaction. Reused
requirements retain their existing release assignments. The command reads the
supplied JSON and posts it directly; it does not inspect a repository checkout
or require a run, content digest or proof report.

A sweep stages one JSON file per group of requirements. Check the staged files
offline, then record the folder in one call:

```sh
modernpath reverse-engineer check --dir groups --repo app=services/app
modernpath reverse-engineer record --dir groups
```

`check` needs no sign-in. It fails on missing or conflicting UUIDs, repeated
statements, shared criterion templates, missing or catch-all actors and fields
the server refuses, and warns on user requirements no system requirement
serves. With `--repo key=dir` it counts the tracked files below `dir` as
linked, excluded (`--exclude`) or unresolved; coverage never changes the exit
code. `--self-test` proves every check on an embedded known-bad set. The store
does not enforce the check.

`record --dir` sends one request per file in lexical order. After a success it
keeps the file's copy and receipt in `groups/recorded/`; a content refusal
(422, 409 or a local input error) is noted in `recorded/<name>.refused` and the
next file is sent. Any other failure stops the run before the next file with a
`stopped_reason`: `sign-in`, `permission`, `server-request` (another 4xx),
`server` (5xx), `unknown-outcome` (a reply with no data), `rate-limit` (exit
75), `connection`, `binding`, `local-write` or `local-read`; rerun to resume. A
rerun skips recorded and unchanged refused files (`--retry-refused` resends
those) and counts the refusals that still stand, and reports a recorded file
that changed. The folder is not
atomic, only each file's request, and the store stays the authority.

Check current verification before running tests:

```sh
modernpath reverse-engineer review --file selection.json --json
```

The selection is `{"requirements":[{"kind":"system_requirement","id":"<UUID>"}]}`;
`user_requirement` is also supported. The read returns `system_id`, `eligible`,
`problems`, `requirements`, `test_cases`, `runs` and `evidence`.

- Each requirement has `kind`, `id`, `revision`, `work_status` and `criteria`.
  Each criterion check has `criterion_id`, `state`, `problem`, `test_keys`
  (required TestCase IDs), `run_id` and `result_ids`.
- `test_cases` contains each required `id` and current `revision` once.
- `runs` contains each current `id`, `sha` and `status` once.
- `evidence` contains each selected result's `id`, `run_id`, `target_id`
  (TestCase ID), `result` and `validity` once.

DONE requirements have empty criterion checks and contribute no execution data.
Reuse applicable passing results; execute an unexecuted shared test once for all
criteria it covers. Each unresolved requirement needs one current run supporting
all its criteria. A newer partial or failed run cannot borrow older passing
results. Independent requirements can reuse different already-recorded runs.
A shared test's current execution affects every requirement linked to that test,
including when its tested code revision differs from an earlier passing run.
The server checks current relationships, executed TestCase revision, outcome and
validity. It does not infer applicability by comparing recording timestamps.
The response contains no historical execution lists, version bodies, copied
content or presentation bundles. Read requirement content separately when needed.

Before executing, ensure each TestCase has an active Trace to a `source_file`
or `file_analysis` in the same System. Use `reverse-engineer record` to establish
missing code relationships. Missing code links cause `native_test_chain_required`
at finalization; repair the link and reuse the retained run ID.

Reserve once with `evidence record`, retain the returned run ID, execute and
finalize actual test outcomes and criterion mappings. Retry finalization with
that same ID; do not reserve again to retry submission. An empty reservation
cannot be safely rediscovered from SHA alone. Missing, failing, stale or
incomplete evidence blocks acceptance.

After explicit human acceptance, send the selection and decision:

```json
{
  "requirements": [{"kind": "system_requirement", "id": "<UUID>"}],
  "source": "USER:2026-10-24",
  "reason": "Observed behavior and passing results are accepted"
}
```

```sh
modernpath reverse-engineer accept --file acceptance.json --json
```

The server validates current database state under locks, marks requirements DONE
and increments their versions atomically. Accepted versions retain content,
criteria, structural traces and selected result IDs without copied executions or
unrelated old result traces. The receipt includes resulting revisions, version
IDs and approval attribution. Repeating acceptance returns the existing version.
Already DONE requirements completed through an Epic or legacy path return
`result: "skipped"` with their unchanged revision and no version body.
Human acceptance remains separate from verification; no review body is uploaded.

### Backlog discoveries

`modernpath backlog create --file discovery.json --json` adds an attributed
`OPEN` record to the bound System. Use `--file -` for stdin. Input requires
`title`; optional `kind` is `backlog` (default), `gap` or `tooling`. Gaps require
`gap_kind` of `capability` or `specification`. Use
`modernpath backlog create --help` for content fields and examples.
Add `--focus` to atomically append a declared lane for the signed-in reporter.
Existing focus remains intact; a focus failure rolls back creation. Automatic
issue reports in ModernPath Systems must use `--focus`.
Read back with `modernpath backlog list --json` and
`modernpath backlog get <uuid> --full --json`. List output supports `--limit`
and `--offset`; detail output supports `--fields` or `--full`. Each served
`links` entry names its target: `title`, `display_ref` (requirements and test
cases) and `available` (false for a deleted target). Text output prints a
routing or closure target's title after the disposition, as
`ROUTED to 288 — <Epic title>`; `--json` serves the disposition as stored.

Route or close a discovery with
`modernpath backlog update <uuid> --file resolution.json --json`.
The JSON object accepts only `notes_md`, `disposition` and `disposition_ref`;
omitted fields are preserved. Notes replace existing notes, and an empty reference
clears the reference. For example:

```json
{"disposition":"CLOSED by PR:972","disposition_ref":"PR:972","notes_md":"Verified the merged fix."}
```

`ROUTED to <id>` must name an Epic of the bound System by integer id (an
`EPIC-`, `EPIC:` or `Epic ` prefix is tolerated, in any letter case) or a user
or system requirement by UUID; the server writes the `routed_to` link with the
record and refuses any other target with 422. `CLOSED by <id>` writes a `closed_by` link when the id names an Epic,
requirement or test case of the System and keeps any other reference, such as a
pull-request number, as text. A record routed to an Epic is listed by
`modernpath epics get <id> --fields backlog --json` (also under `--full`).
The server preserves the original raiser and records the authenticated update
actor and time in metadata. Read back with `backlog get <uuid> --full --json`.
This changes the backlog disposition, not an Epic approval or completion.
Requires a server with the Backlog PATCH endpoint; older servers refuse the write.

### Release acceptance

Release acceptance records what a human tester checked in a release. **It is
not RDD evidence**: it records no test case, evidence run or result, and it
does not accept a requirement. Specs are test steps for a tester, not
acceptance criteria. The commands are not part of the delivery loop.

An initiative (roadmap line) has acceptance specs. A tester opens one
acceptance report for the initiative and a release, records one result per
live spec, and the report is signed off or rejected. Every command works on the
bound System and sends it as `system_id`; another System's initiative, spec,
report or release answers `not_found`.

- `modernpath acceptance specs list --line <uuid> --json` lists the live specs
  in order.
- `modernpath acceptance specs create --line <uuid> --file spec.json --json`
  adds a spec at revision 1. The object takes `title` (required), `steps_md`
  and `expected_md`.
- `modernpath acceptance specs update <spec-uuid> --file spec.json --json`
  changes those fields; a change bumps the spec's revision.
- `modernpath acceptance specs retire <spec-uuid> --json` retires a spec. It is
  kept with its results and is no longer required.
- `modernpath acceptance report open --line <uuid> [--file report.json] --json`
  opens the report. The object may name `release_id`, `environment` and
  `build_ref`; without `release_id` the initiative's effective release is used.
  When a live (draft or signed-off) report exists for the initiative and
  release, that report is returned.
- `modernpath acceptance report get <report-uuid> --json` reads a report with
  its results, `tester_name` and `signed_off_by_name`.
- `modernpath acceptance report record <report-uuid> --spec <spec-uuid> --file result.json --json`
  records `outcome` (`pass`, `fail`, `blocked` or `not_run`) and `notes_md`.
  The result stores the spec's current revision.
- `modernpath acceptance report sign-off <report-uuid> --source USER:<ref> [--note <text>] --json`
  signs the report off as the signed-in person. Every live spec needs a result
  for its current revision, and a `fail` or `blocked` result needs `--note`.
- `modernpath acceptance report reject <report-uuid> --note <text> --source USER:<ref> --json`
  rejects the report. Opening the report again creates a new draft.
- `modernpath acceptance release <release-id> --json` lists every initiative
  in the release with its report id and status (`none`, `draft`, `signed_off`
  or `rejected`).

Only a person signs off or rejects. `sign-off` and `reject` exit non-zero
before any request unless `--source USER:...` names the human decision; the
source is stored on the audit event. An agent may write specs, open reports and
record results. Fields the server sets (tester, status, sign-off, revision,
scope) are refused in `--file` before the workspace loads. A signed-off or
rejected report is closed: further results, sign-off or reject answer
`report_closed` (409). Rule refusals name their code: `results_missing`,
`sign_off_note_required`, `no_specs`, `invalid_order`, `release_required` and
`release_not_open` (422). Requires a server with the acceptance routes.

## Native verification evidence

Reserve an empty non-Epic run before execution with
`{"sha":"<tested code revision>","status":"running"}`. Retain the returned `run.id`.
Active Epic work uses `epics gates start-execution` and its returned `run_id`.
Finalize under that identity:

```json
{
  "id": "<reserved run UUID>",
  "status": "complete",
  "native_verification": {
    "tests": [{
      "test_case_id": "<existing native TestCase UUID>",
      "result": "pass",
      "detail": {"executed_test_case": {"revision": 0}}
    }],
    "criteria": [{
      "criterion_id": "<native criterion UUID>",
      "test_keys": ["<the same TestCase UUID>"]
    }]
  }
}
```

Select existing tests explicitly. The server resolves scope, current test revision
and actual code relationships. One result per executed TestCase records `pass`,
`fail`, `error`, `skip` or `inconclusive`. FAIL means an actual failed assertion;
setup/build failures are ERROR. The producer must classify real execution
correctly; the server does not infer it from a report. Do not upload commands,
runner data, procedures, assertion arrays, output or environment.

Required tests come from TestCase-to-criterion links. Actual result-to-test and
result-to-criterion Traces establish executed mappings. All required tests must
pass at applicable revisions for complete coverage; no separate criterion result
or stored full/partial flag is created. The same test supporting several criteria
has one native identity and one result per run. Project checks use their approved
plan's TestCase identity. Missing, stale, skipped, incomplete or changed-definition
proof cannot establish pass; newer relevant failures cannot expose an older pass.

Each request uses current tenant/System permission and Epic ownership. No
per-run actor pinning or complete submission is stored. Finalized outcomes and
result mappings are immutable. Identical retries return the same identities;
conflicting outcomes, revisions or mappings refuse atomically. Later edits can
invalidate applicability without rewriting historical outcomes or recorded test
revisions. Raw output, full definitions and historical reconciliation are removed.

`evidence runs --full` reads surviving run/result/Trace facts. Use existing
TestCase and Trace identities for current detail. Historical definitions are
never filled from current content. Full traceability distinguishes linked test
files, actual test outcomes and complete criterion verification. Verification
changes no requirement lifecycle; explicit existing-behavior acceptance remains
separate and submits requirement identities, source and reason, not a review body.

## Personal focus on Backlog records

Use a native Backlog UUID with an explicit discriminator:

```sh
modernpath focus <uuid> --ref-kind backlog_record
modernpath focus
modernpath focus --drop <uuid> --ref-kind backlog_record
```

The declaration and scoped drop use the bound System. The list shows the native
record ID and title. `--requirement-kind` remains for requirement UUIDs and cannot
be combined with `--ref-kind`. Backlog focus is explicit only and remains until
dropped, including after the record disposition changes. These operations do
not assign ownership or change the record's disposition.
