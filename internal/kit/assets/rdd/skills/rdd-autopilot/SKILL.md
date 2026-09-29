---
name: rdd-autopilot
description: Build a larger scope — Epics, an initiative, or a greenfield system — under a human's autopilot grant, keeping requirements, sources, and red-first tests while moving human approval to the end of the sprint. Use when a current autopilot grant covers the scope, or when the human asks to start one. Asks product questions without waiting on them, clears or logs process refusals instead of stopping, and ends the sprint with one independent review and one completion page.
---

# Run an autopilot sprint

Read the project `AGENTS.md` and canonical `PROCESS.md`
(`.modernpath/rdd/PROCESS.md` in a consuming repository), especially
§Autopilot. `PROCESS.md` owns the rules; this skill owns the order. The phase
skills still describe how each pass is done. Where their stops and round
bounds differ from §Autopilot, §Autopilot applies.

## Start

1. **Find the grant.** Read the grant that covers the scope and check that it
   is current. Without one, ask the human for it in one message that names:
   - the scope;
   - the sprint end date;
   - any excluded areas;
   - what the human gives up: per-Epic entry approval and waiting on
     questions;
   - what the human keeps: the completion answer at sprint end, and merges.

   Record the answer as §Autopilot says.
2. **Run `rdd-start`'s preflight, without stopping on it.** Clear each
   preflight fact with its documented verb: reconcile, refresh, or release a
   selection this sprint does not need. If a fact will not clear, log it and
   go on. Only a missing or unreachable store stops the sprint.
3. **Order the work.** List the scope's Epics and requirements in build order:
   dependencies first, then what unblocks the most. Show the list in one
   message and start.

## Loop, per Epic or single SR

1. **Plan.** Write or complete the requirements with their sources (`rdd-plan`),
   including reconnaissance and a RED strategy.
2. **Review.** Run one cold review from an independent context
   (`rdd-cold-review`), and record its findings.
   - Fold each material finding about the change into the packet as a test
     or a boundary line, and resolve it as a packet edit.
   - Log the rest.
   - If the review failed, ask the same independent context for one narrow
     confirmation that the folded findings close it, and record that verdict.
   - If it still fails, log it and build the item ahead of its record.
   - No further rounds.
3. **Enter.** Answer the entry gate with the grant's `USER:` source, and apply
   it.
4. **Build.** Build red-first (`rdd-build`): RED evidence, GREEN, cleanup,
   evidence recorded, and requirements advanced to `IN_REVIEW`.
5. **Commit and push** the feature branch at each waypoint. Never merge.
6. Go to the next item without waiting.

## When something refuses

1. Read the refusal. Run the verb it names, or the one `mp-process-cli` (or
   the project's store guide) lists for it, **once**.
2. If it still refuses:
   - file a gap record naming the sprint, the item, the refusal text, and
     what was tried;
   - if the code is ready, keep building it on the branch ahead of its record;
   - move to the next item.
3. Never write the store by hand, never retry the same refused call in a
   loop, and never widen a grant's scope to get past a refusal.

## When a product question comes up

1. Ask it in one plain sentence, with the options and what each changes.
2. If a wrong guess is hard to undo (existing data, security, money, outside
   contracts), park that item and build others until the human answers.
3. Otherwise record the most reasonable default in the Epic's decisions as
   *assumed under autopilot, pending confirmation*, and continue.
4. When the human answers later, apply it. A changed assumption is triaged:
   fix it now if the item is still being built, otherwise queue it for the
   next sprint.

## Sprint end

Run it at the grant's end date, when the scope is built, or when the human
asks.

1. **Reconcile** every record the sprint touched. Try once more to clear the
   logged refusals.
2. **Review.** Run one independent review of everything the sprint built, from
   the code at the branch head, against its requirements and tests
   (`rdd-completion-review`'s audit). Record the findings.
3. **Present one page:**
   - built, with evidence;
   - assumed decisions to confirm;
   - the log;
   - review findings;
   - what is not built and why.

   Give links to the working-set files.
4. **Apply** the human's completion answers item by item. A rejected item
   stays `IN_REVIEW`. A changed assumption goes to `rdd-triage`.
5. **Report** whether the grant continues into another sprint, or ends.

## Report

During the sprint, one line per finished item: its id, RED and GREEN commits,
and anything assumed or logged. At sprint end, the page above.
