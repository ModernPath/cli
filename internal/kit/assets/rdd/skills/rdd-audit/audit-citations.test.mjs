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
  assert.match(result.stdout, /insufficient citations/);
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

test("exempt teaching examples cannot satisfy the checked-citation minimum", t => {
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

test("source citations resolve files regardless of navigation suffixes or contents", t => {
  const f = fixture(t);
  f.file("repo/subject.ex", "# REQ-EXAMPLE-001\n");
  f.file("repo/subject_test.go", "// REQ-EXAMPLE-001\n");
  f.file("report.md", [
    "CODE:subject.ex",
    "CODE:subject.ex:validate/1",
    "CODE:subject.ex:validator",
    "CODE:subject.ex:Sample.Auth",
    "CODE:subject.ex:900-999",
    "CODE:subject.ex:0,2-1",
    "TEST:subject_test.go",
    "TEST:subject_test.go:TestParent/Child",
    "`subject.ex:missing_symbol`",
  ].join("\n"));
  const result = f.run(`--repository=repo=${join(f.root, "repo")}`, "report.md");
  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.match(result.stdout, /9\/9 citations resolve to files/);
  assert.match(result.stdout, /source contents and navigation suffixes are not checked/);
});

test("navigation suffixes never rescue a missing source file", t => {
  const f = fixture(t);
  f.file("repo/subject.ex", "# REQ-EXAMPLE-001\n");
  f.file("report.md", "CODE:subject.ex\nCODE:missing.ex:validate/1\nTEST:missing_test.exs:TestParent/Child\n");
  const result = f.run(`--repository=repo=${join(f.root, "repo")}`, "report.md");
  assert.equal(result.status, 1, result.stdout + result.stderr);
  assert.match(result.stdout, /1\/3 citations resolve to files/);
  assert.match(result.stdout, /missing.ex.*no such file/);
  assert.match(result.stdout, /missing_test.exs.*no such file/);
});

test("absence wording does not excuse missing files", t => {
  const f = fixture(t);
  f.file("repo/subject.py", "# REQ-EXAMPLE-001\n");
  f.file("report.md", "CODE:subject.py\nMissing validation in `CODE:absent.py:1`.\n");
  const result = f.run(`--repository=repo=${join(f.root, "repo")}`, "report.md");
  assert.equal(result.status, 1, result.stdout + result.stderr);
  assert.match(result.stdout, /1\/2 citations resolve to files/);
  assert.match(result.stdout, /absent.py.*no such file/);
});

test("plain gap descriptions are not supporting citations", t => {
  const f = fixture(t);
  f.file("repo/subject.py", "# REQ-EXAMPLE-001\n");
  f.file("report.md", "CODE:subject.py\nThere is no `test_missing.py`; verification remains unresolved.\n");
  const result = f.run(`--repository=repo=${join(f.root, "repo")}`, "report.md");
  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.match(result.stdout, /1\/1 citations resolve to files/);
});

test("a missing document root cannot disappear beside a valid report", t => {
  const f = fixture(t);
  f.file("repo/subject.py", "# REQ-EXAMPLE-001\n");
  f.file("report.md", "CODE:subject.py\n");
  const result = f.run(`--repository=repo=${join(f.root, "repo")}`, "report.md", "missing-docs");
  assert.equal(result.status, 2, result.stdout + result.stderr);
  assert.match(result.stderr, /missing-docs/);
});

test("revision-qualified files must exist in the commit regardless of working-tree changes", t => {
  const f = fixture(t);
  const repo = join(f.root, "repo");
  f.file("repo/subject.py", "# REQ-EXAMPLE-001\n");
  const git = (...args) => execFileSync("git", ["-C", repo, ...args], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
  git("init"); git("add", "."); git("-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "-m", "fixture");
  const revision = git("rev-parse", "HEAD").trim();
  f.file("repo/subject.py", "changed contents\n");
  f.file("repo/untracked.py", "new file\n");
  const args = [`--repository=repo=${repo}`, "report.md"];

  f.file("report.md", `CODE:repo@${revision}:subject.py\nCODE:untracked.py\n`);
  let result = f.run(...args);
  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.match(result.stdout, /2\/2 citations resolve to files/);

  f.file("report.md", `CODE:repo@${revision}:untracked.py\n`);
  result = f.run(...args);
  assert.equal(result.status, 1, result.stdout + result.stderr);
  assert.match(result.stdout, /no such file at the cited revision/);

  rmSync(join(repo, "subject.py"));
  f.file("report.md", `CODE:repo@${revision}:subject.py\n`);
  result = f.run(...args);
  assert.equal(result.status, 0, result.stdout + result.stderr);
  f.file("report.md", "CODE:subject.py\n");
  result = f.run(...args);
  assert.equal(result.status, 1, result.stdout + result.stderr);
});

test("document citations resolve files without inspecting headings", t => {
  const f = fixture(t);
  f.file("repo/guide.md", "REQ-EXAMPLE-001\n");
  const args = [`--repository=repo=${join(f.root, "repo")}`, "report.md"];
  f.file("report.md", "DOC:guide.md#Trace completeness\nDOC:guide.md#missing-heading\nDOC:guide.md\n");
  let result = f.run(...args);
  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.match(result.stdout, /3\/3 citations resolve to files/);
  f.file("report.md", "DOC:missing.md#Trace completeness\n");
  result = f.run(...args);
  assert.equal(result.status, 1, result.stdout + result.stderr);
  assert.match(result.stdout, /missing.md.*no such document/);
});
