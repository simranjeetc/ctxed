# Spec Delta

## ADDED Requirements

### Requirement: One overview for both harnesses

`ctxed overview` SHALL report a Claude Code or OpenCode session's live context in
the same format, taking the session from a file argument, else `--session`, else
`CLAUDE_CODE_SESSION_ID` or `OPENCODE_SESSION_ID`.

#### Scenario: Run inside Claude Code

- **WHEN** the command runs from Claude Code's shell tool with no arguments
- **THEN** it reports the transcript `<CLAUDE_CODE_SESSION_ID>.jsonl` under Claude Code's projects directory

#### Scenario: Run inside OpenCode

- **WHEN** the command runs from OpenCode's shell tool with no arguments
- **THEN** it exports and reports the session named by `OPENCODE_SESSION_ID`

#### Scenario: No session

- **WHEN** no file, `--session` or session variable is given
- **THEN** the command exits 2 and names the ways to pass a session

#### Scenario: Both session variables set

- **WHEN** both `CLAUDE_CODE_SESSION_ID` and `OPENCODE_SESSION_ID` are set and no session is passed
- **THEN** the command exits 2 and asks for `--session`

### Requirement: Only the live context is counted

The overview SHALL count only what the model sees now: entries after the last
compaction, plus the compaction record itself as a separate row. It SHALL state
how many earlier entries were not counted.

#### Scenario: Compacted session

- **WHEN** a session has been compacted
- **THEN** no topic row contains an entry from before the last compaction
- **AND** the compaction record appears as its own row

### Requirement: Topics with size and status

The overview SHALL list topics with message count, estimated tokens, share of
the total and a status of `done`, `in progress` or `?`, followed by the pending
items the categorizer returned. Row totals SHALL equal the header totals and
`ctxed inspect` over the same live view.

#### Scenario: Totals agree

- **WHEN** the overview and `ctxed inspect` run on the same session
- **THEN** the overview's total messages and tokens equal inspect's live totals

#### Scenario: Status from a real model

- **WHEN** one planted topic is finished and another ends with an open "TODO: X"
- **THEN** the first is `done`, the second `in progress`, and X is listed as pending

### Requirement: Read-only

The overview SHALL NOT change the session file, the OpenCode store or any harness
configuration.

#### Scenario: Session unchanged

- **WHEN** the overview runs on a session
- **THEN** the session's bytes (or export) are identical before and after

### Requirement: Pruning is not reachable

No CLI command, shipped skill or installed plugin SHALL invoke the parked pruning
code.

#### Scenario: Old commands

- **WHEN** a user runs `ctxed prune`, `ctxed compact-instruction`, `ctxed drop` or `ctxed opencode`
- **THEN** ctxed reports an unknown command and exits 2
