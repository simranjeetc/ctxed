# Spec Delta

## ADDED Requirements

### Requirement: In-session categorize → select → apply

The plugin SHALL expose one in-session entry point that runs the whole prune
workflow: it categorizes the **live** conversation into labeled buckets, lets
the user select buckets to drop, and records that selection for the dispatch
hook to apply. Categorization SHALL be part of the workflow, not a prerequisite
the user runs in a separate tool on a stale export.

#### Scenario: Categorizing from inside the session

- **WHEN** the user invokes the plugin's ctxed command in a running session
- **THEN** the plugin categorizes the live messages, not a previously exported file
- **AND** presents the resulting buckets with their labels for selection

#### Scenario: Selecting buckets, not ids

- **WHEN** the user reviews the presented buckets
- **THEN** they select whole buckets to drop
- **AND** they never select or type individual message ids

#### Scenario: Selection is recorded, not applied immediately

- **WHEN** the user confirms a selection
- **THEN** the plugin records it as the session's active selection
- **AND** the dispatch hook applies it to subsequent outbound requests

### Requirement: Selection covers messages added after it was made

The plugin SHALL apply the recorded bucket selection to every dispatch,
including messages that did not exist when the selection was made, so a
long-running session does not drift out of the selection.

#### Scenario: New message falls in an already-dropped bucket

- **WHEN** a message is added after the selection was recorded
- **AND** that message belongs to a bucket the user chose to drop
- **THEN** it is absent from the outbound transcript for later dispatches

#### Scenario: New message falls in a kept bucket

- **WHEN** a message is added after the selection was recorded
- **AND** it belongs to a bucket the user kept
- **THEN** it remains in the outbound transcript

### Requirement: Prune set comes from ctxed

The plugin SHALL obtain the dropped id set and the categorization by invoking
ctxed, and SHALL NOT implement categorization, bucket assignment, selection
resolution, or orphan handling itself.

#### Scenario: Delegates to ctxed

- **WHEN** the plugin needs the buckets or the dropped ids
- **THEN** it invokes ctxed, passing the live transcript through stdin or a
  temp file, and consumes ctxed's output

#### Scenario: No policy in the plugin

- **WHEN** the plugin's source is reviewed
- **THEN** it contains no categorization, bucket-assignment, category-selection,
  or tool-call/result validity logic

### Requirement: Live transcript parity

The ids ctxed sees when categorizing the live conversation SHALL be the same ids
the dispatch hook filters on, so a selection made in one dispatch applies to the
next.

#### Scenario: Selection made against live messages applies at dispatch

- **WHEN** the user selects a bucket from the live conversation
- **THEN** the ids of that bucket's messages are exactly the ids the dispatch
  hook removes on the following dispatch

## ADDED Requirements

### Requirement: Id-only prune output

ctxed prune SHALL support an id-only output mode that prints the resolved set of
dropped entry ids — after orphan resolution — instead of a transcript, so a
dispatch plugin can filter the outbound messages by id without re-serialising a
transcript on every dispatch.

#### Scenario: Dropped ids are reported

- **WHEN** prune runs in id-only mode
- **THEN** stdout is a JSON object whose `droppedIds` array is exactly the resolved set of dropped ids

#### Scenario: Orphan resolution is reflected

- **WHEN** orphan resolution adds a dependent entry to the drop set
- **THEN** that entry's id is included in `droppedIds`

#### Scenario: Nothing dropped

- **WHEN** the selection resolves to no ids
- **THEN** `droppedIds` is an empty array

#### Scenario: Deterministic

- **WHEN** the same session and selection are queried twice in id-only mode
- **THEN** the output is identical
