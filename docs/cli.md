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
entry points for these operations. A compatible server must expose
`POST /api/compliance/releases/:id/activate`; UI and CLI share this endpoint.

## Move requirements between releases

Move an exact selection of user and system requirements through the supported
CLI. Sources may have no release, be in Base or be in an open delivery release.
The destination must be a planned or active non-Base release in the bound System. Closed,
release-exempt, deleted or foreign requirements refuse the entire request.

Create `selection.json` with kinds and UUIDs:

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
the new sources. A requirement that an in-place Epic took moves only with that
Epic while the Epic is in the requirement's Release, in any state, DONE
included: the preview and the move answer 409 `selected_requirement_held`, and
the command prints the server's sentence, such as `UR-7-0 Export invoices moves
with the Epic Billing cutover, which took it. Move the Epic to move it.`, with
the requirement and the Epic. Nothing moves; move the Epic instead. A
now-closed target refuses. Already-target eligible rows are
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

- `epics list` prints the numeric ID and title, one row per Epic.
  `requirements list` prints the display reference and title, one row per
  requirement. Titles are kept on one terminal line.
- Both lists default to 20 items (maximum 200). Filtering, ordering and
  continuation run on the server before the page is returned. `--order id`
  is the stable default; `--order title` orders by title with identity
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
  items contain `id` (UUID), `requirement_kind`, `display_ref` and `title`.
  Requirement detail always retains those identity fields (`id`,
  `requirement_kind`, `display_ref`), including when `--fields` selects content.
- Text lists print a continuation hint to stderr when another page exists.
  Progress and warnings also go to stderr. Errors are reported once and exit
  nonzero. Read commands never prompt. Write commands return a short receipt
  identifying the affected resource and result; explicit content reads and
  specification exports may return complete content.

#### Requirement references and identity

A requirement display reference is `UR-<display_number>-<revision>` for a
user requirement or `SR-<display_number>-<revision>` for a system requirement.
For example, `UR-6-0` identifies user requirement number 6 at revision 0.
Its UUID is immutable; the display reference changes with the revision.
All lookups are scoped to the authenticated workspace and bound System.

`requirements get UR-6-0` infers the kind from the prefix. A conflicting
`--kind` is an error. A revision-qualified reference must match the current
revision exactly: a stale reference returns an actionable error rather than
silently returning newer content. This command does not retrieve historical
accepted content. UUID lookup remains available and requires `--kind user`
or `--kind system`. The `user_requirement` and `system_requirement`
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

### Create requirements

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

The CLI supplies the bound `system_id`. The server assigns the UUID,
display reference, revision `0`, `PROPOSED` status and authenticated author,
and records the creation audit atomically. Client identity, lifecycle,
release assignment and approval fields are rejected.

The receipt includes `id`, `requirement_kind`, `display_ref`, `title`,
`system_id`, `work_status` and `revision`. Use its kind and UUID in
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

For grouping-only changes, send `{"context":"BILLING"}` or include
`context_name`. Both use the same rules as `set-context`: context is required,
both values must be nonblank strings of at most 255 bytes, and a requirement
moved without `context_name` takes the name the System's list gives that
context (see `requirements contexts`), or its code for a new context. So
leave the name out for a listed context. These edits preserve content
timestamps and evidence, even on unowned `DONE` or `OBSOLETE` requirements.
Active Epic-owned work remains read-only. A request mixing grouping and content
fields must meet the content-edit rules above and commits atomically. If the
supplied content values are unchanged and only grouping values change, content
timestamps and evidence remain current. Explicitly blank context names are
rejected by both commands.

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
each a UUID listed once. An item's own `reason` overrides the batch
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

### List the System's contexts

A requirement's context is the group it belongs to: a code, such as
`BILLING`, and a display name, such as `Billing`. The System's list of contexts
is every code its requirements carry, each with one name: the name most of the
code's requirements carry, ties broken alphabetically. A requirement without a
name carries its code, so a code whose requirements carry no name has its code
as its name.

```sh
modernpath requirements contexts
modernpath requirements contexts --json
```

The command prints one line per code with its name, its number of requirements
and the other names its requirements carry, then a line labelled
`Unclassified` with the number of requirements without a context. The numbers
count every requirement that is not deleted, `OBSOLETE` ones included, so they
can differ from a Modernization Release's counts, which leave `OBSOLETE`
requirements out. `--json` prints the data of
`GET /api/v1/systems/:system_id/requirements/contexts`:
`{"contexts": [{"code", "name", "other_names", "requirement_count"}],
"without_context": n}`. The command needs a bound System, and a refusal exits
non-zero with the server's reason.

Read the list before recording requirements, and give every new requirement a
listed code with the name left out, or a new code and name that no listed
context has.

### Set a requirement's grouping context

`set-context` is a compatibility shortcut to the same update service used by
`requirements update`. `context` and `context_name` are current grouping metadata. They can change on
`DONE` requirements without changing status, revision, criteria, traces or
immutable accepted versions. Existing accepted versions retain the context
values they recorded; newly accepted versions omit grouping metadata. The
current requirement row can therefore have a newer grouping value than an
older accepted snapshot. Active implementation work rejects regrouping.

```sh
modernpath requirements set-context <uuid> --kind system --context BILLING
modernpath requirements set-context <uuid> --kind system --context snake-game --context-name "Snake game"
```

Without `--context-name` the command sends only the context, and the
requirement takes the name the System's list gives that context, or its code
for a new context. Leave the name out for a listed context. For the
requirement's own code that is the name the whole list shows: sending only the
code gives a requirement that carries a minority name the list's name, and
leaves one that already carries it unchanged. `--context-name` gives a listed
context another name on this requirement, which renames the context one
requirement at a time while its code stays the same.

`set-context` and `requirements update` apply the System's list of contexts as
`reverse-engineer record` does. They refuse with 422 a code that differs from
a listed code only in letter case (`context_code_conflict`), a name that
another context has or its requirements carry (`context_name_conflict`), and a
value with spaces before or after it, the code `—` or `Unclassified` in any
letter case (`invalid_context`); the refusal names the listed value. Moving a
requirement into the exact code the team keeps succeeds while another code
carries the same name or the code is listed in another letter case, so a
legacy ledger can be cleaned up. The server records the actor and old/new
grouping values in the requirement audit trail.

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
literal substring search across the UUID, display reference, title and
description.
Null status and context values remain eligible when those filters are absent.

Each result contains the display reference and title; JSON additionally retains
the UUID and requirement kind. Pages are live reads, so concurrent
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
results retain identities alongside display references.
`OBSOLETE` requirement rows are never returned. Unconfirmed test cases are
found by exact match only and are marked `candidate`. `epics search`
shows the Epic ID as `#<id>` and says `keyword`,
`semantic` or both. The complete Epic hit includes its app link; select `url` or use `--full`
to include it. Links use the server-advertised app address, falling back to the
API host when none is available.

`--limit` is 10 by default and at most 25. These are bounded ranked searches,
not paged inventories; use list commands for continuation. `--json` prints
`query` and the selected `items`. A 404 names its cause: an unknown system (check the binding with
`modernpath status`) or a server without the search routes (update the server).
Use `requirements list` for exact status filters and paging.

### Read Epics and exchange specifications

Epics use IDs. Read, create and update them under the bound System, and exchange
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
`category` and `target_profile_id`, the record IDs `assignee_id` and
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

`epics create` takes `line_id`, the id of an Initiative on the Roadmap
(`roadmap show --json` lists them), beside `release_id`: the server links the
Epic to that Initiative and places it in the Initiative's Release unless
`release_id` names another open Release, else in the System's current
Release. The receipt names the Release (`release_id`) and returns the
Initiative's context (`initiative` with `line_id`, `text`, `context` and
`context_name`); write that context into the requirements it creates for that
Epic (`requirements create` with `context` and `context_name`). `line_id`
needs a server with this change: when the answer names no Initiative,
`epics create` stops with an error that names the Epic the server created
without the link, which you link with Add on the Initiative's panel.
`epics get <id> --fields covered_by` returns the Initiative that covers an
Epic (`line_id` and `text`), or `null`.

`epics create` and `modernpath new` both call `POST /api/work/epics`, which
checks every request before it writes anything; a refused request writes
nothing. It accepts `system_id` (the CLI supplies the bound System),
`line_id` and the client-writable fields of `epics update`; any field other
than `system_id` and `line_id` outside those, such as `metadata`, `evidence`,
`knowledge_config`, `generated_from` or `tenant_id`, is refused with 422
`unsupported Epic fields`. A missing `system_id`, or one that is not a
positive integer, is refused with 422 `invalid_system_id`, and a System
outside the workspace you selected answers 404. A planning-field value that
`epics update` would refuse on a new Epic, such as `shared_context`
references other than `[]` or an `accepted_context_revision` other than `0`,
is refused with 422 and the field named.

`epics get <id> --fields planning_contexts` (also under `--full`) returns the
planning contexts the Epic is planned within: `release`, the Epic's own
Release, and `initiative`, the one Initiative that covers it, each with its
`planning_context`, every reference with its title, and who last changed it
and when (`changed_by`, `changed_at`); either is `null` when the Epic has none.
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

### Read the Roadmap and apply a Roadmap plan

```sh
modernpath roadmap show
modernpath roadmap show --json
modernpath roadmap selection --context <code or name> --out <file>
modernpath roadmap selection --initiative <initiative-id> --json
modernpath roadmap apply --file plan.json --dry-run
modernpath roadmap apply --file plan.json --agent <agent-name>
```

`roadmap show` prints the bound System's Releases with the Initiatives under
each, then the Initiatives with no Release by horizon. A Release's and an
Initiative's planning context prints under its name, as stored; `--json`
carries `planning_context` on each Release and each Initiative, with references
as stored and no titles.

Under each Initiative a line says whether it is done. An Initiative is done
when at least one item of its linked work counts and every one of them is
DONE; its linked work is its Epics, the user requirements linked to it
directly and a system requirement that is its own link, and an item counts
unless it was deleted, is OBSOLETE or is a candidate that still needs
confirmation. The line reads `Done`, `No work yet` when no linked work counts,
or `Not done: 1 of 2 Epics complete`, naming only the kinds it has (Epics,
user requirements, system requirements). Under an Initiative that is not done,
one line per unfinished item gives its title, its step (Blocked, Not shaped
into work yet, Being built, Plan waiting for approval, Completion waiting for
approval, Deferred) and whose turn it is (The team, A person on the team),
blocked items first and deferred ones last; one with no linked work waits for
the team to shape it into work. `--json` carries `done`, `done_when` (the
`work_complete` item with `met` and the counts by kind) and `waiting_for`
(each item's `kind`, `id`, `title`, `step`, `turn` and the `member_epic` that
gives a user requirement its step) on each Initiative.

Under a Modernization Release come the legacy system it replaces, or `in
place`; its reference revision, or `not set`; and one line per context of its
scope with how many of its legacy requirements are undecided and how many are
decided, such as `Billing (BILL): 1 of 4 undecided, 3 decided`. A legacy
requirement is decided when an Initiative of the System has a keep, change or
drop row for it. When the scope holds legacy requirements that the business
owner's latest agreement did not record and no Initiative has decided, a line
says `New since the agreement: 4 legacy requirements`; `--json` carries that
number as the Release's `new_since_agreement_count`, and each context of
`modernization.contexts` with its `in_scope`, `undecided` and `decided`. Under
an Initiative that replaces part of the legacy system a line gives its
decisions, such as `Decisions: 2 keep, 1 change, 1 drop`; `--json` carries them
as the Initiative's `selection`: `keep`, `change`, `drop` and `agreed`, whether
the latest agreement holds them unchanged. An Initiative of new work has
`selection` null.

The context counts and the decisions both end with how many kept or changed
legacy requirements are drafted and built: `Billing (BILL): 1 of 4 undecided, 3 decided, 2 drafted, 1 built`
and `Decisions: 2 keep, 1 change, 1 drop, 2 drafted, 1 built`. Onto a new
system, a legacy requirement is drafted while an Epic's saved plan, or a
written trace, links a new requirement of the System to it with a `modernizes`
trace, and built once a written one has a DONE source. In place, it is drafted
while an Epic's saved plan takes it or links a test to it, and built once that
Epic is DONE. Drafted includes built, and a dropped legacy requirement is
neither. `--json` carries `drafted` and `built` in each context of
`modernization.contexts` and in each Initiative's `selection`.

`roadmap apply` applies a plan file `{"request_id": "...", "operations": [...]}`.
`create_line` and `update_line` take `planning_context`, the Initiative's whole
planning context. A write sends four lists, and a list left out is stored
empty, so `{}` is the empty planning context and on `update_line` clears it:

- a decision `{"statement": "...", "rationale": "..."}`, the rationale optional;
- an assumption `{"statement": "..."}`;
- an open question `{"question": "...", "status": "open" | "resolved", "resolution": "..."}`,
  the resolution optional;
- a reference `{"kind": "document" | "file", "target_ref": "document:doc:<id>" | "document:sd:<uuid>" | "file:v2:..."}`.

A statement or question is refused when empty, each text holds at most 20,000
characters, a `version` key on an entry is dropped and any other key is
refused. Beside the four lists, `version` 1 and `accepted_context_revision` 0
are accepted, so a value read with `roadmap show --json` can be sent back. At
most 100 decisions, assumptions and open questions together, and at most 100
references. A reference must name a document or a file of the workspace; the
older `file:<system>:<digest>` form is refused. A refusal names the operation,
the list and the entry to fix, and nothing is written. Each change is recorded
with the signed-in person and, with `--agent`, the agent.

A Release's planning context is written on the Release page; the command line
creates and edits no Release.

### Read and set an Initiative's selection

An Initiative that replaces part of the legacy system has a selection: a keep,
change or drop row for each legacy requirement of its Release's legacy system
that it decides, with a reason on each drop. A legacy requirement has at most
one row in a System. The business owner agrees the selection with the Now plan
on the Roadmap; no agent and no command records that agreement.

```sh
modernpath roadmap selection --context Integrations --out integrations.json
modernpath roadmap selection --context INTG --release 21 --json
modernpath roadmap selection --initiative <initiative-id> --json
```

`roadmap selection` reads the legacy requirements of one context, or the rows
of one Initiative, and writes them as one JSON document holding every page of
the read: `release`, `legacy_system`, `context` (null for an Initiative),
`initiative` (null for a context), `counts` (`in_scope`, `undecided`,
`decided`, `keep`, `change`, `drop`, `drafted`, `built`) and `requirements`. `--context` takes a
context of the legacy system by code or name. Without `--release` the command
reads the Roadmap and takes the one open Modernization Release whose scope
holds the context; it refuses when none or several do, naming them, and
`--release <id>` picks one. `--initiative` reads the Initiative's rows in every
context. `--out` writes the file and prints
`Wrote 300 legacy requirements to integrations.json`; `--json` prints the
document. Nothing is written when the command refuses.

Each requirement carries its `requirement_kind`, `requirement_id`,
`display_ref`, `title`, `description`, `context` and `work_status`;
`legacy_proof`, the legacy system's latest test results (`state` `passing`,
`failing` or `missing`, and its criteria counts); `group`, the user
requirement a system requirement belongs to; `dependents`, the other legacy
requirements linked to it; `flags`; and `decision`, null while it is
undecided, else its `outcome`, `reason`, `expected_level`,
`expected_level_reason`, `initiative`, `set_by`, `set_at` and `agent`. The
flags are `agreed` (the latest agreement holds this decision),
`revised_since_selected` (the requirement changed, or was made obsolete or
deleted, after it was decided), `context_changed_since_decided`, `not_counted`
(decided, but not among the legacy requirements the agreement counted) and
`new_since_agreement` (undecided, in the scope now and not recorded by the
latest agreement). The flag `new_since_agreement`
marks one legacy requirement; the Release's `new_since_agreement_count` in
`roadmap show --json` counts the undecided ones of a Release.

Each requirement also carries `expected_level`, the level of proof it should
reach (`compared`, `screens_compared` or `tests_only`), and
`expected_level_basis`, the code that says where that level comes from. The
Selection tab gives each code in words:

- `changed`, "changed requirement": a row decided change expects
  `tests_only`.
- `person`, "set by {person}", with the reason: a person set the level on a
  row decided keep, and the reason is the decision's `expected_level_reason`.
- `estimated`, "estimated from its files": the level comes from the roles of
  the source files the requirement's traces reach, as the legacy System's
  analysis gives them or, for a file it did not analyze, as the file's path
  gives them. A controller, API, worker, model or migration gives `compared`;
  otherwise a view or component gives `screens_compared`; otherwise, and with
  no traced file, the level is `tests_only`. An undecided or dropped row gets
  the level it should reach if kept.

Read the codes as an open list: a planned change (Epic 341) adds `recorded`,
"recorded by reverse engineering", which comes before `estimated`. With the
basis `estimated`, `expected_level_files` gives the roles the estimate found
and how many files it read, such as
`{"roles": ["component", "view"], "count": 2}`; with any other basis it is
null.

When the Release modernizes its own System in place, `at_risk` lists the
change rows behind At risk, the Selection tab's filter of kept requirements
at risk: for a row decided keep, each change row of the same Initiative whose
requirement shares a traced source file with it, with its `requirement_kind`,
`requirement_id` and `display_ref`, in order of `display_ref`. `at_risk` is
empty for any other row and for every row onto a new system. The command
writes the four fields as the server serves them and prints no words for
them.

Each requirement also carries `counterpart`, the new requirements and Epics
that draft or build it, and `holder`. `counterpart` is a list of
`{"requirement", "epic", "state"}`, `state` `drafted` or `built`, empty when
nothing drafts it. Onto a new system an entry names the new requirement of a
`modernizes` trace and the Epic whose saved plan holds the trace, or no Epic
once only the written trace remains; in place it names the Epic and no
requirement. `holder` is null, or what holds the row's decision:
`{"kind", "id", "title", "approver"}`, an approved Epic (`epic`) that relies
on the decision, or the new requirement (`requirement`) of a written
`modernizes` trace to it, with the person who approved that Epic or the
completion that wrote the trace.

`roadmap apply` sets the rows with `set_selection`. It names the Initiative by
`line_id`, or by `line_ref`, the `ref` a `create_line` earlier in the plan
gives the Initiative it creates:

```json
{"request_id": "intg-selection", "operations": [
  {"op": "create_line", "ref": "provider", "release_id": 21, "horizon": "next",
   "text": "Provider connections", "context": "INTG", "work_kind": "replaces_legacy"},
  {"op": "set_selection", "line_ref": "provider", "rows": [
    {"requirement_kind": "user_requirement", "requirement_id": "<uuid>", "outcome": "keep",
     "expected_level": "compared", "expected_level_reason": "The bank reads the file."},
    {"requirement_kind": "user_requirement", "requirement_id": "<uuid>", "outcome": "drop",
     "reason": "The bank takes the API now."}],
   "clear": [{"requirement_kind": "system_requirement", "requirement_id": "<uuid>"}]}]}
```

A row takes `requirement_kind`, `requirement_id`, `outcome` (`keep`, `change`
or `drop`), `reason`, `expected_level` (`compared`, `screens_compared` or
`tests_only`), `expected_level_reason` and `move`. A drop needs a reason. An
expected level, with its reason, goes only on a keep row. A row without
`outcome` changes only the expected level of the Initiative's keep row.
`"move": true` takes a legacy requirement another Initiative of the System
holds; without it that row is refused. A `clear` entry deletes the
Initiative's row, so the legacy requirement is undecided again. One
`set_selection` takes at most 1,000 rows and a plan at most 5,000 rows in all;
`set_selection` operations do not count toward the 50 operations of a plan.
The Initiative must replace part of the legacy system, have a context and be
shown under an open Modernization Release.

A refused `set_selection` writes nothing of the plan. `roadmap apply`, with or
without `--dry-run`, prints under the operation's error one line per refused
row, rows first and then `clear` entries:
`row 1: Export invoices: A drop in an initiative's selection needs a reason.`
A row that names no legacy requirement of the Release shows its requirement
id instead of a title, and a legacy requirement another Initiative holds ends
with `(held by <initiative>)`. The receipt gives each `set_selection`'s
`written`, `unchanged`, `cleared` and `moved` counts.

A held row cannot change, be cleared or move to another Initiative: an Epic
approved with a plan that relies on its decision holds it until its approver
returns the Epic to planning (in place, until the Epic completes), and a
written `modernizes` trace holds it until a later Epic that holds the trace's
new requirement retires the trace. Nor can its Initiative be archived, deleted
or sent back to the Roadmap at a close. The refused row has the reason
`selection_row_held` and its `holder`, and `roadmap apply` prints a sentence
that says what holds it and who can act:
`row 1: Export invoices cannot change: the Epic Billing export, approved by Ada Lovelace, relies on its decision. Ada Lovelace can return that Epic to planning.`
For a move it also names the Initiative that holds the decision:
`row 3: Archive statements cannot move to this initiative: the initiative Provider connections holds its decision, and the Epic Billing export, approved by Ada Lovelace, relies on it. Ada Lovelace can return that Epic to planning.`
An `archive_line` of an Initiative with a held row is refused with a sentence
that names the Initiative, the legacy requirement, the holder and who can act.
A held row's expected level stays writable.

An agent decides a selection at volume: it reads a context with
`roadmap selection`, suggests each row's outcome with its reason, sums the file
up in plain words for the person, checks it with `roadmap apply --dry-run`, and
applies it only on the person's word, naming itself with `--agent`.

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
resolutions, `readiness`, and source-file identities. Within `review`,
`test_cases` is keyed by TestCase UUID and `evidence` by criterion UUID.
Each criterion shows its current outcome and result identities.

```sh
modernpath epics plan show <epic-id> --full --json
modernpath epics plan save <epic-id> --file plan.json --reason "<reason>"
modernpath epics gates review-bundle <epic-id> --json
modernpath epics plan approve-implementation <epic-id> --reason "USER:<source>: approve"
modernpath evidence record --file result.json
modernpath epics plan submit-review <epic-id> --reason "<reason>"
modernpath epics gates presentation <epic-id> --json
modernpath epics plan approve-completion <epic-id> --reason "USER:<source>: accept"
modernpath epics plan return-to-implementation <epic-id> --reason "USER:<source>: continue work"
modernpath epics plan return-to-planning <epic-id> --reason "USER:<source>: replan" --carry-decision <decision-id>
modernpath epics plan history <epic-id> --full --json
```

The saved plan has `body`, `requirements`, `traces`, optional `remove_traces`
and `verification`. Requirement proposals retain `kind`, `id`, exact proposed
`content` and ordered `criteria`. Traces retain typed endpoints and
relationship. A trace left out of `traces` is kept; it does not by itself
satisfy the planning gate's implements-or-planned trace rule, which reads only
`traces`. A trace is removed only when the plan lists its ID in `remove_traces`:
each ID must be an undeleted active or stale trace of this System on a plan
requirement or criterion. Save refuses deleted, unknown, foreign-System and
out-of-plan IDs as `trace_removal_not_found`. Listing a stale trace does not
implicitly revive it. Approval records the list and completion removes exactly
those traces.
`verification` declares the plan `author_context`, decision UUIDs, criterion
obligations `{id, changed, test_case_id, boundary}`, project checks
`{id, name, test_case_id, boundary}`, and boundaries `{name, applicable, reason}`.
A check keeps its ID by name: a check saved without an ID takes the ID of the
saved check with the same trimmed name, and a new name gets a new ID. Two checks
with the same name, or a repeated check ID, are refused as
`invalid_verification_obligations`. A nonapplicable boundary needs a reason.
Source files that do not exist yet use the existing plan-only `planned` trace;
completion requires real scoped code endpoints.

A plan trace's `link_kind` is `implements`, `verifies`, `derives`, `relates`
or `modernizes`. A modernizes trace links a new requirement to the legacy
requirement it modernizes in another System. The platform admits it only for
a legacy requirement that a keep or change decision of the Epic's own
Initiative names; reverse engineering never records it. Its source is a user or
system requirement of the plan, its target a user or system requirement of the
legacy system of the Epic's Release:
`{"source_type": "user_requirement", "source_id": "<new requirement>", "target_type": "user_requirement", "target_id": "<legacy requirement>", "link_kind": "modernizes"}`.
Save refuses any other target with 422 `modernizes_not_selected`, whose
message names the legacy requirement and why (no keep or change decision, no
Initiative linked to the Epic, or a deleted legacy requirement) and whose
`legacy_requirement` gives its `kind`, `id`, `display_ref` and `title`, and a
target in the Epic's own System with `modernizes_inside_system`; nothing is
saved. Completion writes the trace in the Epic's System. A later Epic that
holds the source carries a written trace unchanged, or retires it by listing
it in `remove_traces`; a changed kind is refused. In place, where the Epic's Release
modernizes its own System, the plan takes the requirements it changes with
their revised statements, and a `verifies` trace from a test case to a kept
requirement outside the plan is admitted under that requirement's keep
decision.

`epics plan show <epic-id> --fields modernization` (also under `--full`) reads
the plan's `modernization` block: `initiative`, `legacy_system`, `release`,
`in_place`, `traces` (each `modernizes` trace with its `source`, `target`, the
`decision` that admits it and `carried`), and in place `taken` (the
requirements the plan takes), `kept_reached` (the tests linked to kept
requirements) and, before approval, `kept_at_risk` (each kept requirement at
risk with the files it `shares` with the plan). The block is null for an Epic
that no Initiative replacing part of the legacy system covers.

For an Epic of an Initiative that replaces part of the legacy system, the
`implementation` check reports `initiative_not_agreed` until the business
owner's latest agreement holds the Initiative among its Now Initiatives, and
`initiative_changed_since_agreement` when the Initiative's context, kind of
work or planning context, its Release's planning context, or a selection row
the plan relies on changed since that agreement. Each message names the
Initiative, what changed and who agrees the Now plan; `approve-implementation`
is refused with 409 `epic_gate_blocked` until the business owner agrees
again. Report the blocker and ask the business owner; no command records the
agreement.

Run focused assertions before implementation and confirm their expected
failures. After implementation, run focused and proportional checks and record
actual outcomes for each criterion and TestCase with `evidence record`. The
latest committed outcome for each pair is current, including failures.

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

Return to planning carries selected decisions. It clears current proof for each published criterion whose approved meaning differs from its published meaning; unchanged criterion proof remains current. A new plan review and implementation approval are required, followed by retesting each changed published criterion.

Approval and return requests contain reason and optional actor attribution.
The server checks eligible current database state under its existing locks.
They upload no plan, presentation, hash or token. A preview does not certify that
a previously rendered screen stayed unchanged. Independent review applicability
comes from changes, existing revisions and server ordering; formatting
alone is not a semantic change. Unknown applicability blocks.

Implementation approval owns the selected requirements and freezes the active
plan. Submission requires current completion-review readiness. Completion
publishes accepted requirement content, criteria and trace meaning atomically,
releases ownership and records approval attribution. Repeating terminal completion
publishes once. Return to implementation uses current authorization/lifecycle and
works when completion readiness fails. Return to planning withdraws approval and
starts a new planning cycle. Decisions listed with `--carry-decision` retain
their answers. Current proof for each published criterion whose approved meaning
differs from its published meaning is cleared; unchanged criterion proof remains
current. A new plan review and implementation approval are required, followed by
retesting each changed published criterion. Human implementation and
completion decisions remain separate.
No command merges or deploys. Read state and audit after an uncertain response.

`epics gates presentation` also lists `changes_since_approval` and
`assumed_defaults`. The first holds each change since the approval to the
planning contexts of the Epic's Release and Initiative: an opening for change
with who opened it and the reason, an edit, a lock, or a move of the Epic that
changes which planning context it reads. The second holds the assumed defaults
still routed to the Epic, which no person has accepted or rejected yet. Neither
blocks completion. The agent reads both before building and presents both at
completion review.

History exposes surviving decisions and accepted facts. Copied plans, raw output,
executed procedure snapshots and nested historical presentations are removed.
There is no archive or reconstruction from current TestCase content. See the
[model and cutover contract](../../docs/data/minimal-rdd-evidence.md).

### Read requirements, test cases, traces and evidence

Read a requirement by display reference, or by UUID and kind. Use
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
also verify the entity belongs to that System. `traces list
--relation` takes `implements`, `verifies`, `derives`, `relates` or
`modernizes`; a `modernizes` trace's target is a legacy requirement in another
System, and a requirement's detail gives each trace's other requirement end
its System id and name.

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
completion assess current criterion outcomes and scoped Traces.

TestCases retain UUID, tenant/System, title, existing revision, approval
attribution, deletion and server timestamps. Create with `{"title":"Access check"}`;
update with the same title-only shape. The CLI supplies the bound System. Reads
use UUIDs and title; lists search UUID/title and order by UUID or title.
Procedure, preconditions, runner/selector, authority, embeddings, descriptive body,
status and TC-number references are removed. A title change advances the existing
revision; that change does not invalidate proof.

```sh
modernpath test-cases create --file test-case.json
modernpath test-cases update <uuid> --file test-case-update.json
modernpath test-cases get <uuid> --full --json
modernpath evidence latest --target-type criterion --target-id <criterion-uuid> --json
```

Current results and typed relationships remain inspectable
through these existing identities. Historical definitions and raw output are not
retained or reconstructed. See [Verification evidence](#verification-evidence).

### Other lists, content reads and operational output

The same terse output contract applies across the command surface:

| Commands | Discovery and detail |
|---|---|
| `test-cases list`, `get` | UUID, title and revision. `get` and `update` use UUIDs. Lists search UUID/title with `--query` and sort with `--order id|title`. |
| `traces list` | Requires an entity type and ID; supports relation filtering and server continuation. Each row identifies the trace and its endpoints. |
| `evidence latest` | Bounded server queries support criterion, requirement, TestCase, outcome and time filters. Each row is current proof for one criterion and TestCase. |
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

`modernpath dev setup <tool>` writes the connected server's MCP address for
OpenCode, Cursor or Claude Code: `https://<api-host>/api/ex/mcp` on a platform
API host, the local gateway's `http://127.0.0.1:4123/api/ex/mcp` for a local
core on `:4000`, `<api_url>/api/mcp` elsewhere. The address is the same for
every workspace and setup works whether or not the CLI is signed in: the
coding agent signs in at ZITADEL itself, and its sign-in names the workspace.
Codex needs the address configured by hand. For Claude Code, setup also
writes the client id of the MCP assistant app (`oauth.clientId`) on the test
platform and on the local stack, where it adds the mock's fixed
`oauth.callbackPort`. Production has no assistant app yet. See
`../../docs/guides/connect-claude-code.md`.
Verify the connection in your coding agent before using its tools.

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
| requirements | `requirements create`, `requirements update`, `requirements list`, `requirements search`, `requirements get`, `requirements obsolete`, `requirements set-context`, `requirements contexts` |
| Epic records and specifications | `epics list`, `epics get`, `epics create`, `epics update`, `epics search`, `epics specs list/status/generate/pull/push` |
| Roadmap | `roadmap show`, `roadmap selection`, `roadmap apply` |
| Epic plans and their history | `epics plan show/save/approve-implementation/submit-review/approve-completion/return-to-implementation/history` |
| Requirement traces and test records | `traces list`, `test-cases list/get/create/update` |
| Evidence observations | `evidence record/latest/runs` |
| Reverse engineering | `reverse-engineer schema/check/record/review/accept` |
| Release acceptance (not RDD evidence) | `acceptance specs list/create/update/retire`, `acceptance report open/get/record/sign-off/reject`, `acceptance release` |
| Agent integration | `install`, `hooks` |
| Development | `dev run`, `dev setup`, `dev list`, `dev review` |

`dev review` inspects the current Git diff.

`modernpath reverse-engineer record --file <JSON|->` sends requirements,
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
The server creates Base if needed within the recording transaction.

Every new requirement needs a context from the System's list of contexts
(`modernpath requirements contexts`), also one given by a client UUID. Give a
listed code and leave `context_name` out, and the requirement takes the name
the list gives it; give a new code only when no listed context fits, with a
name that no listed context has. The server checks the requirements of one
request in order, so a later one in a code an earlier one introduced takes its
name. It refuses with 422 and writes nothing for a requirement without a
context (`context_required`), a listed code with another name
(`context_name_mismatch`), a code that differs from a listed code only in
letter case (`context_code_conflict`), a name another context has or its
requirements carry (`context_name_conflict`), and a value with spaces before
or after it, a blank name, the code `—` or `Unclassified`
(`invalid_context`). The refusal names the requirement's position in the file,
counted from 0, its title and the listed value. A requirement reused by id
keeps its stored context, and a null `context_name` matches the stored name. Reused
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
`problems`, `requirements`, `test_cases` and current `evidence`. Criterion checks
name required TestCases and result IDs. Every required test needs a passing result
linked to that criterion and a scoped source Trace. Independently recorded results
combine; a shared test has a separate current result for each criterion.

Reuse current passing proof when complete. Otherwise execute the required test and
record its actual outcome for the criterion. Test and requirement revisions do
not determine applicability. Missing, failing or incomplete proof blocks acceptance.

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
`title`; optional `kind` is `backlog` (default), `gap`, `tooling` or `assumption`.
Gaps require `gap_kind` of `capability` or `specification`. A record of kind
`assumption` holds an assumed default: an answer the agent chose during a build
for a question nobody had answered. Its `title` is the default and its
`observation` is the question the default answers. The command refuses an
assumption without a question before it sends anything, and the server gives it
the reference `Assumed default <n>`. Use
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
clears the reference, except on an assumed default after its first routing. For
example:

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

An assumed default is routed once to the Epic being built: its first routing,
from `OPEN` to `ROUTED to <epic id>` while it has no `disposition_ref`, needs no
source. Every change of an assumed default's disposition but its first routing
needs a `USER:` source: after `with` for `ACCEPTED with` and `REJECTED with`, as
in `ACCEPTED with USER:<the person's words>`, and in the same file's
`disposition_ref` for `CLOSED by`, `DEFERRED`, `OPEN` and `ROUTED to`. A source
stored by an earlier change does not count. After its first routing an assumed
default's `disposition_ref` changes only to a `USER:` source, so an empty value no
longer clears it. Without a source the server refuses the whole update, notes
included, and `backlog update` prints the refusal as it is. The server can only
check that a `USER:` source is there, so an agent writes one only from the
person's own words, never from a template. After completion approval the agent
closes each backlog, gap or tooling record routed to the Epic with
`CLOSED by <epic id>`; an assumed default is never closed by the Epic.

### Initiatives

An Initiative is a roadmap line of the bound System; API fields keep the name
`line`. Every command works on the bound System and sends it as `system_id`
(a query on reads, a body field on writes); an Initiative of another System or
an archived one answers `not_found`. A non-UUID Initiative ID fails before any
request.

- `modernpath initiative list [--json]` lists the bound System's Initiatives in
  one read: ID, text, release, horizon and progress. `--json` prints the served
  list.
- `modernpath initiative get <initiative-uuid> [--json]` prints one Initiative
  with its work and sources, read from the same list. An ID not in the list fails with
  `Initiative <id> not found in System <n>`.
- `modernpath initiative create --file initiative.json [--json]` creates an
  Initiative. The object takes `text` (required), `horizon`, `release_id`,
  `goal_label`, `line_type`, `source` and `notes`; any other field is refused
  before the workspace loads (`--file field <name> is not allowed`).
- `modernpath initiative link <initiative-uuid> --epic <id>` or
  `--requirement <uuid> [--requirement-kind user_requirement|system_requirement]`
  links one Epic or one requirement (`user_requirement` is the default kind). The first link
  is the Initiative's own link; later links are extra work.
- `modernpath initiative unlink <initiative-uuid> --epic <id>` or
  `--requirement <uuid>` (a user requirement) removes extra work. The own link is not removed this
  way (`own_link`).

`link` and `unlink` need exactly one of `--epic` or `--requirement`. Work
another Initiative covers is refused as `already_on_roadmap: <ids>`, work that
cannot be linked as `not_linkable: <ids>`, and a field refusal prints
`field: message` lines (for example `text: can't be blank`).

#### Initiative rounds

A round is the explicit list of the Initiative's Epics that one command names
with `--epics <id,id>`: at most 10 distinct Epic IDs (else `--epics must list 1 to 10 distinct
Epic IDs`), each linked to the Initiative and in its System. Nothing stores a round. A round command writes
each Epic's own records, all or nothing: the same records as the single-Epic
commands for every Epic in the round, in one transaction, so every Epic passes
or nothing is written, and a refusal names the Epic by index and ID
(`epic_gate_blocked — item 1 (Epic 7)`). A round submission needs the caller to
be each Epic's implementation approver or its assignee; any one person may
approve a round's completion. Each Epic keeps its own approvals, reviews, gate checks and
completion, and can still be returned to planning or to implementation alone
with the single-Epic commands.

- `modernpath initiative review-bundle <initiative-uuid> --epics <ids> --phase <phase>`
  reads `{initiative: {line_id, title, system_id}, epic_ids, members}`: each
  Epic's review bundle, ordered by Epic ID.
- `modernpath initiative review <initiative-uuid> --file review.json` records
  one review of the round, sent unchanged: `phase` (`plan` or
  `implementation`), `reviewer_context`, `reason` and `members`, listing
  exactly the round: each Epic's `epic_id` and that Epic's `criteria`,
  `obligations`, `findings` and `scope_implemented`. It writes one review per
  Epic, each recording `round: {line_id, epic_ids}`, and answers
  `{items: [{index, epic_id, review_id}]}`.
- `modernpath initiative check <initiative-uuid> --epics <ids> --phase <phase> [--git-sha <sha>]`
  records each Epic's own gate check and exits nonzero when any Epic is not
  ready.
- `modernpath initiative approve-implementation <initiative-uuid> --epics <ids> --reason "USER:..." [--agent <slug>]`
  applies the human's one implementation approval to the round. The three
  approval commands take `--agent` as the single commands do and answer
  `{line_id, items: [{index, epic_id, process_status}]}`; a blocked gate prints
  that Epic's gate report first.
- `modernpath initiative submit-review <initiative-uuid> --epics <ids> --reason <text>`
  submits every Epic for completion review.
- `modernpath initiative presentation <initiative-uuid> --epics <ids>` reads
  `{initiative, epic_ids, members: [{epic_id, presentation}], ready, review}`; `review.current` is true only when every
  Epic's latest implementation review was recorded on this Initiative for the
  same Epic list and is current by recorded changes.
- `modernpath initiative approve-completion <initiative-uuid> --epics <ids> --reason "USER:..."`
  applies the human's one completion approval to the round.

A round runs in one transaction bounded at two minutes. After an uncertain
answer, read an Epic of the round back before retrying; split a large round if
it times out. Membership refusals: `epic_not_in_initiative` (422) and
`member_outside_system` (409) at the Epic's index, `initiative_not_found` (404),
`round_too_large` and `duplicate_items` (422). Requires a server with the
Initiative round routes.

### Release acceptance

Release acceptance records what a human tester checked in a release. **It is
not RDD evidence**: it records no criterion result, and it
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

## Verification evidence

Record actual outcomes for one requirement criterion:

```json
{
  "criterion_id": "<criterion UUID>",
  "tests": [{"test_case_id": "<TestCase UUID>", "result": "pass"}]
}
```

Use `modernpath evidence record --file result.json`. A batch uses
`{"items":[<criterion update>, ...]}` with at most 50 criteria and 500 distinct
TestCases per criterion. The request is atomic. Each TestCase needs a verifies
Trace to the criterion and a scoped source Trace. Outcomes are `pass`, `fail`,
`error`, `skip` or `inconclusive`; distinguish failed assertions from setup errors.

Each result has one criterion Trace and one TestCase Trace. A later committed
outcome replaces the earlier outcome for that pair, including a failure.
Identical retries preserve result and Trace identities. Updates to one criterion
do not change another criterion, even when they share a TestCase. Read current
facts with `modernpath evidence latest`. No proof history, executed-version
comparison, raw output or separate project-check proof is stored. Verification
changes no requirement lifecycle; human acceptance and completion remain separate.

### Modernization test history

Modernization test history is separate from current criterion proof. After a
test execution, capture its commit and actual outcomes in one request, scoped to
an open Modernization Release:

```json
{
  "sha": "<commit>",
  "tests": [{
    "test_case_id": "<TestCase UUID>",
    "result": "pass",
    "criterion_ids": ["<criterion UUID>"]
  }]
}
```

```sh
modernpath modernization record --release-id <release-id> --file modernization-results.json
```

The request records the run, results and their criterion/TestCase Traces
atomically. Failed outcomes remain recorded. A failed request stores none of the
capture. The Release supplies the legacy System and scope; the caller lists the
criteria each executed TestCase covered (up to 500 test cases and 50 distinct
criteria per capture). This command does not register an
empty run or offer progress/finalization steps. Reading or capturing this
history does not change current `CriterionTestResult` proof or ordinary
readiness. The Release's reference commit selects historical outcomes, and
multiple runs can contribute results at that commit.

## Personal focus on Backlog records

Use a Backlog UUID with an explicit discriminator:

```sh
modernpath focus <uuid> --ref-kind backlog_record
modernpath focus
modernpath focus --drop <uuid> --ref-kind backlog_record
```

The declaration and scoped drop use the bound System. The list shows the
record ID and title. `--requirement-kind` remains for requirement UUIDs and cannot
be combined with `--ref-kind`. Backlog focus is explicit only and remains until
dropped, including after the record disposition changes. These operations do
not assign ownership or change the record's disposition.
