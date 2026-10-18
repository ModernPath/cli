# Coverage and citation checks

Use these checks for the exact authorized repositories and behavior scope.
Retain the frozen inventory and persisted read-back used for each measurement.
A narrow request is measured over that scope; a full sweep must include every
authorized context. Do not shrink a denominator to make a result pass.

## Citation validity

Use `rdd-audit` and its supplied `audit-citations.mjs` for citations in recovered
local documents. It checks cited file resolution and a minimum recognized
citation **count** (`--min=N`). It does not enforce percentage coverage. Historical
source availability is checked through the sanctioned immutable source reader;
store citations and links use the store's typed contract, not a retired ledger scan.

Unresolved or ambiguous citations fail this check. Zero recognized citations over
nonempty expected input is a failed measurement, not 100% coverage. Report a
missing local-document corpus as not applicable when only store records are in
scope; still validate their captured identities and links.

## Extraction and source coverage

Define each inventory's unit, scope and enumeration method before counting it.
Use the frozen inventory and authoritative read-back. Retain enumeration and
matching scripts and their actual output when used; these are audit artifacts,
not product tests or another requirement store.

Record stable unit identities and mappings so the checker can report:

| Measurement | Denominator | Numerator / required result |
|---|---|---|
| Requirement extraction | Enumerated in-scope items, such as routes or jobs | Account for every item as extracted, already covered, excluded with a reason, or unresolved; cite the supporting records |
| User outcome extraction, per context | The surface units of the skill's surface inventory: screens, views and dialogs, state-changing actions, actor-facing operations, notifications, observed scheduled outcomes, actor-behavior test cases | Every unit covered by a named UR stating its outcome, technical with a reason, or unresolved; report units per UR, and list each UR whose units span outcomes a user starts separately |
| Source disposition, per repository | Eligible inventory files after documented exclusions | Distinct files cited by persisted rows, excluded with a reason, or unresolved |
| Governed and candidate linkage | The same inventory and mappings | Separate counts for confirmed links, candidate links and their overlap; no invented promotion |
| Production-code provenance | Every baselined requirement | Cites the production code implementing its behavior; one that does not is DERIVED with a reason |
| Test citation | Requirements per context whose code has tests | Cite a test of their behavior; report the rest per context and requirement |
| Executed verification | Requirements in the run | None in this pass; a cited test is never counted here |
| Reverse mapping | Every persisted requirement/source reference in the batch | Resolve inside authorized inventory or to an explicitly authorized existing identity |

A file is assessed by reading it. Fetching its bytes with a script is not
reading it. A script may group files by path and propose
a reason, but each reason is checked against the file before it is recorded,
and a file no rule matched stays unresolved: a default reason such as "outside
the described behavior" is never assigned to a file nobody read. A file that
sets behavior a requirement states — options validation, serialization,
deployment settings such as schedules, retries and message lifetimes — is
linked to that requirement, not assessed as unsupported. A reason is written
for its file and says what the file is; one sentence filled in with each
file's path or digest is a generated reason, however many files it covers.
A file assessed unresolved has not been assessed: report it with the
unassessed files as remaining work, and never assess a file of the context's
own repositories as unresolved to bring the unassessed count to zero. A link
made later does not clear an unresolved assessment: assess the linked file
again as reviewed, naming the requirement that cites it.

State absent surfaces as inapplicable only after checking; unexplored surfaces
remain unresolved. A file disposition does not establish behavioral coverage,
and candidate links do not establish governed or verified behavior. Counts and
percentages describe the inventory; they do not define a passing gate.

An excluded subtree is not enumerated, so the count of excluded files is
unknown whenever a subtree is excluded. That is not remaining work and not
part of the finish condition: report each excluded subtree with a reason that
says what it holds, such as build output or editor settings. A subtree is
excluded for what it is, never because the run did not read it.

Missing denominators, unresolved mappings, and inputs that do not match the
authorized inventory/read-back are incomplete measurements. Validate any checker
against known-good and known-bad inputs. Keep its command, output and exit code;
a successful citation audit cannot substitute for checking inventory completeness.

Report counts with their denominators, exclusions, unresolved references and
uncovered items for each inventory. Unresolved in-scope work remains incomplete.
Preserve meaningful requirement granularity and finish the requested authorized
scope; a row count is not permission to truncate a full sweep.

## Content quality

Coverage counts say nothing about whether a requirement says anything. Run
these checks on every group before publication and on the persisted read-back
after it, and retain the script, its output and the sample read:

| Check | Fails when |
|---|---|
| Distinct statements | Two requirements share a statement, or share it once their title words are removed: the mark of a sentence template |
| Template scenarios | A scenario reads the same once its noun is replaced, across more than a handful of requirements |
| Actor and outcome | A UR has no actor or no outcome, or its actor is a catch-all for several roles |
| Domain titles | A title is a split identifier, a UI control or a test-name fragment, not an outcome the product's users would name |
| UR–SR join | A UR has no serving SR, or one SR serves URs of unrelated outcomes |
| Citation precision | A requirement cites files where its stated behavior is not implemented or tested, such as migrations, wiring, telemetry or a whole folder, or a test of another behavior |
| Fragmentation | One screen, one flow across layers or one guard rule appears as several URs |
| Actor traced | A UR's actor is not the traced caller of its entry point, or an internal call is a UR |
| Live code | A requirement cites obsolete, retired or unreachable code as live behavior |
| Side effects | A handler's outbound effect appears in no acceptance criterion |
| As built | A requirement states intended behavior where the code does something else |
| Entry points | A controller, function, queue processor, timer or request filter in scope has no requirement and no technical reason |
| Capture completeness | The scope is a hand-picked file list, or a requirement calls a fact unverified, unresolved or outside the boundary while the file that settles it exists in a repository the run reads |
| Scope boundary | The end report does not name the layers outside the scope and the rules the scope relies on them for |
| Read against code | For a random sample of at least 20 requirements per context, or every one when there are fewer, the code does not do what the requirement states, the requirement states nothing a rebuild could implement or test, its side effects are missing, or the context is wrong. The read is made by an independent reviewer that did not write the requirements, such as a delegated cold review with read-only access; the session records its verdicts. The writing session's own read is the minimum before publication; the finish condition needs the independent read, and a harness without a delegated reviewer leaves the run unfinished until one has read the sample. |

The sample read gives each requirement one verdict: good, weak, not a
requirement, or wrong. Any requirement that is not a requirement or is wrong
fails the group, and so do weak verdicts on more than one in ten of the
sample. Report the verdict totals.

Validate each mechanical check against a known-bad input. A requirement that
fails is rewritten from the code, never patched by changing its template. It
is rewritten in place, under its own identifier. A new requirement that
restates an existing one while the existing one stays is a duplicate, and
fails the distinct statements check. Rewrite the existing one, or ask for its
retirement before adding a replacement.

## When a run is finished

A run is finished when no file of its inventory is unresolved and every file
is linked to a system requirement, or assessed as reviewed or unsupported with
a reason, and when no surface unit of its user outcome extraction is
unresolved and no UR spans outcomes a user starts separately, and when every
content quality check passes on the persisted records. File linkage
alone is never the finish: a few broad requirements can cite every file. This
is the completeness of the sweep over the run's files and surfaces, not
verification of behavior, and it is not execution evidence. An unresolved
assessment states that work remains; it is never used to complete a count.

The finish condition is measured for one run. A link counts for it only when it
was made through a capture of the same repository key and file list, whichever
run made it, and a file cited only by a user requirement does not count as
linked. A
file the run's coverage shows with neither a governed nor a candidate link,
although a requirement from an earlier run covers it, is assessed as reviewed
with that requirement named in the reason. That assessment covers the file, not
its surface units: a unit whose outcome the earlier requirement does not state
is still extracted.

A captured file outside the repositories and directories its context owns,
which none of the context's requirements cites, is assessed as reviewed with
a reason in the ownership form, `Owned by <context code>, run <run key>: <why
this context does not cite it>`; the owning context's run reads it and gives
it its own disposition. The ownership form is the one reason that repeats
across files. A file no context owns, such as a shared library outside every
scope, is read by the context that captured it. A file a requirement of the
context cites is linked whoever owns it.

A context is finished when its latest run meets the finish condition. A
publication cites only its own run's captures, so a few files the context's
runs left out, such as a gateway definition, are captured by an extra run
scoped to them, and their behavior is a requirement of that run, citing only
them, which the earlier requirements name. The context is then finished when
each of those runs meets the finish condition. A later
run of the same context whose capture holds the whole repositories or
directories an earlier run left out supersedes that earlier run's capture
completeness check: the report names the earlier run as superseded by the
later one, not as failed, and the earlier run's other measurements stand.
Links are never moved from one run's capture to another to repair an
inventory that was fixed at its capture.
