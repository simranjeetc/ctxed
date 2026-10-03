# Spec Delta

## Purpose

Removes entries from a session and writes the reduced session, reporting the token change so a human or a harness can see what pruning did.

## ADDED Requirements

### Requirement: Drop entries by index

ctxed drop SHALL produce an edited session with exactly the named entries removed, preserving the order of the rest. ctxed SHALL reject unknown, out-of-range, or duplicate indices with a non-zero exit and no written file.

#### Scenario: Named entries removed

- **WHEN** drop is given a set of valid indices
- **THEN** exactly those entries are absent and every other entry is unchanged and in its original order

#### Scenario: Invalid index rejected

- **WHEN** drop is given an index that is not in the session
- **THEN** ctxed exits non-zero, reports the offending index, and writes no file

#### Scenario: Duplicate index rejected

- **WHEN** drop is given the same index more than once
- **THEN** ctxed exits non-zero and writes no file

### Requirement: Token delta reporting

Every write command SHALL report entries and tokens before and after the edit, and the net change.

#### Scenario: Drop reports the reduction

- **WHEN** drop completes
- **THEN** the report shows the entries and token total after the edit and the net change from before

#### Scenario: Net change is negative

- **WHEN** at least one entry is removed
- **THEN** the reported token total is lower than before and the net change reflects the removed entries

### Requirement: Non-interactive and harness-invocable

Write commands SHALL require no terminal interaction, SHALL emit a machine-readable JSON stats object on request, and SHALL use stable exit codes (0 on success, non-zero on failure) so a harness can call them programmatically.

#### Scenario: Runs unattended

- **WHEN** a write command runs with stdin closed and no terminal
- **THEN** it completes without prompting

#### Scenario: JSON stats emitted

- **WHEN** a write command runs with JSON output requested
- **THEN** stdout is valid JSON containing entries and tokens before and after, and the removed indices

### Requirement: Structural validity after pruning

ctxed SHALL detect entries that would be orphaned by an edit — such as a tool result whose tool call was removed — and SHALL refuse to write a session it determines is structurally invalid unless explicitly forced.

#### Scenario: Orphaning detected

- **WHEN** an edit would leave a tool result without its tool call
- **THEN** ctxed exits non-zero, names the orphaned entries, and writes no file

#### Scenario: Forced write allowed

- **WHEN** the user explicitly forces the write
- **THEN** the edited session is written and the detected orphaning is reported
