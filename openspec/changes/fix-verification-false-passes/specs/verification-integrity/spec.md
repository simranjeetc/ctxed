# Spec Delta

## Purpose

Rules every verification check in `scripts/` must satisfy, so that a regression
can never be reported as a pass.

## ADDED Requirements

### Requirement: A missing feature fails

A verification check SHALL fail when the feature it guards is absent from the
checkout. It SHALL NOT skip, return success, or run against another worktree or
branch.

#### Scenario: Feature removed

- **WHEN** a shipped command or flag (e.g. `prune --ids-only`) is removed from the checkout
- **THEN** the check guarding it reports FAIL

#### Scenario: No external source tree

- **WHEN** a functional scenario runs
- **THEN** it builds and exercises only the checkout it was invoked from

### Requirement: Each check can fail

Every check SHALL assert a condition that the guarded regression would make
false. A check's evidence SHALL NOT be satisfiable by output produced for a
different check.

#### Scenario: Recall question isolated

- **WHEN** a check asks the model whether it can recall a dropped sentinel
- **THEN** it reads only the reply to that question, and the expected "unknown" token is unique to that question

#### Scenario: Negative control

- **WHEN** the drop checks run with no selection recorded
- **THEN** they report failure

### Requirement: Hard and soft checks are distinct

Checks whose outcome depends on model behavior SHALL be reported as soft and
SHALL NOT determine the report's `ok` value.

#### Scenario: Model-recall check

- **WHEN** a recall check fails but every hard check passes
- **THEN** the report has `ok: true` and lists the check with status `soft-fail`

### Requirement: Scenarios are isolated and portable

A functional scenario SHALL clean up every session and harness project it
creates, SHALL wait on observable state rather than fixed delays, and SHALL NOT
depend on machine-specific paths or credentials defaults.

#### Scenario: Claude Code scratch project

- **WHEN** the Claude Code scenario finishes without `--keep`
- **THEN** no project directory it created remains under `~/.claude/projects`

#### Scenario: Missing prerequisite

- **WHEN** a required tool or credential is absent
- **THEN** the script exits naming that prerequisite
