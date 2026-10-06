# Spec Delta

## Purpose

Prunes a live Claude Code session at category-bucket granularity, in the
session, with no relaunch and no hand-edited files, by using compaction — the
only Claude Code surface that replaces messages while the session runs.

## ADDED Requirements

### Requirement: Category-level, in-session prune

A prune SHALL operate on human-chosen category buckets, not individual
messages, and SHALL land in the running session so the person continues in it
without relaunching.

#### Scenario: The human chooses buckets

- **WHEN** a live session is pruned
- **THEN** the human selects whole buckets to drop and never maps a message to a bucket

#### Scenario: No relaunch, no file edit

- **WHEN** a prune is applied
- **THEN** the session continues in place, with no `--resume` and no hand-edited transcript file

### Requirement: Instruction-mediated path (D3a)

ctxed SHALL provide the instruction text for Claude Code's own `/compact` from a
category selection, and the person SHALL apply it in the session.

#### Scenario: Instruction is applied in the session

- **WHEN** a selection is rendered
- **THEN** the person pastes the instruction after `/compact ` in the running session

#### Scenario: History rewrite is acceptable

- **WHEN** Claude Code applies the instruction
- **THEN** compaction may replace history with a summary and the kept messages; this is permitted

### Requirement: Live mechanisms only

The live prune SHALL NOT use a mechanism that requires a relaunch — copying the
transcript and resuming, or rewriting the `.jsonl` in place — because such a
mechanism violates the in-session requirement.

#### Scenario: Relaunch-based mechanisms are not the live path

- **WHEN** a live prune is applied
- **THEN** neither a transcript copy plus `--resume` nor an in-place `.jsonl` rewrite is used
