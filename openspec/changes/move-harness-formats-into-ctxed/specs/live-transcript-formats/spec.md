# Spec Delta

## Purpose

ctxed reads the live message encodings each harness hands to its plugin, so no
shim translates formats.

## ADDED Requirements

### Requirement: Named live encodings

ctxed SHALL accept live message input on stdin in the encodings `opencode-hook`,
`opencode-context` and `claude-mod`, selected with `--from`, for `categorize`
and `prune`.

#### Scenario: OpenCode dispatch messages

- **WHEN** the OpenCode `context` hook's messages are piped to `ctxed prune - --from opencode-hook`
- **THEN** the dropped ids are the live message ids

#### Scenario: Claude Code mod messages

- **WHEN** a mod's `SessionMessage[]` is piped with `--from claude-mod`
- **THEN** each entry's id is the message's `handle`

#### Scenario: Unknown encoding

- **WHEN** `--from` names an unsupported encoding
- **THEN** ctxed exits with a usage error and writes nothing

### Requirement: Shared engine for every shim

A harness shim SHALL NOT translate message formats, decide bucket membership, or
resolve a selection; ctxed SHALL provide each of these.

#### Scenario: Plugin source contains no translation

- **WHEN** the OpenCode plugin source is checked by its no-policy test
- **THEN** it contains no transcript translation module

### Requirement: Versioned protocol

ctxed SHALL report an integer protocol version, and a shim SHALL refuse to run,
visibly, when its expected protocol differs.

#### Scenario: Version mismatch

- **WHEN** a shim built for protocol N loads against a ctxed reporting protocol M ≠ N
- **THEN** the shim shows a message in the session naming both versions and applies no prune
