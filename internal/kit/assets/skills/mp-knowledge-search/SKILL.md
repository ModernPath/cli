---
name: mp-knowledge-search
description: Find and read the local knowledge-core export before inspecting code. Use when writing about the codebase or locating a subsystem; use live search or read-doc when the export lacks material or a current server answer is needed.
---

# Reading the knowledge core

<!-- TOOL-OWNED. Installed by `modernpath install`. -->

The platform has already analyzed this repository. Its subsystem and module
documentation, data model, and file analyses are derived from the code. In a
bound workspace, `modernpath process prepare-inputs` checks the delivery
context and reports local documentation sync and server update timestamps before
sourced work. It does not refresh the export. Run `modernpath docs sync`
explicitly when a refresh is needed.

## Search locally first

Search locally first, in this order, and stop at the step that answers:

1. **The repository.** Search the code and tests for the requirement id and the
   names you are working with — `rg -n "REQ-CROSS-12" .` finds the code, the
   tests and the records that cite it. The code decides.
2. **The requirement records.** `modernpath working-set pull <id>` materializes
   the requirement, its epic and its traces as readable files.
3. **The docs export.** Find documents under `.modernpath/<system-slug>/` with
   `rg`, then read the matching files directly:

   ```sh
   rg -n "authentication" .modernpath/<system-slug>/
   cat <matching-document>
   ```

4. **`modernpath ask "<question>"`** for why and how questions the local search
   cannot answer. It costs a model call; the steps above do not.

These local reads need no additional ModernPath API call. Use the live commands
below when the export lacks the material or the answer must reflect current
server state. The local export is a cache; if it and the API disagree, the API
is right.

## Live commands when needed

| Command | Use it for |
|---|---|
| `modernpath search "<terms>"` | find documents and files absent from the export, or get live pointers |
| `modernpath read-doc --id=<id>` | read a generated document in full from the server |
| `modernpath ask "<question>"` | get a synthesized live answer when document reads are insufficient |
| `modernpath read-file <path>` | read an analyzed source file through the API when the local source is unavailable |

`search` takes `--docs-only`, `--files-only`, `--limit`. `ask` takes
`--format=markdown` when you want output you can paste.

## When the export lacks material

`modernpath read-doc --list` lists the server's generated documents with their
ids, titles, and summaries. Use it when local lookup cannot find the material,
then `read-doc --id=<id>` for the document you need. `--tier` (`module`,
`subsystem`, `architecture`) and `--angle` (`architecture`, `api`, `data`) narrow
the list.

Enumeration can help when vocabulary differs: a query for
`organisation` returned nothing on a system whose documents say `organization`
throughout. A local filename or content search is the first check after
preparation; a server list is useful if that check misses relevant material.

`modernpath ask` also takes `--brief` (answer only, no sources) and
`--iterations N` (1–10, default 5) when a question needs more or less digging.

## How to query so it actually returns something

**Search is keyword-shaped, not semantic.** Observed :
`"authentication"` returned five subsystem documents; `"authorization roles
permissions"` returned **nothing**. A three-word phrase is not a better query, it
is a narrower one.

1. **Probe with single strong nouns** — `authentication`, `scheduler`, `tenancy`,
   `retry`. One concept per query.
2. **Try the domain's own words too.** A codebase says `organisation` where you
   think `tenant`, `need` where you think `requirement`. If a query returns
   nothing, the vocabulary is usually the reason, not the absence of the thing.
3. **Use `ask` when you want the shape**, `search` when you want the locations.
   `ask` costs a model call; `search` does not.
4. **Empty is a signal, not an answer.** It means "not indexed under that word" —
   never "not present in the codebase". Confirm in the code before writing that
   something does not exist.

## The rule that keeps this honest

**The knowledge core proposes; the code decides.**

Generated documentation describes modules rather than lines, and it can be stale
relative to the working tree. So:

- **Cite the code, not the search result.** A claim's evidence is
  `CODE:path:line`. Use `DOC:` only for a document you actually opened, and only
  where the document itself is the artefact in question.
- **Never write a fact you found only in the knowledge core.** Verify it in the file. A pass once wrote *"no deployment target is described in
  this repository"* from a plausible reading; the repository described one
  plainly in a file the pass had not opened.
- **If the export and the API disagree, the API is right.** The local export
  under `.modernpath/<system-slug>/` is a cache, and a stale cache answers
  confidently. Refresh it with `modernpath docs sync`.

## The local export

`.modernpath/<system-slug>/` holds the analysis as files — free to read, whole
documents rather than excerpts. Use `rg` to find and file reads to inspect
them; fall back to `search` or `read-doc` for material not exported or when a
live answer is needed.

If the directory is missing, run `modernpath docs sync` in the bound workspace
to download it, or use live search/read-doc for the material you need.

## When the corpus is empty

A repository that has never been analyzed returns nothing to every query. That is
not a failure — it means the pass has no prior map and must derive one from the
code, and should say so in its report rather than implying the analysis agreed.
