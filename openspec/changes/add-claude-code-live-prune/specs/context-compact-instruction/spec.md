# Spec Delta

## Purpose

Renders a category selection into a single Claude Code `/compact` instruction,
so a person can prune a live session at bucket granularity by pasting one
sentence — no relaunch, no file edit, no model call by ctxed.

## ADDED Requirements

### Requirement: Instruction from a category selection

ctxed SHALL render a `/compact` instruction from a categories file plus the
selected category ids that names the buckets to keep and the buckets to drop by
their labels. The selected ids are the buckets to drop; every other category is
kept.

#### Scenario: Kept and dropped buckets are named

- **WHEN** a selection is rendered
- **THEN** the instruction names each kept bucket and each dropped bucket by its label

#### Scenario: Buckets are the human's decision

- **WHEN** the instruction is rendered
- **THEN** it contains no message-level detail and no entry ids, only bucket labels

### Requirement: Instruction is offline and deterministic

Rendering SHALL read only the session and the categories file, SHALL call no
model, SHALL write no file, and SHALL be identical for the same inputs.

#### Scenario: No network or model call

- **WHEN** the instruction is rendered
- **THEN** no outbound request is made and the session and categories file are unchanged

#### Scenario: Deterministic

- **WHEN** the same session and selection are rendered twice
- **THEN** the instruction is byte-for-byte identical

### Requirement: Single machine-usable sentence on stdout

The instruction SHALL be emitted as one line on stdout with diagnostics on
stderr and stable exit codes, so a person can paste it after `/compact ` and a
harness could consume it.

#### Scenario: One line on stdout

- **WHEN** rendering succeeds
- **THEN** stdout is exactly one instruction line and stderr is empty

#### Scenario: Unknown category is refused

- **WHEN** a selected category id does not exist in the categories file
- **THEN** ctxed exits non-zero, names the category, and prints no instruction

#### Scenario: Missing selection is a usage error

- **WHEN** the categories file or the category selection is not provided
- **THEN** ctxed exits with the usage code and says which input is missing

### Requirement: Never rewrites or drops anything

ctxed SHALL NOT modify the session, the categories file, or the transcript, and
SHALL NOT drop any entry; the instruction only steers Claude Code's own
compaction.

#### Scenario: Non-destructive

- **WHEN** an instruction is rendered
- **THEN** the session file and categories file are byte-for-byte unchanged and no entry is removed
