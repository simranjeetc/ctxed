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
# Exit 0 only when every hard check passed. The codeword recall checks are hard:
# after a prune the model must name the kept topic's code word and not the
# dropped one's. Other model-recall checks are soft: they are reported as
# soft-pass/soft-fail and never decide the exit code or `ok`.
# Requires a live harness and spends a few small model calls per scenario. See
# docs/verification-strategy.md.
#
# The script builds ctxed from this checkout and exercises nothing else. It
# reports missing prerequisites by name instead of failing obscurely.
#
# Side effects outside the scratch directories, all undone on exit (also on
# failure and on Ctrl-C):
#   - --opencode: the scratch OpenCode server shares the real session store; the
#     scratch session is deleted on exit. `ctxed opencode categorize` with no
#     stub would create a throwaway categorizer session; the suite uses a stub.
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
# transcript it received, i.e. what goes out when no drop list is applied.
# Empty when the log is missing or unreadable. The plugin logs every request.
hook_ids() {
  grep -v '"hook":"compaction"' "$1/hook.log" | tail -1 2>/dev/null | python3 -c '
import json,sys
try: print(" ".join(x for x in json.loads(sys.stdin.read())[sys.argv[1]] if x))
except Exception: print("")' "$2"
}

# hook_outbound <dir> — the ids the dispatch hook sent in its most recent decision.
hook_outbound() { hook_ids "$1" after; }

# oc_buckets <state-file> <out> — writes {"alpha": [...], "beta": [...]}, the
# entry ids of the alpha and beta topics in the state file's last listing.
oc_buckets() {
  python3 - "$1" "$2" <<'PY'
import json, sys
alpha, beta = [], []
try: listing = json.load(open(sys.argv[1])).get("listing") or []
except Exception: listing = []
for c in listing:
    label = (c.get("label") or "").lower()
    if "alpha" in label: alpha += c.get("entryIds", [])
    elif "beta" in label: beta += c.get("entryIds", [])
json.dump({"alpha": sorted(alpha), "beta": sorted(beta)}, open(sys.argv[2], "w"))
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

# Bundles the plugin into a scratch OpenCode project and starts a server there.
# Sets OC_PORT. Returns non-zero if the plugin cannot be built. The drop lists
# live in <dir>/state (CTXED_STATE_DIR), and the plugin logs every request's
# ids before and after its filter to <dir>/hook.log.
start_opencode_with_plugin() {
  local dir="$1"
  local plugin_dir="$ROOT/plugin/opencode"
  [[ -d "$plugin_dir" ]] || return 1

  local proj="$dir/proj"
  mkdir -p "$proj/.opencode/plugins" "$dir/state"
  # No scenario answers a question; deny the interactive `question` tool so a
  # turn can never block waiting for a human.
  printf '%s\n' '{"permissions":[{"action":"question","resource":"*","effect":"deny"}]}' > "$proj/opencode.json"
  printf '{"name":"ctxed-fn","private":true,"type":"module","dependencies":{"@opencode/plugin":"2.0.22"}}\n' \
    > "$proj/.opencode/package.json"
  ( cd "$proj/.opencode" && npm install --no-audit --no-fund --silent >/dev/null 2>&1 ) || return 1
  ( cd "$plugin_dir" && [[ -x node_modules/.bin/esbuild ]] || npm install --no-audit --no-fund --silent >/dev/null 2>&1 ) || return 1
  ( cd "$plugin_dir" && ./node_modules/.bin/esbuild src/plugin.ts --bundle --format=esm --platform=node \
      --external:@opencode/plugin --outfile="$proj/.opencode/plugins/ctxed-prune.js" >/dev/null 2>&1 ) || return 1

  # A deterministic categorizer. It groups the entries ctxed lists by the topic
  # marker in their text (TOPIC-ALPHA / -BETA / -GAMMA); an entry with no marker
  # (a reply, a question) joins the topic before it, and entries before any
  # marker form an "other" topic; a single topic is split in two. Real ctxed
  # exports the live session, chooses what is listed and resolves the ids; the
  # stub only chooses the grouping, so no model call is needed to categorize.
  cat > "$dir/stub-categorize.py" <<'PY'
#!/usr/bin/env python3
import re, sys, json
groups, order, current = {}, [], "other"
for line in sys.stdin.read().splitlines():
    m = re.match(r'- id=(\S+) ', line)
    if not m:
        continue
    low = line.lower()
    for topic in ("alpha", "beta", "gamma"):
        if "topic-" + topic in low:
            current = topic
    if current not in groups:
        groups[current] = []
        order.append(current)
    groups[current].append(m.group(1))
cats = [{"label": t + " topic", "ids": groups[t]} for t in order]
if len(cats) == 1:  # ctxed needs two categories: split the only topic in half
    ids = cats[0]["ids"]
    cats = [{"label": cats[0]["label"], "ids": ids[: len(ids) // 2]},
            {"label": cats[0]["label"] + " (rest)", "ids": ids[len(ids) // 2 :]}]
print(json.dumps({"categories": cats}))
PY
  chmod +x "$dir/stub-categorize.py"

  local port; port="$(free_port)"
  ( cd "$proj" \
    && OPENCODE_PASSWORD="$(opencode_password)" \
       CTXED_STATE_DIR="$dir/state" \
       CTXED_PLUGIN_DEBUG_LOG="$dir/hook.log" \
       opencode serve --port "$port" >"$dir/server.log" 2>&1 & echo $! > "$dir/server.pid" )
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
# session's drop list lives in the scratch directory and goes with it.
abort_opencode() {
  local dir="$1" sid="${2:-}"
  if [[ -n "$sid" ]]; then
    oc_api "$OC_PORT" DELETE "/api/session/$sid" >/dev/null 2>&1
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

# oc_ctxed <dir> <sid> <args…> — runs `ctxed opencode <args…>` on a session the
# way the ctxed-prune skill does, with the scenario's state dir and stub
# categorizer. ctxed exports the session itself.
oc_ctxed() {
  local dir="$1" sid="$2"; shift 2
  CTXED_STATE_DIR="$dir/state" CTXED_CATEGORIZER_CMD="$dir/stub-categorize.py" \
    "$CTXED" opencode "$@" --session "$sid"
}

# oc_category <dir> <sid> <word> — the number of the listed topic whose label
# contains the word, from the session's state file.
oc_category() {
  python3 - "$1/state/$2.json" "$3" <<'PY'
import json, sys
try: s = json.load(open(sys.argv[1]))
except Exception: s = {}
for c in s.get("listing") or []:
    if sys.argv[2] in c.get("label", ""):
        print(c["id"]); break
PY
}

# oc_state_ids <dir> <sid> <what> — space-separated ids from the state file:
# "dropped" (the drop list) or "listed" (every id in the last listing).
oc_state_ids() {
  python3 - "$1/state/$2.json" "$3" <<'PY'
import json, sys
try: s = json.load(open(sys.argv[1]))
except Exception: s = {}
if sys.argv[2] == "dropped":
    ids = (s.get("dropped") or {}).get("ids") or []
else:
    ids = [i for c in s.get("listing") or [] for i in c.get("entryIds", [])]
print(" ".join(ids))
PY
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

  if ! start_opencode_with_plugin "$dir"; then
    fail "opencode:plugin server" "could not build the plugin or start the server (see $dir/server.log)"
    return
  fi
  pass "opencode:plugin server (port $OC_PORT)"

  # A multi-message session across two topics, so a topic spans several
  # messages. Alpha carries the code words to be dropped; beta must survive. One
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
    fail "opencode:create session"; abort_opencode "$dir"; return
  fi
  SCRATCH_SESSIONS+=("$sid")

  oc_api "$OC_PORT" POST "/api/session/$sid/model" \
    "{\"model\":{\"id\":\"${model#*/}\",\"providerID\":\"${model%%/*}\"}}" >/dev/null
  oc_prompt "$OC_PORT" "$sid" "opencode:turn alpha 1" \
    "{\"text\":\"$alpha1\",\"delivery\":\"queue\"}" || { abort_opencode "$dir" "$sid"; return; }
  # The filter runs on every request, with or without a drop list.
  if [[ -s "$dir/hook.log" ]]; then
    pass "opencode:plugin loaded (its filter saw the first request)"
  else
    fail "opencode:plugin loaded" "no request reached the plugin's filter (see $dir/server.log)"
    abort_opencode "$dir" "$sid"; return
  fi
  oc_prompt "$OC_PORT" "$sid" "opencode:turn beta 1" \
    "{\"text\":\"$beta1\",\"delivery\":\"queue\"}" || { abort_opencode "$dir" "$sid"; return; }
  oc_prompt "$OC_PORT" "$sid" "opencode:turn alpha 2" \
    "{\"text\":\"$alpha2\",\"delivery\":\"queue\"}" || { abort_opencode "$dir" "$sid"; return; }
  oc_prompt "$OC_PORT" "$sid" "opencode:turn alpha attachment" \
    "{\"text\":\"TOPIC-ALPHA: here is a file for the alpha topic. Reply with only: ok\",\"files\":[{\"uri\":\"file://$attach\",\"name\":\"$(basename "$attach")\"}],\"delivery\":\"queue\"}" \
    || { abort_opencode "$dir" "$sid"; return; }
  local messages_before
  messages_before="$(oc_api "$OC_PORT" GET "/api/session/$sid/message" | python3 -c 'import json,sys; print(len(json.load(sys.stdin).get("data",[])))')"

  # --- Prune 1: the commands the skill runs ---------------------------------
  local listing
  listing="$(oc_ctxed "$dir" "$sid" categorize 2>&1)"
  if grep -q 'alpha topic' <<<"$listing" && grep -q 'beta topic' <<<"$listing"; then
    pass "opencode:categorize lists the topics (exported the live session itself)"
  else
    fail "opencode:categorize lists the topics" "$(head -c 300 <<<"$listing")"
    abort_opencode "$dir" "$sid"; return
  fi
  local alpha_n; alpha_n="$(oc_category "$dir" "$sid" alpha)"
  local dropped_out; dropped_out="$(oc_ctxed "$dir" "$sid" drop "$alpha_n" 2>&1)"
  if grep -q '^Dropped "alpha topic"' <<<"$dropped_out"; then
    pass "opencode:drop records the pick ($(head -1 <<<"$dropped_out" | cut -c1-60)…)"
  else
    fail "opencode:drop records the pick" "$dropped_out"
    abort_opencode "$dir" "$sid"; return
  fi
  # The prune posts nothing to the session, so the model has nothing to answer.
  local messages_after
  messages_after="$(oc_api "$OC_PORT" GET "/api/session/$sid/message" | python3 -c 'import json,sys; print(len(json.load(sys.stdin).get("data",[])))')"
  if [[ "$messages_after" == "$messages_before" ]]; then
    pass "opencode:prune adds no message to the session ($messages_after before and after)"
  else
    fail "opencode:prune adds no message to the session" "$messages_before before, $messages_after after"
  fi

  # The drop is exact: every alpha id is absent from the next request and every
  # beta id is present. This reads the plugin's filter log, so it does not
  # depend on what a model remembers.
  oc_prompt "$OC_PORT" "$sid" "opencode:turn after prune" \
    '{"text":"Reply with only: ok","delivery":"queue"}' || { abort_opencode "$dir" "$sid"; return; }
  local buckets="$dir/buckets.json"
  oc_buckets "$dir/state/$sid.json" "$buckets"
  local membership counts verdict
  membership="$(drop_verdict "$buckets" "$(hook_outbound "$dir")")"
  counts="$(grep -oE 'ALPHA=[0-9]+ BETA=[0-9]+' <<<"$membership" | head -1)"
  verdict="$(grep -E '^(OK|NO_ALPHA|LEAK |DROPPED_BETA )' <<<"$membership" | head -1)"
  local alpha_count; alpha_count="$(grep -oE 'ALPHA=[0-9]+' <<<"$counts" | cut -d= -f2)"
  if [[ "${alpha_count:-0}" -gt 1 ]]; then
    pass "opencode:topic spans several messages ($counts)"
  else
    fail "opencode:topic spans several messages" "$membership"
  fi
  if [[ "$verdict" == "OK" ]]; then
    pass "opencode:exact drop (every alpha id absent, every beta id retained)"
  else
    fail "opencode:exact drop" "$membership"
  fi

  # Negative control: the same check against what the filter received must fail,
  # or the check cannot fail at all. It also proves the exported ids are the ids
  # the request carries.
  local control_verdict
  control_verdict="$(drop_verdict "$buckets" "$(hook_ids "$dir" before)" | grep -E '^(OK|NO_ALPHA|LEAK |DROPPED_BETA )' | head -1)"
  if [[ "$control_verdict" == LEAK* ]]; then
    pass "opencode:negative control (the request carried the alpha ids before the filter)"
  else
    fail "opencode:negative control (exact drop)" \
      "before the filter the check reported '${control_verdict:-nothing}', not LEAK"
  fi

  # The codeword test: asked in the same session, the model names the beta word
  # and neither alpha word.
  local none_cw="NONE-C$$" recall_q
  recall_q="List every word or number you were asked to remember in this conversation, one per line, exactly as given. If there are none, reply with only $none_cw."
  oc_prompt "$OC_PORT" "$sid" "opencode:codeword recall question" \
    "{\"text\":\"$recall_q\",\"delivery\":\"queue\"}" || { abort_opencode "$dir" "$sid"; return; }
  local cw_reply cw
  cw_reply="$(oc_reply "$OC_PORT" "$sid" "$OC_LAST_ID")"
  cw="$(codeword_verdict "$cw_reply" "$beta1suffix" "$alpha1suffix" "$alpha2suffix")"
  if [[ "$cw" == OK ]]; then
    pass "opencode:codeword recall (the model names BETA-$beta1suffix, not the alpha words)"
  else
    fail "opencode:codeword recall" "$cw; reply: $(head -c 200 <<<"$cw_reply")"
  fi

  # Stored session unchanged: every message is still in the transcript, including
  # the dropped topic's.
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

  # A dropped message that carried a file attachment takes the file content with
  # it. Find THE message that carries the attachment by its file part, then
  # assert it is in the alpha topic and absent from the last request.
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
    pass "opencode:attachment message dropped with its topic (${attach_check#OK })"
  else
    fail "opencode:attachment message dropped with its topic" "$attach_check"
  fi

  # A message added after the prune is kept, even on the dropped topic. Hard: in
  # the same request the new message is present while every dropped message is
  # still absent, so the pass cannot come from a drop list that stopped applying.
  local codeword="CHERRY-$$" none_new="NONE-N$$"
  oc_prompt "$OC_PORT" "$sid" "opencode:turn post-prune alpha" \
    "{\"text\":\"TOPIC-ALPHA: the alpha code word is $codeword. Reply with only: noted\",\"delivery\":\"queue\"}" \
    || { abort_opencode "$dir" "$sid"; return; }
  local new_id="$OC_LAST_ID"
  oc_prompt "$OC_PORT" "$sid" "opencode:post-prune recall question" \
    "{\"text\":\"What is the alpha code word? Reply with only the code word, or with only $none_new if you do not know it.\",\"delivery\":\"queue\"}" \
    || { abort_opencode "$dir" "$sid"; return; }
  local new_q="$OC_LAST_ID" new_out new_verdict
  new_out="$(hook_outbound "$dir")"
  new_verdict="$(drop_verdict "$buckets" "$new_out" | grep -E '^(OK|NO_ALPHA|LEAK |DROPPED_BETA )' | head -1)"
  if [[ -n "$new_out" ]] && grep -q -- "$new_id" <<<"$new_out" && [[ "$new_verdict" == OK ]]; then
    pass "opencode:post-prune message kept (dropped messages still absent in the same request)"
  elif [[ "$new_verdict" != OK ]]; then
    fail "opencode:post-prune message kept" "the drop list no longer applied in that request: ${new_verdict:-nothing logged}"
  else
    fail "opencode:post-prune message kept" "the post-prune message $new_id was dropped from the request"
  fi
  local new_reply
  new_reply="$(oc_reply "$OC_PORT" "$sid" "$new_q")"
  if grep -q -- "$codeword" <<<"$new_reply"; then
    soft 0 "opencode:post-prune recall (the model can name the new code word)"
  else
    soft 1 "opencode:post-prune recall" "reply: $(head -c 200 <<<"$new_reply")"
  fi

  # --- Prune 2: only what is still sent is listed; the drop list grows --------
  local gamma_word="GAMMA-$$"
  oc_prompt "$OC_PORT" "$sid" "opencode:turn gamma" \
    "{\"text\":\"TOPIC-GAMMA: the gamma code word is $gamma_word. Reply with only: noted\",\"delivery\":\"queue\"}" \
    || { abort_opencode "$dir" "$sid"; return; }
  local first_dropped; first_dropped="$(oc_state_ids "$dir" "$sid" dropped)"
  listing="$(oc_ctxed "$dir" "$sid" categorize 2>&1)"
  local relisted
  relisted="$(python3 - "$first_dropped" "$(oc_state_ids "$dir" "$sid" listed)" "$new_id" <<'PY'
import sys
dropped, listed, new_id = set(sys.argv[1].split()), set(sys.argv[2].split()), sys.argv[3]
again = sorted(dropped & listed)
if not listed:
    print("NOTHING_LISTED")
elif again:
    print("RELISTED " + ",".join(again)[:200])
elif new_id not in listed:
    print("MISSING_NEW " + new_id)
else:
    print("OK %d listed" % len(listed))
PY
)"
  if [[ "$relisted" == OK* ]]; then
    pass "opencode:second prune lists only what is still sent (${relisted#OK }, none from the first prune)"
  else
    fail "opencode:second prune lists only what is still sent" "$relisted; listing: $(head -c 200 <<<"$listing")"
  fi
  local gamma_n; gamma_n="$(oc_category "$dir" "$sid" gamma)"
  local gamma_ids; gamma_ids="$(python3 - "$dir/state/$sid.json" <<'PY'
import json, sys
s = json.load(open(sys.argv[1]))
print(" ".join(i for c in s.get("listing") or [] if "gamma" in c["label"] for i in c["entryIds"]))
PY
)"
  if [[ -z "$gamma_n" ]] || ! oc_ctxed "$dir" "$sid" drop "$gamma_n" >/dev/null 2>&1; then
    fail "opencode:second drop" "no gamma topic to drop; listing: $(head -c 200 <<<"$listing")"
    abort_opencode "$dir" "$sid"; return
  fi
  local grew
  grew="$(python3 - "$first_dropped" "$gamma_ids" "$(oc_state_ids "$dir" "$sid" dropped)" <<'PY'
import sys
first, gamma, now = set(sys.argv[1].split()), set(sys.argv[2].split()), set(sys.argv[3].split())
lost = sorted(first - now)
print("LOST " + ",".join(lost)[:200] if lost else ("NO_GAMMA" if not gamma or not gamma <= now else "OK %d" % len(now)))
PY
)"
  if [[ "$grew" == OK* ]]; then
    pass "opencode:drop list grows (first prune kept, gamma added: ${grew#OK } ids)"
  else
    fail "opencode:drop list grows" "$grew"
  fi
  oc_prompt "$OC_PORT" "$sid" "opencode:second codeword recall question" \
    "{\"text\":\"$recall_q\",\"delivery\":\"queue\"}" || { abort_opencode "$dir" "$sid"; return; }
  cw_reply="$(oc_reply "$OC_PORT" "$sid" "$OC_LAST_ID")"
  cw="$(codeword_verdict "$cw_reply" "$beta1suffix" "$alpha1suffix" "$alpha2suffix" "$gamma_word")"
  if [[ "$cw" == OK ]]; then
    pass "opencode:second codeword recall (BETA kept; APPLE, 42 and $gamma_word gone)"
  else
    fail "opencode:second codeword recall" "$cw; reply: $(head -c 200 <<<"$cw_reply")"
  fi

  # --- Across an OpenCode compaction ----------------------------------------
  oc_api "$OC_PORT" POST "/api/session/$sid/compact" '{"delivery":"queue"}' >/dev/null
  oc_wait_idle "$OC_PORT" "$sid" "opencode:compaction" 240 || { abort_opencode "$dir" "$sid"; return; }
  local export_c="$dir/compacted-export.json"
  opencode session export "$sid" > "$export_c" 2>/dev/null
  local pre_compaction
  pre_compaction="$(python3 - "$export_c" <<'PY'
import json, sys
try: ms = json.load(open(sys.argv[1]))["messages"]
except Exception: ms = []
last = max([i for i, m in enumerate(ms) if m.get("type") == "compaction"], default=-1)
print("NONE" if last < 0 else " ".join(m.get("id", "") for m in ms[:last]))
PY
)"
  if [[ "$pre_compaction" == NONE ]]; then
    fail "opencode:compaction happened" "no compaction item in the export"
    abort_opencode "$dir" "$sid"; return
  fi
  pass "opencode:compaction happened"
  # The compaction summary is written by its own request; the plugin must filter
  # that one too, or the summary brings the dropped messages back.
  local cmp_check
  cmp_check="$(python3 - "$dir/hook.log" "$(oc_state_ids "$dir" "$sid" dropped)" <<'PY'
import json, sys
lines = [json.loads(l) for l in open(sys.argv[1]) if '"hook":"compaction"' in l]
dropped = set(sys.argv[2].split())
if not lines:
    print("NO_COMPACTION_REQUEST"); sys.exit()
before, after = set(lines[-1]["before"]), set(lines[-1]["after"])
if not dropped & before:
    print("NOTHING_TO_FILTER the compaction request carried no dropped id"); sys.exit()
leaked = sorted(dropped & after)
print("LEAK " + ",".join(leaked)[:200] if leaked else "OK %d dropped ids removed" % len(dropped & before))
PY
)"
  if [[ "$cmp_check" == OK* ]]; then
    pass "opencode:compaction request filtered (${cmp_check#OK })"
  else
    fail "opencode:compaction request filtered" "$cmp_check"
  fi
  oc_prompt "$OC_PORT" "$sid" "opencode:turn after compaction" \
    '{"text":"TOPIC-BETA: reply with only: ok","delivery":"queue"}' || { abort_opencode "$dir" "$sid"; return; }
  listing="$(oc_ctxed "$dir" "$sid" categorize 2>&1)"
  local stale
  stale="$(python3 - "$pre_compaction" "$(oc_state_ids "$dir" "$sid" listed)" <<'PY'
import sys
pre, listed = set(sys.argv[1].split()), set(sys.argv[2].split())
bad = sorted(pre & listed)
print("STALE " + ",".join(bad)[:200] if bad else "OK %d listed" % len(listed))
PY
)"
  if [[ "$stale" == OK* ]]; then
    pass "opencode:after compaction only newer messages are listed (${stale#OK })"
  else
    fail "opencode:after compaction only newer messages are listed" "$stale; listing: $(head -c 200 <<<"$listing")"
  fi
  oc_prompt "$OC_PORT" "$sid" "opencode:codeword recall after compaction" \
    "{\"text\":\"$recall_q\",\"delivery\":\"queue\"}" || { abort_opencode "$dir" "$sid"; return; }
  cw_reply="$(oc_reply "$OC_PORT" "$sid" "$OC_LAST_ID")"
  local leaked=""
  for w in "$alpha1suffix" "$alpha2suffix" "$gamma_word"; do
    grep -q -- "$w" <<<"$cw_reply" && leaked="$leaked $w"
  done
  if [[ -z "$leaked" && -n "$cw_reply" ]]; then
    pass "opencode:dropped code words stay unknown after compaction"
  else
    fail "opencode:dropped code words stay unknown after compaction" \
      "${leaked:+known again:$leaked; }reply: $(head -c 200 <<<"$cw_reply")"
  fi

  # Regression: a dropped tool result must not accumulate in the request. Read a
  # file (a tool call + id-less result) on the alpha topic, drop that topic, and
  # confirm the id-less content does not grow across later requests.
  # Inside the scratch project, by its real path (the server sees /private/var,
  # not the /var symlink): a read outside the project asks for permission and
  # blocks the session.
  local bulk; bulk="$(cd "$dir/proj" && pwd -P)/bulk-$attachsuffix.txt"
  python3 -c 'import sys; open(sys.argv[1],"w").write("Z"*4000)' "$bulk"
  oc_prompt "$OC_PORT" "$sid" "opencode:turn file read" \
    "{\"text\":\"TOPIC-ALPHA: read the file at $bulk and reply with only: ok\",\"delivery\":\"queue\"}" \
    || { abort_opencode "$dir" "$sid"; return; }
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
  oc_ctxed "$dir" "$sid" categorize >/dev/null 2>&1
  local read_n; read_n="$(oc_category "$dir" "$sid" alpha)"
  [[ -n "$read_n" ]] && oc_ctxed "$dir" "$sid" drop "$read_n" >/dev/null 2>&1
  null_count() { grep -v '"hook":"compaction"' "$dir/hook.log" | tail -1 2>/dev/null | python3 -c '
import json,sys
try: print(sum(1 for m in json.loads(sys.stdin.read())["after"] if m is None))
except Exception: print(-1)'; }
  oc_prompt "$OC_PORT" "$sid" "opencode:turn after file read 1" '{"text":"Reply with only: ok","delivery":"queue"}' \
    || { abort_opencode "$dir" "$sid"; return; }
  local before_null; before_null="$(null_count)"
  oc_prompt "$OC_PORT" "$sid" "opencode:turn after file read 2" '{"text":"Reply with only: ok","delivery":"queue"}' \
    || { abort_opencode "$dir" "$sid"; return; }
  local after_null; after_null="$(null_count)"
  if [[ "$after_null" -ge 0 && "$after_null" -le "${before_null:-0}" ]]; then
    pass "opencode:no tool-result accumulation (id-less messages flat at $after_null)"
  else
    fail "opencode:no tool-result accumulation" "id-less messages grew from ${before_null:-?} to $after_null"
  fi

  # Delete the scratch session while the server is still up.
  abort_opencode "$dir" "$sid"
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

# write_topic_categories <transcript.jsonl> <out> — a deterministic categorizer
# for the Claude Code codeword scenario: groups the transcript's entries by the
# topic marker in their text (TOPIC-ALPHA / TOPIC-BETA), and gives an unmarked
# entry (a reply) the topic of the entry before it. Labels name the topic, so
# the /compact instruction built from them says which topic to drop.
write_topic_categories() {
  python3 - "$1" "$2" <<'PY'
import json, sys
groups, current = {"alpha": [], "beta": []}, "alpha"
for line in open(sys.argv[1]):
    try: o = json.loads(line)
    except Exception: continue
    if o.get("type") not in ("user", "assistant") or not o.get("uuid"):
        continue
    text = json.dumps((o.get("message") or {}).get("content"))
    if "TOPIC-ALPHA" in text: current = "alpha"
    elif "TOPIC-BETA" in text: current = "beta"
    groups[current].append(o["uuid"])
if not groups["alpha"] or not groups["beta"]:
    sys.exit(4)
json.dump({"categories": [
    {"label": "TOPIC-ALPHA (the alpha code word)", "ids": groups["alpha"]},
    {"label": "TOPIC-BETA (the beta code word)", "ids": groups["beta"]},
]}, open(sys.argv[2], "w"))
PY
}

# claude_ask <dir> <sid> <prompt> [extra claude flags…] — one headless turn on a
# session; prints the model's reply text.
claude_ask() {
  local dir="$1" sid="$2" prompt="$3"; shift 3
  ( cd "$dir" && claude -p --resume "$sid" "$@" --model "$CLAUDE_MODEL" --output-format json "$prompt" 2>>"$dir/err" ) \
    | python3 -c 'import json,sys
try: print(json.load(sys.stdin).get("result") or "")
except Exception: print("")'
}

# codeword_verdict <reply> <kept word> <dropped word…> — the codeword test on one
# reply: OK when the kept word is named and no dropped word is.
codeword_verdict() {
  local reply="$1" kept="$2"; shift 2
  local w
  for w in "$@"; do
    grep -q -- "$w" <<<"$reply" && { echo "LEAK $w is still known"; return; }
  done
  grep -q -- "$kept" <<<"$reply" || { echo "LOST $kept is no longer known"; return; }
  echo OK
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

  # The codeword test: two topics, each carrying a unique code word. Alpha is
  # pruned, beta is kept. After the prune the session must still know the beta
  # word and must not know the alpha word.
  local alpha_word="PELICAN-$$" beta_word="TURBINE-$$"
  local recall_q="List every code word you were told in this conversation, one per line, exactly as given. If there are none, reply with only NONE."

  # 1. Create a real session with the two topics, captured as JSON to read the id.
  local out="$dir/first.json"
  if ( cd "$dir" && claude -p --model "$CLAUDE_MODEL" --output-format json \
        "TOPIC-ALPHA: the alpha code word is $alpha_word. Reply with only: ok" > "$out" 2>"$dir/err" ); then
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

  if [[ -n "$(claude_ask "$dir" "$sid" "TOPIC-BETA: the beta code word is $beta_word. Reply with only: ok")" ]]; then
    pass "claude:second topic"
  else
    fail "claude:second topic" "$(tail -2 "$dir/err" 2>/dev/null)"; return
  fi

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

  # Negative control: before the prune, the same question on a fork of the
  # session names BOTH words, so the codeword check below can fail. A fork gets
  # its own session id and leaves this transcript untouched.
  local control
  control="$(claude_ask "$dir" "$sid" "$recall_q" --fork-session)"
  if grep -q -- "$alpha_word" <<<"$control" && grep -q -- "$beta_word" <<<"$control"; then
    pass "claude:negative control (before the prune the model names both code words)"
  else
    fail "claude:negative control" "reply: $(head -c 200 <<<"$control")"
  fi

  # 3. Categorize (offline, deterministic: a stub groups the real entry ids by
  #    topic, so no model call is needed to categorize).
  local cat="$dir/cat.json" catout="$dir/cat.out.json"
  write_topic_categories "$transcript" "$cat" || { fail "claude:categorize" "no alpha/beta entries in the transcript"; return; }
  if "$CTXED" categorize "$transcript" \
        --categorizer-cmd "cat $cat" --out "$catout" >/dev/null 2>&1; then
    cat="$catout"
    pass "claude:categorize"
  else
    fail "claude:categorize"; return
  fi

  # Drop category 1 (alpha), keep category 2 (beta).
  local instruction="$dir/instruction.txt"
  if "$CTXED" compact-instruction "$transcript" --categories-file "$cat" --categories 1 > "$instruction" 2>/dev/null; then
    pass "claude:compact-instruction"
  else
    fail "claude:compact-instruction"; return
  fi

  # 4. Drive the compaction headlessly, as the user would by pasting the line.
  if ( cd "$dir" && claude -p --resume "$sid" --model "$CLAUDE_MODEL" \
        "/compact $(cat "$instruction")" >/dev/null 2>"$dir/err2" ); then
    pass "claude:compact run"
  else
    fail "claude:compact run" "$(tail -2 "$dir/err2" 2>/dev/null)"
    return
  fi

  # Compaction appends to the transcript rather than rewriting it, so an entry
  # count always changes. Assert on what compaction writes instead: a new
  # boundary that shrank the context, followed by the summary. Then the codeword
  # test on the file: the live context (summary, preserved messages, everything
  # after the boundary) carries the beta word and not the alpha word.
  local verdict
  verdict="$(python3 - "$transcript" "$alpha_word" "$beta_word" <<'PY'
import json, sys
path, dropped, kept = sys.argv[1:]
lines = [json.loads(l) for l in open(path) if l.strip()]
b = [i for i, l in enumerate(lines) if l.get("type") == "system" and l.get("subtype") == "compact_boundary"]
if len(b) != 1:
    print("NO_BOUNDARY %d boundaries, want 1 (one /compact)" % len(b)); sys.exit()
m = lines[b[0]].get("compactMetadata") or {}
pre, post = m.get("preTokens"), m.get("postTokens")
if not (isinstance(pre, int) and isinstance(post, int) and post < pre):
    print("NOT_SHRUNK preTokens=%s postTokens=%s" % (pre, post)); sys.exit()
if not any(l.get("isCompactSummary") for l in lines[b[0] + 1:]):
    print("NO_SUMMARY no isCompactSummary entry after the boundary"); sys.exit()
preserved = set((m.get("preservedMessages") or {}).get("uuids") or [])
live = json.dumps([l for i, l in enumerate(lines) if i > b[0] or l.get("uuid") in preserved])
if dropped in live:
    print("LEAK %s is still in the live context" % dropped); sys.exit()
if kept not in live:
    print("LOST %s is not in the live context" % kept); sys.exit()
print("OK %s -> %s tokens" % (pre, post))
PY
)"
  if [[ "$verdict" == OK* ]]; then
    pass "claude:session compacted (${verdict#OK })"
    pass "claude:codeword in the file (live context has $beta_word, not $alpha_word)"
  elif [[ "$verdict" == LEAK* || "$verdict" == LOST* ]]; then
    pass "claude:session compacted"
    fail "claude:codeword in the file" "$verdict"
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

  # 5. The session continues, and the model itself knows beta and not alpha.
  local reply cw
  reply="$(claude_ask "$dir" "$sid" "$recall_q")"
  if [[ -z "$reply" ]]; then
    fail "claude:session continues" "$(tail -2 "$dir/err" 2>/dev/null)"; return
  fi
  pass "claude:session continues"
  cw="$(codeword_verdict "$reply" "$beta_word" "$alpha_word")"
  if [[ "$cw" == OK ]]; then
    pass "claude:codeword recall (the model names $beta_word, not $alpha_word)"
  else
    fail "claude:codeword recall" "$cw; reply: $(head -c 200 <<<"$reply")"
  fi
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

printf '\n===== functional verification\n'
[[ $RUN_OPENCODE -eq 1 ]] && { echo "--- opencode"; opencode_scenario; }
[[ $RUN_CLAUDE -eq 1 ]] && { echo "--- claude code"; claude_scenario; }
printf '\n= %d passed, %d failed (hard); %d soft-passed, %d soft-failed\n' \
  "$PASSES" "$FAILURES" "$SOFT_PASSES" "$SOFT_FAILS"
[[ $FAILURES -eq 0 ]]
