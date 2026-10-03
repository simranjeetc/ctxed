# Tasks

## 1. Scaffold and CLI contract

- [x] 1.1 Create the Go module and `cmd/ctxed` entry point with `inspect` and `drop` subcommand dispatch; verify `go build ./...` succeeds, `ctxed` with no args prints usage, and an unknown subcommand exits with the usage exit code
- [x] 1.2 Implement the shared flag set and the output contract from design D5 (data/stats to stdout, diagnostics to stderr, exit codes 0/1/2/3); verify tests assert the stream split and each exit code
- [x] 1.3 Write the README with the positioning paragraph (ctxed vs OpenCode DCP: file-based, inspectable, human-overridable), the `inspect` and `drop` commands, and a pointer to `docs/future-enhancements.md` for compression; verify every command shown in the README runs as written

## 2. Canonical model and adapters

- [x] 2.1 Define the `Document`/`Entry` model, the `Detect`/`Parse`/`Write` adapter interface, and the adapter registry from design D1; verify unit tests cover registry lookup and that detection failure lists supported formats
- [x] 2.2 Capture fixtures into `testdata/`: the JSON from `opencode session export <id>` and a real `~/.claude/projects/**/*.jsonl` transcript, pinning each schema; verify both are committed and each is detected by exactly one adapter
- [x] 2.3 Implement the OpenCode adapter parse/write; verify a parse→write round-trip re-parses and that unknown top-level and entry fields survive unchanged
- [x] 2.4 Implement the Claude Code JSONL adapter parse/write; verify a round-trip re-parses and that non-event lines are preserved
- [x] 2.5 Verify the registry is the only source of supported formats by registering a fake adapter fixture and confirming no command option or behavior changes
- [x] 2.6 Document how to add an adapter in `docs/adapters.md`; verify the documented steps reproduce the fake-adapter test fixture
- [x] 2.7 Investigate the OpenCode return path and record the finding: `opencode session import` rejects an edited export of an existing session (`UNIQUE constraint failed: session_message.id`), so the edited document is a portable artifact, not an in-place write; document this in `docs/adapters.md` and note that returning a prune to a live session is handled by `add-category-prune`

## 3. Token accounting

- [x] 3.1 Implement the tokenizer interface with a known-encoding path and the documented approximation fallback, including the approximate label (design D4); verify tests cover both modes and that the label appears only in the fallback
- [x] 3.2 Document the approximation formula and the tokenizer override flag; verify the documented example count matches the implementation's output for a fixed input

## 4. Inspection

- [x] 4.1 Implement `inspect` table output with index, role, kind, token count, and first-line preview, plus totals; verify a fixture with N entries prints N rows and totals equal the per-entry sum
- [x] 4.2 Implement `inspect --json`; verify stdout parses and each entry carries index, role, kind, token count, and preview
- [x] 4.3 Verify inspect is read-only by snapshotting the working tree before and after; assert no file is created or changed
- [x] 4.4 Document inspect usage in the README; verify the documented command runs against the committed fixture

## 5. Pruning: drop

- [x] 5.1 Implement `drop --indices` with validation for unknown, out-of-range, and duplicate indices; verify each rejection exits non-zero and writes no file
- [x] 5.2 Implement the default derived output path and the `--out` override; verify the written file is correct and the input file is byte-for-byte unchanged
- [x] 5.3 Implement before/after entry and token reporting and the `--json` stats object for drop; verify the JSON contains the removed indices and both token totals
- [x] 5.4 Implement the structural validity check and `--force` from design D3; verify an orphaning edit is refused by default, names the orphaned entries, and that `--force` writes and reports the orphaning
- [x] 5.5 Document drop in the README including the refusal and force behavior; verify the documented commands reproduce the round-trip and the refusal

## 6. Integration and acceptance

- [x] 6.1 Run inspect then drop on both committed fixtures and verify each edited file re-parses under its own adapter, with reported tokens lower than the input
- [x] 6.2 Verify harness-invocability: run the write command with stdin closed and `--json`, asserting stable exit codes and that stdout is a single valid stats object while diagnostics stay on stderr
- [x] 6.3 Verify the change still validates with `openspec validate add-ctxed-cli` and that the three delta specs match the implemented behavior on the committed fixtures
