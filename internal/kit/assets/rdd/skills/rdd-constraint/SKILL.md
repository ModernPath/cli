---
name: rdd-constraint
description: Turn a rule in a person's words into a Proposed Engineering Constraint a reviewer can apply to a change. Use when a person states a rule about how the System is built; creates the Proposed record on their word and leaves acceptance to them.
---

# Record an Engineering Constraint

Read `PROCESS.md` and the project `AGENTS.md`. An Engineering Constraint is a
human-owned rule of the System, ref `EC-<number>`, Proposed until a person
accepts it and effective from accept to deactivate. The rule's text is the
product: no engine evaluates it, every pass reads it as written and every
review cites it as written. This skill is where a rule is made judgeable
before a person accepts it.

## Purpose and limits

Turn the rule as the person says it into a constraint a reviewer can apply to
a diff, and create the Proposed record on the person's word, nothing more. The
skill never accepts, never deactivates, never edits an Active rule's effective
text, and never writes from a delegated agent: a delegated agent drafts and
returns the file, and the main session runs the one write.

## Inputs

- The rule in the person's words.
- The System's Active and Proposed constraints: `modernpath constraints list`
  (`--state active`, `--state proposed`).
- The repository files the person names, such as agent instructions or an
  ADR, read for wording and for the reason.

## Steps, in order

1. Restate the rule in one sentence and confirm it with the person. A rule
   with "and" in it is usually two records.
2. Pick the shape with the person: target platform (a file and a value),
   sanctioned mechanism (a forbidden thing and its substitute), design rule
   (a question the reviewer asks) or change obligation (an artifact the
   change must carry). The shape is writing guidance, not a stored field.
3. Write the statement as a sentence a reviewer can test on a change, naming
   the thing forbidden or required.
4. Write the verification prose for the change, not for the codebase: what
   the reviewer looks at in the diff and how they decide. A rule that cannot
   be judged on a change goes back to the person as a question.
5. Write the reason in the person's words, naming the document when there is
   one, such as the ADR or the instruction file it comes from.
6. Check the list for a duplicate or a conflict and report what it found,
   with refs.
7. Show the constraint file and, on the person's word, run
   `modernpath constraints create --file <file>`. Read back the ref and the
   Proposed state with `modernpath constraints get <ref>`.
8. Draft the acceptance reason the person can reuse, and say that
   acceptance is the person's.

## Writing guidance per shape

| Shape | Write it as | Good | Bad, and why |
| --- | --- | --- | --- |
| Target platform | a named file and the value it must hold | "`.tool-versions` pins Elixir 1.19 and OTP 28; a change to it is a decision, not a bump." | "We run on modern Elixir." No file, no value, nothing to judge. |
| Sanctioned mechanism | the forbidden thing, the sanctioned one and what to scan for | "Caches are Nebulex caches. A diff that adds ETS or GenServer state holding request data is a cache in disguise; name the file and the Nebulex substitute." | "Use Nebulex for caching." Nothing forbidden, so nothing to find. |
| Design rule | the question the reviewer asks of the change | "Does every new or changed query name its columns, and is each preload used by the face that triggers it?" | "Keep queries efficient." An adjective, not a question. |
| Change obligation | the artifact the change must carry with it | "A change to a route under `/api/v1` updates `docs/07-api-contracts.md` in the same pull request." | "Keep the API docs current." No change, no artifact, no moment. |

## Refusals

The skill does not file:

- a rule it cannot make judgeable on a change: say which part no reviewer
  could decide on a diff, and ask for the file, the forbidden thing, the
  question or the artifact;
- a rule that restates an Active one: name the Active ref and ask whether the
  person wants a revision of that rule instead;
- a rule that contradicts an Active or Proposed one without the person
  deciding which stands: name both refs and ask which stands before any write.

In each case say why and ask; do not file a weaker rule to get past the
refusal.

## Interfaces

In: the rule, the list, the named files. Out: the constraint file and one
`constraints create`. `modernpath constraints create --file <file>` is the
skill's only store write. Accept and deactivate are the person's acts,
`modernpath constraints accept <ref> --reason "USER:<date>: ..."` and
`modernpath constraints deactivate <ref> --reason "USER:<date>: ..."`, and are
forbidden for the skill, as are editing an Active rule's effective text and
any store write from a delegated agent.

The constraint file is one JSON object:

```json
{"title": "...", "statement": "...", "rationale": "...", "verification_method": "..."}
```

`title`, `statement` and `verification_method` are required; `rationale` is
optional. Any other key is refused.

Report the ref and the state read back, the refs of any duplicate or conflict
found, and the drafted acceptance reason. Acceptance stays with the person.
