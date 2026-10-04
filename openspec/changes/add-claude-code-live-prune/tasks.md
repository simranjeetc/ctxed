# Tasks

## 1. ctxed `compact-instruction` (D3a)

- [x] 1.1 Add `internal/compact` rendering a single `/compact` instruction from a categories file plus the selected category ids: name the kept buckets (unselected) and the dropped buckets (selected) by label; reuse `categorize.Select` so an unknown id errors as in `prune`
- [x] 1.2 Unit-test the renderer: two of three buckets; a single bucket; the serial-comma list; all buckets dropped; an unknown id; a missing id among valid ones; determinism; empty-label fallback
- [x] 1.3 Wire `ctxed compact-instruction <session> --categories-file F --categories 1,3` into `internal/cli`, reading the session only to validate the categories file; usage/exit codes match the other commands
- [x] 1.4 CLI tests: labels of the kept/dropped buckets appear; output is one line; unknown category refused; missing `--categories` is a usage error; deterministic; the session file is unchanged; works with stdin closed
- [x] 1.5 Document the flow in the README (`categorize` → pick buckets → `compact-instruction` → `/compact <text>`) and in `docs/adapters.md` Claude Code notes

## 2. Claude Code compact mod (D3b — specified, not built)

- [ ] 2.1 Scaffold a Claude Code mod (early-access API, v2.1.287+) that registers the `session.compact` hook; verify it loads in a real session
- [ ] 2.2 Confirm against a real session that `$.session.compact()` installs its result live between turns and that the hook runs for the main conversation, not only subagents (decisions §6); record the answers
- [ ] 2.3 Classify the live message list into the same buckets ctxed produces (read the categories file), preserving message handles so engine-owned messages round-trip
- [ ] 2.4 Drop the selected buckets and return the kept list; a message returned with its handle is the engine's own, so a hand-built message is not required
- [ ] 2.5 Optionally summarise selected buckets via `next`; verify a deterministic drop still happens when the hook answers without calling `next`
- [ ] 2.6 Fail-open: on a missing or invalid categories file, leave the transcript unchanged and report it; never block compaction
- [ ] 2.7 Document the mod and its configuration; verify the documented flow drops a known bucket live in a real session with no relaunch

## 3. Acceptance

- [x] 3.1 Verify `openspec validate add-claude-code-live-prune --strict` passes and both delta specs match the implemented D3a behavior
- [x] 3.2 Verify `go test ./...` is green and `go vet ./...` is clean
- [ ] 3.3 (D3b) End-to-end: in a live Claude Code session, drop a selected bucket via the mod and confirm the stored transcript is rewritten in-session and the session continues without relaunch
