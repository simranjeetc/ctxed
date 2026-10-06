#!/usr/bin/env bash
# Modular verification for ctxed.
#
# Usage:
#   scripts/verify.sh                 # offline checks: build, unit tests, vet, openspec, plugin
#   scripts/verify.sh --workspaces    # the above, run in every worktree of this repository
#   scripts/verify.sh --live          # add the live, opt-in checks (Claude Code / OpenCode)
#   scripts/verify.sh --all           # offline + live
#   scripts/verify.sh --claude        # live Claude Code checks only
#   scripts/verify.sh --opencode      # live OpenCode checks only
#
# Exit code is 0 only when every selected check passed. Each check is one
# function and prints `ok`/`FAIL`/`SKIP`; add new checks by adding a function
# and calling it from the relevant group below. A check guards a shipped
# feature: when the feature is absent the check FAILS, it never skips.
#
# Design: offline checks never touch the network, the model, or a real session,
# so they run on every change and in CI. Live checks shell out to a real harness
# and are opt-in. Nothing here rewrites a session or mutates the repo. The live
# Claude Code check creates one throwaway session and deletes its project
# directory afterwards.
#
# Configuration (optional):
#   CTXED_TEST_EXTRA_PATH    prepended to PATH, for tools installed off-PATH
#   CTXED_TEST_CLAUDE_MODEL  model for the live Claude Code check (default haiku)

set -uo pipefail

# ---------------------------------------------------------------------------
# Setup
# ---------------------------------------------------------------------------

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

[[ -n "${CTXED_TEST_EXTRA_PATH:-}" ]] && export PATH="$CTXED_TEST_EXTRA_PATH:$PATH"
CLAUDE_MODEL="${CTXED_TEST_CLAUDE_MODEL:-haiku}"

BIN=""
CMD_GROUP="offline"
RUN_WORKSPACES=0
RUN_LIVE=0
LIVE_CLAUDE=0
LIVE_OPENCODE=0

FAILURES=0
PASSES=0
SKIPS=0

for arg in "$@"; do
  case "$arg" in
    --workspaces) RUN_WORKSPACES=1 ;;
    --live)       RUN_LIVE=1 ;;
    --all)        RUN_LIVE=1 ;;
    --claude)     RUN_LIVE=1; LIVE_CLAUDE=1 ;;
    --opencode)   RUN_LIVE=1; LIVE_OPENCODE=1 ;;
    -h|--help)    sed -n '2,24p' "$0"; exit 0 ;;
    *) echo "unknown flag: $arg" >&2; exit 2 ;;
  esac
done

# A check runs in a subshell, so it cannot touch the counters itself; its exit
# status says what happened: 0 pass, 77 skip, anything else fail.
run() {
  local name="$1"; shift
  local out rc=0
  out="$("$@" 2>&1)" || rc=$?
  case "$rc" in
    0)
      PASSES=$((PASSES + 1))
      printf 'ok   %s\n' "$name"
      ;;
    77)
      SKIPS=$((SKIPS + 1))
      printf 'SKIP %s (%s)\n' "$name" "$(tail -1 <<<"$out")"
      ;;
    *)
      FAILURES=$((FAILURES + 1))
      printf 'FAIL %s\n' "$name"
      printf '%s\n' "$out" | sed 's/^/     | /'
      ;;
  esac
}

# skip <reason> — inside a check, as `skip "why"; return`. Never use it for an
# absent feature: that is a failure.
skip() { printf '%s\n' "$1"; return 77; }

# need <tool> <what> — inside a check, as `need x "why" || return 1`.
need() { command -v "$1" >/dev/null 2>&1 || { echo "missing prerequisite: $1 ($2)"; return 1; }; }

note() { printf '\n--- %s\n' "$1"; }

build_binary() {
  BIN="$ROOT/.verify/ctxed"
  mkdir -p "$ROOT/.verify"
  go build -o "$BIN" ./cmd/ctxed
}

# categories_file <transcript> <model-response> — prints the path of a real
# categories file (numbered buckets) built from a canned categorizer response.
# The fixtures under testdata/ are model responses, not categories files, so a
# selection like `--categories 2` resolves only after this step.
categories_file() {
  local out="$ROOT/.verify/$(basename "$1").categories.json"
  "$BIN" categorize "$1" --categorizer-cmd "cat $2" --out "$out" >/dev/null || return 1
  printf '%s\n' "$out"
}

# ---------------------------------------------------------------------------
# Offline checks (run on every change)
# ---------------------------------------------------------------------------

check_go_build()      { need go "Go toolchain" || return 1; build_binary; }
check_go_vet()        { go vet ./...; }
check_go_test()       { go clean -testcache && go test ./...; }
check_tokenizer_real() { CTXED_TEST_TIKTOKEN=1 go test ./internal/tokenize/; }
check_openspec() {
  need openspec "OpenSpec CLI" || return 1
  # Validate every change directory. Archived changes live under changes/archive.
  local d ok=1
  for d in openspec/changes/*/; do
    [[ -f "$d/proposal.md" ]] || continue
    openspec validate "$(basename "$d")" --strict >/dev/null || ok=0
  done
  [[ $ok -eq 1 ]]
}

# --- CLI behavior on the shipped fixtures (approximate tokenizer, no model) ---

check_cli_inspect() {
  build_binary || return 1
  "$BIN" inspect testdata/claude_session.jsonl | grep -q '^TOTAL'
}

check_cli_drop() {
  build_binary || return 1
  local tmp; tmp="$(mktemp -d)"
  # Entry 0 is a standalone reasoning block, so dropping it is structurally
  # valid. A safe drop exits 0 and writes a smaller transcript.
  "$BIN" drop testdata/claude_session.jsonl --indices 0 --out "$tmp/out.jsonl" >/dev/null 2>&1 || return 1
  [[ -s "$tmp/out.jsonl" ]] || return 1
  # Deterministic and non-destructive: a repeat produces identical bytes.
  "$BIN" drop testdata/claude_session.jsonl --indices 0 --out "$tmp/again.jsonl" >/dev/null 2>&1 || return 1
  cmp -s "$tmp/out.jsonl" "$tmp/again.jsonl" || return 1
  # Dropping a tool call while its result is kept is refused (exit 3).
  local rc=0
  "$BIN" drop testdata/claude_session.jsonl --indices 1 --out "$tmp/bad.jsonl" >/dev/null 2>&1 || rc=$?
  [[ "$rc" -eq 3 ]]
}

check_cli_prune_ids_only() {
  build_binary || return 1
  local cats got
  cats="$(categories_file testdata/claude_session.jsonl testdata/claude_session.categorize.json)" || return 1
  got="$("$BIN" prune testdata/claude_session.jsonl --categories-file "$cats" --categories 2 --ids-only)" || return 1
  local want='{"droppedIds":["c3990fee-117c-4879-90e0-158f2245a45e"],"droppedToolCallIds":["toolu_01JCsQr3BcKZ7jWo2ERi6wH6"]}'
  [[ "$got" == "$want" ]] || { printf 'want: %s\ngot:  %s\n' "$want" "$got"; return 1; }
}

check_cli_prune_empty_selection() {
  build_binary || return 1
  local got want='{"droppedIds":[],"droppedToolCallIds":[]}'
  got="$("$BIN" prune testdata/claude_session.jsonl --ids '' --ids-only)" || return 1
  [[ "$got" == "$want" ]] || { printf 'want: %s\ngot:  %s\n' "$want" "$got"; return 1; }
}

check_cli_compact_instruction() {
  build_binary || return 1
  local cats got
  cats="$(categories_file testdata/claude_session.jsonl testdata/claude_session.categorize.json)" || return 1
  # stdout is exactly the sentence to paste after `/compact `; the caveat note
  # goes to stderr.
  got="$("$BIN" compact-instruction testdata/claude_session.jsonl \
        --categories-file "$cats" --categories 2 2>/dev/null)" || return 1
  local want='When you compact this session, keep the context about Adapter investigation, and drop the context about Tool output.'
  [[ "$(wc -l <<<"$got" | tr -d ' ')" == 1 ]] || { echo "stdout is not one line:"; printf '%s\n' "$got"; return 1; }
  [[ "$got" == "$want" ]] || { printf 'want: %s\ngot:  %s\n' "$want" "$got"; return 1; }
}

check_cli_unknown_id_refused() {
  build_binary || return 1
  # An unknown category id must be refused (exit 3), never silently dropped, on
  # both the id-only and the transcript path. The categories file is valid, so
  # the refusal is about the id and nothing else.
  local cats rc
  cats="$(categories_file testdata/claude_session.jsonl testdata/claude_session.categorize.json)" || return 1
  rc=0
  "$BIN" prune testdata/claude_session.jsonl --categories-file "$cats" --categories 99 --ids-only >/dev/null 2>&1 || rc=$?
  [[ "$rc" -eq 3 ]] || { echo "--ids-only: want exit 3, got $rc"; return 1; }
  rc=0
  "$BIN" prune testdata/claude_session.jsonl --categories-file "$cats" --categories 99 >/dev/null 2>&1 || rc=$?
  [[ "$rc" -eq 3 ]] || { echo "transcript: want exit 3, got $rc"; return 1; }
}

check_offline_purity() {
  # The offline path must not reach the network. `--categorizer-cmd` supplies
  # the model response from a command, so ctxed's HTTP client is never used.
  # The command prints the categorization JSON on stdout (the prompt is ignored),
  # which is exactly the offline/deterministic transport the docs describe.
  build_binary || return 1
  local tmp; tmp="$(mktemp -d)"
  "$BIN" categorize testdata/claude_session.jsonl \
      --categorizer-cmd 'echo "{\"categories\":[{\"label\":\"all\",\"ids\":[\"61e80e18-146b-46e3-bd72-c6bc5e568a42\"]},{\"label\":\"tool\",\"ids\":[\"c3990fee-117c-4879-90e0-158f2245a45e\"]}]}"' \
      --out "$tmp/out.json" >/dev/null 2>&1 || return 1
  [[ -s "$tmp/out.json" ]]
}

# --- Plugin (OpenCode) offline: source tests only ---

check_plugin_opencode_tests() {
  local dir="plugin/opencode"
  need node "Node.js" || return 1
  need npm "npm" || return 1
  [[ -d "$dir" ]] || { echo "missing $dir"; return 1; }
  [[ -d "$dir/node_modules" ]] || ( cd "$dir" && npm install --silent --no-audit --no-fund ) >/dev/null 2>&1 || true
  ( cd "$dir" && node --test test/ )
}

check_plugin_opencode_no_policy() {
  local f="plugin/opencode/src/core.ts"
  [[ -f "$f" ]] || { echo "missing $f"; return 1; }
  # The plugin must carry no prune policy: it runs ctxed, presents the buckets
  # ctxed returned, and filters by id. It may not decide bucket membership or
  # resolve a selection itself. The precise guard is the plugin's own
  # no-policy.test.ts; this is the coarse offline mirror.
  ! grep -qiE 'orphan|validity|classif' "$f"
}

# ---------------------------------------------------------------------------
# Live checks (opt-in; shell out to a real harness)
# ---------------------------------------------------------------------------

# Claude Code: prove the offline half end-to-end against a real transcript, and
# print the exact in-session step. The transcript comes from a throwaway session
# this check creates (never an arbitrary one already on disk), and the project
# directory Claude Code made for it is deleted afterwards.
check_live_claude_cli() {
  need claude "Claude Code CLI, authenticated" || return 1
  need python3 "Python 3" || return 1
  build_binary || return 1
  local tmp project sid rc=0
  tmp="$(mktemp -d)"
  project="$(claude_project_dir "$tmp")"
  [[ -e "$project" ]] && { echo "$project already exists; refusing to reuse it"; rm -rf "$tmp"; return 1; }
  sid="$(cd "$tmp" && claude -p --model "$CLAUDE_MODEL" --output-format json 'Reply with only: ok' 2>/dev/null \
        | python3 -c 'import json,sys
try: print(json.load(sys.stdin).get("session_id",""))
except Exception: print("")')"
  if [[ -n "$sid" ]]; then
    live_claude_cli_steps "$project/$sid.jsonl" || rc=$?
  else
    echo "claude -p in $tmp returned no session id"; rc=1
  fi
  rm -rf "$project" "$tmp"
  return "$rc"
}

live_claude_cli_steps() {
  local transcript="$1"
  [[ -f "$transcript" ]] || { echo "no transcript at $transcript"; return 1; }
  "$BIN" inspect "$transcript" | grep -q '^TOTAL' || return 1
  local tmp; tmp="$(mktemp -d)"
  # Deterministic, offline categorize: a canned response that splits the
  # transcript's real entry ids into two buckets (ctxed requires at least two).
  python3 - "$transcript" "$tmp/response.json" <<'PY2'
import json, sys
ids = []
for line in open(sys.argv[1]):
    try: o = json.loads(line)
    except Exception: continue
    if o.get("type") in ("user", "assistant") and o.get("uuid"):
        ids.append(o["uuid"])
half = max(1, len(ids) // 2)
json.dump({"categories": [{"label": "first", "ids": ids[:half]}, {"label": "second", "ids": ids[half:]}]},
          open(sys.argv[2], "w"))
PY2
  "$BIN" categorize "$transcript" --categorizer-cmd "cat $tmp/response.json" --out "$tmp/cat.json" \
      || { rm -rf "$tmp"; return 1; }
  [[ -s "$tmp/cat.json" ]]; local rc=$?
  rm -rf "$tmp"
  return "$rc"
}

# claude_project_dir <dir> — the directory Claude Code keeps sessions started in
# <dir> under: its real path with every non-alphanumeric character replaced.
claude_project_dir() {
  local real; real="$(cd "$1" && pwd -P)"
  printf '%s/.claude/projects/%s\n' "$HOME" "${real//[^a-zA-Z0-9]/-}"
}

check_live_claude_resume_hint() {
  # Not an assertion: print the manual step a human must do in a real session.
  # Recorded so the live flow is documented at run time.
  note "Claude Code live step (manual)"
  cat <<'EOF'
     1. go build -o ctxed ./cmd/ctxed
     2. ./ctxed categorize <transcript> --model M
     3. edit the buckets in <transcript>.categories.json
     4. ./ctxed compact-instruction <transcript> \
            --categories-file <f> --categories <ids>
     5. paste the printed sentence after `/compact ` in the live session
   This step is model-mediated and cannot be asserted automatically.
EOF
}

# OpenCode: the dispatch hook needs a live OpenCode. We assert the pieces we
# can, then print what a human must do to close task 3.1.
check_live_opencode_pieces() {
  local dir="plugin/opencode"
  need node "Node.js" || return 1
  [[ -d "$dir" ]] || { echo "missing $dir"; return 1; }
  build_binary || return 1
  ( cd "$dir" && node --test test/ ) || return 1
  # The id-only output must be a single-line JSON object.
  local cats got
  cats="$(categories_file testdata/opencode_session.json testdata/opencode_session.categorize.json)" || return 1
  got="$("$BIN" prune testdata/opencode_session.json --categories-file "$cats" --categories 1 --ids-only)" || return 1
  [[ "$(wc -l <<<"$got" | tr -d ' ')" == 1 && "$got" == '{"droppedIds":['* ]] || { echo "got: $got"; return 1; }
}

check_live_opencode_dispatch_hint() {
  note "OpenCode live step (manual, closes task 3.1)"
  cat <<'EOF'
     - configure CTXED_PLUGIN_CTXED_PATH, CTXED_PLUGIN_SESSION_EXPORT,
       CTXED_PLUGIN_CATEGORIES_FILE, CTXED_PLUGIN_CATEGORIES
     - dispatch in a live OpenCode session and confirm the dropped message is
       absent from the request while the stored session is unchanged
     - also confirm the live message id equals the session-export entry id
EOF
}

# ---------------------------------------------------------------------------
# Groups
# ---------------------------------------------------------------------------

run_offline() {
  note "offline: go"
  run "go:build"            check_go_build
  run "go:vet"              check_go_vet
  run "go:test"             check_go_test
  run "go:tokenizer-real"   check_tokenizer_real

  note "offline: openspec"
  run "openspec:validate"   check_openspec

  note "offline: cli"
  run "cli:inspect"             check_cli_inspect
  run "cli:drop"                check_cli_drop
  run "cli:prune --ids-only"    check_cli_prune_ids_only
  run "cli:prune empty"         check_cli_prune_empty_selection
  run "cli:compact-instruction" check_cli_compact_instruction
  run "cli:unknown-id refused"  check_cli_unknown_id_refused
  run "cli:offline purity"      check_offline_purity

  note "offline: plugin/opencode"
  run "plugin:opencode:tests"     check_plugin_opencode_tests
  run "plugin:opencode:no-policy" check_plugin_opencode_no_policy
}

run_live() {
  note "live: Claude Code"
  run "live:claude:cli"  check_live_claude_cli
  check_live_claude_resume_hint

  note "live: OpenCode"
  run "live:opencode:pieces" check_live_opencode_pieces
  check_live_opencode_dispatch_hint
}

run_here() {
  local label="$1"
  printf '\n===== %s (%s)\n' "$label" "$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo '?')"
  run_offline
  [[ $RUN_LIVE -eq 1 ]] && run_live
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

if [[ $RUN_WORKSPACES -eq 1 ]]; then
  while IFS= read -r wt; do
    [[ -f "$wt/scripts/verify.sh" ]] || continue
    ( cd "$wt" && bash scripts/verify.sh $([[ $RUN_LIVE -eq 1 ]] && echo --live) )
    rc=$?
    [[ $rc -eq 0 ]] || FAILURES=$((FAILURES + 1))
  done < <(git worktree list --porcelain | sed -n 's/^worktree //p')
else
  run_here "verify"
fi

printf '\n= %d passed, %d failed, %d skipped\n' "$PASSES" "$FAILURES" "$SKIPS"
[[ $FAILURES -eq 0 ]]
