# Design

## The one flow

```text
/ctxed-prune
  ctxed: categorizing… (shown immediately)
  1  Adapter work          38 entries  96k tokens
  2  Codex investigation   12 entries  28k tokens
  Reply with the bucket numbers to drop (e.g. 2), or ignore.
2
  ctxed dropped "Codex investigation" (12 entries, ~28k tokens). Continue.
/ctxed-prune clear
  ctxed restored all buckets.
```

Wording lives in ctxed (`ctxed render buckets|confirmation`, or in the `serve`
responses), so both shims print identical text.

Guarantee line: under Claude Code before D3b ships, the confirmation says
"steered via /compact (best-effort)". After D3b, both say nothing extra: the drop
is exact.

## Decisions

### Categorizer auto-detection

`--categorizer auto` resolves in order: explicit flags/env → the harness from
`--session-key` → available CLI on PATH (`claude`, then `opencode`). For Claude
Code: `claude -p --model haiku --output-format text` with the prompt on stdin;
confirm this works without tools and within the bounded prompt
(`context-model-transport` "Input is bounded"). Ship it as an embedded script so
`--categorizer-cmd` semantics are unchanged.

### Installer

- Assets are embedded with `go:embed`: the OpenCode bundle (built by
  `scripts/verify.sh` bundle step), the Claude Code mod directory, the skill.
- The installer writes ctxed's absolute path (`os.Executable()`) into a small
  config file next to the shim; the shim reads it first and falls back to PATH
  discovery only if absent. This removes reliance on `resolveCtxedPath`'s fixed
  list.
- Idempotent; refuses to overwrite a file it did not write unless `--force`;
  prints every path it touched.
- `doctor` exit code is non-zero when anything required is missing.

### Session identity in Claude Code

The mod knows the session it runs in; it passes `claude/<session-id>` as the
session key. The skill, when used without the mod, asks the mod/hook for the
transcript path instead of mapping `$PWD`. Verify which identifier the mod API
exposes (`$.session` fields) before relying on it.

## Open Questions

- Can a Claude Code mod register a slash command (`/ctxed-prune`), or must the
  entry stay a skill that calls into the mod? Check the installed
  `.claude-plugin/types/` for a command registration API. If none: the skill is
  the entry point and the mod does the drop; keep the same wording.
- Global vs project install defaults for each harness.

## Non-Goals

- Automatic pruning or budgets.
- Supporting a third harness (but the conformance fixtures make it cheap).
