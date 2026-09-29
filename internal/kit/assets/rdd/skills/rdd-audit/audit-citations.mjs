#!/usr/bin/env node
// Resolve cited files in explicitly selected Markdown documents.
// Usage: node audit-citations.mjs <document-root...> --repository=<key>=<root>
// Exits 0 for a passing check, 1 for findings or insufficient citations,
// and 2 for missing document targets, invalid source inventory, or refused prompt targets.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, statSync, realpathSync } from "node:fs";
import { join, resolve, relative, basename, isAbsolute } from "node:path";

// Unknown prefixed source extensions are reported, not silently skipped.
const EXT = "heex|leex|eex|exs|tsx|yaml|proto|json|xslt|xml|jsp|xsl|mjs|cjs|ex|go|js|ts|yml|sh|py|rb|rs|java|kt|toml|sql|cs|vb|fs|php|swift|scala|erl";

// Consume optional navigation suffixes, but validate only the cited file.
const PREFIXED = new RegExp(`(?:CODE|TEST): ?((?:[A-Za-z0-9_.-]+@[A-Za-z0-9_.-]+:)?[A-Za-z0-9_./\\[\\]\\-]+?\\.(?:${EXT}))(?!\\.?[A-Za-z0-9])(?::([^\\s\\x60<>|;)]*))?`, "g");
const UNKNOWN_PREFIX = /(?:CODE|TEST): ?[^\s`<>|;)]+/g;
// Bare path:locator citations also resolve only the file.
const BARE = new RegExp("`([A-Za-z0-9_./\\[\\]\\-]+\\.(?:" + EXT + ")):([^\\s`<>|;)]*)", "g");
const DOCREF = /DOC: ?([A-Za-z0-9_./\-]+\.md)(?:#([^\s)|`\u00b7]+))?/g;
const ELIDED = /(?:CODE:|`)(?:[A-Za-z0-9_.\\[\\]\-]+\/)*\.\.\.\/[A-Za-z0-9_.\/\\[\\]\-]*\.(?:exs|tsx|ex|go|js|ts|py|rb|rs|java|kt|sql|yml|yaml|json|sh|heex)\b/g;

const roots = process.argv.slice(2).filter((a) => !a.startsWith("--"));
// The minimum is a caller-supplied citation count, not semantic coverage.
const minArg = process.argv.find((a) => a.startsWith("--min="));
const minCitations = minArg ? Math.max(1, parseInt(minArg.slice(6), 10) || 1) : 1;
const targets = roots.length ? roots : ["docs", ...(existsSync("ARCHITECTURE.md") ? ["ARCHITECTURE.md"] : [])];

// Prompt corpora contain deliberate examples; auditing them requires opt-in.
const prompts = targets.filter((t) => {
  const p = t.replace(/^\.\//, "");
  return p.startsWith(".claude") || p.startsWith(".modernpath/rdd/skills");
});
if (prompts.length) {
  console.error(
    `refusing to audit ${prompts.join(", ")} — installed skills are teaching material, and their\n` +
    "citations may be deliberate examples rather than claims about this checkout.\n" +
    "Pass --force-prompts only if you intend to read every hit before acting.",
  );
  if (!process.argv.includes("--force-prompts")) process.exit(2);
}

// The parent workspace need not be Git. A declared root is one repository;
// never silently discover and mix neighboring checkouts into the source scope.
const declared = process.argv.filter(a => a.startsWith("--repository="));
const repositories = [];
const SKIP = new Set([".git", "node_modules", ".elixir_ls", "tmp", "coverage"]);
function walk(dir, out) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if ((entry.name.startsWith(".") && entry.name !== ".modernpath") || SKIP.has(entry.name) || entry.isSymbolicLink()) continue;
    const path = join(dir, entry.name);
    if (entry.isDirectory()) walk(path, out);
    else if (entry.isFile()) out.push(path);
  }
  return out;
}
function inside(root, file) {
  const path = relative(root, file);
  return path !== ".." && !path.startsWith("../") && !isAbsolute(path);
}
try {
  const specs = declared.length ? declared.map(a => a.slice("--repository=".length)) : [`${basename(process.cwd())}=${process.cwd()}`];
  for (const spec of specs) {
    const separator = spec.indexOf("=");
    const key = spec.slice(0, separator);
    if (separator < 1 || !/^[A-Za-z0-9_.-]+$/.test(key)) throw new Error("expected --repository=key=directory");
    let root = realpathSync(spec.slice(separator + 1));
    if (!declared.length && !existsSync(join(root, ".git"))) {
      root = realpathSync(execFileSync("git", ["-C", root, "rev-parse", "--show-toplevel"], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim());
    }
    if (!statSync(root).isDirectory() || repositories.some(r => r.key === key || r.root === root)) throw new Error("repository keys and roots must be distinct directories");
    let revision = "unversioned";
    let files;
    let committedFiles;
    if (existsSync(join(root, ".git"))) {
      const git = (...args) => execFileSync("git", ["-C", root, ...args], { encoding: "utf8", maxBuffer: 64 << 20, stdio: ["ignore", "pipe", "pipe"] });
      revision = git("rev-parse", "HEAD").trim();
      committedFiles = new Set(git("ls-tree", "-r", "--name-only", "-z", revision).split("\0").filter(Boolean));
      files = git("ls-files", "-z", "--cached", "--others", "--exclude-standard").split("\0").filter(Boolean).map(path => join(root, path));
    } else {
      if (!declared.length) throw new Error("not a Git repository; declare every source root with --repository=key=directory");
      files = walk(root, []);
    }
    files = [...new Set(files.filter(path => existsSync(path) && statSync(path).isFile() && inside(root, realpathSync(path))))];
    repositories.push({ key, root, revision, files, committedFiles });
  }
} catch (error) {
  console.error(`source inventory refused: ${error.message}`);
  process.exit(2);
}

const resolutionErrors = new Map();
// A path suffix with multiple matches is ambiguous. Typed references resolve
// only exact paths at the declared revision.
function candidates(path, exactOnly = false) {
  const typed = path.match(/^([A-Za-z0-9_.-]+)@([A-Za-z0-9_.-]+):(.+)$/);
  let found;
  if (typed) {
    const repo = repositories.find(r => r.key === typed[1]);
    if (!repo || repo.revision !== typed[2]) {
      resolutionErrors.set(path, "unknown repository or revision differs from the declared checkout");
      return [];
    }
    const exact = resolve(repo.root, typed[3]);
    if (!inside(repo.root, exact)) return [];
    if (repo.committedFiles) {
      if (!repo.committedFiles.has(relative(repo.root, exact))) {
        resolutionErrors.set(path, "no such file at the cited revision");
        return [];
      }
      found = [exact];
    } else {
      found = repo.files.includes(exact) ? [exact] : [];
    }
  } else {
    const exact = repositories.flatMap(repo => repo.files.filter(file => relative(repo.root, file) === path));
    found = exact.length || exactOnly ? exact : repositories.flatMap(repo => repo.files.filter(file => file.endsWith("/" + path)));
  }
  found = [...new Set(found)];
  if (found.length > 1) {
    resolutionErrors.set(path, `ambiguous path (${found.length} matches); name repository@revision:exact/path`);
    return [];
  }
  return found;
}

const missingTargets = targets.filter(target => !existsSync(target));
if (missingTargets.length) {
  console.error(`document inventory refused: missing target(s): ${missingTargets.join(", ")}`);
  process.exit(2);
}

function markdownFiles(target) {
  if (statSync(target).isFile()) return target.endsWith(".md") ? [target] : [];
  return readdirSync(target, { withFileTypes: true }).flatMap((e) =>
    e.isDirectory() ? markdownFiles(join(target, e.name))
      : e.name.endsWith(".md") ? [join(target, e.name)] : []);
}

const docs = targets.flatMap(markdownFiles).sort();
let ok = 0;
let examples = 0;
const broken = [];
const elided = [];

// Only explicitly marked teaching fences are exempt; ordinary code fences
// still contain auditable citations.
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
    if (inExemptFence(m.index)) { examples++; continue; }
    check(doc, text, m);
  }
  for (const m of text.matchAll(UNKNOWN_PREFIX)) {
    if (spans.some(([a, b]) => m.index >= a && m.index < b) || inExemptFence(m.index)) continue;
    const lineNo = text.slice(0, m.index).split("\n").length;
    if (/<!--\s*example-citation\s*-->/.test(lines[lineNo - 1] ?? "")) continue;
    const value = m[0].replace(/^(?:CODE|TEST): ?/, "");
    if (value.endsWith(".md")) {
      if (candidates(value, true).length) ok++;
      else broken.push({ doc, lineNo, path: value, why: "no such document" });
      continue;
    }
    // Directory references are orientation, not file/line claims. Their contents
    // are inventoried separately; bare grammar examples are not source files.
    if (value.endsWith("/") || !/^[A-Za-z0-9_./@:\[\]-]+\.[A-Za-z0-9]+(?::.*)?$/.test(value)) continue;
    broken.push({ doc, lineNo, path: m[0], why: "unsupported or malformed source citation; not silently omitted" });
  }
  for (const m of text.matchAll(BARE)) {
    if (spans.some(([a, b]) => m.index >= a && m.index < b)) continue;
    if (inExemptFence(m.index)) { examples++; continue; }
    check(doc, text, m);
  }
  for (const m of text.matchAll(DOCREF)) {
    const lineNo = text.slice(0, m.index).split("\n").length;
    if (inExemptFence(m.index) || /<!--\s*example-citation\s*-->/.test(lines[lineNo - 1] ?? "")) { examples++; continue; }
    const found = candidates(m[1], true);
    if (!found.length) { broken.push({ doc, lineNo, path: m[1], why: resolutionErrors.get(m[1]) || "no such document" }); continue; }
    ok++;
  }
}

function check(doc, text, m) {
  const [, path] = m;
  const lineNo = text.slice(0, m.index).split("\n").length;

  // Explicit teaching examples are excluded from the checked denominator.
  if (/<!--\s*example-citation\s*-->/.test(text.split("\n")[lineNo - 1] ?? "")) { examples++; return; }

  const found = candidates(path);
  if (!found.length) {
    broken.push({ doc, lineNo, path, why: resolutionErrors.get(path) || "no such file" });
    return;
  }
  ok++;
}

const total = ok + broken.length;
const belowMinimum = total < minCitations;
console.log(`${ok}/${total} citations resolve to files across ${docs.length} documents` +
  (examples ? `  (${examples} teaching examples skipped; not checked citations)` : ""));
console.log("File resolution only; source contents and navigation suffixes are not checked.");

if (elided.length) {
  console.log(`\n${elided.length} elided path(s) — a citation that resolves to nothing:`);
  for (const e of elided) console.log(`  ${e.doc}:${e.line}  ${e.snippet}`);
}
if (broken.length) {
  console.log(`\n${broken.length} broken:`);
  for (const b of broken) console.log(`  ${b.doc}:${b.lineNo}  ${b.path}  — ${b.why}`);
}
if (belowMinimum) {
  console.log(`\ninsufficient citations: ${total} checked; --min requires ${minCitations}`);
}
if (!elided.length && !broken.length && !belowMinimum) console.log("no elided paths");

process.exit(broken.length || elided.length || belowMinimum ? 1 : 0);
