# ctxed — OpenCode dispatch plugin

Applies a [ctxed](../../README.md) prune to OpenCode's outbound transcript at
dispatch. It is non-destructive, fail-open, and holds no prune policy of its
own: it asks ctxed for the set of dropped entry ids, removes matching live
messages by id, and does nothing else.

## How it works

1. On each dispatch, OpenCode runs the plugin's `session.hook("context")`
   handler with the live message list.
2. The plugin invokes `ctxed prune <session-export> … --ids-only`, which prints
   `{"droppedIds":[…]}` — the resolved drop set, after orphan resolution.
3. It removes every live message whose `id` is in that set, preserving order and
   every other message. Stored history is never touched.

Any ctxed failure — non-zero exit, timeout, or unparseable output — leaves the
transcript unchanged and logs the error. Pruning is an optimization; it never
blocks a turn.

The dropped set is cached per selection and per session revision (the ordered
message ids), so repeated dispatches do not re-invoke ctxed.

## Requirements

- OpenCode with the v2 plugin API (`ctx.session.hook`).
- `ctxed` on `PATH`, or a configured path (see below).
- A session export produced by `opencode session export <session-id>`, and a
  selection: either a categories file plus category ids, or explicit entry ids.
  See [the plugin contract](../../docs/plugin-contract.md#id-only-prune-output).

## Package

`package.json` pins `@opencode/plugin` (the OpenCode v2 plugin package that
exposes `session.hook`). The earlier `@opencode-ai/plugin` package is the v1
API, which has no `session.hook` seam.

## Install

OpenCode loads local plugins from `.opencode/plugins/` and installs their
dependencies from `.opencode/package.json` with Bun at startup.

```sh
# from your project root
mkdir -p .opencode/plugins
cp -R <ctxed>/plugin/opencode .opencode/plugins/ctxed
```

Add the dependency to your OpenCode config directory's `package.json` (this is
the file OpenCode runs `bun install` against):

```json
{
  "dependencies": { "@opencode/plugin": "2.0.22" }
}
```

## Configure

Configuration is read from plugin options first, then from environment
variables. If the session export or the selection is missing, the plugin is a
no-op.

| Option          | Environment variable             | Meaning                                                            |
| --------------- | -------------------------------- | ------------------------------------------------------------------ |
| `ctxedPath`     | `CTXED_PLUGIN_CTXED_PATH`        | Path to the ctxed binary (default: `ctxed`, resolved on `PATH`).   |
| `sessionExport` | `CTXED_PLUGIN_SESSION_EXPORT`    | Path to `opencode session export` JSON.                            |
| `categoriesFile`| `CTXED_PLUGIN_CATEGORIES_FILE`   | Categories file from `ctxed categorize`.                           |
| `categories`    | `CTXED_PLUGIN_CATEGORIES`        | Comma-separated category ids to drop (with `categoriesFile`).      |
| `ids`           | `CTXED_PLUGIN_IDS`               | Comma-separated entry ids to drop (alternative to categories).     |
| `timeoutMs`     | `CTXED_PLUGIN_TIMEOUT_MS`        | Hard timeout per ctxed invocation (default: 2000).                 |

Plugin options in `opencode.json`:

```jsonc
{
  "plugins": [
    {
      "package": "./.opencode/plugins/ctxed",
      "options": {
        "ctxedPath": "/usr/local/bin/ctxed",
        "sessionExport": "/tmp/session.json",
        "categoriesFile": "/tmp/session.categories.json",
        "categories": "2,3"
      }
    }
  ]
}
```

## Develop

```sh
node --test test/          # offline; uses stub ctxed binaries, no network
```

The tests exercise the id filter, fail-open behavior (non-zero exit, hang,
invalid JSON), caching, configuration, and the absence of policy in the source.
