import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { execFileSync, spawnSync } from "node:child_process";

const script = fileURLToPath(new URL("./audit-citations.mjs", import.meta.url));
function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), "rdd-audit-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const file = (path, content) => { const target = join(root, path); mkdirSync(join(target, ".."), { recursive: true }); writeFileSync(target, content); return target; };
  const run = (...args) => spawnSync(process.execPath, [script, ...args], { cwd: root, encoding: "utf8" });
  return { root, file, run };
}

test("declared non-Git repositories resolve XML/JSP/XSL/XSLT with exact typed paths", t => {
  const f = fixture(t);
  for (const extension of ["xml", "jsp", "xsl", "xslt"]) f.file(`legacy/src/catalog.${extension}`, "one\ntwo\n");
  f.file("report.md", ["xml", "jsp", "xsl", "xslt"].map(ext => `CODE:legacy@unversioned:src/catalog.${ext}:2`).join("\n"));
  const result = f.run(`--repository=legacy=${join(f.root, "legacy")}`, "report.md");
  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.match(result.stdout, /4\/4 citations resolve/);
});

test("ambiguous suffixes across repositories fail even when one candidate is long enough", t => {
  const f = fixture(t);
  f.file("a/src/catalog.xml", "one\n");
  f.file("b/src/catalog.xml", "one\ntwo\nthree\n");
  f.file("report.md", "CODE:catalog.xml:3");
  const result = f.run(`--repository=a=${join(f.root, "a")}`, `--repository=b=${join(f.root, "b")}`, "report.md");
  assert.equal(result.status, 1, result.stdout + result.stderr);
  assert.match(result.stdout, /ambiguous/);
});

test("exact root paths win over suffixes and missing documents never select a namesake", t => {
  const f = fixture(t);
  f.file("repo/AGENTS.md", "# Root");
  f.file("repo/nested/AGENTS.md", "# Nested");
  f.file("repo/nested/BACKLOG.md", "# Other backlog");
  f.file("repo/check.mjs", "export {};\n");
  f.file("report.md", "DOC:AGENTS.md\nCODE:check.mjs:1");
  const args = [`--repository=repo=${join(f.root, "repo")}`, "report.md"];
  assert.equal(f.run(...args).status, 0);
  f.file("report.md", "DOC:BACKLOG.md");
  const result = f.run(...args);
  assert.equal(result.status, 1);
  assert.match(result.stdout, /no such document/);
});

test("Git worktrees declared from a non-Git parent preserve revision identity", t => {
  const f = fixture(t);
  const repo = join(f.root, "repo");
  f.file("repo/code.xml", "one\n");
  const git = (...args) => execFileSync("git", ["-C", repo, ...args], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
  git("init"); git("add", "."); git("-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "-m", "fixture");
  const revision = git("rev-parse", "HEAD").trim();
  const worktree = join(f.root, "checkout");
  git("worktree", "add", "--detach", worktree);
  f.file("report.md", `CODE:legacy@${revision}:code.xml:1`);
  assert.equal(f.run(`--repository=legacy=${worktree}`, "report.md").status, 0);
  f.file("report.md", `CODE:legacy@${"a".repeat(40)}:code.xml:1`);
  const wrong = f.run(`--repository=legacy=${worktree}`, "report.md");
  assert.equal(wrong.status, 1, wrong.stdout + wrong.stderr);
  assert.match(wrong.stdout, /revision/);
});

test("a nonempty corpus with no recognized references is not a passing audit", t => {
  const f = fixture(t);
  f.file("repo/code.xml", "one");
  f.file("report.md", "No traceable citations were emitted.");
  const result = f.run(`--repository=legacy=${join(f.root, "repo")}`, "report.md");
  assert.equal(result.status, 1, result.stdout + result.stderr);
  assert.match(result.stdout, /vacuity/);
});

test("the default single-repository audit still works from a nested directory", t => {
  const f = fixture(t);
  f.file("project/code.ex", "one\ntwo\n");
  f.file("project/report.md", "CODE:code.ex:2");
  const git = (...args) => execFileSync("git", ["-C", f.root, ...args], { stdio: ["ignore", "pipe", "pipe"] });
  git("init"); git("add", "."); git("-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "-m", "fixture");
  const result = spawnSync(process.execPath, [script, "report.md"], { cwd: join(f.root, "project"), encoding: "utf8" });
  assert.equal(result.status, 0, result.stdout + result.stderr);
});

test("unknown prefixed source extensions cannot disappear beside a valid citation", t => {
  const f = fixture(t);
  f.file("repo/code.xml", "one");
  f.file("report.md", "CODE:code.xml:1\nCODE:missing.unknown:1");
  const result = f.run(`--repository=legacy=${join(f.root, "repo")}`, "report.md");
  assert.equal(result.status, 1, result.stdout + result.stderr);
  assert.match(result.stdout, /missing.unknown/);
});

test("exempt teaching examples cannot satisfy the checked-citation floor", t => {
  const f = fixture(t);
  f.file("repo/code.xml", "one");
  f.file("report.md", "CODE:missing.xml:2 <!-- example-citation -->\n");
  const args = [`--repository=legacy=${join(f.root, "repo")}`, "report.md"];
  const empty = f.run(...args);
  assert.equal(empty.status, 1, empty.stdout + empty.stderr);
  assert.match(empty.stdout, /0\/0 citations resolve/);
  f.file("report.md", "CODE:missing.xml:2 <!-- example-citation -->\nCODE:code.xml:1\n");
  const mixed = f.run(...args);
  assert.equal(mixed.status, 0, mixed.stdout + mixed.stderr);
  assert.match(mixed.stdout, /1\/1 citations resolve/);
});
