# Tasks

## 1. ctxed

- [x] 1.1 `internal/ocprune`: state file (load, atomic save, session-id validation, default dir), live view (after last compaction, minus dropped), cumulative drop
- [x] 1.2 Built-in OpenCode categorizer transport (`opencode run --format json`), and session export to a temp file
- [x] 1.3 `ctxed opencode categorize` and `ctxed opencode drop` in `internal/cli`; usage text
- [x] 1.4 Unit and CLI tests: live view across a compaction, already-dropped ids excluded, drop list grows, bad numbers refused, bad session id refused, nothing to sort

## 2. Plugin

- [x] 2.1 Reduce the plugin to the context hook: read the drop list, filter by id and tool-call id, fail open; keep the opt-in debug log
- [x] 2.2 Remove the command, prompt hook, synthetic messages, ctxed runner and transcript translation; update tests and README

## 3. Skill

- [x] 3.1 `plugin/opencode/skill/ctxed-prune/SKILL.md`: categorize, multi-select via `question`, drop, report
- [x] 3.2 Install globally (`~/.config/opencode/skills/`, plugin in `~/.config/opencode/plugins/`)

## 4. Verification

- [x] 4.1 Rewrite the OpenCode scenario around `ctxed opencode categorize/drop`; keep the exact-drop, negative-control, attachment and tool-result checks
- [x] 4.2 Prune twice: the second listing has no dropped id; the drop list keeps the first prune's ids; codeword checks for both prunes
- [x] 4.3 Across an OpenCode compaction: the listing has no pre-compaction id; dropped code words stay unknown
- [x] 4.4 Remove the self-config scenario; `go test ./...`, plugin tests, `verify.sh` and `verify-functionally.sh --opencode` pass
