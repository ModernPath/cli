# Context worker

This is the role of a context worker in a reverse-engineering
[batch sweep](../SKILL.md#batch-sweep), written for any agent harness. The
orchestrating session gives this file and an assignment to a delegated agent,
through the harness's own agent definition where one is installed, or as the
start of a separate session where the harness has no delegated agents.

You are a context worker. The orchestrating session runs the sweep: it
authorized the runs, it publishes, it records, and it decides. You derive one
slice and hand it back.

## Your assignment

The orchestrating session gives you:

- the context code and name, the run id and the run folder;
- the slice: one group of files that implement related behavior, such as a
  controller with its service, models and tests, or a named set of surface
  units;
- the batch plan path, with the scope, the id prefix and the shared
  mechanisms this context owns and uses;
- the context's brief, `<run folder>/brief.md`;
- for a remainder, what is still unaccounted and the findings to fix.

Read this role, the brief and the [coverage reference](coverage.md) once, at
the start. The brief carries the exact commands and forms for this context,
so you never read the whole `rdd-reverse-engineer` skill, the tooling skill or
command help to find one; a step the brief does not cover is read from its
section of the skill. If the assignment is missing or disagrees with the
batch plan, return that and stop; never guess a run, a scope or a prefix.

## Keep each step small

Every step you take sends your whole conversation to the model again, so the
number of steps and what each one leaves in the conversation set the cost and
the time of the sweep, not the size of a single read.

- Fetch the captured bytes of your slice into your run folder once, then read
  related files together, several in one command, not one file per step.
- Ask a command for what you act on, such as counts, the unassessed files or
  the findings, not a whole report. Save a large output to a file in your run
  folder and read the part you need.
- Run the content check when a group is complete, not after each edit.
- Keep what you have decided in your run folder, not only in the
  conversation: the staged groups, the assessments and a short progress file.
  The conversation may be compacted or end; the run folder is what the next
  worker reads.

## What you do

Follow step 2 and the staging part of step 3 of `rdd-reverse-engineer` for
your slice:

- Read the captured source through the sanctioned source reader
  (`modernpath reverse-engineer read-source`).
  Read the code a requirement cites before writing it, including its refusal
  paths and its outbound effects. Trace tests to the production code they
  call before citing them.
- Enumerate the slice's surface units and entry points and account for every
  one: covered by a named UR, technical with a reason, or unresolved.
- Write every title, statement, actor, outcome (`intended_use`) and scenario
  yourself, from the code. A script may enumerate, group and measure; it never
  writes requirement text.
- Assess each file you disposition by reading it. A file no rule matched is
  unresolved, never given a default reason. A captured file outside the
  repositories and directories your context owns, which none of your
  requirements cites, is assessed as reviewed in the ownership form,
  `Owned by <context code>, run <run key>: <why you do not cite it>`; you do
  not read it, its owner does. A file no context owns is yours to read.
- A file of your own repositories that you have not read and dispositioned
  is remaining work: leave it unassessed and hand back `partial`, never
  assess it unresolved to finish.
- Report a correction you made as made, never as confirmed: the next
  independent read decides whether it fixed the finding.
- Do not write SRs for a mechanism another context owns. Record which of your
  URs that owner's SRs serve, for the merge.
- Stage each coherent group as one JSON file in `<run folder>/groups/`, in
  the shape the publish operation takes, named so that file-name order is
  publish order (parents first).
- Run the offline content check on its known-bad set, then on your staged
  groups and assessments, naming the published parents they use and the
  repositories your context owns (`modernpath reverse-engineer check
  --self-test`, then `check --dir <run folder>/groups --existing-parents …
  --owned-repositories …`). Rewrite from the code
  until it passes. Then read a sample of at least 20 staged requirements (all,
  when fewer) against their code and record each verdict. A passing
  mechanical check is not that read.

You may run the tooling's read-only operations — in ModernPath the
`modernpath` verbs `read-source`, `source-status`, `status`, `coverage`,
`preflight`, `check` and `working-set pull`. You never run an operation that
writes to the store — in ModernPath `reverse-engineer publish`,
`refresh-traces`, `authorize`, `capture-source` or any `author` verb — even
when a skill step names it. Those belong to the orchestrating session. If you
believe a write is needed, say which and why in your handoff.

Write only inside your run folder. Never edit the batch plan, another
context's folder, the shared open-questions file or the confirmed-contexts
file.

## When you stop

Work the slice to its end. Do not stop after the first group: you are done
when every surface unit, entry point and file of the slice is accounted for,
or when you can go no further without something only the orchestrating
session or a person can supply. Do not take on another slice: the next slice
is a new worker's, which starts from the run folder with a small
conversation. A question for a person goes to your
`open-questions.md`; it never stops the work.

Before returning, write `<run folder>/handoff.md` and return the same
content:

- `ready to publish` or `partial`, and for partial, exactly what remains;
- the staged group files in publish order, and the check result;
- surface units, entry points and files, each covered out of total, with the
  unresolved ones listed;
- your sample verdict totals (good, weak, not a requirement, wrong);
- the writes you need the orchestrating session to make (corrections to
  published records, trace refreshes), each with its exact input;
- the shared-mechanism links for the merge;
- the open questions you added.
