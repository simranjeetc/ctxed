# Spec Delta

## ADDED Requirements

### Requirement: Id-only prune output

ctxed prune SHALL support an id-only output mode that prints the resolved set of
dropped entry ids — after orphan resolution — instead of a transcript, so a
dispatch plugin can filter the outbound messages by id without re-serialising a
transcript on every dispatch.

#### Scenario: Dropped ids are reported

- **WHEN** prune runs in id-only mode
- **THEN** stdout is a JSON object whose `droppedIds` array is exactly the resolved set of dropped ids

#### Scenario: Dropped tool-call ids are reported

- **WHEN** prune runs in id-only mode
- **AND** a dropped entry issued or answered a tool call
- **THEN** stdout's `droppedToolCallIds` array includes those tool-call ids
- **AND** a plugin can drop a live message that carries such an id even when the
  message has no id of its own, so a dropped tool result is not left behind to
  accumulate across dispatches

#### Scenario: Orphan resolution is reflected

- **WHEN** orphan resolution adds a dependent entry to the drop set
- **THEN** that entry's id is included in `droppedIds`

#### Scenario: Nothing dropped

- **WHEN** the selection resolves to no ids
- **THEN** `droppedIds` is an empty array

#### Scenario: Deterministic

- **WHEN** the same session and selection are queried twice in id-only mode
- **THEN** the output is identical
