# Spec Delta

## Purpose

One command installs, checks and removes ctxed's integration for a harness.

## ADDED Requirements

### Requirement: Install from the binary

`ctxed install <harness>` SHALL write the integration files embedded in the
ctxed binary and record ctxed's absolute path for the integration to use.

#### Scenario: Fresh install

- **WHEN** `ctxed install opencode --project DIR` runs
- **THEN** the plugin is in `DIR/.opencode/plugins/`, it uses the installing ctxed binary, and every written path is printed

#### Scenario: Idempotent

- **WHEN** the same install runs twice
- **THEN** the second run changes nothing and exits 0

#### Scenario: Foreign file

- **WHEN** a target file exists that ctxed did not write
- **THEN** install refuses without `--force` and names the file

### Requirement: Doctor and uninstall

ctxed SHALL provide `doctor`, reporting installed integrations, versions,
protocol compatibility and categorizer availability, and `uninstall`, removing
only files ctxed wrote.

#### Scenario: Protocol mismatch reported

- **WHEN** an installed shim expects a different protocol than the binary
- **THEN** `ctxed doctor` exits non-zero and names both versions

#### Scenario: Clean uninstall

- **WHEN** `ctxed uninstall claude --project DIR` runs after an install
- **THEN** no file written by the install remains
