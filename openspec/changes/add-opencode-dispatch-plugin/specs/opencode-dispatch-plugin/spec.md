# Spec Delta

## Purpose

Applies a ctxed prune to an OpenCode session's outbound transcript at dispatch —
non-destructively and fail-open — so the same session continues with a slimmer
context and the harness carries no categorization or prune logic itself.

## ADDED Requirements

### Requirement: Dispatch hook binding

The plugin SHALL register on OpenCode's context hook and run on each dispatch,
so every outbound request is considered for pruning.

#### Scenario: Runs on dispatch

- **WHEN** OpenCode assembles an outbound request
- **THEN** the plugin's context hook runs for that dispatch

### Requirement: Prune set comes from ctxed

The plugin SHALL obtain the dropped id set by invoking ctxed and SHALL NOT
implement categorization, selection, or orphan handling itself.

#### Scenario: Delegates to ctxed

- **WHEN** the plugin needs the dropped ids
- **THEN** it invokes ctxed in id-only prune mode with the configured session source and selection

#### Scenario: No policy in the plugin

- **WHEN** the plugin's source is reviewed
- **THEN** it contains no categorization, category-selection, or tool-call/result validity logic

### Requirement: Outbound messages are filtered by id

The plugin SHALL remove only the messages whose id is in the dropped set,
preserving the order and content of every other message.

#### Scenario: Dropped messages removed

- **WHEN** the dropped set contains a message id
- **THEN** that message is absent from the outbound transcript for that dispatch

#### Scenario: Other messages untouched

- **WHEN** a message's id is not in the dropped set
- **THEN** it remains, in its original order, unchanged

### Requirement: Non-destructive

The plugin SHALL NOT modify stored session history, and the session id SHALL be
unchanged.

#### Scenario: Stored history untouched

- **WHEN** the plugin prunes a dispatch
- **THEN** the stored session is unmodified

### Requirement: Fail-open

On a ctxed error, timeout, or unparseable output, the plugin SHALL leave the
outbound transcript unchanged and report the error; it SHALL NOT block or fail
the turn.

#### Scenario: ctxed failure passes the turn through

- **WHEN** the ctxed invocation fails or times out
- **THEN** the outbound transcript is sent unchanged and the error is reported

#### Scenario: Malformed output passes the turn through

- **WHEN** ctxed's output cannot be parsed
- **THEN** the outbound transcript is sent unchanged

### Requirement: Bounded per-dispatch cost

The plugin SHALL avoid re-invoking ctxed on every dispatch when the inputs are
unchanged, and SHALL refresh when the selection or the session revision changes.

#### Scenario: Repeated dispatches reuse the result

- **WHEN** two dispatches occur with an unchanged selection and session revision
- **THEN** ctxed is not re-invoked for the second dispatch

#### Scenario: Change refreshes the result

- **WHEN** the selection or the session revision changes
- **THEN** the plugin re-invokes ctxed
