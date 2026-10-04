# OpenCode plugin API — spike findings

Status: **verified empirically** (2026-10-04) against `@opencode/plugin@2.0.22`
and OpenCode 2.0.19 on this machine. Sources: the installed `.d.ts` files and a
probe plugin run against a real session. This closes the implementation-shaping
risks in `add-opencode-dispatch-plugin`.

## What the running spike proved

A probe plugin was loaded into a real session and logged what the dispatch hook
receives:

```
SETUP keys=[app, location, options, agent, aisdk, command, event, experimental,
            generate, model, provider, integration, mcp, permission, plugin,
            reference, rpc, skill, storage, tool, vcs, websearch, worktree,
            session, shell]
session=[hook, create, get, switchAgent, switchModel, prompt, generate, command,
         synthetic, interrupt, update, move, wait, context]
HOOK REGISTERED
CONTEXT {"n":1,"first":[{"id":"msg_105d38f1f001IjW4Ck5vn7HrJT","role":"user"}]}
```

The exported `opencode session export` for the same session:

```
["msg_105d38f1f001IjW4Ck5vn7HrJT", "msg_105d3905f001DSArPDpqMZd2TO", …]
```

**Conclusion: the live message id is byte-identical to the export's message id.**
The id-parity assumption the whole id-based design rests on is now confirmed,
not assumed. Task 4.4's "switch to content/tool-id matching if not" is not
needed.

## The API surface (from the installed types)

The package ships two APIs; the plugin uses the **promise** one (`@opencode/plugin`):

- **Dispatch hook.** `ctx.session.hook("context", async (event) => { … })`
  where `event: SessionContext` extends `SessionRequest`:
  `{ sessionID, model, system: SystemPart[], messages: Message[], options }`.
  `messages` is mutable — assign `event.messages` to change the request. This is
  the seam the plugin filters on.
- **Command.** `ctx.command.transform((editor) => editor.add({ name, description,
  execute }))`, where `execute(input: CommandInvocation)` gets
  `{ sessionID, prompt, delivery }`. This is the in-session entry point for
  categorize → select.
- **Storage.** `ctx.storage.get(key) / set(key, value) / remove(key) / scan()` —
  a real key-value store, so the active selection persists per session.
- **Message.** `{ id?: string; role: "system" | "user" | "assistant" | "tool";
  content: ContentPart[] }`, where content parts are tagged
  `text | media | tool-call | tool-result | reasoning | compaction | effort`.
  Note `id` is typed optional, but is **present in practice** on the messages the
  context hook receives (proved above).

## Operational requirements (learned the hard way)

1. **A local plugin needs its own `node_modules`.** Declaring
   `@opencode/plugin` in `.opencode/package.json` is not enough; without the
   package installed, load fails with
   `Cannot find package '@opencode/plugin'`. OpenCode installs npm plugins with
   Bun at startup; a local plugin dir needs `node_modules` present.
2. **The module must default-export a definition with `id` and `setup`** (or an
   `effect`). Anything else:
   `PluginModule.LoadError: Plugin must export a default definition with an id
   and an effect or setup function`.
3. **Load failures are silent in a normal run.** They appear only with
   `--print-logs --log-level debug` (`failed to load plugin … cause=…`). Any
   plugin work must be verified with debug logging, not by "it seemed fine".
4. Local plugins load from `.opencode/plugins/` (project) or
   `~/.config/opencode/plugins/` (global). `opencode plugin list` confirms load.

## Packaging: plugins are flat files, not packages

**A plugin must be a single file directly under `.opencode/plugins/`.** A
subdirectory is *not* scanned: `.opencode/plugins/ctxed/plugin.ts` produced
`opencode plugin list` → "No plugins found". Copying the same file up to
`.opencode/plugins/ctxed-prune.ts` made it appear and load.

Consequence: a multi-file plugin (`plugin.ts` importing `./core.ts`,
`./transcript.ts`) must be **bundled to one file** before install. The plugin
build step uses `esbuild`:

```sh
esbuild src/plugin.ts --bundle --format=esm --platform=node \
  --external:@opencode/plugin --outfile=plugins/ctxed-prune.js
```

`@opencode/plugin` stays external because OpenCode resolves it from the project's
`.opencode/node_modules` at load time.

## The hook can change what the model receives

Proved with a throwaway plugin that drops one message in the `context` hook. The
token message was dropped, and on the next turn the model replied **"unknown."**
to "what token did I ask you to remember?" — so mutating `event.messages` really
does change the outbound request, and the drop persists across dispatches.

The same run showed the live message list contains entries with a **`null` id**
(assistant/tool parts). A serializer must tolerate those and an id filter must
leave them alone.

## Commands do not run under `opencode run`; they do run over the server API

`opencode run "/ctxed-prune"` sends the text to the model; it does **not**
execute the plugin command. Commands are a server/TUI surface. The programmatic
path, confirmed working:

- `GET /api/command` lists registered commands. A command registered by
  `ctx.command.transform(editor => editor.add({name, description, execute}))`
  appears there by name (**after** the plugin loads; the list is empty while the
  plugin is still being discovered).
- `POST /api/session/{sessionID}/command` with body `{name, text}` invokes it.
  `name` is the command name; `text` is the argument string.
- `execute(input)` receives `input.prompt` as an object with a **`text` field**
  (`PromptInput.Prompt`), not a bare string — read `input.prompt.text`.

The server requires auth even from localhost: HTTP Basic with username
`opencode` and the `OPENCODE_PASSWORD` value. Example:

```sh
auth=$(printf 'opencode:%s' "$OPENCODE_PASSWORD" | base64)
curl -H "Authorization: Basic $auth" \
  -X POST -H 'Content-Type: application/json' \
  -d '{"name":"ctxed-prune","text":"1"}' \
  "http://127.0.0.1:$PORT/api/session/$SID/command"
```

`GET /api/session/{sessionID}/context` returns the session messages; a prompt is
queued with `POST /api/session/{sessionID}/prompt` body
`{text, delivery}` where `delivery` is `steer` or `queue` (not `terminal`).

## The categorizer output must be the JSON, not a table

`ctxed categorize --out <path>` writes the categories file and prints a human
table. A plugin needs the JSON without managing a temp path, so
`ctxed categorize --out -` prints the categories document to stdout (the output
counterpart to the `-` stdin input).

## Impact on the change

- **D1/D2 as designed stand** — `session.hook("context")` is the seam.
- **D4 is now safer**: the plugin can re-derive the dropped set over the live
  transcript because the live ids are the export ids ctxed categorized.
- **D3 picks a concrete transport**: serialize `event.messages` to the session
  shape and pipe to `ctxed categorize -` / `ctxed prune - --ids-only` (stdin
  support added in task 1.3).
- **New task 2.x detail**: the in-session command uses `command.transform`, and
  the selection persists via `ctx.storage`.
- **Packaging is a build step**: bundle to one file; a directory does not load.
- The plugin's `package.json` must ship or install the pinned
  `@opencode/plugin`, and the functional verifier must run with debug logging so
  a load failure is visible.
