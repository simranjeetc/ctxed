# context-dispatch-prune Specification

## Purpose
Emits a pruned transcript for the outbound request — the same session, minus the
chosen entries — without modifying the session's stored history, so a harness
can apply the prune at dispatch.

## Requirements

### Requirement: Pruning is non-destructive

Applying a prune SHALL NOT modify the stored session or its file. The prune
applies only to the transcript emitted for the request.

#### Scenario: Stored session is untouched

- **WHEN** a prune set is applied
- **THEN** the stored session is byte-for-byte unchanged

#### Scenario: Same session identity

- **WHEN** a prune is applied
- **THEN** the session id is unchanged; no new session is created

### Requirement: Prune set input

ctxed SHALL accept a prune set as a category selection or as explicit stable
entry ids, so a person can choose at the level they were shown.

#### Scenario: Selection by category

- **WHEN** a category is selected for pruning
- **THEN** every entry in that category is excluded from the emitted transcript

#### Scenario: Selection by id

- **WHEN** specific entry ids are selected
- **THEN** those entries are excluded and all others retained

### Requirement: Emitted transcript is structurally valid

The emitted transcript SHALL NOT contain an entry that depends on an excluded
entry — specifically, a tool result whose tool call was excluded. ctxed SHALL
resolve such cases (by also excluding the dependent entry, or retaining the pair)
and SHALL report what it did.

#### Scenario: Orphaned result is resolved

- **WHEN** a prune set would leave a tool result whose tool call is excluded
- **THEN** ctxed excludes the dependent result as well and reports the adjustment

#### Scenario: Resolution is reported

- **WHEN** ctxed adjusts a prune set for validity
- **THEN** the adjustment is named in the output

### Requirement: Machine-consumable dispatch output

ctxed SHALL emit the pruned transcript on stdout and diagnostics on stderr, with
stable exit codes, so a harness plugin can call it and substitute the result.

#### Scenario: Transcript on stdout

- **WHEN** a prune is emitted for a harness plugin
- **THEN** stdout is the pruned transcript and stderr carries only diagnostics

#### Scenario: Deterministic given the prune set

- **WHEN** the same session and prune set are emitted twice
- **THEN** the emitted transcript is identical

### Requirement: Core logic stays in ctxed

Categorization, selection, and validity handling SHALL live in ctxed, and the
harness integration SHALL be a thin call documented in a plugin contract, so a
new harness needs only a minimal adapter.

#### Scenario: A new harness needs only a thin caller

- **WHEN** a new harness plugin is written
- **THEN** it only invokes ctxed and substitutes the returned transcript, carrying no categorization or prune logic of its own

#### Scenario: Contract is documented

- **WHEN** the plugin contract is documented
- **THEN** it states the exact inputs and outputs a plugin provides and consumes
