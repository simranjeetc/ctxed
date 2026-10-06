# context-session-format Specification

## Purpose
Reads session files from supported agent harnesses into one canonical, ordered entry list, and writes edited sessions back in the source harness's own schema so they can be used unchanged.

## Requirements

### Requirement: Session format detection

ctxed SHALL identify the source harness of a session file without the user naming it. If no registered adapter recognizes the file, ctxed SHALL exit non-zero, name the supported formats, and write nothing.

#### Scenario: OpenCode session recognized

- **WHEN** a valid OpenCode session file is inspected
- **THEN** the source harness is reported as `opencode`

#### Scenario: Claude Code transcript recognized

- **WHEN** a valid Claude Code transcript is inspected
- **THEN** the source harness is reported as `claude-code`

#### Scenario: Unknown format rejected

- **WHEN** a file matches no registered adapter
- **THEN** ctxed exits non-zero, lists the supported formats, and writes no output

### Requirement: Canonical entry model

A parsed session SHALL expose an ordered list of entries. Each entry SHALL carry a stable index, its role, its kind (message, tool-call, tool-result, reasoning, or summary), and its text content.

#### Scenario: Every entry is addressable

- **WHEN** a session is parsed
- **THEN** each entry has a unique index and can be referenced by that index in later operations

#### Scenario: Tool result retains its call

- **WHEN** a parsed session contains tool calls and tool results
- **THEN** each result entry can be associated with the call entry it answers

### Requirement: Pluggable adapters

New harness formats SHALL be addable as adapters without changing inspection or pruning behavior. The registered adapter set SHALL be the sole source of supported formats.

#### Scenario: Adding an adapter changes no command behavior

- **WHEN** a new adapter is registered
- **THEN** existing commands operate on it with no change to their behavior or options

#### Scenario: Supported formats come from the registry

- **WHEN** ctxed reports supported formats
- **THEN** the list equals the registered adapters, not a hard-coded one

### Requirement: Schema-preserving round-trip

When writing an edited session, ctxed SHALL preserve the source harness's file schema and all non-entry fields, changing only entry content targeted by the operation, so the written document remains a valid instance of that schema and the adapter can parse it back. Whether the harness's own import path accepts it is harness-specific and out of scope.

#### Scenario: Edited file re-parses

- **WHEN** a session is inspected, edited, and then parsed again
- **THEN** the edited file parses successfully under the same adapter

#### Scenario: Unrelated fields survive

- **WHEN** a session contains fields ctxed does not model
- **THEN** those fields are present and unchanged in the written output

### Requirement: Input file is never mutated

Write commands SHALL NOT modify the input file. They SHALL write a separate edited file whose path defaults to one derived from the input and can be set with an override.

#### Scenario: Input bytes unchanged

- **WHEN** a drop command completes
- **THEN** the input file is byte-for-byte identical to before the command

#### Scenario: Output path is controllable

- **WHEN** an output path override is supplied
- **THEN** the edited session is written there and nowhere else

### Requirement: Compacted Claude Code transcripts expose the live context

When a Claude Code transcript contains one or more compaction boundaries, the
parsed entry list SHALL contain only the live context: the compaction summary as
a `summary` entry, then the entries the last boundary lists as preserved
(`compactMetadata.preservedMessages.uuids`), then the entries after the last
boundary. Writing the session SHALL preserve
every line, including those before the boundary.

#### Scenario: Pre-compaction entries hidden

- **WHEN** a transcript with a `compact_boundary` line is inspected
- **THEN** only the summary, the preserved entries and the entries after the last boundary are listed, and the token total counts only those

#### Scenario: Preserved messages stay live

- **WHEN** the last boundary lists pre-boundary entries in `preservedMessages.uuids`
- **THEN** those entries are listed right after the summary, and other pre-boundary entries are not

#### Scenario: Categorize sees the live context

- **WHEN** a compacted transcript is categorized
- **THEN** no category references an entry from before the last boundary

#### Scenario: Round-trip keeps history

- **WHEN** an entry is dropped from a compacted transcript
- **THEN** every line before the boundary is written unchanged
