# Proposal

## Why

A review on 2026-10-06 (see `docs/handover-review-2026-10-06.md`) found that
several checks in the verification scripts pass when the behavior they guard is
broken. A verifier that cannot fail is worse than none: it reports green while
the feature regresses. Line numbers below refer to the working tree reviewed
(HEAD `12b1d07` plus the uncommitted edits to `scripts/verify-functionally.sh`).

- **Anti-drift false pass.** `opencode_scenario` greps *all* assistant text in
  the session for `unknown` (`scripts/verify-functionally.sh:824`). The
  preceding attachment question (~line 786) explicitly asks the model to reply
  `unknown`, so the anti-drift check passes regardless of the drop.
- **Duplicate attachment check.** The "attachment message dropped with its
  bucket" check (~lines 750–783) computes the same `alpha ∩ after` set as the
  exact-drop check. It never identifies the message that carries the
  attachment, so it proves nothing new.
- **Feature detection hides regressions.** Both feature branches are merged
  (`feat/opencode-dispatch-plugin` and `feat/claude-code-live-prune` are 0
  commits ahead of `main`). Yet:
  - `scripts/verify.sh` skips or returns 0 when `--ids-only`,
    `compact-instruction` or `plugin/opencode` are absent (lines ~129, 142,
    154, 198, 207). Removing a shipped feature now reads as SKIP or ok.
  - `scripts/verify-functionally.sh` falls back to `~/codebase/ctxed-oc` and
    `~/codebase/ctxed-cc` (lines ~475, 894, 1016), worktrees that are 16 and 28
    commits behind `main`, so a grep miss silently tests stale code.
- **`run()` double-counts a skip.** In `scripts/verify.sh`, a check that calls
  `skip` and returns 0 increments both `SKIPS` and `PASSES`.
- **`check_cli_compact_instruction` is near-vacuous.** Its glob
  `*'keep'*L* || *'drop'*` passes on almost any output, and the check returns 0
  when the command is missing.
- **Self-config tests the installed binary, not the checkout.**
  `opencode_selfconfig_scenario` SKIPs unless a `ctxed` exists in a standard
  location (line ~908), then the plugin runs *that* binary, which may be stale.
- **`verify.sh --claude` is non-deterministic.** `check_live_claude_cli` picks
  `find ~/.claude/projects -name '*.jsonl' | head -1`, an arbitrary real
  transcript.
- **Claude scenario leaks scratch projects.** `claude_scenario` runs `claude -p`
  in a `mktemp -d` directory, which creates a project directory under
  `~/.claude/projects/-private-var-folders-…` that is never deleted.
- **Not runnable by a stranger.** Hardcoded `/opt/homebrew/bin`,
  `~/codebase/ctxed-*`, and a default `OPENCODE_PASSWORD=opencode` contradict
  the rule in `docs/verification-strategy.md` ("a stranger can clone the repo,
  run one command").
- **Sleeps instead of waiting.** The OpenCode scenarios use fixed `sleep 6/8/14`
  after each prompt, so they are slow and flaky under load.

## What Changes

- Every check either asserts the real behavior or fails; no check passes because
  a feature is absent.
- Remove the sibling-worktree fallback and all branch feature-detection guards.
- Fix the anti-drift and attachment checks so each asserts something distinct.
- Replace fixed sleeps with polling until the session is idle.
- The self-config scenario installs the freshly built binary into a scratch
  location the plugin searches, instead of relying on whatever is installed.
- The Claude live check uses a transcript the script created, and the Claude
  scenario deletes the project directory it created.
- Remove machine-specific defaults; prerequisites are configurable and reported
  by name when missing.
- Update `docs/verification-strategy.md`: drop the per-branch table and the
  "auto-runs against the sibling worktree" paragraph.

## Capabilities

### New Capabilities

- `verification-integrity`: rules every verification check must satisfy so a
  regression cannot read as a pass.

## Impact

- `scripts/verify.sh`, `scripts/verify-functionally.sh`,
  `docs/verification-strategy.md`, `docs/verifier-agents.md`.
- No product code changes. Runtime of the functional suite should drop because
  waiting replaces fixed sleeps.
