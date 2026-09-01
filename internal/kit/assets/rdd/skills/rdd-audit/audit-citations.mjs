#!/usr/bin/env node
// Citation audit — the C8 procedure, as a program rather than a paragraph.
//
// Every `file:line` a document claims must resolve. This was re-derived by hand
// five times in one session and was wrong three of those — a regex whose
// non-greedy alternation matched `.ex` inside `.exs`, a display truncated at
// column 46 that made six correct citations look wrong, and a sweep that covered
// one subset while reporting a fraction as if it covered the corpus.
// Those are the three failures this file exists to stop repeating.
//
//   node .modernpath/rdd/skills/rdd-audit/audit-citations.mjs [root...]
//
// KNOWN FALSE POSITIVES. Two shapes look like citations and are not, and both
// appear in real epic records:
//   * example data inside a BDD scenario — "GIVEN a requirement whose code
//     citation is `services/…/models.py:1477`" describes another workspace's
//     file on purpose;
//   * an illustrative quotation — prose showing how to write a good finding,
//     quoting a path that need not exist.
// Neither is worth suppressing automatically: a rule broad enough to catch them
// would hide real rot. Read the failures before acting on them.
//
// Exits 0 when every citation resolves, 1 otherwise. Default roots: docs/ plus
// ARCHITECTURE.md if present.

import { execSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";

// An extension missing here is not reported as unresolved — it is not seen at
// all. On a .NET estate this printed "10/10 citations resolve" while silently
// skipping 710 of 720, which reads as a pass. Longest-first still holds.
const EXT = "exs|tsx|yaml|proto|json|ex|go|js|ts|yml|sh|py|rb|rs|java|kt|toml|sql|cs|vb|fs|php|swift|scala|erl";

// Longest extensions first, and a boundary after — `.ex` must not match inside
// `.exs`, nor `.ts` inside `.tsx`. This is the first of the three failures.
// `CODE: path` with a space is a rare deviation (2 instances in one corpus,
// RUN:2026-08-14) — tolerated so the citation is CHECKED rather than skipped.
// A citation may end at a NAME instead of a line — a test
// identifier survives edits to the file, a line number does not. Group 4 is
// that name; unchecked, `file.go:TestGoneForever` passed on file existence.
const PREFIXED = new RegExp(`CODE: ?([A-Za-z0-9_./\\[\\]\\-]+?\\.(?:${EXT}))(?![A-Za-z0-9])(?::(?:((?:\\d+(?:-\\d+)?)(?:,\\d+(?:-\\d+)?)*)|([A-Za-z_][A-Za-z0-9_]{2,})))?`, "g");
// The full grammar, not the first two parts: a citation may list lines and
// ranges — file:N, file:N-M, file:N,M, file:N-M,P. A pattern holding only two
// groups matches every one of them and silently skips the rest; 188 citations
// in one corpus had 3+ parts, up to 9 (RUN:2026-08-14).
const BARE = new RegExp("`([A-Za-z0-9_./\\[\\]\\-]+\\.(?:" + EXT + ")):((?:\\d+(?:-\\d+)?)(?:,\\d+(?:-\\d+)?)*)", "g");
// The ellipsis may sit mid-path, and
// the citation must END at a source file — an ellipsis inside /var/folders/…/
// or a URL is prose. The narrower `CODE:...` form missed 22 real elisions.
// DOC: references point at documents and were checked by nothing until
// RUN:2026-08-14, when a sweep found one broken in 131 — a ledger citing a spec
// that had never existed under that name, while its siblings cited the real
// prompt file. Same shape as CODE:, so it gets the same treatment.
// An anchored form exists too — DOC:file.md#Section — and the section was
// checked by nothing until RUN:2026-08-14, when 17 of them turned out to hide
// one malformed compound anchor (`#2.1/#5`).
const DOCREF = /DOC: ?([A-Za-z0-9_./\-]+\.md)(?:#([^ )|`\u00b7]+))?/g;
const ELIDED = /(?:CODE:|`)(?:[A-Za-z0-9_.\\[\\]\-]+\/)*\.\.\.\/[A-Za-z0-9_.\/\\[\\]\-]*\.(?:exs|tsx|ex|go|js|ts|py|rb|rs|java|kt|sql|yml|yaml|json|sh|heex)\b/g;

// C8: a row naming a gap is not a citation. "there is no test_draft_service.py"
// is the most useful thing a derivation pass produces, and an audit that counts
// it as broken teaches the next pass to stop naming what is missing.
const ABSENCE = /\b(no|not|missing|absent|never|does not exist|there is no|without)\b[^.]{0,60}$/i;

const roots = process.argv.slice(2).filter((a) => !a.startsWith("--"));
const targets = roots.length ? roots : ["docs", ...(existsSync("ARCHITECTURE.md") ? ["ARCHITECTURE.md"] : [])];

// Refuse to audit the prompts. Teaching material deliberately
// quotes citations that do not resolve — an elided `CODE:.../job_processor.py`
// shown as the thing NOT to write, a template row citing `domain/user.ts`, a
// finding cited from the workspace it was found in. Run on `.claude/` this
// reports 3 broken and 2 elided, every one of them correct prose, and the
// obvious next move is to "fix" them, which deletes the lesson.
//
// Checked by hand on RUN:2026-08-14: 132 backticked paths across the prompts,
// 21 unresolved, and all 21 legitimate — generic templates, evidence from other
// repositories, and paths the skill *instructs a pass to create*. There is
// nothing here for this tool to find, so the refusal costs no coverage. The
// gate's own elided check excludes `.claude/` for the same reason; this is the
// standalone half of that guard.
const prompts = targets.filter((t) => {
  const p = t.replace(/^\.\//, "");
  return p.startsWith(".claude") || p.startsWith(".modernpath/rdd/skills");
});
if (prompts.length) {
  console.error(
    `refusing to audit ${prompts.join(", ")} — installed skills are teaching material, and their\n` +
    "unresolvable citations are deliberate (bad examples, other repositories,\n" +
    "prescribed outputs). Verified by hand RUN:2026-08-14: no real rot there.\n" +
    "Pass --force-prompts only if you intend to read every hit before acting.",
  );
  if (!process.argv.includes("--force-prompts")) process.exit(2);
}

let tracked = [];
try {
  tracked = execSync("git ls-files", { encoding: "utf8", maxBuffer: 64 << 20 }).split("\n").filter(Boolean);
} catch {
  console.error("not a git repository — cannot resolve citations by path suffix");
  process.exit(2);
}

const lineCache = new Map();
function lineCount(p) {
  if (!lineCache.has(p)) {
    try { lineCache.set(p, readFileSync(p, "utf8").split("\n").length); }
    catch { lineCache.set(p, -1); }
  }
  return lineCache.get(p);
}

// Vendored and generated trees — deps/, node_modules/, _build/ — are gitignored
// but real, and a ledger may legitimately cite one. Falling back to the working
// tree lets those resolve. Built lazily on the first miss and walked once, so a
// corpus with no failures never pays for it.
let onDisk = null;
const SKIP = new Set([".git", "node_modules", ".elixir_ls", "tmp", "coverage"]);
function walk(dir, out) {
  let entries;
  try { entries = readdirSync(dir, { withFileTypes: true }); } catch { return out; }
  for (const e of entries) {
    if (e.name.startsWith(".") && e.name !== ".modernpath") continue;
    if (SKIP.has(e.name)) continue;
    const p = dir === "." ? e.name : join(dir, e.name);
    if (e.isDirectory()) walk(p, out);
    else out.push(p);
  }
  return out;
}

// Resolve on path suffix and accept if ANY candidate satisfies the line: a bare
// `admin.py` may match two files, only one of which is long enough.
function candidates(path) {
  if (existsSync(path)) return [path];
  // `tracked` is the git index, and the index can name a file deleted from the
  // working tree (an unstaged deletion). A citation cannot be satisfied by a
  // file that is not on disk — and reading one crashed the whole audit instead
  // of reporting the one broken citation (RUN:2026-08-20, the retired process
  // manuals). Flag it, don't die on it.
  const hit = tracked.filter((f) => (f === path || f.endsWith("/" + path)) && existsSync(f));
  if (hit.length) return hit;
  if (onDisk === null) onDisk = walk(".", []);
  return onDisk.filter((f) => f === path || f.endsWith("/" + path));
}

function markdownFiles(target) {
  if (!existsSync(target)) return [];
  if (statSync(target).isFile()) return target.endsWith(".md") ? [target] : [];
  return readdirSync(target, { withFileTypes: true }).flatMap((e) =>
    e.isDirectory() ? markdownFiles(join(target, e.name))
      : e.name.endsWith(".md") ? [join(target, e.name)] : []);
}

const docs = targets.flatMap(markdownFiles).sort();
let ok = 0;
let gaps = 0;
const broken = [];
const elided = [];

// A fenced block whose opening line carries `example-citation` is a VERBATIM
// DISPLAY of citations, not a set of claims: a client-facing document showing a
// real ledger row cites paths in the CLIENT's repository, which can never
// resolve here. The per-line marker would work but renders inside the code
// sample a client reads, so the opt-out moves to the fence's info string where
// it is invisible. Opt-in per block, never blanket — citations in unmarked
// fences are still audited (RUN:2026-08-17).
function exemptFenceRanges(lines) {
  const ranges = [];
  let open = null;
  let offset = 0;
  const starts = lines.map((l) => { const s = offset; offset += l.length + 1; return s; });
  for (let i = 0; i < lines.length; i++) {
    if (!/^\s*```/.test(lines[i])) continue;
    if (open === null) {
      open = /example-citation/.test(lines[i]) ? starts[i] : -1;
    } else {
      if (open >= 0) ranges.push([open, starts[i] + lines[i].length]);
      open = null;
    }
  }
  return ranges;
}

for (const doc of docs) {
  const text = readFileSync(doc, "utf8");
  const lines = text.split("\n");
  const exemptRanges = exemptFenceRanges(lines);
  const inExemptFence = (idx) => exemptRanges.some(([a, b]) => idx >= a && idx < b);

  for (const m of text.matchAll(ELIDED)) {
    const line = text.slice(0, m.index).split("\n").length;
    // Same per-line opt-out as `check()`: a document teaching the grammar has to
    // SHOW a rejected form, and an elided path is one of them.
    if (/<!--\s*example-citation\s*-->/.test(lines[line - 1] ?? "")) continue;
    if (inExemptFence(m.index)) continue;
    elided.push({ doc, line, snippet: lines[line - 1].trim().slice(0, 90) });
  }

  // Prefixed citations first; record their spans so a bare match inside one is
  // not counted twice.
  const spans = [];
  for (const m of text.matchAll(PREFIXED)) {
    spans.push([m.index, m.index + m[0].length]);
    if (inExemptFence(m.index)) { ok++; continue; }
    check(doc, text, m);
  }
  for (const m of text.matchAll(BARE)) {
    if (spans.some(([a, b]) => m.index >= a && m.index < b)) continue;
    if (inExemptFence(m.index)) { ok++; continue; }
    check(doc, text, m);
  }
  for (const m of text.matchAll(DOCREF)) {
    const lineNo = text.slice(0, m.index).split("\n").length;
    const found = candidates(m[1]);
    if (!found.length) { broken.push({ doc, lineNo, path: m[1], why: "no such document" }); continue; }
    if (m[2]) {
      // A heading may hyphenate where the anchor spaces, and vice versa.
      // Escape FIRST, then loosen: doing it the other way mangles the character
      // class you just inserted, and "#Follow-Up" reported a missing section that
      // was plainly there (RUN:2026-08-14).
      const want = m[2].replace(/[.*+?^${}()|[\]\\]/g, (c) => "\\" + c).replace(/(?:\\?-|\s)+/g, "[-\\s]+");
      const re = new RegExp("^#{1,6} .*" + want, "mi");
      if (!found.some((f) => re.test(readFileSync(f, "utf8")))) {
        broken.push({ doc, lineNo, path: `${m[1]}#${m[2]}`, why: "no such section" });
        continue;
      }
    }
    ok++;
  }
}

// The absence exemption is applied ONLY to a citation that would otherwise fail.
// Applied up front it is a false-negative machine: on a real corpus it skipped
// 116 resolvable citations, because prose like "the mechanism has no test of any
// kind (`CODE:…/job_processor.py`)" says *no test*, not *no file* — the cited
// path exists and must still be checked. Excusing a gap must never be able to
// hide a citation that could have been verified.
function check(doc, text, m) {
  const [, path, spec, name] = m;
  const lineNo = text.slice(0, m.index).split("\n").length;

  // A document that TEACHES citation grammar must contain citations that do not
  // resolve — that is the lesson, not rot (`docs/onboarding/05-contracts.md` §3
  // shows accepted and rejected forms). The same judgement the gate already
  // makes for `.claude/` teaching material, narrowed from a whole tree to one
  // line: an `<!-- example-citation -->` marker on the citing line opts that
  // line out. Deliberately per-line — exempting the file would blind the audit
  // to real rot in the same document, and this audit has caught real rot there
  // (RUN:2026-08-17: two migration paths and a dead anchor).
  if (/<!--\s*example-citation\s*-->/.test(text.split("\n")[lineNo - 1] ?? "")) { ok++; return; }

  const found = candidates(path);
  // A named citation is verified by the name, not the position — that is the
  // whole point of citing one.
  if (name && found.length) {
    if (found.some((f) => readFileSync(f, "utf8").includes(name))) { ok++; return; }
    broken.push({ doc, lineNo, path: `${path}:${name}`, why: "no such test or symbol in the file" });
    return;
  }
  // Every part of a compound citation is a citation. The highest line any part
  // names is what the file must reach.
  const parts = spec ? spec.split(",") : [];
  const hi = parts.reduce((a, p) => Math.max(a, ...p.split("-").map(Number)), 0);
  const resolves = found.length && (!parts.length || found.some((f) => lineCount(f) >= hi));
  if (resolves) { ok++; return; }

  if (ABSENCE.test(text.slice(Math.max(0, m.index - 70), m.index))) { gaps++; return; }

  broken.push({
    doc, lineNo,
    path: spec ? `${path}:${spec}` : path,
    why: found.length ? `no candidate reaches line ${hi}` : "no such file",
  });
}

const total = ok + broken.length;
console.log(`${ok}/${total} citations resolve across ${docs.length} documents` +
  (gaps ? `  (+${gaps} named gaps excused — a cited path stated as absent)` : ""));

if (elided.length) {
  console.log(`\n${elided.length} elided path(s) — a citation that resolves to nothing:`);
  for (const e of elided) console.log(`  ${e.doc}:${e.line}  ${e.snippet}`);
}
if (broken.length) {
  console.log(`\n${broken.length} broken:`);
  for (const b of broken) console.log(`  ${b.doc}:${b.lineNo}  ${b.path}  — ${b.why}`);
}
if (!elided.length && !broken.length) console.log("no elided paths");

process.exit(broken.length || elided.length ? 1 : 0);
