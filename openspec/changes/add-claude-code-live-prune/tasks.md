# Tasks

## 1. ctxed `compact-instruction` (D3a)

- [x] 1.1 Add `internal/compact` rendering a single `/compact` instruction from a categories file plus the selected category ids: name the kept buckets (unselected) and the dropped buckets (selected) by label; reuse `categorize.Select` so an unknown id errors as in `prune`
- [x] 1.2 Unit-test the renderer: two of three buckets; a single bucket; the serial-comma list; all buckets dropped; an unknown id; a missing id among valid ones; determinism; empty-label fallback
- [x] 1.3 Wire `ctxed compact-instruction <session> --categories-file F --categories 1,3` into `internal/cli`, reading the session only to validate the categories file; usage/exit codes match the other commands
- [x] 1.4 CLI tests: labels of the kept/dropped buckets appear; output is one line; unknown category refused; missing `--categories` is a usage error; deterministic; the session file is unchanged; works with stdin closed
- [x] 1.5 Document the flow in the README (`categorize` → pick buckets → `compact-instruction` → `/compact <text>`) and in `docs/adapters.md` Claude Code notes

## 2. Claude Code compact mod (D3b)

Revised 2026-10-06 (review handover `docs/handover-review-2026-10-06.md`). The mod
API is GA since Claude Code 2.1.287 and the drop-with-handles mechanism is proven
offline (`docs/claude-code-mods-spike.md`). Build on
`move-harness-formats-into-ctxed` (`--from claude-mod`, `--session-key`); do not
re-implement format translation or selection state in the mod.

- [ ] 2.1 Scaffold the mod (`.claude-plugin/plugin.json`, `hooks/hooks.json`, `hooks/register.js`) registering `session.compact`; `claude plugin validate` passes; loads in a live session (use `CLAUDE_CODE_HOOKS_SAME_THREAD=1` if the `modsOffAt` kill-switch from the spike is still set)
- [ ] 2.2 Confirm in a real session that `$.session.compact()` installs its result live between turns and that the hook runs for the main conversation (`agentId` absent); record the answers in the spike doc
- [ ] 2.3 Mapping decision (settled): categorize the **live** `$.session.messages()` by piping them to `ctxed categorize - --from claude-mod --session-key claude/<session>`, so bucket entry ids **are** handles. Do not map `.jsonl` uuids to live messages. First verify that a message's `handle` is stable across turns and between `$.session.messages()` and `e.messages` in the compact hook; if it is not, stop and record the finding before choosing a fallback (content match)
- [ ] 2.4 In the `session.compact` hook: get the dropped handles from `ctxed prune - --from claude-mod --session-key …` and return `{ messages: e.messages.filter(m => !dropped.has(m.handle)) }`; kept messages keep their handles
- [ ] 2.5 Optionally summarise dropped buckets via `next`; verify the exact drop holds when the hook answers without calling `next`
- [ ] 2.6 Fail-open: missing selection, ctxed error, timeout, or protocol mismatch → call `next(e)` unchanged and report visibly; never block compaction
- [ ] 2.7 `claude plugin test` cases: drop by handle, kept handles intact, fail-open paths, `agentId` present (subagent) → no-op unless designed otherwise
- [ ] 2.8 Wire-level functional scenario (from `add-wire-level-verification`): after a selection and compaction, no request contains a dropped sentinel and every kept sentinel survives
- [ ] 2.9 Document the mod; the skill and README describe the exact path, with `/compact` steering kept as the fallback when the mod is not installed

## 3. Acceptance

- [x] 3.1 Verify `openspec validate add-claude-code-live-prune --strict` passes and both delta specs match the implemented D3a behavior
- [x] 3.2 Verify `go test ./...` is green and `go vet ./...` is clean
- [ ] 3.3 (D3b) End-to-end: in a live Claude Code session, drop a selected bucket via the mod and confirm the stored transcript is rewritten in-session and the session continues without relaunch
