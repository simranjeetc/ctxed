# Spec Delta

## Purpose

Reaches a model for categorization through an OpenAI-compatible endpoint or a
caller-named command, so the same logic works against hosted, local, or
CLI-only providers and can be tested offline.

## ADDED Requirements

### Requirement: OpenAI-compatible endpoint

ctxed SHALL call a model through an OpenAI-compatible chat-completions endpoint,
with base URL, API key, and model read from flags or environment.

#### Scenario: Endpoint receives the prompt

- **WHEN** categorization runs with a configured endpoint and no command override
- **THEN** ctxed sends the prompt to that endpoint and uses the returned text

#### Scenario: Missing credentials fail cleanly

- **WHEN** no endpoint credentials are available and no command override is set
- **THEN** ctxed exits non-zero with a clear message and produces no categories

### Requirement: Command override

When a command override is configured, ctxed SHALL run that command, pass the
prompt on stdin, and use its stdout as the model response. The override SHALL
take precedence over the built-in endpoint.

#### Scenario: Command output is used

- **WHEN** a command override is configured and prints a response
- **THEN** that response is used and the built-in endpoint is not called

#### Scenario: Command failure aborts

- **WHEN** the command override exits non-zero
- **THEN** ctxed exits non-zero and writes no categories file

### Requirement: Deterministic offline path

With a command override, ctxed SHALL make no outbound network request, so a
fixed-output command yields a reproducible result.

#### Scenario: No network with a command override

- **WHEN** categorization runs with a command override
- **THEN** ctxed opens no network connection and the result is fully determined by the command's output

### Requirement: Input is bounded

ctxed SHALL send only the session material the operation needs and SHALL report
an error rather than silently truncating when that material exceeds a configured
limit.

#### Scenario: Oversized input errors

- **WHEN** the material to send exceeds the configured limit
- **THEN** ctxed exits non-zero, states the limit, and sends nothing
