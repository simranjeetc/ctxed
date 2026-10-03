# context-inspection Specification

## Purpose
Shows a session's entries and their token cost read-only, so a human or an agent harness can decide which entries to keep or drop.

## Requirements

### Requirement: Entry listing

ctxed inspect SHALL print every entry in order with its index, role, kind, token count, and a first-line preview. It SHALL also print total entries and total tokens.

#### Scenario: All entries listed

- **WHEN** a session with N entries is inspected
- **THEN** the output contains exactly N entry rows, one per index

#### Scenario: Totals shown

- **WHEN** the listing is produced
- **THEN** the reported total tokens equal the sum of the per-entry token counts

### Requirement: Inspection is read-only

The inspect command SHALL NOT create, modify, or delete any file.

#### Scenario: No file written

- **WHEN** inspect completes
- **THEN** no new or changed file exists anywhere in the working tree

### Requirement: Token accounting

ctxed SHALL report per-entry and total token counts using the tokenizer of the configured model. When no model is configured, ctxed SHALL use a documented approximation and label the counts as approximate.

#### Scenario: Configured model tokenizer

- **WHEN** a model with a known tokenizer is configured
- **THEN** counts are computed with that tokenizer and not labelled approximate

#### Scenario: Approximate counts are labelled

- **WHEN** no model tokenizer is configured
- **THEN** counts are produced by the documented approximation and the output states they are approximate

### Requirement: Machine-readable inspection

ctxed inspect SHALL support a JSON output carrying the same fields as the table, keyed by entry index, for harness consumption.

#### Scenario: JSON inspection is parseable

- **WHEN** inspect runs with JSON output requested
- **THEN** stdout is valid JSON whose entries each carry an index, role, kind, token count, and preview
