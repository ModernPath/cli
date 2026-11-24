---
name: rdd-modernize
description: Take a modernization through its stages, from recording the legacy requirements to closing the Release, and record how the legacy system behaves for each kept or changed legacy requirement. Use when an Initiative replaces part of a legacy system.
---

# Modernize a legacy system

This procedure gives the stages of a modernization and the steps an agent takes in each.
Read the project's `AGENTS.md`, `PROCESS.md` and current CLI reference first.
This procedure names no command: the commands are in the CLI reference.

## The stages

A modernization goes through five stages in order: Get ready, Plan
Initiatives, Deliver an Initiative, Prove and accept, and Close the Release.

- In Get ready, reverse engineering records the legacy requirements with
  `rdd-reverse-engineer`.
- Plan Initiatives has no skill of its own. `PROCESS.md` says how an agent
  suggests an Initiative's keep, change and drop rows and applies them on a
  person's word.
- In Deliver an Initiative, the capture steps below come first, and each Epic
  then runs the ordinary loop with `rdd-deliver`.
- Prove and accept and Close the Release have no skill of their own.

Stop where the business owner agrees the selection: the agent never agrees it.
The business owner agrees it with the Now plan on the Roadmap. Tell the person
what waits for that agreement, and continue only once it is recorded.

## Start the proof

Before the capture steps, do these three things in this order.

1. The agent commits the capture scripts to the legacy repository. A capture
   is a recording of the legacy system's behaviour, and the platform accepts a
   capture only when it was made at the reference revision: the commit of the
   legacy system that every proof compares against. So the scripts must be in
   the commit that becomes the reference revision. When the modernization goes
   onto a new system, the platform also accepts a capture made at a later
   commit of the legacy repository, if that commit is in its analysed history
   and the reference revision is in it too.
2. At that commit, the agent runs the legacy system's own tests and records
   their results under the Release with the capture command. The agent runs
   the command from a folder that is bound to the new system. When the
   modernization goes onto a new system, the agent also gives the command the
   path of the legacy checkout, so that the platform keeps the commit's
   message, author and date.
3. The agent asks a person on the team to choose that commit as the reference
   revision, on the Modernization tab of the Release page. No command can
   choose it.

## The capture steps

A capture is a recording of the legacy system's behaviour, not the source
capture the process removed. Record a capture only for a kept or changed
legacy requirement whose proof needs one. A replay test in the new system runs
the capture again and compares the new system's answer with it; its break test
gives the replay an altered copy of the capture and passes when the replay
reports the difference.

Record the legacy runs and the captures at a commit that is not a merge
commit, because the platform's history of the legacy repository holds no merge
commit, so it can place no later capture or legacy change against one.

### On the legacy system

1. Work from a folder bound to the legacy system: at the reference revision,
   or, onto a new system, at a later legacy revision for one requirement fixed
   on the legacy system; in place, at the reference revision before anything
   changes.
2. Capture where the legacy system's own tests run, on their test data and
   with their stand-ins for outside systems, never on production data.
3. Create one capture test case per requirement, trace it to its capture
   script and to the legacy files it exercises, and link it to the requirement
   with `relates`, never `verifies`.
4. Capture twice, naming the values that change on every run as masks before
   the second capture.
5. Write in the capture's procedure what it ran on: its starting data, its
   clock, the outside systems it stubbed with their recorded calls, and the
   class of the data, and where a stored capture lives when it stays outside
   the platform.

### Recording the capture

6. Record the second capture as the capture test case's run with the capture
   command, from a folder bound to the new system, which in place is the same
   folder, under the Release; the command passes when the two captures match.
   Give the capture test case's entry no criterion, because the platform takes
   such an entry only for a capture test case, and a capture test case that
   verifies a criterion stops being one. In place the folder holds the run's
   commit, so the command also sends the chain of commits the platform checks
   and keeps with the run; onto a new system the capture is recorded without a
   chain, and the command says so.
7. Link no capture that did not match, but tell the person. Read the capture
   back in the requirement's recording before you go on.

### On the new system

8. In the new system's repository, create the replay and its break test, and
   trace each to its files with `relates`: the replay to its test file, its
   stored capture, its masks and its data mapping.
9. Link both to the capture before either runs, so that the masks are named
   before the replay first fails. Work from a folder bound to the new system,
   check the links before you write them, and read them back.
10. When the capture runs again and the replay is linked to its newer passing
    result (a move), the platform clears the replay's current results, so run
    the replay and its break test again and record their results after the
    move. A replay keeps its break test's results when its link moves.

The replay and its break test are tests of an Epic of the Initiative. Keep the
replay's first failing run in the pull request's notes, and record the passing
outcomes of the replay and its break test with the evidence command as the
current results of the criterion they verify: onto a new system the new
requirement's criterion, in place the kept requirement's own.

## Differences

A difference is a place where the new system behaves differently from the
legacy system on a kept or changed legacy requirement.

- Record a difference with the backlog command, as a gap of kind difference
  that names the kept or changed legacy requirement, what changed and the
  evidence.
- Never accept a difference: a person on the team accepts it by name on the
  Initiative's Proof tab, in their own words, and sets its classification and
  whether it changes stored values there.
- Close a difference only by the done Epic that fixed it or by the merged pull
  request that fixed it, written PR #<number> or its address.
- In a mask added to a replay after its first failing run, cite an accepted
  difference by its reference, read in the Initiative's proof.

## Prove and accept

Once an Epic is done and its change is merged, prove the kept and changed
requirements it took or replayed on the merged code, then record the domain
expert's acceptance of the Initiative.

### Prove

- Record on the Epic the merge commit of the last pull request that carried
  the change, with the merge-commit command; recording it again replaces it.
- Run every test of the criteria of each kept or changed requirement the Epic
  took or replayed on the merged code, the replay and its break test among
  them.
- Capture their outcomes under the Release with the capture command, from a
  folder bound to the new system, in a full-depth checkout at the merge commit
  or at a commit that contains it, in runs of at most 50 criteria and 500
  tests, all the tests of one requirement in the same run.
- Also capture every test of the criteria of each kept requirement in the
  Release's scope that the completion presentation listed as touched by the
  change, its replay and break test among them. Put all the tests of one
  requirement in one run, with the runs that prove the Epic's requirements,
  or in runs of their own when the capture's limits of 500 tests and 50
  criteria in one run require it. A touched kept requirement outside the
  Release's scope (the legacy contexts the Release covers) is left out,
  because the capture refuses a whole run that holds one criterion outside
  it.
- Read the Initiative's proof with the proof command.
- Record an acceptance of a lower level of proof, with the
  accept-lower-level command, only on the person's explicit word, with the
  person's own words as the USER: source and as the words, never from a
  template; otherwise ask the person to accept the lower level on the
  Initiative's Proof tab.

Only a run of the modernized system captured at the commit that merged the
change, or at a commit whose verified chain of commits reaches it, counts, and
only when it holds a passing result for every test of the requirement's
criteria; the newest run that counts decides.

Until a run counts, the proof gives each row's reason in these words; each
says what to do next:

- "waiting for a run captured after {Epic}": the Epic is in progress or in
  review; finish it, and once it is done record its merge commit.
- "waiting for the merge commit to be recorded on {Epic}": record the Epic's
  merge commit with the merge-commit command.
- "waiting for a run at the merge commit or later": capture a run at the merge
  commit or at a commit that contains it.
- "does not contain the change: this run's commit does not contain the merge
  commit of {Epic}": capture a run at a commit that contains the merge commit.
- "the commits sent with this run do not reach the merge commit of {Epic};
  record a run at a commit that contains it, from a full-depth checkout":
  capture the run again from a full-depth checkout.
- "no chain of commits came with this run; record it again from a full-depth
  checkout": capture the run again from a full-depth checkout that holds its
  commit.
- "no result in the latest run captured after the change for {tests}": capture
  a run that holds every test of the requirement's criteria.
- "its test fails": fix the new system; when a replay fails because the new
  system behaves differently from the legacy system, record a difference.
- "no passing result": the criterion has no test, or a test of it neither
  passed nor failed; give it a test and capture a run in which it passes.
- "no run captured after the change yet": deliver the Epic whose saved plan
  takes or reaches the requirement.
- "not drafted yet": no Epic's saved plan takes or reaches the requirement
  yet.

A requirement that the done Epic only took, without a replay, reads Reviewed
only when an implementation review of the Epic names its criteria and no run
decides or the run lacks a passing result.

### Accept

- Refresh the Initiative's specs and open its acceptance report with the
  acceptance commands.
- Record each result only on the domain expert's word, with notes for Fail,
  Blocked and Not run.
- Sign the report off only with a USER: reference to the domain expert's
  decision as the source and their words as the note.
- A sign-off with a Fail on a requirement's spec raises a difference on each
  legacy requirement that the spec's requirement modernizes, with the notes as
  what changed; resolve it as the Differences section says.
- When a spec moved after the sign-off, open a new report with a new test.

Stop where the domain expert accepts the Initiative and where a person accepts
a difference: the agent records neither on its own. Tell the person what waits
for them, and continue only once it is recorded.
