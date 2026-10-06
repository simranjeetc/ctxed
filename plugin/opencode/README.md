# ctxed — OpenCode prune (skill + plugin) — PARKED

> **Parked (2026-10-06).** Nothing ships or installs this plugin or skill, and
> `ctxed opencode` is no longer a command. OpenCode's compaction keeps a
> verbatim tail of recent messages outside the plugin's reach, so dropped
> topics came back after a compaction. The code and its tests stay here for
> reference; ctxed now offers a read-only `ctxed overview` instead. See
> `openspec/changes/context-overview`.


Drops whole topics from a running OpenCode session so the conversation continues
without them. Three parts, each doing one thing:

| Part | Does |
|---|---|
| `ctxed opencode categorize` | Exports the session, sorts what the model still sees into numbered topics |
| `ctxed opencode drop 1,3` | Adds those topics' messages to the session's drop list |
| Skill `ctxed-prune` (`skill/`) | Runs the two commands and asks you which topics to drop (multi-select) |
| Plugin (`src/`) | Before each request, removes the messages on the drop list |

```
you: "prune"
  └─ skill ─▶ ctxed opencode categorize ─▶ 1. Topic A  2. Topic B  3. Topic C
     you tick ☑1 ☑3
  └─ skill ─▶ ctxed opencode drop 1,3 ─▶ ~/.local/state/ctxed/opencode/<session>.json
next request ─▶ plugin reads the drop list ─▶ leaves out Topic A and C ─▶ model
```

## How it behaves

- **The stored session is never changed.** Only what is sent to the model shrinks.
- **Each prune sorts only what is still sent**: messages after OpenCode's last
  compaction, minus everything already dropped. A topic you return to after a
  prune shows up again with only the new messages.
- **The drop list only grows.** Picks from later prunes are added to it.
- **Messages sent after a prune are kept**, even on a dropped topic, until you
  prune again.
- **Compaction respects it.** The plugin filters the request that writes an
  OpenCode compaction summary too, so a summary does not bring dropped topics back.
- **The plugin is silent and fail-open.** It posts nothing, runs nothing, and
  sends the request unchanged if the drop list is missing or unreadable.

The session id comes from `$OPENCODE_SESSION_ID`, which OpenCode sets for every
shell command it runs, so the skill passes nothing.

## Install (global)

```sh
go install ./cmd/ctxed                                   # ctxed in ~/go/bin
cd plugin/opencode && npm install && npm run build       # dist/ctxed-prune.js
cp dist/ctxed-prune.js ~/.config/opencode/plugins/
mkdir -p ~/.config/opencode/skills/ctxed-prune
cp skill/ctxed-prune/SKILL.md ~/.config/opencode/skills/ctxed-prune/
```

`~/.config/opencode/package.json` must list the plugin API:

```json
{ "dependencies": { "@opencode/plugin": "2.0.22" } }
```

Restart OpenCode's server(s) afterwards: plugins load when a server starts. A
plugin that fails to load is silent unless you run with
`--print-logs --log-level debug`.

## Configure (optional)

| Variable | Read by | Meaning |
|---|---|---|
| `CTXED_STATE_DIR` | ctxed and plugin | Drop-list directory (default `~/.local/state/ctxed/opencode`) |
| `CTXED_CATEGORIZER_CMD` | ctxed | Command that takes the prompt on stdin and prints the model's answer |
| `CTXED_CATEGORIZER_MODEL` | ctxed | Model for the built-in `opencode run` categorizer (default `opencode-go/deepseek-v4-flash`) |
| `CTXED_OPENCODE_BIN` | ctxed | Path to `opencode` if it is not on PATH |
| `CTXED_PLUGIN_DEBUG_LOG` | plugin | Verifier only: log each request's ids before and after the filter |

## Test

```sh
npm test                                         # filter and policy tests
../../scripts/verify-functionally.sh --opencode  # live: two prunes and a compaction, codeword checks
```
