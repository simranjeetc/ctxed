# Proposal

## Why

The plugin contract (`docs/plugin-contract.md`) says a plugin should only "call
ctxed and substitute", and that missing behavior "belongs in ctxed, so it stays
shared across harnesses". The OpenCode plugin has outgrown that:

- **Format translation lives in TypeScript.** `plugin/opencode/src/transcript.ts`
  converts two live OpenCode encodings (the `context` hook shape and the
  `session.context` shape) into ctxed's export document. That logic cannot be
  reused by the Claude Code mod, and it is tested only in Node.
- **Selection state lives in the plugin.** The categories file is written to
  `$TMPDIR/ctxed-opencode/<session>.categories.json` (`writeCategoriesFile`,
  `plugin.ts`) and the selection to OpenCode's plugin storage. The verifier
  reaches into that temp directory by glob. The Claude Code flow writes its own
  file to `/tmp/ctxed-cats.json` (the skill). There is no shared notion of "the
  active selection for session X".
- **One process per dispatch.** The dispatch hook spawns `ctxed prune - …
  --ids-only` for each new transcript revision, under a 2 s timeout
  (`DEFAULT_TIMEOUT_MS`, `core.ts`). It fails open, so a slow machine silently
  stops pruning.

The coming Claude Code mod (D3b in `add-claude-code-live-prune`) needs exactly
the same three things: translate live messages, find the active selection, and
resolve the dropped set. Building them a second time in the mod would double the
surface to keep in sync.

## What Changes

- **Live formats in Go.** New input formats selectable with `--from`:
  `opencode-hook`, `opencode-context`, `claude-mod` (live `SessionMessage[]` with
  `handle` as the entry id), alongside the existing auto-detected file formats.
  `categorize -` and `prune -` accept them. `transcript.ts` is deleted.
- **Selection state in ctxed.** `ctxed select <harness> <session-id>
  --categories 1,3` (and `--clear`, and `ctxed select … --show`) persists the
  categories document and the chosen ids under
  `${XDG_STATE_HOME:-~/.local/state}/ctxed/<harness>/<session-id>/`.
  `categorize` with `--session-key <harness>/<id>` stores its output there.
  `prune --session-key <harness>/<id>` uses the stored selection. Shims no
  longer write temp files or keep selection state.
- **Optional long-lived mode.** `ctxed serve --stdio`: newline-delimited JSON
  requests (`categorize`, `select`, `prune`) over stdin/stdout, so a shim keeps
  one process and avoids a spawn per dispatch. The one-shot CLI remains and
  shares the same code path.
- **Protocol version.** `ctxed version --json` prints
  `{"version": "...", "protocol": N}`; shims check `protocol` at load.
- **OpenCode plugin slimmed** to: forward live messages, render the bucket table
  ctxed returns, record the selection through ctxed, and filter by the dropped
  ids.

## Capabilities

### New Capabilities

- `live-transcript-formats`: ctxed reads harness live-message encodings
  directly.
- `prune-selection-state`: ctxed owns the per-session categories and selection.

## Impact

- `internal/adapter/` (new live-format adapters), `internal/cli` (`--from`,
  `select`, `--session-key`, `serve`, `version --json`), new
  `internal/state/` package.
- `plugin/opencode/src/` shrinks; `transcript.ts` removed; tests move to Go
  fixtures.
- `docs/plugin-contract.md` rewritten around the new verbs.
- Verifier reads the selection with `ctxed select … --show` instead of globbing
  `$TMPDIR`.
- Depends on `add-wire-level-verification`, so the refactor is guarded at the
  wire before it starts.
