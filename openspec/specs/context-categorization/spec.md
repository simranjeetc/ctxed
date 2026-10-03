# context-categorization Specification

## Purpose
Groups a session's entries into a small set of labeled, high-level categories so
a person can understand what a long session is about and choose a whole category
to drop without reading it entry by entry.

## Requirements

### Requirement: High-level categorization

ctxed SHALL group a session's entries into a small number of categories (at
least two and no more than a configured maximum, five by default), so the result
is a legible overview rather than a re-listing of every entry.

#### Scenario: Entries are grouped

- **WHEN** a session is categorized
- **THEN** every entry is assigned to exactly one category, or to an explicit uncategorized group

#### Scenario: Category count is bounded

- **WHEN** the model proposes more categories than the configured maximum
- **THEN** ctxed reports the bound and does not emit a result exceeding it

### Requirement: Each category is labeled and costed

Each category SHALL carry a short human-readable label and the total token count
of its entries, so a person can judge what dropping it saves.

#### Scenario: Overview shows cost

- **WHEN** categories are presented
- **THEN** each shows a label, its entry count, and its total tokens

### Requirement: Categories reference stable entry ids

A category's membership SHALL be expressed as the harness's stable entry ids
(such as an OpenCode message id or a Claude `uuid`), not positional indices, so
a prune decision remains valid when applied to the outbound transcript.

#### Scenario: Membership uses ids

- **WHEN** a category is written out
- **THEN** its members are identified by stable ids, not indices

#### Scenario: Entries without a native id are still addressable

- **WHEN** an entry has no harness-provided id
- **THEN** ctxed assigns a stable fallback id and uses it consistently

### Requirement: Categories are published as an editable file

ctxed SHALL write the categories to a machine-readable file that a person can
edit — renaming labels, moving an entry, or marking entries to keep — and SHALL
accept that edited file as input on a later invocation.

#### Scenario: Proposals can be adjusted

- **WHEN** a person edits the categories file and ctxed reads it back
- **THEN** the edited labels and membership are honored

#### Scenario: Invalid edits are rejected

- **WHEN** the file references an id that is not in the session
- **THEN** ctxed exits non-zero and names the offending id

### Requirement: Categorization is a proposal, never an automatic action

ctxed SHALL NOT drop anything as part of categorization. Dropping SHALL happen
only from an explicit selection in a separate invocation.

#### Scenario: Categorize never prunes

- **WHEN** `categorize` completes
- **THEN** the session and its stored history are unchanged and no entries are removed
