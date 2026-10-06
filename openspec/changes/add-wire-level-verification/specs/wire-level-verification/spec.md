# Spec Delta

## Purpose

Functional verification proves a drop by inspecting the request the harness
actually sends to the model, not the plugin's own report or the model's recall.

## ADDED Requirements

### Requirement: Outbound requests are recorded

The functional suite SHALL route each harness's model traffic to a local
recording provider and SHALL record every request body it receives.

#### Scenario: OpenCode dispatch recorded

- **WHEN** an OpenCode scenario sends a prompt
- **THEN** the request OpenCode sent for that turn is in the provider log

#### Scenario: Claude Code compaction recorded

- **WHEN** the Claude Code scenario runs `/compact <instruction>`
- **THEN** the compaction request is in the provider log

### Requirement: Drops are asserted on the wire

A hard drop check SHALL assert on recorded request bodies: every sentinel of a
dropped bucket is absent from requests after the selection, and every sentinel
of a kept bucket is present.

#### Scenario: Exact drop on OpenCode

- **WHEN** a bucket is selected and the next turn is dispatched
- **THEN** that request contains no sentinel of the dropped bucket and every sentinel of the kept buckets

#### Scenario: Filter disabled

- **WHEN** the plugin's filter is disabled
- **THEN** the exact-drop check fails

### Requirement: Default run needs no model

The default functional run SHALL complete without model entitlement or network
access beyond localhost. A real-model run SHALL be opt-in and its model-quality
results SHALL be soft.

#### Scenario: Offline run

- **WHEN** the suite runs without `--with-model`
- **THEN** every hard check is evaluated against the recording provider

#### Scenario: Steering metric

- **WHEN** the Claude Code scenario runs with `--with-model`
- **THEN** whether a dropped sentinel survives in the summary is reported as a soft check
