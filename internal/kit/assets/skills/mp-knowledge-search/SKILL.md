---
name: mp-knowledge-search
description: Query the platform's knowledge core — search, ask, read-doc, read-file — before reading a codebase by hand. Use when writing about the codebase, locating which subsystem owns a behavior, or deciding where to look; the analysis says where to look, grep only says where a word appears.
---

# Querying the knowledge core

<!-- TOOL-OWNED. Installed by `modernpath install`. -->

The platform has already analyzed this repository. Its subsystem and module
documentation, its data model and its file analyses are queryable — and they are
**derived from this code**, not from anyone's memory of it.

Any pass that writes about the codebase should ask the knowledge core **before**
it starts reading files, for one reason: it tells you *where to look*. Grep tells
you where a word appears.

## The four commands

| Command | Use it for |
|---|---|
| `modernpath search "<terms>"` | find which documents and files concern a topic |
| `modernpath ask "<question>"` | agentic search — finds material and synthesises an answer |
| `modernpath read-doc --id=<id>` | read a generated document in full |
| `modernpath read-file <path>` | read an analyzed source file through the API |

`search` takes `--docs-only`, `--files-only`, `--limit`. `ask` takes
`--format=markdown` when you want output you can paste.

## Enumerate before you search

**`modernpath read-doc --list` prints the whole corpus** — every generated
document with its id, title and one-line summary. Verified : 40
documents on a real system.

Do this **first**. Searching requires guessing the word the corpus used; listing
does not, and the list is short enough to read. `--tier` (`module`, `subsystem`,
`architecture`) and `--angle` (`architecture`, `api`, `data`) narrow it.

That ordering matters because the failure below is real: a query for
`organisation` returned nothing on a system whose documents say `organization`
throughout. An enumeration would have shown the vocabulary in seconds, and no
guess can recover from a word the corpus does not use.

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
  under `.modernpath/modernpath/` is a cache, and a stale cache answers
  confidently. Refresh it with `modernpath docs sync`.

## The local export

`.modernpath/modernpath/` holds the analysis as files — free to read, whole
documents rather than excerpts. Use `search` to *find* the id, then read the
export; fall back to `read-doc` for anything not exported.

If the directory is missing, the export has not been run here.

## When the corpus is empty

A repository that has never been analyzed returns nothing to every query. That is
not a failure — it means the pass has no prior map and must derive one from the
code, and should say so in its report rather than implying the analysis agreed.
