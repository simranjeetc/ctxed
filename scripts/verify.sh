#!/usr/bin/env bash
# Modular verification for ctxed.
#
# Usage:
#   scripts/verify.sh                 # offline checks: build, unit tests, vet, parked unreachable
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
# so they run on every change and in CI. Live checks run
# scripts/verify-functionally.sh against real harness sessions and are opt-in.
# Nothing here rewrites a session or mutates the repo.
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

# ---------------------------------------------------------------------------
# Offline checks (run on every change)
# ---------------------------------------------------------------------------

check_go_build()      { need go "Go toolchain" || return 1; build_binary; }
check_go_vet()        { go vet ./...; }
check_go_test()       { go clean -testcache && go test ./...; }
check_tokenizer_real() { CTXED_TEST_TIKTOKEN=1 go test ./internal/tokenize/; }

# --- CLI behavior on the shipped fixtures (approximate tokenizer, no model) ---

check_cli_inspect() {
  build_binary || return 1
  "$BIN" inspect testdata/claude_session.jsonl | grep -q '^TOTAL'
}

check_cli_overview() {
  build_binary || return 1
  # The canned response names two entries; the rest join the nearest topic.
  local out insp
  out="$("$BIN" overview testdata/opencode_session.json --json \
        --categorizer-cmd "cat testdata/opencode_session.categorize.json")" || return 1
  insp="$("$BIN" inspect testdata/opencode_session.json | sed -n 's/^TOTAL \([0-9]*\) entries · \([0-9,]*\) tokens.*/\1 \2/p' | tr -d ,)"
  python3 -c '
import json, sys
r = json.load(sys.stdin); want = sys.argv[1].split()
got = [str(r["entries"]), str(r["tokens"])]
rows = [str(sum(x["entries"] for x in r["rows"])), str(sum(x["tokens"] for x in r["rows"]))]
sys.exit(0 if got == want == rows else "overview %s, rows %s, inspect %s" % (got, rows, want))' "$insp" <<<"$out"
}

check_cli_parked_unreachable() {
  build_binary || return 1
  # Pruning is parked: the binary must not reach any of its commands.
  local c rc
  for c in drop prune compact-instruction opencode; do
    rc=0; "$BIN" "$c" >/dev/null 2>&1 || rc=$?
    [[ "$rc" -eq 2 ]] || { echo "ctxed $c: want exit 2 (unknown command), got $rc"; return 1; }
  done
}

check_offline_purity() {
  # The offline path must not reach the network. `--categorizer-cmd` supplies
  # the model response from a command, so ctxed's HTTP client is never used.
  # The command prints the categorization JSON on stdout (the prompt is ignored),
  # which is exactly the offline/deterministic transport the docs describe.
  build_binary || return 1
  local tmp; tmp="$(mktemp -d)"
  "$BIN" categorize testdata/claude_session.jsonl \
      --categorizer-cmd 'echo "{\"categories\":[{\"label\":\"all\",\"ids\":[\"aaaaaaaa-0000-4000-8000-0000000000a1\"]},{\"label\":\"tool\",\"ids\":[\"cccccccc-0000-4000-8000-0000000000c3\"]}]}"' \
      --out "$tmp/out.json" >/dev/null 2>&1 || return 1
  [[ -s "$tmp/out.json" ]]
}

# ---------------------------------------------------------------------------
# Live checks (opt-in; real harness sessions, cheap models)
# ---------------------------------------------------------------------------

check_live() { "$ROOT/scripts/verify-functionally.sh" "$1"; }

# ---------------------------------------------------------------------------
# Groups
# ---------------------------------------------------------------------------

run_offline() {
  note "offline: go"
  run "go:build"            check_go_build
  run "go:vet"              check_go_vet
  run "go:test"             check_go_test
  run "go:tokenizer-real"   check_tokenizer_real

  note "offline: cli"
  run "cli:inspect"                 check_cli_inspect
  run "cli:overview"                check_cli_overview
  run "cli:parked unreachable"      check_cli_parked_unreachable
  run "cli:offline purity"          check_offline_purity
}

run_live() {
  local any=0
  [[ $LIVE_CLAUDE -eq 1 ]] && { note "live: Claude Code"; run "live:claude" check_live --claude; any=1; }
  [[ $LIVE_OPENCODE -eq 1 ]] && { note "live: OpenCode"; run "live:opencode" check_live --opencode; any=1; }
  [[ $any -eq 1 ]] || { note "live: both harnesses"; run "live:all" check_live --all; }
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
