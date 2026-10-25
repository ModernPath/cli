# Coverage reporting

When the user requests a coverage report, state the inspected repositories,
directories and behavior surfaces. Report requirements recorded, existing
requirements reused, source files traced, exclusions and unresolved work.

Read files before claiming their behavior is covered. A file link identifies a
source; it does not prove test execution or behavioral completeness. Counts
need an explicit denominator and a description of what was inspected.

Coverage is descriptive. Missing files, tests or unresolved questions do not
block recording grounded requirements and traces. Do not create a run, frozen
inventory, hash, source-capture requirement or passing coverage threshold.

`modernpath reverse-engineer check --repo <key>=<dir>` counts the tracked files
below a directory as linked, excluded or unresolved, and lists system
requirements without a verifies trace and user requirements without a serving
system requirement. A batch summary labels each context finished or
unfinished: finished when the unresolved file list is empty or every remaining
entry is named in its handoff with a reason, no surface unit is unresolved,
every user requirement has a serving system requirement, and the sample read
meets its thresholds. This label is descriptive, not a passing coverage
threshold: it blocks neither recording nor the batch.
