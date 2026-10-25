# Requirement quality

Apply these rules to every requirement written from existing code. A worker's
brief carries them in the context's own terms.

## User and system requirements

A user requirement is one actor reaching one outcome they would start on their
own and notice if it broke: add a payment card, see why a payment failed, start
a charging session. A user requirement whose scenarios cover outcomes the actor
would start separately is several user requirements. One user requirement per
context or subsystem is under-derivation, not a summary level. The actor is
the real role, traced from who calls the entry point: a customer call through
a gateway is the customer's, a service-to-service call is that service's. A
call with no outside caller is a system requirement.

A system requirement is one boundary's behavior serving a user requirement,
with criteria a test could check. A user requirement is served by every system
requirement its outcome needs. A user requirement with no serving system
requirement is unfinished. A mechanism several contexts share, such as sign-in
or a client library, is one system requirement, owned by one context, serving
each context's user requirements.

## Where the requirements come from

Derive from the surface inventory, never from a count. The inventory lists
each surface unit an actor reaches: screens and dialogs, actions that change
state, API operations, notifications the actor receives, scheduled outcomes,
and on a service every controller, queue or message processor, timer and
request filter. Each surface unit is covered by a named user requirement,
technical with a reason, or unresolved. A number someone names is a signal to
check the inventory, never a quota and never a cap.

## Writing from the code

- A script may enumerate and group; it never writes a title, statement, actor,
  outcome or scenario. Text made by filling a template is not a requirement.
- A UI control is a step inside the outcome it serves, not an outcome. A
  method, handler, view or test name is never a title.
- One outcome across its layers is one user requirement; the layers are its
  system requirements. A guard or validation is a criterion of the outcome it
  guards.
- Titles use the domain's words. Scenarios carry the code's rules:
  preconditions, limits, refusals and what the actor sees.
- State the scope boundary: the layers outside the scope and the rules the code
  relies on them for. Never claim behavior the scope does not hold.
- Every side effect is a criterion with its trigger: a message sent, an event
  published, a session ended, a provider called.
- As built, not as intended. Where the code differs from evident intent, state
  what it does and add it to the open questions.
- State limits, intervals and timeouts with units and where they are
  configured. Give each platform its own scenario.
- Live code only. Obsolete or unreachable code is an open question.
- Name an actor or caller only when you found the code that calls in;
  otherwise state the scope boundary. An event type a service accepts does
  not show that anything sends it.
- Every refusal path of a handler is a criterion, and so is every event or
  record it saves, including one saved before a check.
- Follow each call a handler makes one level down and list what it throws or
  refuses: a validator, a lookup that throws when two rows match, a resolver,
  a provider or push call. Each is a criterion, and so is a failure the handler
  does not catch: state what is already saved when it fails and what the caller
  sees.
- A validator or guard is a criterion of the outcome it guards, never a
  requirement of its own. An endpoint or controller is never a record of its
  own either: its authorization, body and validation checks and its request
  mapping are criteria of the outcome the handler behind it produces. A set of
  options is a limit of the outcomes it controls, never a requirement of its
  own.

## Citations and tests

A citation is a file where the stated behavior is implemented or tested, not a
migration, wiring or a whole folder. A cited test asserts the stated rule; a
test that only reaches a screen verifies nothing. An outcome found only in a
test is traced to the production code the test calls.

## Identities and links

Every requirement and criterion carries a UUID in the staged file, so a rerun
reuses it instead of creating a duplicate. Read a stored requirement against
the code before reusing it, with the verdict rubric below. Reuse a good one,
and a record the batch plan names as kept, through a reference entry: only its
`kind` and `id`. Replace a weak, wrong or not-a-requirement record only when it
is PROPOSED with no Epic owner: stage a replacement with new UUIDs and name the
stored id it replaces. The replacement carries the derives traces the stored
record had. After the replacement is recorded, the coordinator asks to make
the stored record obsolete (`modernpath requirements obsolete <id> --kind user|system --reason "replaced by <new id>"`), and reports the refusal verbatim when that is
refused. Never replace the rest: a published, blocked, deferred or Epic-owned
record that fails is reused by reference and listed in the open questions, and
a failing kept record is listed in the open questions too. A
sweep that only reuses never improves an earlier sweep's records.

Link a user requirement to each serving system requirement with `derives`, a
system requirement to its code with `implements`, and a test to what it checks
with `verifies`: a verifies trace has source_type `source_file` (the test
file) and target_type `system_requirement`. Use the repository key the store
already uses for the repository, read from a stored trace with `modernpath
traces list`, so a file keeps one identity.

## Check before recording

Run `modernpath reverse-engineer check` on the staged files and rewrite what
fails from the code before recording. The store does not enforce this.

## Independent sample read

After a context is staged or recorded, a reviewer that wrote none of it reads
at least 20 records of a context, or all of them when it has fewer, against the
code they cite. Each record gets one verdict: good, weak, not a requirement, or
wrong. The sample fails on any not-a-requirement or wrong verdict, or on more
than one weak in ten. A record the writer fixed gets a new read.

Verdict rubric:

- good states what the code does with its refusals, saved effects and limits.
- weak is true but omits a refusal, side effect, unit or limit the code has, or cites a file that does not implement it.
- not a requirement is a step, a guard, an option set or a restated function.
- wrong contradicts the code or names a caller no code shows.
