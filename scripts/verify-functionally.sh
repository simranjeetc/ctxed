#!/usr/bin/env bash
# Functional verification for ctxed overview — the gate.
#
# Creates a REAL session in each harness (Claude Code / OpenCode) with two
# planted topics — one finished, one left open — runs `ctxed overview` on it
# the way the ctxed-overview skill does, compacts the session, and runs it
# again. It asserts on observable outcomes:
#   - the session is found from the harness's environment variable;
#   - rows add up to the header, and the header equals `ctxed inspect`;
#   - the session is unchanged by the overview;
#   - after a compaction, earlier messages are not counted and the compaction
#     is its own row;
#   - with the harness's own cheap model, the finished topic is "done", the open
#     one "in_progress", and the open item is listed as pending.
#
# Usage:
#   scripts/verify-functionally.sh --opencode
#   scripts/verify-functionally.sh --claude
#   scripts/verify-functionally.sh --all
#   scripts/verify-functionally.sh --opencode --keep   # keep scratch on failure
#   scripts/verify-functionally.sh --opencode --report <path>
#
# Exit 0 only when every hard check passed. The status checks use a real model
# and are hard; the pending-item check is soft (reported, never decides `ok`).
# Spends a few small model calls per scenario. See docs/verification-strategy.md.
#
# The script builds ctxed from this checkout and reports missing prerequisites
# by name. Side effects outside the scratch directories, undone on exit (also on
# failure and on Ctrl-C):
#   - --opencode: the scratch OpenCode server shares the real session store; the
#     scratch session is deleted on exit.
#   - --claude: Claude Code creates a project directory under ~/.claude/projects
#     for the scratch session; it is deleted (kept with --keep on failure).
#
# Configuration (all optional):
#   CTXED_TEST_EXTRA_PATH       prepended to PATH, for tools installed off-PATH
#   CTXED_TEST_OPENCODE_MODEL   default opencode-go/deepseek-v4-flash
#   CTXED_TEST_CLAUDE_MODEL     default haiku
#   CTXED_TEST_KEEP=1           same as --keep

set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
[[ -n "${CTXED_TEST_EXTRA_PATH:-}" ]] && export PATH="$CTXED_TEST_EXTRA_PATH:$PATH"

# --- configuration (all optional) -------------------------------------------
# Default to an OpenCode Go model: on this machine only OpenCode Go is entitled
# for OpenCode, so a silent fallback to whatever `opencode models` lists first
# (e.g. github-copilot/…) would fail on a fresh clone. Pin an explicit default
# and fail loudly if it cannot be resolved.
OPENCODE_MODEL="${CTXED_TEST_OPENCODE_MODEL:-opencode-go/deepseek-v4-flash}"
CLAUDE_MODEL="${CTXED_TEST_CLAUDE_MODEL:-haiku}"
KEEP="${CTXED_TEST_KEEP:-0}"

FAILURES=0
PASSES=0
SOFT_PASSES=0
SOFT_FAILS=0
SCRATCH=()
SCRATCH_SESSIONS=()  # OpenCode session ids created on the scratch server, deleted on exit
CLI_SESSIONS=()      # OpenCode session ids created through the CLI, deleted on exit
CLAUDE_PROJECTS=()   # ~/.claude/projects/<dir> created for scratch sessions, deleted on exit
OC_PORT=""
OC_LAST_ID=""        # id of the user message the last oc_prompt created
CHECKS=()            # "status<TAB>name<TAB>detail" for the JSON report
REPORT=""            # optional report path (--report json writes here)
MODEL_USED=""


RUN_OPENCODE=0
RUN_CLAUDE=0
SUITE=""

args=("$@")
i=0
while [[ $i -lt ${#args[@]} ]]; do
  arg="${args[$i]}"
  case "$arg" in
    --opencode) RUN_OPENCODE=1 ;;
    --claude)   RUN_CLAUDE=1 ;;
    --all)      RUN_OPENCODE=1; RUN_CLAUDE=1 ;;
    --keep)     KEEP=1 ;;
    --report)   i=$((i + 1)); REPORT="${args[$i]:-}" ;;
    --report=*) REPORT="${arg#--report=}" ;;
    -h|--help)  sed -n '2,40p' "$0"; exit 0 ;;
    *) echo "unknown flag: $arg" >&2; exit 2 ;;
  esac
  i=$((i + 1))
done

[[ $RUN_OPENCODE -eq 1 || $RUN_CLAUDE -eq 1 ]] || { echo "pick --opencode, --claude, or --all" >&2; exit 2; }
if [[ $RUN_OPENCODE -eq 1 && $RUN_CLAUDE -eq 1 ]]; then
  SUITE="all"
elif [[ $RUN_OPENCODE -eq 1 ]]; then
  SUITE="opencode"
else
  SUITE="claude"
fi

cleanup() {
  # Delete any OpenCode session a scenario created, so a real store is never
  # left with scratch sessions.
  if [[ ${#SCRATCH_SESSIONS[@]} -gt 0 && -n "$OC_PORT" ]]; then
    for s in "${SCRATCH_SESSIONS[@]}"; do
      oc_api "$OC_PORT" DELETE "/api/session/$s" >/dev/null 2>&1 || true
    done
  fi
  for s in "${CLI_SESSIONS[@]:-}"; do
    [[ -n "$s" ]] && opencode session delete "$s" >/dev/null 2>&1
  done
  [[ -n "${OC_DIR:-}" ]] && stop_opencode_server "$OC_DIR"
  if [[ "$KEEP" == "1" && $FAILURES -gt 0 ]]; then
    echo "kept scratch: ${SCRATCH[*]:- (none)} ${CLAUDE_PROJECTS[*]:-}"
    [[ -n "$REPORT" ]] && write_report
    return
  fi
  for s in "${CLAUDE_PROJECTS[@]:-}"; do [[ -n "$s" ]] && rm -rf "$s" 2>/dev/null; done
  for s in "${SCRATCH[@]:-}"; do [[ -n "$s" ]] && rm -rf "$s" 2>/dev/null; done
  [[ -n "$REPORT" ]] && write_report
}
trap cleanup EXIT
# Turn an interrupt into a normal exit so the EXIT trap restores everything.
trap 'exit 130' INT
trap 'exit 143' TERM

pass() {
  PASSES=$((PASSES + 1))
  CHECKS+=("pass	$1	${2:-}")
  printf 'ok   %s\n' "$1"
}
fail() {
  FAILURES=$((FAILURES + 1))
  CHECKS+=("fail	$1	${2:-}")
  printf 'FAIL %s\n' "$1"
  [[ -n "${2:-}" ]] && printf '     | %s\n' "$2"
  return 0
}
# soft <0|1> <name> [detail] — a model-mediated check. 0 is a soft pass. Soft
# checks are reported but never decide `ok` or the exit code.
soft() {
  if [[ "$1" == 0 ]]; then
    SOFT_PASSES=$((SOFT_PASSES + 1))
    CHECKS+=("soft-pass	$2	${3:-}")
    printf 'ok~  %s\n' "$2"
  else
    SOFT_FAILS=$((SOFT_FAILS + 1))
    CHECKS+=("soft-fail	$2	${3:-}")
    printf 'SOFT %s\n' "$2"
    [[ -n "${3:-}" ]] && printf '     | %s\n' "$3"
  fi
  return 0
}

# Emits the verifier's contract: what ran, what passed, what to do next.
# `passed`/`failed` count hard checks only; `ok` is computed from them alone.
write_report() {
  python3 - "$REPORT" "$SUITE" "$MODEL_USED" "$PASSES" "$FAILURES" "$SOFT_PASSES" "$SOFT_FAILS" "${CHECKS[@]}" <<'PY'
import json, sys
path, suite, model, passed, failed, soft_passed, soft_failed, *checks = sys.argv[1:]
rows = []
for c in checks:
    parts = c.split("\t")
    rows.append({"status": parts[0], "name": parts[1], "detail": parts[2] if len(parts) > 2 else ""})
report = {
    "suite": suite,
    "model": model,
    "passed": int(passed),
    "failed": int(failed),
    "softPassed": int(soft_passed),
    "softFailed": int(soft_failed),
    "ok": int(failed) == 0,
    "checks": rows,
}
open(path, "w").write(json.dumps(report, indent=2))
PY
  echo "report: $REPORT"
}
need() { command -v "$1" >/dev/null 2>&1 || { echo "missing prerequisite: $1 ($2)" >&2; exit 3; }; }
# mkscratch — sets SCRATCH_DIR to a new temp dir, removed on exit. Not called
# in $(...): the registration must happen in this shell.
mkscratch() { SCRATCH_DIR="$(mktemp -d)"; SCRATCH+=("$SCRATCH_DIR"); }

need go "Go toolchain; the script builds ctxed from this checkout"
need python3 "Python 3"
need curl "curl"

# --- shared: build ctxed ----------------------------------------------------

# Always from this checkout: a scenario never exercises another source tree.
build_ctxed() {
  CTXED="$ROOT/.verify/ctxed"
  mkdir -p "$ROOT/.verify"
  ( cd "$ROOT" && go build -o "$CTXED" ./cmd/ctxed )
}

# ---------------------------------------------------------------------------
# OpenCode helpers
# ---------------------------------------------------------------------------

# A free TCP port, so parallel runs do not collide.
free_port() {
  python3 -c 'import socket
s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'
}

# The OpenCode v2 server needs a password; the CLI and SDK both read
# OPENCODE_PASSWORD. The scenarios start their own scratch server, so they pick
# its password per run instead of depending on (or defaulting) a credential.
OC_PASSWORD="ctxed-verify-$(python3 -c 'import secrets; print(secrets.token_hex(8))')"
opencode_password() { printf '%s' "$OC_PASSWORD"; }

# oc_api <port> <method> <path> [json-body] — prints the response body.
oc_api() {
  local port="$1" method="$2" path="$3" body="${4:-}"
  local auth; auth="$(printf '%s:%s' opencode "$(opencode_password)" | base64)"
  if [[ -n "$body" ]]; then
    curl -s -X "$method" -H "Authorization: Basic $auth" \
      -H 'Content-Type: application/json' -d "$body" "http://127.0.0.1:$port$path"
  else
    curl -s -X "$method" -H "Authorization: Basic $auth" "http://127.0.0.1:$port$path"
  fi
}

# oc_idle_state <port> <sid> — "idle:<message count>:<newest id>" when the
# session is not running and nothing waits in its inbox; otherwise
# "busy:<what it is doing>", which a timeout reports.
oc_idle_state() {
  local port="$1" sid="$2" active inbox
  active="$(oc_api "$port" GET /api/session/active | python3 -c '
import json, sys
try:
    a = json.load(sys.stdin).get("data", {}).get(sys.argv[1])
    print("free" if a is None else "active=" + str(a.get("type", a)))
except Exception: print("active=unreadable")' "$sid")"
  if [[ "$active" != free ]]; then
    # A pending permission request blocks the turn until a human answers; name it.
    local asks
    asks="$(oc_api "$port" GET "/api/session/$sid/permission" | python3 -c '
import json, sys
try: print(",".join(p.get("action", "?") for p in json.load(sys.stdin).get("data", [])))
except Exception: print("")')"
    echo "busy:$active${asks:+ awaiting-permission=$asks}"
    return
  fi
  inbox="$(oc_api "$port" GET "/api/session/$sid/inbox" | python3 -c '
import json, sys
try:
    d = json.load(sys.stdin)["data"]
    print(0 if not d else "%d(%s)" % (len(d), ",".join(str(i.get("type", "?")) for i in d)))
except Exception: print("unreadable")')"
  [[ "$inbox" == 0 ]] || { echo "busy:inbox=$inbox"; return; }
  oc_api "$port" GET "/api/session/$sid/message" | python3 -c '
import json, sys
try:
    d = json.load(sys.stdin)["data"]
    print("idle:%d:%s" % (len(d), d[0]["id"] if d else ""))
except Exception:
    print("busy:messages=unreadable")'
}

# oc_wait_idle <port> <sid> <step> [timeout-seconds] — waits until the session
# is idle with the same message list on two reads 500 ms apart. On timeout it
# records a failure naming the step and what the session was doing, and
# returns 1.
oc_wait_idle() {
  local port="$1" sid="$2" step="$3" timeout="${4:-120}"
  local deadline=$((SECONDS + timeout)) prev="" cur=""
  while [[ $SECONDS -lt $deadline ]]; do
    cur="$(oc_idle_state "$port" "$sid")"
    [[ "$cur" == idle:* && "$cur" == "$prev" ]] && return 0
    prev="$cur"
    sleep 0.5
  done
  fail "$step" "timed out after ${timeout}s waiting for the session to go idle (last state: $cur)"
  return 1
}

# oc_prompt <port> <sid> <step> <json-body> — sends a prompt, waits for the turn
# to finish, and sets OC_LAST_ID to the id of the user message it created.
oc_prompt() {
  local port="$1" sid="$2" step="$3" body="$4" resp
  resp="$(oc_api "$port" POST "/api/session/$sid/prompt" "$body")"
  OC_LAST_ID="$(python3 -c '
import json, sys
try: print(json.load(sys.stdin)["data"]["id"])
except Exception: print("")' <<<"$resp")"
  if [[ -z "$OC_LAST_ID" ]]; then
    fail "$step" "prompt rejected: $(head -c 300 <<<"$resp")"
    return 1
  fi
  oc_wait_idle "$port" "$sid" "$step"
}

stop_opencode_server() {
  local dir="$1"
  [[ -f "$dir/server.pid" ]] || return 0
  local pid; pid="$(cat "$dir/server.pid" 2>/dev/null)"
  [[ -n "$pid" ]] && kill "$pid" 2>/dev/null
  # The server may spawn a child service; kill by port as a fallback.
  [[ -n "${OC_PORT:-}" ]] && {
    local p; p="$(lsof -tiTCP:"$OC_PORT" -sTCP:LISTEN 2>/dev/null)"
    [[ -n "$p" ]] && kill $p 2>/dev/null
  }
  return 0
}


# ---------------------------------------------------------------------------
# Shared overview checks
# ---------------------------------------------------------------------------

# The planted topics. PELICAN is finished in the conversation; TURBINE ends on
# an open TODO naming a unique item, which should come back as pending.
plant_words() {
  PELICAN_MSG="TOPIC-PELICAN: rename the helper parse_date to parse_day. Reply with only: ok"
  PELICAN_DONE="TOPIC-PELICAN is finished and confirmed; nothing more to do on it. Reply with only: ok"
  TURBINE_ITEM="WIDGET-$$"
  TURBINE_MSG="TOPIC-TURBINE: TODO, still open: we must set the deploy URL for $TURBINE_ITEM. I will send the URL later. Reply with only: ok"
}

# write_stub <path> — a deterministic categorizer: groups the numbered entries
# of the prompt by topic marker (an unmarked entry joins the topic before it),
# PELICAN done and TURBINE in progress.
write_stub() {
  cat > "$1" <<'PY'
#!/usr/bin/env python3
import re, sys, json
groups, current = {"pelican": [], "turbine": []}, "pelican"
for line in sys.stdin.read().splitlines():
    m = re.match(r'- \[(\d+)\] ', line)
    if not m:
        continue
    if "TOPIC-PELICAN" in line: current = "pelican"
    elif "TOPIC-TURBINE" in line: current = "turbine"
    groups[current].append(int(m.group(1)))
print(json.dumps({"categories": [
    {"label": "Pelican topic", "status": "done", "ids": groups["pelican"]},
    {"label": "Turbine topic", "status": "in_progress", "ids": groups["turbine"]}],
    "pending": ["stub pending item"]}))
PY
  chmod +x "$1"
}

# totals_check <overview.json> <inspect.txt> — OK when the rows add up to the
# header, no entry is in two rows, and the header equals inspect's TOTAL line.
totals_check() {
  python3 - "$1" "$2" <<'PY'
import json, re, sys
try: r = json.load(open(sys.argv[1]))
except Exception as e: print("BAD overview JSON: %s" % e); sys.exit()
m = re.search(r"TOTAL (\d+) entries · ([\d,]+) tokens", open(sys.argv[2]).read())
if not m: print("BAD no TOTAL line from inspect"); sys.exit()
ie, it = int(m.group(1)), int(m.group(2).replace(",", ""))
se = sum(x["entries"] for x in r["rows"]); st = sum(x["tokens"] for x in r["rows"])
ids = [i for x in r["rows"] for i in x["entryIds"]]
if len(ids) != len(set(ids)): print("DUP an entry is in two rows"); sys.exit()
if (se, st) != (r["entries"], r["tokens"]): print("SUM rows %d/%d, header %d/%d" % (se, st, r["entries"], r["tokens"])); sys.exit()
if (ie, it) != (r["entries"], r["tokens"]): print("INSPECT overview %d/%d, inspect %d/%d" % (r["entries"], r["tokens"], ie, it)); sys.exit()
print("OK %d messages, ~%d tokens" % (ie, it))
PY
}

# status_check <overview.json> <pelican-id> <turbine-id> <item> — reads the rows
# holding the planted messages: OK when PELICAN's is done and TURBINE's is
# in_progress (and they are different rows). Prints PENDING_OK/PENDING_MISSING
# on a second line for the soft pending check.
status_check() {
  python3 - "$@" <<'PY'
import json, sys
r = json.load(open(sys.argv[1])); pel, tur, item = sys.argv[2:5]
row = lambda i: next((x for x in r["rows"] if i in x["entryIds"]), None)
p, t = row(pel), row(tur)
if p is None or t is None: print("MISSING a planted message is in no row")
elif p is t: print("SAME both topics landed in one row (%s)" % p["label"])
elif p.get("status") != "done" or t.get("status") != "in_progress":
    print("STATUS pelican=%r (%s) turbine=%r (%s)" % (p.get("status"), p["label"], t.get("status"), t["label"]))
else: print("OK %s=done, %s=in_progress" % (p["label"], t["label"]))
pend = " ".join(r.get("pending") or [])
print("PENDING_OK" if item.lower() in pend.lower() or "url" in pend.lower() else "PENDING_MISSING " + pend[:200])
PY
}

# compacted_check <overview.json> — OK when earlier messages are reported as
# not counted and the last row is the compaction.
compacted_check() {
  python3 - "$1" <<'PY'
import json, sys
r = json.load(open(sys.argv[1]))
last = r["rows"][-1] if r["rows"] else {}
if r.get("compacted", 0) <= 0: print("NOCOUNT compacted=%s" % r.get("compacted")); sys.exit()
if last.get("kind") != "summary": print("NOROW last row %r" % last.get("label")); sys.exit()
print("OK %d earlier messages not counted; %s ~%d tokens" % (r["compacted"], last["label"], last["tokens"]))
PY
}

# overview_checks <tag> <session-file> <run-overview-fn> — the checks shared by
# both harnesses, on one snapshot. <run-overview-fn> <out.json> [args…] runs
# `ctxed overview --json` the way the skill does (from the environment).
overview_checks() {
  local tag="$1" snap="$2" runfn="$3" dir="$4"
  local out="$dir/$tag-stub.json" v
  if CTXED_CATEGORIZER_CMD="$dir/stub.py" "$runfn" "$out" 2>"$dir/$tag-stub.err"; then
    pass "$tag:overview runs from the session's environment variable"
  else
    fail "$tag:overview runs from the session's environment variable" "$(tail -2 "$dir/$tag-stub.err")"; return 1
  fi
  "$CTXED" inspect "$snap" > "$dir/$tag-inspect.txt" 2>&1
  v="$(totals_check "$out" "$dir/$tag-inspect.txt")"
  [[ "$v" == OK* ]] && pass "$tag:totals add up and equal inspect (${v#OK })" || fail "$tag:totals add up and equal inspect" "$v"
}

# ---------------------------------------------------------------------------
# Claude Code scenario
# ---------------------------------------------------------------------------

# claude_project_dir <dir> — the directory Claude Code keeps sessions started in
# <dir> under: its real path with every non-alphanumeric character replaced.
claude_project_dir() {
  local real; real="$(cd "$1" && pwd -P)"
  printf '%s/.claude/projects/%s\n' "$HOME" "${real//[^a-zA-Z0-9]/-}"
}

# claude_turn <dir> <sid|""> <prompt> — one headless turn; prints the session id.
claude_turn() {
  local dir="$1" sid="$2" prompt="$3"
  local resume=(--model "$CLAUDE_MODEL"); [[ -n "$sid" ]] && resume+=(--resume "$sid")
  ( cd "$dir" && claude -p "${resume[@]}" --model "$CLAUDE_MODEL" --output-format json "$prompt" 2>>"$dir/err" ) \
    | python3 -c 'import json,sys
try: o=json.load(sys.stdin); print(o.get("session_id") or "")
except Exception: print("")'
}

# claude_msg_id <transcript> <marker> — uuid of the last user line containing marker.
claude_msg_id() {
  python3 - "$1" "$2" <<'PY'
import json, sys
found = ""
for line in open(sys.argv[1]):
    try: o = json.loads(line)
    except Exception: continue
    if o.get("type") == "user" and sys.argv[2] in json.dumps(o.get("message")):
        found = o.get("uuid", "")
print(found)
PY
}

CL_DIR=""; CL_SID=""
claude_overview() { # <out.json> — as the skill runs it: inside the session's env
  ( cd "$CL_DIR" && env -u OPENCODE_SESSION_ID CLAUDE_CODE_SESSION_ID="$CL_SID" "$CTXED" overview --json ) > "$1"
}

claude_scenario() {
  need claude "Claude Code CLI, authenticated"
  build_ctxed || { fail "claude:build ctxed"; return; }
  mkscratch; local dir="$SCRATCH_DIR"; CL_DIR="$dir"
  MODEL_USED="$CLAUDE_MODEL"
  local project; project="$(claude_project_dir "$dir")"
  if [[ -e "$project" ]]; then
    fail "claude:scratch project" "$project already exists; refusing to reuse it"; return
  fi
  CLAUDE_PROJECTS+=("$project")
  plant_words; write_stub "$dir/stub.py"

  local sid; sid="$(claude_turn "$dir" "" "$PELICAN_MSG")"
  [[ -n "$sid" ]] || { fail "claude:create session" "$(tail -2 "$dir/err" 2>/dev/null)"; return; }
  CL_SID="$sid"
  [[ -n "$(claude_turn "$dir" "$sid" "$PELICAN_DONE")" ]] \
    && [[ -n "$(claude_turn "$dir" "$sid" "$TURBINE_MSG")" ]] \
    || { fail "claude:planted topics" "$(tail -2 "$dir/err" 2>/dev/null)"; return; }
  local transcript="$project/$sid.jsonl"
  [[ -f "$transcript" ]] || { fail "claude:find transcript" "no $transcript"; return; }
  pass "claude:session with two planted topics ($sid)"

  # Read-only: the transcript is byte-identical after the overview.
  local sha_before; sha_before="$(shasum "$transcript" | cut -d' ' -f1)"
  overview_checks claude "$transcript" claude_overview "$dir" || return
  [[ "$(shasum "$transcript" | cut -d' ' -f1)" == "$sha_before" ]] \
    && pass "claude:transcript unchanged by the overview" \
    || fail "claude:transcript unchanged by the overview"

  # The harness's own cheap model reads the status.
  local real="$dir/claude-real.json" v
  if CTXED_CATEGORIZER_MODEL="$CLAUDE_MODEL" claude_overview "$real" 2>"$dir/real.err"; then
    v="$(status_check "$real" "$(claude_msg_id "$transcript" PELICAN)" "$(claude_msg_id "$transcript" TURBINE)" "$TURBINE_ITEM")"
    [[ "$(head -1 <<<"$v")" == OK* ]] && pass "claude:real model status ($(head -1 <<<"$v" | cut -c4-))" \
      || fail "claude:real model status" "$(head -1 <<<"$v"); stderr: $(tail -1 "$dir/real.err")"
    [[ "$(tail -1 <<<"$v")" == PENDING_OK ]]; soft $? "claude:open item listed as pending" "$(tail -1 <<<"$v")"
  else
    fail "claude:real model overview" "$(tail -2 "$dir/real.err")"
  fi

  # Compact, then the overview counts only what follows.
  ( cd "$dir" && claude -p --resume "$sid" --model "$CLAUDE_MODEL" "/compact" >/dev/null 2>>"$dir/err" ) \
    || { fail "claude:compact" "$(tail -2 "$dir/err")"; return; }
  overview_checks claude-compacted "$transcript" claude_overview "$dir" || return
  v="$(compacted_check "$dir/claude-compacted-stub.json")"
  [[ "$v" == OK* ]] && pass "claude:after /compact (${v#OK })" || fail "claude:after /compact" "$v"
  # Negative control: the same check against the pre-compaction inspect must
  # fail, so the totals check can tell the two views apart.
  v="$(totals_check "$dir/claude-compacted-stub.json" "$dir/claude-inspect.txt")"
  [[ "$v" != OK* ]] && pass "claude:negative control (pre-compaction totals do not match: ${v%% *})" \
    || fail "claude:negative control" "the compacted overview matched the pre-compaction inspect"
}

# ---------------------------------------------------------------------------
# OpenCode scenario
# ---------------------------------------------------------------------------

OC_DIR=""; OC_SID=""
oc_overview() { # <out.json> — as the skill runs it: inside the session's env
  env -u CLAUDE_CODE_SESSION_ID OPENCODE_SESSION_ID="$OC_SID" "$CTXED" overview --json > "$1"
}

# oc_snapshot <out> — the session's export, written the way ctxed reads it.
oc_snapshot() { opencode session export "$OC_SID" > "$1" 2>/dev/null; }

# oc_msg_id <export> <marker> — id of the last user message containing marker.
oc_msg_id() {
  python3 - "$1" "$2" <<'PY'
import json, sys
found = ""
for m in json.load(open(sys.argv[1])).get("messages", []):
    if m.get("type") == "user" and sys.argv[2] in json.dumps(m):
        found = m.get("id", "")
print(found)
PY
}

# messages_digest <export> — a digest of the stored messages (not of export
# metadata such as timestamps of the export itself).
messages_digest() {
  python3 -c 'import json,sys,hashlib
print(hashlib.sha256(json.dumps(json.load(open(sys.argv[1])).get("messages"), sort_keys=True).encode()).hexdigest())' "$1"
}

opencode_scenario() {
  need opencode "OpenCode CLI, authenticated"
  need lsof "lsof, to stop the scratch server"
  build_ctxed || { fail "opencode:build ctxed"; return; }
  local model="$OPENCODE_MODEL" ok=0
  for _ in $(seq 1 10); do
    opencode models 2>/dev/null | grep -qx "$model" && { ok=1; break; }
    sleep 1
  done
  [[ $ok -eq 1 ]] || { fail "opencode:model" "'$model' is not in \`opencode models\`; set CTXED_TEST_OPENCODE_MODEL"; return; }
  MODEL_USED="$model"
  mkscratch; local dir="$SCRATCH_DIR"; OC_DIR="$dir"
  plant_words; write_stub "$dir/stub.py"

  # A scratch server with no plugins and no project config; the question tool
  # is denied so a turn never waits for a human.
  mkdir -p "$dir/proj"
  printf '%s\n' '{"permissions":[{"action":"question","resource":"*","effect":"deny"}]}' > "$dir/proj/opencode.json"
  local port; port="$(free_port)"
  ( cd "$dir/proj" && OPENCODE_PASSWORD="$(opencode_password)" opencode serve --port "$port" >"$dir/server.log" 2>&1 & echo $! > "$dir/server.pid" )
  local i
  for i in $(seq 1 30); do
    curl -s -o /dev/null "http://127.0.0.1:$port/api/session" \
      -H "Authorization: Basic $(printf '%s:%s' opencode "$(opencode_password)" | base64)" && break
    sleep 0.5
  done
  OC_PORT="$port"

  local sid
  sid="$(oc_api "$OC_PORT" POST /api/session '{}' | python3 -c 'import json,sys; print(json.load(sys.stdin).get("data",{}).get("id",""))')"
  [[ -n "$sid" ]] || { fail "opencode:create session" "(see $dir/server.log)"; return; }
  SCRATCH_SESSIONS+=("$sid"); OC_SID="$sid"
  oc_api "$OC_PORT" POST "/api/session/$sid/model" \
    "{\"model\":{\"id\":\"${model#*/}\",\"providerID\":\"${model%%/*}\"}}" >/dev/null
  local msg
  for msg in "$PELICAN_MSG" "$PELICAN_DONE" "$TURBINE_MSG"; do
    oc_prompt "$OC_PORT" "$sid" "opencode:planted topics" "$(python3 -c 'import json,sys; print(json.dumps({"text": sys.argv[1], "delivery": "queue"}))' "$msg")" || return
  done
  pass "opencode:session with two planted topics ($sid)"

  local snap="$dir/export.json"; oc_snapshot "$snap"
  local digest_before; digest_before="$(messages_digest "$snap")"
  overview_checks opencode "$snap" oc_overview "$dir" || return
  oc_snapshot "$dir/export-after.json"
  [[ "$(messages_digest "$dir/export-after.json")" == "$digest_before" ]] \
    && pass "opencode:stored messages unchanged by the overview" \
    || fail "opencode:stored messages unchanged by the overview"

  local real="$dir/opencode-real.json" v
  if CTXED_CATEGORIZER_MODEL="$model" oc_overview "$real" 2>"$dir/real.err"; then
    v="$(status_check "$real" "$(oc_msg_id "$snap" PELICAN)" "$(oc_msg_id "$snap" TURBINE)" "$TURBINE_ITEM")"
    [[ "$(head -1 <<<"$v")" == OK* ]] && pass "opencode:real model status ($(head -1 <<<"$v" | cut -c4-))" \
      || fail "opencode:real model status" "$(head -1 <<<"$v"); stderr: $(tail -1 "$dir/real.err")"
    [[ "$(tail -1 <<<"$v")" == PENDING_OK ]]; soft $? "opencode:open item listed as pending" "$(tail -1 <<<"$v")"
  else
    fail "opencode:real model overview" "$(tail -2 "$dir/real.err")"
  fi

  oc_api "$OC_PORT" POST "/api/session/$sid/compact" '{"delivery":"queue"}' >/dev/null
  oc_wait_idle "$OC_PORT" "$sid" "opencode:compaction" 240 || return
  oc_snapshot "$snap"
  overview_checks opencode-compacted "$snap" oc_overview "$dir" || return
  v="$(compacted_check "$dir/opencode-compacted-stub.json")"
  [[ "$v" == OK* ]] && pass "opencode:after compaction (${v#OK })" || fail "opencode:after compaction" "$v"
  # Negative control: the same check against the pre-compaction inspect must
  # fail, so the totals check can tell the two views apart.
  v="$(totals_check "$dir/opencode-compacted-stub.json" "$dir/opencode-inspect.txt")"
  [[ "$v" != OK* ]] && pass "opencode:negative control (pre-compaction totals do not match: ${v%% *})" \
    || fail "opencode:negative control" "the compacted overview matched the pre-compaction inspect"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

printf '\n===== functional verification (ctxed overview)\n'
[[ $RUN_OPENCODE -eq 1 ]] && { echo "--- opencode"; opencode_scenario; }
[[ $RUN_CLAUDE -eq 1 ]] && { echo "--- claude code"; claude_scenario; }
printf '\n= %d passed, %d failed (hard); %d soft-passed, %d soft-failed\n' \
  "$PASSES" "$FAILURES" "$SOFT_PASSES" "$SOFT_FAILS"
[[ $FAILURES -eq 0 ]]
