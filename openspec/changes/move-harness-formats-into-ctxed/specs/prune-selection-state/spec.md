# Spec Delta

## Purpose

ctxed owns the categories and the active selection for each harness session.

## ADDED Requirements

### Requirement: Per-session selection state

ctxed SHALL persist, per harness and session id, the last categories document
and the selected category ids, under the user's state directory, and SHALL
never write to a harness's own store.

#### Scenario: Selection recorded

- **WHEN** `ctxed select opencode ses_1 --categories 2` runs after a categorize for that session
- **THEN** a later `ctxed prune - --session-key opencode/ses_1` drops bucket 2's entries

#### Scenario: No selection

- **WHEN** `prune --session-key` runs for a session with no selection
- **THEN** the drop set is empty and ctxed exits 0

#### Scenario: Unknown bucket

- **WHEN** `select` names a category id not in the stored categories
- **THEN** ctxed exits 3 and the stored selection is unchanged

### Requirement: Atomic and inspectable

State writes SHALL be atomic, and the stored selection SHALL be readable through
`ctxed select … --show`.

#### Scenario: Inspect selection

- **WHEN** `ctxed select opencode ses_1 --show` runs
- **THEN** it prints the selected ids and their labels as JSON
