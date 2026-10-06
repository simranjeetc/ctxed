# Spec Delta

## ADDED Requirements

### Requirement: Compacted Claude Code transcripts expose the live context

When a Claude Code transcript contains one or more compaction boundaries, the
parsed entry list SHALL contain only the entries after the last boundary, with
the compaction summary as a `summary` entry. Writing the session SHALL preserve
every line, including those before the boundary.

#### Scenario: Pre-compaction entries hidden

- **WHEN** a transcript with a `compact_boundary` line is inspected
- **THEN** only the summary and the entries after the last boundary are listed, and the token total counts only those

#### Scenario: Categorize sees the live context

- **WHEN** a compacted transcript is categorized
- **THEN** no category references an entry from before the last boundary

#### Scenario: Round-trip keeps history

- **WHEN** an entry is dropped from a compacted transcript
- **THEN** every line before the boundary is written unchanged

#### Scenario: Full-history audit

- **WHEN** `inspect --include-compacted` is used
- **THEN** all entries are listed and those before the last boundary are marked
