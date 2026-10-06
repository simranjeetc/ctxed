# Spec Delta

## ADDED Requirements

### Requirement: Session found from the environment

`ctxed opencode categorize` and `ctxed opencode drop` SHALL take the session id
from `--session`, or else from `$OPENCODE_SESSION_ID`, and SHALL refuse an id
that is not `ses_` followed by letters and digits.

#### Scenario: Run from inside a session

- **WHEN** the command runs from OpenCode's shell tool with no `--session`
- **THEN** it acts on the session in `$OPENCODE_SESSION_ID`

#### Scenario: Unsafe id

- **WHEN** the session id contains a path separator or does not start with `ses_`
- **THEN** the command exits 2 and touches no file

### Requirement: Only what the model still sees is sorted

`ctxed opencode categorize` SHALL sort only entries after the session's last
compaction, and SHALL leave out every entry already on the session's drop list.

#### Scenario: Second prune

- **WHEN** a prune has dropped topic A
- **AND** the user prunes again
- **THEN** no listed category contains an entry from the first prune
- **AND** a category about topic A contains only entries added after the first prune

#### Scenario: After a compaction

- **WHEN** the session was compacted
- **THEN** no listed category contains an entry from before the compaction

#### Scenario: Nothing left to sort

- **WHEN** fewer than two entries remain
- **THEN** the command says so and exits 0 without calling a model

### Requirement: The drop list only grows

`ctxed opencode drop` SHALL add the chosen categories' entry ids and tool-call
ids to the session's drop list, and SHALL never remove an id from it.

#### Scenario: Picks add up

- **WHEN** the user drops categories in two separate prunes
- **THEN** the drop list holds the entries of both

#### Scenario: Unknown number

- **WHEN** a chosen number names no category in the last listing
- **THEN** the command exits 3 and the drop list is unchanged

### Requirement: The skill asks, ctxed decides

The `ctxed-prune` skill SHALL run `ctxed opencode categorize`, offer the
categories to the user as a multi-select, and pass the chosen numbers to
`ctxed opencode drop`. It SHALL NOT choose categories on the user's behalf.

#### Scenario: Multi-select

- **WHEN** the skill presents the categories
- **THEN** the user can choose several at once, or none

#### Scenario: The user chooses nothing

- **WHEN** the user picks no category
- **THEN** the skill runs no drop and the drop list is unchanged

### Requirement: Verified by codeword

The functional suite SHALL check the OpenCode prune with code words: after a
prune, the model asked in the same session names the kept topic's word and not
a dropped topic's word. It SHALL do so for a first prune, a second prune, and
after an OpenCode compaction.

#### Scenario: Two prunes and a compaction

- **WHEN** the suite runs `--opencode`
- **THEN** each codeword check is a hard check that decides the exit code
