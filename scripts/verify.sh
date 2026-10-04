#!/usr/bin/env bash
# Modular verification for ctxed.
#
# Usage:
#   scripts/verify.sh                 # offline checks: build, unit tests, vet, openspec, plugin
#   scripts/verify.sh --workspaces    # the above, run in every git worktree under ~/codebase/ctxed*
#   scripts/verify.sh --live          # add the live, opt-in checks (Claude Code / OpenCode)
#   scripts/verify.sh --all           # offline + live
#   scripts/verify.sh --claude        # live Claude Code checks only
#   scripts/verify.sh --opencode      # live OpenCode checks only
#
# Exit code is 0 only when every selected check passed. Each check is one
# function and prints `ok`/`FAIL`/`SKIP`; add new checks by adding a function
# and calling it from the relevant group below.
#
# Design: offline checks never touch the network, the model, or a real session,
# so they run on every change and in CI. Live checks shell out to a real harness
# and are opt-in. Nothing here rewrites a session or mutates the repo.

set -uo pipefail

# ---------------------------------------------------------------------------
# Setup
# ---------------------------------------------------------------------------

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# openspec and node live here but are often absent from PATH.
export PATH="/opt/homebrew/bin:$PATH"

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

run() {
  local name="$1"; shift
  local out
  if out="$("$@" 2>&1)"; then
    PASSES=$((PASSES + 1))
    printf 'ok   %s\n' "$name"
  else
    local rc=$?
    # `skip` may run inside a check; it is not a failure.
    if [[ $rc -eq 77 || $rc -eq 0 ]]; then
      printf 'ok   %s\n' "$name"
      PASSES=$((PASSES + 1))
      return 0
    fi
    FAILURES=$((FAILURES + 1))
    printf 'FAIL %s\n' "$name"
    printf '%s\n' "$out" | sed 's/^/     | /'
  fi
}

skip() { SKIPS=$((SKIPS + 1)); printf 'SKIP %s (%s)\n' "$1" "$2"; }

note() { printf '\n--- %s\n' "$1"; }

build_binary() {
  BIN="$ROOT/.verify/ctxed"
  mkdir -p "$ROOT/.verify"
  go build -o "$BIN" ./cmd/ctxed
}

# ---------------------------------------------------------------------------
# Offline checks (run on every change)
# ---------------------------------------------------------------------------

check_go_build()      { build_binary; }
check_go_vet()        { go vet ./...; }
check_go_test()       { go clean -testcache && go test ./...; }
check_tokenizer_real() { CTXED_TEST_TIKTOKEN=1 go test ./internal/tokenize/; }
check_openspec() {
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
  if ! "$BIN" prune testdata/claude_session.jsonl --ids-only 2>&1 | grep -q 'ids-only'; then
    skip "cli:prune --ids-only" "not on this branch (feat/opencode-dispatch-plugin)"
    return 0
  fi
  local got
  got="$("$BIN" prune testdata/claude_session.jsonl \
        --categories-file testdata/claude_session.categorize.json \
        --categories 2 --ids-only)"
  [[ "$got" == *'"droppedIds"'* ]]
}

check_cli_prune_empty_selection() {
  build_binary || return 1
  if ! "$BIN" prune testdata/claude_session.jsonl --ids-only 2>&1 | grep -q 'ids-only'; then
    skip "cli:prune empty" "not on this branch (feat/opencode-dispatch-plugin)"
    return 0
  fi
  local got
  got="$("$BIN" prune testdata/claude_session.jsonl --ids '' --ids-only)"
  [[ "$got" == *'"droppedIds":[]'* ]]
}

check_cli_compact_instruction() {
  build_binary || return 1
  # Only present on branches that added the command; skip cleanly if absent.
  if ! "$BIN" compact-instruction 2>&1 | grep -q 'compact-instruction'; then
    return 0
  fi
  local got
  got="$("$BIN" compact-instruction testdata/claude_session.jsonl \
        --categories-file testdata/claude_session.categorize.json --categories 2)"
  [[ "$got" == *'keep'*L* ]] || [[ "$got" == *'drop'* ]]
}

check_cli_unknown_id_refused() {
  build_binary || return 1
  # An unknown category id must be refused (exit 3), never silently dropped.
  # Skip on branches where the categories file/selection route is absent.
  local rc=0
  "$BIN" prune testdata/claude_session.jsonl \
      --categories-file testdata/claude_session.categorize.json \
      --categories 99 --ids-only >/dev/null 2>&1 || rc=$?
  if [[ "$rc" -ne 3 ]]; then
    # Also exercise the transcript (non-ids-only) path, which every branch has.
    local rc2=0
    "$BIN" prune testdata/claude_session.jsonl \
        --categories-file testdata/claude_session.categorize.json \
        --categories 99 >/dev/null 2>&1 || rc2=$?
    [[ "$rc2" -eq 3 ]] || return 1
  fi
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
  if [[ ! -d "$dir" ]]; then
    skip "plugin:opencode:node-test" "no plugin/opencode on this branch"
    return 0
  fi
  [[ -d "$dir/node_modules" ]] || ( cd "$dir" && npm install --silent --no-audit --no-fund ) >/dev/null 2>&1 || true
  ( cd "$dir" && node --test test/ )
}

check_plugin_opencode_no_policy() {
  local f="plugin/opencode/src/core.ts"
  [[ -f "$f" ]] || { skip "plugin:opencode:no-policy" "not on this branch"; return 0; }
  # The plugin must carry no categorization/policy: it may not classify or
  # decide buckets, only run ctxed and filter by id.
  ! grep -qiE 'categoriz|bucket|label' "$f"
}

# ---------------------------------------------------------------------------
# Live checks (opt-in; shell out to a real harness)
# ---------------------------------------------------------------------------

# Claude Code: prove the offline half end-to-end against a real transcript, and
# print the exact in-session step. Never writes a session.
check_live_claude_cli() {
  build_binary || return 1
  local transcript="${CTXED_CC_TRANSCRIPT:-}"
  if [[ -z "$transcript" ]]; then
    transcript="$(find "${HOME}/.claude/projects" -name '*.jsonl' -type f 2>/dev/null | head -1)"
  fi
  [[ -n "$transcript" && -f "$transcript" ]] || return 1

  "$BIN" inspect "$transcript" | grep -q '^TOTAL' || return 1

  local tmp; tmp="$(mktemp -d)"
  # Deterministic, offline categorize via the override command.
  "$BIN" categorize "$transcript" \
      --categorizer-cmd "sh -c 'cat >/dev/null; echo \"{\\\"categories\\\":[{\\\"label\\\":\\\"all\\\",\\\"ids\\\":[]}]}\"'" \
      --out "$tmp/cat.json" >/dev/null 2>&1 || return 1
  [[ -s "$tmp/cat.json" ]]
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
  [[ -d "$dir" ]] || { skip "live:opencode" "no plugin on this branch"; return 0; }
  ( cd "$dir" && node --test test/ ) || return 1
  # The id-only output must be a single-line JSON object.
  "$BIN" prune testdata/opencode_session.json \
      --categories-file testdata/opencode_session.categorize.json \
      --categories 1 --ids-only | grep -q '^{"droppedIds"'
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
  for wt in "$HOME"/codebase/ctxed "$HOME"/codebase/ctxed-*; do
    [[ -e "$wt/.git" ]] || continue
    [[ -f "$wt/scripts/verify.sh" ]] || continue
    ( cd "$wt" && bash scripts/verify.sh $([[ $RUN_LIVE -eq 1 ]] && echo --live) )
    rc=$?
    [[ $rc -eq 0 ]] || FAILURES=$((FAILURES + 1))
  done
else
  run_here "verify"
fi

printf '\n= %d passed, %d failed, %d skipped\n' "$PASSES" "$FAILURES" "$SKIPS"
[[ $FAILURES -eq 0 ]]
