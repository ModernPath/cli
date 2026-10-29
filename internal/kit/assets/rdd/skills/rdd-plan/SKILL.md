---
name: rdd-plan
description: Prepare and save a reviewable plan and proposed content for a human-selected Epic before implementation approval. Use for new or materially changed scope, acceptance, or technical context.
---

# Prepare the Epic plan

Start the saved plan with a short, plain-language summary of what will change,
why it is needed, and its expected impact on users and affected parts of the
product. Put this before the detailed scope, requirements and implementation
approach so the human can quickly understand the proposed change.

For a small change, follow `PROCESS.md` §Small changes; this Epic planning
procedure does not apply.

Read `PROCESS.md`, the selected Epic and its requirement links. Inspect
relevant product documents, code and tests; do not treat repository state as
process authority.

Before planning, read the Roadmap with `modernpath roadmap show`, which lists
each Initiative with the Epics it covers. Place the Epic's requirements in the
context of the Initiative its Epic is linked to, and name in the plan any
requirement that falls outside it.

If the plan already has an independent review, present the plan and that review
to the human through `rdd-entry-review`. Revise a reviewed plan only when the
human requests it. A requested revision gets one new review, then returns to
the human; do not start an autonomous revision-and-review loop.

A fix is a design change, and in practice fixes cause many of the findings of
the review that follows them. Before the new review of a requested revision:
- Make the smallest change that removes each finding. Add no name, folder,
  flag, general claim or record shape that the finding does not need.
- Treat a correction a reviewer suggests, and each option you offered the
  human, as a claim to check, not as a verified fix.
- Give every claim, path, name, tool and record shape a fix adds the same
  reconnaissance as the first draft: a question for the code, answered with
  file and line. Run any tool a fix names. Follow a changed record shape, such
  as a trace, an obligation, a test link or an id, through approval, evidence
  recording and completion; a passing plan-ready check does not show that it
  works there.
- Check each change for side effects: search the plan body, every requirement
  field, the criteria and the test titles (stored with the test cases, not in
  the plan) for the old wording, and inspect what else reads the changed
  behavior, including code that reads any path, folder or name the change
  introduces and the actual output of any tool it names.
- Then give the changes to a read-only agent that did not write them, to check
  each fix against the code in the same way, and correct what it finds. This
  check records no review.

1. Confirm the exact Epic, its lifecycle and intended outcome. Preserve
   IDs and identify included requirements and explicit exclusions.
   Create or revise its plan only while it is `PROPOSED`. Once implementation
   is approved, use `PROCESS.md` §Returning to planning with an explicit human
   decision before changing the plan. Read back PROPOSED and withdrawn approval,
   then prepare the revision for fresh independent review and implementation
   approval. DONE stays closed; changed work needs a follow-up Epic.
   Include one or more UR/SR requirements covering all in-scope
   behavior, with explicit criteria and planned code/test traces. Use each
   kind as applicable for Epic work; plan-only acceptance checks do not
   substitute for requirements.
2. Ground claims in source passages, code, tests or observed runs. Verify
   important source references by reading the source; a path alone is insufficient.
   Before writing the design, turn each claim it depends on into a question
   for the code, such as which transaction a write runs in and who calls it,
   and record the answer with file and line. Delegate many such reads to a
   read-only agent that returns the answers. Also search the stored requirements
   and the kit's own rules (`PROCESS.md`, the skills and the CLI reference) for
   anything the change would contradict.
3. Then, still before drafting, list the questions only the human can answer
   and ask them in one batch:
   - conflicts with stored requirements, documented behavior or the kit's own rules;
   - which entry points, callers and paths are in scope;
   - names for any new user-visible label or term;
   - behavior at the largest expected input, such as bulk operations,
     notification volume, locks and timeouts;
   - how earlier answers apply after later changes.

   If the prompt you ask through holds fewer questions than you have, ask the
   ones that change scope and put the rest in the plan as assumptions the human
   approves. Declare each question as a decision through the CLI and name it in
   the plan's verification decisions. Give each question a one-line title that
   names the choice, and put its alternatives in `options`, with the
   recommended one marked; the background stays in the question text. Mission
   Control shows the title on a short card and the rest when the card is opened.
   Record the answer after the plan save,
   because the server accepts an answer only for a decision the saved plan lists.
4. Inspect affected control/data flow, persistence, interfaces, integrations,
   failure modes, dependencies and project test gates. For shared or persisted
   state, identify actual writers, readers, existing shapes, partial-failure
   behavior and recovery. Read the planning contexts of the Epic's Release and
   Initiative again before planning (`epics get <id> --fields planning_contexts`),
   plan within them, and name in the plan the decisions and assumptions it
   relies on and any it conflicts with.
5. Define behavior boundaries, acceptance scenarios, code/test targets,
   expected RED assertions, focused and proportional regression checks,
   operational and security risks, and the requirement content that completion would publish.
6. Create or reuse test definitions before readiness review. Declare
   every active criterion as a verification obligation,
   with test identity, boundary and expected observation. Identify changed
   criteria requiring RED, applicable boundaries and exclusion reasons, author
   context, and required human decisions using IDs issued through the CLI.
   Retain each declared question beside its effective human answer.
   This creates test definitions, not executable tests before approval.
   Retain criterion identities when their behavior remains the same; link the
   TestCases required to verify each criterion.
   Write criteria as observable behavior, with kit or prompt wording checked
   by phrase tests. Gate only on deterministic tests; plan a model-dependent
   run as a measurement reported at the end.
7. Record open questions, conflicts, assumptions, deferred scope and risk in
   plain language. Do not invent a CLI command or local plan format.
8. Before the first review, check the draft:
   - every design claim cites a file and line you read;
   - each expected RED says what the code does at the first commit and which
     assertion fails because of it;
   - the plan states behavior at the largest expected input, and the
     privileges and bypasses of any database or security mechanism it relies on;
   - every screen, serializer and other reader of changed data is covered;
   - documented behavior the change alters is named.

   State each behavior once, in its criterion, and let requirement fields and
   the plan body refer to it instead of restating it. If the plan grows beyond
   about 15 criteria, offer the human a split.
9. Save the plan through the CLI and read back the exact
   saved content. Then use the CLI to mark each backlog, gap or tooling
   record the plan delivers as routed to the Epic, with a reference to this
   plan save, and read the record back. List those records in the plan body.
   Do the same when a later plan save adds a record. Obtain and record
   answers to declared decisions. Inspect the CLI's read-only phase readiness
   and correct structural blockers before retaining the review bundle; a missing
   independent review is expected at this point. Hand off for one independent
   review, then present the saved plan and findings to the human. Do not change
   tests or product code in this pass.

Report the Epic ID, saved plan and proposed content, material decisions, sources and the
next phase. If saving or reading back is unsupported, surface that tooling gap.
