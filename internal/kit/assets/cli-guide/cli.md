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

`modernpath auth` opens your browser to sign in. For a machine without a
browser, run `modernpath auth --device-flow`. `modernpath init` lets you choose
the system represented by this repository, and `modernpath status` confirms
the connection.

### Install the process and agent hooks

From the connected repository root:

```sh
modernpath install
modernpath hooks install
```

`modernpath install` writes the requirement-driven process and agent instructions
for this repository. `modernpath hooks install` gives supported coding agents
ModernPath context. Commit the generated process and instruction files.

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

`modernpath feedback "<line>"` files a tooling gap — a surface the CLI, the
store or the harness lacked — as a `BACKLOG-TOOL-<n>` record only in the
current ModernPath workspace, with the CLI build, server contract version
and time captured. It requires a credential issued by `https://id.modernpath.ai`
for the ModernPath organization (`371734807656268047`). The server must identify
the checkout's bound system with slug `modernpath`. Customer credentials and
customer workspace bindings are refused; feedback never switches destinations.
`--last` also attaches the previous command you ran, its exit status and the
tail of its output, read from `.modernpath/cli-history.log`. That history
records every user-run invocation (never `auth`, never a hook invocation, and
token-like flag values redacted); `dev` and `ralph` keep the real terminal and
record only their command line and exit. If the credential or destination
cannot be verified, feedback fails without creating a record or local fallback
file. Report the tooling issue in the session instead of adding it to a
customer backlog. Read feedback records from a checkout bound to ModernPath.

Before its first store write in a process the CLI reads the server's sync
contract (`GET /sync/contract`): a write that needs a capability this build
does not implement is refused by name — `this build (<version>) predates
<capability> … rebuild and install` — and nothing is sent; a read that meets a
newer contract version (the `x-modernpath-contract` header) warns once. A
server without the read is older than the check and is written to as before.

A work selection carries a lane: `working-set select <scope> … --lane defect`
marks a customer-blocking defect, and `process next`, `your-move` and the
session brief say so (`lane: customer-blocking defect`, a `⚑ defect` chip), so
a session learns from the store that the clock is different. The default is
`planned`; a suspend and resume keep the lane.

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

The upload is the only import method: `--local` is accepted and changes
nothing. Import from a git URL was removed together with its `--git` flag,
since the server no longer accepts it; a script that passes `--git` now fails
with `unknown flag: --git`.

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

The verb packs the directory with exactly the import filters (skip lists,
`.gitignore`, `--exclude`, `--keep`, `--max-size`; see below), computes the
content revision the server uses, and compares it with the server's before
uploading anything.
Nothing changed and the last refresh completed: nothing is uploaded and the
system is reported unchanged at that revision. Nothing changed but the last
refresh failed or was stopped: the refresh is queued again without an upload.
Changed: the archive is uploaded, the previous source is superseded, and an
incremental refresh of the knowledge core is queued. The checkout's git head,
branch and dirty flag travel as metadata and show on the system's Define page
beside the revision and the time it was received.

The verb never prompts. A server refusal — a repository of the system is
being analysed or its documentation refreshed, the push was cancelled while
it was being sealed, or the repository is linked to a git provider the
platform refreshes itself — exits non-zero with the server's error code and
message, so a CI job fails loudly. A busy refusal names the repository and
the time it has been busy since; a mark older than eight hours no longer
refuses. A changed tree keeps its repository busy from the push until the
refresh it queued has run, so a second push in that window is refused too.
Cancelling the analysis or refresh in the UI clears a busy mark at once.
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
unattended runs` instead of printing "cancelled" and exiting 0.

`docs refresh`, `docs preview`, `docs repair` and `analysis status` act on the
repository `.modernpath/config.json` records (`repository_id`, written by
`import --local`), else the system's single upload repository, else the
repository whose local path is the current directory. When none resolves they
exit non-zero and name the `repository_id` remedy.

### Explore the Knowledge Core

Before planning a sourced change in a bound workspace, prepare the current
delivery context and compare the local and server document timestamps:

```sh
modernpath process prepare-inputs
```

The command reads the combined context once and reports the actor, bound
system, server contract, release provenance, held-piece count, pending
decisions, local last-sync time and latest server document update. It does not
download documents, select a release or select a piece. A null server update
means the server has no documents. If the local system document tree is missing,
the report says it is unavailable and points to `modernpath docs sync`. If the
export manifest has no server `generated_at` value, its timestamp is unknown
and the report also points to `modernpath docs sync`.
Human output shows a compact readiness summary; add `--verbose` for the store,
contract, CLI, kit and source diagnostics. Scripts can use
`modernpath process prepare-inputs --json` and check
`ready` before continuing.

When the local document tree is available, locate and read the full exported
document without another ModernPath API call. If it is unavailable, its sync
time is unknown, or the server update is newer than its recorded sync time, run
`modernpath docs sync` first:

```sh
rg -n "authentication" .modernpath/<system-slug>
cat .modernpath/<system-slug>/architecture/overview.md
```

For API-backed discovery or a synthesized answer, use `modernpath search` or
`modernpath ask`. `modernpath read-doc --list` accepts `--tier` and `--angle`
to narrow an API-backed document list.

`modernpath ask` runs the question in a background job on the server. An
answer ready within 45 seconds comes back at once; otherwise `ask` waits for
it, showing `Still working (m:ss)…` on stderr in pretty format, for up to 10
minutes (`MODERNPATH_ASK_WAIT_LIMIT`, a Go duration such as `20m`, changes the
limit). A failed ask, or one still running at the limit, prints its reason and
its ask id and exits non-zero. The job answers within 5 minutes. It starts
from matching code files as well as documents, and can also search the
curated patterns and capabilities the system uses. When the model cannot
answer in time, the answer lists what the search gathered and says that it is
partial. Each source is
listed once, by its title or, for a file, by its path. The server names each
source's kind (document, file, pattern, capability) and id; `--format=json`
keeps its three keys.

### Connect requirement-driven development to Mission Control

Repositories using ModernPath's requirement-driven process have a second,
independent synchronization path. Install the process package, select the
active release recorded by the repository, verify the binding, and perform the
first foreground sync:

```sh
modernpath install
modernpath factory release use <release-slug>
modernpath factory status
modernpath factory sync
```

`modernpath init` already selects and records a system. Do not immediately run
`factory connect` after a successful initialization. Use `factory connect` only
to create or repair a factory-only binding without downloading the Knowledge
Core export, or to rebind an existing workspace deliberately:

```sh
modernpath factory connect --system <system-id>
```

Install agent hooks after the foreground sync succeeds:

```sh
modernpath hooks install
modernpath hooks status
```

The hooks can inject relevant codebase context, run the local process gate, and
automatically synchronize coherent repository state. The exact families vary
by supported coding agent; `hooks status` and `hooks doctor` report what is
configured and which CLI binary will run.

### Know which sync you are running

The two sync commands have opposite directions and different data:

| Command | Direction | Purpose |
|---|---|---|
| `modernpath process prepare-inputs` | read-only server context | Reads preparation context once and reports release readiness and document timestamps; it does not download or write documents. |
| `modernpath docs sync` | server to repository | Downloads the latest Knowledge Core export into `.modernpath/<system-slug>/`; reserved top-level names use `.modernpath/docs/<system-slug>/`. |
| `modernpath factory sync` | repository to server | Uploads requirements, epics, specifications, gates, evidence, releases, and loop activity for Mission Control. |

`modernpath status` reports their timestamps separately as **Docs Sync** and
**State Sync**.

### Find synced requirements

Use `requirements list` to search the requirements already synced for the
bound system without downloading the full ledger:

```sh
modernpath requirements list
modernpath requirements list --status IN_REVIEW --kind user --context AUTH --query "two factor" --limit 25
modernpath requirements list --status IN_REVIEW --kind user --context AUTH --query "two factor" --limit 25 --cursor "<next_cursor>" --json
```

The command returns one page, 50 rows by default and at most 200. Status means
the exact `work_status`; kind is `system` or `user`; context is the exact
context code. These filters combine, while `--query` is a case-insensitive
literal substring search across the full external ID, title and description.
Without `--status`, `DERIVED` candidates are omitted; use `--status DERIVED`
to find them. Null status and context values remain eligible when those filters
are absent.

Each result contains a title preview up to 200 characters and a description
preview up to 400 characters, with truncation flags. Pages are live reads, so
concurrent changes may affect a later page. Continue with the returned cursor
and the same filters; the cursor does not provide a snapshot. `--json` prints
`items` and `next_cursor` for scripts. The command does not retrieve detail,
acceptance criteria or relations; use `modernpath working-set pull <id>` for
full content. A server that does not support discovery returns an explicit
error; the CLI does not fall back to downloading the full corpus.

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
similarity. Each hit shows its id, status, name and how it matched.

`requirements search` covers user requirements, system requirements and test
cases. It matches external ids and display references (`UR-5`, `SR-12-0`,
`TC-8`); a row without an external id is shown by its display reference.
`OBSOLETE` rows are never returned. `DERIVED` requirements and unconfirmed test
cases are found by exact match only and are marked `candidate`. `epics search`
shows an epic's code, or `#<id>` when it has none, and says `keyword`,
`semantic` or both.

`--limit` is 10 by default and at most 25. `--json` prints the server's `query`
and `items`. A 404 names its cause: an unknown system (check the binding with
`modernpath status`) or a server without the search routes (update the server).
Use `requirements list` for exact status filters and paging, and
`working-set pull <id>` for full content.

### Work with server decisions

Mission Control can record human answers and specification edits on the
server. Inspect and materialize them into the versioned repository with:

```sh
modernpath factory gates
modernpath factory gates <external_id>
modernpath factory gates <external_id> --audit
modernpath factory gates <external_id> --audit --json
modernpath factory gates --state answered
modernpath factory gates --state all --json
modernpath factory pull
modernpath factory pull --apply
```

`factory gates` alone is the open decision queue. `factory gates <external_id>`
shows one gate — its state and, once it is answered, the answer, its `USER:`
source, who answered and whether the answer has been applied — so you can
confirm an approval landed without reading the event stream. An answer that has
been applied reads as state `closed` with its answer intact; read it by id or
under `--state all`. `--state open|answered|dismissed|superseded|all` lists gate
history in the store's vocabulary (an unknown value is refused before any
request), and `--json` prints the server's gate envelope on stdout and nothing
else — `{"gates": […]}` for a listing, `{"gate": {…}}` by id — for scripts.

A listing's text output prints at most 50 gates. `--limit N` changes that and
`--offset K` pages; when more remain, the listing ends with
`showing N of M · --offset K for more`. Under `--json` every gate is printed
unless `--limit` is given, so `--state all --json` still returns the whole
history. `process backlog list` and `process findings list` page their text the
same way, and `process backlog list --json` prints `{"backlog": […]}` with
every record unless `--limit` is given. A `--json` listing also carries
`total`, the number of records, and `has_more`, true when records remain after
the page.

The by-id text output includes the stored decision brief without `--json` or
`--verbose`: **What**, **Why now**, **Changes if approved**, **Risk if wrong**
and **Recommendation**, in that order. Missing or blank fields are omitted;
multiline content is retained. Compact queue and history listings remain brief.

Applying a lifecycle decision requires a human answer with exactly the `approve`
option selected. Echoing a decline or free text alone does not authorize the
transition. Authoring a lifecycle gate requires an `approve` option from the
start. An entry or completion approval must declare the exact transition,
including imported approvals; an older decision without it needs a fresh reviewed
approval before it can be applied. Entry also requires a named independent cold review and no unresolved
material findings for the reviewed scope; these checks run when the gate opens,
when it is answered, and when its answer is applied. Completion readiness and
answering also recheck current passing evidence and reconciled item states.
Consuming a reentry approval closes it and records it as applied in the same transaction.

`factory gates <external_id> --audit` adds separate answer and application
results, simultaneous blockers with recovery guidance, and named review contexts
with their independence results. It reads the gate's own scope without selecting
work or changing records. Normal listings and detail reads retain their existing
cost. `--json` returns the same diagnostics under `gate.audit`; an older server
that omits the requested audit produces an error.

Application checks cover ordinary lifecycle advances. Separate release, reentry,
declaration and chat application paths are labelled `unsupported`, so `pending`
is not reported as a stranded approval. Postpone and resume transitions also use
a separate path: `author advance --decision USER:<source>` moves the item and
leaves the gate unconsumed. The audit reports this path as `unsupported` and gives
the decision-reference guidance. The audit preserves exact remaining scope
IDs and flags closed/pending or fully consumed/unsettled history for reconciliation;
it does not replay, normalize or withdraw decisions. Independence is unknown when
the pinned review scope cannot be resolved. These are current read-only checks;
the write path still validates again when applying a decision.

Run `pull` without `--apply` first to inspect pending intents. Applying a gate
answer writes its receipt under `mission-control/`; applying a specification
edit writes the corresponding specification file. The CLI then acknowledges
the materialized intent to the server. An applying agent or human must still
propagate a gate decision through the affected requirements and specifications.
Resolve reported content conflicts manually; the CLI does not choose between
competing edits.

### Run each delivery phase in one call

In a store-backed workspace every write is still one fingerprint-guarded
record, but the batch verbs do a whole phase step in one call and report
each record's result. A refused record is printed with the server's refusal.
Rerun the same file after a fix: records that were already written are
skipped or reported unchanged.

```sh
modernpath author apply --file plan.yaml            # epic, URs, SRs, relations, membership
modernpath working-set pull --scope --for-review    # one REVIEW.md with the aggregate stamp
modernpath process review record --file review.json --review-context <id> # findings, dispositions, cold-review trace
modernpath process enter EPIC-X
modernpath author advance --gate ENTRY-EPIC-X       # every record the gate names, members first
modernpath factory evidence --file runs.json        # RED and passing runs
modernpath process advance --all --piece EPIC-X --log "go test ./..."
```

- `author apply --file` uses the same write engine as `working-set push`. It
  checks the whole plan before the first write: one invalid record stops the
  call and nothing is written. A missing record is created, then patched. Each
  record's changed fields, criteria, relations and membership go in one atomic
  patch that carries the authoring context. An updated record's line shows the
  fingerprint it replaced and the new one. **Pull first, or pin
  fingerprints:** an existing record that would change is guarded by its
  `expected_fingerprint` in the plan, or else by the fingerprint of its
  `working-set pull`. Without either, apply refuses it; with a fingerprint
  that has moved since, apply refuses the call and keeps the other session's
  edit. New records need neither. `--dry-run` prints the plan. (`--from-pull`
  is deprecated and has no effect: the pull is always used.)
- `working-set push` never creates a record: an item file whose id the store
  does not know is refused before any write, and `author apply` is the only
  create path. A pulled SR shows its `lane_class`, and push can change it.
- `working-set pull --scope --for-review` writes `REVIEW.md`, the whole packet
  in one file, and stamps the packet aggregate. `process review record`
  refuses before any write when that stamp is missing, when the aggregate has
  moved since the pull, or when a PASS would leave a material finding open.
  Each requirement in `REVIEW.md` has a `Sources` line with its served
  citations. The pull also stamps each record's and packet section's
  fingerprint (`reviewed: <key> <fingerprint>` lines in `.context`), and
  `process review record` records them on the trace as
  `reviewed_fingerprints`.
- An epic created from a chat carries what the chat's search found. The
  scope pull's epic file shows them in a read-only `found_in_chat` block and
  `REVIEW.md` lists them under "Found in chat", each as its kind and name.
- `working-set pull --scope --for-review --since <trace>` writes a later
  round's `REVIEW.md`: the previous trace's code revision beside HEAD, what
  changed since that review in full (no text diff: prior content is not
  stored), the open findings, the previous verdict, and the unchanged ids with
  their fingerprints. When the revision moved, the reviewer re-verifies the
  citations of the unchanged items. It refuses a trace that is not a cold
  review, belongs to another scope, or carries no reviewed fingerprints, and
  it is refused with the by-id narrow review. The stamp and the trace still
  pin the full aggregate.
- `author demote --ids A,B,C --to … --basis … --reason USER:…` (or `--file`,
  one id per line) opens one demotion gate over items that are all IN_REVIEW
  or all DONE; a mixed batch is refused before any write, naming both groups,
  the group holding user requirements first. The gate id is
  `DEMOTE-<first id>`, or the next free `-R<n>` when earlier gates in the
  series are closed or withdrawn; an open or answered one is refused.
  `author demote --gate-id <gate> --apply` advances every item after the
  answer, user requirements first: an item already applied is skipped, an
  item another gate's follow moved is passed over (the output then names
  `author gate-withdraw` for the gate that cannot close), and any other
  refusal stops the run listing what was applied and what remains — re-run to
  resume. `author demote <id> --apply` applies the newest gate in its series.
- `process findings add --file` and `process findings disposition --file`
  record lists of findings and dispositions. A single disposition given
  `--scope` reads the finding's fingerprint itself and needs `--from`, the
  disposition you saw: it is written only while the finding is still in it.
  These files, and the review file, refuse a key they do not know and name
  it. A finding entry takes `introduced_by`, and a disposition entry takes
  `resolution` (`packet-edit`, `scope` or `decision`) and `widens`, with the
  rules of `--introduced-by`, `--resolution` and `--widens`: a `RESOLVED`
  entry must name its resolution. The batch verbs refuse a bad entry and run
  the rest; a review file with a bad entry is refused before any write.
- `author advance --gate <GATE>` reads the gate's fingerprint, transition and
  `approve` answer. Without a record id it advances every record the gate
  names that is still in the FROM state. A member already past the transition
  is skipped; a member that is neither, or is missing, keeps the epic where it
  is, and the call exits non-zero naming it.
- `factory evidence --file` posts one run per entry. The run ID is derived
  from the entry, so rerunning the file updates the same runs.
- `process advance --all --piece <EPIC>` advances every system requirement of
  the piece. It reports why each one it could not move was refused.
- `process advance <SR>` judges a TODO member by its own entry at the current
  packet aggregate, the way reconcile does, not by the epic's entry pin. It
  needs no `--piece` when the SR is itself one of the pieces you hold.
- `process next` with several held pieces prints one block per piece instead
  of refusing.
- Live requirements under a retained OBSOLETE owning Epic can complete together
  at the Epic packet pin or individually at their member pins, with current proof
  and human approval. The owner stays OBSOLETE. Entry, deleted authority and
  DONE/DEFERRED owners retain their existing guards.
- Delivery checks ignore OBSOLETE and DEFERRED requirements. Their records,
  postponement metadata and approval history remain stored. A live user
  requirement with no applicable required SRs still needs its current upper
  proof. If a selected scope has no applicable requirements, `process next`
  reports nothing to deliver and no acceptance recorded; its checks are
  NOT_APPLICABLE. Restoring deferred work requires reopening completed owners
  and fresh applicable entry before implementation can resume.
- `process reconcile` reports the exact trace leaves blocking a reviewed Epic,
  including purpose, scope, state and expected/observed pins. Fix the failed or
  stale evaluation and record new current proof. Reapplying a human entry
  approval does not refresh its old entry audit; record a new independent audit.
  An unavailable selected authority is reported as a refusal rather than
  "nothing to do".
- A DONE scope with current member evidence and its original attributable
  applied completion receipt can remain settled after canonical section-only
  edits. Reopened work, live changes and contradictory trace leaves retain
  their normal guards. The original receipt supplies no new approval.

The `mp-process-cli` skill gives these sequences phase by phase, with the
single-record verbs as the fallback. Run `modernpath install` after upgrading
to refresh the skill and `.modernpath/cli-reference.md`. The kit's permission
rules allow `author apply`, `process review record` and `process advance` like
the other routine writes. `modernpath hooks install` adds them where the
workspace has not placed them in a list itself.

### Take a small change through the lane

A small change is one SR in no epic, of a class the System's lane
authorization covers, with at most five non-test source files and no excluded
area. It gets one narrow independent review, enters by the standing
authorization instead of a per-change human gate, and completes in a batch.
The server checks every rule and refuses by name.

```sh
modernpath process lane authorize --classes wording,defect_with_failing_test \
  --appliers 7 --expires 30d --cap 10     # once per System
modernpath process lane approve LANE-AUTH  # a workspace admin answers it (or in the web app)
modernpath author apply --file sr.yaml                   # the SR with lane_class and sources
modernpath working-set pull REQ-X --for-review           # REVIEW.md for the reviewer
modernpath process lane review REQ-X --file review.json  # the narrow review
modernpath process lane enter REQ-X                      # PROPOSED->TODO by the authorization
modernpath factory evidence --file runs.json             # RED, then the passing run
modernpath process advance --all --piece REQ-X --log "npx vitest run"
modernpath process lane check REQ-X --commit <merged sha>
modernpath process lane complete --log <ci run>          # one lane-batch gate
modernpath process lane complete --apply                 # after the answer
```

- Only a workspace admin answers a lane authorization: in the web app, or with
  `process lane approve <gate> [--text <decision>]`, which answers it as the
  signed-in user. Admin is read from the roles in your sign-in token.
  `factory answer` on it is refused, and so is any client other than the web
  app and the CLI. The approver reads the terms as the server wrote them from
  what it enforces; any text the requester added follows as a note.
  `process lane` shows the current one.
- Set the SR's `lane_class` before the review, because the review pins it.
  The classes are `defect_with_failing_test`, `wording`, `presentation` and
  `dependency_patch`.
- `process lane review` and `process lane enter` take the SR as a `single_sr`
  work selection when you do not hold it. The server serves the SR's
  single-SR aggregate only for a held piece.
- `process lane check` reads the change as it reached the default branch: a
  merge or squash commit against its first parent, or `--base <sha>` for a
  rebase delivery. The server cannot read the repository: it judges the file
  list the CLI reports and records that verdict. The first full report for a
  commit counts; a later report for the same commit that leaves a file out or
  uses another base is refused. A DONE or OBSOLETE SR is refused. A FAIL takes
  the change out of the lane.
- `process lane complete --log` records the run you name as the run you
  report; nothing verifies it, and the approver's brief says so.
- In the lane-batch gate, the answer approves every change it does not reject
  with `reject:<SR>`. After the answer the batch is pinned to the approved
  changes only, so fixing a rejected one does not block them.

The subagent guard denies the lane's write verbs to delegated agents. The
kit's permission rules allow them, except `process lane authorize` and
`process lane approve`, which ask.

The subagent guard allows a delegated agent only the verbs listed as reads
(`process next`, `working-set pull`, `search` and the other reads) and denies
every other `modernpath` subcommand, so a new write verb is denied until it is
listed as a read. It reads the command as the shell will run it: quoted verbs
count, and a subcommand it cannot read literally (`$VAR`, `$(…)`, `eval`,
`xargs` input, an indirect path to the binary) is denied.

### Diagnose a connection

Start with:

```sh
modernpath env
modernpath env test
modernpath status
modernpath factory status
modernpath hooks doctor
```

Common repairs:

| Symptom | Repair |
|---|---|
| Authentication is absent or the server rejects the session | Run `modernpath auth` against the intended server. |
| Knowledge Core content is old | Run `modernpath docs sync`. |
| `factory status` reports no system | Run `modernpath init`, or deliberately bind with `factory connect --system <id>`. |
| `factory status` shows no local sync stamp | Set one with `factory release use <slug>`; this affects only later syncs. The server's shared active release is reported separately. |
| Hooks use an unexpected or stale binary | Run `modernpath hooks doctor`, update the binary it identifies, and reinstall the hooks if directed. |
| Local and server process state differ | Run `factory sync` in the foreground, then `factory reconcile`. |

Use `modernpath <command> --help` for the current flags and subcommands.

## Technical Description

### Source authority

Paths in this section are relative to the `modernpath-core` repository.

| Concern | Authoritative source |
|---|---|
| Command registration and global flags | `CODE:tools/modernpath/cmd/root.go:rootCmd` and each command file's `init` function |
| Authentication and credential writes | `CODE:tools/modernpath/cmd/auth.go:runAuth`, `CODE:tools/modernpath/internal/config/config.go:WriteAuth` |
| ZITADEL login: flow choice, loopback authorization code + PKCE, device flow | `CODE:tools/modernpath/internal/zitadel/login.go:Login`, `CODE:tools/modernpath/internal/zitadel/authcode.go:AuthCodeLogin`, `CODE:tools/modernpath/internal/zitadel/deviceflow.go:DeviceLogin`, `CODE:tools/modernpath/internal/browser/host.go:LoopbackUnavailable`, `CODE:tools/modernpath/internal/browser/launch.go:Open` |
| Repository initialization and Knowledge Core export | `CODE:tools/modernpath/cmd/init.go:runInit`, `CODE:tools/modernpath/cmd/init.go:finalizeSystemInit` |
| Binding discovery and persistence | `CODE:tools/modernpath/internal/config/config.go:FindConfigDir`, `CODE:tools/modernpath/internal/config/config.go:WriteConfig`, `CODE:tools/modernpath/internal/config/config.go:InitConfig` |
| Environment mutation | `CODE:tools/modernpath/cmd/env.go:setEnvironment` |
| Knowledge Core synchronization | `CODE:tools/modernpath/cmd/docs.go:runDocsSync` |
| Mission Control binding and synchronization | `CODE:tools/modernpath/cmd/factory.go:factoryEnvLoad`, `CODE:tools/modernpath/cmd/factory.go:factorySyncRun` |
| Requirement discovery | `CODE:tools/modernpath/cmd/requirements_list.go:runRequirementsList`, `CODE:apps/core/lib/core/compliance/requirement_discovery.ex:Core.Compliance.RequirementDiscovery.list/2`, `CODE:apps/core_http_api/lib/core_http_api/controllers/sync_api_controller.ex:requirement_discovery/2` |
| Server batch authorization and application | `CODE:apps/core_http_api/lib/core_http_api/controllers/sync_api_controller.ex:batch`, `CODE:apps/core_http_api/lib/core_http_api/controllers/sync_api_controller.ex:caller_identity` |
| Agent hooks | `CODE:tools/modernpath/cmd/hooks.go:runHooksInstall`, `CODE:tools/modernpath/cmd/hooks_pi.go:installPi`, `CODE:tools/modernpath/cmd/hooks_sync.go:syncHookCommand`, `CODE:tools/modernpath/cmd/hooks_doctor.go:runHooksDoctor` |
| Installers and release archives | `CODE:tools/modernpath/scripts/install.sh`, `CODE:tools/modernpath/scripts/install.ps1`, `CODE:tools/modernpath/scripts/build-all.sh` |

Generated `.modernpath` documents are useful orientation, but they are caches.
They are not evidence for CLI behavior when they disagree with the sources
above.

### Connection model

The CLI and browser UI are separate clients of the same server. The UI does not
read `.modernpath/` and the CLI does not connect to the browser.

```text
browser sign-in at ZITADEL
        |
        v
ZITADEL bearer -----------------+
                                 |
.modernpath/config.json          |
  server URL                     |
  system ID ---------------------+--> ModernPath API --> tenant/system rows
  current release (optional)     |                         |
                                 |                         v
repository records --------------+                  product UI views
```

A complete connection therefore has four independent coordinates:

1. server URL;
2. authenticated workspace identity;
3. system ID within that workspace;
4. current release when factory synchronization is release-scoped.

The server rejects a system outside the authenticated workspace as not found.
The bearer is a ZITADEL access token; its tenant claim becomes the request's
tenant context before the sync controller looks up the system. The credential
file records which workspace the token was issued for (`workspace_id`,
`workspace_name`) and which issuer issued it (`issuer`); `modernpath auth`
reads them to apply a stored choice on the same plane and to show the
workspace in `modernpath env`.

### Authentication and credentials

`modernpath auth` resolves the server in this order:

1. global `--api-url`;
2. `auth --local`;
3. the current repository config;
4. `https://api.modernpath.ai` (cloud production, the default).

Against a baked-in profile the CLI runs OAuth authorization code with PKCE:
it listens on a free `127.0.0.1` port at `/api/identity/callback`, opens the
authorization URL in the browser, and exchanges the code the browser brings
back. The whole sign-in is bounded by a five-minute timeout. When the
surroundings rule the browser out, or the listener or browser cannot start,
it falls back to the device flow: a device-authorization request, the
verification URL printed, and the token endpoint polled until approval or
expiry. Either flow returns access and refresh tokens; the CLI stores both,
although current command paths send the access token and do not
automatically exchange the stored refresh token.

`auth.json` is written with mode `0600`. Before writing it, the CLI ensures that
the bound `.modernpath/.gitignore` excludes the credential and refuses the
write if it cannot establish that protection.

Server-authenticated factory commands require the bearer in `auth.json`.
Credential-free local commands do not load authentication, and `next-id`
skips optional server enrichment when the bearer is absent.

`modernpath auth login` is not a registered subcommand. Use
`modernpath auth`; current Cobra argument handling may tolerate the extra word,
but it has no defined semantics.

### Repository-local state

The nearest `.modernpath/` directory at or above the current working directory
is the active binding:

| Path | Purpose | Persistence |
|---|---|---|
| `.modernpath/config.json` | Server, system, the upload repository `import --local` created, initialization mode, selected epic, last sync times, and current release. | Machine-local, gitignored. |
| `.modernpath/auth.json` | Workspace-scoped bearer and refresh token. | Secret, mode `0600`, gitignored. |
| `.modernpath/modernpath/` | Downloaded Knowledge Core export. | Regenerated cache. |
| `.modernpath/rdd/` | Versioned requirement-driven process snapshot. | Repository state; intended to be committed. |
| `.modernpath/cli-reference.md` | This binary's command reference — every verb, flag and default — rendered from its own command tree by `modernpath install`; `install --check` reports a stale copy as drift. | Repository state; intended to be committed. |
| `.modernpath/sync-hooks.log` | Automatic factory-sync outcomes and skips. | Machine-local log. |

Read-modify-write commands update the binding they found, even when invoked
from a subdirectory. `modernpath init` is the deliberate exception: it binds
the current directory through `InitConfig`, allowing an explicitly nested
workspace.

`modernpath init` constructs a new config from the selected system rather than
merging every prior field. Reinitializing therefore clears fields such as
`current_release`; a requirement-driven workspace must reselect its active
release afterward.

### Environment behavior

The named environments are URL aliases:

| Name | URL |
|---|---|
| `production` | `https://api.modernpath.ai` (cloud, default) |
| `test` | `https://api.workload.test-plat.modernpath.ai` (cloud test-plat) |
| `local` | `http://localhost:4000` |
| `custom` | User-supplied URL |

`modernpath env --set` changes `config.json.api_url` and, with it, the
system binding and the credential: the binding being left is stashed under
`config.json.environments.<name>` (the system and repository ids, the system
name and slug, the release) and the credential
under `auth.json.stash.<issuer>`, and whatever is stashed for the target is
restored. A custom URL with no known identity provider keeps the active
credential and says so. `modernpath status` and `modernpath factory status`
print the server, system and sign-in lines through one renderer, so they
always agree.

### Initialization and binding

`modernpath init`:

1. resolves the server and reads the repository bearer;
2. checks server health;
3. lists systems visible in the bearer's selected workspace;
4. resolves `--system-id`, `--system`, or an interactive selection;
5. downloads the system export;
6. extracts it under `.modernpath/`;
7. writes the system binding and ignore rules.

`modernpath factory connect` is narrower. It records a system ID and optional
API URL, then tries to refresh the system name and slug. The binding is saved
even when the identity lookup cannot reach the server. It does not download a
Knowledge Core export.

### Knowledge Core synchronization

`modernpath process prepare-inputs` is the combined pre-planning path. It
checks the bound credential, local kit and source freshness, and reads the
current server context with one request. It reports ready only when the system
binding is valid, the server contract is supported, exactly one active release
has an answered `USER:`-sourced selection, and the timestamp/context fields are
valid. It returns the held-piece count without listing piece details and keeps
pending decisions in the personal feed's order, including decisions addressed
to you.

The command does not refresh local files or change `.modernpath/config.json`.
It reads the export's server-generated `generated_at` from
`docs_push_manifest.json`; it never uses the client clock or shared config as a
fallback, so a sibling worktree's timestamp cannot claim freshness for this
checkout. Exports from older servers without this field show an unknown local
time. Both `modernpath init` and `modernpath docs sync` store the same server
timestamp in the export manifest and `last_sync_at` config field. A null server
update means the server has no documents; an unknown local time suggests an
explicit sync but does not make preparation fail. Invalid server timestamps
still make preparation not ready. Local and server times are displayed in UTC.
`docs sync` validates the archive, replaces the bound export and its exported
companion files, and rolls back if the config update fails. Existing legacy
exports under `.modernpath/docs/<slug>/` refresh in place; new exports also use
that layout when their name would collide with a CLI directory. The old root
command `modernpath sync` is not registered.

Preparation does not regenerate `working-set/GATES.md`, fetch the latest-
release cache, refresh credentials, or write process records. After a ready
result, `rg` and `cat` under `.modernpath/<system-slug>/` locate and read
available documents locally. API-backed `search` and `ask` remain available
for discovery and synthesis.

### Mission Control synchronization

`modernpath factory sync` loads the active binding and credential, parses the
repository into typed operations, validates the operation schema, and sends:

```http
POST /api/v1/sync/batch
Authorization: Bearer <workspace-scoped token>
Content-Type: application/json
```

```json
{
  "schema_version": 1,
  "system_id": 243,
  "release": "example-09",
  "ops": []
}
```

The `release` key is omitted when no current release is selected; the CLI warns
but permits an unscoped sync. The server tenant-guards the system lookup,
resolves or creates the named release, and applies operations sequentially.
Each operation has its own transaction. The first invalid operation stops the
batch, while previously applied operations remain idempotently replayable
through sync shadows.

The default parser recognizes the repository's requirement ledgers, epic
records, specification files, decision registers, work-list, commits, sessions,
and evidence. `factory manifest show` displays the selected document layout;
`factory manifest init` writes an explicit manifest for a nonstandard layout.

Useful verification paths:

```sh
modernpath factory sync --dry-run
modernpath factory sync --dry-run --json
modernpath factory sync
modernpath factory reconcile
```

`--dry-run` still parses and validates locally. `--json` prints the complete
batch that would be sent.

### Releases, pull, evidence, and sessions

`factory release use <slug>` writes a local `current_release` sync stamp into
the active config. It scopes later syncs but never activates a release or
changes the shared server selection. `factory release activate <slug>` changes
the server's active release for the bound system. It uses the compliance PIN:
use `--pin`, pipe one value with `--pin-stdin`, or answer the hidden prompt.
When no PIN exists, interactive activation performs hidden
confirm-match setup; non-interactive use must first run
`modernpath factory pin set --pin-stdin`. `--close-current` explicitly closes
incumbents, while `--reason` and optional `--source` are sent to the server.
The server composes the source when `--source` is absent and returns the actual
source and any closed releases.
If activation is refused because other releases are open, the refusal lists
each incumbent's name, slug and status. Retry with `--close-current` only when
you intend to close those releases as part of the activation.

The Releases UI can activate a slugless imported release by its ID. Activation
assigns a stable `release-<id>` slug (with a suffix if that slug is already in
use) and keeps the selected release ID stable; the CLI can then use
that slug for later activation.

Retrying the already-active release records or reuses its approval without
closing planned releases. A signed-off Validation Bundle must be reopened
before its release can be activated again. During the ownership migration,
`migration_pending` means the rollout must finish before that operation can run;
it does not authorize using another system's release.

`factory pull --apply` handles server-originated gate answers and edited
specifications. Specification conflicts are refused when both server and local
content changed since their last common hash.

`factory evidence` posts revision-pinned test evidence. Each invocation gets a
distinct run ID with a random suffix, so separate observations at the same
commit and timestamp cannot overwrite each other. The server still updates an
existing run when the same payload and run ID are replayed; rerunning the CLI
command records a new observation. `factory evidence --file` is the exception:
each entry's run ID is derived from the entry, so rerunning the file updates
the same runs and records only the ones that failed.

`factory drift` compares recorded evidence revisions with the current working tree.
`factory watch` maintains a server-visible session, synchronizes periodically,
pulls pending intents, and closes the session on interrupt.

### Working-set record reads

`modernpath working-set pull selection` reads a selection overview into
`WORK-SELECTION.md`. `WORK-SELECTION` and `WORK-SELECTION.md` are equivalent
aliases. It shows all parked work in the bound system with its holders,
reasons and blocker gate IDs. When several current pieces are held, it lists
them as ambiguous; it does not pick one or report that no work is current.
Use `--piece <id>` to read a named current selection. Scope-dependent reads
and writes retain their ambiguity refusals. The overview does not select,
resume or claim work, and an unavailable or unsupported read remains an error.

`working-set select <id> --suspend --reason "waiting for review" --waiting-on GATE-1`
stores the blocker shown in that overview. Suspend and resume preserve the
blocker when `--waiting-on` is omitted; pass `--waiting-on ""` to clear it,
or a new value to replace it. The closed suspension row retains its history.

`modernpath working-set pull <id>…` downloads only the named records and
their relevant gate history. The CLI uses the exact-item sync endpoint instead
of listing every Epic, requirement, gate and backlog record. It preserves
local edits by writing a conflicting fresh snapshot beside the existing file
as `.md.pulled`. Use `--include-candidates` explicitly to read a known
`DERIVED` requirement; pulling it does not confirm it.

Standalone gate snapshots and gate blocks attached to other records include
the same five decision-brief fields as the by-id terminal output, formatted as
Markdown. Partial briefs show only their available fields.

`working-set pull --scope` reads the selected record and its frozen member
IDs through the same bounded endpoint. Packet sections, findings and delivery
facts remain separate scoped reads. Trace authoring also reads the content
fingerprints of its named requirements directly.

`working-set check` verifies both the recorded store identity and the hash of
the snapshot body as it was written. It reports store staleness and local body
edits independently, including when both are present. It reads metadata only
from the snapshot header and hashes the body separately; malformed or
incomplete framing is unverified, and body text is never treated as metadata.
The check covers named root snapshots and `WORK-SELECTION.md`, not editable
scoped member files.
`working-set check --refresh` refreshes clean remote-stale snapshots in place.
When a local edit or unverified snapshot prevents a safe replacement, it
preserves the original, writes a fresh comparison copy beside it as `.pulled`,
continues checking other snapshots, and returns a non-zero result. A refresh
with only server changes succeeds after updating the snapshot.

`working-set pull --scope --for-review` creates a separate read-only snapshot
under `.modernpath/working-set-reviews/<scope>/<context-id>/`. It brackets
repeated selection, selected item, packet and required-section reads with the
scope aggregate and refuses unavailable aggregates or observed drift before
writing. It leaves authoring files and context unchanged and scaffolds no
missing sections. This is a bounded consistency check, not a server transaction.

Use the printed ID as `--review-context <id>` on `process review record`, `process findings add` or
`author trace --purpose cold-review`. The CLI validates the selected manifest,
store/system/scope identity, exact file hashes and snapshot digest before
posting. Its aggregate is the default pin; a conflicting `--aggregate`,
`--fingerprint` or a finding entry's `aggregate` refuses before any write.
Review recording retains the originally selected digest through its final
trace, refusing a replaced snapshot even if its manifest hashes are updated.
Disposition batches check that same digest before every update; a mismatch
stops the remaining writes. Restore the original snapshot before retrying;
already accepted dispositions are skipped.
The existing stored finding/trace body includes the
selected context and digest. Cold-review traces require explicit selection;
ordinary findings without a selector carry no inherited review context.
Actor attribution remains server-owned. Findings and process projections in a
snapshot are informational, and changes between bracketed reads can escape
detection if they change back before the next read.

Scoped authoring pulls stage required reads before changing local files. A
local hash baseline guards files the pull would replace or remove: an edited,
deleted or unbaselined managed file refuses the whole refresh. Unserved drafts
and untracked files remain in place. Packet 404 removes only verified clean
managed files; it does not recursively remove the packet directory. Server CAS
fingerprints still guard pushes independently of this local baseline.

If a push reports an error after accepting a write, preserve the scope and
retry `working-set push` (optionally inspect `--dry-run` first). When canonical
content already equals the authored content, retry repairs fingerprints and
baselines without rewriting the authored body. Concurrent local edits refuse
that metadata refresh. Missing legacy pins or accepted operation markers can
require manual recovery: rename the whole scope directory to a dated recovery
directory under `.modernpath/working-set/`, pull a fresh scope, then compare and
reapply wanted edits. Keep the recovery directory until the edits are verified.

After a successful patch, push refreshes the local file from the mutation's
canonical record and current fingerprint. The response retains the record kind
that was actually written, even if another record type shares its external ID.
When multiple records change, push can reread the exact changed set to include
effects from later writes in the
same push. It does not reload the system's Epic and requirement collections.

The CLI sends at most 100 unique IDs per exact-item request and keeps each
URL-encoded request target at most 7,000 bytes, including the API base path,
system ID and candidate option. Larger scopes use several requests, each with
its own server read snapshot. A server that lacks this endpoint is refused;
the CLI does not fall back to system-wide collection reads. Deploy the
compatible server before installing or publishing this CLI change.

Sources: `CODE:tools/modernpath/cmd/workingset_read.go:fetchDirectItems`,
`CODE:tools/modernpath/cmd/workingset_pull.go:workingSetPullScope`,
`CODE:tools/modernpath/cmd/author.go:readContentHashesFor`.

### Edit requirement citations

Replace an existing requirement's complete citation set with a JSON file:

```sh
modernpath author update SR-EXAMPLE-001 --citations-file citations.json \
  --expected-fingerprint '<current-content-fingerprint>'
```

For example, `citations.json` can combine a legacy process source with a typed
code citation:

```json
[
  {"kind": "process_source", "source_tag": "USER:2026-09-27:approved"},
  {"kind": "code", "ref": "src/service.ts", "source_file_id": "captured-source-id",
   "revision": "tested-revision", "sha256": "source-digest"}
]
```

The file replaces the whole set, so include every citation you intend to retain.
Supplied kinds, source IDs, revisions, hashes and document or locator metadata
are preserved. Legacy `process_source` citations can use `source_tag` or `id`.
A file containing `[]` clears the set; omitting both citation flags preserves it.
`--source` and `--citations-file` are mutually exclusive. Invalid input and
unreadable files are refused before an author write; backlog records do not
accept this flag. The edit uses the existing authenticated, fingerprint-guarded
endpoint and retains the requirement's lifecycle state.

### Automatic hooks

`modernpath hooks install` merges ModernPath-owned entries into supported agent
configuration without replacing project-owned hooks. Target a flavor with
`--cursor`, `--claude`, `--codex`, `--pi`, or `--all`; with no flag it installs
for every detected agent directory (`.cursor`, `.claude`, `.codex`, `.pi`).
It manages four independent families:

| Family | Behavior |
|---|---|
| Context | Runs `modernpath context --hook <event>` before a prompt where supported. |
| Process gate | Runs `modernpath check --hook PreToolUse` before shell execution where supported. |
| State sync | Detaches `modernpath factory sync --if-quiescent --trigger <event>` at supported session lifecycle events. |
| Session brief | Runs `modernpath your-move --hook SessionStart` at session start where supported. |

Cursor, Claude Code, and Codex store those families as hook JSON. Pi has no
Claude-style hooks file: `--pi` writes a generated TypeScript extension at
`.pi/extensions/modernpath.ts` that shells out to the installed CLI, and
merges a skills pointer at `../.claude/skills` into `.pi/settings.json`.
Pi loads project `.pi/` only after the project is trusted (`pi --approve`).

When `process/store-backed.md` is present, Pi installs omit retired file-derived
sync. Reinstalling with `modernpath hooks install --pi` removes old generated
sync while retaining context, process gates, the session brief and the skills
pointer; no `--no-sync` workaround is needed. File-backed Pi installs retain
sync by default.

In store-backed workspaces, `modernpath hooks status` and
`modernpath hooks doctor` report absent sync as `retired (store-backed)` for
Claude Code, Codex and Pi. Configured, partial or legacy sync produces a warning
with the matching reinstall command: `modernpath hooks install --claude`,
`modernpath hooks install --codex` or `modernpath hooks install --pi`.
For Pi legacy sync, the warning says to move `.pi/extensions/modernpath.ts`
to `.pi/modernpath.ts.bak` (or another unused path outside `.pi/extensions`)
first, then reinstall: that extension is not managed by ModernPath, and
reinstall replaces it unconditionally. Renaming a `.ts` or `.js` file inside
`.pi/extensions` leaves it discoverable by Pi. Store-backed Pi diagnostics
inspect the extension even when optional `.pi/settings.json` is absent.
Invalid configuration still produces a warning. File-backed diagnostics are
unchanged.

The quiescence path checks ledger coherence and operation validity, uses a
lockfile and minimum interval, logs one outcome, and never surfaces its network
failure to the coding-agent lifecycle. A successful detached hook is therefore
not proof that a particular change reached the server; use foreground sync and
reconciliation for consequential handoffs.

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
different endpoint. The endpoint inventory is grounded in
[`mcp-and-slack.md`](mcp-and-slack.md#cli-integration).

### Command surface

The current root commands are grouped below. Run each command's `--help` for
its exact flags and registered children.

| Area | Commands |
|---|---|
| Connection | `auth`, `init`, `env`, `status` |
| Knowledge | `search`, `ask`, `context`, `read-doc`, `read-file`, `docs`, `system-docs`, `datamodel` |
| Requirement-driven delivery | `install`, `check`, `coverage`, `factory`, `hooks` |
| Planning and development | `work`, `tasks`, `dev` |
| System creation and analysis | `new`, `import`, `source`, `analysis`, `scan`, `github` |

Important registered subcommands:

| Parent | Subcommands |
|---|---|
| `docs` | `sync`, `generate`, `refresh`, `preview`, `repair`, `cleanup`, `push` |
| `source` | `push` |
| `analysis` | `start`, `reanalyze`, `reset`, `status` |
| `factory` | `connect`, `status`, `release`, `sync`, `gates`, `answer`, `pull`, `evidence`, `drift`, `watch`, `reconcile`, `manifest`, `next-id`, `image` |
| `hooks` | `install`, `uninstall`, `status`, `doctor` |
| `author` | `apply`, `epic`, `requirement`, `update`, `relate`, `member`, `trace`, `gate`, `gate-withdraw`, `advance`, `demote`, `backlog` |
| `process` | `prepare-inputs`, `next`, `check`, `reconcile`, `enter`, `advance`, `complete`, `review record`, `findings`, `backlog`, `reapply-entry`, `reenter`, `supersede`, `cascade-mode` |
| `working-set` | `select`, `pull`, `push`, `check` |
| `requirements` | `list`, `search` |
| `epics` | `search` |
| `work` | `list`, `select`, `status`, `subtasks`, `new`, `derive`, `review`, `specs` |
| `dev` | `setup`, `list`, `run`, `task`, `ralph` |

Current source caveats:

- The registered one-shot development path is `modernpath dev run <tool>
  [task]`. The parent help still advertises `modernpath dev <tool> <prompt>`,
  but the parent has no `RunE` handler.
- The root `modernpath sync` command was removed; use `modernpath docs sync` or
  `modernpath factory sync`, depending on the direction and data.
- `modernpath init` currently tells an already initialized user to run the
  removed root `modernpath sync`; the correct Knowledge Core update command is
  `modernpath docs sync`.

### Build, test, and release

Contributor commands:

| Task | Command | Source |
|---|---|---|
| Build local binary | `cd tools/modernpath && go build -o modernpath .` | `CODE:tools/modernpath/go.mod` |
| Run the CLI suite | `cd tools/modernpath && go test ./...` | `CODE:.github/workflows/deploy.yml` |
| Build release archives | `cd tools/modernpath && bash scripts/build-all.sh [version]` | `CODE:tools/modernpath/scripts/build-all.sh` |
| Install the working tree for local testing | `cd tools/modernpath && bash scripts/install-local.sh` | `CODE:tools/modernpath/scripts/install-local.sh` |
| Refresh embedded process assets | `tools/modernpath/scripts/sync-rdd-assets.sh /path/to/req-driven-dev` | `CODE:tools/modernpath/scripts/sync-rdd-assets.sh` |

The release build targets Darwin amd64/arm64, Linux amd64/arm64, and Windows
amd64 when the required archive tools are available.
