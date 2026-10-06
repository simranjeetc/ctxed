#!/usr/bin/env bash
# Functional verification for ctxed — the gate.
#
# Spawns a REAL harness (Claude Code / OpenCode), runs the real prune flow, and
# asserts on observable outcomes: the session shrank, the session id is
# unchanged, stored history is untouched, the session continues.
#
# Usage:
#   scripts/verify-functionally.sh --opencode
#   scripts/verify-functionally.sh --claude
#   scripts/verify-functionally.sh --all
#   scripts/verify-functionally.sh --opencode --keep   # keep scratch on failure
#   scripts/verify-functionally.sh --opencode --report <path>
#
# Exit 0 only when every hard check passed. Model-recall checks are soft: they
# are reported as soft-pass/soft-fail and never decide the exit code or `ok`.
# Requires a live harness and spends a few small model calls per scenario. See
# docs/verification-strategy.md.
#
# The script builds ctxed from this checkout and exercises nothing else. It
# reports missing prerequisites by name instead of failing obscurely.
#
# Side effects outside the scratch directories, all undone on exit (also on
# failure and on Ctrl-C):
#   - --opencode: the self-config scenario puts a shim for the freshly built
#     ctxed at the plugin's first search location (~/go/bin/ctxed) and moves any
#     file already there to <path>.ctxed-verify-backup, restoring it on exit.
#     A run killed with SIGKILL leaves the backup; restore it by hand.
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

# The ctxed shim the self-config scenario installs (see the header).
SWAP_PATH=""
SWAP_INSTALLED=0
SWAP_BACKUP=""
SWAP_MADE_DIR=""

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
    -h|--help)  sed -n '2,36p' "$0"; exit 0 ;;
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
  restore_ctxed
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
# checks are reported but never decide `ok` or the exit code: the hard gate for
# a drop is the id-level check beside each one.
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

# --- shared helpers ---------------------------------------------------------

# Builds a categories file from a transcript by splitting entries into two
# buckets, so ctxed's minimum-two rule is satisfied. Handles both shapes: a
# Claude Code JSONL (entries keyed by uuid) and an OpenCode export (a single
# JSON object with messages[].id). Arguments: transcript path, output path.
write_categories() {
  local transcript="$1" out="$2"
  python3 - "$transcript" "$out" <<'PY'
import json, sys
path, out = sys.argv[1], sys.argv[2]
ids = []

text = open(path).read()
stripped = text.strip()
ids = []

doc = None
if stripped.startswith("{"):
    try:
        parsed = json.loads(stripped)
        if isinstance(parsed, dict) and "messages" in parsed:
            doc = parsed
    except Exception:
        doc = None

if doc is not None:
    for m in doc.get("messages", []):
        mid = m.get("id") or (m.get("info") or {}).get("id")
        if mid:
            ids.append(mid)
else:
    for line in text.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            o = json.loads(line)
        except Exception:
            continue
        if o.get("type") in ("user", "assistant") and o.get("uuid"):
            ids.append(o["uuid"])

if len(ids) < 2:
    sys.exit(4)
half = max(1, len(ids) // 2)
cats = {"categories": [
    {"label": "first half", "ids": ids[:half]},
    {"label": "second half", "ids": ids[half:]},
]}
open(out, "w").write(json.dumps(cats))
PY
}

# --- shared: build ctxed ----------------------------------------------------

# Always from this checkout: a scenario never exercises another source tree.
build_ctxed() {
  CTXED="$ROOT/.verify/ctxed"
  mkdir -p "$ROOT/.verify"
  ( cd "$ROOT" && go build -o "$CTXED" ./cmd/ctxed )
}

# ---------------------------------------------------------------------------
# OpenCode functional scenario
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

# oc_status <port> <method> <path> [json-body] — prints only the HTTP status.
# Used by the guard to tell "route moved" (404) from "command rejected" (4xx/5xx).
oc_status() {
  local port="$1" method="$2" path="$3" body="${4:-}"
  local auth; auth="$(printf '%s:%s' opencode "$(opencode_password)" | base64)"
  if [[ -n "$body" ]]; then
    curl -s -o /dev/null -w '%{http_code}' -X "$method" -H "Authorization: Basic $auth" \
      -H 'Content-Type: application/json' -d "$body" "http://127.0.0.1:$port$path"
  else
    curl -s -o /dev/null -w '%{http_code}' -X "$method" -H "Authorization: Basic $auth" \
      "http://127.0.0.1:$port$path"
  fi
}

# oc_session_text <port> <sid> — prints the concatenated text of a session's
# messages. This is the channel a user actually sees. Assertions about
# user-visible output must read it, never the server's stdout: a command's
# console.log lands in the server log, so asserting there passes even when the
# user sees nothing.
oc_session_text() {
  local port="$1" sid="$2"
  oc_api "$port" GET "/api/session/$sid/message" | python3 -c '
import json, sys
try:
    d = json.load(sys.stdin)
except Exception:
    print(""); sys.exit(0)
items = d.get("data", d) if isinstance(d, dict) else d
parts = []
for m in (items if isinstance(items, list) else []):
    t = m.get("text")
    if isinstance(t, str):
        parts.append(t)
print("\n".join(parts))'
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

# oc_reply <port> <sid> <user-message-id> — prints the assistant text that
# answers that user message: the text parts of the assistant messages after it,
# up to the next user message. Reasoning is excluded. A recall check reads only
# this, so a reply to a different question can never satisfy it.
oc_reply() {
  local port="$1" sid="$2" qid="$3"
  oc_api "$port" GET "/api/session/$sid/context" | python3 -c '
import json, sys
qid = sys.argv[1]
try: rows = json.load(sys.stdin).get("data", [])
except Exception: rows = []
out, seen = [], False
for m in rows:
    if m.get("id") == qid:
        seen = True
        continue
    if not seen:
        continue
    if m.get("type") == "user":
        break
    if m.get("type") == "assistant":
        out += [c.get("text", "") for c in m.get("content", []) if c.get("type") == "text"]
print(" ".join(out))' "$qid"
}

# hook_ids <dir> <after|before> — the ids in the dispatch hook's most recent
# decision, space-separated: "after" is what it sent, "before" is the
# transcript it received, i.e. what goes out when no selection is applied.
# Empty when the log is missing or unreadable. The hook logs only while a
# selection is recorded.
hook_ids() {
  tail -1 "$1/hook.log" 2>/dev/null | python3 -c '
import json,sys
try: print(" ".join(x for x in json.loads(sys.stdin.read())[sys.argv[1]] if x))
except Exception: print("")' "$2"
}

# hook_outbound <dir> — the ids the dispatch hook sent in its most recent decision.
hook_outbound() { hook_ids "$1" after; }

# oc_buckets <sid> <out> — writes {"alpha": [...], "beta": [...]}, the entry ids
# of the session's categories file (written by the plugin on a selection),
# split by bucket label.
oc_buckets() {
  python3 - "$1" "$2" <<'PY'
import json, glob, os, sys
session, out = sys.argv[1], sys.argv[2]
alpha, beta = set(), set()
for base in (os.environ.get("TMPDIR", "/tmp"), "/tmp", "/private/tmp"):
    for f in glob.glob(os.path.join(base, "ctxed-opencode", session + ".categories.json")):
        try:
            d = json.load(open(f))
            for c in d.get("categories", []):
                label = (c.get("label") or "").lower()
                if "alpha" in label:
                    alpha |= set(c.get("entryIds", []))
                elif "beta" in label:
                    beta |= set(c.get("entryIds", []))
        except Exception:
            pass
json.dump({"alpha": sorted(alpha), "beta": sorted(beta)}, open(out, "w"))
PY
}

# drop_verdict <buckets.json> <outbound ids> — the exact-drop check for one
# dispatch: every alpha id absent, every beta id present. Prints a counts line,
# then one of OK, NO_ALPHA, LEAK …, DROPPED_BETA ….
drop_verdict() {
  python3 - "$1" "$2" <<'PY'
import json, sys
b = json.load(open(sys.argv[1]))
alpha, beta = set(b["alpha"]), set(b["beta"])
after = set(sys.argv[2].split())
alpha_leaked = sorted(alpha & after)
beta_missing = sorted(beta - after)
print("ALPHA=%d BETA=%d LEAKED=%d MISSING=%d" % (len(alpha), len(beta), len(alpha_leaked), len(beta_missing)))
if len(alpha) == 0:
    print("NO_ALPHA")
elif alpha_leaked:
    print("LEAK " + ",".join(alpha_leaked)[:200])
elif beta_missing:
    print("DROPPED_BETA " + ",".join(beta_missing)[:200])
else:
    print("OK")
PY
}

# oc_drain <port> <sid> — run one cheap turn so the session inbox materializes.
# A plugin's synthetic message is admitted to the session inbox and appears in
# the message list on the next turn. Reading the list without a turn races that
# drain, which makes a visibility assertion flaky: the plugin has already said
# its piece, but the message has not landed yet.
oc_drain() {
  oc_prompt "$1" "$2" "opencode:drain turn" '{"text":"Reply with only: ok","delivery":"queue"}'
}

# oc_wait_text <port> <sid> <pattern> [attempts] — poll the session messages for
# a pattern, running a cheap turn between attempts so a synthetic message that
# was admitted to the inbox lands in the transcript. 0 once it appears.
oc_wait_text() {
  local port="$1" sid="$2" pattern="$3" attempts="${4:-8}" i
  for i in $(seq 1 "$attempts"); do
    grep -q "$pattern" <<<"$(oc_session_text "$port" "$sid")" && return 0
    oc_drain "$port" "$sid" || return 1
  done
  return 1
}

# oc_wait_outbound <port> <sid> <dir> <id> <present|absent> [attempts] — poll the
# dispatch hook's most recent outbound list until the id is present (or gone),
# running a cheap turn between attempts so a dispatch actually happens. Drains
# are safe: a message that is not a selection leaves a pending offer standing.
oc_wait_outbound() {
  local port="$1" sid="$2" dir="$3" id="$4" want="$5" attempts="${6:-12}" i out
  for i in $(seq 1 "$attempts"); do
    out="$(hook_outbound "$dir")"
    if [[ -n "$out" ]]; then
      if [[ "$want" == "present" ]] && grep -q -- "$id" <<<"$out"; then return 0; fi
      if [[ "$want" == "absent" ]] && ! grep -q -- "$id" <<<"$out"; then return 0; fi
    fi
    oc_drain "$port" "$sid" || return 1
  done
  return 1
}

# Guard: the command API this verifier drives is a real, declared surface, so a
# future OpenCode upgrade that moves it fails with a clear message instead of a
# confusing timeout. Two facts are checked:
#   1. the plugin command is discoverable in GET /api/command (registration), and
#   2. the command route exists (not 404) — the route the plugin never uses at
#      dispatch time; only this verifier uses it.
# The core prune uses session.hook("context"), which is independent of this.
oc_command_guard() {
  local port="$1"
  # 1. Registration: the command must appear in the command list. Registration
  #    happens as the plugin loads, which is asynchronous, so poll briefly.
  local listed="no" attempt
  for attempt in $(seq 1 20); do
    listed="$(oc_api "$port" GET /api/command | python3 -c '
import json,sys
try:
    d=json.load(sys.stdin)
except Exception:
    print(""); sys.exit(0)
names=[c.get("name") for c in d.get("data",[])]
print("yes" if "ctxed-prune" in names else "no")' 2>/dev/null)"
    [[ "$listed" == "yes" ]] && break
    sleep 0.5
  done
  if [[ "$listed" != "yes" ]]; then
    fail "opencode:command guard (registration)" "ctxed-prune is not in GET /api/command"
    return 1
  fi

  # 2. Route liveness: a POST to the command route must not 404. A 404 means the
  #    route moved; other 4xx/5xx codes are the command's own behavior.
  local status
  status="$(oc_status "$port" POST "/api/session/__guard__/command" '{"name":"ctxed-prune","text":""}')"
  if [[ "$status" == "404" ]]; then
    fail "opencode:command guard (route)" \
      "POST /api/session/{id}/command returned 404 — OpenCode moved the command API; update this verifier"
    return 1
  fi

  pass "opencode:command guard (registered; route present, http $status)"
  return 0
}

# Bundles the plugin into a scratch OpenCode project and starts a server there.
# Sets OC_PORT. Returns non-zero if the plugin cannot be built.
#
# mode "env" (default): start with CTXED_PLUGIN_* set and ctxed on PATH — the
#   deterministic path the main scenario uses.
# mode "selfconfig": start with a minimal PATH and NO CTXED_PLUGIN_* at all,
#   mirroring the launchd daemon the plugin really runs under. The plugin must
#   then resolve ctxed and the categorizer on its own.
start_opencode_with_plugin() {
  local ctxed_bin="$1" dir="$2" mode="${3:-env}"
  local plugin_dir="$ROOT/plugin/opencode"
  [[ -d "$plugin_dir" ]] || return 1

  local proj="$dir/proj"
  mkdir -p "$proj/.opencode/plugins"
  # The plugin's bucket listing is a synthetic message, and OpenCode starts an
  # agent turn on it. A model may answer with the interactive `question` tool,
  # which blocks until a human replies, so the session would never go idle.
  # No scenario needs that tool; deny it in this scratch project only.
  printf '%s\n' '{"permissions":[{"action":"question","resource":"*","effect":"deny"}]}' > "$proj/opencode.json"
  printf '{"name":"ctxed-fn","private":true,"type":"module","dependencies":{"@opencode/plugin":"2.0.22"}}\n' \
    > "$proj/.opencode/package.json"
  ( cd "$proj/.opencode" && npm install --no-audit --no-fund --silent >/dev/null 2>&1 ) || return 1
  ( cd "$plugin_dir" && [[ -x node_modules/.bin/esbuild ]] || npm install --no-audit --no-fund --silent >/dev/null 2>&1 ) || return 1
  ( cd "$plugin_dir" && ./node_modules/.bin/esbuild src/plugin.ts --bundle --format=esm --platform=node \
      --external:@opencode/plugin --outfile="$proj/.opencode/plugins/ctxed-prune.js" >/dev/null 2>&1 ) || return 1

  # A deterministic categorizer. It groups the entries ctxed lists by the topic
  # marker in their text (ALPHA / BETA), which is how a real categorizer would
  # separate two topics — a bucket here spans several messages, not one. Entries
  # with no marker fall back to the first bucket so every id stays assigned. Real
  # ctxed parses the live transcript and resolves ids; the stub only chooses the
  # grouping, so no model call is needed to categorize.
  cat > "$dir/stub-categorize.py" <<'PY'
#!/usr/bin/env python3
import re, sys, json, time
prompt = sys.stdin.read()
# A real categorizer calls a model and takes seconds. Sleep here so the plugin's
# command timeout is genuinely exercised: under the old shared 2000ms default
# this categorize times out, which is the bug this guards against.
time.sleep(3)
# Group by scanning each "- id=... : <text>" line for a topic marker.
alpha, beta, other = [], [], []
for line in prompt.splitlines():
    m = re.match(r'- id=(\S+) ', line)
    if not m:
        continue
    i = m.group(1)
    low = line.lower()
    if "topic-alpha" in low:
        alpha.append(i)
    elif "topic-beta" in low:
        beta.append(i)
    else:
        other.append(i)
# Assign anything unmarked to the alpha bucket so no id is dropped by omission.
alpha += other
cats = []
if alpha:
    cats.append({"label": "alpha topic", "ids": alpha})
if beta:
    cats.append({"label": "beta topic", "ids": beta})
if not cats:
    cats = [{"label": "all", "ids": []}]
print(json.dumps({"categories": cats}))
PY
  chmod +x "$dir/stub-categorize.py"

  local port; port="$(free_port)"
  if [[ "$mode" == "selfconfig" ]]; then
    # Place the stub where the plugin's bundled-categorizer fallback looks:
    # <proj>/scripts/ (two levels up from .opencode/plugins/). No ctxed or
    # categorizer configuration is set.
    mkdir -p "$proj/scripts"
    cp "$dir/stub-categorize.py" "$proj/scripts/ctxed-categorizer-opencode.sh"
    # A minimal PATH and no ctxed/categorizer config: the plugin must find both
    # through its own search (~/go/bin, Homebrew, the bundled script), exactly as
    # on a real machine. The real HOME is kept so the harness stays
    # authenticated. CTXED_PLUGIN_DEBUG_LOG is a verifier-only knob: it changes
    # no plugin behavior and helps the plugin resolve nothing, so it does not
    # weaken the self-configuration claim.
    ( cd "$proj" \
      && env -i HOME="$HOME" PATH="/usr/bin:/bin:/usr/sbin:/sbin" \
         OPENCODE_PASSWORD="$(opencode_password)" \
         CTXED_PLUGIN_DEBUG_LOG="$dir/hook.log" \
         "$(command -v opencode)" serve --port "$port" >"$dir/server.log" 2>&1 & echo $! > "$dir/server.pid" )
  else
    ( cd "$proj" \
      && OPENCODE_PASSWORD="$(opencode_password)" \
         PATH="$(dirname "$ctxed_bin"):$PATH" \
         CTXED_PLUGIN_CTXED_PATH="$ctxed_bin" \
         CTXED_PLUGIN_CATEGORIZER_CMD="$dir/stub-categorize.py" \
         CTXED_PLUGIN_DEBUG_LOG="$dir/hook.log" \
         opencode serve --port "$port" >"$dir/server.log" 2>&1 & echo $! > "$dir/server.pid" )
  fi
  local i
  for i in $(seq 1 30); do
    if curl -s -o /dev/null "http://127.0.0.1:$port/api/plugin" \
         -H "Authorization: Basic $(printf '%s:%s' opencode "$(opencode_password)" | base64)"; then break; fi
    sleep 0.5
  done
  OC_PORT="$port"
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

# Deletes a scratch session while the server is still running, then stops the
# server. Used on every early exit so no scratch session is left behind. The
# plugin's per-session categories file goes with it.
abort_opencode() {
  local dir="$1" sid="${2:-}"
  if [[ -n "$sid" ]]; then
    oc_api "$OC_PORT" DELETE "/api/session/$sid" >/dev/null 2>&1
    rm -f "${TMPDIR:-/tmp}/ctxed-opencode/$sid.categories.json" "/tmp/ctxed-opencode/$sid.categories.json"
  fi
  SCRATCH_SESSIONS=()
  stop_opencode_server "$dir"
}

opencode_prereqs() {
  need opencode "OpenCode CLI, authenticated"
  need node "Node.js, to bundle the plugin"
  need npm "npm, to install the plugin's build dependencies"
  need lsof "lsof, to stop the scratch server"
}

opencode_scenario() {
  opencode_prereqs
  build_ctxed || { fail "opencode:build ctxed"; return; }

  local model="$OPENCODE_MODEL"
  # Verify the configured model is offered before spending time on a session.
  # `opencode models` can be empty on the first call while the background server
  # warms up, so retry before declaring the model unavailable.
  local model_ok=0
  for _ in $(seq 1 10); do
    if opencode models 2>/dev/null | grep -qx "$model"; then model_ok=1; break; fi
    sleep 1
  done
  if [[ "$model_ok" -ne 1 ]]; then
    fail "opencode:model" "'$model' is not in \`opencode models\`; set CTXED_TEST_OPENCODE_MODEL to an entitled model"
    return
  fi
  MODEL_USED="$model"
  echo "     model: $model"

  mkscratch; local dir="$SCRATCH_DIR"
  # Sessions are addressed by id, so nothing needs the working directory to
  # change; keep the caller's directory so a later build still finds go.mod.

  # 1. Create a real session with two clearly separable topics.
  local sid
  sid="$(opencode run --model "$model" --format json \
        "In one short sentence: the sky is blue. Then in one short sentence: water is wet." \
        2>/dev/null | python3 -c 'import sys,json
for line in sys.stdin:
    line=line.strip()
    if not line: continue
    try: o=json.loads(line)
    except: continue
    for k in ("sessionID","sessionId","session_id","id"):
        if isinstance(o,dict) and o.get(k): print(o[k]); sys.exit(0)' )"
  if [[ -z "$sid" ]]; then
    fail "opencode:create session" "could not parse a session id from opencode run"
    return
  fi
  CLI_SESSIONS+=("$sid")
  pass "opencode:create session ($sid)"

  # 2. Export the session, prove ctxed can see it and categorize it offline.
  local export="$dir/export.json"
  if opencode session export "$sid" > "$export" 2>/dev/null && [[ -s "$export" ]]; then
    pass "opencode:export session"
  else
    fail "opencode:export session"; return
  fi

  "$CTXED" inspect "$export" >/dev/null 2>&1 && pass "opencode:ctxed inspect" \
    || fail "opencode:ctxed inspect"

  # 3. Categorize (offline, deterministic: we feed ctxed the real entry ids, so
  #    no model call is needed for this scenario).
  local cat="$dir/cat.json" catout="$dir/cat.out.json"
  write_categories "$export" "$cat"
  if "$CTXED" categorize "$export" \
        --categorizer-cmd "cat $cat" --out "$catout" >/dev/null 2>&1; then
    cat="$catout"
    pass "opencode:ctxed categorize"
  else
    fail "opencode:ctxed categorize"; return
  fi

  # 4. Resolve a selection to dropped ids (the plugin's id-only path).
  local ids
  ids="$("$CTXED" prune "$export" --categories-file "$cat" --categories 1 --ids-only 2>/dev/null)"
  case "$ids" in
    '{"droppedIds":['*) pass "opencode:prune --ids-only" ;;
    *) fail "opencode:prune --ids-only" "$ids" ;;
  esac

  # 5. Drive the in-session command and the dispatch hook through a real server,
  #    and assert the drop is real (tasks 4.1-4.4).
  if ! start_opencode_with_plugin "$CTXED" "$dir"; then
    fail "opencode:plugin server" "could not build the plugin or start the server (see $dir/server.log)"
    return
  fi
  pass "opencode:plugin server (port $OC_PORT)"

  # Guard the command API before relying on it, so a moved route is a clear
  # failure, not a mystery.
  oc_command_guard "$OC_PORT" || { abort_opencode "$dir"; return; }

  # A multi-message session across two topics, so a bucket spans several
  # messages. Alpha carries the markers to be dropped; beta must survive. One
  # alpha message carries a file attachment, whose content arrives as parts of
  # the message — dropping the message must drop that content too.
  local alpha1suffix="AAA$$" alpha2suffix="BBB$$" beta1suffix="CCC$$" attachsuffix="DDD$$"
  local alpha1="TOPIC-ALPHA: remember the word APPLE-$alpha1suffix. Reply with only: ok"
  local alpha2="TOPIC-ALPHA: remember the number 42-$alpha2suffix as well."
  local beta1="TOPIC-BETA: remember the word BETA-$beta1suffix. Reply with only: ok"
  local attach="$dir/attach-$attachsuffix.txt"
  printf 'ATTACHED-CONTENT-%s\n' "$attachsuffix" > "$attach"

  local sid
  sid="$(oc_api "$OC_PORT" POST /api/session '{}' \
        | python3 -c 'import json,sys; print(json.load(sys.stdin).get("data",{}).get("id",""))')"
  if [[ -z "$sid" ]]; then
    fail "opencode:in-session create session"; abort_opencode "$dir"; return
  fi
  SCRATCH_SESSIONS+=("$sid")

  oc_api "$OC_PORT" POST "/api/session/$sid/model" \
    "{\"model\":{\"id\":\"${model#*/}\",\"providerID\":\"${model%%/*}\"}}" >/dev/null
  oc_prompt "$OC_PORT" "$sid" "opencode:turn alpha 1" \
    "{\"text\":\"$alpha1\",\"delivery\":\"queue\"}" || { abort_opencode "$dir" "$sid"; return; }
  oc_prompt "$OC_PORT" "$sid" "opencode:turn beta 1" \
    "{\"text\":\"$beta1\",\"delivery\":\"queue\"}" || { abort_opencode "$dir" "$sid"; return; }
  oc_prompt "$OC_PORT" "$sid" "opencode:turn alpha 2" \
    "{\"text\":\"$alpha2\",\"delivery\":\"queue\"}" || { abort_opencode "$dir" "$sid"; return; }
  oc_prompt "$OC_PORT" "$sid" "opencode:turn alpha attachment" \
    "{\"text\":\"TOPIC-ALPHA: here is a file for the alpha topic. Reply with only: ok\",\"files\":[{\"uri\":\"file://$attach\",\"name\":\"$(basename "$attach")\"}],\"delivery\":\"queue\"}" \
    || { abort_opencode "$dir" "$sid"; return; }

  # 4.1 — the in-session command runs, lists buckets over the live session, and
  # the buckets are VISIBLE TO THE USER. The visibility half is the point: assert
  # on the session messages, not the server log, or a plugin that prints to
  # stdout (invisible) would pass.
  oc_api "$OC_PORT" POST "/api/session/$sid/command" '{"name":"ctxed-prune","text":""}' >/dev/null
  if oc_wait_text "$OC_PORT" "$sid" 'Categories:'; then
    pass "opencode:in-session command (categorized the live session; buckets visible in the session)"
  else
    fail "opencode:in-session command" \
      "no 'Categories:' in the session messages — user-visible output missing (printed to stdout instead?)"
    abort_opencode "$dir" "$sid"; return
  fi

  # Select the alpha bucket (id 1 by the stub's grouping). Every alpha message,
  # across several turns, must be dropped; beta must survive. The confirmation
  # must likewise be visible in the session.
  oc_api "$OC_PORT" POST "/api/session/$sid/command" '{"name":"ctxed-prune","text":"1"}' >/dev/null
  if oc_wait_text "$OC_PORT" "$sid" 'Selected bucket'; then
    pass "opencode:selection recorded (confirmation visible in the session)"
  else
    fail "opencode:selection recorded" \
      "no 'Selected bucket'; session said: $(oc_session_text "$OC_PORT" "$sid" | tr '\n' ' ' | tail -c 300)"
    abort_opencode "$dir" "$sid"; return
  fi

  # 4.4 — id parity: the ids ctxed categorized over the live transcript are the
  # session message ids. Checked after a selection, because that is when the
  # plugin writes the categories file. Export the session and assert every id
  # ctxed saw is present there.
  local export_live="$dir/live-export.json"
  oc_api "$OC_PORT" GET "/api/experimental/session/$sid/export" > "$export_live" 2>/dev/null || true
  if [[ ! -s "$export_live" ]]; then
    opencode session export "$sid" > "$export_live" 2>/dev/null || true
  fi
  local parity
  parity="$(python3 - "$export_live" "$sid" <<'PY'
import json, sys, glob, os
session = sys.argv[2]
# Only the categories file for this session (the plugin names it by session id).
files = []
for base in (os.environ.get("TMPDIR", "/tmp"), "/tmp", "/private/tmp"):
    files += glob.glob(os.path.join(base, "ctxed-opencode", session + ".categories.json"))
cat_ids = set()
for f in files:
    try:
        d = json.load(open(f))
        for c in d.get("categories", []):
            cat_ids.update(c.get("entryIds", []))
    except Exception:
        pass
export_ids = set()
try:
    doc = json.load(open(sys.argv[1]))
    raw = doc.get("data", doc)
    if isinstance(raw, dict):
        for m in raw.get("messages", []):
            if isinstance(m, dict) and m.get("id"):
                export_ids.add(m["id"])
except Exception:
    pass
if not cat_ids:
    print("NO_CATEGORIZED_IDS")
elif not export_ids:
    print("NO_EXPORT_IDS")
elif not cat_ids <= export_ids:
    print("MISMATCH " + ",".join(sorted(cat_ids - export_ids))[:200])
else:
    print("OK %d" % len(cat_ids))
PY
)"
  case "$parity" in
    OK*) pass "opencode:id parity ($parity)" ;;
    *)   fail "opencode:id parity" "$parity" ;;
  esac

  # 4.2a — the dropped bucket spans several messages, not one, and the drop is
  # exact: every id in the alpha bucket is absent from what the hook sent, and
  # every id in the beta bucket is present. This reads the plugin's own dispatch
  # decision (CTXED_PLUGIN_DEBUG_LOG), so it does not depend on what a model
  # remembers.
  oc_prompt "$OC_PORT" "$sid" "opencode:turn after selection" \
    '{"text":"Reply with only: ok","delivery":"queue"}' || { abort_opencode "$dir" "$sid"; return; }
  local buckets="$dir/buckets.json"
  oc_buckets "$sid" "$buckets"
  local membership counts verdict
  membership="$(drop_verdict "$buckets" "$(hook_outbound "$dir")")"
  counts="$(grep -oE 'ALPHA=[0-9]+ BETA=[0-9]+' <<<"$membership" | head -1)"
  verdict="$(grep -E '^(OK|NO_ALPHA|LEAK |DROPPED_BETA )' <<<"$membership" | head -1)"
  local alpha_n; alpha_n="$(grep -oE 'ALPHA=[0-9]+' <<<"$counts" | cut -d= -f2)"
  if [[ "${alpha_n:-0}" -gt 1 ]]; then
    pass "opencode:bucket spans several messages ($counts)"
  else
    fail "opencode:bucket spans several messages" "$membership"
  fi
  if [[ "$verdict" == "OK" ]]; then
    pass "opencode:exact drop (every alpha id absent, every beta id retained)"
  else
    fail "opencode:exact drop" "$membership"
  fi

  # Negative control: the same check against the same dispatch with no
  # selection applied (the transcript the hook received) must fail, or the
  # check cannot fail at all.
  local control_verdict
  control_verdict="$(drop_verdict "$buckets" "$(hook_ids "$dir" before)" | grep -E '^(OK|NO_ALPHA|LEAK |DROPPED_BETA )' | head -1)"
  if [[ "$control_verdict" == LEAK* ]]; then
    pass "opencode:negative control (exact drop fails with no selection applied)"
  else
    fail "opencode:negative control (exact drop)" \
      "with no selection applied the check reported '${control_verdict:-nothing}', not LEAK"
  fi

  # Stored session unchanged: every message is still in the transcript, including
  # the dropped bucket's.
  local stored
  stored="$(oc_api "$OC_PORT" GET "/api/session/$sid/context" | python3 -c '
import json,sys
d=json.load(sys.stdin)
rows=d.get("data",[])
texts=" ".join((m.get("text") or "") for m in rows)
print(all(s in texts for s in sys.argv[1:]))' "$alpha1suffix" "$beta1suffix" "$alpha2suffix")"
  if [[ "$stored" == "True" ]]; then
    pass "opencode:stored session unchanged (every topic turn still in history)"
  else
    fail "opencode:stored session unchanged" "a turn was removed from the transcript"
  fi

  # 4.2c — a dropped message that carried a file attachment takes the file
  # content with it. Find THE message that carries the attachment by its file
  # part, then assert that message is in the alpha (dropped) bucket and absent
  # from the hook's last outbound list.
  local attach_id
  attach_id="$(oc_api "$OC_PORT" GET "/api/session/$sid/message" | python3 -c '
import json, sys
name = sys.argv[1]
try: items = json.load(sys.stdin).get("data", [])
except Exception: items = []
for m in items:
    if any((f.get("name") == name) for f in (m.get("files") or [])):
        print(m.get("id", "")); break
else:
    print("")' "$(basename "$attach")")"
  local attach_check
  attach_check="$(python3 - "$buckets" "$attach_id" "$(hook_outbound "$dir")" <<'PY'
import json, sys
alpha = set(json.load(open(sys.argv[1]))["alpha"])
mid, after = sys.argv[2], set(sys.argv[3].split())
if not mid:
    print("NO_ATTACHMENT_MESSAGE")
elif mid not in alpha:
    print("NOT_IN_ALPHA " + mid)
elif mid in after:
    print("LEAKED " + mid)
else:
    print("OK " + mid)
PY
)"
  if [[ "$attach_check" == OK* ]]; then
    pass "opencode:attachment message dropped with its bucket (${attach_check#OK })"
  else
    fail "opencode:attachment message dropped with its bucket" "$attach_check"
  fi

  # Soft signal: the model cannot quote the attached text either. Read only the
  # reply to this question; its "don't know" token is unique to it.
  local none_attach="NONE-A$$"
  oc_prompt "$OC_PORT" "$sid" "opencode:attachment recall question" \
    "{\"text\":\"What exact text was in the file attached for the alpha topic? Reply with only that text, or with only $none_attach if you do not know it.\",\"delivery\":\"queue\"}" \
    || { abort_opencode "$dir" "$sid"; return; }
  local attach_ans
  attach_ans="$(oc_reply "$OC_PORT" "$sid" "$OC_LAST_ID")"
  if grep -q -- "$none_attach" <<<"$attach_ans" && ! grep -q -- "$attachsuffix" <<<"$attach_ans"; then
    soft 0 "opencode:attachment content not recallable"
  else
    soft 1 "opencode:attachment content not recallable" "reply: $(head -c 200 <<<"$attach_ans")"
  fi

  # 4.3 — a message added after the selection is kept, even on the dropped
  # topic: a selection covers only the messages it was made over. Hard: in the
  # same dispatch the new message is present while every originally selected
  # message is still absent, so the pass cannot come from a selection that
  # silently stopped applying. Soft: the model can name the code word it carried.
  local codeword="CHERRY-$$" none_new="NONE-N$$"
  oc_prompt "$OC_PORT" "$sid" "opencode:turn post-selection alpha" \
    "{\"text\":\"TOPIC-ALPHA: the alpha code word is $codeword. Reply with only: noted\",\"delivery\":\"queue\"}" \
    || { abort_opencode "$dir" "$sid"; return; }
  local new_id="$OC_LAST_ID"
  oc_prompt "$OC_PORT" "$sid" "opencode:post-selection recall question" \
    "{\"text\":\"What is the alpha code word? Reply with only the code word, or with only $none_new if you do not know it.\",\"delivery\":\"queue\"}" \
    || { abort_opencode "$dir" "$sid"; return; }
  local new_q="$OC_LAST_ID" new_out new_verdict
  new_out="$(hook_outbound "$dir")"
  new_verdict="$(drop_verdict "$buckets" "$new_out" | grep -E '^(OK|NO_ALPHA|LEAK |DROPPED_BETA )' | head -1)"
  if [[ -n "$new_out" ]] && grep -q -- "$new_id" <<<"$new_out" && [[ "$new_verdict" == OK ]]; then
    pass "opencode:post-selection message kept (selected messages still dropped in the same dispatch)"
  elif [[ "$new_verdict" != OK ]]; then
    fail "opencode:post-selection message kept" "the selection no longer applied in that dispatch: ${new_verdict:-no dispatch logged}"
  else
    fail "opencode:post-selection message kept" "the post-selection message $new_id was dropped from the dispatch"
  fi
  local new_reply
  new_reply="$(oc_reply "$OC_PORT" "$sid" "$new_q")"
  if grep -q -- "$codeword" <<<"$new_reply"; then
    soft 0 "opencode:post-selection recall (the model can name the new code word)"
  else
    soft 1 "opencode:post-selection recall" "reply: $(head -c 200 <<<"$new_reply")"
  fi

  # Regression: a dropped tool result must not accumulate in the request. Read a
  # file (a tool call + id-less result), before the selection, then confirm the
  # null-id content does not grow across later dispatches. Without the
  # droppedToolCallIds fix this grows every turn.
  # Inside the scratch project, by its real path (the server sees /private/var,
  # not the /var symlink): a read outside the project asks for permission and
  # blocks the session.
  local bulk; bulk="$(cd "$dir/proj" && pwd -P)/bulk-$attachsuffix.txt"
  python3 -c 'import sys; open(sys.argv[1],"w").write("Z"*4000)' "$bulk"
  # A file read lands in whichever bucket the stub assigns; add its turn before
  # the selection so it is part of the dropped set if it falls in the alpha half.
  oc_prompt "$OC_PORT" "$sid" "opencode:turn file read" \
    "{\"text\":\"TOPIC-ALPHA: read the file at $bulk and reply with only: ok\",\"delivery\":\"queue\"}" \
    || { abort_opencode "$dir" "$sid"; return; }
  local before_bytes after_bytes
  before_bytes="$(tail -1 "$dir/hook.log" 2>/dev/null | python3 -c '
import json,sys
try: print(sum(1 for m in json.loads(sys.stdin.read())["after"] if m is None))
except Exception: print(-1)')"
  # Confirm the file read really produced a tool result in the session, so the
  # assertion below is meaningful (otherwise a flat zero proves nothing).
  local session_tools
  session_tools="$(oc_api "$OC_PORT" GET "/api/session/$sid/context" | python3 -c '
import json,sys
d=json.load(sys.stdin)
n=0
for m in d.get("data",[]):
    if m.get("type")=="assistant":
        n += sum(1 for c in m.get("content",[]) if c.get("type")=="tool")
print(n)')"
  if [[ "${session_tools:-0}" -ge 1 ]]; then
    pass "opencode:tool result present in the session ($session_tools tool part(s))"
  else
    fail "opencode:tool result present in the session" "the file read produced no tool part"
  fi
  # Two more dispatches; the count of null-id messages must stay flat.
  oc_prompt "$OC_PORT" "$sid" "opencode:turn after file read 1" '{"text":"Reply with only: ok","delivery":"queue"}' \
    || { abort_opencode "$dir" "$sid"; return; }
  oc_prompt "$OC_PORT" "$sid" "opencode:turn after file read 2" '{"text":"Reply with only: ok","delivery":"queue"}' \
    || { abort_opencode "$dir" "$sid"; return; }
  after_bytes="$(tail -1 "$dir/hook.log" 2>/dev/null | python3 -c '
import json,sys
try: print(sum(1 for m in json.loads(sys.stdin.read())["after"] if m is None))
except Exception: print(-1)')"
  if [[ "$after_bytes" -ge 0 && "$after_bytes" -le "${before_bytes:-0}" ]]; then
    pass "opencode:no tool-result accumulation (null-id messages flat at $after_bytes)"
  else
    fail "opencode:no tool-result accumulation" "null-id messages grew from ${before_bytes:-?} to $after_bytes"
  fi

  # Delete the scratch session while the server is still up.
  abort_opencode "$dir" "$sid"
}

# plugin_ctxed_locations — the plugin's own ctxed search list, in order, one
# path per line (resolveCtxedPath in plugin/opencode/src/core.ts). Read from the
# source so the scenario and the plugin cannot disagree.
plugin_ctxed_locations() {
  python3 - "$ROOT/plugin/opencode/src/core.ts" "$HOME" <<'PY'
import re, sys
src = open(sys.argv[1]).read()
m = re.search(r'function resolveCtxedPath[\s\S]*?const candidates = \[([\s\S]*?)\]', src)
if not m:
    sys.exit(1)
for entry in m.group(1).split("\n"):
    entry = entry.strip().rstrip(",")
    if not entry:
        continue
    j = re.fullmatch(r'join\(home,\s*(.*)\)', entry)
    s = re.fullmatch(r'"([^"]*)"', entry)
    if j:
        print("/".join([sys.argv[2]] + re.findall(r'"([^"]*)"', j.group(1))))
    elif s:
        print(s.group(1))
    else:
        sys.exit(1)
PY
}

# swap_in_ctxed <target> <built-binary> <invocation-log> — puts a shim at
# <target> that records each invocation and execs the freshly built binary, so
# the plugin can only ever run this checkout's ctxed, and the scenario can prove
# which path the plugin resolved. Any file already at <target> is moved aside
# and restored by restore_ctxed (run from the EXIT trap).
swap_in_ctxed() {
  local target="$1" bin="$2" log="$3" backup="$1.ctxed-verify-backup"
  if [[ -e "$backup" ]]; then
    fail "opencode:self-config install ctxed" \
      "$backup exists (left by an interrupted run); move it back to $target by hand"
    return 1
  fi
  SWAP_PATH="$target"
  if [[ ! -d "$(dirname "$target")" ]]; then
    mkdir -p "$(dirname "$target")" || return 1
    SWAP_MADE_DIR="$(dirname "$target")"
  fi
  if [[ -e "$target" ]]; then
    SWAP_BACKUP="$backup"
    mv "$target" "$backup" || { SWAP_BACKUP=""; return 1; }
  fi
  SWAP_INSTALLED=1
  printf '#!/bin/sh\n# Installed by scripts/verify-functionally.sh; removed on exit.\necho "$0" >> %q\nexec %q "$@"\n' \
    "$log" "$bin" > "$target" && chmod +x "$target"
}

restore_ctxed() {
  [[ -n "$SWAP_PATH" ]] || return 0
  [[ "$SWAP_INSTALLED" == 1 ]] && rm -f "$SWAP_PATH"
  [[ -n "$SWAP_BACKUP" && -e "$SWAP_BACKUP" ]] && mv -f "$SWAP_BACKUP" "$SWAP_PATH"
  [[ -n "$SWAP_MADE_DIR" ]] && rmdir "$SWAP_MADE_DIR" 2>/dev/null
  SWAP_PATH="" SWAP_INSTALLED=0 SWAP_BACKUP="" SWAP_MADE_DIR=""
  return 0
}

# Self-configuration scenario. The main scenario hands the plugin CTXED_PLUGIN_*
# env and puts ctxed on PATH; a real install has neither. OpenCode's server runs
# as a launchd daemon with a minimal PATH, and a local plugin cannot take options
# from opencode.json. So this scenario starts the server with a minimal PATH and
# no CTXED_PLUGIN_* at all, and asserts the plugin still works end to end — with
# its output visible in the session.
opencode_selfconfig_scenario() {
  opencode_prereqs
  build_ctxed || { fail "opencode:self-config build ctxed"; return; }

  # The plugin must run this checkout's ctxed, never a pre-installed one. A
  # scratch HOME is impossible (harness credentials live in the real HOME), and
  # passing the path through env would weaken the self-configuration claim, so
  # the built binary goes where the plugin looks first.
  mkscratch; local dir="$SCRATCH_DIR"
  local locations first
  locations="$(plugin_ctxed_locations)"
  first="$(head -1 <<<"$locations")"
  if [[ -z "$first" ]]; then
    fail "opencode:self-config ctxed location" "could not read resolveCtxedPath's first candidate from plugin/opencode/src/core.ts"
    return
  fi
  swap_in_ctxed "$first" "$CTXED" "$dir/ctxed-invocations.log" || return

  start_opencode_with_plugin "$CTXED" "$dir" selfconfig \
    || { fail "opencode:self-config server"; return; }
  oc_command_guard "$OC_PORT" || { abort_opencode "$dir"; return; }

  local model="$OPENCODE_MODEL"
  local sid
  sid="$(oc_api "$OC_PORT" POST /api/session '{}' \
        | python3 -c 'import json,sys; print(json.load(sys.stdin).get("data",{}).get("id",""))')"
  if [[ -z "$sid" ]]; then
    fail "opencode:self-config create session"; abort_opencode "$dir"; return
  fi
  SCRATCH_SESSIONS+=("$sid")

  oc_api "$OC_PORT" POST "/api/session/$sid/model" \
    "{\"model\":{\"id\":\"${model#*/}\",\"providerID\":\"${model%%/*}\"}}" >/dev/null
  oc_prompt "$OC_PORT" "$sid" "opencode:self-config turn alpha" \
    '{"text":"TOPIC-ALPHA: the database schema. Reply with only: ok","delivery":"queue"}' \
    || { abort_opencode "$dir" "$sid"; return; }
  oc_prompt "$OC_PORT" "$sid" "opencode:self-config turn beta" \
    '{"text":"TOPIC-BETA: the CSS color palette. Reply with only: ok","delivery":"queue"}' \
    || { abort_opencode "$dir" "$sid"; return; }

  # 1. The command must run and its buckets must be VISIBLE IN THE SESSION —
  #    with no plugin env and a minimal PATH.
  oc_api "$OC_PORT" POST "/api/session/$sid/command" '{"name":"ctxed-prune","text":""}' >/dev/null
  if oc_wait_text "$OC_PORT" "$sid" 'Categories:'; then
    pass "opencode:self-config (no plugin env, minimal PATH: buckets visible in the session)"
  else
    fail "opencode:self-config" \
      "with no CTXED_PLUGIN_* env and a minimal PATH the plugin produced no visible buckets"
  fi

  # The plugin ran the built binary through the path it searches first.
  if grep -qxF -- "$first" "$dir/ctxed-invocations.log" 2>/dev/null; then
    pass "opencode:self-config resolved ctxed at $first (this checkout's build)"
  else
    local others="" c
    while IFS= read -r c; do
      [[ -e "$c" ]] && others="$others $c"
    done < <(tail -n +2 <<<"$locations")
    fail "opencode:self-config resolved ctxed" \
      "the plugin never ran $first; it used another location (installed:${others:- none})"
  fi

  # 2. A selection must apply under the same conditions.
  oc_api "$OC_PORT" POST "/api/session/$sid/command" '{"name":"ctxed-prune","text":"1"}' >/dev/null
  if oc_wait_text "$OC_PORT" "$sid" 'Selected bucket'; then
    pass "opencode:self-config selection (visible with no plugin env)"
  else
    fail "opencode:self-config selection" \
      "no 'Selected bucket'; session said: $(oc_session_text "$OC_PORT" "$sid" | tr '\n' ' ' | tail -c 300)"
  fi

  # 3. The tight path: list the buckets, then answer with just the number. The
  #    prompt hook must match that reply, record the selection, and the next
  #    dispatch must drop the bucket — no second command, and no second model
  #    call to categorize. Beta (id 2) is still present, so dropping it is new.
  oc_api "$OC_PORT" POST "/api/session/$sid/command" '{"name":"ctxed-prune","text":""}' >/dev/null
  if oc_wait_text "$OC_PORT" "$sid" 'Reply with the bucket numbers'; then
    pass "opencode:self-config listing offers a bare-number reply"
  else
    fail "opencode:self-config listing" \
      "the listing did not offer a bare-number reply: $(oc_session_text "$OC_PORT" "$sid" | tr '\n' ' ' | tail -c 200)"
  fi

  # Assert the drop by the beta message's id leaving the outbound list — never by
  # a total count. The listing itself adds synthetic messages, so the count can
  # rise even when a bucket is dropped.
  local beta_id
  beta_id="$(oc_api "$OC_PORT" GET "/api/session/$sid/message" | python3 -c '
import json,sys
d=json.load(sys.stdin); items=d.get("data",d) if isinstance(d,dict) else d
for m in (items if isinstance(items,list) else []):
    t=m.get("text")
    if isinstance(t,str) and "TOPIC-BETA" in t and isinstance(m.get("id"),str):
        print(m["id"]); break
else: print("")')"

  # Beta must be in the outbound list before the reply, or the assertion after it
  # proves nothing.
  if [[ -n "$beta_id" ]] && oc_wait_outbound "$OC_PORT" "$sid" "$dir" "$beta_id" present; then
    pass "opencode:self-config beta message is in the outbound list before the reply"
  else
    fail "opencode:self-config beta in outbound list" \
      "the beta message ($beta_id) was not in the hook's last dispatch"
  fi

  oc_prompt "$OC_PORT" "$sid" "opencode:self-config bare-number reply" '{"text":"2","delivery":"queue"}' \
    || { abort_opencode "$dir" "$sid"; return; }
  if [[ -n "$beta_id" ]] && oc_wait_outbound "$OC_PORT" "$sid" "$dir" "$beta_id" absent; then
    pass "opencode:self-config bare-number reply pruned (a plain \"2\" dropped the beta bucket)"
  else
    fail "opencode:self-config bare-number reply" \
      "the beta message ($beta_id) was still outbound after the reply"
  fi

  if oc_wait_text "$OC_PORT" "$sid" 'ctxed dropped bucket'; then
    pass "opencode:self-config bare-number reply rewrote the message"
  else
    fail "opencode:self-config bare-number rewrite" \
      "the reply was not rewritten; session said: $(oc_session_text "$OC_PORT" "$sid" | tr '\n' ' ' | tail -c 200)"
  fi

  abort_opencode "$dir" "$sid"
  restore_ctxed
}

# ---------------------------------------------------------------------------
# Claude Code functional scenario
# ---------------------------------------------------------------------------

# claude_project_dir <dir> — the directory Claude Code keeps sessions started in
# <dir> under: its real path with every non-alphanumeric character replaced.
claude_project_dir() {
  local real; real="$(cd "$1" && pwd -P)"
  printf '%s/.claude/projects/%s\n' "$HOME" "${real//[^a-zA-Z0-9]/-}"
}

claude_scenario() {
  need claude "Claude Code CLI, authenticated"
  build_ctxed || { fail "claude:build ctxed"; return; }

  mkscratch; local dir="$SCRATCH_DIR"
  MODEL_USED="$CLAUDE_MODEL"
  # Every claude call runs in the scratch dir, so all of the session's state
  # lands in one project directory, which is removed on exit.
  local project; project="$(claude_project_dir "$dir")"
  if [[ -e "$project" ]]; then
    fail "claude:scratch project" "$project already exists; refusing to reuse it"
    return
  fi
  CLAUDE_PROJECTS+=("$project")

  # 1. Create a real session with content, captured as JSON to read the id.
  local out="$dir/first.json"
  if ( cd "$dir" && claude -p --model "$CLAUDE_MODEL" --output-format json \
        "Reply with exactly: alpha. Then a second line: beta." > "$out" 2>"$dir/err" ); then
    pass "claude:create session"
  else
    fail "claude:create session" "$(tail -2 "$dir/err" 2>/dev/null)"
    return
  fi

  local sid
  sid="$(python3 -c 'import json,sys
try: o=json.load(open(sys.argv[1]))
except Exception: sys.exit(1)
print(o.get("session_id") or o.get("sessionId") or "")' "$out" 2>/dev/null)"
  if [[ -z "$sid" ]]; then
    fail "claude:session id" "could not read session_id from JSON output"
    return
  fi
  pass "claude:session id ($sid)"

  # 2. Locate the transcript for that session, in the scratch project.
  local transcript="$project/$sid.jsonl"
  if [[ ! -f "$transcript" ]]; then
    fail "claude:find transcript" "no $transcript"
    return
  fi
  pass "claude:find transcript"

  local before
  before="$("$CTXED" inspect "$transcript" | awk '/^TOTAL/{print $2}')"
  [[ -n "$before" ]] && pass "claude:inspect before ($before entries)" \
    || fail "claude:inspect before"

  # 3. Categorize (offline, deterministic: we feed ctxed the real entry ids, so
  #    no model call is needed for this scenario).
  local cat="$dir/cat.json" catout="$dir/cat.out.json"
  write_categories "$transcript" "$cat"
  if "$CTXED" categorize "$transcript" \
        --categorizer-cmd "cat $cat" --out "$catout" >/dev/null 2>&1; then
    cat="$catout"
    pass "claude:categorize"
  else
    fail "claude:categorize"; return
  fi

  local instruction="$dir/instruction.txt"
  if "$CTXED" compact-instruction "$transcript" --categories-file "$cat" --categories 2 > "$instruction" 2>/dev/null; then
    pass "claude:compact-instruction"
  else
    fail "claude:compact-instruction"; return
  fi

  # 4. Drive the compaction headlessly and assert the session shrank + continued.
  if ( cd "$dir" && claude -p --resume "$sid" --model "$CLAUDE_MODEL" \
        "/compact $(cat "$instruction")" >/dev/null 2>"$dir/err2" ); then
    pass "claude:compact run"
  else
    fail "claude:compact run" "$(tail -2 "$dir/err2" 2>/dev/null)"
    return
  fi

  # Compaction appends to the transcript rather than rewriting it, so an entry
  # count always changes. Assert on what compaction writes instead: a new
  # boundary that shrank the context, followed by the summary.
  local verdict
  verdict="$(python3 -c '
import json, sys
lines = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
b = [i for i, l in enumerate(lines) if l.get("type") == "system" and l.get("subtype") == "compact_boundary"]
if len(b) != 1:
    print("NO_BOUNDARY %d boundaries, want 1 (one /compact)" % len(b)); sys.exit()
m = lines[b[0]].get("compactMetadata") or {}
pre, post = m.get("preTokens"), m.get("postTokens")
if not (isinstance(pre, int) and isinstance(post, int) and post < pre):
    print("NOT_SHRUNK preTokens=%s postTokens=%s" % (pre, post)); sys.exit()
if not any(l.get("isCompactSummary") for l in lines[b[0] + 1:]):
    print("NO_SUMMARY no isCompactSummary entry after the boundary"); sys.exit()
print("OK %s -> %s tokens" % (pre, post))' "$transcript" 2>&1)"
  if [[ "$verdict" == OK* ]]; then
    pass "claude:session compacted (${verdict#OK })"
  else
    fail "claude:session compacted" "$verdict"
  fi

  # ctxed reads the compacted transcript as its live context, which starts at
  # the summary.
  local live
  live="$("$CTXED" inspect "$transcript" --json 2>/dev/null | python3 -c '
import json, sys
r = json.load(sys.stdin)
e = r["entries"]
print("OK" if e and e[0]["kind"] == "summary" else "first entry is %s" % (e[0]["kind"] if e else "missing"))')"
  if [[ "$live" == OK ]]; then
    pass "claude:inspect shows the live context (summary first)"
  else
    fail "claude:inspect shows the live context" "$live"
  fi

  if ( cd "$dir" && claude -p --resume "$sid" --model "$CLAUDE_MODEL" "reply with only: ok" >/dev/null 2>&1 ); then
    pass "claude:session continues"
  else
    fail "claude:session continues"
  fi
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

printf '\n===== functional verification\n'
[[ $RUN_OPENCODE -eq 1 ]] && { echo "--- opencode"; opencode_scenario; }
[[ $RUN_OPENCODE -eq 1 ]] && { echo "--- opencode (self-config, minimal env)"; opencode_selfconfig_scenario; }
[[ $RUN_CLAUDE -eq 1 ]] && { echo "--- claude code"; claude_scenario; }
printf '\n= %d passed, %d failed (hard); %d soft-passed, %d soft-failed\n' \
  "$PASSES" "$FAILURES" "$SOFT_PASSES" "$SOFT_FAILS"
[[ $FAILURES -eq 0 ]]
