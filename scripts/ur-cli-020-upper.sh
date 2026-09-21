#!/usr/bin/env bash
# UR-CLI-020 upper evidence: the first hour of a freshly connected system from
# the installed kit and the CLI alone. Runs the built binary in a temporary
# workspace against a fake platform server and prints one PASS/FAIL line per
# scenario. Exits non-zero when any scenario fails.
#
# Usage: scripts/ur-cli-020-upper.sh [path-to-modernpath-binary]
# Needs: bash, python3, git. Builds the binary into a temp dir when no path is given.
set -u

here="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'kill "${server_pid:-}" 2>/dev/null; wait "${server_pid:-}" 2>/dev/null; rm -rf "$work"' EXIT

bin="${1:-}"
if [ -z "$bin" ]; then
  bin="$work/modernpath"
  (cd "$here" && go build -o "$bin" .) || { echo "build failed"; exit 2; }
fi

# ---------------------------------------------------------------- fake server
# Answers the health check; lists systems only for the bearer "good-token";
# serves an export job that downloads a one-file zip. Every request is logged
# as "METHOD PATH bearer=<token|none>" so a scenario can assert what was sent.
cat > "$work/fake_server.py" <<'PY'
import io, json, os, sys, zipfile
from http.server import BaseHTTPRequestHandler, HTTPServer

LOG = os.environ["FAKE_LOG"]
SYSTEMS = json.loads(os.environ.get("FAKE_SYSTEMS", '[{"id":1,"name":"Demo System","slug":"demo-system","description":"one"}]'))
PROCESS_STORE = {}  # the system's store-backed declaration record (SCN-CLI020-002)
RELEASES = {}       # slug -> status (SCN-CLI020-003)
GATES = []          # served on the gates reads (SCN-CLI020-003)

def zip_bytes():
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w") as z:
        z.writestr(".modernpath/modernpath/README.md", "# demo\n")
    return buf.getvalue()

class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def bearer(self):
        h = self.headers.get("Authorization", "")
        return h[len("Bearer "):] if h.startswith("Bearer ") else "none"
    def record(self):
        with open(LOG, "a") as f:
            f.write(f"{self.command} {self.path.split('?')[0]} bearer={self.bearer()}\n")
    def send(self, code, body, ctype="application/json"):
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.end_headers()
        self.wfile.write(body if isinstance(body, bytes) else body.encode())
    def authorized(self):
        if self.bearer() == "good-token":
            return True
        self.send(401, '{"error":"unauthorized"}')
        return False
    def do_GET(self):
        self.record()
        p = self.path.split("?")[0]
        if p == "/_health":
            return self.send(200, '{"status":"ok"}')
        if p == "/api/systems":
            if self.authorized():
                self.send(200, json.dumps(SYSTEMS))
            return
        if p.startswith("/api/systems/") and p.endswith("/export/jobs/job1"):
            if self.authorized():
                self.send(200, json.dumps({"id": "job1", "status": "ready", "download_path": p + "/file"}))
            return
        if p.startswith("/api/systems/") and p.endswith("/export/jobs/job1/file"):
            if self.authorized():
                self.send(200, zip_bytes(), "application/zip")
            return
        if p == "/api/v1/sync/store-backed":
            if self.authorized():
                self.send(200, json.dumps({"data": {"process_store": PROCESS_STORE}}))
            return
        if p == "/api/v1/sync/work-selection":
            if self.authorized():
                active = [{"slug": k, "status": v} for k, v in RELEASES.items() if v == "active"]
                self.send(200, json.dumps({"data": {"active_release": active, "current": None, "suspended": [], "history": []}}))
            return
        for key in ("requirements", "epics", "backlog", "findings", "packet-sections"):
            if p == "/api/v1/sync/" + key:
                if self.authorized():
                    data = {key.replace("-", "_"): []}
                    if key == "requirements":
                        data["user_requirements"] = []
                    self.send(200, json.dumps({"data": data}))
                return
        if p == "/api/v1/sync/gates":
            if self.authorized():
                self.send(200, json.dumps({"data": {"gates": GATES}}))
            return
        if p.startswith("/api/v1/sync/gates/"):
            if self.authorized():
                gid = p[len("/api/v1/sync/gates/"):]
                for g in GATES:
                    if g["external_id"] == gid:
                        return self.send(200, json.dumps({"data": {"gate": g}}))
                self.send(404, json.dumps({"error": {"message": "no such gate: " + gid}}))
            return
        if p == "/api/v1/sync/delivery-context":
            if not self.authorized():
                return
            if os.path.exists(os.environ.get("FAKE_CTX_FLAG", "")):
                # SCN-CLI020-004: a selection whose cold-review check fails
                return self.send(200, json.dumps({"data": {
                    "derived_phase": "cold_review", "declared_phase": "cold_review",
                    "packet_fingerprint": "c" * 64, "process_revision": "a" * 40,
                    "checks": {"cold_review": [{"name": "independent_verdict", "state": "FAIL"}, {"name": "findings", "state": "FAIL"}]},
                    "facts_state": "served",
                    "facts": {"scope": {"external_id": "EPIC-G", "kind": "epic", "status": "PROPOSED"},
                              "cold_review": {"verdict": "pass", "independent": False, "trace_external_id": "CR-G",
                                              "independence_reason": "authored_section", "open_finding_ids": ["F-1", "F-2"], "stale_traces": []}}}}))
            # no current selection: process next routes nothing and says so
            self.send(200, json.dumps({"data": {}}))
            return
        self.send(404, '{"error":"not found"}')
    def do_POST(self):
        self.record()
        p = self.path.split("?")[0]
        if p == "/api/v1/sync/author":
            if not self.authorized():
                return
            n = int(self.headers.get("Content-Length", "0"))
            body = json.loads(self.rfile.read(n) or b"{}")
            if body.get("action") == "release_activate":
                if not body.get("pin"):
                    return self.send(403, '{"error":{"reason":"pin_required"}}')
                slug = body.get("slug"); RELEASES[slug] = "active"
                gid = "GATE-RELEASE-" + slug
                if not any(g["external_id"] == gid for g in GATES):
                    GATES.append({"external_id": gid, "kind": "approval_request", "gate_class": "human",
                                  "purpose": "release_selection", "state": "answered", "answer": "approve",
                                  "answered_at": "2026-09-14T12:00:00Z", "source_tag": body.get("source"),
                                  "chosen_option_keys": ["approve"], "exact_scope": ["release:" + slug],
                                  "options": [{"key": "approve", "label": "Select"}], "title": "Release " + slug})
                return self.send(200, json.dumps({"data": {"release_activation": {"slug": slug, "status": "active", "gate_ref": gid}}}))
            if body.get("action") == "create" and (body.get("record") or {}).get("kind") == "backlog":
                rec = body["record"]
                return self.send(200, json.dumps({"data": {"backlog": {"external_id": rec.get("external_id"), "disposition": "OPEN", "fingerprint": "f" * 64, "observation": rec.get("observation")}}}))
            if body.get("action") == "store_backed_declare":
                changed = PROCESS_STORE.get("state") != "active"
                if changed:
                    PROCESS_STORE.update({"state": "active", "gate_ref": "GATE-STORE-BACKED", "source_tag": body.get("source")})
                decl = dict(PROCESS_STORE); decl["changed"] = changed
                return self.send(200, json.dumps({"data": {"store_backed_declaration": decl}}))
            return self.send(422, '{"error":{"details":{"action":["is invalid"]}}}')
        if p.startswith("/api/systems/") and p.endswith("/export/jobs"):
            if self.authorized():
                base = p + "/job1"
                self.send(202, json.dumps({"id": "job1", "status": "queued", "poll_path": base, "download_path": base + "/file"}))
            return
        self.send(404, '{"error":"not found"}')

HTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()
PY

port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"
url="http://127.0.0.1:$port"
log="$work/requests.log"
: > "$log"
FAKE_LOG="$log" FAKE_CTX_FLAG="$work/ctx.flag" python3 "$work/fake_server.py" "$port" &
server_pid=$!
for _ in $(seq 1 50); do curl -sf "$url/_health" >/dev/null 2>&1 && break; sleep 0.1; done

fail=0
pass() { echo "PASS $1 — $2"; }
failed() { echo "FAIL $1 — $2"; fail=1; }

fresh_workspace() {
  local ws="$work/ws-$1"
  rm -rf "$ws"; mkdir -p "$ws"; (cd "$ws" && git init -q)
  echo "$ws"
}

# ------------------------------------------------- SCN-CLI020-001: the on-ramp
# GIVEN a fresh checkout, no config and no credential
# WHEN init runs (non-interactive) THEN it stops naming the auth command,
# lists nothing and binds nothing; signed in, it lists, binds and factory
# status shows the system; no later verb answers a bare 401.
scn001() {
  local ws out rc
  ws="$(fresh_workspace 001)"
  : > "$log"
  out="$(cd "$ws" && "$bin" init --api-url "$url" --system-id 1 </dev/null 2>&1)"; rc=$?
  if [ "$rc" -eq 0 ]; then failed SCN-CLI020-001 "init without a credential exited 0"; echo "$out" | sed 's/^/    /'; return; fi
  if ! grep -q "modernpath auth --api-url=$url" <<<"$out"; then failed SCN-CLI020-001 "init without a credential did not name the auth command"; echo "$out" | sed 's/^/    /'; return; fi
  if grep -q "GET /api/systems " "$log"; then failed SCN-CLI020-001 "init listed systems with no credential"; cat "$log" | sed 's/^/    /'; return; fi
  if [ -f "$ws/.modernpath/config.json" ]; then failed SCN-CLI020-001 "init wrote a binding without a credential"; return; fi

  mkdir -p "$ws/.modernpath"
  printf 'auth.json\nconfig.json\n' > "$ws/.modernpath/.gitignore"
  printf '{"token":"good-token","expires_at":"2099-01-01T00:00:00Z","actor":"tester@example.test"}\n' > "$ws/.modernpath/auth.json"
  : > "$log"
  out="$(cd "$ws" && "$bin" init --api-url "$url" </dev/null 2>&1)"; rc=$?
  if [ "$rc" -ne 0 ]; then failed SCN-CLI020-001 "signed-in init with one system failed ($rc)"; echo "$out" | sed 's/^/    /'; return; fi
  if ! grep -q '"system_id": 1' "$ws/.modernpath/config.json" 2>/dev/null; then failed SCN-CLI020-001 "init did not bind system 1"; return; fi
  out="$(cd "$ws" && "$bin" factory status 2>&1)"
  if ! grep -q "System:  Demo System (ID: 1)" <<<"$out"; then failed SCN-CLI020-001 "factory status does not show the bound system"; echo "$out" | sed 's/^/    /'; return; fi

  # A later verb with the credential gone: a statement, not a bare 401.
  rm "$ws/.modernpath/auth.json"
  : > "$log"
  out="$(cd "$ws" && "$bin" docs sync 2>&1)"
  if grep -q "HTTP 401" <<<"$out" || ! grep -q "modernpath auth --api-url=$url" <<<"$out"; then failed SCN-CLI020-001 "docs sync without a credential answered a bare 401 or named no auth command"; echo "$out" | sed 's/^/    /'; return; fi
  if [ -s "$log" ]; then failed SCN-CLI020-001 "docs sync sent a request with no credential"; cat "$log" | sed 's/^/    /'; return; fi
  pass SCN-CLI020-001 "init signs in first, binds, status shows the system; docs sync names the auth command"
}

# --------------------------------------------- SCN-CLI020-002: the declaration
# GIVEN a bound system that never had file ledgers and no marker
# WHEN store-backed mode is declared through the tool with a USER: source
# THEN the marker is written in the documented shape, process next and status
# disclose store-backed on their own line, and install --check withholds the
# ledger skill.
scn002() {
  local ws out rc
  ws="$(fresh_workspace 002)"
  mkdir -p "$ws/.modernpath"
  printf 'auth.json\nconfig.json\n' > "$ws/.modernpath/.gitignore"
  printf '{"api_url":"%s","system_id":1,"system_name":"Demo System"}\n' "$url" > "$ws/.modernpath/config.json"
  printf '{"token":"good-token","expires_at":"2099-01-01T00:00:00Z","actor":"tester@example.test"}\n' > "$ws/.modernpath/auth.json"
  : > "$log"
  out="$(cd "$ws" && "$bin" install --store-backed --source "USER:2026-09-14:declare the demo system store-backed" 2>&1)"; rc=$?
  if [ "$rc" -ne 0 ]; then failed SCN-CLI020-002 "install --store-backed failed ($rc)"; echo "$out" | sed 's/^/    /'; return; fi
  if ! grep -q "POST /api/v1/sync/author " "$log"; then failed SCN-CLI020-002 "no authoring call was made"; cat "$log" | sed 's/^/    /'; return; fi
  if [ ! -f "$ws/process/store-backed.md" ]; then failed SCN-CLI020-002 "no marker written"; echo "$out" | sed 's/^/    /'; return; fi
  if ! grep -q "^- \*\*Accepted:\*\* USER:2026-09-14:declare the demo system store-backed (gate GATE-STORE-BACKED)$" "$ws/process/store-backed.md" \
     || ! grep -q "^- \*\*Server:\*\* $url · system 1$" "$ws/process/store-backed.md" \
     || grep -Eq "^retired: [^ ]+$" "$ws/process/store-backed.md"; then
    failed SCN-CLI020-002 "marker is not in the documented shape"; sed 's/^/    /' "$ws/process/store-backed.md"; return
  fi
  out="$(cd "$ws" && "$bin" status 2>&1)"
  if ! grep -qi "store-backed" <<<"$out"; then failed SCN-CLI020-002 "status does not disclose store-backed"; echo "$out" | sed 's/^/    /'; return; fi
  out="$(cd "$ws" && "$bin" process next 2>&1)"
  if ! grep -q "^store-backed workspace" <<<"$out"; then failed SCN-CLI020-002 "process next does not disclose store-backed on its own line"; echo "$out" | sed 's/^/    /'; return; fi
  if [ -f "$ws/.claude/skills/rdd-ledger/SKILL.md" ]; then failed SCN-CLI020-002 "the ledger skill was installed under the marker"; return; fi
  out="$(cd "$ws" && "$bin" install --check 2>&1)"; rc=$?
  if [ "$rc" -ne 0 ]; then failed SCN-CLI020-002 "install --check is not clean after the declaration"; echo "$out" | sed 's/^/    /'; return; fi
  pass SCN-CLI020-002 "declared in one step; marker in shape; status and process next disclose it; ledger skill withheld"
}

# ------------------------------------------ SCN-CLI020-003: release activation
# GIVEN a release exists WHEN factory release activate runs without a PIN,
# then with the human's PIN THEN the first is refused naming who holds the PIN
# and the verb; the second records the answered release_selection gate with
# the USER: source, and factory gates and working-set pull serve it.
scn003() {
  local ws out rc
  ws="$(fresh_workspace 003)"
  mkdir -p "$ws/.modernpath"
  printf 'auth.json\nconfig.json\n' > "$ws/.modernpath/.gitignore"
  printf '{"api_url":"%s","system_id":1,"system_name":"Demo System"}\n' "$url" > "$ws/.modernpath/config.json"
  printf '{"token":"good-token","expires_at":"2099-01-01T00:00:00Z","actor":"tester@example.test"}\n' > "$ws/.modernpath/auth.json"
  mkdir -p "$ws/process"; printf '# Store-backed declaration\n\n- **Accepted:** USER:x (gate GATE-STORE-BACKED)\n- **Server:** %s · system 1\n\n' "$url" > "$ws/process/store-backed.md"
  out="$(cd "$ws" && "$bin" factory release activate modernpath-v1-10 --source "USER:2026-09-14:activate v1-10" 2>&1)"; rc=$?
  if [ "$rc" -eq 0 ]; then failed SCN-CLI020-003 "activation without a PIN was accepted"; echo "$out" | sed 's/^/    /'; return; fi
  if ! grep -q "release PIN" <<<"$out" || ! grep -q "Mission Control" <<<"$out" || ! grep -q -- "--pin" <<<"$out"; then failed SCN-CLI020-003 "the PIN refusal does not name the holder, Mission Control and --pin"; echo "$out" | sed 's/^/    /'; return; fi
  out="$(cd "$ws" && "$bin" factory release activate modernpath-v1-10 --source "USER:2026-09-14:activate v1-10" --pin 1234 2>&1)"; rc=$?
  if [ "$rc" -ne 0 ]; then failed SCN-CLI020-003 "activation with the PIN failed ($rc)"; echo "$out" | sed 's/^/    /'; return; fi
  out="$(cd "$ws" && "$bin" factory gates GATE-RELEASE-modernpath-v1-10 2>&1)"; rc=$?
  if [ "$rc" -ne 0 ] || ! grep -q "USER:2026-09-14:activate v1-10" <<<"$out"; then failed SCN-CLI020-003 "factory gates does not serve the release gate with its source"; echo "$out" | sed 's/^/    /'; return; fi
  out="$(cd "$ws" && "$bin" working-set pull GATE-RELEASE-modernpath-v1-10 2>&1)"; rc=$?
  if [ "$rc" -ne 0 ] || ! grep -q "USER:2026-09-14:activate v1-10" "$ws/.modernpath/working-set/GATE-RELEASE-modernpath-v1-10.md" 2>/dev/null; then failed SCN-CLI020-003 "working-set pull does not serve the release gate"; echo "$out" | sed 's/^/    /'; return; fi
  out="$(cd "$ws" && "$bin" factory status 2>&1)"
  if ! grep -q "active release modernpath-v1-10 — USER:2026-09-14:activate v1-10 (gate GATE-RELEASE-modernpath-v1-10)" <<<"$out"; then failed SCN-CLI020-003 "factory status does not name the active release with its source"; echo "$out" | sed 's/^/    /'; return; fi
  pass SCN-CLI020-003 "pin_required names the holder and --pin; the activation records the gate; gates, pull and status serve it"
}

# ---------------------------------------------- SCN-CLI020-004: the reasons
# GIVEN a selection whose cold-review or completion check fails WHEN process
# check runs THEN the FAIL line names the trace and the reason, or the
# completion facts not met; a your-move [Drift] label carries its reason.
# Exercised by the sandboxed suites: the server facts (Elixir) and the two
# renderers (Go). Set MP_UR_SKIP_ELIXIR=1 to run only the renderer half.
scn004() {
  local ws out
  ws="$(fresh_workspace 004)"
  mkdir -p "$ws/.modernpath" "$ws/process"
  printf 'auth.json\nconfig.json\n' > "$ws/.modernpath/.gitignore"
  printf '{"api_url":"%s","system_id":1,"system_name":"Demo System"}\n' "$url" > "$ws/.modernpath/config.json"
  printf '{"token":"good-token","expires_at":"2099-01-01T00:00:00Z","actor":"tester@example.test"}\n' > "$ws/.modernpath/auth.json"
  printf '# Store-backed declaration\n\n- **Accepted:** USER:x (gate GATE-STORE-BACKED)\n- **Server:** %s · system 1\n\n' "$url" > "$ws/process/store-backed.md"
  : > "$work/ctx.flag"
  out="$(cd "$ws" && "$bin" process check --phase cold_review 2>&1)"
  rm -f "$work/ctx.flag"
  if ! grep -q "FAIL         independent_verdict" <<<"$out" \
     || ! grep -q "trace CR-G (verdict PASS): not independent: the review context authored a section of this scope" <<<"$out" \
     || ! grep -q "open or deferred material findings: F-1, F-2" <<<"$out"; then
    failed SCN-CLI020-004 "the check does not name the trace, the reason and the finding ids"; echo "$out" | sed 's/^/    /'; return
  fi
  out="$(cd "$here" && go test -count=1 ./cmd -run 'TestREQCROSS408ProcessCheckNamesTheReasons|TestYourMoveDriftLineCarriesItsBasisAndTheRemedy' 2>&1)" || { failed SCN-CLI020-004 "renderer tests fail"; echo "$out" | tail -20 | sed 's/^/    /'; return; }
  if [ "${MP_UR_SKIP_ELIXIR:-0}" != "1" ]; then
    out="$(cd "$here/../.." && DATABASE_PORT="${DATABASE_PORT:-5434}" mix test apps/core/test/core/rdd/phase_facts_test.exs apps/core/test/core/rdd/delivery_context_facts_test.exs 2>&1)" || { failed SCN-CLI020-004 "facts tests fail"; echo "$out" | tail -20 | sed 's/^/    /'; return; }
  fi
  pass SCN-CLI020-004 "check FAIL lines carry the trace, the reason and the finding ids; completion names its facts; [Drift] carries its basis"
}

# ------------------------------------------- SCN-CLI020-005: the reviewer
# GIVEN a workspace where install has run WHEN a cold review is delegated
# THEN .claude/agents/rdd-cold-reviewer.md exists as installed by the kit,
# carries no shell, and install --check reports a hand-edited copy as drift.
scn005() {
  local ws out rc
  ws="$(fresh_workspace 005)"
  (cd "$ws" && "$bin" install >/dev/null 2>&1) || { failed SCN-CLI020-005 "install failed"; return; }
  if [ ! -f "$ws/.claude/agents/rdd-cold-reviewer.md" ]; then failed SCN-CLI020-005 "the reviewer definition is not installed"; return; fi
  if ! grep -q "^tools: Read, Grep, Glob$" "$ws/.claude/agents/rdd-cold-reviewer.md" || grep -q "Bash" "$ws/.claude/agents/rdd-cold-reviewer.md"; then failed SCN-CLI020-005 "the reviewer definition carries a shell or lacks its tools line"; return; fi
  out="$(cd "$ws" && "$bin" install --check 2>&1)"; rc=$?
  if [ "$rc" -ne 0 ]; then failed SCN-CLI020-005 "install --check is not clean after install"; echo "$out" | sed 's/^/    /'; return; fi
  printf '\ntools: Bash\n' >> "$ws/.claude/agents/rdd-cold-reviewer.md"
  out="$(cd "$ws" && "$bin" install --check 2>&1)"; rc=$?
  if [ "$rc" -eq 0 ] || ! grep -q ".claude/agents/rdd-cold-reviewer.md" <<<"$out"; then failed SCN-CLI020-005 "a hand-edited reviewer is not reported as drift"; echo "$out" | sed 's/^/    /'; return; fi
  pass SCN-CLI020-005 "the reviewer definition is installed without a shell; a hand-edited copy is drift"
}

# ---------------------------------------- SCN-CLI020-006: feedback --last
# GIVEN a refusing command followed by an unrelated command WHEN feedback
# --last runs THEN it shows the command it attaches before writing and --ref
# picks the refusing one; the record carries that command and its output.
scn006() {
  local ws out rc
  ws="$(fresh_workspace 006)"
  mkdir -p "$ws/.modernpath"
  printf 'auth.json\nconfig.json\ncli-history.log\n' > "$ws/.modernpath/.gitignore"
  printf '{"api_url":"%s","system_id":1,"system_name":"Demo System"}\n' "$url" > "$ws/.modernpath/config.json"
  printf '{"token":"good-token","expires_at":"2099-01-01T00:00:00Z","actor":"tester@example.test"}\n' > "$ws/.modernpath/auth.json"
  (cd "$ws" && "$bin" factory gates GATE-NOPE >/dev/null 2>&1)   # refuses: no such gate
  (cd "$ws" && "$bin" factory status >/dev/null 2>&1)             # unrelated
  out="$(cd "$ws" && "$bin" feedback --last "status said nothing new" 2>&1)"; rc=$?
  if [ "$rc" -ne 0 ]; then failed SCN-CLI020-006 "feedback --last failed ($rc)"; echo "$out" | sed 's/^/    /'; return; fi
  if ! grep -q "attaching: modernpath factory status (exit 0)" <<<"$out"; then failed SCN-CLI020-006 "--last does not show the entry it attaches"; echo "$out" | sed 's/^/    /'; return; fi
  if [ "$(grep -n "attaching:" <<<"$out" | cut -d: -f1)" -gt "$(grep -n "filed BACKLOG-TOOL" <<<"$out" | cut -d: -f1)" ]; then failed SCN-CLI020-006 "the entry is shown after the record id"; echo "$out" | sed 's/^/    /'; return; fi
  : > "$log"
  out="$(cd "$ws" && "$bin" feedback --ref 3 "the gate refusal named no next verb" 2>&1)"; rc=$?
  if [ "$rc" -ne 0 ] || ! grep -q "attaching: modernpath factory gates GATE-NOPE (exit 1)" <<<"$out"; then failed SCN-CLI020-006 "--ref 3 does not attach the refusing command"; echo "$out" | sed 's/^/    /'; return; fi
  if ! grep -q "POST /api/v1/sync/author " "$log"; then failed SCN-CLI020-006 "no record was written"; return; fi
  out="$(cd "$ws" && "$bin" feedback --ref 99 "x" 2>&1)"; rc=$?
  if [ "$rc" -eq 0 ] || ! grep -q "are recorded" <<<"$out"; then failed SCN-CLI020-006 "--ref beyond the count is not refused naming the count"; echo "$out" | sed 's/^/    /'; return; fi
  pass SCN-CLI020-006 "--last shows the entry before writing; --ref picks the refusing command; a value beyond the count is refused"
}

scn001
scn002
scn003
scn004
scn005
scn006

exit $fail
