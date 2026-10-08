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
| Source disposition, per repository | Eligible inventory files after documented exclusions | Distinct files cited by persisted rows, excluded with a reason, or unresolved |
| Governed and candidate linkage | The same inventory and mappings | Separate counts for confirmed links, candidate links and their overlap; no invented promotion |
| Reverse mapping | Every persisted requirement/source reference in the batch | Resolve inside authorized inventory or to an explicitly authorized existing identity |

State absent surfaces as inapplicable only after checking; unexplored surfaces
remain unresolved. A file disposition does not establish behavioral coverage,
and candidate links do not establish governed or verified behavior. Counts and
percentages describe the inventory; they do not define a passing gate.

Missing denominators, unresolved mappings, and inputs that do not match the
authorized inventory/read-back are incomplete measurements. Validate any checker
against known-good and known-bad inputs. Keep its command, output and exit code;
a successful citation audit cannot substitute for checking inventory completeness.

Report counts with their denominators, exclusions, unresolved references and
uncovered items for each inventory. Unresolved in-scope work remains incomplete.
Preserve meaningful requirement granularity and finish the requested authorized
scope; a row count is not permission to truncate a full sweep.

## When a run is finished

A run is finished when no file of its inventory is unresolved and every file
is linked to a system requirement, or assessed as reviewed or unsupported with
a reason. This is the completeness of the sweep over the run's files, not
verification of behavior, and it is not execution evidence. An unresolved
assessment states that work remains; it is never used to complete a count.

The finish condition is measured for one run. A link counts for it only when it
was made through a capture of the same repository key and file list, whichever
run made it, and a file cited only by a user requirement does not count as
linked. A
file the run's coverage shows with neither a governed nor a candidate link,
although a requirement from an earlier run covers it, is assessed as reviewed
with that requirement named in the reason.
