# Spec Delta

## ADDED Requirements

### Requirement: The plugin only filters

The plugin SHALL, before each request, read the session's drop list and remove
every message whose id is listed or that carries a listed tool-call id. It
SHALL NOT post messages, register commands, run binaries, or keep state.

#### Scenario: Listed messages are removed

- **WHEN** a request is about to be sent and the drop list names some of its messages
- **THEN** those messages are absent from the request and every other message is unchanged

#### Scenario: Listed ids missing from the request

- **WHEN** the drop list names an id the request does not contain
- **THEN** the id is ignored and the rest of the list still applies

#### Scenario: Fail-open

- **WHEN** the drop list is missing or cannot be read
- **THEN** the request is sent unchanged

#### Scenario: The model is never prompted by the plugin

- **WHEN** a prune runs
- **THEN** the plugin adds no message to the session

## MODIFIED Requirements

### Requirement: Selection covers only the messages it was made over

The plugin SHALL apply the session's drop list to every request. A message
added after a prune SHALL be kept, whatever its topic: returning to a dropped
topic brings it back, and the user prunes again to drop it.

#### Scenario: Selected messages stay dropped

- **WHEN** a prune is recorded and later requests happen
- **THEN** every message on the drop list is absent from each of them

#### Scenario: Ids dropped by an earlier dispatch do not break later ones

- **WHEN** an id on the drop list is not in a request
- **THEN** the plugin ignores it and still removes the listed messages that are present

#### Scenario: New message on a dropped topic

- **WHEN** a message is added after the prune
- **AND** it is about a topic the user chose to drop
- **THEN** it remains in the outbound request

#### Scenario: New message on a kept topic

- **WHEN** a message is added after the prune
- **AND** it is about a topic the user kept
- **THEN** it remains in the outbound request

### Requirement: Live transcript parity

The ids ctxed sorts from the session export SHALL be the ids the plugin sees in
the request, so a drop list made from the export applies to the request.

#### Scenario: Selection made against live messages applies at dispatch

- **WHEN** the user drops a category
- **THEN** the ids of that category's messages are exactly the ids the plugin removes from the next request

## REMOVED Requirements

### Requirement: In-session categorize → select → apply

**Reason**: The plugin's command posted notices the model answered, and its
listing included ids the request never carries.
**Migration**: Use the `ctxed-prune` skill, which runs `ctxed opencode categorize` and `ctxed opencode drop`.

### Requirement: Prune set comes from ctxed

**Reason**: The plugin no longer calls ctxed; ctxed writes the drop list ahead of time.
**Migration**: See "The plugin only filters".

### Requirement: Id-only prune output

**Reason**: The plugin no longer uses `prune --ids-only`; the requirement stays in `context-dispatch-prune`.
**Migration**: None needed.
