# Tasks

## 1. Remove feature detection and stale fallbacks

- [ ] 1.1 In `scripts/verify.sh`, delete the "not on this branch" guards in `check_cli_prune_ids_only`, `check_cli_prune_empty_selection`, `check_cli_compact_instruction`, `check_plugin_opencode_tests`, `check_plugin_opencode_no_policy`; a missing feature fails
- [ ] 1.2 Fix `run()` so a check that calls `skip` is counted only as a skip, never also as a pass
- [ ] 1.3 Rewrite `check_cli_compact_instruction` to assert the exact expected sentence for `testdata/claude_session.categorize.json --categories 2` (golden string), and that stdout is one line
- [ ] 1.4 In `scripts/verify-functionally.sh`, delete the `~/codebase/ctxed-oc` and `~/codebase/ctxed-cc` fallbacks in `opencode_scenario`, `opencode_selfconfig_scenario`, `claude_scenario`; always build from `$ROOT`
- [ ] 1.5 Update `docs/verification-strategy.md` and `docs/verifier-agents.md`: remove the per-branch table row and the sibling-worktree paragraph; state that an absent feature fails

## 2. Fix checks that cannot fail

- [ ] 2.1 Anti-drift: use a per-question sentinel (`NONE-<n>`) and read only the assistant reply to that question; never grep all assistant text
- [ ] 2.2 Apply the same per-question reading to the "attachment content not recallable" check
- [ ] 2.3 Attachment-dropped check: locate the message carrying the attached file by its file part, assert its id is in the alpha bucket and absent from the last outbound list
- [ ] 2.4 Introduce `soft-pass`/`soft-fail` statuses for model-recall checks; `ok` in the JSON report is computed from hard checks only; document this in the report contract
- [ ] 2.5 Add a negative control: run the anti-drift and exact-drop checks once with **no** selection recorded and assert they FAIL (proves the checks can fail); keep it as a scenario step

## 3. Determinism and isolation

- [ ] 3.1 Add `oc_wait_idle` and replace every fixed `sleep` after a prompt with it (hard timeout, failure message names the step)
- [ ] 3.2 Self-config scenario swaps the freshly built binary into the plugin's first search location (`~/go/bin/ctxed`) and restores the original (or removes the copy) in the EXIT trap, including on failure and interrupt; assert the plugin resolved that path (see design); never runs a pre-installed binary
- [ ] 3.3 `check_live_claude_cli` uses a transcript created by the script (a throwaway `claude -p` session), not `find … | head -1`
- [ ] 3.4 `claude_scenario` deletes the `~/.claude/projects/<sanitized scratch dir>` it created, unless `--keep` and a failure
- [ ] 3.5 Remove hardcoded `/opt/homebrew/bin`, `~/codebase/ctxed-*`, and the `OPENCODE_PASSWORD` default; add `CTXED_TEST_EXTRA_PATH`; report each missing prerequisite by name

## 4. Acceptance

- [ ] 4.1 `scripts/verify.sh` passes on `main`; temporarily removing the `--ids-only` flag from `internal/cli` makes it FAIL (check by hand, then revert)
- [ ] 4.2 `scripts/verify-functionally.sh --opencode` passes; the negative-control step from 2.5 reports the expected failures as passes of the control
- [ ] 4.3 `scripts/verify-functionally.sh --claude` passes and leaves no new directory under `~/.claude/projects`
- [ ] 4.4 `openspec validate fix-verification-false-passes --strict` passes
