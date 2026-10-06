# Proposal

## Why

The two harnesses offer the same idea through different experiences:

| | OpenCode | Claude Code |
|---|---|---|
| Trigger | `/ctxed-prune`, then reply `2` | Ask the agent; skill runs ctxed |
| Apply | Automatic, exact | User pastes `/compact <sentence>`, best-effort |
| Categorizer | Bundled OpenCode script | The **same OpenCode script** (`SKILL.md` step 2) |
| Install | Copy bundle to `.opencode/plugins/`; plugin guesses ctxed's path (`resolveCtxedPath`) | Copy skill; ctxed must be on PATH or built in the repo |

Problems:

- A Claude Code user must have OpenCode installed and entitled to categorize.
- Setup is manual and differs per harness; binary discovery is heuristic and can
  pick a stale `ctxed` (see `fix-verification-false-passes`).
- The verbs, wording, and confirmation differ, so docs and support double.
- The Claude skill finds the transcript with `${PWD//\//-}` + newest `.jsonl`,
  which picks the wrong session when two run in one project.

## What Changes

- **Same verbs in both harnesses.** `/ctxed-prune` lists buckets with sizes;
  replying with numbers (or `/ctxed-prune 2,3`) drops them; the session shows a
  one-line confirmation naming what was dropped. `/ctxed-prune clear` restores.
  In Claude Code this is delivered by the mod (from `add-claude-code-live-prune`
  D3b); the skill remains as a natural-language trigger that invokes the same
  flow.
- **Categorizer picks the host harness.** `ctxed categorize --categorizer auto`
  (the default when no model is configured) uses `claude -p --model haiku` under
  Claude Code and the OpenCode Go script under OpenCode, chosen by the shim's
  `--session-key` harness. Explicit `--model` / `--categorizer-cmd` still win.
- **One installer.** `ctxed install opencode [--project DIR | --global]` and
  `ctxed install claude [--project DIR | --user]` write the embedded
  (`go:embed`) plugin/mod/skill and record ctxed's absolute path in the shim's
  config. `ctxed uninstall …` reverses it. `ctxed doctor` reports what is
  installed, versions, protocol match, and categorizer availability.
- **Session identity, not guessing.** The Claude Code shim gets the session from
  the mod API (or the transcript path hooks receive), never from "newest
  `.jsonl`".
- **Shared conformance fixtures.** `testdata/conformance/` holds live-message
  inputs per harness and the expected bucket/drop results; both shims' tests and
  the Go adapter tests run against them.

## Capabilities

### New Capabilities

- `unified-harness-ux`: the same in-session verbs, output and guarantees in
  every supported harness.
- `harness-install`: one command installs, verifies and removes ctxed's
  integration for a harness.

## Impact

- `internal/cli` (`install`, `uninstall`, `doctor`, `--categorizer auto`),
  embedded assets, `plugin/opencode/` wording, the Claude Code mod and skill,
  README "Two live flows" table (collapses to one flow once D3b ships).
- Depends on `move-harness-formats-into-ctxed` and the D3b tasks in
  `add-claude-code-live-prune`.
