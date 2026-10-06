# Spec Delta

## Purpose

Every supported harness offers the same in-session prune experience.

## ADDED Requirements

### Requirement: Same verbs in every harness

Each harness integration SHALL offer `/ctxed-prune` to list buckets, a numeric
reply or `/ctxed-prune <ids>` to drop buckets, and `/ctxed-prune clear` to
restore them, with identical wording rendered by ctxed.

#### Scenario: Listing

- **WHEN** the user runs `/ctxed-prune` in either harness
- **THEN** the session shows the same bucket table format with labels, entry counts and token sizes

#### Scenario: Numeric reply

- **WHEN** the user replies `2` after a listing
- **THEN** bucket 2 is dropped and the session shows a one-line confirmation naming it

#### Scenario: Clear

- **WHEN** the user runs `/ctxed-prune clear`
- **THEN** no bucket is dropped on the next turn and the session confirms it

### Requirement: Categorize with the host harness

When no model is configured, categorization SHALL use a model reachable through
the harness the session runs in, and SHALL NOT require a different harness to be
installed.

#### Scenario: Claude Code without OpenCode

- **WHEN** a Claude Code user without OpenCode runs `/ctxed-prune`
- **THEN** buckets are produced using Claude Code's own CLI

### Requirement: Guarantee is stated

The confirmation SHALL state when a drop is best-effort rather than exact.

#### Scenario: Best-effort path

- **WHEN** a drop is applied through `/compact` steering
- **THEN** the confirmation says it is best-effort

### Requirement: Session identity is explicit

An integration SHALL identify the session from the harness API, never by
choosing the newest transcript file.

#### Scenario: Two sessions in one project

- **WHEN** two Claude Code sessions run in the same directory and one runs `/ctxed-prune`
- **THEN** only that session's messages are categorized
