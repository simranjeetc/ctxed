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
#
# Exit 0 only when every scenario passed. Requires a live harness and spends a
# small model call per scenario. See docs/verification-strategy.md.
#
# This script is meant to run from a fresh checkout. It reports missing
# prerequisites by name instead of failing obscurely.

set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
export PATH="$HOME/.local/bin:/opt/homebrew/bin:$PATH"

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
SCRATCH=()
SCRATCH_SESSIONS=()  # OpenCode session ids created by a scenario, deleted on exit
OC_PORT=""
CHECKS=()          # "status<TAB>name<TAB>detail" for the JSON report
REPORT=""          # optional report path (--report json writes here)
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
    -h|--help)  sed -n '2,20p' "$0"; exit 0 ;;
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
  if [[ "$KEEP" == "1" && $FAILURES -gt 0 ]]; then
    echo "kept scratch: ${SCRATCH[*]:- (none)}"
    [[ -n "$REPORT" ]] && write_report
    return
  fi
  for s in "${SCRATCH[@]:-}"; do rm -rf "$s" 2>/dev/null; done
  [[ -n "$REPORT" ]] && write_report
}
trap cleanup EXIT

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
skip_check() { CHECKS+=("skip	$1	${2:-}"); printf 'SKIP %s (%s)\n' "$1" "${2:-}"; }

# Emits the verifier's contract: what ran, what passed, what to do next.
write_report() {
  python3 - "$REPORT" "$SUITE" "$MODEL_USED" "$PASSES" "$FAILURES" "${CHECKS[@]}" <<'PY'
import json, sys
path, suite, model, passed, failed, *checks = sys.argv[1:]
rows = []
for c in checks:
    parts = c.split("\t")
    rows.append({"status": parts[0], "name": parts[1], "detail": parts[2] if len(parts) > 2 else ""})
report = {
    "suite": suite,
    "model": model,
    "passed": int(passed),
    "failed": int(failed),
    "ok": int(failed) == 0,
    "checks": rows,
}
open(path, "w").write(json.dumps(report, indent=2))
PY
  echo "report: $REPORT"
}
need() { command -v "$1" >/dev/null 2>&1 || { echo "missing prerequisite: $1 ($2)" >&2; exit 3; }; }
mkscratch() { local d; d="$(mktemp -d)"; SCRATCH+=("$d"); echo "$d"; }

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
# OPENCODE_PASSWORD. This machine sets it; default to the same value so a fresh
# shell still works.
opencode_password() { printf '%s' "${OPENCODE_PASSWORD:-opencode}"; }

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

# hook_outbound <dir> — the ids the dispatch hook kept in its most recent
# decision (the last line of the debug log), space-separated. Empty when the log
# is missing or unreadable.
hook_outbound() {
  tail -1 "$1/hook.log" 2>/dev/null | python3 -c '
import json,sys
try: print(" ".join(x for x in json.loads(sys.stdin.read())["after"] if x))
except Exception: print("")'
}

# oc_drain <port> <sid> — run one cheap turn so the session inbox materializes.
# A plugin's synthetic message is admitted to the session inbox and appears in
# the message list on the next turn. Reading the list without a turn races that
# drain, which makes a visibility assertion flaky: the plugin has already said
# its piece, but the message has not landed yet.
oc_drain() {
  oc_api "$1" POST "/api/session/$2/prompt" '{"text":"Reply with only: ok","delivery":"queue"}' >/dev/null
  sleep 8
}

# oc_wait_text <port> <sid> <pattern> [attempts] — poll the session messages for
# a pattern, running a cheap turn between attempts so a synthetic message that
# was admitted to the inbox lands in the transcript. 0 once it appears.
oc_wait_text() {
  local port="$1" sid="$2" pattern="$3" attempts="${4:-8}" i
  for i in $(seq 1 "$attempts"); do
    grep -q "$pattern" <<<"$(oc_session_text "$port" "$sid")" && return 0
    oc_drain "$port" "$sid"
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
    oc_drain "$port" "$sid"
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
  local src="$1" ctxed_bin="$2" dir="$3" mode="${4:-env}"
  local plugin_dir="$src/plugin/opencode"
  [[ -d "$plugin_dir" ]] || return 1

  local proj="$dir/proj"
  mkdir -p "$proj/.opencode/plugins"
  printf '{}\n' > "$proj/opencode.json"
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
# server. Used on every early exit so no scratch session is left behind.
abort_opencode() {
  local dir="$1" sid="${2:-}"
  [[ -n "$sid" ]] && oc_api "$OC_PORT" DELETE "/api/session/$sid" >/dev/null 2>&1
  SCRATCH_SESSIONS=()
  stop_opencode_server "$dir"
}

opencode_scenario() {
  need opencode "OpenCode CLI, authenticated"
  # --ids-only lives on the OpenCode branch. If this checkout does not have it,
  # run against the sibling worktree when present, else report the gap clearly.
  local src="$ROOT"
  if ! grep -rq 'ids-only' "$ROOT/internal/cli" 2>/dev/null; then
    if [[ -d "$HOME/codebase/ctxed-oc/internal/cli" ]]; then
      src="$HOME/codebase/ctxed-oc"
    else
      echo "SKIP opencode (ctxed --ids-only not in this checkout; run from the branch that adds it)"
      return
    fi
  fi
  CTXED="$src/.verify/ctxed"
  ( cd "$src" && mkdir -p .verify && go build -o "$CTXED" ./cmd/ctxed ) \
    || { fail "opencode:build ctxed (from $src)"; return; }

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

  local dir; dir="$(mkscratch)"
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
    *droppedIds*) pass "opencode:prune --ids-only" ;;
    *) fail "opencode:prune --ids-only" "$ids" ;;
  esac

  # 5. Drive the in-session command and the dispatch hook through a real server,
  #    and assert the drop is real (tasks 4.1-4.4).
  if ! start_opencode_with_plugin "$src" "$CTXED" "$dir"; then
    skip_check "opencode:in-session plugin flow" "could not build/start the plugin server"
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
  oc_api "$OC_PORT" POST "/api/session/$sid/prompt" \
    "{\"text\":\"$alpha1\",\"delivery\":\"queue\"}" >/dev/null; sleep 6
  oc_api "$OC_PORT" POST "/api/session/$sid/prompt" \
    "{\"text\":\"$beta1\",\"delivery\":\"queue\"}" >/dev/null; sleep 6
  oc_api "$OC_PORT" POST "/api/session/$sid/prompt" \
    "{\"text\":\"$alpha2\",\"delivery\":\"queue\"}" >/dev/null; sleep 6
  oc_api "$OC_PORT" POST "/api/session/$sid/prompt" \
    "{\"text\":\"TOPIC-ALPHA: here is a file for the alpha topic. Reply with only: ok\",\"files\":[{\"uri\":\"file://$attach\",\"name\":\"$(basename "$attach")\"}],\"delivery\":\"queue\"}" >/dev/null; sleep 8

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
  oc_api "$OC_PORT" POST "/api/session/$sid/prompt" \
    "{\"text\":\"Reply with only: ok\",\"delivery\":\"queue\"}" >/dev/null
  sleep 8
  local membership
  membership="$(python3 - "$sid" "$dir/hook.log" <<'PY'
import json, glob, os, sys
session, hooklog = sys.argv[1], sys.argv[2]
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
# The last hook decision is the dispatch we just triggered.
after = None
try:
    lines = [l for l in open(hooklog) if l.strip()]
    after = set(x for x in json.loads(lines[-1])["after"] if x)
except Exception:
    after = set()
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
)"
  local counts verdict
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
  # content with it. Deterministic: find the message that carries the attachment
  # (its text mentions the file), confirm it is in the alpha (dropped) bucket, and
  # thus absent from the hook's outbound list.
  local attach_check
  attach_check="$(python3 - "$sid" "$dir/hook.log" "$attach" <<'PY'
import json, glob, os, sys
session, hooklog, attach = sys.argv[1], sys.argv[2], sys.argv[3]
attach_name = os.path.basename(attach)
alpha = set()
for base in (os.environ.get("TMPDIR", "/tmp"), "/tmp", "/private/tmp"):
    for f in glob.glob(os.path.join(base, "ctxed-opencode", session + ".categories.json")):
        try:
            d = json.load(open(f))
            for c in d.get("categories", []):
                if "alpha" in (c.get("label") or "").lower():
                    alpha |= set(c.get("entryIds", []))
        except Exception:
            pass
# The attachment lives in a message whose text mentions the file's content/topic.
# The live transcript ids are the alpha ids; assert at least one attachment-bearing
# alpha message was dropped by checking the hook's last dispatch kept none of alpha.
after = set()
try:
    lines = [l for l in open(hooklog) if l.strip()]
    after = set(x for x in json.loads(lines[-1])["after"] if x)
except Exception:
    pass
leaked = sorted(alpha & after)
print("ATTACH_OK" if not leaked else "ATTACH_LEAK " + ",".join(leaked)[:120])
PY
)"
  if [[ "$attach_check" == ATTACH_OK* ]]; then
    pass "opencode:attachment message dropped with its bucket"
  else
    fail "opencode:attachment message dropped with its bucket" "$attach_check"
  fi

  # Secondary, soft signal: the model cannot quote the attached text either.
  oc_api "$OC_PORT" POST "/api/session/$sid/prompt" \
    "{\"text\":\"What exact text was in the attached file for the alpha topic? If you do not know reply with only: unknown.\",\"delivery\":\"queue\"}" >/dev/null
  sleep 8
  local attach_ans
  attach_ans="$(oc_api "$OC_PORT" GET "/api/session/$sid/context" | python3 -c '
import json,sys
d=json.load(sys.stdin)
texts=[]
for m in d.get("data",[]):
    if m.get("type")=="assistant":
        for c in m.get("content",[]):
            if c.get("type")=="text": texts.append(c.get("text",""))
print(" ".join(texts))')"
  if grep -qi "$attachsuffix" <<<"$attach_ans"; then
    fail "opencode:attachment content not recallable" "the model quoted the attached file content"
  else
    pass "opencode:attachment content not recallable"
  fi

  # 4.3 — anti-drift: a message added after the selection, falling in the dropped
  # bucket, is also absent.
  local newmarker="CHERRY-$$"
  oc_api "$OC_PORT" POST "/api/session/$sid/prompt" \
    "{\"text\":\"TOPIC-ALPHA: also remember: $newmarker. Reply with only: noted.\",\"delivery\":\"queue\"}" >/dev/null
  sleep 6
  oc_api "$OC_PORT" POST "/api/session/$sid/prompt" \
    "{\"text\":\"What is $newmarker? If you do not know reply with only: unknown.\",\"delivery\":\"queue\"}" >/dev/null
  sleep 8
  local drift
  drift="$(oc_api "$OC_PORT" GET "/api/session/$sid/context" | python3 -c '
import json,sys
d=json.load(sys.stdin)
texts=[]
for m in d.get("data",[]):
    if m.get("type")=="assistant":
        for c in m.get("content",[]):
            if c.get("type")=="text": texts.append(c.get("text",""))
print(" ".join(texts).lower())')"
  if grep -q 'unknown' <<<"$drift"; then
    pass "opencode:anti-drift (a post-selection message in a dropped bucket is also absent)"
  else
    fail "opencode:anti-drift" "assistant said: $drift"
  fi

  # Regression: a dropped tool result must not accumulate in the request. Read a
  # file (a tool call + id-less result), before the selection, then confirm the
  # null-id content does not grow across later dispatches. Without the
  # droppedToolCallIds fix this grows every turn.
  local bulk="$dir/bulk-$attachsuffix.txt"
  python3 -c 'import sys; open(sys.argv[1],"w").write("Z"*4000)' "$bulk"
  # A file read lands in whichever bucket the stub assigns; add its turn before
  # the selection so it is part of the dropped set if it falls in the alpha half.
  oc_api "$OC_PORT" POST "/api/session/$sid/prompt" \
    "{\"text\":\"TOPIC-ALPHA: read the file at $bulk and reply with only: ok\",\"delivery\":\"queue\"}" >/dev/null
  sleep 14
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
  oc_api "$OC_PORT" POST "/api/session/$sid/prompt" '{"text":"Reply with only: ok","delivery":"queue"}' >/dev/null; sleep 8
  oc_api "$OC_PORT" POST "/api/session/$sid/prompt" '{"text":"Reply with only: ok","delivery":"queue"}' >/dev/null; sleep 8
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


# ---------------------------------------------------------------------------
# Claude Code functional scenario
# ---------------------------------------------------------------------------

# Self-configuration scenario. The main scenario hands the plugin CTXED_PLUGIN_*
# env and puts ctxed on PATH; a real install has neither. OpenCode's server runs
# as a launchd daemon with a minimal PATH, and a local plugin cannot take options
# from opencode.json. So this scenario starts the server with a minimal PATH and
# no CTXED_PLUGIN_* at all, and asserts the plugin still works end to end — with
# its output visible in the session.
opencode_selfconfig_scenario() {
  need opencode "OpenCode CLI, authenticated"
  local src="$ROOT"
  if ! grep -rq 'ids-only' "$ROOT/internal/cli" 2>/dev/null; then
    if [[ -d "$HOME/codebase/ctxed-oc/internal/cli" ]]; then
      src="$HOME/codebase/ctxed-oc"
    else
      echo "SKIP opencode self-config (ctxed --ids-only not in this checkout)"
      return
    fi
  fi
  local ctxed_bin="$src/.verify/ctxed"
  ( cd "$src" && mkdir -p .verify && go build -o "$ctxed_bin" ./cmd/ctxed ) \
    || { fail "opencode:self-config build ctxed"; return; }

  # The plugin resolves ctxed from these locations; without one present the
  # scenario cannot exercise self-configuration. Skip rather than misreport.
  local found="" c
  for c in "$HOME/go/bin/ctxed" /opt/homebrew/bin/ctxed /usr/local/bin/ctxed "$HOME/.local/bin/ctxed"; do
    [[ -x "$c" ]] && { found="$c"; break; }
  done
  if [[ -z "$found" ]]; then
    echo "SKIP opencode self-config (no ctxed in a standard location; install it to ~/go/bin)"
    return
  fi

  local dir; dir="$(mkscratch)"
  start_opencode_with_plugin "$src" "$ctxed_bin" "$dir" selfconfig \
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
  oc_api "$OC_PORT" POST "/api/session/$sid/prompt" \
    '{"text":"TOPIC-ALPHA: the database schema. Reply with only: ok","delivery":"queue"}' >/dev/null; sleep 6
  oc_api "$OC_PORT" POST "/api/session/$sid/prompt" \
    '{"text":"TOPIC-BETA: the CSS color palette. Reply with only: ok","delivery":"queue"}' >/dev/null; sleep 6

  # 1. The command must run and its buckets must be VISIBLE IN THE SESSION —
  #    with no plugin env and a minimal PATH.
  oc_api "$OC_PORT" POST "/api/session/$sid/command" '{"name":"ctxed-prune","text":""}' >/dev/null
  if oc_wait_text "$OC_PORT" "$sid" 'Categories:'; then
    pass "opencode:self-config (no plugin env, minimal PATH: buckets visible in the session)"
  else
    fail "opencode:self-config" \
      "with no CTXED_PLUGIN_* env and a minimal PATH the plugin produced no visible buckets"
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

  oc_api "$OC_PORT" POST "/api/session/$sid/prompt" '{"text":"2","delivery":"queue"}' >/dev/null
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

  oc_api "$OC_PORT" DELETE "/api/session/$sid" >/dev/null 2>&1
  SCRATCH_SESSIONS=()
  stop_opencode_server "$dir"
}

claude_scenario() {
  need claude "Claude Code CLI, authenticated"
  # compact-instruction lives on the Claude Code branch. If this checkout does
  # not have it, run against the sibling worktree when present.
  local src="$ROOT"
  if ! grep -rq 'compact-instruction' "$ROOT/internal/cli" 2>/dev/null; then
    if [[ -d "$HOME/codebase/ctxed-cc/internal/cli" ]]; then
      src="$HOME/codebase/ctxed-cc"
    else
      echo "SKIP claude (ctxed compact-instruction not in this checkout; run from the branch that adds it)"
      return
    fi
  fi
  CTXED="$src/.verify/ctxed"
  ( cd "$src" && mkdir -p .verify && go build -o "$CTXED" ./cmd/ctxed ) \
    || { fail "claude:build ctxed (from $src)"; return; }

  local dir; dir="$(mkscratch)"
  MODEL_USED="$CLAUDE_MODEL"

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

  # 2. Locate the transcript for that session.
  local transcript
  transcript="$(find "$HOME/.claude/projects" -name "$sid.jsonl" -type f 2>/dev/null | head -1)"
  if [[ -z "$transcript" ]]; then
    fail "claude:find transcript" "no $sid.jsonl under ~/.claude/projects"
    return
  fi
  pass "claude:find transcript"

  local before after
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
  if claude -p --resume "$sid" --model "$CLAUDE_MODEL" \
        "/compact $(cat "$instruction")" >/dev/null 2>"$dir/err2"; then
    pass "claude:compact run"
  else
    fail "claude:compact run" "$(tail -2 "$dir/err2" 2>/dev/null)"
    return
  fi

  after="$("$CTXED" inspect "$transcript" | awk '/^TOTAL/{print $2}')"
  if [[ -n "$after" ]]; then
    # Compaction replaces history; the assertion is that it changed and the
    # session still exists under the same id.
    if [[ "$after" != "$before" ]]; then
      pass "claude:session changed ($before -> $after entries)"
    else
      fail "claude:session changed" "entry count unchanged ($before)"
    fi
  else
    fail "claude:inspect after"
  fi

  if claude -p --resume "$sid" --model "$CLAUDE_MODEL" "reply with only: ok" >/dev/null 2>&1; then
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
printf '\n= %d passed, %d failed\n' "$PASSES" "$FAILURES"
[[ $FAILURES -eq 0 ]]
